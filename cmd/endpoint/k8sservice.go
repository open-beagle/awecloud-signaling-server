package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

// handleSVCProxyRequest 处理来自 Agent 的 K8S Service 代理请求
// 通过 gRPC OpenSVCProxy 双向流，桥接 Agent 和本地 K8S Service ClusterIP
func handleSVCProxyRequest(ctx context.Context, client pb.EndpointServiceClient, cfg *EndpointConfig, req *pb.SVCProxyRequest) {
	logger.Infof("收到 SVC 代理请求: session_id=%s, %s/%s:%d", req.SessionId, req.Namespace, req.ServiceName, req.Port)

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
