// Package grpc 提供 gRPC 服务实现
package grpc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	v1 "github.com/juanfont/headscale/gen/go/headscale/v1"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/cache"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/headscale"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/service"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

// DesktopConnection Desktop 连接信息
type DesktopConnection struct {
	NodeID         uint64
	UserID         uint64
	Stream         pb.DesktopService_HeartbeatServer
	TunnelIP       string
	Connected      bool
	LastSeen       time.Time
	Cancel         context.CancelFunc
	HeartbeatCount int // 心跳计数器，用于定期检查用户状态
}

// DesktopDataStream Desktop 数据流连接信息
type DesktopDataStream struct {
	NodeID uint64
	UserID uint64
	Stream pb.DesktopService_DataStreamServer
	Cancel context.CancelFunc
}

// DesktopServiceServer Desktop 服务实现
type DesktopServiceServer struct {
	pb.UnimplementedDesktopServiceServer
	connections       map[uint64]*DesktopConnection
	connMutex         sync.RWMutex
	dataStreams       map[uint64]*DesktopDataStream // 数据流连接（key: nodeID）
	dataStreamMutex   sync.RWMutex
	headscaleClient   *headscale.Client
	config            *config.ServerConfig
	agentService      *AgentServiceServer
	loginService      *service.DesktopLoginService
	runtimeStore      *cache.NodeRuntimeStore
	runtimePersister  *cache.NodeRuntimePersister
	snapshotRefresher *headscale.SnapshotRefresher
	dataAssembler     *service.DesktopDataAssembler
	updateService     *service.UpdateService
}

type headscaleDeviceIPIndex struct {
	byID   map[uint64]string
	byName map[string]*v1.Node
}

// NewDesktopServiceServer 创建 Desktop 服务
func NewDesktopServiceServer(cfg *config.ServerConfig) *DesktopServiceServer {
	s := &DesktopServiceServer{
		connections: make(map[uint64]*DesktopConnection),
		dataStreams: make(map[uint64]*DesktopDataStream),
		config:      cfg,
	}
	if cfg.Tailscale.HeadscaleURL != "" && cfg.Tailscale.HeadscaleAPIKey != "" {
		client, err := headscale.NewClient(headscale.Config{
			URL:    cfg.Tailscale.HeadscaleURL,
			APIKey: cfg.Tailscale.HeadscaleAPIKey,
		})
		if err != nil {
			logger.Errorf("初始化 Desktop 服务 Headscale 客户端失败: %v", err)
		} else {
			s.headscaleClient = client
		}
	}
	return s
}

func (s *DesktopServiceServer) SetUpdateService(updateService *service.UpdateService) {
	s.updateService = updateService
}

func (s *DesktopServiceServer) SetRuntimeStore(store *cache.NodeRuntimeStore) {
	s.runtimeStore = store
}

func (s *DesktopServiceServer) SetRuntimePersister(persister *cache.NodeRuntimePersister) {
	s.runtimePersister = persister
}

func (s *DesktopServiceServer) SetSnapshotRefresher(refresher *headscale.SnapshotRefresher) {
	s.snapshotRefresher = refresher
}

func (s *DesktopServiceServer) SetDataAssembler(assembler *service.DesktopDataAssembler) {
	s.dataAssembler = assembler
}

func buildHeadscaleDeviceIPIndex(nodes []*v1.Node) headscaleDeviceIPIndex {
	index := headscaleDeviceIPIndex{
		byID:   make(map[uint64]string),
		byName: make(map[string]*v1.Node),
	}
	for _, node := range nodes {
		if node == nil || len(node.IpAddresses) == 0 {
			continue
		}
		index.byID[node.Id] = node.IpAddresses[0]
		selected := index.byName[node.GivenName]
		if selected == nil || (node.Online && !selected.Online) || (node.Online == selected.Online && node.Id > selected.Id) {
			index.byName[node.GivenName] = node
		}
	}
	return index
}

func resolveDeviceIP(node model.Node, index headscaleDeviceIPIndex) string {
	if node.HeadscaleNodeID != 0 {
		if ip := index.byID[node.HeadscaleNodeID]; ip != "" {
			return ip
		}
	}
	if hsNode := index.byName[node.Name]; hsNode != nil && len(hsNode.IpAddresses) > 0 {
		return hsNode.IpAddresses[0]
	}
	return node.IP
}

// SetAgentService 设置 Agent 服务
func (s *DesktopServiceServer) SetAgentService(agentService *AgentServiceServer) {
	s.agentService = agentService
}

// SetLoginService 设置 Desktop 登录服务
func (s *DesktopServiceServer) SetLoginService(loginService *service.DesktopLoginService) {
	s.loginService = loginService
}

// CreateLoginSession 创建登录会话，返回 session_id 和 login_url
func (s *DesktopServiceServer) CreateLoginSession(ctx context.Context, req *pb.CreateLoginSessionRequest) (*pb.CreateLoginSessionResponse, error) {
	logger.Infof("创建登录会话: device_name=%s, username_hint=%s", req.DeviceName, req.UsernameHint)

	// 检查 Logto 是否已配置
	if s.loginService == nil || !s.loginService.IsLogtoConfigured() {
		return &pb.CreateLoginSessionResponse{
			Success: false,
			Message: "Logto 未配置",
		}, nil
	}

	// 创建登录会话
	session, _, err := s.loginService.CreateLoginSession(req.DeviceFingerprint, req.DeviceName, req.UsernameHint)
	if err != nil {
		logger.Errorf("创建登录会话失败: %v", err)
		return &pb.CreateLoginSessionResponse{
			Success: false,
			Message: "创建登录会话失败",
		}, nil
	}

	// 注册登录结果通道（WaitForLoginResult 会用到）
	s.loginService.RegisterLoginSession(session.SessionID)

	// 返回相对路径，Desktop 端拼接 server 地址
	loginURL := "/auth/desktop/" + session.SessionID

	logger.Infof("登录会话创建成功: sessionID=%s, loginURL=%s", session.SessionID, loginURL)

	return &pb.CreateLoginSessionResponse{
		Success:   true,
		Message:   "登录会话创建成功",
		SessionId: session.SessionID,
		LoginUrl:  loginURL,
	}, nil
}

// createOrGetDesktopNode 创建或获取 Desktop 节点
func (s *DesktopServiceServer) createOrGetDesktopNode(ctx context.Context, userID uint64, userName, deviceName string, systemInfo *pb.DesktopSystemInfo) (*model.Node, string, error) {
	var node model.Node
	err := db.DB.WithContext(ctx).Where("user_id = ? AND type = ? AND name = ?", userID, model.NodeTypeDesktop, deviceName).First(&node).Error
	isNewDevice := err != nil

	nodeSecret := generateDesktopSecret()
	secretHash, _ := bcrypt.GenerateFromPassword([]byte(nodeSecret), bcrypt.DefaultCost)

	if isNewDevice {
		var systemInfoJSON string
		if systemInfo != nil {
			si := model.NodeSystemInfo{
				OS: systemInfo.Os, OSVersion: systemInfo.OsVersion, Arch: systemInfo.Arch,
				Hostname: systemInfo.Hostname, CPU: systemInfo.Cpu,
				CPUCores: int(systemInfo.CpuCores), MemoryGB: int(systemInfo.MemoryGb),
			}
			if data, err := json.Marshal(si); err == nil {
				systemInfoJSON = string(data)
			}
		}
		now := time.Now()
		node = model.Node{
			UserID: userID, Name: deviceName, Type: model.NodeTypeDesktop,
			SecretHash: string(secretHash), SystemInfo: systemInfoJSON, LastHeartbeat: &now,
		}
		if systemInfo != nil {
			node.Hostname = systemInfo.Hostname
		}
		if err := db.DB.WithContext(ctx).Create(&node).Error; err != nil {
			return nil, "", err
		}
	} else {
		node.SecretHash = string(secretHash)
		now := time.Now()
		node.LastHeartbeat = &now
		if systemInfo != nil {
			si := model.NodeSystemInfo{
				OS: systemInfo.Os, OSVersion: systemInfo.OsVersion, Arch: systemInfo.Arch,
				Hostname: systemInfo.Hostname, CPU: systemInfo.Cpu,
				CPUCores: int(systemInfo.CpuCores), MemoryGB: int(systemInfo.MemoryGb),
			}
			if data, err := json.Marshal(si); err == nil {
				node.SystemInfo = string(data)
			}
			node.Hostname = systemInfo.Hostname
		}
		db.DB.WithContext(ctx).Save(&node)
	}

	return &node, nodeSecret, nil
}

// Authenticate Desktop 认证
func (s *DesktopServiceServer) Authenticate(ctx context.Context, req *pb.DesktopAuthenticateRequest) (*pb.DesktopAuthenticateResponse, error) {
	var node model.Node
	if err := db.DB.WithContext(ctx).First(&node, req.DesktopId).Error; err != nil {
		return &pb.DesktopAuthenticateResponse{Success: false, Message: "设备不存在"}, nil
	}
	if node.Type != model.NodeTypeDesktop {
		return &pb.DesktopAuthenticateResponse{Success: false, Message: "设备类型错误"}, nil
	}
	if err := bcrypt.CompareHashAndPassword([]byte(node.SecretHash), []byte(req.Secret)); err != nil {
		return &pb.DesktopAuthenticateResponse{Success: false, Message: "认证失败"}, nil
	}

	now := time.Now()
	node.LastHeartbeat = &now
	if req.SystemInfo != nil {
		si := model.NodeSystemInfo{
			OS: req.SystemInfo.Os, OSVersion: req.SystemInfo.OsVersion, Arch: req.SystemInfo.Arch,
			Hostname: req.SystemInfo.Hostname, CPU: req.SystemInfo.Cpu,
			CPUCores: int(req.SystemInfo.CpuCores), MemoryGB: int(req.SystemInfo.MemoryGb),
		}
		if data, err := json.Marshal(si); err == nil {
			node.SystemInfo = string(data)
		}
		node.Hostname = req.SystemInfo.Hostname
	}
	db.DB.WithContext(ctx).Save(&node)

	var user model.User
	if err := db.DB.WithContext(ctx).First(&user, node.UserID).Error; err != nil {
		return &pb.DesktopAuthenticateResponse{Success: false, Message: "用户不存在"}, nil
	}

	// 检查用户是否启用
	if !user.Enabled {
		logger.Warnf("Desktop 认证失败: 用户已禁用, desktopId=%d, userId=%d, userName=%s", req.DesktopId, user.ID, user.Name)
		return &pb.DesktopAuthenticateResponse{Success: false, Message: "您的账号已被禁用"}, nil
	}

	resp := &pb.DesktopAuthenticateResponse{Success: true, Message: "认证成功"}
	if s.headscaleClient != nil && s.config != nil {
		if authKey, serverURL, err := s.getOrCreateAuthKey(ctx, user.ID, user.Name); err == nil {
			resp.AuthKey = authKey
			resp.ServerUrl = serverURL
		}
	}
	return resp, nil
}

// Heartbeat Desktop 心跳（双向流）
func (s *DesktopServiceServer) Heartbeat(stream pb.DesktopService_HeartbeatServer) error {
	ctx, cancel := context.WithCancel(stream.Context())
	defer cancel()

	firstReq, err := stream.Recv()
	if err != nil {
		return status.Error(codes.InvalidArgument, "无法接收初始消息")
	}

	nodeID := firstReq.DesktopId
	logger.Infof("Desktop 心跳流建立: desktopId=%d, tunnelIp=%s", nodeID, firstReq.TunnelIp)

	var node model.Node
	if err := db.DB.WithContext(ctx).First(&node, nodeID).Error; err != nil {
		logger.Errorf("Desktop 心跳流建立失败: desktopId=%d 不存在", nodeID)
		return status.Error(codes.NotFound, "Desktop 不存在")
	}
	if node.Type != model.NodeTypeDesktop {
		logger.Errorf("Desktop 心跳流建立失败: desktopId=%d 类型错误 (type=%s)", nodeID, node.Type)
		return status.Error(codes.InvalidArgument, "设备类型错误")
	}

	logger.Infof("Desktop 心跳流验证通过: desktopId=%d, name=%s, userId=%d", nodeID, node.Name, node.UserID)

	// 检查用户是否启用
	var user model.User
	if err := db.DB.WithContext(ctx).First(&user, node.UserID).Error; err != nil {
		logger.Errorf("Desktop 心跳流建立失败: 用户不存在, userId=%d", node.UserID)
		return status.Error(codes.NotFound, "用户不存在")
	}
	if !user.Enabled {
		logger.Warnf("Desktop 心跳流建立失败: 用户已禁用, desktopId=%d, userId=%d, userName=%s", nodeID, user.ID, user.Name)
		return status.Error(codes.PermissionDenied, "用户已禁用")
	}

	conn := &DesktopConnection{
		NodeID: nodeID, UserID: node.UserID, Stream: stream,
		TunnelIP: firstReq.TunnelIp, Connected: firstReq.TunnelConnected,
		LastSeen: time.Now(), Cancel: cancel,
	}

	s.connMutex.Lock()
	if oldConn, exists := s.connections[nodeID]; exists {
		oldConn.Cancel()
	}
	s.connections[nodeID] = conn
	s.connMutex.Unlock()

	defer func() {
		s.connMutex.Lock()
		delete(s.connections, nodeID)
		s.connMutex.Unlock()
		// 清除数据库中的心跳时间和 IP，确保离线状态正确
		db.DB.Model(&model.Node{}).Where("id = ?", nodeID).Updates(map[string]any{"last_heartbeat": nil, "ip": ""})
		logger.Infof("Desktop %d gRPC 连接断开，已清除心跳时间和 IP", nodeID)
	}()

	s.handleDesktopHeartbeat(ctx, nodeID, firstReq)
	if err := s.sendDesktopHeartbeatResponse(ctx, stream, node.UserID); err != nil {
		return err
	}

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
			conn.TunnelIP = req.TunnelIp
			conn.Connected = req.TunnelConnected
			conn.LastSeen = time.Now()

			// 定期检查用户状态（每 10 次心跳检查一次，约 5 分钟）
			if conn.HeartbeatCount%10 == 0 {
				var user model.User
				if err := db.DB.WithContext(ctx).First(&user, node.UserID).Error; err == nil {
					if !user.Enabled {
						logger.Warnf("Desktop 心跳检测到用户已禁用，断开连接: desktopId=%d, userId=%d, userName=%s", nodeID, user.ID, user.Name)
						// 清空心跳时间和 IP
						db.DB.Model(&model.Node{}).Where("id = ?", nodeID).Updates(map[string]any{"last_heartbeat": nil, "ip": ""})
						return status.Error(codes.PermissionDenied, "用户已禁用")
					}
				}
			}
			conn.HeartbeatCount++

			s.handleDesktopHeartbeat(ctx, nodeID, req)
			if err := s.sendDesktopHeartbeatResponse(ctx, stream, node.UserID); err != nil {
				return err
			}
		}
	}
}

func (s *DesktopServiceServer) handleDesktopHeartbeat(ctx context.Context, nodeID uint64, req *pb.DesktopHeartbeatRequest) {
	now := time.Now()

	if s.runtimeStore != nil {
		if _, err := s.runtimeStore.UpdateHeartbeat(nodeID, req.TunnelIp, "", "", "", "", "", "", "", now); err != nil {
			logger.Warnf("Desktop 心跳 RuntimeStore 更新失败: nodeID=%d, err=%v", nodeID, err)
		}
	} else {
		// 兜底：未初始化 runtimeStore 时落库
		var node model.Node
		if err := db.DB.WithContext(ctx).First(&node, nodeID).Error; err == nil {
			db.DB.WithContext(ctx).Model(&model.Node{}).
				Where("user_id = ? AND type = ? AND name = ?", node.UserID, model.NodeTypeDesktop, node.Name).
				Updates(map[string]any{"last_heartbeat": now, "ip": req.TunnelIp})
		}
	}

	if s.snapshotRefresher != nil && s.runtimeStore != nil {
		if rn, ok := s.runtimeStore.GetNode(nodeID); ok {
			var user model.User
			if err := db.DB.WithContext(ctx).First(&user, rn.UserID).Error; err == nil {
				hsUserName := fmt.Sprintf("client-%s", user.Name)
				tunnelIP := strings.TrimSpace(req.TunnelIp)
				snapshot := s.snapshotRefresher.LoadSnapshot()

				if req.TunnelConnected && tunnelIP != "" {
					if hsView, found := snapshot.GetByIP(tunnelIP); found && hsView.User == hsUserName {
						if s.runtimeStore.UpdateHeadscaleNodeID(nodeID, hsView.ID) && s.runtimePersister != nil {
							s.runtimePersister.NotifyHighPriority()
						}
					}
				} else if rn.HeadscaleNodeID == 0 {
					if hsView, found := snapshot.GetByUserNameAndNodeName(hsUserName, rn.Name); found {
						if s.runtimeStore.UpdateHeadscaleNodeID(nodeID, hsView.ID) && s.runtimePersister != nil {
							s.runtimePersister.NotifyHighPriority()
						}
					}
				}
			}
		}
	}
}

func validDesktopHeadscaleNode(node *v1.Node, expectedUser, tunnelIP string) bool {
	if node == nil || node.Id == 0 || node.User == nil || node.User.Name != expectedUser {
		return false
	}
	for _, nodeIP := range node.IpAddresses {
		if nodeIP == tunnelIP {
			return true
		}
	}
	return false
}

func (s *DesktopServiceServer) sendDesktopHeartbeatResponse(ctx context.Context, stream pb.DesktopService_HeartbeatServer, userID uint64) error {
	// 纯心跳确认，不携带业务数据
	return stream.Send(&pb.DesktopHeartbeatResponse{})
}

func (s *DesktopServiceServer) getOrCreateAuthKey(ctx context.Context, userID uint64, userName string) (string, string, error) {
	hsUserName := fmt.Sprintf("client-%s", userName)
	user, err := s.headscaleClient.GetUserByName(ctx, hsUserName)
	if err != nil {
		return "", "", fmt.Errorf("查询 Headscale User 失败: %w", err)
	}
	if user == nil {
		user, err = s.headscaleClient.CreateUser(ctx, hsUserName)
		if err != nil {
			return "", "", fmt.Errorf("创建 Headscale User 失败: %w", err)
		}
	}

	// 注意：不再删除旧节点，保持节点稳定性
	// Desktop 使用持久化状态，重连时应复用现有节点

	tags := []string{fmt.Sprintf("tag:client-%s", userName)}
	var groupMembers []model.GroupMember
	if err := db.DB.WithContext(ctx).Preload("Group").Where("user_id = ?", userID).Find(&groupMembers).Error; err == nil {
		for _, gm := range groupMembers {
			if gm.Group != nil {
				tags = append(tags, fmt.Sprintf("tag:group-%s", gm.Group.Name))
			}
		}
	}
	// ephemeral=false：保持节点稳定，不自动删除
	authKey, err := s.headscaleClient.CreatePreAuthKeyWithTags(ctx, user.Id, 24*time.Hour, false, tags)
	if err != nil {
		return "", "", fmt.Errorf("创建预认证密钥失败: %w", err)
	}
	return authKey.Key, s.config.Tailscale.HeadscalePublicURL, nil
}

func (s *DesktopServiceServer) IsDesktopOnline(nodeID uint64) bool {
	// 只检查内存中的连接状态，不再检查数据库
	// 因为 gRPC 断开时会清除数据库的 last_heartbeat
	s.connMutex.RLock()
	conn, exists := s.connections[nodeID]
	s.connMutex.RUnlock()
	return exists && time.Since(conn.LastSeen) < 60*time.Second
}

// DisconnectDesktop 断开指定 Desktop 的连接
func (s *DesktopServiceServer) DisconnectDesktop(nodeID uint64) {
	// 断开心跳连接
	s.connMutex.Lock()
	if conn, exists := s.connections[nodeID]; exists {
		conn.Cancel()
		delete(s.connections, nodeID)
		logger.Infof("已断开 Desktop 心跳连接: nodeId=%d", nodeID)
	}
	s.connMutex.Unlock()

	// 断开数据流连接
	s.dataStreamMutex.Lock()
	if ds, exists := s.dataStreams[nodeID]; exists {
		ds.Cancel()
		delete(s.dataStreams, nodeID)
		logger.Infof("已断开 Desktop 数据流连接: nodeId=%d", nodeID)
	}
	s.dataStreamMutex.Unlock()
}

// GetAuthorizedHosts 获取已授权主机列表（SSH 授权）
func (s *DesktopServiceServer) GetAuthorizedHosts(ctx context.Context, req *pb.GetAuthorizedHostsRequest) (*pb.GetAuthorizedHostsResponse, error) {
	logger.Infof("GetAuthorizedHosts: desktopId=%d", req.DesktopId)

	// 验证 Desktop 是否存在
	var node model.Node
	if err := db.DB.WithContext(ctx).First(&node, req.DesktopId).Error; err != nil {
		logger.Warnf("GetAuthorizedHosts: Desktop %d 不存在", req.DesktopId)
		return nil, status.Error(codes.NotFound, "Desktop 不存在")
	}
	if node.Type != model.NodeTypeDesktop {
		logger.Warnf("GetAuthorizedHosts: 设备 %d 类型错误 (type=%s)", req.DesktopId, node.Type)
		return nil, status.Error(codes.InvalidArgument, "设备类型错误")
	}

	// 获取用户信息
	var user model.User
	if err := db.DB.WithContext(ctx).First(&user, node.UserID).Error; err != nil {
		logger.Warnf("GetAuthorizedHosts: 用户 %d 不存在", node.UserID)
		return nil, status.Error(codes.NotFound, "用户不存在")
	}
	logger.Infof("GetAuthorizedHosts: user=%s (id=%d)", user.Name, user.ID)

	// 获取用户所属的分组 ID 列表
	var groupIDs []int64
	db.DB.WithContext(ctx).Model(&model.GroupMember{}).Where("user_id = ?", user.ID).Pluck("group_id", &groupIDs)
	logger.Infof("GetAuthorizedHosts: user %s 所属分组: %v", user.Name, groupIDs)

	// 收集已授权的 Agent 及其 SSH 用户（按 Agent 分组）
	// key: agentID, value: SSH 用户名列表
	authorizedAgents := make(map[uint64][]string)

	// 1. 通过 SSH 用户授权（AclSSHUserPermission）
	var sshUserPerms []model.AclSSHUserPermission
	if err := db.DB.WithContext(ctx).Where("user_id = ? AND enabled = ?", user.ID, true).Find(&sshUserPerms).Error; err == nil {
		logger.Infof("GetAuthorizedHosts: 找到 %d 个 SSH 用户授权", len(sshUserPerms))
		for _, perm := range sshUserPerms {
			// 解析 SSH 用户名列表
			var sshUsers []string
			if err := json.Unmarshal([]byte(perm.SSHUsers), &sshUsers); err == nil {
				authorizedAgents[perm.TargetUserID] = appendUniqueStrings(authorizedAgents[perm.TargetUserID], sshUsers...)
			}
		}
	}

	// 2. 通过 SSH 分组授权（AclSSHGroupPermission）
	if len(groupIDs) > 0 {
		var sshGroupPerms []model.AclSSHGroupPermission
		if err := db.DB.WithContext(ctx).Where("group_id IN ? AND enabled = ?", groupIDs, true).Find(&sshGroupPerms).Error; err == nil {
			logger.Infof("GetAuthorizedHosts: 找到 %d 个 SSH 分组授权", len(sshGroupPerms))
			for _, perm := range sshGroupPerms {
				// 解析 SSH 用户名列表
				var sshUsers []string
				if err := json.Unmarshal([]byte(perm.SSHUsers), &sshUsers); err == nil {
					authorizedAgents[perm.TargetUserID] = appendUniqueStrings(authorizedAgents[perm.TargetUserID], sshUsers...)
				}
			}
		}
	}

	logger.Infof("GetAuthorizedHosts: 共找到 %d 个已授权 SSH 主机", len(authorizedAgents))

	// 查询所有已授权 Agent 的信息
	resp := &pb.GetAuthorizedHostsResponse{
		Hosts: make([]*pb.AuthorizedHost, 0, len(authorizedAgents)),
	}

	for agentID, sshUsers := range authorizedAgents {
		// 查询 Agent 用户信息
		var agentUser model.User
		if err := db.DB.WithContext(ctx).First(&agentUser, agentID).Error; err != nil {
			continue
		}
		if agentUser.Role != model.UserRoleAgent {
			continue
		}

		// 查询 Agent 的 SSH 域名，获取实际可用的 SSH 用户列表
		var sshDomain model.DomainRegistry
		availableUsers := []string{} // Agent 实际可用的 SSH 用户
		if err := db.DB.WithContext(ctx).
			Where("user_id = ? AND type = ? AND status = ?", agentID, model.DomainTypeSSH, model.DomainStatusOnline).
			First(&sshDomain).Error; err == nil {
			// 解析 SSH 用户列表
			availableUsers = sshDomain.GetSSHUsers()
		}

		// 求交集：ACL 授权的用户 ∩ Agent 实际可用的用户
		authorizedUsers := intersectStrings(sshUsers, availableUsers)
		if len(authorizedUsers) == 0 {
			// 如果交集为空，跳过这个 Agent（没有可用的 SSH 用户）
			logger.Debugf("Agent %d: ACL 授权用户 %v 与实际可用用户 %v 无交集，跳过", agentID, sshUsers, availableUsers)
			continue
		}

		// 检查 Agent 是否在线
		agentOnline := false
		if s.agentService != nil {
			agentOnline = s.agentService.IsAgentOnline(agentID)
		}

		// 查询 Agent 节点信息
		var agentNode model.Node
		tunnelIP := ""
		lastSeen := ""
		if err := db.DB.WithContext(ctx).Where("user_id = ? AND type = ?", agentID, model.NodeTypeAgent).First(&agentNode).Error; err == nil {
			tunnelIP = agentNode.IP
			if agentNode.LastHeartbeat != nil {
				lastSeen = agentNode.LastHeartbeat.Format(time.RFC3339)
			}
		}

		// 主机名格式：用户名.设备名
		hostName := agentUser.Name
		if agentNode.Name != "" {
			hostName = fmt.Sprintf("%s.%s", agentUser.Name, agentNode.Name)
		}

		host := &pb.AuthorizedHost{
			HostId:   fmt.Sprintf("%d", agentID),
			HostName: hostName,
			TunnelIp: tunnelIP,
			SshUsers: authorizedUsers, // 使用交集后的用户列表
			Status:   "offline",
			LastSeen: lastSeen,
		}
		if agentOnline {
			host.Status = "online"
		}
		resp.Hosts = append(resp.Hosts, host)
	}

	logger.Infof("Desktop %d 获取已授权主机列表: %d 个主机", req.DesktopId, len(resp.Hosts))
	return resp, nil
}

// appendUniqueStrings 追加不重复的字符串
func appendUniqueStrings(slice []string, items ...string) []string {
	existing := make(map[string]bool)
	for _, s := range slice {
		existing[s] = true
	}
	for _, item := range items {
		if !existing[item] {
			slice = append(slice, item)
			existing[item] = true
		}
	}
	return slice
}

// intersectStrings 计算两个字符串切片的交集
func intersectStrings(a, b []string) []string {
	if len(a) == 0 || len(b) == 0 {
		return []string{}
	}

	// 将 b 转换为 map 以提高查找效率
	bMap := make(map[string]bool)
	for _, s := range b {
		bMap[s] = true
	}

	// 查找交集
	result := []string{}
	seen := make(map[string]bool)
	for _, s := range a {
		if bMap[s] && !seen[s] {
			result = append(result, s)
			seen[s] = true
		}
	}

	return result
}

// contains 检查字符串切片中是否包含指定字符串
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// GetHostServices 获取指定主机的服务列表
func (s *DesktopServiceServer) GetHostServices(ctx context.Context, req *pb.GetHostServicesRequest) (*pb.GetHostServicesResponse, error) {
	// 验证 Desktop 是否存在
	var node model.Node
	if err := db.DB.WithContext(ctx).First(&node, req.DesktopId).Error; err != nil {
		return nil, status.Error(codes.NotFound, "Desktop 不存在")
	}
	if node.Type != model.NodeTypeDesktop {
		return nil, status.Error(codes.InvalidArgument, "设备类型错误")
	}

	// 解析主机 ID
	var hostUserID uint64
	if _, err := fmt.Sscanf(req.HostId, "%d", &hostUserID); err != nil {
		return nil, status.Error(codes.InvalidArgument, "主机ID格式错误")
	}

	// 查询该主机的所有服务
	var services []model.ProxyService
	if err := db.DB.WithContext(ctx).Preload("User").Where("user_id = ? AND enabled = ?", hostUserID, true).Find(&services).Error; err != nil {
		return nil, status.Error(codes.Internal, "查询服务失败")
	}

	// 检查 Agent 是否在线
	agentOnline := false
	if s.agentService != nil {
		agentOnline = s.agentService.IsAgentOnline(hostUserID)
	}

	// 转换为响应格式
	resp := &pb.GetHostServicesResponse{
		Services: make([]*pb.AuthorizedService, 0, len(services)),
	}
	for _, svc := range services {
		agentName := ""
		if svc.User != nil {
			agentName = svc.User.Name
		}

		// 服务状态取决于 Agent 状态
		status := "offline"
		if agentOnline {
			status = "online"
		}

		resp.Services = append(resp.Services, &pb.AuthorizedService{
			Id:         svc.ID,
			Name:       svc.Name,
			AgentName:  agentName,
			ListenAddr: svc.SourceAddr,
			TargetAddr: svc.TargetAddr,
		})
		_ = status // 暂时未使用，后续可以添加到 AuthorizedService 消息中
	}

	logger.Infof("Desktop %d 获取主机 %s 的服务列表: %d 个服务", req.DesktopId, req.HostId, len(resp.Services))
	return resp, nil
}

// GetMyDevices 获取我的设备列表
func (s *DesktopServiceServer) GetMyDevices(ctx context.Context, req *pb.GetMyDevicesRequest) (*pb.GetMyDevicesResponse, error) {
	// 验证 Desktop 是否存在
	var currentNode model.Node
	if err := db.DB.WithContext(ctx).First(&currentNode, req.DesktopId).Error; err != nil {
		return nil, status.Error(codes.NotFound, "Desktop 不存在")
	}
	if currentNode.Type != model.NodeTypeDesktop {
		return nil, status.Error(codes.InvalidArgument, "设备类型错误")
	}

	// 查询该用户的所有 Desktop 设备
	var nodes []model.Node
	if err := db.DB.WithContext(ctx).Where("user_id = ? AND type = ?", currentNode.UserID, model.NodeTypeDesktop).Find(&nodes).Error; err != nil {
		return nil, status.Error(codes.Internal, "查询设备失败")
	}

	// 从 Headscale 获取该用户的所有节点 IP（实时数据）
	nodeIPIndex := headscaleDeviceIPIndex{}
	if s.headscaleClient != nil {
		var user model.User
		if err := db.DB.WithContext(ctx).First(&user, currentNode.UserID).Error; err == nil {
			hsUserName := fmt.Sprintf("client-%s", user.Name)
			hsNodes, err := s.headscaleClient.ListNodesByUser(ctx, hsUserName)
			if err == nil {
				nodeIPIndex = buildHeadscaleDeviceIPIndex(hsNodes)
			}
		}
	}

	// 转换为响应格式
	resp := &pb.GetMyDevicesResponse{
		Devices: make([]*pb.DeviceInfo, 0, len(nodes)),
	}
	for _, node := range nodes {
		// 解析系统信息
		var sysInfo model.NodeSystemInfo
		os := "未知"
		arch := "未知"
		hostname := node.Hostname
		if node.SystemInfo != "" {
			if err := json.Unmarshal([]byte(node.SystemInfo), &sysInfo); err == nil {
				os = sysInfo.OS
				if sysInfo.OSVersion != "" {
					os = sysInfo.OSVersion
				}
				arch = sysInfo.Arch
				if sysInfo.Hostname != "" {
					hostname = sysInfo.Hostname
				}
			}
		}

		// 判断设备状态
		deviceStatus := "offline"
		if node.LastHeartbeat != nil && time.Since(*node.LastHeartbeat) < 60*time.Second {
			deviceStatus = "online"
		}

		// 格式化时间
		lastUsedAt := ""
		if node.LastHeartbeat != nil {
			lastUsedAt = node.LastHeartbeat.Format(time.RFC3339)
		}
		createdAt := node.CreatedAt.Format(time.RFC3339)

		ip := resolveDeviceIP(node, nodeIPIndex)

		resp.Devices = append(resp.Devices, &pb.DeviceInfo{
			DeviceToken: fmt.Sprintf("%d:%s", node.ID, "***"), // 不返回真实 secret
			DeviceName:  node.Name,
			Os:          os,
			Arch:        arch,
			Hostname:    hostname,
			Status:      deviceStatus,
			LastUsedAt:  lastUsedAt,
			CreatedAt:   createdAt,
			IsCurrent:   node.ID == req.DesktopId,
			Ip:          ip,
		})
	}

	logger.Infof("Desktop %d 获取设备列表: %d 个设备", req.DesktopId, len(resp.Devices))
	return resp, nil
}

// OfflineDevice 设备下线
func (s *DesktopServiceServer) OfflineDevice(ctx context.Context, req *pb.OfflineDeviceRequest) (*pb.OfflineDeviceResponse, error) {
	// 设备管理是安全敏感写操作，不能只信任调用方提交的 Desktop ID。
	var currentNode model.Node
	if err := db.DB.WithContext(ctx).First(&currentNode, req.DesktopId).Error; err != nil {
		return &pb.OfflineDeviceResponse{Success: false, Message: "当前设备认证失败"}, nil
	}
	if currentNode.Type != model.NodeTypeDesktop || bcrypt.CompareHashAndPassword([]byte(currentNode.SecretHash), []byte(req.Secret)) != nil {
		return &pb.OfflineDeviceResponse{Success: false, Message: "当前设备认证失败"}, nil
	}

	// 解析目标设备 ID
	var targetNodeID uint64
	if _, err := fmt.Sscanf(req.DeviceToken, "%d:", &targetNodeID); err != nil {
		return &pb.OfflineDeviceResponse{Success: false, Message: "设备令牌格式错误"}, nil
	}

	// 验证目标设备是否存在且属于同一用户
	var targetNode model.Node
	if err := db.DB.WithContext(ctx).First(&targetNode, targetNodeID).Error; err != nil {
		return &pb.OfflineDeviceResponse{Success: false, Message: "目标设备不存在"}, nil
	}
	if targetNode.UserID != currentNode.UserID {
		return &pb.OfflineDeviceResponse{Success: false, Message: "无权操作该设备"}, nil
	}
	if targetNode.ID == currentNode.ID {
		return &pb.OfflineDeviceResponse{Success: false, Message: "不能下线当前设备"}, nil
	}

	// 关闭该设备的连接
	s.connMutex.Lock()
	if conn, exists := s.connections[targetNodeID]; exists {
		conn.Cancel()
		delete(s.connections, targetNodeID)
	}
	s.connMutex.Unlock()

	// 更新数据库：清除心跳时间
	if err := db.DB.WithContext(ctx).Model(&targetNode).Update("last_heartbeat", nil).Error; err != nil {
		return &pb.OfflineDeviceResponse{Success: false, Message: "下线失败"}, nil
	}

	logger.Infof("Desktop %d 下线设备 %d", req.DesktopId, targetNodeID)
	return &pb.OfflineDeviceResponse{Success: true, Message: "设备已下线"}, nil
}

// DeleteDevice 删除设备
func (s *DesktopServiceServer) DeleteDevice(ctx context.Context, req *pb.DeleteDeviceRequest) (*pb.DeleteDeviceResponse, error) {
	// 设备管理是安全敏感写操作，不能只信任调用方提交的 Desktop ID。
	var currentNode model.Node
	if err := db.DB.WithContext(ctx).First(&currentNode, req.DesktopId).Error; err != nil {
		return &pb.DeleteDeviceResponse{Success: false, Message: "当前设备认证失败"}, nil
	}
	if currentNode.Type != model.NodeTypeDesktop || bcrypt.CompareHashAndPassword([]byte(currentNode.SecretHash), []byte(req.Secret)) != nil {
		return &pb.DeleteDeviceResponse{Success: false, Message: "当前设备认证失败"}, nil
	}

	// 解析目标设备 ID
	var targetNodeID uint64
	if _, err := fmt.Sscanf(req.DeviceToken, "%d:", &targetNodeID); err != nil {
		return &pb.DeleteDeviceResponse{Success: false, Message: "设备令牌格式错误"}, nil
	}

	// 验证目标设备是否存在且属于同一用户
	var targetNode model.Node
	if err := db.DB.WithContext(ctx).First(&targetNode, targetNodeID).Error; err != nil {
		return &pb.DeleteDeviceResponse{Success: false, Message: "目标设备不存在"}, nil
	}
	if targetNode.UserID != currentNode.UserID {
		return &pb.DeleteDeviceResponse{Success: false, Message: "无权操作该设备"}, nil
	}
	if targetNode.ID == currentNode.ID {
		return &pb.DeleteDeviceResponse{Success: false, Message: "不能删除当前设备"}, nil
	}

	// 关闭该设备的连接
	s.connMutex.Lock()
	if conn, exists := s.connections[targetNodeID]; exists {
		conn.Cancel()
		delete(s.connections, targetNodeID)
	}
	s.connMutex.Unlock()

	// 删除数据库记录
	if err := db.DB.WithContext(ctx).Delete(&targetNode).Error; err != nil {
		return &pb.DeleteDeviceResponse{Success: false, Message: "删除失败"}, nil
	}

	logger.Infof("Desktop %d 删除设备 %d", req.DesktopId, targetNodeID)
	return &pb.DeleteDeviceResponse{Success: true, Message: "设备已删除"}, nil
}

func generateDesktopSecret() string {
	bytes := make([]byte, 32)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// CheckSavedCredentials 检查保存的凭据
func (s *DesktopServiceServer) CheckSavedCredentials(ctx context.Context, req *pb.CheckSavedCredentialsRequest) (*pb.CheckSavedCredentialsResponse, error) {
	logger.Infof("检查保存的凭据: username=%s", req.Username)

	// 查询用户是否存在
	var user model.User
	if err := db.DB.WithContext(ctx).Where("name = ? AND role = ?", req.Username, model.UserRoleClient).First(&user).Error; err != nil {
		// 用户不存在
		return &pb.CheckSavedCredentialsResponse{
			HasCredentials: false,
		}, nil
	}

	// 查询该用户的 Desktop 节点（任意一个即可）
	var node model.Node
	if err := db.DB.WithContext(ctx).Where("user_id = ? AND type = ?", user.ID, model.NodeTypeDesktop).First(&node).Error; err != nil {
		// 没有 Desktop 节点
		return &pb.CheckSavedCredentialsResponse{
			HasCredentials: false,
		}, nil
	}

	// 返回用户信息和 Desktop ID
	// 注意：不返回 secret，由前端本地存储
	return &pb.CheckSavedCredentialsResponse{
		HasCredentials: true,
		Username:       user.Name,
		DesktopId:      node.ID,
	}, nil
}

// ToggleFavorite 切换服务收藏状态
func (s *DesktopServiceServer) ToggleFavorite(ctx context.Context, req *pb.ToggleFavoriteRequest) (*pb.ToggleFavoriteResponse, error) {
	// 验证 Desktop 是否存在
	var node model.Node
	if err := db.DB.WithContext(ctx).First(&node, req.DesktopId).Error; err != nil {
		return &pb.ToggleFavoriteResponse{Success: false, Message: "设备不存在"}, nil
	}

	// 解析服务 ID (格式: "agent_user_id:service_id")
	var agentUserID, serviceID uint64
	if _, err := fmt.Sscanf(req.ServiceId, "%d:%d", &agentUserID, &serviceID); err != nil {
		return &pb.ToggleFavoriteResponse{Success: false, Message: "服务 ID 格式错误"}, nil
	}

	// 验证服务是否存在
	var service model.ProxyService
	if err := db.DB.WithContext(ctx).Where("user_id = ? AND id = ?", agentUserID, serviceID).First(&service).Error; err != nil {
		return &pb.ToggleFavoriteResponse{Success: false, Message: "服务不存在"}, nil
	}

	// 查询是否已收藏
	var favorite model.ServiceFavorite
	err := db.DB.WithContext(ctx).Where("client_id = ? AND stcp_instance_id = ?", node.UserID, service.ID).First(&favorite).Error

	if err == nil {
		// 已收藏，取消收藏
		if err := db.DB.WithContext(ctx).Delete(&favorite).Error; err != nil {
			return &pb.ToggleFavoriteResponse{Success: false, Message: "取消收藏失败"}, nil
		}
		logger.Infof("Desktop %d 取消收藏服务 %s", req.DesktopId, req.ServiceId)
		// 推送收藏列表变更
		go s.NotifyDesktopDataChange(req.DesktopId, pb.DesktopDataType_DESKTOP_DATA_TYPE_FAVORITES)
		return &pb.ToggleFavoriteResponse{Success: true, Message: "已取消收藏", IsFavorite: false}, nil
	} else {
		// 未收藏，添加收藏
		favorite = model.ServiceFavorite{
			ClientID:       int64(node.UserID),
			STCPInstanceID: service.ID,
		}
		if err := db.DB.WithContext(ctx).Create(&favorite).Error; err != nil {
			return &pb.ToggleFavoriteResponse{Success: false, Message: "添加收藏失败"}, nil
		}
		logger.Infof("Desktop %d 收藏服务 %s", req.DesktopId, req.ServiceId)
		// 推送收藏列表变更
		go s.NotifyDesktopDataChange(req.DesktopId, pb.DesktopDataType_DESKTOP_DATA_TYPE_FAVORITES)
		return &pb.ToggleFavoriteResponse{Success: true, Message: "已添加收藏", IsFavorite: true}, nil
	}
}

// GetFavoriteServices 获取收藏的服务列表
func (s *DesktopServiceServer) GetFavoriteServices(ctx context.Context, req *pb.GetFavoriteServicesRequest) (*pb.GetFavoriteServicesResponse, error) {
	// 验证 Desktop 是否存在
	var node model.Node
	if err := db.DB.WithContext(ctx).First(&node, req.DesktopId).Error; err != nil {
		return &pb.GetFavoriteServicesResponse{}, nil
	}

	// 查询收藏的服务
	var favorites []model.ServiceFavorite
	if err := db.DB.WithContext(ctx).Where("client_id = ?", node.UserID).Find(&favorites).Error; err != nil {
		return &pb.GetFavoriteServicesResponse{}, nil
	}

	// 构建服务 ID 列表
	serviceIDs := make([]string, 0, len(favorites))
	for _, fav := range favorites {
		// 查询服务的 user_id
		var service model.ProxyService
		if err := db.DB.WithContext(ctx).Select("user_id").First(&service, fav.STCPInstanceID).Error; err == nil {
			serviceID := fmt.Sprintf("%d:%s", service.UserID, fav.STCPInstanceID)
			serviceIDs = append(serviceIDs, serviceID)
		}
	}

	return &pb.GetFavoriteServicesResponse{ServiceIds: serviceIDs}, nil
}

// WaitForLoginResult 等待登录结果（gRPC 双向流）
// Desktop 通过此方法等待登录完成，Server 会在用户完成 Logto 登录后推送结果
func (s *DesktopServiceServer) WaitForLoginResult(stream pb.DesktopService_WaitForLoginResultServer) error {
	ctx := stream.Context()

	// 接收第一条请求消息
	req, err := stream.Recv()
	if err != nil {
		logger.Errorf("WaitForLoginResult 接收请求失败: %v", err)
		return status.Error(codes.InvalidArgument, "无法接收请求")
	}

	sessionID := req.SessionId
	deviceFingerprint := req.DeviceFingerprint

	logger.Infof("WaitForLoginResult 流建立: sessionId=%s, deviceFingerprint=%s", sessionID, deviceFingerprint)

	// 检查登录会话是否存在
	var session model.DesktopLoginSession
	if err := db.DB.WithContext(ctx).Where("session_id = ?", sessionID).First(&session).Error; err != nil {
		logger.Errorf("登录会话不存在: sessionId=%s", sessionID)
		return stream.Send(&pb.WaitForLoginResultResponse{
			Status:  pb.WaitForLoginResultStatus_WAIT_FOR_LOGIN_RESULT_STATUS_FAILED,
			Message: "登录会话不存在",
		})
	}

	// 检查会话是否已过期
	if session.IsExpired() {
		logger.Warnf("登录会话已过期: sessionId=%s", sessionID)
		return stream.Send(&pb.WaitForLoginResultResponse{
			Status:  pb.WaitForLoginResultStatus_WAIT_FOR_LOGIN_RESULT_STATUS_TIMEOUT,
			Message: "登录会话已过期",
		})
	}

	// 获取已注册的登录结果通道
	// 通道应该已经在 GetLoginURL() 中注册了
	resultCh := s.loginService.GetLoginResultChannel(sessionID)
	if resultCh == nil {
		logger.Errorf("登录结果通道不存在: sessionId=%s", sessionID)
		return stream.Send(&pb.WaitForLoginResultResponse{
			Status:  pb.WaitForLoginResultStatus_WAIT_FOR_LOGIN_RESULT_STATUS_FAILED,
			Message: "登录会话不存在",
		})
	}

	// 创建超时上下文（5 分钟）
	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	logger.Infof("等待登录结果: sessionId=%s", sessionID)

	// 等待登录结果或超时
	select {
	case <-timeoutCtx.Done():
		logger.Warnf("登录超时: sessionId=%s", sessionID)
		return stream.Send(&pb.WaitForLoginResultResponse{
			Status:  pb.WaitForLoginResultStatus_WAIT_FOR_LOGIN_RESULT_STATUS_TIMEOUT,
			Message: "登录超时，请重试",
		})

	case result, ok := <-resultCh:
		if !ok {
			logger.Errorf("登录结果通道已关闭: sessionId=%s", sessionID)
			return stream.Send(&pb.WaitForLoginResultResponse{
				Status:  pb.WaitForLoginResultStatus_WAIT_FOR_LOGIN_RESULT_STATUS_FAILED,
				Message: "登录会话已关闭",
			})
		}

		if !result.Success {
			// 区分禁用/待审批和普通失败
			resultStatus := pb.WaitForLoginResultStatus_WAIT_FOR_LOGIN_RESULT_STATUS_FAILED
			if result.IsDisabled {
				resultStatus = pb.WaitForLoginResultStatus_WAIT_FOR_LOGIN_RESULT_STATUS_DISABLED
			}
			logger.Warnf("登录失败: sessionId=%s, error=%s, disabled=%v", sessionID, result.ErrorMessage, result.IsDisabled)
			return stream.Send(&pb.WaitForLoginResultResponse{
				Status:  resultStatus,
				Message: result.ErrorMessage,
			})
		}

		// 登录成功，生成 Desktop 凭证
		logger.Infof("登录成功，生成凭证: sessionId=%s, userId=%d, username=%s", sessionID, result.UserID, result.UserName)

		// 使用 session 中保存的主机名作为设备名（由 Desktop 端在 CreateLoginSession 时传入）
		deviceName := session.DeviceName
		if deviceName == "" {
			deviceName = fmt.Sprintf("desktop-%d", result.UserID)
		}
		node, nodeSecret, err := s.createOrGetDesktopNode(ctx, result.UserID, result.UserName, deviceName, nil)
		if err != nil {
			logger.Errorf("创建 Desktop 节点失败: %v", err)
			return stream.Send(&pb.WaitForLoginResultResponse{
				Status:  pb.WaitForLoginResultStatus_WAIT_FOR_LOGIN_RESULT_STATUS_FAILED,
				Message: "生成凭证失败",
			})
		}

		// 获取 Headscale AuthKey
		var authKey string
		serverURL := ""
		if s.headscaleClient != nil && s.config != nil {
			if key, url, err := s.getOrCreateAuthKey(ctx, result.UserID, result.UserName); err == nil {
				authKey = key
				serverURL = url
			} else {
				logger.Warnf("获取 AuthKey 失败: %v", err)
			}
		}
		if serverURL == "" {
			serverURL = s.config.Tailscale.HeadscalePublicURL
			if serverURL == "" {
				serverURL = s.config.Tailscale.HeadscaleURL
			}
		}

		logger.Infof("推送登录成功结果: sessionId=%s, desktopId=%d", sessionID, node.ID)

		// 清理会话（在返回结果后）
		defer s.loginService.UnregisterLoginSession(sessionID)

		return stream.Send(&pb.WaitForLoginResultResponse{
			Status:      pb.WaitForLoginResultStatus_WAIT_FOR_LOGIN_RESULT_STATUS_SUCCESS,
			Message:     "登录成功",
			DesktopId:   node.ID,
			DeviceToken: nodeSecret,
			AuthKey:     authKey,
			ServerUrl:   serverURL,
			Username:    result.UserName,
		})
	}
}

// Logout Desktop 注销（安全离场）
func (s *DesktopServiceServer) Logout(ctx context.Context, req *pb.DesktopLogoutRequest) (*pb.DesktopLogoutResponse, error) {
	logger.Infof("Desktop 注销请求: desktopId=%d", req.DesktopId)

	// 验证 Desktop 是否存在
	var node model.Node
	if err := db.DB.WithContext(ctx).First(&node, req.DesktopId).Error; err != nil {
		return &pb.DesktopLogoutResponse{Success: false, Message: "设备不存在"}, nil
	}
	if node.Type != model.NodeTypeDesktop {
		return &pb.DesktopLogoutResponse{Success: false, Message: "设备类型错误"}, nil
	}

	// 关闭该设备的心跳连接
	s.connMutex.Lock()
	if conn, exists := s.connections[req.DesktopId]; exists {
		conn.Cancel()
		delete(s.connections, req.DesktopId)
	}
	s.connMutex.Unlock()

	// 关闭该设备的数据流连接
	s.dataStreamMutex.Lock()
	if ds, exists := s.dataStreams[req.DesktopId]; exists {
		ds.Cancel()
		delete(s.dataStreams, req.DesktopId)
	}
	s.dataStreamMutex.Unlock()

	// 清除数据库中的心跳时间
	db.DB.WithContext(ctx).Model(&model.Node{}).Where("id = ?", req.DesktopId).Update("last_heartbeat", nil)

	// 注销 Logto 上游会话（尽力而为，失败不阻塞）
	var logoutURL string
	if s.loginService != nil {
		logoutURL = s.loginService.LogoutSession(node.UserID)
	}

	logger.Infof("Desktop %d 注销成功, logoutURL=%s", req.DesktopId, logoutURL)
	return &pb.DesktopLogoutResponse{Success: true, Message: "注销成功", LogoutUrl: logoutURL}, nil
}

// maskToken 隐藏 token 中间部分，用于日志
func maskToken(token string) string {
	if token == "" {
		return "<empty>"
	}
	if len(token) <= 10 {
		return "***"
	}
	return token[:5] + "***" + token[len(token)-5:]
}

// DataStream Desktop 数据流（双向流）
// Server 主动推送业务数据变更，Desktop 可发送刷新请求
func (s *DesktopServiceServer) DataStream(stream pb.DesktopService_DataStreamServer) error {
	ctx, cancel := context.WithCancel(stream.Context())
	defer cancel()

	// 接收首条消息，获取 desktop_id
	firstReq, err := stream.Recv()
	if err != nil {
		return status.Error(codes.InvalidArgument, "无法接收初始消息")
	}

	nodeID := firstReq.DesktopId
	logger.Infof("Desktop 数据流建立: desktopId=%d", nodeID)

	// 验证 Desktop 是否存在
	var node model.Node
	if err := db.DB.WithContext(ctx).First(&node, nodeID).Error; err != nil {
		return status.Error(codes.NotFound, "Desktop 不存在")
	}
	if node.Type != model.NodeTypeDesktop {
		return status.Error(codes.InvalidArgument, "设备类型错误")
	}

	// 注册数据流连接
	ds := &DesktopDataStream{
		NodeID: nodeID,
		UserID: node.UserID,
		Stream: stream,
		Cancel: cancel,
	}

	s.dataStreamMutex.Lock()
	if oldDS, exists := s.dataStreams[nodeID]; exists {
		oldDS.Cancel()
	}
	s.dataStreams[nodeID] = ds
	s.dataStreamMutex.Unlock()

	defer func() {
		s.dataStreamMutex.Lock()
		delete(s.dataStreams, nodeID)
		s.dataStreamMutex.Unlock()
		logger.Infof("Desktop %d 数据流断开", nodeID)
	}()

	// 发送初始数据快照（ALL）
	if err := s.sendDataSnapshot(ctx, stream, node.UserID, nodeID, pb.DesktopDataType_DESKTOP_DATA_TYPE_ALL); err != nil {
		logger.Errorf("Desktop %d 发送初始数据快照失败: %v", nodeID, err)
		return err
	}

	// 持续接收 Desktop 的刷新请求
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

			// 处理刷新请求
			refreshType := req.RefreshType
			if refreshType == pb.DesktopDataType_DESKTOP_DATA_TYPE_UNSPECIFIED {
				refreshType = pb.DesktopDataType_DESKTOP_DATA_TYPE_ALL
			}
			if err := s.sendDataSnapshot(ctx, stream, node.UserID, nodeID, refreshType); err != nil {
				logger.Errorf("Desktop %d 发送刷新数据失败: %v", nodeID, err)
				return err
			}
		}
	}
}

// sendDataSnapshot 发送数据快照
func (s *DesktopServiceServer) sendDataSnapshot(ctx context.Context, stream pb.DesktopService_DataStreamServer, userID, nodeID uint64, dataType pb.DesktopDataType) error {
	switch dataType {
	case pb.DesktopDataType_DESKTOP_DATA_TYPE_ALL:
		// 发送所有数据
		resp := &pb.DesktopDataResponse{
			Type:               pb.DesktopDataType_DESKTOP_DATA_TYPE_ALL,
			Services:           s.buildServicesData(ctx),
			Hosts:              s.buildHostsData(ctx, userID),
			Devices:            s.buildDevicesData(ctx, userID, nodeID),
			FavoriteServiceIds: s.buildFavoritesData(ctx, userID),
		}
		return stream.Send(resp)
	case pb.DesktopDataType_DESKTOP_DATA_TYPE_SERVICES:
		return stream.Send(&pb.DesktopDataResponse{
			Type:     pb.DesktopDataType_DESKTOP_DATA_TYPE_SERVICES,
			Services: s.buildServicesData(ctx),
		})
	case pb.DesktopDataType_DESKTOP_DATA_TYPE_HOSTS:
		return stream.Send(&pb.DesktopDataResponse{
			Type:  pb.DesktopDataType_DESKTOP_DATA_TYPE_HOSTS,
			Hosts: s.buildHostsData(ctx, userID),
		})
	case pb.DesktopDataType_DESKTOP_DATA_TYPE_DEVICES:
		return stream.Send(&pb.DesktopDataResponse{
			Type:    pb.DesktopDataType_DESKTOP_DATA_TYPE_DEVICES,
			Devices: s.buildDevicesData(ctx, userID, nodeID),
		})
	case pb.DesktopDataType_DESKTOP_DATA_TYPE_FAVORITES:
		return stream.Send(&pb.DesktopDataResponse{
			Type:               pb.DesktopDataType_DESKTOP_DATA_TYPE_FAVORITES,
			FavoriteServiceIds: s.buildFavoritesData(ctx, userID),
		})
	}
	return nil
}

// buildServicesData 构建服务列表数据（所有在线 Agent 的已启用服务）
func (s *DesktopServiceServer) buildServicesData(ctx context.Context) []*pb.AuthorizedService {
	var services []model.ProxyService
	if err := db.DB.WithContext(ctx).Preload("User").Where("enabled = ?", true).Find(&services).Error; err != nil {
		logger.Errorf("构建服务列表数据失败: %v", err)
		return nil
	}

	var result []*pb.AuthorizedService
	for _, svc := range services {
		if s.agentService != nil && !s.agentService.IsAgentOnline(svc.UserID) {
			continue
		}
		agentName := ""
		if svc.User != nil {
			agentName = svc.User.Name
		}
		result = append(result, &pb.AuthorizedService{
			Id: svc.ID, Name: svc.Name, AgentName: agentName,
			ListenAddr: svc.SourceAddr, TargetAddr: svc.TargetAddr,
		})
	}
	return result
}

// buildHostsData 构建主机列表数据（复用 GetAuthorizedHosts 的查询逻辑）
func (s *DesktopServiceServer) buildHostsData(ctx context.Context, userID uint64) []*pb.AuthorizedHost {
	// 获取用户信息
	var user model.User
	if err := db.DB.WithContext(ctx).First(&user, userID).Error; err != nil {
		return nil
	}

	// 获取用户所属的分组 ID 列表
	var groupIDs []int64
	db.DB.WithContext(ctx).Model(&model.GroupMember{}).Where("user_id = ?", user.ID).Pluck("group_id", &groupIDs)

	// 收集已授权的 Agent 及其 SSH 用户
	authorizedAgents := make(map[uint64][]string)

	// 通过 SSH 用户授权
	var sshUserPerms []model.AclSSHUserPermission
	if err := db.DB.WithContext(ctx).Where("user_id = ? AND enabled = ?", user.ID, true).Find(&sshUserPerms).Error; err == nil {
		for _, perm := range sshUserPerms {
			var sshUsers []string
			if err := json.Unmarshal([]byte(perm.SSHUsers), &sshUsers); err == nil {
				authorizedAgents[perm.TargetUserID] = appendUniqueStrings(authorizedAgents[perm.TargetUserID], sshUsers...)
			}
		}
	}

	// 通过 SSH 分组授权
	if len(groupIDs) > 0 {
		var sshGroupPerms []model.AclSSHGroupPermission
		if err := db.DB.WithContext(ctx).Where("group_id IN ? AND enabled = ?", groupIDs, true).Find(&sshGroupPerms).Error; err == nil {
			for _, perm := range sshGroupPerms {
				var sshUsers []string
				if err := json.Unmarshal([]byte(perm.SSHUsers), &sshUsers); err == nil {
					authorizedAgents[perm.TargetUserID] = appendUniqueStrings(authorizedAgents[perm.TargetUserID], sshUsers...)
				}
			}
		}
	}

	// 构建主机列表
	var hosts []*pb.AuthorizedHost
	for agentID, sshUsers := range authorizedAgents {
		var agentUser model.User
		if err := db.DB.WithContext(ctx).First(&agentUser, agentID).Error; err != nil {
			continue
		}
		if agentUser.Role != model.UserRoleAgent {
			continue
		}

		// 查询 Agent 的 SSH 域名，获取实际可用的 SSH 用户列表
		var sshDomain model.DomainRegistry
		availableUsers := []string{} // Agent 实际可用的 SSH 用户
		if err := db.DB.WithContext(ctx).
			Where("user_id = ? AND type = ? AND status = ?", agentID, model.DomainTypeSSH, model.DomainStatusOnline).
			First(&sshDomain).Error; err == nil {
			// 解析 SSH 用户列表
			availableUsers = sshDomain.GetSSHUsers()
		}

		// 求交集：ACL 授权的用户 ∩ Agent 实际可用的用户
		authorizedUsers := intersectStrings(sshUsers, availableUsers)
		if len(authorizedUsers) == 0 {
			// 如果交集为空，跳过这个 Agent（没有可用的 SSH 用户）
			logger.Debugf("buildHostsData: Agent %d: ACL 授权用户 %v 与实际可用用户 %v 无交集，跳过", agentID, sshUsers, availableUsers)
			continue
		}

		agentOnline := false
		if s.agentService != nil {
			agentOnline = s.agentService.IsAgentOnline(agentID)
		}

		var agentNode model.Node
		tunnelIP := ""
		lastSeen := ""
		if err := db.DB.WithContext(ctx).Where("user_id = ? AND type = ?", agentID, model.NodeTypeAgent).First(&agentNode).Error; err == nil {
			tunnelIP = agentNode.IP
			if agentNode.LastHeartbeat != nil {
				lastSeen = agentNode.LastHeartbeat.Format(time.RFC3339)
			}
		}

		hostName := agentUser.Name
		if agentNode.Name != "" {
			hostName = fmt.Sprintf("%s.%s", agentUser.Name, agentNode.Name)
		}

		host := &pb.AuthorizedHost{
			HostId: fmt.Sprintf("%d", agentID), HostName: hostName,
			TunnelIp: tunnelIP, SshUsers: authorizedUsers, // 使用交集后的用户列表
			Status: "offline", LastSeen: lastSeen,
		}
		if agentOnline {
			host.Status = "online"
		}
		hosts = append(hosts, host)
	}
	return hosts
}

// buildDevicesData 构建设备列表数据（复用 GetMyDevices 的查询逻辑）
func (s *DesktopServiceServer) buildDevicesData(ctx context.Context, userID, currentNodeID uint64) []*pb.DeviceInfo {
	var nodes []model.Node
	if err := db.DB.WithContext(ctx).Where("user_id = ? AND type = ?", userID, model.NodeTypeDesktop).Find(&nodes).Error; err != nil {
		return nil
	}

	// 从 Headscale 获取 IP
	nodeIPIndex := headscaleDeviceIPIndex{}
	if s.headscaleClient != nil {
		var user model.User
		if err := db.DB.WithContext(ctx).First(&user, userID).Error; err == nil {
			hsUserName := fmt.Sprintf("client-%s", user.Name)
			hsNodes, err := s.headscaleClient.ListNodesByUser(ctx, hsUserName)
			if err == nil {
				nodeIPIndex = buildHeadscaleDeviceIPIndex(hsNodes)
			}
		}
	}

	var devices []*pb.DeviceInfo
	for _, node := range nodes {
		var sysInfo model.NodeSystemInfo
		os := "未知"
		arch := "未知"
		hostname := node.Hostname
		if node.SystemInfo != "" {
			if err := json.Unmarshal([]byte(node.SystemInfo), &sysInfo); err == nil {
				os = sysInfo.OS
				if sysInfo.OSVersion != "" {
					os = sysInfo.OSVersion
				}
				arch = sysInfo.Arch
				if sysInfo.Hostname != "" {
					hostname = sysInfo.Hostname
				}
			}
		}

		deviceStatus := "offline"
		if node.LastHeartbeat != nil && time.Since(*node.LastHeartbeat) < 60*time.Second {
			deviceStatus = "online"
		}

		lastUsedAt := ""
		if node.LastHeartbeat != nil {
			lastUsedAt = node.LastHeartbeat.Format(time.RFC3339)
		}
		createdAt := node.CreatedAt.Format(time.RFC3339)
		ip := resolveDeviceIP(node, nodeIPIndex)

		devices = append(devices, &pb.DeviceInfo{
			DeviceToken: fmt.Sprintf("%d:%s", node.ID, "***"),
			DeviceName:  node.Name, Os: os, Arch: arch, Hostname: hostname,
			Status: deviceStatus, LastUsedAt: lastUsedAt, CreatedAt: createdAt,
			IsCurrent: node.ID == currentNodeID, Ip: ip,
		})
	}
	return devices
}

// buildFavoritesData 构建收藏列表数据
func (s *DesktopServiceServer) buildFavoritesData(ctx context.Context, userID uint64) []string {
	var favorites []model.ServiceFavorite
	if err := db.DB.WithContext(ctx).Where("client_id = ?", userID).Find(&favorites).Error; err != nil {
		return nil
	}

	var serviceIDs []string
	for _, fav := range favorites {
		var service model.ProxyService
		if err := db.DB.WithContext(ctx).Select("user_id").First(&service, fav.STCPInstanceID).Error; err == nil {
			serviceIDs = append(serviceIDs, fmt.Sprintf("%d:%s", service.UserID, fav.STCPInstanceID))
		}
	}
	return serviceIDs
}

// NotifyDesktopDataChange 通知指定 Desktop 数据变更，推送更新
func (s *DesktopServiceServer) NotifyDesktopDataChange(nodeID uint64, dataType pb.DesktopDataType) {
	s.dataStreamMutex.RLock()
	ds, exists := s.dataStreams[nodeID]
	s.dataStreamMutex.RUnlock()

	if !exists {
		return
	}

	ctx := context.Background()
	if err := s.sendDataSnapshot(ctx, ds.Stream, ds.UserID, ds.NodeID, dataType); err != nil {
		logger.Errorf("推送数据变更到 Desktop %d 失败: %v", nodeID, err)
	}
}

// NotifyAllDesktopsDataChange 通知所有在线 Desktop 数据变更
func (s *DesktopServiceServer) NotifyAllDesktopsDataChange(dataType pb.DesktopDataType) {
	s.dataStreamMutex.RLock()
	streams := make([]*DesktopDataStream, 0, len(s.dataStreams))
	for _, ds := range s.dataStreams {
		streams = append(streams, ds)
	}
	s.dataStreamMutex.RUnlock()

	ctx := context.Background()
	for _, ds := range streams {
		if err := s.sendDataSnapshot(ctx, ds.Stream, ds.UserID, ds.NodeID, dataType); err != nil {
			logger.Errorf("推送数据变更到 Desktop %d 失败: %v", ds.NodeID, err)
		}
	}
}

// ResolveDomain 域名解析 - Desktop 查询 .beagle 域名对应的 Agent 地址
func (s *DesktopServiceServer) ResolveDomain(ctx context.Context, req *pb.ResolveDomainRequest) (*pb.ResolveDomainResponse, error) {
	if req.Domain == "" {
		return &pb.ResolveDomainResponse{Success: false, Message: "域名不能为空"}, nil
	}

	// 验证 Desktop 是否存在
	var node model.Node
	if err := db.DB.WithContext(ctx).First(&node, req.DesktopId).Error; err != nil {
		return &pb.ResolveDomainResponse{Success: false, Message: "设备不存在"}, nil
	}
	if node.Type != model.NodeTypeDesktop {
		return &pb.ResolveDomainResponse{Success: false, Message: "设备类型错误"}, nil
	}

	// 查询域名注册表
	var record model.DomainRegistry
	if err := db.DB.WithContext(ctx).Preload("User").
		Where("domain = ? AND status = ?", req.Domain, model.DomainStatusOnline).
		First(&record).Error; err != nil {
		return &pb.ResolveDomainResponse{Success: false, Message: "域名未注册或已离线"}, nil
	}

	// 确定 Agent IP：优先使用域名记录的 target_ip，再查 Node 表
	agentIP := record.TargetIP

	if agentIP == "" && record.NodeID > 0 {
		// 域名记录无 target_ip，通过关联的 NodeID 查询
		var agentNode model.Node
		if err := db.DB.WithContext(ctx).First(&agentNode, record.NodeID).Error; err == nil {
			agentIP = agentNode.IP
		}
	}
	if agentIP == "" {
		// 回退：按 UserID 查有 IP 的 Agent 节点（兼容旧数据）
		var agentNode model.Node
		if err := db.DB.WithContext(ctx).
			Where("user_id = ? AND type = ? AND ip != ''", record.UserID, model.NodeTypeAgent).
			Order("last_heartbeat DESC").
			First(&agentNode).Error; err == nil {
			agentIP = agentNode.IP
		}
	}

	if agentIP == "" {
		return &pb.ResolveDomainResponse{Success: false, Message: "Agent 节点未找到或无 IP"}, nil
	}

	userName := ""
	if record.User != nil {
		userName = record.User.Name
	}

	logger.Infof("Desktop %d 解析域名: %s → %s:%d (user=%s, target_ip=%s)", req.DesktopId, req.Domain, agentIP, record.TargetPort, userName, record.TargetIP)

	// P10 重构：删除 endpoint_name 字段，Agent 自动选择实现路径
	return &pb.ResolveDomainResponse{
		Success:      true,
		Message:      "解析成功",
		Domain:       record.Domain,
		AgentIp:      agentIP,
		TargetPort:   int32(record.TargetPort),
		AgentName:    userName,
		DomainType:   string(record.Type),
		Namespace:    record.Namespace,
		ServiceName:  record.ServiceName,
		SvcProxyPort: 50051, // Agent SVCProxy 默认端口
	}, nil
}

// GetResources 资源发现 - Desktop 查询可访问的资源列表
func (s *DesktopServiceServer) GetResources(ctx context.Context, req *pb.GetResourcesRequest) (*pb.GetResourcesResponse, error) {
	// 验证 Desktop 是否存在
	var node model.Node
	if err := db.DB.WithContext(ctx).First(&node, req.DesktopId).Error; err != nil {
		return nil, status.Errorf(codes.NotFound, "设备不存在")
	}
	if node.Type != model.NodeTypeDesktop {
		return nil, status.Errorf(codes.InvalidArgument, "设备类型错误")
	}

	clientID := node.UserID
	if req.TenantId != "" {
		if req.ResourceProtocol != sessionAuthorizationProtocolV2 {
			return nil, status.Error(codes.InvalidArgument, "Tenant 作用域需要 resource_session_v2")
		}
		if !desktopHasActiveTenantMembership(ctx, clientID, req.TenantId) {
			return nil, status.Error(codes.PermissionDenied, "当前设备无权访问该 Tenant")
		}
	}

	// 查询用户所属分组
	var groupIDs []int64
	var groupMembers []model.GroupMember
	if err := db.DB.WithContext(ctx).Where("user_id = ?", clientID).Find(&groupMembers).Error; err == nil {
		for _, gm := range groupMembers {
			groupIDs = append(groupIDs, gm.GroupID)
		}
	}

	resp := &pb.GetResourcesResponse{}

	// 旧资源没有 Tenant 强身份，只能在未选择 Tenant 的兼容请求中返回。
	// 显式 Tenant 请求必须失败关闭，避免把旧 Agent 级 ACL 混入当前作用域。
	if req.TenantId == "" {
		resp.Ssh = s.querySSHResourcesGRPC(ctx, clientID, groupIDs)
		resp.K8SApi = s.queryK8SAPIResourcesGRPC(ctx, clientID, groupIDs)
		resp.K8SService = s.queryK8SServiceResourcesGRPC(ctx, clientID, groupIDs)
	}
	resp.Ssh = appendUniqueSSHResources(resp.Ssh, s.queryUnifiedHostSSHResourcesGRPC(ctx, clientID, groupIDs, req.TenantId)...)
	if req.ResourceProtocol == sessionAuthorizationProtocolV2 {
		containerSSH, containerServices := s.queryTenantContainerResourcesGRPC(ctx, &node, groupIDs, req.TenantId)
		resp.ContainerSsh = append(resp.ContainerSsh, containerSSH...)
		resp.ContainerService = containerServices
	}

	logger.Infof("Desktop %d 资源发现: SSH=%d, K8SAPI=%d, K8SService=%d, ContainerSSH=%d, ContainerService=%d",
		req.DesktopId, len(resp.Ssh), len(resp.K8SApi), len(resp.K8SService), len(resp.ContainerSsh), len(resp.ContainerService))

	return resp, nil
}

type desktopTenantResourceProjection struct {
	resource model.TenantResource
	tenant   model.Tenant
	session  *model.ResourceSession
	target   model.TenantResourceTargetRevision
	agent    model.Node
	targetV2 service.SessionAuthorizationTarget
}

func desktopHasActiveTenantMembership(ctx context.Context, userID uint64, tenantID string) bool {
	if userID == 0 || tenantID == "" {
		return false
	}
	now := time.Now().UTC()
	var count int64
	err := db.DB.WithContext(ctx).Table("tenant_membership AS membership").
		Joins("JOIN tenant AS tenant ON tenant.id = membership.tenant_id").
		Where("membership.user_id = ? AND membership.tenant_id = ? AND membership.enabled = ? AND (membership.expires_at IS NULL OR membership.expires_at > ?) AND tenant.status = ?",
			userID, tenantID, true, now, model.TenantStatusActive).
		Count(&count).Error
	return err == nil && count == 1
}

func (s *DesktopServiceServer) queryTenantContainerResourcesGRPC(ctx context.Context, desktop *model.Node, groupIDs []int64, tenantID string) ([]*pb.ContainerSSHResource, []*pb.ContainerServiceResource) {
	if desktop == nil || desktop.ID == 0 || desktop.UserID == 0 {
		return nil, nil
	}
	now := time.Now().UTC()
	var memberships []model.TenantMembership
	membershipQuery := db.DB.WithContext(ctx).Where("user_id = ? AND enabled = ? AND (expires_at IS NULL OR expires_at > ?)", desktop.UserID, true, now)
	if tenantID != "" {
		membershipQuery = membershipQuery.Where("tenant_id = ?", tenantID)
	}
	if err := membershipQuery.Find(&memberships).Error; err != nil {
		return nil, nil
	}
	tenantIDs := make([]string, 0, len(memberships))
	for _, membership := range memberships {
		tenantIDs = append(tenantIDs, membership.TenantID)
	}
	if len(tenantIDs) == 0 {
		return nil, nil
	}
	var tenants []model.Tenant
	if err := db.DB.WithContext(ctx).Where("id IN ? AND status = ?", tenantIDs, model.TenantStatusActive).Find(&tenants).Error; err != nil {
		return nil, nil
	}
	tenantByID := make(map[string]model.Tenant, len(tenants))
	activeTenantIDs := make([]string, 0, len(tenants))
	for _, tenant := range tenants {
		tenantByID[tenant.ID] = tenant
		activeTenantIDs = append(activeTenantIDs, tenant.ID)
	}
	if len(activeTenantIDs) == 0 {
		return nil, nil
	}
	grantQuery := db.DB.WithContext(ctx).Where("tenant_id IN ? AND status = ? AND valid_from <= ? AND (expires_at IS NULL OR expires_at > ?)", activeTenantIDs, model.TenantAccessGrantEnabled, now, now).
		Where("subject_type = ? AND subject_user_id = ?", model.TenantAccessGrantSubjectUser, desktop.UserID)
	if len(groupIDs) > 0 {
		grantQuery = db.DB.WithContext(ctx).Where("tenant_id IN ? AND status = ? AND valid_from <= ? AND (expires_at IS NULL OR expires_at > ?)", activeTenantIDs, model.TenantAccessGrantEnabled, now, now).
			Where("(subject_type = ? AND subject_user_id = ?) OR (subject_type = ? AND subject_group_id IN ?)",
				model.TenantAccessGrantSubjectUser, desktop.UserID, model.TenantAccessGrantSubjectGroup, groupIDs)
	}
	var grants []model.TenantAccessGrant
	if err := grantQuery.Order("tenant_resource_id ASC, revision DESC").Find(&grants).Error; err != nil {
		return nil, nil
	}
	actionByResource := make(map[string]string)
	for _, grant := range grants {
		if grant.SubjectType == model.TenantAccessGrantSubjectGroup && !grpcTenantGroupGrantMatches(ctx, grant) {
			continue
		}
		actions := parseJSONStringArray(grant.Actions)
		if containsAction(actions, "shell") {
			actionByResource[grant.TenantResourceID] = "shell"
		} else if containsAction(actions, "connect") {
			actionByResource[grant.TenantResourceID] = "connect"
		}
	}
	resourceIDs := make([]string, 0, len(actionByResource))
	for resourceID := range actionByResource {
		resourceIDs = append(resourceIDs, resourceID)
	}
	if len(resourceIDs) == 0 {
		return nil, nil
	}
	var resources []model.TenantResource
	if err := db.DB.WithContext(ctx).Where("id IN ? AND tenant_id IN ? AND visibility_state = ? AND availability_state IN ?", resourceIDs, activeTenantIDs,
		model.TenantResourceVisible, []model.TenantResourceAvailabilityState{model.TenantResourceAvailable, model.TenantResourceDegraded}).
		Order("display_name ASC, id ASC").Find(&resources).Error; err != nil {
		return nil, nil
	}

	sessionService := service.NewResourceSessionService(db.DB)
	projections := make([]desktopTenantResourceProjection, 0, len(resources))
	for _, resource := range resources {
		action := actionByResource[resource.ID]
		if (resource.Type == model.TenantResourceContainerSSH && action != "shell") ||
			(resource.Type == model.TenantResourceContainerService && action != "connect") {
			continue
		}
		session, err := sessionService.EnsureForDesktop(ctx, desktop.UserID, service.CreateResourceSessionInput{
			TenantID: resource.TenantID, ResourceID: resource.ID, Action: action, DeviceID: desktop.ID,
			ClientCapability: "resource_session_v2", RequestID: "desktop-resource:" + uuid.NewString(),
		})
		if err != nil {
			logger.Warnf("Desktop ResourceSession 授权失败: desktop_id=%d resource_id=%s err=%v", desktop.ID, resource.ID, err)
			continue
		}
		var target model.TenantResourceTargetRevision
		if err := db.DB.WithContext(ctx).Where("id = ? AND tenant_resource_source_id = ? AND superseded_at IS NULL", session.TargetRevisionID, session.TenantResourceSourceID).First(&target).Error; err != nil {
			continue
		}
		var targetV2 service.SessionAuthorizationTarget
		if json.Unmarshal([]byte(target.TargetSnapshot), &targetV2) != nil {
			continue
		}
		agentNode, err := desktopAccessAgentNode(ctx, target.AccessTechnicalResourceID)
		if err != nil {
			continue
		}
		projections = append(projections, desktopTenantResourceProjection{
			resource: resource, tenant: tenantByID[resource.TenantID], session: session, target: target, agent: agentNode, targetV2: targetV2,
		})
	}

	permissions := make(map[string]service.SessionAuthorizationPermission)
	listenPorts := make(map[string]map[string]uint16)
	technicalIDs := make(map[string]struct{})
	for _, projection := range projections {
		technicalIDs[projection.session.AccessTechnicalResourceID] = struct{}{}
	}
	for technicalID := range technicalIDs {
		snapshot, err := service.NewSessionAuthorizationService(db.DB).BuildSnapshot(ctx, technicalID, true)
		if err != nil {
			continue
		}
		for _, permission := range snapshot.Permissions {
			permissions[permission.SessionID] = permission
		}
		listenPorts[technicalID] = allocateSessionAuthorizationListenPorts(snapshot.Permissions)
	}

	domainSuffix := desktopResourceDomainSuffix(ctx)
	sshResources := make([]*pb.ContainerSSHResource, 0)
	serviceResources := make([]*pb.ContainerServiceResource, 0)
	for _, projection := range projections {
		permission, allowed := permissions[projection.session.ID]
		if !allowed {
			continue
		}
		state := string(projection.resource.AvailabilityState)
		if projection.resource.Type == model.TenantResourceContainerSSH {
			listenPort := listenPorts[projection.session.AccessTechnicalResourceID][projection.resource.ID]
			if listenPort == 0 {
				continue
			}
			domain, err := service.ContainerSSHBusinessDomain(ctx, db.DB, projection.session.AccessTechnicalResourceID, projection.target.TargetSnapshot)
			if err != nil {
				logger.Warnf("Desktop ContainerSSH 业务域名无效: resource_id=%s err=%v", projection.resource.ID, err)
				continue
			}
			sshResources = append(sshResources, &pb.ContainerSSHResource{
				ResourceId: projection.resource.ID, TenantId: projection.resource.TenantID, TenantName: projection.tenant.Name,
				DisplayName: projection.resource.DisplayName, State: state, TargetRevision: projection.target.Revision,
				AgentNodeId: projection.agent.ID, Capability: string(model.TenantResourceContainerSSH), ListenPort: uint32(listenPort),
				Domain: domain, AgentIp: projection.agent.IP, SshUsers: permission.SSHUsers,
				SessionId: projection.session.ID, SourceId: projection.session.TenantResourceSourceID,
				TargetRevisionId: projection.session.TargetRevisionID, AuthorizationRevision: permission.AuthorizationRevision,
			})
			continue
		}
		serviceResources = append(serviceResources, &pb.ContainerServiceResource{
			ResourceId: projection.resource.ID, TenantId: projection.resource.TenantID, TenantName: projection.tenant.Name,
			DisplayName: projection.resource.DisplayName, State: state, TargetRevision: projection.target.Revision,
			AgentNodeId: projection.agent.ID, AgentIp: projection.agent.IP, SvcProxyPort: 50051,
			Domain: projection.resource.ID + ".service" + domainSuffix, Namespace: projection.targetV2.NamespaceName,
			ServiceUid: projection.targetV2.ServiceUID, ServiceName: projection.targetV2.ServiceName,
			PortName: projection.targetV2.PortName, PortNumber: projection.targetV2.PortNumber, Protocol: projection.targetV2.Protocol,
			SessionId: projection.session.ID, SourceId: projection.session.TenantResourceSourceID,
			TargetRevisionId: projection.session.TargetRevisionID, AuthorizationRevision: permission.AuthorizationRevision,
		})
	}
	return sshResources, serviceResources
}

func grpcTenantGroupGrantMatches(ctx context.Context, grant model.TenantAccessGrant) bool {
	if grant.SubjectGroupID == nil || grant.TenantID == "" {
		return false
	}
	var count int64
	return db.DB.WithContext(ctx).Model(&model.Group{}).Where("id = ? AND tenant_id = ?", *grant.SubjectGroupID, grant.TenantID).Count(&count).Error == nil && count == 1
}

func desktopAccessAgentNode(ctx context.Context, technicalResourceID string) (model.Node, error) {
	var technical model.TechnicalResource
	if err := db.DB.WithContext(ctx).Where("id = ? AND lifecycle_state = ?", technicalResourceID, model.TechnicalResourceRegistered).First(&technical).Error; err != nil {
		return model.Node{}, err
	}
	if technical.Type == model.TechnicalResourceEndpoint {
		if technical.ParentID == nil || *technical.ParentID == "" {
			return model.Node{}, fmt.Errorf("Endpoint parent Agent is missing")
		}
		if err := db.DB.WithContext(ctx).Where("id = ? AND type = ? AND lifecycle_state = ?", *technical.ParentID, model.TechnicalResourceAgent, model.TechnicalResourceRegistered).First(&technical).Error; err != nil {
			return model.Node{}, err
		}
	}
	if technical.Type != model.TechnicalResourceAgent {
		return model.Node{}, fmt.Errorf("unsupported access TechnicalResource type %s", technical.Type)
	}
	var binding model.TechnicalResourceBinding
	if err := db.DB.WithContext(ctx).Where("technical_resource_id = ? AND source_type = ? AND enabled = ?", technical.ID, model.TechnicalResourceBindingLegacyNode, true).First(&binding).Error; err != nil {
		return model.Node{}, err
	}
	nodeID, err := strconv.ParseUint(binding.SourceID, 10, 64)
	if err != nil {
		return model.Node{}, err
	}
	var node model.Node
	if err := db.DB.WithContext(ctx).Where("id = ? AND type = ? AND ip <> ?", nodeID, model.NodeTypeAgent, "").First(&node).Error; err != nil {
		return model.Node{}, err
	}
	return node, nil
}

func desktopResourceDomainSuffix(ctx context.Context) string {
	domainSuffix := model.DefaultDomainSuffix
	var domainConfig model.SystemConfig
	if err := db.DB.WithContext(ctx).Where("key = ?", model.ConfigDomainSuffix).First(&domainConfig).Error; err == nil && domainConfig.Value != "" {
		domainSuffix = domainConfig.Value
	}
	if !strings.HasPrefix(domainSuffix, ".") {
		domainSuffix = "." + domainSuffix
	}
	return domainSuffix
}

func grpcGroupGrantMatchesTenant(ctx context.Context, grant model.AccessGrant) bool {
	if grant.SubjectGroupID == nil || grant.TenantID == "" {
		return false
	}
	var group model.Group
	return db.DB.WithContext(ctx).Where("id = ? AND tenant_id = ?", *grant.SubjectGroupID, grant.TenantID).First(&group).Error == nil
}

// querySSHResourcesGRPC 查询 SSH 资源（gRPC 版本）
func (s *DesktopServiceServer) querySSHResourcesGRPC(ctx context.Context, clientID uint64, groupIDs []int64) []*pb.SSHResource {
	var resources []*pb.SSHResource
	userCache := make(map[uint64]*model.User)

	// 直接用户授权
	var userPerms []model.AclSSHUserPermission
	db.DB.WithContext(ctx).Preload("TargetUser").Where("user_id = ? AND enabled = ?", clientID, true).Find(&userPerms)
	for _, p := range userPerms {
		if p.TargetUser == nil {
			continue
		}
		userCache[p.TargetUserID] = p.TargetUser
		domain := s.findDomainForResource(ctx, p.TargetUserID, model.DomainTypeSSH)
		resources = append(resources, &pb.SSHResource{
			AgentId:   p.TargetUserID,
			AgentName: p.TargetUser.Name,
			Domain:    domain,
			SshUsers:  parseJSONStringArrayGRPC(p.SSHUsers),
		})
	}

	// 分组授权
	if len(groupIDs) > 0 {
		var groupPerms []model.AclSSHGroupPermission
		db.DB.WithContext(ctx).Preload("TargetUser").Where("group_id IN ? AND enabled = ?", groupIDs, true).Find(&groupPerms)
		for _, p := range groupPerms {
			if p.TargetUser == nil {
				continue
			}
			if _, exists := userCache[p.TargetUserID]; exists {
				continue
			}
			userCache[p.TargetUserID] = p.TargetUser
			domain := s.findDomainForResource(ctx, p.TargetUserID, model.DomainTypeSSH)
			resources = append(resources, &pb.SSHResource{
				AgentId:   p.TargetUserID,
				AgentName: p.TargetUser.Name,
				Domain:    domain,
				SshUsers:  parseJSONStringArrayGRPC(p.SSHUsers),
			})
		}
	}

	return resources
}

func (s *DesktopServiceServer) queryUnifiedHostSSHResourcesGRPC(ctx context.Context, clientID uint64, groupIDs []int64, tenantID string) []*pb.SSHResource {
	domains := s.queryUnifiedHostSSHDomains(ctx, clientID, groupIDs, tenantID)
	if len(domains) == 0 {
		return nil
	}
	nodeIDs := make([]uint64, 0, len(domains))
	for _, domain := range domains {
		if domain.NodeID != 0 {
			nodeIDs = append(nodeIDs, domain.NodeID)
		}
	}
	nodesByID := make(map[uint64]model.Node, len(nodeIDs))
	if len(nodeIDs) > 0 {
		var nodes []model.Node
		if err := db.DB.WithContext(ctx).Where("id IN ?", nodeIDs).Find(&nodes).Error; err == nil {
			for _, node := range nodes {
				nodesByID[node.ID] = node
			}
		}
	}
	resources := make([]*pb.SSHResource, 0, len(domains))
	for _, domain := range domains {
		users := parseJSONStringArrayGRPC(domain.SshUsers)
		if len(users) == 0 {
			continue
		}
		agentName := domain.Domain
		if node, ok := nodesByID[domain.NodeID]; ok && node.Name != "" {
			agentName = node.Name
		}
		resources = append(resources, &pb.SSHResource{
			AgentId: domain.UserID, AgentName: agentName, Domain: domain.Domain, SshUsers: users,
		})
	}
	return resources
}

func appendUniqueSSHResources(resources []*pb.SSHResource, extra ...*pb.SSHResource) []*pb.SSHResource {
	seen := make(map[string]struct{}, len(resources)+len(extra))
	for _, resource := range resources {
		if resource == nil || resource.Domain == "" {
			continue
		}
		seen[resource.Domain] = struct{}{}
	}
	for _, resource := range extra {
		if resource == nil || resource.Domain == "" {
			continue
		}
		if _, exists := seen[resource.Domain]; exists {
			continue
		}
		seen[resource.Domain] = struct{}{}
		resources = append(resources, resource)
	}
	return resources
}

// queryK8SAPIResourcesGRPC 查询 K8S API 资源（gRPC 版本）
func (s *DesktopServiceServer) queryK8SAPIResourcesGRPC(ctx context.Context, clientID uint64, groupIDs []int64) []*pb.K8SAPIResource {
	var resources []*pb.K8SAPIResource
	userCache := make(map[uint64]*model.User)

	var userPerms []model.AclK8SUserPermission
	db.DB.WithContext(ctx).Preload("TargetUser").Where("user_id = ? AND enabled = ?", clientID, true).Find(&userPerms)
	for _, p := range userPerms {
		if p.TargetUser == nil {
			continue
		}
		userCache[p.TargetUserID] = p.TargetUser
		domain := s.findDomainForResource(ctx, p.TargetUserID, model.DomainTypeK8SAPI)
		resources = append(resources, &pb.K8SAPIResource{
			AgentId:    p.TargetUserID,
			AgentName:  p.TargetUser.Name,
			Domain:     domain,
			K8SGroups:  parseJSONStringArrayGRPC(p.K8SGroups),
			Namespaces: parseJSONStringArrayGRPC(p.Namespaces),
		})
	}

	if len(groupIDs) > 0 {
		var groupPerms []model.AclK8SGroupPermission
		db.DB.WithContext(ctx).Preload("TargetUser").Where("group_id IN ? AND enabled = ?", groupIDs, true).Find(&groupPerms)
		for _, p := range groupPerms {
			if p.TargetUser == nil {
				continue
			}
			if _, exists := userCache[p.TargetUserID]; exists {
				continue
			}
			userCache[p.TargetUserID] = p.TargetUser
			domain := s.findDomainForResource(ctx, p.TargetUserID, model.DomainTypeK8SAPI)
			resources = append(resources, &pb.K8SAPIResource{
				AgentId:    p.TargetUserID,
				AgentName:  p.TargetUser.Name,
				Domain:     domain,
				K8SGroups:  parseJSONStringArrayGRPC(p.K8SGroups),
				Namespaces: parseJSONStringArrayGRPC(p.Namespaces),
			})
		}
	}

	return resources
}

// queryK8SServiceResourcesGRPC 查询 K8S Service 资源（gRPC 版本）
func (s *DesktopServiceServer) queryK8SServiceResourcesGRPC(ctx context.Context, clientID uint64, groupIDs []int64) []*pb.K8SServiceResource {
	var resources []*pb.K8SServiceResource
	agentIDs := make(map[uint64]*model.User)

	var userPerms []model.AclK8SServiceUserPermission
	db.DB.WithContext(ctx).Preload("TargetUser").Where("user_id = ? AND enabled = ?", clientID, true).Find(&userPerms)
	for _, p := range userPerms {
		if p.TargetUser != nil {
			agentIDs[p.TargetUserID] = p.TargetUser
		}
	}

	if len(groupIDs) > 0 {
		var groupPerms []model.AclK8SServiceGroupPermission
		db.DB.WithContext(ctx).Preload("TargetUser").Where("group_id IN ? AND enabled = ?", groupIDs, true).Find(&groupPerms)
		for _, p := range groupPerms {
			if p.TargetUser != nil {
				agentIDs[p.TargetUserID] = p.TargetUser
			}
		}
	}

	for agentID, agentUser := range agentIDs {
		discoveredServices := cache.GetK8SServiceDiscovery(agentID)
		for _, ds := range discoveredServices {
			var domainReg model.DomainRegistry
			domain := ""
			if err := db.DB.WithContext(ctx).Where("user_id = ? AND type = ? AND namespace = ? AND service_name = ?",
				agentID, model.DomainTypeK8SSVC, ds.Namespace, ds.ServiceName).
				First(&domainReg).Error; err == nil {
				domain = domainReg.Domain
			}

			var port int32
			if len(ds.Ports) > 0 {
				port = ds.Ports[0].Port
			}

			resources = append(resources, &pb.K8SServiceResource{
				AgentId:     agentID,
				AgentName:   agentUser.Name,
				Namespace:   ds.Namespace,
				ServiceName: ds.ServiceName,
				Domain:      domain,
				Port:        port,
			})
		}
	}

	return resources
}

// findDomainForResource 查找指定 Agent 和类型的域名
func (s *DesktopServiceServer) findDomainForResource(ctx context.Context, agentUserID uint64, domainType model.DomainType) string {
	var domainReg model.DomainRegistry
	if err := db.DB.WithContext(ctx).Where("user_id = ? AND type = ? AND status = ?",
		agentUserID, domainType, model.DomainStatusOnline).
		First(&domainReg).Error; err == nil {
		return domainReg.Domain
	}
	return ""
}

// parseJSONStringArrayGRPC 解析 JSON 字符串数组
func parseJSONStringArrayGRPC(jsonStr string) []string {
	if jsonStr == "" || jsonStr == "[]" {
		return nil
	}
	var result []string
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil
	}
	return result
}

// GetDomainList 获取域名列表（按类型分类）
func (s *DesktopServiceServer) GetDomainList(ctx context.Context, req *pb.GetDomainListRequest) (*pb.GetDomainListResponse, error) {
	// 验证 Desktop 是否存在
	var node model.Node
	if err := db.DB.WithContext(ctx).First(&node, req.DesktopId).Error; err != nil {
		return nil, status.Errorf(codes.NotFound, "设备不存在")
	}
	if node.Type != model.NodeTypeDesktop {
		return nil, status.Errorf(codes.InvalidArgument, "设备类型错误")
	}

	clientID := node.UserID

	// 查询用户所属分组
	var groupIDs []int64
	var groupMembers []model.GroupMember
	if err := db.DB.WithContext(ctx).Where("user_id = ?", clientID).Find(&groupMembers).Error; err == nil {
		for _, gm := range groupMembers {
			groupIDs = append(groupIDs, gm.GroupID)
		}
	}

	// 查询所有可访问的域名
	domains := s.queryAccessibleDomains(ctx, clientID, groupIDs)
	domains = appendUniqueDomainItems(domains, s.queryUnifiedHostSSHDomainItems(ctx, clientID, groupIDs))

	logger.Infof("Desktop %d 域名列表查询: 共 %d 条域名", req.DesktopId, len(domains))

	return &pb.GetDomainListResponse{
		Domains: domains,
	}, nil
}

// queryAccessibleDomains 查询用户可访问的所有域名
// queryAccessibleDomains 查询用户可访问的所有域名
// 按权限类型分别收集有权限的 Agent User ID，然后按类型过滤域名
// SSH 类型还需要做用户列表交集（ACL 授权的用户 ∩ Agent 实际可用的用户）
func (s *DesktopServiceServer) queryAccessibleDomains(ctx context.Context, clientID uint64, groupIDs []int64) []*pb.DomainItem {
	var domains []*pb.DomainItem

	// 按类型分别收集有权限的 Agent User ID
	sshAgentIDs := make(map[uint64]bool)    // SSH 权限
	k8sAgentIDs := make(map[uint64]bool)    // K8S API 权限
	k8sSvcAgentIDs := make(map[uint64]bool) // K8S Service 权限

	// SSH 权限的授权用户列表（agent_user_id → 授权的 SSH 用户名列表）
	sshAuthorizedUsers := make(map[uint64][]string)

	// Endpoint 权限：endpoint_id(UUID) → 是否有权限
	endpointSSHIDs := make(map[string]bool)    // Endpoint SSH 权限（key 为 UUID）
	endpointK8SAPIIDs := make(map[string]bool) // Endpoint K8S API 权限（key 为 UUID）
	endpointK8SSvcIDs := make(map[string]bool) // Endpoint K8S Service 权限（key 为 UUID）

	// Endpoint SSH 权限的授权用户列表（endpoint_id(UUID) → 授权的 SSH 用户名列表）
	endpointSSHAuthorizedUsers := make(map[string][]string)

	// Endpoint 权限（按 Name 索引，用于匹配 DomainRegistry.endpoint_id）
	// DomainRegistry.endpoint_id 存的是 Endpoint.Name，而 ACL 表存的是 Endpoint.ID(UUID)
	endpointSSHNames := make(map[string]bool)    // Endpoint SSH 权限（key 为 Name）
	endpointK8SAPINames := make(map[string]bool) // Endpoint K8S API 权限（key 为 Name）
	endpointK8SSvcNames := make(map[string]bool) // Endpoint K8S Service 权限（key 为 Name）

	// Endpoint SSH 权限的授权用户列表（endpoint_name → 授权的 SSH 用户名列表）
	endpointSSHAuthorizedUsersByName := make(map[string][]string)

	// 1. 收集 SSH 权限
	var sshUserPerms []model.AclSSHUserPermission
	db.DB.WithContext(ctx).Where("user_id = ? AND enabled = ?", clientID, true).Find(&sshUserPerms)
	for _, p := range sshUserPerms {
		sshAgentIDs[p.TargetUserID] = true
		var users []string
		if err := json.Unmarshal([]byte(p.SSHUsers), &users); err == nil {
			sshAuthorizedUsers[p.TargetUserID] = appendUniqueStrings(sshAuthorizedUsers[p.TargetUserID], users...)
		}
	}

	if len(groupIDs) > 0 {
		var sshGroupPerms []model.AclSSHGroupPermission
		db.DB.WithContext(ctx).Where("group_id IN ? AND enabled = ?", groupIDs, true).Find(&sshGroupPerms)
		for _, p := range sshGroupPerms {
			sshAgentIDs[p.TargetUserID] = true
			var users []string
			if err := json.Unmarshal([]byte(p.SSHUsers), &users); err == nil {
				sshAuthorizedUsers[p.TargetUserID] = appendUniqueStrings(sshAuthorizedUsers[p.TargetUserID], users...)
			}
		}
	}

	// 2. 收集 K8S API 权限
	var k8sUserPerms []model.AclK8SUserPermission
	db.DB.WithContext(ctx).Where("user_id = ? AND enabled = ?", clientID, true).Find(&k8sUserPerms)
	for _, p := range k8sUserPerms {
		k8sAgentIDs[p.TargetUserID] = true
	}

	if len(groupIDs) > 0 {
		var k8sGroupPerms []model.AclK8SGroupPermission
		db.DB.WithContext(ctx).Where("group_id IN ? AND enabled = ?", groupIDs, true).Find(&k8sGroupPerms)
		for _, p := range k8sGroupPerms {
			k8sAgentIDs[p.TargetUserID] = true
		}
	}

	// 3. 收集 K8S Service 权限
	var k8sSvcUserPerms []model.AclK8SServiceUserPermission
	db.DB.WithContext(ctx).Where("user_id = ? AND enabled = ?", clientID, true).Find(&k8sSvcUserPerms)
	for _, p := range k8sSvcUserPerms {
		k8sSvcAgentIDs[p.TargetUserID] = true
	}

	if len(groupIDs) > 0 {
		var k8sSvcGroupPerms []model.AclK8SServiceGroupPermission
		db.DB.WithContext(ctx).Where("group_id IN ? AND enabled = ?", groupIDs, true).Find(&k8sSvcGroupPerms)
		for _, p := range k8sSvcGroupPerms {
			k8sSvcAgentIDs[p.TargetUserID] = true
		}
	}

	// 4. 收集 Endpoint SSH 权限
	// P12 重构：已废弃，Endpoint SSH 复用 Agent SSH 授权

	// 5. 收集 Endpoint K8S API 权限
	// P11 重构：已废弃，不再查询 Endpoint K8SAPI 权限

	// 6. 收集 Endpoint K8S Service 权限
	// P10 重构：已废弃，不再查询 Endpoint K8SService 权限

	// 7. 合并所有有权限的 Agent User ID（用于一次性查询域名）
	allAgentIDs := make(map[uint64]bool)
	for uid := range sshAgentIDs {
		allAgentIDs[uid] = true
	}
	for uid := range k8sAgentIDs {
		allAgentIDs[uid] = true
	}
	for uid := range k8sSvcAgentIDs {
		allAgentIDs[uid] = true
	}

	// 查询 Endpoint 对应的 Agent User ID
	var endpointIDs []string
	for eid := range endpointSSHIDs {
		endpointIDs = append(endpointIDs, eid)
	}
	for eid := range endpointK8SAPIIDs {
		if !contains(endpointIDs, eid) {
			endpointIDs = append(endpointIDs, eid)
		}
	}
	for eid := range endpointK8SSvcIDs {
		if !contains(endpointIDs, eid) {
			endpointIDs = append(endpointIDs, eid)
		}
	}

	if len(endpointIDs) > 0 {
		var endpoints []model.Endpoint
		db.DB.WithContext(ctx).Where("id IN ?", endpointIDs).Find(&endpoints)
		for _, ep := range endpoints {
			allAgentIDs[ep.UserID] = true
			// 建立 UUID → Name 映射，将权限 map 转换为以 Name 为 key
			// DomainRegistry.endpoint_id 存的是 Endpoint.Name，ACL 表存的是 Endpoint.ID(UUID)
			if endpointSSHIDs[ep.ID] {
				endpointSSHNames[ep.Name] = true
				// 转换 SSH 授权用户列表
				if users, ok := endpointSSHAuthorizedUsers[ep.ID]; ok {
					endpointSSHAuthorizedUsersByName[ep.Name] = appendUniqueStrings(endpointSSHAuthorizedUsersByName[ep.Name], users...)
				}
			}
			if endpointK8SAPIIDs[ep.ID] {
				endpointK8SAPINames[ep.Name] = true
			}
			if endpointK8SSvcIDs[ep.ID] {
				endpointK8SSvcNames[ep.Name] = true
			}
		}
	}

	if len(allAgentIDs) == 0 {
		return domains
	}

	var userIDList []uint64
	for uid := range allAgentIDs {
		userIDList = append(userIDList, uid)
	}

	var domainRegs []model.DomainRegistry
	db.DB.WithContext(ctx).Where("user_id IN ?", userIDList).Find(&domainRegs)

	// 8. 按类型过滤域名，只返回用户有对应类型权限的域名
	// 注意：dr.EndpointID 是 Endpoint.Name（如 beagle-002），需要用 endpointXxxNames 匹配
	for _, dr := range domainRegs {
		// 检查用户是否有该类型的权限
		hasPermission := false

		switch dr.Type {
		case model.DomainTypeSSH:
			// 检查 Agent 级别权限或 Endpoint 级别权限
			if sshAgentIDs[dr.UserID] {
				hasPermission = true
			} else if dr.EndpointID != "" && endpointSSHNames[dr.EndpointID] {
				hasPermission = true
			}
		case model.DomainTypeK8SAPI:
			// 检查 Agent 级别权限或 Endpoint 级别权限
			if k8sAgentIDs[dr.UserID] {
				hasPermission = true
			} else if dr.EndpointID != "" && endpointK8SAPINames[dr.EndpointID] {
				hasPermission = true
			}
		case model.DomainTypeK8SSVC:
			// 检查 Agent 级别权限或 Endpoint 级别权限
			if k8sSvcAgentIDs[dr.UserID] {
				hasPermission = true
			} else if dr.EndpointID != "" && endpointK8SSvcNames[dr.EndpointID] {
				hasPermission = true
			}
		default:
			continue
		}

		if !hasPermission {
			continue // 没有权限，跳过
		}

		// P10 重构：删除 endpoint_id 字段
		item := &pb.DomainItem{
			Domain:      dr.Domain,
			Type:        string(dr.Type),
			Namespace:   dr.Namespace,
			ServiceName: dr.ServiceName,
		}

		// 解析 region（从 domain 中提取）
		item.Region = s.extractRegionFromDomain(dr.Domain)

		// 解析 service_ports（k8ssvc 类型）
		if dr.Type == model.DomainTypeK8SSVC && dr.ServicePorts != "" {
			var ports []int32
			if err := json.Unmarshal([]byte(dr.ServicePorts), &ports); err == nil {
				item.ServicePorts = ports
			}
		}

		// SSH 类型：做用户列表交集（ACL 授权的用户 ∩ Agent 实际可用的用户）
		if dr.Type == model.DomainTypeSSH {
			// Agent 实际可用的 SSH 用户
			availableUsers := dr.GetSSHUsers()

			// 获取 ACL 授权的 SSH 用户（优先 Agent 级别，其次 Endpoint 级别）
			var authorizedUsers []string
			if sshAgentIDs[dr.UserID] {
				authorizedUsers = sshAuthorizedUsers[dr.UserID]
			} else if dr.EndpointID != "" && endpointSSHNames[dr.EndpointID] {
				authorizedUsers = endpointSSHAuthorizedUsersByName[dr.EndpointID]
			}

			// 求交集
			filteredUsers := intersectStrings(authorizedUsers, availableUsers)
			if len(filteredUsers) == 0 {
				continue // 交集为空，跳过该域名
			}
			item.SshUsers = filteredUsers
		}

		// 判断状态
		item.Status = s.getDomainStatus(ctx, &dr)

		domains = append(domains, item)
	}

	return domains
}

func (s *DesktopServiceServer) queryUnifiedHostSSHDomainItems(ctx context.Context, userID uint64, groupIDs []int64) []*pb.DomainItem {
	records := s.queryUnifiedHostSSHDomains(ctx, userID, groupIDs, "")
	items := make([]*pb.DomainItem, 0, len(records))
	for _, record := range records {
		item := &pb.DomainItem{
			Domain: record.Domain, Type: string(record.Type), Status: s.getDomainStatus(ctx, &record),
			Namespace: record.Namespace, ServiceName: record.ServiceName, Region: s.extractRegionFromDomain(record.Domain),
		}
		if record.Type == model.DomainTypeSSH && record.SshUsers != "" {
			var users []string
			if err := json.Unmarshal([]byte(record.SshUsers), &users); err == nil {
				item.SshUsers = users
			}
		}
		items = append(items, item)
	}
	return items
}

func appendUniqueDomainItems(items []*pb.DomainItem, extra []*pb.DomainItem) []*pb.DomainItem {
	seen := make(map[string]struct{}, len(items)+len(extra))
	for _, item := range items {
		if item == nil || item.Domain == "" {
			continue
		}
		seen[item.Domain] = struct{}{}
	}
	for _, item := range extra {
		if item == nil || item.Domain == "" {
			continue
		}
		if _, exists := seen[item.Domain]; exists {
			continue
		}
		seen[item.Domain] = struct{}{}
		items = append(items, item)
	}
	return items
}

// extractRegionFromDomain 从域名中提取 region
// 例如：beagle-242.beijing.beagle → beijing
//
//	kubernetes.beijing.beagle → beijing
//	postgres.yygl.beijing.beagle → beijing
func (s *DesktopServiceServer) extractRegionFromDomain(domain string) string {
	parts := strings.Split(domain, ".")
	if len(parts) >= 3 {
		// 倒数第二个部分是 region
		return parts[len(parts)-2]
	}
	return ""
}

// getDomainStatus 判断域名状态
func (s *DesktopServiceServer) getDomainStatus(ctx context.Context, dr *model.DomainRegistry) string {
	if dr.Status == model.DomainStatusOffline {
		return string(model.DomainStatusOffline)
	}
	// 判断是 Node 域名还是 Endpoint 域名
	if dr.EndpointID == "" {
		// Node 域名：通过 NodeStatusCache 判断
		return s.getNodeDomainStatus(ctx, dr)
	}
	// Endpoint 域名：通过 EndpointStatusCache 判断
	return s.getEndpointDomainStatus(dr)
}

// getNodeDomainStatus 判断 Node 域名状态
func (s *DesktopServiceServer) getNodeDomainStatus(ctx context.Context, dr *model.DomainRegistry) string {
	// 查询 NodeStatusCache
	nodeStatus, exists := cache.GetNodeStatus(dr.NodeID)
	if !exists {
		return "offline"
	}

	// 检查心跳超时（60 秒）
	if time.Since(nodeStatus.LastHeartbeat) < 60*time.Second {
		return "online"
	}

	// 心跳超时，查询 Headscale 验证
	if dr.TargetIP != "" {
		if s.agentService != nil && s.agentService.headscaleClient != nil {
			node, err := s.agentService.headscaleClient.GetNodeByIP(ctx, dr.TargetIP)
			if err == nil && node != nil && node.Online {
				// Headscale 显示在线，更新缓存
				nodeStatus.LastHeartbeat = time.Now()
				cache.SetNodeStatus(dr.NodeID, nodeStatus)
				return "online"
			}
		}
	}

	// Headscale 查询失败或显示离线
	return "offline"
}

// getEndpointDomainStatus 判断 Endpoint 域名状态
func (s *DesktopServiceServer) getEndpointDomainStatus(dr *model.DomainRegistry) string {
	// 查询 EndpointStatusCache
	endpointStatus, exists := cache.GetEndpointStatus(dr.EndpointID)
	if !exists {
		return "offline"
	}

	// 检查心跳超时（60 秒）
	if time.Since(endpointStatus.LastHeartbeat) < 60*time.Second {
		return "online"
	}

	// Endpoint 无法通过 Headscale 查询，心跳超时直接判断为离线
	return "offline"
}

// ListDomains 列出域名（Client/Agent 模式通用）
// - Client 模式：返回有权限访问的域名（用于 Desktop 客户端）
// - Agent 模式：返回该 Agent 自己的域名（用于 CloudIDE）
// ListDomains 列出域名（Client/Agent 模式通用）
// - Client 模式（Desktop）：使用 JWT Token，返回有权限访问的域名
// - Agent 模式（CloudIDE）：使用 Device Token，返回有权限访问的域名（与 Desktop 权限一致）
func (s *DesktopServiceServer) ListDomains(ctx context.Context, req *pb.ListDomainsRequest) (*pb.ListDomainsResponse, error) {
	// 尝试从 context 中获取 client_id（Desktop 客户端）
	clientID, hasClientID := ctx.Value("client_id").(uint64)

	// 尝试从 context 中获取 user_id（Agent/CloudIDE）
	agentUserID, hasAgentUserID := ctx.Value("user_id").(uint64)

	// 必须至少有一种认证方式
	if (!hasClientID || clientID == 0) && (!hasAgentUserID || agentUserID == 0) {
		return nil, status.Errorf(codes.Unauthenticated, "未认证")
	}

	// 确定用户 ID（Agent 模式使用 agentUserID，Client 模式使用 clientID）
	var userID uint64
	var userType string
	if hasAgentUserID && agentUserID > 0 {
		userID = agentUserID
		userType = "Agent"
	} else {
		userID = clientID
		userType = "Client"
	}

	// 查询用户所属分组
	var groupIDs []int64
	var groupMembers []model.GroupMember
	if err := db.DB.WithContext(ctx).Where("user_id = ?", userID).Find(&groupMembers).Error; err == nil {
		for _, gm := range groupMembers {
			groupIDs = append(groupIDs, gm.GroupID)
		}
	}

	// 查询所有可访问的域名记录
	var domainRecords []model.DomainRegistry
	query := db.DB.WithContext(ctx)

	// 根据权限过滤域名
	// 1. 收集有权限的 Agent User ID
	agentUserIDs := make(map[uint64]bool)

	// SSH 权限
	var sshUserPerms []model.AclSSHUserPermission
	db.DB.WithContext(ctx).Where("user_id = ? AND enabled = ?", userID, true).Find(&sshUserPerms)
	for _, p := range sshUserPerms {
		agentUserIDs[p.TargetUserID] = true
	}

	if len(groupIDs) > 0 {
		var sshGroupPerms []model.AclSSHGroupPermission
		db.DB.WithContext(ctx).Where("group_id IN ? AND enabled = ?", groupIDs, true).Find(&sshGroupPerms)
		for _, p := range sshGroupPerms {
			agentUserIDs[p.TargetUserID] = true
		}
	}

	// K8S API 权限
	var k8sUserPerms []model.AclK8SUserPermission
	db.DB.WithContext(ctx).Where("user_id = ? AND enabled = ?", userID, true).Find(&k8sUserPerms)
	for _, p := range k8sUserPerms {
		agentUserIDs[p.TargetUserID] = true
	}

	if len(groupIDs) > 0 {
		var k8sGroupPerms []model.AclK8SGroupPermission
		db.DB.WithContext(ctx).Where("group_id IN ? AND enabled = ?", groupIDs, true).Find(&k8sGroupPerms)
		for _, p := range k8sGroupPerms {
			agentUserIDs[p.TargetUserID] = true
		}
	}

	// K8S Service 权限
	var k8sSvcUserPerms []model.AclK8SServiceUserPermission
	db.DB.WithContext(ctx).Where("user_id = ? AND enabled = ?", userID, true).Find(&k8sSvcUserPerms)
	for _, p := range k8sSvcUserPerms {
		agentUserIDs[p.TargetUserID] = true
	}

	if len(groupIDs) > 0 {
		var k8sSvcGroupPerms []model.AclK8SServiceGroupPermission
		db.DB.WithContext(ctx).Where("group_id IN ? AND enabled = ?", groupIDs, true).Find(&k8sSvcGroupPerms)
		for _, p := range k8sSvcGroupPerms {
			agentUserIDs[p.TargetUserID] = true
		}
	}

	// Endpoint SSH 权限
	// P12 重构：已废弃，Endpoint SSH 复用 Agent SSH 授权

	// Endpoint K8S API 权限
	// P11 重构：已废弃，不再查询 Endpoint K8SAPI 权限

	// Endpoint K8S Service 权限
	// P10 重构：已废弃，不再查询 Endpoint K8SService 权限

	// 转换为 ID 列表
	var allowedAgentIDs []uint64
	for id := range agentUserIDs {
		allowedAgentIDs = append(allowedAgentIDs, id)
	}
	unifiedHostDomains := s.queryUnifiedHostSSHDomains(ctx, userID, groupIDs, "")

	if len(allowedAgentIDs) == 0 && len(unifiedHostDomains) == 0 {
		// 没有任何权限，返回空列表
		logger.Infof("%s %d 域名列表查询: 无权限，返回空列表", userType, userID)
		return &pb.ListDomainsResponse{
			Domains: []*pb.DomainInfo{},
		}, nil
	}

	// 查询域名记录（Preload Endpoint 关联，用于填充 endpoint_name）
	if len(allowedAgentIDs) > 0 {
		query.Preload("Endpoint").Where("user_id IN ?", allowedAgentIDs).Find(&domainRecords)
	}
	domainRecords = appendUniqueDomainRecords(domainRecords, unifiedHostDomains)

	// 构建响应
	var domains []*pb.DomainInfo
	for _, record := range domainRecords {
		// 调试：打印原始数据库记录
		if record.Type == model.DomainTypeK8SSVC {
			logger.Infof("ListDomains: 处理 K8SSVC 域名 %s, DB service_ports='%s', len=%d",
				record.Domain, record.ServicePorts, len(record.ServicePorts))
		}

		// 使用 DomainRegistry 中的 target_ip（已经是正确的 Tailscale IP）
		// 不再查询 Node 表，避免多节点时选择错误
		domainInfo := &pb.DomainInfo{
			Domain:      record.Domain,
			Type:        string(record.Type),
			TargetIp:    record.TargetIP, // 直接使用 DomainRegistry.target_ip
			TargetPort:  int32(record.TargetPort),
			Namespace:   record.Namespace,
			ServiceName: record.ServiceName,
			Status:      s.getDomainStatus(ctx, &record), // 使用统一的状态判断逻辑
		}

		// 填充 cluster_name（从 domain 中提取 region）
		domainInfo.ClusterName = s.extractRegionFromDomain(record.Domain)

		// P10 重构：删除 endpoint_id 和 endpoint_name 字段
		// 客户端不需要知道底层实现细节

		// 解析 service_ports（仅 k8ssvc 类型）
		if record.Type == model.DomainTypeK8SSVC && record.ServicePorts != "" {
			var ports []int32
			if err := json.Unmarshal([]byte(record.ServicePorts), &ports); err == nil {
				domainInfo.ServicePorts = ports
				logger.Infof("ListDomains: domain=%s service_ports=%v", record.Domain, ports)
			} else {
				logger.Warnf("解析 service_ports 失败: domain=%s, err=%v", record.Domain, err)
			}
		} else if record.Type == model.DomainTypeK8SSVC {
			logger.Warnf("ListDomains: K8SSVC 域名 %s service_ports 为空（DB值='%s'）", record.Domain, record.ServicePorts)
		}

		// 解析 ssh_users（仅 ssh 类型）
		if record.Type == model.DomainTypeSSH && record.SshUsers != "" {
			var users []string
			if err := json.Unmarshal([]byte(record.SshUsers), &users); err == nil {
				domainInfo.SshUsers = users
				logger.Infof("ListDomains: domain=%s ssh_users=%v", record.Domain, users)
			} else {
				logger.Warnf("解析 ssh_users 失败: domain=%s, err=%v", record.Domain, err)
			}
		}

		domains = append(domains, domainInfo)
	}

	logger.Infof("%s %d 域名列表查询: 共 %d 条域名", userType, userID, len(domains))

	return &pb.ListDomainsResponse{
		Domains: domains,
	}, nil
}

func appendUniqueDomainRecords(records []model.DomainRegistry, extra []model.DomainRegistry) []model.DomainRegistry {
	seen := make(map[string]struct{}, len(records)+len(extra))
	for _, record := range records {
		seen[record.Domain] = struct{}{}
	}
	for _, record := range extra {
		if _, exists := seen[record.Domain]; exists {
			continue
		}
		seen[record.Domain] = struct{}{}
		records = append(records, record)
	}
	return records
}

func (s *DesktopServiceServer) queryUnifiedHostSSHDomains(ctx context.Context, userID uint64, groupIDs []int64, tenantID string) []model.DomainRegistry {
	now := time.Now().UTC()
	var memberships []model.TenantMembership
	membershipQuery := db.DB.WithContext(ctx).Where("user_id = ? AND enabled = ? AND (expires_at IS NULL OR expires_at > ?)", userID, true, now)
	if tenantID != "" {
		membershipQuery = membershipQuery.Where("tenant_id = ?", tenantID)
	}
	if err := membershipQuery.Find(&memberships).Error; err != nil {
		return nil
	}
	tenantIDs := make([]string, 0, len(memberships))
	for _, membership := range memberships {
		tenantIDs = append(tenantIDs, membership.TenantID)
	}
	if len(tenantIDs) == 0 {
		return nil
	}
	var tenants []model.Tenant
	if err := db.DB.WithContext(ctx).Where("id IN ? AND status = ?", tenantIDs, model.TenantStatusActive).Find(&tenants).Error; err != nil {
		return nil
	}
	activeTenantIDs := make([]string, 0, len(tenants))
	for _, tenant := range tenants {
		activeTenantIDs = append(activeTenantIDs, tenant.ID)
	}
	if len(activeTenantIDs) == 0 {
		return nil
	}
	grantQuery := db.DB.WithContext(ctx).Where("tenant_id IN ? AND status = ? AND datetime(valid_from) <= datetime(?) AND datetime(expires_at) > datetime(?)", activeTenantIDs, "enabled", now, now).
		Where("subject_type = ? AND subject_user_id = ?", "user", userID)
	if len(groupIDs) > 0 {
		grantQuery = db.DB.WithContext(ctx).Where("tenant_id IN ? AND status = ? AND datetime(valid_from) <= datetime(?) AND datetime(expires_at) > datetime(?)", activeTenantIDs, "enabled", now, now).
			Where("(subject_type = ? AND subject_user_id = ?) OR (subject_type = ? AND subject_group_id IN ?)", "user", userID, "group", groupIDs)
	}
	var grants []model.AccessGrant
	if err := grantQuery.Find(&grants).Error; err != nil {
		return nil
	}
	resourceIDs := make([]string, 0, len(grants))
	seenResource := make(map[string]struct{}, len(grants))
	for _, grant := range grants {
		if grant.SubjectType == "group" && !grpcGroupGrantMatchesTenant(ctx, grant) {
			continue
		}
		if !containsAction(parseJSONStringArray(grant.Actions), "shell") {
			continue
		}
		if _, exists := seenResource[grant.ResourceID]; exists {
			continue
		}
		seenResource[grant.ResourceID] = struct{}{}
		resourceIDs = append(resourceIDs, grant.ResourceID)
	}
	if len(resourceIDs) == 0 {
		return nil
	}
	var resources []model.Resource
	if err := db.DB.WithContext(ctx).Where("id IN ? AND tenant_id IN ? AND type = ? AND state IN ?", resourceIDs, activeTenantIDs, model.ResourceTypeHostSSH,
		[]model.ResourceState{model.ResourceStateAvailable, model.ResourceStateDegraded}).Find(&resources).Error; err != nil {
		return nil
	}
	nodeIDs := make([]uint64, 0, len(resources))
	nodeIDStrings := make([]string, 0, len(resources))
	for _, resource := range resources {
		if resource.AgentNodeID == 0 {
			continue
		}
		nodeIDs = append(nodeIDs, resource.AgentNodeID)
		nodeIDStrings = append(nodeIDStrings, strconv.FormatUint(resource.AgentNodeID, 10))
	}
	if len(nodeIDs) == 0 {
		return nil
	}
	var domains []model.DomainRegistry
	if err := db.DB.WithContext(ctx).Where("type = ? AND resource_kind = ? AND resource_id IN ?", model.DomainTypeSSH, model.DomainResourceNode, nodeIDStrings).
		Or("type = ? AND node_id IN ?", model.DomainTypeSSH, nodeIDs).
		Order("domain ASC").Find(&domains).Error; err != nil {
		return nil
	}
	return domains
}

// getNodeStatus 获取节点状态
func getNodeStatus(node *model.Node) string {
	if node.LastHeartbeat == nil {
		return "offline"
	}
	// 5 分钟内有心跳认为在线
	if time.Since(*node.LastHeartbeat) < 5*time.Minute {
		return "online"
	}
	return "offline"
}

// ============================================
// REST API 专用方法（gRPC 降级兜底）
// ============================================

// HandleDesktopHeartbeatREST REST 心跳处理（替代双向流）
func (s *DesktopServiceServer) HandleDesktopHeartbeatREST(ctx context.Context, desktopID uint64, tunnelIP string, tunnelConnected bool) {
	// 查找 node
	var node model.Node
	if err := db.DB.WithContext(ctx).First(&node, desktopID).Error; err != nil {
		logger.Errorf("[DesktopREST] 心跳: desktop_id=%d 不存在", desktopID)
		return
	}

	s.handleDesktopHeartbeat(ctx, node.ID, &pb.DesktopHeartbeatRequest{
		DesktopId:       desktopID,
		TunnelIp:        tunnelIP,
		TunnelConnected: tunnelConnected,
	})
}

// GetDataSnapshotREST REST 数据快照（替代双向流 DataStream）
func (s *DesktopServiceServer) GetDataSnapshotREST(ctx context.Context, desktopID uint64) (map[string]any, error) {
	// 查找 node 获取 userID
	var node model.Node
	if err := db.DB.WithContext(ctx).First(&node, desktopID).Error; err != nil {
		return nil, fmt.Errorf("desktop_id=%d 不存在", desktopID)
	}

	return map[string]any{
		"services":             s.buildServicesData(ctx),
		"hosts":                s.buildHostsData(ctx, node.UserID),
		"devices":              s.buildDevicesData(ctx, node.UserID, node.ID),
		"favorite_service_ids": s.buildFavoritesData(ctx, node.UserID),
	}, nil
}
