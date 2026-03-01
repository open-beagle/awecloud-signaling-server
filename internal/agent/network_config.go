// Package agent 提供 Agent 端功能
// network_config.go 管理网络接口配置（VIP 地址段）
package agent

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
)

const (
	vipCIDR = "127.1.0.0/16" // VIP 地址段
)

// NetworkConfigManager 管理网络接口配置
type NetworkConfigManager struct {
	configured bool // 是否已配置
}

// NewNetworkConfigManager 创建网络配置管理器
func NewNetworkConfigManager() *NetworkConfigManager {
	return &NetworkConfigManager{}
}

// Setup 配置 VIP 地址段到 lo 接口
func (m *NetworkConfigManager) Setup() error {
	// 检查是否已配置
	if m.isConfigured() {
		logger.Info("[Network] VIP 地址段已配置")
		m.configured = true
		return nil
	}

	// 添加 VIP 地址段到 lo 接口
	cmd := exec.Command("ip", "addr", "add", vipCIDR, "dev", "lo")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("配置 VIP 地址段失败: %w, output: %s", err, string(output))
	}

	logger.Infof("[Network] 已配置 VIP 地址段: %s", vipCIDR)
	m.configured = true
	return nil
}

// Cleanup 清理 VIP 地址段配置
func (m *NetworkConfigManager) Cleanup() error {
	if !m.configured {
		return nil
	}

	// 删除 VIP 地址段
	cmd := exec.Command("ip", "addr", "del", vipCIDR, "dev", "lo")
	output, err := cmd.CombinedOutput()
	if err != nil {
		// 如果地址不存在，忽略错误
		if strings.Contains(string(output), "Cannot assign requested address") ||
			strings.Contains(string(output), "not found") {
			logger.Info("[Network] VIP 地址段已不存在，无需清理")
			return nil
		}
		return fmt.Errorf("清理 VIP 地址段失败: %w, output: %s", err, string(output))
	}

	logger.Infof("[Network] 已清理 VIP 地址段: %s", vipCIDR)
	m.configured = false
	return nil
}

// isConfigured 检查 VIP 地址段是否已配置
func (m *NetworkConfigManager) isConfigured() bool {
	cmd := exec.Command("ip", "addr", "show", "dev", "lo")
	output, err := cmd.CombinedOutput()
	if err != nil {
		logger.Warnf("[Network] 检查 VIP 地址段失败: %v", err)
		return false
	}

	return strings.Contains(string(output), vipCIDR)
}
