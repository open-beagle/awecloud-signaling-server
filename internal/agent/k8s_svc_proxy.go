package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strconv"
	"time"

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

	config         *config.SVCSection
	tsManager      *TailscaleManager
	permCache      *PermissionCache
	authorizations *SessionAuthorizationCache
	sessions       *ContainerSessionManager
	identity       *IdentityExtractor
	informer       *K8SServiceInformer
	auditCollector *AuditCollector

	// Endpoint Server（用于 Endpoint 跳跃路径）
	endpointServer *EndpointServer

	listener   net.Listener
	grpcServer *grpc.Server

	// fallback handler 取消注册函数
	deregisterFallback func()

	ctx    context.Context
	cancel context.CancelFunc
}

func (p *K8SSVCProxy) SetSessionAuthorizations(authorizations *SessionAuthorizationCache, sessions *ContainerSessionManager) {
	p.authorizations = authorizations
	p.sessions = sessions
}

// NewK8SSVCProxy 创建 K8S Service gRPC 代理
func NewK8SSVCProxy(cfg *config.SVCSection, tsManager *TailscaleManager, permCache *PermissionCache, informer *K8SServiceInformer, auditCollector *AuditCollector, parentCtx context.Context) (*K8SSVCProxy, error) {
	ctx, cancel := context.WithCancel(parentCtx)

	identity, err := NewIdentityExtractor(tsManager)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("创建身份提取器失败: %w", err)
	}

	return &K8SSVCProxy{
		config:         cfg,
		tsManager:      tsManager,
		permCache:      permCache,
		identity:       identity,
		informer:       informer,
		auditCollector: auditCollector,
		ctx:            ctx,
		cancel:         cancel,
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
// P10 重构：Agent 自动选择实现路径（直连或 Endpoint 跳跃）
func (p *K8SSVCProxy) SVCProxy(stream pb.AgentService_SVCProxyServer) error {
	// 1. 接收首包（连接请求）
	firstMsg, err := stream.Recv()
	if err != nil {
		return fmt.Errorf("接收首包失败: %w", err)
	}

	if !firstMsg.IsConnect {
		return fmt.Errorf("首包必须是连接请求")
	}

	// 2. 从 gRPC peer 提取身份
	peerIdentity, err := p.extractIdentityFromStream(stream)
	if err != nil {
		logger.Warnf("SVCProxy 身份提取失败: %v", err)
		_ = stream.Send(&pb.SVCProxyData{Error: "身份验证失败"})
		return err
	}
	now := time.Now().UTC()
	if p.endpointServer != nil {
		if endpointName, endpointPermission, ok := p.endpointServer.ResolveSessionAuthorization(firstMsg.SessionId, now); ok {
			if !v2SVCRequestMatchesPermission(firstMsg, peerIdentity, endpointPermission) {
				_ = stream.Send(&pb.SVCProxyData{Error: "权限不足"})
				return fmt.Errorf("resource_session_v2 Endpoint SVCProxy request does not match the trusted snapshot")
			}
			return p.handleEndpointProxyV2(stream, peerIdentity, endpointName, endpointPermission)
		}
	}
	permission, authorizeErr := p.authorizeV2Request(firstMsg, peerIdentity, now)
	if authorizeErr != nil {
		logger.Warnf("SVCProxy v2 权限拒绝: user=%s node_id=%d err=%v", peerIdentity.UserName, peerIdentity.NodeID, authorizeErr)
		_ = stream.Send(&pb.SVCProxyData{Error: "权限不足"})
		return authorizeErr
	}
	if p.informer == nil {
		_ = stream.Send(&pb.SVCProxyData{Error: "无可用的 v2 K8S 访问路径"})
		return fmt.Errorf("无可用的 v2 K8S 访问路径")
	}
	return p.handleDirectConnectionV2(stream, peerIdentity, permission)
}

func (p *K8SSVCProxy) authorizeV2Request(firstMsg *pb.SVCProxyData, identity *PeerIdentity, now time.Time) (*pb.ResourceSessionPermissionV2, error) {
	if p == nil || p.authorizations == nil || !p.authorizations.Enabled(now) || firstMsg == nil || identity == nil ||
		identity.Role != "client" || identity.UserName == "" || identity.NodeID == 0 || firstMsg.SessionId == "" ||
		firstMsg.ResourceId == "" || firstMsg.SourceId == "" || firstMsg.TargetRevisionId == "" || firstMsg.ServiceUid == "" ||
		firstMsg.Protocol != "TCP" || firstMsg.Port <= 0 || firstMsg.Port > 65535 || firstMsg.AuthorizationRevision <= 0 {
		return nil, fmt.Errorf("invalid resource_session_v2 SVCProxy request")
	}
	permission, allowed := p.authorizations.Permission(firstMsg.SessionId, now)
	if !allowed || !v2SVCRequestMatchesPermission(firstMsg, identity, permission) {
		return nil, fmt.Errorf("resource_session_v2 SVCProxy request does not match the trusted snapshot")
	}
	return permission, nil
}

func v2SVCRequestMatchesPermission(firstMsg *pb.SVCProxyData, identity *PeerIdentity, permission *pb.ResourceSessionPermissionV2) bool {
	return firstMsg != nil && identity != nil && permission != nil && permission.ResourceType == "container_service" &&
		permission.UserName == identity.UserName && permission.DeviceHeadscaleNodeId == identity.NodeID && permission.SessionId == firstMsg.SessionId &&
		permission.ResourceId == firstMsg.ResourceId && permission.SourceId == firstMsg.SourceId &&
		permission.TargetRevisionId == firstMsg.TargetRevisionId && permission.AuthorizationRevision == firstMsg.AuthorizationRevision &&
		permission.Target != nil && permission.Target.NamespaceName == firstMsg.Namespace &&
		permission.Target.ServiceName == firstMsg.ServiceName && permission.Target.ServiceUid == firstMsg.ServiceUid &&
		permission.Target.PortName == firstMsg.PortName && permission.Target.PortNumber == firstMsg.Port && permission.Target.Protocol == firstMsg.Protocol
}

func (p *K8SSVCProxy) handleEndpointProxyV2(stream pb.AgentService_SVCProxyServer, peerIdentity *PeerIdentity, endpointName string, permission *pb.ResourceSessionPermissionV2) error {
	epStream, err := p.endpointServer.RequestSVCProxyV2(stream.Context(), endpointName, permission)
	if err != nil {
		_ = stream.Send(&pb.SVCProxyData{Error: fmt.Sprintf("Endpoint 连接失败: %v", err)})
		return err
	}
	if err := stream.Send(&pb.SVCProxyData{}); err != nil {
		return err
	}
	done := make(chan error, 2)
	go func() {
		for {
			msg, recvErr := stream.Recv()
			if recvErr != nil {
				_ = epStream.Send(&pb.EndpointSVCProxyData{IsClose: true})
				done <- recvErr
				return
			}
			if msg.IsClose {
				_ = epStream.Send(&pb.EndpointSVCProxyData{IsClose: true})
				done <- nil
				return
			}
			if len(msg.Data) > 0 {
				if sendErr := epStream.Send(&pb.EndpointSVCProxyData{Data: msg.Data}); sendErr != nil {
					done <- sendErr
					return
				}
			}
		}
	}()
	go func() {
		for {
			msg, recvErr := epStream.Recv()
			if recvErr != nil {
				done <- recvErr
				return
			}
			if msg.IsClose {
				done <- nil
				return
			}
			if msg.Error != "" {
				_ = stream.Send(&pb.SVCProxyData{Error: msg.Error})
				done <- fmt.Errorf("Endpoint SVCProxy: %s", msg.Error)
				return
			}
			if len(msg.Data) > 0 {
				if sendErr := stream.Send(&pb.SVCProxyData{Data: msg.Data}); sendErr != nil {
					done <- sendErr
					return
				}
			}
		}
	}()
	select {
	case err = <-done:
	case <-stream.Context().Done():
		err = stream.Context().Err()
		_ = epStream.Send(&pb.EndpointSVCProxyData{IsClose: true})
	}
	if p.auditCollector != nil {
		target := permission.Target
		auditTarget := fmt.Sprintf("%s.%s:%d@%s", target.ServiceName, target.NamespaceName, target.PortNumber, endpointName)
		p.auditCollector.Record(peerIdentity.UserName, endpointName, "k8s_service_connect", auditTarget, "", time.Now(), time.Now())
	}
	if err == io.EOF {
		return nil
	}
	return err
}

func (p *K8SSVCProxy) handleDirectConnectionV2(stream pb.AgentService_SVCProxyServer, peerIdentity *PeerIdentity, permission *pb.ResourceSessionPermissionV2) error {
	if permission == nil || permission.Target == nil || p.informer == nil || p.sessions == nil {
		return fmt.Errorf("SVCProxy v2 is not configured")
	}
	target := permission.Target
	svc := p.informer.FindService(target.NamespaceName, target.ServiceName)
	if !serviceMatchesV2Permission(svc, target) {
		_ = p.sessions.FailV2(permission.SessionId, "SERVICE_TARGET_CHANGED")
		_ = stream.Send(&pb.SVCProxyData{Error: "Service 目标已变更"})
		return fmt.Errorf("SVCProxy Service UID/Port/Protocol 不匹配")
	}
	targetAddr := net.JoinHostPort(svc.ClusterIP, strconv.Itoa(int(target.PortNumber)))
	targetConn, err := net.Dial("tcp", targetAddr)
	if err != nil {
		_ = p.sessions.FailV2(permission.SessionId, "SERVICE_CONNECT_FAILED")
		_ = stream.Send(&pb.SVCProxyData{Error: fmt.Sprintf("连接失败: %v", err)})
		return err
	}
	defer targetConn.Close()
	sessionCtx, err := p.sessions.BeginV2(stream.Context(), permission)
	if err != nil {
		_ = stream.Send(&pb.SVCProxyData{Error: "会话注册失败"})
		return err
	}
	startedAt := time.Now()
	if err := stream.Send(&pb.SVCProxyData{}); err != nil {
		_ = p.sessions.EndV2(permission.SessionId, "failed", "CLIENT_STREAM_FAILED")
		return err
	}

	done := make(chan error, 2)
	go func() {
		for {
			msg, recvErr := stream.Recv()
			if recvErr != nil {
				done <- recvErr
				return
			}
			if msg.IsClose {
				done <- nil
				return
			}
			if len(msg.Data) > 0 {
				if _, writeErr := targetConn.Write(msg.Data); writeErr != nil {
					done <- writeErr
					return
				}
			}
		}
	}()
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, readErr := targetConn.Read(buf)
			if n > 0 {
				if sendErr := stream.Send(&pb.SVCProxyData{Data: buf[:n]}); sendErr != nil {
					done <- sendErr
					return
				}
			}
			if readErr != nil {
				done <- readErr
				return
			}
		}
	}()

	result, reason := "success", "stream_closed"
	select {
	case bridgeErr := <-done:
		if bridgeErr != nil && !errors.Is(bridgeErr, io.EOF) {
			result, reason = "failed", "SERVICE_STREAM_FAILED"
		}
	case <-sessionCtx.Done():
		result, reason = "ended", "context_canceled"
		_ = stream.Send(&pb.SVCProxyData{IsClose: true})
	}
	_ = targetConn.Close()
	if endErr := p.sessions.EndV2(permission.SessionId, result, reason); endErr != nil {
		logger.Errorf("SVCProxy v2 会话事件持久化失败: session_id=%s err=%v", permission.SessionId, endErr)
	}
	if p.auditCollector != nil {
		auditTarget := fmt.Sprintf("%s.%s:%d", target.ServiceName, target.NamespaceName, target.PortNumber)
		p.auditCollector.Record(peerIdentity.UserName, "", "k8s_service_connect", auditTarget, "", startedAt, time.Now())
	}
	return nil
}

func serviceMatchesV2Permission(svc *DiscoveredService, target *pb.ResourceSessionTargetV2) bool {
	if svc == nil || target == nil || svc.UID == "" || svc.UID != target.ServiceUid || svc.Namespace != target.NamespaceName ||
		svc.Name != target.ServiceName || svc.ClusterIP == "" || target.Protocol != "TCP" {
		return false
	}
	for _, port := range svc.Ports {
		if port.Name == target.PortName && port.Port == target.PortNumber && port.Protocol == target.Protocol {
			return true
		}
	}
	return false
}

func (p *K8SSVCProxy) SetEndpointServer(es *EndpointServer) {
	p.endpointServer = es
}

func (p *K8SSVCProxy) extractIdentityFromStream(stream pb.AgentService_SVCProxyServer) (*PeerIdentity, error) {
	peer, ok := grpcpeer.FromContext(stream.Context())
	if !ok {
		return nil, fmt.Errorf("无法获取 gRPC peer 信息")
	}
	return p.identity.ExtractFromConn(stream.Context(), peer.Addr)
}
