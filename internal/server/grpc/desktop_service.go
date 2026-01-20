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
	NodeID    uint64
	UserID    uint64
	Stream    pb.DesktopService_HeartbeatServer
	TunnelIP  string
	Connected bool
	LastSeen  time.Time
	Cancel    context.CancelFunc
}

// DesktopServiceServer Desktop 服务实现
type DesktopServiceServer struct {
	pb.UnimplementedDesktopServiceServer
	connections     map[uint64]*DesktopConnection
	connMutex       sync.RWMutex
	headscaleClient *headscale.Client
	config          *config.ServerConfig
	agentService    *AgentServiceServer
}

// NewDesktopServiceServer 创建 Desktop 服务
func NewDesktopServiceServer(cfg *config.ServerConfig) *DesktopServiceServer {
	s := &DesktopServiceServer{
		connections: make(map[uint64]*DesktopConnection),
		config:      cfg,
	}
	if cfg.Tailscale.HeadscaleURL != "" && cfg.Tailscale.HeadscaleAPIKey != "" {
		client, err := headscale.NewClient(headscale.Config{
			URL:    cfg.Tailscale.HeadscaleURL,
			APIKey: cfg.Tailscale.HeadscaleAPIKey,
		})
		if err != nil {
			logger.Errorf("初始化 Desktop 服务 Headscale 客户端失败: %v", err)
		} else {
			s.headscaleClient = client
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

	var user model.User
	if err := db.DB.WithContext(ctx).Where("name = ? AND role = ?", req.ClientName, model.UserRoleClient).First(&user).Error; err != nil {
		return &pb.DesktopLoginResponse{Success: false, Message: "Client 不存在"}, nil
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.SecretHash), []byte(req.ClientSecret)); err != nil {
		return &pb.DesktopLoginResponse{Success: false, Message: "认证失败"}, nil
	}

	var node model.Node
	err := db.DB.WithContext(ctx).Where("user_id = ? AND type = ? AND name = ?", user.ID, model.NodeTypeDesktop, req.DeviceName).First(&node).Error
	isNewDevice := err != nil
	var nodeSecret string

	if isNewDevice {
		nodeSecret = generateDesktopSecret()
		secretHash, _ := bcrypt.GenerateFromPassword([]byte(nodeSecret), bcrypt.DefaultCost)
		var systemInfoJSON string
		if req.SystemInfo != nil {
			si := model.NodeSystemInfo{
				OS: req.SystemInfo.Os, OSVersion: req.SystemInfo.OsVersion, Arch: req.SystemInfo.Arch,
				Hostname: req.SystemInfo.Hostname, CPU: req.SystemInfo.Cpu,
				CPUCores: int(req.SystemInfo.CpuCores), MemoryGB: int(req.SystemInfo.MemoryGb),
			}
			if data, err := json.Marshal(si); err == nil {
				systemInfoJSON = string(data)
			}
		}
		now := time.Now()
		node = model.Node{
			UserID: user.ID, Name: req.DeviceName, Type: model.NodeTypeDesktop,
			SecretHash: string(secretHash), SystemInfo: systemInfoJSON, LastHeartbeat: &now,
		}
		if req.SystemInfo != nil {
			node.Hostname = req.SystemInfo.Hostname
		}
		if err := db.DB.WithContext(ctx).Create(&node).Error; err != nil {
			return &pb.DesktopLoginResponse{Success: false, Message: "创建设备失败"}, nil
		}
	} else {
		nodeSecret = generateDesktopSecret()
		secretHash, _ := bcrypt.GenerateFromPassword([]byte(nodeSecret), bcrypt.DefaultCost)
		node.SecretHash = string(secretHash)
		now := time.Now()
		node.LastHeartbeat = &now
		if req.SystemInfo != nil {
			si := model.NodeSystemInfo{
				OS: req.SystemInfo.Os, OSVersion: req.SystemInfo.OsVersion, Arch: req.SystemInfo.Arch,
				Hostname: req.SystemInfo.Hostname, CPU: req.SystemInfo.Cpu,
				CPUCores: int(req.SystemInfo.CpuCores), MemoryGB: int(req.SystemInfo.MemoryGb),
			}
			if data, err := json.Marshal(si); err == nil {
				node.SystemInfo = string(data)
			}
			node.Hostname = req.SystemInfo.Hostname
		}
		db.DB.WithContext(ctx).Save(&node)
	}

	resp := &pb.DesktopLoginResponse{Success: true, Message: "登录成功", DesktopId: node.ID, Secret: nodeSecret}
	if s.headscaleClient != nil && s.config != nil {
		if authKey, serverURL, err := s.getOrCreateAuthKey(ctx, user.ID, user.Name); err == nil {
			resp.AuthKey = authKey
			resp.ServerUrl = serverURL
		}
	}
	return resp, nil
}

// Authenticate Desktop 认证
func (s *DesktopServiceServer) Authenticate(ctx context.Context, req *pb.DesktopAuthenticateRequest) (*pb.DesktopAuthenticateResponse, error) {
	var node model.Node
	if err := db.DB.WithContext(ctx).First(&node, req.DesktopId).Error; err != nil {
		return &pb.DesktopAuthenticateResponse{Success: false, Message: "设备不存在"}, nil
	}
	if node.Type != model.NodeTypeDesktop {
		return &pb.DesktopAuthenticateResponse{Success: false, Message: "设备类型错误"}, nil
	}
	if err := bcrypt.CompareHashAndPassword([]byte(node.SecretHash), []byte(req.Secret)); err != nil {
		return &pb.DesktopAuthenticateResponse{Success: false, Message: "认证失败"}, nil
	}

	now := time.Now()
	node.LastHeartbeat = &now
	if req.SystemInfo != nil {
		si := model.NodeSystemInfo{
			OS: req.SystemInfo.Os, OSVersion: req.SystemInfo.OsVersion, Arch: req.SystemInfo.Arch,
			Hostname: req.SystemInfo.Hostname, CPU: req.SystemInfo.Cpu,
			CPUCores: int(req.SystemInfo.CpuCores), MemoryGB: int(req.SystemInfo.MemoryGb),
		}
		if data, err := json.Marshal(si); err == nil {
			node.SystemInfo = string(data)
		}
		node.Hostname = req.SystemInfo.Hostname
	}
	db.DB.WithContext(ctx).Save(&node)

	var user model.User
	if err := db.DB.WithContext(ctx).First(&user, node.UserID).Error; err != nil {
		return &pb.DesktopAuthenticateResponse{Success: false, Message: "用户不存在"}, nil
	}

	resp := &pb.DesktopAuthenticateResponse{Success: true, Message: "认证成功"}
	if s.headscaleClient != nil && s.config != nil {
		if authKey, serverURL, err := s.getOrCreateAuthKey(ctx, user.ID, user.Name); err == nil {
			resp.AuthKey = authKey
			resp.ServerUrl = serverURL
		}
	}
	return resp, nil
}

// Heartbeat Desktop 心跳（双向流）
func (s *DesktopServiceServer) Heartbeat(stream pb.DesktopService_HeartbeatServer) error {
	ctx, cancel := context.WithCancel(stream.Context())
	defer cancel()

	firstReq, err := stream.Recv()
	if err != nil {
		return status.Error(codes.InvalidArgument, "无法接收初始消息")
	}

	nodeID := firstReq.DesktopId
	var node model.Node
	if err := db.DB.WithContext(ctx).First(&node, nodeID).Error; err != nil {
		return status.Error(codes.NotFound, "Desktop 不存在")
	}
	if node.Type != model.NodeTypeDesktop {
		return status.Error(codes.InvalidArgument, "设备类型错误")
	}

	conn := &DesktopConnection{
		NodeID: nodeID, UserID: node.UserID, Stream: stream,
		TunnelIP: firstReq.TunnelIp, Connected: firstReq.TunnelConnected,
		LastSeen: time.Now(), Cancel: cancel,
	}

	s.connMutex.Lock()
	if oldConn, exists := s.connections[nodeID]; exists {
		oldConn.Cancel()
	}
	s.connections[nodeID] = conn
	s.connMutex.Unlock()

	defer func() {
		s.connMutex.Lock()
		delete(s.connections, nodeID)
		s.connMutex.Unlock()
	}()

	s.handleDesktopHeartbeat(ctx, nodeID, firstReq)
	if err := s.sendDesktopHeartbeatResponse(ctx, stream, node.UserID); err != nil {
		return err
	}

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
			conn.TunnelIP = req.TunnelIp
			conn.Connected = req.TunnelConnected
			conn.LastSeen = time.Now()
			s.handleDesktopHeartbeat(ctx, nodeID, req)
			if err := s.sendDesktopHeartbeatResponse(ctx, stream, node.UserID); err != nil {
				return err
			}
		}
	}
}

func (s *DesktopServiceServer) handleDesktopHeartbeat(ctx context.Context, nodeID uint64, req *pb.DesktopHeartbeatRequest) {
	now := time.Now()
	db.DB.WithContext(ctx).Model(&model.Node{}).Where("id = ?", nodeID).Updates(map[string]any{
		"last_heartbeat": now, "ip": req.TunnelIp,
	})
}

func (s *DesktopServiceServer) sendDesktopHeartbeatResponse(ctx context.Context, stream pb.DesktopService_HeartbeatServer, userID uint64) error {
	resp := &pb.DesktopHeartbeatResponse{}
	var services []model.ProxyService
	if err := db.DB.WithContext(ctx).Preload("User").Where("enabled = ?", true).Find(&services).Error; err == nil {
		for _, svc := range services {
			if s.agentService != nil && !s.agentService.IsAgentOnline(svc.UserID) {
				continue
			}
			agentName := ""
			if svc.User != nil {
				agentName = svc.User.Name
			}
			resp.AuthorizedServices = append(resp.AuthorizedServices, &pb.AuthorizedService{
				Id: svc.ID, Name: svc.Name, AgentName: agentName,
				ListenAddr: svc.SourceAddr, TargetAddr: svc.TargetAddr,
			})
		}
	}
	return stream.Send(resp)
}

func (s *DesktopServiceServer) getOrCreateAuthKey(ctx context.Context, userID uint64, userName string) (string, string, error) {
	hsUserName := fmt.Sprintf("client-%s", userName)
	user, err := s.headscaleClient.GetUserByName(ctx, hsUserName)
	if err != nil {
		return "", "", fmt.Errorf("查询 Headscale User 失败: %w", err)
	}
	if user == nil {
		user, err = s.headscaleClient.CreateUser(ctx, hsUserName)
		if err != nil {
			return "", "", fmt.Errorf("创建 Headscale User 失败: %w", err)
		}
	}
	tags := []string{fmt.Sprintf("tag:client-%s", userName)}
	var groupMembers []model.GroupMember
	if err := db.DB.WithContext(ctx).Preload("Group").Where("user_id = ?", userID).Find(&groupMembers).Error; err == nil {
		for _, gm := range groupMembers {
			if gm.Group != nil {
				tags = append(tags, fmt.Sprintf("tag:group-%s", gm.Group.Name))
			}
		}
	}
	authKey, err := s.headscaleClient.CreatePreAuthKeyWithTags(ctx, user.Id, 24*time.Hour, true, tags)
	if err != nil {
		return "", "", fmt.Errorf("创建预认证密钥失败: %w", err)
	}
	return authKey.Key, s.config.Tailscale.HeadscalePublicURL, nil
}

func (s *DesktopServiceServer) IsDesktopOnline(nodeID uint64) bool {
	s.connMutex.RLock()
	conn, exists := s.connections[nodeID]
	s.connMutex.RUnlock()
	if exists && time.Since(conn.LastSeen) < 60*time.Second {
		return true
	}
	// 使用 background context，因为这是状态检查
	var node model.Node
	if err := db.DB.First(&node, nodeID).Error; err != nil {
		return false
	}
	if node.LastHeartbeat == nil {
		return false
	}
	return time.Since(*node.LastHeartbeat) < 60*time.Second
}

func generateDesktopSecret() string {
	bytes := make([]byte, 32)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}
