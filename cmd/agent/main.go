package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/open-beagle/awecloud-signaling-server/internal/agent"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/banner"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/telemetry"
)

var (
	version   = "dev"
	gitCommit = "unknown"
	buildDate = "unknown"
	goVersion = "unknown"
	BUILD_URL = "" // 编译时注入的默认 Server 地址
)

func main() {
	// 检查是否是 be-child ssh 子命令
	if len(os.Args) >= 3 && os.Args[1] == "be-child" && os.Args[2] == "ssh" {
		runSSHChild()
		return
	}

	configPath := flag.String("c", "config/agent.toml", "配置文件路径")
	showVersion := flag.Bool("v", false, "显示版本信息")
	showVersionLong := flag.Bool("version", false, "显示版本信息")

	// 部署模式参数
	deployToken := flag.String("t", "", "部署 Token（用于首次部署或升级）")
	agentName := flag.String("n", "", "Agent 名称（部署模式必填）")
	deviceName := flag.String("d", "", "设备名称（部署模式必填）")
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

	// 检查是否为部署模式
	if *deployToken != "" {
		// 部署模式：使用 Token 向 Server 注册
		if *agentName == "" || *deviceName == "" || *serverAddr == "" {
			log.Fatalf("部署模式需要指定: -n <agent_name> -d <device_name> -s <server_address>")
		}

		fmt.Printf("进入部署模式...\n")
		fmt.Printf("Agent Name: %s\n", *agentName)
		fmt.Printf("Device Name: %s\n", *deviceName)
		fmt.Printf("Server Address: %s\n", *serverAddr)

		// 向 Server 注册并获取配置
		cfg, err = registerWithToken(*serverAddr, *deployToken, *agentName, *deviceName)
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
	}

	// 初始化日志
	logFile := cfg.Log.File
	if logFile == "" {
		logFile = "logs/agent.log"
	}
	if err := logger.InitLogrus(cfg.Log.Level, logFile); err != nil {
		log.Fatalf("初始化日志失败: %v", err)
	}

	// 重定向标准库 log 到 logrus（用于 tsnet 等第三方库）
	log.SetOutput(logger.NewLogrusWriter())
	log.SetFlags(0) // 不需要时间戳，logrus 会添加

	// 应用配置优先级：环境变量 > 配置文件 > BUILD_URL > 默认值
	if addr := os.Getenv("AGENT_ADDRESS"); addr != "" {
		cfg.Server.Address = addr
		logger.Infof("使用环境变量 AGENT_ADDRESS: %s", addr)
	} else if cfg.Server.Address == "" && BUILD_URL != "" {
		cfg.Server.Address = BUILD_URL
		logger.Infof("使用编译时注入的 BUILD_URL: %s", BUILD_URL)
	} else if cfg.Server.Address == "" {
		cfg.Server.Address = "http://localhost:8080"
		logger.Infof("使用默认 Server 地址: http://localhost:8080")
	}

	logger.Infof("Agent Name: %s", cfg.Agent.AgentName)
	logger.Infof("Server Address: %s", cfg.Server.Address)

	// 初始化 OpenTelemetry
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
		Node: cfg.Agent.AgentName, // 使用 AgentName 作为节点标识
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
	agt, err := agent.NewAgent(cfg, version)
	if err != nil {
		logger.Fatalf("创建Agent失败: %v", err)
	}

	if err := agt.Run(); err != nil {
		logger.Fatalf("Agent运行失败: %v", err)
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
		} else if arg == "--shell" {
			shell = true
		}
	}

	// 如果没有指定 login-shell，使用默认值
	if loginShell == "" {
		loginShell = "/bin/bash"
	}

	// 如果指定了 --shell，启动交互式 shell
	if shell {
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

		// 显示登录横幅
		printSSHBanner(localUser, remoteUser, remoteIP)

		// 使用平台特定的方式启动 shell
		if err := execShell(loginShell, env); err != nil {
			fmt.Fprintf(os.Stderr, "failed to exec shell: %v\n", err)
			os.Exit(1)
		}
	}

	// 如果没有 --shell 参数，直接退出（不应该到这里）
	os.Exit(0)
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

	fmt.Println("================================================================")
	fmt.Println("           AWECloud Signaling - SSH Access")
	fmt.Println("================================================================")
	fmt.Printf("  Version:     %s\n", version)
	fmt.Printf("  Build Date:  %s\n", buildDate)
	fmt.Printf("  Git Commit:  %s\n", gitCommit)
	fmt.Println("----------------------------------------------------------------")
	if displayRemoteUser != "" {
		fmt.Printf("  Remote User: %s\n", displayRemoteUser)
	}
	if remoteIP != "" {
		fmt.Printf("  Remote IP:   %s\n", remoteIP)
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
func registerWithToken(serverAddr, token, agentName, deviceName string) (*config.AgentConfig, error) {
	// 生成设备指纹
	fingerprint := generateDeviceFingerprint()

	// 构建请求
	reqBody := map[string]string{
		"token":              token,
		"device_fingerprint": fingerprint,
	}
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	// 发送注册请求
	url := strings.TrimSuffix(serverAddr, "/") + "/api/v1/agent/register"
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &errResp) == nil && errResp.Error != "" {
			return nil, fmt.Errorf("%s", errResp.Error)
		}
		return nil, fmt.Errorf("注册失败: HTTP %d", resp.StatusCode)
	}

	// 解析响应
	var result struct {
		Success bool `json:"success"`
		Data    struct {
			Message      string                 `json:"message"`
			Config       map[string]interface{} `json:"config"`
			HeadscaleURL string                 `json:"headscale_url"`
			AuthKey      string                 `json:"auth_key"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	if !result.Success {
		return nil, fmt.Errorf("注册失败")
	}

	fmt.Printf("注册成功: %s\n", result.Data.Message)

	// 构建配置
	cfg := &config.AgentConfig{
		Agent: config.AgentSection{
			AgentName: agentName,
			Device:    deviceName,
		},
		Server: config.ServerConnect{
			Address: serverAddr,
		},
	}

	return cfg, nil
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
func generateDeviceFingerprint() string {
	// 收集设备信息
	hostname, _ := os.Hostname()
	info := fmt.Sprintf("%s-%s-%s-%s",
		hostname,
		runtime.GOOS,
		runtime.GOARCH,
		getMachineID(),
	)

	// 计算 SHA256 哈希
	hash := sha256.Sum256([]byte(info))
	return hex.EncodeToString(hash[:])
}

// getMachineID 获取机器 ID
func getMachineID() string {
	// Linux: /etc/machine-id
	if data, err := os.ReadFile("/etc/machine-id"); err == nil {
		return strings.TrimSpace(string(data))
	}

	// macOS: 使用 IOPlatformUUID（简化处理，使用 hostname）
	// Windows: 使用注册表（简化处理，使用 hostname）
	hostname, _ := os.Hostname()
	return hostname
}
