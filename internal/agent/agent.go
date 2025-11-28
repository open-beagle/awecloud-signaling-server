package agent

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
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
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

// Agent Agent进程
type Agent struct {
	config *config.AgentConfig

	// gRPC连接
	grpcConn      *grpc.ClientConn
	grpcClient    pb.AgentServiceClient
	grpcConnected bool
	grpcMutex     sync.RWMutex

	// Agent信息
	agentID  int64
	frpToken string

	// 命令处理
	commandChan chan *pb.Command

	// STCP代理管理
	stcpProxies map[string]*STCPProxy // instance_name -> proxy
	proxyMutex  sync.RWMutex

	// FRP客户端管理
	frpManager *FRPManager

	// 上下文
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// STCPProxy STCP代理信息
type STCPProxy struct {
	InstanceName string
	SecretKey    string
	LocalIP      string
	LocalPort    int32
	CreatedAt    time.Time
	Status       string // "running", "stopped", "error"
}

// NewAgent 创建Agent
func NewAgent(cfg *config.AgentConfig) (*Agent, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// 创建FRP管理器
	frpManager, err := NewFRPManager(cfg, ctx)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("创建FRP管理器失败: %w", err)
	}

	return &Agent{
		config:      cfg,
		commandChan: make(chan *pb.Command, 100),
		stcpProxies: make(map[string]*STCPProxy),
		frpManager:  frpManager,
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

	// 启动Agent-FRP线程（FRP客户端）
	// 注意：FRP客户端可能需要额外配置（如认证token）
	// 如果连接失败，不影响Agent-Web线程的运行
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		log.Println("启动FRP客户端...")
		if err := a.frpManager.Run(); err != nil {
			log.Printf("FRP客户端错误: %v", err)
			log.Println("FRP客户端已停止，但Agent-Web线程继续运行")
		}
	}()

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

	log.Println("正在关闭Agent...")
	a.cancel()
	a.frpManager.Stop()
	a.wg.Wait()

	log.Println("Agent已关闭")
	return nil
}

// connectToServer 连接到Server
func (a *Agent) connectToServer() error {
	// 解析Server地址
	// Server地址格式：https://signaling.example.com 或 https://signaling.example.com:8080
	// gRPC 和 HTTP/API 复用同一个端口（HTTP/2）
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
		// URL中指定了端口
		grpcAddr = parsedURL.Host
	} else {
		// URL中没有端口，使用协议默认端口
		port := 80
		if parsedURL.Scheme == "https" {
			port = 443
		}
		grpcAddr = fmt.Sprintf("%s:%d", parsedURL.Hostname(), port)
	}

	log.Printf("连接到Server gRPC: %s (scheme: %s)", grpcAddr, parsedURL.Scheme)

	// 根据协议选择传输凭证
	var opts []grpc.DialOption
	if parsedURL.Scheme == "https" {
		// HTTPS：使用TLS，跳过证书验证（支持自签名证书）
		tlsConfig := &tls.Config{
			InsecureSkipVerify: true,
			ServerName:         parsedURL.Hostname(),
		}
		creds := credentials.NewTLS(tlsConfig)
		opts = append(opts, grpc.WithTransportCredentials(creds))
		log.Printf("gRPC使用TLS连接（跳过证书验证）")
	} else {
		// HTTP：不使用TLS
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
		log.Printf("gRPC使用明文连接")
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

	log.Println("gRPC连接建立成功")
	return nil
}

// register 注册Agent
func (a *Agent) register() error {
	log.Printf("注册Agent: %s", a.config.Agent.AgentName)

	resp, err := a.grpcClient.Register(a.ctx, &pb.RegisterRequest{
		AgentName:  a.config.Agent.AgentName,
		AgentToken: a.config.Agent.AgentToken,
	})

	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("注册失败: %s", resp.Message)
	}

	oldAgentID := a.agentID
	a.agentID = resp.AgentId
	a.frpToken = resp.Token

	if a.frpToken != "" {
		log.Printf("注册成功，Agent ID: %d, Token: %s...", a.agentID, a.frpToken[:16])
		// 将 Token 传递给 FRP Manager
		a.frpManager.SetToken(a.frpToken)
	} else {
		log.Printf("注册成功，Agent ID: %d (无 Token)", a.agentID)
	}

	// 更新 FRP 连接信息
	// 优先使用配置文件或环境变量中的 public_url
	if a.config.Server.PublicURL != "" {
		log.Printf("使用配置的 FRP 公网地址: %s (忽略 Server 返回的地址)", a.config.Server.PublicURL)
		a.frpManager.SetServerURL(a.config.Server.PublicURL)
	} else if resp.Server != "" {
		// 使用 Server 返回的完整 URL
		log.Printf("使用 Server 提供的隧道地址: %s", resp.Server)
		a.frpManager.SetServerURL(resp.Server)
	} else if resp.Port > 0 {
		// 使用 Server 地址 + 端口
		log.Printf("使用隧道端口: %d", resp.Port)
		a.frpManager.SetServerPort(int(resp.Port))
	}

	// 如果是重新注册（Agent ID变化），需要重新同步所有STCP代理
	if oldAgentID != 0 && oldAgentID != a.agentID {
		log.Printf("Agent ID 变化 (%d -> %d)，重新同步STCP代理", oldAgentID, a.agentID)
		a.resyncSTCPProxies()
	}

	return nil
}

// resyncSTCPProxies 重新同步所有STCP代理（Server重启后恢复）
func (a *Agent) resyncSTCPProxies() {
	a.proxyMutex.RLock()
	proxies := make([]*STCPProxy, 0, len(a.stcpProxies))
	for _, proxy := range a.stcpProxies {
		proxies = append(proxies, proxy)
	}
	a.proxyMutex.RUnlock()

	if len(proxies) == 0 {
		log.Println("没有需要同步的STCP代理")
		return
	}

	log.Printf("开始重新同步 %d 个STCP代理", len(proxies))

	for _, proxy := range proxies {
		log.Printf("重新创建STCP代理: %s", proxy.InstanceName)

		// 重新添加到FRP Manager
		if err := a.frpManager.AddSTCPProxy(
			proxy.InstanceName,
			proxy.SecretKey,
			proxy.LocalIP,
			proxy.LocalPort,
		); err != nil {
			log.Printf("重新创建FRP代理失败: %s, error: %v", proxy.InstanceName, err)

			// 更新状态为错误
			a.proxyMutex.Lock()
			if p, exists := a.stcpProxies[proxy.InstanceName]; exists {
				p.Status = "error"
			}
			a.proxyMutex.Unlock()
		} else {
			log.Printf("重新创建FRP代理成功: %s", proxy.InstanceName)

			// 更新状态为运行中
			a.proxyMutex.Lock()
			if p, exists := a.stcpProxies[proxy.InstanceName]; exists {
				p.Status = "running"
			}
			a.proxyMutex.Unlock()
		}
	}

	log.Printf("STCP代理同步完成")
}

// heartbeatLoop 心跳循环（支持自动重连）
func (a *Agent) heartbeatLoop() {
	defer a.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	consecutiveFailures := 0
	maxFailures := 3

	for {
		select {
		case <-ticker.C:
			if err := a.sendHeartbeat(); err != nil {
				consecutiveFailures++
				log.Printf("心跳失败 (%d/%d): %v", consecutiveFailures, maxFailures, err)

				// 如果连续失败次数过多，尝试重新注册
				if consecutiveFailures >= maxFailures {
					log.Printf("心跳连续失败 %d 次，尝试重新注册", consecutiveFailures)
					if err := a.register(); err != nil {
						log.Printf("重新注册失败: %v", err)
					} else {
						log.Println("重新注册成功")
						consecutiveFailures = 0
					}
				}
			} else {
				// 心跳成功，重置失败计数
				if consecutiveFailures > 0 {
					log.Printf("心跳恢复正常")
					consecutiveFailures = 0
				}
			}

		case <-a.ctx.Done():
			return
		}
	}
}

// sendHeartbeat 发送心跳
func (a *Agent) sendHeartbeat() error {
	resp, err := a.grpcClient.Heartbeat(a.ctx, &pb.HeartbeatRequest{
		AgentId:    a.agentID,
		AgentToken: a.config.Agent.AgentToken,
	})

	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("心跳失败")
	}

	log.Printf("心跳成功，时间戳: %d", resp.Timestamp)
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
			log.Println("命令接收线程退出")
			return
		default:
		}

		log.Println("建立命令接收流...")

		stream, err := a.grpcClient.ReceiveCommands(a.ctx)
		if err != nil {
			log.Printf("建立命令流失败: %v，%v后重试", err, retryDelay)
			time.Sleep(retryDelay)
			// 指数退避
			retryDelay *= 2
			if retryDelay > maxRetryDelay {
				retryDelay = maxRetryDelay
			}
			continue
		}

		// 重置重试延迟
		retryDelay = 5 * time.Second

		// 发送初始消息（确认连接，包含agent_id）
		if err := stream.Send(&pb.CommandResponse{
			CommandId: fmt.Sprintf("init-%d", a.agentID),
			Success:   true,
			Message:   fmt.Sprintf("Agent已连接: %d", a.agentID),
		}); err != nil {
			log.Printf("发送初始消息失败: %v，%v后重试", err, retryDelay)
			time.Sleep(retryDelay)
			continue
		}

		log.Println("命令接收流已建立")

		// 接收命令
		streamBroken := false
		for !streamBroken {
			cmd, err := stream.Recv()
			if err != nil {
				log.Printf("接收命令失败: %v，将重新建立连接", err)
				streamBroken = true
				break
			}

			log.Printf("收到命令: %s, type=%v, instance=%s",
				cmd.CommandId, cmd.Type, cmd.InstanceName)

			// 发送到命令处理通道
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
				log.Printf("发送命令响应失败: %v，将重新建立连接", err)
				streamBroken = true
				break
			}
		}

		// 流断开，等待后重试
		log.Printf("命令流已断开，%v后重新连接", retryDelay)
		time.Sleep(retryDelay)
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
	log.Printf("处理命令: %s", cmd.CommandId)

	switch cmd.Type {
	case pb.Command_CREATE_STCP:
		a.handleCreateSTCP(cmd)

	case pb.Command_DELETE_STCP:
		a.handleDeleteSTCP(cmd)

	default:
		log.Printf("未知命令类型: %v", cmd.Type)
	}
}

// handleCreateSTCP 处理创建STCP命令
func (a *Agent) handleCreateSTCP(cmd *pb.Command) {
	log.Printf("创建STCP代理: instance=%s, local=%s:%d, secret=%s",
		cmd.InstanceName, cmd.LocalIp, cmd.LocalPort, cmd.SecretKey)

	// 检查是否已存在
	a.proxyMutex.Lock()
	if _, exists := a.stcpProxies[cmd.InstanceName]; exists {
		log.Printf("STCP代理已存在: %s", cmd.InstanceName)
		a.proxyMutex.Unlock()
		return
	}

	// 创建代理记录
	proxy := &STCPProxy{
		InstanceName: cmd.InstanceName,
		SecretKey:    cmd.SecretKey,
		LocalIP:      cmd.LocalIp,
		LocalPort:    cmd.LocalPort,
		CreatedAt:    time.Now(),
		Status:       "running",
	}
	a.stcpProxies[cmd.InstanceName] = proxy
	a.proxyMutex.Unlock()

	// 通知Agent-FRP线程创建实际的FRP代理
	if err := a.frpManager.AddSTCPProxy(cmd.InstanceName, cmd.SecretKey, cmd.LocalIp, cmd.LocalPort); err != nil {
		log.Printf("创建FRP代理失败: %v", err)

		// 更新状态为错误
		a.proxyMutex.Lock()
		if p, exists := a.stcpProxies[cmd.InstanceName]; exists {
			p.Status = "error"
		}
		a.proxyMutex.Unlock()
		return
	}

	log.Printf("STCP代理创建成功: %s (总计: %d个)", cmd.InstanceName, len(a.stcpProxies))
}

// handleDeleteSTCP 处理删除STCP命令
func (a *Agent) handleDeleteSTCP(cmd *pb.Command) {
	log.Printf("删除STCP代理: instance=%s", cmd.InstanceName)

	// 检查代理是否存在
	a.proxyMutex.Lock()
	if _, exists := a.stcpProxies[cmd.InstanceName]; !exists {
		log.Printf("STCP代理不存在: %s", cmd.InstanceName)
		a.proxyMutex.Unlock()
		return
	}
	a.proxyMutex.Unlock()

	// 通知Agent-FRP线程删除实际的FRP代理
	if err := a.frpManager.RemoveSTCPProxy(cmd.InstanceName); err != nil {
		log.Printf("删除FRP代理失败: %v", err)
		return
	}

	// 删除代理记录
	a.proxyMutex.Lock()
	delete(a.stcpProxies, cmd.InstanceName)
	a.proxyMutex.Unlock()

	log.Printf("STCP代理删除成功: %s (剩余: %d个)", cmd.InstanceName, len(a.stcpProxies))
}

// GetSTCPProxies 获取所有STCP代理（用于状态上报）
func (a *Agent) GetSTCPProxies() []*STCPProxy {
	a.proxyMutex.RLock()
	defer a.proxyMutex.RUnlock()

	proxies := make([]*STCPProxy, 0, len(a.stcpProxies))
	for _, proxy := range a.stcpProxies {
		proxies = append(proxies, proxy)
	}
	return proxies
}

// IsGRPCConnected 检查gRPC连接是否正常
func (a *Agent) IsGRPCConnected() bool {
	a.grpcMutex.RLock()
	defer a.grpcMutex.RUnlock()
	return a.grpcConnected && a.grpcConn != nil
}

// IsFRPConnected 检查FRP连接是否正常
func (a *Agent) IsFRPConnected() bool {
	if a.frpManager == nil {
		return false
	}
	return a.frpManager.IsConnected()
}

// startHealthServer 启动健康检查HTTP服务器
func (a *Agent) startHealthServer() error {
	router := gin.New()
	router.Use(gin.Recovery())

	healthAPI := NewHealthAPI(a)

	// 健康检查接口（根路径，K8s探测用）
	router.GET("/health", healthAPI.Health)
	router.GET("/health/ready", healthAPI.Ready)

	// 获取健康检查端口（默认8090）
	healthPort := 8090
	if a.config.Health.Port > 0 {
		healthPort = a.config.Health.Port
	}

	addr := fmt.Sprintf(":%d", healthPort)

	// 启动HTTP服务器（用于健康检查）
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		log.Printf("健康检查服务器启动在: http://0.0.0.0%s", addr)
		if err := router.Run(addr); err != nil {
			log.Printf("健康检查服务器启动失败: %v", err)
		}
	}()

	return nil
}
