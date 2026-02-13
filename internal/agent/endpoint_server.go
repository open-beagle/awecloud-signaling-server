package agent

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

// EndpointCapability Endpoint 单项能力信息
type EndpointCapability struct {
	Type      string // 能力类型：ssh / k8sapi / k8sservice
	Host      string // SSH 内网地址（SSH 类型时）
	Port      int32  // SSH 端口（SSH 类型时）
	APIServer string // K8S API Server 地址（K8SAPI 类型时）
}

// EndpointConnection Endpoint 连接信息
type EndpointConnection struct {
	Name         string
	Token        string
	Version      string
	Capabilities []EndpointCapability // 多能力列表
	RemoteIP     string
	LastSeen     time.Time
	Cancel       context.CancelFunc
}

// shellSession 等待中的 shell 会话（Agent 创建，等待 Endpoint 回调）
type shellSession struct {
	sessionID string
	login     string
	rows      uint32
	cols      uint32
	// Endpoint 回调 OpenShell 后，Agent 通过此 channel 获取 gRPC 流
	streamCh chan pb.EndpointService_OpenShellServer
	// 创建时间，用于超时清理
	createdAt time.Time
}

// EndpointServer Agent 内网 gRPC Server，接受 Endpoint 连接
type EndpointServer struct {
	pb.UnimplementedEndpointServiceServer

	listenPort int
	token      string // Server 下发的 endpoint_token，用于验证 Endpoint

	// 已连接的 Endpoint
	connections map[string]*EndpointConnection // key: endpoint name
	connMutex   sync.RWMutex

	// 等待中的 shell 会话（session_id → shellSession）
	shellSessions map[string]*shellSession
	shellMutex    sync.Mutex

	// 待下发的 shell 请求（endpoint name → []*ShellRequest）
	pendingShellReqs map[string][]*pb.ShellRequest
	pendingMutex     sync.Mutex

	grpcServer *net.Listener
	server     *grpc.Server
	ctx        context.Context
	cancel     context.CancelFunc
}

// NewEndpointServer 创建 Endpoint gRPC Server
func NewEndpointServer(listenPort int, token string, parentCtx context.Context) *EndpointServer {
	ctx, cancel := context.WithCancel(parentCtx)
	return &EndpointServer{
		listenPort:       listenPort,
		token:            token,
		connections:      make(map[string]*EndpointConnection),
		shellSessions:    make(map[string]*shellSession),
		pendingShellReqs: make(map[string][]*pb.ShellRequest),
		ctx:              ctx,
		cancel:           cancel,
	}
}

// Start 启动 gRPC Server
func (s *EndpointServer) Start() error {
	addr := fmt.Sprintf("0.0.0.0:%d", s.listenPort)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("监听端口 %d 失败: %w", s.listenPort, err)
	}
	s.grpcServer = &lis

	s.server = grpc.NewServer()
	pb.RegisterEndpointServiceServer(s.server, s)

	go func() {
		logger.Infof("Endpoint gRPC Server 启动: %s", addr)
		if err := s.server.Serve(lis); err != nil {
			logger.Errorf("Endpoint gRPC Server 错误: %v", err)
		}
	}()

	// 监听 context 取消，优雅关闭
	go func() {
		<-s.ctx.Done()
		s.server.GracefulStop()
		logger.Info("Endpoint gRPC Server 已关闭")
	}()

	return nil
}

// Stop 停止 gRPC Server
func (s *EndpointServer) Stop() {
	s.cancel()
	// 断开所有 Endpoint 连接
	s.connMutex.Lock()
	for _, conn := range s.connections {
		conn.Cancel()
	}
	s.connections = make(map[string]*EndpointConnection)
	s.connMutex.Unlock()
}

// UpdateToken 更新 Endpoint 令牌
func (s *EndpointServer) UpdateToken(token string) {
	s.token = token
}

// GetConnectedEndpoints 获取已连接的 Endpoint 列表
func (s *EndpointServer) GetConnectedEndpoints() []string {
	s.connMutex.RLock()
	defer s.connMutex.RUnlock()
	names := make([]string, 0, len(s.connections))
	for name := range s.connections {
		names = append(names, name)
	}
	return names
}

// GetConnectedEndpointDetails 获取已连接 Endpoint 的详细信息（供 Agent 心跳上报）
func (s *EndpointServer) GetConnectedEndpointDetails() []*EndpointConnection {
	s.connMutex.RLock()
	defer s.connMutex.RUnlock()
	result := make([]*EndpointConnection, 0, len(s.connections))
	for _, conn := range s.connections {
		result = append(result, conn)
	}
	return result
}

// Register Endpoint 注册（gRPC 实现）
func (s *EndpointServer) Register(ctx context.Context, req *pb.EndpointRegisterRequest) (*pb.EndpointRegisterResponse, error) {
	logger.Infof("Endpoint 注册请求: name=%s, version=%s", req.Name, req.Version)

	// 验证 token
	if s.token == "" {
		logger.Warnf("Endpoint 注册拒绝: Agent 未配置 endpoint_token")
		return &pb.EndpointRegisterResponse{
			Success: false,
			Message: "Agent 未启用 Endpoint 功能",
		}, nil
	}

	if req.Token != s.token {
		logger.Warnf("Endpoint 注册拒绝: token 不匹配, name=%s", req.Name)
		return &pb.EndpointRegisterResponse{
			Success: false,
			Message: "认证失败：token 无效",
		}, nil
	}

	logger.Infof("Endpoint 注册成功: name=%s, version=%s", req.Name, req.Version)
	return &pb.EndpointRegisterResponse{
		Success:    true,
		Message:    "注册成功",
		EndpointId: req.Name, // 暂用 name 作为 ID
	}, nil
}

// Heartbeat Endpoint 心跳（双向流）
func (s *EndpointServer) Heartbeat(stream pb.EndpointService_HeartbeatServer) error {
	ctx, cancel := context.WithCancel(stream.Context())
	defer cancel()

	// 接收第一个心跳
	firstReq, err := stream.Recv()
	if err != nil {
		return err
	}

	// 验证 token
	if firstReq.Token != s.token {
		logger.Warnf("Endpoint 心跳拒绝: token 不匹配, name=%s", firstReq.Name)
		return fmt.Errorf("认证失败")
	}

	name := firstReq.Name
	logger.Infof("Endpoint 心跳流建立: name=%s", name)

	// 注册连接（解析能力信息）
	conn := &EndpointConnection{
		Name:         name,
		Token:        firstReq.Token,
		Capabilities: parseCapabilities(firstReq.Capabilities),
		LastSeen:     time.Now(),
		Cancel:       cancel,
	}

	s.connMutex.Lock()
	if oldConn, exists := s.connections[name]; exists {
		oldConn.Cancel()
	}
	s.connections[name] = conn
	s.connMutex.Unlock()

	defer func() {
		s.connMutex.Lock()
		delete(s.connections, name)
		s.connMutex.Unlock()
		logger.Infof("Endpoint 心跳流断开: name=%s", name)
	}()

	// 发送首次响应
	if err := stream.Send(&pb.EndpointHeartbeatResponse{
		Success: true,
		Message: "心跳已建立",
	}); err != nil {
		return err
	}

	// 持续接收心跳
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.ctx.Done():
			return nil
		default:
			req, err := stream.Recv()
			if err != nil {
				return err
			}

			conn.LastSeen = time.Now()
			// 更新能力信息（Endpoint 可能在运行中变更配置）
			if len(req.Capabilities) > 0 {
				conn.Capabilities = parseCapabilities(req.Capabilities)
			}

			// 取出待下发的 shell 请求
			s.pendingMutex.Lock()
			shellReqs := s.pendingShellReqs[name]
			delete(s.pendingShellReqs, name)
			s.pendingMutex.Unlock()

			// 发送响应（携带 shell 请求通知）
			if err := stream.Send(&pb.EndpointHeartbeatResponse{
				Success:       true,
				ShellRequests: shellReqs,
			}); err != nil {
				return err
			}
		}
	}
}

// parseCapabilities 将 proto 能力列表转换为内部结构
func parseCapabilities(caps []*pb.EndpointCapabilityInfo) []EndpointCapability {
	result := make([]EndpointCapability, 0, len(caps))
	for _, c := range caps {
		result = append(result, EndpointCapability{
			Type:      c.Type,
			Host:      c.Host,
			Port:      c.Port,
			APIServer: c.ApiServer,
		})
	}
	return result
}

// OpenShell Endpoint 回调的 shell 会话（gRPC 双向流）
// Endpoint 收到心跳中的 ShellRequest 通知后，主动调用此 RPC
// 首包携带 session_id 和 token，Agent 根据 session_id 匹配等待中的 Desktop SSH 连接
func (s *EndpointServer) OpenShell(stream pb.EndpointService_OpenShellServer) error {
	// 接收首包（Endpoint 发送 session_id + token）
	firstMsg, err := stream.Recv()
	if err != nil {
		return fmt.Errorf("接收 OpenShell 首包失败: %w", err)
	}

	if !firstMsg.IsOpen {
		return fmt.Errorf("OpenShell 首包缺少 is_open 标志")
	}

	// 验证 token
	if firstMsg.Token != s.token {
		logger.Warnf("OpenShell 拒绝: token 不匹配, session_id=%s", firstMsg.SessionId)
		return fmt.Errorf("认证失败")
	}

	sessionID := firstMsg.SessionId
	logger.Infof("OpenShell 回调: session_id=%s", sessionID)

	// 检查首包是否携带错误（Endpoint 无法启动 shell）
	if firstMsg.IsClose && firstMsg.Error != "" {
		logger.Warnf("OpenShell 错误: session_id=%s, error=%s", sessionID, firstMsg.Error)
	}

	// 查找等待中的 session
	s.shellMutex.Lock()
	session, exists := s.shellSessions[sessionID]
	if !exists {
		s.shellMutex.Unlock()
		logger.Warnf("OpenShell 找不到 session: session_id=%s（可能已超时）", sessionID)
		return fmt.Errorf("session 不存在或已超时")
	}
	s.shellMutex.Unlock()

	// 通知等待方：Endpoint 已回调，传递 gRPC 流
	select {
	case session.streamCh <- stream:
	default:
		logger.Warnf("OpenShell session channel 已满: session_id=%s", sessionID)
		return fmt.Errorf("session channel 已满")
	}

	// 保持流存活，直到流关闭
	// 实际的 I/O 桥接由 RequestShell 的调用方（DialSocket）完成
	// 这里只需要等待流结束
	<-stream.Context().Done()
	return nil
}

// RequestShell 请求 Endpoint 开启 shell 会话
// 由 DialSocket 调用，创建 session → 通知 Endpoint → 等待回调 → 返回 gRPC 流
// 返回的 stream 可用于双向 I/O 桥接
func (s *EndpointServer) RequestShell(ctx context.Context, endpointName, login string, rows, cols uint32) (pb.EndpointService_OpenShellServer, error) {
	// 检查 Endpoint 是否在线
	s.connMutex.RLock()
	_, connected := s.connections[endpointName]
	s.connMutex.RUnlock()
	if !connected {
		return nil, fmt.Errorf("Endpoint %s 不在线", endpointName)
	}

	// 创建 session
	sessionID := uuid.New().String()
	session := &shellSession{
		sessionID: sessionID,
		login:     login,
		rows:      rows,
		cols:      cols,
		streamCh:  make(chan pb.EndpointService_OpenShellServer, 1),
		createdAt: time.Now(),
	}

	s.shellMutex.Lock()
	s.shellSessions[sessionID] = session
	s.shellMutex.Unlock()

	// 清理函数
	defer func() {
		s.shellMutex.Lock()
		delete(s.shellSessions, sessionID)
		s.shellMutex.Unlock()
	}()

	// 将 ShellRequest 加入待下发队列（下次心跳响应时携带）
	shellReq := &pb.ShellRequest{
		SessionId: sessionID,
		Login:     login,
		Rows:      rows,
		Cols:      cols,
	}

	s.pendingMutex.Lock()
	s.pendingShellReqs[endpointName] = append(s.pendingShellReqs[endpointName], shellReq)
	s.pendingMutex.Unlock()

	logger.Infof("Shell 请求已排队: session_id=%s, endpoint=%s, login=%s", sessionID, endpointName, login)

	// 等待 Endpoint 回调 OpenShell（超时 30 秒）
	select {
	case stream := <-session.streamCh:
		logger.Infof("Shell 会话已建立: session_id=%s, endpoint=%s", sessionID, endpointName)
		return stream, nil
	case <-time.After(30 * time.Second):
		return nil, fmt.Errorf("等待 Endpoint %s 回调超时（30s）", endpointName)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// IsEndpointConnected 检查指定 Endpoint 是否在线
func (s *EndpointServer) IsEndpointConnected(name string) bool {
	s.connMutex.RLock()
	defer s.connMutex.RUnlock()
	_, exists := s.connections[name]
	return exists
}
