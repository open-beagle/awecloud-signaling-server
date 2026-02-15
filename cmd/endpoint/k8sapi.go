package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

// handleK8SAPIProxyRequest 处理来自 Agent 的 K8S API 代理请求
// 通过 gRPC OpenK8SAPIProxy 双向流，桥接 Agent 和本地 K8S API Server
func handleK8SAPIProxyRequest(ctx context.Context, client pb.EndpointServiceClient, cfg *EndpointConfig, req *pb.K8SAPIProxyRequest) {
	logger.Infof("收到 K8SAPI 代理请求: session_id=%s", req.SessionId)

	// 建立 OpenK8SAPIProxy gRPC 流
	stream, err := client.OpenK8SAPIProxy(ctx)
	if err != nil {
		logger.Warnf("建立 OpenK8SAPIProxy 流失败: %v", err)
		return
	}

	// 发送首包（携带 session_id 和 token）
	if err := stream.Send(&pb.K8SAPIProxyData{
		IsOpen:    true,
		SessionId: req.SessionId,
		Token:     cfg.Agent.Token,
	}); err != nil {
		logger.Warnf("发送 OpenK8SAPIProxy 首包失败: %v", err)
		return
	}

	// 连接本地 K8S API Server
	apiServer := cfg.K8S.APIServer
	if apiServer == "" {
		logger.Warnf("K8SAPI 代理失败: 未配置 api_server")
		_ = stream.Send(&pb.K8SAPIProxyData{IsClose: true, Error: "未配置 K8S API Server"})
		return
	}

	// 解析 API Server 地址，建立 TCP/TLS 连接
	targetConn, err := dialK8SAPIServer(apiServer)
	if err != nil {
		logger.Warnf("K8SAPI 连接失败: %s, err=%v", apiServer, err)
		_ = stream.Send(&pb.K8SAPIProxyData{IsClose: true, Error: fmt.Sprintf("连接 K8S API 失败: %v", err)})
		return
	}
	defer targetConn.Close()

	logger.Infof("K8SAPI 代理已建立: session_id=%s, api_server=%s", req.SessionId, apiServer)

	// 双向桥接：gRPC stream ↔ K8S API TCP/TLS 连接
	var wg sync.WaitGroup
	wg.Add(2)

	// gRPC → K8S API（Agent/Desktop 请求 → K8S API Server）
	go func() {
		defer wg.Done()
		for {
			msg, err := stream.Recv()
			if err != nil {
				if err != io.EOF {
					logger.Debugf("K8SAPI gRPC 接收结束: %v", err)
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
					logger.Debugf("K8SAPI TCP 写入失败: %v", writeErr)
					return
				}
			}
		}
	}()

	// K8S API → gRPC（K8S API Server 响应 → Agent/Desktop）
	go func() {
		defer wg.Done()
		buf := make([]byte, 32*1024)
		for {
			n, err := targetConn.Read(buf)
			if n > 0 {
				if sendErr := stream.Send(&pb.K8SAPIProxyData{Data: buf[:n]}); sendErr != nil {
					logger.Debugf("K8SAPI gRPC 发送失败: %v", sendErr)
					return
				}
			}
			if err != nil {
				if err != io.EOF {
					logger.Debugf("K8SAPI TCP 读取结束: %v", err)
				}
				_ = stream.Send(&pb.K8SAPIProxyData{IsClose: true})
				return
			}
		}
	}()

	wg.Wait()
	logger.Infof("K8SAPI 代理已关闭: session_id=%s", req.SessionId)
}

// dialK8SAPIServer 连接 K8S API Server
// 支持 https:// 和 http:// 前缀，默认 https
func dialK8SAPIServer(apiServer string) (net.Conn, error) {
	// 解析地址
	host := apiServer
	useTLS := true

	if len(host) > 8 && host[:8] == "https://" {
		host = host[8:]
		useTLS = true
	} else if len(host) > 7 && host[:7] == "http://" {
		host = host[7:]
		useTLS = false
	}

	// 确保有端口
	if _, _, err := net.SplitHostPort(host); err != nil {
		if useTLS {
			host = host + ":6443"
		} else {
			host = host + ":8080"
		}
	}

	if useTLS {
		return tls.Dial("tcp", host, &tls.Config{
			InsecureSkipVerify: true, // Endpoint 访问本地 K8S API，跳过证书验证
		})
	}
	return net.Dial("tcp", host)
}
