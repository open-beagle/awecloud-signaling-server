// Package grpc 提供 gRPC 服务实现
package grpc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/headscale"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

// DesktopConnection Desktop 连接信息
type DesktopConnection struct {
	DesktopID uint64
	ClientID  uint64
	Stream    pb.DesktopService_HeartbeatServer
	TunnelIP  string
	Connected bool
	LastSeen  time.Time
	Cancel    context.CancelFunc
}

// DesktopServiceServer Desktop 服务实现
type DesktopServiceServer struct {
	pb.UnimplementedDesktopServiceServer

	// Desktop 连接管理
	connections map[uint64]*DesktopConnection
	connMutex   sync.RWMutex

	// Headscale 客户端
	headscaleClient *headscale.Client
	config          *config.ServerConfig

	// Agent 服务（用于获取服务列表）
	agentService *AgentServiceServer
}

// NewDesktopServiceServer 创建 Desktop 服务
func NewDesktopServiceServer(cfg *config.ServerConfig) *DesktopServiceServer {
	s := &DesktopServiceServer{
		connections: make(map[uint64]*DesktopConnection),
		config:      cfg,
	}

	// 初始化 Headscale 客户端
	if cfg.Tailscale.HeadscaleURL != "" && cfg.Tailscale.HeadscaleAPIKey != "" {
		client, err := headscale.NewClient(headscale.Config{
			URL:    cfg.Tailscale.HeadscaleURL,
			APIKey: cfg.Tailscale.HeadscaleAPIKey,
		})
		if err != nil {
			logger.Errorf("初始化 Desktop 服务 Headscale 客户端失败: %v", err)
		} else {
			s.headscaleClient = client
			logger.Infof("Desktop 服务 Headscale 客户端已初始化")
		}
	}

	return s
}

// SetAgentService 设置 Agent 服务
func (s *DesktopServiceServer) SetAgentService(agentService *AgentServiceServer) {
	s.agentService = agentService
}

// Login Desktop 首次登录
func (s *DesktopServiceServer) Login(ctx context.Context, req *pb.DesktopLoginRequest) (*pb.DesktopLoginResponse, error) {
	logger.Infof("Desktop 登录请求: client_name=%s, device_name=%s", req.ClientName, req.DeviceName)

	// 验证 Client
	var client model.Client
	if err := db.DB.Where("name = ?", req.ClientName).First(&client).Error; err != nil {
		logger.Warnf("Client 不存在: %s", req.ClientName)
		return &pb.DesktopLoginResponse{
			Success: false,
			Message: "Client 不存在",
		}, nil
	}

	// 验证 Client 密钥
	if err := bcrypt.CompareHashAndPassword([]byte(client.SecretHash), []byte(req.ClientSecret)); err != nil {
		logger.Warnf("Client 密钥验证失败: %s", req.ClientName)
		return &pb.DesktopLoginResponse{
			Success: false,
			Message: "认证失败",
		}, nil
	}

	// 检查设备是否已存在（通过设备指纹）
	var desktop model.Desktop
	err := db.DB.Where("client_id = ?", client.ID).
		Where("name = ?", req.DeviceName).
		First(&desktop).Error

	isNewDevice := err != nil
	var desktopSecret string

	if isNewDevice {
		// 新设备，创建 Desktop 记录
		desktopSecret = generateSecret()
		secretHash, err := bcrypt.GenerateFromPassword([]byte(desktopSecret), bcrypt.DefaultCost)
		if err != nil {
			logger.Errorf("生成密钥哈希失败: %v", err)
			return &pb.DesktopLoginResponse{
				Success: false,
				Message: "内部错误",
			}, nil
		}

		// 准备系统信息
		var systemInfoJSON string
		if req.SystemInfo != nil {
			systemInfo := model.DesktopSystemInfo{
				OS:        req.SystemInfo.Os,
				OSVersion: req.SystemInfo.OsVersion,
				Arch:      req.SystemInfo.Arch,
				Hostname:  req.SystemInfo.Hostname,
				CPU:       req.SystemInfo.Cpu,
				CPUCores:  int(req.SystemInfo.CpuCores),
				MemoryGB:  int(req.SystemInfo.MemoryGb),
			}
			if data, err := json.Marshal(systemInfo); err == nil {
				systemInfoJSON = string(data)
			}
		}

		// 创建 Desktop
		now := time.Now()
		desktop = model.Desktop{
			ClientID:   client.ID,
			Name:       req.DeviceName,
			SecretHash: string(secretHash),
			SystemInfo: systemInfoJSON,
			LastOnline: &now,
		}

		// 如果有 Headscale，先创建 Node 获取 ID
		if s.headscaleClient != nil {
			nodeID, authKey, err := s.createDesktopNode(ctx, client.ID, req.DeviceName)
			if err != nil {
				logger.Errorf("创建 Headscale Node 失败: %v", err)
				// 继续创建 Desktop，但不设置 Node ID
			} else {
				desktop.ID = nodeID
				// 保存 authKey 用于返回
				defer func() {
					// 在响应中设置 authKey
				}()
				_ = authKey // 稍后使用
			}
		}

		if err := db.DB.Create(&desktop).Error; err != nil {
			logger.Errorf("创建 Desktop 失败: %v", err)
			return &pb.DesktopLoginResponse{
				Success: false,
				Message: "创建设备失败",
			}, nil
		}

		logger.Infof("新 Desktop 创建成功: id=%d, client_id=%d, name=%s", desktop.ID, client.ID, req.DeviceName)
	} else {
		// 已存在的设备，更新信息
		now := time.Now()
		desktop.LastOnline = &now

		if req.SystemInfo != nil {
			systemInfo := model.DesktopSystemInfo{
				OS:        req.SystemInfo.Os,
				OSVersion: req.SystemInfo.OsVersion,
				Arch:      req.SystemInfo.Arch,
				Hostname:  req.SystemInfo.Hostname,
				CPU:       req.SystemInfo.Cpu,
				CPUCores:  int(req.SystemInfo.CpuCores),
				MemoryGB:  int(req.SystemInfo.MemoryGb),
			}
			if data, err := json.Marshal(systemInfo); err == nil {
				desktop.SystemInfo = string(data)
			}
		}

		if err := db.DB.Save(&desktop).Error; err != nil {
			logger.Errorf("更新 Desktop 失败: %v", err)
		}

		logger.Infof("Desktop 登录成功（已存在设备）: id=%d", desktop.ID)
	}

	// 构建响应
	resp := &pb.DesktopLoginResponse{
		Success:   true,
		Message:   "登录成功",
		DesktopId: desktop.ID,
	}

	// 仅首次登录返回 secret
	if isNewDevice {
		resp.Secret = desktopSecret
	}

	// 创建或获取 Tailscale 预认证密钥
	if s.headscaleClient != nil && s.config != nil {
		authKey, serverURL, err := s.getOrCreateAuthKey(ctx, client.ID, desktop.ID)
		if err != nil {
			logger.Errorf("获取 Tailscale 预认证密钥失败: %v", err)
		} else {
			resp.AuthKey = authKey
			resp.ServerUrl = serverURL
		}
	}

	return resp, nil
}

// Authenticate Desktop 认证
func (s *DesktopServiceServer) Authenticate(ctx context.Context, req *pb.DesktopAuthenticateRequest) (*pb.DesktopAuthenticateResponse, error) {
	logger.Infof("Desktop 认证请求: desktop_id=%d", req.DesktopId)

	// 查询 Desktop
	var desktop model.Desktop
	if err := db.DB.First(&desktop, req.DesktopId).Error; err != nil {
		logger.Warnf("Desktop 不存在: %d", req.DesktopId)
		return &pb.DesktopAuthenticateResponse{
			Success: false,
			Message: "设备不存在",
		}, nil
	}

	// 验证密钥
	if err := bcrypt.CompareHashAndPassword([]byte(desktop.SecretHash), []byte(req.Secret)); err != nil {
		logger.Warnf("Desktop 密钥验证失败: %d", req.DesktopId)
		return &pb.DesktopAuthenticateResponse{
			Success: false,
			Message: "认证失败",
		}, nil
	}

	// 更新 Desktop 信息
	now := time.Now()
	desktop.LastOnline = &now

	if req.SystemInfo != nil {
		systemInfo := model.DesktopSystemInfo{
			OS:        req.SystemInfo.Os,
			OSVersion: req.SystemInfo.OsVersion,
			Arch:      req.SystemInfo.Arch,
			Hostname:  req.SystemInfo.Hostname,
			CPU:       req.SystemInfo.Cpu,
			CPUCores:  int(req.SystemInfo.CpuCores),
			MemoryGB:  int(req.SystemInfo.MemoryGb),
		}
		if data, err := json.Marshal(systemInfo); err == nil {
			desktop.SystemInfo = string(data)
		}
	}

	if err := db.DB.Save(&desktop).Error; err != nil {
		logger.Errorf("更新 Desktop 失败: %v", err)
	}

	// 构建响应
	resp := &pb.DesktopAuthenticateResponse{
		Success: true,
		Message: "认证成功",
	}

	// 检查是否需要重新创建预认证密钥
	if s.headscaleClient != nil && s.config != nil {
		authKey, serverURL, err := s.getOrCreateAuthKey(ctx, desktop.ClientID, desktop.ID)
		if err != nil {
			logger.Errorf("获取 Tailscale 预认证密钥失败: %v", err)
		} else {
			resp.AuthKey = authKey
			resp.ServerUrl = serverURL
		}
	}

	logger.Infof("Desktop 认证成功: %d", req.DesktopId)
	return resp, nil
}

// Heartbeat Desktop 心跳（双向流）
func (s *DesktopServiceServer) Heartbeat(stream pb.DesktopService_HeartbeatServer) error {
	ctx, cancel := context.WithCancel(stream.Context())
	defer cancel()

	var desktopID uint64
	var conn *DesktopConnection

	// 接收第一个心跳消息获取 Desktop ID
	firstReq, err := stream.Recv()
	if err != nil {
		return status.Error(codes.InvalidArgument, "无法接收初始消息")
	}

	desktopID = firstReq.DesktopId
	logger.Infof("Desktop 心跳流建立: desktop_id=%d", desktopID)

	// 验证 Desktop 存在
	var desktop model.Desktop
	if err := db.DB.First(&desktop, desktopID).Error; err != nil {
		return status.Error(codes.NotFound, "Desktop 不存在")
	}

	// 注册连接
	conn = &DesktopConnection{
		DesktopID: desktopID,
		ClientID:  desktop.ClientID,
		Stream:    stream,
		TunnelIP:  firstReq.TunnelIp,
		Connected: firstReq.TunnelConnected,
		LastSeen:  time.Now(),
		Cancel:    cancel,
	}

	s.connMutex.Lock()
	// 如果已有连接，先关闭旧连接
	if oldConn, exists := s.connections[desktopID]; exists {
		oldConn.Cancel()
	}
	s.connections[desktopID] = conn
	s.connMutex.Unlock()

	defer func() {
		s.connMutex.Lock()
		delete(s.connections, desktopID)
		s.connMutex.Unlock()
		logger.Infof("Desktop 心跳流断开: desktop_id=%d", desktopID)
	}()

	// 处理第一个心跳
	s.handleDesktopHeartbeat(desktopID, firstReq)

	// 发送首次响应（包含已授权服务）
	if err := s.sendDesktopHeartbeatResponse(stream, desktop.ClientID); err != nil {
		logger.Errorf("发送首次心跳响应失败: %v", err)
		return err
	}

	// 持续接收心跳
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			req, err := stream.Recv()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return err
			}

			// 更新连接信息
			conn.TunnelIP = req.TunnelIp
			conn.Connected = req.TunnelConnected
			conn.LastSeen = time.Now()

			// 处理心跳
			s.handleDesktopHeartbeat(desktopID, req)

			// 发送响应
			if err := s.sendDesktopHeartbeatResponse(stream, desktop.ClientID); err != nil {
				logger.Errorf("发送心跳响应失败: %v", err)
				return err
			}
		}
	}
}

// handleDesktopHeartbeat 处理 Desktop 心跳请求
func (s *DesktopServiceServer) handleDesktopHeartbeat(desktopID uint64, req *pb.DesktopHeartbeatRequest) {
	// 更新数据库
	now := time.Now()
	if err := db.DB.Model(&model.Desktop{}).Where("id = ?", desktopID).Updates(map[string]interface{}{
		"last_online": now,
		"ip":          req.TunnelIp,
	}).Error; err != nil {
		logger.Errorf("更新 Desktop 心跳失败: %v", err)
	}
}

// sendDesktopHeartbeatResponse 发送 Desktop 心跳响应
func (s *DesktopServiceServer) sendDesktopHeartbeatResponse(stream pb.DesktopService_HeartbeatServer, clientID uint64) error {
	resp := &pb.DesktopHeartbeatResponse{}

	// 查询该 Client 有权访问的服务
	// 这里简化处理，实际应该根据权限系统查询
	var services []model.ProxyService
	if err := db.DB.Preload("Agent").Find(&services).Error; err != nil {
		logger.Errorf("查询服务列表失败: %v", err)
	} else {
		for _, svc := range services {
			if !svc.Enabled {
				continue
			}

			// 检查 Agent 是否在线
			if s.agentService != nil && !s.agentService.IsAgentOnline(svc.AgentID) {
				continue
			}

			agentName := ""
			if svc.Agent != nil {
				agentName = svc.Agent.Name
			}

			resp.AuthorizedServices = append(resp.AuthorizedServices, &pb.AuthorizedService{
				Id:         svc.ID,
				Name:       svc.Name,
				AgentName:  agentName,
				ListenAddr: svc.ListenAddr,
			})
		}
	}

	return stream.Send(resp)
}

// createDesktopNode 为 Desktop 创建 Headscale Node
func (s *DesktopServiceServer) createDesktopNode(ctx context.Context, clientID uint64, deviceName string) (uint64, string, error) {
	// 为每个 Client 创建独立的 Headscale User
	userName := fmt.Sprintf("client-%d", clientID)

	// 获取或创建 User
	user, err := s.headscaleClient.GetOrCreateUser(ctx, userName)
	if err != nil {
		return 0, "", fmt.Errorf("获取或创建 Headscale User 失败: %w", err)
	}

	// 创建预认证密钥（24 小时有效，临时节点）
	authKey, err := s.headscaleClient.CreatePreAuthKey(ctx, user.Id, 24*time.Hour, true)
	if err != nil {
		return 0, "", fmt.Errorf("创建预认证密钥失败: %w", err)
	}

	// 注意：Node ID 在 Tailscale 连接后才能获取
	// 这里暂时返回 0，后续通过心跳更新
	return 0, authKey.Key, nil
}

// getOrCreateAuthKey 获取或创建预认证密钥
func (s *DesktopServiceServer) getOrCreateAuthKey(ctx context.Context, clientID uint64, desktopID uint64) (string, string, error) {
	// 为每个 Client 创建独立的 Headscale User
	userName := fmt.Sprintf("client-%d", clientID)

	// 获取或创建 User
	user, err := s.headscaleClient.GetOrCreateUser(ctx, userName)
	if err != nil {
		return "", "", fmt.Errorf("获取或创建 Headscale User 失败: %w", err)
	}

	// 创建预认证密钥（24 小时有效，临时节点）
	authKey, err := s.headscaleClient.CreatePreAuthKey(ctx, user.Id, 24*time.Hour, true)
	if err != nil {
		return "", "", fmt.Errorf("创建预认证密钥失败: %w", err)
	}

	return authKey.Key, s.config.Tailscale.HeadscalePublicURL, nil
}

// IsDesktopOnline 检查 Desktop 是否在线
func (s *DesktopServiceServer) IsDesktopOnline(desktopID uint64) bool {
	s.connMutex.RLock()
	conn, exists := s.connections[desktopID]
	s.connMutex.RUnlock()

	if exists && time.Since(conn.LastSeen) < 60*time.Second {
		return true
	}

	// 检查数据库
	var desktop model.Desktop
	if err := db.DB.First(&desktop, desktopID).Error; err != nil {
		return false
	}

	if desktop.LastOnline == nil {
		return false
	}

	return time.Since(*desktop.LastOnline) < 60*time.Second
}

// generateSecret 生成随机密钥
func generateSecret() string {
	bytes := make([]byte, 32)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}
