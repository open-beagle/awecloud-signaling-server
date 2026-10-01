package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/open-beagle/awecloud-signaling-server/internal/agent"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/banner"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
)

// TunnelConfig run-tunnel 运行配置
type TunnelConfig struct {
	ServerAddr  string
	DeployToken string
	TargetAgent string
	StateDir    string
}

// ParseTunnelFlags 解析 run-tunnel 命令行参数与环境变量（CLI 参数优先覆盖环境变量）
func ParseTunnelFlags(args []string, getenv func(string) string) (*TunnelConfig, error) {
	if getenv == nil {
		getenv = os.Getenv
	}

	fs := flag.NewFlagSet("run-tunnel", flag.ContinueOnError)

	// 从环境变量读取默认值
	envServer := getenv("SIGNAL_SERVER")
	envToken := getenv("SIGNAL_DEPLOY_TOKEN")
	if envToken == "" {
		envToken = getenv("SIGNAL_TOKEN")
	}
	envTarget := getenv("SIGNAL_TARGET_AGENT")
	envStateDir := getenv("SIGNAL_STATE_DIR")
	if envStateDir == "" {
		envStateDir = "/var/run/beagle-signal"
	}

	var serverShort, serverLong string
	var tokenShort, tokenLong string
	var targetAgent string
	var stateDir string

	fs.StringVar(&serverShort, "s", "", "Signal Server API/gRPC 地址")
	fs.StringVar(&serverLong, "server", "", "Signal Server API/gRPC 地址")
	fs.StringVar(&tokenShort, "t", "", "服务身份专属 Deploy Token")
	fs.StringVar(&tokenLong, "token", "", "服务身份专属 Deploy Token")
	fs.StringVar(&targetAgent, "target-agent", "", "目标边缘 Agent 名称（如 edge-gpu-5090）")
	fs.StringVar(&stateDir, "state-dir", "", "本地设备与证书凭据缓存目录")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	cfg := &TunnelConfig{
		ServerAddr:  envServer,
		DeployToken: envToken,
		TargetAgent: envTarget,
		StateDir:    envStateDir,
	}

	// 命令行参数具有最高优先级，覆盖环境变量
	if serverShort != "" {
		cfg.ServerAddr = serverShort
	} else if serverLong != "" {
		cfg.ServerAddr = serverLong
	}

	if tokenShort != "" {
		cfg.DeployToken = tokenShort
	} else if tokenLong != "" {
		cfg.DeployToken = tokenLong
	}

	if targetAgent != "" {
		cfg.TargetAgent = targetAgent
	}

	if stateDir != "" {
		cfg.StateDir = stateDir
	}

	// 必填参数校验：-s, -t, --target-agent
	var missing []string
	if cfg.ServerAddr == "" {
		missing = append(missing, "-s/--server (或环境变量 SIGNAL_SERVER)")
	}
	if cfg.DeployToken == "" {
		missing = append(missing, "-t/--token (或环境变量 SIGNAL_DEPLOY_TOKEN)")
	}
	if cfg.TargetAgent == "" {
		missing = append(missing, "--target-agent (或环境变量 SIGNAL_TARGET_AGENT)")
	}

	if len(missing) > 0 {
		return nil, fmt.Errorf("缺少必填参数: %s\n用法: signal_agent run-tunnel -s <server> -t <token> --target-agent <agent_name> [--state-dir <dir>]",
			strings.Join(missing, ", "))
	}

	return cfg, nil
}

// RegisterTunnelWithToken 使用部署 Token 向 Server 统一注册接口认证并进行双向防呆校验
func RegisterTunnelWithToken(serverAddr, token, targetAgent string) (*config.RegisterResult, error) {
	fingerprint := generateDeviceFingerprint()

	reqBody := map[string]string{
		"token":              token,
		"device_fingerprint": fingerprint,
		"target_agent":       targetAgent,
	}
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("序列化注册请求失败: %w", err)
	}

	url := strings.TrimSuffix(serverAddr, "/") + "/api/v1/register"
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("请求 Server 注册接口失败 (%s): %w", url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取 Server 注册响应失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &errResp) == nil && errResp.Error != "" {
			return nil, fmt.Errorf("注册失败 (HTTP %d): %s", resp.StatusCode, errResp.Error)
		}
		return nil, fmt.Errorf("注册失败: HTTP %d", resp.StatusCode)
	}

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
			DeviceName   string                 `json:"device_name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析 Server 注册响应失败: %w", err)
	}
	if !result.Success {
		return nil, fmt.Errorf("Server 返回注册未成功")
	}

	return &config.RegisterResult{
		UserRole:     result.Data.UserRole,
		UserID:       result.Data.UserID,
		HeadscaleURL: result.Data.HeadscaleURL,
		AuthKey:      result.Data.AuthKey,
		UserName:     result.Data.UserName,
		DeviceName:   result.Data.DeviceName,
	}, nil
}

// RunTunnelCLI 执行 run-tunnel 命令分支
func RunTunnelCLI(args []string) error {
	cfg, err := ParseTunnelFlags(args, os.Getenv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		return err
	}

	banner.Print(banner.BuildInfo{
		AppName:   "AWECloud Signal Tunnel",
		Version:   version,
		GitCommit: gitCommit,
		BuildDate: buildDate,
		GoVersion: goVersion,
	})

	fmt.Printf("[Tunnel] 正在向 Signal Server 注册 (Server=%s, TargetAgent=%s)...\n", cfg.ServerAddr, cfg.TargetAgent)
	regResult, err := RegisterTunnelWithToken(cfg.ServerAddr, cfg.DeployToken, cfg.TargetAgent)
	if err != nil {
		log.Printf("[FATAL] Tunnel 注册与防呆核验失败: %v", err)
		return err
	}

	fmt.Printf("[Tunnel] 注册成功: user=%s, role=%s, device=%s\n", regResult.UserName, regResult.UserRole, regResult.DeviceName)

	agentConfig := &config.AgentConfig{
		Agent: config.AgentSection{
			AgentToken: cfg.DeployToken,
			Server:     cfg.ServerAddr,
		},
		Tunnel: config.TunnelSection{
			StateDir: cfg.StateDir,
		},
		Log: config.LogConfig{
			Level: "info",
		},
	}

	if err := logger.InitLogrus(agentConfig.Log.Level, ""); err != nil {
		log.Fatalf("初始化日志失败: %v", err)
	}
	log.SetOutput(logger.NewLogrusWriter())
	log.SetFlags(0)

	agt, err := agent.NewAgent(agentConfig, version, gitCommit, gitCommitDate, buildDate)
	if err != nil {
		log.Fatalf("创建 Tunnel Agent 失败: %v", err)
	}

	return agt.RunTunnel(regResult, cfg.TargetAgent)
}
