package api

import (
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

type ClientAuthAPI struct {
	config *config.ServerConfig
}

func NewClientAuthAPI(cfg *config.ServerConfig) *ClientAuthAPI {
	return &ClientAuthAPI{config: cfg}
}

type ClientAuthRequest struct {
	ClientID     string `json:"client_id" binding:"required"`
	ClientSecret string `json:"client_secret" binding:"required"`
}

type ClientAuthResponse struct {
	Success   bool   `json:"success"`
	Token     string `json:"token,omitempty"`
	ExpiresIn int    `json:"expires_in,omitempty"`
	Message   string `json:"message,omitempty"`
}

type ServiceInfo struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	SecretKey string `json:"secret_key"`
}

type ServicesResponse struct {
	Success  bool          `json:"success"`
	Services []ServiceInfo `json:"services,omitempty"`
	Message  string        `json:"message,omitempty"`
}

func (a *ClientAuthAPI) Auth(c *gin.Context) {
	var req ClientAuthRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ClientAuthResponse{
			Success: false,
			Message: "请求参数错误",
		})
		return
	}

	// 查询Client
	var client model.Client
	if err := db.DB.Where("client_id = ?", req.ClientID).First(&client).Error; err != nil {
		c.JSON(http.StatusUnauthorized, ClientAuthResponse{
			Success: false,
			Message: "Client ID或Secret错误",
		})
		return
	}

	// 验证Secret
	if client.ClientSecret != req.ClientSecret {
		c.JSON(http.StatusUnauthorized, ClientAuthResponse{
			Success: false,
			Message: "Client ID或Secret错误",
		})
		return
	}

	// 检查状态
	if !client.Enabled {
		c.JSON(http.StatusForbidden, ClientAuthResponse{
			Success: false,
			Message: "Client已被禁用",
		})
		return
	}

	// 生成JWT Token
	expiresIn := a.config.Security.JWTExpireHours * 3600
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"client_id": client.ID,
		"exp":       time.Now().Add(time.Hour * time.Duration(a.config.Security.JWTExpireHours)).Unix(),
	})

	tokenString, err := token.SignedString([]byte(a.config.Security.JWTSecret))
	if err != nil {
		c.JSON(http.StatusInternalServerError, ClientAuthResponse{
			Success: false,
			Message: "生成Token失败",
		})
		return
	}

	// 创建会话记录
	session := &model.ClientSession{
		ClientID:     client.ID,
		SessionToken: tokenString,
		ExpiresAt:    time.Now().Add(time.Hour * time.Duration(a.config.Security.JWTExpireHours)),
	}
	if err := db.DB.Create(session).Error; err != nil {
		log.Printf("创建会话记录失败: %v", err)
		// 不影响登录流程，继续
	}

	c.JSON(http.StatusOK, ClientAuthResponse{
		Success:   true,
		Token:     tokenString,
		ExpiresIn: expiresIn,
	})
}

// GetTunnelConfig 获取隧道配置
func (a *ClientAuthAPI) GetTunnelConfig(c *gin.Context) {
	// 从Authorization header获取token
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "未提供认证信息",
		})
		return
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || parts[0] != "Bearer" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "认证格式错误",
		})
		return
	}

	// 验证Token
	token, err := jwt.Parse(parts[1], func(token *jwt.Token) (interface{}, error) {
		return []byte(a.config.Security.JWTSecret), nil
	})

	if err != nil || !token.Valid {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "Token无效或已过期",
		})
		return
	}

	// 构建 FRP 连接信息
	frpServer := ""
	frpPort := a.config.Server.BindPort
	if a.config.Server.PublicURL != "" {
		frpServer = a.config.Server.PublicURL
		frpPort = 0 // 使用完整 URL 时，端口信息已包含在 URL 中
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"token":   a.config.Server.Token,
		"server":  frpServer,
		"port":    frpPort,
	})
}

func (a *ClientAuthAPI) GetServices(c *gin.Context) {
	// 从Authorization header获取token
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		c.JSON(http.StatusUnauthorized, ServicesResponse{
			Success: false,
			Message: "未提供认证信息",
		})
		return
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || parts[0] != "Bearer" {
		c.JSON(http.StatusUnauthorized, ServicesResponse{
			Success: false,
			Message: "认证格式错误",
		})
		return
	}

	// 验证Token
	token, err := jwt.Parse(parts[1], func(token *jwt.Token) (interface{}, error) {
		return []byte(a.config.Security.JWTSecret), nil
	})

	if err != nil || !token.Valid {
		c.JSON(http.StatusUnauthorized, ServicesResponse{
			Success: false,
			Message: "Token无效或已过期",
		})
		return
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		c.JSON(http.StatusUnauthorized, ServicesResponse{
			Success: false,
			Message: "Token格式错误",
		})
		return
	}

	clientID := int64(claims["client_id"].(float64))

	// 权限过滤逻辑：查询用户可访问的服务
	var allInstances []model.STCPInstance

	// 1. 查询所有 public 服务
	var publicInstances []model.STCPInstance
	if err := db.DB.Where("access_type = ?", "public").Find(&publicInstances).Error; err != nil {
		c.JSON(http.StatusInternalServerError, ServicesResponse{
			Success: false,
			Message: "查询 public 服务失败",
		})
		return
	}
	allInstances = append(allInstances, publicInstances...)

	// 2. 查询用户有权限的 private 服务
	var privateAccess []model.STCPAccess
	if err := db.DB.Preload("STCPInstance").
		Where("client_id = ?", clientID).
		Find(&privateAccess).Error; err != nil {
		c.JSON(http.StatusInternalServerError, ServicesResponse{
			Success: false,
			Message: "查询 private 服务失败",
		})
		return
	}
	for _, access := range privateAccess {
		if access.STCPInstance != nil && access.STCPInstance.AccessType == "private" {
			allInstances = append(allInstances, *access.STCPInstance)
		}
	}

	// 3. 查询用户所在组的 group 服务
	var groupInstances []model.STCPInstance
	if err := db.DB.
		Joins("JOIN group_members ON group_members.group_id = stcp_instances.group_id").
		Where("group_members.client_id = ? AND stcp_instances.access_type = ?", clientID, "group").
		Find(&groupInstances).Error; err != nil {
		c.JSON(http.StatusInternalServerError, ServicesResponse{
			Success: false,
			Message: "查询 group 服务失败",
		})
		return
	}
	allInstances = append(allInstances, groupInstances...)

	// 构建服务列表
	services := make([]ServiceInfo, 0, len(allInstances))
	for _, instance := range allInstances {
		services = append(services, ServiceInfo{
			ID:        instance.ID,
			Name:      instance.InstanceName,
			SecretKey: instance.SecretKey,
		})
	}

	c.JSON(http.StatusOK, ServicesResponse{
		Success:  true,
		Services: services,
	})
}
