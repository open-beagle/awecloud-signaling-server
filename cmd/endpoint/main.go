package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/BurntSushi/toml"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/banner"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

var (
	version   = "dev"
	gitCommit = "unknown"
	buildDate = "unknown"
	goVersion = "unknown"
)

// EndpointConfig Endpoint 配置
// 本地只配 Agent 连接信息和能力开关，其余设置从 Server 通过 Agent 下发
type EndpointConfig struct {
	Agent AgentConnect `toml:"agent"`
	SSH   SSHConfig    `toml:"ssh"`
	K8S   K8SConfig    `toml:"k8s"`
	SVC   SVCConfig    `toml:"svc"`
}

// AgentConnect Agent 连接配置
type AgentConnect struct {
	Address string `toml:"address"` // Agent 内网 gRPC 地址，如 192.168.1.1:50052
	Token   string `toml:"token"`   // 注册令牌（Server 生成，ep_ 前缀）
	Name    string `toml:"name"`    // Endpoint 名称（可选，默认 hostname）
}

// SSHConfig SSH 能力配置
type SSHConfig struct {
	Enabled bool     `toml:"enabled"` // 是否启用 SSH 能力
	Host    string   `toml:"host"`    // SSH 目标地址（默认 127.0.0.1）
	Port    int      `toml:"port"`    // SSH 端口（默认 22）
	Users   []string `toml:"users"`   // 允许的 SSH 用户列表（自动检测，可选配置）
}

// K8SConfig K8S API 能力配置
type K8SConfig struct {
	Enabled   bool   `toml:"enabled"`    // 是否启用 K8S API 能力
	APIServer string `toml:"api_server"` // K8S API Server 地址（默认自动检测）
}

// SVCConfig K8S Service 能力配置
type SVCConfig struct {
	Enabled       bool   `toml:"enabled"`        // 是否启用 K8S Service 能力
	LabelSelector string `toml:"label_selector"` // 标签选择器（可选）
}

func main() {
	configPath := flag.String("c", "config/endpoint.toml", "配置文件路径")
	showVersion := flag.Bool("v", false, "显示版本信息")
	showVersionLong := flag.Bool("version", false, "显示版本信息")
	flag.Parse()

	// 显示版本信息
	if *showVersion || *showVersionLong {
		fmt.Printf("AWECloud Signaling Endpoint\n")
		fmt.Printf("Version:    %s\n", version)
		fmt.Printf("Git Commit: %s\n", gitCommit)
		fmt.Printf("Build Date: %s\n", buildDate)
		fmt.Printf("Go Version: %s\n", goVersion)
		os.Exit(0)
	}

	// 打印启动横幅
	banner.Print(banner.BuildInfo{
		AppName:   "AWECloud Signaling Endpoint",
		Version:   version,
		GitCommit: gitCommit,
		BuildDate: buildDate,
		GoVersion: goVersion,
	})

	// 加载配置
	cfg, err := loadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "加载配置失败: %v\n", err)
		os.Exit(1)
	}

	// 初始化日志
	if err := logger.InitLogrus("info", "logs/endpoint.log"); err != nil {
		fmt.Fprintf(os.Stderr, "初始化日志失败: %v\n", err)
		os.Exit(1)
	}

	// 验证必要配置
	if cfg.Agent.Address == "" {
		logger.Fatalf("缺少 Agent 地址配置（agent.address）")
	}
	if cfg.Agent.Token == "" {
		logger.Fatalf("缺少注册令牌配置（agent.token）")
	}

	logger.Infof("Endpoint 名称: %s", cfg.Agent.Name)
	logger.Infof("Agent 地址: %s", cfg.Agent.Address)

	// 能力配置（可选，也可以完全由 Web 界面管理）
	if !cfg.SSH.Enabled && !cfg.K8S.Enabled && !cfg.SVC.Enabled {
		logger.Warnf("本地未启用任何能力，将由 Web 界面管理能力配置")
	} else {
		if cfg.SSH.Enabled {
			logger.Infof("SSH 能力: 启用 (host=%s, port=%d)", cfg.SSH.Host, cfg.SSH.Port)
		}
		if cfg.K8S.Enabled {
			logger.Infof("K8S API 能力: 启用 (api_server=%s)", cfg.K8S.APIServer)
		}
		if cfg.SVC.Enabled {
			logger.Infof("K8S Service 能力: 启用 (label_selector=%s)", cfg.SVC.LabelSelector)
		}
	}

	// 创建上下文
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 启动连接循环（自动重连）
	go connectLoop(ctx, cfg)

	// 等待中断信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("正在关闭 Endpoint...")
	cancel()
	time.Sleep(500 * time.Millisecond)
	logger.Info("Endpoint 已关闭")
}

// buildCapabilities 根据配置构建能力列表
func buildCapabilities(cfg *EndpointConfig) []*pb.EndpointCapabilityInfo {
	var caps []*pb.EndpointCapabilityInfo
	if cfg.SSH.Enabled {
		caps = append(caps, &pb.EndpointCapabilityInfo{
			Type: "ssh",
			Host: cfg.SSH.Host,
			Port: int32(cfg.SSH.Port),
		})
	}
	if cfg.K8S.Enabled {
		caps = append(caps, &pb.EndpointCapabilityInfo{
			Type:      "k8sapi",
			ApiServer: cfg.K8S.APIServer,
		})
	}
	if cfg.SVC.Enabled {
		caps = append(caps, &pb.EndpointCapabilityInfo{
			Type: "k8sservice",
		})
	}
	return caps
}

// buildConnectedEndpoint 根据配置构建 ConnectedEndpoint 消息（用于心跳上报）
func buildConnectedEndpoint(cfg *EndpointConfig, discovery *K8SServiceDiscovery) *pb.ConnectedEndpoint {
	ep := &pb.ConnectedEndpoint{
		Name:         cfg.Agent.Name,
		Status:       "online",
		Capabilities: buildCapabilities(cfg),
	}

	// SSH 用户列表
	if cfg.SSH.Enabled {
		ep.SshUsers = cfg.SSH.Users
	}

	// K8S API 配置
	if cfg.K8S.Enabled {
		ep.K8SapiApiServer = cfg.K8S.APIServer
	}

	// K8S Service 配置
	if cfg.SVC.Enabled {
		ep.K8SserviceLabelSelector = cfg.SVC.LabelSelector
		// 添加发现的 Service 列表
		if discovery != nil {
			ep.DiscoveredServices = discovery.GetDiscoveredServices()
		}
	}

	return ep
}

// connectLoop 连接 Agent 的主循环（自动重连）
func connectLoop(ctx context.Context, cfg *EndpointConfig) {
	retryDelay := 5 * time.Second
	maxRetryDelay := 60 * time.Second

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		logger.Infof("连接 Agent: %s", cfg.Agent.Address)

		err := connectAndRun(ctx, cfg)
		if err != nil {
			logger.Warnf("连接断开: %v", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(retryDelay):
			retryDelay *= 2
			if retryDelay > maxRetryDelay {
				retryDelay = maxRetryDelay
			}
		}
	}
}

// connectAndRun 连接 Agent 并运行注册 + 心跳
func connectAndRun(ctx context.Context, cfg *EndpointConfig) error {
	// 建立 gRPC 连接（内网明文）
	conn, err := grpc.NewClient(cfg.Agent.Address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return fmt.Errorf("创建 gRPC 连接失败: %w", err)
	}
	defer conn.Close()

	client := pb.NewEndpointServiceClient(conn)

	// 注册
	regResp, err := client.Register(ctx, &pb.EndpointRegisterRequest{
		Token:   cfg.Agent.Token,
		Name:    cfg.Agent.Name,
		Version: version,
	})
	if err != nil {
		return fmt.Errorf("注册失败: %w", err)
	}
	if !regResp.Success {
		return fmt.Errorf("注册被拒绝: %s", regResp.Message)
	}

	logger.Infof("注册成功: endpoint_id=%s", regResp.EndpointId)

	// 建立心跳流
	stream, err := client.Heartbeat(ctx)
	if err != nil {
		return fmt.Errorf("建立心跳流失败: %w", err)
	}

	// 构建能力列表
	caps := buildCapabilities(cfg)

	// 自动检测 SSH 用户列表（无论是否启用 SSH 能力都检测，供 Server 使用）
	var sshUsers []string
	// 优先使用配置文件中的值，如果没有则自动检测
	if len(cfg.SSH.Users) > 0 {
		sshUsers = cfg.SSH.Users
	} else {
		// 自动检测系统用户
		sshUsers = detectSSHConfig()
	}

	// 自动检测 K8S API Server 地址（无论是否启用 K8S 能力都检测，供 Server 使用）
	// 如果配置文件中没有指定，则尝试自动检测
	if cfg.K8S.APIServer == "" {
		// 优先级1：Pod 内环境变量（集群内部署）
		host := os.Getenv("KUBERNETES_SERVICE_HOST")
		port := os.Getenv("KUBERNETES_SERVICE_PORT")
		if host != "" && port != "" {
			cfg.K8S.APIServer = "https://" + net.JoinHostPort(host, port)
			logger.Infof("从环境变量检测到 K8S API Server: %s", cfg.K8S.APIServer)
		} else {
			// 优先级2：从 kubeconfig 读取（节点部署）
			if apiServer := detectK8SAPIServerFromKubeconfig(); apiServer != "" {
				cfg.K8S.APIServer = apiServer
			}
		}
	}

	// 发送首次心跳（携带能力信息和配置）
	heartbeatReq := &pb.EndpointHeartbeatRequest{
		Token:        cfg.Agent.Token,
		Name:         cfg.Agent.Name,
		Capabilities: caps,
		SshUsers:     sshUsers, // 始终上报 SSH 用户列表
	}
	// 添加 K8S API 配置（始终上报，无论是否启用）
	if cfg.K8S.APIServer != "" {
		heartbeatReq.K8SapiApiServer = cfg.K8S.APIServer
	}
	// 添加 K8S Service 配置
	if cfg.SVC.Enabled {
		heartbeatReq.K8SserviceLabelSelector = cfg.SVC.LabelSelector
	}

	if err := stream.Send(heartbeatReq); err != nil {
		return fmt.Errorf("发送首次心跳失败: %w", err)
	}

	// 接收首次响应
	resp, err := stream.Recv()
	if err != nil {
		return fmt.Errorf("接收心跳响应失败: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("心跳被拒绝: %s", resp.Message)
	}

	// 处理首次响应中 Server 下发的能力配置
	if resp.SshEnabledSet {
		cfg.SSH.Enabled = resp.SshEnabled
	}
	if resp.K8SapiEnabledSet {
		cfg.K8S.Enabled = resp.K8SapiEnabled
		if resp.K8SapiApiServer != "" && cfg.K8S.APIServer == "" {
			cfg.K8S.APIServer = resp.K8SapiApiServer
		}
	}
	if resp.K8SserviceEnabledSet {
		cfg.SVC.Enabled = resp.K8SserviceEnabled
	}
	// 重新构建能力列表（基于 Server 下发的配置）
	caps = buildCapabilities(cfg)
	logger.Infof("Server 下发能力配置: ssh=%v, k8sapi=%v(api_server=%s), k8ssvc=%v",
		cfg.SSH.Enabled, cfg.K8S.Enabled, cfg.K8S.APIServer, cfg.SVC.Enabled)

	// 启动 K8S Service 自动发现（如果启用）
	var discovery *K8SServiceDiscovery
	if cfg.SVC.Enabled && cfg.K8S.APIServer != "" {
		discovery = NewK8SServiceDiscovery(cfg, ctx)
		if err := discovery.Start(); err != nil {
			logger.Warnf("启动 K8S Service 自动发现失败: %v", err)
		} else {
			defer discovery.Stop()
		}
	}

	logger.Debug("心跳流已建立，保持连接中...")

	// 心跳循环
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	// 启动接收协程（处理心跳响应和 Shell 请求通知）
	recvDone := make(chan error, 1)
	go func() {
		for {
			resp, err := stream.Recv()
			if err != nil {
				recvDone <- err
				return
			}

			// 处理 Server 下发的能力配置（更新运行时配置）
			if resp.SshEnabledSet {
				cfg.SSH.Enabled = resp.SshEnabled
			}
			if resp.K8SapiEnabledSet {
				cfg.K8S.Enabled = resp.K8SapiEnabled
				// 如果 Server 下发了 api_server，优先使用（本地配置为空时）
				if resp.K8SapiApiServer != "" && cfg.K8S.APIServer == "" {
					cfg.K8S.APIServer = resp.K8SapiApiServer
				}
			}
			if resp.K8SserviceEnabledSet {
				cfg.SVC.Enabled = resp.K8SserviceEnabled
			}

			// 重新构建能力列表（基于最新配置）
			caps = buildCapabilities(cfg)

			// 处理 Shell 请求通知
			for _, shellReq := range resp.ShellRequests {
				go handleShellRequest(ctx, client, cfg, shellReq)
			}

			// 处理 K8S API 代理请求通知
			for _, k8sapiReq := range resp.K8SapiProxyRequests {
				go handleK8SAPIProxyRequest(ctx, client, cfg, k8sapiReq)
			}

			// 处理 K8S Service 代理请求通知
			for _, svcReq := range resp.SvcProxyRequests {
				go handleSVCProxyRequest(ctx, client, cfg, svcReq)
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-recvDone:
			return fmt.Errorf("心跳流断开: %w", err)
		case <-ticker.C:
			heartbeatReq := &pb.EndpointHeartbeatRequest{
				Token:        cfg.Agent.Token,
				Name:         cfg.Agent.Name,
				Capabilities: caps,
				SshUsers:     sshUsers, // 始终上报 SSH 用户列表
			}
			// 添加 K8S API 配置（始终上报，无论是否启用）
			if cfg.K8S.APIServer != "" {
				heartbeatReq.K8SapiApiServer = cfg.K8S.APIServer
			}
			// 添加 K8S Service 配置
			if cfg.SVC.Enabled {
				heartbeatReq.K8SserviceLabelSelector = cfg.SVC.LabelSelector
				// 添加发现的 Service 列表
				if discovery != nil {
					heartbeatReq.DiscoveredServices = discovery.GetDiscoveredServices()
				}
			}

			logger.Debugf("发送心跳: ssh_users=%v, discovered_services=%d", sshUsers, len(heartbeatReq.DiscoveredServices))
			if err := stream.Send(heartbeatReq); err != nil {
				return fmt.Errorf("发送心跳失败: %w", err)
			}
		}
	}
}

// loadConfig 加载 Endpoint 配置
func loadConfig(path string) (*EndpointConfig, error) {
	var cfg EndpointConfig

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("配置文件不存在: %s", path)
		}
		return nil, err
	}

	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	// 环境变量覆盖
	if v := os.Getenv("SIGNAL_ENDPOINT_AGENT_ADDRESS"); v != "" {
		cfg.Agent.Address = v
	}
	if v := os.Getenv("SIGNAL_ENDPOINT_TOKEN"); v != "" {
		cfg.Agent.Token = v
	}
	if v := os.Getenv("SIGNAL_ENDPOINT_NAME"); v != "" {
		cfg.Agent.Name = v
	}
	if v := os.Getenv("SIGNAL_ENDPOINT_SSH_ENABLED"); v == "true" {
		cfg.SSH.Enabled = true
	}
	if v := os.Getenv("SIGNAL_ENDPOINT_SSH_HOST"); v != "" {
		cfg.SSH.Host = v
	}
	if v := os.Getenv("SIGNAL_ENDPOINT_SSH_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			cfg.SSH.Port = p
		}
	}
	if v := os.Getenv("SIGNAL_ENDPOINT_K8S_ENABLED"); v == "true" {
		cfg.K8S.Enabled = true
	}
	if v := os.Getenv("SIGNAL_ENDPOINT_K8S_API_SERVER"); v != "" {
		cfg.K8S.APIServer = v
	}
	if v := os.Getenv("SIGNAL_ENDPOINT_SVC_ENABLED"); v == "true" {
		cfg.SVC.Enabled = true
	}
	if v := os.Getenv("SIGNAL_ENDPOINT_SVC_LABEL_SELECTOR"); v != "" {
		cfg.SVC.LabelSelector = v
	}

	// 默认名称：使用 hostname
	if cfg.Agent.Name == "" {
		hostname, _ := os.Hostname()
		cfg.Agent.Name = hostname
	}

	// SSH 默认值
	if cfg.SSH.Enabled {
		if cfg.SSH.Host == "" {
			cfg.SSH.Host = "127.0.0.1"
		}
		if cfg.SSH.Port == 0 {
			cfg.SSH.Port = 22
		}
	}

	// K8S API 默认值：自动检测（在心跳发送前进行，这里不检测）
	// 检测逻辑已移到 connectAndRun 函数中，确保无论是否启用都能检测并上报

	return &cfg, nil
}

// detectK8SAPIServerFromKubeconfig 从 kubeconfig 读取 K8S API Server 地址
func detectK8SAPIServerFromKubeconfig() string {
	// 优先使用 KUBECONFIG 环境变量
	kubeconfigPath := os.Getenv("KUBECONFIG")
	if kubeconfigPath == "" {
		// 默认路径
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		kubeconfigPath = homeDir + "/.kube/config"
	}

	// 读取 kubeconfig 文件
	data, err := os.ReadFile(kubeconfigPath)
	if err != nil {
		return ""
	}

	// 简单解析：查找 "server:" 行
	// 这是一个简化实现，只提取第一个 cluster 的 server 地址
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "server:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				server := strings.TrimSpace(parts[1])
				// 移除可能的引号
				server = strings.Trim(server, "\"'")
				if server != "" {
					logger.Infof("从 kubeconfig 检测到 K8S API Server: %s", server)
					return server
				}
			}
		}
	}

	return ""
}
