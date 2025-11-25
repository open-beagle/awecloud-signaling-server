package api

import (
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
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	ServerName string `json:"server_name"`
	SecretKey  string `json:"secret_key"`
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
	if client.Status != "active" {
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

	// 更新最后在线时间
	now := time.Now()
	client.IsOnline = true
	client.LastSeen = &now
	db.DB.Save(&client)

	c.JSON(http.StatusOK, ClientAuthResponse{
		Success:   true,
		Token:     tokenString,
		ExpiresIn: expiresIn,
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

	// 查询Client有权限访问的服务
	var permissions []model.ClientPermission
	if err := db.DB.Preload("STCPInstance").Where("client_id = ?", clientID).Find(&permissions).Error; err != nil {
		c.JSON(http.StatusInternalServerError, ServicesResponse{
			Success: false,
			Message: "查询失败",
		})
		return
	}

	// 构建服务列表
	services := make([]ServiceInfo, 0, len(permissions))
	for _, perm := range permissions {
		if perm.STCPInstance != nil {
			services = append(services, ServiceInfo{
				ID:         perm.STCPInstance.ID,
				Name:       perm.STCPInstance.InstanceName,
				Type:       perm.STCPInstance.ServiceType,
				ServerName: perm.STCPInstance.ServerName,
				SecretKey:  perm.STCPInstance.SecretKey,
			})
		}
	}

	c.JSON(http.StatusOK, ServicesResponse{
		Success:  true,
		Services: services,
	})
}
