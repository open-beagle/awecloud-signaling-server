package main

import (
	"bufio"
	"os"
	"strconv"
	"strings"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
)

// detectSSHConfig 自动检测 SSH 配置信息
// 返回：可用用户列表
func detectSSHConfig() []string {
	// 读取系统用户列表（从 /etc/passwd）
	users := detectSystemUsers()

	logger.Infof("SSH 用户自动检测: users=%v", users)
	return users
}

// detectSystemUsers 从 /etc/passwd 读取系统用户列表
// 过滤掉系统服务用户，只保留真实用户
func detectSystemUsers() []string {
	file, err := os.Open("/etc/passwd")
	if err != nil {
		logger.Warnf("无法读取 /etc/passwd: %v", err)
		return []string{}
	}
	defer file.Close()

	var users []string
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// 格式：username:x:uid:gid:comment:home:shell
		fields := strings.Split(line, ":")
		if len(fields) < 7 {
			continue
		}

		username := fields[0]
		uidStr := fields[2]
		shell := fields[6]

		// 过滤条件：
		// 1. UID >= 1000（真实用户）或 UID = 0（root）
		// 2. shell 不是 /usr/sbin/nologin 或 /bin/false
		uid, err := strconv.Atoi(uidStr)
		if err != nil {
			continue
		}

		if (uid >= 1000 || uid == 0) &&
			!strings.HasSuffix(shell, "/nologin") &&
			!strings.HasSuffix(shell, "/false") {
			users = append(users, username)
		}
	}

	if err := scanner.Err(); err != nil {
		logger.Warnf("读取 /etc/passwd 出错: %v", err)
	}

	return users
}
