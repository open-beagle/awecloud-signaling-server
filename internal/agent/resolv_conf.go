package agent

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
)

const (
	resolvConfPath       = "/etc/resolv.conf"
	resolvConfBackupPath = "/etc/resolv.conf.signal.bak"
)

// detectUpstreamDNS 从 /etc/resolv.conf 检测上游 DNS
// 优先从备份文件读取，避免重复启动时丢失原始 DNS
func detectUpstreamDNS() string {
	// 1. 优先从备份文件读取
	if _, err := os.Stat(resolvConfBackupPath); err == nil {
		if dns := readUpstreamDNSFromFile(resolvConfBackupPath); dns != "" {
			logger.Debugf("从备份文件检测到上游 DNS: %s", dns)
			return dns
		}
	}

	// 2. 从当前 resolv.conf 读取
	if dns := readUpstreamDNSFromFile(resolvConfPath); dns != "" {
		logger.Debugf("从 resolv.conf 检测到上游 DNS: %s", dns)
		return dns
	}

	// 3. 使用默认 DNS
	logger.Warn("未检测到有效的上游 DNS，使用默认 DNS 8.8.8.8")
	return "8.8.8.8:53"
}

// readUpstreamDNSFromFile 从指定文件读取上游 DNS
func readUpstreamDNSFromFile(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "nameserver") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				dns := parts[1]
				// 跳过 127.0.0.1（可能是之前的本地 DNS）
				if dns != "127.0.0.1" {
					return dns + ":53"
				}
			}
		}
	}

	return ""
}

// modifyResolvConf 修改 /etc/resolv.conf，指向本地 DNS
// 保留原始 DNS 作为备份（fallback）
func modifyResolvConf(upstreamDNS string) error {
	// 备份原文件
	if err := backupResolvConf(); err != nil {
		return fmt.Errorf("备份 resolv.conf 失败: %w", err)
	}

	// 提取上游 DNS IP（去掉端口）
	upstreamIP := strings.TrimSuffix(upstreamDNS, ":53")

	// 写入新配置（指向 127.0.0.1:15353，保留原始 DNS 作为备份）
	content := fmt.Sprintf("# Signal Agent DNS (upstream: %s)\nnameserver 127.0.0.1\nnameserver %s\noptions ndots:0\n", upstreamDNS, upstreamIP)
	if err := os.WriteFile(resolvConfPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("写入 resolv.conf 失败: %w", err)
	}

	logger.Infof("已修改 %s，指向本地 DNS 127.0.0.1:15353（备份 DNS: %s）", resolvConfPath, upstreamIP)
	return nil
}

// restoreResolvConf 恢复 /etc/resolv.conf
func restoreResolvConf() error {
	// 检查备份文件是否存在
	if _, err := os.Stat(resolvConfBackupPath); os.IsNotExist(err) {
		logger.Debug("备份文件不存在，跳过恢复")
		return nil
	}

	// 读取备份内容
	content, err := os.ReadFile(resolvConfBackupPath)
	if err != nil {
		return fmt.Errorf("读取备份文件失败: %w", err)
	}

	// 恢复原文件
	if err := os.WriteFile(resolvConfPath, content, 0644); err != nil {
		return fmt.Errorf("恢复 resolv.conf 失败: %w", err)
	}

	// 删除备份文件
	os.Remove(resolvConfBackupPath)

	logger.Infof("已恢复 %s", resolvConfPath)
	return nil
}

// backupResolvConf 备份 /etc/resolv.conf
func backupResolvConf() error {
	content, err := os.ReadFile(resolvConfPath)
	if err != nil {
		return err
	}

	if err := os.WriteFile(resolvConfBackupPath, content, 0644); err != nil {
		return err
	}

	logger.Debugf("已备份 %s -> %s", resolvConfPath, resolvConfBackupPath)
	return nil
}
