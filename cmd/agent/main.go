package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/open-beagle/awecloud-signaling-server/internal/agent"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/banner"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/telemetry"
	"github.com/open-beagle/awecloud-signaling-server/internal/updater"
	"tailscale.com/cmd/tailscaled/childproc"
	_ "tailscale.com/ssh/tailssh"
)

var (
	version   = "dev"
	gitCommit = "unknown"
	buildDate = "unknown"
	goVersion = "unknown"
	BUILD_URL = "" // 编译时注入的默认 Server 地址
)

func main() {
	runTailscaleChildIfRequested()

	if len(os.Args) > 1 && os.Args[1] == "updater-apply" {
		if err := updater.RunApplyCLI(os.Args[2:]); err != nil {
			log.Printf("updater apply failed: %v", err)
			os.Exit(1)
		}
		return
	}

	// 禁用 Tailscale 内置的 logtail（避免向 log.tailscale.io 发送遥测数据）
	// 并防止在 DNS 解析失败时触发到公共 DERP 节点的 bootstrapDNS 请求
	os.Setenv("TS_NO_LOGS_NO_SUPPORT", "true")

	// 检查是否是 dial 子命令：signal_agent dial <host> <port>
	if len(os.Args) >= 4 && os.Args[1] == "dial" {
		runDial(os.Args[2], os.Args[3])
		return
	}

	configPath := flag.String("c", "config/agent.toml", "配置文件路径")
	showVersion := flag.Bool("v", false, "显示版本信息")
	showVersionLong := flag.Bool("version", false, "显示版本信息")

	// 部署模式参数
	deployToken := flag.String("t", "", "部署 Token（用于首次部署或升级）")
	serverAddr := flag.String("s", "", "Server 地址（部署模式必填）")

	flag.Parse()

	// 显示版本信息
	if *showVersion || *showVersionLong {
		fmt.Printf("AWECloud Signaling Agent\n")
		fmt.Printf("Version:    %s\n", version)
		fmt.Printf("Git Commit: %s\n", gitCommit)
		fmt.Printf("Build Date: %s\n", buildDate)
		fmt.Printf("Go Version: %s\n", goVersion)
		os.Exit(0)
	}

	// 打印启动横幅
	banner.Print(banner.BuildInfo{
		AppName:   "AWECloud Signaling Agent",
		Version:   version,
		GitCommit: gitCommit,
		BuildDate: buildDate,
		GoVersion: goVersion,
	})

	var cfg *config.AgentConfig
	var err error
	var registerResult *config.RegisterResult

	if *deployToken != "" {
		if *serverAddr == "" {
			log.Fatalf("部署模式需要指定: -s <server_address>")
		}

		fmt.Printf("进入部署模式...\n")
		fmt.Printf("Server Address: %s\n", *serverAddr)
	}
	cfg, registerResult, err = resolveAgentStartupConfig(*configPath, *deployToken, *serverAddr, registerWithToken)
	if err != nil {
		log.Fatalf("加载 Agent 配置失败: %v", err)
	}
	if *deployToken != "" {
		if err := saveAgentConfig(*configPath, cfg); err != nil {
			fmt.Printf("警告: 保存配置文件失败: %v（将使用内存配置继续运行）\n", err)
		} else {
			fmt.Printf("配置已保存到: %s\n", *configPath)
		}
	}

	// 初始化日志
	logFile := cfg.Log.File
	// 如果配置文件中没有指定日志文件，只输出到标准输出（不创建 logs 目录）
	if err := logger.InitLogrus(cfg.Log.Level, logFile); err != nil {
		log.Fatalf("初始化日志失败: %v", err)
	}

	// 重定向标准库 log 到 logrus（用于 tsnet 等第三方库）
	log.SetOutput(logger.NewLogrusWriter())
	log.SetFlags(0) // 不需要时间戳，logrus 会添加

	// 应用配置优先级：环境变量 > 配置文件 > BUILD_URL > 默认值
	// 注意：SIGNAL_SERVER/AGENT_ADDRESS 已在 LoadAgentConfig 中处理
	// 这里只处理 BUILD_URL 兜底逻辑
	if cfg.Agent.Server == "" && BUILD_URL != "" {
		cfg.Agent.Server = BUILD_URL
		logger.Infof("使用编译时注入的 BUILD_URL: %s", BUILD_URL)
	} else if cfg.Agent.Server == "" {
		cfg.Agent.Server = "http://localhost:8080"
		logger.Infof("使用默认 Server 地址: http://localhost:8080")
	}

	logger.Infof("Server Address: %s", cfg.Agent.Server)

	// 初始化 OpenTelemetry
	hostname, _ := os.Hostname()
	if err := telemetry.Init(telemetry.Config{
		Endpoint:    cfg.Telemetry.Endpoint,
		ServiceName: cfg.Telemetry.Name,
		Namespace:   cfg.Telemetry.Namespace,
		Cluster:     cfg.Telemetry.Cluster,
	}, &telemetry.BuildInfo{
		Version:   version,
		GitCommit: gitCommit,
		BuildDate: buildDate,
		GoVersion: goVersion,
	}, &telemetry.ProcessAttributes{
		Node: hostname, // 使用 hostname 作为节点标识
	}); err != nil {
		logger.Warnf("初始化 OpenTelemetry 失败: %v", err)
	} else {
		// 初始化 gRPC trace 限流器（每分钟每方法最多 10 条）
		telemetry.InitGRPCLimiter(10)

		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := telemetry.Shutdown(ctx); err != nil {
				logger.Warnf("关闭 OpenTelemetry 失败: %v", err)
			}
		}()
	}

	// 创建并启动Agent
	agt, err := agent.NewAgent(cfg, version, gitCommit, buildDate)
	if err != nil {
		logger.Fatalf("创建Agent失败: %v", err)
	}

	// 根据注册结果决定运行模式
	if registerResult != nil && registerResult.IsClientMode() {
		// Client 模式（CloudIDE 等）：只启动 tsnet + SSH，不启动 gRPC 心跳
		logger.Infof("以 Client 模式运行（user_role=%s）", registerResult.UserRole)
		if err := agt.RunClient(registerResult); err != nil {
			logger.Fatalf("Client 模式运行失败: %v", err)
		}
	} else {
		// Agent 模式：完整的 gRPC 注册 + 心跳 + ProxyManager + VisitorManager
		if err := agt.Run(); err != nil {
			logger.Fatalf("Agent运行失败: %v", err)
		}
	}
}

func runTailscaleChildIfRequested() {
	if len(os.Args) < 2 || os.Args[1] != "be-child" {
		return
	}
	if err := runTailscaleChild(os.Args[2:]); err != nil {
		log.Printf("tailscale child process failed: %v", err)
		os.Exit(1)
	}
	os.Exit(0)
}

func runTailscaleChild(args []string) error {
	if len(args) == 0 {
		return errors.New("missing be-child mode")
	}
	fn, ok := childproc.Code[args[0]]
	if !ok {
		return fmt.Errorf("unknown be-child mode %q", args[0])
	}
	return fn(args[1:])
}

type agentTokenRegistrar func(serverAddr, token string) (*config.AgentConfig, *config.RegisterResult, error)

func resolveAgentStartupConfig(configPath, deployToken, serverAddr string, registrar agentTokenRegistrar) (*config.AgentConfig, *config.RegisterResult, error) {
	if deployToken == "" {
		cfg, err := config.LoadAgentConfig(configPath)
		return cfg, nil, err
	}
	if serverAddr == "" {
		return nil, nil, fmt.Errorf("部署模式需要指定 Server 地址")
	}
	cfg, result, err := registrar(serverAddr, deployToken)
	if err != nil {
		return nil, nil, err
	}
	if local, err := config.LoadAgentConfig(configPath); err == nil {
		mergeLocalAgentConfig(cfg, local)
	} else if !os.IsNotExist(err) {
		return nil, nil, err
	}
	return cfg, result, nil
}

func mergeLocalAgentConfig(cfg, local *config.AgentConfig) {
	cfg.Tunnel.EnableSSH = local.Tunnel.EnableSSH
	cfg.Tunnel.StateDir = local.Tunnel.StateDir
	cfg.Tunnel.StateSyncInterval = local.Tunnel.StateSyncInterval
	cfg.CloudIDE = local.CloudIDE
	cfg.Health = local.Health
	cfg.Log = local.Log
	cfg.Telemetry = local.Telemetry
	cfg.K8S = local.K8S
	cfg.SVC = local.SVC
	cfg.Container = local.Container
}

// registerWithToken 使用部署 Token 向 Server 注册
func registerWithToken(serverAddr, token string) (*config.AgentConfig, *config.RegisterResult, error) {
	// 生成设备指纹
	fingerprint := generateDeviceFingerprint()

	// 构建请求
	reqBody := map[string]string{
		"token":              token,
		"device_fingerprint": fingerprint,
	}
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	// 发送注册请求（统一注册接口）
	url := strings.TrimSuffix(serverAddr, "/") + "/api/v1/register"
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("读取响应失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &errResp) == nil && errResp.Error != "" {
			return nil, nil, fmt.Errorf("%s", errResp.Error)
		}
		return nil, nil, fmt.Errorf("注册失败: HTTP %d", resp.StatusCode)
	}

	// 解析响应
	var result struct {
		Success bool `json:"success"`
		Data    struct {
			Message      string                 `json:"message"`
			UserRole     string                 `json:"user_role"`
			UserID       uint64                 `json:"user_id"`
			Config       map[string]interface{} `json:"config"`
			HeadscaleURL string                 `json:"headscale_url"`
			AuthKey      string                 `json:"auth_key"`
			UserName     string                 `json:"user_name"`
			DeviceName   string                 `json:"device_name"` // 设备名称（Node.Name）
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, nil, fmt.Errorf("解析响应失败: %w", err)
	}

	if !result.Success {
		return nil, nil, fmt.Errorf("注册失败")
	}

	fmt.Printf("注册成功: %s (role=%s, user=%s)\n", result.Data.Message, result.Data.UserRole, result.Data.UserName)

	// 构建注册结果
	regResult := &config.RegisterResult{
		UserRole:     result.Data.UserRole,
		UserID:       result.Data.UserID,
		HeadscaleURL: result.Data.HeadscaleURL,
		AuthKey:      result.Data.AuthKey,
		UserName:     result.Data.UserName,
		DeviceName:   result.Data.DeviceName, // 保存 Server 返回的设备名称
	}

	// 构建配置
	cfg := &config.AgentConfig{
		Agent: config.AgentSection{
			AgentToken: token,
			Server:     serverAddr,
		},
	}

	return cfg, regResult, nil
}

// saveAgentConfig 保存配置到 TOML 文件
func saveAgentConfig(path string, cfg *config.AgentConfig) error {
	// 创建目录
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}

	// 序列化为 TOML
	var buf bytes.Buffer
	encoder := toml.NewEncoder(&buf)
	if err := encoder.Encode(cfg); err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}

	// 写入文件
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("写入文件失败: %w", err)
	}

	return nil
}

// generateDeviceFingerprint 生成设备指纹
// 统一使用 hostname 的 SHA256 哈希。
// hostname 在各场景下都稳定：Desktop 是用户机器名，Agent 是主机名，
// CloudIDE 是平台分配的 Pod 名。不使用 machine-id（容器重建后会变）。
func generateDeviceFingerprint() string {
	hostname, _ := os.Hostname()
	hash := sha256.Sum256([]byte(hostname))
	return hex.EncodeToString(hash[:])
}

// runDial 处理 dial 子命令
// 连接 Agent/Client 进程的 Unix Socket，请求 tsnet 代理转发
// 用法：signal_agent dial <host> <port>
// 协议：发送 [2字节大端长度][host:port]，读取 [1字节状态码]，然后桥接 stdin/stdout
func runDial(host, port string) {
	// 确定 Unix Socket 路径
	socketPath := os.Getenv("SIGNAL_DIAL_SOCKET")
	if socketPath == "" {
		socketPath = "/tmp/signaling.sock"
	}

	// 构建目标地址
	targetAddr := net.JoinHostPort(host, port)

	// 连接 Unix Socket
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "连接 Unix Socket 失败 (%s): %v\n", socketPath, err)
		os.Exit(1)
	}
	defer conn.Close()

	// 发送目标地址（2字节大端长度 + 地址字符串）
	addrBytes := []byte(targetAddr)
	if len(addrBytes) > 512 {
		fmt.Fprintf(os.Stderr, "目标地址过长: %s\n", targetAddr)
		os.Exit(1)
	}
	lenBuf := make([]byte, 2)
	binary.BigEndian.PutUint16(lenBuf, uint16(len(addrBytes)))
	if _, err := conn.Write(lenBuf); err != nil {
		fmt.Fprintf(os.Stderr, "发送地址长度失败: %v\n", err)
		os.Exit(1)
	}
	if _, err := conn.Write(addrBytes); err != nil {
		fmt.Fprintf(os.Stderr, "发送地址失败: %v\n", err)
		os.Exit(1)
	}

	// 读取状态码（0x00 成功，0x01 失败）
	statusBuf := make([]byte, 1)
	if _, err := io.ReadFull(conn, statusBuf); err != nil {
		fmt.Fprintf(os.Stderr, "读取状态码失败: %v\n", err)
		os.Exit(1)
	}
	if statusBuf[0] != 0x00 {
		fmt.Fprintf(os.Stderr, "代理连接失败: 目标 %s 不可达\n", targetAddr)
		os.Exit(1)
	}

	// 桥接 stdin/stdout ↔ Unix Socket
	done := make(chan struct{}, 2)
	go func() {
		io.Copy(conn, os.Stdin)
		done <- struct{}{}
	}()
	go func() {
		io.Copy(os.Stdout, conn)
		done <- struct{}{}
	}()

	// 等待任一方向完成
	<-done
}
