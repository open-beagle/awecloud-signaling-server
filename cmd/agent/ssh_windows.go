//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
)

// switchUserIdentity Windows 不支持用户身份切换
func switchUserIdentity(uid, gid int, groups []int) error {
	// Windows 不支持 Unix 风格的用户切换
	// 如果尝试使用这些参数，返回错误
	if uid >= 0 || gid >= 0 || len(groups) > 0 {
		return fmt.Errorf("user identity switching is not supported on Windows")
	}
	return nil
}

// execShell Windows 使用 cmd.exe 或 PowerShell
// 如果 cmd 不为空，执行命令；否则启动交互式 shell
func execShell(loginShell string, env []string, cmdStr string) error {
	// Windows 不支持 syscall.Exec，使用 exec.Command 替代
	// 但这不会替换当前进程，而是创建子进程

	// 如果没有指定 shell，使用 cmd.exe
	if loginShell == "" || loginShell == "/bin/bash" {
		loginShell = "cmd.exe"
	}

	var cmd *exec.Cmd
	if cmdStr != "" {
		// 命令执行模式：使用 /c 参数执行命令
		cmd = exec.Command(loginShell, "/c", cmdStr)
	} else {
		// 交互式模式：启动 shell
		cmd = exec.Command(loginShell)
	}

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = env

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to execute shell: %w", err)
	}

	os.Exit(0)
	return nil
}
