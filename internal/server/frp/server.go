package frp

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	v1 "github.com/fatedier/frp/pkg/config/v1"
	"github.com/fatedier/frp/server"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
)

// ConnectionType 连接类型
type ConnectionType string

const (
	ConnectionTypeAgent   ConnectionType = "agent"
	ConnectionTypeDesktop ConnectionType = "desktop"
)

// ConnectionInfo 连接信息
type ConnectionInfo struct {
	Type        ConnectionType // 连接类型（agent或desktop）
	Name        string         // Agent名称或Desktop ID
	ConnectedAt time.Time      // 连接时间
	LastActive  time.Time      // 最后活跃时间
}

// ProxyInfo 代理信息
type ProxyInfo struct {
	Name      string    // 代理名称（STCP实例名）
	Type      string    // 代理类型（stcp）
	AgentName string    // 所属Agent
	CreatedAt time.Time // 创建时间
	Status    string    // 状态（active/inactive）
}

// FRPServer FRP服务器（Server-FRP线程）
type FRPServer struct {
	config *config.ServerConfig
	svr    *server.Service

	// 连接管理
	connections map[string]*ConnectionInfo // name -> connection info
	connMutex   sync.RWMutex

	// 代理管理
	proxies    map[string]*ProxyInfo // proxy_name -> proxy info
	proxyMutex sync.RWMutex

	// 上下文
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewFRPServer 创建FRP服务器
func NewFRPServer(cfg *config.ServerConfig) (*FRPServer, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// 创建FRP Server配置
	svrCfg := &v1.ServerConfig{
		BindAddr: cfg.Server.BindAddr,
		BindPort: cfg.Server.BindPort,
	}

	// 完成配置（填充默认值）- 这是关键步骤！
	if err := svrCfg.Complete(); err != nil {
		cancel()
		return nil, fmt.Errorf("完成FRP Server配置失败: %w", err)
	}
	log.Println("FRP Server 配置已完成（默认值已填充）")

	// 配置 FRP 认证
	if cfg.Server.Token != "" {
		svrCfg.Auth.Method = v1.AuthMethod("token")
		svrCfg.Auth.Token = cfg.Server.Token
		log.Printf("FRP Server 认证已启用: token=%s...", cfg.Server.Token[:min(16, len(cfg.Server.Token))])
	} else {
		log.Println("FRP Server 认证未启用（不推荐用于生产环境）")
	}

	// 配置TLS
	if cfg.Server.TLSCertFile != "" && cfg.Server.TLSKeyFile != "" {
		svrCfg.Transport.TLS = v1.TLSServerConfig{
			Force: true, // 强制使用TLS
			TLSConfig: v1.TLSConfig{
				CertFile: cfg.Server.TLSCertFile,
				KeyFile:  cfg.Server.TLSKeyFile,
			},
		}
		log.Println("FRP Server TLS已启用")
	}

	// 注意：FRP Server 自动支持所有传输协议（TCP, KCP, QUIC, WebSocket, WSS）
	// 通过 muxer 在同一端口上复用，无需额外配置
	log.Println("FRP Server 将自动支持所有传输协议（TCP, WebSocket 等）")

	// 创建FRP Server实例
	svr, err := server.NewService(svrCfg)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("创建FRP Server失败: %w", err)
	}

	return &FRPServer{
		config:      cfg,
		svr:         svr,
		connections: make(map[string]*ConnectionInfo),
		proxies:     make(map[string]*ProxyInfo),
		ctx:         ctx,
		cancel:      cancel,
	}, nil
}

// Run 运行FRP服务器
func (f *FRPServer) Run() error {
	log.Printf("FRP Server启动在: %s:%d", f.config.Server.BindAddr, f.config.Server.BindPort)
	log.Printf("传输协议: %s", f.config.Server.TransportProtocol)

	// 启动连接监控
	f.wg.Add(1)
	go f.monitorConnections()

	// 启动FRP Server（会阻塞直到context取消）
	f.svr.Run(f.ctx)

	// 等待所有goroutine结束
	f.wg.Wait()

	return nil
}

// Stop 停止FRP服务器
func (f *FRPServer) Stop() error {
	log.Println("正在停止FRP Server...")
	f.cancel()
	f.wg.Wait()
	log.Println("FRP Server已停止")
	return nil
}

// monitorConnections 监控连接状态
func (f *FRPServer) monitorConnections() {
	defer f.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			f.logConnectionStats()

		case <-f.ctx.Done():
			return
		}
	}
}

// logConnectionStats 记录连接统计
func (f *FRPServer) logConnectionStats() {
	f.connMutex.RLock()
	agentCount := 0
	desktopCount := 0
	for _, conn := range f.connections {
		if conn.Type == ConnectionTypeAgent {
			agentCount++
		} else {
			desktopCount++
		}
	}
	f.connMutex.RUnlock()

	f.proxyMutex.RLock()
	proxyCount := len(f.proxies)
	f.proxyMutex.RUnlock()

	log.Printf("FRP连接统计: Agent=%d, Desktop=%d, Proxy=%d",
		agentCount, desktopCount, proxyCount)
}

// RegisterConnection 注册连接（由FRP事件触发）
func (f *FRPServer) RegisterConnection(name string, connType ConnectionType) {
	f.connMutex.Lock()
	defer f.connMutex.Unlock()

	now := time.Now()
	f.connections[name] = &ConnectionInfo{
		Type:        connType,
		Name:        name,
		ConnectedAt: now,
		LastActive:  now,
	}

	log.Printf("FRP连接已注册: %s (类型: %s)", name, connType)
}

// UnregisterConnection 注销连接
func (f *FRPServer) UnregisterConnection(name string) {
	f.connMutex.Lock()
	defer f.connMutex.Unlock()

	if conn, exists := f.connections[name]; exists {
		delete(f.connections, name)
		log.Printf("FRP连接已注销: %s (类型: %s)", name, conn.Type)
	}
}

// UpdateConnectionActivity 更新连接活跃时间
func (f *FRPServer) UpdateConnectionActivity(name string) {
	f.connMutex.Lock()
	defer f.connMutex.Unlock()

	if conn, exists := f.connections[name]; exists {
		conn.LastActive = time.Now()
	}
}

// RegisterProxy 注册代理
func (f *FRPServer) RegisterProxy(name, proxyType, agentName string) {
	f.proxyMutex.Lock()
	defer f.proxyMutex.Unlock()

	f.proxies[name] = &ProxyInfo{
		Name:      name,
		Type:      proxyType,
		AgentName: agentName,
		CreatedAt: time.Now(),
		Status:    "active",
	}

	log.Printf("FRP代理已注册: %s (类型: %s, Agent: %s)", name, proxyType, agentName)
}

// UnregisterProxy 注销代理
func (f *FRPServer) UnregisterProxy(name string) {
	f.proxyMutex.Lock()
	defer f.proxyMutex.Unlock()

	if proxy, exists := f.proxies[name]; exists {
		delete(f.proxies, name)
		log.Printf("FRP代理已注销: %s (Agent: %s)", name, proxy.AgentName)
	}
}

// GetConnectionInfo 获取连接信息
func (f *FRPServer) GetConnectionInfo(name string) (*ConnectionInfo, bool) {
	f.connMutex.RLock()
	defer f.connMutex.RUnlock()

	conn, exists := f.connections[name]
	return conn, exists
}

// GetProxyInfo 获取代理信息
func (f *FRPServer) GetProxyInfo(name string) (*ProxyInfo, bool) {
	f.proxyMutex.RLock()
	defer f.proxyMutex.RUnlock()

	proxy, exists := f.proxies[name]
	return proxy, exists
}

// GetAllConnections 获取所有连接
func (f *FRPServer) GetAllConnections() map[string]*ConnectionInfo {
	f.connMutex.RLock()
	defer f.connMutex.RUnlock()

	// 返回副本
	connections := make(map[string]*ConnectionInfo, len(f.connections))
	for k, v := range f.connections {
		connCopy := *v
		connections[k] = &connCopy
	}
	return connections
}

// GetAllProxies 获取所有代理
func (f *FRPServer) GetAllProxies() map[string]*ProxyInfo {
	f.proxyMutex.RLock()
	defer f.proxyMutex.RUnlock()

	// 返回副本
	proxies := make(map[string]*ProxyInfo, len(f.proxies))
	for k, v := range f.proxies {
		proxyCopy := *v
		proxies[k] = &proxyCopy
	}
	return proxies
}

// IsAgentConnected 检查Agent是否已连接
func (f *FRPServer) IsAgentConnected(agentName string) bool {
	f.connMutex.RLock()
	defer f.connMutex.RUnlock()

	conn, exists := f.connections[agentName]
	return exists && conn.Type == ConnectionTypeAgent
}

// IsProxyActive 检查代理是否活跃
func (f *FRPServer) IsProxyActive(proxyName string) bool {
	f.proxyMutex.RLock()
	defer f.proxyMutex.RUnlock()

	proxy, exists := f.proxies[proxyName]
	return exists && proxy.Status == "active"
}

// IsRunning 检查FRP Server是否正在运行
func (f *FRPServer) IsRunning() bool {
	select {
	case <-f.ctx.Done():
		return false
	default:
		return true
	}
}
