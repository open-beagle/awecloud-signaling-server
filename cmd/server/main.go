package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server"
)

var (
	version   = "dev"
	gitCommit = "unknown"
	buildDate = "unknown"
)

func main() {
	configPath := flag.String("c", "config/server.toml", "配置文件路径")
	showVersion := flag.Bool("v", false, "显示版本信息")
	flag.Parse()

	// 显示版本信息
	if *showVersion {
		fmt.Printf("AWECloud Signaling Server\n")
		fmt.Printf("Version:    %s\n", version)
		fmt.Printf("Git Commit: %s\n", gitCommit)
		fmt.Printf("Build Date: %s\n", buildDate)
		os.Exit(0)
	}

	// 加载配置
	cfg, err := config.LoadServerConfig(*configPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 初始化日志
	if err := logger.InitLogrus(cfg.Log.Level, cfg.Log.File); err != nil {
		log.Fatalf("初始化日志失败: %v", err)
	}

	logger.Infof("AWECloud Signaling Server 启动中...")
	logger.Infof("版本: %s (commit: %s, built: %s)", version, gitCommit, buildDate)
	logger.Infof("配置文件: %s", *configPath)

	// 创建并启动服务器
	srv, err := server.NewServer(cfg)
	if err != nil {
		logger.Fatalf("创建服务器失败: %v", err)
	}

	if err := srv.Run(); err != nil {
		logger.Fatalf("服务器运行失败: %v", err)
	}
}
