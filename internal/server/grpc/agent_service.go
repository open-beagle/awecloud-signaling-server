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
type AgentConnection struct {
	AgentID   uint64
	NodeID    uint64 // 当前心跳流对应的 Node ID（用于查询 Node 级别的能力配置）
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

	if user.Role != model.UserRoleAgent && user.Role != model.UserRoleClient {
		return fmt.Errorf("用户角色不支持心跳: %d (role=%s)", agentID, user.Role)
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

		// Agent 断连时，清空 Node 的 IP 和心跳时间，确保离线状态正确
		if err := db.DB.Model(&model.Node{}).
			Where("user_id = ?", agentID).
			Updates(map[string]any{"ip": "", "last_heartbeat": nil}).Error; err != nil {
			logger.Errorf("Agent 断连时清空 Node IP 失败: agent_id=%d, err=%v", agentID, err)
		}

		// Agent 断连时，将其所有域名设为离线
		if err := db.DB.Model(&model.DomainRegistry{}).
			Where("user_id = ? AND status = ?", agentID, model.DomainStatusOnline).
			Update("status", model.DomainStatusOffline).Error; err != nil {
			logger.Errorf("Agent 断连时设置域名离线失败: agent_id=%d, err=%v", agentID, err)
		} else {
			logger.Infof("Agent 断连，域名已设为离线，Node IP 已清空: agent_id=%d", agentID)
		}
	}()

	// 处理第一个心跳（使用独立 context，避免 stream 断开导致数据库操作失败）
	conn.NodeID = s.handleHeartbeat(context.Background(), agentID, firstReq)

	// 发送首次响应
	if err := s.sendHeartbeatResponse(context.Background(), stream, agentID, conn.NodeID); err != nil {
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
			conn.NodeID = s.handleHeartbeat(context.Background(), agentID, req)

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
