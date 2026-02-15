package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
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
	Enabled bool   `toml:"enabled"` // 是否启用 SSH 能力
	Host    string `toml:"host"`    // SSH 目标地址（默认 127.0.0.1）
	Port    int    `toml:"port"`    // SSH 端口（默认 22）
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
	if !cfg.SSH.Enabled && !cfg.K8S.Enabled && !cfg.SVC.Enabled {
		logger.Fatalf("至少需要启用一种能力（ssh / k8s / svc）")
	}

	logger.Infof("Endpoint 名称: %s", cfg.Agent.Name)
	logger.Infof("Agent 地址: %s", cfg.Agent.Address)
	if cfg.SSH.Enabled {
		logger.Infof("SSH 能力: 启用 (host=%s, port=%d)", cfg.SSH.Host, cfg.SSH.Port)
	}
	if cfg.K8S.Enabled {
		logger.Infof("K8S API 能力: 启用 (api_server=%s)", cfg.K8S.APIServer)
	}
	if cfg.SVC.Enabled {
		logger.Infof("K8S Service 能力: 启用 (label_selector=%s)", cfg.SVC.LabelSelector)
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

	// 发送首次心跳（携带能力信息）
	if err := stream.Send(&pb.EndpointHeartbeatRequest{
		Token:        cfg.Agent.Token,
		Name:         cfg.Agent.Name,
		Capabilities: caps,
	}); err != nil {
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

	logger.Info("心跳流已建立，保持连接中...")

	// 心跳循环
	ticker := time.NewTicker(30 * time.Second)
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
			if err := stream.Send(&pb.EndpointHeartbeatRequest{
				Token:        cfg.Agent.Token,
				Name:         cfg.Agent.Name,
				Capabilities: caps,
			}); err != nil {
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

	// K8S API 默认值：自动检测集群内地址
	if cfg.K8S.Enabled && cfg.K8S.APIServer == "" {
		host := os.Getenv("KUBERNETES_SERVICE_HOST")
		port := os.Getenv("KUBERNETES_SERVICE_PORT")
		if host != "" && port != "" {
			cfg.K8S.APIServer = "https://" + net.JoinHostPort(host, port)
		}
	}

	return &cfg, nil
}
