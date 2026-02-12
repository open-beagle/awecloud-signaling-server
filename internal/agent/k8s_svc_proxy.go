package agent

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/netip"
	"sync"

	"google.golang.org/grpc"
	grpcpeer "google.golang.org/grpc/peer"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

// K8SSVCProxy K8S Service gRPC 代理
// 使用 RegisterFallbackTCPHandler 在 tsnet 网络上接收 Desktop 的 SVCProxy 请求，
// 根据权限检查后透明转发到 K8S ClusterIP
type K8SSVCProxy struct {
	pb.UnimplementedAgentServiceServer

	config    *config.SVCSection
	tsManager *TailscaleManager
	permCache *PermissionCache
	identity  *IdentityExtractor
	informer  *K8SServiceInformer

	listener   net.Listener
	grpcServer *grpc.Server

	// fallback handler 取消注册函数
	deregisterFallback func()

	ctx    context.Context
	cancel context.CancelFunc
}

// NewK8SSVCProxy 创建 K8S Service gRPC 代理
func NewK8SSVCProxy(cfg *config.SVCSection, tsManager *TailscaleManager, permCache *PermissionCache, informer *K8SServiceInformer, parentCtx context.Context) (*K8SSVCProxy, error) {
	ctx, cancel := context.WithCancel(parentCtx)

	identity, err := NewIdentityExtractor(tsManager)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("创建身份提取器失败: %w", err)
	}

	return &K8SSVCProxy{
		config:    cfg,
		tsManager: tsManager,
		permCache: permCache,
		identity:  identity,
		informer:  informer,
		ctx:       ctx,
		cancel:    cancel,
	}, nil
}

// Start 启动 SVCProxy gRPC 服务
// 使用 RegisterFallbackTCPHandler 绕过 tsnet.Listen 的已知 bug。
func (p *K8SSVCProxy) Start() error {
	tailscaleIP := p.tsManager.GetIP()
	if tailscaleIP == "" {
		return fmt.Errorf("Tailscale IP 未就绪，无法启动 SVCProxy")
	}
	listenPort := uint16(p.config.ListenPortBase)

	// 创建 channel-based listener
	chanListener := newChannelListener()
	p.listener = chanListener

	// 注册 fallback TCP handler
	deregister := p.tsManager.RegisterFallbackTCPHandler(func(src, dst netip.AddrPort) (func(net.Conn), bool) {
		if dst.Port() == listenPort {
			return func(conn net.Conn) {
				chanListener.Enqueue(conn)
			}, true
		}
		return nil, false
	})
	p.deregisterFallback = deregister

	// 创建 gRPC Server
	p.grpcServer = grpc.NewServer()
	pb.RegisterAgentServiceServer(p.grpcServer, p)

	// 启动服务
	go func() {
		logger.Infof("K8S SVCProxy gRPC 已启动（FallbackTCPHandler）: port=%d", listenPort)
		if err := p.grpcServer.Serve(chanListener); err != nil {
			logger.Errorf("K8S SVCProxy gRPC 服务错误: %v", err)
		}
	}()

	return nil
}

// Stop 停止 SVCProxy
func (p *K8SSVCProxy) Stop() {
	p.cancel()
	// 取消注册 fallback handler
	if p.deregisterFallback != nil {
		p.deregisterFallback()
	}
	if p.grpcServer != nil {
		p.grpcServer.GracefulStop()
	}
	if p.listener != nil {
		p.listener.Close()
	}
	logger.Info("K8S SVCProxy 已停止")
}

// SVCProxy 实现 AgentService 的 SVCProxy RPC
func (p *K8SSVCProxy) SVCProxy(stream pb.AgentService_SVCProxyServer) error {
	// 1. 接收首包（连接请求）
	firstMsg, err := stream.Recv()
	if err != nil {
		return fmt.Errorf("接收首包失败: %w", err)
	}

	if !firstMsg.IsConnect {
		return fmt.Errorf("首包必须是连接请求")
	}

	namespace := firstMsg.Namespace
	serviceName := firstMsg.ServiceName
	port := firstMsg.Port

	// 2. 从 gRPC peer 提取身份
	peerIdentity, err := p.extractIdentityFromStream(stream)
	if err != nil {
		logger.Warnf("SVCProxy 身份提取失败: %v", err)
		_ = stream.Send(&pb.SVCProxyData{Error: "身份验证失败"})
		return err
	}

	// 3. 检查权限
	if !p.permCache.CheckK8SServiceAccess(peerIdentity.UserName, namespace, serviceName) {
		logger.Warnf("SVCProxy 权限拒绝: user=%s, ns=%s, svc=%s",
			peerIdentity.UserName, namespace, serviceName)
		_ = stream.Send(&pb.SVCProxyData{Error: "权限不足"})
		return fmt.Errorf("权限不足")
	}

	// 4. 查找 Service 的 ClusterIP
	svc := p.informer.FindService(namespace, serviceName)
	if svc == nil {
		logger.Warnf("SVCProxy Service 未找到: %s/%s", namespace, serviceName)
		_ = stream.Send(&pb.SVCProxyData{Error: "Service 未找到"})
		return fmt.Errorf("Service 未找到: %s/%s", namespace, serviceName)
	}

	targetAddr := fmt.Sprintf("%s:%d", svc.ClusterIP, port)

	// 5. 建立到 ClusterIP 的 TCP 连接
	targetConn, err := net.Dial("tcp", targetAddr)
	if err != nil {
		logger.Errorf("SVCProxy 连接目标失败: %s, err=%v", targetAddr, err)
		_ = stream.Send(&pb.SVCProxyData{Error: fmt.Sprintf("连接失败: %v", err)})
		return err
	}
	defer targetConn.Close()

	logger.Infof("SVCProxy 连接建立: user=%s, %s/%s:%d -> %s",
		peerIdentity.UserName, namespace, serviceName, port, targetAddr)

	// 5.5 发送连接确认（空数据包），让 Desktop 立即进入桥接阶段
	// 这是必要的：PostgreSQL 等协议需要客户端先发数据，
	// 如果 Desktop 还在等首响应就不会转发客户端数据，导致死锁
	if err := stream.Send(&pb.SVCProxyData{}); err != nil {
		logger.Errorf("SVCProxy 发送连接确认失败: %v", err)
		return err
	}

	// 6. 双向流桥接（gRPC stream ↔ TCP conn）
	var wg sync.WaitGroup
	wg.Add(2)

	// gRPC → TCP
	go func() {
		defer wg.Done()
		for {
			msg, err := stream.Recv()
			if err != nil {
				if err != io.EOF {
					logger.Debugf("SVCProxy gRPC 接收结束: %v", err)
				}
				targetConn.Close()
				return
			}
			if msg.IsClose {
				targetConn.Close()
				return
			}
			if len(msg.Data) > 0 {
				if _, err := targetConn.Write(msg.Data); err != nil {
					logger.Debugf("SVCProxy TCP 写入失败: %v", err)
					return
				}
			}
		}
	}()

	// TCP → gRPC
	go func() {
		defer wg.Done()
		buf := make([]byte, 32*1024)
		for {
			n, err := targetConn.Read(buf)
			if n > 0 {
				if sendErr := stream.Send(&pb.SVCProxyData{Data: buf[:n]}); sendErr != nil {
					logger.Debugf("SVCProxy gRPC 发送失败: %v", sendErr)
					return
				}
			}
			if err != nil {
				if err != io.EOF {
					logger.Debugf("SVCProxy TCP 读取结束: %v", err)
				}
				// 发送关闭通知
				_ = stream.Send(&pb.SVCProxyData{IsClose: true})
				return
			}
		}
	}()

	wg.Wait()
	return nil
}

// extractIdentityFromStream 从 gRPC stream 中提取对端身份
func (p *K8SSVCProxy) extractIdentityFromStream(stream pb.AgentService_SVCProxyServer) (*PeerIdentity, error) {
	// 从 gRPC peer 获取远程地址
	peer, ok := grpcpeer.FromContext(stream.Context())
	if !ok {
		return nil, fmt.Errorf("无法获取 gRPC peer 信息")
	}

	// 通过 IdentityExtractor 从 tsnet 连接提取身份
	return p.identity.ExtractFromConn(stream.Context(), peer.Addr)
}
