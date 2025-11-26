package frp

import (
	"context"
	"fmt"
	"log"

	v1 "github.com/fatedier/frp/pkg/config/v1"
	"github.com/fatedier/frp/server"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
)

type FRPServer struct {
	config *config.ServerConfig
	svr    *server.Service
	ctx    context.Context
	cancel context.CancelFunc
}

func NewFRPServer(cfg *config.ServerConfig) (*FRPServer, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// 创建FRP Server配置
	svrCfg := &v1.ServerConfig{
		BindAddr: cfg.Server.BindAddr,
		BindPort: cfg.Server.BindPort,
	}

	// 配置TLS
	if cfg.Server.TLSCertFile != "" && cfg.Server.TLSKeyFile != "" {
		svrCfg.Transport.TLS = v1.TLSServerConfig{
			Force: true, // 强制使用TLS
			TLSConfig: v1.TLSConfig{
				CertFile: cfg.Server.TLSCertFile,
				KeyFile:  cfg.Server.TLSKeyFile,
			},
		}
		log.Println("FRP Server TLS已启用")
	}

	// 创建FRP Server实例
	svr, err := server.NewService(svrCfg)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("创建FRP Server失败: %w", err)
	}

	return &FRPServer{
		config: cfg,
		svr:    svr,
		ctx:    ctx,
		cancel: cancel,
	}, nil
}

func (f *FRPServer) Run() error {
	log.Printf("FRP Server启动在: %s:%d", f.config.Server.BindAddr, f.config.Server.BindPort)
	log.Printf("传输协议: %s", f.config.Server.TransportProtocol)

	// 启动FRP Server（Run方法没有返回值，会阻塞直到context取消）
	f.svr.Run(f.ctx)

	return nil
}

func (f *FRPServer) Stop() error {
	log.Println("正在停止FRP Server...")
	f.cancel()
	return nil
}
