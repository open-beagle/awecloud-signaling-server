// Package agent 提供 Agent 端功能
// ssh_config.go 自动维护 ~/.ssh/config，添加 *.beagle 的 ProxyCommand 配置
package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
)

// buildSSHConfigBlock 构建 SSH config 标记块
// 生成 Host *.beagle 的 ProxyCommand 配置，使用 signal_agent dial 子命令
func buildSSHConfigBlock(execPath, dialSocketPath string) string {
	var sb strings.Builder
	sb.WriteString(sshConfigMarkerBegin + "\n")
	sb.WriteString("Host *.beagle\n")
	sb.WriteString(fmt.Sprintf("  ProxyCommand %s dial %%h %%p\n", execPath))
	sb.WriteString("  StrictHostKeyChecking no\n")
	sb.WriteString("  UserKnownHostsFile /dev/null\n")
	sb.WriteString("  LogLevel ERROR\n")
	sb.WriteString(sshConfigMarkerEnd + "\n")
	return sb.String()
}

// replaceOrAppendBlock 替换或追加标记块
// 如果已有标记块则替换，否则追加到文件末尾
func replaceOrAppendBlock(existing, block string) string {
	beginIdx := strings.Index(existing, sshConfigMarkerBegin)
	endIdx := strings.Index(existing, sshConfigMarkerEnd)

	if beginIdx >= 0 && endIdx >= 0 {
		// 找到已有标记块，替换
		endIdx += len(sshConfigMarkerEnd)
		// 跳过标记块后的换行符
		if endIdx < len(existing) && existing[endIdx] == '\n' {
			endIdx++
		}
		return existing[:beginIdx] + block + existing[endIdx:]
	}

	// 没有已有标记块，追加到末尾
	result := strings.TrimRight(existing, "\n")
	if result != "" {
		result += "\n\n"
	}
	result += block
	return result
}

const (
	// sshConfigMarkerBegin 标记块开始
	sshConfigMarkerBegin = "# >>> AWECloud Signaling >>>"
	// sshConfigMarkerEnd 标记块结束
	sshConfigMarkerEnd = "# <<< AWECloud Signaling <<<"
)

// MaintainSSHConfig 维护 ~/.ssh/config，添加 *.beagle 的 ProxyCommand 配置
// 使用标记块包裹，避免影响用户已有配置
// 注意：Agent 可能以 sudo 运行，需要获取真实用户的 home 目录
func MaintainSSHConfig(dialSocketPath string) error {
	// 获取当前二进制路径
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取二进制路径失败: %w", err)
	}
	// 解析符号链接
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("解析二进制路径失败: %w", err)
	}

	// 构建 SSH config 块
	block := buildSSHConfigBlock(execPath, dialSocketPath)

	// 获取真实用户的 home 目录（sudo 场景下用 SUDO_USER）
	homeDir := getRealUserHomeDir()

	sshDir := filepath.Join(homeDir, ".ssh")
	sshConfigPath := filepath.Join(sshDir, "config")

	// 确保 ~/.ssh 目录存在
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		return fmt.Errorf("创建 ~/.ssh 目录失败: %w", err)
	}

	// 读取现有配置
	existingContent := ""
	if data, err := os.ReadFile(sshConfigPath); err == nil {
		existingContent = string(data)
	}

	// 替换或追加标记块
	newContent := replaceOrAppendBlock(existingContent, block)

	// 写入文件
	if err := os.WriteFile(sshConfigPath, []byte(newContent), 0600); err != nil {
		return fmt.Errorf("写入 ~/.ssh/config 失败: %w", err)
	}

	logger.Infof("[SSHConfig] 已更新 ~/.ssh/config（ProxyCommand → %s）", dialSocketPath)
	return nil
}

// getRealUserHomeDir 获取真实用户的 home 目录
// sudo 运行时 os.UserHomeDir() 返回 /root，需要通过 SUDO_USER 获取原始用户的 home
func getRealUserHomeDir() string {
	// 优先检查 SUDO_USER（sudo 场景）
	if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" {
		// 尝试从 /etc/passwd 或 HOME 环境变量推断
		// SUDO_USER 存在时，原始用户的 home 通常是 /home/{user}
		homeDir := "/home/" + sudoUser
		if _, err := os.Stat(homeDir); err == nil {
			logger.Infof("[SSHConfig] 检测到 sudo 运行，使用真实用户 home: %s", homeDir)
			return homeDir
		}
	}

	// 非 sudo 场景，使用标准方式
	homeDir, err := os.UserHomeDir()
	if err != nil {
		// 兜底
		return os.Getenv("HOME")
	}
	return homeDir
}
