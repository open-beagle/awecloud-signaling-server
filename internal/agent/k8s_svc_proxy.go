package agent

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strconv"
	"sync"
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
	requireV2 := hasV2SVCProxyFields(firstMsg) || (p.authorizations != nil && p.authorizations.EnforceV2()) ||
		(p.endpointServer != nil && p.endpointServer.SessionAuthorizationEnforced())
	if requireV2 {
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

	namespace := firstMsg.Namespace
	serviceName := firstMsg.ServiceName
	port := firstMsg.Port

	// 3. 统一权限检查（Agent 级别）
	if !p.permCache.CheckK8SServiceAccess(peerIdentity.UserName, namespace, serviceName) {
		logger.Warnf("SVCProxy 权限拒绝: user=%s, ns=%s, svc=%s",
			peerIdentity.UserName, namespace, serviceName)
		_ = stream.Send(&pb.SVCProxyData{Error: "权限不足"})
		return fmt.Errorf("权限不足")
	}

	// 4. 判断实现方式
	if p.informer != nil {
		// 路径 A：Agent 有 K8S 访问能力，直接连接
		return p.handleDirectConnection(stream, peerIdentity, namespace, serviceName, port)
	} else if p.endpointServer != nil {
		// 路径 B：Agent 无 K8S 访问能力，自动选择 Endpoint
		return p.handleEndpointProxyAuto(stream, peerIdentity, namespace, serviceName, port)
	} else {
		logger.Warnf("SVCProxy 无可用的 K8S 访问路径")
		_ = stream.Send(&pb.SVCProxyData{Error: "无可用的 K8S 访问路径"})
		return fmt.Errorf("无可用的 K8S 访问路径")
	}
}

func hasV2SVCProxyFields(message *pb.SVCProxyData) bool {
	return message != nil && (message.SessionId != "" || message.ResourceId != "" || message.SourceId != "" ||
		message.TargetRevisionId != "" || message.ServiceUid != "" || message.PortName != "" ||
		message.Protocol != "" || message.AuthorizationRevision != 0)
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

	var bridgeErr error
	select {
	case bridgeErr = <-done:
	case <-sessionCtx.Done():
		bridgeErr = sessionCtx.Err()
		_ = stream.Send(&pb.SVCProxyData{IsClose: true})
	}
	_ = targetConn.Close()
	result, reason := containerSessionOutcome(bridgeErr)
	if endErr := p.sessions.EndV2(permission.SessionId, result, reason); endErr != nil {
		logger.Errorf("SVCProxy v2 会话事件持久化失败: session_id=%s err=%v", permission.SessionId, endErr)
	}
	if p.auditCollector != nil {
		auditTarget := fmt.Sprintf("%s.%s:%d", target.ServiceName, target.NamespaceName, target.PortNumber)
		p.auditCollector.Record(peerIdentity.UserName, "", "k8s_service_connect", auditTarget, "", startedAt, time.Now())
	}
	if bridgeErr == io.EOF {
		return nil
	}
	return bridgeErr
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

// handleEndpointSVCProxy 处理 Endpoint 跳跃路径的 SVCProxy
// Desktop → Agent SVCProxy(endpoint_name=xxx) → Agent RequestSVCProxy → Endpoint OpenSVCProxy → K8S ClusterIP
func (p *K8SSVCProxy) handleEndpointSVCProxy(stream pb.AgentService_SVCProxyServer, peerIdentity *PeerIdentity, endpointName, namespace, serviceName, clusterIP string, port int32) error {
	// P10 重构：直接使用 Agent 级别权限检查（不再区分 Endpoint）
	if !p.permCache.CheckK8SServiceAccess(peerIdentity.UserName, namespace, serviceName) {
		logger.Warnf("SVCProxy Endpoint 权限拒绝: user=%s, endpoint=%s, ns=%s, svc=%s",
			peerIdentity.UserName, endpointName, namespace, serviceName)
		_ = stream.Send(&pb.SVCProxyData{Error: "权限不足"})
		return fmt.Errorf("权限不足")
	}

	// 检查 EndpointServer 是否可用
	if p.endpointServer == nil {
		_ = stream.Send(&pb.SVCProxyData{Error: "Endpoint 功能未启用"})
		return fmt.Errorf("Endpoint 功能未启用")
	}

	// 请求 Endpoint 开启 SVC 代理
	epStream, err := p.endpointServer.RequestSVCProxy(stream.Context(), endpointName, namespace, serviceName, clusterIP, port)
	if err != nil {
		logger.Warnf("SVCProxy Endpoint 请求失败: %v", err)
		_ = stream.Send(&pb.SVCProxyData{Error: fmt.Sprintf("Endpoint 连接失败: %v", err)})
		return err
	}

	logger.Infof("SVCProxy Endpoint 跳跃: user=%s, endpoint=%s, %s/%s:%d",
		peerIdentity.UserName, endpointName, namespace, serviceName, port)

	startedAt := time.Now()

	// 发送连接确认
	if err := stream.Send(&pb.SVCProxyData{}); err != nil {
		return err
	}

	// 双向桥接：Desktop gRPC stream ↔ Endpoint gRPC stream
	var wg sync.WaitGroup
	wg.Add(2)

	// Desktop → Endpoint
	go func() {
		defer wg.Done()
		for {
			msg, err := stream.Recv()
			if err != nil {
				_ = epStream.Send(&pb.EndpointSVCProxyData{IsClose: true})
				return
			}
			if msg.IsClose {
				_ = epStream.Send(&pb.EndpointSVCProxyData{IsClose: true})
				return
			}
			if len(msg.Data) > 0 {
				if sendErr := epStream.Send(&pb.EndpointSVCProxyData{Data: msg.Data}); sendErr != nil {
					return
				}
			}
		}
	}()

	// Endpoint → Desktop
	go func() {
		defer wg.Done()
		for {
			msg, err := epStream.Recv()
			if err != nil {
				_ = stream.Send(&pb.SVCProxyData{IsClose: true})
				return
			}
			if msg.IsClose {
				_ = stream.Send(&pb.SVCProxyData{IsClose: true})
				return
			}
			if msg.Error != "" {
				_ = stream.Send(&pb.SVCProxyData{Error: msg.Error})
				return
			}
			if len(msg.Data) > 0 {
				if sendErr := stream.Send(&pb.SVCProxyData{Data: msg.Data}); sendErr != nil {
					return
				}
			}
		}
	}()

	wg.Wait()

	// 记录审计
	if p.auditCollector != nil {
		target := fmt.Sprintf("%s.%s:%d@%s", serviceName, namespace, port, endpointName)
		p.auditCollector.Record(peerIdentity.UserName, endpointName, "k8s_service_connect", target, "", startedAt, time.Now())
	}

	return nil
}

// SetEndpointServer 设置 EndpointServer 引用（用于 Endpoint 跳跃路径）
func (p *K8SSVCProxy) SetEndpointServer(es *EndpointServer) {
	p.endpointServer = es
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

// handleDirectConnection 处理 Agent 直连路径
// Agent 有 K8S 访问能力，直接连接 ClusterIP
func (p *K8SSVCProxy) handleDirectConnection(stream pb.AgentService_SVCProxyServer, peerIdentity *PeerIdentity, namespace, serviceName string, port int32) error {
	// 查找 Service 的 ClusterIP
	svc := p.informer.FindService(namespace, serviceName)
	if svc == nil {
		logger.Warnf("SVCProxy Service 未找到: %s/%s", namespace, serviceName)
		_ = stream.Send(&pb.SVCProxyData{Error: "Service 未找到"})
		return fmt.Errorf("Service 未找到: %s/%s", namespace, serviceName)
	}

	targetAddr := net.JoinHostPort(svc.ClusterIP, strconv.Itoa(int(port)))

	// 建立到 ClusterIP 的 TCP 连接
	targetConn, err := net.Dial("tcp", targetAddr)
	if err != nil {
		logger.Errorf("SVCProxy 连接目标失败: %s, err=%v", targetAddr, err)
		_ = stream.Send(&pb.SVCProxyData{Error: fmt.Sprintf("连接失败: %v", err)})
		return err
	}
	defer targetConn.Close()

	logger.Infof("SVCProxy 直连: user=%s, %s/%s:%d -> %s",
		peerIdentity.UserName, namespace, serviceName, port, targetAddr)

	startedAt := time.Now()

	// 发送连接确认
	if err := stream.Send(&pb.SVCProxyData{}); err != nil {
		logger.Errorf("SVCProxy 发送连接确认失败: %v", err)
		return err
	}

	// 双向流桥接
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
				_ = stream.Send(&pb.SVCProxyData{IsClose: true})
				return
			}
		}
	}()

	wg.Wait()

	// 记录审计
	if p.auditCollector != nil {
		target := fmt.Sprintf("%s.%s:%d", serviceName, namespace, port)
		p.auditCollector.Record(peerIdentity.UserName, "", "k8s_service_connect", target, "", startedAt, time.Now())
	}

	return nil
}

// handleEndpointProxyAuto 处理 Endpoint 自动选择路径
// Agent 无 K8S 访问能力，自动选择可用的 Endpoint 代理
func (p *K8SSVCProxy) handleEndpointProxyAuto(stream pb.AgentService_SVCProxyServer, peerIdentity *PeerIdentity, namespace, serviceName string, port int32) error {
	// 自动选择可用的 Endpoint
	endpointName, clusterIP := p.selectAvailableEndpoint(namespace, serviceName)
	if endpointName == "" {
		logger.Warnf("SVCProxy 无可用的 Endpoint: %s/%s", namespace, serviceName)
		_ = stream.Send(&pb.SVCProxyData{Error: "无可用的 Endpoint"})
		return fmt.Errorf("无可用的 Endpoint")
	}

	logger.Infof("SVCProxy 自动选择 Endpoint: %s/%s -> endpoint=%s", namespace, serviceName, endpointName)

	// 请求 Endpoint 开启 SVC 代理
	epStream, err := p.endpointServer.RequestSVCProxy(stream.Context(), endpointName, namespace, serviceName, clusterIP, port)
	if err != nil {
		logger.Warnf("SVCProxy Endpoint 请求失败: %v", err)
		_ = stream.Send(&pb.SVCProxyData{Error: fmt.Sprintf("Endpoint 连接失败: %v", err)})
		return err
	}

	logger.Infof("SVCProxy Endpoint 跳跃: user=%s, endpoint=%s, %s/%s:%d",
		peerIdentity.UserName, endpointName, namespace, serviceName, port)

	startedAt := time.Now()

	// 发送连接确认
	if err := stream.Send(&pb.SVCProxyData{}); err != nil {
		return err
	}

	// 双向桥接：Desktop gRPC stream ↔ Endpoint gRPC stream
	var wg sync.WaitGroup
	wg.Add(2)

	// Desktop → Endpoint
	go func() {
		defer wg.Done()
		for {
			msg, err := stream.Recv()
			if err != nil {
				_ = epStream.Send(&pb.EndpointSVCProxyData{IsClose: true})
				return
			}
			if msg.IsClose {
				_ = epStream.Send(&pb.EndpointSVCProxyData{IsClose: true})
				return
			}
			if len(msg.Data) > 0 {
				if sendErr := epStream.Send(&pb.EndpointSVCProxyData{Data: msg.Data}); sendErr != nil {
					return
				}
			}
		}
	}()

	// Endpoint → Desktop
	go func() {
		defer wg.Done()
		for {
			msg, err := epStream.Recv()
			if err != nil {
				_ = stream.Send(&pb.SVCProxyData{IsClose: true})
				return
			}
			if msg.IsClose {
				_ = stream.Send(&pb.SVCProxyData{IsClose: true})
				return
			}
			if msg.Error != "" {
				_ = stream.Send(&pb.SVCProxyData{Error: msg.Error})
				return
			}
			if len(msg.Data) > 0 {
				if sendErr := stream.Send(&pb.SVCProxyData{Data: msg.Data}); sendErr != nil {
					return
				}
			}
		}
	}()

	wg.Wait()

	// 记录审计
	if p.auditCollector != nil {
		target := fmt.Sprintf("%s.%s:%d@%s", serviceName, namespace, port, endpointName)
		p.auditCollector.Record(peerIdentity.UserName, endpointName, "k8s_service_connect", target, "", startedAt, time.Now())
	}

	return nil
}

// selectAvailableEndpoint 自动选择可用的 Endpoint
// 返回: endpointName, clusterIP
func (p *K8SSVCProxy) selectAvailableEndpoint(namespace, serviceName string) (string, string) {
	if p.endpointServer == nil {
		return "", ""
	}

	// 从已连接的 Endpoint 中选择
	// 优先选择：
	// 1. 有该 Service 发现数据的 Endpoint
	// 2. K8SService 能力已启用的 Endpoint
	// 3. 状态为 online 的 Endpoint

	endpoints := p.endpointServer.GetConnectedEndpointDetails()
	for _, ep := range endpoints {
		// 检查是否有 K8SService 能力
		hasK8SSvcCapability := false
		for _, cap := range ep.Capabilities {
			if cap.Type == "k8sservice" {
				hasK8SSvcCapability = true
				break
			}
		}
		if !hasK8SSvcCapability {
			continue
		}

		// 检查是否有该 Service 的发现数据
		clusterIP := p.endpointServer.FindEndpointServiceClusterIP(ep.Name, namespace, serviceName)
		if clusterIP != "" {
			logger.Debugf("选择 Endpoint: %s (有 Service 发现数据)", ep.Name)
			return ep.Name, clusterIP
		}
	}

	// 如果没有找到有发现数据的 Endpoint，选择第一个有 K8SService 能力的
	for _, ep := range endpoints {
		for _, cap := range ep.Capabilities {
			if cap.Type == "k8sservice" {
				logger.Debugf("选择 Endpoint: %s (无 Service 发现数据，但有 K8SService 能力)", ep.Name)
				return ep.Name, ""
			}
		}
	}

	return "", ""
}
