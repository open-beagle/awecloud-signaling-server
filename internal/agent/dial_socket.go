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
	socketPath  string
	tsManager   *TailscaleManager
	domainCache *DomainCache // 域名缓存，用于将魔法 DNS 域名解析为 Tailscale IP
	listener    net.Listener
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
}

// NewDialSocketServer 创建 DialSocketServer
func NewDialSocketServer(socketPath string, tsManager *TailscaleManager, domainCache *DomainCache) *DialSocketServer {
	ctx, cancel := context.WithCancel(context.Background())
	return &DialSocketServer{
		socketPath:  socketPath,
		tsManager:   tsManager,
		domainCache: domainCache,
		ctx:         ctx,
		cancel:      cancel,
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

	// 3. 解析域名为 Tailscale IP
	// tsnet.Dial 的 DNS 解析会走系统 DNS（被我们劫持到 127.0.0.1:53），
	// 返回 VIP（127.1.0.x），导致连接到本机而不是远端 Tailscale 节点。
	// 因此需要从 domainCache 查找域名对应的 target_ip（Tailscale IP），
	// 用 Tailscale IP 替换域名后再通过 tsnet.Dial 连接。
	dialAddr := s.resolveDialAddr(targetAddr)

	// 4. 通过 tsnet 拨号到目标地址
	targetConn, err := s.tsManager.Dial(s.ctx, "tcp", dialAddr)
	if err != nil {
		logger.Warnf("DialSocket tsnet 拨号失败 (%s -> %s): %v", targetAddr, dialAddr, err)
		conn.Write([]byte{0x01}) // 失败
		return
	}
	defer targetConn.Close()

	// 5. 回复成功状态码
	if _, err := conn.Write([]byte{0x00}); err != nil {
		return
	}

	logger.Infof("DialSocket 连接建立: %s -> %s", targetAddr, dialAddr)

	// 6. 双向桥接
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

// resolveDialAddr 将 dial 目标地址中的魔法 DNS 域名解析为 Tailscale IP
// 输入: "beagle-242.beijing.beagle:22"
// 输出: "100.64.0.19:22"（从 domainCache 查找 target_ip）
// 如果域名不在缓存中，返回原始地址（兜底，让 tsnet 自行解析）
func (s *DialSocketServer) resolveDialAddr(targetAddr string) string {
	if s.domainCache == nil {
		return targetAddr
	}

	// 分离 host 和 port
	host, port, err := net.SplitHostPort(targetAddr)
	if err != nil {
		return targetAddr
	}

	// 从域名缓存查找
	domainInfo, ok := s.domainCache.Get(host)
	if !ok || domainInfo.TargetIp == "" {
		logger.Debugf("DialSocket 域名未在缓存中: %s，使用原始地址", host)
		return targetAddr
	}

	// 使用 domainInfo 中的 target_ip 和 target_port
	// 对于 SSH 类型，保持用户请求的端口（22），因为 Tailscale SSH 监听在 22 端口
	// 对于其他类型，使用 target_port（如 K8SAPI 的 50050、K8SSVC 的 50051）
	resolvedPort := port
	if domainInfo.Type != "ssh" && domainInfo.TargetPort > 0 {
		resolvedPort = fmt.Sprintf("%d", domainInfo.TargetPort)
	}

	resolved := net.JoinHostPort(domainInfo.TargetIp, resolvedPort)
	logger.Infof("DialSocket 域名解析: %s -> %s (type=%s)", targetAddr, resolved, domainInfo.Type)
	return resolved
}
