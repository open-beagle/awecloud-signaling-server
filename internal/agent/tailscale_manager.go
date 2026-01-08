// Package agent 提供 Agent 端功能
package agent

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"tailscale.com/tsnet"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
)

// TailscaleManager 管理 Tailscale 客户端连接
type TailscaleManager struct {
	tsServer *tsnet.Server
	config   *config.AgentConfig

	tailscaleIP string
	connected   bool
	connType    string // "p2p" or "derp"
	mutex       sync.RWMutex

	ctx    context.Context
	cancel context.CancelFunc
}

// NewTailscaleManager 创建 TailscaleManager
func NewTailscaleManager(cfg *config.AgentConfig, parentCtx context.Context) *TailscaleManager {
	ctx, cancel := context.WithCancel(parentCtx)
	return &TailscaleManager{
		config: cfg,
		ctx:    ctx,
		cancel: cancel,
	}
}

// Start 启动 Tailscale 客户端
func (m *TailscaleManager) Start(controlURL, authKey string) error {
	logger.Infof("启动 Tailscale 客户端，连接到: %s", controlURL)

	// 确定状态存储目录
	stateDir := m.getStateDir()
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		return fmt.Errorf("创建状态目录失败: %w", err)
	}

	// 创建 tsnet.Server
	m.tsServer = &tsnet.Server{
		Hostname:   m.config.Agent.AgentName,
		Dir:        stateDir,
		ControlURL: controlURL,
		AuthKey:    authKey,
		Ephemeral:  false, // Agent 需要持久化节点
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
		m.connType = "p2p" // 默认 P2P，实际连接类型需要从状态中获取
		m.mutex.Unlock()

		logger.Infof("Tailscale 已连接，IP: %s", m.tailscaleIP)
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

	if m.tsServer != nil {
		if err := m.tsServer.Close(); err != nil {
			logger.Warnf("关闭 Tailscale 失败: %v", err)
			return err
		}
	}

	logger.Info("Tailscale 已停止")
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

// getStateDir 获取状态存储目录
func (m *TailscaleManager) getStateDir() string {
	// 优先使用配置的目录
	if m.config.Tailscale.StateDir != "" {
		return m.config.Tailscale.StateDir
	}

	// 默认目录
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "/tmp"
	}
	return filepath.Join(homeDir, ".awecloud-agent", "tailscale")
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
