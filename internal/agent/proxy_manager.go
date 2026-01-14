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
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

// ProxyManager 管理 TCP 端口代理
type ProxyManager struct {
	proxies     map[string]*TCPProxyService
	pendingList []ServiceConfig // 等待 VPN 就绪的服务列表
	tsManager   *TailscaleManager
	grpcClient  pb.AgentServiceClient
	agentID     uint64
	mutex       sync.RWMutex
	ctx         context.Context
}

// ServiceConfig 服务配置
type ServiceConfig struct {
	ID         string
	Name       string
	SourceAddr string // VPN IP:端口
	TargetAddr string // 局域网地址
	Enabled    bool
}

// TCPProxyService TCP 代理服务
type TCPProxyService struct {
	ID         string
	Name       string
	SourceAddr string // VPN IP:端口
	TargetAddr string // 局域网地址
	Listener   net.Listener
	Status     string // running/disabled/error/pending
	ErrorMsg   string
	ErrorCode  string

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
	ID          string `json:"id"`
	Name        string `json:"name"`
	SourceAddr  string `json:"source_addr"`
	TargetAddr  string `json:"target_addr"`
	Status      string `json:"status"`
	Connections int64  `json:"connections"`
	BytesIn     int64  `json:"bytes_in"`
	BytesOut    int64  `json:"bytes_out"`
	ErrorMsg    string `json:"error_msg,omitempty"`
	ErrorCode   string `json:"error_code,omitempty"`
}

// NewProxyManager 创建 ProxyManager
func NewProxyManager(tsManager *TailscaleManager, grpcClient pb.AgentServiceClient, agentID uint64, ctx context.Context) *ProxyManager {
	return &ProxyManager{
		proxies:     make(map[string]*TCPProxyService),
		pendingList: make([]ServiceConfig, 0),
		tsManager:   tsManager,
		grpcClient:  grpcClient,
		agentID:     agentID,
		ctx:         ctx,
	}
}

// Start 启动端口代理
func (m *ProxyManager) Start(config ServiceConfig) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// 检查是否已存在
	if _, exists := m.proxies[config.ID]; exists {
		return fmt.Errorf("代理 %s 已存在", config.ID)
	}

	// 检查 VPN 是否就绪
	if !m.tsManager.IsConnected() {
		// 加入等待队列
		m.pendingList = append(m.pendingList, config)
		logger.Infof("VPN 未就绪，服务 %s 加入等待队列", config.Name)

		// 上报 pending 状态
		go m.reportStatus(config.ID, "pending", "", "VPN_NOT_READY")
		return nil
	}

	// 启动服务
	return m.startService(config)
}

// startService 启动服务（内部方法，调用前需持有锁）
func (m *ProxyManager) startService(config ServiceConfig) error {
	// 解析源地址，提取端口
	_, port, err := net.SplitHostPort(config.SourceAddr)
	if err != nil {
		logger.Errorf("解析源地址失败: %s, error: %v", config.SourceAddr, err)
		go m.reportStatus(config.ID, "error", "解析源地址失败", "INVALID_SOURCE_ADDR")
		return err
	}

	// 在 VPN IP 上监听
	vpnIP := m.tsManager.GetIP()
	if vpnIP == "" {
		logger.Errorf("无法获取 VPN IP")
		go m.reportStatus(config.ID, "error", "无法获取 VPN IP", "VPN_IP_NOT_FOUND")
		return fmt.Errorf("无法获取 VPN IP")
	}

	listenAddr := fmt.Sprintf("%s:%s", vpnIP, port)
	listener, err := m.tsManager.Listen("tcp", listenAddr)
	if err != nil {
		logger.Errorf("监听地址 %s 失败: %v", listenAddr, err)
		go m.reportStatus(config.ID, "error", fmt.Sprintf("监听失败: %v", err), "PORT_IN_USE")
		return err
	}

	ctx, cancel := context.WithCancel(m.ctx)

	proxy := &TCPProxyService{
		ID:         config.ID,
		Name:       config.Name,
		SourceAddr: listenAddr,
		TargetAddr: config.TargetAddr,
		Listener:   listener,
		Status:     "running",
		StartedAt:  time.Now(),
		ctx:        ctx,
		cancel:     cancel,
		conns:      make([]net.Conn, 0),
	}

	m.proxies[config.ID] = proxy

	// 启动代理协程
	go m.runProxy(proxy)

	logger.Infof("端口代理已启动: %s (%s -> %s)", config.Name, listenAddr, config.TargetAddr)

	// 上报 running 状态
	go m.reportStatus(config.ID, "running", "", "")

	return nil
}

// OnVPNReady VPN 就绪后启动等待的服务
func (m *ProxyManager) OnVPNReady() {
	m.mutex.Lock()
	pendingConfigs := m.pendingList
	m.pendingList = make([]ServiceConfig, 0)
	m.mutex.Unlock()

	logger.Infof("VPN 已就绪，启动 %d 个等待中的服务", len(pendingConfigs))

	for _, config := range pendingConfigs {
		m.mutex.Lock()
		err := m.startService(config)
		m.mutex.Unlock()

		if err != nil {
			logger.Errorf("启动服务 %s 失败: %v", config.Name, err)
		}
	}
}

// Stop 停止端口代理
func (m *ProxyManager) Stop(id string) error {
	m.mutex.Lock()
	proxy, exists := m.proxies[id]
	if !exists {
		m.mutex.Unlock()
		return fmt.Errorf("代理 %s 不存在", id)
	}
	delete(m.proxies, id)
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

	logger.Infof("端口代理已停止: %s", proxy.Name)

	// 上报 disabled 状态
	go m.reportStatus(id, "disabled", "", "")

	return nil
}

// reportStatus 上报服务状态到 Server
func (m *ProxyManager) reportStatus(serviceID, status, errorMsg, errorCode string) {
	if m.grpcClient == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req := &pb.ReportProxyStatusRequest{
		AgentId: m.agentID,
		Statuses: []*pb.ProxyStatus{
			{
				ServiceId: serviceID,
				Status:    status,
				ErrorMsg:  errorMsg,
				ErrorCode: errorCode,
			},
		},
	}

	_, err := m.grpcClient.ReportProxyStatus(ctx, req)
	if err != nil {
		logger.Warnf("上报服务状态失败: %v", err)
	}
}

// ReportAllStatus 上报所有服务状态
func (m *ProxyManager) ReportAllStatus() {
	m.mutex.RLock()
	statuses := make([]*pb.ProxyStatus, 0, len(m.proxies))
	for _, proxy := range m.proxies {
		statuses = append(statuses, &pb.ProxyStatus{
			ServiceId: proxy.ID,
			Status:    proxy.Status,
			ErrorMsg:  proxy.ErrorMsg,
			ErrorCode: proxy.ErrorCode,
		})
	}
	m.mutex.RUnlock()

	if len(statuses) == 0 || m.grpcClient == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req := &pb.ReportProxyStatusRequest{
		AgentId:  m.agentID,
		Statuses: statuses,
	}

	_, err := m.grpcClient.ReportProxyStatus(ctx, req)
	if err != nil {
		logger.Warnf("批量上报服务状态失败: %v", err)
	}
}

// UpdateConfig 更新服务配置
func (m *ProxyManager) UpdateConfig(configs []ServiceConfig) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// 构建新配置映射
	newConfigs := make(map[string]ServiceConfig)
	for _, config := range configs {
		newConfigs[config.ID] = config
	}

	// 停止不在新配置中的服务
	for id, proxy := range m.proxies {
		if _, exists := newConfigs[id]; !exists {
			go func(p *TCPProxyService) {
				m.Stop(p.ID)
			}(proxy)
		}
	}

	// 启动或更新服务
	for id, config := range newConfigs {
		if !config.Enabled {
			// 如果服务被禁用，停止它
			if _, exists := m.proxies[id]; exists {
				go func(serviceID string) {
					m.Stop(serviceID)
				}(id)
			}
			continue
		}

		// 检查服务是否已存在
		if proxy, exists := m.proxies[id]; exists {
			// 如果配置有变化，重启服务
			if proxy.SourceAddr != config.SourceAddr || proxy.TargetAddr != config.TargetAddr {
				go func(serviceID string, cfg ServiceConfig) {
					m.Stop(serviceID)
					time.Sleep(100 * time.Millisecond)
					m.Start(cfg)
				}(id, config)
			}
		} else {
			// 启动新服务
			go func(cfg ServiceConfig) {
				m.Start(cfg)
			}(config)
		}
	}
}

// List 列出所有代理状态
func (m *ProxyManager) List() []ProxyStatus {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	result := make([]ProxyStatus, 0, len(m.proxies))
	for _, proxy := range m.proxies {
		result = append(result, ProxyStatus{
			ID:          proxy.ID,
			Name:        proxy.Name,
			SourceAddr:  proxy.SourceAddr,
			TargetAddr:  proxy.TargetAddr,
			Status:      proxy.Status,
			Connections: atomic.LoadInt64(&proxy.Connections),
			BytesIn:     atomic.LoadInt64(&proxy.BytesIn),
			BytesOut:    atomic.LoadInt64(&proxy.BytesOut),
			ErrorMsg:    proxy.ErrorMsg,
			ErrorCode:   proxy.ErrorCode,
		})
	}
	return result
}

// GetStats 获取代理统计信息
func (m *ProxyManager) GetStats(id string) *ProxyStatus {
	m.mutex.RLock()
	proxy, exists := m.proxies[id]
	m.mutex.RUnlock()

	if !exists {
		return nil
	}

	return &ProxyStatus{
		ID:          proxy.ID,
		Name:        proxy.Name,
		SourceAddr:  proxy.SourceAddr,
		TargetAddr:  proxy.TargetAddr,
		Status:      proxy.Status,
		Connections: atomic.LoadInt64(&proxy.Connections),
		BytesIn:     atomic.LoadInt64(&proxy.BytesIn),
		BytesOut:    atomic.LoadInt64(&proxy.BytesOut),
		ErrorMsg:    proxy.ErrorMsg,
		ErrorCode:   proxy.ErrorCode,
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
func (m *ProxyManager) Exists(id string) bool {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	_, exists := m.proxies[id]
	return exists
}

// GetStatus 获取所有代理的运行状态
func (m *ProxyManager) GetStatus() map[string]bool {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	result := make(map[string]bool)
	for id, proxy := range m.proxies {
		result[id] = proxy.Status == "running"
	}
	return result
}
