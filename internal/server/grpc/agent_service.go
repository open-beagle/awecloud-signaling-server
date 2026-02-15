// Package grpc 提供 gRPC 服务实现
package grpc

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/google/uuid"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/cache"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/headscale"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
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
	AgentID   uint64
	NodeID    uint64 // 当前心跳流对应的 Node ID（connections map 的 key）
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

	// 立即上报标志：当为 true 时，下一次心跳响应会携带 request_immediate_report=true
	requestImmediateReport bool
	immediateReportMutex   sync.Mutex

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

	// 验证密钥（支持两种方式：user secret 或 deploy token）
	authenticated := false

	// 方式1：bcrypt 验证 user secret
	if user.SecretHash != "" {
		if err := bcrypt.CompareHashAndPassword([]byte(user.SecretHash), []byte(req.Secret)); err == nil {
			authenticated = true
		}
	}

	// 方式2：deploy token 验证（查询该用户的有效 deploy token）
	if !authenticated {
		var deployToken model.DeployToken
		if err := db.DB.WithContext(ctx).Where(
			"user_id = ? AND token = ? AND status = ?",
			user.ID, req.Secret, model.DeployTokenStatusBound,
		).First(&deployToken).Error; err == nil {
			authenticated = true
			// 更新最后使用时间
			deployToken.UpdateLastUsed()
			db.DB.WithContext(ctx).Save(&deployToken)
		}
	}

	if !authenticated {
		logger.Warnf("Agent 认证失败: %s", req.Name)
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

		// 断连时只清理当前 Node 的数据，不影响同 Agent 下其他 Node
		if err := db.DB.Model(&model.Node{}).
			Where("id = ?", nodeID).
			Updates(map[string]any{"ip": "", "last_heartbeat": nil}).Error; err != nil {
			logger.Errorf("Node 断连时清空 IP 失败: node_id=%d, err=%v", nodeID, err)
		}

		// 断连时只将当前 Node 关联的域名设为离线
		if err := db.DB.Model(&model.DomainRegistry{}).
			Where("node_id = ? AND status = ?", nodeID, model.DomainStatusOnline).
			Update("status", model.DomainStatusOffline).Error; err != nil {
			logger.Errorf("Node 断连时设置域名离线失败: node_id=%d, err=%v", nodeID, err)
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

		// 只有当该 Agent 下没有其他在线 Node 时，才将 Endpoint 设为离线
		// 因为 Endpoint 是 Agent 级别的（user_id），不是 Node 级别的
		if !hasOtherOnline {
			db.DB.Model(&model.EndpointSSH{}).Where("user_id = ? AND status = ?", agentID, "online").Update("status", "offline")
			db.DB.Model(&model.EndpointK8SAPI{}).Where("user_id = ? AND status = ?", agentID, "online").Update("status", "offline")
			db.DB.Model(&model.EndpointK8SService{}).Where("user_id = ? AND status = ?", agentID, "online").Update("status", "offline")
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

	// 确定 Node 名称
	nodeName := req.Hostname
	if nodeName == "" {
		nodeName = user.Name
	}

	// 查询或创建 Node（用 user_id + type + name 唯一定位）
	var node model.Node
	if err := db.DB.WithContext(ctx).Where("user_id = ? AND type = ? AND name = ?", agentID, nodeType, nodeName).First(&node).Error; err != nil {
		// Node 不存在，创建
		now := time.Now()
		node = model.Node{
			UserID:        agentID,
			Name:          nodeName,
			Type:          nodeType,
			Hostname:      nodeName,
			IP:            req.TunnelIp,
			LastHeartbeat: &now,
		}
		if err := db.DB.WithContext(ctx).Create(&node).Error; err != nil {
			logger.Errorf("创建 Node 失败: user_id=%d, type=%s, name=%s, err=%v", agentID, nodeType, nodeName, err)
		} else {
			logger.Infof("创建 Node: user_id=%d, name=%s, type=%s", agentID, nodeName, nodeType)
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

	// 如果 Headscale 客户端可用，查询并更新 HeadscaleNodeID
	if s.headscaleClient != nil && node.HeadscaleNodeID == 0 {
		hsUserName := fmt.Sprintf("%s-%s", hsPrefix, user.Name)
		// 按用户名 + 节点名精确匹配（一个 Headscale 用户下可能有多个节点）
		hsNode, err := s.headscaleClient.GetNodeByUserAndName(ctx, hsUserName, nodeName)
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
		s.handleDomainRegistrations(ctx, agentID, req.DomainRegistrations, req.TunnelIp)
	}

	// 处理已连接的 Endpoint 上报
	if len(req.ConnectedEndpoints) > 0 {
		s.handleConnectedEndpoints(ctx, agentID, req.ConnectedEndpoints)
	}

	// 处理操作审计记录上报
	if len(req.AuditRecords) > 0 {
		s.handleAuditRecords(ctx, agentID, req.AuditRecords)
	}

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

	// 查询该 Agent 关联的 Endpoint SSH 授权信息
	epSSHPerms := s.queryEndpointSSHPermissions(ctx, agentID)
	resp.EndpointSshPermissions = epSSHPerms

	// 查询该 Agent 关联的 Endpoint K8SAPI 授权信息
	epK8SAPIPerms := s.queryEndpointK8SAPIPermissions(ctx, agentID)
	resp.EndpointK8SapiPermissions = epK8SAPIPerms

	// 查询该 Agent 关联的 Endpoint K8SService 授权信息
	epK8SSvcPerms := s.queryEndpointK8SServicePermissions(ctx, agentID)
	resp.EndpointK8SservicePermissions = epK8SSvcPerms

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

	for _, p := range userPerms {
		if p.User == nil {
			continue
		}
		result = append(result, &pb.K8SPermission{
			UserId:     p.UserID,
			UserName:   p.User.Name,
			K8SGroups:  parseJSONStringArray(p.K8SGroups),
			Namespaces: parseJSONStringArray(p.Namespaces),
			IsGroup:    false,
		})
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
func (s *AgentServiceServer) handleDomainRegistrations(ctx context.Context, agentID uint64, registrations []*pb.DomainRegistration, tunnelIp string) {
	registered := 0
	updated := 0

	for _, reg := range registrations {
		var existing model.DomainRegistry
		var err error

		// 按联合唯一条件查询：node_id 和 endpoint_id 互斥
		if reg.NodeId > 0 {
			err = db.DB.WithContext(ctx).Where("domain = ? AND node_id = ?", reg.Domain, reg.NodeId).First(&existing).Error
		} else if reg.EndpointId != "" {
			err = db.DB.WithContext(ctx).Where("domain = ? AND endpoint_id = ?", reg.Domain, reg.EndpointId).First(&existing).Error
		} else {
			// 兼容：无 node_id 也无 endpoint_id，按 domain + user_id 查询
			err = db.DB.WithContext(ctx).Where("domain = ? AND user_id = ? AND node_id = 0 AND endpoint_id = ''", reg.Domain, agentID).First(&existing).Error
		}

		// 当 TargetIp 为空时，使用 Agent 的隧道 IP 自动填充
		targetIp := reg.TargetIp
		if targetIp == "" && tunnelIp != "" {
			targetIp = tunnelIp
		}

		record := model.DomainRegistry{
			Domain:      reg.Domain,
			Type:        model.DomainType(reg.Type),
			UserID:      agentID,
			NodeID:      reg.NodeId,
			EndpointID:  reg.EndpointId,
			TargetIP:    targetIp,
			TargetPort:  int(reg.TargetPort),
			Namespace:   reg.Namespace,
			ServiceName: reg.ServiceName,
			Status:      model.DomainStatusOnline,
		}

		if err != nil {
			// 不存在，创建
			if err := db.DB.WithContext(ctx).Create(&record).Error; err != nil {
				logger.Errorf("域名注册失败: domain=%s, err=%v", reg.Domain, err)
				continue
			}
			registered++
		} else {
			// 存在，更新
			updates := map[string]any{
				"type":         model.DomainType(reg.Type),
				"user_id":      agentID,
				"node_id":      reg.NodeId,
				"endpoint_id":  reg.EndpointId,
				"target_ip":    targetIp,
				"target_port":  int(reg.TargetPort),
				"namespace":    reg.Namespace,
				"service_name": reg.ServiceName,
				"status":       model.DomainStatusOnline,
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

// handleConnectedEndpoints 处理 Agent 心跳中的已连接 Endpoint 上报
// 一个 Endpoint 可以同时提供多种能力，每种能力写入对应的数据库表
// 按 (user_id, name) 唯一 upsert，不在列表中的标记为 offline
func (s *AgentServiceServer) handleConnectedEndpoints(ctx context.Context, agentID uint64, endpoints []*pb.ConnectedEndpoint) {
	// 收集本次上报的 Endpoint 名称（按类型分组）
	sshNames := make(map[string]bool)
	k8sapiNames := make(map[string]bool)
	k8ssvcNames := make(map[string]bool)

	for _, ep := range endpoints {
		for _, cap := range ep.Capabilities {
			switch cap.Type {
			case "ssh":
				sshNames[ep.Name] = true
				s.upsertEndpointSSH(ctx, agentID, ep.Name, cap)
			case "k8sapi":
				k8sapiNames[ep.Name] = true
				s.upsertEndpointK8SAPI(ctx, agentID, ep.Name, cap)
			case "k8sservice":
				k8ssvcNames[ep.Name] = true
				s.upsertEndpointK8SService(ctx, agentID, ep.Name)
			default:
				logger.Warnf("未知的 Endpoint 能力类型: type=%s, name=%s", cap.Type, ep.Name)
			}
		}

		// 更新 Endpoint K8S Service 发现缓存
		if len(ep.DiscoveredServices) > 0 {
			discoveredServices := make([]cache.EndpointDiscoveredService, 0, len(ep.DiscoveredServices))
			for _, ds := range ep.DiscoveredServices {
				ports := make([]cache.DiscoveredServicePort, 0, len(ds.Ports))
				for _, p := range ds.Ports {
					ports = append(ports, cache.DiscoveredServicePort{
						Name:     p.Name,
						Port:     p.Port,
						Protocol: p.Protocol,
					})
				}
				discoveredServices = append(discoveredServices, cache.EndpointDiscoveredService{
					Namespace:   ds.Namespace,
					ServiceName: ds.ServiceName,
					ClusterIP:   ds.ClusterIp,
					Ports:       ports,
				})
			}
			cache.UpdateEndpointK8SServiceDiscovery(ep.Name, discoveredServices)
		}
	}

	// 将不在列表中的 Endpoint 标记为 offline
	if len(sshNames) > 0 {
		names := make([]string, 0, len(sshNames))
		for n := range sshNames {
			names = append(names, n)
		}
		db.DB.WithContext(ctx).Model(&model.EndpointSSH{}).
			Where("user_id = ? AND status = ? AND name NOT IN ? AND revoked = ?", agentID, "online", names, false).
			Update("status", "offline")
	} else {
		db.DB.WithContext(ctx).Model(&model.EndpointSSH{}).
			Where("user_id = ? AND status = ? AND revoked = ?", agentID, "online", false).
			Update("status", "offline")
	}

	if len(k8sapiNames) > 0 {
		names := make([]string, 0, len(k8sapiNames))
		for n := range k8sapiNames {
			names = append(names, n)
		}
		db.DB.WithContext(ctx).Model(&model.EndpointK8SAPI{}).
			Where("user_id = ? AND status = ? AND name NOT IN ? AND revoked = ?", agentID, "online", names, false).
			Update("status", "offline")
	} else {
		db.DB.WithContext(ctx).Model(&model.EndpointK8SAPI{}).
			Where("user_id = ? AND status = ? AND revoked = ?", agentID, "online", false).
			Update("status", "offline")
	}

	if len(k8ssvcNames) > 0 {
		names := make([]string, 0, len(k8ssvcNames))
		for n := range k8ssvcNames {
			names = append(names, n)
		}
		db.DB.WithContext(ctx).Model(&model.EndpointK8SService{}).
			Where("user_id = ? AND status = ? AND name NOT IN ? AND revoked = ?", agentID, "online", names, false).
			Update("status", "offline")
	} else {
		db.DB.WithContext(ctx).Model(&model.EndpointK8SService{}).
			Where("user_id = ? AND status = ? AND revoked = ?", agentID, "online", false).
			Update("status", "offline")
	}

	logger.Infof("Agent Endpoint 上报完成: agent_id=%d, ssh=%d, k8sapi=%d, k8ssvc=%d",
		agentID, len(sshNames), len(k8sapiNames), len(k8ssvcNames))
}

// upsertEndpointSSH 创建或更新 SSH Endpoint
func (s *AgentServiceServer) upsertEndpointSSH(ctx context.Context, agentID uint64, name string, cap *pb.EndpointCapabilityInfo) {
	var existing model.EndpointSSH
	err := db.DB.WithContext(ctx).Where("user_id = ? AND name = ? AND revoked = ?", agentID, name, false).First(&existing).Error
	if err != nil {
		// 不存在，创建
		record := model.EndpointSSH{
			ID:       uuid.New().String(),
			UserID:   agentID,
			Name:     name,
			Host:     cap.Host,
			Port:     int(cap.Port),
			SSHUsers: "[]",
			Status:   "online",
			Enabled:  true,
		}
		if err := db.DB.WithContext(ctx).Create(&record).Error; err != nil {
			logger.Errorf("创建 SSH Endpoint 失败: name=%s, err=%v", name, err)
		}
	} else {
		// 存在，更新
		updates := map[string]any{
			"host":   cap.Host,
			"port":   int(cap.Port),
			"status": "online",
		}
		db.DB.WithContext(ctx).Model(&existing).Updates(updates)
	}
}

// upsertEndpointK8SAPI 创建或更新 K8SAPI Endpoint
func (s *AgentServiceServer) upsertEndpointK8SAPI(ctx context.Context, agentID uint64, name string, cap *pb.EndpointCapabilityInfo) {
	var existing model.EndpointK8SAPI
	err := db.DB.WithContext(ctx).Where("user_id = ? AND name = ? AND revoked = ?", agentID, name, false).First(&existing).Error
	if err != nil {
		record := model.EndpointK8SAPI{
			ID:        uuid.New().String(),
			UserID:    agentID,
			Name:      name,
			APIServer: cap.ApiServer,
			Status:    "online",
			Enabled:   true,
		}
		if err := db.DB.WithContext(ctx).Create(&record).Error; err != nil {
			logger.Errorf("创建 K8SAPI Endpoint 失败: name=%s, err=%v", name, err)
		}
	} else {
		updates := map[string]any{
			"api_server": cap.ApiServer,
			"status":     "online",
		}
		db.DB.WithContext(ctx).Model(&existing).Updates(updates)
	}
}

// upsertEndpointK8SService 创建或更新 K8SService Endpoint
func (s *AgentServiceServer) upsertEndpointK8SService(ctx context.Context, agentID uint64, name string) {
	var existing model.EndpointK8SService
	err := db.DB.WithContext(ctx).Where("user_id = ? AND name = ? AND revoked = ?", agentID, name, false).First(&existing).Error
	if err != nil {
		record := model.EndpointK8SService{
			ID:      uuid.New().String(),
			UserID:  agentID,
			Name:    name,
			Status:  "online",
			Enabled: true,
		}
		if err := db.DB.WithContext(ctx).Create(&record).Error; err != nil {
			logger.Errorf("创建 K8SService Endpoint 失败: name=%s, err=%v", name, err)
		}
	} else {
		updates := map[string]any{
			"status": "online",
		}
		db.DB.WithContext(ctx).Model(&existing).Updates(updates)
	}
}

// queryEndpointSSHPermissions 查询 Agent 关联的 Endpoint SSH 授权列表
func (s *AgentServiceServer) queryEndpointSSHPermissions(ctx context.Context, agentID uint64) []*pb.EndpointSSHPermission {
	var result []*pb.EndpointSSHPermission

	// 查询该 Agent 下的所有 Endpoint SSH
	var endpoints []model.EndpointSSH
	if err := db.DB.WithContext(ctx).Where("user_id = ? AND revoked = ? AND status = ?", agentID, false, "online").Find(&endpoints).Error; err != nil {
		return nil
	}

	for _, ep := range endpoints {
		// 查询用户授权
		var userPerms []model.AclEndpointSSHUserPermission
		if err := db.DB.WithContext(ctx).Preload("User").
			Where("endpoint_id = ? AND enabled = ?", ep.ID, true).
			Find(&userPerms).Error; err != nil {
			continue
		}
		for _, p := range userPerms {
			if p.User == nil {
				continue
			}
			result = append(result, &pb.EndpointSSHPermission{
				EndpointId:   ep.ID,
				EndpointName: ep.Name,
				UserId:       p.UserID,
				UserName:     p.User.Name,
				SshUsers:     parseJSONStringArray(p.SSHUsers),
				IsGroup:      false,
			})
		}

		// 查询分组授权，展开成员
		var groupPerms []model.AclEndpointSSHGroupPermission
		if err := db.DB.WithContext(ctx).
			Where("endpoint_id = ? AND enabled = ?", ep.ID, true).
			Find(&groupPerms).Error; err != nil {
			continue
		}
		for _, gp := range groupPerms {
			sshUsers := parseJSONStringArray(gp.SSHUsers)
			var members []model.GroupMember
			if err := db.DB.WithContext(ctx).Preload("User").
				Where("group_id = ?", gp.GroupID).Find(&members).Error; err != nil {
				continue
			}
			for _, m := range members {
				if m.User == nil {
					continue
				}
				result = append(result, &pb.EndpointSSHPermission{
					EndpointId:   ep.ID,
					EndpointName: ep.Name,
					UserId:       m.UserID,
					UserName:     m.User.Name,
					SshUsers:     sshUsers,
					IsGroup:      true,
				})
			}
		}
	}

	return result
}

// queryEndpointK8SAPIPermissions 查询 Agent 关联的 Endpoint K8SAPI 授权列表
func (s *AgentServiceServer) queryEndpointK8SAPIPermissions(ctx context.Context, agentID uint64) []*pb.EndpointK8SAPIPermission {
	var result []*pb.EndpointK8SAPIPermission

	var endpoints []model.EndpointK8SAPI
	if err := db.DB.WithContext(ctx).Where("user_id = ? AND revoked = ? AND status = ?", agentID, false, "online").Find(&endpoints).Error; err != nil {
		return nil
	}

	for _, ep := range endpoints {
		var userPerms []model.AclEndpointK8SAPIUserPermission
		if err := db.DB.WithContext(ctx).Preload("User").
			Where("endpoint_id = ? AND enabled = ?", ep.ID, true).
			Find(&userPerms).Error; err != nil {
			continue
		}
		for _, p := range userPerms {
			if p.User == nil {
				continue
			}
			result = append(result, &pb.EndpointK8SAPIPermission{
				EndpointId:   ep.ID,
				EndpointName: ep.Name,
				UserId:       p.UserID,
				UserName:     p.User.Name,
				K8SGroups:    parseJSONStringArray(p.K8SGroups),
				Namespaces:   parseJSONStringArray(p.Namespaces),
				IsGroup:      false,
			})
		}

		var groupPerms []model.AclEndpointK8SAPIGroupPermission
		if err := db.DB.WithContext(ctx).
			Where("endpoint_id = ? AND enabled = ?", ep.ID, true).
			Find(&groupPerms).Error; err != nil {
			continue
		}
		for _, gp := range groupPerms {
			k8sGroups := parseJSONStringArray(gp.K8SGroups)
			namespaces := parseJSONStringArray(gp.Namespaces)
			var members []model.GroupMember
			if err := db.DB.WithContext(ctx).Preload("User").
				Where("group_id = ?", gp.GroupID).Find(&members).Error; err != nil {
				continue
			}
			for _, m := range members {
				if m.User == nil {
					continue
				}
				result = append(result, &pb.EndpointK8SAPIPermission{
					EndpointId:   ep.ID,
					EndpointName: ep.Name,
					UserId:       m.UserID,
					UserName:     m.User.Name,
					K8SGroups:    k8sGroups,
					Namespaces:   namespaces,
					IsGroup:      true,
				})
			}
		}
	}

	return result
}

// queryEndpointK8SServicePermissions 查询 Agent 关联的 Endpoint K8SService 授权列表
func (s *AgentServiceServer) queryEndpointK8SServicePermissions(ctx context.Context, agentID uint64) []*pb.EndpointK8SServicePermission {
	var result []*pb.EndpointK8SServicePermission

	var endpoints []model.EndpointK8SService
	if err := db.DB.WithContext(ctx).Where("user_id = ? AND revoked = ? AND status = ?", agentID, false, "online").Find(&endpoints).Error; err != nil {
		return nil
	}

	for _, ep := range endpoints {
		var userPerms []model.AclEndpointK8SServiceUserPermission
		if err := db.DB.WithContext(ctx).Preload("User").
			Where("endpoint_id = ? AND enabled = ?", ep.ID, true).
			Find(&userPerms).Error; err != nil {
			continue
		}
		for _, p := range userPerms {
			if p.User == nil {
				continue
			}
			result = append(result, &pb.EndpointK8SServicePermission{
				EndpointId:   ep.ID,
				EndpointName: ep.Name,
				UserId:       p.UserID,
				UserName:     p.User.Name,
				Namespaces:   parseJSONStringArray(p.Namespaces),
				ServiceNames: parseJSONStringArray(p.ServiceNames),
				IsGroup:      false,
			})
		}

		var groupPerms []model.AclEndpointK8SServiceGroupPermission
		if err := db.DB.WithContext(ctx).
			Where("endpoint_id = ? AND enabled = ?", ep.ID, true).
			Find(&groupPerms).Error; err != nil {
			continue
		}
		for _, gp := range groupPerms {
			namespaces := parseJSONStringArray(gp.Namespaces)
			serviceNames := parseJSONStringArray(gp.ServiceNames)
			var members []model.GroupMember
			if err := db.DB.WithContext(ctx).Preload("User").
				Where("group_id = ?", gp.GroupID).Find(&members).Error; err != nil {
				continue
			}
			for _, m := range members {
				if m.User == nil {
					continue
				}
				result = append(result, &pb.EndpointK8SServicePermission{
					EndpointId:   ep.ID,
					EndpointName: ep.Name,
					UserId:       m.UserID,
					UserName:     m.User.Name,
					Namespaces:   namespaces,
					ServiceNames: serviceNames,
					IsGroup:      true,
				})
			}
		}
	}

	return result
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
