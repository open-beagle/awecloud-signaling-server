// Package agent 提供 Agent 端功能
// endpoint_k8sapi.go 实现 Endpoint K8S API 代理（结构化 gRPC 方案）
// Agent 作为 HTTP Server 接收 Desktop 请求，解析 HTTP 后通过 gRPC 发送结构化请求到 Endpoint
//
// 架构：
//
//	Desktop → TLS → Desktop（TLS 终止）→ Tailscale → Agent(FallbackTCPHandler, 端口 50153+N, http.Server 解析 HTTP)
//	→ Agent 提取身份（WhoIs）+ 检查权限（PermCache）+ 注入身份信息
//	→ gRPC 发送结构化请求（method/path/headers/body + user_name + k8s_groups）到 Endpoint
//	→ Endpoint 使用 client-go 构造新请求（Impersonate 头 + kubeconfig 证书）→ K8S API Server
//
//	每个 Endpoint 分配一个独立端口（50153 起），Agent 根据端口号确定目标 Endpoint
//	Desktop 端无需修改，Server ResolveDomain 返回 agent_ip:分配端口 即可
package agent

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"sync"
	"time"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

// EndpointK8SAPIPortBase Endpoint K8S API 代理的起始端口
const EndpointK8SAPIPortBase = 50153

// EndpointK8SAPIProxy Endpoint K8S API 代理
// 在 Agent 内部运行 HTTP Server，接收 Desktop HTTP 连接（Desktop 已做 TLS 终止）
// 通过 gRPC OpenK8SAPIProxy 发送结构化 HTTP 请求到 Endpoint
type EndpointK8SAPIProxy struct {
	endpointServer *EndpointServer
	tsManager      *TailscaleManager
	permCache      *PermissionCache
	auditCollector *AuditCollector

	// 端口 → Endpoint 名称映射
	portMap map[uint16]string
	// Endpoint 名称 → 端口映射
	nameMap map[string]uint16
	mapMu   sync.RWMutex

	// 下一个可分配的端口
	nextPort uint16

	// fallback handler 取消注册函数
	deregisterFallback func()

	ctx    context.Context
	cancel context.CancelFunc
}

// NewEndpointK8SAPIProxy 创建 Endpoint K8S API 代理
func NewEndpointK8SAPIProxy(endpointServer *EndpointServer, tsManager *TailscaleManager, permCache *PermissionCache, auditCollector *AuditCollector, parentCtx context.Context) *EndpointK8SAPIProxy {
	ctx, cancel := context.WithCancel(parentCtx)
	return &EndpointK8SAPIProxy{
		endpointServer: endpointServer,
		tsManager:      tsManager,
		permCache:      permCache,
		auditCollector: auditCollector,
		portMap:        make(map[uint16]string),
		nameMap:        make(map[string]uint16),
		nextPort:       EndpointK8SAPIPortBase,
		ctx:            ctx,
		cancel:         cancel,
	}
}

// Start 启动 Endpoint K8S API 代理
func (p *EndpointK8SAPIProxy) Start() error {
	// 注册 fallback TCP handler，拦截 Endpoint K8SAPI 端口范围的连接
	deregister := p.tsManager.RegisterFallbackTCPHandler(func(src, dst netip.AddrPort) (func(net.Conn), bool) {
		port := dst.Port()
		if port >= EndpointK8SAPIPortBase && port < EndpointK8SAPIPortBase+100 {
			p.mapMu.RLock()
			endpointName, exists := p.portMap[port]
			p.mapMu.RUnlock()

			if !exists {
				return nil, false
			}

			return func(conn net.Conn) {
				go p.handleConn(conn, endpointName)
			}, true
		}
		return nil, false
	})
	p.deregisterFallback = deregister

	logger.Infof("[EndpointK8SAPI] 代理已启动（FallbackTCPHandler + HTTP Server）: 端口范围 %d-%d", EndpointK8SAPIPortBase, EndpointK8SAPIPortBase+99)
	return nil
}

// Stop 停止 Endpoint K8S API 代理
func (p *EndpointK8SAPIProxy) Stop() {
	p.cancel()
	if p.deregisterFallback != nil {
		p.deregisterFallback()
	}
	logger.Info("[EndpointK8SAPI] 代理已停止")
}

// AllocatePort 为 Endpoint 分配端口
func (p *EndpointK8SAPIProxy) AllocatePort(endpointName string) uint16 {
	p.mapMu.Lock()
	defer p.mapMu.Unlock()

	if port, exists := p.nameMap[endpointName]; exists {
		return port
	}

	port := p.nextPort
	p.nextPort++
	p.portMap[port] = endpointName
	p.nameMap[endpointName] = port

	logger.Infof("[EndpointK8SAPI] 分配端口: %s → %d", endpointName, port)
	return port
}

// AllocateSpecificPort 为 Endpoint 分配指定端口（Server 预分配）
// 如果端口已被占用（且不是同一个 Endpoint），返回错误
func (p *EndpointK8SAPIProxy) AllocateSpecificPort(endpointName string, port uint16) error {
	p.mapMu.Lock()
	defer p.mapMu.Unlock()

	// 检查端口是否已被其他 Endpoint 占用
	if existingName, exists := p.portMap[port]; exists && existingName != endpointName {
		return fmt.Errorf("端口 %d 已被 %s 占用", port, existingName)
	}

	// 检查该 Endpoint 是否已分配了不同的端口
	if existingPort, exists := p.nameMap[endpointName]; exists && existingPort != port {
		// 释放旧端口
		delete(p.portMap, existingPort)
		logger.Infof("[EndpointK8SAPI] 释放旧端口: %s ← %d", endpointName, existingPort)
	}

	// 分配端口
	p.portMap[port] = endpointName
	p.nameMap[endpointName] = port
	logger.Infof("[EndpointK8SAPI] 分配指定端口: %s → %d", endpointName, port)
	return nil
}

// ReleasePort 释放 Endpoint 的端口
func (p *EndpointK8SAPIProxy) ReleasePort(endpointName string) {
	p.mapMu.Lock()
	defer p.mapMu.Unlock()

	if port, exists := p.nameMap[endpointName]; exists {
		delete(p.portMap, port)
		delete(p.nameMap, endpointName)
		logger.Infof("[EndpointK8SAPI] 释放端口: %s ← %d", endpointName, port)
	}
}

// GetPort 获取 Endpoint 的分配端口（0 表示未分配）
func (p *EndpointK8SAPIProxy) GetPort(endpointName string) uint16 {
	p.mapMu.RLock()
	defer p.mapMu.RUnlock()
	return p.nameMap[endpointName]
}

// handleConn 处理 Desktop TCP 连接（HTTP Server 解析 HTTP 请求）
// Desktop 发送明文 HTTP 请求（已在 Desktop 端做 TLS 终止）到 Agent 分配的端口
// Agent 解析 HTTP 请求，通过 gRPC 发送结构化请求到 Endpoint
func (p *EndpointK8SAPIProxy) handleConn(conn net.Conn, endpointName string) {
	defer conn.Close()

	// 提取 Desktop 用户身份
	clientUserName := ""
	if p.tsManager != nil {
		if lc, err := p.tsManager.LocalClient(); err == nil {
			if whois, err := lc.WhoIs(p.ctx, conn.RemoteAddr().String()); err == nil && whois.UserProfile != nil {
				clientUserName, _ = parseHeadscaleUserName(whois.UserProfile.LoginName)
			}
		}
	}

	// 检查 Endpoint 是否在线
	if !p.endpointServer.IsEndpointConnected(endpointName) {
		logger.Warnf("[EndpointK8SAPI] Endpoint 不在线: %s", endpointName)
		return
	}

	// 创建单连接 listener，用于 http.Server
	connListener := newSingleConnListener(conn)

	// 创建 HTTP Server
	httpServer := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p.handleHTTPRequest(w, r, endpointName, clientUserName)
		}),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	// 在连接上运行 HTTP Server
	if err := httpServer.Serve(connListener); err != nil && err != http.ErrServerClosed {
		logger.Debugf("[EndpointK8SAPI] HTTP Server 结束: endpoint=%s, err=%v", endpointName, err)
	}
}

// handleHTTPRequest 处理单个 HTTP 请求：提取身份 → 检查权限 → 发送结构化请求到 Endpoint
func (p *EndpointK8SAPIProxy) handleHTTPRequest(w http.ResponseWriter, r *http.Request, endpointName string, clientUserName string) {
	startedAt := time.Now()

	logger.Infof("[EndpointK8SAPI] 收到请求: endpoint=%s, client=%s, method=%s, path=%s",
		endpointName, clientUserName, r.Method, r.URL.Path)

	// 检查是否需要协议升级
	upgradeHeader := r.Header.Get("Upgrade")
	if upgradeHeader != "" {
		// 协议升级请求，使用字节流透传模式
		logger.Infof("[EndpointK8SAPI] 检测到协议升级请求: upgrade=%s", upgradeHeader)
		p.handleProtocolUpgrade(w, r, endpointName, clientUserName, upgradeHeader)
		return
	}

	// 普通 HTTP 请求，使用当前的结构化消息模式
	// 检查权限，获取 Impersonation 分组
	var k8sGroups []string
	if p.permCache != nil && clientUserName != "" {
		// P11 重构：直接使用 Agent 级别权限检查（不再区分 Endpoint）
		groups, allowed := p.permCache.CheckK8SAccess(clientUserName, "")
		if !allowed {
			logger.Warnf("[EndpointK8SAPI] 权限拒绝: user=%s, endpoint=%s", clientUserName, endpointName)
			http.Error(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","message":"权限不足","reason":"Forbidden","code":403}`, http.StatusForbidden)
			return
		}
		k8sGroups = groups
	}

	logger.Infof("[EndpointK8SAPI] 权限检查通过: user=%s, groups=%v", clientUserName, k8sGroups)

	// 读取请求体
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		logger.Warnf("[EndpointK8SAPI] 读取请求体失败: %v", err)
		http.Error(w, "读取请求体失败", http.StatusBadRequest)
		return
	}
	r.Body.Close()

	// 构造结构化 HTTP 请求
	httpReq := &pb.K8SAPIHTTPRequest{
		Method: r.Method,
		Path:   r.URL.RequestURI(), // 包含 query string
		Headers: make(map[string]string),
		Body:    bodyBytes,
	}

	// 只传递必要的请求头（避免传递过多无用头）
	necessaryHeaders := []string{
		"Accept", "Content-Type", "User-Agent",
		"Kubectl-Session", "Kubectl-Command",
	}
	for _, key := range necessaryHeaders {
		if val := r.Header.Get(key); val != "" {
			httpReq.Headers[key] = val
		}
	}

	// 请求 Endpoint 开启 K8S API 代理（通过 gRPC，传递用户身份和分组）
	stream, err := p.endpointServer.RequestK8SAPIProxy(p.ctx, endpointName, clientUserName, k8sGroups)
	if err != nil {
		logger.Warnf("[EndpointK8SAPI] 请求 Endpoint K8S API 代理失败: %v", err)
		http.Error(w, "连接 Endpoint 失败", http.StatusBadGateway)
		return
	}

	// 发送结构化 HTTP 请求到 Endpoint
	if err := stream.Send(&pb.K8SAPIProxyData{
		HttpRequest: httpReq,
	}); err != nil {
		logger.Warnf("[EndpointK8SAPI] 发送 HTTP 请求失败: %v", err)
		http.Error(w, "发送请求失败", http.StatusBadGateway)
		return
	}

	logger.Debugf("[EndpointK8SAPI] 已发送结构化请求: method=%s, path=%s", httpReq.Method, httpReq.Path)

	// 接收 Endpoint 的响应（可能是流式的）
	var statusCode int
	headersSent := false

	for {
		respMsg, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				logger.Debugf("[EndpointK8SAPI] 响应流结束")
				break
			}
			logger.Warnf("[EndpointK8SAPI] 接收响应失败: %v", err)
			if !headersSent {
				http.Error(w, "接收响应失败", http.StatusBadGateway)
			}
			return
		}

		// 检查关闭标志
		if respMsg.IsClose {
			logger.Debugf("[EndpointK8SAPI] Endpoint 关闭流")
			break
		}

		// 检查错误
		if respMsg.Error != "" {
			logger.Warnf("[EndpointK8SAPI] Endpoint 返回错误: %s", respMsg.Error)
			if !headersSent {
				http.Error(w, fmt.Sprintf("Endpoint 错误: %s", respMsg.Error), http.StatusBadGateway)
			}
			return
		}

		// 检查是否有结构化响应
		if respMsg.HttpResponse == nil {
			continue
		}

		httpResp := respMsg.HttpResponse

		// 第一次收到响应时，写入响应头和状态码
		if !headersSent {
			statusCode = int(httpResp.StatusCode)
			logger.Infof("[EndpointK8SAPI] 收到响应: status=%d", statusCode)

			// 写入响应头
			for key, val := range httpResp.Headers {
				w.Header().Set(key, val)
			}

			// 写入状态码
			w.WriteHeader(statusCode)
			headersSent = true
		}

		// 写入响应体（可能是分块的）
		if len(httpResp.Body) > 0 {
			if _, err := w.Write(httpResp.Body); err != nil {
				logger.Debugf("[EndpointK8SAPI] 写入响应体失败: %v", err)
				return
			}

			// Flush 数据到客户端（对于流式响应很重要）
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		}
	}

	// 关闭 gRPC 流
	_ = stream.Send(&pb.K8SAPIProxyData{IsClose: true})

	logger.Debugf("[EndpointK8SAPI] 请求完成: endpoint=%s, client=%s, status=%d",
		endpointName, clientUserName, statusCode)

	// 记录审计
	if p.auditCollector != nil {
		target := fmt.Sprintf("%s %s", r.Method, r.URL.Path)
		p.auditCollector.Record(clientUserName, endpointName, "k8s_api_request", target, "", startedAt, time.Now())
	}
}

// singleConnListener 单连接 listener，用于将一个 net.Conn 交给 http.Server
type singleConnListener struct {
	conn   net.Conn
	connCh chan net.Conn
	closed bool
}

// newSingleConnListener 创建单连接 listener
func newSingleConnListener(conn net.Conn) *singleConnListener {
	l := &singleConnListener{
		conn:   conn,
		connCh: make(chan net.Conn, 1),
	}
	l.connCh <- conn
	return l
}

// Accept 返回连接（只返回一次，之后阻塞直到关闭）
func (l *singleConnListener) Accept() (net.Conn, error) {
	conn, ok := <-l.connCh
	if !ok {
		return nil, fmt.Errorf("listener 已关闭")
	}
	return conn, nil
}

// Close 关闭 listener
func (l *singleConnListener) Close() error {
	if !l.closed {
		l.closed = true
		close(l.connCh)
	}
	return nil
}

// Addr 返回地址
func (l *singleConnListener) Addr() net.Addr {
	return l.conn.LocalAddr()
}

// handleProtocolUpgrade 处理协议升级请求（kubectl logs/exec/cp/port-forward）
// 使用原始字节流透传模式
func (p *EndpointK8SAPIProxy) handleProtocolUpgrade(w http.ResponseWriter, r *http.Request, endpointName string, clientUserName string, upgradeProto string) {
	logger.Infof("[EndpointK8SAPI] 协议升级请求: endpoint=%s, client=%s, upgrade=%s, path=%s",
		endpointName, clientUserName, upgradeProto, r.URL.Path)

	// 1. 检查权限
	var k8sGroups []string
	if p.permCache != nil && clientUserName != "" {
		// P11 重构：直接使用 Agent 级别权限检查（不再区分 Endpoint）
		groups, allowed := p.permCache.CheckK8SAccess(clientUserName, "")
		if !allowed {
			logger.Warnf("[EndpointK8SAPI] 协议升级权限拒绝: user=%s, endpoint=%s", clientUserName, endpointName)
			http.Error(w, "权限不足", http.StatusForbidden)
			return
		}
		k8sGroups = groups
	}

	logger.Infof("[EndpointK8SAPI] 协议升级权限检查通过: user=%s, groups=%v", clientUserName, k8sGroups)

	// 2. Hijack HTTP 连接
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		logger.Warnf("[EndpointK8SAPI] 无法 Hijack 连接")
		http.Error(w, "不支持协议升级", http.StatusInternalServerError)
		return
	}

	conn, bufrw, err := hijacker.Hijack()
	if err != nil {
		logger.Warnf("[EndpointK8SAPI] Hijack 失败: %v", err)
		http.Error(w, "Hijack 失败", http.StatusInternalServerError)
		return
	}
	defer conn.Close()

	logger.Debugf("[EndpointK8SAPI] 连接已 Hijack: endpoint=%s, user=%s", endpointName, clientUserName)

	// 3. 请求 Endpoint 开启原始字节流
	stream, err := p.endpointServer.RequestRawStream(p.ctx, endpointName, clientUserName, k8sGroups)
	if err != nil {
		logger.Warnf("[EndpointK8SAPI] 请求 Endpoint 原始流失败: %v", err)
		return
	}

	logger.Infof("[EndpointK8SAPI] 原始流已建立: endpoint=%s, user=%s", endpointName, clientUserName)

	// 4. 构造原始 HTTP 请求（包含所有头和请求行）
	var reqBuf []byte
	reqLine := fmt.Sprintf("%s %s %s\r\n", r.Method, r.URL.RequestURI(), r.Proto)
	reqBuf = append(reqBuf, []byte(reqLine)...)

	// 写入所有请求头
	for key, vals := range r.Header {
		for _, val := range vals {
			headerLine := fmt.Sprintf("%s: %s\r\n", key, val)
			reqBuf = append(reqBuf, []byte(headerLine)...)
		}
	}
	reqBuf = append(reqBuf, []byte("\r\n")...)

	// 如果有请求体，也要发送（对于 POST/PUT 请求）
	if r.Body != nil {
		bodyBytes, err := io.ReadAll(r.Body)
		if err == nil && len(bodyBytes) > 0 {
			reqBuf = append(reqBuf, bodyBytes...)
		}
		r.Body.Close()
	}

	// 5. 发送原始请求到 Endpoint
	if err := stream.Send(&pb.RawStreamData{
		Data: reqBuf,
	}); err != nil {
		logger.Warnf("[EndpointK8SAPI] 发送原始请求失败: %v", err)
		return
	}

	logger.Debugf("[EndpointK8SAPI] 已发送原始请求: size=%d bytes", len(reqBuf))

	// 6. 双向复制字节流
	errChan := make(chan error, 2)

	// Desktop → Endpoint
	go func() {
		buf := make([]byte, 32*1024)
		for {
			// 从 bufrw 读取（可能有缓冲数据）
			n, err := bufrw.Read(buf)
			if n > 0 {
				if sendErr := stream.Send(&pb.RawStreamData{Data: buf[:n]}); sendErr != nil {
					errChan <- fmt.Errorf("发送到 Endpoint 失败: %w", sendErr)
					return
				}
			}
			if err != nil {
				if err != io.EOF {
					logger.Debugf("[EndpointK8SAPI] 从 Desktop 读取结束: %v", err)
				}
				// 发送关闭标志
				_ = stream.Send(&pb.RawStreamData{IsClose: true})
				errChan <- nil
				return
			}
		}
	}()

	// Endpoint → Desktop
	go func() {
		for {
			msg, err := stream.Recv()
			if err != nil {
				if err != io.EOF {
					logger.Debugf("[EndpointK8SAPI] 从 Endpoint 接收结束: %v", err)
				}
				errChan <- nil
				return
			}

			if msg.IsClose {
				logger.Debugf("[EndpointK8SAPI] Endpoint 关闭流")
				errChan <- nil
				return
			}

			if msg.Error != "" {
				logger.Warnf("[EndpointK8SAPI] Endpoint 错误: %s", msg.Error)
				errChan <- fmt.Errorf("Endpoint 错误: %s", msg.Error)
				return
			}

			if len(msg.Data) > 0 {
				if _, writeErr := conn.Write(msg.Data); writeErr != nil {
					errChan <- fmt.Errorf("写入到 Desktop 失败: %w", writeErr)
					return
				}
			}
		}
	}()

	// 等待任一方向出错或完成
	err = <-errChan
	if err != nil {
		logger.Debugf("[EndpointK8SAPI] 协议升级流结束: %v", err)
	}

	logger.Infof("[EndpointK8SAPI] 协议升级流已关闭: endpoint=%s, user=%s, path=%s", endpointName, clientUserName, r.URL.Path)

	// 记录审计日志
	// TODO: 实现 RecordK8SAPIAccess 方法
	// if p.auditCollector != nil {
	// 	p.auditCollector.RecordK8SAPIAccess(clientUserName, endpointName, r.Method, r.URL.Path, 0, time.Since(time.Now()))
	// }
}
