package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"sync"
	"time"

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
	if req.Subsystem != "" {
		if req.Subsystem != "sftp" {
			stream.Send(&pb.ShellData{IsClose: true, Error: fmt.Sprintf("不支持的 SSH subsystem: %s", req.Subsystem)})
			return
		}
		handleSFTPProcess(stream, u, shell, req)
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
	if err := configureShellProcess(cmd, u); err != nil {
		logger.Warnf("配置 Shell 进程失败: %v", err)
		stream.Send(&pb.ShellData{IsClose: true, Error: fmt.Sprintf("配置 shell 进程失败: %v", err)})
		return
	}
	if !shouldAllocateShellPTY(req) {
		handleNonPTYShellProcess(cmd, stream, req)
		return
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
	processDone := make(chan struct{})

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
				terminateShellProcess(cmd, processDone)
				return
			}

			if msg.IsClose {
				terminateShellProcess(cmd, processDone)
				return
			}

			if msg.IsResize {
				pty.Setsize(ptmx, &pty.Winsize{
					Rows: uint16(msg.Rows),
					Cols: uint16(msg.Cols),
				})
				continue
			}
			if msg.Signal != "" {
				_ = signalShellProcessByName(cmd, msg.Signal)
				continue
			}

			if len(msg.Data) > 0 {
				ptmx.Write(msg.Data)
			}
		}
	}()

	// 等待 shell 退出
	exitCode, exitSignal := shellProcessResult(cmd.Wait())
	close(processDone)

	// shell 已退出，关闭 PTY 以解除 PTY→gRPC 协程的 Read 阻塞
	ptmx.Close()

	// 等待 PTY→gRPC 输出完成（确保所有输出都已发送）
	<-outputDone

	// 发送退出码（在输出完成后发送）
	stream.Send(&pb.ShellData{
		IsClose:  true,
		ExitCode: int32(exitCode),
		Signal:   exitSignal,
	})

	// 等待 gRPC→PTY 协程完成
	wg.Wait()

	logger.Infof("Shell 已退出: session_id=%s, exit_code=%d", req.SessionId, exitCode)
}

// shouldAllocateShellPTY keeps interactive shells and explicitly allocated exec
// sessions on a terminal. A non-interactive exec is represented by zero terminal
// dimensions by the Agent.
func shouldAllocateShellPTY(req *pb.ShellRequest) bool {
	switch req.Mode {
	case pb.ShellMode_SHELL_MODE_PTY:
		return true
	case pb.ShellMode_SHELL_MODE_PIPES:
		return false
	}
	return req.Command == "" || req.Rows != 0 || req.Cols != 0
}

type shellDataStream interface {
	Send(*pb.ShellData) error
	Recv() (*pb.ShellData, error)
}

// handleNonPTYShellProcess bridges an exec command through ordinary pipes so
// stdout and stderr remain independent byte streams.
func handleNonPTYShellProcess(cmd *exec.Cmd, stream shellDataStream, req *pb.ShellRequest) {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		sendShellProcessError(stream, fmt.Sprintf("创建 shell stdin 失败: %v", err))
		return
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		sendShellProcessError(stream, fmt.Sprintf("创建 shell stdout 失败: %v", err))
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		sendShellProcessError(stream, fmt.Sprintf("创建 shell stderr 失败: %v", err))
		return
	}
	if err := cmd.Start(); err != nil {
		sendShellProcessError(stream, fmt.Sprintf("启动 shell 失败: %v", err))
		return
	}

	logger.Infof("Shell 无 TTY exec 已启动: session_id=%s, login=%s, command=%s, pid=%d",
		req.SessionId, req.Login, req.Command, cmd.Process.Pid)
	processDone := make(chan struct{})

	var sendMutex sync.Mutex
	send := func(message *pb.ShellData) error {
		sendMutex.Lock()
		defer sendMutex.Unlock()
		return stream.Send(message)
	}
	copyOutput := func(reader io.Reader, isStderr bool, done chan<- struct{}) {
		defer func() { done <- struct{}{} }()
		buf := make([]byte, 32*1024)
		for {
			n, readErr := reader.Read(buf)
			if n > 0 {
				data := append([]byte(nil), buf[:n]...)
				if sendErr := send(&pb.ShellData{Data: data, IsStderr: isStderr}); sendErr != nil {
					return
				}
			}
			if readErr != nil {
				return
			}
		}
	}

	outputDone := make(chan struct{}, 2)
	go copyOutput(stdout, false, outputDone)
	go copyOutput(stderr, true, outputDone)
	go func() {
		defer stdin.Close()
		for {
			message, recvErr := stream.Recv()
			if recvErr != nil {
				terminateShellProcess(cmd, processDone)
				return
			}
			if message.IsClose {
				return
			}
			if message.Signal != "" {
				_ = signalShellProcessByName(cmd, message.Signal)
				continue
			}
			if len(message.Data) > 0 {
				if _, writeErr := stdin.Write(message.Data); writeErr != nil {
					return
				}
			}
		}
	}()

	// StdoutPipe/StderrPipe must be drained before Wait closes their descriptors.
	<-outputDone
	<-outputDone

	exitCode, exitSignal := shellProcessResult(cmd.Wait())
	close(processDone)
	_ = send(&pb.ShellData{IsClose: true, ExitCode: int32(exitCode), Signal: exitSignal})
	logger.Infof("Shell 无 TTY exec 已退出: session_id=%s, exit_code=%d", req.SessionId, exitCode)
}

func sendShellProcessError(stream shellDataStream, message string) {
	_ = stream.Send(&pb.ShellData{IsClose: true, ExitCode: 1, Error: message})
}

func handleSFTPProcess(stream pb.EndpointService_OpenShellClient, u *user.User, shell string, req *pb.ShellRequest) {
	executable, err := os.Executable()
	if err != nil {
		logger.Warnf("SFTP 请求失败: session_id=%s err=%v", req.SessionId, err)
		stream.Send(&pb.ShellData{IsClose: true, Error: fmt.Sprintf("定位 Endpoint SFTP 子进程失败: %v", err)})
		return
	}

	cmd := exec.Command(executable, "be-child", "sftp")
	cmd.Dir = u.HomeDir
	cmd.Env = buildShellEnv(u, shell)
	if err := configureShellProcess(cmd, u); err != nil {
		stream.Send(&pb.ShellData{IsClose: true, Error: fmt.Sprintf("配置 SFTP 进程失败: %v", err)})
		return
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		stream.Send(&pb.ShellData{IsClose: true, Error: fmt.Sprintf("创建 SFTP stdin 失败: %v", err)})
		return
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stream.Send(&pb.ShellData{IsClose: true, Error: fmt.Sprintf("创建 SFTP stdout 失败: %v", err)})
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		stream.Send(&pb.ShellData{IsClose: true, Error: fmt.Sprintf("创建 SFTP stderr 失败: %v", err)})
		return
	}
	if err := cmd.Start(); err != nil {
		stream.Send(&pb.ShellData{IsClose: true, Error: fmt.Sprintf("启动 SFTP 服务失败: %v", err)})
		return
	}
	logger.Infof("SFTP 已启动: session_id=%s login=%s pid=%d", req.SessionId, req.Login, cmd.Process.Pid)
	processDone := make(chan struct{})

	var sendMutex sync.Mutex
	send := func(message *pb.ShellData) error {
		sendMutex.Lock()
		defer sendMutex.Unlock()
		return stream.Send(message)
	}
	copyOutput := func(reader io.Reader, isStderr bool, done chan<- struct{}) {
		defer func() { done <- struct{}{} }()
		buf := make([]byte, 32*1024)
		for {
			n, readErr := reader.Read(buf)
			if n > 0 {
				data := append([]byte(nil), buf[:n]...)
				if sendErr := send(&pb.ShellData{Data: data, IsStderr: isStderr}); sendErr != nil {
					return
				}
			}
			if readErr != nil {
				return
			}
		}
	}
	outputDone := make(chan struct{}, 2)
	go copyOutput(stdout, false, outputDone)
	go copyOutput(stderr, true, outputDone)

	go func() {
		defer stdin.Close()
		for {
			message, recvErr := stream.Recv()
			if recvErr != nil {
				terminateShellProcess(cmd, processDone)
				return
			}
			if message.IsClose {
				return
			}
			if len(message.Data) > 0 {
				if _, writeErr := stdin.Write(message.Data); writeErr != nil {
					return
				}
			}
		}
	}()

	exitCode, exitSignal := shellProcessResult(cmd.Wait())
	close(processDone)
	<-outputDone
	<-outputDone
	_ = send(&pb.ShellData{IsClose: true, ExitCode: int32(exitCode), Signal: exitSignal})
	logger.Infof("SFTP 已退出: session_id=%s exit_code=%d", req.SessionId, exitCode)
}

func terminateShellProcess(cmd *exec.Cmd, processDone <-chan struct{}) {
	_ = signalShellProcess(cmd)
	go func() {
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		select {
		case <-processDone:
		case <-timer.C:
			_ = killShellProcess(cmd)
		}
	}()
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
