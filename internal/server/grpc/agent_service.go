// Package grpc 提供 gRPC 服务实现
package grpc

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/cache"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/headscale"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

// AgentConnection Agent 连接信息
type AgentConnection struct {
	AgentID   uint64
	Stream    pb.AgentService_HeartbeatServer
	TunnelIP  string
	Connected bool
	LastSeen  time.Time
	Cancel    context.CancelFunc
}

// AgentServiceServer Agent 服务实现
type AgentServiceServer struct {
	pb.UnimplementedAgentServiceServer

	connections map[uint64]*AgentConnection
	connMutex   sync.RWMutex

	configVersion int64
	versionMutex  sync.RWMutex

	headscaleClient *headscale.Client
	config          *config.ServerConfig
}

// NewAgentServiceServer 创建 Agent 服务
func NewAgentServiceServer(cfg *config.ServerConfig) *AgentServiceServer {
	s := &AgentServiceServer{
		connections:   make(map[uint64]*AgentConnection),
		configVersion: time.Now().Unix(),
		config:        cfg,
	}

	if cfg.Tailscale.HeadscaleURL != "" && cfg.Tailscale.HeadscaleAPIKey != "" {
		client, err := headscale.NewClient(headscale.Config{
			URL:    cfg.Tailscale.HeadscaleURL,
			APIKey: cfg.Tailscale.HeadscaleAPIKey,
		})
		if err != nil {
			logger.Errorf("初始化 Headscale 客户端失败: %v", err)
		} else {
			s.headscaleClient = client
			logger.Infof("Headscale 客户端已初始化: %s", cfg.Tailscale.HeadscaleURL)
		}
	} else {
		logger.Warnf("Headscale 配置不完整，Tailscale 功能将不可用")
	}

	return s
}

// Register Agent 注册
func (s *AgentServiceServer) Register(ctx context.Context, req *pb.AgentRegisterRequest) (*pb.AgentRegisterResponse, error) {
	logger.Infof("Agent 注册请求: name=%s, version=%s", req.Name, req.Version)

	// 查询 User（Agent 角色）
	var user model.User
	if err := db.DB.WithContext(ctx).Where("name = ? AND role = ?", req.Name, model.UserRoleAgent).First(&user).Error; err != nil {
		logger.Warnf("Agent 用户不存在: %s", req.Name)
		return &pb.AgentRegisterResponse{
			Success: false,
			Message: "Agent 不存在",
		}, nil
	}

	// 验证密钥
	if err := bcrypt.CompareHashAndPassword([]byte(user.SecretHash), []byte(req.Secret)); err != nil {
		logger.Warnf("Agent 密钥验证失败: %s", req.Name)
		return &pb.AgentRegisterResponse{
			Success: false,
			Message: "认证失败",
		}, nil
	}

	// 查询或创建 Node（Agent 类型按 user_id + type 唯一）
	var node model.Node
	// 设备名：优先使用 SystemInfo.Hostname，否则使用 Agent 名称
	deviceName := req.Name
	if req.SystemInfo != nil && req.SystemInfo.Hostname != "" {
		deviceName = req.SystemInfo.Hostname
	}
	if err := db.DB.WithContext(ctx).Where("user_id = ? AND type = ?", user.ID, model.NodeTypeAgent).First(&node).Error; err != nil {
		// 创建新 Node
		node = model.Node{
			UserID: user.ID,
			Name:   deviceName,
			Type:   model.NodeTypeAgent,
		}
		db.DB.WithContext(ctx).Create(&node)
	} else {
		// 更新 Node 名称
		node.Name = deviceName
	}

	// 更新 Node 信息
	now := time.Now()
	node.LastHeartbeat = &now
	if req.Version != "" {
		node.Version = req.Version
	}
	if req.SystemInfo != nil {
		systemInfo := model.NodeSystemInfo{
			OS:        req.SystemInfo.Os,
			OSVersion: req.SystemInfo.OsVersion,
			Arch:      req.SystemInfo.Arch,
			Hostname:  req.SystemInfo.Hostname,
			CPU:       req.SystemInfo.Cpu,
			CPUCores:  int(req.SystemInfo.CpuCores),
			MemoryGB:  int(req.SystemInfo.MemoryGb),
		}
		if data, err := json.Marshal(systemInfo); err == nil {
			node.SystemInfo = string(data)
		}
		node.Hostname = req.SystemInfo.Hostname
	}

	if err := db.DB.WithContext(ctx).Save(&node).Error; err != nil {
		logger.Errorf("更新 Node 信息失败: %v", err)
	}

	// 构建响应
	resp := &pb.AgentRegisterResponse{
		Success: true,
		Message: "注册成功",
		AgentId: user.ID,
	}

	// 创建 Tailscale 预认证密钥
	if s.headscaleClient != nil && s.config != nil {
		authKey, serverURL, err := s.createAgentAuthKey(ctx, req.Name, user.ID)
		if err != nil {
			logger.Errorf("创建 Tailscale 预认证密钥失败: %v", err)
		} else {
			resp.AuthKey = authKey
			resp.ServerUrl = serverURL
			logger.Infof("已为 Agent %s 创建 Tailscale 预认证密钥", req.Name)
		}
	}

	logger.Infof("Agent 注册成功: %s (ID: %d)", req.Name, user.ID)
	return resp, nil
}

// Authenticate Agent 认证
func (s *AgentServiceServer) Authenticate(ctx context.Context, req *pb.AgentAuthenticateRequest) (*pb.AgentAuthenticateResponse, error) {
	logger.Infof("Agent 认证请求: agent_id=%d, version=%s", req.AgentId, req.Version)

	// 查询 User
	var user model.User
	if err := db.DB.WithContext(ctx).First(&user, req.AgentId).Error; err != nil {
		logger.Warnf("Agent 用户不存在: %d", req.AgentId)
		return &pb.AgentAuthenticateResponse{
			Success: false,
			Message: "Agent 不存在",
		}, nil
	}

	if user.Role != model.UserRoleAgent {
		return &pb.AgentAuthenticateResponse{
			Success: false,
			Message: "用户角色不是 Agent",
		}, nil
	}

	// 验证密钥
	if err := bcrypt.CompareHashAndPassword([]byte(user.SecretHash), []byte(req.Secret)); err != nil {
		logger.Warnf("Agent 密钥验证失败: %d", req.AgentId)
		return &pb.AgentAuthenticateResponse{
			Success: false,
			Message: "认证失败",
		}, nil
	}

	// 查询或创建 Node
	var node model.Node
	if err := db.DB.WithContext(ctx).Where("user_id = ? AND type = ?", user.ID, model.NodeTypeAgent).First(&node).Error; err != nil {
		node = model.Node{
			UserID: user.ID,
			Name:   user.Name,
			Type:   model.NodeTypeAgent,
		}
		db.DB.WithContext(ctx).Create(&node)
	}

	// 更新 Node 信息
	now := time.Now()
	node.LastHeartbeat = &now
	if req.Version != "" {
		node.Version = req.Version
	}
	if req.SystemInfo != nil {
		systemInfo := model.NodeSystemInfo{
			OS:        req.SystemInfo.Os,
			OSVersion: req.SystemInfo.OsVersion,
			Arch:      req.SystemInfo.Arch,
			Hostname:  req.SystemInfo.Hostname,
			CPU:       req.SystemInfo.Cpu,
			CPUCores:  int(req.SystemInfo.CpuCores),
			MemoryGB:  int(req.SystemInfo.MemoryGb),
		}
		if data, err := json.Marshal(systemInfo); err == nil {
			node.SystemInfo = string(data)
		}
		node.Hostname = req.SystemInfo.Hostname
	}

	if err := db.DB.WithContext(ctx).Save(&node).Error; err != nil {
		logger.Errorf("更新 Node 信息失败: %v", err)
	}

	// 构建响应
	resp := &pb.AgentAuthenticateResponse{
		Success: true,
		Message: "认证成功",
	}

	// 检查是否需要重新创建预认证密钥
	if s.headscaleClient != nil && s.config != nil {
		needAuthKey := false

		if node.HeadscaleNodeID == 0 {
			needAuthKey = true
		} else {
			hsNode, err := s.headscaleClient.GetNode(ctx, node.HeadscaleNodeID)
			if err != nil || hsNode == nil {
				needAuthKey = true
			}
		}

		if needAuthKey {
			authKey, serverURL, err := s.createAgentAuthKey(ctx, user.Name, user.ID)
			if err != nil {
				logger.Errorf("创建 Tailscale 预认证密钥失败: %v", err)
			} else {
				resp.AuthKey = authKey
				resp.ServerUrl = serverURL
				logger.Infof("已为 Agent %s 创建新的 Tailscale 预认证密钥", user.Name)
			}
		} else {
			resp.ServerUrl = s.config.Tailscale.HeadscalePublicURL
		}
	}

	logger.Infof("Agent 认证成功: %d", req.AgentId)
	return resp, nil
}

// Heartbeat Agent 心跳（双向流）
func (s *AgentServiceServer) Heartbeat(stream pb.AgentService_HeartbeatServer) error {
	ctx, cancel := context.WithCancel(stream.Context())
	defer cancel()

	var agentID uint64
	var conn *AgentConnection

	// 接收第一个心跳消息获取 Agent ID
	firstReq, err := stream.Recv()
	if err != nil {
		return err
	}

	agentID = firstReq.AgentId
	logger.Infof("Agent 心跳流建立: agent_id=%d", agentID)

	// 验证 Agent 存在
	var user model.User
	if err := db.DB.WithContext(ctx).First(&user, agentID).Error; err != nil {
		return fmt.Errorf("Agent 不存在: %d", agentID)
	}

	if user.Role != model.UserRoleAgent {
		return fmt.Errorf("用户角色不是 Agent: %d", agentID)
	}

	// 注册连接
	conn = &AgentConnection{
		AgentID:   agentID,
		Stream:    stream,
		TunnelIP:  firstReq.TunnelIp,
		Connected: firstReq.TunnelConnected,
		LastSeen:  time.Now(),
		Cancel:    cancel,
	}

	s.connMutex.Lock()
	// 如果已有连接，先关闭旧连接
	if oldConn, exists := s.connections[agentID]; exists {
		oldConn.Cancel()
	}
	s.connections[agentID] = conn
	s.connMutex.Unlock()

	defer func() {
		s.connMutex.Lock()
		delete(s.connections, agentID)
		s.connMutex.Unlock()

		// Agent 断连时，将其所有域名设为离线
		if err := db.DB.Model(&model.DomainRegistry{}).
			Where("agent_user_id = ? AND status = ?", agentID, model.DomainStatusOnline).
			Update("status", model.DomainStatusOffline).Error; err != nil {
			logger.Errorf("Agent 断连时设置域名离线失败: agent_id=%d, err=%v", agentID, err)
		} else {
			logger.Infof("Agent 断连，域名已设为离线: agent_id=%d", agentID)
		}
	}()

	// 处理第一个心跳
	s.handleHeartbeat(ctx, agentID, firstReq)

	// 发送首次响应
	if err := s.sendHeartbeatResponse(ctx, stream, agentID); err != nil {
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
			if err != nil {
				return err
			}

			// 更新连接信息
			conn.TunnelIP = req.TunnelIp
			conn.Connected = req.TunnelConnected
			conn.LastSeen = time.Now()

			// 处理心跳
			s.handleHeartbeat(ctx, agentID, req)

			// 发送响应
			if err := s.sendHeartbeatResponse(ctx, stream, agentID); err != nil {
				logger.Errorf("发送心跳响应失败: %v", err)
				return err
			}
		}
	}
}

// handleHeartbeat 处理心跳请求
func (s *AgentServiceServer) handleHeartbeat(ctx context.Context, agentID uint64, req *pb.AgentHeartbeatRequest) {
	// 更新 Node 信息
	now := time.Now()
	updates := map[string]any{
		"last_heartbeat": now,
		"ip":             req.TunnelIp,
	}

	// 如果 Headscale 客户端可用，查询并更新 HeadscaleNodeID
	if s.headscaleClient != nil {
		// 先查询当前 Node 的 HeadscaleNodeID 和对应的 User 名称
		var node model.Node
		var user model.User
		if err := db.DB.WithContext(ctx).Where("user_id = ? AND type = ?", agentID, model.NodeTypeAgent).First(&node).Error; err == nil {
			// 只有当 HeadscaleNodeID 为 0 时才查询 Headscale
			if node.HeadscaleNodeID == 0 {
				// 查询 User 获取名称
				if err := db.DB.WithContext(ctx).First(&user, agentID).Error; err == nil {
					// Headscale User 命名规则: agent-{name}
					hsUserName := fmt.Sprintf("agent-%s", user.Name)
					hsNode, err := s.headscaleClient.GetNodeByUser(ctx, hsUserName)
					if err != nil {
						logger.Warnf("通过 User 查询 Headscale 节点失败: %v", err)
					} else if hsNode != nil {
						updates["headscale_node_id"] = hsNode.Id
						logger.Infof("Agent %d 关联 Headscale 节点: id=%d, name=%s, user=%s", agentID, hsNode.Id, hsNode.GivenName, hsUserName)
					}
				}
			}
		}
	}

	if err := db.DB.WithContext(ctx).Model(&model.Node{}).
		Where("user_id = ? AND type = ?", agentID, model.NodeTypeAgent).
		Updates(updates).Error; err != nil {
		logger.Errorf("更新 Node 心跳失败: %v", err)
	}

	// 处理域名注册上报
	if len(req.DomainRegistrations) > 0 {
		s.handleDomainRegistrations(ctx, agentID, req.DomainRegistrations)
	}
}

// sendHeartbeatResponse 发送心跳响应
func (s *AgentServiceServer) sendHeartbeatResponse(ctx context.Context, stream pb.AgentService_HeartbeatServer, agentID uint64) error {
	s.versionMutex.RLock()
	configVersion := s.configVersion
	s.versionMutex.RUnlock()

	resp := &pb.AgentHeartbeatResponse{
		ConfigVersion: configVersion,
	}

	// 查询该 Agent 的服务配置
	var services []model.ProxyService
	if err := db.DB.WithContext(ctx).Where("user_id = ? AND enabled = ?", agentID, true).Find(&services).Error; err != nil {
		logger.Errorf("查询服务配置失败: %v", err)
	} else {
		for _, svc := range services {
			resp.Services = append(resp.Services, &pb.ServiceConfig{
				Id:         svc.ID,
				Name:       svc.Name,
				SourceAddr: svc.SourceAddr,
				TargetAddr: svc.TargetAddr,
			})
		}
	}

	// 查询端口转发配置
	var forwards []model.PortForward
	if err := db.DB.WithContext(ctx).Where("user_id = ? AND enabled = ?", agentID, true).Find(&forwards).Error; err != nil {
		logger.Errorf("查询端口转发配置失败: %v", err)
	} else {
		for _, fwd := range forwards {
			resp.Forwards = append(resp.Forwards, &pb.ForwardConfig{
				Id:         fwd.ID,
				ServiceId:  fwd.TargetServiceID,
				SourceAddr: fwd.SourceAddr,
				TargetAddr: fwd.TargetAddr,
			})
		}
	}

	return stream.Send(resp)
}

// GetRealtimeStatus 获取 Agent 实时状态
func (s *AgentServiceServer) GetRealtimeStatus(ctx context.Context, req *pb.GetRealtimeStatusRequest) (*pb.GetRealtimeStatusResponse, error) {
	s.connMutex.RLock()
	conn, exists := s.connections[req.AgentId]
	s.connMutex.RUnlock()

	if !exists {
		return &pb.GetRealtimeStatusResponse{
			TunnelConnected: false,
		}, nil
	}

	return &pb.GetRealtimeStatusResponse{
		TunnelIp:        conn.TunnelIP,
		TunnelConnected: conn.Connected,
	}, nil
}

// createAgentAuthKey 为 Agent 创建 Tailscale 预认证密钥
func (s *AgentServiceServer) createAgentAuthKey(ctx context.Context, agentName string, agentID uint64) (string, string, error) {
	if s.headscaleClient == nil {
		return "", "", fmt.Errorf("Headscale 客户端未初始化")
	}

	// User 命名规则: agent-{name}
	userName := fmt.Sprintf("agent-%s", agentName)

	// 获取或创建 User
	user, err := s.headscaleClient.GetUserByName(ctx, userName)
	if err != nil {
		return "", "", fmt.Errorf("查询 Headscale User 失败: %w", err)
	}
	if user == nil {
		// 创建 User
		user, err = s.headscaleClient.CreateUser(ctx, userName)
		if err != nil {
			return "", "", fmt.Errorf("创建 Headscale User 失败: %w", err)
		}
	}

	// 注意：不再删除旧节点，让 Agent 复用现有节点以保持 IP 稳定
	// Agent 使用 tsnet.Server 的 StateDir 持久化状态，重启后会自动复用现有节点

	// 构建 Tags 列表
	// 身份 Tag: tag:agent-{name}
	tags := []string{fmt.Sprintf("tag:agent-%s", agentName)}

	// 查询 Agent 所属的分组，添加分组 Tag
	var groupMembers []model.GroupMember
	if err := db.DB.WithContext(ctx).Preload("Group").Where("user_id = ?", agentID).Find(&groupMembers).Error; err == nil {
		for _, gm := range groupMembers {
			if gm.Group != nil {
				// 分组 Tag: tag:group-{group.name}
				tags = append(tags, fmt.Sprintf("tag:group-%s", gm.Group.Name))
			}
		}
	}

	// 创建预认证密钥（24 小时有效，持久节点，带 Tags）
	// Agent 使用持久节点以保持 IP 稳定，避免断开后节点被删除
	authKey, err := s.headscaleClient.CreatePreAuthKeyWithTags(ctx, user.Id, 24*time.Hour, false, tags)
	if err != nil {
		return "", "", fmt.Errorf("创建预认证密钥失败: %w", err)
	}

	return authKey.Key, s.config.Tailscale.HeadscalePublicURL, nil
}

// IsAgentOnline 检查 Agent 是否在线
func (s *AgentServiceServer) IsAgentOnline(agentID uint64) bool {
	s.connMutex.RLock()
	conn, exists := s.connections[agentID]
	s.connMutex.RUnlock()

	if exists && time.Since(conn.LastSeen) < 60*time.Second {
		return true
	}

	// 检查数据库中的 Node（状态检查不需要 trace，使用 background context）
	var node model.Node
	ctx := context.Background()
	if err := db.DB.WithContext(ctx).Where("user_id = ? AND type = ?", agentID, model.NodeTypeAgent).First(&node).Error; err != nil {
		return false
	}

	if node.LastHeartbeat == nil {
		return false
	}

	return time.Since(*node.LastHeartbeat) < 60*time.Second
}

// GetAgentConnection 获取 Agent 连接
func (s *AgentServiceServer) GetAgentConnection(agentID uint64) *AgentConnection {
	s.connMutex.RLock()
	defer s.connMutex.RUnlock()
	return s.connections[agentID]
}

// NotifyConfigChange 通知配置变更
func (s *AgentServiceServer) NotifyConfigChange() {
	s.versionMutex.Lock()
	s.configVersion = time.Now().Unix()
	s.versionMutex.Unlock()
}

// ReportProxyStatus 上报代理服务状态
func (s *AgentServiceServer) ReportProxyStatus(ctx context.Context, req *pb.ReportProxyStatusRequest) (*pb.ReportProxyStatusResponse, error) {
	logger.Debugf("收到代理服务状态上报: agent_id=%d, count=%d", req.AgentId, len(req.Statuses))

	// 更新缓存中的服务状态
	for _, status := range req.Statuses {
		cache.UpdateProxyServiceStatus(status.ServiceId, status.Status, status.ErrorCode, status.ErrorMsg)
	}

	return &pb.ReportProxyStatusResponse{
		Success: true,
	}, nil
}

// ReportVisitorStatus 上报访问者状态
func (s *AgentServiceServer) ReportVisitorStatus(ctx context.Context, req *pb.ReportVisitorStatusRequest) (*pb.ReportVisitorStatusResponse, error) {
	logger.Debugf("收到访问者状态上报: agent_id=%d, count=%d", req.AgentId, len(req.Statuses))

	return &pb.ReportVisitorStatusResponse{
		Success: true,
	}, nil
}

// ReportNetworkChange 上报网络变化
func (s *AgentServiceServer) ReportNetworkChange(ctx context.Context, req *pb.ReportNetworkChangeRequest) (*pb.ReportNetworkChangeResponse, error) {
	logger.Infof("收到网络变化上报: agent_id=%d, networks=%d", req.AgentId, len(req.Networks))

	return &pb.ReportNetworkChangeResponse{
		Success: true,
	}, nil
}

// handleDomainRegistrations 处理 Agent 心跳中的域名注册上报
// 逻辑：遍历上报的域名列表，逐条 upsert 到 domain_registry 表
func (s *AgentServiceServer) handleDomainRegistrations(ctx context.Context, agentID uint64, registrations []*pb.DomainRegistration) {
	registered := 0
	updated := 0

	for _, reg := range registrations {
		var existing model.DomainRegistry
		err := db.DB.WithContext(ctx).Where("domain = ?", reg.Domain).First(&existing).Error

		if err != nil {
			// 不存在，创建
			record := model.DomainRegistry{
				Domain:      reg.Domain,
				Type:        model.DomainType(reg.Type),
				AgentUserID: agentID,
				EndpointID:  reg.EndpointId,
				TargetIP:    reg.TargetIp,
				TargetPort:  int(reg.TargetPort),
				Namespace:   reg.Namespace,
				ServiceName: reg.ServiceName,
				Status:      model.DomainStatusOnline,
			}
			if err := db.DB.WithContext(ctx).Create(&record).Error; err != nil {
				logger.Errorf("域名注册失败: domain=%s, err=%v", reg.Domain, err)
				continue
			}
			registered++
		} else {
			// 存在，更新
			updates := map[string]any{
				"type":          model.DomainType(reg.Type),
				"agent_user_id": agentID,
				"endpoint_id":   reg.EndpointId,
				"target_ip":     reg.TargetIp,
				"target_port":   int(reg.TargetPort),
				"namespace":     reg.Namespace,
				"service_name":  reg.ServiceName,
				"status":        model.DomainStatusOnline,
			}
			if err := db.DB.WithContext(ctx).Model(&existing).Updates(updates).Error; err != nil {
				logger.Errorf("域名更新失败: domain=%s, err=%v", reg.Domain, err)
				continue
			}
			updated++
		}
	}

	logger.Infof("Agent 域名注册上报完成: agent_id=%d, registered=%d, updated=%d", agentID, registered, updated)
}
