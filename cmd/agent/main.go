package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
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
)

var (
	version   = "dev"
	gitCommit = "unknown"
	buildDate = "unknown"
	goVersion = "unknown"
	BUILD_URL = "" // 编译时注入的默认 Server 地址
)

func main() {
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

	// 检查是否是 be-child ssh 子命令
	if len(os.Args) >= 3 && os.Args[1] == "be-child" && os.Args[2] == "ssh" {
		runSSHChild()
		return
	}

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

	// 检查是否为部署模式
	if *deployToken != "" {
		// 部署模式：使用 Token 向 Server 注册（命令行参数，通常是 install_agent.sh 调用）
		if *serverAddr == "" {
			log.Fatalf("部署模式需要指定: -s <server_address>")
		}

		fmt.Printf("进入部署模式...\n")
		fmt.Printf("Server Address: %s\n", *serverAddr)

		// 向 Server 注册并获取配置
		cfg, registerResult, err = registerWithToken(*serverAddr, *deployToken)
		if err != nil {
			log.Fatalf("部署注册失败: %v", err)
		}

		// 保存配置到文件
		if err := saveAgentConfig(*configPath, cfg); err != nil {
			fmt.Printf("警告: 保存配置文件失败: %v（将使用内存配置继续运行）\n", err)
		} else {
			fmt.Printf("配置已保存到: %s\n", *configPath)
		}
	} else {
		// 正常模式：从配置文件加载
		cfg, err = config.LoadAgentConfig(*configPath)
		if err != nil {
			log.Fatalf("加载配置失败: %v", err)
		}

		// 检查是否有 SIGNAL_TOKEN 环境变量（Token 自动注册模式，CloudIDE 等场景）
		if cfg.Agent.AgentToken != "" {
			// Token 注册模式
			srvAddr := cfg.Agent.Server
			if srvAddr == "" && BUILD_URL != "" {
				srvAddr = BUILD_URL
			}
			if srvAddr == "" {
				log.Fatalf("Token 注册模式需要 SIGNAL_SERVER 环境变量")
			}

			hostname, _ := os.Hostname()
			fmt.Printf("进入 Token 注册模式...\n")
			fmt.Printf("Server Address: %s\n", srvAddr)
			fmt.Printf("Device: %s\n", hostname)

			cfg, registerResult, err = registerWithToken(srvAddr, cfg.Agent.AgentToken)
			if err != nil {
				log.Fatalf("Token 注册失败: %v", err)
			}

			// 合并环境变量中的其他配置（SSH、SOCKS 等）
			envCfg, _ := config.LoadAgentConfig(*configPath)
			if envCfg != nil {
				cfg.Tunnel.EnableSSH = envCfg.Tunnel.EnableSSH
				cfg.Tunnel.StateDir = envCfg.Tunnel.StateDir
				cfg.Tunnel.StateSyncInterval = envCfg.Tunnel.StateSyncInterval
				cfg.CloudIDE = envCfg.CloudIDE
				cfg.Health = envCfg.Health
				cfg.Log = envCfg.Log
				cfg.Telemetry = envCfg.Telemetry
				cfg.K8S = envCfg.K8S
				cfg.SVC = envCfg.SVC
			}
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
	agt, err := agent.NewAgent(cfg, version, buildDate)
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

// runSSHChild 处理 be-child ssh 子命令
// 这个命令由 Tailscale SSH 调用，用于在指定用户身份下启动 shell
func runSSHChild() {
	// 解析命令行参数
	var (
		loginShell string
		uid        int = -1
		gid        int = -1
		groups     []int
		homeDir    string
		localUser  string
		remoteUser string
		remoteIP   string
		shell      bool
		cmd        string // 命令执行模式的命令
	)

	// 解析参数
	for i := 3; i < len(os.Args); i++ {
		arg := os.Args[i]

		// 处理 --key=value 格式
		if idx := strings.Index(arg, "="); idx > 0 {
			key := arg[:idx]
			value := arg[idx+1:]

			switch key {
			case "--login-shell":
				loginShell = value
			case "--uid":
				fmt.Sscanf(value, "%d", &uid)
			case "--gid":
				fmt.Sscanf(value, "%d", &gid)
			case "--groups":
				for _, gStr := range splitString(value, ',') {
					var g int
					if _, err := fmt.Sscanf(gStr, "%d", &g); err == nil {
						groups = append(groups, g)
					}
				}
			case "--home-dir":
				homeDir = value
			case "--local-user":
				localUser = value
			case "--remote-user":
				remoteUser = value
			case "--remote-ip":
				remoteIP = value
			case "--cmd":
				// Tailscale SSH 传递的命令参数
				cmd = value
			}
			continue
		}

		// 处理 --key value 格式和标志
		if arg == "--login-shell" && i+1 < len(os.Args) {
			loginShell = os.Args[i+1]
			i++
		} else if arg == "--uid" && i+1 < len(os.Args) {
			fmt.Sscanf(os.Args[i+1], "%d", &uid)
			i++
		} else if arg == "--gid" && i+1 < len(os.Args) {
			fmt.Sscanf(os.Args[i+1], "%d", &gid)
			i++
		} else if arg == "--groups" && i+1 < len(os.Args) {
			groupsStr := os.Args[i+1]
			for _, gStr := range splitString(groupsStr, ',') {
				var g int
				if _, err := fmt.Sscanf(gStr, "%d", &g); err == nil {
					groups = append(groups, g)
				}
			}
			i++
		} else if arg == "--home-dir" && i+1 < len(os.Args) {
			homeDir = os.Args[i+1]
			i++
		} else if arg == "--local-user" && i+1 < len(os.Args) {
			localUser = os.Args[i+1]
			i++
		} else if arg == "--remote-user" && i+1 < len(os.Args) {
			remoteUser = os.Args[i+1]
			i++
		} else if arg == "--remote-ip" && i+1 < len(os.Args) {
			remoteIP = os.Args[i+1]
			i++
		} else if arg == "--cmd" && i+1 < len(os.Args) {
			// Tailscale SSH 传递的命令参数
			cmd = os.Args[i+1]
			i++
		} else if arg == "--shell" {
			shell = true
		}
	}

	// 如果没有指定 login-shell，使用默认值
	if loginShell == "" {
		loginShell = "/bin/bash"
	}

	// 切换用户身份（如果指定了 uid/gid）
	if err := switchUserIdentity(uid, gid, groups); err != nil {
		fmt.Fprintf(os.Stderr, "failed to switch user identity: %v\n", err)
		os.Exit(1)
	}

	// 设置环境变量
	env := os.Environ()
	if homeDir != "" {
		env = append(env, "HOME="+homeDir)
	}
	if localUser != "" {
		env = append(env, "USER="+localUser)
		env = append(env, "LOGNAME="+localUser)
	}

	// 切换到用户主目录
	if homeDir != "" {
		if err := os.Chdir(homeDir); err != nil {
			fmt.Fprintf(os.Stderr, "failed to chdir to %s: %v\n", homeDir, err)
		}
	}

	// 只在交互式会话（--shell 参数）时显示横幅
	// 命令执行会话（--cmd 参数）不显示横幅，避免污染命令输出
	if shell {
		printSSHBanner(localUser, remoteUser, remoteIP)
	}

	// 使用平台特定的方式启动 shell
	// 如果有 --cmd 参数，执行命令；否则启动交互式 shell
	if err := execShell(loginShell, env, cmd); err != nil {
		fmt.Fprintf(os.Stderr, "failed to exec shell: %v\n", err)
		os.Exit(1)
	}
}

// splitString 分割字符串
func splitString(s string, sep rune) []string {
	var result []string
	var current string
	for _, c := range s {
		if c == sep {
			if current != "" {
				result = append(result, current)
				current = ""
			}
		} else {
			current += string(c)
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}

// printSSHBanner 打印 SSH 登录横幅
func printSSHBanner(localUser, remoteUser, remoteIP string) {
	// 提取真实的用户名（去掉 tag: 前缀）
	displayRemoteUser := extractRealUser(remoteUser)

	// 获取当前时间
	connectTime := time.Now().Format("2006-01-02 15:04:05")

	fmt.Println("================================================================")
	fmt.Println("           AWECloud Signaling - SSH Access")
	fmt.Println("================================================================")
	fmt.Printf("  Version:      %s\n", version)
	fmt.Printf("  Build Date:   %s\n", buildDate)
	fmt.Printf("  Connect Time: %s\n", connectTime)
	fmt.Println("----------------------------------------------------------------")

	// 构建 Remote User 行
	if displayRemoteUser != "" {
		fmt.Printf("  Remote User:   %s\n", displayRemoteUser)
	}

	// 构建 Remote Device 行
	if remoteIP != "" {
		fmt.Printf("  Remote Device: %s\n", remoteIP)
	}

	fmt.Println("================================================================")
	fmt.Println()
}

// extractRealUser 从 Tailscale 用户标签中提取真实用户名
// 输入: "tag:desktop-group-dev,tag:desktop-shucheng@bd-apaas.com"
// 输出: "shucheng@bd-apaas.com"
func extractRealUser(remoteUser string) string {
	if remoteUser == "" {
		return ""
	}

	// 按逗号分割多个标签
	tags := strings.Split(remoteUser, ",")
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)

		// 查找以 "tag:client-" 开头的标签（优先）
		if strings.HasPrefix(tag, "tag:client-") {
			// 去掉 "tag:client-" 前缀
			return strings.TrimPrefix(tag, "tag:client-")
		}

		// 查找以 "tag:desktop-" 开头但不是 "tag:desktop-group-" 的标签
		if strings.HasPrefix(tag, "tag:desktop-") && !strings.HasPrefix(tag, "tag:desktop-group-") {
			// 去掉 "tag:desktop-" 前缀
			return strings.TrimPrefix(tag, "tag:desktop-")
		}
	}

	// 如果没找到，返回原始值
	return remoteUser
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
