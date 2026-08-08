package agent

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

const (
	ContainerSSHPortBase uint16 = 50200
	ContainerSSHPortEnd  uint16 = 51199
)

type peerIdentityExtractor interface {
	ExtractFromConn(context.Context, net.Addr) (*PeerIdentity, error)
}

// ContainerSSHProxy exposes raw SSH on Agent-scoped ports. The port resolves
// a Resource only through the last authoritative Server snapshot.
type ContainerSSHProxy struct {
	tsManager      *TailscaleManager
	permissions    *PermissionCache
	authorizations *SessionAuthorizationCache
	broker         *ContainerExecBroker
	sessions       *ContainerSessionManager
	identity       peerIdentityExtractor
	sshConfig      *ssh.ServerConfig
	deregister     func()
	ctx            context.Context
	cancel         context.CancelFunc
}

func NewContainerSSHProxy(tsManager *TailscaleManager, permissions *PermissionCache, broker *ContainerExecBroker, sessions *ContainerSessionManager, stateDir string, parentCtx context.Context) (*ContainerSSHProxy, error) {
	if tsManager == nil || permissions == nil || broker == nil || sessions == nil {
		return nil, fmt.Errorf("ContainerSSH proxy dependencies are required")
	}
	identity, err := NewIdentityExtractor(tsManager)
	if err != nil {
		return nil, err
	}
	privKey, err := loadOrGenerateHostKey(stateDir)
	if err != nil {
		return nil, fmt.Errorf("load ContainerSSH host key: %w", err)
	}
	hostKey, err := ssh.NewSignerFromKey(privKey)
	if err != nil {
		return nil, fmt.Errorf("create ContainerSSH host signer: %w", err)
	}
	ctx, cancel := context.WithCancel(parentCtx)
	config := &ssh.ServerConfig{NoClientAuth: true}
	config.AddHostKey(hostKey)
	return &ContainerSSHProxy{
		tsManager: tsManager, permissions: permissions, broker: broker, sessions: sessions,
		identity: identity, sshConfig: config, ctx: ctx, cancel: cancel,
	}, nil
}

func (p *ContainerSSHProxy) SetSessionAuthorizations(authorizations *SessionAuthorizationCache) {
	p.authorizations = authorizations
}

func (p *ContainerSSHProxy) Start() error {
	if p == nil || p.tsManager == nil {
		return fmt.Errorf("ContainerSSH proxy is not configured")
	}
	p.deregister = p.tsManager.RegisterFallbackTCPHandler(func(_ netip.AddrPort, dst netip.AddrPort) (func(net.Conn), bool) {
		port := dst.Port()
		if port < ContainerSSHPortBase || port > ContainerSSHPortEnd {
			return nil, false
		}
		resourceID, legacyRoute := p.permissions.ResolveContainerSSHRoute(port)
		v2Route := p.authorizations != nil && p.authorizations.HasContainerSSHRoute(port, time.Now().UTC())
		if p.authorizations != nil && p.authorizations.EnforceV2() {
			legacyRoute = false
		}
		if !legacyRoute && !v2Route {
			return nil, false
		}
		return func(conn net.Conn) {
			go p.handleConn(p.ctx, conn, port, resourceID)
		}, true
	})
	logger.Infof("[ContainerSSH] 入口已启动: 端口范围 %d-%d", ContainerSSHPortBase, ContainerSSHPortEnd)
	return nil
}

func (p *ContainerSSHProxy) Stop() {
	if p == nil {
		return
	}
	p.cancel()
	if p.deregister != nil {
		p.deregister()
	}
}

func (p *ContainerSSHProxy) HandleConn(ctx context.Context, conn net.Conn, resourceID string) {
	p.handleConn(ctx, conn, 0, resourceID)
}

func (p *ContainerSSHProxy) handleConn(ctx context.Context, conn net.Conn, listenPort uint16, legacyResourceID string) {
	defer conn.Close()
	identity, err := p.identity.ExtractFromConn(ctx, conn.RemoteAddr())
	if err != nil || identity == nil || identity.Role != "client" || identity.UserName == "" {
		logger.Warnf("[ContainerSSH] 无法确认 Desktop 身份，拒绝连接: err=%v", err)
		return
	}
	var v2Permission *pb.ResourceSessionPermissionV2
	if listenPort != 0 && p.authorizations != nil {
		v2Permission, _ = p.authorizations.ResolveContainerSSH(listenPort, identity.UserName, identity.NodeID, time.Now().UTC())
	}
	var legacyPermission *ContainerSSHUserPermission
	if v2Permission == nil {
		if p.authorizations != nil && p.authorizations.EnforceV2() {
			logger.Warnf("[ContainerSSH] v2 权限不可用，禁止回落旧授权: user=%s node_id=%d port=%d", identity.UserName, identity.NodeID, listenPort)
			return
		}
		var allowed bool
		legacyPermission, allowed = p.permissions.CheckContainerSSHAccess(identity.UserName, legacyResourceID)
		if !allowed {
			logger.Warnf("[ContainerSSH] 用户或设备无权访问入口: user=%s node_id=%d port=%d", identity.UserName, identity.NodeID, listenPort)
			return
		}
	}
	resourceID := legacyResourceID
	if v2Permission != nil {
		resourceID = v2Permission.ResourceId
	}

	sshConn, channels, requests, err := ssh.NewServerConn(conn, p.sshConfig)
	if err != nil {
		logger.Warnf("[ContainerSSH] SSH 握手失败: resource=%s err=%v", resourceID, err)
		return
	}
	defer sshConn.Close()
	go ssh.DiscardRequests(requests)
	sshUser := strings.TrimSpace(sshConn.User())
	if v2Permission != nil && !containsContainerSSHUser(v2Permission.SshUsers, sshUser) {
		logger.Warnf("[ContainerSSH] SSH 用户未由 Agent 发现，拒绝连接: desktop_user=%s ssh_user=%s resource=%s allowed=%v", identity.UserName, sshUser, resourceID, v2Permission.SshUsers)
		return
	}
	logger.Infof("[ContainerSSH] SSH 握手完成: desktop_user=%s ssh_user=%s resource=%s", identity.UserName, sshUser, resourceID)

	for newChannel := range channels {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "unsupported channel type")
			continue
		}
		channel, channelRequests, err := newChannel.Accept()
		if err != nil {
			return
		}
		if v2Permission != nil {
			sessionCtx, beginErr := p.sessions.BeginV2(ctx, v2Permission)
			if beginErr != nil {
				_ = channel.Close()
				logger.Warnf("[ContainerSSH] v2 会话注册失败: session_id=%s err=%v", v2Permission.SessionId, beginErr)
				return
			}
			opener := &authorizedContainerShellOpener{broker: p.broker, authorizations: p.authorizations, permission: v2Permission}
			err = ServeContainerSSHSession(sessionCtx, channel, channelRequests, opener, identity.UserName, resourceID)
			result, reason := containerSessionOutcome(err)
			if endErr := p.sessions.EndV2(v2Permission.SessionId, result, reason); endErr != nil {
				logger.Errorf("[ContainerSSH] v2 会话事件持久化失败: session_id=%s err=%v", v2Permission.SessionId, endErr)
			}
		} else {
			sessionID, sessionCtx := p.sessions.Begin(ctx, identity, legacyPermission)
			err = ServeContainerSSHSession(sessionCtx, channel, channelRequests, p.broker, identity.UserName, resourceID)
			result, reason := containerSessionOutcome(err)
			p.sessions.End(sessionID, result, reason)
		}
		if err != nil {
			logger.Warnf("[ContainerSSH] 会话结束: user=%s resource=%s err=%v", identity.UserName, resourceID, err)
		}
		return
	}
}

func containsContainerSSHUser(users []string, requested string) bool {
	if requested == "" {
		return false
	}
	for _, user := range users {
		if user == requested {
			return true
		}
	}
	return false
}

type authorizedContainerShellOpener struct {
	broker         *ContainerExecBroker
	authorizations *SessionAuthorizationCache
	permission     *pb.ResourceSessionPermissionV2
}

func (o *authorizedContainerShellOpener) OpenShell(ctx context.Context, userName, resourceID string, stream ContainerExecStream) error {
	if o == nil || o.permission == nil || o.permission.UserName != userName || o.permission.ResourceId != resourceID {
		return fmt.Errorf("ContainerSSH access denied")
	}
	return o.broker.OpenAuthorizedShell(ctx, o.authorizations, o.permission, stream)
}

func containerSessionOutcome(err error) (string, string) {
	if err == nil {
		return "success", "shell_exited"
	}
	message := err.Error()
	switch {
	case strings.Contains(message, "target no longer exists"), strings.Contains(message, "Pod UID changed"), strings.Contains(message, "container is not ready"):
		return "failed", "target_gone"
	case strings.Contains(message, "access denied"):
		return "rejected", "grant_revoked"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "ended", "context_canceled"
	default:
		return "failed", "exec_failed"
	}
}
