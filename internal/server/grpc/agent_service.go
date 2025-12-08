package grpc

import (
	"context"
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
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

	// FRP 配置
	frpToken     string
	frpPublicURL string
	frpPort      int
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

// Register Agent注册
func (s *AgentServiceServer) Register(ctx context.Context, req *pb.RegisterRequest) (*pb.RegisterResponse, error) {
	logger.Infof("Agent注册请求: %s", req.AgentName)

	// 查询Agent
	var agent model.Agent
	if err := db.DB.Where("agent_name = ? AND agent_token = ?", req.AgentName, req.AgentToken).First(&agent).Error; err != nil {
		logger.Infof("Agent认证失败: %v", err)
		return &pb.RegisterResponse{
			Success: false,
			Message: "Agent认证失败",
		}, nil
	}

	// 更新状态为在线
	agent.Status = "online"
	now := time.Now()
	agent.LastHeartbeat = &now
	if err := db.DB.Save(&agent).Error; err != nil {
		logger.Infof("更新Agent状态失败: %v", err)
	}

	logger.Infof("Agent注册成功: %s (ID: %d)", req.AgentName, agent.ID)

	// 构建 FRP 连接信息
	// 如果配置了公网 URL，使用公网 URL；否则返回空字符串和端口
	frpServer := ""
	frpPort := int32(s.frpPort)
	if s.frpPublicURL != "" {
		frpServer = s.frpPublicURL
		frpPort = 0 // 使用完整 URL 时，端口信息已包含在 URL 中
	}

	return &pb.RegisterResponse{
		Success: true,
		Message: "注册成功",
		AgentId: agent.ID,
		Token:   s.frpToken, // 返回统一的隧道认证 Token
		Server:  frpServer,
		Port:    frpPort,
	}, nil
}

// Heartbeat Agent心跳
func (s *AgentServiceServer) Heartbeat(ctx context.Context, req *pb.HeartbeatRequest) (*pb.HeartbeatResponse, error) {
	// 验证Agent
	var agent model.Agent
	if err := db.DB.Where("id = ? AND agent_token = ?", req.AgentId, req.AgentToken).First(&agent).Error; err != nil {
		return nil, status.Error(codes.Unauthenticated, "Agent认证失败")
	}

	// 更新心跳时间
	now := time.Now()
	agent.LastHeartbeat = &now
	agent.Status = "online"
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
func (s *AgentServiceServer) IsAgentOnline(agentID int64) bool {
	s.streamsMutex.RLock()
	defer s.streamsMutex.RUnlock()
	_, exists := s.agentStreams[agentID]

	// 调试日志：显示所有在线的Agent
	if !exists {
		onlineAgents := make([]int64, 0, len(s.agentStreams))
		for id := range s.agentStreams {
			onlineAgents = append(onlineAgents, id)
		}
		logger.Debugf("Agent %d 不在线，当前在线的Agent: %v", agentID, onlineAgents)
	}

	return exists
}

// syncSTCPInstances 同步Agent的所有STCP实例
func (s *AgentServiceServer) syncSTCPInstances(agentID int64) {
	// 等待一小段时间，确保Agent完全准备好
	time.Sleep(1 * time.Second)

	// 查询该Agent的所有STCP实例
	var instances []model.STCPInstance
	if err := db.DB.Where("agent_id = ?", agentID).Find(&instances).Error; err != nil {
		logger.Infof("查询Agent STCP实例失败: %v", err)
		return
	}

	if len(instances) == 0 {
		logger.Infof("Agent %d 没有需要同步的STCP实例", agentID)
		return
	}

	logger.Infof("开始同步Agent %d 的 %d 个STCP实例", agentID, len(instances))

	// 为每个实例发送创建命令
	for _, instance := range instances {
		cmd := &pb.Command{
			CommandId:    fmt.Sprintf("sync-%d-%d", instance.ID, time.Now().Unix()),
			Type:         pb.Command_CREATE_STCP,
			InstanceName: instance.InstanceName,
			SecretKey:    instance.SecretKey,
			LocalIp:      instance.LocalIP,
			LocalPort:    int32(instance.LocalPort),
		}

		if err := s.SendCommand(agentID, cmd); err != nil {
			logger.Infof("同步STCP实例失败: instance=%s, error=%v", instance.InstanceName, err)
		} else {
			logger.Infof("已同步STCP实例: instance=%s", instance.InstanceName)
		}

		// 避免发送过快
		time.Sleep(100 * time.Millisecond)
	}

	logger.Infof("Agent %d 的STCP实例同步完成", agentID)
}

// GetEnabledTCPServices 获取Agent的已启用TCP服务列表
func (s *AgentServiceServer) GetEnabledTCPServices(ctx context.Context, req *pb.GetTCPServicesRequest) (*pb.GetTCPServicesResponse, error) {
	logger.Infof("Agent请求TCP服务列表: agent_id=%d", req.AgentId)

	// 查询该Agent的所有已启用TCP服务
	var services []model.TCPService
	if err := db.DB.Where("agent_id = ? AND enabled = ?", req.AgentId, true).Find(&services).Error; err != nil {
		logger.Errorf("查询TCP服务失败: %v", err)
		return &pb.GetTCPServicesResponse{
			Success:  false,
			Services: nil,
		}, status.Error(codes.Internal, "查询TCP服务失败")
	}

	// 转换为proto格式
	pbServices := make([]*pb.TCPServiceInfo, 0, len(services))
	for _, service := range services {
		pbServices = append(pbServices, &pb.TCPServiceInfo{
			ServiceName: service.ServiceName,
			LocalIp:     service.LocalIP,
			LocalPort:   int32(service.LocalPort),
			RemotePort:  int32(service.RemotePort),
		})
	}

	logger.Infof("返回%d个已启用的TCP服务给Agent %d", len(pbServices), req.AgentId)

	return &pb.GetTCPServicesResponse{
		Success:  true,
		Services: pbServices,
	}, nil
}
