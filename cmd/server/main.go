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
	if err := initLogger(cfg.Log); err != nil {
		log.Fatalf("初始化日志失败: %v", err)
	}

	log.Printf("AWECloud Signaling Server 启动中...")
	log.Printf("版本: %s (commit: %s, built: %s)", version, gitCommit, buildDate)
	log.Printf("配置文件: %s", *configPath)

	// 创建并启动服务器
	srv, err := server.NewServer(cfg)
	if err != nil {
		log.Fatalf("创建服务器失败: %v", err)
	}

	if err := srv.Run(); err != nil {
		log.Fatalf("服务器运行失败: %v", err)
	}
}

func initLogger(cfg config.LogConfig) error {
	logFile := cfg.File
	if logFile == "" {
		logFile = "logs/server.log"
	}

	// 创建日志目录
	if err := os.MkdirAll("logs", 0755); err != nil {
		return fmt.Errorf("创建日志目录失败: %w", err)
	}

	// 设置轮转日志
	_, err := logger.SetupLogger(logFile)
	if err != nil {
		return fmt.Errorf("设置日志失败: %w", err)
	}

	log.Printf("日志输出到: %s (最大5000行，自动轮转)", logFile)
	return nil
}
