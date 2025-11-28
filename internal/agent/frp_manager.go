package agent

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/fatedier/frp/client"
	v1 "github.com/fatedier/frp/pkg/config/v1"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/constants"
)

// ProxyCommand 代理命令
type ProxyCommand struct {
	Action       string // "add" or "remove"
	InstanceName string
	SecretKey    string
	LocalIP      string
	LocalPort    int32
	Response     chan error // 用于返回操作结果
}

// FRPManager FRP客户端管理器（Agent-FRP线程）
type FRPManager struct {
	config *config.AgentConfig

	// FRP客户端服务
	service *client.Service

	// 代理配置
	proxies map[string]*v1.STCPProxyConfig // instance_name -> proxy config
	mutex   sync.RWMutex

	// 命令通道（用于动态管理代理）
	commandChan chan *ProxyCommand

	// 连接配置
	token      string
	serverURL  string // 完整的 Server URL（如果配置了公网地址）
	serverPort int    // Server 端口（如果没有配置公网地址）
	connMutex  sync.RWMutex

	// 上下文
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// 重启标志
	needRestart bool
}

// NewFRPManager 创建FRP管理器
func NewFRPManager(cfg *config.AgentConfig, ctx context.Context) (*FRPManager, error) {
	frpCtx, cancel := context.WithCancel(ctx)

	return &FRPManager{
		config:      cfg,
		proxies:     make(map[string]*v1.STCPProxyConfig),
		commandChan: make(chan *ProxyCommand, 10),
		ctx:         frpCtx,
		cancel:      cancel,
	}, nil
}

// Run 运行FRP管理器
func (f *FRPManager) Run() error {
	log.Printf("FRP管理器启动，连接到: %s:%d", f.config.Server.Address, f.config.Server.Port)

	// 启动命令处理循环
	f.wg.Add(1)
	go f.commandLoop()

	// 启动FRP客户端循环（支持重启）
	for {
		select {
		case <-f.ctx.Done():
			log.Println("FRP管理器收到停止信号")
			f.wg.Wait()
			return nil

		default:
			// 创建FRP客户端并运行
			if err := f.runFRPClient(); err != nil {
				log.Printf("FRP客户端错误: %v", err)
			}

			// 检查是否需要重启
			f.mutex.Lock()
			needRestart := f.needRestart
			f.needRestart = false
			f.mutex.Unlock()

			if !needRestart {
				// 如果不是主动重启，说明是异常退出，等待一段时间后重试
				log.Println("FRP客户端异常退出，5秒后重试...")
				select {
				case <-time.After(5 * time.Second):
				case <-f.ctx.Done():
					f.wg.Wait()
					return nil
				}
			} else {
				log.Println("FRP客户端正在重启以应用新配置...")
			}
		}
	}
}

// runFRPClient 运行FRP客户端
func (f *FRPManager) runFRPClient() error {
	// 获取当前代理配置
	f.mutex.RLock()
	proxyCfgs := make([]v1.ProxyConfigurer, 0, len(f.proxies))
	for _, cfg := range f.proxies {
		proxyCfgs = append(proxyCfgs, cfg)
	}
	f.mutex.RUnlock()

	// 获取连接配置
	f.connMutex.RLock()
	token := f.token
	serverURL := f.serverURL
	serverPort := f.serverPort
	f.connMutex.RUnlock()

	// 解析 Server URL
	serverAddr := f.config.Server.Address
	port := f.config.Server.Port
	websocketPath := constants.DefaultWebSocketPath // 默认路径
	protocol := "websocket"
	insecureSkipVerify := true // 默认跳过证书验证（开发环境）

	if serverURL != "" {
		parsedURL, err := parseServerURL(serverURL)
		if err != nil {
			return fmt.Errorf("解析隧道 URL 失败: %w", err)
		}

		serverAddr = parsedURL.Host
		port = parsedURL.Port
		websocketPath = parsedURL.Path
		protocol = parsedURL.Protocol

		log.Printf("使用隧道 URL: %s (解析为 %s:%d%s, 协议: %s)",
			serverURL, serverAddr, port, websocketPath, protocol)
	} else if serverPort > 0 {
		port = serverPort
	}

	// 创建FRP客户端配置
	clientCfg := v1.ClientCommonConfig{
		ServerAddr: serverAddr,
		ServerPort: port,
	}

	// 配置传输协议
	clientCfg.Transport.Protocol = protocol

	// 配置 TLS
	if protocol == "wss" || f.config.Server.TLSEnable {
		enable := true
		clientCfg.Transport.TLS.Enable = &enable
		clientCfg.Transport.TLS.ServerName = serverAddr
		log.Printf("FRP客户端TLS已启用（跳过证书验证: %v）", insecureSkipVerify)
	}

	// 完成配置（填充默认值）- 这是关键步骤！
	if err := clientCfg.Complete(); err != nil {
		return fmt.Errorf("完成FRP客户端配置失败: %w", err)
	}
	log.Println("FRP客户端配置已完成（默认值已填充）")

	// 配置认证
	if token != "" {
		clientCfg.Auth.Method = v1.AuthMethod("token")
		clientCfg.Auth.Token = token
		log.Printf("FRP客户端配置: ServerAddr=%s, ServerPort=%d, Protocol=%s, Path=%s, Auth=token",
			serverAddr, port, protocol, websocketPath)
	} else {
		log.Printf("FRP客户端配置: ServerAddr=%s, ServerPort=%d, Protocol=%s, Path=%s, Auth=none (等待Token)",
			serverAddr, port, protocol, websocketPath)
	}

	// 创建FRP客户端服务（使用自定义 connector）
	svr, err := client.NewService(client.ServiceOptions{
		Common:         &clientCfg,
		ProxyCfgs:      proxyCfgs,
		VisitorCfgs:    []v1.VisitorConfigurer{},
		ConfigFilePath: "",
		ConnectorCreator: func(ctx context.Context, cfg *v1.ClientCommonConfig) client.Connector {
			// 使用自定义 connector，支持自定义 WebSocket path 和跳过证书验证
			connector, err := NewCustomConnector(ctx, cfg, websocketPath, insecureSkipVerify)
			if err != nil {
				log.Printf("创建自定义 Connector 失败: %v，使用默认 Connector", err)
				return client.NewConnector(ctx, cfg)
			}
			return connector
		},
	})
	if err != nil {
		return fmt.Errorf("创建FRP客户端失败: %w", err)
	}

	f.mutex.Lock()
	f.service = svr
	f.mutex.Unlock()

	log.Printf("FRP客户端已创建，代理数量: %d", len(proxyCfgs))

	// 启动FRP客户端（会阻塞直到停止或出错）
	if err := svr.Run(f.ctx); err != nil {
		return fmt.Errorf("FRP客户端运行失败: %w", err)
	}

	return nil
}

// parseServerURL 解析 Server URL
func parseServerURL(serverURL string) (*struct {
	Host     string
	Port     int
	Path     string
	Protocol string
}, error) {
	parsedURL, err := url.Parse(serverURL)
	if err != nil {
		return nil, err
	}

	result := &struct {
		Host     string
		Port     int
		Path     string
		Protocol string
	}{
		Host: parsedURL.Hostname(),
		Path: parsedURL.Path,
	}

	// 如果路径为空，使用默认路径
	if result.Path == "" {
		result.Path = constants.DefaultWebSocketPath
	}

	// 提取端口
	if parsedURL.Port() != "" {
		port, err := strconv.Atoi(parsedURL.Port())
		if err != nil {
			return nil, fmt.Errorf("解析端口失败: %w", err)
		}
		result.Port = port
	} else {
		// 根据协议设置默认端口
		if parsedURL.Scheme == "wss" || parsedURL.Scheme == "https" {
			result.Port = 443
		} else {
			result.Port = 80
		}
	}

	// 确定协议
	if parsedURL.Scheme == "wss" || parsedURL.Scheme == "https" {
		result.Protocol = "wss"
	} else if parsedURL.Scheme == "ws" || parsedURL.Scheme == "http" {
		result.Protocol = "websocket"
	} else {
		result.Protocol = "websocket" // 默认
	}

	return result, nil
}

// commandLoop 命令处理循环
func (f *FRPManager) commandLoop() {
	defer f.wg.Done()

	for {
		select {
		case cmd := <-f.commandChan:
			f.handleCommand(cmd)

		case <-f.ctx.Done():
			return
		}
	}
}

// handleCommand 处理命令
func (f *FRPManager) handleCommand(cmd *ProxyCommand) {
	var err error

	switch cmd.Action {
	case "add":
		err = f.addProxyInternal(cmd.InstanceName, cmd.SecretKey, cmd.LocalIP, cmd.LocalPort)

	case "remove":
		err = f.removeProxyInternal(cmd.InstanceName)

	default:
		err = fmt.Errorf("未知命令: %s", cmd.Action)
	}

	// 返回结果
	if cmd.Response != nil {
		cmd.Response <- err
		close(cmd.Response)
	}
}

// addProxyInternal 内部添加代理
func (f *FRPManager) addProxyInternal(instanceName, secretKey, localIP string, localPort int32) error {
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
	f.needRestart = true

	log.Printf("FRP STCP代理已添加: %s -> %s:%d (总计: %d个)",
		instanceName, localIP, localPort, len(f.proxies))

	// 停止当前FRP客户端以触发重启
	if f.service != nil {
		f.service.Close()
	}

	return nil
}

// removeProxyInternal 内部删除代理
func (f *FRPManager) removeProxyInternal(instanceName string) error {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	// 检查是否存在
	if _, exists := f.proxies[instanceName]; !exists {
		return fmt.Errorf("代理不存在: %s", instanceName)
	}

	// 删除代理
	delete(f.proxies, instanceName)
	f.needRestart = true

	log.Printf("FRP STCP代理已删除: %s (剩余: %d个)", instanceName, len(f.proxies))

	// 停止当前FRP客户端以触发重启
	if f.service != nil {
		f.service.Close()
	}

	return nil
}

// AddSTCPProxy 添加STCP代理（公共接口）
func (f *FRPManager) AddSTCPProxy(instanceName, secretKey, localIP string, localPort int32) error {
	respChan := make(chan error, 1)

	cmd := &ProxyCommand{
		Action:       "add",
		InstanceName: instanceName,
		SecretKey:    secretKey,
		LocalIP:      localIP,
		LocalPort:    localPort,
		Response:     respChan,
	}

	// 发送命令
	select {
	case f.commandChan <- cmd:
	case <-f.ctx.Done():
		return fmt.Errorf("FRP管理器已停止")
	}

	// 等待响应
	select {
	case err := <-respChan:
		return err
	case <-f.ctx.Done():
		return fmt.Errorf("FRP管理器已停止")
	}
}

// RemoveSTCPProxy 删除STCP代理（公共接口）
func (f *FRPManager) RemoveSTCPProxy(instanceName string) error {
	respChan := make(chan error, 1)

	cmd := &ProxyCommand{
		Action:       "remove",
		InstanceName: instanceName,
		Response:     respChan,
	}

	// 发送命令
	select {
	case f.commandChan <- cmd:
	case <-f.ctx.Done():
		return fmt.Errorf("FRP管理器已停止")
	}

	// 等待响应
	select {
	case err := <-respChan:
		return err
	case <-f.ctx.Done():
		return fmt.Errorf("FRP管理器已停止")
	}
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

// SetToken 设置认证 Token
func (f *FRPManager) SetToken(token string) {
	f.connMutex.Lock()
	f.token = token
	f.connMutex.Unlock()

	log.Printf("隧道 Token 已设置: %s...", token[:16])

	// 如果客户端正在运行，重启以应用新 Token
	f.mutex.Lock()
	if f.service != nil {
		f.needRestart = true
		f.service.Close()
	}
	f.mutex.Unlock()
}

// SetServerURL 设置服务器 URL（完整 URL）
func (f *FRPManager) SetServerURL(url string) {
	f.connMutex.Lock()
	f.serverURL = url
	f.connMutex.Unlock()

	log.Printf("隧道服务器 URL 已设置: %s", url)

	// 如果客户端正在运行，重启以应用新配置
	f.mutex.Lock()
	if f.service != nil {
		f.needRestart = true
		f.service.Close()
	}
	f.mutex.Unlock()
}

// SetServerPort 设置服务器端口
func (f *FRPManager) SetServerPort(port int) {
	f.connMutex.Lock()
	f.serverPort = port
	f.connMutex.Unlock()

	log.Printf("隧道服务器端口已设置: %d", port)

	// 如果客户端正在运行，重启以应用新配置
	f.mutex.Lock()
	if f.service != nil {
		f.needRestart = true
		f.service.Close()
	}
	f.mutex.Unlock()
}

// Stop 停止FRP管理器
func (f *FRPManager) Stop() {
	log.Println("正在停止FRP管理器...")
	f.cancel()

	// 停止FRP客户端
	f.mutex.Lock()
	if f.service != nil {
		f.service.Close()
	}
	f.mutex.Unlock()

	f.wg.Wait()
	log.Println("FRP管理器已停止")
}

// IsConnected 检查FRP客户端是否已连接
func (f *FRPManager) IsConnected() bool {
	f.mutex.RLock()
	defer f.mutex.RUnlock()

	// 检查service是否存在且token已设置
	// 注意：这是一个简化的检查，实际连接状态可能需要更复杂的逻辑
	f.connMutex.RLock()
	hasToken := f.token != ""
	f.connMutex.RUnlock()

	return f.service != nil && hasToken
}
