// Package grpc 提供 gRPC 服务实现
package grpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
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
	AgentID                               uint64
	NodeID                                uint64 // 当前心跳流对应的 Node ID（connections map 的 key）
	Stream                                pb.AgentService_HeartbeatServer
	TunnelIP                              string
	Connected                             bool
	LastSeen                              time.Time
	Cancel                                context.CancelFunc
	HeartbeatCount                        int // 心跳计数器，用于定期检查用户状态
	SessionAuthorizationProtocol          string
	EndpointSessionAuthorizationProtocols map[string]string
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

	headscaleClient             *headscale.Client
	config                      *config.ServerConfig
	domainService               *service.DomainService
	updateService               *service.UpdateService
	resourceReconciler          *service.ResourceReconciliationService
	providerSupply              *service.ProviderSupplyService
	workloadInventory           *service.WorkloadInventoryService
	sessionAuthorization        *service.SessionAuthorizationService
	containerSessionAckMutex    sync.Mutex
	containerSessionAcks        map[uint64][]string
	authorizationSnapshotMutex  sync.Mutex
	authorizationSnapshotStates map[string]*authorizationSnapshotState

	runtimeStore      *cache.NodeRuntimeStore
	runtimePersister  *cache.NodeRuntimePersister
	snapshotRefresher *headscale.SnapshotRefresher
}

func (s *AgentServiceServer) SetRuntimeStore(store *cache.NodeRuntimeStore) {
	s.runtimeStore = store
}

func (s *AgentServiceServer) SetRuntimePersister(persister *cache.NodeRuntimePersister) {
	s.runtimePersister = persister
}

func (s *AgentServiceServer) SetSnapshotRefresher(refresher *headscale.SnapshotRefresher) {
	s.snapshotRefresher = refresher
}

// NewAgentServiceServer 创建 Agent 服务
func NewAgentServiceServer(cfg *config.ServerConfig, workloadSnapshots *service.WorkloadSnapshotStore) *AgentServiceServer {
	s := &AgentServiceServer{
		connections:                 make(map[uint64]*AgentConnection),
		configVersion:               time.Now().Unix(),
		config:                      cfg,
		domainService:               service.NewDomainService(db.DB),
		updateService:               service.NewUpdateService(db.DB),
		resourceReconciler:          service.NewResourceReconciliationService(db.DB),
		providerSupply:              service.NewProviderSupplyService(db.DB),
		workloadInventory:           service.NewWorkloadInventoryService(db.DB, workloadSnapshots),
		sessionAuthorization:        service.NewSessionAuthorizationService(db.DB),
		containerSessionAcks:        make(map[uint64][]string),
		authorizationSnapshotStates: make(map[string]*authorizationSnapshotState),
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

	var userID uint64
	var deviceName string
	resumeExistingNode := false

	// Legacy credentials remain valid for Agents that have not moved to the
	// TechnicalResource admission flow.
	var deployToken model.DeployToken
	legacyErr := db.DB.WithContext(ctx).Where("token = ? AND status = ?", req.Secret, model.DeployTokenStatusBound).First(&deployToken).Error
	if legacyErr == nil {
		deployToken.UpdateLastUsed()
		if err := db.DB.WithContext(ctx).Save(&deployToken).Error; err != nil {
			return nil, err
		}
		userID = deployToken.UserID
		deviceName = deployToken.Name
	} else if !errors.Is(legacyErr, gorm.ErrRecordNotFound) {
		return nil, legacyErr
	} else {
		// A consumed TechnicalResource token is the persisted runtime
		// credential. Restarting or upgrading must not consume it again.
		var resourceToken model.TechnicalResourceDeployToken
		resourceErr := db.DB.WithContext(ctx).Table("technical_resource_deploy_token AS token").
			Select("token.*").
			Joins("JOIN technical_resource AS resource ON resource.id = token.technical_resource_id AND resource.runtime_user_id = token.runtime_user_id").
			Where("token.token = ? AND token.status = ?", req.Secret, model.TechnicalResourceDeployTokenConsumed).
			Where("resource.type = ? AND resource.lifecycle_state = ? AND resource.deleted_at IS NULL", model.TechnicalResourceAgent, model.TechnicalResourceRegistered).
			First(&resourceToken).Error
		if resourceErr != nil {
			if !errors.Is(resourceErr, gorm.ErrRecordNotFound) {
				return nil, resourceErr
			}
			logger.Warn("Agent 运行凭据无效")
			return &pb.AgentRegisterResponse{Success: false, Message: "无效的凭据"}, nil
		}
		userID = resourceToken.RuntimeUserID
		deviceName = resourceToken.Name
		resumeExistingNode = true
	}

	// 查询用户
	var user model.User
	if err := db.DB.WithContext(ctx).First(&user, userID).Error; err != nil {
		logger.Warnf("用户不存在: user_id=%d", userID)
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

	// 每个部署名称对应一个独立 Agent Node。
	var node model.Node
	if err := db.DB.WithContext(ctx).Where("user_id = ? AND type = ? AND name = ?", user.ID, model.NodeTypeAgent, deviceName).First(&node).Error; err != nil {
		if resumeExistingNode {
			logger.Warnf("Agent 运行凭据没有已绑定 Node: user_id=%d, device=%s", user.ID, deviceName)
			return &pb.AgentRegisterResponse{Success: false, Message: "Agent Node 不存在"}, nil
		}
		// 创建新 Node
		node = model.Node{
			UserID:          user.ID,
			Name:            deviceName,
			Type:            model.NodeTypeAgent,
			HostDomainLabel: service.SuggestedHostDomainLabel(ctx, db.DB, user.ID, deviceName),
		}
		db.DB.WithContext(ctx).Create(&node)
	}

	// 更新 Node 信息
	now := time.Now()
	node.LastHeartbeat = &now
	if req.Version != "" {
		node.Version = req.Version
	}
	if req.CommitId != "" {
		node.CommitID = req.CommitId
	}
	if req.BinarySha256 != "" {
		node.BinarySHA256 = req.BinarySha256
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

	// Resource deployment tokens remain the Agent credential after admission.
	if !authenticated {
		var resourceToken model.TechnicalResourceDeployToken
		if err := db.DB.WithContext(ctx).Table("technical_resource_deploy_token AS token").
			Select("token.*").
			Joins("JOIN technical_resource AS resource ON resource.id = token.technical_resource_id").
			Where("token.runtime_user_id = ? AND token.token = ? AND token.status = ?", user.ID, req.Secret, model.TechnicalResourceDeployTokenConsumed).
			Where("resource.lifecycle_state = ? AND resource.deleted_at IS NULL", model.TechnicalResourceRegistered).
			First(&resourceToken).Error; err == nil {
			authenticated = true
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
	nodeName := user.Name
	if req.SystemInfo != nil && req.SystemInfo.Hostname != "" {
		nodeName = req.SystemInfo.Hostname
	}
	if err := db.DB.WithContext(ctx).Where("user_id = ? AND type = ? AND name = ?", user.ID, model.NodeTypeAgent, nodeName).First(&node).Error; err != nil {
		node = model.Node{
			UserID:          user.ID,
			Name:            nodeName,
			Type:            model.NodeTypeAgent,
			HostDomainLabel: service.SuggestedHostDomainLabel(ctx, db.DB, user.ID, nodeName),
		}
		db.DB.WithContext(ctx).Create(&node)
	}

	// 更新 Node 信息
	now := time.Now()
	node.LastHeartbeat = &now
	if req.Version != "" {
		node.Version = req.Version
	}
	if req.CommitId != "" {
		node.CommitID = req.CommitId
	}
	if req.BinarySha256 != "" {
		node.BinarySHA256 = req.BinarySha256
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
		AgentID:                               agentID,
		NodeID:                                nodeID,
		Stream:                                stream,
		TunnelIP:                              firstReq.TunnelIp,
		Connected:                             firstReq.TunnelConnected,
		LastSeen:                              time.Now(),
		Cancel:                                cancel,
		EndpointSessionAuthorizationProtocols: make(map[string]string),
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
		s.containerSessionAckMutex.Lock()
		delete(s.containerSessionAcks, nodeID)
		s.containerSessionAckMutex.Unlock()
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

	firstAcks := s.processSessionAuthorizationHeartbeat(context.Background(), conn, firstReq)
	// 发送首次响应
	if err := s.sendHeartbeatResponse(heartbeatResponseContext(ctx), stream, conn, firstAcks); err != nil {
		logger.Errorf("发送首次心跳响应失败: %v", err)
		return err
	}

	recvRequests := make(chan *pb.AgentHeartbeatRequest)
	recvErrors := make(chan error, 1)
	go func() {
		defer close(recvRequests)
		for {
			req, recvErr := stream.Recv()
			if recvErr != nil {
				recvErrors <- recvErr
				return
			}
			select {
			case recvRequests <- req:
			case <-ctx.Done():
				return
			}
		}
	}()
	pushTicker := time.NewTicker(sessionAuthorizationPushPeriod)
	defer pushTicker.Stop()

	// 持续接收心跳；v2 协商后允许 Server 在两次请求之间主动刷新授权。
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-recvErrors:
			return err
		case req, ok := <-recvRequests:
			if !ok {
				return nil
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

			acks := s.processSessionAuthorizationHeartbeat(context.Background(), conn, req)
			// 发送响应
			if err := s.sendHeartbeatResponse(heartbeatResponseContext(ctx), stream, conn, acks); err != nil {
				logger.Errorf("发送心跳响应失败: %v", err)
				return err
			}
		case <-pushTicker.C:
			if conn.SessionAuthorizationProtocol != sessionAuthorizationProtocolV2 && len(conn.EndpointSessionAuthorizationProtocols) == 0 {
				continue
			}
			if err := s.sendHeartbeatResponse(heartbeatResponseContext(ctx), stream, conn, nil); err != nil {
				logger.Errorf("主动推送 Session 授权响应失败: %v", err)
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
			UserID:          agentID,
			Name:            nodeName,
			Type:            nodeType,
			Hostname:        req.Hostname,
			HostDomainLabel: service.SuggestedHostDomainLabel(ctx, db.DB, agentID, req.Hostname),
			IP:              req.TunnelIp,
			LastHeartbeat:   &now,
		}
		if err := db.DB.WithContext(ctx).Create(&node).Error; err != nil {
			logger.Errorf("创建 Node 失败: user_id=%d, type=%s, name=%s, err=%v", agentID, nodeType, nodeName, err)
		} else {
			logger.Infof("心跳时创建 Node: user_id=%d, name=%s, type=%s", agentID, nodeName, nodeType)
			if s.runtimeStore != nil {
				s.runtimeStore.UpsertNode(&node)
			}
		}
	}

	// 更新 Node 信息
	now := time.Now()
	containerSSHProtocol := ""
	if req.ContainerSshProtocol == "v1" {
		containerSSHProtocol = "v1"
	}
	sysInfoJSON := ""
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
			sysInfoJSON = string(data)
		}
	}

	if s.runtimeStore != nil {
		if _, err := s.runtimeStore.UpdateHeartbeat(node.ID, req.TunnelIp, req.Hostname, req.Version, req.CommitId, req.BinarySha256, sysInfoJSON, req.UpdaterProtocol, containerSSHProtocol, now); err != nil {
			// 如果内存中缺失，重新从 DB 读并加载
			var freshNode model.Node
			if errDb := db.DB.WithContext(ctx).First(&freshNode, node.ID).Error; errDb == nil {
				s.runtimeStore.UpsertNode(&freshNode)
				s.runtimeStore.UpdateHeartbeat(node.ID, req.TunnelIp, req.Hostname, req.Version, req.CommitId, req.BinarySha256, sysInfoJSON, req.UpdaterProtocol, containerSSHProtocol, now)
			}
		}
	} else {
		// 兜底：未启用 runtimeStore 时写入 DB
		updates := map[string]any{
			"last_heartbeat":         now,
			"ip":                     req.TunnelIp,
			"container_ssh_protocol": containerSSHProtocol,
		}
		if req.UpdaterProtocol != "" {
			updates["updater_protocol"] = req.UpdaterProtocol
		}
		if req.Version != "" {
			updates["version"] = req.Version
		}
		if req.CommitId != "" {
			updates["commit_id"] = req.CommitId
		}
		if req.BinarySha256 != "" {
			updates["binary_sha256"] = req.BinarySha256
		}
		if sysInfoJSON != "" {
			updates["system_info"] = sysInfoJSON
		}
		if req.Hostname != "" {
			updates["hostname"] = req.Hostname
		}
		db.DB.WithContext(ctx).Model(&model.Node{}).Where("id = ?", node.ID).Updates(updates)
	}

	// 更新 Node 内存缓存
	cache.SetNodeStatus(node.ID, cache.NodeStatus{
		NodeID:        node.ID,
		UserID:        agentID,
		TunnelIP:      req.TunnelIp,
		LastHeartbeat: now,
	})

	if s.snapshotRefresher != nil && s.runtimeStore != nil {
		hsUserName := fmt.Sprintf("%s-%s", hsPrefix, user.Name)
		tunnelIP := strings.TrimSpace(req.TunnelIp)
		snapshot := s.snapshotRefresher.LoadSnapshot()

		if tunnelIP != "" {
			if hsView, found := snapshot.GetByIP(tunnelIP); found && hsView.User == hsUserName {
				if s.runtimeStore.UpdateHeadscaleNodeID(node.ID, hsView.ID) && s.runtimePersister != nil {
					s.runtimePersister.NotifyHighPriority()
				}
			}
		} else if node.HeadscaleNodeID == 0 {
			if hsView, found := snapshot.GetByUserNameAndNodeName(hsUserName, node.Name); found {
				if s.runtimeStore.UpdateHeadscaleNodeID(node.ID, hsView.ID) && s.runtimePersister != nil {
					s.runtimePersister.NotifyHighPriority()
				}
			}
		}
	}

	if len(req.DomainRegistrations) > 0 && nodeType == model.NodeTypeAgent {
		s.handleNodeDomainRegistrations(ctx, &node, &user, req.TunnelIp, req.DomainRegistrations)
	}
	if req.TunnelIp != "" {
		updates := map[string]any{"target_ip": req.TunnelIp}
		if sshPort, ok := nodeSSHTargetPortFromRegistrations(req.DomainRegistrations); ok {
			updates["target_port"] = sshPort
		}
		if err := db.DB.WithContext(ctx).Model(&model.DomainRegistry{}).
			Where("node_id = ? AND resource_kind = ? AND type = ?", node.ID, model.DomainResourceNode, model.DomainTypeSSH).
			Updates(updates).Error; err != nil {
			logger.Errorf("同步 Node SSH 域名目标地址失败: node_id=%d, ip=%s, err=%v", node.ID, req.TunnelIp, err)
		}
	}

	// 处理已连接的 Endpoint 上报
	if len(req.ConnectedEndpoints) > 0 {
		s.handleConnectedEndpoints(ctx, agentID, node.ID, req.ConnectedEndpoints)
	}

	// ContainerSSH candidates are runtime evidence. The NodeID comes from the
	// authenticated heartbeat stream, never from an Agent-supplied field.
	if len(req.ContainerCandidates) > 0 {
		s.handleContainerCandidates(ctx, node.ID, req.ContainerCandidates)
	}
	if req.ContainerSshProtocol == "v1" && len(req.ContainerSshSessionEvents) > 0 {
		acks := s.handleContainerSessionEvents(ctx, node.ID, req.ContainerSshSessionEvents)
		if len(acks) > 0 {
			s.containerSessionAckMutex.Lock()
			if s.containerSessionAcks == nil {
				s.containerSessionAcks = make(map[uint64][]string)
			}
			s.containerSessionAcks[node.ID] = append(s.containerSessionAcks[node.ID], acks...)
			s.containerSessionAckMutex.Unlock()
		}
	}

	if s.updateService != nil {
		for _, report := range req.UpdateStatuses {
			if err := s.updateService.Report(ctx, report.TaskId, service.UpdateStatusReporter{
				Source:     "agent",
				Component:  model.ComponentAgent,
				TargetType: model.UpdateTargetNode,
				TargetID:   fmt.Sprintf("%d", node.ID),
			}, service.UpdateStatusReport{
				Phase:           report.Phase,
				Progress:        int(report.Progress),
				CurrentVersion:  report.CurrentVersion,
				CurrentCommitID: report.CurrentCommitId,
				CurrentSHA256:   report.CurrentSha256,
				Sequence:        report.Sequence,
				ErrorCode:       report.ErrorCode,
				ErrorMessage:    report.ErrorMessage,
			}); err != nil {
				logger.Warnf("处理 Agent 更新状态失败: task_id=%s, err=%v", report.TaskId, err)
			}
		}
	}

	// 处理操作审计记录上报
	if len(req.AuditRecords) > 0 {
		s.handleAuditRecords(ctx, agentID, req.AuditRecords)
	}

	// 注意：K8S Service 域名现在通过 DomainRegistrations 上报（Agent 统一上报所有域名）
	// 旧的 DiscoveredServices 字段已废弃，不再处理

	return node.ID
}

func (s *AgentServiceServer) handleNodeDomainRegistrations(ctx context.Context, node *model.Node, user *model.User, tunnelIP string, registrations []*pb.DomainRegistration) {
	if node == nil || user == nil || node.ID == 0 {
		return
	}
	domains := s.domainService
	if domains == nil {
		domains = service.NewDomainService(db.DB)
	}
	for _, registration := range registrations {
		if registration == nil || strings.ToLower(strings.TrimSpace(registration.Type)) != string(model.DomainTypeSSH) || registration.EndpointId != "" {
			continue
		}
		if !user.SSHEnabled {
			if err := domains.DeleteNodeSSHDomain(ctx, node, user); err != nil {
				logger.Errorf("处理 Agent SSH 域名禁用失败: node_id=%d, reported_domain=%s, err=%v", node.ID, registration.Domain, err)
			}
			continue
		}
		if tunnelIP != "" {
			node.IP = strings.TrimSpace(tunnelIP)
		} else if registration.TargetIp != "" {
			node.IP = strings.TrimSpace(registration.TargetIp)
		}
		if err := domains.CreateNodeSSHDomainWithPortAndUsers(ctx, node, user, int(registration.TargetPort), registration.SshUsers); err != nil {
			logger.Errorf("处理 Agent SSH 域名注册失败: node_id=%d, reported_domain=%s, err=%v", node.ID, registration.Domain, err)
			continue
		}
		if err := s.syncNodeHostSSHResource(ctx, node); err != nil {
			logger.Warnf("同步 HostSSH 租户资源失败: node_id=%d, reported_domain=%s, err=%v", node.ID, registration.Domain, err)
		}
	}
}

func nodeSSHTargetPortFromRegistrations(registrations []*pb.DomainRegistration) (int, bool) {
	for _, registration := range registrations {
		if registration == nil {
			continue
		}
		if strings.ToLower(strings.TrimSpace(registration.Type)) != string(model.DomainTypeSSH) || registration.EndpointId != "" {
			continue
		}
		if registration.TargetPort > 0 {
			return int(registration.TargetPort), true
		}
	}
	return 0, false
}

func (s *AgentServiceServer) syncNodeHostSSHResource(ctx context.Context, node *model.Node) error {
	if node == nil || node.ID == 0 {
		return nil
	}
	now := time.Now().UTC()
	var binding model.TechnicalResourceBinding
	if err := db.DB.WithContext(ctx).
		Where("source_type = ? AND source_id = ? AND enabled = ?", model.TechnicalResourceBindingLegacyNode, fmt.Sprint(node.ID), true).
		Order("updated_at DESC, created_at DESC").
		First(&binding).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	var membership model.UserTenantManagementMembership
	if err := db.DB.WithContext(ctx).
		Where("user_id = ? AND enabled = ? AND valid_from <= ? AND (expires_at IS NULL OR expires_at > ?)", binding.BoundByUserID, true, now, now).
		Order("CASE role WHEN 'tenant_admin' THEN 0 WHEN 'security_auditor' THEN 1 ELSE 2 END, updated_at DESC").
		First(&membership).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	var tenant model.Tenant
	if err := db.DB.WithContext(ctx).Where("id = ? AND status = ?", membership.TenantID, model.TenantStatusActive).First(&tenant).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	var domain model.DomainRegistry
	if err := db.DB.WithContext(ctx).
		Where("type = ? AND resource_kind = ? AND resource_id = ?", model.DomainTypeSSH, model.DomainResourceNode, fmt.Sprint(node.ID)).
		Or("type = ? AND node_id = ?", model.DomainTypeSSH, node.ID).
		Order("updated_at DESC, id DESC").
		First(&domain).Error; err != nil {
		return err
	}
	displayName := strings.TrimSpace(domain.Domain)
	if displayName == "" {
		displayName = strings.TrimSpace(node.Name)
	}
	if displayName == "" {
		displayName = fmt.Sprintf("node-%d", node.ID)
	}
	state := model.ResourceStateDegraded
	if domain.Status == model.DomainStatusOnline {
		state = model.ResourceStateAvailable
	}
	providerID := strings.TrimSpace(domain.ProviderID)
	if providerID == "" {
		var technical model.TechnicalResource
		if err := db.DB.WithContext(ctx).Select("provider_id").First(&technical, "id = ?", binding.TechnicalResourceID).Error; err == nil {
			providerID = technical.ProviderID
		}
	}

	var resource model.Resource
	err := db.DB.WithContext(ctx).Where("type = ? AND agent_node_id = ?", model.ResourceTypeHostSSH, node.ID).First(&resource).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		resource = model.Resource{
			ID:             uuid.NewString(),
			TenantID:       tenant.ID,
			Type:           model.ResourceTypeHostSSH,
			DisplayName:    displayName,
			ProviderID:     providerID,
			AgentNodeID:    node.ID,
			State:          state,
			TargetRevision: 1,
		}
		return db.DB.WithContext(ctx).Create(&resource).Error
	}
	if err != nil {
		return err
	}
	updates := map[string]any{
		"tenant_id":       tenant.ID,
		"display_name":    displayName,
		"provider_id":     providerID,
		"agent_node_id":   node.ID,
		"state":           state,
		"target_revision": gorm.Expr("CASE WHEN target_revision < 1 THEN 1 ELSE target_revision END"),
		"updated_at":      now,
	}
	return db.DB.WithContext(ctx).Model(&resource).Updates(updates).Error
}

func (s *AgentServiceServer) handleContainerCandidates(ctx context.Context, nodeID uint64, reports []*pb.ContainerDiscoveryCandidate) {
	now := time.Now()
	for _, report := range reports {
		if report == nil || report.PodUid == "" || report.Namespace == "" || report.ContainerName == "" {
			continue
		}
		var candidate model.DiscoveryCandidate
		err := db.DB.WithContext(ctx).Where("agent_node_id = ? AND pod_uid = ? AND container_name = ?", nodeID, report.PodUid, report.ContainerName).First(&candidate).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			candidate = model.DiscoveryCandidate{ID: uuid.NewString(), AgentNodeID: nodeID, PodUID: report.PodUid, ContainerName: report.ContainerName, Status: model.DiscoveryCandidateObserved}
		} else if err != nil {
			logger.Warnf("查询 ContainerSSH Candidate 失败: node_id=%d pod_uid=%s err=%v", nodeID, report.PodUid, err)
			continue
		}
		if candidate.Status == "" || candidate.Status == model.DiscoveryCandidateStale {
			candidate.Status = model.DiscoveryCandidateObserved
		}
		candidate.ProviderHint = strings.TrimSpace(report.ProviderHint)
		candidate.WorkspaceHint = strings.TrimSpace(report.WorkspaceHint)
		candidate.GenerationHint = report.GenerationHint
		candidate.ClusterID = strings.TrimSpace(report.ClusterId)
		candidate.Namespace = strings.TrimSpace(report.Namespace)
		candidate.PodName = strings.TrimSpace(report.PodName)
		candidate.Ready = report.Ready
		candidate.ObservedAt = now
		leaseSeconds := int(report.LeaseSeconds)
		if leaseSeconds <= 0 || leaseSeconds > 24*60*60 {
			leaseSeconds = 120
		}
		expires := now.Add(time.Duration(leaseSeconds) * time.Second)
		candidate.LeaseExpiresAt = &expires
		if labelsJSON, marshalErr := json.Marshal(report.Labels); marshalErr == nil {
			candidate.LabelSnapshot = string(labelsJSON)
		}
		if err := db.DB.WithContext(ctx).Save(&candidate).Error; err != nil {
			logger.Warnf("保存 ContainerSSH Candidate 失败: node_id=%d pod_uid=%s err=%v", nodeID, report.PodUid, err)
			continue
		}
		reconciler := s.resourceReconciler
		if reconciler == nil {
			reconciler = service.NewResourceReconciliationService(db.DB)
		}
		if _, reconcileErr := reconciler.ReconcileCandidate(ctx, candidate.ID); reconcileErr != nil {
			logger.Warnf("ContainerSSH Candidate 自动匹配失败: candidate_id=%s err=%v", candidate.ID, reconcileErr)
		}
	}
}

// heartbeatResponseContext lets response-side database work finish without
// discarding incoming gRPC metadata. Inventory capability negotiation uses the
// existing Agent bearer from that metadata to resolve its trusted binding.
func heartbeatResponseContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return context.WithoutCancel(ctx)
}

// sendHeartbeatResponse 发送心跳响应
func (s *AgentServiceServer) sendHeartbeatResponse(ctx context.Context, stream pb.AgentService_HeartbeatServer, conn *AgentConnection, sessionAcks *sessionAuthorizationHeartbeatAcks) error {
	if conn == nil {
		return fmt.Errorf("Agent connection is required")
	}
	agentID, nodeID := conn.AgentID, conn.NodeID
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
	supplyInventoryAuthorized := s.authorizeSupplyInventoryNegotiation(ctx, nodeID)
	if supplyInventoryAuthorized {
		resp.SupplyInventoryConfig = s.supplyInventoryConfigForBinding(ctx, model.TechnicalResourceBindingLegacyNode, fmt.Sprintf("%d", nodeID))
		resp.WorkloadInventoryConfig = s.workloadInventoryConfigForBinding(ctx, model.TechnicalResourceBindingLegacyNode, nodeSourceID(nodeID))
	}

	// 构建 Agent 能力配置
	// SSH：仅 Agent 角色从 User 表读取（User 级别共享）；Client/CloudIDE 保持本地配置
	// K8S/SVC：从 Node 表读取（Node 级别独立）
	var capUser model.User
	if err := db.DB.WithContext(ctx).First(&capUser, agentID).Error; err == nil {
		capConfig := &pb.AgentCapabilityConfig{}

		// Client/CloudIDE 使用 client token 注册，User.SSHEnabled 默认是 false。
		// 如果这里下发 SSH=false，会覆盖 CloudIDE 本地 enable_ssh=true，导致
		// Tailscale SSH 在启动后立刻被关闭，Desktop 连接 100.64.x.x:22 被拒绝。
		if capUser.Role == model.UserRoleAgent {
			capConfig.SshEnabled = capUser.SSHEnabled
			capConfig.SshEnabledSet = true
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

	// Only an Agent that explicitly negotiated v1 may interpret an empty list
	// as a complete revocation snapshot. Old Agents continue unchanged.
	if nodeID > 0 {
		var protocol string
		if err := db.DB.WithContext(ctx).Model(&model.Node{}).Where("id = ?", nodeID).Pluck("container_ssh_protocol", &protocol).Error; err == nil && protocol == "v1" {
			resp.ContainerSshProtocol = "v1"
			resp.ContainerSshPermissions = s.queryContainerSSHPermissions(ctx, nodeID)
			s.containerSessionAckMutex.Lock()
			resp.ContainerSshSessionAckEventIds = s.containerSessionAcks[nodeID]
			delete(s.containerSessionAcks, nodeID)
			s.containerSessionAckMutex.Unlock()
			var revoked []model.ContainerSession
			if err := db.DB.WithContext(ctx).Where("agent_node_id = ? AND status = ? AND disconnect_acknowledged_at IS NULL", nodeID, model.ContainerSessionRevoked).Find(&revoked).Error; err == nil {
				for _, session := range revoked {
					resp.ContainerSshDisconnectSessionIds = append(resp.ContainerSshDisconnectSessionIds, session.ID)
				}
			}

		}
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
			endpointConfig := &pb.EndpointCapabilityConfig{
				EndpointName:      ep.Name,
				SshEnabled:        ep.SSHEnabled,
				SshPort:           uint32(ep.SSHPort), // 新增：从 Endpoint 表读取端口
				K8SapiEnabled:     ep.K8SAPIEnabled,
				K8SapiPort:        uint32(ep.K8SAPIPort), // 新增：从 Endpoint 表读取端口
				K8SapiApiServer:   ep.K8SAPIApiServer,
				K8SserviceEnabled: ep.K8SServiceEnabled,
			}
			if supplyInventoryAuthorized {
				endpointConfig.SupplyInventoryConfig = s.supplyInventoryConfigForBinding(ctx, model.TechnicalResourceBindingLegacyEndpoint, ep.ID)
				endpointConfig.WorkloadInventoryConfig = s.workloadInventoryConfigForBinding(ctx, model.TechnicalResourceBindingLegacyEndpoint, ep.ID)
			}
			resp.EndpointCapabilityConfigs = append(resp.EndpointCapabilityConfigs, endpointConfig)
		}
		logger.Infof("准备发送心跳响应: EndpointCapabilityConfigs 数量=%d", len(resp.EndpointCapabilityConfigs))
	} else {
		logger.Errorf("查询 Endpoint 配置失败 (agent_id=%d): %v", agentID, err)
	}

	if s.updateService != nil {
		directives, err := s.updateService.DirectivesForNode(ctx, nodeID, model.ComponentAgent)
		if err != nil {
			logger.Warnf("查询 Agent 更新任务失败: node_id=%d, err=%v", nodeID, err)
		} else {
			for _, directive := range directives {
				resp.UpdateDirectives = append(resp.UpdateDirectives, toProtoUpdateDirective(directive))
			}
		}

		endpointDirectives, err := s.updateService.DirectivesForAgentEndpoints(ctx, agentID)
		if err != nil {
			logger.Warnf("查询 Endpoint 更新任务失败: agent_id=%d, err=%v", agentID, err)
		} else {
			for _, directive := range endpointDirectives {
				resp.EndpointUpdateDirectives = append(resp.EndpointUpdateDirectives, toProtoUpdateDirective(directive))
			}
		}

		if nodeID > 0 {
			var restartingTasks []model.UpdateTask
			if err := db.DB.WithContext(ctx).
				Where("target_type = ? AND target_id = ? AND component = ? AND status = ?", model.UpdateTargetNode, strconv.FormatUint(nodeID, 10), model.ComponentAgent, model.UpdateTaskRestarting).
				Find(&restartingTasks).Error; err == nil && len(restartingTasks) > 0 {
				var currentNode model.Node
				if err := db.DB.WithContext(ctx).First(&currentNode, nodeID).Error; err == nil {
					for _, task := range restartingTasks {
						if task.DesiredCommitID != "" && task.DesiredSHA256 != "" &&
							currentNode.Version == task.DesiredVersion &&
							currentNode.CommitID == task.DesiredCommitID &&
							currentNode.BinarySHA256 == task.DesiredSHA256 {
							resp.UpdateHealthConfirmations = append(resp.UpdateHealthConfirmations, &pb.UpdateHealthConfirmation{
								TaskId:          task.ID,
								Version:         currentNode.Version,
								CommitId:        currentNode.CommitID,
								ArtifactSha256:  currentNode.BinarySHA256,
								ConfirmedAtUnix: time.Now().Unix(),
							})
						}
					}
				}
			}
		}

		if len(allEndpoints) > 0 {
			endpointByID := make(map[string]model.Endpoint, len(allEndpoints))
			endpointIDs := make([]string, 0, len(allEndpoints))
			for _, endpoint := range allEndpoints {
				endpointByID[endpoint.ID] = endpoint
				endpointIDs = append(endpointIDs, endpoint.ID)
			}
			var restartingEndpointTasks []model.UpdateTask
			if err := db.DB.WithContext(ctx).
				Where("target_type = ? AND target_id IN ? AND component = ? AND status = ?", model.UpdateTargetEndpoint, endpointIDs, model.ComponentEndpoint, model.UpdateTaskRestarting).
				Find(&restartingEndpointTasks).Error; err == nil {
				for _, task := range restartingEndpointTasks {
					if confirmation := endpointUpdateHealthConfirmation(task, endpointByID[task.TargetID], time.Now()); confirmation != nil {
						resp.UpdateHealthConfirmations = append(resp.UpdateHealthConfirmations, confirmation)
					}
				}
			}
		}
	}
	s.appendSessionAuthorizationResponse(ctx, conn, resp, sessionAcks)

	return stream.Send(resp)
}

func (s *AgentServiceServer) queryContainerSSHPermissions(ctx context.Context, nodeID uint64) []*pb.ContainerSSHPermission {
	if nodeID == 0 {
		return nil
	}
	now := time.Now()
	var resources []model.Resource
	if err := db.DB.WithContext(ctx).
		Where("agent_node_id = ? AND type = ? AND target_revision > 0 AND state IN ?", nodeID, model.ResourceTypeContainerSSH, []model.ResourceState{model.ResourceStateAvailable, model.ResourceStateDegraded}).
		Find(&resources).Error; err != nil {
		logger.Warnf("查询 ContainerSSH Resource 快照失败: node_id=%d err=%v", nodeID, err)
		return nil
	}

	result := make([]*pb.ContainerSSHPermission, 0)
	for _, resource := range resources {
		var target model.ResourceTarget
		if err := db.DB.WithContext(ctx).Where("resource_id = ? AND revision = ? AND agent_node_id = ? AND ready = ?", resource.ID, resource.TargetRevision, nodeID, true).First(&target).Error; err != nil {
			continue
		}
		var grants []model.AccessGrant
		if err := db.DB.WithContext(ctx).Where("resource_id = ? AND tenant_id = ? AND subject_type IN ? AND status = ? AND valid_from <= ? AND expires_at > ?", resource.ID, resource.TenantID, []string{"user", "group"}, "enabled", now, now).Order("subject_type DESC").Find(&grants).Error; err != nil {
			logger.Warnf("查询 ContainerSSH Grant 快照失败: resource_id=%s err=%v", resource.ID, err)
			continue
		}
		resolved := make(map[uint64]*pb.ContainerSSHPermission)
		for _, grant := range grants {
			if !containsAction(parseJSONStringArray(grant.Actions), "shell") {
				continue
			}
			userIDs := []uint64{grant.SubjectUserID}
			if grant.SubjectType == "group" {
				if grant.SubjectGroupID == nil {
					continue
				}
				var group model.Group
				if err := db.DB.WithContext(ctx).Where("id = ? AND tenant_id = ?", *grant.SubjectGroupID, resource.TenantID).First(&group).Error; err != nil {
					continue
				}
				userIDs = nil
				if err := db.DB.WithContext(ctx).Model(&model.GroupMember{}).Where("group_id = ?", group.ID).Pluck("user_id", &userIDs).Error; err != nil {
					continue
				}
			}
			for _, userID := range userIDs {
				var user model.User
				if err := db.DB.WithContext(ctx).Where("id = ? AND enabled = ?", userID, true).First(&user).Error; err != nil {
					continue
				}
				var membership model.TenantMembership
				if err := db.DB.WithContext(ctx).Where("tenant_id = ? AND user_id = ? AND enabled = ? AND (expires_at IS NULL OR expires_at > ?)", resource.TenantID, user.ID, true, now).First(&membership).Error; err != nil {
					continue
				}
				permission := &pb.ContainerSSHPermission{
					UserId: user.ID, UserName: user.Name, ResourceId: resource.ID,
					Namespace: target.Namespace, PodName: target.PodName, PodUid: target.PodUID, ContainerName: target.ContainerName,
					TargetRevision: target.Revision, GrantRevision: grant.Revision, MaxSessionSeconds: int32(grant.MaxSessionSeconds), ListenPort: uint32(resource.ContainerSSHPort),
				}
				current, exists := resolved[user.ID]
				if !exists || permission.GrantRevision > current.GrantRevision {
					resolved[user.ID] = permission
				}
			}
		}
		for _, permission := range resolved {
			result = append(result, permission)
		}
	}
	return result
}

func containsAction(actions []string, expected string) bool {
	for _, action := range actions {
		if action == expected {
			return true
		}
	}
	return false
}

func toProtoUpdateDirective(directive service.UpdateDirective) *pb.UpdateDirective {
	return &pb.UpdateDirective{
		TaskId:        directive.TaskID,
		Component:     directive.Component,
		Version:       directive.Version,
		ArtifactId:    directive.ArtifactID,
		DownloadUrl:   directive.DownloadURL,
		Filename:      directive.Filename,
		Os:            directive.OS,
		Arch:          directive.Arch,
		Size:          directive.Size,
		Sha256:        directive.SHA256,
		Force:         directive.Force,
		NotBeforeUnix: directive.NotBeforeUnix,
		DeadlineUnix:  directive.DeadlineUnix,
		Action:        directive.Action,
		TargetName:    directive.TargetName,
		CommitId:      directive.CommitID,
	}
}

func endpointUpdateHealthConfirmation(task model.UpdateTask, endpoint model.Endpoint, confirmedAt time.Time) *pb.UpdateHealthConfirmation {
	if task.DesiredCommitID == "" || task.DesiredSHA256 == "" || endpoint.ID == "" ||
		endpoint.Version != task.DesiredVersion || endpoint.CommitID != task.DesiredCommitID ||
		endpoint.BinarySHA256 != task.DesiredSHA256 {
		return nil
	}
	return &pb.UpdateHealthConfirmation{
		TaskId: task.ID, Version: endpoint.Version, CommitId: endpoint.CommitID,
		ArtifactSha256: endpoint.BinarySHA256, ConfirmedAtUnix: confirmedAt.Unix(), TargetName: endpoint.Name,
	}
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

// DisconnectNode 断开指定 Node 的 Agent 连接
func (s *AgentServiceServer) DisconnectNode(nodeID uint64) {
	s.connMutex.Lock()
	defer s.connMutex.Unlock()

	if conn, exists := s.connections[nodeID]; exists {
		conn.Cancel()
		delete(s.connections, nodeID)
		logger.Infof("已断开 Agent Node 连接: agentId=%d, nodeId=%d", conn.AgentID, nodeID)
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
		endpointID := ""

		// 更新 Endpoint 内存缓存
		cache.SetEndpointStatus(ep.Name, cache.EndpointStatus{
			EndpointName:  ep.Name,
			UserID:        agentID,
			LastHeartbeat: now,
		})
		logger.Debugf("更新 Endpoint 内存缓存: endpoint_name=%s, user_id=%d", ep.Name, agentID)

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
				Hostname:                ep.Name,
				HostDomainLabel:         service.SuggestedHostDomainLabel(ctx, db.DB, agentID, ep.Name),
				Version:                 ep.Version,
				CommitID:                ep.CommitId,
				BinarySHA256:            ep.BinarySha256,
				UpdaterProtocol:         ep.UpdaterProtocol,
				OS:                      ep.Os,
				Arch:                    ep.Arch,
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
				endpointID = record.ID
				logger.Infof("创建 Endpoint: name=%s, id=%s, ssh_port=%d, k8sapi_port=%d, ssh_users=%v",
					ep.Name, record.ID, record.SSHPort, record.K8SAPIPort, ep.SshUsers)

				// Endpoint 创建成功后，创建域名记录（使用预分配的端口）
				// 查询 Agent Node 和 User 信息
				var agentNode model.Node
				var user model.User
				if err := db.DB.WithContext(ctx).First(&agentNode, "user_id = ? AND type = ?", agentID, model.NodeTypeAgent).Error; err == nil {
					if err := db.DB.WithContext(ctx).First(&user, agentID).Error; err == nil {
						// 创建 SSH 域名（使用 Endpoint 的端口）
						if record.SSHEnabled {
							if err := s.domainService.CreateEndpointSSHDomain(ctx, &record, &agentNode, &user); err != nil {
								logger.Errorf("创建 Endpoint SSH 域名失败: endpoint=%s, err=%v", ep.Name, err)
							}
						}
						// 创建 K8SAPI 域名（使用 Endpoint 的端口）
						if record.K8SAPIEnabled {
							if err := s.domainService.CreateEndpointK8SAPIDomain(ctx, &record, &agentNode, &user); err != nil {
								logger.Errorf("创建 Endpoint K8SAPI 域名失败: endpoint=%s, err=%v", ep.Name, err)
							}
						}
					}
				}
			}
		} else {
			// 存在，更新状态和配置（不更新能力开关，能力由 Web 界面管理）
			updates := map[string]any{
				"status":           "online",
				"updater_protocol": ep.UpdaterProtocol,
			}
			endpointID = existing.ID
			if ep.Version != "" {
				updates["version"] = ep.Version
			}
			if ep.CommitId != "" {
				updates["commit_id"] = ep.CommitId
			}
			if ep.BinarySha256 != "" {
				updates["binary_sha256"] = ep.BinarySha256
			}
			if ep.Os != "" {
				updates["os"] = ep.Os
			}
			if ep.Arch != "" {
				updates["arch"] = ep.Arch
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
							Where("endpoint_id = ? AND user_id = ? AND type = ?", updatedEndpoint.ID, agentID, model.DomainTypeSSH).
							Update("target_port", updatedEndpoint.SSHPort).Error; err != nil {
							logger.Errorf("更新 Endpoint SSH 域名端口失败: endpoint=%s, port=%d, err=%v", ep.Name, updatedEndpoint.SSHPort, err)
						} else {
							logger.Infof("更新 Endpoint SSH 域名端口: endpoint=%s, port=%d", ep.Name, updatedEndpoint.SSHPort)
						}
					}

					// 更新 K8SAPI 域名的端口
					if updatedEndpoint.K8SAPIPort > 0 {
						if err := db.DB.WithContext(ctx).Model(&model.DomainRegistry{}).
							Where("endpoint_id = ? AND user_id = ? AND type = ?", updatedEndpoint.ID, agentID, model.DomainTypeK8SAPI).
							Update("target_port", updatedEndpoint.K8SAPIPort).Error; err != nil {
							logger.Errorf("更新 Endpoint K8SAPI 域名端口失败: endpoint=%s, port=%d, err=%v", ep.Name, updatedEndpoint.K8SAPIPort, err)
						} else {
							logger.Infof("更新 Endpoint K8SAPI 域名端口: endpoint=%s, port=%d", ep.Name, updatedEndpoint.K8SAPIPort)
						}
					}
				}
			}
		}

		if endpointID != "" && s.updateService != nil {
			for _, report := range ep.UpdateStatuses {
				if err := s.updateService.Report(ctx, report.TaskId, service.UpdateStatusReporter{
					Source:     "endpoint",
					Component:  model.ComponentEndpoint,
					TargetType: model.UpdateTargetEndpoint,
					TargetID:   endpointID,
				}, service.UpdateStatusReport{
					Phase:           report.Phase,
					Progress:        int(report.Progress),
					CurrentVersion:  report.CurrentVersion,
					CurrentCommitID: report.CurrentCommitId,
					CurrentSHA256:   report.CurrentSha256,
					Sequence:        report.Sequence,
					ErrorCode:       report.ErrorCode,
					ErrorMessage:    report.ErrorMessage,
				}); err != nil {
					logger.Warnf("处理 Endpoint 更新状态失败: endpoint=%s, task_id=%s, err=%v", ep.Name, report.TaskId, err)
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
		UserName: req.UserName,
		DeviceIp: req.DeviceIp,
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
