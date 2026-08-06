package agent

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
)

type nodeSSHRegisterFunc func(func(src, dst netip.AddrPort) (func(net.Conn), bool)) func()
type nodeSSHDialFunc func(context.Context, string, string) (net.Conn, error)

// NodeSSHProxy exposes a non-default host sshd port on the Agent tsnet IP.
// Port 22 remains handled by Tailscale SSH.
type NodeSSHProxy struct {
	listenPort uint16
	targetAddr string
	register   nodeSSHRegisterFunc
	dial       nodeSSHDialFunc
	ctx        context.Context
	cancel     context.CancelFunc

	deregister func()
	mu         sync.Mutex
	conns      []net.Conn
}

func NewNodeSSHProxy(tsManager *TailscaleManager, listenPort uint16, parent context.Context) *NodeSSHProxy {
	ctx, cancel := context.WithCancel(parent)
	return &NodeSSHProxy{
		listenPort: listenPort,
		targetAddr: fmt.Sprintf("127.0.0.1:%d", listenPort),
		register:   tsManager.RegisterFallbackTCPHandler,
		dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		},
		ctx:    ctx,
		cancel: cancel,
	}
}

func (p *NodeSSHProxy) Start() error {
	if p == nil {
		return nil
	}
	if p.listenPort == 0 || p.listenPort == 22 {
		logger.Infof("[NodeSSH] 使用 Tailscale SSH 默认端口: %d", p.listenPort)
		return nil
	}
	if p.register == nil || p.dial == nil {
		return fmt.Errorf("NodeSSH proxy is not configured")
	}
	p.deregister = p.register(func(_ netip.AddrPort, dst netip.AddrPort) (func(net.Conn), bool) {
		if dst.Port() != p.listenPort {
			return nil, false
		}
		return func(conn net.Conn) {
			go p.handleConn(conn)
		}, true
	})
	logger.Infof("[NodeSSH] 代理已启动: tsnet:%d -> %s", p.listenPort, p.targetAddr)
	return nil
}

func (p *NodeSSHProxy) Stop() {
	if p == nil {
		return
	}
	if p.cancel != nil {
		p.cancel()
	}
	if p.deregister != nil {
		p.deregister()
	}
	p.mu.Lock()
	for _, conn := range p.conns {
		_ = conn.Close()
	}
	p.conns = nil
	p.mu.Unlock()
	logger.Info("[NodeSSH] 代理已停止")
}

func (p *NodeSSHProxy) handleConn(client net.Conn) {
	defer client.Close()
	ctx, cancel := context.WithTimeout(p.ctx, 10*time.Second)
	target, err := p.dial(ctx, "tcp", p.targetAddr)
	cancel()
	if err != nil {
		logger.Warnf("[NodeSSH] 连接本机 SSH 失败: target=%s err=%v", p.targetAddr, err)
		return
	}
	defer target.Close()

	p.track(client, target)
	defer p.untrack(client, target)

	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(target, client)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(client, target)
		done <- struct{}{}
	}()
	<-done
}

func (p *NodeSSHProxy) track(conns ...net.Conn) {
	p.mu.Lock()
	p.conns = append(p.conns, conns...)
	p.mu.Unlock()
}

func (p *NodeSSHProxy) untrack(conns ...net.Conn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, target := range conns {
		for i, conn := range p.conns {
			if conn == target {
				p.conns = append(p.conns[:i], p.conns[i+1:]...)
				break
			}
		}
	}
}
