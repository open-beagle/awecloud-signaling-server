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

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

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

	// 调试：打印所有域名的类型和 service_ports
	for _, domain := range domains {
		logger.Infof("[LocalProxy] 域名 %s type='%s' service_ports=%v (len=%d)",
			domain.Domain, domain.Type, domain.ServicePorts, len(domain.ServicePorts))
	}

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
		switch domain.Type {
		case "ssh":
			// SSH 类型：监听 VIP:22
			listenAddr := fmt.Sprintf("%s:22", vip)
			newProxies[listenAddr] = domain
		case "k8sapi":
			// K8S API Server 标准端口
			listenAddr := fmt.Sprintf("%s:6443", vip)
			newProxies[listenAddr] = domain
		case "k8ssvc":
			// K8SSVC 需要为每个 service_port 创建监听
			logger.Debugf("[LocalProxy] K8SSVC 域名 %s service_ports=%v", domain.Domain, domain.ServicePorts)
			if len(domain.ServicePorts) == 0 {
				logger.Warnf("[LocalProxy] K8SSVC 域名 %s 没有 service_ports，跳过", domain.Domain)
				continue
			}
			// 为每个 service_port 创建独立的监听
			for _, port := range domain.ServicePorts {
				listenAddr := fmt.Sprintf("%s:%d", vip, port)
				// 创建域名副本，并设置当前端口（用于连接时识别）
				domainCopy := &pb.DomainInfo{
					Domain:       domain.Domain,
					Type:         domain.Type,
					TargetIp:     domain.TargetIp,
					TargetPort:   port, // 临时存储当前服务端口，用于 handleConn 识别
					ClusterName:  domain.ClusterName,
					Namespace:    domain.Namespace,
					ServiceName:  domain.ServiceName,
					Status:       domain.Status,
					ServicePorts: domain.ServicePorts,
				}
				newProxies[listenAddr] = domainCopy
			}
		default:
			// 其他类型使用 target_port
			listenAddr := fmt.Sprintf("%s:%d", vip, domain.TargetPort)
			newProxies[listenAddr] = domain
		}
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

	// 根据类型输出不同的日志
	if domain.Type == "k8ssvc" {
		// K8SSVC 类型：显示服务名称和端口
		logger.Infof("[LocalProxy] 启动代理: %s -> %s:%d (%s, %s.%s:%d)",
			listenAddr, domain.TargetIp, 50051, domain.Type, domain.ServiceName, domain.Namespace, domain.TargetPort)
	} else {
		logger.Infof("[LocalProxy] 启动代理: %s -> %s:%d (%s)",
			listenAddr, domain.TargetIp, domain.TargetPort, domain.Type)
	}

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
		// SSH：连接到 Endpoint SSH 代理端口（如 100.64.0.22:50053）
		targetAddr = fmt.Sprintf("%s:%d", domain.TargetIp, domain.TargetPort)

	case "k8sapi":
		// K8SAPI：连接到 Agent K8SAPI 代理端口（如 100.64.0.19:50050）
		targetAddr = fmt.Sprintf("%s:%d", domain.TargetIp, domain.TargetPort)

	case "k8ssvc":
		// K8SSVC：需要 gRPC 协议包装
		m.handleK8SSVCConn(clientConn, domain)
		return

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

// handleK8SSVCConn 处理 K8S Service 连接（gRPC 包装）
// Desktop.Pod 本地代理通过 gRPC 连接到 Agent SVCProxy 服务
func (m *LocalProxyManager) handleK8SSVCConn(clientConn net.Conn, domain *pb.DomainInfo) {
	defer clientConn.Close()

	// 1. 建立到 Agent SVCProxy 的 TCP 连接
	targetAddr := fmt.Sprintf("%s:50051", domain.TargetIp)
	ctx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
	defer cancel()

	targetConn, err := m.tsManager.Dial(ctx, "tcp", targetAddr)
	if err != nil {
		logger.Warnf("[LocalProxy] K8SSVC 拨号失败: %s -> %s, %v", domain.Domain, targetAddr, err)
		return
	}
	defer targetConn.Close()

	logger.Infof("[LocalProxy] K8SSVC 连接建立: %s -> %s (%s.%s:%d)",
		clientConn.RemoteAddr(), targetAddr, domain.ServiceName, domain.Namespace, domain.TargetPort)

	// 2. 创建 gRPC 客户端（使用已建立的 TCP 连接）
	grpcConn, err := newGRPCClientConn(targetConn)
	if err != nil {
		logger.Errorf("[LocalProxy] K8SSVC 创建 gRPC 客户端失败: %v", err)
		return
	}
	defer grpcConn.Close()

	client := pb.NewAgentServiceClient(grpcConn)

	// 3. 调用 SVCProxy 方法，获取双向流
	stream, err := client.SVCProxy(ctx)
	if err != nil {
		logger.Errorf("[LocalProxy] K8SSVC 调用 SVCProxy 失败: %v", err)
		return
	}

	// 4. 发送首包（连接参数）
	// P10 重构：不再发送 endpoint_name，由 neimeng Agent 自动选择实现路径
	err = stream.Send(&pb.SVCProxyData{
		Namespace:   domain.Namespace,
		ServiceName: domain.ServiceName,
		Port:        domain.TargetPort,
		IsConnect:   true,
	})
	if err != nil {
		logger.Errorf("[LocalProxy] K8SSVC 发送首包失败: %v", err)
		return
	}

	logger.Debugf("[LocalProxy] K8SSVC 首包已发送: %s.%s:%d", domain.ServiceName, domain.Namespace, domain.TargetPort)

	// 5. 桥接客户端连接和 gRPC 流
	m.bridgeK8SSVCStream(clientConn, stream, domain)
}

// bridgeK8SSVCStream 桥接客户端连接和 gRPC 流
func (m *LocalProxyManager) bridgeK8SSVCStream(clientConn net.Conn, stream pb.AgentService_SVCProxyClient, domain *pb.DomainInfo) {
	var wg sync.WaitGroup
	wg.Add(2)

	// 客户端 -> gRPC 流
	go func() {
		defer wg.Done()
		buffer := make([]byte, 32*1024)
		totalBytes := int64(0)
		for {
			n, err := clientConn.Read(buffer)
			if n > 0 {
				totalBytes += int64(n)
				sendErr := stream.Send(&pb.SVCProxyData{
					Data: buffer[:n],
				})
				if sendErr != nil {
					logger.Debugf("[LocalProxy] K8SSVC %s: 发送数据失败: %v", domain.Domain, sendErr)
					return
				}
			}
			if err != nil {
				if err != io.EOF {
					logger.Debugf("[LocalProxy] K8SSVC %s: 客户端读取失败: %v", domain.Domain, err)
				}
				// 发送关闭通知
				stream.Send(&pb.SVCProxyData{IsClose: true})
				logger.Debugf("[LocalProxy] K8SSVC %s: 客户端->gRPC 传输完成: %d 字节", domain.Domain, totalBytes)
				return
			}
		}
	}()

	// gRPC 流 -> 客户端
	go func() {
		defer wg.Done()
		totalBytes := int64(0)
		for {
			resp, err := stream.Recv()
			if err != nil {
				if err != io.EOF {
					logger.Debugf("[LocalProxy] K8SSVC %s: gRPC 接收失败: %v", domain.Domain, err)
				}
				logger.Debugf("[LocalProxy] K8SSVC %s: gRPC->客户端 传输完成: %d 字节", domain.Domain, totalBytes)
				return
			}

			// 检查错误
			if resp.Error != "" {
				logger.Warnf("[LocalProxy] K8SSVC %s: 收到错误: %s", domain.Domain, resp.Error)
				return
			}

			// 检查关闭通知
			if resp.IsClose {
				logger.Debugf("[LocalProxy] K8SSVC %s: 收到关闭通知", domain.Domain)
				return
			}

			// 写入数据到客户端
			if len(resp.Data) > 0 {
				totalBytes += int64(len(resp.Data))
				_, writeErr := clientConn.Write(resp.Data)
				if writeErr != nil {
					logger.Debugf("[LocalProxy] K8SSVC %s: 客户端写入失败: %v", domain.Domain, writeErr)
					return
				}
			}
		}
	}()

	// 等待双向传输完成
	wg.Wait()
	logger.Debugf("[LocalProxy] K8SSVC %s: 连接关闭", domain.Domain)
}

// newGRPCClientConn 基于已建立的 TCP 连接创建 gRPC 客户端连接
func newGRPCClientConn(conn net.Conn) (*grpc.ClientConn, error) {
	// 使用 grpc.WithContextDialer 将已建立的连接包装为 gRPC 连接
	return grpc.NewClient(
		"passthrough:///agent", // 目标地址（实际不使用，因为连接已建立）
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return conn, nil
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
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
