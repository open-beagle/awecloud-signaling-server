// Package agent 提供 Agent 端功能
package agent

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"tailscale.com/tsnet"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

const (
	// ConnTypeP2P 表示点对点直连
	ConnTypeP2P = "p2p"
	// ConnTypeDERP 表示通过 DERP 中继
	ConnTypeDERP = "derp"
	// ConnTypeUnknown 表示未知连接类型
	ConnTypeUnknown = "unknown"
)

// TailscaleManager 管理 Tailscale 客户端连接
type TailscaleManager struct {
	tsServer   *tsnet.Server
	config     *config.AgentConfig
	grpcClient pb.AgentServiceClient
	agentID    uint64
	agentToken string

	tailscaleIP string
	connected   bool
	connType    string    // "p2p" or "derp"
	connectedAt time.Time // 连接时间
	stateDir    string    // 状态目录（本地或临时）
	isTemp      bool      // 是否使用临时目录
	mutex       sync.RWMutex

	ctx    context.Context
	cancel context.CancelFunc
}

// NewTailscaleManager 创建 TailscaleManager
func NewTailscaleManager(cfg *config.AgentConfig, grpcClient pb.AgentServiceClient, agentID uint64, agentToken string, parentCtx context.Context) *TailscaleManager {
	ctx, cancel := context.WithCancel(parentCtx)
	return &TailscaleManager{
		config:     cfg,
		grpcClient: grpcClient,
		agentID:    agentID,
		agentToken: agentToken,
		ctx:        ctx,
		cancel:     cancel,
	}
}

// Start 启动 Tailscale 客户端
func (m *TailscaleManager) Start(controlURL, authKey string) error {
	logger.Infof("启动 Tailscale 客户端，连接到: %s", controlURL)

	// 初始化状态目录
	if err := m.initStateDir(); err != nil {
		return fmt.Errorf("初始化状态目录失败: %w", err)
	}

	// 创建 tsnet.Server
	m.tsServer = &tsnet.Server{
		Hostname:   m.config.Agent.AgentName,
		Dir:        m.stateDir,
		ControlURL: controlURL,
		AuthKey:    authKey,
		Ephemeral:  false,           // Agent 需要持久化节点
		Logf:       m.tailscaleLogf, // 使用自定义日志函数
	}

	// 启动 Tailscale
	status, err := m.tsServer.Up(m.ctx)
	if err != nil {
		return fmt.Errorf("启动 Tailscale 失败: %w", err)
	}

	// 获取 Tailscale IP
	if len(status.TailscaleIPs) > 0 {
		m.mutex.Lock()
		m.tailscaleIP = status.TailscaleIPs[0].String()
		m.connected = true
		m.connType = ConnTypeP2P   // 默认 P2P，后续通过状态更新
		m.connectedAt = time.Now() // 记录连接时间
		m.mutex.Unlock()

		logger.Infof("Tailscale 已连接，IP: %s", m.tailscaleIP)

		// 启动状态监控协程
		go m.monitorConnectionStatus()

		// 启动定期状态同步（如果不是临时目录且 grpcClient 可用）
		if !m.isTemp && m.grpcClient != nil {
			go m.periodicStateSave()
		}
	} else {
		return fmt.Errorf("未获取到 Tailscale IP")
	}

	return nil
}

// Stop 停止 Tailscale 客户端
func (m *TailscaleManager) Stop() error {
	m.cancel()

	m.mutex.Lock()
	m.connected = false
	m.mutex.Unlock()

	// 尝试保存状态到 Server（尽力而为）
	if !m.isTemp && m.grpcClient != nil {
		logger.Debug("停止前保存状态到 Server...")
		if err := m.saveStateToServer(); err != nil {
			logger.Warnf("保存状态到 Server 失败: %v", err)
		} else {
			logger.Debug("状态已保存到 Server")
		}
	}

	if m.tsServer != nil {
		if err := m.tsServer.Close(); err != nil {
			logger.Warnf("关闭 Tailscale 失败: %v", err)
			return err
		}
	}

	// 如果使用临时目录，清理临时目录
	if m.isTemp && m.stateDir != "" {
		if err := m.cleanupTempDir(); err != nil {
			logger.Warnf("清理临时目录失败: %v", err)
		}
	}

	logger.Info("Tailscale 已停止")
	return nil
}

// cleanupTempDir 清理临时目录（仅无状态模式）
func (m *TailscaleManager) cleanupTempDir() error {
	if !m.isTemp || m.stateDir == "" {
		return nil
	}

	logger.Infof("清理临时状态目录: %s", m.stateDir)
	if err := os.RemoveAll(m.stateDir); err != nil {
		return fmt.Errorf("删除临时目录失败: %w", err)
	}

	return nil
}

// GetIP 获取 Tailscale IP
func (m *TailscaleManager) GetIP() string {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.tailscaleIP
}

// IsConnected 检查连接状态
func (m *TailscaleManager) IsConnected() bool {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.connected
}

// GetConnType 获取连接类型
func (m *TailscaleManager) GetConnType() string {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.connType
}

// GetConnectedAt 获取连接时间（Unix 时间戳，秒）
func (m *TailscaleManager) GetConnectedAt() int64 {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	if m.connectedAt.IsZero() {
		return 0
	}
	return m.connectedAt.Unix()
}

// Listen 在 Tailscale 网络上监听端口
func (m *TailscaleManager) Listen(network, addr string) (net.Listener, error) {
	if m.tsServer == nil {
		return nil, fmt.Errorf("Tailscale 未启动")
	}
	return m.tsServer.Listen(network, addr)
}

// Dial 通过 Tailscale 网络拨号
func (m *TailscaleManager) Dial(ctx context.Context, network, addr string) (net.Conn, error) {
	if m.tsServer == nil {
		return nil, fmt.Errorf("Tailscale 未启动")
	}
	return m.tsServer.Dial(ctx, network, addr)
}

// expandHomeDir 展开 ~ 为用户主目录
func (m *TailscaleManager) expandHomeDir(path string) string {
	if path == "" {
		return path
	}

	// 如果路径以 ~ 开头，展开为用户主目录
	if strings.HasPrefix(path, "~/") {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			logger.Warnf("获取用户主目录失败: %v，使用原始路径", err)
			return path
		}
		return filepath.Join(homeDir, path[2:])
	}

	// 如果路径就是 ~，返回用户主目录
	if path == "~" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			logger.Warnf("获取用户主目录失败: %v，使用 /tmp", err)
			return "/tmp"
		}
		return homeDir
	}

	return path
}

// initStateDir 初始化状态目录（本地或临时）
func (m *TailscaleManager) initStateDir() error {
	configuredDir := m.config.Tailscale.StateDir

	// 如果配置为空，使用临时目录（无状态模式）
	if configuredDir == "" {
		tmpDir, err := os.MkdirTemp("", "tailscale-*")
		if err != nil {
			return fmt.Errorf("创建临时目录失败: %w", err)
		}
		// 设置权限为 700（仅所有者可访问）
		if err := os.Chmod(tmpDir, 0700); err != nil {
			return fmt.Errorf("设置临时目录权限失败: %w", err)
		}
		m.stateDir = tmpDir
		m.isTemp = true
		logger.Infof("使用临时状态目录（无状态模式）: %s", tmpDir)

		// 无状态模式也尝试从 Server 加载历史状态
		if err := m.loadAndRestoreState(); err != nil {
			logger.Debugf("从 Server 加载状态失败（无状态模式）: %v，使用空状态", err)
		}

		return nil
	}

	// 展开 ~ 为用户主目录
	expandedDir := m.expandHomeDir(configuredDir)

	// 检查本地状态是否存在
	stateFile := filepath.Join(expandedDir, "tailscaled.state")
	_, err := os.Stat(stateFile)
	localStateExists := err == nil

	if localStateExists {
		// 本地状态存在，直接使用（快速路径）
		logger.Infof("使用本地状态目录（有状态模式）: %s", expandedDir)
		m.stateDir = expandedDir
		m.isTemp = false
		return nil
	}

	// 本地状态不存在，创建目录
	if err := os.MkdirAll(expandedDir, 0700); err != nil {
		return fmt.Errorf("创建状态目录失败: %w", err)
	}

	logger.Infof("本地状态不存在，尝试从 Server 加载: %s", expandedDir)
	m.stateDir = expandedDir
	m.isTemp = false

	// 从 Server 加载历史状态
	if err := m.loadAndRestoreState(); err != nil {
		logger.Debugf("从 Server 加载状态失败: %v，使用空状态", err)
	}

	return nil
}

// loadAndRestoreState 从 Server 加载状态并恢复到本地
func (m *TailscaleManager) loadAndRestoreState() error {
	// 如果 grpcClient 为 nil，跳过加载
	if m.grpcClient == nil {
		return fmt.Errorf("gRPC 客户端未初始化")
	}

	// 从 Server 加载状态
	stateData, err := m.loadStateFromServer()
	if err != nil {
		return fmt.Errorf("从 Server 加载状态失败: %w", err)
	}

	// 如果没有历史状态，返回
	if len(stateData) == 0 {
		logger.Debug("Server 上没有历史状态")
		return nil
	}

	// 恢复状态到本地
	if err := m.restoreState(stateData); err != nil {
		return fmt.Errorf("恢复状态失败: %w", err)
	}

	logger.Info("成功从 Server 加载并恢复状态")
	return nil
}

// loadStateFromServer 从 Server 加载状态
// 注意：当前 proto 设计中未包含状态同步 API，此功能暂时禁用
func (m *TailscaleManager) loadStateFromServer() ([]byte, error) {
	// TODO: 如需状态同步功能，需要在 proto 中添加相应的 RPC 方法
	logger.Debug("状态同步功能暂未启用")
	return nil, nil
}

// restoreState 恢复状态到本地（解压并写入）
func (m *TailscaleManager) restoreState(compressedData []byte) error {
	if len(compressedData) == 0 {
		return fmt.Errorf("状态数据为空")
	}

	// 解压数据
	stateData, err := m.decompressState(compressedData)
	if err != nil {
		return fmt.Errorf("解压状态数据失败: %w", err)
	}

	// 验证状态数据（基本检查）
	if len(stateData) == 0 {
		return fmt.Errorf("解压后的状态数据为空")
	}

	// 写入状态文件
	stateFile := filepath.Join(m.stateDir, "tailscaled.state")
	if err := os.WriteFile(stateFile, stateData, 0600); err != nil {
		return fmt.Errorf("写入状态文件失败: %w", err)
	}

	logger.Infof("状态已恢复到: %s", stateFile)
	return nil
}

// decompressState 解压状态数据
func (m *TailscaleManager) decompressState(compressedData []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(compressedData))
	if err != nil {
		return nil, fmt.Errorf("创建 gzip reader 失败: %w", err)
	}
	defer reader.Close()

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, reader); err != nil {
		return nil, fmt.Errorf("解压数据失败: %w", err)
	}

	return buf.Bytes(), nil
}

// compressState 压缩状态文件
func (m *TailscaleManager) compressState() ([]byte, error) {
	// 读取状态文件
	stateFile := filepath.Join(m.stateDir, "tailscaled.state")
	stateData, err := os.ReadFile(stateFile)
	if err != nil {
		// 如果状态文件不存在或无法读取，返回错误
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("状态文件不存在: %s", stateFile)
		}
		return nil, fmt.Errorf("读取状态文件失败: %w", err)
	}

	// 检查状态文件是否为空
	if len(stateData) == 0 {
		return nil, fmt.Errorf("状态文件为空")
	}

	// 压缩数据
	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	if _, err := writer.Write(stateData); err != nil {
		writer.Close()
		return nil, fmt.Errorf("压缩数据失败: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("关闭 gzip writer 失败: %w", err)
	}

	logger.Debugf("状态压缩完成: %d -> %d 字节", len(stateData), buf.Len())
	return buf.Bytes(), nil
}

// saveStateToServer 保存状态到 Server
// 注意：当前 proto 设计中未包含状态同步 API，此功能暂时禁用
func (m *TailscaleManager) saveStateToServer() error {
	// TODO: 如需状态同步功能，需要在 proto 中添加相应的 RPC 方法
	logger.Debug("状态同步功能暂未启用")
	return nil
}

// periodicStateSave 定期保存状态到 Server
func (m *TailscaleManager) periodicStateSave() {
	// 获取同步间隔（分钟）
	interval := m.config.Tailscale.StateSyncInterval
	if interval <= 0 {
		interval = 5 // 默认 5 分钟
	}

	ticker := time.NewTicker(time.Duration(interval) * time.Minute)
	defer ticker.Stop()

	logger.Infof("启动定期状态同步，间隔: %d 分钟", interval)

	for {
		select {
		case <-m.ctx.Done():
			logger.Debug("定期状态同步协程退出")
			return
		case <-ticker.C:
			logger.Debug("执行定期状态同步...")
			if err := m.saveStateToServer(); err != nil {
				logger.Warnf("定期状态同步失败: %v", err)
			} else {
				logger.Debug("定期状态同步成功")
			}
		}
	}
}

// WaitForConnection 等待 Tailscale 连接就绪
func (m *TailscaleManager) WaitForConnection(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if m.IsConnected() && m.GetIP() != "" {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("等待 Tailscale 连接超时")
}

// monitorConnectionStatus 监控连接状态并更新连接类型
func (m *TailscaleManager) monitorConnectionStatus() {
	// 启动时立即更新一次状态
	m.updateConnectionStatus()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.updateConnectionStatus()
		}
	}
}

// updateConnectionStatus 更新连接状态
func (m *TailscaleManager) updateConnectionStatus() {
	if m.tsServer == nil {
		return
	}

	// 获取本地客户端
	lc, err := m.tsServer.LocalClient()
	if err != nil {
		logger.Debugf("获取 LocalClient 失败: %v", err)
		return
	}

	// 获取状态
	status, err := lc.Status(m.ctx)
	if err != nil {
		logger.Debugf("获取 Tailscale 状态失败: %v", err)
		return
	}

	// 更新连接状态
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// 检查后端状态
	if status.BackendState == "Running" {
		m.connected = true
	} else {
		m.connected = false
		logger.Debugf("Tailscale 后端状态: %s", status.BackendState)
	}

	// 检查连接类型
	// 优先检查是否有 peer 使用 DERP 中继
	hasDERP := false
	hasDirectPeer := false
	for _, peer := range status.Peer {
		if peer.CurAddr == "" && peer.Relay != "" {
			hasDERP = true
		} else if peer.CurAddr != "" {
			hasDirectPeer = true
		}
	}

	if hasDERP && !hasDirectPeer {
		// 所有连接都通过 DERP
		m.connType = ConnTypeDERP
	} else if hasDirectPeer {
		// 有直连的 peer
		m.connType = ConnTypeP2P
	} else if m.connected {
		// 已连接但没有 peer，保持当前类型或默认 P2P
		// 这种情况下 Agent 已连接到 Headscale，只是还没有其他节点通信
		if m.connType == "" || m.connType == ConnTypeUnknown {
			m.connType = ConnTypeP2P
		}
	} else {
		m.connType = ConnTypeUnknown
	}
}

// UpdateConnType 手动触发连接类型更新
func (m *TailscaleManager) UpdateConnType() {
	m.updateConnectionStatus()
}

// tailscaleLogf 自定义 Tailscale 日志输出函数，统一格式
func (m *TailscaleManager) tailscaleLogf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)

	// 过滤掉重复的错误信息和调试信息
	if strings.Contains(msg, "certificate signed by unknown authority") {
		// 证书错误只记录一次
		return
	}
	if strings.Contains(msg, "authRoutine:") ||
		strings.Contains(msg, "TryLogin:") ||
		strings.Contains(msg, "doLogin(") ||
		strings.Contains(msg, "LoginInteractive") ||
		strings.Contains(msg, "sendStatus:") ||
		strings.Contains(msg, "backoff:") {
		// 过滤认证重试的详细日志
		return
	}
	if strings.Contains(msg, "Received error: fetch control key") {
		// 过滤重复的连接错误
		return
	}
	if strings.Contains(msg, "[v1]") && (strings.Contains(msg, "using fake") ||
		strings.Contains(msg, "DNS configurator") ||
		strings.Contains(msg, "OS network configurator")) {
		// 过滤初始化的详细信息
		return
	}
	if strings.Contains(msg, "magicsock: [warning] failed to force-set UDP") {
		// 过滤 UDP buffer 警告（不影响功能）
		return
	}

	// 使用 logrus 输出，保持格式一致
	logger.Infof("[tunnel] %s", msg)
}
