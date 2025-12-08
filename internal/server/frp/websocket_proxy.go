package frp

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/constants"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
)

// WebSocketPathProxy WebSocket 路径代理
// 将自定义 path 的 WebSocket 请求转换为 FRP 默认 path 后转发
// 这样可以在不修改 FRP 源码的情况下支持自定义 WebSocket 路径
type WebSocketPathProxy struct {
	listenAddr string // 监听地址，如 "0.0.0.0:7001"
	targetAddr string // FRP Server 地址，如 "127.0.0.1:7000"
	customPath string // 自定义路径，如 "/ws"
	server     *http.Server
}

// NewWebSocketPathProxy 创建 WebSocket 路径代理
// listenAddr: 代理监听地址（对外暴露）
// targetAddr: FRP Server 地址（内部）
// customPath: 自定义 WebSocket 路径
func NewWebSocketPathProxy(listenAddr, targetAddr, customPath string) *WebSocketPathProxy {
	if customPath == "" {
		customPath = constants.DefaultWebSocketPath
	}

	proxy := &WebSocketPathProxy{
		listenAddr: listenAddr,
		targetAddr: targetAddr,
		customPath: customPath,
	}

	mux := http.NewServeMux()
	mux.HandleFunc(customPath, proxy.handleWebSocket)

	proxy.server = &http.Server{
		Addr:              listenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 60 * time.Second,
	}

	return proxy
}

// Start 启动代理
func (p *WebSocketPathProxy) Start() error {
	logger.Infof("WebSocket 路径代理启动: %s%s -> %s%s",
		p.listenAddr, p.customPath, p.targetAddr, constants.FRPDefaultPath)
	return p.server.ListenAndServe()
}

// Stop 停止代理
func (p *WebSocketPathProxy) Stop() error {
	return p.server.Close()
}

// handleWebSocket 处理 WebSocket 请求
func (p *WebSocketPathProxy) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// 检查是否是 WebSocket 升级请求
	if !isWebSocketUpgrade(r) {
		http.Error(w, "Expected WebSocket upgrade", http.StatusBadRequest)
		return
	}

	// 连接到 Tunnel Server
	serverConn, err := net.DialTimeout("tcp", p.targetAddr, 10*time.Second)
	if err != nil {
		logger.Infof("连接 Tunnel Server 失败: %v", err)
		http.Error(w, "Failed to connect to backend", http.StatusBadGateway)
		return
	}
	defer serverConn.Close()

	// 修改请求路径为 FRP 默认路径
	modifiedReq := r.Clone(r.Context())
	modifiedReq.URL.Path = constants.FRPDefaultPath
	modifiedReq.RequestURI = constants.FRPDefaultPath

	// 将修改后的请求发送到 Tunnel Server
	if err := modifiedReq.Write(serverConn); err != nil {
		logger.Infof("发送请求到 Tunnel Server 失败: %v", err)
		http.Error(w, "Failed to forward request", http.StatusBadGateway)
		return
	}

	// 读取 Tunnel Server 的响应
	serverReader := bufio.NewReader(serverConn)
	resp, err := http.ReadResponse(serverReader, modifiedReq)
	if err != nil {
		logger.Infof("读取 Tunnel Server 响应失败: %v", err)
		http.Error(w, "Failed to read backend response", http.StatusBadGateway)
		return
	}

	// 检查是否成功升级到 WebSocket
	if resp.StatusCode != http.StatusSwitchingProtocols {
		logger.Infof("WebSocket 升级失败: %d %s", resp.StatusCode, resp.Status)
		http.Error(w, "Backend refused WebSocket upgrade", http.StatusBadGateway)
		return
	}

	// Hijack 客户端连接
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		logger.Infof("ResponseWriter 不支持 Hijack")
		http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
		return
	}

	clientConn, clientBuf, err := hijacker.Hijack()
	if err != nil {
		logger.Infof("Hijack 客户端连接失败: %v", err)
		return
	}
	defer clientConn.Close()

	// 将 FRP Server 的响应发送给客户端
	if err := resp.Write(clientConn); err != nil {
		logger.Infof("发送响应到客户端失败: %v", err)
		return
	}

	logger.Infof("WebSocket 连接建立: %s%s -> %s%s (客户端: %s)",
		p.listenAddr, p.customPath, p.targetAddr, constants.FRPDefaultPath, r.RemoteAddr)

	// 双向转发数据
	errChan := make(chan error, 2)

	// Client -> Server
	go func() {
		_, err := io.Copy(serverConn, clientBuf)
		errChan <- err
	}()

	// Server -> Client
	go func() {
		_, err := io.Copy(clientConn, serverConn)
		errChan <- err
	}()

	// 等待任一方向出错或关闭
	<-errChan

	logger.Infof("WebSocket 连接关闭: %s", r.RemoteAddr)
}

// isWebSocketUpgrade 检查是否是 WebSocket 升级请求
func isWebSocketUpgrade(r *http.Request) bool {
	return strings.ToLower(r.Header.Get("Upgrade")) == "websocket" &&
		strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}
