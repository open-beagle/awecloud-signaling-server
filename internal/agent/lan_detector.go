// Package agent 提供 Agent 端功能
package agent

import (
	"net"
	"sort"
	"strings"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
)

// LANDetector 局域网 IP 检测器
type LANDetector struct {
	// 接口黑名单前缀
	blacklistPrefixes []string
	// 优先接口前缀（按优先级排序）
	priorityPrefixes []string
}

// NewLANDetector 创建局域网 IP 检测器
func NewLANDetector() *LANDetector {
	return &LANDetector{
		blacklistPrefixes: []string{
			// Docker
			"docker", "br-", "veth",
			// K8s CNI
			"cni", "flannel", "calico", "weave",
			// libvirt
			"virbr",
			// VMware
			"vmnet", "VMware",
			// Hyper-V
			"vEthernet",
			// 回环
			"lo",
			// Tailscale
			"tailscale", "ts",
		},
		priorityPrefixes: []string{
			// 物理以太网（最高优先级）
			"eth", "en", "ens", "eno", "enp",
			// 无线网卡（次选）
			"wlan", "wl", "wlp",
		},
	}
}

// DetectLANIP 检测局域网 IP
// 返回检测到的局域网 IP，如果检测失败则返回 127.0.0.1
func (d *LANDetector) DetectLANIP() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		logger.Warnf("获取网络接口失败: %v，回退到 127.0.0.1", err)
		return "127.0.0.1"
	}

	type candidate struct {
		iface    string
		ip       string
		priority int // 优先级，数字越小优先级越高
	}

	var candidates []candidate

	for _, iface := range interfaces {
		// 跳过 down 的接口
		if iface.Flags&net.FlagUp == 0 {
			continue
		}

		// 跳过回环接口
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		// 检查黑名单
		if d.isBlacklisted(iface.Name) {
			logger.Debugf("跳过黑名单接口: %s", iface.Name)
			continue
		}

		// 获取接口地址
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}

			ip := ipNet.IP
			// 只处理 IPv4
			if ip.To4() == nil {
				continue
			}

			// 只保留私有 IP
			if !d.isPrivateIP(ip) {
				continue
			}

			priority := d.getPriority(iface.Name)
			candidates = append(candidates, candidate{
				iface:    iface.Name,
				ip:       ip.String(),
				priority: priority,
			})
		}
	}

	if len(candidates) == 0 {
		logger.Warn("未检测到局域网 IP，回退到 127.0.0.1")
		return "127.0.0.1"
	}

	// 按优先级排序
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].priority < candidates[j].priority
	})

	selected := candidates[0]
	logger.Infof("检测到局域网 IP: %s (接口: %s)", selected.ip, selected.iface)

	return selected.ip
}

// isBlacklisted 检查接口是否在黑名单中
func (d *LANDetector) isBlacklisted(name string) bool {
	nameLower := strings.ToLower(name)
	for _, prefix := range d.blacklistPrefixes {
		if strings.HasPrefix(nameLower, strings.ToLower(prefix)) {
			return true
		}
	}
	return false
}

// isPrivateIP 检查是否为私有 IP
// 私有 IP 范围: 10.x.x.x, 172.16-31.x.x, 192.168.x.x
func (d *LANDetector) isPrivateIP(ip net.IP) bool {
	if ip4 := ip.To4(); ip4 != nil {
		// 10.0.0.0/8
		if ip4[0] == 10 {
			return true
		}
		// 172.16.0.0/12
		if ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31 {
			return true
		}
		// 192.168.0.0/16
		if ip4[0] == 192 && ip4[1] == 168 {
			return true
		}
	}
	return false
}

// getPriority 获取接口优先级
// 返回值越小优先级越高
func (d *LANDetector) getPriority(name string) int {
	nameLower := strings.ToLower(name)
	for i, prefix := range d.priorityPrefixes {
		if strings.HasPrefix(nameLower, strings.ToLower(prefix)) {
			return i
		}
	}
	// 其他接口优先级最低
	return len(d.priorityPrefixes)
}
