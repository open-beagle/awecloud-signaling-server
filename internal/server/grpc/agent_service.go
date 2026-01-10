package grpc

import (
	"context"
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/headscale"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

// AgentServiceServer Agent服务实现
type AgentServiceServer struct {
	pb.UnimplementedAgentServiceServer

	// Agent连接管理
	agentStreams map[int64]pb.AgentService_ReceiveCommandsServer
	streamsMutex sync.RWMutex

	// 命令队列
	commandQueues map[int64]chan *pb.Command
	queuesMutex   sync.RWMutex

	// FRP 配置（废弃，保留兼容）
	frpToken     string
	frpPublicURL string
	frpPort      int

	// Tailscale 配置
	headscaleClient *headscale.Client
	config          *config.ServerConfig
}

// NewAgentServiceServer 创建Agent服务
func NewAgentServiceServer(frpToken string, frpPublicURL string, frpPort int) *AgentServiceServer {
	return &AgentServiceServer{
		agentStreams:  make(map[int64]pb.AgentService_ReceiveCommandsServer),
		commandQueues: make(map[int64]chan *pb.Command),
		frpToken:      frpToken,
		frpPublicURL:  frpPublicURL,
		frpPort:       frpPort,
	}
}

// NewAgentServiceServerWithConfig 创建Agent服务（带配置）
func NewAgentServiceServerWithConfig(cfg *config.ServerConfig) *AgentServiceServer {
	s := &AgentServiceServer{
		agentStreams:  make(map[int64]pb.AgentService_ReceiveCommandsServer),
		commandQueues: make(map[int64]chan *pb.Command),
		frpToken:      cfg.Server.Token,
		frpPublicURL:  cfg.Server.PublicURL,
		frpPort:       cfg.Server.FRPServerPort,
		config:        cfg,
	}

	// 初始化 Headscale 客户端
	if cfg.Tailscale.HeadscaleURL != "" && cfg.Tailscale.HeadscaleAPIKey != "" {
		s.headscaleClient = headscale.NewClient(headscale.Config{
			URL:    cfg.Tailscale.HeadscaleURL,
			APIKey: cfg.Tailscale.HeadscaleAPIKey,
		})
		logger.Infof("Headscale 客户端已初始化: %s", cfg.Tailscale.HeadscaleURL)
	} else {
		logger.Warnf("Headscale 配置不完整，Tailscale 功能将不可用")
	}

	return s
}

// Register Agent注册
func (s *AgentServiceServer) Register(ctx context.Context, req *pb.RegisterRequest) (*pb.RegisterResponse, error) {
	logger.Infof("Agent注册请求: %s, version=%s", req.AgentName, req.Version)

	// 查询Agent
	var agent model.Agent
	if err := db.DB.Where("agent_name = ? AND agent_token = ?", req.AgentName, req.AgentToken).First(&agent).Error; err != nil {
		logger.Infof("Agent认证失败: %v", err)
		return &pb.RegisterResponse{
			Success: false,
			Message: "Agent认证失败",
		}, nil
	}

	// 更新状态为在线，同时更新版本
	agent.Status = "online"
	now := time.Now()
	agent.LastHeartbeat = &now
	if req.Version != "" {
		agent.Version = req.Version
	}
	if err := db.DB.Save(&agent).Error; err != nil {
		logger.Infof("更新Agent状态失败: %v", err)
	}

	logger.Infof("Agent注册成功: %s (ID: %d, Version: %s)", req.AgentName, agent.ID, agent.Version)

	// 构建响应
	resp := &pb.RegisterResponse{
		Success: true,
		Message: "注册成功",
		AgentId: agent.ID,
	}

	// Tailscale 模式：创建预认证密钥
	if s.headscaleClient != nil && s.config != nil {
		// 从数据库获取配置（或使用默认值）
		authKeyExpiry := 24 * time.Hour // 默认 24 小时

		// 为每个 Agent 创建独立的 Headscale User
		// User 命名规则：agent-{agent_name}
		userName := fmt.Sprintf("agent-%s", req.AgentName)

		// 获取或创建 User
		user, err := s.headscaleClient.GetOrCreateUser(ctx, userName)
		if err != nil {
			logger.Errorf("获取或创建 Headscale User 失败: %v", err)
			// 不影响注册，继续返回成功
		} else {
			// 创建预认证密钥（非临时节点，Agent 需要持久化）
			authKey, err := s.headscaleClient.CreatePreAuthKey(ctx, user.Name, authKeyExpiry, false)
			if err != nil {
				logger.Errorf("创建 Tailscale 预认证密钥失败: %v", err)
				// 不影响注册，继续返回成功
			} else {
				resp.ControlUrl = s.config.Tailscale.HeadscaleURL
				resp.AuthKey = authKey.Key
				// DERP URL 从数据库配置获取，这里使用默认值
				resp.DerpUrl = s.config.Tailscale.HeadscaleURL + "/derp"
				logger.Infof("已为 Agent %s 创建 Tailscale 预认证密钥（User: %s）", req.AgentName, userName)
			}
		}
	} else {
		// FRP 模式（废弃，保留兼容）
		if s.frpPublicURL != "" {
			resp.Server = s.frpPublicURL
			resp.Port = 0 // 使用完整 URL 时，端口信息已包含在 URL 中
		}
		resp.Token = s.frpToken
	}

	return resp, nil
}

// Heartbeat Agent心跳
func (s *AgentServiceServer) Heartbeat(ctx context.Context, req *pb.HeartbeatRequest) (*pb.HeartbeatResponse, error) {
	// 验证Agent
	var agent model.Agent
	if err := db.DB.Where("id = ? AND agent_token = ?", req.AgentId, req.AgentToken).First(&agent).Error; err != nil {
		return nil, status.Error(codes.Unauthenticated, "Agent认证失败")
	}

	// 更新心跳时间和版本
	now := time.Now()
	agent.LastHeartbeat = &now
	agent.Status = "online"
	if req.Version != "" {
		agent.Version = req.Version
	}

	// 更新 Tailscale 状态
	if req.TailscaleIp != "" {
		agent.TailscaleIP = req.TailscaleIp
	}
	agent.TsConnected = req.TsConnected
	if req.TsConnType != "" {
		agent.TsConnType = req.TsConnType
	}
	// 首次连接时记录注册时间
	if req.TsConnected && agent.TsRegisteredAt == nil {
		agent.TsRegisteredAt = &now
	}

	if err := db.DB.Save(&agent).Error; err != nil {
		logger.Infof("更新心跳失败: %v", err)
	}

	return &pb.HeartbeatResponse{
		Success:   true,
		Timestamp: now.Unix(),
	}, nil
}

// ReceiveCommands 接收Server指令（双向流）
func (s *AgentServiceServer) ReceiveCommands(stream pb.AgentService_ReceiveCommandsServer) error {
	// 等待第一个消息（包含agent_id）
	initResp, err := stream.Recv()
	if err != nil {
		return status.Error(codes.InvalidArgument, "无法接收初始消息")
	}

	// 从初始响应中获取agent_id
	// CommandId格式："init-{agent_id}"
	var agentID int64
	if _, err := fmt.Sscanf(initResp.CommandId, "init-%d", &agentID); err != nil {
		logger.Infof("解析Agent ID失败: %v, 使用默认值1", err)
		agentID = 1
	}

	logger.Infof("Agent连接建立: agent_id=%d, message=%s", agentID, initResp.Message)

	// 注册stream
	s.streamsMutex.Lock()
	s.agentStreams[agentID] = stream
	s.streamsMutex.Unlock()

	// 创建命令队列
	s.queuesMutex.Lock()
	if s.commandQueues[agentID] == nil {
		s.commandQueues[agentID] = make(chan *pb.Command, 100)
	}
	cmdQueue := s.commandQueues[agentID]
	s.queuesMutex.Unlock()

	// 同步该Agent的所有STCP实例
	go s.syncSTCPInstances(agentID)

	defer func() {
		// 清理连接
		s.streamsMutex.Lock()
		delete(s.agentStreams, agentID)
		s.streamsMutex.Unlock()

		logger.Infof("Agent连接断开: %d", agentID)
	}()

	// 启动接收响应的goroutine
	go func() {
		for {
			resp, err := stream.Recv()
			if err != nil {
				return
			}
			logger.Infof("收到Agent响应: command_id=%s, success=%v", resp.CommandId, resp.Success)
		}
	}()

	// 发送命令
	for {
		select {
		case cmd := <-cmdQueue:
			if err := stream.Send(cmd); err != nil {
				logger.Infof("发送命令失败: %v", err)
				return err
			}
			logger.Infof("发送命令: %s", cmd.CommandId)

		case <-stream.Context().Done():
			return stream.Context().Err()
		}
	}
}

// ReportStatus 状态上报
func (s *AgentServiceServer) ReportStatus(ctx context.Context, req *pb.StatusReport) (*pb.StatusResponse, error) {
	logger.Infof("收到Agent状态上报: agent_id=%d, stcp_count=%d", req.AgentId, len(req.StcpStatuses))

	// TODO: 保存状态信息到数据库或缓存

	return &pb.StatusResponse{
		Success: true,
	}, nil
}

// SendCommand 发送命令给Agent（供内部调用）
func (s *AgentServiceServer) SendCommand(agentID int64, cmd *pb.Command) error {
	s.queuesMutex.RLock()
	cmdQueue, exists := s.commandQueues[agentID]
	s.queuesMutex.RUnlock()

	if !exists {
		return fmt.Errorf("Agent未连接: %d", agentID)
	}

	select {
	case cmdQueue <- cmd:
		return nil
	case <-time.After(5 * time.Second):
		return fmt.Errorf("发送命令超时")
	}
}

// IsAgentOnline 检查Agent是否在线
// 优先检查 gRPC 流连接，如果流不存在则检查数据库心跳时间
func (s *AgentServiceServer) IsAgentOnline(agentID int64) bool {
	// 首先检查是否有活跃的 gRPC 流连接
	s.streamsMutex.RLock()
	_, streamExists := s.agentStreams[agentID]
	s.streamsMutex.RUnlock()

	if streamExists {
		return true
	}

	// 如果没有流连接，检查数据库中的心跳时间
	// 如果最近 60 秒内有心跳，认为 Agent 在线
	var agent model.Agent
	if err := db.DB.First(&agent, agentID).Error; err != nil {
		logger.Debugf("Agent %d 不存在: %v", agentID, err)
		return false
	}

	if agent.LastHeartbeat == nil {
		logger.Debugf("Agent %d 从未发送过心跳", agentID)
		return false
	}

	// 检查心跳是否在 60 秒内
	heartbeatAge := time.Since(*agent.LastHeartbeat)
	isOnline := heartbeatAge < 60*time.Second

	if !isOnline {
		logger.Debugf("Agent %d 心跳超时: 最后心跳 %v 前", agentID, heartbeatAge)
	}

	return isOnline
}

// syncSTCPInstances 同步Agent的所有STCP实例（废弃，保留兼容）
func (s *AgentServiceServer) syncSTCPInstances(agentID int64) {
	// 废弃：STCP 已被 Tailscale 端口映射替代
	// 改为同步 ProxyService
	s.SyncProxies(agentID)
}

// GetEnabledTCPServices 获取Agent的已启用TCP服务列表（废弃，保留兼容）
func (s *AgentServiceServer) GetEnabledTCPServices(ctx context.Context, req *pb.GetTCPServicesRequest) (*pb.GetTCPServicesResponse, error) {
	logger.Infof("Agent请求TCP服务列表: agent_id=%d (废弃接口)", req.AgentId)

	// 废弃：TCP 服务已被 Tailscale 端口映射替代
	return &pb.GetTCPServicesResponse{
		Success:  true,
		Services: nil,
	}, nil
}

// GetEnabledSTCPVisitors 获取Agent的已启用STCP访问列表（废弃，保留兼容）
func (s *AgentServiceServer) GetEnabledSTCPVisitors(ctx context.Context, req *pb.GetSTCPVisitorsRequest) (*pb.GetSTCPVisitorsResponse, error) {
	logger.Infof("Agent请求STCP访问列表: agent_name=%s (废弃接口)", req.AgentName)

	// 废弃：STCP 访问已被 Tailscale 端口映射替代
	return &pb.GetSTCPVisitorsResponse{
		Success:  true,
		Visitors: nil,
	}, nil
}

// ============================================
// Tailscale 相关方法
// ============================================

// ReportTailscaleStatus 上报 Tailscale 状态
func (s *AgentServiceServer) ReportTailscaleStatus(ctx context.Context, req *pb.TailscaleStatusReport) (*pb.StatusResponse, error) {
	logger.Infof("收到 Tailscale 状态上报: agent_id=%d, ip=%s, connected=%v, conn_type=%s",
		req.AgentId, req.TailscaleIp, req.Connected, req.ConnType)

	// 更新 Agent 的 Tailscale 状态
	var agent model.Agent
	if err := db.DB.First(&agent, req.AgentId).Error; err != nil {
		logger.Errorf("Agent 不存在: %d", req.AgentId)
		return &pb.StatusResponse{Success: false}, nil
	}

	agent.TailscaleIP = req.TailscaleIp
	agent.TsConnected = req.Connected
	agent.TsConnType = req.ConnType

	if req.Connected && agent.TsRegisteredAt == nil {
		now := time.Now()
		agent.TsRegisteredAt = &now
	}

	if err := db.DB.Save(&agent).Error; err != nil {
		logger.Errorf("更新 Agent Tailscale 状态失败: %v", err)
		return &pb.StatusResponse{Success: false}, nil
	}

	return &pb.StatusResponse{Success: true}, nil
}

// ReportProxyStatus 上报端口映射状态
func (s *AgentServiceServer) ReportProxyStatus(ctx context.Context, req *pb.ProxyStatusReport) (*pb.StatusResponse, error) {
	logger.Infof("收到端口映射状态上报: agent_id=%d, proxy_count=%d", req.AgentId, len(req.Proxies))

	// 更新每个代理服务的状态
	for _, proxy := range req.Proxies {
		var service model.ProxyService
		if err := db.DB.Where("agent_id = ? AND name = ?", req.AgentId, proxy.Name).First(&service).Error; err != nil {
			logger.Debugf("代理服务不存在: agent_id=%d, name=%s", req.AgentId, proxy.Name)
			continue
		}

		service.Status = proxy.Status
		service.Connections = int(proxy.Connections)
		service.BytesIn = proxy.BytesIn
		service.BytesOut = proxy.BytesOut

		if err := db.DB.Save(&service).Error; err != nil {
			logger.Errorf("更新代理服务状态失败: %v", err)
		}
	}

	return &pb.StatusResponse{Success: true}, nil
}

// SyncProxies 同步 Agent 的所有端口映射服务
func (s *AgentServiceServer) SyncProxies(agentID int64) {
	// 等待一小段时间，确保 Agent 完全准备好
	time.Sleep(1 * time.Second)

	// 查询该 Agent 的所有端口映射服务
	var services []model.ProxyService
	if err := db.DB.Where("agent_id = ?", agentID).Find(&services).Error; err != nil {
		logger.Errorf("查询 Agent 端口映射服务失败: %v", err)
		return
	}

	if len(services) == 0 {
		logger.Infof("Agent %d 没有需要同步的端口映射服务", agentID)
		return
	}

	logger.Infof("开始同步 Agent %d 的 %d 个端口映射服务", agentID, len(services))

	// 为每个服务发送启动命令
	for _, service := range services {
		// 只同步状态为 running 的服务
		if service.Status != model.ProxyStatusRunning {
			continue
		}

		cmd := &pb.Command{
			CommandId: fmt.Sprintf("sync-proxy-%d-%d", service.ID, time.Now().Unix()),
			Type:      pb.Command_START_PROXY,
			ProxyCommand: &pb.ProxyCommand{
				Name:       service.Name,
				ListenPort: int32(service.ListenPort),
				TargetAddr: service.TargetAddr,
			},
		}

		if err := s.SendCommand(agentID, cmd); err != nil {
			logger.Errorf("同步端口映射服务失败: name=%s, error=%v", service.Name, err)
		} else {
			logger.Infof("已同步端口映射服务: name=%s", service.Name)
		}

		// 避免发送过快
		time.Sleep(100 * time.Millisecond)
	}

	logger.Infof("Agent %d 的端口映射服务同步完成", agentID)
}

// SendProxyCommand 发送端口映射指令
func (s *AgentServiceServer) SendProxyCommand(agentID int64, action string, service *model.ProxyService) error {
	var cmdType pb.Command_Type
	switch action {
	case "start":
		cmdType = pb.Command_START_PROXY
	case "stop":
		cmdType = pb.Command_STOP_PROXY
	default:
		return fmt.Errorf("未知的操作类型: %s", action)
	}

	cmd := &pb.Command{
		CommandId: fmt.Sprintf("proxy-%s-%d-%d", action, service.ID, time.Now().Unix()),
		Type:      cmdType,
		ProxyCommand: &pb.ProxyCommand{
			Name:       service.Name,
			ListenPort: int32(service.ListenPort),
			TargetAddr: service.TargetAddr,
		},
	}

	return s.SendCommand(agentID, cmd)
}

// GetTailscaleState 获取 Agent 的 Tailscale 状态
func (s *AgentServiceServer) GetTailscaleState(ctx context.Context, req *pb.GetStateRequest) (*pb.GetStateResponse, error) {
	logger.Infof("Agent 请求获取 Tailscale 状态: agent_id=%d", req.AgentId)

	// 验证 Agent 认证
	var agent model.Agent
	if err := db.DB.Where("id = ? AND agent_token = ?", req.AgentId, req.AgentToken).First(&agent).Error; err != nil {
		logger.Warnf("Agent 认证失败: agent_id=%d", req.AgentId)
		return nil, status.Error(codes.Unauthenticated, "Agent 认证失败")
	}

	// 查询状态数据
	var state model.AgentTailscaleState
	err := db.DB.Where("agent_id = ?", req.AgentId).First(&state).Error
	if err != nil {
		// 状态不存在，返回空状态
		logger.Infof("Agent %d 没有历史 Tailscale 状态", req.AgentId)
		return &pb.GetStateResponse{
			StateData: nil,
			Exists:    false,
		}, nil
	}

	logger.Infof("Agent %d 获取 Tailscale 状态成功，数据大小: %d bytes", req.AgentId, len(state.StateData))
	return &pb.GetStateResponse{
		StateData: state.StateData,
		Exists:    true,
	}, nil
}

// SaveTailscaleState 保存 Agent 的 Tailscale 状态
func (s *AgentServiceServer) SaveTailscaleState(ctx context.Context, req *pb.SaveStateRequest) (*pb.SaveStateResponse, error) {
	logger.Infof("Agent 请求保存 Tailscale 状态: agent_id=%d, data_size=%d bytes", req.AgentId, len(req.StateData))

	// 验证 Agent 认证
	var agent model.Agent
	if err := db.DB.Where("id = ? AND agent_token = ?", req.AgentId, req.AgentToken).First(&agent).Error; err != nil {
		logger.Warnf("Agent 认证失败: agent_id=%d", req.AgentId)
		return nil, status.Error(codes.Unauthenticated, "Agent 认证失败")
	}

	// 查询是否已存在状态记录
	var state model.AgentTailscaleState
	err := db.DB.Where("agent_id = ?", req.AgentId).First(&state).Error

	if err != nil {
		// 不存在，创建新记录
		state = model.AgentTailscaleState{
			AgentID:   req.AgentId,
			StateData: req.StateData,
		}
		if err := db.DB.Create(&state).Error; err != nil {
			logger.Errorf("创建 Tailscale 状态失败: %v", err)
			return &pb.SaveStateResponse{Success: false}, nil
		}
		logger.Infof("Agent %d 创建 Tailscale 状态成功", req.AgentId)
	} else {
		// 已存在，更新记录
		state.StateData = req.StateData
		if err := db.DB.Save(&state).Error; err != nil {
			logger.Errorf("更新 Tailscale 状态失败: %v", err)
			return &pb.SaveStateResponse{Success: false}, nil
		}
		logger.Infof("Agent %d 更新 Tailscale 状态成功", req.AgentId)
	}

	return &pb.SaveStateResponse{Success: true}, nil
}
