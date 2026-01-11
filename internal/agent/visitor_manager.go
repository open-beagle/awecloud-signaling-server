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

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
)

// VisitorManager 管理 Visitor（服务访问）
// Visitor 在局域网 IP 上监听端口，将流量通过 Tailscale VPN 转发到其他节点暴露的服务
type VisitorManager struct {
	visitors    map[string]*VisitorService
	tsManager   *TailscaleManager
	lanDetector *LANDetector
	config      *config.AgentConfig
	lanIP       string // 检测到的局域网 IP
	mutex       sync.RWMutex
	ctx         context.Context
}

// VisitorService Visitor 服务
type VisitorService struct {
	Name       string
	ListenPort int
	ListenAddr string // 实际监听地址（局域网 IP:端口）
	TargetAddr string // VPN 网络目标地址
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

// VisitorStatusInfo Visitor 状态信息
type VisitorStatusInfo struct {
	Name        string `json:"name"`
	ListenPort  int    `json:"listen_port"`
	ListenAddr  string `json:"listen_addr"`
	TargetAddr  string `json:"target_addr"`
	Status      string `json:"status"`
	Connections int64  `json:"connections"`
	BytesIn     int64  `json:"bytes_in"`
	BytesOut    int64  `json:"bytes_out"`
	ErrorMsg    string `json:"error_msg,omitempty"`
}

// NewVisitorManager 创建 VisitorManager
func NewVisitorManager(tsManager *TailscaleManager, cfg *config.AgentConfig, parentCtx context.Context) *VisitorManager {
	vm := &VisitorManager{
		visitors:    make(map[string]*VisitorService),
		tsManager:   tsManager,
		lanDetector: NewLANDetector(),
		config:      cfg,
		ctx:         parentCtx,
	}

	// 检测或使用配置的局域网 IP
	vm.detectLANIP()

	return vm
}

// detectLANIP 检测局域网 IP
func (m *VisitorManager) detectLANIP() {
	// 优先使用配置的地址
	if m.config.Visitor.ListenAddr != "" {
		m.lanIP = m.config.Visitor.ListenAddr
		logger.Infof("使用配置的 Visitor 监听地址: %s", m.lanIP)
		return
	}

	// 自动检测
	m.lanIP = m.lanDetector.DetectLANIP()
	logger.Infof("自动检测 Visitor 监听地址: %s", m.lanIP)
}

// GetLANIP 获取局域网 IP
func (m *VisitorManager) GetLANIP() string {
	return m.lanIP
}

// Start 启动 Visitor
func (m *VisitorManager) Start(name string, listenPort int, targetAddr string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// 检查是否已存在
	if _, exists := m.visitors[name]; exists {
		return fmt.Errorf("Visitor %s 已存在", name)
	}

	// 在局域网 IP 上监听端口
	listenAddr := fmt.Sprintf("%s:%d", m.lanIP, listenPort)
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("监听 %s 失败: %w", listenAddr, err)
	}

	ctx, cancel := context.WithCancel(m.ctx)

	visitor := &VisitorService{
		Name:       name,
		ListenPort: listenPort,
		ListenAddr: listenAddr,
		TargetAddr: targetAddr,
		Listener:   listener,
		Status:     "running",
		StartedAt:  time.Now(),
		ctx:        ctx,
		cancel:     cancel,
		conns:      make([]net.Conn, 0),
	}

	m.visitors[name] = visitor

	// 启动 Visitor 协程
	go m.runVisitor(visitor)

	logger.Infof("Visitor 已启动: %s (%s -> %s via Tailscale)", name, listenAddr, targetAddr)
	return nil
}

// Stop 停止 Visitor
func (m *VisitorManager) Stop(name string) error {
	m.mutex.Lock()
	visitor, exists := m.visitors[name]
	if !exists {
		m.mutex.Unlock()
		return fmt.Errorf("Visitor %s 不存在", name)
	}
	delete(m.visitors, name)
	m.mutex.Unlock()

	// 取消上下文
	visitor.cancel()

	// 关闭监听器
	if visitor.Listener != nil {
		visitor.Listener.Close()
	}

	// 关闭所有活跃连接
	visitor.mutex.Lock()
	for _, conn := range visitor.conns {
		conn.Close()
	}
	visitor.conns = nil
	visitor.mutex.Unlock()

	visitor.Status = "stopped"

	logger.Infof("Visitor 已停止: %s", name)
	return nil
}

// List 列出所有 Visitor 状态
func (m *VisitorManager) List() []VisitorStatusInfo {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	result := make([]VisitorStatusInfo, 0, len(m.visitors))
	for _, visitor := range m.visitors {
		result = append(result, VisitorStatusInfo{
			Name:        visitor.Name,
			ListenPort:  visitor.ListenPort,
			ListenAddr:  visitor.ListenAddr,
			TargetAddr:  visitor.TargetAddr,
			Status:      visitor.Status,
			Connections: atomic.LoadInt64(&visitor.Connections),
			BytesIn:     atomic.LoadInt64(&visitor.BytesIn),
			BytesOut:    atomic.LoadInt64(&visitor.BytesOut),
			ErrorMsg:    visitor.ErrorMsg,
		})
	}
	return result
}

// GetStats 获取 Visitor 统计信息
func (m *VisitorManager) GetStats(name string) *VisitorStatusInfo {
	m.mutex.RLock()
	visitor, exists := m.visitors[name]
	m.mutex.RUnlock()

	if !exists {
		return nil
	}

	return &VisitorStatusInfo{
		Name:        visitor.Name,
		ListenPort:  visitor.ListenPort,
		ListenAddr:  visitor.ListenAddr,
		TargetAddr:  visitor.TargetAddr,
		Status:      visitor.Status,
		Connections: atomic.LoadInt64(&visitor.Connections),
		BytesIn:     atomic.LoadInt64(&visitor.BytesIn),
		BytesOut:    atomic.LoadInt64(&visitor.BytesOut),
		ErrorMsg:    visitor.ErrorMsg,
	}
}

// Count 获取运行中的 Visitor 数量
func (m *VisitorManager) Count() int {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return len(m.visitors)
}

// runVisitor 运行 Visitor 服务
func (m *VisitorManager) runVisitor(visitor *VisitorService) {
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("Visitor %s panic: %v", visitor.Name, r)
			visitor.Status = "error"
			visitor.ErrorMsg = fmt.Sprintf("panic: %v", r)
		}
	}()

	for {
		select {
		case <-visitor.ctx.Done():
			return
		default:
		}

		// 接受连接
		conn, err := visitor.Listener.Accept()
		if err != nil {
			select {
			case <-visitor.ctx.Done():
				return
			default:
				logger.Debugf("Visitor %s 接受连接失败: %v", visitor.Name, err)
				continue
			}
		}

		// 处理连接
		go m.handleVisitorConnection(visitor, conn)
	}
}

// handleVisitorConnection 处理 Visitor 连接
func (m *VisitorManager) handleVisitorConnection(visitor *VisitorService, clientConn net.Conn) {
	defer clientConn.Close()

	// 记录连接
	visitor.mutex.Lock()
	visitor.conns = append(visitor.conns, clientConn)
	visitor.mutex.Unlock()

	atomic.AddInt64(&visitor.Connections, 1)

	defer func() {
		atomic.AddInt64(&visitor.Connections, -1)

		// 从连接列表移除
		visitor.mutex.Lock()
		for i, c := range visitor.conns {
			if c == clientConn {
				visitor.conns = append(visitor.conns[:i], visitor.conns[i+1:]...)
				break
			}
		}
		visitor.mutex.Unlock()
	}()

	// 通过 Tailscale 拨号到目标
	ctx, cancel := context.WithTimeout(visitor.ctx, 10*time.Second)
	defer cancel()

	targetConn, err := m.tsManager.Dial(ctx, "tcp", visitor.TargetAddr)
	if err != nil {
		logger.Debugf("Visitor %s 连接目标失败: %v", visitor.Name, err)
		return
	}
	defer targetConn.Close()

	// 双向转发
	var wg sync.WaitGroup
	wg.Add(2)

	// 客户端 -> 目标（通过 Tailscale）
	go func() {
		defer wg.Done()
		n, _ := io.Copy(targetConn, clientConn)
		atomic.AddInt64(&visitor.BytesIn, n)
	}()

	// 目标 -> 客户端
	go func() {
		defer wg.Done()
		n, _ := io.Copy(clientConn, targetConn)
		atomic.AddInt64(&visitor.BytesOut, n)
	}()

	wg.Wait()
}

// StopAll 停止所有 Visitor
func (m *VisitorManager) StopAll() {
	m.mutex.Lock()
	names := make([]string, 0, len(m.visitors))
	for name := range m.visitors {
		names = append(names, name)
	}
	m.mutex.Unlock()

	for _, name := range names {
		if err := m.Stop(name); err != nil {
			logger.Warnf("停止 Visitor %s 失败: %v", name, err)
		}
	}
}

// Exists 检查 Visitor 是否存在
func (m *VisitorManager) Exists(name string) bool {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	_, exists := m.visitors[name]
	return exists
}
