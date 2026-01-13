// Package agent 提供 Agent 端功能
package agent

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
)

// ProxyManager 管理 TCP 端口代理
type ProxyManager struct {
	proxies   map[string]*TCPProxyService
	tsManager *TailscaleManager
	mutex     sync.RWMutex
	ctx       context.Context
}

// TCPProxyService TCP 代理服务
type TCPProxyService struct {
	Name       string
	ListenPort int
	TargetAddr string
	Listener   net.Listener
	Status     string // running/stopped/error
	ErrorMsg   string

	// 统计信息
	Connections int64
	BytesIn     int64
	BytesOut    int64
	StartedAt   time.Time

	// 内部管理
	ctx    context.Context
	cancel context.CancelFunc
	conns  []net.Conn
	mutex  sync.Mutex
}

// ProxyStatus 代理状态
type ProxyStatus struct {
	Name        string `json:"name"`
	ListenPort  int    `json:"listen_port"`
	TargetAddr  string `json:"target_addr"`
	Status      string `json:"status"`
	Connections int64  `json:"connections"`
	BytesIn     int64  `json:"bytes_in"`
	BytesOut    int64  `json:"bytes_out"`
	ErrorMsg    string `json:"error_msg,omitempty"`
}

// NewProxyManager 创建 ProxyManager
func NewProxyManager(tsManager *TailscaleManager, ctx context.Context) *ProxyManager {
	return &ProxyManager{
		proxies:   make(map[string]*TCPProxyService),
		tsManager: tsManager,
		ctx:       ctx,
	}
}

// Start 启动端口代理
func (m *ProxyManager) Start(name string, listenPort int, targetAddr string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// 检查是否已存在
	if _, exists := m.proxies[name]; exists {
		return fmt.Errorf("代理 %s 已存在", name)
	}

	// 在 Tailscale IP 上监听端口
	addr := fmt.Sprintf(":%d", listenPort)
	listener, err := m.tsManager.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("监听端口 %d 失败: %w", listenPort, err)
	}

	ctx, cancel := context.WithCancel(m.ctx)

	proxy := &TCPProxyService{
		Name:       name,
		ListenPort: listenPort,
		TargetAddr: targetAddr,
		Listener:   listener,
		Status:     "running",
		StartedAt:  time.Now(),
		ctx:        ctx,
		cancel:     cancel,
		conns:      make([]net.Conn, 0),
	}

	m.proxies[name] = proxy

	// 启动代理协程
	go m.runProxy(proxy)

	logger.Infof("端口代理已启动: %s (:%d -> %s)", name, listenPort, targetAddr)
	return nil
}

// Stop 停止端口代理
func (m *ProxyManager) Stop(name string) error {
	m.mutex.Lock()
	proxy, exists := m.proxies[name]
	if !exists {
		m.mutex.Unlock()
		return fmt.Errorf("代理 %s 不存在", name)
	}
	delete(m.proxies, name)
	m.mutex.Unlock()

	// 取消上下文
	proxy.cancel()

	// 关闭监听器
	if proxy.Listener != nil {
		proxy.Listener.Close()
	}

	// 关闭所有活跃连接
	proxy.mutex.Lock()
	for _, conn := range proxy.conns {
		conn.Close()
	}
	proxy.conns = nil
	proxy.mutex.Unlock()

	proxy.Status = "stopped"

	logger.Infof("端口代理已停止: %s", name)
	return nil
}

// List 列出所有代理状态
func (m *ProxyManager) List() []ProxyStatus {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	result := make([]ProxyStatus, 0, len(m.proxies))
	for _, proxy := range m.proxies {
		result = append(result, ProxyStatus{
			Name:        proxy.Name,
			ListenPort:  proxy.ListenPort,
			TargetAddr:  proxy.TargetAddr,
			Status:      proxy.Status,
			Connections: atomic.LoadInt64(&proxy.Connections),
			BytesIn:     atomic.LoadInt64(&proxy.BytesIn),
			BytesOut:    atomic.LoadInt64(&proxy.BytesOut),
			ErrorMsg:    proxy.ErrorMsg,
		})
	}
	return result
}

// GetStats 获取代理统计信息
func (m *ProxyManager) GetStats(name string) *ProxyStatus {
	m.mutex.RLock()
	proxy, exists := m.proxies[name]
	m.mutex.RUnlock()

	if !exists {
		return nil
	}

	return &ProxyStatus{
		Name:        proxy.Name,
		ListenPort:  proxy.ListenPort,
		TargetAddr:  proxy.TargetAddr,
		Status:      proxy.Status,
		Connections: atomic.LoadInt64(&proxy.Connections),
		BytesIn:     atomic.LoadInt64(&proxy.BytesIn),
		BytesOut:    atomic.LoadInt64(&proxy.BytesOut),
		ErrorMsg:    proxy.ErrorMsg,
	}
}

// Count 获取运行中的代理数量
func (m *ProxyManager) Count() int {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return len(m.proxies)
}

// runProxy 运行代理服务
func (m *ProxyManager) runProxy(proxy *TCPProxyService) {
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("代理 %s panic: %v", proxy.Name, r)
			proxy.Status = "error"
			proxy.ErrorMsg = fmt.Sprintf("panic: %v", r)
		}
	}()

	for {
		select {
		case <-proxy.ctx.Done():
			return
		default:
		}

		// 接受连接
		conn, err := proxy.Listener.Accept()
		if err != nil {
			select {
			case <-proxy.ctx.Done():
				return
			default:
				logger.Debugf("代理 %s 接受连接失败: %v", proxy.Name, err)
				continue
			}
		}

		// 处理连接
		go m.handleConnection(proxy, conn)
	}
}

// handleConnection 处理单个连接
func (m *ProxyManager) handleConnection(proxy *TCPProxyService, clientConn net.Conn) {
	defer clientConn.Close()

	// 记录连接
	proxy.mutex.Lock()
	proxy.conns = append(proxy.conns, clientConn)
	proxy.mutex.Unlock()

	atomic.AddInt64(&proxy.Connections, 1)

	defer func() {
		atomic.AddInt64(&proxy.Connections, -1)

		// 从连接列表移除
		proxy.mutex.Lock()
		for i, c := range proxy.conns {
			if c == clientConn {
				proxy.conns = append(proxy.conns[:i], proxy.conns[i+1:]...)
				break
			}
		}
		proxy.mutex.Unlock()
	}()

	// 拨号到目标
	targetConn, err := net.DialTimeout("tcp", proxy.TargetAddr, 10*time.Second)
	if err != nil {
		logger.Debugf("代理 %s 连接目标失败: %v", proxy.Name, err)
		return
	}
	defer targetConn.Close()

	// 双向转发
	var wg sync.WaitGroup
	wg.Add(2)

	// 客户端 -> 目标
	go func() {
		defer wg.Done()
		n, _ := io.Copy(targetConn, clientConn)
		atomic.AddInt64(&proxy.BytesIn, n)
	}()

	// 目标 -> 客户端
	go func() {
		defer wg.Done()
		n, _ := io.Copy(clientConn, targetConn)
		atomic.AddInt64(&proxy.BytesOut, n)
	}()

	wg.Wait()
}

// StopAll 停止所有代理
func (m *ProxyManager) StopAll() {
	m.mutex.Lock()
	names := make([]string, 0, len(m.proxies))
	for name := range m.proxies {
		names = append(names, name)
	}
	m.mutex.Unlock()

	for _, name := range names {
		if err := m.Stop(name); err != nil {
			logger.Warnf("停止代理 %s 失败: %v", name, err)
		}
	}
}

// Exists 检查代理是否存在
func (m *ProxyManager) Exists(name string) bool {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	_, exists := m.proxies[name]
	return exists
}

// GetStatus 获取所有代理的运行状态
func (m *ProxyManager) GetStatus() map[string]bool {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	result := make(map[string]bool)
	for name, proxy := range m.proxies {
		result[name] = proxy.Status == "running"
	}
	return result
}
