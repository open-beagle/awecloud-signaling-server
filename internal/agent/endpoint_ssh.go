// Package agent 提供 Agent 端功能
// endpoint_ssh.go 实现 Endpoint SSH 代理
// Agent 作为 SSH Server 接收 Desktop 连接，提取用户名后通过 gRPC 转发到 Endpoint
//
// 架构：
//
//	Desktop → tsnet → Agent(FallbackTCPHandler, 端口 50053+N) → SSH 握手 → gRPC OpenShell → Endpoint PTY
//	每个 Endpoint 分配一个独立端口（50053 起），Agent 根据端口号确定目标 Endpoint
//	Desktop 端无需修改，Server ResolveDomain 返回 agent_ip:分配端口 即可
package agent

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

// EndpointSSHPortBase Endpoint SSH 代理的起始端口
// 每个 Endpoint 分配一个端口：50053, 50054, 50055, ...
const EndpointSSHPortBase = 50053

// EndpointSSHProxy Endpoint SSH 代理
// 在 Agent 内部运行 SSH Server，接收 Desktop SSH 连接
// 从 SSH 握手中提取 login 用户名，然后通过 gRPC 转发到 Endpoint
type EndpointSSHProxy struct {
	endpointServer *EndpointServer
	tsManager      *TailscaleManager
	auditCollector *AuditCollector
	sshConfig      *ssh.ServerConfig
	hostKey        ssh.Signer

	// 端口 → Endpoint 名称映射
	portMap map[uint16]string
	// Endpoint 名称 → 端口映射
	nameMap map[string]uint16
	mapMu   sync.RWMutex

	// 下一个可分配的端口
	nextPort uint16

	// fallback handler 取消注册函数
	deregisterFallback func()

	ctx    context.Context
	cancel context.CancelFunc
}

// NewEndpointSSHProxy 创建 Endpoint SSH 代理
func NewEndpointSSHProxy(endpointServer *EndpointServer, tsManager *TailscaleManager, auditCollector *AuditCollector, parentCtx context.Context) (*EndpointSSHProxy, error) {
	// 生成临时 Ed25519 主机密钥（每次启动重新生成，不需要持久化）
	_, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("生成主机密钥失败: %w", err)
	}

	hostKey, err := ssh.NewSignerFromKey(privKey)
	if err != nil {
		return nil, fmt.Errorf("创建 SSH Signer 失败: %w", err)
	}

	ctx, cancel := context.WithCancel(parentCtx)

	proxy := &EndpointSSHProxy{
		endpointServer: endpointServer,
		tsManager:      tsManager,
		auditCollector: auditCollector,
		hostKey:        hostKey,
		portMap:        make(map[uint16]string),
		nameMap:        make(map[string]uint16),
		nextPort:       EndpointSSHPortBase,
		ctx:            ctx,
		cancel:         cancel,
	}

	// 配置 SSH Server（不做客户端认证，信任 Tailscale 隧道）
	proxy.sshConfig = &ssh.ServerConfig{
		NoClientAuth: true,
	}
	proxy.sshConfig.AddHostKey(hostKey)

	return proxy, nil
}

// Start 启动 Endpoint SSH 代理
// 注册 FallbackTCPHandler，拦截端口范围 [50053, 50153) 的连接
func (p *EndpointSSHProxy) Start() error {
	// 注册 fallback TCP handler，拦截 Endpoint SSH 端口范围的连接
	deregister := p.tsManager.RegisterFallbackTCPHandler(func(src, dst netip.AddrPort) (func(net.Conn), bool) {
		port := dst.Port()
		if port >= EndpointSSHPortBase && port < EndpointSSHPortBase+100 {
			// 查找端口对应的 Endpoint
			p.mapMu.RLock()
			endpointName, exists := p.portMap[port]
			p.mapMu.RUnlock()

			if !exists {
				return nil, false
			}

			return func(conn net.Conn) {
				go p.HandleConn(p.ctx, conn, endpointName)
			}, true
		}
		return nil, false
	})
	p.deregisterFallback = deregister

	logger.Infof("[EndpointSSH] 代理已启动（FallbackTCPHandler）: 端口范围 %d-%d", EndpointSSHPortBase, EndpointSSHPortBase+99)
	return nil
}

// Stop 停止 Endpoint SSH 代理
func (p *EndpointSSHProxy) Stop() {
	p.cancel()
	if p.deregisterFallback != nil {
		p.deregisterFallback()
	}
	logger.Info("[EndpointSSH] 代理已停止")
}

// AllocatePort 为 Endpoint 分配端口
// 如果已分配则返回已有端口，否则分配新端口
func (p *EndpointSSHProxy) AllocatePort(endpointName string) uint16 {
	p.mapMu.Lock()
	defer p.mapMu.Unlock()

	// 已分配
	if port, exists := p.nameMap[endpointName]; exists {
		return port
	}

	// 分配新端口
	port := p.nextPort
	p.nextPort++
	p.portMap[port] = endpointName
	p.nameMap[endpointName] = port

	logger.Infof("[EndpointSSH] 分配端口: %s → %d", endpointName, port)
	return port
}

// ReleasePort 释放 Endpoint 的端口
func (p *EndpointSSHProxy) ReleasePort(endpointName string) {
	p.mapMu.Lock()
	defer p.mapMu.Unlock()

	if port, exists := p.nameMap[endpointName]; exists {
		delete(p.portMap, port)
		delete(p.nameMap, endpointName)
		logger.Infof("[EndpointSSH] 释放端口: %s ← %d", endpointName, port)
	}
}

// GetPort 获取 Endpoint 的分配端口（0 表示未分配）
func (p *EndpointSSHProxy) GetPort(endpointName string) uint16 {
	p.mapMu.RLock()
	defer p.mapMu.RUnlock()
	return p.nameMap[endpointName]
}

// HandleConn 处理 Desktop SSH 连接
// 运行 SSH 握手，提取用户名，请求 Endpoint 开启 shell，桥接 I/O
func (p *EndpointSSHProxy) HandleConn(ctx context.Context, conn net.Conn, endpointName string) {
	defer conn.Close()
	startedAt := time.Now()

	// 尝试通过 tsnet WhoIs 提取 Desktop 用户身份
	clientUserName := ""
	if p.tsManager != nil {
		if lc, err := p.tsManager.LocalClient(); err == nil {
			if whois, err := lc.WhoIs(ctx, conn.RemoteAddr().String()); err == nil && whois.UserProfile != nil {
				clientUserName, _ = parseHeadscaleUserName(whois.UserProfile.LoginName)
			}
		}
	}

	// 检查 Endpoint 是否在线
	if !p.endpointServer.IsEndpointConnected(endpointName) {
		logger.Warnf("[EndpointSSH] Endpoint 不在线: %s", endpointName)
		return
	}

	// SSH 握手
	sshConn, chans, reqs, err := ssh.NewServerConn(conn, p.sshConfig)
	if err != nil {
		logger.Warnf("[EndpointSSH] SSH 握手失败: %v", err)
		return
	}
	defer sshConn.Close()

	// 提取登录用户名
	login := sshConn.User()
	logger.Infof("[EndpointSSH] SSH 连接: endpoint=%s, login=%s, client=%s", endpointName, login, clientUserName)

	// 丢弃全局请求
	go ssh.DiscardRequests(reqs)

	// 等待 session channel
	for newChannel := range chans {
		if newChannel.ChannelType() != "session" {
			newChannel.Reject(ssh.UnknownChannelType, "不支持的 channel 类型")
			continue
		}

		channel, requests, err := newChannel.Accept()
		if err != nil {
			logger.Warnf("[EndpointSSH] 接受 channel 失败: %v", err)
			return
		}

		// 处理 session（只处理第一个）
		p.handleSession(ctx, channel, requests, endpointName, login)

		// 会话结束，记录审计
		if p.auditCollector != nil {
			endedAt := time.Now()
			target := fmt.Sprintf("%s@%s", login, endpointName)
			p.auditCollector.Record(
				clientUserName,
				endpointName,
				"ssh_session",
				target,
				"",
				startedAt,
				endedAt,
			)
		}
		return
	}
}

// handleSession 处理 SSH session channel
// 等待 pty-req 和 shell 请求，然后请求 Endpoint 开启 shell 并桥接 I/O
func (p *EndpointSSHProxy) handleSession(ctx context.Context, channel ssh.Channel, requests <-chan *ssh.Request, endpointName, login string) {
	defer channel.Close()

	var rows, cols uint32 = 24, 80 // 默认终端大小
	var shellStream pb.EndpointService_OpenShellServer
	shellReady := make(chan struct{})
	shellErr := make(chan error, 1)

	// 处理 SSH 请求（pty-req, shell, window-change 等）
	go func() {
		ptyReceived := false
		for req := range requests {
			switch req.Type {
			case "pty-req":
				// 解析终端大小
				// pty-req payload: string term, uint32 cols, uint32 rows, ...
				if len(req.Payload) >= 4 {
					termLen := int(req.Payload[3]) | int(req.Payload[2])<<8 | int(req.Payload[1])<<16 | int(req.Payload[0])<<24
					if len(req.Payload) >= 4+termLen+8 {
						offset := 4 + termLen
						cols = uint32(req.Payload[offset])<<24 | uint32(req.Payload[offset+1])<<16 | uint32(req.Payload[offset+2])<<8 | uint32(req.Payload[offset+3])
						rows = uint32(req.Payload[offset+4])<<24 | uint32(req.Payload[offset+5])<<16 | uint32(req.Payload[offset+6])<<8 | uint32(req.Payload[offset+7])
					}
				}
				ptyReceived = true
				if req.WantReply {
					req.Reply(true, nil)
				}

			case "shell":
				if req.WantReply {
					req.Reply(true, nil)
				}
				if !ptyReceived {
					rows, cols = 24, 80
				}
				stream, err := p.endpointServer.RequestShell(ctx, endpointName, login, rows, cols)
				if err != nil {
					logger.Warnf("[EndpointSSH] 请求 Endpoint shell 失败: %v", err)
					shellErr <- err
					return
				}
				shellStream = stream
				close(shellReady)

			case "window-change":
				if len(req.Payload) >= 8 {
					newCols := uint32(req.Payload[0])<<24 | uint32(req.Payload[1])<<16 | uint32(req.Payload[2])<<8 | uint32(req.Payload[3])
					newRows := uint32(req.Payload[4])<<24 | uint32(req.Payload[5])<<16 | uint32(req.Payload[6])<<8 | uint32(req.Payload[7])
					if shellStream != nil {
						shellStream.Send(&pb.ShellData{
							IsResize: true,
							Rows:     newRows,
							Cols:     newCols,
						})
					}
				}
				if req.WantReply {
					req.Reply(true, nil)
				}

			default:
				if req.WantReply {
					req.Reply(false, nil)
				}
			}
		}
	}()

	// 等待 shell 就绪
	select {
	case <-shellReady:
	case err := <-shellErr:
		logger.Warnf("[EndpointSSH] Shell 未就绪: %v", err)
		return
	case <-ctx.Done():
		return
	}

	// 双向桥接：SSH channel ↔ gRPC Shell 流
	var wg sync.WaitGroup

	// SSH channel → gRPC（Desktop stdin → Endpoint）
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 32*1024)
		for {
			n, err := channel.Read(buf)
			if n > 0 {
				if sendErr := shellStream.Send(&pb.ShellData{
					Data: buf[:n],
				}); sendErr != nil {
					return
				}
			}
			if err != nil {
				shellStream.Send(&pb.ShellData{IsClose: true})
				return
			}
		}
	}()

	// gRPC → SSH channel（Endpoint stdout → Desktop）
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			msg, err := shellStream.Recv()
			if err != nil {
				channel.Close()
				return
			}
			if msg.IsClose {
				exitMsg := ssh.Marshal(struct{ Status uint32 }{uint32(msg.ExitCode)})
				channel.SendRequest("exit-status", false, exitMsg)
				return
			}
			if len(msg.Data) > 0 {
				if _, writeErr := channel.Write(msg.Data); writeErr != nil {
					return
				}
			}
		}
	}()

	wg.Wait()
}
