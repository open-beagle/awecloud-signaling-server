package agent

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"sync"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
)

// DialSocketServer Unix Socket 代理服务器
// 供 Desktop.Pod（CloudIDE）模式下的 dial 子命令使用
// 监听 Unix Socket，接受连接后通过 tsnet 拨号到目标地址，桥接数据
type DialSocketServer struct {
	socketPath string
	tsManager  *TailscaleManager
	listener   net.Listener
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
}

// NewDialSocketServer 创建 DialSocketServer
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
		return fmt.Errorf("监听 Unix Socket 失败 (%s): %w", s.socketPath, err)
	}
	s.listener = listener

	// 设置 socket 文件权限（允许同 Pod 内其他容器访问）
	os.Chmod(s.socketPath, 0666)

	s.wg.Add(1)
	go s.acceptLoop()

	logger.Infof("DialSocket 已启动: %s", s.socketPath)
	return nil
}

// Stop 停止 DialSocketServer
func (s *DialSocketServer) Stop() {
	s.cancel()
	if s.listener != nil {
		s.listener.Close()
	}
	s.wg.Wait()
	os.Remove(s.socketPath)
	logger.Info("DialSocket 已停止")
}

// acceptLoop 接受连接循环
func (s *DialSocketServer) acceptLoop() {
	defer s.wg.Done()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.ctx.Done():
				return
			default:
				logger.Errorf("DialSocket Accept 失败: %v", err)
				continue
			}
		}
		go s.handleConn(conn)
	}
}

// handleConn 处理单个 dial 连接
// 协议：客户端发送 [2字节大端长度][host:port]，服务端回复 [1字节状态码]，然后桥接
func (s *DialSocketServer) handleConn(conn net.Conn) {
	defer conn.Close()

	// 1. 读取目标地址长度（2字节大端）
	lenBuf := make([]byte, 2)
	if _, err := io.ReadFull(conn, lenBuf); err != nil {
		logger.Debugf("DialSocket 读取地址长度失败: %v", err)
		return
	}
	addrLen := binary.BigEndian.Uint16(lenBuf)
	if addrLen == 0 || addrLen > 512 {
		conn.Write([]byte{0x01}) // 失败
		return
	}

	// 2. 读取目标地址
	addrBuf := make([]byte, addrLen)
	if _, err := io.ReadFull(conn, addrBuf); err != nil {
		logger.Debugf("DialSocket 读取地址失败: %v", err)
		conn.Write([]byte{0x01})
		return
	}
	targetAddr := string(addrBuf)

	// 3. 通过 tsnet 拨号到目标地址
	targetConn, err := s.tsManager.Dial(s.ctx, "tcp", targetAddr)
	if err != nil {
		logger.Warnf("DialSocket tsnet 拨号失败 (%s): %v", targetAddr, err)
		conn.Write([]byte{0x01}) // 失败
		return
	}
	defer targetConn.Close()

	// 4. 回复成功状态码
	if _, err := conn.Write([]byte{0x00}); err != nil {
		return
	}

	logger.Debugf("DialSocket 连接建立: %s", targetAddr)

	// 5. 双向桥接
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		io.Copy(targetConn, conn)
	}()

	go func() {
		defer wg.Done()
		io.Copy(conn, targetConn)
	}()

	wg.Wait()
}
