// Package agent 提供 Agent 端功能
package agent

import (
	"net"
	"os"
	"sort"
	"strings"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
)

// NetworkInfo 网络信息
type NetworkInfo struct {
	LanIP        string `json:"lan_ip"`        // 局域网 IP
	LanMask      string `json:"lan_mask"`      // 子网掩码
	LanGateway   string `json:"lan_gateway"`   // 网关地址
	LanInterface string `json:"lan_interface"` // 网卡名称
	RuntimeEnv   string `json:"runtime_env"`   // 运行环境: native/docker/kubernetes
	Hostname     string `json:"hostname"`      // 主机名
}

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

// DetectNetworkInfo 检测完整的网络信息
func (d *LANDetector) DetectNetworkInfo() *NetworkInfo {
	info := &NetworkInfo{
		LanIP:      "127.0.0.1",
		RuntimeEnv: d.detectRuntimeEnv(),
		Hostname:   d.detectHostname(),
	}

	// 检测局域网接口
	iface, ip, mask := d.detectLANInterface()
	if ip != "" {
		info.LanIP = ip
		info.LanMask = mask
		info.LanInterface = iface
		info.LanGateway = d.detectGateway(iface)
	}

	return info
}

// DetectLANIP 检测局域网 IP（保持向后兼容）
// 返回检测到的局域网 IP，如果检测失败则返回 127.0.0.1
func (d *LANDetector) DetectLANIP() string {
	_, ip, _ := d.detectLANInterface()
	if ip == "" {
		return "127.0.0.1"
	}
	return ip
}

// detectLANInterface 检测局域网接口和 IP
// 返回接口名称、IP 地址和子网掩码
func (d *LANDetector) detectLANInterface() (string, string, string) {
	interfaces, err := net.Interfaces()
	if err != nil {
		logger.Warnf("获取网络接口失败: %v", err)
		return "", "", ""
	}

	type candidate struct {
		iface    string
		ip       string
		mask     string
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

			// 获取子网掩码
			mask := net.IP(ipNet.Mask).String()

			priority := d.getPriority(iface.Name)
			candidates = append(candidates, candidate{
				iface:    iface.Name,
				ip:       ip.String(),
				mask:     mask,
				priority: priority,
			})
		}
	}

	if len(candidates) == 0 {
		logger.Warn("未检测到局域网 IP")
		return "", "", ""
	}

	// 按优先级排序
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].priority < candidates[j].priority
	})

	selected := candidates[0]
	logger.Infof("检测到局域网 IP: %s/%s (接口: %s)", selected.ip, selected.mask, selected.iface)

	return selected.iface, selected.ip, selected.mask
}

// detectGateway 检测网关地址
// 通过读取 /proc/net/route 获取默认网关
func (d *LANDetector) detectGateway(ifaceName string) string {
	// 读取路由表
	data, err := os.ReadFile("/proc/net/route")
	if err != nil {
		logger.Debugf("读取路由表失败: %v", err)
		return ""
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines[1:] { // 跳过标题行
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}

		// 检查是否是默认路由（目标为 00000000）
		if fields[1] == "00000000" {
			// 解析网关地址（小端序十六进制）
			gateway := d.parseHexIP(fields[2])
			if gateway != "" {
				logger.Debugf("检测到网关: %s (接口: %s)", gateway, fields[0])
				return gateway
			}
		}
	}

	return ""
}

// parseHexIP 解析十六进制 IP 地址（小端序）
func (d *LANDetector) parseHexIP(hex string) string {
	if len(hex) != 8 {
		return ""
	}

	// 小端序转换：每两个字符是一个字节，从后往前读
	var ip [4]byte
	for i := 0; i < 4; i++ {
		b := d.hexToByte(hex[i*2 : i*2+2])
		ip[3-i] = byte(b)
	}

	return net.IPv4(ip[0], ip[1], ip[2], ip[3]).String()
}

// hexToByte 十六进制字符串转字节
func (d *LANDetector) hexToByte(s string) int {
	var result int
	for _, c := range s {
		result *= 16
		if c >= '0' && c <= '9' {
			result += int(c - '0')
		} else if c >= 'A' && c <= 'F' {
			result += int(c - 'A' + 10)
		} else if c >= 'a' && c <= 'f' {
			result += int(c - 'a' + 10)
		}
	}
	return result
}

// detectRuntimeEnv 检测运行环境
func (d *LANDetector) detectRuntimeEnv() string {
	// 检测 Kubernetes
	if _, err := os.Stat("/var/run/secrets/kubernetes.io"); err == nil {
		return "kubernetes"
	}

	// 检测 Docker
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return "docker"
	}

	// 检查 cgroup（Docker 和 K8s 都会有）
	if data, err := os.ReadFile("/proc/1/cgroup"); err == nil {
		content := string(data)
		if strings.Contains(content, "docker") || strings.Contains(content, "kubepods") {
			if strings.Contains(content, "kubepods") {
				return "kubernetes"
			}
			return "docker"
		}
	}

	return "native"
}

// detectHostname 检测主机名
func (d *LANDetector) detectHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		logger.Debugf("获取主机名失败: %v", err)
		return ""
	}
	return hostname
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
