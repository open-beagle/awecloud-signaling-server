package agent

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/telemetry"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

// Agent Agent进程
type Agent struct {
	config  *config.AgentConfig
	version string // Agent版本

	// gRPC连接
	grpcConn      *grpc.ClientConn
	grpcClient    pb.AgentServiceClient
	grpcConnected bool
	grpcMutex     sync.RWMutex

	// Agent信息
	agentID uint64

	// Tailscale 管理
	tsManager      *TailscaleManager
	proxyManager   *ProxyManager
	visitorManager *VisitorManager
	tailscaleIP    string

	// K8S 能力（P1 新增）
	permCache   *PermissionCache    // 权限缓存
	k8sAPIProxy *K8SAPIProxy        // K8S API 反向代理
	svcInformer *K8SServiceInformer // K8S Service Informer
	svcProxy    *K8SSVCProxy        // K8S Service gRPC 代理

	// Endpoint 能力（P2 新增）
	endpointServer      *EndpointServer      // Endpoint 内网 gRPC Server
	endpointSSHProxy    *EndpointSSHProxy    // Endpoint SSH 代理（tsnet 端口）
	endpointK8SAPIProxy *EndpointK8SAPIProxy // Endpoint K8SAPI 代理（tsnet 端口）

	// 审计收集器（P2-3.9）
	auditCollector *AuditCollector

	// 网络信息
	networkInfo *NetworkInfo
	lanDetector *LANDetector

	// 配置版本（用于增量同步）
	configVersion int64

	// 域名后缀（从 Server 心跳响应获取）
	domainSuffix string

	// 上下文
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewAgent 创建Agent
func NewAgent(cfg *config.AgentConfig, version string) (*Agent, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// 初始化网络检测器
	lanDetector := NewLANDetector()
	networkInfo := lanDetector.DetectNetworkInfo()
	logger.Infof("检测到网络信息: IP=%s, 网关=%s, 网卡=%s, 环境=%s, 主机名=%s",
		networkInfo.LanIP, networkInfo.LanGateway, networkInfo.LanInterface,
		networkInfo.RuntimeEnv, networkInfo.Hostname)

	return &Agent{
		config:         cfg,
		version:        version,
		lanDetector:    lanDetector,
		networkInfo:    networkInfo,
		auditCollector: NewAuditCollector(),
		ctx:            ctx,
		cancel:         cancel,
	}, nil
}

// Run 运行Agent（Agent 模式：完整的 gRPC 注册 + 心跳 + ProxyManager + VisitorManager）
func (a *Agent) Run() error {
	// 启动健康检查HTTP服务器
	if err := a.startHealthServer(); err != nil {
		return fmt.Errorf("启动健康检查服务器失败: %w", err)
	}

	// 连接到Server
	if err := a.connectToServer(); err != nil {
		return fmt.Errorf("连接Server失败: %w", err)
	}
	defer a.grpcConn.Close()

	// 注册Agent
	if err := a.register(); err != nil {
		return fmt.Errorf("注册失败: %w", err)
	}

	// 初始化权限缓存（K8S 模块共用）
	a.permCache = NewPermissionCache()

	// 启动 K8S API 代理（如果配置启用）
	if a.config.K8S.Enabled {
		if err := a.startK8SAPIProxy(); err != nil {
			logger.Warnf("启动 K8S API 代理失败: %v", err)
		}
	}

	// 启动 K8S Service 发现和代理（如果配置启用）
	if a.config.SVC.Enabled {
		if err := a.startK8SServiceModules(); err != nil {
			logger.Warnf("启动 K8S Service 模块失败: %v", err)
		}
	}

	// 启动 tsnet 诊断（异步，不阻塞启动流程）
	if a.tsManager != nil {
		go func() {
			// 等待 3 秒让 listener 完全就绪
			time.Sleep(3 * time.Second)
			var diagPorts []int
			if a.config.K8S.Enabled && a.k8sAPIProxy != nil {
				diagPorts = append(diagPorts, a.config.K8S.ListenPort)
			}
			if a.config.SVC.Enabled && a.svcProxy != nil {
				diagPorts = append(diagPorts, a.config.SVC.ListenPortBase)
			}
			if len(diagPorts) > 0 {
				a.tsManager.DiagnoseTsnet(diagPorts)
			}
		}()
	}

	// 启动心跳流（双向流，用于同步配置）
	a.wg.Add(1)
	go a.heartbeatLoop()

	// 等待中断信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("正在关闭Agent...")
	a.cancel()

	// 停止 K8S 模块
	if a.k8sAPIProxy != nil {
		a.k8sAPIProxy.Stop()
	}
	if a.svcProxy != nil {
		a.svcProxy.Stop()
	}
	if a.svcInformer != nil {
		a.svcInformer.Stop()
	}

	// 停止 Endpoint K8SAPI 代理
	if a.endpointK8SAPIProxy != nil {
		a.endpointK8SAPIProxy.Stop()
	}

	// 停止 Endpoint SSH 代理
	if a.endpointSSHProxy != nil {
		a.endpointSSHProxy.Stop()
	}

	// 停止 Endpoint Server
	if a.endpointServer != nil {
		a.endpointServer.Stop()
	}

	// 停止 Tailscale
	if a.tsManager != nil {
		a.tsManager.Stop()
	}

	// 停止 ProxyManager
	if a.proxyManager != nil {
		a.proxyManager.StopAll()
	}

	// 停止 VisitorManager
	if a.visitorManager != nil {
		a.visitorManager.StopAll()
	}

	a.wg.Wait()

	logger.Info("Agent已关闭")
	return nil
}

// RunClient 运行 Client 模式（CloudIDE 等场景）
// 启动 tsnet 连接网络 + SSH + gRPC 心跳（精简版，不需要 ProxyManager/VisitorManager）
func (a *Agent) RunClient(regResult *config.RegisterResult) error {
	// 启动健康检查HTTP服务器
	if err := a.startHealthServer(); err != nil {
		return fmt.Errorf("启动健康检查服务器失败: %w", err)
	}

	// 检查注册结果
	if regResult.HeadscaleURL == "" || regResult.AuthKey == "" {
		return fmt.Errorf("注册结果缺少 Headscale 认证信息")
	}

	logger.Infof("Client 模式启动，用户: %s (ID: %d)", regResult.UserName, regResult.UserID)

	// 启动 Tailscale（仅网络连接）
	a.tsManager = NewTailscaleManager(a.config, nil, 0, "", a.ctx)

	if err := a.tsManager.Start(regResult.HeadscaleURL, regResult.AuthKey); err != nil {
		return fmt.Errorf("启动 Tailscale 失败: %w", err)
	}

	a.tailscaleIP = a.tsManager.GetIP()
	logger.Infof("Tailscale 已连接，IP: %s", a.tailscaleIP)

	// 初始化域名缓存
	domainCache := NewDomainCache()

	// 初始化 VIP 分配器
	vipAlloc := NewVIPAllocator()

	// 连接 gRPC Server（用于心跳，让 Server 创建/更新 Node 记录）
	if err := a.connectToServer(); err != nil {
		logger.Warnf("连接 gRPC Server 失败: %v（Client 将无法在设备管理中显示）", err)
	} else {
		// 使用 HTTP 注册返回的 UserID 作为 agentID
		a.agentID = regResult.UserID
		// 启动精简心跳（只上报状态，不处理配置同步）
		a.wg.Add(1)
		go a.heartbeatLoop()

		// 启动域名同步（定期从 Server 获取可访问的域名列表）
		a.wg.Add(1)
		go a.syncDomainsLoop(domainCache)
	}

	// 启动 DNS 劫持（如果配置启用）
	var dnsServer *DNSServer
	var proxyManager *LocalProxyManager
	if a.config.CloudIDE.DNSEnabled {
		// 检测上游 DNS（从 /etc/resolv.conf 读取）
		upstreamDNS := detectUpstreamDNS()
		logger.Infof("检测到上游 DNS: %s", upstreamDNS)

		// 启动本地 DNS 服务器
		dnsServer = NewDNSServer("127.0.0.1:15353", domainCache, vipAlloc, upstreamDNS, a.ctx)
		if err := dnsServer.Start(); err != nil {
			logger.Warnf("启动 DNS 服务器失败: %v（需要 root 权限）", err)
		} else {
			// 修改 /etc/resolv.conf
			if err := modifyResolvConf(upstreamDNS); err != nil {
				logger.Warnf("修改 /etc/resolv.conf 失败: %v", err)
			}
			defer func() {
				// 恢复 /etc/resolv.conf
				if err := restoreResolvConf(); err != nil {
					logger.Warnf("恢复 /etc/resolv.conf 失败: %v", err)
				}
			}()

			// 启动本地代理管理器
			proxyManager = NewLocalProxyManager(domainCache, vipAlloc, a.tsManager, a.ctx)
			if err := proxyManager.Start(); err != nil {
				logger.Warnf("启动本地代理管理器失败: %v", err)
			} else {
				// 启动代理更新循环
				a.wg.Add(1)
				go a.updateProxiesLoop(proxyManager, domainCache)
			}
		}
	}

	// 启动 Dial Socket 服务（供 dial 子命令使用）
	dialSocketPath := a.config.CloudIDE.DialSocket
	if dialSocketPath == "" {
		dialSocketPath = "/tmp/signaling.sock"
	}
	dialSocket := NewDialSocketServer(dialSocketPath, a.tsManager)
	if err := dialSocket.Start(); err != nil {
		logger.Warnf("启动 Dial Socket 失败: %v", err)
	} else {
		defer dialSocket.Stop()
	}

	// 自动维护 ~/.ssh/config（如果配置启用）
	if a.config.CloudIDE.SSHConfig {
		if err := MaintainSSHConfig(dialSocketPath); err != nil {
			logger.Warnf("维护 ~/.ssh/config 失败: %v", err)
		}
	}

	// 等待中断信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("正在关闭 Client...")
	a.cancel()

	// 停止本地代理管理器
	if proxyManager != nil {
		proxyManager.Stop()
	}

	// 停止 DNS 服务器
	if dnsServer != nil {
		dnsServer.Stop()
	}

	// 停止 Tailscale
	if a.tsManager != nil {
		a.tsManager.Stop()
	}

	// 关闭 gRPC 连接
	if a.grpcConn != nil {
		a.grpcConn.Close()
	}

	a.wg.Wait()

	logger.Info("Client 已关闭")
	return nil
}

// connectToServer 连接到Server
func (a *Agent) connectToServer() error {
	serverAddr := a.config.Server.Address

	// 如果没有协议前缀，添加默认的 http://
	if !strings.Contains(serverAddr, "://") {
		serverAddr = "http://" + serverAddr
	}

	parsedURL, err := url.Parse(serverAddr)
	if err != nil {
		return fmt.Errorf("解析Server地址失败: %w", err)
	}

	// 构建gRPC连接地址（host:port）
	var grpcAddr string
	if parsedURL.Port() != "" {
		grpcAddr = parsedURL.Host
	} else {
		port := 80
		if parsedURL.Scheme == "https" {
			port = 443
		}
		grpcAddr = fmt.Sprintf("%s:%d", parsedURL.Hostname(), port)
	}

	logger.Debugf("连接到Server gRPC: %s (scheme: %s)", grpcAddr, parsedURL.Scheme)

	// 根据协议选择传输凭证
	var opts []grpc.DialOption
	if parsedURL.Scheme == "https" {
		tlsConfig := &tls.Config{
			InsecureSkipVerify: true,
			ServerName:         parsedURL.Hostname(),
			// 必须设置 NextProtos 以支持 HTTP/2（gRPC 要求）
			NextProtos: []string{"h2"},
		}
		creds := credentials.NewTLS(tlsConfig)
		opts = append(opts, grpc.WithTransportCredentials(creds))
		logger.Debug("gRPC使用TLS连接（跳过证书验证）")
	} else {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
		logger.Debug("gRPC使用明文连接")
	}

	// 添加 OpenTelemetry gRPC 客户端追踪（带限流）
	clientOpts := []otelgrpc.Option{}
	if filter := telemetry.GetGRPCLimiterFilter(); filter != nil {
		clientOpts = append(clientOpts, otelgrpc.WithFilter(filter))
		logger.Debug("gRPC OpenTelemetry 追踪已启用（带限流）")
	} else {
		logger.Debug("gRPC OpenTelemetry 追踪已启用")
	}
	opts = append(opts, grpc.WithStatsHandler(otelgrpc.NewClientHandler(clientOpts...)))

	// 创建gRPC连接
	conn, err := grpc.NewClient(grpcAddr, opts...)
	if err != nil {
		return err
	}

	a.grpcConn = conn
	a.grpcClient = pb.NewAgentServiceClient(conn)

	// 标记gRPC已连接
	a.grpcMutex.Lock()
	a.grpcConnected = true
	a.grpcMutex.Unlock()

	logger.Debug("gRPC连接建立成功")
	return nil
}

// register 注册Agent
func (a *Agent) register() error {
	logger.Infof("注册Agent: %s (version: %s)", a.config.Agent.AgentName, a.version)

	// 构建系统信息
	// 如果配置了 device，使用 device 作为 hostname；否则使用自动检测的 hostname
	hostname := a.config.Agent.Device
	if hostname == "" && a.networkInfo != nil {
		hostname = a.networkInfo.Hostname
	}

	var systemInfo *pb.SystemInfo
	if hostname != "" {
		systemInfo = &pb.SystemInfo{
			Hostname: hostname,
		}
	}

	resp, err := a.grpcClient.Register(a.ctx, &pb.AgentRegisterRequest{
		Name:       a.config.Agent.AgentName,
		Secret:     a.config.Agent.AgentToken,
		Version:    a.version,
		SystemInfo: systemInfo,
	})

	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("注册失败: %s", resp.Message)
	}

	a.agentID = resp.AgentId

	// 检查是否返回了 Tailscale 认证信息
	if resp.ServerUrl != "" && resp.AuthKey != "" {
		logger.Infof("注册成功，Agent ID: %d，使用 Tailscale 模式", a.agentID)

		// 启动 Tailscale
		if a.tsManager == nil {
			a.tsManager = NewTailscaleManager(a.config, a.grpcClient, a.agentID, a.config.Agent.AgentToken, a.ctx)
		}

		if err := a.tsManager.Start(resp.ServerUrl, resp.AuthKey); err != nil {
			return fmt.Errorf("启动 Tailscale 失败: %w", err)
		}

		a.tailscaleIP = a.tsManager.GetIP()
		logger.Infof("Tailscale 已连接，IP: %s", a.tailscaleIP)

		// 初始化 ProxyManager
		if a.proxyManager == nil {
			a.proxyManager = NewProxyManager(a.tsManager, a.grpcClient, a.agentID, a.ctx)
		}

		// 初始化 VisitorManager
		if a.visitorManager == nil {
			a.visitorManager = NewVisitorManager(a.tsManager, a.config, a.grpcClient, a.agentID, a.ctx)
		}

		// VPN 就绪，通知 ProxyManager 启动等待中的服务
		a.proxyManager.OnVPNReady()
	} else {
		return fmt.Errorf("Server 未返回 Tailscale 认证信息")
	}

	return nil
}

// heartbeatLoop 心跳循环（双向流，支持自动重连）
func (a *Agent) heartbeatLoop() {
	defer a.wg.Done()

	retryDelay := 5 * time.Second
	maxRetryDelay := 60 * time.Second

	for {
		select {
		case <-a.ctx.Done():
			logger.Debug("心跳线程退出")
			return
		default:
		}

		logger.Infof("建立心跳流...")

		stream, err := a.grpcClient.Heartbeat(a.ctx)
		if err != nil {
			logger.Infof("建立心跳流失败: %v，%v后重试", err, retryDelay)

			select {
			case <-time.After(retryDelay):
				retryDelay *= 2
				if retryDelay > maxRetryDelay {
					retryDelay = maxRetryDelay
				}
				continue
			case <-a.ctx.Done():
				logger.Info("心跳线程退出")
				return
			}
		}

		retryDelay = 5 * time.Second
		logger.Infof("心跳流已建立")

		// 心跳循环
		a.runHeartbeatStream(stream)

		select {
		case <-a.ctx.Done():
			logger.Info("心跳线程退出")
			return
		default:
			logger.Infof("心跳流已断开，%v后重新连接", retryDelay)
		}

		select {
		case <-time.After(retryDelay):
		case <-a.ctx.Done():
			logger.Info("心跳线程退出")
			return
		}
	}
}

// runHeartbeatStream 运行心跳流
func (a *Agent) runHeartbeatStream(stream pb.AgentService_HeartbeatClient) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// 发送初始心跳
	if err := a.sendHeartbeat(stream); err != nil {
		logger.Warnf("发送初始心跳失败: %v", err)
		return
	}

	// 立即上报通知通道
	immediateReport := make(chan struct{}, 1)

	// 启动接收协程
	recvDone := make(chan struct{})
	go func() {
		defer close(recvDone)
		for {
			resp, err := stream.Recv()
			if err != nil {
				logger.Infof("接收心跳响应失败: %v", err)
				return
			}

			// 处理配置更新
			a.handleHeartbeatResponse(resp)

			// 检查是否需要立即上报
			if resp.RequestImmediateReport {
				logger.Infof("收到 Server 立即上报请求，准备发送心跳")
				select {
				case immediateReport <- struct{}{}:
				default:
				}
			}
		}
	}()

	// 定时发送心跳
	for {
		select {
		case <-ticker.C:
			if err := a.sendHeartbeat(stream); err != nil {
				logger.Warnf("发送心跳失败: %v", err)
				return
			}

		case <-immediateReport:
			if err := a.sendHeartbeat(stream); err != nil {
				logger.Warnf("发送立即上报心跳失败: %v", err)
				return
			}
			logger.Infof("立即上报心跳已发送")

		case <-recvDone:
			logger.Debug("心跳接收协程退出")
			return

		case <-a.ctx.Done():
			return
		}
	}
}

// sendHeartbeat 发送心跳
func (a *Agent) sendHeartbeat(stream pb.AgentService_HeartbeatClient) error {
	req := &pb.AgentHeartbeatRequest{
		AgentId: a.agentID,
	}

	// 添加 Tailscale 状态
	if a.tsManager != nil {
		req.TunnelIp = a.tsManager.GetIP()
		req.TunnelConnected = a.tsManager.IsConnected()
	}

	// 添加网络信息
	// 如果配置了 device，使用 device 作为 hostname
	hostname := a.config.Agent.Device
	if hostname == "" && a.networkInfo != nil {
		hostname = a.networkInfo.Hostname
	}
	if hostname != "" {
		req.Hostname = hostname
	}
	if a.networkInfo != nil {
		req.Runtime = a.networkInfo.RuntimeEnv
		// 添加网络接口列表
		if a.networkInfo.LanIP != "" && a.networkInfo.LanIP != "127.0.0.1" {
			req.Networks = append(req.Networks, &pb.NetworkInterface{
				Name:    a.networkInfo.LanInterface,
				Ip:      a.networkInfo.LanIP,
				Mask:    a.networkInfo.LanMask,
				Gateway: a.networkInfo.LanGateway,
			})
		}
	}

	// 添加服务状态
	if a.proxyManager != nil {
		for name, running := range a.proxyManager.GetStatus() {
			req.ServiceStatus = append(req.ServiceStatus, &pb.ServiceStatus{
				ServiceId: name,
				Running:   running,
			})
		}
	}

	// 添加端口访问状态
	if a.visitorManager != nil {
		for name, running := range a.visitorManager.GetStatus() {
			req.ForwardStatus = append(req.ForwardStatus, &pb.ForwardStatus{
				ForwardId: name,
				Running:   running,
			})
		}
	}

	// 添加 K8S Service 发现数据
	if a.svcInformer != nil {
		for _, svc := range a.svcInformer.GetDiscoveredServices() {
			ds := &pb.DiscoveredK8SService{
				Namespace:   svc.Namespace,
				ServiceName: svc.Name,
				ClusterIp:   svc.ClusterIP,
				Labels:      svc.Labels,
			}
			for _, p := range svc.Ports {
				ds.Ports = append(ds.Ports, &pb.ServicePort{
					Name:     p.Name,
					Port:     p.Port,
					Protocol: p.Protocol,
				})
			}
			req.DiscoveredServices = append(req.DiscoveredServices, ds)
		}
	}

	// 构建域名注册列表
	req.DomainRegistrations = a.buildDomainRegistrations()

	// 添加已连接的 Endpoint 列表（包含配置信息，供 Server 存储）
	if a.endpointServer != nil {
		for _, ep := range a.endpointServer.GetConnectedEndpointDetails() {
			connEp := &pb.ConnectedEndpoint{
				Name:               ep.Name,
				Status:             "online",
				DiscoveredServices: ep.DiscoveredServices,
				// SSH 配置
				SshUsers: ep.SSHUsers,
				// K8S API 配置
				K8SapiApiServer: ep.K8SAPIApiServer,
				// K8S Service 配置
				K8SserviceLabelSelector: ep.K8SServiceLabelSelector,
				K8SserviceNamespaces:    ep.K8SServiceNamespaces,
			}
			req.ConnectedEndpoints = append(req.ConnectedEndpoints, connEp)
		}
	}

	// 上报操作审计记录
	if a.auditCollector != nil {
		req.AuditRecords = a.auditCollector.Flush()
	}

	return stream.Send(req)
}

// handleHeartbeatResponse 处理心跳响应（配置同步）
func (a *Agent) handleHeartbeatResponse(resp *pb.AgentHeartbeatResponse) {
	// 更新域名后缀
	if resp.DomainSuffix != "" {
		a.domainSuffix = resp.DomainSuffix
	}

	// 同步 K8S 权限到本地缓存（每次心跳都更新，不受 configVersion 控制）
	// 注意：必须无条件调用 Update，空列表表示权限已全部撤销，需要清空缓存
	if a.permCache != nil {
		a.permCache.UpdateK8SPermissions(resp.K8SPermissions)
		a.permCache.UpdateK8SServicePermissions(resp.K8SServicePermissions)
		a.permCache.UpdateEndpointSSHPermissions(resp.EndpointSshPermissions)
		a.permCache.UpdateEndpointK8SAPIPermissions(resp.EndpointK8SapiPermissions)
		a.permCache.UpdateEndpointK8SServicePermissions(resp.EndpointK8SservicePermissions)
	}

	// 同步 Endpoint 能力配置到 EndpointServer（让 Agent 能把 Server 配置下发给 Endpoint）
	if a.endpointServer != nil {
		a.syncEndpointServerConfigs(resp)
	}

	// 处理 Server 远程能力配置（每次心跳都检查，不受 configVersion 控制）
	if resp.CapabilityConfig != nil {
		a.applyCapabilityConfig(resp.CapabilityConfig)
	}

	// 检查配置版本
	if resp.ConfigVersion <= a.configVersion {
		return
	}

	logger.Infof("收到配置更新，版本: %d -> %d", a.configVersion, resp.ConfigVersion)
	a.configVersion = resp.ConfigVersion

	// 同步端口映射服务
	if len(resp.Services) > 0 {
		a.syncServices(resp.Services)
	}

	// 同步端口访问服务
	if len(resp.Forwards) > 0 {
		a.syncForwards(resp.Forwards)
	}
}

// syncEndpointServerConfigs 将 Server 下发的 Endpoint 能力配置同步到 EndpointServer
// 这样 EndpointServer 在心跳响应里就能把配置下发给 Endpoint 进程
func (a *Agent) syncEndpointServerConfigs(resp *pb.AgentHeartbeatResponse) {
	// 直接从 EndpointCapabilityConfigs 读取（Server 直接下发每个 Endpoint 的 enabled 状态）
	logger.Infof("收到 Server 下发的 Endpoint 配置，共 %d 个", len(resp.EndpointCapabilityConfigs))
	for _, cfg := range resp.EndpointCapabilityConfigs {
		serverCfg := &EndpointServerConfig{
			SSHEnabled:       cfg.SshEnabled,
			SSHEnabledSet:    true,
			K8SAPIEnabled:    cfg.K8SapiEnabled,
			K8SAPIEnabledSet: true,
			K8SAPIApiServer:  cfg.K8SapiApiServer,
			K8SSvcEnabled:    cfg.K8SserviceEnabled,
			K8SSvcEnabledSet: true,
		}
		a.endpointServer.UpdateServerConfig(cfg.EndpointName, serverCfg)
		logger.Infof("同步 Endpoint 配置: name=%s, ssh=%v, k8sapi=%v, api_server=%s, k8ssvc=%v",
			cfg.EndpointName, serverCfg.SSHEnabled, serverCfg.K8SAPIEnabled, serverCfg.K8SAPIApiServer, serverCfg.K8SSvcEnabled)
	}
}

// syncServices 同步端口映射服务
func (a *Agent) syncServices(services []*pb.ServiceConfig) {
	if a.proxyManager == nil {
		logger.Warn("ProxyManager 未初始化，跳过服务同步")
		return
	}

	// 构建配置列表
	configs := make([]ServiceConfig, 0, len(services))
	for _, svc := range services {
		configs = append(configs, ServiceConfig{
			ID:         svc.Id,
			Name:       svc.Name,
			SourceAddr: svc.SourceAddr,
			TargetAddr: svc.TargetAddr,
			Enabled:    svc.Enabled,
		})
	}

	// 使用 UpdateConfig 批量更新
	a.proxyManager.UpdateConfig(configs)
}

// syncForwards 同步端口访问服务
func (a *Agent) syncForwards(forwards []*pb.ForwardConfig) {
	if a.visitorManager == nil {
		logger.Warn("VisitorManager 未初始化，跳过端口访问同步")
		return
	}

	// 构建配置列表
	configs := make([]VisitorConfig, 0, len(forwards))
	for _, fwd := range forwards {
		configs = append(configs, VisitorConfig{
			ID:          fwd.Id,
			ServiceID:   fwd.ServiceId,
			ServiceName: fwd.ServiceName,
			SourceAddr:  fwd.SourceAddr,
			TargetAddr:  fwd.TargetAddr,
			Enabled:     fwd.Enabled,
		})
	}

	// 使用 UpdateConfig 批量更新
	a.visitorManager.UpdateConfig(configs)
}

// IsGRPCConnected 检查gRPC连接是否正常
func (a *Agent) IsGRPCConnected() bool {
	a.grpcMutex.RLock()
	defer a.grpcMutex.RUnlock()
	return a.grpcConnected && a.grpcConn != nil
}

// IsTailscaleConnected 检查 Tailscale 连接状态
func (a *Agent) IsTailscaleConnected() bool {
	if a.tsManager == nil {
		return false
	}
	return a.tsManager.IsConnected()
}

// GetTailscaleIP 获取 Tailscale IP
func (a *Agent) GetTailscaleIP() string {
	if a.tsManager == nil {
		return ""
	}
	return a.tsManager.GetIP()
}

// GetProxyCount 获取运行中的代理数量
func (a *Agent) GetProxyCount() int {
	if a.proxyManager == nil {
		return 0
	}
	return a.proxyManager.Count()
}

// GetVisitorCount 获取运行中的 Visitor 数量
func (a *Agent) GetVisitorCount() int {
	if a.visitorManager == nil {
		return 0
	}
	return a.visitorManager.Count()
}

// GetVisitorLANIP 获取 Visitor 监听的局域网 IP
func (a *Agent) GetVisitorLANIP() string {
	if a.visitorManager == nil {
		return ""
	}
	return a.visitorManager.GetLANIP()
}

// buildDomainRegistrations 构建域名注册列表
// 根据 Agent 当前配置和状态，生成需要上报给 Server 的域名列表
func (a *Agent) buildDomainRegistrations() []*pb.DomainRegistration {
	// 域名后缀尚未从 Server 获取，跳过注册
	if a.domainSuffix == "" {
		return nil
	}

	var registrations []*pb.DomainRegistration

	// 获取设备名（优先使用配置的 device，否则使用自动检测的 hostname）
	device := a.config.Agent.Device
	if device == "" && a.networkInfo != nil {
		device = a.networkInfo.Hostname
	}

	// 获取 Agent 的 Tailscale IP（用于域名注册的目标 IP）
	agentIP := ""
	if a.tsManager != nil {
		agentIP = a.tsManager.GetIP()
	}

	// 1. Agent SSH 域名：{device}.{agent_name}{domain_suffix}（如 beagle-242.beijing.beagle）
	if a.config.Tunnel.EnableSSH && a.tsManager != nil && a.tsManager.IsConnected() && device != "" {
		registrations = append(registrations, &pb.DomainRegistration{
			Domain:     device + "." + a.config.Agent.AgentName + a.domainSuffix,
			Type:       "ssh",
			TargetIp:   agentIP,
			TargetPort: 22,
		})
	}

	// 2. Agent 端口映射服务域名：从 ProxyManager 获取运行中的服务
	if a.proxyManager != nil {
		for name, running := range a.proxyManager.GetStatus() {
			if running {
				registrations = append(registrations, &pb.DomainRegistration{
					Domain:   name + "." + a.config.Agent.AgentName + a.domainSuffix,
					Type:     "k8ssvc",
					TargetIp: agentIP,
				})
			}
		}
	}

	// 3. K8S API 域名：api.{agent_name}{domain_suffix}:50050
	if a.config.K8S.Enabled && a.k8sAPIProxy != nil && a.tsManager != nil && a.tsManager.IsConnected() {
		registrations = append(registrations, &pb.DomainRegistration{
			Domain:     "kubernetes." + a.config.Agent.AgentName + a.domainSuffix,
			Type:       "k8sapi",
			TargetIp:   agentIP,
			TargetPort: int32(a.config.K8S.ListenPort),
		})
	}

	// 4. K8S Service 域名：{service}.{namespace}.{agent_name}{domain_suffix}
	if a.config.SVC.Enabled && a.svcInformer != nil && a.tsManager != nil && a.tsManager.IsConnected() {
		for _, svc := range a.svcInformer.GetDiscoveredServices() {
			// 使用 alias 或 service name 作为域名前缀
			prefix := svc.Name
			if svc.Alias != "" {
				prefix = svc.Alias
			}
			domain := prefix + "." + svc.Namespace + "." + a.config.Agent.AgentName + a.domainSuffix

			// 注册第一个端口的域名，TargetPort 使用 K8S Service 的实际端口（如 5432）
			// Agent SVCProxy gRPC 端口（50051）由 Server 在 ResolveDomain 响应中通过 svc_proxy_port 字段返回
			if len(svc.Ports) > 0 {
				registrations = append(registrations, &pb.DomainRegistration{
					Domain:      domain,
					Type:        "k8ssvc",
					TargetIp:    agentIP,
					TargetPort:  svc.Ports[0].Port, // K8S Service 实际端口
					Namespace:   svc.Namespace,
					ServiceName: svc.Name,
				})
			}
		}
	}

	// 5. Endpoint SSH 域名：{endpoint-name}.{agent-name}{domain_suffix}:分配端口
	if a.endpointServer != nil && a.endpointSSHProxy != nil && a.tsManager != nil && a.tsManager.IsConnected() {
		for _, ep := range a.endpointServer.GetConnectedEndpointDetails() {
			// 检查 Endpoint 是否有 SSH 能力
			hasSSH := false
			for _, cap := range ep.Capabilities {
				if cap.Type == "ssh" {
					hasSSH = true
					break
				}
			}
			if !hasSSH {
				continue
			}

			// 为 Endpoint 分配端口（幂等，已分配则返回已有端口）
			port := a.endpointSSHProxy.AllocatePort(ep.Name)

			registrations = append(registrations, &pb.DomainRegistration{
				Domain:     ep.Name + "." + a.config.Agent.AgentName + a.domainSuffix,
				Type:       "ssh",
				TargetIp:   agentIP,
				TargetPort: int32(port),
				EndpointId: ep.Name,
			})
		}
	}

	// 6. Endpoint K8SAPI 域名：{endpoint-name}.{agent-name}{domain_suffix}:分配端口
	// 注意：Endpoint 不创建 K8S API 域名，K8S API 域名只属于 Agent Node
	// Endpoint 的 K8S API 访问通过 Endpoint 自身的能力配置和端口分配实现
	// 这里注释掉，避免创建错误的域名格式
	/*
		if a.endpointServer != nil && a.endpointK8SAPIProxy != nil && a.tsManager != nil && a.tsManager.IsConnected() {
			for _, ep := range a.endpointServer.GetConnectedEndpointDetails() {
				hasK8SAPI := false
				for _, cap := range ep.Capabilities {
					if cap.Type == "k8sapi" {
						hasK8SAPI = true
						break
					}
				}
				if !hasK8SAPI {
					continue
				}

				port := a.endpointK8SAPIProxy.AllocatePort(ep.Name)

				registrations = append(registrations, &pb.DomainRegistration{
					Domain:     "kubernetes." + ep.Name + "." + a.config.Agent.AgentName + a.domainSuffix,
					Type:       "k8sapi",
					TargetIp:   agentIP,
					TargetPort: int32(port),
					EndpointId: ep.Name,
				})
			}
		}
	*/

	// 7. Endpoint K8SService 域名：{service}.{namespace}.{endpoint-name}.{agent-name}{domain_suffix}
	// Endpoint K8SService 通过 SVCProxy endpoint_name 字段路由，不需要独立端口
	// 使用 Agent SVCProxy gRPC 端口（50051），Desktop 首包携带 endpoint_name
	if a.endpointServer != nil && a.svcProxy != nil && a.tsManager != nil && a.tsManager.IsConnected() {
		for _, ep := range a.endpointServer.GetConnectedEndpointDetails() {
			// 检查 Endpoint 是否有 K8SService 能力
			hasK8SService := false
			for _, cap := range ep.Capabilities {
				if cap.Type == "k8sservice" {
					hasK8SService = true
					break
				}
			}
			if !hasK8SService {
				continue
			}

			// 遍历 Endpoint 发现的 K8S Service
			for _, svc := range ep.DiscoveredServices {
				if len(svc.Ports) == 0 {
					continue
				}
				domain := svc.ServiceName + "." + svc.Namespace + "." + ep.Name + "." + a.config.Agent.AgentName + a.domainSuffix
				registrations = append(registrations, &pb.DomainRegistration{
					Domain:      domain,
					Type:        "k8ssvc",
					TargetIp:    agentIP,
					TargetPort:  svc.Ports[0].Port,
					Namespace:   svc.Namespace,
					ServiceName: svc.ServiceName,
					EndpointId:  ep.Name,
				})
			}
		}
	}

	return registrations
}

// startK8SAPIProxy 启动 K8S API 代理
func (a *Agent) startK8SAPIProxy() error {
	proxy, err := NewK8SAPIProxy(&a.config.K8S, a.tsManager, a.permCache, a.auditCollector, a.ctx)
	if err != nil {
		return fmt.Errorf("创建 K8S API 代理失败: %w", err)
	}

	if err := proxy.Start(); err != nil {
		return fmt.Errorf("启动 K8S API 代理失败: %w", err)
	}

	a.k8sAPIProxy = proxy
	logger.Infof("K8S API 代理已启用: 端口 %d", a.config.K8S.ListenPort)
	return nil
}

// startK8SServiceModules 启动 K8S Service 发现和代理模块
func (a *Agent) startK8SServiceModules() error {
	// 启动 Informer
	informer, err := NewK8SServiceInformer(&a.config.SVC, nil, a.ctx)
	if err != nil {
		return fmt.Errorf("创建 K8S Service Informer 失败: %w", err)
	}

	if err := informer.Start(); err != nil {
		return fmt.Errorf("启动 K8S Service Informer 失败: %w", err)
	}
	a.svcInformer = informer

	// 启动 SVCProxy gRPC 服务
	svcProxy, err := NewK8SSVCProxy(&a.config.SVC, a.tsManager, a.permCache, informer, a.auditCollector, a.ctx)
	if err != nil {
		return fmt.Errorf("创建 K8S SVCProxy 失败: %w", err)
	}

	if err := svcProxy.Start(); err != nil {
		return fmt.Errorf("启动 K8S SVCProxy 失败: %w", err)
	}
	a.svcProxy = svcProxy

	logger.Infof("K8S Service 模块已启用: selector=%s, gRPC 端口=%d",
		a.config.SVC.LabelSelector, a.config.SVC.ListenPortBase)
	return nil
}

// startHealthServer 启动健康检查HTTP服务器
func (a *Agent) startHealthServer() error {
	gin.SetMode(gin.ReleaseMode)

	router := gin.New()
	router.Use(gin.Recovery())

	healthAPI := NewHealthAPI(a)

	router.GET("/health", healthAPI.Health)
	router.GET("/health/ready", healthAPI.Ready)

	healthPort := 8090
	if a.config.Health.Port > 0 {
		healthPort = a.config.Health.Port
	}

	addr := fmt.Sprintf(":%d", healthPort)

	srv := &http.Server{
		Addr:    addr,
		Handler: router,
	}

	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		logger.Infof("健康检查服务器启动在: http://0.0.0.0%s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Infof("健康检查服务器错误: %v", err)
		}
	}()

	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		<-a.ctx.Done()
		logger.Info("正在关闭健康检查服务器...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Infof("健康检查服务器关闭错误: %v", err)
		} else {
			logger.Info("健康检查服务器已关闭")
		}
	}()

	return nil
}

// applyCapabilityConfig 应用 Server 远程能力配置
// 根据 xxx_set 标志位决定是否使用 Server 下发的值，否则使用本地配置
func (a *Agent) applyCapabilityConfig(cap *pb.AgentCapabilityConfig) {
	// === SSH ===
	if cap.SshEnabledSet {
		oldSSH := a.config.Tunnel.EnableSSH
		if cap.SshEnabled != oldSSH {
			a.config.Tunnel.EnableSSH = cap.SshEnabled
			logger.Infof("远程配置: SSH %s -> %s", boolStr(oldSSH), boolStr(cap.SshEnabled))
			// 动态开关 SSH
			if a.tsManager != nil {
				if cap.SshEnabled {
					if err := a.tsManager.EnableSSHRemote(); err != nil {
						logger.Warnf("远程启用 SSH 失败: %v", err)
					}
				} else {
					if err := a.tsManager.DisableSSHRemote(); err != nil {
						logger.Warnf("远程禁用 SSH 失败: %v", err)
					}
				}
			}
		}
	}

	// === K8S API ===
	if cap.K8SEnabledSet {
		oldEnabled := a.config.K8S.Enabled
		needRestart := false

		// 检查开关变化
		if cap.K8SEnabled != oldEnabled {
			a.config.K8S.Enabled = cap.K8SEnabled
			logger.Infof("远程配置: K8S API %s -> %s", boolStr(oldEnabled), boolStr(cap.K8SEnabled))
			needRestart = true
		}

		// 检查参数变化（仅在启用时有意义）
		if cap.K8SEnabled {
			if cap.K8SListenPortSet && int(cap.K8SListenPort) != a.config.K8S.ListenPort {
				logger.Infof("远程配置: K8S ListenPort %d -> %d", a.config.K8S.ListenPort, cap.K8SListenPort)
				a.config.K8S.ListenPort = int(cap.K8SListenPort)
				needRestart = true
			}
			if cap.K8SApiServerSet && cap.K8SApiServer != a.config.K8S.APIServer {
				logger.Infof("远程配置: K8S APIServer %s -> %s", a.config.K8S.APIServer, cap.K8SApiServer)
				a.config.K8S.APIServer = cap.K8SApiServer
				needRestart = true
			}
		}

		if needRestart {
			a.applyK8SAPIConfig()
		}
	}

	// === K8S Service ===
	if cap.SvcEnabledSet {
		oldEnabled := a.config.SVC.Enabled
		needRestart := false

		// 检查开关变化
		if cap.SvcEnabled != oldEnabled {
			a.config.SVC.Enabled = cap.SvcEnabled
			logger.Infof("远程配置: K8S Service %s -> %s", boolStr(oldEnabled), boolStr(cap.SvcEnabled))
			needRestart = true
		}

		// 检查参数变化（仅在启用时有意义）
		if cap.SvcEnabled {
			if cap.SvcLabelSelectorSet && cap.SvcLabelSelector != a.config.SVC.LabelSelector {
				logger.Infof("远程配置: SVC LabelSelector %s -> %s", a.config.SVC.LabelSelector, cap.SvcLabelSelector)
				a.config.SVC.LabelSelector = cap.SvcLabelSelector
				needRestart = true
			}
			if cap.SvcNamespacesSet {
				newNS := parseNamespaces(cap.SvcNamespaces)
				if !stringSliceEqual(newNS, a.config.SVC.Namespaces) {
					logger.Infof("远程配置: SVC Namespaces %v -> %v", a.config.SVC.Namespaces, newNS)
					a.config.SVC.Namespaces = newNS
					needRestart = true
				}
			}
			if cap.SvcListenPortBaseSet && int(cap.SvcListenPortBase) != a.config.SVC.ListenPortBase {
				logger.Infof("远程配置: SVC ListenPortBase %d -> %d", a.config.SVC.ListenPortBase, cap.SvcListenPortBase)
				a.config.SVC.ListenPortBase = int(cap.SvcListenPortBase)
				needRestart = true
			}
		}

		if needRestart {
			a.applySVCConfig()
		}
	}

	// === Endpoint ===
	if cap.EndpointEnabledSet {
		a.applyEndpointConfig(cap)
	}
}

// applyK8SAPIConfig 应用 K8S API 配置变更（启停模块）
func (a *Agent) applyK8SAPIConfig() {
	// 先停止现有模块
	if a.k8sAPIProxy != nil {
		a.k8sAPIProxy.Stop()
		a.k8sAPIProxy = nil
	}

	// 如果启用，重新启动
	if a.config.K8S.Enabled {
		if err := a.startK8SAPIProxy(); err != nil {
			logger.Warnf("远程启动 K8S API 代理失败: %v", err)
		}
	} else {
		logger.Info("K8S API 代理已通过远程配置关闭")
	}
}

// applySVCConfig 应用 K8S Service 配置变更（启停模块）
func (a *Agent) applySVCConfig() {
	// 先停止现有模块
	if a.svcProxy != nil {
		a.svcProxy.Stop()
		a.svcProxy = nil
	}
	if a.svcInformer != nil {
		a.svcInformer.Stop()
		a.svcInformer = nil
	}

	// 如果启用，重新启动
	if a.config.SVC.Enabled {
		if err := a.startK8SServiceModules(); err != nil {
			logger.Warnf("远程启动 K8S Service 模块失败: %v", err)
		}
	} else {
		logger.Info("K8S Service 模块已通过远程配置关闭")
	}
}

// applyEndpointConfig 应用 Endpoint 配置变更（启停 Endpoint gRPC Server）
func (a *Agent) applyEndpointConfig(cap *pb.AgentCapabilityConfig) {
	enabled := cap.EndpointEnabled

	// 确定监听端口
	listenPort := 50052
	if cap.EndpointListenPortSet && cap.EndpointListenPort > 0 {
		listenPort = int(cap.EndpointListenPort)
	}

	// 确定 token
	token := ""
	if cap.EndpointTokenSet {
		token = cap.EndpointToken
	}

	if enabled {
		if a.endpointServer == nil {
			// 首次启动
			logger.Infof("远程配置: Endpoint 功能启用，端口=%d", listenPort)
			a.endpointServer = NewEndpointServer(listenPort, token, a.ctx)
			if err := a.endpointServer.Start(); err != nil {
				logger.Warnf("启动 Endpoint gRPC Server 失败: %v", err)
				a.endpointServer = nil
			} else {
				// 启动 Endpoint SSH 代理（在 tsnet 上监听，接收 Desktop SSH 连接）
				if a.tsManager != nil && a.tsManager.IsConnected() {
					sshProxy, err := NewEndpointSSHProxy(a.endpointServer, a.tsManager, a.auditCollector, a.config.Tunnel.StateDir, a.ctx)
					if err != nil {
						logger.Warnf("创建 Endpoint SSH 代理失败: %v", err)
					} else if err := sshProxy.Start(); err != nil {
						logger.Warnf("启动 Endpoint SSH 代理失败: %v", err)
					} else {
						a.endpointSSHProxy = sshProxy
					}

					// 启动 Endpoint K8SAPI 代理（在 tsnet 上监听，接收 Desktop K8SAPI 连接）
					k8sapiProxy := NewEndpointK8SAPIProxy(a.endpointServer, a.tsManager, a.permCache, a.auditCollector, a.ctx)
					if err := k8sapiProxy.Start(); err != nil {
						logger.Warnf("启动 Endpoint K8SAPI 代理失败: %v", err)
					} else {
						a.endpointK8SAPIProxy = k8sapiProxy
					}

					// 设置 SVCProxy 的 EndpointServer 引用（用于 Endpoint 跳跃路径）
					if a.svcProxy != nil {
						a.svcProxy.SetEndpointServer(a.endpointServer)
					}
				}
			}
		} else {
			// 已运行，更新 token
			if cap.EndpointTokenSet {
				a.endpointServer.UpdateToken(token)
			}
		}
	} else {
		// 关闭
		if a.endpointK8SAPIProxy != nil {
			a.endpointK8SAPIProxy.Stop()
			a.endpointK8SAPIProxy = nil
		}
		if a.endpointSSHProxy != nil {
			a.endpointSSHProxy.Stop()
			a.endpointSSHProxy = nil
		}
		if a.endpointServer != nil {
			logger.Info("远程配置: Endpoint 功能关闭")
			a.endpointServer.Stop()
			a.endpointServer = nil
		}
		// 清除 SVCProxy 的 EndpointServer 引用
		if a.svcProxy != nil {
			a.svcProxy.SetEndpointServer(nil)
		}
	}
}

// updateProxiesLoop 代理更新循环（Client 模式专用）
// 监听域名缓存变化，自动更新本地代理
func (a *Agent) updateProxiesLoop(proxyManager *LocalProxyManager, domainCache *DomainCache) {
	defer a.wg.Done()

	// 立即执行一次更新
	if err := proxyManager.UpdateProxies(); err != nil {
		logger.Warnf("更新代理失败: %v", err)
	}

	// 定期检查（每 10 秒）
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	lastCount := domainCache.Count()

	for {
		select {
		case <-ticker.C:
			// 检查域名数量是否变化
			currentCount := domainCache.Count()
			if currentCount != lastCount {
				logger.Infof("域名列表已变化: %d -> %d，更新代理", lastCount, currentCount)
				if err := proxyManager.UpdateProxies(); err != nil {
					logger.Warnf("更新代理失败: %v", err)
				}
				lastCount = currentCount
			}
		case <-a.ctx.Done():
			logger.Debug("代理更新线程退出")
			return
		}
	}
}

// syncDomainsLoop 域名同步循环（Client 模式专用）
// 定期从 Server 获取可访问的域名列表，更新到本地缓存
func (a *Agent) syncDomainsLoop(cache *DomainCache) {
	defer a.wg.Done()

	// 立即执行一次同步
	a.syncDomains(cache)

	// 定期刷新（每 30 秒）
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			a.syncDomains(cache)
		case <-a.ctx.Done():
			logger.Debug("域名同步线程退出")
			return
		}
	}
}

// syncDomains 执行一次域名同步
func (a *Agent) syncDomains(cache *DomainCache) {
	// 检查 gRPC 连接
	if !a.IsGRPCConnected() {
		logger.Debug("gRPC 未连接，跳过域名同步")
		return
	}

	// 调用 ListDomains API
	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
	defer cancel()

	// 使用 Desktop gRPC 客户端
	desktopClient := pb.NewDesktopServiceClient(a.grpcConn)
	resp, err := desktopClient.ListDomains(ctx, &pb.ListDomainsRequest{})
	if err != nil {
		logger.Warnf("获取域名列表失败: %v", err)
		return
	}

	// 更新缓存
	cache.Update(resp.Domains)
	logger.Infof("域名列表已更新，共 %d 个域名", cache.Count())
}

// boolStr 布尔值转字符串（用于日志）
func boolStr(b bool) string {
	if b {
		return "启用"
	}
	return "禁用"
}

// parseNamespaces 解析逗号分隔的命名空间列表
func parseNamespaces(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// stringSliceEqual 比较两个字符串切片是否相等
func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
