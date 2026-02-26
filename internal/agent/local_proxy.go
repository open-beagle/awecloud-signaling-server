package agent

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"io"
	"math/big"
	"net"
	"sync"
	"time"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

// LocalProxyManager 本地代理管理器
// 为每个域名创建本地监听器（VIP:port），转发到 Tailscale 网络
type LocalProxyManager struct {
	domainCache *DomainCache
	vipAlloc    *VIPAllocator
	tsManager   *TailscaleManager

	// VIP:port -> listener 映射
	listeners map[string]net.Listener
	// VIP:port -> 域名信息映射
	proxyInfo map[string]*pb.DomainInfo

	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.RWMutex
	wg     sync.WaitGroup
}

// NewLocalProxyManager 创建本地代理管理器
func NewLocalProxyManager(domainCache *DomainCache, vipAlloc *VIPAllocator, tsManager *TailscaleManager, ctx context.Context) *LocalProxyManager {
	ctx, cancel := context.WithCancel(ctx)
	return &LocalProxyManager{
		domainCache: domainCache,
		vipAlloc:    vipAlloc,
		tsManager:   tsManager,
		listeners:   make(map[string]net.Listener),
		proxyInfo:   make(map[string]*pb.DomainInfo),
		ctx:         ctx,
		cancel:      cancel,
	}
}

// Start 启动本地代理管理器
func (m *LocalProxyManager) Start() error {
	logger.Info("[LocalProxy] 本地代理管理器已启动")
	return nil
}

// Stop 停止本地代理管理器
func (m *LocalProxyManager) Stop() {
	m.cancel()

	m.mu.Lock()
	// 关闭所有监听器
	for addr, listener := range m.listeners {
		listener.Close()
		logger.Debugf("[LocalProxy] 关闭监听器: %s", addr)
	}
	m.listeners = make(map[string]net.Listener)
	m.proxyInfo = make(map[string]*pb.DomainInfo)
	m.mu.Unlock()

	m.wg.Wait()
	logger.Info("[LocalProxy] 本地代理管理器已停止")
}

// UpdateProxies 更新代理列表（根据域名缓存）
func (m *LocalProxyManager) UpdateProxies() error {
	domains := m.domainCache.List()
	logger.Infof("[LocalProxy] 更新代理列表，共 %d 个域名", len(domains))

	// 构建新的代理映射（VIP:port -> 域名信息）
	newProxies := make(map[string]*pb.DomainInfo)
	for _, domain := range domains {
		// 为域名分配 VIP
		vip, err := m.vipAlloc.Allocate(domain.Domain)
		if err != nil {
			logger.Warnf("[LocalProxy] 分配 VIP 失败: %s, %v", domain.Domain, err)
			continue
		}

		// 根据域名类型决定本地监听端口（用户访问端口）
		// 注意：这里是 Desktop 本地代理监听的端口，不是 target_port
		// target_port 是 Desktop 通过 Tailscale 连接到 Agent 的端口
		var listenPort int32
		switch domain.Type {
		case "ssh":
			// SSH 类型通过 ProxyCommand + DialSocket 连接，不需要本地代理
			// 且 22 端口通常被系统 sshd 占用（监听 0.0.0.0:22），会导致绑定失败
			continue
		case "k8sapi":
			listenPort = 6443 // K8S API Server 标准端口
		case "k8ssvc":
			// K8SSVC 需要为每个 service_port 创建监听
			// 这里暂时使用 target_port，后续需要解析 service_ports JSON 数组
			listenPort = domain.TargetPort
		default:
			listenPort = domain.TargetPort // 其他类型使用 target_port
		}

		// 构建监听地址（VIP:listenPort）
		listenAddr := fmt.Sprintf("%s:%d", vip, listenPort)
		newProxies[listenAddr] = domain
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// 停止不再需要的代理
	for addr := range m.listeners {
		if _, exists := newProxies[addr]; !exists {
			m.stopProxy(addr)
		}
	}

	// 启动新的代理
	for addr, domain := range newProxies {
		if _, exists := m.listeners[addr]; !exists {
			if err := m.startProxy(addr, domain); err != nil {
				logger.Warnf("[LocalProxy] 启动代理失败: %s, %v", addr, err)
			}
		}
	}

	return nil
}

// startProxy 启动单个代理（需要持有锁）
func (m *LocalProxyManager) startProxy(listenAddr string, domain *pb.DomainInfo) error {
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("监听失败: %w", err)
	}

	m.listeners[listenAddr] = listener
	m.proxyInfo[listenAddr] = domain

	logger.Infof("[LocalProxy] 启动代理: %s -> %s:%d (%s)",
		listenAddr, domain.TargetIp, domain.TargetPort, domain.Type)

	// 启动接受连接的协程
	m.wg.Add(1)
	go m.acceptLoop(listener, domain)

	return nil
}

// stopProxy 停止单个代理（需要持有锁）
func (m *LocalProxyManager) stopProxy(listenAddr string) {
	if listener, ok := m.listeners[listenAddr]; ok {
		listener.Close()
		delete(m.listeners, listenAddr)
		delete(m.proxyInfo, listenAddr)
		logger.Infof("[LocalProxy] 停止代理: %s", listenAddr)
	}
}

// acceptLoop 接受连接循环
func (m *LocalProxyManager) acceptLoop(listener net.Listener, domain *pb.DomainInfo) {
	defer m.wg.Done()

	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-m.ctx.Done():
				return
			default:
				// 监听器被关闭
				return
			}
		}

		// 异步处理连接
		go m.handleConn(conn, domain)
	}
}

// handleConn 处理单个连接
func (m *LocalProxyManager) handleConn(clientConn net.Conn, domain *pb.DomainInfo) {
	defer clientConn.Close()

	// 构建目标地址
	var targetAddr string

	switch domain.Type {
	case "ssh":
		// SSH：直接连接到 Tailscale IP:22
		targetAddr = fmt.Sprintf("%s:22", domain.TargetIp)

	case "k8sapi":
		// K8SAPI：连接到 Agent K8SAPI 代理端口（如 100.64.0.19:50050）
		targetAddr = fmt.Sprintf("%s:%d", domain.TargetIp, domain.TargetPort)

	case "k8ssvc":
		// K8SSVC：连接到 Agent SVCProxy gRPC 端口（50051）
		// Desktop 会在首包携带 namespace/service_name 信息
		targetAddr = fmt.Sprintf("%s:%d", domain.TargetIp, domain.TargetPort)

	default:
		logger.Warnf("[LocalProxy] 未知的域名类型: %s", domain.Type)
		return
	}

	// 通过 tsnet 拨号到目标
	ctx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
	defer cancel()

	targetConn, err := m.tsManager.Dial(ctx, "tcp", targetAddr)
	if err != nil {
		logger.Warnf("[LocalProxy] 拨号失败: %s -> %s, %v", domain.Domain, targetAddr, err)
		return
	}
	defer targetConn.Close()

	logger.Infof("[LocalProxy] 连接建立: %s -> %s (%s)",
		clientConn.RemoteAddr(), targetAddr, domain.Type)

	// K8S API 类型需要 TLS 包装
	// Agent 返回 HTTP，Desktop.Pod 本地代理包装成 HTTPS
	if domain.Type == "k8sapi" {
		m.handleK8SAPIConn(clientConn, targetConn, domain.Domain)
	} else {
		// 其他类型直接桥接
		m.bridgeConns(clientConn, targetConn, domain.Domain)
	}
}

// bridgeConns 双向桥接两个连接
func (m *LocalProxyManager) bridgeConns(client, target net.Conn, domain string) {
	var wg sync.WaitGroup
	wg.Add(2)

	// 客户端 -> 目标
	go func() {
		defer wg.Done()
		written, err := io.Copy(target, client)
		if err != nil && err != io.EOF {
			logger.Debugf("[LocalProxy] %s: 客户端->目标 复制失败: %v", domain, err)
		}
		logger.Debugf("[LocalProxy] %s: 客户端->目标 传输完成: %d 字节", domain, written)
		// 关闭目标的写入端，触发对端读取结束
		if tc, ok := target.(*net.TCPConn); ok {
			tc.CloseWrite()
		}
	}()

	// 目标 -> 客户端
	go func() {
		defer wg.Done()
		written, err := io.Copy(client, target)
		if err != nil && err != io.EOF {
			logger.Debugf("[LocalProxy] %s: 目标->客户端 复制失败: %v", domain, err)
		}
		logger.Debugf("[LocalProxy] %s: 目标->客户端 传输完成: %d 字节", domain, written)
		// 关闭客户端的写入端，触发对端读取结束
		if cc, ok := client.(*net.TCPConn); ok {
			cc.CloseWrite()
		}
	}()

	// 等待双向传输完成
	wg.Wait()
	logger.Debugf("[LocalProxy] %s: 连接关闭", domain)
}

// GetStatus 获取代理状态（用于调试）
func (m *LocalProxyManager) GetStatus() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	status := make(map[string]string, len(m.listeners))
	for addr, domain := range m.proxyInfo {
		status[addr] = fmt.Sprintf("%s (%s)", domain.Domain, domain.Type)
	}
	return status
}

// handleK8SAPIConn 处理 K8S API 连接（TLS 包装）
// Agent 返回 HTTP，Desktop.Pod 本地代理包装成 HTTPS
func (m *LocalProxyManager) handleK8SAPIConn(clientConn net.Conn, targetConn net.Conn, domain string) {
	// 生成自签名证书
	cert, err := generateSelfSignedCert(domain)
	if err != nil {
		logger.Errorf("[LocalProxy] %s: 生成自签名证书失败: %v", domain, err)
		return
	}

	// 创建 TLS 配置
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}

	// 将客户端连接包装为 TLS 服务器
	tlsConn := tls.Server(clientConn, tlsConfig)

	// TLS 握手
	if err := tlsConn.Handshake(); err != nil {
		logger.Warnf("[LocalProxy] %s: TLS 握手失败: %v", domain, err)
		return
	}

	logger.Debugf("[LocalProxy] %s: TLS 握手成功", domain)

	// 双向桥接：TLS 客户端 <-> HTTP 后端
	m.bridgeConns(tlsConn, targetConn, domain)
}

// generateSelfSignedCert 生成自签名证书
func generateSelfSignedCert(domain string) (tls.Certificate, error) {
	// 生成 RSA 私钥
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("生成私钥失败: %w", err)
	}

	// 证书模板
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"AWECloud Signaling"},
			CommonName:   domain,
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour), // 1 年有效期
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{domain},
	}

	// 自签名证书
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("创建证书失败: %w", err)
	}

	// 构造 tls.Certificate
	return tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  priv,
	}, nil
}
