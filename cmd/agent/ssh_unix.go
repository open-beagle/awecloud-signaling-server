//go:build !windows

package main

import (
	"fmt"
	"os"
	"runtime"
	"syscall"
)

// switchUserIdentity 切换用户身份（仅 Unix-like 系统）
func switchUserIdentity(uid, gid int, groups []int) error {
	// 锁定当前 goroutine 到 OS 线程，确保 setuid/setgid 生效
	runtime.LockOSThread()

	// 切换用户身份
	// 顺序很重要：setgroups -> setgid -> setuid
	// 参考 Tailscale 的实现
	if len(groups) > 0 {
		// 在 Darwin/FreeBSD 上，第一个组应该是 gid
		if (runtime.GOOS == "darwin" || runtime.GOOS == "freebsd") && (len(groups) == 0 || groups[0] != gid) {
			groups = append([]int{gid}, groups...)
		}
		if err := syscall.Setgroups(groups); err != nil {
			return fmt.Errorf("failed to setgroups: %w", err)
		}
	}
	if gid >= 0 && os.Getegid() != gid {
		if err := syscall.Setgid(gid); err != nil {
			return fmt.Errorf("failed to setgid(%d): %w", gid, err)
		}
	}
	if uid >= 0 && os.Geteuid() != uid {
		if err := syscall.Setuid(uid); err != nil {
			return fmt.Errorf("failed to setuid(%d): %w", uid, err)
		}
	}

	return nil
}

// execShell 使用 syscall.Exec 替换当前进程为 shell
// 如果 cmd 不为空，执行命令；否则启动交互式登录 shell
func execShell(loginShell string, env []string, cmd string) error {
	if cmd != "" {
		// 命令执行模式：使用 -c 参数执行命令
		return syscall.Exec(loginShell, []string{loginShell, "-c", cmd}, env)
	}
	// 交互式模式：启动登录 shell
	return syscall.Exec(loginShell, []string{loginShell, "-l"}, env)
}
