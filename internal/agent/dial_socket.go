// Package agent 提供 Agent 端功能
// dial_socket.go 实现 Unix Socket 代理服务
// Agent/Client 进程监听 Unix Socket，dial 子命令连接后请求 tsnet 代理转发
package agent

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
)

// DialSocketServer Unix Socket 代理服务
// 监听 Unix Socket，接受 dial 子命令的连接请求
// 协议：客户端发送 [2字节长度][目标地址]，服务端通过 tsnet Dial 连接后双向转发
type DialSocketServer struct {
	socketPath string
	tsManager  *TailscaleManager
	listener   net.Listener

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewDialSocketServer 创建 Unix Socket 代理服务
func NewDialSocketServer(socketPath string, tsManager *TailscaleManager) *DialSocketServer {
	ctx, cancel := context.WithCancel(context.Background())
	return &DialSocketServer{
		socketPath: socketPath,
		tsManager:  tsManager,
		ctx:        ctx,
		cancel:     cancel,
	}
}

// Start 启动 Unix Socket 监听
func (s *DialSocketServer) Start() error {
	// 清理旧的 socket 文件
	os.Remove(s.socketPath)

	listener, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("监听 Unix Socket 失败: %w", err)
	}

	// 设置权限（允许所有用户访问，Agent 可能以 sudo 运行）
	if err := os.Chmod(s.socketPath, 0666); err != nil {
		listener.Close()
		return fmt.Errorf("设置 Socket 权限失败: %w", err)
	}

	s.listener = listener
	logger.Infof("[DialSocket] 已启动: %s", s.socketPath)

	s.wg.Add(1)
	go s.acceptLoop()

	return nil
}

// Stop 停止 Unix Socket 服务
func (s *DialSocketServer) Stop() {
	s.cancel()
	if s.listener != nil {
		s.listener.Close()
	}
	s.wg.Wait()
	os.Remove(s.socketPath)
	logger.Infof("[DialSocket] 已停止")
}

// acceptLoop 接受连接循环
func (s *DialSocketServer) acceptLoop() {
	defer s.wg.Done()

	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.ctx.Done():
				return
			default:
				logger.Warnf("[DialSocket] Accept 失败: %v", err)
				continue
			}
		}

		go s.handleConn(conn)
	}
}

// handleConn 处理单个连接
// 协议：客户端发送 [2字节大端长度][目标地址字符串]
// 服务端通过 tsnet Dial 连接目标后，双向转发数据
func (s *DialSocketServer) handleConn(conn net.Conn) {
	defer conn.Close()

	// 读取目标地址（2字节长度 + 地址字符串）
	var addrLen uint16
	if err := binary.Read(conn, binary.BigEndian, &addrLen); err != nil {
		logger.Warnf("[DialSocket] 读取地址长度失败: %v", err)
		return
	}

	if addrLen == 0 || addrLen > 512 {
		logger.Warnf("[DialSocket] 地址长度无效: %d", addrLen)
		return
	}

	addrBuf := make([]byte, addrLen)
	if _, err := io.ReadFull(conn, addrBuf); err != nil {
		logger.Warnf("[DialSocket] 读取地址失败: %v", err)
		return
	}

	targetAddr := string(addrBuf)
	logger.Infof("[DialSocket] 代理请求: %s", targetAddr)

	// 通过 tsnet Dial 连接目标
	dialCtx, dialCancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer dialCancel()

	remoteConn, err := s.tsManager.Dial(dialCtx, "tcp", targetAddr)
	if err != nil {
		logger.Warnf("[DialSocket] 连接目标失败 (%s): %v", targetAddr, err)
		// 发送错误标记（1字节 0x01 表示失败）
		conn.Write([]byte{0x01})
		return
	}
	defer remoteConn.Close()

	// 发送成功标记（1字节 0x00 表示成功）
	if _, err := conn.Write([]byte{0x00}); err != nil {
		return
	}

	// 双向转发
	done := make(chan struct{}, 2)
	go func() {
		io.Copy(remoteConn, conn)
		done <- struct{}{}
	}()
	go func() {
		io.Copy(conn, remoteConn)
		done <- struct{}{}
	}()

	// 等待任一方向完成
	select {
	case <-done:
	case <-s.ctx.Done():
	}
}
