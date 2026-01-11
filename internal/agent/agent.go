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
	agentID int64

	// 命令处理
	commandChan chan *pb.Command

	// Tailscale 管理
	tsManager    *TailscaleManager
	proxyManager *ProxyManager
	tailscaleIP  string

	// 上下文
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewAgent 创建Agent
func NewAgent(cfg *config.AgentConfig, version string) (*Agent, error) {
	ctx, cancel := context.WithCancel(context.Background())

	return &Agent{
		config:      cfg,
		version:     version,
		commandChan: make(chan *pb.Command, 100),
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

	// 启动心跳
	a.wg.Add(1)
	go a.heartbeatLoop()

	// 启动命令接收
	a.wg.Add(1)
	go a.receiveCommands()

	// 启动命令处理
	a.wg.Add(1)
	go a.processCommands()

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

	resp, err := a.grpcClient.Register(a.ctx, &pb.RegisterRequest{
		AgentName:  a.config.Agent.AgentName,
		AgentToken: a.config.Agent.AgentToken,
		Version:    a.version,
	})

	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("注册失败: %s", resp.Message)
	}

	a.agentID = resp.AgentId

	// 检查是否返回了 Tailscale 认证信息
	if resp.ControlUrl != "" && resp.AuthKey != "" {
		logger.Infof("注册成功，Agent ID: %d，使用 Tailscale 模式", a.agentID)

		// 启动 Tailscale
		if a.tsManager == nil {
			a.tsManager = NewTailscaleManager(a.config, a.grpcClient, a.agentID, a.config.Agent.AgentToken, a.ctx)
		}

		if err := a.tsManager.Start(resp.ControlUrl, resp.AuthKey); err != nil {
			return fmt.Errorf("启动 Tailscale 失败: %w", err)
		}

		a.tailscaleIP = a.tsManager.GetIP()
		logger.Infof("Tailscale 已连接，IP: %s", a.tailscaleIP)

		// 初始化 ProxyManager
		if a.proxyManager == nil {
			a.proxyManager = NewProxyManager(a.tsManager, a.ctx)
		}
	} else {
		return fmt.Errorf("Server 未返回 Tailscale 认证信息")
	}

	return nil
}

// heartbeatLoop 心跳循环（支持自动重连）
func (a *Agent) heartbeatLoop() {
	defer a.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	consecutiveFailures := 0
	maxFailures := 3
	lastSuccessLogged := false

	for {
		select {
		case <-ticker.C:
			if err := a.sendHeartbeat(); err != nil {
				consecutiveFailures++
				logger.Infof("心跳失败 (%d/%d): %v", consecutiveFailures, maxFailures, err)
				lastSuccessLogged = false

				if consecutiveFailures >= maxFailures {
					logger.Infof("心跳连续失败 %d 次，尝试重新注册", consecutiveFailures)
					if err := a.register(); err != nil {
						logger.Infof("重新注册失败: %v", err)
					} else {
						logger.Info("重新注册成功")
						consecutiveFailures = 0
					}
				}
			} else {
				if consecutiveFailures > 0 {
					logger.Infof("心跳恢复正常")
					consecutiveFailures = 0
					lastSuccessLogged = true
				} else if !lastSuccessLogged {
					logger.Infof("心跳正常")
					lastSuccessLogged = true
				}
			}

		case <-a.ctx.Done():
			return
		}
	}
}

// sendHeartbeat 发送心跳
func (a *Agent) sendHeartbeat() error {
	req := &pb.HeartbeatRequest{
		AgentId:    a.agentID,
		AgentToken: a.config.Agent.AgentToken,
		Version:    a.version,
	}

	// 添加 Tailscale 状态
	if a.tsManager != nil {
		req.TailscaleIp = a.tsManager.GetIP()
		req.TsConnected = a.tsManager.IsConnected()
		req.TsConnType = a.tsManager.GetConnType()
		req.TsConnectedAt = a.tsManager.GetConnectedAt()
	}

	resp, err := a.grpcClient.Heartbeat(a.ctx, req)

	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("心跳失败")
	}

	return nil
}

// receiveCommands 接收命令（双向流，支持自动重连）
func (a *Agent) receiveCommands() {
	defer a.wg.Done()

	retryDelay := 5 * time.Second
	maxRetryDelay := 60 * time.Second

	for {
		select {
		case <-a.ctx.Done():
			logger.Debug("命令接收线程退出")
			return
		default:
		}

		logger.Debug("建立命令接收流...")

		stream, err := a.grpcClient.ReceiveCommands(a.ctx)
		if err != nil {
			logger.Debugf("建立命令流失败: %v，%v后重试", err, retryDelay)

			select {
			case <-time.After(retryDelay):
				retryDelay *= 2
				if retryDelay > maxRetryDelay {
					retryDelay = maxRetryDelay
				}
				continue
			case <-a.ctx.Done():
				logger.Info("命令接收线程退出")
				return
			}
		}

		retryDelay = 5 * time.Second

		// 发送初始消息
		if err := stream.Send(&pb.CommandResponse{
			CommandId: fmt.Sprintf("init-%d", a.agentID),
			Success:   true,
			Message:   fmt.Sprintf("Agent已连接: %d", a.agentID),
		}); err != nil {
			logger.Infof("发送初始消息失败: %v，%v后重试", err, retryDelay)

			select {
			case <-time.After(retryDelay):
				continue
			case <-a.ctx.Done():
				logger.Debug("命令接收线程退出")
				return
			}
		}

		logger.Debug("命令接收流已建立")

		// 接收命令
		streamBroken := false
		for !streamBroken {
			cmd, err := stream.Recv()
			if err != nil {
				select {
				case <-a.ctx.Done():
					logger.Info("命令接收线程退出")
					return
				default:
					logger.Infof("接收命令失败: %v，将重新建立连接", err)
					streamBroken = true
					break
				}
			}

			logger.Infof("收到命令: %s, type=%v", cmd.CommandId, cmd.Type)

			select {
			case a.commandChan <- cmd:
			case <-a.ctx.Done():
				return
			}

			// 发送响应
			resp := &pb.CommandResponse{
				CommandId: cmd.CommandId,
				Success:   true,
				Message:   "命令已接收",
			}

			if err := stream.Send(resp); err != nil {
				select {
				case <-a.ctx.Done():
					logger.Info("命令接收线程退出")
					return
				default:
					logger.Infof("发送命令响应失败: %v，将重新建立连接", err)
					streamBroken = true
					break
				}
			}
		}

		select {
		case <-a.ctx.Done():
			logger.Info("命令接收线程退出")
			return
		default:
			logger.Infof("命令流已断开，%v后重新连接", retryDelay)
		}

		select {
		case <-time.After(retryDelay):
		case <-a.ctx.Done():
			logger.Info("命令接收线程退出")
			return
		}
	}
}

// processCommands 处理命令
func (a *Agent) processCommands() {
	defer a.wg.Done()

	for {
		select {
		case cmd := <-a.commandChan:
			a.handleCommand(cmd)

		case <-a.ctx.Done():
			return
		}
	}
}

// handleCommand 处理单个命令
func (a *Agent) handleCommand(cmd *pb.Command) {
	logger.Infof("处理命令: %s", cmd.CommandId)

	switch cmd.Type {
	case pb.Command_START_PROXY:
		a.handleStartProxy(cmd)

	case pb.Command_STOP_PROXY:
		a.handleStopProxy(cmd)

	case pb.Command_SYNC_PROXIES:
		a.handleSyncProxies(cmd)

	default:
		logger.Infof("未知命令类型: %v", cmd.Type)
	}
}

// handleStartProxy 处理启动端口映射命令
func (a *Agent) handleStartProxy(cmd *pb.Command) {
	if cmd.ProxyCommand == nil {
		logger.Warnf("START_PROXY 命令缺少 proxy_command")
		return
	}

	pc := cmd.ProxyCommand
	logger.Infof("启动端口映射: name=%s, port=%d, target=%s",
		pc.Name, pc.ListenPort, pc.TargetAddr)

	if a.proxyManager == nil {
		logger.Errorf("ProxyManager 未初始化")
		return
	}

	if a.proxyManager.Exists(pc.Name) {
		logger.Infof("端口映射已存在: %s", pc.Name)
		return
	}

	if err := a.proxyManager.Start(pc.Name, int(pc.ListenPort), pc.TargetAddr); err != nil {
		logger.Errorf("启动端口映射失败: %v", err)
		return
	}

	logger.Infof("端口映射启动成功: %s", pc.Name)
}

// handleStopProxy 处理停止端口映射命令
func (a *Agent) handleStopProxy(cmd *pb.Command) {
	if cmd.ProxyCommand == nil {
		logger.Warnf("STOP_PROXY 命令缺少 proxy_command")
		return
	}

	pc := cmd.ProxyCommand
	logger.Infof("停止端口映射: name=%s", pc.Name)

	if a.proxyManager == nil {
		logger.Errorf("ProxyManager 未初始化")
		return
	}

	// 检查代理是否存在
	if !a.proxyManager.Exists(pc.Name) {
		logger.Warnf("端口映射不存在，跳过停止: %s", pc.Name)
		return
	}

	if err := a.proxyManager.Stop(pc.Name); err != nil {
		logger.Errorf("停止端口映射失败: %v", err)
		return
	}

	logger.Infof("端口映射停止成功: %s", pc.Name)
}

// handleSyncProxies 处理同步端口映射命令
func (a *Agent) handleSyncProxies(_ *pb.Command) {
	logger.Info("收到同步端口映射命令")
	// 同步逻辑由 Server 通过多个 START_PROXY 命令实现
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
