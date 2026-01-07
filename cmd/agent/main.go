package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/open-beagle/awecloud-signaling-server/internal/agent"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
)

var (
	version   = "dev"
	gitCommit = "unknown"
	buildDate = "unknown"
	goVersion = "unknown"
	BUILD_URL = "" // 编译时注入的默认 Server 地址
)

func main() {
	configPath := flag.String("c", "config/agent.toml", "配置文件路径")
	showVersion := flag.Bool("v", false, "显示版本信息")
	flag.Parse()

	// 显示版本信息
	if *showVersion {
		fmt.Printf("AWECloud Signaling Agent\n")
		fmt.Printf("Version:    %s\n", version)
		fmt.Printf("Git Commit: %s\n", gitCommit)
		fmt.Printf("Build Date: %s\n", buildDate)
		fmt.Printf("Go Version: %s\n", goVersion)
		os.Exit(0)
	}

	// 加载配置
	cfg, err := config.LoadAgentConfig(*configPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 初始化日志
	logFile := cfg.Log.File
	if logFile == "" {
		logFile = "logs/agent.log"
	}
	if err := logger.InitLogrus(cfg.Log.Level, logFile); err != nil {
		log.Fatalf("初始化日志失败: %v", err)
	}

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

	logger.Infof("AWECloud Signaling Agent 启动中...")
	logger.Infof("版本: %s (commit: %s, built: %s, go: %s)", version, gitCommit, buildDate, goVersion)
	logger.Infof("Agent Name: %s", cfg.Agent.AgentName)
	logger.Infof("Server Address: %s", cfg.Server.Address)

	// 创建并启动Agent
	agt, err := agent.NewAgent(cfg, version)
	if err != nil {
		logger.Fatalf("创建Agent失败: %v", err)
	}

	if err := agt.Run(); err != nil {
		logger.Fatalf("Agent运行失败: %v", err)
	}
}
