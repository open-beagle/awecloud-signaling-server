package agent

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"k8s.io/client-go/rest"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
)

// K8SAPIProxy K8S API 反向代理
// 在 tsnet 网络上监听，接收 Desktop 的 kubectl 请求，
// 通过 Impersonation 转发到本机 K8S API Server
type K8SAPIProxy struct {
	tsManager      *TailscaleManager
	permCache      *PermissionCache
	identity       *IdentityExtractor
	auditCollector *AuditCollector
	kubeconfigMgr  *KubeconfigManager

	listenPort uint16 // tsnet 监听端口
	listener   net.Listener
	httpServer *http.Server

	// fallback handler 取消注册函数
	deregisterFallback func()

	ctx    context.Context
	cancel context.CancelFunc
}

// NewK8SAPIProxy 创建 K8S API 代理
func NewK8SAPIProxy(listenPort uint16, tsManager *TailscaleManager, permCache *PermissionCache, auditCollector *AuditCollector, parentCtx context.Context) (*K8SAPIProxy, error) {
	ctx, cancel := context.WithCancel(parentCtx)

	// 创建身份提取器
	identity, err := NewIdentityExtractor(tsManager)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("创建身份提取器失败: %w", err)
	}

	// 创建 Kubeconfig 管理器
	kubeconfigMgr, err := NewKubeconfigManager()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("创建 Kubeconfig 管理器失败: %w", err)
	}

	logger.Infof("K8S API 代理初始化: mode=%s, api_server=%s, auth=%s",
		kubeconfigMgr.GetMode(),
		kubeconfigMgr.GetAPIServerURL(),
		kubeconfigMgr.GetAuthInfo())

	return &K8SAPIProxy{
		tsManager:      tsManager,
		permCache:      permCache,
		identity:       identity,
		auditCollector: auditCollector,
		kubeconfigMgr:  kubeconfigMgr,
		listenPort:     listenPort,
		ctx:            ctx,
		cancel:         cancel,
	}, nil
}

// Start 启动 K8S API 代理
// 使用 RegisterFallbackTCPHandler 绕过 tsnet.Listen 的已知 bug。
// Agent 端使用纯 HTTP 监听（不做 TLS），TLS 终止由 Desktop 本地完成。
// tsnet 隧道本身是 WireGuard 加密的，中间无需再套 TLS。
func (p *K8SAPIProxy) Start() error {
	tailscaleIP := p.tsManager.GetIP()
	if tailscaleIP == "" {
		return fmt.Errorf("Tailscale IP 未就绪，无法启动 K8S API 代理")
	}

	// 创建 channel-based listener，用于桥接 fallback handler 和 http.Server
	chanListener := newChannelListener()
	p.listener = chanListener

	// 注册 fallback TCP handler，按目标端口分发连接
	deregister := p.tsManager.RegisterFallbackTCPHandler(func(src, dst netip.AddrPort) (func(net.Conn), bool) {
		if dst.Port() == p.listenPort {
			// 拦截此端口的连接，交给 chanListener
			return func(conn net.Conn) {
				chanListener.Enqueue(conn)
			}, true
		}
		return nil, false // 不拦截，交给下一个 handler
	})
	p.deregisterFallback = deregister

	// 使用 KubeconfigManager 创建 HTTP 客户端
	httpClient, err := p.kubeconfigMgr.GetHTTPClient()
	if err != nil {
		return fmt.Errorf("创建 HTTP 客户端失败: %w", err)
	}

	// 解析 API Server URL
	apiServerURL, err := url.Parse(p.kubeconfigMgr.GetAPIServerURL())
	if err != nil {
		return fmt.Errorf("解析 API Server URL 失败: %w", err)
	}

	// 创建反向代理
	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = apiServerURL.Scheme
			req.URL.Host = apiServerURL.Host
			req.Host = apiServerURL.Host
		},
		Transport: httpClient.Transport,
	}

	// 创建 HTTP Server（纯 HTTP，Desktop 通过 tsnet 发来的是明文 HTTP）
	p.httpServer = &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p.handleRequest(w, r, proxy, apiServerURL)
		}),
	}

	// 启动服务
	go func() {
		logger.Infof("K8S API 代理已启动（FallbackTCPHandler, HTTP）: port=%d -> %s", p.listenPort, apiServerURL.String())
		if err := p.httpServer.Serve(chanListener); err != nil && err != http.ErrServerClosed {
			logger.Errorf("K8S API 代理服务错误: %v", err)
		}
	}()

	return nil
}

// Stop 停止 K8S API 代理
func (p *K8SAPIProxy) Stop() {
	p.cancel()
	// 取消注册 fallback handler
	if p.deregisterFallback != nil {
		p.deregisterFallback()
	}
	if p.httpServer != nil {
		p.httpServer.Close()
	}
	if p.listener != nil {
		p.listener.Close()
	}
	logger.Info("K8S API 代理已停止")
}

// handleRequest 处理请求：身份提取 → 权限检查 → Impersonation → 转发
// 支持普通 HTTP 请求和 SPDY/WebSocket 协议升级（kubectl exec/logs/cp）
func (p *K8SAPIProxy) handleRequest(w http.ResponseWriter, r *http.Request, proxy *httputil.ReverseProxy, apiServerURL *url.URL) {
	logger.Infof("K8SAPI 收到请求: remote=%s, method=%s, path=%s", r.RemoteAddr, r.Method, r.URL.Path)

	// 1. 从 tsnet 连接提取对端身份
	peerIdentity, err := p.identity.ExtractFromConn(r.Context(), &remoteAddrWrapper{addr: r.RemoteAddr})
	if err != nil {
		logger.Warnf("K8SAPI 身份提取失败: remote=%s, err=%v", r.RemoteAddr, err)
		// 返回 403 而非 401，避免 kubectl 提示输入用户名密码
		http.Error(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","message":"身份验证失败","reason":"Forbidden","code":403}`, http.StatusForbidden)
		return
	}

	logger.Infof("K8SAPI 身份提取成功: user=%s, role=%s, node=%s", peerIdentity.UserName, peerIdentity.Role, peerIdentity.NodeName)

	// 2. 从 URL 路径提取命名空间（如 /api/v1/namespaces/default/pods）
	namespace := extractNamespaceFromPath(r.URL.Path)

	// 3. 查询权限缓存
	k8sGroups, allowed := p.permCache.CheckK8SAccess(peerIdentity.UserName, namespace)
	if !allowed {
		logger.Warnf("K8SAPI 权限拒绝: user=%s, namespace=%s", peerIdentity.UserName, namespace)
		// 返回 403，使用 K8S 风格的 JSON 响应
		http.Error(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","message":"权限不足","reason":"Forbidden","code":403}`, http.StatusForbidden)
		return
	}

	// 4. 设置 Impersonation 头
	r.Header.Set("Impersonate-User", peerIdentity.UserName)
	// 清除已有的 Impersonate-Group 头，重新设置
	r.Header.Del("Impersonate-Group")
	for _, group := range k8sGroups {
		r.Header.Add("Impersonate-Group", group)
	}

	logger.Infof("K8SAPI 转发: user=%s, groups=%v, namespace=%s, path=%s",
		peerIdentity.UserName, k8sGroups, namespace, r.URL.Path)

	// 记录审计
	startedAt := time.Now()
	target := fmt.Sprintf("%s %s", r.Method, r.URL.Path)

	// 5. 检查是否为协议升级请求（SPDY/WebSocket，用于 kubectl exec/logs/cp/attach/port-forward）
	if isUpgradeRequest(r) {
		logger.Infof("K8SAPI 协议升级: %s, upgrade=%s", r.URL.Path, r.Header.Get("Upgrade"))
		p.handleUpgrade(w, r, apiServerURL)
		// 记录审计（升级请求）
		if p.auditCollector != nil {
			p.auditCollector.Record(peerIdentity.UserName, "", "k8s_api_request", target, r.Header.Get("Upgrade"), startedAt, time.Now())
		}
		return
	}

	// 6. 普通 HTTP 请求，使用反向代理
	proxy.ServeHTTP(w, r)

	// 记录审计（普通请求）
	if p.auditCollector != nil {
		p.auditCollector.Record(peerIdentity.UserName, "", "k8s_api_request", target, "", startedAt, time.Now())
	}
}

// RemoteAddr 实现从 http.Request 获取远程地址的辅助方法
func (r *remoteAddrWrapper) Network() string { return "tcp" }
func (r *remoteAddrWrapper) String() string  { return r.addr }

type remoteAddrWrapper struct {
	addr string
}

// extractNamespaceFromPath 从 K8S API 路径中提取命名空间
// 路径格式: /api/v1/namespaces/{namespace}/... 或 /apis/{group}/{version}/namespaces/{namespace}/...
func extractNamespaceFromPath(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if part == "namespaces" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return "" // 集群级别请求，无命名空间
}

// isUpgradeRequest 检查是否为协议升级请求（SPDY/WebSocket）
// kubectl exec/attach/port-forward 使用 SPDY，kubectl logs -f 可能使用 chunked 或 WebSocket
func isUpgradeRequest(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Connection"), "Upgrade") ||
		strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}

// handleUpgrade 处理协议升级请求（SPDY/WebSocket 隧道）
// 直接建立到 K8S API Server 的 TCP 连接，双向透传数据
func (p *K8SAPIProxy) handleUpgrade(w http.ResponseWriter, r *http.Request, apiServerURL *url.URL) {
	// 构建到 K8S API Server 的请求 URL
	targetURL := *r.URL
	targetURL.Scheme = apiServerURL.Scheme
	targetURL.Host = apiServerURL.Host

	// 使用 KubeconfigManager 的 rest.Config 创建 TLS 连接
	restConfig := p.kubeconfigMgr.GetRESTConfig()

	// 创建 TLS 配置
	tlsConfig, err := rest.TLSConfigFor(restConfig)
	if err != nil {
		logger.Errorf("K8SAPI 升级: 创建 TLS 配置失败: %v", err)
		http.Error(w, "创建 TLS 配置失败", http.StatusInternalServerError)
		return
	}

	// 建立到 K8S API Server 的 TLS 连接
	dialer := &net.Dialer{
		Timeout: 30 * time.Second,
	}
	backendConn, err := tls.DialWithDialer(dialer, "tcp", apiServerURL.Host, tlsConfig)
	if err != nil {
		logger.Errorf("K8SAPI 升级: 连接后端失败: %v", err)
		http.Error(w, "连接后端失败", http.StatusBadGateway)
		return
	}
	defer backendConn.Close()

	// 修改请求头：设置 Host
	r.Host = apiServerURL.Host
	r.URL = &targetURL
	// 确保请求 URL 使用正确的路径（去掉 scheme 和 host，只保留 path+query）
	r.RequestURI = r.URL.RequestURI()

	// 将原始请求写入后端连接
	if err := r.Write(backendConn); err != nil {
		logger.Errorf("K8SAPI 升级: 写入请求失败: %v", err)
		http.Error(w, "写入请求失败", http.StatusBadGateway)
		return
	}

	// 劫持客户端连接
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		logger.Error("K8SAPI 升级: ResponseWriter 不支持 Hijack")
		http.Error(w, "不支持协议升级", http.StatusInternalServerError)
		return
	}

	clientConn, clientBuf, err := hijacker.Hijack()
	if err != nil {
		logger.Errorf("K8SAPI 升级: Hijack 失败: %v", err)
		return
	}
	defer clientConn.Close()

	// 双向透传数据，等待两个方向都结束
	done := make(chan struct{}, 2)

	// 后端 → 客户端
	go func() {
		io.Copy(clientConn, backendConn)
		// 后端读完，关闭客户端写方向（如果支持半关闭）
		if tc, ok := clientConn.(closeWriter); ok {
			tc.CloseWrite()
		}
		done <- struct{}{}
	}()

	// 客户端 → 后端（先刷 hijack 缓冲区中的残留数据）
	go func() {
		if clientBuf != nil && clientBuf.Reader.Buffered() > 0 {
			io.CopyN(backendConn, clientBuf, int64(clientBuf.Reader.Buffered()))
		}
		io.Copy(backendConn, clientConn)
		// TLS 连接不支持半关闭，直接关闭整个连接
		// 这会导致后端 → 客户端的 goroutine 也结束
		done <- struct{}{}
	}()

	// 等待两个方向都结束
	<-done
	<-done
	logger.Debugf("K8SAPI 升级连接关闭: %s", r.URL.Path)
}

// closeWriter 半关闭写方向接口（仅 TCP 连接支持，TLS 连接不支持）
type closeWriter interface {
	CloseWrite() error
}
