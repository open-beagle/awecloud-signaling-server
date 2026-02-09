// Package agent 提供 Agent 端功能
// ssh_config.go 自动维护 ~/.ssh/config，添加 *.k8s 的 ProxyCommand 配置
package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
)

// buildSSHConfigBlock 构建 SSH config 标记块
// 生成 Host *.k8s 的 ProxyCommand 配置，使用 signal_agent dial 子命令
func buildSSHConfigBlock(execPath, dialSocketPath string) string {
	var sb strings.Builder
	sb.WriteString(sshConfigMarkerBegin + "\n")
	sb.WriteString("Host *.k8s\n")
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

// MaintainSSHConfig 维护 ~/.ssh/config，添加 *.k8s 的 ProxyCommand 配置
// 使用标记块包裹，避免影响用户已有配置
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

	// 获取 ~/.ssh/config 路径
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("获取用户主目录失败: %w", err)
	}

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
