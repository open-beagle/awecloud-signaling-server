package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/creack/pty"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

// handleShellRequest 处理来自 Agent 的 Shell 请求
// 通过 gRPC OpenShell 双向流，spawn shell 并桥接 I/O
func handleShellRequest(ctx context.Context, client pb.EndpointServiceClient, cfg *EndpointConfig, req *pb.ShellRequest) {
	if req.Command != "" {
		logger.Infof("收到 Shell exec 请求: session_id=%s, login=%s, command=%s", req.SessionId, req.Login, req.Command)
	} else {
		logger.Infof("收到 Shell 请求: session_id=%s, login=%s", req.SessionId, req.Login)
	}

	// 查找系统用户
	u, err := user.Lookup(req.Login)
	if err != nil {
		logger.Warnf("Shell 请求失败: 用户 %s 不存在: %v", req.Login, err)
		sendShellError(ctx, client, cfg, req.SessionId, fmt.Sprintf("用户 %s 不存在", req.Login))
		return
	}

	// 获取用户的 login shell
	loginShell := u.HomeDir
	shell := getLoginShell(u)

	// 解析 UID/GID
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)

	// 建立 OpenShell gRPC 流
	stream, err := client.OpenShell(ctx)
	if err != nil {
		logger.Warnf("建立 OpenShell 流失败: %v", err)
		return
	}

	// 发送首包（携带 session_id 和 token）
	if err := stream.Send(&pb.ShellData{
		IsOpen:    true,
		SessionId: req.SessionId,
		Token:     cfg.Agent.Token,
	}); err != nil {
		logger.Warnf("发送 OpenShell 首包失败: %v", err)
		return
	}

	// 根据是否有 command 参数，选择不同的执行方式
	var cmd *exec.Cmd
	if req.Command != "" {
		// exec 模式：执行单个命令
		cmd = exec.Command(shell, "-c", req.Command)
	} else {
		// shell 模式：启动交互式 shell
		cmd = exec.Command(shell, "-l")
	}

	cmd.Dir = loginShell
	cmd.Env = buildShellEnv(u, shell)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{
			Uid: uint32(uid),
			Gid: uint32(gid),
		},
		Setsid: true,
	}

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Rows: uint16(req.Rows),
		Cols: uint16(req.Cols),
	})
	if err != nil {
		logger.Warnf("启动 PTY 失败: %v", err)
		stream.Send(&pb.ShellData{
			IsClose: true,
			Error:   fmt.Sprintf("启动 shell 失败: %v", err),
		})
		return
	}
	// 注意：ptmx 在 shell 退出后手动关闭（cmd.Wait 之后），不使用 defer

	if req.Command != "" {
		logger.Infof("Shell exec 已启动: session_id=%s, login=%s, command=%s, pid=%d",
			req.SessionId, req.Login, req.Command, cmd.Process.Pid)
	} else {
		logger.Infof("Shell 已启动: session_id=%s, login=%s, shell=%s, pid=%d",
			req.SessionId, req.Login, shell, cmd.Process.Pid)
	}

	// 双向桥接：gRPC stream ↔ PTY
	var wg sync.WaitGroup
	outputDone := make(chan struct{}) // PTY→gRPC 输出完成信号

	// PTY stdout → gRPC stream
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(outputDone)
		buf := make([]byte, 32*1024)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				if sendErr := stream.Send(&pb.ShellData{
					Data: buf[:n],
				}); sendErr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// gRPC stream → PTY stdin
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			msg, err := stream.Recv()
			if err != nil {
				// 流关闭，终止 shell
				cmd.Process.Signal(syscall.SIGHUP)
				return
			}

			if msg.IsClose {
				cmd.Process.Signal(syscall.SIGHUP)
				return
			}

			if msg.IsResize {
				pty.Setsize(ptmx, &pty.Winsize{
					Rows: uint16(msg.Rows),
					Cols: uint16(msg.Cols),
				})
				continue
			}

			if len(msg.Data) > 0 {
				ptmx.Write(msg.Data)
			}
		}
	}()

	// 等待 shell 退出
	exitCode := 0
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}

	// shell 已退出，关闭 PTY 以解除 PTY→gRPC 协程的 Read 阻塞
	ptmx.Close()

	// 等待 PTY→gRPC 输出完成（确保所有输出都已发送）
	<-outputDone

	// 发送退出码（在输出完成后发送）
	stream.Send(&pb.ShellData{
		IsClose:  true,
		ExitCode: int32(exitCode),
	})

	// 等待 gRPC→PTY 协程完成
	wg.Wait()

	logger.Infof("Shell 已退出: session_id=%s, exit_code=%d", req.SessionId, exitCode)
}

// sendShellError 发送错误响应（无法启动 shell 时）
func sendShellError(ctx context.Context, client pb.EndpointServiceClient, cfg *EndpointConfig, sessionID, errMsg string) {
	stream, err := client.OpenShell(ctx)
	if err != nil {
		logger.Warnf("发送 Shell 错误失败: %v", err)
		return
	}
	stream.Send(&pb.ShellData{
		IsOpen:    true,
		SessionId: sessionID,
		Token:     cfg.Agent.Token,
		IsClose:   true,
		Error:     errMsg,
	})
}

// getLoginShell 获取用户的 login shell
func getLoginShell(u *user.User) string {
	// 尝试从 /etc/passwd 读取
	data, err := os.ReadFile("/etc/passwd")
	if err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Split(line, ":")
			if len(fields) >= 7 && fields[0] == u.Username {
				shell := strings.TrimSpace(fields[6])
				if shell != "" {
					return shell
				}
			}
		}
	}
	// 默认 /bin/sh
	return "/bin/sh"
}

// buildShellEnv 构建 shell 环境变量
func buildShellEnv(u *user.User, shell string) []string {
	return []string{
		"HOME=" + u.HomeDir,
		"USER=" + u.Username,
		"LOGNAME=" + u.Username,
		"SHELL=" + shell,
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"TERM=xterm-256color",
	}
}
