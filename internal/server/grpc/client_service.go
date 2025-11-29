package grpc

import (
	"context"
	"log"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/auth"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

// ClientServiceServer Client服务实现
type ClientServiceServer struct {
	pb.UnimplementedClientServiceServer
	config       *config.ServerConfig
	agentService *AgentServiceServer
}

// NewClientServiceServer 创建Client服务
func NewClientServiceServer(cfg *config.ServerConfig) *ClientServiceServer {
	return &ClientServiceServer{
		config: cfg,
	}
}

// SetAgentService 设置AgentService（用于检查Agent状态）
func (s *ClientServiceServer) SetAgentService(agentService *AgentServiceServer) {
	s.agentService = agentService
}

// Authenticate Client认证
func (s *ClientServiceServer) Authenticate(ctx context.Context, req *pb.AuthRequest) (*pb.AuthResponse, error) {
	log.Printf("Client认证请求: %s", req.ClientId)

	// 查询Client
	var client model.Client
	if err := db.DB.Where("client_id = ?", req.ClientId).First(&client).Error; err != nil {
		log.Printf("Client不存在: %s", req.ClientId)
		return &pb.AuthResponse{
			Success: false,
			Message: "Client ID或Secret错误",
		}, nil
	}

	// 验证Secret
	if client.ClientSecret != req.ClientSecret {
		log.Printf("Client密钥错误: %s", req.ClientId)
		return &pb.AuthResponse{
			Success: false,
			Message: "Client ID或Secret错误",
		}, nil
	}

	// 检查状态
	if !client.Enabled {
		log.Printf("Client已禁用: %s", req.ClientId)
		return &pb.AuthResponse{
			Success: false,
			Message: "Client已被禁用",
		}, nil
	}

	// 创建Device Token（用于记住登录）
	// 使用客户端提供的设备信息
	deviceInfo := auth.DeviceInfo{
		OS:       "desktop",
		Arch:     "unknown",
		Hostname: "desktop-client",
	}

	// 如果客户端提供了设备信息，使用客户端的信息
	if req.DeviceInfo != nil {
		deviceInfo.OS = req.DeviceInfo.Os
		deviceInfo.Arch = req.DeviceInfo.Arch
		deviceInfo.Hostname = req.DeviceInfo.Hostname
	}

	deviceToken, err := auth.CreateDeviceToken(db.DB, client.ID, deviceInfo)
	if err != nil {
		log.Printf("创建Device Token失败: %v", err)
		return &pb.AuthResponse{
			Success: false,
			Message: "创建Device Token失败",
		}, nil
	}

	// 生成JWT Token
	expiresAt := time.Now().Add(time.Hour * time.Duration(s.config.Security.JWTExpireHours))
	jwtToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"client_id":    client.ID,
		"device_token": deviceToken.DeviceToken,
		"exp":          expiresAt.Unix(),
	})

	jwtTokenString, err := jwtToken.SignedString([]byte(s.config.Security.JWTSecret))
	if err != nil {
		log.Printf("生成JWT Token失败: %v", err)
		return &pb.AuthResponse{
			Success: false,
			Message: "生成JWT Token失败",
		}, nil
	}

	log.Printf("Client认证成功: %s, device_token=%s", req.ClientId, deviceToken.DeviceToken)

	// 构建 FRP 连接信息
	// 如果配置了公网 URL，使用公网 URL；否则返回空字符串和端口
	frpServer := ""
	frpPort := int32(s.config.Server.BindPort)
	if s.config.Server.PublicURL != "" {
		frpServer = s.config.Server.PublicURL
		frpPort = 0 // 使用完整 URL 时，端口信息已包含在 URL 中
	}

	return &pb.AuthResponse{
		Success:      true,
		Message:      "认证成功",
		SessionToken: jwtTokenString,
		ExpiresAt:    expiresAt.Unix(),
		DeviceToken:  deviceToken.DeviceToken,
		Token:        s.config.Server.Token,
		Server:       frpServer,
		Port:         frpPort,
	}, nil
}

// LoginWithToken 使用Device Token登录获取JWT
func (s *ClientServiceServer) LoginWithToken(ctx context.Context, req *pb.LoginWithTokenRequest) (*pb.LoginWithTokenResponse, error) {
	log.Printf("Device Token登录请求: client_id=%s", req.ClientId)

	// 查询Client
	var client model.Client
	if err := db.DB.Where("client_id = ?", req.ClientId).First(&client).Error; err != nil {
		log.Printf("Client不存在: %s", req.ClientId)
		return &pb.LoginWithTokenResponse{
			Success: false,
			Message: "Client ID错误",
		}, nil
	}

	// 检查状态
	if !client.Enabled {
		log.Printf("Client已禁用: %s", req.ClientId)
		return &pb.LoginWithTokenResponse{
			Success: false,
			Message: "Client已被禁用",
		}, nil
	}

	// 验证Device Token
	var deviceToken model.DeviceToken
	if err := db.DB.Where("client_id = ? AND device_token = ? AND revoked = ?",
		client.ID, req.DeviceToken, false).First(&deviceToken).Error; err != nil {
		log.Printf("Device Token无效: %s", req.DeviceToken)
		return &pb.LoginWithTokenResponse{
			Success: false,
			Message: "Device Token无效或已过期",
		}, nil
	}

	// 检查是否过期
	if time.Now().After(deviceToken.ExpiresAt) {
		log.Printf("Device Token已过期: %s", req.DeviceToken)
		return &pb.LoginWithTokenResponse{
			Success: false,
			Message: "Device Token已过期",
		}, nil
	}

	// 更新最后使用时间
	deviceToken.LastUsedAt = time.Now()
	db.DB.Save(&deviceToken)

	// 生成JWT Token
	jwtExpiresIn := s.config.Security.JWTExpireHours * 3600
	expiresAt := time.Now().Add(time.Hour * time.Duration(s.config.Security.JWTExpireHours))
	jwtToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"client_id":    client.ID,
		"device_token": deviceToken.DeviceToken,
		"exp":          expiresAt.Unix(),
	})

	jwtTokenString, err := jwtToken.SignedString([]byte(s.config.Security.JWTSecret))
	if err != nil {
		log.Printf("生成JWT Token失败: %v", err)
		return &pb.LoginWithTokenResponse{
			Success: false,
			Message: "生成JWT Token失败",
		}, nil
	}

	log.Printf("Device Token验证成功: client_id=%d, device_token=%s", client.ID, deviceToken.DeviceToken)

	return &pb.LoginWithTokenResponse{
		Success:      true,
		JwtToken:     jwtTokenString,
		JwtExpiresIn: int32(jwtExpiresIn),
	}, nil
}

// GetServices 获取可访问服务列表
func (s *ClientServiceServer) GetServices(ctx context.Context, req *pb.GetServicesRequest) (*pb.GetServicesResponse, error) {
	// 验证Token
	token, err := jwt.Parse(req.SessionToken, func(token *jwt.Token) (interface{}, error) {
		return []byte(s.config.Security.JWTSecret), nil
	})

	if err != nil || !token.Valid {
		return nil, status.Error(codes.Unauthenticated, "Token无效或已过期")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "Token格式错误")
	}

	clientID := int64(claims["client_id"].(float64))

	// 权限过滤逻辑：查询用户可访问的服务
	var allInstances []model.STCPInstance

	// 1. 查询所有 public 服务
	var publicInstances []model.STCPInstance
	if err := db.DB.Preload("Agent").Where("access_type = ?", "public").Find(&publicInstances).Error; err != nil {
		log.Printf("查询 public 服务失败: %v", err)
		return nil, status.Error(codes.Internal, "查询失败")
	}
	allInstances = append(allInstances, publicInstances...)

	// 2. 查询用户有权限的 private 服务
	var privateAccess []model.STCPAccess
	if err := db.DB.Preload("STCPInstance").Preload("STCPInstance.Agent").
		Where("client_id = ?", clientID).
		Find(&privateAccess).Error; err != nil {
		log.Printf("查询 private 服务失败: %v", err)
		return nil, status.Error(codes.Internal, "查询失败")
	}
	for _, access := range privateAccess {
		if access.STCPInstance != nil && access.STCPInstance.AccessType == "private" {
			allInstances = append(allInstances, *access.STCPInstance)
		}
	}

	// 3. 查询用户所在组的 group 服务
	var groupInstances []model.STCPInstance
	if err := db.DB.Preload("Agent").
		Joins("JOIN group_members ON group_members.group_id = stcp_instances.group_id").
		Where("group_members.client_id = ? AND stcp_instances.access_type = ?", clientID, "group").
		Find(&groupInstances).Error; err != nil {
		log.Printf("查询 group 服务失败: %v", err)
		return nil, status.Error(codes.Internal, "查询失败")
	}
	allInstances = append(allInstances, groupInstances...)

	// 构建服务列表
	services := make([]*pb.ServiceInfo, 0, len(allInstances))
	for _, instance := range allInstances {
		agentName := ""
		if instance.Agent != nil {
			agentName = instance.Agent.AgentName
		}

		accessType := instance.AccessType
		if accessType == "" {
			accessType = "public"
		}

		// 检查状态
		serviceStatus := "offline"
		if s.agentService != nil && s.agentService.IsAgentOnline(instance.AgentID) {
			serviceStatus = "online"
		}

		services = append(services, &pb.ServiceInfo{
			InstanceId:   instance.ID,
			InstanceName: instance.InstanceName,
			AgentName:    agentName,
			Description:  instance.Description,
			LocalPort:    int32(instance.LocalPort),
			AccessType:   accessType,
			Status:       serviceStatus,
			LocalIp:      instance.LocalIP,
		})
	}

	log.Printf("返回服务列表: client_id=%d, count=%d", clientID, len(services))

	return &pb.GetServicesResponse{
		Success:  true,
		Services: services,
	}, nil
}

// ConnectService 连接服务（获取连接信息）
func (s *ClientServiceServer) ConnectService(ctx context.Context, req *pb.ConnectRequest) (*pb.ConnectResponse, error) {
	// 验证Token
	token, err := jwt.Parse(req.SessionToken, func(token *jwt.Token) (interface{}, error) {
		return []byte(s.config.Security.JWTSecret), nil
	})

	if err != nil || !token.Valid {
		return nil, status.Error(codes.Unauthenticated, "Token无效或已过期")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "Token格式错误")
	}

	clientID := int64(claims["client_id"].(float64))

	// 查询STCP实例
	var instance model.STCPInstance
	if err := db.DB.First(&instance, req.InstanceId).Error; err != nil {
		log.Printf("STCP实例不存在: %d", req.InstanceId)
		return &pb.ConnectResponse{
			Success: false,
			Message: "服务不存在",
		}, nil
	}

	// 检查权限
	hasAccess := false
	switch instance.AccessType {
	case "public", "":
		// Public 服务，所有用户可访问
		hasAccess = true
	case "private":
		// Private 服务，检查 stcp_access 表
		var access model.STCPAccess
		if err := db.DB.Where("client_id = ? AND stcp_instance_id = ?", clientID, req.InstanceId).First(&access).Error; err == nil {
			hasAccess = true
		}
	case "group":
		// Group 服务，检查用户是否在组中
		if instance.GroupID != nil {
			var member model.GroupMember
			if err := db.DB.Where("client_id = ? AND group_id = ?", clientID, *instance.GroupID).First(&member).Error; err == nil {
				hasAccess = true
			}
		}
	}

	if !hasAccess {
		log.Printf("Client无权限访问该服务: client_id=%d, instance_id=%d, access_type=%s", clientID, req.InstanceId, instance.AccessType)
		return &pb.ConnectResponse{
			Success: false,
			Message: "无权限访问该服务",
		}, nil
	}

	log.Printf("Client连接服务: client_id=%d, instance=%s", clientID, instance.InstanceName)

	// 获取隧道服务器地址
	serverURL := s.config.Server.PublicURL
	if serverURL == "" {
		// 如果没有配置 public_url，返回空字符串
		// Desktop 将使用其连接的 Server 地址 + 隧道端口
		serverURL = ""
	}

	return &pb.ConnectResponse{
		Success:            true,
		Message:            "连接信息获取成功",
		InstanceName:       instance.InstanceName,
		SecretKey:          instance.SecretKey,
		SuggestedLocalPort: int32(instance.LocalPort),
		ServerUrl:          serverURL,
	}, nil
}
