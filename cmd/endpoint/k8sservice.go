package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

// handleSVCProxyRequest 处理来自 Agent 的 K8S Service 代理请求
// 通过 gRPC OpenSVCProxy 双向流，桥接 Agent 和本地 K8S Service ClusterIP
func handleSVCProxyRequest(ctx context.Context, client pb.EndpointServiceClient, cfg *EndpointConfig, req *pb.SVCProxyRequest, authorization *endpointSessionAuthorization, discovery *K8SServiceDiscovery) {
	logger.Infof("收到 SVC 代理请求: session_id=%s, %s/%s:%d", req.SessionId, req.Namespace, req.ServiceName, req.Port)
	now := time.Now().UTC()
	requireV2 := endpointRequestHasV2Fields(req) || (authorization != nil && authorization.enforceV2())
	if requireV2 {
		if authorization == nil || !authorization.enabled(now) {
			logger.Warnf("Endpoint SVCProxy v2 权限缓存不可用，拒绝请求: session_id=%s", req.SessionId)
			return
		}
		handleSVCProxyRequestV2(ctx, client, cfg, req, authorization, discovery)
		return
	}

	// 建立 OpenSVCProxy gRPC 流
	stream, err := client.OpenSVCProxy(ctx)
	if err != nil {
		logger.Warnf("建立 OpenSVCProxy 流失败: %v", err)
		return
	}

	// 发送首包（携带 session_id 和 token）
	if err := stream.Send(&pb.EndpointSVCProxyData{
		IsOpen:    true,
		SessionId: req.SessionId,
		Token:     cfg.Agent.Token,
	}); err != nil {
		logger.Warnf("发送 OpenSVCProxy 首包失败: %v", err)
		return
	}

	// 连接本地 K8S Service
	// 优先使用 ClusterIP 直连（物理节点部署时 cluster.local DNS 不可用）
	// 回退到 DNS 名（Pod 内部署时 ClusterIP 可能未知）
	var targetAddr string
	var targetConn net.Conn
	var dialErr error

	if req.ClusterIp != "" {
		// 优先：ClusterIP 直连，不依赖 DNS
		targetAddr = fmt.Sprintf("%s:%d", req.ClusterIp, req.Port)
		targetConn, dialErr = net.Dial("tcp", targetAddr)
	}

	if targetConn == nil {
		// 回退：cluster.local DNS 名（Pod 内部署）
		dnsAddr := fmt.Sprintf("%s.%s.svc.cluster.local:%d", req.ServiceName, req.Namespace, req.Port)
		targetConn, dialErr = net.Dial("tcp", dnsAddr)
		if targetConn != nil {
			targetAddr = dnsAddr
		}
	}

	if targetConn == nil {
		logger.Warnf("SVC 连接失败: cluster_ip=%s, svc=%s/%s:%d, err=%v",
			req.ClusterIp, req.Namespace, req.ServiceName, req.Port, dialErr)
		_ = stream.Send(&pb.EndpointSVCProxyData{IsClose: true, Error: fmt.Sprintf("连接失败: %v", dialErr)})
		return
	}
	defer targetConn.Close()

	logger.Infof("SVC 代理已建立: session_id=%s, target=%s", req.SessionId, targetAddr)

	// 双向桥接：gRPC stream ↔ K8S Service TCP 连接
	var wg sync.WaitGroup
	wg.Add(2)

	// gRPC → K8S Service（Agent/Desktop 数据 → ClusterIP）
	go func() {
		defer wg.Done()
		for {
			msg, err := stream.Recv()
			if err != nil {
				if err != io.EOF {
					logger.Debugf("SVC gRPC 接收结束: %v", err)
				}
				targetConn.Close()
				return
			}
			if msg.IsClose {
				targetConn.Close()
				return
			}
			if len(msg.Data) > 0 {
				if _, writeErr := targetConn.Write(msg.Data); writeErr != nil {
					logger.Debugf("SVC TCP 写入失败: %v", writeErr)
					return
				}
			}
		}
	}()

	// K8S Service → gRPC（ClusterIP 响应 → Agent/Desktop）
	go func() {
		defer wg.Done()
		buf := make([]byte, 32*1024)
		for {
			n, err := targetConn.Read(buf)
			if n > 0 {
				if sendErr := stream.Send(&pb.EndpointSVCProxyData{Data: buf[:n]}); sendErr != nil {
					logger.Debugf("SVC gRPC 发送失败: %v", sendErr)
					return
				}
			}
			if err != nil {
				if err != io.EOF {
					logger.Debugf("SVC TCP 读取结束: %v", err)
				}
				_ = stream.Send(&pb.EndpointSVCProxyData{IsClose: true})
				return
			}
		}
	}()

	wg.Wait()
	logger.Infof("SVC 代理已关闭: session_id=%s", req.SessionId)
}

func endpointRequestHasV2Fields(req *pb.SVCProxyRequest) bool {
	return req != nil && (req.ResourceId != "" || req.SourceId != "" || req.TargetRevisionId != "" ||
		req.ServiceUid != "" || req.PortName != "" || req.Protocol != "" || req.AuthorizationRevision != 0)
}

func handleSVCProxyRequestV2(ctx context.Context, client pb.EndpointServiceClient, cfg *EndpointConfig, req *pb.SVCProxyRequest, authorization *endpointSessionAuthorization, discovery *K8SServiceDiscovery) {
	now := time.Now().UTC()
	permission, allowed := authorization.cache.Permission(req.SessionId, now)
	if !allowed || !endpointSVCRequestMatchesPermission(req, permission) {
		logger.Warnf("Endpoint SVCProxy v2 权限拒绝: session_id=%s", req.SessionId)
		return
	}
	if discovery == nil {
		_ = authorization.sessions.FailV2(req.SessionId, "SERVICE_TARGET_UNAVAILABLE")
		logger.Warnf("Endpoint SVCProxy v2 本地 Service 发现不可用: session_id=%s", req.SessionId)
		return
	}
	clusterIP, matched := discovery.ResolveService(req.Namespace, req.ServiceName, req.ServiceUid, req.PortName, req.Port, req.Protocol)
	if !matched {
		_ = authorization.sessions.FailV2(req.SessionId, "SERVICE_TARGET_CHANGED")
		logger.Warnf("Endpoint SVCProxy v2 本地 Service UID/Port/Protocol 不匹配: session_id=%s", req.SessionId)
		return
	}
	targetAddr := net.JoinHostPort(clusterIP, strconv.Itoa(int(req.Port)))
	targetConn, err := net.Dial("tcp", targetAddr)
	if err != nil {
		_ = authorization.sessions.FailV2(req.SessionId, "SERVICE_CONNECT_FAILED")
		logger.Warnf("Endpoint SVCProxy v2 连接失败: session_id=%s target=%s err=%v", req.SessionId, targetAddr, err)
		return
	}
	defer targetConn.Close()
	sessionCtx, err := authorization.sessions.BeginV2(ctx, permission)
	if err != nil {
		logger.Warnf("Endpoint SVCProxy v2 会话注册失败: session_id=%s err=%v", req.SessionId, err)
		return
	}
	stream, err := client.OpenSVCProxy(sessionCtx)
	if err != nil {
		_ = authorization.sessions.EndV2(req.SessionId, "failed", "ENDPOINT_STREAM_FAILED")
		return
	}
	if err := stream.Send(&pb.EndpointSVCProxyData{IsOpen: true, SessionId: req.SessionId, Token: cfg.Agent.Token}); err != nil {
		_ = authorization.sessions.EndV2(req.SessionId, "failed", "ENDPOINT_STREAM_FAILED")
		return
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
				if sendErr := stream.Send(&pb.EndpointSVCProxyData{Data: buf[:n]}); sendErr != nil {
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
		if bridgeErr != nil && bridgeErr != io.EOF {
			result, reason = "failed", "SERVICE_STREAM_FAILED"
		}
	case <-sessionCtx.Done():
		result, reason = "ended", "context_canceled"
		_ = stream.Send(&pb.EndpointSVCProxyData{IsClose: true})
	}
	_ = targetConn.Close()
	_ = stream.CloseSend()
	if err := authorization.sessions.EndV2(req.SessionId, result, reason); err != nil {
		logger.Errorf("Endpoint SVCProxy v2 会话事件持久化失败: session_id=%s err=%v", req.SessionId, err)
	}
	logger.Infof("Endpoint SVCProxy v2 已关闭: session_id=%s", req.SessionId)
}

func endpointSVCRequestMatchesPermission(req *pb.SVCProxyRequest, permission *pb.ResourceSessionPermissionV2) bool {
	return req != nil && permission != nil && permission.ResourceType == "container_service" && permission.Target != nil &&
		req.SessionId == permission.SessionId && req.ResourceId == permission.ResourceId && req.SourceId == permission.SourceId &&
		req.TargetRevisionId == permission.TargetRevisionId && req.AuthorizationRevision == permission.AuthorizationRevision &&
		req.Namespace == permission.Target.NamespaceName && req.ServiceName == permission.Target.ServiceName &&
		req.ServiceUid == permission.Target.ServiceUid && req.PortName == permission.Target.PortName &&
		req.Port == permission.Target.PortNumber && req.Protocol == permission.Target.Protocol && req.Protocol == "TCP" && req.ClusterIp == ""
}
