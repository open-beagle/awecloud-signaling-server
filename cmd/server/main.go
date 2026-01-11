package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/banner"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server"
)

var (
	version   = "dev"
	gitCommit = "unknown"
	buildDate = "unknown"
	goVersion = "unknown"
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
		fmt.Printf("Go Version: %s\n", goVersion)
		os.Exit(0)
	}

	// 打印启动横幅
	banner.Print(banner.BuildInfo{
		AppName:   "AWECloud Signaling Server",
		Version:   version,
		GitCommit: gitCommit,
		BuildDate: buildDate,
		GoVersion: goVersion,
	})

	// 加载配置
	cfg, err := config.LoadServerConfig(*configPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 初始化日志
	if err := logger.InitLogrus(cfg.Log.Level, cfg.Log.File); err != nil {
		log.Fatalf("初始化日志失败: %v", err)
	}

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
