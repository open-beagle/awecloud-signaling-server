// Package grpc 提供 gRPC 服务实现
package grpc

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/cache"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/headscale"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/service"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

// parseJSONStringArray 解析 JSON 字符串数组
func parseJSONStringArray(jsonStr string) []string {
	if jsonStr == "" || jsonStr == "[]" {
		return []string{}
	}
	var result []string
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return []string{}
	}
	return result
}

// AgentConnection Agent 连接信息
// 以 NodeID 为 key 存储，同一 AgentID（UserID）下可以有多个 Node 同时在线
type AgentConnection struct {
	AgentID        uint64
	NodeID         uint64 // 当前心跳流对应的 Node ID（connections map 的 key）
	Stream         pb.AgentService_HeartbeatServer
	TunnelIP       string
	Connected      bool
	LastSeen       time.Time
	Cancel         context.CancelFunc
	HeartbeatCount int // 心跳计数器，用于定期检查用户状态
}

// AgentServiceServer Agent 服务实现
type AgentServiceServer struct {
	pb.UnimplementedAgentServiceServer

	connections map[uint64]*AgentConnection
	connMutex   sync.RWMutex

	configVersion int64
	versionMutex  sync.RWMutex

	// 立即上报标志：当为 true 时，下一次心跳响应会携带 request_immediate_report=true
	requestImmediateReport bool
	immediateReportMutex   sync.Mutex

	headscaleClient *headscale.Client
	config          *config.ServerConfig
	domainService   *service.DomainService
}

// NewAgentServiceServer 创建 Agent 服务
func NewAgentServiceServer(cfg *config.ServerConfig) *AgentServiceServer {
	s := &AgentServiceServer{
		connections:   make(map[uint64]*AgentConnection),
		configVersion: time.Now().Unix(),
		config:        cfg,
		domainService: service.NewDomainService(db.DB),
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
	logger.Infof("Agent 注册请求: version=%s", req.Version)

	// 先通过 DeployToken 查询用户
	var deployToken model.DeployToken
	if err := db.DB.WithContext(ctx).Where("token = ? AND status = ?", req.Secret, model.DeployTokenStatusBound).First(&deployToken).Error; err != nil {
		logger.Warnf("DeployToken 不存在或未绑定: %s", req.Secret)
		return &pb.AgentRegisterResponse{
			Success: false,
			Message: "无效的 Token",
		}, nil
	}

	// 更新 DeployToken 最后使用时间
	deployToken.UpdateLastUsed()
	db.DB.WithContext(ctx).Save(&deployToken)

	// 查询用户
	var user model.User
	if err := db.DB.WithContext(ctx).First(&user, deployToken.UserID).Error; err != nil {
		logger.Warnf("用户不存在: user_id=%d", deployToken.UserID)
		return &pb.AgentRegisterResponse{
			Success: false,
			Message: "用户不存在",
		}, nil
	}

	// 验证用户角色
	if user.Role != model.UserRoleAgent {
		logger.Warnf("用户角色不是 Agent: user_id=%d, role=%s", user.ID, user.Role)
		return &pb.AgentRegisterResponse{
			Success: false,
			Message: "用户角色不匹配",
		}, nil
	}

	// 检查用户是否启用
	if !user.Enabled {
		logger.Warnf("Agent 注册失败: 用户已禁用, name=%s, userId=%d", req.Name, user.ID)
		return &pb.AgentRegisterResponse{
			Success: false,
			Message: "用户已禁用，请联系管理员",
		}, nil
	}

	// 查询或创建 Node（Agent 类型按 user_id + type 唯一）
	var node model.Node
	// Agent 设备名：使用 DeployToken.Name（不使用系统 hostname）
	deviceName := deployToken.Name
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
		Success:    true,
		Message:    "注册成功",
		AgentId:    user.ID,
		UserName:   user.Name,
		DeviceName: node.Name, // 返回 Node.Name（即 DeployToken.Name）
	}

	// 创建 Tailscale 预认证密钥
	if s.headscaleClient != nil && s.config != nil {
		authKey, serverURL, err := s.createAgentAuthKey(ctx, user.Name, user.ID)
		if err != nil {
			logger.Errorf("创建 Tailscale 预认证密钥失败: %v", err)
		} else {
			resp.AuthKey = authKey
			resp.ServerUrl = serverURL
			logger.Infof("已为 Agent %s 创建 Tailscale 预认证密钥", user.Name)
		}
	}

	logger.Infof("Agent 注册成功: %s (ID: %d)", user.Name, user.ID)
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

	// 检查用户是否启用
	if !user.Enabled {
		logger.Warnf("Agent 认证失败: 用户已禁用, agentId=%d, userId=%d, userName=%s", req.AgentId, user.ID, user.Name)
		return &pb.AgentAuthenticateResponse{
			Success: false,
			Message: "用户已禁用，请联系管理员",
		}, nil
	}

	// 验证密钥（支持两种方式：user secret 或 deploy token）
	authenticated := false

	// 方式1：bcrypt 验证 user secret
	if user.SecretHash != "" {
		if err := bcrypt.CompareHashAndPassword([]byte(user.SecretHash), []byte(req.Secret)); err == nil {
			authenticated = true
		}
	}

	// 方式2：deploy token 验证
	if !authenticated {
		var deployToken model.DeployToken
		if err := db.DB.WithContext(ctx).Where(
			"user_id = ? AND token = ? AND status = ?",
			user.ID, req.Secret, model.DeployTokenStatusBound,
		).First(&deployToken).Error; err == nil {
			authenticated = true
			deployToken.UpdateLastUsed()
			db.DB.WithContext(ctx).Save(&deployToken)
		}
	}

	if !authenticated {
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
// connections 以 NodeID 为 key，同一 AgentID 下多个设备独立共存
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
	logger.Infof("Agent 心跳流建立: agent_id=%d, hostname=%s", agentID, firstReq.Hostname)

	// 验证 Agent 存在
	var user model.User
	if err := db.DB.WithContext(ctx).First(&user, agentID).Error; err != nil {
		return fmt.Errorf("Agent 不存在: %d", agentID)
	}

	if user.Role != model.UserRoleAgent && user.Role != model.UserRoleClient {
		return fmt.Errorf("用户角色不支持心跳: %d (role=%s)", agentID, user.Role)
	}

	// 检查用户是否启用
	if !user.Enabled {
		logger.Warnf("Agent 心跳流建立失败: 用户已禁用, agentId=%d, userId=%d, userName=%s", agentID, user.ID, user.Name)
		return status.Error(codes.PermissionDenied, "用户已禁用")
	}

	// 先处理第一个心跳，获取 NodeID（handleHeartbeat 会创建或查询 Node）
	nodeID := s.handleHeartbeat(context.Background(), agentID, firstReq)
	if nodeID == 0 {
		return fmt.Errorf("无法确定 NodeID: agent_id=%d", agentID)
	}

	// 以 NodeID 为 key 注册连接，同一 AgentID 下多个 Node 独立共存
	conn = &AgentConnection{
		AgentID:   agentID,
		NodeID:    nodeID,
		Stream:    stream,
		TunnelIP:  firstReq.TunnelIp,
		Connected: firstReq.TunnelConnected,
		LastSeen:  time.Now(),
		Cancel:    cancel,
	}

	s.connMutex.Lock()
	// 同一 NodeID 的旧连接才关闭（同一设备重连），不影响其他 Node
	if oldConn, exists := s.connections[nodeID]; exists {
		logger.Infof("Node %d 重连，关闭旧连接", nodeID)
		oldConn.Cancel()
	}
	s.connections[nodeID] = conn
	s.connMutex.Unlock()

	logger.Infof("Agent 连接注册: agent_id=%d, node_id=%d, hostname=%s", agentID, nodeID, firstReq.Hostname)

	defer func() {
		s.connMutex.Lock()
		delete(s.connections, nodeID)
		s.connMutex.Unlock()

		// 清理 Node 内存缓存
		cache.DeleteNodeStatus(nodeID)
		logger.Infof("Node 断连，清理内存缓存: node_id=%d", nodeID)

		// 断连时只清理当前 Node 的数据，不影响同 Agent 下其他 Node
		if err := db.DB.Model(&model.Node{}).
			Where("id = ?", nodeID).
			Updates(map[string]any{"ip": "", "last_heartbeat": nil}).Error; err != nil {
			logger.Errorf("Node 断连时清空 IP 失败: node_id=%d, err=%v", nodeID, err)
		}

		// 检查同 Agent 下是否还有其他在线 Node
		hasOtherOnline := false
		s.connMutex.RLock()
		for _, c := range s.connections {
			if c.AgentID == agentID && c.NodeID != nodeID {
				hasOtherOnline = true
				break
			}
		}
		s.connMutex.RUnlock()

		// 只有当该 Agent 下没有其他在线 Node 时，才清理 Endpoint 缓存
		// 因为 Endpoint 是 Agent 级别的（user_id），不是 Node 级别的
		if !hasOtherOnline {
			// 查询该 Agent 的所有 Endpoint，清理缓存
			var endpoints []model.Endpoint
			if err := db.DB.Where("user_id = ?", agentID).Find(&endpoints).Error; err == nil {
				for _, ep := range endpoints {
					cache.DeleteEndpointStatus(ep.Name)
					logger.Infof("Endpoint 断连，清理内存缓存: endpoint_name=%s", ep.Name)
				}
			}
			db.DB.Model(&model.Endpoint{}).Where("user_id = ? AND status = ?", agentID, "online").Update("status", "offline")
		}

		logger.Infof("Node 断连: agent_id=%d, node_id=%d, 同 Agent 其他在线=%v", agentID, nodeID, hasOtherOnline)
	}()

	// 发送首次响应
	if err := s.sendHeartbeatResponse(context.Background(), stream, agentID, nodeID); err != nil {
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

			// 定期检查用户状态（每 10 次心跳检查一次，约 5 分钟）
			if conn.HeartbeatCount%10 == 0 {
				var user model.User
				if err := db.DB.WithContext(context.Background()).First(&user, agentID).Error; err == nil {
					if !user.Enabled {
						logger.Warnf("Agent 心跳检测到用户已禁用，断开连接: agentId=%d, nodeId=%d, userName=%s", agentID, nodeID, user.Name)
						// 清空心跳时间和 IP
						db.DB.Model(&model.Node{}).Where("id = ?", nodeID).Updates(map[string]any{"last_heartbeat": nil, "ip": ""})
						return status.Error(codes.PermissionDenied, fmt.Sprintf("用户已禁用: %d", agentID))
					}
				}
			}
			conn.HeartbeatCount++

			// 处理心跳（使用独立 context）
			newNodeID := s.handleHeartbeat(context.Background(), agentID, req)
			if newNodeID != 0 {
				conn.NodeID = newNodeID
			}

			// 发送响应
			if err := s.sendHeartbeatResponse(context.Background(), stream, agentID, conn.NodeID); err != nil {
				logger.Errorf("发送心跳响应失败: %v", err)
				return err
			}
		}
	}
}

// handleHeartbeat 处理心跳请求，返回对应的 Node ID
func (s *AgentServiceServer) handleHeartbeat(ctx context.Context, agentID uint64, req *pb.AgentHeartbeatRequest) uint64 {
	// 处理 K8S Service 发现数据上报
	if len(req.DiscoveredServices) > 0 {
		discoveredServices := make([]cache.DiscoveredService, 0, len(req.DiscoveredServices))
		for _, ds := range req.DiscoveredServices {
			ports := make([]cache.DiscoveredServicePort, 0, len(ds.Ports))
			for _, p := range ds.Ports {
				ports = append(ports, cache.DiscoveredServicePort{
					Name:     p.Name,
					Port:     p.Port,
					Protocol: p.Protocol,
				})
			}
			discoveredServices = append(discoveredServices, cache.DiscoveredService{
				Namespace:   ds.Namespace,
				ServiceName: ds.ServiceName,
				ClusterIP:   ds.ClusterIp,
				Ports:       ports,
				Labels:      ds.Labels,
			})
		}
		cache.UpdateK8SServiceDiscovery(agentID, discoveredServices)
		logger.Infof("Agent K8S Service 发现数据已更新: agent_id=%d, count=%d", agentID, len(discoveredServices))

		// 为发现的 K8S Service 创建域名（需要先获取 Node 和 User 信息）
		// 注意：这里需要在 Node 创建/查询之后才能调用，所以将逻辑移到后面
		// 暂存发现的 Service 列表，稍后处理
	}

	// 查询用户角色，确定 Node 类型
	var user model.User
	nodeType := model.NodeTypeAgent
	hsPrefix := "agent"
	if err := db.DB.WithContext(ctx).First(&user, agentID).Error; err == nil {
		if user.Role == model.UserRoleClient {
			nodeType = model.NodeTypeDesktop
			hsPrefix = "client"
		}
	}

	// 查询或创建 Node（Agent/Desktop 按 user_id + type + name 唯一）
	// 注意：Node.Name 来自 DeployToken.Name，在 Register 时已设置
	// 心跳时应该按 user_id + type + name 查询，确保每个 Token 对应独立的 Node
	var node model.Node
	nodeName := req.DeviceName // DeviceName 在 Register 时设置为 DeployToken.Name
	if nodeName == "" {
		// 兜底：如果 DeviceName 为空（旧版本 Agent），使用 hostname
		nodeName = req.Hostname
		if nodeName == "" {
			nodeName = user.Name
		}
		logger.Warnf("心跳时 DeviceName 为空，使用 hostname 作为 Node.Name: user_id=%d, hostname=%s", agentID, nodeName)
	}

	if err := db.DB.WithContext(ctx).Where("user_id = ? AND type = ? AND name = ?", agentID, nodeType, nodeName).First(&node).Error; err != nil {
		// Node 不存在，创建新 Node
		// 注意：正常情况下，Node 应该在 Register 时已创建，这里是兜底逻辑
		now := time.Now()
		node = model.Node{
			UserID:        agentID,
			Name:          nodeName,
			Type:          nodeType,
			Hostname:      req.Hostname,
			IP:            req.TunnelIp,
			LastHeartbeat: &now,
		}
		if err := db.DB.WithContext(ctx).Create(&node).Error; err != nil {
			logger.Errorf("创建 Node 失败: user_id=%d, type=%s, name=%s, err=%v", agentID, nodeType, nodeName, err)
		} else {
			logger.Infof("心跳时创建 Node: user_id=%d, name=%s, type=%s", agentID, nodeName, nodeType)
		}
	}

	// 更新 Node 信息
	now := time.Now()
	updates := map[string]any{
		"last_heartbeat": now,
		"ip":             req.TunnelIp,
	}

	if req.Hostname != "" {
		updates["hostname"] = req.Hostname
	}

	// 更新 Node 内存缓存
	cache.SetNodeStatus(node.ID, cache.NodeStatus{
		NodeID:        node.ID,
		UserID:        agentID,
		TunnelIP:      req.TunnelIp,
		LastHeartbeat: now,
	})
	logger.Debugf("更新 Node 内存缓存: node_id=%d, tunnel_ip=%s", node.ID, req.TunnelIp)

	// 如果 Headscale 客户端可用，查询并更新 HeadscaleNodeID
	if s.headscaleClient != nil && node.HeadscaleNodeID == 0 {
		hsUserName := fmt.Sprintf("%s-%s", hsPrefix, user.Name)
		// 按用户名 + 节点名精确匹配（一个 Headscale 用户下可能有多个节点）
		hsNode, err := s.headscaleClient.GetNodeByUserAndName(ctx, hsUserName, node.Name)
		if err != nil {
			logger.Warnf("通过 User+Name 查询 Headscale 节点失败: %v", err)
		} else if hsNode != nil {
			updates["headscale_node_id"] = hsNode.Id
			logger.Infof("用户 %d 关联 Headscale 节点: id=%d, name=%s, ip=%v, user=%s", agentID, hsNode.Id, hsNode.GivenName, hsNode.IpAddresses, hsUserName)
		} else {
			// 精确匹配失败，回退到第一个节点（兼容单节点场景）
			hsNodeFallback, err := s.headscaleClient.GetNodeByUser(ctx, hsUserName)
			if err != nil {
				logger.Warnf("通过 User 查询 Headscale 节点失败: %v", err)
			} else if hsNodeFallback != nil {
				updates["headscale_node_id"] = hsNodeFallback.Id
				logger.Infof("用户 %d 关联 Headscale 节点(回退): id=%d, name=%s, ip=%v, user=%s", agentID, hsNodeFallback.Id, hsNodeFallback.GivenName, hsNodeFallback.IpAddresses, hsUserName)
			}
		}
	}

	if err := db.DB.WithContext(ctx).Model(&model.Node{}).
		Where("user_id = ? AND type = ? AND name = ?", agentID, nodeType, nodeName).
		Updates(updates).Error; err != nil {
		logger.Errorf("更新 Node 心跳失败: %v", err)
	}

	// 处理域名注册上报
	if len(req.DomainRegistrations) > 0 {
		s.handleDomainRegistrations(ctx, agentID, node.ID, req.DomainRegistrations, req.TunnelIp)
	}

	// 处理已连接的 Endpoint 上报
	if len(req.ConnectedEndpoints) > 0 {
		s.handleConnectedEndpoints(ctx, agentID, node.ID, req.ConnectedEndpoints)
	}

	// 处理操作审计记录上报
	if len(req.AuditRecords) > 0 {
		s.handleAuditRecords(ctx, agentID, req.AuditRecords)
	}

	// 注意：K8S Service 域名现在通过 DomainRegistrations 上报（Agent 统一上报所有域名）
	// 旧的 DiscoveredServices 字段已废弃，不再处理

	return node.ID
}

// sendHeartbeatResponse 发送心跳响应
func (s *AgentServiceServer) sendHeartbeatResponse(ctx context.Context, stream pb.AgentService_HeartbeatServer, agentID uint64, nodeID uint64) error {
	s.versionMutex.RLock()
	configVersion := s.configVersion
	s.versionMutex.RUnlock()

	// 读取域名后缀配置
	domainSuffix := model.DefaultDomainSuffix
	var sysConfig model.SystemConfig
	if err := db.DB.WithContext(ctx).Where("key = ?", model.ConfigDomainSuffix).First(&sysConfig).Error; err == nil {
		if sysConfig.Value != "" {
			domainSuffix = sysConfig.Value
		}
	}

	resp := &pb.AgentHeartbeatResponse{
		ConfigVersion:          configVersion,
		DomainSuffix:           domainSuffix,
		RequestImmediateReport: s.consumeImmediateReport(),
	}

	// 构建 Agent 能力配置
	// SSH：从 User 表读取（User 级别共享）
	// K8S/SVC：从 Node 表读取（Node 级别独立）
	var capUser model.User
	if err := db.DB.WithContext(ctx).First(&capUser, agentID).Error; err == nil {
		capConfig := &pb.AgentCapabilityConfig{
			// SSH：User.SSHEnabled 是 bool 非指针，始终有值，始终下发
			SshEnabled:    capUser.SSHEnabled,
			SshEnabledSet: true,
		}

		// K8S/SVC：从 Node 表读取
		if nodeID > 0 {
			var capNode model.Node
			if err := db.DB.WithContext(ctx).First(&capNode, nodeID).Error; err == nil {
				// K8S API
				if capNode.K8SEnabled != nil {
					capConfig.K8SEnabled = *capNode.K8SEnabled
					capConfig.K8SEnabledSet = true
				}
				if capNode.K8SListenPort != nil {
					capConfig.K8SListenPort = int32(*capNode.K8SListenPort)
					capConfig.K8SListenPortSet = true
				}
				if capNode.K8SApiServer != "" {
					capConfig.K8SApiServer = capNode.K8SApiServer
					capConfig.K8SApiServerSet = true
				}
				// K8S Service
				if capNode.SVCEnabled != nil {
					capConfig.SvcEnabled = *capNode.SVCEnabled
					capConfig.SvcEnabledSet = true
				}
				if capNode.SVCLabelSelector != "" {
					capConfig.SvcLabelSelector = capNode.SVCLabelSelector
					capConfig.SvcLabelSelectorSet = true
				}
				if capNode.SVCNamespaces != "" {
					capConfig.SvcNamespaces = capNode.SVCNamespaces
					capConfig.SvcNamespacesSet = true
				}
				if capNode.SVCListenPortBase != nil {
					capConfig.SvcListenPortBase = int32(*capNode.SVCListenPortBase)
					capConfig.SvcListenPortBaseSet = true
				}

				// Endpoint：先从当前 Node 读取
				if capNode.EndpointEnabled != nil {
					capConfig.EndpointEnabled = *capNode.EndpointEnabled
					capConfig.EndpointEnabledSet = true
				}
				if capNode.EndpointListenPort != nil {
					capConfig.EndpointListenPort = int32(*capNode.EndpointListenPort)
					capConfig.EndpointListenPortSet = true
				}
				if capNode.EndpointToken != "" {
					capConfig.EndpointToken = capNode.EndpointToken
					capConfig.EndpointTokenSet = true
				}

				// Endpoint 回退：当前 Node 没有 Endpoint 配置时，查询同 Agent 下其他 Node
				// 因为 Endpoint 是 Agent 级别功能，可能配置在不同 hostname 的 Node 上
				if !capConfig.EndpointEnabledSet {
					var epNodes []model.Node
					db.DB.WithContext(ctx).
						Where("user_id = ? AND type = ? AND endpoint_enabled = ? AND id != ?", agentID, model.NodeTypeAgent, true, nodeID).
						Limit(1).Find(&epNodes)
					if len(epNodes) > 0 {
						epNode := epNodes[0]
						capConfig.EndpointEnabled = true
						capConfig.EndpointEnabledSet = true
						if epNode.EndpointListenPort != nil {
							capConfig.EndpointListenPort = int32(*epNode.EndpointListenPort)
							capConfig.EndpointListenPortSet = true
						}
						if epNode.EndpointToken != "" {
							capConfig.EndpointToken = epNode.EndpointToken
							capConfig.EndpointTokenSet = true
						}
						logger.Infof("Endpoint 配置回退: 从 Node %d(%s) 读取到 agent_id=%d 的 Endpoint 配置", epNode.ID, epNode.Name, agentID)
					}
				}
			}
		}

		resp.CapabilityConfig = capConfig
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

	// 查询该 Agent 的 K8S API 授权信息
	k8sPerms := s.queryK8SPermissions(ctx, agentID)
	if len(k8sPerms) > 0 {
		resp.K8SPermissions = k8sPerms
	}

	// 查询该 Agent 的 K8S Service 授权信息
	k8sSvcPerms := s.queryK8SServicePermissions(ctx, agentID)
	if len(k8sSvcPerms) > 0 {
		resp.K8SServicePermissions = k8sSvcPerms
	}

	// 查询该 Agent 下所有 Endpoint 的 SSH 授权信息（P12 简化：复用 Agent SSH 授权）
	endpointSSHPerms := s.queryEndpointSSHPermissions(ctx, agentID)
	if len(endpointSSHPerms) > 0 {
		resp.EndpointSshPermissions = endpointSSHPerms
	}

	// 查询该 Agent 关联的 Endpoint K8SAPI 授权信息
	// P11 重构：Endpoint K8SAPI 权限已废弃，统一使用 Agent 级别权限
	// 保留字段以兼容旧版本 Agent，但不再填充数据
	resp.EndpointK8SapiPermissions = nil

	// P10 重构：Endpoint K8SService 权限已废弃，统一使用 Agent 级别权限
	// 保留字段以兼容旧版本 Agent，但不再填充数据
	resp.EndpointK8SservicePermissions = nil

	// 查询该 Agent 关联的所有 Endpoint 能力开关配置（直接从数据库读取，不依赖权限列表）
	var allEndpoints []model.Endpoint
	if err := db.DB.WithContext(ctx).Where("user_id = ? AND revoked = ?", agentID, false).Find(&allEndpoints).Error; err == nil {
		logger.Infof("查询到 %d 个 Endpoint 配置 (agent_id=%d)", len(allEndpoints), agentID)
		for _, ep := range allEndpoints {
			logger.Infof("Endpoint 配置: name=%s, ssh_enabled=%v, ssh_port=%d, k8sapi_enabled=%v, k8sapi_port=%d, k8sapi_api_server=%s, k8sservice_enabled=%v",
				ep.Name, ep.SSHEnabled, ep.SSHPort, ep.K8SAPIEnabled, ep.K8SAPIPort, ep.K8SAPIApiServer, ep.K8SServiceEnabled)
			resp.EndpointCapabilityConfigs = append(resp.EndpointCapabilityConfigs, &pb.EndpointCapabilityConfig{
				EndpointName:      ep.Name,
				SshEnabled:        ep.SSHEnabled,
				SshPort:           uint32(ep.SSHPort), // 新增：从 Endpoint 表读取端口
				K8SapiEnabled:     ep.K8SAPIEnabled,
				K8SapiPort:        uint32(ep.K8SAPIPort), // 新增：从 Endpoint 表读取端口
				K8SapiApiServer:   ep.K8SAPIApiServer,
				K8SserviceEnabled: ep.K8SServiceEnabled,
			})
		}
		logger.Infof("准备发送心跳响应: EndpointCapabilityConfigs 数量=%d", len(resp.EndpointCapabilityConfigs))
	} else {
		logger.Errorf("查询 Endpoint 配置失败 (agent_id=%d): %v", agentID, err)
	}

	return stream.Send(resp)
}

// queryK8SPermissions 查询 Agent 的 K8S API 授权列表
// 包含直接用户授权和分组授权（展开分组成员）
func (s *AgentServiceServer) queryK8SPermissions(ctx context.Context, agentID uint64) []*pb.K8SPermission {
	var result []*pb.K8SPermission

	// 1. 查询直接用户授权
	var userPerms []model.AclK8SUserPermission
	if err := db.DB.WithContext(ctx).Preload("User").
		Where("target_user_id = ? AND enabled = ?", agentID, true).
		Find(&userPerms).Error; err != nil {
		logger.Errorf("查询 K8S 用户授权失败: %v", err)
		return nil
	}

	logger.Debugf("[queryK8SPermissions] agent_id=%d, 查询到 %d 条用户权限", agentID, len(userPerms))

	for _, p := range userPerms {
		if p.User == nil {
			logger.Warnf("[queryK8SPermissions] user_id=%d 的 User 为 nil，跳过", p.UserID)
			continue
		}
		perm := &pb.K8SPermission{
			UserId:     p.UserID,
			UserName:   p.User.Name,
			K8SGroups:  parseJSONStringArray(p.K8SGroups),
			Namespaces: parseJSONStringArray(p.Namespaces),
			IsGroup:    false,
		}
		result = append(result, perm)
		logger.Debugf("[queryK8SPermissions] 添加用户权限: user_id=%d, user_name=%s, k8s_groups=%v", 
			p.UserID, p.User.Name, perm.K8SGroups)
	}

	// 2. 查询分组授权，展开分组成员
	var groupPerms []model.AclK8SGroupPermission
	if err := db.DB.WithContext(ctx).
		Where("target_user_id = ? AND enabled = ?", agentID, true).
		Find(&groupPerms).Error; err != nil {
		logger.Errorf("查询 K8S 分组授权失败: %v", err)
		return result
	}

	for _, gp := range groupPerms {
		k8sGroups := parseJSONStringArray(gp.K8SGroups)
		namespaces := parseJSONStringArray(gp.Namespaces)

		// 展开分组成员
		var members []model.GroupMember
		if err := db.DB.WithContext(ctx).Preload("User").
			Where("group_id = ?", gp.GroupID).
			Find(&members).Error; err != nil {
			logger.Errorf("查询分组成员失败: group_id=%d, err=%v", gp.GroupID, err)
			continue
		}

		for _, m := range members {
			if m.User == nil {
				continue
			}
			result = append(result, &pb.K8SPermission{
				UserId:     m.UserID,
				UserName:   m.User.Name,
				K8SGroups:  k8sGroups,
				Namespaces: namespaces,
				IsGroup:    true,
			})
		}
	}

	return result
}

// queryK8SServicePermissions 查询 Agent 的 K8S Service 授权列表
func (s *AgentServiceServer) queryK8SServicePermissions(ctx context.Context, agentID uint64) []*pb.K8SServicePermission {
	var result []*pb.K8SServicePermission

	// 1. 查询直接用户授权
	var userPerms []model.AclK8SServiceUserPermission
	if err := db.DB.WithContext(ctx).Preload("User").
		Where("target_user_id = ? AND enabled = ?", agentID, true).
		Find(&userPerms).Error; err != nil {
		logger.Errorf("查询 K8SService 用户授权失败: %v", err)
		return nil
	}

	for _, p := range userPerms {
		if p.User == nil {
			continue
		}
		result = append(result, &pb.K8SServicePermission{
			UserId:       p.UserID,
			UserName:     p.User.Name,
			Namespaces:   parseJSONStringArray(p.Namespaces),
			ServiceNames: parseJSONStringArray(p.ServiceNames),
			IsGroup:      false,
		})
	}

	// 2. 查询分组授权，展开分组成员
	var groupPerms []model.AclK8SServiceGroupPermission
	if err := db.DB.WithContext(ctx).
		Where("target_user_id = ? AND enabled = ?", agentID, true).
		Find(&groupPerms).Error; err != nil {
		logger.Errorf("查询 K8SService 分组授权失败: %v", err)
		return result
	}

	for _, gp := range groupPerms {
		namespaces := parseJSONStringArray(gp.Namespaces)
		serviceNames := parseJSONStringArray(gp.ServiceNames)

		var members []model.GroupMember
		if err := db.DB.WithContext(ctx).Preload("User").
			Where("group_id = ?", gp.GroupID).
			Find(&members).Error; err != nil {
			logger.Errorf("查询分组成员失败: group_id=%d, err=%v", gp.GroupID, err)
			continue
		}

		for _, m := range members {
			if m.User == nil {
				continue
			}
			result = append(result, &pb.K8SServicePermission{
				UserId:       m.UserID,
				UserName:     m.User.Name,
				Namespaces:   namespaces,
				ServiceNames: serviceNames,
				IsGroup:      true,
			})
		}
	}

	return result
}

// queryEndpointSSHPermissions 查询 Agent 下所有 Endpoint 的 SSH 授权列表
// P12 简化：Endpoint SSH 复用 Agent SSH 授权
// 用户如果有权限 SSH 到某个 Agent，就自动有权限 SSH 到该 Agent 下的所有 Endpoint
func (s *AgentServiceServer) queryEndpointSSHPermissions(ctx context.Context, agentID uint64) []*pb.EndpointSSHPermission {
	var result []*pb.EndpointSSHPermission

	// 1. 查询该 Agent 下的所有 Endpoint
	var endpoints []model.Endpoint
	if err := db.DB.WithContext(ctx).
		Where("user_id = ?", agentID).
		Find(&endpoints).Error; err != nil {
		logger.Errorf("查询 Endpoint 列表失败: agent_id=%d, err=%v", agentID, err)
		return nil
	}

	if len(endpoints) == 0 {
		return nil
	}

	// 2. 查询 Agent SSH 用户授权
	var userPerms []model.AclSSHUserPermission
	if err := db.DB.WithContext(ctx).Preload("User").
		Where("target_user_id = ? AND enabled = ?", agentID, true).
		Find(&userPerms).Error; err != nil {
		logger.Errorf("查询 SSH 用户授权失败: %v", err)
		return nil
	}

	// 3. 查询 Agent SSH 分组授权
	var groupPerms []model.AclSSHGroupPermission
	if err := db.DB.WithContext(ctx).
		Where("target_user_id = ? AND enabled = ?", agentID, true).
		Find(&groupPerms).Error; err != nil {
		logger.Errorf("查询 SSH 分组授权失败: %v", err)
		return nil
	}

	// 4. 为每个 Endpoint 构建权限列表
	for _, endpoint := range endpoints {
		// 4.1 添加用户授权
		for _, p := range userPerms {
			if p.User == nil {
				continue
			}
			result = append(result, &pb.EndpointSSHPermission{
				EndpointId:   endpoint.ID,
				EndpointName: endpoint.Name,
				UserId:       p.UserID,
				UserName:     p.User.Name,
				SshUsers:     parseJSONStringArray(p.SSHUsers),
				IsGroup:      false,
			})
		}

		// 4.2 添加分组授权（展开分组成员）
		for _, gp := range groupPerms {
			sshUsers := parseJSONStringArray(gp.SSHUsers)

			var members []model.GroupMember
			if err := db.DB.WithContext(ctx).Preload("User").
				Where("group_id = ?", gp.GroupID).
				Find(&members).Error; err != nil {
				logger.Errorf("查询分组成员失败: group_id=%d, err=%v", gp.GroupID, err)
				continue
			}

			for _, m := range members {
				if m.User == nil {
					continue
				}
				result = append(result, &pb.EndpointSSHPermission{
					EndpointId:   endpoint.ID,
					EndpointName: endpoint.Name,
					UserId:       m.UserID,
					UserName:     m.User.Name,
					SshUsers:     sshUsers,
					IsGroup:      true,
				})
			}
		}
	}

	logger.Debugf("[queryEndpointSSHPermissions] agent_id=%d, endpoints=%d, permissions=%d",
		agentID, len(endpoints), len(result))

	return result
}

// GetRealtimeStatus 获取 Agent 实时状态
// 遍历 connections 查找该 AgentID 下的所有在线 Node
func (s *AgentServiceServer) GetRealtimeStatus(ctx context.Context, req *pb.GetRealtimeStatusRequest) (*pb.GetRealtimeStatusResponse, error) {
	s.connMutex.RLock()
	defer s.connMutex.RUnlock()

	// 查找该 Agent 下任意一个在线连接（返回第一个找到的）
	for _, conn := range s.connections {
		if conn.AgentID == req.AgentId {
			return &pb.GetRealtimeStatusResponse{
				TunnelIp:        conn.TunnelIP,
				TunnelConnected: conn.Connected,
			}, nil
		}
	}

	return &pb.GetRealtimeStatusResponse{
		TunnelConnected: false,
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

// IsAgentOnline 检查 Agent 是否在线（该 AgentID 下任意一个 Node 在线即为在线）
func (s *AgentServiceServer) IsAgentOnline(agentID uint64) bool {
	s.connMutex.RLock()
	for _, conn := range s.connections {
		if conn.AgentID == agentID && time.Since(conn.LastSeen) < 60*time.Second {
			s.connMutex.RUnlock()
			return true
		}
	}
	s.connMutex.RUnlock()

	// 内存中没找到，回退到数据库检查
	var node model.Node
	ctx := context.Background()
	if err := db.DB.WithContext(ctx).Where("user_id = ? AND type = ?", agentID, model.NodeTypeAgent).
		Order("last_heartbeat DESC").First(&node).Error; err != nil {
		return false
	}

	if node.LastHeartbeat == nil {
		return false
	}

	return time.Since(*node.LastHeartbeat) < 60*time.Second
}

// DisconnectAgent 断开指定 Agent 的所有连接
func (s *AgentServiceServer) DisconnectAgent(agentID uint64) {
	s.connMutex.Lock()
	defer s.connMutex.Unlock()

	// 遍历所有连接，断开属于该 Agent 的所有 Node 连接
	for nodeID, conn := range s.connections {
		if conn.AgentID == agentID {
			conn.Cancel()
			delete(s.connections, nodeID)
			logger.Infof("已断开 Agent 连接: agentId=%d, nodeId=%d", agentID, nodeID)
		}
	}
}

// GetAgentConnection 获取 Agent 连接（返回该 AgentID 下第一个在线连接）
func (s *AgentServiceServer) GetAgentConnection(agentID uint64) *AgentConnection {
	s.connMutex.RLock()
	defer s.connMutex.RUnlock()
	for _, conn := range s.connections {
		if conn.AgentID == agentID {
			return conn
		}
	}
	return nil
}

// GetAgentConnections 获取 Agent 下所有在线连接
func (s *AgentServiceServer) GetAgentConnections(agentID uint64) []*AgentConnection {
	s.connMutex.RLock()
	defer s.connMutex.RUnlock()
	var result []*AgentConnection
	for _, conn := range s.connections {
		if conn.AgentID == agentID {
			result = append(result, conn)
		}
	}
	return result
}

// NotifyConfigChange 通知配置变更
func (s *AgentServiceServer) NotifyConfigChange() {
	s.versionMutex.Lock()
	s.configVersion = time.Now().Unix()
	s.versionMutex.Unlock()
}

// SetRequestImmediateReport 设置立即上报标志（由 REST API 调用）
func (s *AgentServiceServer) SetRequestImmediateReport() {
	s.immediateReportMutex.Lock()
	s.requestImmediateReport = true
	s.immediateReportMutex.Unlock()
	logger.Info("已设置立即上报标志，等待下一次心跳响应通知 Agent")
}

// consumeImmediateReport 消费立即上报标志（读取并清除）
func (s *AgentServiceServer) consumeImmediateReport() bool {
	s.immediateReportMutex.Lock()
	defer s.immediateReportMutex.Unlock()
	if s.requestImmediateReport {
		s.requestImmediateReport = false
		return true
	}
	return false
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
// 逻辑：按 (domain, node_id) 或 (domain, endpoint_id) 联合唯一 upsert
// 同一域名可有多条记录（不同 node_id），支持负载均衡
// tunnelIp: Agent 的隧道 IP，当域名记录的 TargetIp 为空时自动填充
// nodeID: 当前心跳对应的 Node ID，用于自动填充 Agent 自身域名的 node_id
func (s *AgentServiceServer) handleDomainRegistrations(ctx context.Context, agentID uint64, nodeID uint64, registrations []*pb.DomainRegistration, tunnelIp string) {
	registered := 0
	updated := 0

	for _, reg := range registrations {
		var existing model.DomainRegistry
		var err error

		// 自动填充 node_id 和 endpoint_id：
		// 1. 如果有 endpoint_id，说明是 Endpoint 域名，保持不变
		// 2. 如果没有 endpoint_id 也没有 node_id，说明是 Agent 自身域名，自动填充当前 nodeID
		actualNodeID := reg.NodeId
		actualEndpointID := reg.EndpointId
		if actualEndpointID == "" && actualNodeID == 0 {
			actualNodeID = nodeID
		}

		// 按联合唯一条件查询：node_id 和 endpoint_id 互斥
		if actualNodeID > 0 {
			err = db.DB.WithContext(ctx).Where("domain = ? AND node_id = ?", reg.Domain, actualNodeID).First(&existing).Error
		} else if actualEndpointID != "" {
			err = db.DB.WithContext(ctx).Where("domain = ? AND endpoint_id = ?", reg.Domain, actualEndpointID).First(&existing).Error
		} else {
			// 兼容：无 node_id 也无 endpoint_id，按 domain + user_id 查询
			err = db.DB.WithContext(ctx).Where("domain = ? AND user_id = ? AND node_id = 0 AND endpoint_id = ''", reg.Domain, agentID).First(&existing).Error
		}

		// 当 TargetIp 为空时，使用 Agent 的隧道 IP 自动填充
		targetIp := reg.TargetIp
		if targetIp == "" && tunnelIp != "" {
			targetIp = tunnelIp
		}

		// 将 ServicePorts 序列化为 JSON 数组
		servicePortsJSON := "[]"
		if len(reg.ServicePorts) > 0 {
			if data, err := json.Marshal(reg.ServicePorts); err == nil {
				servicePortsJSON = string(data)
			}
		}

		// 将 SshUsers 序列化为 JSON 数组
		sshUsersJSON := "[]"
		if len(reg.SshUsers) > 0 {
			if data, err := json.Marshal(reg.SshUsers); err == nil {
				sshUsersJSON = string(data)
			}
		}

		record := model.DomainRegistry{
			Domain:       reg.Domain,
			Type:         model.DomainType(reg.Type),
			UserID:       agentID,
			NodeID:       actualNodeID,
			EndpointID:   actualEndpointID,
			TargetIP:     targetIp,
			TargetPort:   int(reg.TargetPort),
			Namespace:    reg.Namespace,
			ServiceName:  reg.ServiceName,
			ServicePorts: servicePortsJSON,
			SshUsers:     sshUsersJSON,
			Status:       model.DomainStatusOnline,
		}

		if err != nil {
			// 不存在，创建
			if err := db.DB.WithContext(ctx).Create(&record).Error; err != nil {
				logger.Errorf("域名注册失败: domain=%s, err=%v", reg.Domain, err)
				continue
			}
			registered++
		} else {
			// 存在，更新（保留已有的 node_id，避免覆盖）
			updates := map[string]any{
				"type":          model.DomainType(reg.Type),
				"user_id":       agentID,
				"target_ip":     targetIp,
				"target_port":   int(reg.TargetPort),
				"namespace":     reg.Namespace,
				"service_name":  reg.ServiceName,
				"service_ports": servicePortsJSON,
				"ssh_users":     sshUsersJSON,
				"status":        model.DomainStatusOnline,
			}
			// 只有当上报的 node_id 不为 0 时才更新（避免覆盖已有的正确值）
			if actualNodeID > 0 {
				updates["node_id"] = actualNodeID
			}
			// 只有当上报的 endpoint_id 不为空时才更新
			if actualEndpointID != "" {
				updates["endpoint_id"] = actualEndpointID
			}
			if err := db.DB.WithContext(ctx).Model(&existing).Updates(updates).Error; err != nil {
				logger.Errorf("域名更新失败: domain=%s, err=%v", reg.Domain, err)
				continue
			}
			updated++
		}
	}

	logger.Infof("Agent 域名注册上报完成: agent_id=%d, node_id=%d, registered=%d, updated=%d", agentID, nodeID, registered, updated)
}

// handleConnectedEndpoints 处理 Agent 心跳中的已连接 Endpoint 上报
// 一个 Endpoint 节点对应统一 endpoint 表中的一行，各能力通过字段开关控制
// 按 (user_id, name) 唯一 upsert，不在列表中的标记为 offline
// 接收并存储 Endpoint 上报的 SSH 用户列表
// nodeID: Endpoint 连接到的 Agent Node ID，用于填充 domain_registry.node_id
func (s *AgentServiceServer) handleConnectedEndpoints(ctx context.Context, agentID uint64, nodeID uint64, endpoints []*pb.ConnectedEndpoint) {
	// 收集本次上报的 Endpoint 名称
	reportedNames := make(map[string]bool)
	now := time.Now()

	for _, ep := range endpoints {
		reportedNames[ep.Name] = true

		// 更新 Endpoint 内存缓存
		cache.SetEndpointStatus(ep.Name, cache.EndpointStatus{
			EndpointName:  ep.Name,
			UserID:        agentID,
			LastHeartbeat: now,
		})
		logger.Debugf("更新 Endpoint 内存缓存: endpoint_name=%s, user_id=%d", ep.Name, agentID)

		// 填充 domain_registry.node_id（如果 Endpoint 已有域名记录但 node_id=0）
		// 查询该 Endpoint 的所有域名记录
		var domains []model.DomainRegistry
		if err := db.DB.WithContext(ctx).Where("endpoint_id = ? AND user_id = ? AND node_id = ?", ep.Name, agentID, 0).Find(&domains).Error; err == nil && len(domains) > 0 {
			// 查询 Agent Node 的 Tailscale IP
			var agentNode model.Node
			if err := db.DB.WithContext(ctx).First(&agentNode, nodeID).Error; err == nil {
				// 更新所有域名记录的 node_id 和 target_ip
				if err := db.DB.WithContext(ctx).Model(&model.DomainRegistry{}).
					Where("endpoint_id = ? AND user_id = ? AND node_id = ?", ep.Name, agentID, 0).
					Updates(map[string]any{
						"node_id":   nodeID,
						"target_ip": agentNode.IP,
					}).Error; err == nil {
					logger.Infof("填充 Endpoint 域名记录: endpoint=%s, node_id=%d, target_ip=%s, count=%d",
						ep.Name, nodeID, agentNode.IP, len(domains))
				} else {
					logger.Errorf("填充 Endpoint 域名记录失败: endpoint=%s, err=%v", ep.Name, err)
				}
			}
		}

		// upsert 统一 endpoint 表
		var existing model.Endpoint
		err := db.DB.WithContext(ctx).Where("user_id = ? AND name = ? AND revoked = ?", agentID, ep.Name, false).First(&existing).Error
		if err != nil {
			// 不存在，创建（能力默认全部关闭，由 Web 界面管理）
			// 将上报的 SSH 用户列表存储到数据库
			sshUsersJSON := "[]"
			if len(ep.SshUsers) > 0 {
				if data, err := json.Marshal(ep.SshUsers); err == nil {
					sshUsersJSON = string(data)
				}
			}
			k8sServiceNamespacesJSON := "[]"
			if len(ep.K8SserviceNamespaces) > 0 {
				if data, err := json.Marshal(ep.K8SserviceNamespaces); err == nil {
					k8sServiceNamespacesJSON = string(data)
				}
			}

			// 分配端口：查询该 Agent 下已有的 Endpoint 数量
			var count int64
			db.DB.WithContext(ctx).Model(&model.Endpoint{}).
				Where("user_id = ? AND revoked = ?", agentID, false).
				Count(&count)

			// 计算端口（50053 起步）
			sshPort := uint16(50053 + count)
			k8sapiPort := uint16(50153 + count)

			record := model.Endpoint{
				ID:                      uuid.New().String(),
				UserID:                  agentID,
				Name:                    ep.Name,
				Status:                  "online",
				SSHEnabled:              false,
				SSHUsers:                sshUsersJSON,
				SSHPort:                 sshPort, // 新增：分配 SSH 端口
				K8SAPIEnabled:           false,
				K8SAPIApiServer:         ep.K8SapiApiServer,
				K8SAPIPort:              k8sapiPort, // 新增：分配 K8SAPI 端口
				K8SServiceEnabled:       false,
				K8SServiceLabelSelector: ep.K8SserviceLabelSelector,
				K8SServiceNamespaces:    k8sServiceNamespacesJSON,
			}
			if err := db.DB.WithContext(ctx).Create(&record).Error; err != nil {
				logger.Errorf("创建 Endpoint 失败: name=%s, err=%v", ep.Name, err)
			} else {
				logger.Infof("创建 Endpoint: name=%s, id=%s, ssh_port=%d, k8sapi_port=%d, ssh_users=%v",
					ep.Name, record.ID, record.SSHPort, record.K8SAPIPort, ep.SshUsers)

				// Endpoint 创建成功后，创建域名记录（使用预分配的端口）
				// 查询 Agent Node 和 User 信息
				var agentNode model.Node
				var user model.User
				if err := db.DB.WithContext(ctx).First(&agentNode, "user_id = ? AND type = ?", agentID, model.NodeTypeAgent).Error; err == nil {
					if err := db.DB.WithContext(ctx).First(&user, agentID).Error; err == nil {
						// 创建 SSH 域名（使用 Endpoint 的端口）
						if err := s.domainService.CreateEndpointSSHDomain(ctx, &record, &agentNode, &user); err != nil {
							logger.Errorf("创建 Endpoint SSH 域名失败: endpoint=%s, err=%v", ep.Name, err)
						}
						// 创建 K8SAPI 域名（使用 Endpoint 的端口）
						if err := s.domainService.CreateEndpointK8SAPIDomain(ctx, &record, &agentNode, &user); err != nil {
							logger.Errorf("创建 Endpoint K8SAPI 域名失败: endpoint=%s, err=%v", ep.Name, err)
						}
					}
				}
			}
		} else {
			// 存在，更新状态和配置（不更新能力开关，能力由 Web 界面管理）
			updates := map[string]any{
				"status": "online",
			}

			// 自动修复端口（如果端口为 0，说明是旧版本创建的 Endpoint）
			needAutoRepair := existing.SSHPort == 0 || existing.K8SAPIPort == 0
			if needAutoRepair {
				// 查询该 Agent 下已有的 Endpoint 数量（用于计算端口偏移）
				var count int64
				db.DB.WithContext(ctx).Model(&model.Endpoint{}).
					Where("user_id = ? AND revoked = ?", agentID, false).
					Count(&count)

				if existing.SSHPort == 0 {
					// 分配 SSH 端口（使用当前 Endpoint 的索引）
					// 注意：count 已经包含了当前 Endpoint，所以需要 -1
					sshPort := uint16(50053 + count - 1)
					updates["ssh_port"] = sshPort
					logger.Infof("自动修复 Endpoint SSH 端口: name=%s, port=%d", ep.Name, sshPort)
				}

				if existing.K8SAPIPort == 0 {
					// 分配 K8SAPI 端口
					k8sapiPort := uint16(50153 + count - 1)
					updates["k8sapi_port"] = k8sapiPort
					logger.Infof("自动修复 Endpoint K8SAPI 端口: name=%s, port=%d", ep.Name, k8sapiPort)
				}
			}

			// 始终更新 SSH 用户列表（即使为空）
			if data, err := json.Marshal(ep.SshUsers); err == nil {
				updates["ssh_users"] = string(data)
			}
			// 更新 K8S API 配置
			if ep.K8SapiApiServer != "" {
				updates["k8sapi_api_server"] = ep.K8SapiApiServer
			}
			// 更新 K8S Service 配置
			if ep.K8SserviceLabelSelector != "" {
				updates["k8sservice_label_selector"] = ep.K8SserviceLabelSelector
			}
			if len(ep.K8SserviceNamespaces) > 0 {
				if data, err := json.Marshal(ep.K8SserviceNamespaces); err == nil {
					updates["k8sservice_namespaces"] = string(data)
				}
			}

			logger.Infof("更新 Endpoint: name=%s, id=%s, ssh_users=%v",
				ep.Name, existing.ID, ep.SshUsers)
			db.DB.WithContext(ctx).Model(&existing).Updates(updates)

			// 如果修复了端口，需要更新域名记录
			if needAutoRepair {
				// 重新读取 Endpoint（获取更新后的端口）
				var updatedEndpoint model.Endpoint
				if err := db.DB.WithContext(ctx).Where("user_id = ? AND name = ? AND revoked = ?", agentID, ep.Name, false).First(&updatedEndpoint).Error; err == nil {
					// 更新 SSH 域名的端口
					if updatedEndpoint.SSHPort > 0 {
						if err := db.DB.WithContext(ctx).Model(&model.DomainRegistry{}).
							Where("endpoint_id = ? AND user_id = ? AND type = ?", ep.Name, agentID, model.DomainTypeSSH).
							Update("target_port", updatedEndpoint.SSHPort).Error; err != nil {
							logger.Errorf("更新 Endpoint SSH 域名端口失败: endpoint=%s, port=%d, err=%v", ep.Name, updatedEndpoint.SSHPort, err)
						} else {
							logger.Infof("更新 Endpoint SSH 域名端口: endpoint=%s, port=%d", ep.Name, updatedEndpoint.SSHPort)
						}
					}

					// 更新 K8SAPI 域名的端口
					if updatedEndpoint.K8SAPIPort > 0 {
						if err := db.DB.WithContext(ctx).Model(&model.DomainRegistry{}).
							Where("endpoint_id = ? AND user_id = ? AND type = ?", ep.Name, agentID, model.DomainTypeK8SAPI).
							Update("target_port", updatedEndpoint.K8SAPIPort).Error; err != nil {
							logger.Errorf("更新 Endpoint K8SAPI 域名端口失败: endpoint=%s, port=%d, err=%v", ep.Name, updatedEndpoint.K8SAPIPort, err)
						} else {
							logger.Infof("更新 Endpoint K8SAPI 域名端口: endpoint=%s, port=%d", ep.Name, updatedEndpoint.K8SAPIPort)
						}
					}
				}
			}
		}

		// 更新 Endpoint K8S Service 发现缓存（复用 Agent 的缓存结构）
		logger.Debugf("处理 Endpoint %s 的 K8S Service 发现数据: discovered_services=%d", ep.Name, len(ep.DiscoveredServices))
		if len(ep.DiscoveredServices) > 0 {
			// 转换为 Agent 的 DiscoveredService 结构，添加 EndpointName 字段
			discoveredServices := make([]cache.DiscoveredService, 0, len(ep.DiscoveredServices))
			for _, ds := range ep.DiscoveredServices {
				ports := make([]cache.DiscoveredServicePort, 0, len(ds.Ports))
				for _, p := range ds.Ports {
					ports = append(ports, cache.DiscoveredServicePort{
						Name:     p.Name,
						Port:     p.Port,
						Protocol: p.Protocol,
					})
				}
				discoveredServices = append(discoveredServices, cache.DiscoveredService{
					Namespace:    ds.Namespace,
					ServiceName:  ds.ServiceName,
					ClusterIP:    ds.ClusterIp,
					Ports:        ports,
					EndpointName: ep.Name, // 标记来源为 Endpoint
				})
			}

			// 获取当前 Agent 的所有发现数据
			currentServices := cache.GetK8SServiceDiscovery(agentID)

			// 过滤掉该 Endpoint 的旧数据
			filteredServices := make([]cache.DiscoveredService, 0)
			for _, svc := range currentServices {
				if svc.EndpointName != ep.Name {
					filteredServices = append(filteredServices, svc)
				}
			}

			// 合并新数据
			allServices := append(filteredServices, discoveredServices...)

			// 更新到 Agent 的缓存中（复用 Agent 的资源发现逻辑）
			cache.UpdateK8SServiceDiscovery(agentID, allServices)
			logger.Infof("Endpoint K8S Service 发现数据已合并到 Agent 缓存: agent_id=%d, endpoint=%s, count=%d",
				agentID, ep.Name, len(discoveredServices))

			// 为发现的 K8S Service 创建域名
			// 查询 Endpoint 记录
			var endpoint model.Endpoint
			if err := db.DB.WithContext(ctx).Where("user_id = ? AND name = ? AND revoked = ?", agentID, ep.Name, false).First(&endpoint).Error; err == nil {
				// 查询 Agent Node 和 User 信息
				var agentNode model.Node
				var user model.User
				if err := db.DB.WithContext(ctx).Where("user_id = ? AND type = ?", agentID, model.NodeTypeAgent).First(&agentNode).Error; err == nil {
					if err := db.DB.WithContext(ctx).First(&user, agentID).Error; err == nil {
						// 处理每个发现的 Service
						for _, ds := range ep.DiscoveredServices {
							// 提取端口列表
							ports := make([]int32, 0, len(ds.Ports))
							for _, p := range ds.Ports {
								ports = append(ports, p.Port)
							}

							// 创建或更新域名记录
							if err := s.domainService.CreateEndpointK8SSVCDomain(ctx, &endpoint, &agentNode, &user, ds.Namespace, ds.ServiceName, ports); err != nil {
								logger.Errorf("创建 Endpoint K8S Service 域名失败: endpoint=%s, service=%s.%s, err=%v",
									ep.Name, ds.ServiceName, ds.Namespace, err)
							}
						}

						// 清理不再存在的 Service 域名
						// 查询数据库中该 Endpoint 的所有 k8ssvc 域名
						var existingDomains []model.DomainRegistry
						if err := db.DB.WithContext(ctx).Where("endpoint_id = ? AND type = ?", ep.Name, model.DomainTypeK8SSVC).Find(&existingDomains).Error; err == nil {
							// 构建上报的 Service 列表（namespace.service_name）
							reportedServices := make(map[string]bool)
							for _, ds := range ep.DiscoveredServices {
								key := fmt.Sprintf("%s.%s", ds.Namespace, ds.ServiceName)
								reportedServices[key] = true
							}

							// 删除不再存在的 Service 域名
							for _, domain := range existingDomains {
								key := fmt.Sprintf("%s.%s", domain.Namespace, domain.ServiceName)
								if !reportedServices[key] {
									if err := db.DB.WithContext(ctx).Delete(&domain).Error; err != nil {
										logger.Errorf("删除 Endpoint K8S Service 域名失败: domain=%s, err=%v", domain.Domain, err)
									} else {
										logger.Infof("删除 Endpoint K8S Service 域名: domain=%s, endpoint=%s", domain.Domain, ep.Name)
									}
								}
							}
						}
					}
				}
			}
		}
	}

	// 清理不在列表中的 Endpoint 缓存
	var allEndpoints []model.Endpoint
	if err := db.DB.WithContext(ctx).Where("user_id = ? AND revoked = ?", agentID, false).Find(&allEndpoints).Error; err == nil {
		for _, ep := range allEndpoints {
			if !reportedNames[ep.Name] {
				// 不在上报列表中，清理缓存
				cache.DeleteEndpointStatus(ep.Name)
				logger.Infof("Endpoint 不在上报列表，清理内存缓存: endpoint_name=%s", ep.Name)
			}
		}
	}

	// 将不在列表中的 Endpoint 标记为 offline
	if len(reportedNames) > 0 {
		names := make([]string, 0, len(reportedNames))
		for n := range reportedNames {
			names = append(names, n)
		}
		// 标记 Endpoint 为 offline
		db.DB.WithContext(ctx).Model(&model.Endpoint{}).
			Where("user_id = ? AND status = ? AND name NOT IN ? AND revoked = ?", agentID, "online", names, false).
			Update("status", "offline")
	} else {
		// 标记所有 Endpoint 为 offline
		db.DB.WithContext(ctx).Model(&model.Endpoint{}).
			Where("user_id = ? AND status = ? AND revoked = ?", agentID, "online", false).
			Update("status", "offline")
	}

	logger.Infof("Agent Endpoint 上报完成: agent_id=%d, count=%d", agentID, len(reportedNames))
}

// queryEndpointK8SAPIPermissions 查询 Agent 关联的 Endpoint K8SAPI 授权列表
// queryEndpointK8SAPIPermissions 查询 Agent 关联的 Endpoint K8SAPI 授权列表
// P11 重构：已废弃，Endpoint K8SAPI 权限统一使用 Agent 级别权限
func (s *AgentServiceServer) queryEndpointK8SAPIPermissions(ctx context.Context, agentID uint64) []*pb.EndpointK8SAPIPermission {
	// 返回空列表，保留方法以兼容旧代码
	return nil
}

// queryEndpointK8SServicePermissions 查询 Agent 关联的 Endpoint K8SService 授权列表
// queryEndpointK8SServicePermissions 查询 Agent 关联的 Endpoint K8SService 授权列表
// P10 重构：已废弃，Endpoint K8SService 权限统一使用 Agent 级别权限
func (s *AgentServiceServer) queryEndpointK8SServicePermissions(ctx context.Context, agentID uint64) []*pb.EndpointK8SServicePermission {
	// 返回空列表，保留方法以兼容旧代码
	return nil
}

// handleAuditRecords 处理 Agent 上报的操作审计记录
func (s *AgentServiceServer) handleAuditRecords(ctx context.Context, agentID uint64, records []*pb.OperationAuditRecord) {
	// 查询 Agent 名称
	var agentUser model.User
	agentName := ""
	if err := db.DB.WithContext(ctx).First(&agentUser, agentID).Error; err == nil {
		agentName = agentUser.Name
	}

	for _, r := range records {
		// 查询 Client 用户 ID
		var clientUserID uint64
		if r.ClientUserName != "" {
			var clientUser model.User
			if err := db.DB.WithContext(ctx).Where("name = ?", r.ClientUserName).First(&clientUser).Error; err == nil {
				clientUserID = clientUser.ID
			}
		}

		log := &model.OperationAuditLog{
			AgentUserID:   agentID,
			AgentName:     agentName,
			ClientUserID:  clientUserID,
			ClientName:    r.ClientUserName,
			EndpointName:  r.EndpointName,
			OperationType: r.OperationType,
			Target:        r.Target,
			Detail:        r.Detail,
			StartedAt:     time.Unix(r.StartedAt, 0),
			EndedAt:       time.Unix(r.EndedAt, 0),
			DurationMs:    (r.EndedAt - r.StartedAt) * 1000,
		}

		if err := db.DB.WithContext(ctx).Create(log).Error; err != nil {
			logger.Errorf("保存操作审计记录失败: %v", err)
		}
	}

	logger.Infof("保存操作审计记录: agent_id=%d, count=%d", agentID, len(records))
}

// handleK8SServiceDiscovery 处理 K8S Service 自动发现，创建域名
func (s *AgentServiceServer) handleK8SServiceDiscovery(ctx context.Context, agentID uint64, nodeID uint64, services []*pb.DiscoveredK8SService) {
	// 查询 User 信息
	var user model.User
	if err := db.DB.WithContext(ctx).First(&user, agentID).Error; err != nil {
		logger.Errorf("查询 User 失败: agent_id=%d, err=%v", agentID, err)
		return
	}

	// 查询 Node 信息
	var node model.Node
	if err := db.DB.WithContext(ctx).First(&node, nodeID).Error; err != nil {
		logger.Errorf("查询 Node 失败: node_id=%d, err=%v", nodeID, err)
		return
	}

	// 收集本次上报的 Service 域名
	reportedDomains := make(map[string]bool)

	// 为每个发现的 Service 创建域名
	for _, svc := range services {
		// 跳过没有端口的 Service
		if len(svc.Ports) == 0 {
			continue
		}

		// 使用第一个端口创建域名
		port := svc.Ports[0].Port

		// 生成域名
		domain := fmt.Sprintf("%s.%s.%s.beagle", svc.ServiceName, svc.Namespace, user.Name)
		reportedDomains[domain] = true

		// 创建或更新域名
		if err := s.domainService.CreateNodeK8SSVCDomain(ctx, &node, &user, svc.Namespace, svc.ServiceName, svc.ClusterIp, int(port)); err != nil {
			logger.Errorf("创建 K8S Service 域名失败: service=%s.%s, err=%v", svc.ServiceName, svc.Namespace, err)
		}
	}

	// 清理不再存在的 K8S Service 域名
	// 查询该 Node 的所有 K8SSVC 域名
	var existingDomains []model.DomainRegistry
	if err := db.DB.WithContext(ctx).Where("node_id = ? AND type = ?", nodeID, model.DomainTypeK8SSVC).Find(&existingDomains).Error; err != nil {
		logger.Errorf("查询 K8S Service 域名失败: node_id=%d, err=%v", nodeID, err)
		return
	}

	// 删除不在上报列表中的域名
	deletedCount := 0
	for _, existing := range existingDomains {
		if !reportedDomains[existing.Domain] {
			if err := db.DB.WithContext(ctx).Delete(&existing).Error; err != nil {
				logger.Errorf("删除过期 K8S Service 域名失败: domain=%s, err=%v", existing.Domain, err)
			} else {
				logger.Infof("删除过期 K8S Service 域名: domain=%s", existing.Domain)
				deletedCount++
			}
		}
	}

	logger.Infof("K8S Service 域名处理完成: agent_id=%d, node_id=%d, created/updated=%d, deleted=%d",
		agentID, nodeID, len(reportedDomains), deletedCount)
}

// GetUserDeviceInfo 获取用户设备信息（用于 SSH 横幅显示）
func (s *AgentServiceServer) GetUserDeviceInfo(ctx context.Context, req *pb.GetUserDeviceInfoRequest) (*pb.GetUserDeviceInfoResponse, error) {
	logger.Debugf("收到用户设备信息查询: user_name=%s, device_ip=%s", req.UserName, req.DeviceIp)

	resp := &pb.GetUserDeviceInfoResponse{
		UserName:  req.UserName,
		DeviceIp:  req.DeviceIp,
	}

	// 1. 查询用户信息（获取 Alias）
	var user model.User
	if err := db.DB.WithContext(ctx).Where("name = ? AND role = ?", req.UserName, model.UserRoleClient).First(&user).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			logger.Warnf("用户不存在: user_name=%s", req.UserName)
			return resp, nil
		}
		logger.Errorf("查询用户失败: user_name=%s, err=%v", req.UserName, err)
		return nil, fmt.Errorf("查询用户失败: %v", err)
	}

	resp.DisplayName = user.Alias

	// 2. 根据 IP 查询 Node 信息（获取设备名称和操作系统）
	var node model.Node
	if err := db.DB.WithContext(ctx).Where("user_id = ? AND ip = ? AND type = ?", user.ID, req.DeviceIp, model.NodeTypeDesktop).First(&node).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			logger.Debugf("未找到设备信息: user_id=%d, ip=%s", user.ID, req.DeviceIp)
			return resp, nil
		}
		logger.Errorf("查询设备失败: user_id=%d, ip=%s, err=%v", user.ID, req.DeviceIp, err)
		return nil, fmt.Errorf("查询设备失败: %v", err)
	}

	// 3. 解析 SystemInfo JSON 获取操作系统
	if node.SystemInfo != "" {
		var sysInfo model.NodeSystemInfo
		if err := json.Unmarshal([]byte(node.SystemInfo), &sysInfo); err != nil {
			logger.Warnf("解析设备系统信息失败: node_id=%d, err=%v", node.ID, err)
		} else {
			// 直接使用 OS 字段（已包含完整信息，如 "Windows 10"）
			resp.DeviceOs = sysInfo.OS
		}
	}

	// 4. 设备名称优先使用 Node.Name，其次使用 Hostname
	resp.DeviceName = node.Name
	if resp.DeviceName == "" {
		resp.DeviceName = node.Hostname
	}

	logger.Debugf("用户设备信息查询成功: user_name=%s, display_name=%s, device_name=%s, device_os=%s",
		req.UserName, resp.DisplayName, resp.DeviceName, resp.DeviceOs)

	return resp, nil
}
