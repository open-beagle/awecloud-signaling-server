package grpc

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

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
}

// NewAgentServiceServer 创建Agent服务
func NewAgentServiceServer() *AgentServiceServer {
	return &AgentServiceServer{
		agentStreams:  make(map[int64]pb.AgentService_ReceiveCommandsServer),
		commandQueues: make(map[int64]chan *pb.Command),
	}
}

// Register Agent注册
func (s *AgentServiceServer) Register(ctx context.Context, req *pb.RegisterRequest) (*pb.RegisterResponse, error) {
	log.Printf("Agent注册请求: %s", req.AgentName)

	// 查询Agent
	var agent model.Agent
	if err := db.DB.Where("agent_name = ? AND agent_token = ?", req.AgentName, req.AgentToken).First(&agent).Error; err != nil {
		log.Printf("Agent认证失败: %v", err)
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
		log.Printf("更新Agent状态失败: %v", err)
	}

	log.Printf("Agent注册成功: %s (ID: %d)", req.AgentName, agent.ID)

	return &pb.RegisterResponse{
		Success: true,
		Message: "注册成功",
		AgentId: agent.ID,
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
		log.Printf("更新心跳失败: %v", err)
	}

	return &pb.HeartbeatResponse{
		Success:   true,
		Timestamp: now.Unix(),
	}, nil
}

// ReceiveCommands 接收Server指令（双向流）
func (s *AgentServiceServer) ReceiveCommands(stream pb.AgentService_ReceiveCommandsServer) error {
	// 等待第一个消息（包含agent_id）
	_, err := stream.Recv()
	if err != nil {
		return status.Error(codes.InvalidArgument, "无法接收初始消息")
	}

	// 从响应中获取agent_id（这里简化处理，实际应该在metadata中传递）
	// TODO: 改进认证机制
	log.Printf("Agent连接建立，等待指令...")

	// 这里需要从context或第一个消息中获取agent_id
	// 暂时使用一个临时方案
	agentID := int64(1) // TODO: 从认证信息中获取

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

	defer func() {
		// 清理连接
		s.streamsMutex.Lock()
		delete(s.agentStreams, agentID)
		s.streamsMutex.Unlock()

		log.Printf("Agent连接断开: %d", agentID)
	}()

	// 启动接收响应的goroutine
	go func() {
		for {
			resp, err := stream.Recv()
			if err != nil {
				return
			}
			log.Printf("收到Agent响应: command_id=%s, success=%v", resp.CommandId, resp.Success)
		}
	}()

	// 发送命令
	for {
		select {
		case cmd := <-cmdQueue:
			if err := stream.Send(cmd); err != nil {
				log.Printf("发送命令失败: %v", err)
				return err
			}
			log.Printf("发送命令: %s", cmd.CommandId)

		case <-stream.Context().Done():
			return stream.Context().Err()
		}
	}
}

// ReportStatus 状态上报
func (s *AgentServiceServer) ReportStatus(ctx context.Context, req *pb.StatusReport) (*pb.StatusResponse, error) {
	log.Printf("收到Agent状态上报: agent_id=%d, stcp_count=%d", req.AgentId, len(req.StcpStatuses))

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
	return exists
}
