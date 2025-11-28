package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/open-beagle/awecloud-signaling-server/internal/agent"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
)

var (
	version   = "dev"
	gitCommit = "unknown"
	buildDate = "unknown"
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
		os.Exit(0)
	}

	// 加载配置
	cfg, err := config.LoadAgentConfig(*configPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 应用配置优先级：环境变量 > 配置文件 > BUILD_URL > 默认值
	if addr := os.Getenv("AGENT_ADDRESS"); addr != "" {
		cfg.Server.Address = addr
		log.Printf("使用环境变量 AGENT_ADDRESS: %s", addr)
	} else if cfg.Server.Address == "" && BUILD_URL != "" {
		cfg.Server.Address = BUILD_URL
		log.Printf("使用编译时注入的 BUILD_URL: %s", BUILD_URL)
	} else if cfg.Server.Address == "" {
		cfg.Server.Address = "http://localhost:8080"
		log.Printf("使用默认 Server 地址: http://localhost:8080")
	}

	log.Printf("AWECloud Signaling Agent 启动中...")
	log.Printf("版本: %s (commit: %s, built: %s)", version, gitCommit, buildDate)
	log.Printf("Agent Name: %s", cfg.Agent.AgentName)
	log.Printf("Server Address: %s", cfg.Server.Address)

	// 创建并启动Agent
	agt, err := agent.NewAgent(cfg)
	if err != nil {
		log.Fatalf("创建Agent失败: %v", err)
	}

	if err := agt.Run(); err != nil {
		log.Fatalf("Agent运行失败: %v", err)
	}
}
