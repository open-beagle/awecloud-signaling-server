// Package grpc 提供 gRPC 服务实现
package grpc

import (
	"context"
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

	// Agent 连接管理
	connections map[uint64]*AgentConnection
	connMutex   sync.RWMutex

	// 配置版本号（用于配置同步）
	configVersion int64
	versionMutex  sync.RWMutex

	// Headscale 客户端
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

	// 初始化 Headscale 客户端
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

	// 查询 Agent
	var agent model.Agent
	if err := db.DB.Where("name = ?", req.Name).First(&agent).Error; err != nil {
		logger.Warnf("Agent 不存在: %s", req.Name)
		return &pb.AgentRegisterResponse{
			Success: false,
			Message: "Agent 不存在",
		}, nil
	}

	// 验证密钥
	if err := bcrypt.CompareHashAndPassword([]byte(agent.SecretHash), []byte(req.Secret)); err != nil {
		logger.Warnf("Agent 密钥验证失败: %s", req.Name)
		return &pb.AgentRegisterResponse{
			Success: false,
			Message: "认证失败",
		}, nil
	}

	// 更新 Agent 信息
	now := time.Now()
	agent.LastHeartbeat = &now
	if req.Version != "" {
		agent.Version = req.Version
	}
	if req.SystemInfo != nil {
		systemInfo := model.SystemInfoData{
			OS:        req.SystemInfo.Os,
			OSVersion: req.SystemInfo.OsVersion,
			Arch:      req.SystemInfo.Arch,
			Hostname:  req.SystemInfo.Hostname,
			CPU:       req.SystemInfo.Cpu,
			CPUCores:  int(req.SystemInfo.CpuCores),
			MemoryGB:  int(req.SystemInfo.MemoryGb),
		}
		if data, err := json.Marshal(systemInfo); err == nil {
			agent.SystemInfo = string(data)
		}
	}

	if err := db.DB.Save(&agent).Error; err != nil {
		logger.Errorf("更新 Agent 信息失败: %v", err)
	}

	// 构建响应
	resp := &pb.AgentRegisterResponse{
		Success: true,
		Message: "注册成功",
		AgentId: agent.ID,
	}

	// 创建 Tailscale 预认证密钥
	if s.headscaleClient != nil && s.config != nil {
		authKey, serverURL, err := s.createAgentAuthKey(ctx, req.Name, agent.ID)
		if err != nil {
			logger.Errorf("创建 Tailscale 预认证密钥失败: %v", err)
			// 不影响注册，继续返回成功
		} else {
			resp.AuthKey = authKey
			resp.ServerUrl = serverURL
			logger.Infof("已为 Agent %s 创建 Tailscale 预认证密钥", req.Name)
		}
	}

	logger.Infof("Agent 注册成功: %s (ID: %d)", req.Name, agent.ID)
	return resp, nil
}

// Authenticate Agent 认证
func (s *AgentServiceServer) Authenticate(ctx context.Context, req *pb.AgentAuthenticateRequest) (*pb.AgentAuthenticateResponse, error) {
	logger.Infof("Agent 认证请求: agent_id=%d, version=%s", req.AgentId, req.Version)

	// 查询 Agent
	var agent model.Agent
	if err := db.DB.First(&agent, req.AgentId).Error; err != nil {
		logger.Warnf("Agent 不存在: %d", req.AgentId)
		return &pb.AgentAuthenticateResponse{
			Success: false,
			Message: "Agent 不存在",
		}, nil
	}

	// 验证密钥
	if err := bcrypt.CompareHashAndPassword([]byte(agent.SecretHash), []byte(req.Secret)); err != nil {
		logger.Warnf("Agent 密钥验证失败: %d", req.AgentId)
		return &pb.AgentAuthenticateResponse{
			Success: false,
			Message: "认证失败",
		}, nil
	}

	// 更新 Agent 信息
	now := time.Now()
	agent.LastHeartbeat = &now
	if req.Version != "" {
		agent.Version = req.Version
	}
	if req.SystemInfo != nil {
		systemInfo := model.SystemInfoData{
			OS:        req.SystemInfo.Os,
			OSVersion: req.SystemInfo.OsVersion,
			Arch:      req.SystemInfo.Arch,
			Hostname:  req.SystemInfo.Hostname,
			CPU:       req.SystemInfo.Cpu,
			CPUCores:  int(req.SystemInfo.CpuCores),
			MemoryGB:  int(req.SystemInfo.MemoryGb),
		}
		if data, err := json.Marshal(systemInfo); err == nil {
			agent.SystemInfo = string(data)
		}
	}

	if err := db.DB.Save(&agent).Error; err != nil {
		logger.Errorf("更新 Agent 信息失败: %v", err)
	}

	// 构建响应
	resp := &pb.AgentAuthenticateResponse{
		Success: true,
		Message: "认证成功",
	}

	// 检查是否需要重新创建预认证密钥
	if s.headscaleClient != nil && s.config != nil {
		needAuthKey := false

		// 检查 Node 是否存在
		if agent.NodeID == 0 {
			needAuthKey = true
		} else {
			// 检查 Headscale Node 状态
			node, err := s.headscaleClient.GetNode(ctx, agent.NodeID)
			if err != nil || node == nil {
				needAuthKey = true
			}
		}

		if needAuthKey {
			authKey, serverURL, err := s.createAgentAuthKey(ctx, agent.Name, agent.ID)
			if err != nil {
				logger.Errorf("创建 Tailscale 预认证密钥失败: %v", err)
			} else {
				resp.AuthKey = authKey
				resp.ServerUrl = serverURL
				logger.Infof("已为 Agent %s 创建新的 Tailscale 预认证密钥", agent.Name)
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
		return status.Error(codes.InvalidArgument, "无法接收初始消息")
	}

	agentID = firstReq.AgentId
	logger.Infof("Agent 心跳流建立: agent_id=%d", agentID)

	// 验证 Agent 存在
	var agent model.Agent
	if err := db.DB.First(&agent, agentID).Error; err != nil {
		return status.Error(codes.NotFound, "Agent 不存在")
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
		logger.Infof("Agent 心跳流断开: agent_id=%d", agentID)

		// 更新 Agent 离线状态
		cache.UpdateAgentTsConnectedAt(int64(agentID), nil)
	}()

	// 处理第一个心跳
	s.handleHeartbeat(agentID, firstReq)

	// 发送首次响应（包含配置）
	if err := s.sendHeartbeatResponse(stream, agentID, true); err != nil {
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
			s.handleHeartbeat(agentID, req)

			// 发送响应
			if err := s.sendHeartbeatResponse(stream, agentID, false); err != nil {
				logger.Errorf("发送心跳响应失败: %v", err)
				return err
			}
		}
	}
}

// handleHeartbeat 处理心跳请求
func (s *AgentServiceServer) handleHeartbeat(agentID uint64, req *pb.AgentHeartbeatRequest) {
	// 更新数据库心跳时间
	now := time.Now()
	if err := db.DB.Model(&model.Agent{}).Where("id = ?", agentID).Updates(map[string]interface{}{
		"last_heartbeat": now,
		"ip":             req.TunnelIp,
	}).Error; err != nil {
		logger.Errorf("更新 Agent 心跳失败: %v", err)
	}

	// 更新内存缓存 - 连接时间
	if req.TunnelConnected {
		cache.UpdateAgentTsConnectedAt(int64(agentID), &now)
	} else {
		cache.UpdateAgentTsConnectedAt(int64(agentID), nil)
	}

	// 更新内存缓存 - 网络信息
	if req.Hostname != "" || len(req.Networks) > 0 {
		networks := make([]cache.NetworkInterface, len(req.Networks))
		for i, n := range req.Networks {
			networks[i] = cache.NetworkInterface{
				Name:    n.Name,
				IP:      n.Ip,
				Mask:    n.Mask,
				Gateway: n.Gateway,
			}
		}
		cache.UpdateAgentNetworkInfo(int64(agentID), req.Hostname, req.Runtime, networks)
	}

	// 处理服务状态上报
	for _, svc := range req.ServiceStatus {
		logger.Debugf("Agent %d 服务状态: service_id=%s, running=%v, error=%s",
			agentID, svc.ServiceId, svc.Running, svc.Error)
	}

	// 处理端口访问状态上报
	for _, fwd := range req.ForwardStatus {
		logger.Debugf("Agent %d 端口访问状态: forward_id=%s, running=%v, error=%s",
			agentID, fwd.ForwardId, fwd.Running, fwd.Error)
	}
}

// sendHeartbeatResponse 发送心跳响应
func (s *AgentServiceServer) sendHeartbeatResponse(stream pb.AgentService_HeartbeatServer, agentID uint64, includeConfig bool) error {
	s.versionMutex.RLock()
	version := s.configVersion
	s.versionMutex.RUnlock()

	resp := &pb.AgentHeartbeatResponse{
		ConfigVersion: version,
	}

	// 如果需要包含配置（首次连接或配置变更）
	if includeConfig {
		// 查询端口映射服务
		var services []model.ProxyService
		if err := db.DB.Where("agent_id = ?", agentID).Find(&services).Error; err != nil {
			logger.Errorf("查询端口映射服务失败: %v", err)
		} else {
			for _, svc := range services {
				resp.Services = append(resp.Services, &pb.ServiceConfig{
					Id:         svc.ID,
					Name:       svc.Name,
					SourceAddr: svc.SourceAddr,
					TargetAddr: svc.TargetAddr,
					Enabled:    svc.Enabled,
				})
			}
		}

		// 查询端口访问配置
		var forwards []model.PortForward
		if err := db.DB.Preload("TargetService").Where("agent_id = ?", agentID).Find(&forwards).Error; err != nil {
			logger.Errorf("查询端口访问配置失败: %v", err)
		} else {
			for _, fwd := range forwards {
				serviceName := ""
				if fwd.TargetService != nil {
					serviceName = fwd.TargetService.Name
				}
				resp.Forwards = append(resp.Forwards, &pb.ForwardConfig{
					Id:          fwd.ID,
					ServiceId:   fwd.TargetServiceID,
					ServiceName: serviceName,
					SourceAddr:  fwd.SourceAddr,
					TargetAddr:  fwd.TargetAddr,
					Enabled:     fwd.Enabled,
				})
			}
		}
	}

	return stream.Send(resp)
}

// GetRealtimeStatus 获取 Agent 实时状态
func (s *AgentServiceServer) GetRealtimeStatus(ctx context.Context, req *pb.GetRealtimeStatusRequest) (*pb.GetRealtimeStatusResponse, error) {
	logger.Infof("获取 Agent 实时状态: agent_id=%d", req.AgentId)

	// 检查 Agent 是否在线
	s.connMutex.RLock()
	conn, exists := s.connections[req.AgentId]
	s.connMutex.RUnlock()

	if !exists {
		return nil, status.Error(codes.Unavailable, "Agent 不在线")
	}

	// 从数据库获取 Agent 信息
	var agent model.Agent
	if err := db.DB.First(&agent, req.AgentId).Error; err != nil {
		return nil, status.Error(codes.NotFound, "Agent 不存在")
	}

	// 解析系统信息
	var systemInfo model.SystemInfoData
	if agent.SystemInfo != "" {
		json.Unmarshal([]byte(agent.SystemInfo), &systemInfo)
	}

	// 获取连接时间
	var connectedTime int64
	if connTime := cache.GetAgentTsConnectedAt(int64(req.AgentId)); connTime != nil {
		connectedTime = connTime.Unix()
	}

	// 构建响应
	resp := &pb.GetRealtimeStatusResponse{
		Hostname:            systemInfo.Hostname,
		Runtime:             detectRuntime(systemInfo),
		TunnelIp:            conn.TunnelIP,
		TunnelConnected:     conn.Connected,
		TunnelConnectedTime: connectedTime,
	}

	// 网络接口信息需要从 Agent 实时获取
	// 这里返回基本信息，详细信息可以通过扩展协议获取

	return resp, nil
}

// createAgentAuthKey 为 Agent 创建 Tailscale 预认证密钥
func (s *AgentServiceServer) createAgentAuthKey(ctx context.Context, agentName string, agentID uint64) (string, string, error) {
	// 为每个 Agent 创建独立的 Headscale User
	// User 命名规则: agent-{agent_name}，参见 docs/design_headscale_integration.md
	userName := fmt.Sprintf("agent-%s", agentName)

	// 获取或创建 User
	user, err := s.headscaleClient.GetOrCreateUser(ctx, userName)
	if err != nil {
		return "", "", fmt.Errorf("获取或创建 Headscale User 失败: %w", err)
	}

	// 更新 Agent 的 Headscale User ID
	if err := db.DB.Model(&model.Agent{}).Where("id = ?", agentID).Update("id", user.Id).Error; err != nil {
		logger.Warnf("更新 Agent Headscale User ID 失败: %v", err)
	}

	// 构建 Tags 列表
	// 身份 Tag: tag:agent-{agent.name}，参见 docs/design_headscale_integration.md 第 10 节
	tags := []string{fmt.Sprintf("tag:agent-%s", agentName)}

	// 查询 Agent 所属的分组，添加分组 Tag
	var groupMembers []model.AgentGroupMember
	if err := db.DB.Preload("Group").Where("agent_id = ?", agentID).Find(&groupMembers).Error; err == nil {
		for _, gm := range groupMembers {
			if gm.Group != nil {
				// 分组 Tag: tag:agent-group-{group.name}
				tags = append(tags, fmt.Sprintf("tag:agent-group-%s", gm.Group.Name))
			}
		}
	}

	// 创建预认证密钥（24 小时有效，非临时节点，带 Tags）
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

	// 检查数据库心跳
	var agent model.Agent
	if err := db.DB.First(&agent, agentID).Error; err != nil {
		return false
	}

	if agent.LastHeartbeat == nil {
		return false
	}

	return time.Since(*agent.LastHeartbeat) < 60*time.Second
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

// detectRuntime 检测运行环境
func detectRuntime(info model.SystemInfoData) string {
	// 简单检测逻辑，实际应该由 Agent 上报
	if info.Hostname == "" {
		return "unknown"
	}
	// 默认返回 physical
	return "physical"
}

// ReportProxyStatus 上报本地服务状态
func (s *AgentServiceServer) ReportProxyStatus(ctx context.Context, req *pb.ReportProxyStatusRequest) (*pb.ReportProxyStatusResponse, error) {
	logger.Infof("Agent %d 上报本地服务状态，共 %d 个服务", req.AgentId, len(req.Statuses))

	// 验证 Agent 是否在线
	if !s.IsAgentOnline(req.AgentId) {
		return &pb.ReportProxyStatusResponse{
			Success: false,
			Message: "Agent 不在线",
		}, nil
	}

	// 更新内存缓存中的服务状态
	for _, status := range req.Statuses {
		cache.UpdateProxyServiceStatus(status.ServiceId, status.Status, status.ErrorCode, status.ErrorMsg)
	}

	return &pb.ReportProxyStatusResponse{
		Success: true,
		Message: "状态已更新",
	}, nil
}

// ReportVisitorStatus 上报远程服务状态
func (s *AgentServiceServer) ReportVisitorStatus(ctx context.Context, req *pb.ReportVisitorStatusRequest) (*pb.ReportVisitorStatusResponse, error) {
	logger.Infof("Agent %d 上报远程服务状态，共 %d 个服务", req.AgentId, len(req.Statuses))

	// 验证 Agent 是否在线
	if !s.IsAgentOnline(req.AgentId) {
		return &pb.ReportVisitorStatusResponse{
			Success: false,
			Message: "Agent 不在线",
		}, nil
	}

	// 更新内存缓存中的远程服务状态
	for _, status := range req.Statuses {
		cache.UpdatePortForwardStatus(status.ForwardId, status.Status, status.ErrorCode, status.ErrorMsg)

		// 如果 IP 变化，更新数据库中的 source_addr
		if status.IpChanged && status.ActualAddr != "" {
			if err := db.DB.Model(&model.PortForward{}).
				Where("id = ? AND agent_id = ?", status.ForwardId, req.AgentId).
				Update("source_addr", status.ActualAddr).Error; err != nil {
				logger.Warnf("更新远程服务源地址失败: forward_id=%s, error=%v", status.ForwardId, err)
			}
			logger.Infof("远程服务 IP 变化: forward_id=%s, configured=%s, actual=%s, reason=%s",
				status.ForwardId, status.ConfiguredAddr, status.ActualAddr, status.ChangeReason)
		}
	}

	return &pb.ReportVisitorStatusResponse{
		Success: true,
		Message: "状态已更新",
	}, nil
}

// ReportNetworkChange 上报网络变化
func (s *AgentServiceServer) ReportNetworkChange(ctx context.Context, req *pb.ReportNetworkChangeRequest) (*pb.ReportNetworkChangeResponse, error) {
	logger.Infof("Agent %d 上报网络变化，共 %d 个网络接口", req.AgentId, len(req.Networks))

	// 验证 Agent 是否在线
	if !s.IsAgentOnline(req.AgentId) {
		return &pb.ReportNetworkChangeResponse{
			Success: false,
			Message: "Agent 不在线",
		}, nil
	}

	// 转换网络接口信息
	networks := make([]cache.NetworkInterface, len(req.Networks))
	for i, n := range req.Networks {
		networks[i] = cache.NetworkInterface{
			Name:    n.Name,
			IP:      n.Ip,
			Mask:    n.Mask,
			Gateway: n.Gateway,
		}
	}

	// 更新缓存中的网络信息
	tsStatus := cache.GetAgentTsStatus(int64(req.AgentId))
	if tsStatus != nil {
		tsStatus.Networks = networks
		// 注意：这里直接修改了指针指向的对象，不需要 SetAgentTsStatus
	} else {
		// 如果缓存不存在，创建新的
		cache.UpdateAgentNetworkInfo(int64(req.AgentId), "", "", networks)
	}

	return &pb.ReportNetworkChangeResponse{
		Success: true,
		Message: "网络信息已更新",
	}, nil
}
