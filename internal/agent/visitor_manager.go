// Package agent 提供 Agent 端功能
package agent

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

// VisitorManager 管理 Visitor（服务访问）
// Visitor 在局域网 IP 上监听端口，将流量通过 Tailscale VPN 转发到其他节点暴露的服务
type VisitorManager struct {
	visitors    map[string]*VisitorService
	tsManager   *TailscaleManager
	lanDetector *LANDetector
	config      *config.AgentConfig
	grpcClient  pb.AgentServiceClient
	agentID     uint64
	lanIP       string // 检测到的局域网 IP
	mutex       sync.RWMutex
	ctx         context.Context
}

// VisitorService Visitor 服务
type VisitorService struct {
	ID           string
	ServiceID    string // 关联的远程服务 ID
	ServiceName  string // 关联的远程服务名称
	SourceAddr   string // 配置的源地址（局域网 IP:端口）
	ActualAddr   string // 实际监听地址（可能因 IP 变化而不同）
	TargetAddr   string // VPN 网络目标地址
	Listener     net.Listener
	Status       string // running/disabled/error/pending
	ErrorMsg     string
	ErrorCode    string
	ConfiguredIP string // 配置的 IP
	IPChanged    bool   // IP 是否变化
	ChangeReason string // 变化原因

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
	ID           string `json:"id"`
	ServiceID    string `json:"service_id"`
	ServiceName  string `json:"service_name"`
	SourceAddr   string `json:"source_addr"`
	ActualAddr   string `json:"actual_addr"`
	TargetAddr   string `json:"target_addr"`
	Status       string `json:"status"`
	Connections  int64  `json:"connections"`
	BytesIn      int64  `json:"bytes_in"`
	BytesOut     int64  `json:"bytes_out"`
	ErrorMsg     string `json:"error_msg,omitempty"`
	ErrorCode    string `json:"error_code,omitempty"`
	IPChanged    bool   `json:"ip_changed"`
	ChangeReason string `json:"change_reason,omitempty"`
}

// VisitorConfig Visitor 配置
type VisitorConfig struct {
	ID          string
	ServiceID   string
	ServiceName string
	SourceAddr  string // 局域网 IP:端口
	TargetAddr  string // VPN 地址
	Enabled     bool
}

// NewVisitorManager 创建 VisitorManager
func NewVisitorManager(tsManager *TailscaleManager, cfg *config.AgentConfig, grpcClient pb.AgentServiceClient, agentID uint64, parentCtx context.Context) *VisitorManager {
	vm := &VisitorManager{
		visitors:    make(map[string]*VisitorService),
		tsManager:   tsManager,
		lanDetector: NewLANDetector(),
		config:      cfg,
		grpcClient:  grpcClient,
		agentID:     agentID,
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

// resolveSourceAddr 解析源地址，处理 IP 变化
// 返回：实际监听地址、是否变化、变化原因、错误
func (m *VisitorManager) resolveSourceAddr(configuredAddr string) (string, bool, string, error) {
	// 解析配置的地址
	configuredIP, port, err := net.SplitHostPort(configuredAddr)
	if err != nil {
		return "", false, "", fmt.Errorf("解析源地址失败: %w", err)
	}

	// 1. 如果是 0.0.0.0，直接使用
	if configuredIP == "0.0.0.0" {
		return configuredAddr, false, "", nil
	}

	// 获取当前所有局域网 IP
	currentIPs := m.lanDetector.GetAllLANIPs()

	// 2. 检查配置的 IP 是否存在
	for _, ip := range currentIPs {
		if ip == configuredIP {
			return configuredAddr, false, "", nil
		}
	}

	// 3. 查找同网段的 IP
	configuredSubnet := getSubnet(configuredIP)
	for _, ip := range currentIPs {
		if isInSubnet(ip, configuredSubnet) {
			// 找到同网段 IP，自动适配
			newAddr := net.JoinHostPort(ip, port)
			logger.Infof("检测到 IP 变化，自动适配: %s -> %s", configuredAddr, newAddr)
			return newAddr, true, "DHCP_IP_CHANGE", nil
		}
	}

	// 4. 未找到可用 IP，返回错误
	return "", false, "NETWORK_INTERFACE_LOST",
		fmt.Errorf("配置的网段 %s 在本机未找到可用 IP，请检查网卡状态或更新配置", configuredSubnet)
}

// getSubnet 获取 IP 的网段（简化版，假设 /24）
func getSubnet(ip string) string {
	parts := strings.Split(ip, ".")
	if len(parts) != 4 {
		return ""
	}
	return fmt.Sprintf("%s.%s.%s.0/24", parts[0], parts[1], parts[2])
}

// isInSubnet 检查 IP 是否在指定网段（简化版，仅支持 /24）
func isInSubnet(ip, subnet string) bool {
	// 提取网段前缀（如 192.168.1）
	subnetPrefix := strings.TrimSuffix(subnet, ".0/24")
	ipPrefix := ip[:strings.LastIndex(ip, ".")]
	return ipPrefix == subnetPrefix
}

// Start 启动 Visitor
func (m *VisitorManager) Start(config VisitorConfig) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// 检查是否已存在
	if _, exists := m.visitors[config.ID]; exists {
		return fmt.Errorf("Visitor %s 已存在", config.ID)
	}

	// 解析源地址，处理 IP 变化
	actualAddr, ipChanged, changeReason, err := m.resolveSourceAddr(config.SourceAddr)
	if err != nil {
		logger.Errorf("解析源地址失败: %s, error: %v", config.SourceAddr, err)
		// 上报错误状态
		go m.reportStatus(config.ID, config.ServiceID, "error", config.SourceAddr, "", false, changeReason, err.Error(), changeReason)
		return err
	}

	// 在局域网上监听
	listener, err := net.Listen("tcp", actualAddr)
	if err != nil {
		logger.Errorf("监听地址 %s 失败: %v", actualAddr, err)
		go m.reportStatus(config.ID, config.ServiceID, "error", config.SourceAddr, actualAddr, ipChanged, changeReason, fmt.Sprintf("监听失败: %v", err), "PORT_IN_USE")
		return fmt.Errorf("监听 %s 失败: %w", actualAddr, err)
	}

	ctx, cancel := context.WithCancel(m.ctx)

	visitor := &VisitorService{
		ID:           config.ID,
		ServiceID:    config.ServiceID,
		ServiceName:  config.ServiceName,
		SourceAddr:   config.SourceAddr,
		ActualAddr:   actualAddr,
		TargetAddr:   config.TargetAddr,
		Listener:     listener,
		Status:       "running",
		StartedAt:    time.Now(),
		ConfiguredIP: strings.Split(config.SourceAddr, ":")[0],
		IPChanged:    ipChanged,
		ChangeReason: changeReason,
		ctx:          ctx,
		cancel:       cancel,
		conns:        make([]net.Conn, 0),
	}

	m.visitors[config.ID] = visitor

	// 启动 Visitor 协程
	go m.runVisitor(visitor)

	logger.Infof("Visitor 已启动: %s (%s -> %s via Tailscale)", config.ServiceName, actualAddr, config.TargetAddr)

	// 上报 running 状态
	go m.reportStatus(config.ID, config.ServiceID, "running", config.SourceAddr, actualAddr, ipChanged, changeReason, "", "")

	return nil
}

// Stop 停止 Visitor
func (m *VisitorManager) Stop(id string) error {
	m.mutex.Lock()
	visitor, exists := m.visitors[id]
	if !exists {
		m.mutex.Unlock()
		return fmt.Errorf("Visitor %s 不存在", id)
	}
	delete(m.visitors, id)
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

	visitor.Status = "disabled"

	logger.Infof("Visitor 已停止: %s", visitor.ServiceName)

	// 上报 disabled 状态
	go m.reportStatus(id, visitor.ServiceID, "disabled", visitor.SourceAddr, visitor.ActualAddr, false, "", "", "")

	return nil
}

// reportStatus 上报服务状态到 Server
func (m *VisitorManager) reportStatus(forwardID, serviceID, status, configuredAddr, actualAddr string, ipChanged bool, changeReason, errorMsg, errorCode string) {
	if m.grpcClient == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req := &pb.ReportVisitorStatusRequest{
		AgentId: m.agentID,
		Statuses: []*pb.VisitorStatus{
			{
				ForwardId:      forwardID,
				Status:         status,
				ConfiguredAddr: configuredAddr,
				ActualAddr:     actualAddr,
				IpChanged:      ipChanged,
				ChangeReason:   changeReason,
				ErrorMsg:       errorMsg,
				ErrorCode:      errorCode,
			},
		},
	}

	_, err := m.grpcClient.ReportVisitorStatus(ctx, req)
	if err != nil {
		logger.Warnf("上报 Visitor 状态失败: %v", err)
	}
}

// ReportAllStatus 上报所有 Visitor 状态
func (m *VisitorManager) ReportAllStatus() {
	m.mutex.RLock()
	statuses := make([]*pb.VisitorStatus, 0, len(m.visitors))
	for _, visitor := range m.visitors {
		statuses = append(statuses, &pb.VisitorStatus{
			ForwardId:      visitor.ID,
			Status:         visitor.Status,
			ConfiguredAddr: visitor.SourceAddr,
			ActualAddr:     visitor.ActualAddr,
			IpChanged:      visitor.IPChanged,
			ChangeReason:   visitor.ChangeReason,
			ErrorMsg:       visitor.ErrorMsg,
			ErrorCode:      visitor.ErrorCode,
		})
	}
	m.mutex.RUnlock()

	if len(statuses) == 0 || m.grpcClient == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req := &pb.ReportVisitorStatusRequest{
		AgentId:  m.agentID,
		Statuses: statuses,
	}

	_, err := m.grpcClient.ReportVisitorStatus(ctx, req)
	if err != nil {
		logger.Warnf("批量上报 Visitor 状态失败: %v", err)
	}
}

// UpdateConfig 更新服务配置
func (m *VisitorManager) UpdateConfig(configs []VisitorConfig) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// 构建新配置映射
	newConfigs := make(map[string]VisitorConfig)
	for _, config := range configs {
		newConfigs[config.ID] = config
	}

	// 停止不在新配置中的服务
	for id, visitor := range m.visitors {
		if _, exists := newConfigs[id]; !exists {
			go func(v *VisitorService) {
				m.Stop(v.ID)
			}(visitor)
		}
	}

	// 启动或更新服务
	for id, config := range newConfigs {
		if !config.Enabled {
			// 如果服务被禁用，停止它
			if _, exists := m.visitors[id]; exists {
				go func(visitorID string) {
					m.Stop(visitorID)
				}(id)
			}
			continue
		}

		// 检查服务是否已存在
		if visitor, exists := m.visitors[id]; exists {
			// 如果配置有变化，重启服务
			if visitor.SourceAddr != config.SourceAddr || visitor.TargetAddr != config.TargetAddr {
				go func(visitorID string, cfg VisitorConfig) {
					m.Stop(visitorID)
					time.Sleep(100 * time.Millisecond)
					m.Start(cfg)
				}(id, config)
			}
		} else {
			// 启动新服务
			go func(cfg VisitorConfig) {
				m.Start(cfg)
			}(config)
		}
	}
}

// List 列出所有 Visitor 状态
func (m *VisitorManager) List() []VisitorStatusInfo {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	result := make([]VisitorStatusInfo, 0, len(m.visitors))
	for _, visitor := range m.visitors {
		result = append(result, VisitorStatusInfo{
			ID:           visitor.ID,
			ServiceID:    visitor.ServiceID,
			ServiceName:  visitor.ServiceName,
			SourceAddr:   visitor.SourceAddr,
			ActualAddr:   visitor.ActualAddr,
			TargetAddr:   visitor.TargetAddr,
			Status:       visitor.Status,
			Connections:  atomic.LoadInt64(&visitor.Connections),
			BytesIn:      atomic.LoadInt64(&visitor.BytesIn),
			BytesOut:     atomic.LoadInt64(&visitor.BytesOut),
			ErrorMsg:     visitor.ErrorMsg,
			ErrorCode:    visitor.ErrorCode,
			IPChanged:    visitor.IPChanged,
			ChangeReason: visitor.ChangeReason,
		})
	}
	return result
}

// GetStats 获取 Visitor 统计信息
func (m *VisitorManager) GetStats(id string) *VisitorStatusInfo {
	m.mutex.RLock()
	visitor, exists := m.visitors[id]
	m.mutex.RUnlock()

	if !exists {
		return nil
	}

	return &VisitorStatusInfo{
		ID:           visitor.ID,
		ServiceID:    visitor.ServiceID,
		ServiceName:  visitor.ServiceName,
		SourceAddr:   visitor.SourceAddr,
		ActualAddr:   visitor.ActualAddr,
		TargetAddr:   visitor.TargetAddr,
		Status:       visitor.Status,
		Connections:  atomic.LoadInt64(&visitor.Connections),
		BytesIn:      atomic.LoadInt64(&visitor.BytesIn),
		BytesOut:     atomic.LoadInt64(&visitor.BytesOut),
		ErrorMsg:     visitor.ErrorMsg,
		ErrorCode:    visitor.ErrorCode,
		IPChanged:    visitor.IPChanged,
		ChangeReason: visitor.ChangeReason,
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
			logger.Errorf("Visitor %s panic: %v", visitor.ServiceName, r)
			visitor.Status = "error"
			visitor.ErrorMsg = fmt.Sprintf("panic: %v", r)
			visitor.ErrorCode = "PANIC"
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
				logger.Debugf("Visitor %s 接受连接失败: %v", visitor.ServiceName, err)
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
		logger.Debugf("Visitor %s 连接目标失败: %v", visitor.ServiceName, err)
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
	ids := make([]string, 0, len(m.visitors))
	for id := range m.visitors {
		ids = append(ids, id)
	}
	m.mutex.Unlock()

	for _, id := range ids {
		if err := m.Stop(id); err != nil {
			logger.Warnf("停止 Visitor %s 失败: %v", id, err)
		}
	}
}

// Exists 检查 Visitor 是否存在
func (m *VisitorManager) Exists(id string) bool {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	_, exists := m.visitors[id]
	return exists
}

// GetStatus 获取所有 Visitor 的运行状态
func (m *VisitorManager) GetStatus() map[string]bool {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	result := make(map[string]bool)
	for id, visitor := range m.visitors {
		result[id] = visitor.Status == "running"
	}
	return result
}
