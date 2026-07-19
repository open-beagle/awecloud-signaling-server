package agent

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"

	"golang.org/x/crypto/ssh"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
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
	tsManager   *TailscaleManager
	permissions *PermissionCache
	broker      *ContainerExecBroker
	sessions    *ContainerSessionManager
	identity    peerIdentityExtractor
	sshConfig   *ssh.ServerConfig
	deregister  func()
	ctx         context.Context
	cancel      context.CancelFunc
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

func (p *ContainerSSHProxy) Start() error {
	if p == nil || p.tsManager == nil {
		return fmt.Errorf("ContainerSSH proxy is not configured")
	}
	p.deregister = p.tsManager.RegisterFallbackTCPHandler(func(_ netip.AddrPort, dst netip.AddrPort) (func(net.Conn), bool) {
		port := dst.Port()
		if port < ContainerSSHPortBase || port > ContainerSSHPortEnd {
			return nil, false
		}
		resourceID, ok := p.permissions.ResolveContainerSSHRoute(port)
		if !ok {
			return nil, false
		}
		return func(conn net.Conn) {
			go p.HandleConn(p.ctx, conn, resourceID)
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
	defer conn.Close()
	identity, err := p.identity.ExtractFromConn(ctx, conn.RemoteAddr())
	if err != nil || identity == nil || identity.Role != "client" || identity.UserName == "" {
		logger.Warnf("[ContainerSSH] 无法确认 Desktop 身份，拒绝连接: err=%v", err)
		return
	}
	permission, allowed := p.permissions.CheckContainerSSHAccess(identity.UserName, resourceID)
	if !allowed {
		logger.Warnf("[ContainerSSH] 用户无权访问资源: user=%s resource=%s", identity.UserName, resourceID)
		return
	}

	sshConn, channels, requests, err := ssh.NewServerConn(conn, p.sshConfig)
	if err != nil {
		logger.Warnf("[ContainerSSH] SSH 握手失败: resource=%s err=%v", resourceID, err)
		return
	}
	defer sshConn.Close()
	go ssh.DiscardRequests(requests)

	for newChannel := range channels {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "unsupported channel type")
			continue
		}
		channel, channelRequests, err := newChannel.Accept()
		if err != nil {
			return
		}
		sessionID, sessionCtx := p.sessions.Begin(ctx, identity, permission)
		err = ServeContainerSSHSession(sessionCtx, channel, channelRequests, p.broker, identity.UserName, resourceID)
		result, reason := containerSessionOutcome(err)
		p.sessions.End(sessionID, result, reason)
		if err != nil {
			logger.Warnf("[ContainerSSH] 会话结束: user=%s resource=%s err=%v", identity.UserName, resourceID, err)
		}
		return
	}
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
