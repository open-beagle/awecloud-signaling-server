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
	// 状态检查不需要 trace，使用 background context
	ctx := context.Background()
	var node model.Node
	if err := db.DB.WithContext(ctx).First(&node, nodeID).Error; err != nil {
		return false
	}
	if node.LastHeartbeat == nil {
		return false
	}
	return time.Since(*node.LastHeartbeat) < 60*time.Second
}

// GetAuthorizedHosts 获取已授权主机列表
func (s *DesktopServiceServer) GetAuthorizedHosts(ctx context.Context, req *pb.GetAuthorizedHostsRequest) (*pb.GetAuthorizedHostsResponse, error) {
	// 验证 Desktop 是否存在
	var node model.Node
	if err := db.DB.WithContext(ctx).First(&node, req.DesktopId).Error; err != nil {
		return nil, status.Error(codes.NotFound, "Desktop 不存在")
	}
	if node.Type != model.NodeTypeDesktop {
		return nil, status.Error(codes.InvalidArgument, "设备类型错误")
	}

	// 获取用户信息
	var user model.User
	if err := db.DB.WithContext(ctx).First(&user, node.UserID).Error; err != nil {
		return nil, status.Error(codes.NotFound, "用户不存在")
	}

	// 查询已授权的 Agent（通过服务权限）
	var services []model.ProxyService
	if err := db.DB.WithContext(ctx).Preload("User").Where("enabled = ?", true).Find(&services).Error; err != nil {
		return nil, status.Error(codes.Internal, "查询服务失败")
	}

	// 按 Agent 分组统计
	hostMap := make(map[uint64]*pb.AuthorizedHost)
	for _, svc := range services {
		if svc.User == nil {
			continue
		}

		// 检查 Agent 是否在线
		agentOnline := false
		if s.agentService != nil {
			agentOnline = s.agentService.IsAgentOnline(svc.UserID)
		}

		// 获取或创建主机信息
		host, exists := hostMap[svc.UserID]
		if !exists {
			// 查询 Agent 节点信息
			var agentNode model.Node
			tunnelIP := ""
			lastSeen := ""
			if err := db.DB.WithContext(ctx).Where("user_id = ? AND type = ?", svc.UserID, model.NodeTypeAgent).First(&agentNode).Error; err == nil {
				tunnelIP = agentNode.IP
				if agentNode.LastHeartbeat != nil {
					lastSeen = agentNode.LastHeartbeat.Format(time.RFC3339)
				}
			}

			host = &pb.AuthorizedHost{
				HostId:       fmt.Sprintf("%d", svc.UserID),
				HostName:     svc.User.Name,
				TunnelIp:     tunnelIP,
				ServiceCount: 0,
				Status:       "offline",
				LastSeen:     lastSeen,
			}
			if agentOnline {
				host.Status = "online"
			}
			hostMap[svc.UserID] = host
		}

		// 增加服务计数
		host.ServiceCount++
	}

	// 转换为列表
	resp := &pb.GetAuthorizedHostsResponse{
		Hosts: make([]*pb.AuthorizedHost, 0, len(hostMap)),
	}
	for _, host := range hostMap {
		resp.Hosts = append(resp.Hosts, host)
	}

	logger.Infof("Desktop %d 获取已授权主机列表: %d 个主机", req.DesktopId, len(resp.Hosts))
	return resp, nil
}

// GetHostServices 获取指定主机的服务列表
func (s *DesktopServiceServer) GetHostServices(ctx context.Context, req *pb.GetHostServicesRequest) (*pb.GetHostServicesResponse, error) {
	// 验证 Desktop 是否存在
	var node model.Node
	if err := db.DB.WithContext(ctx).First(&node, req.DesktopId).Error; err != nil {
		return nil, status.Error(codes.NotFound, "Desktop 不存在")
	}
	if node.Type != model.NodeTypeDesktop {
		return nil, status.Error(codes.InvalidArgument, "设备类型错误")
	}

	// 解析主机 ID
	var hostUserID uint64
	if _, err := fmt.Sscanf(req.HostId, "%d", &hostUserID); err != nil {
		return nil, status.Error(codes.InvalidArgument, "主机ID格式错误")
	}

	// 查询该主机的所有服务
	var services []model.ProxyService
	if err := db.DB.WithContext(ctx).Preload("User").Where("user_id = ? AND enabled = ?", hostUserID, true).Find(&services).Error; err != nil {
		return nil, status.Error(codes.Internal, "查询服务失败")
	}

	// 检查 Agent 是否在线
	agentOnline := false
	if s.agentService != nil {
		agentOnline = s.agentService.IsAgentOnline(hostUserID)
	}

	// 转换为响应格式
	resp := &pb.GetHostServicesResponse{
		Services: make([]*pb.AuthorizedService, 0, len(services)),
	}
	for _, svc := range services {
		agentName := ""
		if svc.User != nil {
			agentName = svc.User.Name
		}

		// 服务状态取决于 Agent 状态
		status := "offline"
		if agentOnline {
			status = "online"
		}

		resp.Services = append(resp.Services, &pb.AuthorizedService{
			Id:         svc.ID,
			Name:       svc.Name,
			AgentName:  agentName,
			ListenAddr: svc.SourceAddr,
			TargetAddr: svc.TargetAddr,
		})
		_ = status // 暂时未使用，后续可以添加到 AuthorizedService 消息中
	}

	logger.Infof("Desktop %d 获取主机 %s 的服务列表: %d 个服务", req.DesktopId, req.HostId, len(resp.Services))
	return resp, nil
}

// GetMyDevices 获取我的设备列表
func (s *DesktopServiceServer) GetMyDevices(ctx context.Context, req *pb.GetMyDevicesRequest) (*pb.GetMyDevicesResponse, error) {
	// 验证 Desktop 是否存在
	var currentNode model.Node
	if err := db.DB.WithContext(ctx).First(&currentNode, req.DesktopId).Error; err != nil {
		return nil, status.Error(codes.NotFound, "Desktop 不存在")
	}
	if currentNode.Type != model.NodeTypeDesktop {
		return nil, status.Error(codes.InvalidArgument, "设备类型错误")
	}

	// 查询该用户的所有 Desktop 设备
	var nodes []model.Node
	if err := db.DB.WithContext(ctx).Where("user_id = ? AND type = ?", currentNode.UserID, model.NodeTypeDesktop).Find(&nodes).Error; err != nil {
		return nil, status.Error(codes.Internal, "查询设备失败")
	}

	// 转换为响应格式
	resp := &pb.GetMyDevicesResponse{
		Devices: make([]*pb.DeviceInfo, 0, len(nodes)),
	}
	for _, node := range nodes {
		// 解析系统信息
		var sysInfo model.NodeSystemInfo
		os := "未知"
		arch := "未知"
		hostname := node.Hostname
		if node.SystemInfo != "" {
			if err := json.Unmarshal([]byte(node.SystemInfo), &sysInfo); err == nil {
				os = sysInfo.OS
				if sysInfo.OSVersion != "" {
					os = sysInfo.OSVersion
				}
				arch = sysInfo.Arch
				if sysInfo.Hostname != "" {
					hostname = sysInfo.Hostname
				}
			}
		}

		// 判断设备状态
		deviceStatus := "offline"
		if node.LastHeartbeat != nil && time.Since(*node.LastHeartbeat) < 60*time.Second {
			deviceStatus = "online"
		}

		// 格式化时间
		lastUsedAt := ""
		if node.LastHeartbeat != nil {
			lastUsedAt = node.LastHeartbeat.Format(time.RFC3339)
		}
		createdAt := node.CreatedAt.Format(time.RFC3339)

		resp.Devices = append(resp.Devices, &pb.DeviceInfo{
			DeviceToken: fmt.Sprintf("%d:%s", node.ID, "***"), // 不返回真实 secret
			DeviceName:  node.Name,
			Os:          os,
			Arch:        arch,
			Hostname:    hostname,
			Status:      deviceStatus,
			LastUsedAt:  lastUsedAt,
			CreatedAt:   createdAt,
			IsCurrent:   node.ID == req.DesktopId,
		})
	}

	logger.Infof("Desktop %d 获取设备列表: %d 个设备", req.DesktopId, len(resp.Devices))
	return resp, nil
}

// OfflineDevice 设备下线
func (s *DesktopServiceServer) OfflineDevice(ctx context.Context, req *pb.OfflineDeviceRequest) (*pb.OfflineDeviceResponse, error) {
	// 验证当前 Desktop 是否存在
	var currentNode model.Node
	if err := db.DB.WithContext(ctx).First(&currentNode, req.DesktopId).Error; err != nil {
		return &pb.OfflineDeviceResponse{Success: false, Message: "当前设备不存在"}, nil
	}

	// 解析目标设备 ID
	var targetNodeID uint64
	if _, err := fmt.Sscanf(req.DeviceToken, "%d:", &targetNodeID); err != nil {
		return &pb.OfflineDeviceResponse{Success: false, Message: "设备令牌格式错误"}, nil
	}

	// 验证目标设备是否存在且属于同一用户
	var targetNode model.Node
	if err := db.DB.WithContext(ctx).First(&targetNode, targetNodeID).Error; err != nil {
		return &pb.OfflineDeviceResponse{Success: false, Message: "目标设备不存在"}, nil
	}
	if targetNode.UserID != currentNode.UserID {
		return &pb.OfflineDeviceResponse{Success: false, Message: "无权操作该设备"}, nil
	}
	if targetNode.ID == currentNode.ID {
		return &pb.OfflineDeviceResponse{Success: false, Message: "不能下线当前设备"}, nil
	}

	// 关闭该设备的连接
	s.connMutex.Lock()
	if conn, exists := s.connections[targetNodeID]; exists {
		conn.Cancel()
		delete(s.connections, targetNodeID)
	}
	s.connMutex.Unlock()

	// 更新数据库：清除心跳时间
	if err := db.DB.WithContext(ctx).Model(&targetNode).Update("last_heartbeat", nil).Error; err != nil {
		return &pb.OfflineDeviceResponse{Success: false, Message: "下线失败"}, nil
	}

	logger.Infof("Desktop %d 下线设备 %d", req.DesktopId, targetNodeID)
	return &pb.OfflineDeviceResponse{Success: true, Message: "设备已下线"}, nil
}

// DeleteDevice 删除设备
func (s *DesktopServiceServer) DeleteDevice(ctx context.Context, req *pb.DeleteDeviceRequest) (*pb.DeleteDeviceResponse, error) {
	// 验证当前 Desktop 是否存在
	var currentNode model.Node
	if err := db.DB.WithContext(ctx).First(&currentNode, req.DesktopId).Error; err != nil {
		return &pb.DeleteDeviceResponse{Success: false, Message: "当前设备不存在"}, nil
	}

	// 解析目标设备 ID
	var targetNodeID uint64
	if _, err := fmt.Sscanf(req.DeviceToken, "%d:", &targetNodeID); err != nil {
		return &pb.DeleteDeviceResponse{Success: false, Message: "设备令牌格式错误"}, nil
	}

	// 验证目标设备是否存在且属于同一用户
	var targetNode model.Node
	if err := db.DB.WithContext(ctx).First(&targetNode, targetNodeID).Error; err != nil {
		return &pb.DeleteDeviceResponse{Success: false, Message: "目标设备不存在"}, nil
	}
	if targetNode.UserID != currentNode.UserID {
		return &pb.DeleteDeviceResponse{Success: false, Message: "无权操作该设备"}, nil
	}
	if targetNode.ID == currentNode.ID {
		return &pb.DeleteDeviceResponse{Success: false, Message: "不能删除当前设备"}, nil
	}

	// 关闭该设备的连接
	s.connMutex.Lock()
	if conn, exists := s.connections[targetNodeID]; exists {
		conn.Cancel()
		delete(s.connections, targetNodeID)
	}
	s.connMutex.Unlock()

	// 删除数据库记录
	if err := db.DB.WithContext(ctx).Delete(&targetNode).Error; err != nil {
		return &pb.DeleteDeviceResponse{Success: false, Message: "删除失败"}, nil
	}

	logger.Infof("Desktop %d 删除设备 %d", req.DesktopId, targetNodeID)
	return &pb.DeleteDeviceResponse{Success: true, Message: "设备已删除"}, nil
}

func generateDesktopSecret() string {
	bytes := make([]byte, 32)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// ToggleFavorite 切换服务收藏状态
func (s *DesktopServiceServer) ToggleFavorite(ctx context.Context, req *pb.ToggleFavoriteRequest) (*pb.ToggleFavoriteResponse, error) {
	// 验证 Desktop 是否存在
	var node model.Node
	if err := db.DB.WithContext(ctx).First(&node, req.DesktopId).Error; err != nil {
		return &pb.ToggleFavoriteResponse{Success: false, Message: "设备不存在"}, nil
	}

	// 解析服务 ID (格式: "agent_user_id:service_id")
	var agentUserID, serviceID uint64
	if _, err := fmt.Sscanf(req.ServiceId, "%d:%d", &agentUserID, &serviceID); err != nil {
		return &pb.ToggleFavoriteResponse{Success: false, Message: "服务 ID 格式错误"}, nil
	}

	// 验证服务是否存在
	var service model.ProxyService
	if err := db.DB.WithContext(ctx).Where("user_id = ? AND id = ?", agentUserID, serviceID).First(&service).Error; err != nil {
		return &pb.ToggleFavoriteResponse{Success: false, Message: "服务不存在"}, nil
	}

	// 查询是否已收藏
	var favorite model.ServiceFavorite
	err := db.DB.WithContext(ctx).Where("client_id = ? AND stcp_instance_id = ?", node.UserID, service.ID).First(&favorite).Error

	if err == nil {
		// 已收藏，取消收藏
		if err := db.DB.WithContext(ctx).Delete(&favorite).Error; err != nil {
			return &pb.ToggleFavoriteResponse{Success: false, Message: "取消收藏失败"}, nil
		}
		logger.Infof("Desktop %d 取消收藏服务 %s", req.DesktopId, req.ServiceId)
		return &pb.ToggleFavoriteResponse{Success: true, Message: "已取消收藏", IsFavorite: false}, nil
	} else {
		// 未收藏，添加收藏
		favorite = model.ServiceFavorite{
			ClientID:       int64(node.UserID),
			STCPInstanceID: service.ID,
		}
		if err := db.DB.WithContext(ctx).Create(&favorite).Error; err != nil {
			return &pb.ToggleFavoriteResponse{Success: false, Message: "添加收藏失败"}, nil
		}
		logger.Infof("Desktop %d 收藏服务 %s", req.DesktopId, req.ServiceId)
		return &pb.ToggleFavoriteResponse{Success: true, Message: "已添加收藏", IsFavorite: true}, nil
	}
}

// GetFavoriteServices 获取收藏的服务列表
func (s *DesktopServiceServer) GetFavoriteServices(ctx context.Context, req *pb.GetFavoriteServicesRequest) (*pb.GetFavoriteServicesResponse, error) {
	// 验证 Desktop 是否存在
	var node model.Node
	if err := db.DB.WithContext(ctx).First(&node, req.DesktopId).Error; err != nil {
		return &pb.GetFavoriteServicesResponse{}, nil
	}

	// 查询收藏的服务
	var favorites []model.ServiceFavorite
	if err := db.DB.WithContext(ctx).Where("client_id = ?", node.UserID).Find(&favorites).Error; err != nil {
		return &pb.GetFavoriteServicesResponse{}, nil
	}

	// 构建服务 ID 列表
	serviceIDs := make([]string, 0, len(favorites))
	for _, fav := range favorites {
		// 查询服务的 user_id
		var service model.ProxyService
		if err := db.DB.WithContext(ctx).Select("user_id").First(&service, fav.STCPInstanceID).Error; err == nil {
			serviceID := fmt.Sprintf("%d:%s", service.UserID, fav.STCPInstanceID)
			serviceIDs = append(serviceIDs, serviceID)
		}
	}

	return &pb.GetFavoriteServicesResponse{ServiceIds: serviceIDs}, nil
}
