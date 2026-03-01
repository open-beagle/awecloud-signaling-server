// Package agent 提供 Agent 端功能
// dns_config.go 管理 /etc/resolv.conf 配置
package agent

import (
	"fmt"
	"os"
	"strings"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
)

const (
	dnsResolvConfPath = "/etc/resolv.conf"
	dnsMarkerBegin    = "# AWECloud Signaling Agent DNS Configuration"
	dnsMarkerEnd      = "# End of AWECloud Signaling Agent DNS Configuration"
)

// DNSConfigManager 管理 /etc/resolv.conf 配置
type DNSConfigManager struct {
	originalContent string // 原始配置内容
	dnsServer       string // Signal DNS 服务器地址（如 127.0.0.2）
	upstreamDNS     string // 上游 DNS 服务器地址（从原始配置提取）
}

// NewDNSConfigManager 创建 DNS 配置管理器
func NewDNSConfigManager(dnsServer string) *DNSConfigManager {
	return &DNSConfigManager{
		dnsServer: dnsServer,
	}
}

// Setup 设置 DNS 配置
// 1. 备份原始配置
// 2. 提取上游 DNS
// 3. 修改 resolv.conf，将 Signal DNS 放在第一位
func (m *DNSConfigManager) Setup() error {
	// 1. 读取原始配置
	content, err := os.ReadFile(dnsResolvConfPath)
	if err != nil {
		return fmt.Errorf("读取 %s 失败: %w", dnsResolvConfPath, err)
	}
	m.originalContent = string(content)

	// 2. 提取上游 DNS（第一个 nameserver）
	m.upstreamDNS = extractUpstreamDNS(m.originalContent)
	if m.upstreamDNS == "" {
		m.upstreamDNS = "8.8.8.8" // 兜底
		logger.Warnf("[DNS] 未找到上游 DNS，使用默认: %s", m.upstreamDNS)
	} else {
		logger.Infof("[DNS] 检测到上游 DNS: %s", m.upstreamDNS)
	}

	// 3. 构建新配置
	newContent := m.buildNewConfig()

	// 4. 写入新配置
	if err := os.WriteFile(dnsResolvConfPath, []byte(newContent), 0644); err != nil {
		return fmt.Errorf("写入 %s 失败: %w", dnsResolvConfPath, err)
	}

	logger.Infof("[DNS] 已更新 %s（Signal DNS: %s, 上游: %s）", dnsResolvConfPath, m.dnsServer, m.upstreamDNS)
	return nil
}

// Restore 恢复原始 DNS 配置
func (m *DNSConfigManager) Restore() error {
	if m.originalContent == "" {
		logger.Warn("[DNS] 没有原始配置可恢复")
		return nil
	}

	if err := os.WriteFile(dnsResolvConfPath, []byte(m.originalContent), 0644); err != nil {
		return fmt.Errorf("恢复 %s 失败: %w", dnsResolvConfPath, err)
	}

	logger.Infof("[DNS] 已恢复 %s", dnsResolvConfPath)
	return nil
}

// GetUpstreamDNS 获取上游 DNS 地址
func (m *DNSConfigManager) GetUpstreamDNS() string {
	return m.upstreamDNS
}

// buildNewConfig 构建新的 resolv.conf 内容
func (m *DNSConfigManager) buildNewConfig() string {
	var sb strings.Builder

	// 标记块开始
	sb.WriteString(dnsMarkerBegin + "\n")
	sb.WriteString("# Managed by Signal Agent - DO NOT EDIT MANUALLY\n")
	sb.WriteString(fmt.Sprintf("# Original DNS: %s\n", m.upstreamDNS))
	sb.WriteString(fmt.Sprintf("# Signal DNS Server: %s:53 (intercepts .beagle domains only)\n", m.dnsServer))
	sb.WriteString(dnsMarkerEnd + "\n")

	// Signal DNS（第一位）
	sb.WriteString(fmt.Sprintf("nameserver %s\n", m.dnsServer))

	// 上游 DNS（第二位）
	sb.WriteString(fmt.Sprintf("nameserver %s\n", m.upstreamDNS))

	// 保留原始配置中的其他行（search、options 等）
	for _, line := range strings.Split(m.originalContent, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "nameserver") {
			continue // 跳过 nameserver 行（已处理）
		}
		// 保留 search、options 等行
		sb.WriteString(line + "\n")
	}

	return sb.String()
}

// extractUpstreamDNS 从 resolv.conf 内容中提取第一个 nameserver
// 跳过 127.0.0.1（Tailscale DNS）和 127.0.0.2（Signal DNS）
func extractUpstreamDNS(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "nameserver") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				dns := fields[1]
				// 跳过本地 DNS（Tailscale 和 Signal）
				if dns == "127.0.0.1" || dns == "127.0.0.2" {
					continue
				}
				return dns
			}
		}
	}
	return ""
}
