package agent

import (
	"bufio"
	"os"
	"strconv"
	"strings"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
)

// detectSystemSSHUsers 自动检测系统 SSH 用户列表
// 返回：可用用户列表
func detectSystemSSHUsers() []string {
	var users []string

	// 方案 1：优先从环境变量获取当前用户（CloudIDE 等场景）
	currentUser := os.Getenv("USER")
	if currentUser == "" {
		currentUser = os.Getenv("LOGNAME")
	}
	if currentUser != "" && currentUser != "root" {
		// 验证用户在 /etc/passwd 中存在
		if userExists(currentUser) {
			users = append(users, currentUser)
			logger.Infof("SSH 用户检测: 从环境变量获取当前用户: %s", currentUser)
		}
	}

	// 方案 2：从 /etc/passwd 读取系统用户列表
	systemUsers := detectSystemUsers()
	for _, user := range systemUsers {
		// 避免重复添加
		if !contains(users, user) {
			users = append(users, user)
		}
	}

	logger.Infof("SSH 用户自动检测: users=%v", users)
	return users
}

// userExists 检查用户是否在 /etc/passwd 中存在
func userExists(username string) bool {
	file, err := os.Open("/etc/passwd")
	if err != nil {
		return false
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, ":")
		if len(fields) > 0 && fields[0] == username {
			return true
		}
	}
	return false
}

// contains 检查字符串切片是否包含指定元素
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
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
