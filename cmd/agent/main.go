package main

import (
	"flag"
	"log"

	"github.com/open-beagle/awecloud-signaling-server/internal/agent"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
)

func main() {
	configPath := flag.String("c", "config/agent.toml", "配置文件路径")
	flag.Parse()

	// 加载配置
	cfg, err := config.LoadAgentConfig(*configPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	log.Printf("AWECloud Signaling Agent 启动中...")
	log.Printf("Agent Name: %s", cfg.Agent.AgentName)
	log.Printf("Server: %s:%d", cfg.Server.Address, cfg.Server.Port)

	// 创建并启动Agent
	agt, err := agent.NewAgent(cfg)
	if err != nil {
		log.Fatalf("创建Agent失败: %v", err)
	}

	if err := agt.Run(); err != nil {
		log.Fatalf("Agent运行失败: %v", err)
	}
}
