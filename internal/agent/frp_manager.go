package agent

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/fatedier/frp/client"
	v1 "github.com/fatedier/frp/pkg/config/v1"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
)

// FRPManager FRP客户端管理器（Agent-FRP线程）
type FRPManager struct {
	config *config.AgentConfig

	// FRP客户端服务
	service *client.Service

	// 代理配置
	proxies map[string]*v1.STCPProxyConfig // instance_name -> proxy config
	mutex   sync.RWMutex

	// 上下文
	ctx    context.Context
	cancel context.CancelFunc
}

// NewFRPManager 创建FRP管理器
func NewFRPManager(cfg *config.AgentConfig, ctx context.Context) (*FRPManager, error) {
	frpCtx, cancel := context.WithCancel(ctx)

	// 创建FRP客户端配置
	clientCfg := v1.ClientCommonConfig{
		ServerAddr: cfg.Server.Address,
		ServerPort: cfg.Server.Port,
	}

	// 配置TLS
	if cfg.Server.TLSEnable {
		enable := true
		clientCfg.Transport.TLS.Enable = &enable
		log.Println("FRP客户端TLS已启用")
	}

	// 创建FRP客户端服务（使用空的代理配置列表）
	svr, err := client.NewService(client.ServiceOptions{
		Common:         &clientCfg,
		ProxyCfgs:      []v1.ProxyConfigurer{},
		VisitorCfgs:    []v1.VisitorConfigurer{},
		ConfigFilePath: "",
	})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("创建FRP客户端失败: %w", err)
	}

	return &FRPManager{
		config:  cfg,
		service: svr,
		proxies: make(map[string]*v1.STCPProxyConfig),
		ctx:     frpCtx,
		cancel:  cancel,
	}, nil
}

// Run 运行FRP管理器
func (f *FRPManager) Run() error {
	log.Printf("FRP客户端连接到: %s:%d", f.config.Server.Address, f.config.Server.Port)

	// 启动FRP客户端（会阻塞）
	if err := f.service.Run(f.ctx); err != nil {
		return fmt.Errorf("FRP客户端运行失败: %w", err)
	}

	return nil
}

// AddSTCPProxy 添加STCP代理
func (f *FRPManager) AddSTCPProxy(instanceName, secretKey, localIP string, localPort int32) error {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	// 检查是否已存在
	if _, exists := f.proxies[instanceName]; exists {
		return fmt.Errorf("代理已存在: %s", instanceName)
	}

	// 创建STCP代理配置
	proxyConfig := &v1.STCPProxyConfig{
		ProxyBaseConfig: v1.ProxyBaseConfig{
			Name: instanceName,
			Type: "stcp",
			ProxyBackend: v1.ProxyBackend{
				LocalIP:   localIP,
				LocalPort: int(localPort),
			},
		},
		Secretkey: secretKey,
	}

	f.proxies[instanceName] = proxyConfig

	// TODO: FRP 0.65.0 可能不支持动态添加代理
	// 这里先记录配置，实际使用时可能需要重启FRP客户端
	// 或者使用FRP的Admin API（如果有）

	log.Printf("FRP STCP代理已添加到配置: %s (总计: %d个)", instanceName, len(f.proxies))
	return nil
}

// RemoveSTCPProxy 删除STCP代理
func (f *FRPManager) RemoveSTCPProxy(instanceName string) error {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	// 检查是否存在
	if _, exists := f.proxies[instanceName]; !exists {
		return fmt.Errorf("代理不存在: %s", instanceName)
	}

	// 删除代理
	delete(f.proxies, instanceName)

	// TODO: FRP 0.65.0 可能不支持动态删除代理
	// 这里先从配置中删除，实际使用时可能需要重启FRP客户端

	log.Printf("FRP STCP代理已从配置删除: %s (剩余: %d个)", instanceName, len(f.proxies))
	return nil
}

// GetProxies 获取所有代理配置
func (f *FRPManager) GetProxies() map[string]*v1.STCPProxyConfig {
	f.mutex.RLock()
	defer f.mutex.RUnlock()

	// 返回副本
	proxies := make(map[string]*v1.STCPProxyConfig, len(f.proxies))
	for k, v := range f.proxies {
		proxies[k] = v
	}
	return proxies
}

// Stop 停止FRP管理器
func (f *FRPManager) Stop() {
	log.Println("正在停止FRP客户端...")
	f.cancel()
}
