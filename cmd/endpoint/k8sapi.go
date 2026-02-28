package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

// handleK8SAPIProxyRequest 处理来自 Agent 的 K8S API 代理请求
// 通过 gRPC OpenK8SAPIProxy 双向流，接收结构化 HTTP 请求，使用 client-go 发送到 K8S API Server
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

	// 接收 Agent 发送的身份信息（user_name 和 k8s_groups）
	identityMsg, err := stream.Recv()
	if err != nil {
		logger.Warnf("接收身份信息失败: session_id=%s, err=%v", req.SessionId, err)
		return
	}

	userName := identityMsg.UserName
	k8sGroups := identityMsg.K8SGroups
	logger.Infof("K8SAPI 代理身份信息: session_id=%s, user=%s, groups=%v", req.SessionId, userName, k8sGroups)

	if userName == "" {
		logger.Warnf("K8SAPI 代理失败: 未收到用户身份信息, session_id=%s", req.SessionId)
		_ = stream.Send(&pb.K8SAPIProxyData{IsClose: true, Error: "未收到用户身份信息"})
		return
	}

	// 加载 KubeconfigManager
	kubeconfigMgr, err := newEndpointKubeconfigManager()
	if err != nil {
		logger.Warnf("K8SAPI 代理失败: 加载 kubeconfig 失败: %v", err)
		_ = stream.Send(&pb.K8SAPIProxyData{IsClose: true, Error: fmt.Sprintf("加载 kubeconfig 失败: %v", err)})
		return
	}

	// 创建 HTTP 客户端（带 K8S 认证）
	httpClient, err := kubeconfigMgr.getHTTPClient()
	if err != nil {
		logger.Warnf("K8SAPI 代理失败: 创建 HTTP 客户端失败: %v", err)
		_ = stream.Send(&pb.K8SAPIProxyData{IsClose: true, Error: fmt.Sprintf("创建 HTTP 客户端失败: %v", err)})
		return
	}

	logger.Infof("K8SAPI 代理已建立: session_id=%s, user=%s, api_server=%s", req.SessionId, userName, kubeconfigMgr.apiServerURL)

	// 接收结构化 HTTP 请求
	reqMsg, err := stream.Recv()
	if err != nil {
		logger.Warnf("接收 HTTP 请求失败: session_id=%s, err=%v", req.SessionId, err)
		return
	}

	// 检查是否有结构化请求
	if reqMsg.HttpRequest == nil {
		logger.Warnf("未收到结构化 HTTP 请求: session_id=%s", req.SessionId)
		_ = stream.Send(&pb.K8SAPIProxyData{IsClose: true, Error: "未收到结构化 HTTP 请求"})
		return
	}

	httpReq := reqMsg.HttpRequest

	logger.Infof("K8SAPI Endpoint 收到请求: method=%s, path=%s, user=%s, session_id=%s",
		httpReq.Method, httpReq.Path, userName, req.SessionId)

	// 使用 client-go 构造新的 HTTP 请求
	fullURL := kubeconfigMgr.apiServerURL + httpReq.Path
	k8sReq, err := http.NewRequest(httpReq.Method, fullURL, bytes.NewReader(httpReq.Body))
	if err != nil {
		logger.Warnf("构造 HTTP 请求失败: %v", err)
		_ = stream.Send(&pb.K8SAPIProxyData{
			IsClose: true,
			Error:   fmt.Sprintf("构造 HTTP 请求失败: %v", err),
		})
		return
	}

	// 设置请求头（从 Agent 传来的）
	for key, val := range httpReq.Headers {
		k8sReq.Header.Set(key, val)
	}

	// 清除 Desktop 的 Authorization 头（防止与 kubeconfig 证书认证冲突）
	k8sReq.Header.Del("Authorization")

	// 注入 Impersonation 头
	k8sReq.Header.Set("Impersonate-User", userName)
	for _, group := range k8sGroups {
		k8sReq.Header.Add("Impersonate-Group", group)
	}

	logger.Infof("K8SAPI Endpoint 转发: user=%s, groups=%v, path=%s", userName, k8sGroups, httpReq.Path)

	// 记录所有请求头
	logger.Infof("K8SAPI Endpoint 请求头:")
	for key, vals := range k8sReq.Header {
		logger.Infof("  %s: %v", key, vals)
	}

	// 发送请求到 K8S API Server
	k8sResp, err := httpClient.Do(k8sReq)
	if err != nil {
		// 详细记录错误信息
		logger.Warnf("K8SAPI 请求失败: method=%s, url=%s", httpReq.Method, fullURL)
		logger.Warnf("错误详情: %+v", err)
		logger.Warnf("错误类型: %T", err)
		
		// 尝试获取更详细的错误信息
		if urlErr, ok := err.(*url.Error); ok {
			logger.Warnf("URL Error 详情: Op=%s, URL=%s, Err=%v, Err_type=%T", urlErr.Op, urlErr.URL, urlErr.Err, urlErr.Err)
		}
		
		_ = stream.Send(&pb.K8SAPIProxyData{
			IsClose: true,
			Error:   fmt.Sprintf("K8S API 请求失败: %v", err),
		})
		return
	}
	defer k8sResp.Body.Close()

	logger.Infof("K8SAPI 后端响应: session_id=%s, status=%d", req.SessionId, k8sResp.StatusCode)

	// 检查是否是流式响应（Transfer-Encoding: chunked 或 follow=true）
	isStreaming := k8sResp.Header.Get("Transfer-Encoding") == "chunked" ||
		k8sResp.Header.Get("Content-Type") == "text/plain" // kubectl logs 返回 text/plain

	if isStreaming {
		logger.Infof("K8SAPI 检测到流式响应: session_id=%s", req.SessionId)

		// 先发送响应头（status code 和 headers）
		httpResp := &pb.K8SAPIHTTPResponse{
			StatusCode: int32(k8sResp.StatusCode),
			Headers:    make(map[string]string),
		}

		// 传递所有响应头
		for key, vals := range k8sResp.Header {
			if len(vals) > 0 {
				httpResp.Headers[key] = vals[0]
			}
		}

		if err := stream.Send(&pb.K8SAPIProxyData{
			HttpResponse: httpResp,
		}); err != nil {
			logger.Warnf("发送响应头失败: %v", err)
			return
		}

		// 持续读取并发送响应体（分块）
		buf := make([]byte, 32*1024) // 32KB 缓冲区
		totalBytes := 0
		for {
			n, err := k8sResp.Body.Read(buf)
			if n > 0 {
				totalBytes += n
				// 发送数据块
				if sendErr := stream.Send(&pb.K8SAPIProxyData{
					HttpResponse: &pb.K8SAPIHTTPResponse{
						Body: buf[:n],
					},
				}); sendErr != nil {
					logger.Warnf("发送响应数据失败: %v", sendErr)
					return
				}
			}
			if err != nil {
				if err != io.EOF {
					logger.Warnf("读取响应体失败: %v", err)
					_ = stream.Send(&pb.K8SAPIProxyData{
						IsClose: true,
						Error:   fmt.Sprintf("读取响应体失败: %v", err),
					})
				}
				break
			}
		}

		logger.Infof("K8SAPI 流式响应完成: session_id=%s, total_bytes=%d", req.SessionId, totalBytes)

	} else {
		// 非流式响应，一次性读取
		respBody, err := io.ReadAll(k8sResp.Body)
		if err != nil {
			logger.Warnf("读取响应体失败: %v", err)
			_ = stream.Send(&pb.K8SAPIProxyData{
				IsClose: true,
				Error:   fmt.Sprintf("读取响应体失败: %v", err),
			})
			return
		}

		logger.Infof("K8SAPI 后端响应体: session_id=%s, body_size=%d", req.SessionId, len(respBody))

		// 构造结构化响应
		httpResp := &pb.K8SAPIHTTPResponse{
			StatusCode: int32(k8sResp.StatusCode),
			Headers:    make(map[string]string),
			Body:       respBody,
		}

		// 传递必要的响应头
		necessaryHeaders := []string{
			"Content-Type", "Content-Length",
		}
		for _, key := range necessaryHeaders {
			if val := k8sResp.Header.Get(key); val != "" {
				httpResp.Headers[key] = val
			}
		}

		// 发送结构化响应到 Agent
		if err := stream.Send(&pb.K8SAPIProxyData{
			HttpResponse: httpResp,
		}); err != nil {
			logger.Warnf("发送响应失败: %v", err)
			return
		}

		logger.Infof("K8SAPI 代理已完成: session_id=%s, status=%d", req.SessionId, k8sResp.StatusCode)
	}

	// 发送关闭标志
	_ = stream.Send(&pb.K8SAPIProxyData{IsClose: true})

	// 等待 Agent 关闭流
	closeMsg, err := stream.Recv()
	if err != nil || (closeMsg != nil && closeMsg.IsClose) {
		logger.Debugf("K8SAPI 代理已关闭: session_id=%s", req.SessionId)
	}
}

// endpointKubeconfigManager Endpoint 轻量版 Kubeconfig 管理器
// 只需要 GetHTTPClient 和 GetRESTConfig，不需要心跳上报等功能
type endpointKubeconfigManager struct {
	restConfig   *rest.Config
	apiServerURL string
	mode         string // "host" / "pod"
}

// newEndpointKubeconfigManager 创建 Endpoint Kubeconfig 管理器
func newEndpointKubeconfigManager() (*endpointKubeconfigManager, error) {
	// 1. 尝试主机模式：从 ~/.kube/config 加载
	restConfig, err := loadEndpointKubeconfig()
	if err == nil {
		logger.Infof("Endpoint Kubeconfig: 使用主机模式, API Server: %s", restConfig.Host)
		logger.Debugf("Endpoint Kubeconfig 详情: CertFile=%s, KeyFile=%s, CAFile=%s",
			restConfig.TLSClientConfig.CertFile,
			restConfig.TLSClientConfig.KeyFile,
			restConfig.TLSClientConfig.CAFile)
		return &endpointKubeconfigManager{
			restConfig:   restConfig,
			apiServerURL: restConfig.Host,
			mode:         "host",
		}, nil
	}

	logger.Debugf("Endpoint 主机模式加载失败: %v，尝试 Pod 模式", err)

	// 2. 尝试 Pod 模式：从 ServiceAccount 加载
	restConfig, err = rest.InClusterConfig()
	if err == nil {
		logger.Infof("Endpoint Kubeconfig: 使用 Pod 模式, API Server: %s", restConfig.Host)
		return &endpointKubeconfigManager{
			restConfig:   restConfig,
			apiServerURL: restConfig.Host,
			mode:         "pod",
		}, nil
	}

	return nil, fmt.Errorf("无法加载 kubeconfig：主机模式和 Pod 模式都失败")
}

// getHTTPClient 创建配置好认证的 HTTP 客户端
func (m *endpointKubeconfigManager) getHTTPClient() (*http.Client, error) {
	// 使用 rest.TransportFor 创建完整的 Transport（包含证书加载）
	transport, err := rest.TransportFor(m.restConfig)
	if err != nil {
		return nil, fmt.Errorf("创建 Transport 失败: %w", err)
	}

	// 禁用代理（K8S API Server 是内网地址，不应该通过代理访问）
	// rest.TransportFor 返回的可能是 *http.Transport 或其他类型
	if httpTransport, ok := transport.(*http.Transport); ok {
		httpTransport.Proxy = nil
		logger.Debugf("已禁用 HTTP 代理")
	}

	logger.Debugf("Transport 创建成功: type=%T", transport)
	return &http.Client{Transport: transport}, nil
}

// loadEndpointKubeconfig 从 ~/.kube/config 加载配置
func loadEndpointKubeconfig() (*rest.Config, error) {
	kubeconfigPath := os.Getenv("KUBECONFIG")
	if kubeconfigPath == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("获取用户主目录失败: %w", err)
		}
		kubeconfigPath = filepath.Join(homeDir, ".kube", "config")
	}

	if _, err := os.Stat(kubeconfigPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("kubeconfig 文件不存在: %s", kubeconfigPath)
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("加载 kubeconfig 失败: %w", err)
	}

	return config, nil
}

// handleRawStreamRequest 处理来自 Agent 的原始字节流请求（协议升级）
// 用于 kubectl logs/exec/cp/port-forward 等需要协议升级的命令
func handleRawStreamRequest(ctx context.Context, client pb.EndpointServiceClient, cfg *EndpointConfig, req *pb.RawStreamRequest) {
	logger.Infof("[RawStream] 收到原始流请求: session_id=%s, user=%s, groups=%v",
		req.SessionId, req.UserName, req.K8SGroups)

	// 建立 OpenRawStream gRPC 流
	stream, err := client.OpenRawStream(ctx)
	if err != nil {
		logger.Warnf("[RawStream] 建立流失败: %v", err)
		return
	}

	// 发送首包（携带 session_id 和 token）
	if err := stream.Send(&pb.RawStreamData{
		IsOpen:    true,
		SessionId: req.SessionId,
		Token:     cfg.Agent.Token,
	}); err != nil {
		logger.Warnf("[RawStream] 发送首包失败: %v", err)
		return
	}

	userName := req.UserName
	k8sGroups := req.K8SGroups

	logger.Infof("[RawStream] 原始流已建立: session_id=%s, user=%s", req.SessionId, userName)

	// 加载 KubeconfigManager
	kubeconfigMgr, err := newEndpointKubeconfigManager()
	if err != nil {
		logger.Warnf("[RawStream] 加载 kubeconfig 失败: %v", err)
		_ = stream.Send(&pb.RawStreamData{IsClose: true, Error: fmt.Sprintf("加载 kubeconfig 失败: %v", err)})
		return
	}

	// 接收原始 HTTP 请求
	reqMsg, err := stream.Recv()
	if err != nil {
		logger.Warnf("[RawStream] 接收原始请求失败: %v", err)
		return
	}

	if len(reqMsg.Data) == 0 {
		logger.Warnf("[RawStream] 未收到原始请求数据")
		_ = stream.Send(&pb.RawStreamData{IsClose: true, Error: "未收到原始请求数据"})
		return
	}

	// 解析 HTTP 请求（提取 method、path、headers）
	reqReader := bufio.NewReader(bytes.NewReader(reqMsg.Data))
	httpReq, err := http.ReadRequest(reqReader)
	if err != nil {
		logger.Warnf("[RawStream] 解析 HTTP 请求失败: %v", err)
		_ = stream.Send(&pb.RawStreamData{IsClose: true, Error: fmt.Sprintf("解析 HTTP 请求失败: %v", err)})
		return
	}

	logger.Infof("[RawStream] 解析请求: method=%s, path=%s, upgrade=%s",
		httpReq.Method, httpReq.URL.Path, httpReq.Header.Get("Upgrade"))

	// 建立到 K8S API Server 的 TLS 连接
	apiServerURL, err := url.Parse(kubeconfigMgr.apiServerURL)
	if err != nil {
		logger.Warnf("[RawStream] 解析 API Server URL 失败: %v", err)
		_ = stream.Send(&pb.RawStreamData{IsClose: true, Error: fmt.Sprintf("解析 API Server URL 失败: %v", err)})
		return
	}

	// 使用 rest.TransportFor 创建 TLS 配置
	transport, err := rest.TransportFor(kubeconfigMgr.restConfig)
	if err != nil {
		logger.Warnf("[RawStream] 创建 Transport 失败: %v", err)
		_ = stream.Send(&pb.RawStreamData{IsClose: true, Error: fmt.Sprintf("创建 Transport 失败: %v", err)})
		return
	}

	httpTransport, ok := transport.(*http.Transport)
	if !ok {
		logger.Warnf("[RawStream] Transport 类型不是 *http.Transport")
		_ = stream.Send(&pb.RawStreamData{IsClose: true, Error: "Transport 类型错误"})
		return
	}

	// 禁用代理
	httpTransport.Proxy = nil

	// 建立 TLS 连接
	addr := apiServerURL.Host
	if apiServerURL.Port() == "" {
		addr = addr + ":443"
	}

	// 复制 TLS 配置，强制 ALPN 只使用 HTTP/1.1
	// 协议升级（WebSocket/SPDY）需要 HTTP/1.1 的 101 Switching Protocols 响应
	// 如果 ALPN 协商了 HTTP/2，K8S API Server 会返回 HTTP/2 帧，导致 kubectl 报错
	tlsConfig := httpTransport.TLSClientConfig.Clone()
	tlsConfig.NextProtos = []string{"http/1.1"}

	tlsConn, err := tls.Dial("tcp", addr, tlsConfig)
	if err != nil {
		logger.Warnf("[RawStream] 连接 K8S API Server 失败: %v", err)
		_ = stream.Send(&pb.RawStreamData{IsClose: true, Error: fmt.Sprintf("连接 K8S API Server 失败: %v", err)})
		return
	}
	defer tlsConn.Close()

	logger.Infof("[RawStream] 已连接到 K8S API Server: %s", addr)

	// 修改请求头（注入 Impersonation、清除 Authorization）
	httpReq.Header.Del("Authorization")
	httpReq.Header.Set("Impersonate-User", userName)
	for _, group := range k8sGroups {
		httpReq.Header.Add("Impersonate-Group", group)
	}

	// 修改 Host 头和 URL
	httpReq.Host = apiServerURL.Host
	httpReq.URL.Scheme = "https"
	httpReq.URL.Host = apiServerURL.Host

	logger.Infof("[RawStream] 转发请求: user=%s, groups=%v, path=%s", userName, k8sGroups, httpReq.URL.Path)

	// 发送修改后的 HTTP 请求到 K8S API Server
	if err := httpReq.Write(tlsConn); err != nil {
		logger.Warnf("[RawStream] 发送请求到 K8S API Server 失败: %v", err)
		_ = stream.Send(&pb.RawStreamData{IsClose: true, Error: fmt.Sprintf("发送请求失败: %v", err)})
		return
	}

	logger.Infof("[RawStream] 已发送请求到 K8S API Server: user=%s, path=%s", userName, httpReq.URL.Path)

	// 双向复制字节流
	errChan := make(chan error, 2)

	// Agent → K8S API Server
	go func() {
		for {
			msg, err := stream.Recv()
			if err != nil {
				if err != io.EOF {
					logger.Debugf("[RawStream] 从 Agent 接收失败: %v", err)
				}
				errChan <- nil
				return
			}

			if msg.IsClose {
				logger.Debugf("[RawStream] Agent 关闭流")
				errChan <- nil
				return
			}

			if len(msg.Data) > 0 {
				if _, writeErr := tlsConn.Write(msg.Data); writeErr != nil {
					logger.Debugf("[RawStream] 写入到 K8S API Server 失败: %v", writeErr)
					errChan <- writeErr
					return
				}
			}
		}
	}()

	// K8S API Server → Agent
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := tlsConn.Read(buf)
			if n > 0 {
				if sendErr := stream.Send(&pb.RawStreamData{Data: buf[:n]}); sendErr != nil {
					logger.Debugf("[RawStream] 发送到 Agent 失败: %v", sendErr)
					errChan <- sendErr
					return
				}
			}
			if err != nil {
				if err != io.EOF {
					logger.Debugf("[RawStream] 从 K8S API Server 读取失败: %v", err)
				}
				errChan <- nil
				return
			}
		}
	}()

	// 等待任一方向出错或完成
	err = <-errChan
	if err != nil {
		logger.Debugf("[RawStream] 原始流结束: %v", err)
	}

	logger.Infof("[RawStream] 原始流已关闭: session_id=%s, user=%s", req.SessionId, userName)

	// 发送关闭标志
	_ = stream.Send(&pb.RawStreamData{IsClose: true})
}
