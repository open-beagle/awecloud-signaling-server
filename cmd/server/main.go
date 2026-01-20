package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/banner"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/telemetry"
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
	showVersionLong := flag.Bool("version", false, "显示版本信息")
	flag.Parse()

	// 显示版本信息
	if *showVersion || *showVersionLong {
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

	// 初始化 OpenTelemetry
	if err := telemetry.Init(telemetry.Config{
		Endpoint:    cfg.Telemetry.Endpoint,
		ServiceName: cfg.Telemetry.ServiceName,
		Namespace:   cfg.Telemetry.Namespace,
	}, telemetry.BuildInfo{
		Version:   version,
		GitCommit: gitCommit,
		BuildDate: buildDate,
		GoVersion: goVersion,
	}); err != nil {
		logger.Warnf("初始化 OpenTelemetry 失败: %v", err)
	} else if cfg.Telemetry.Endpoint != "" {
		// 初始化 gRPC trace 限流器（每分钟每方法最多 10 条）
		telemetry.InitGRPCLimiter(10)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := telemetry.Shutdown(ctx); err != nil {
			logger.Warnf("关闭 OpenTelemetry 失败: %v", err)
		}
	}()

	// 创建并启动服务器
	srv, err := server.NewServer(cfg)
	if err != nil {
		logger.Fatalf("创建服务器失败: %v", err)
	}

	if err := srv.Run(); err != nil {
		logger.Fatalf("服务器运行失败: %v", err)
	}
}
