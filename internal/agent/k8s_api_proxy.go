package agent

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
)

// K8SAPIProxy K8S API 反向代理
// 在 tsnet 网络上监听，接收 Desktop 的 kubectl 请求，
// 通过 Impersonation 转发到本机 K8S API Server
type K8SAPIProxy struct {
	config    *config.K8SSection
	tsManager *TailscaleManager
	permCache *PermissionCache
	identity  *IdentityExtractor

	apiServerURL *url.URL // K8S API Server 地址
	listener     net.Listener
	httpServer   *http.Server

	ctx    context.Context
	cancel context.CancelFunc
}

// NewK8SAPIProxy 创建 K8S API 代理
func NewK8SAPIProxy(cfg *config.K8SSection, tsManager *TailscaleManager, permCache *PermissionCache, parentCtx context.Context) (*K8SAPIProxy, error) {
	ctx, cancel := context.WithCancel(parentCtx)

	// 创建身份提取器
	identity, err := NewIdentityExtractor(tsManager)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("创建身份提取器失败: %w", err)
	}

	// 解析 K8S API Server 地址
	apiServerURL, err := resolveAPIServerURL(cfg)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("解析 K8S API Server 地址失败: %w", err)
	}

	return &K8SAPIProxy{
		config:       cfg,
		tsManager:    tsManager,
		permCache:    permCache,
		identity:     identity,
		apiServerURL: apiServerURL,
		ctx:          ctx,
		cancel:       cancel,
	}, nil
}

// Start 启动 K8S API 代理
func (p *K8SAPIProxy) Start() error {
	listenAddr := fmt.Sprintf(":%d", p.config.ListenPort)

	// 在 tsnet 网络上监听
	listener, err := p.tsManager.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("tsnet 监听 %s 失败: %w", listenAddr, err)
	}
	p.listener = listener

	// 创建反向代理
	proxy := &httputil.ReverseProxy{
		Director: p.director,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, // Agent 访问本机 K8S API，跳过证书验证
			},
		},
	}

	// 创建 HTTP Server
	p.httpServer = &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p.handleRequest(w, r, proxy)
		}),
	}

	// 启动服务
	go func() {
		logger.Infof("K8S API 代理已启动: tsnet%s -> %s", listenAddr, p.apiServerURL.String())
		if err := p.httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			logger.Errorf("K8S API 代理服务错误: %v", err)
		}
	}()

	return nil
}

// Stop 停止 K8S API 代理
func (p *K8SAPIProxy) Stop() {
	p.cancel()
	if p.httpServer != nil {
		p.httpServer.Close()
	}
	if p.listener != nil {
		p.listener.Close()
	}
	logger.Info("K8S API 代理已停止")
}

// handleRequest 处理请求：身份提取 → 权限检查 → Impersonation → 转发
func (p *K8SAPIProxy) handleRequest(w http.ResponseWriter, r *http.Request, proxy *httputil.ReverseProxy) {
	// 1. 从 tsnet 连接提取对端身份
	peerIdentity, err := p.identity.ExtractFromConn(r.Context(), &remoteAddrWrapper{addr: r.RemoteAddr})
	if err != nil {
		logger.Warnf("K8SAPI 身份提取失败: %v", err)
		http.Error(w, "身份验证失败", http.StatusUnauthorized)
		return
	}

	// 2. 从 URL 路径提取命名空间（如 /api/v1/namespaces/default/pods）
	namespace := extractNamespaceFromPath(r.URL.Path)

	// 3. 查询权限缓存
	k8sGroups, allowed := p.permCache.CheckK8SAccess(peerIdentity.UserName, namespace)
	if !allowed {
		logger.Warnf("K8SAPI 权限拒绝: user=%s, namespace=%s", peerIdentity.UserName, namespace)
		http.Error(w, "权限不足", http.StatusForbidden)
		return
	}

	// 4. 设置 Impersonation 头
	r.Header.Set("Impersonate-User", peerIdentity.UserName)
	// 清除已有的 Impersonate-Group 头，重新设置
	r.Header.Del("Impersonate-Group")
	for _, group := range k8sGroups {
		r.Header.Add("Impersonate-Group", group)
	}

	logger.Debugf("K8SAPI 转发: user=%s, groups=%v, namespace=%s, path=%s",
		peerIdentity.UserName, k8sGroups, namespace, r.URL.Path)

	// 5. 反向代理到 K8S API Server
	proxy.ServeHTTP(w, r)
}

// director 修改请求目标
func (p *K8SAPIProxy) director(req *http.Request) {
	req.URL.Scheme = p.apiServerURL.Scheme
	req.URL.Host = p.apiServerURL.Host
	req.Host = p.apiServerURL.Host
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

// resolveAPIServerURL 解析 K8S API Server 地址
// 优先使用配置的 api_server，否则从 kubeconfig 自动获取
func resolveAPIServerURL(cfg *config.K8SSection) (*url.URL, error) {
	if cfg.APIServer != "" {
		return url.Parse(cfg.APIServer)
	}

	// 从 kubeconfig 读取
	kubeconfigPath := cfg.Kubeconfig
	if kubeconfigPath == "" {
		kubeconfigPath = "~/.kube/config"
	}

	// 展开 ~
	if strings.HasPrefix(kubeconfigPath, "~/") {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("获取用户主目录失败: %w", err)
		}
		kubeconfigPath = filepath.Join(homeDir, kubeconfigPath[2:])
	}

	// 简单解析 kubeconfig 获取 server 地址
	apiServer, err := parseKubeconfigServer(kubeconfigPath)
	if err != nil {
		// 回退到默认地址
		logger.Warnf("从 kubeconfig 解析 API Server 失败: %v，使用默认 https://localhost:6443", err)
		return url.Parse("https://localhost:6443")
	}

	return url.Parse(apiServer)
}

// parseKubeconfigServer 从 kubeconfig 文件中解析当前 context 的 server 地址
// 简化实现：查找 server: 行
func parseKubeconfigServer(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("读取 kubeconfig 失败: %w", err)
	}

	// 简单行扫描查找 server: 字段
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "server:") {
			server := strings.TrimSpace(strings.TrimPrefix(trimmed, "server:"))
			// 去除可能的引号
			server = strings.Trim(server, "\"'")
			if server != "" {
				return server, nil
			}
		}
	}

	return "", fmt.Errorf("kubeconfig 中未找到 server 字段")
}
