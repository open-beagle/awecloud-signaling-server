package grpc

import (
	"context"
	"log"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

// ClientServiceServer Client服务实现
type ClientServiceServer struct {
	pb.UnimplementedClientServiceServer
	config *config.ServerConfig
}

// NewClientServiceServer 创建Client服务
func NewClientServiceServer(cfg *config.ServerConfig) *ClientServiceServer {
	return &ClientServiceServer{
		config: cfg,
	}
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

	// 生成JWT Token
	expiresAt := time.Now().Add(time.Hour * time.Duration(s.config.Security.JWTExpireHours))
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"client_id": client.ID,
		"exp":       expiresAt.Unix(),
	})

	tokenString, err := token.SignedString([]byte(s.config.Security.JWTSecret))
	if err != nil {
		log.Printf("生成Token失败: %v", err)
		return &pb.AuthResponse{
			Success: false,
			Message: "生成Token失败",
		}, nil
	}

	// 创建会话记录
	session := &model.ClientSession{
		ClientID:     client.ID,
		SessionToken: tokenString,
		ExpiresAt:    expiresAt,
	}
	if err := db.DB.Create(session).Error; err != nil {
		log.Printf("创建会话记录失败: %v", err)
		// 不影响登录流程
	}

	log.Printf("Client认证成功: %s", req.ClientId)

	return &pb.AuthResponse{
		Success:      true,
		Message:      "认证成功",
		SessionToken: tokenString,
		ExpiresAt:    expiresAt.Unix(),
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

	// 查询Client有权限访问的服务
	var accessList []model.STCPAccess
	if err := db.DB.Preload("STCPInstance").Preload("STCPInstance.Agent").
		Where("client_id = ?", clientID).Find(&accessList).Error; err != nil {
		log.Printf("查询服务列表失败: %v", err)
		return nil, status.Error(codes.Internal, "查询失败")
	}

	// 构建服务列表
	services := make([]*pb.ServiceInfo, 0, len(accessList))
	for _, access := range accessList {
		if access.STCPInstance != nil {
			agentName := ""
			if access.STCPInstance.Agent != nil {
				agentName = access.STCPInstance.Agent.AgentName
			}

			services = append(services, &pb.ServiceInfo{
				InstanceId:   access.STCPInstance.ID,
				InstanceName: access.STCPInstance.InstanceName,
				AgentName:    agentName,
				Description:  access.STCPInstance.Description,
				LocalPort:    int32(access.STCPInstance.LocalPort),
			})
		}
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

	// 检查权限
	var access model.STCPAccess
	if err := db.DB.Where("client_id = ? AND stcp_instance_id = ?", clientID, req.InstanceId).First(&access).Error; err != nil {
		log.Printf("Client无权限访问该服务: client_id=%d, instance_id=%d", clientID, req.InstanceId)
		return &pb.ConnectResponse{
			Success: false,
			Message: "无权限访问该服务",
		}, nil
	}

	// 查询STCP实例
	var instance model.STCPInstance
	if err := db.DB.First(&instance, req.InstanceId).Error; err != nil {
		log.Printf("STCP实例不存在: %d", req.InstanceId)
		return &pb.ConnectResponse{
			Success: false,
			Message: "服务不存在",
		}, nil
	}

	log.Printf("Client连接服务: client_id=%d, instance=%s", clientID, instance.InstanceName)

	return &pb.ConnectResponse{
		Success:            true,
		Message:            "连接信息获取成功",
		InstanceName:       instance.InstanceName,
		SecretKey:          instance.SecretKey,
		SuggestedLocalPort: int32(instance.LocalPort),
	}, nil
}
