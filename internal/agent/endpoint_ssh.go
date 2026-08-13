// Package agent 提供 Agent 端功能
// endpoint_ssh.go 实现 Endpoint SSH 代理
// Agent 作为 SSH Server 接收 Desktop 连接，提取用户名后通过 gRPC 转发到 Endpoint
//
// 架构：
//
//	Desktop → tsnet → Agent(FallbackTCPHandler, 端口 50053+N) → SSH 握手 → gRPC OpenShell → Endpoint PTY
//	每个 Endpoint 分配一个独立端口（50053 起），Agent 根据端口号确定目标 Endpoint
//	Desktop 端无需修改，Server ResolveDomain 返回 agent_ip:分配端口 即可
package agent

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

var (
	// 版本信息（由 Agent 主程序设置）
	version   = "dev"
	buildDate = "unknown"
)

// SetVersionInfo 设置版本信息（由 Agent 主程序调用）
func SetVersionInfo(ver, date string) {
	version = ver
	buildDate = date
}

// EndpointSSHPortBase Endpoint SSH 代理的起始端口
// 每个 Endpoint 分配一个端口：50053, 50054, 50055, ...
const EndpointSSHPortBase = 50053

// EndpointSSHProxy Endpoint SSH 代理
// 在 Agent 内部运行 SSH Server，接收 Desktop SSH 连接
// 从 SSH 握手中提取 login 用户名，然后通过 gRPC 转发到 Endpoint
type EndpointSSHProxy struct {
	endpointServer *EndpointServer
	tsManager      *TailscaleManager
	auditCollector *AuditCollector
	permCache      *PermissionCache      // 权限缓存
	grpcClient     pb.AgentServiceClient // gRPC 客户端（用于查询用户设备信息）
	sshConfig      *ssh.ServerConfig
	hostKey        ssh.Signer

	// 端口 → Endpoint 名称映射
	portMap map[uint16]string
	// Endpoint 名称 → 端口映射
	nameMap map[string]uint16
	mapMu   sync.RWMutex

	// 下一个可分配的端口
	nextPort uint16

	// fallback handler 取消注册函数
	deregisterFallback func()

	ctx    context.Context
	cancel context.CancelFunc
}

// NewEndpointSSHProxy 创建 Endpoint SSH 代理
func NewEndpointSSHProxy(endpointServer *EndpointServer, tsManager *TailscaleManager, auditCollector *AuditCollector, permCache *PermissionCache, grpcClient pb.AgentServiceClient, stateDir string, parentCtx context.Context) (*EndpointSSHProxy, error) {
	// 加载或生成 Ed25519 主机密钥（持久化到 stateDir，避免重启后 Host Key 变化）
	privKey, err := loadOrGenerateHostKey(stateDir)
	if err != nil {
		return nil, fmt.Errorf("加载主机密钥失败: %w", err)
	}

	hostKey, err := ssh.NewSignerFromKey(privKey)
	if err != nil {
		return nil, fmt.Errorf("创建 SSH Signer 失败: %w", err)
	}

	ctx, cancel := context.WithCancel(parentCtx)

	proxy := &EndpointSSHProxy{
		endpointServer: endpointServer,
		tsManager:      tsManager,
		auditCollector: auditCollector,
		permCache:      permCache,
		grpcClient:     grpcClient,
		hostKey:        hostKey,
		portMap:        make(map[uint16]string),
		nameMap:        make(map[string]uint16),
		nextPort:       EndpointSSHPortBase,
		ctx:            ctx,
		cancel:         cancel,
	}

	// 配置 SSH Server（不做客户端认证，信任 Tailscale 隧道）
	proxy.sshConfig = &ssh.ServerConfig{
		NoClientAuth: true,
	}
	proxy.sshConfig.AddHostKey(hostKey)

	return proxy, nil
}

// Start 启动 Endpoint SSH 代理
// 注册 FallbackTCPHandler，拦截端口范围 [50053, 50153) 的连接
func (p *EndpointSSHProxy) Start() error {
	// 注册 fallback TCP handler，拦截 Endpoint SSH 端口范围的连接
	deregister := p.tsManager.RegisterFallbackTCPHandler(func(src, dst netip.AddrPort) (func(net.Conn), bool) {
		port := dst.Port()
		if port >= EndpointSSHPortBase && port < EndpointSSHPortBase+100 {
			// 查找端口对应的 Endpoint
			p.mapMu.RLock()
			endpointName, exists := p.portMap[port]
			p.mapMu.RUnlock()

			if !exists {
				return nil, false
			}

			return func(conn net.Conn) {
				go p.HandleConn(p.ctx, conn, endpointName)
			}, true
		}
		return nil, false
	})
	p.deregisterFallback = deregister

	logger.Infof("[EndpointSSH] 代理已启动（FallbackTCPHandler）: 端口范围 %d-%d", EndpointSSHPortBase, EndpointSSHPortBase+99)
	return nil
}

// Stop 停止 Endpoint SSH 代理
func (p *EndpointSSHProxy) Stop() {
	p.cancel()
	if p.deregisterFallback != nil {
		p.deregisterFallback()
	}
	logger.Info("[EndpointSSH] 代理已停止")
}

// AllocatePort 为 Endpoint 分配端口
// 如果已分配则返回已有端口，否则分配新端口
func (p *EndpointSSHProxy) AllocatePort(endpointName string) uint16 {
	p.mapMu.Lock()
	defer p.mapMu.Unlock()

	// 已分配
	if port, exists := p.nameMap[endpointName]; exists {
		return port
	}

	// 分配新端口
	port := p.nextPort
	p.nextPort++
	p.portMap[port] = endpointName
	p.nameMap[endpointName] = port

	logger.Infof("[EndpointSSH] 分配端口: %s → %d", endpointName, port)
	return port
}

// AllocateSpecificPort 为 Endpoint 分配指定端口（Server 预分配）
// 如果端口已被占用（且不是同一个 Endpoint），返回错误
func (p *EndpointSSHProxy) AllocateSpecificPort(endpointName string, port uint16) error {
	p.mapMu.Lock()
	defer p.mapMu.Unlock()

	// 检查端口是否已被其他 Endpoint 占用
	if existingName, exists := p.portMap[port]; exists && existingName != endpointName {
		return fmt.Errorf("端口 %d 已被 %s 占用", port, existingName)
	}

	// 检查该 Endpoint 是否已分配了不同的端口
	if existingPort, exists := p.nameMap[endpointName]; exists && existingPort != port {
		// 释放旧端口
		delete(p.portMap, existingPort)
		logger.Infof("[EndpointSSH] 释放旧端口: %s ← %d", endpointName, existingPort)
	}

	// 分配端口
	p.portMap[port] = endpointName
	p.nameMap[endpointName] = port
	logger.Infof("[EndpointSSH] 分配指定端口: %s → %d", endpointName, port)
	return nil
}

// ReleasePort 释放 Endpoint 的端口
func (p *EndpointSSHProxy) ReleasePort(endpointName string) {
	p.mapMu.Lock()
	defer p.mapMu.Unlock()

	if port, exists := p.nameMap[endpointName]; exists {
		delete(p.portMap, port)
		delete(p.nameMap, endpointName)
		logger.Infof("[EndpointSSH] 释放端口: %s ← %d", endpointName, port)
	}
}

// GetPort 获取 Endpoint 的分配端口（0 表示未分配）
func (p *EndpointSSHProxy) GetPort(endpointName string) uint16 {
	p.mapMu.RLock()
	defer p.mapMu.RUnlock()
	return p.nameMap[endpointName]
}

// HandleConn 处理 Desktop SSH 连接
// 运行 SSH 握手，提取用户名，请求 Endpoint 开启 shell，桥接 I/O
func (p *EndpointSSHProxy) HandleConn(ctx context.Context, conn net.Conn, endpointName string) {
	defer conn.Close()
	startedAt := time.Now()

	// 尝试通过 tsnet WhoIs 提取 Desktop 用户身份
	clientUserName := ""
	clientIP := ""
	if p.tsManager != nil {
		if lc, err := p.tsManager.LocalClient(); err == nil {
			if whois, err := lc.WhoIs(ctx, conn.RemoteAddr().String()); err == nil && whois.UserProfile != nil {
				clientUserName, _ = parseHeadscaleUserName(whois.UserProfile.LoginName)
				// 提取 IP 地址（去掉端口号）
				if addr, _, err := net.SplitHostPort(conn.RemoteAddr().String()); err == nil {
					clientIP = addr
				} else {
					clientIP = conn.RemoteAddr().String()
				}
			}
		}
	}

	// 检查 Endpoint 是否在线
	if !p.endpointServer.IsEndpointConnected(endpointName) {
		logger.Warnf("[EndpointSSH] Endpoint 不在线: %s", endpointName)
		return
	}

	// SSH 握手
	sshConn, chans, reqs, err := ssh.NewServerConn(conn, p.sshConfig)
	if err != nil {
		logger.Warnf("[EndpointSSH] SSH 握手失败: %v", err)
		return
	}
	defer sshConn.Close()

	// 提取登录用户名
	login := sshConn.User()
	logger.Infof("[EndpointSSH] SSH 连接: endpoint=%s, login=%s, client=%s", endpointName, login, clientUserName)

	// 权限检查：验证用户是否有权访问该 Endpoint
	if clientUserName == "" {
		logger.Warnf("[EndpointSSH] 无法识别 Desktop 用户身份，拒绝连接: endpoint=%s", endpointName)
		return
	}

	if p.permCache != nil {
		allowedSSHUsers, allowed := p.permCache.CheckEndpointSSHAccess(clientUserName, endpointName)
		if !allowed {
			logger.Warnf("[EndpointSSH] 用户无权访问 Endpoint: user=%s, endpoint=%s", clientUserName, endpointName)
			return
		}

		// 检查 SSH 登录用户名是否在允许列表中
		loginAllowed := false
		for _, allowedUser := range allowedSSHUsers {
			if allowedUser == login || allowedUser == "*" {
				loginAllowed = true
				break
			}
		}
		if !loginAllowed {
			logger.Warnf("[EndpointSSH] SSH 登录用户名不在允许列表中: user=%s, endpoint=%s, login=%s, allowed=%v",
				clientUserName, endpointName, login, allowedSSHUsers)
			return
		}

		logger.Infof("[EndpointSSH] 权限验证通过: user=%s, endpoint=%s, login=%s", clientUserName, endpointName, login)
	} else {
		logger.Warnf("[EndpointSSH] 权限缓存未初始化，跳过权限检查")
	}

	// 丢弃全局请求
	go ssh.DiscardRequests(reqs)

	// 等待 session channel
	for newChannel := range chans {
		if newChannel.ChannelType() != "session" {
			newChannel.Reject(ssh.UnknownChannelType, "不支持的 channel 类型")
			continue
		}

		channel, requests, err := newChannel.Accept()
		if err != nil {
			logger.Warnf("[EndpointSSH] 接受 channel 失败: %v", err)
			return
		}

		// 处理 session（只处理第一个）
		p.handleSession(ctx, channel, requests, endpointName, login, clientUserName, clientIP)

		// 会话结束，记录审计
		if p.auditCollector != nil {
			endedAt := time.Now()
			target := fmt.Sprintf("%s@%s", login, endpointName)
			p.auditCollector.Record(
				clientUserName,
				endpointName,
				"ssh_session",
				target,
				"",
				startedAt,
				endedAt,
			)
		}
		return
	}
}

// handleSession 处理 SSH session channel
// 等待 pty-req 和 shell 请求，然后请求 Endpoint 开启 shell 并桥接 I/O
func (p *EndpointSSHProxy) handleSession(ctx context.Context, channel ssh.Channel, requests <-chan *ssh.Request, endpointName, login, clientUserName, clientIP string) {
	defer channel.Close()

	var rows, cols uint32 = 24, 80 // 默认终端大小
	var shellStream pb.EndpointService_OpenShellServer
	shellReady := make(chan struct{})
	shellErr := make(chan error, 1)
	var execCommand string // exec 模式的命令

	// 处理 SSH 请求（pty-req, shell, exec, window-change 等）
	isPTY := false // 标记是否为交互式会话（有 PTY）
	isSFTP := false
	go func() {
		for req := range requests {
			switch req.Type {
			case "pty-req":
				// 解析终端大小
				// pty-req payload: string term, uint32 cols, uint32 rows, ...
				if len(req.Payload) >= 4 {
					termLen := int(req.Payload[3]) | int(req.Payload[2])<<8 | int(req.Payload[1])<<16 | int(req.Payload[0])<<24
					if len(req.Payload) >= 4+termLen+8 {
						offset := 4 + termLen
						cols = uint32(req.Payload[offset])<<24 | uint32(req.Payload[offset+1])<<16 | uint32(req.Payload[offset+2])<<8 | uint32(req.Payload[offset+3])
						rows = uint32(req.Payload[offset+4])<<24 | uint32(req.Payload[offset+5])<<16 | uint32(req.Payload[offset+6])<<8 | uint32(req.Payload[offset+7])
					}
				}
				isPTY = true // 收到 pty-req 表示交互式会话
				if req.WantReply {
					req.Reply(true, nil)
				}

			case "shell":
				if req.WantReply {
					req.Reply(true, nil)
				}
				if !isPTY {
					rows, cols = 24, 80
				}
				stream, err := p.endpointServer.RequestShell(ctx, endpointName, login, rows, cols, "", "")
				if err != nil {
					logger.Warnf("[EndpointSSH] 请求 Endpoint shell 失败: %v", err)
					shellErr <- err
					return
				}
				shellStream = stream
				close(shellReady)

			case "exec":
				// exec 请求：执行单个命令
				if req.WantReply {
					req.Reply(true, nil)
				}
				// 解析命令：payload 是 string (uint32 length + data)
				if len(req.Payload) >= 4 {
					cmdLen := int(req.Payload[0])<<24 | int(req.Payload[1])<<16 | int(req.Payload[2])<<8 | int(req.Payload[3])
					if len(req.Payload) >= 4+cmdLen {
						execCommand = string(req.Payload[4 : 4+cmdLen])
						logger.Infof("[EndpointSSH] exec 请求: endpoint=%s, login=%s, command=%s", endpointName, login, execCommand)
					}
				}
				// exec 不需要 pty，使用默认大小
				stream, err := p.endpointServer.RequestShell(ctx, endpointName, login, rows, cols, execCommand, "")
				if err != nil {
					logger.Warnf("[EndpointSSH] 请求 Endpoint exec 失败: %v", err)
					shellErr <- err
					return
				}
				shellStream = stream
				close(shellReady)

			case "subsystem":
				subsystem := parseSSHString(req.Payload)
				if subsystem != "sftp" {
					if req.WantReply {
						req.Reply(false, nil)
					}
					logger.Warnf("[EndpointSSH] 不支持的 subsystem: endpoint=%s subsystem=%q", endpointName, subsystem)
					return
				}
				if req.WantReply {
					req.Reply(true, nil)
				}
				isSFTP = true
				stream, err := p.endpointServer.RequestShell(ctx, endpointName, login, rows, cols, "", subsystem)
				if err != nil {
					logger.Warnf("[EndpointSSH] 请求 Endpoint SFTP 失败: %v", err)
					shellErr <- err
					return
				}
				shellStream = stream
				close(shellReady)

			case "window-change":
				if len(req.Payload) >= 8 {
					newCols := uint32(req.Payload[0])<<24 | uint32(req.Payload[1])<<16 | uint32(req.Payload[2])<<8 | uint32(req.Payload[3])
					newRows := uint32(req.Payload[4])<<24 | uint32(req.Payload[5])<<16 | uint32(req.Payload[6])<<8 | uint32(req.Payload[7])
					if shellStream != nil {
						shellStream.Send(&pb.ShellData{
							IsResize: true,
							Rows:     newRows,
							Cols:     newCols,
						})
					}
				}
				if req.WantReply {
					req.Reply(true, nil)
				}

			default:
				if req.WantReply {
					req.Reply(false, nil)
				}
			}
		}
	}()

	// 等待 shell 就绪
	select {
	case <-shellReady:
	case err := <-shellErr:
		logger.Warnf("[EndpointSSH] Shell 未就绪: %v", err)
		return
	case <-ctx.Done():
		return
	}

	// 显示 SSH 横幅（仅在交互式会话时显示，exec 模式不显示）
	// 判断依据：收到 pty-req 请求表示交互式会话
	if shouldShowSSHBanner(isPTY, isSFTP) {
		banner := p.buildSSHBanner(ctx, clientUserName, clientIP)
		if banner != "" {
			channel.Write([]byte(banner))
		}
	}

	// 双向桥接：SSH channel ↔ gRPC Shell 流
	var wg sync.WaitGroup

	// SSH channel → gRPC（Desktop stdin → Endpoint）
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 32*1024)
		for {
			n, err := channel.Read(buf)
			if n > 0 {
				if sendErr := shellStream.Send(&pb.ShellData{
					Data: buf[:n],
				}); sendErr != nil {
					return
				}
			}
			if err != nil {
				shellStream.Send(&pb.ShellData{IsClose: true})
				return
			}
		}
	}()

	// gRPC → SSH channel（Endpoint stdout → Desktop）
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			msg, err := shellStream.Recv()
			if err != nil {
				channel.Close()
				return
			}
			if msg.IsClose {
				if msg.Error != "" {
					_, _ = channel.Stderr().Write([]byte(msg.Error + "\r\n"))
				}
				exitCode := msg.ExitCode
				if msg.Error != "" && exitCode == 0 {
					exitCode = 1
				}
				exitMsg := ssh.Marshal(struct{ Status uint32 }{uint32(exitCode)})
				channel.SendRequest("exit-status", false, exitMsg)
				// 关闭 SSH channel，解除 SSH→gRPC 协程的 Read 阻塞
				channel.Close()
				return
			}
			if len(msg.Data) > 0 {
				writer := io.Writer(channel)
				if msg.IsStderr {
					writer = channel.Stderr()
				}
				if _, writeErr := writer.Write(msg.Data); writeErr != nil {
					return
				}
			}
		}
	}()

	wg.Wait()
}

func shouldShowSSHBanner(isPTY, isSFTP bool) bool {
	return isPTY && !isSFTP
}

func parseSSHString(payload []byte) string {
	if len(payload) < 4 {
		return ""
	}
	length := int(payload[0])<<24 | int(payload[1])<<16 | int(payload[2])<<8 | int(payload[3])
	if length < 0 || len(payload) < 4+length {
		return ""
	}
	return string(payload[4 : 4+length])
}

// loadOrGenerateHostKey 从文件加载或生成 SSH Host Key
// 密钥保存在 stateDir/ssh_host_ed25519_key，重启后保持不变
func loadOrGenerateHostKey(stateDir string) (ed25519.PrivateKey, error) {
	keyPath := filepath.Join(stateDir, "ssh_host_ed25519_key")

	// 尝试从文件加载
	data, err := os.ReadFile(keyPath)
	if err == nil {
		block, _ := pem.Decode(data)
		if block != nil {
			key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
			if err == nil {
				if privKey, ok := key.(ed25519.PrivateKey); ok {
					logger.Infof("[EndpointSSH] 已加载持久化 Host Key: %s", keyPath)
					return privKey, nil
				}
			}
		}
		// 文件存在但解析失败，重新生成
		logger.Warnf("[EndpointSSH] Host Key 文件损坏，重新生成: %s", keyPath)
	}

	// 生成新密钥
	_, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("生成 Host Key 失败: %w", err)
	}

	// 序列化为 PKCS8 PEM 格式并保存
	pkcs8Bytes, err := x509.MarshalPKCS8PrivateKey(privKey)
	if err != nil {
		// 序列化失败不影响使用，只是无法持久化
		logger.Warnf("[EndpointSSH] Host Key 序列化失败，使用临时密钥: %v", err)
		return privKey, nil
	}

	pemBlock := &pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: pkcs8Bytes,
	}

	if err := os.MkdirAll(stateDir, 0700); err != nil {
		logger.Warnf("[EndpointSSH] 创建目录失败，使用临时密钥: %v", err)
		return privKey, nil
	}

	if err := os.WriteFile(keyPath, pem.EncodeToMemory(pemBlock), 0600); err != nil {
		logger.Warnf("[EndpointSSH] 保存 Host Key 失败，使用临时密钥: %v", err)
		return privKey, nil
	}

	logger.Infof("[EndpointSSH] 已生成并保存 Host Key: %s", keyPath)
	return privKey, nil
}

// buildSSHBanner 构建 SSH 登录横幅
// 注意：横幅直接写入 SSH channel（不经过 PTY），需要使用 \r\n 换行
// PTY 的 onlcr 设置会自动将 \n 转换为 \r\n，但 SSH channel 是 raw mode
func (p *EndpointSSHProxy) buildSSHBanner(ctx context.Context, clientUserName, clientIP string) string {
	if clientUserName == "" {
		return "" // 无法获取客户端信息，不显示横幅
	}

	userInfo := p.queryUserDeviceInfo(ctx, clientUserName, clientIP)
	info := SSHBannerInfo{RemoteUser: clientUserName, RemoteIP: clientIP}
	if userInfo != nil {
		info.DisplayName = userInfo.DisplayName
		info.RemoteDeviceName = userInfo.DeviceName
		info.RemoteDeviceOS = userInfo.DeviceOs
	}
	return BuildSSHBanner(info)
}

// queryUserDeviceInfo 查询用户设备信息
func (p *EndpointSSHProxy) queryUserDeviceInfo(ctx context.Context, userName, deviceIP string) *pb.GetUserDeviceInfoResponse {
	if p.grpcClient == nil {
		logger.Debugf("[EndpointSSH] gRPC 客户端未初始化，跳过用户设备信息查询")
		return nil
	}

	// 设置超时
	queryCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	// 调用 Server 的 GetUserDeviceInfo API
	resp, err := p.grpcClient.GetUserDeviceInfo(queryCtx, &pb.GetUserDeviceInfoRequest{
		UserName: userName,
		DeviceIp: deviceIP,
	})
	if err != nil {
		logger.Debugf("[EndpointSSH] 查询用户设备信息失败: user=%s, ip=%s, err=%v", userName, deviceIP, err)
		return nil
	}

	logger.Debugf("[EndpointSSH] 查询用户设备信息成功: user=%s, display_name=%s, device_name=%s, device_os=%s",
		userName, resp.DisplayName, resp.DeviceName, resp.DeviceOs)

	return resp
}
