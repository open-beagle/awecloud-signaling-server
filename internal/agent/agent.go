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

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/gin-gonic/gin"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
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

	// 网络信息
	networkInfo *NetworkInfo
	lanDetector *LANDetector

	// 配置版本（用于增量同步）
	configVersion int64

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
		config:      cfg,
		version:     version,
		lanDetector: lanDetector,
		networkInfo: networkInfo,
		ctx:         ctx,
		cancel:      cancel,
	}, nil
}

// Run 运行Agent
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

	// 启动心跳流（双向流，用于同步配置）
	a.wg.Add(1)
	go a.heartbeatLoop()

	// 等待中断信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("正在关闭Agent...")
	a.cancel()

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
		}
		creds := credentials.NewTLS(tlsConfig)
		opts = append(opts, grpc.WithTransportCredentials(creds))
		logger.Debug("gRPC使用TLS连接（跳过证书验证）")
	} else {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
		logger.Debug("gRPC使用明文连接")
	}

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
	var systemInfo *pb.SystemInfo
	if a.networkInfo != nil {
		systemInfo = &pb.SystemInfo{
			Hostname: a.networkInfo.Hostname,
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
			a.proxyManager = NewProxyManager(a.tsManager, a.ctx)
		}

		// 初始化 VisitorManager
		if a.visitorManager == nil {
			a.visitorManager = NewVisitorManager(a.tsManager, a.config, a.ctx)
		}
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
	if a.networkInfo != nil {
		req.Hostname = a.networkInfo.Hostname
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

	return stream.Send(req)
}

// handleHeartbeatResponse 处理心跳响应（配置同步）
func (a *Agent) handleHeartbeatResponse(resp *pb.AgentHeartbeatResponse) {
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

// syncServices 同步端口映射服务
func (a *Agent) syncServices(services []*pb.ServiceConfig) {
	if a.proxyManager == nil {
		logger.Warn("ProxyManager 未初始化，跳过服务同步")
		return
	}

	// 构建期望的服务集合
	expected := make(map[string]*pb.ServiceConfig)
	for _, svc := range services {
		if svc.Enabled {
			expected[svc.Id] = svc
		}
	}

	// 停止不需要的服务
	for name := range a.proxyManager.GetStatus() {
		if _, ok := expected[name]; !ok {
			logger.Infof("停止端口映射服务: %s", name)
			if err := a.proxyManager.Stop(name); err != nil {
				logger.Warnf("停止端口映射服务失败: %s, %v", name, err)
			}
		}
	}

	// 启动新服务
	for id, svc := range expected {
		if !a.proxyManager.Exists(id) {
			// 解析监听端口
			listenPort := 0
			if _, err := fmt.Sscanf(svc.ListenAddr, ":%d", &listenPort); err != nil {
				logger.Warnf("解析监听地址失败: %s, %v", svc.ListenAddr, err)
				continue
			}

			logger.Infof("启动端口映射服务: %s, port=%d, target=%s", svc.Name, listenPort, svc.TargetAddr)
			if err := a.proxyManager.Start(id, listenPort, svc.TargetAddr); err != nil {
				logger.Warnf("启动端口映射服务失败: %s, %v", svc.Name, err)
			}
		}
	}
}

// syncForwards 同步端口访问服务
func (a *Agent) syncForwards(forwards []*pb.ForwardConfig) {
	if a.visitorManager == nil {
		logger.Warn("VisitorManager 未初始化，跳过端口访问同步")
		return
	}

	// 构建期望的服务集合
	expected := make(map[string]*pb.ForwardConfig)
	for _, fwd := range forwards {
		if fwd.Enabled {
			expected[fwd.Id] = fwd
		}
	}

	// 停止不需要的服务
	for name := range a.visitorManager.GetStatus() {
		if _, ok := expected[name]; !ok {
			logger.Infof("停止端口访问服务: %s", name)
			if err := a.visitorManager.Stop(name); err != nil {
				logger.Warnf("停止端口访问服务失败: %s, %v", name, err)
			}
		}
	}

	// 启动新服务
	for id, fwd := range expected {
		if !a.visitorManager.Exists(id) {
			// 解析监听端口
			listenPort := 0
			if _, err := fmt.Sscanf(fwd.ListenAddr, ":%d", &listenPort); err != nil {
				logger.Warnf("解析监听地址失败: %s, %v", fwd.ListenAddr, err)
				continue
			}

			logger.Infof("启动端口访问服务: %s, port=%d, target=%s", fwd.Name, listenPort, fwd.TargetAddr)
			if err := a.visitorManager.Start(id, listenPort, fwd.TargetAddr); err != nil {
				logger.Warnf("启动端口访问服务失败: %s, %v", fwd.Name, err)
			}
		}
	}
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
