package service

import (
	"fmt"
	"net"

	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

// IPAllocator IP 分配服务
type IPAllocator struct {
	db            *gorm.DB
	networkConfig *NetworkConfig
}

// NewIPAllocator 创建 IP 分配服务
func NewIPAllocator(db *gorm.DB, networkConfig *NetworkConfig) *IPAllocator {
	return &IPAllocator{
		db:            db,
		networkConfig: networkConfig,
	}
}

// AllocateAgentIP 为 Agent 分配固定 IP
func (ia *IPAllocator) AllocateAgentIP() (string, error) {
	plan, err := ia.networkConfig.GetNetworkPlan()
	if err != nil {
		return "", fmt.Errorf("failed to get network plan: %w", err)
	}

	return ia.allocateIP(plan.Agent, "agent")
}

// AllocateDesktopIP 为 Desktop 分配固定 IP
func (ia *IPAllocator) AllocateDesktopIP() (string, error) {
	plan, err := ia.networkConfig.GetNetworkPlan()
	if err != nil {
		return "", fmt.Errorf("failed to get network plan: %w", err)
	}

	return ia.allocateIP(plan.Desktop, "desktop")
}

// AllocateServerIP 为 Server 分配固定 IP
func (ia *IPAllocator) AllocateServerIP() (string, error) {
	plan, err := ia.networkConfig.GetNetworkPlan()
	if err != nil {
		return "", fmt.Errorf("failed to get network plan: %w", err)
	}

	return ia.allocateIP(plan.Server, "server")
}

// allocateIP 从指定网段分配 IP
func (ia *IPAllocator) allocateIP(segment NetworkSegment, nodeType string) (string, error) {
	// 获取已分配的 IP 列表
	usedIPs, err := ia.getUsedIPs(nodeType)
	if err != nil {
		return "", fmt.Errorf("failed to get used IPs: %w", err)
	}

	// 遍历可用 IP 范围，找到第一个未使用的 IP
	startIP := net.ParseIP(segment.IPStart)
	endIP := net.ParseIP(segment.IPEnd)

	for ip := startIP; !ip.Equal(endIP); ip = nextIP(ip) {
		ipStr := ip.String()
		if !contains(usedIPs, ipStr) {
			return ipStr, nil
		}
	}

	// 检查结束 IP
	if !contains(usedIPs, endIP.String()) {
		return endIP.String(), nil
	}

	return "", fmt.Errorf("no available IP in %s segment", nodeType)
}

// getUsedIPs 获取已分配的 IP 列表
func (ia *IPAllocator) getUsedIPs(nodeType string) ([]string, error) {
	var ips []string

	switch nodeType {
	case "agent":
		// 查询 Agent 角色用户的所有 Node
		var nodes []model.Node
		if err := ia.db.Joins("JOIN user ON user.id = node.user_id").
			Where("user.role = ? AND node.type = ?", model.UserRoleAgent, model.NodeTypeAgent).
			Select("node.ip").Find(&nodes).Error; err != nil {
			return nil, err
		}
		for _, node := range nodes {
			if node.IP != "" {
				ips = append(ips, node.IP)
			}
		}

	case "desktop":
		// 查询 Client 角色用户的所有 Node
		var nodes []model.Node
		if err := ia.db.Joins("JOIN user ON user.id = node.user_id").
			Where("user.role = ? AND node.type = ?", model.UserRoleClient, model.NodeTypeDesktop).
			Select("node.ip").Find(&nodes).Error; err != nil {
			return nil, err
		}
		for _, node := range nodes {
			if node.IP != "" {
				ips = append(ips, node.IP)
			}
		}

	case "server":
		// TODO: 从 Server 节点表查询（如果有的话）
	}

	return ips, nil
}

// nextIP 计算下一个 IP
func nextIP(ip net.IP) net.IP {
	next := make(net.IP, len(ip))
	copy(next, ip)

	for i := len(next) - 1; i >= 0; i-- {
		next[i]++
		if next[i] > 0 {
			break
		}
	}

	return next
}

// contains 检查 IP 是否在列表中
func contains(ips []string, ip string) bool {
	for _, i := range ips {
		if i == ip {
			return true
		}
	}
	return false
}
