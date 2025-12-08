package agent

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fatedier/frp/client"
	v1 "github.com/fatedier/frp/pkg/config/v1"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/constants"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
)

// ProxyCommand 代理命令
type ProxyCommand struct {
	Action       string // "add" or "remove"
	ProxyType    string // "stcp" or "tcp"
	InstanceName string // STCP实例名称
	ServiceName  string // TCP实例名称
	SecretKey    string // STCP密钥
	RemotePort   int32  // TCP远程端口
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
	proxies    map[string]*v1.STCPProxyConfig // instance_name -> STCP proxy config
	tcpProxies map[string]*v1.TCPProxyConfig  // service_name -> TCP proxy config
	mutex      sync.RWMutex

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
		tcpProxies:  make(map[string]*v1.TCPProxyConfig),
		commandChan: make(chan *ProxyCommand, 10),
		ctx:         frpCtx,
		cancel:      cancel,
	}, nil
}

// Run 运行FRP管理器
func (f *FRPManager) Run() error {
	logger.Debugf("隧道管理器启动，连接到: %s:%d", f.config.Server.Address, f.config.Server.Port)

	// 启动命令处理循环
	f.wg.Add(1)
	go f.commandLoop()

	// 启动FRP客户端循环（支持重启）
	for {
		select {
		case <-f.ctx.Done():
			logger.Debug("隧道管理器收到停止信号")
			f.wg.Wait()
			return nil

		default:
			// 创建FRP客户端并运行
			if err := f.runFRPClient(); err != nil {
				// 检查是否是因为context取消导致的错误
				select {
				case <-f.ctx.Done():
					logger.Debug("隧道管理器收到停止信号")
					f.wg.Wait()
					return nil
				default:
					logger.Debugf("隧道客户端错误: %v", err)
				}
			}

			// 再次检查context，避免在停止时继续重试
			select {
			case <-f.ctx.Done():
				logger.Debug("隧道管理器收到停止信号")
				f.wg.Wait()
				return nil
			default:
			}

			// 检查是否需要重启
			f.mutex.Lock()
			needRestart := f.needRestart
			f.needRestart = false
			f.mutex.Unlock()

			if !needRestart {
				// 如果不是主动重启，说明是异常退出，等待一段时间后重试
				logger.Info("客户端异常退出，5秒后重试...")
				select {
				case <-time.After(5 * time.Second):
				case <-f.ctx.Done():
					f.wg.Wait()
					return nil
				}
			} else {
				logger.Info("客户端正在重启以应用新配置...")
			}
		}
	}
}

// runFRPClient 运行FRP客户端
func (f *FRPManager) runFRPClient() error {
	// 获取当前代理配置（STCP + TCP）
	f.mutex.RLock()
	proxyCfgs := make([]v1.ProxyConfigurer, 0, len(f.proxies)+len(f.tcpProxies))
	for _, cfg := range f.proxies {
		proxyCfgs = append(proxyCfgs, cfg)
	}
	for _, cfg := range f.tcpProxies {
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

		logger.Debugf("使用隧道 URL: %s (解析为 %s://%s:%d%s)",
			serverURL, protocol, serverAddr, port, websocketPath)
	} else if serverPort > 0 {
		port = serverPort
	}

	// 创建FRP客户端配置
	clientCfg := v1.ClientCommonConfig{
		ServerAddr: serverAddr,
		ServerPort: port,
	}

	// 配置 FRP 日志级别
	// FRP库的日志比较冗余，默认设置为warn级别，只显示警告和错误
	frpLogLevel := "warn"
	if f.config.Log.Level == "debug" {
		frpLogLevel = "info" // 如果应用日志是debug，FRP设置为info
	}
	clientCfg.Log.Level = frpLogLevel
	clientCfg.Log.To = "console"
	logger.Debugf("隧道客户端日志级别: %s", frpLogLevel)

	// 配置传输协议
	clientCfg.Transport.Protocol = protocol

	// 配置 TLS
	if protocol == "wss" || f.config.Server.TLSEnable {
		enable := true
		clientCfg.Transport.TLS.Enable = &enable
		clientCfg.Transport.TLS.ServerName = serverAddr
		logger.Debugf("隧道客户端TLS已启用（跳过证书验证: %v）", insecureSkipVerify)
	}

	// 完成配置（填充默认值）- 这是关键步骤！
	if err := clientCfg.Complete(); err != nil {
		return fmt.Errorf("完成隧道客户端配置失败: %w", err)
	}
	logger.Debug("隧道客户端配置已完成（默认值已填充）")

	// 配置认证
	if token != "" {
		clientCfg.Auth.Method = v1.AuthMethod("token")
		clientCfg.Auth.Token = token
		logger.Debugf("隧道客户端配置: ServerAddr=%s, ServerPort=%d, Protocol=%s, Path=%s, Auth=token",
			serverAddr, port, protocol, websocketPath)
	} else {
		logger.Debugf("隧道客户端配置: ServerAddr=%s, ServerPort=%d, Protocol=%s, Path=%s, Auth=none (等待Token)",
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
				logger.Debugf("创建自定义 Connector 失败: %v，使用默认 Connector", err)
				return client.NewConnector(ctx, cfg)
			}
			return connector
		},
	})
	if err != nil {
		return fmt.Errorf("创建隧道客户端失败: %w", err)
	}

	f.mutex.Lock()
	f.service = svr
	f.mutex.Unlock()

	logger.Debugf("隧道客户端已创建，代理数量: %d", len(proxyCfgs))

	// 启动FRP客户端（会阻塞直到停止或出错）
	if err := svr.Run(f.ctx); err != nil {
		return fmt.Errorf("隧道客户端运行失败: %w", err)
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
	// 如果没有协议前缀，添加默认的 ws://
	if !strings.Contains(serverURL, "://") {
		serverURL = "ws://" + serverURL
	}

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

	// 如果路径为空，使用FRP原生路径（内网直连场景）
	// 注意：只有在使用Traefik等反向代理时才应该使用 /ws
	if result.Path == "" {
		result.Path = constants.FRPDefaultPath // 使用 /~!frp
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
		if cmd.ProxyType == "tcp" {
			err = f.addTCPProxyInternal(cmd.ServiceName, cmd.LocalIP, cmd.LocalPort, cmd.RemotePort)
		} else {
			err = f.addProxyInternal(cmd.InstanceName, cmd.SecretKey, cmd.LocalIP, cmd.LocalPort)
		}

	case "remove":
		if cmd.ProxyType == "tcp" {
			err = f.removeTCPProxyInternal(cmd.ServiceName)
		} else {
			err = f.removeProxyInternal(cmd.InstanceName)
		}

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

	logger.Debugf("隧道 STCP代理已添加: %s -> %s:%d (总计: %d个)",
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

	logger.Debugf("隧道 STCP代理已删除: %s (剩余: %d个)", instanceName, len(f.proxies))

	// 停止当前FRP客户端以触发重启
	if f.service != nil {
		f.service.Close()
	}

	return nil
}

// addTCPProxyInternal 内部添加TCP代理
func (f *FRPManager) addTCPProxyInternal(serviceName string, localIP string, localPort int32, remotePort int32) error {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	// 检查是否已存在
	if _, exists := f.tcpProxies[serviceName]; exists {
		return fmt.Errorf("TCP代理已存在: %s", serviceName)
	}

	// 创建TCP代理配置
	proxyConfig := &v1.TCPProxyConfig{
		ProxyBaseConfig: v1.ProxyBaseConfig{
			Name: serviceName,
			Type: "tcp",
			ProxyBackend: v1.ProxyBackend{
				LocalIP:   localIP,
				LocalPort: int(localPort),
			},
		},
		RemotePort: int(remotePort),
	}

	f.tcpProxies[serviceName] = proxyConfig
	f.needRestart = true

	logger.Debugf("隧道 TCP代理已添加: %s -> %s:%d (远程端口: %d, 总计: %d个)",
		serviceName, localIP, localPort, remotePort, len(f.tcpProxies))

	// 停止当前FRP客户端以触发重启
	if f.service != nil {
		f.service.Close()
	}

	return nil
}

// removeTCPProxyInternal 内部删除TCP代理
func (f *FRPManager) removeTCPProxyInternal(serviceName string) error {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	// 检查是否存在
	if _, exists := f.tcpProxies[serviceName]; !exists {
		return fmt.Errorf("TCP代理不存在: %s", serviceName)
	}

	// 删除代理
	delete(f.tcpProxies, serviceName)
	f.needRestart = true

	logger.Debugf("隧道 TCP代理已删除: %s (剩余: %d个)", serviceName, len(f.tcpProxies))

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
		ProxyType:    "stcp",
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
		ProxyType:    "stcp",
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

// AddTCPProxy 添加TCP代理（公共接口）
func (f *FRPManager) AddTCPProxy(serviceName, localIP string, localPort int32, remotePort int32) error {
	respChan := make(chan error, 1)

	cmd := &ProxyCommand{
		Action:      "add",
		ProxyType:   "tcp",
		ServiceName: serviceName,
		LocalIP:     localIP,
		LocalPort:   localPort,
		RemotePort:  remotePort,
		Response:    respChan,
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

// RemoveTCPProxy 删除TCP代理（公共接口）
func (f *FRPManager) RemoveTCPProxy(serviceName string) error {
	respChan := make(chan error, 1)

	cmd := &ProxyCommand{
		Action:      "remove",
		ProxyType:   "tcp",
		ServiceName: serviceName,
		Response:    respChan,
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

	logger.Debugf("隧道 Token 已设置: %s...", token[:16])

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

	logger.Debugf("隧道服务器 URL 已设置: %s", url)

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

	logger.Debugf("隧道服务器端口已设置: %d", port)

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
	logger.Info("正在停止隧道管理器...")
	f.cancel()

	// 停止FRP客户端
	f.mutex.Lock()
	if f.service != nil {
		f.service.Close()
	}
	f.mutex.Unlock()

	f.wg.Wait()
	logger.Info("客户端管理器已停止")
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
