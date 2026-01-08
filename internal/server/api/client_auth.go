package api

import (
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/headscale"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

type ClientAuthAPI struct {
	config          *config.ServerConfig
	headscaleClient *headscale.Client
}

func NewClientAuthAPI(cfg *config.ServerConfig) *ClientAuthAPI {
	api := &ClientAuthAPI{config: cfg}

	// 初始化 Headscale 客户端
	if cfg.Tailscale.HeadscaleURL != "" && cfg.Tailscale.HeadscaleAPIKey != "" {
		api.headscaleClient = headscale.NewClient(headscale.Config{
			URL:    cfg.Tailscale.HeadscaleURL,
			APIKey: cfg.Tailscale.HeadscaleAPIKey,
		})
	}

	return api
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

	// 查询所有端口映射服务（Tailscale 模式）
	var proxyServices []model.ProxyService
	if err := db.DB.Find(&proxyServices).Error; err != nil {
		c.JSON(http.StatusInternalServerError, ServicesResponse{
			Success: false,
			Message: "查询服务失败",
		})
		return
	}

	// 构建服务列表
	services := make([]ServiceInfo, 0, len(proxyServices))
	for _, svc := range proxyServices {
		services = append(services, ServiceInfo{
			ID:        svc.ID,
			Name:      svc.Name,
			SecretKey: "",
		})
	}

	c.JSON(http.StatusOK, ServicesResponse{
		Success:  true,
		Services: services,
	})
}

// TailscaleAuthResponse Tailscale 认证响应
type TailscaleAuthResponse struct {
	Success    bool   `json:"success"`
	Message    string `json:"message,omitempty"`
	ControlURL string `json:"control_url,omitempty"`
	AuthKey    string `json:"auth_key,omitempty"`
	DerpURL    string `json:"derp_url,omitempty"`
}

// GetTailscaleAuth 获取 Tailscale 认证信息（Desktop 用）
func (a *ClientAuthAPI) GetTailscaleAuth(c *gin.Context) {
	// 从 Authorization header 获取 token
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		c.JSON(http.StatusUnauthorized, TailscaleAuthResponse{
			Success: false,
			Message: "未提供认证信息",
		})
		return
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || parts[0] != "Bearer" {
		c.JSON(http.StatusUnauthorized, TailscaleAuthResponse{
			Success: false,
			Message: "认证格式错误",
		})
		return
	}

	// 验证 Token
	token, err := jwt.Parse(parts[1], func(token *jwt.Token) (interface{}, error) {
		return []byte(a.config.Security.JWTSecret), nil
	})

	if err != nil || !token.Valid {
		c.JSON(http.StatusUnauthorized, TailscaleAuthResponse{
			Success: false,
			Message: "Token 无效或已过期",
		})
		return
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		c.JSON(http.StatusUnauthorized, TailscaleAuthResponse{
			Success: false,
			Message: "Token 格式错误",
		})
		return
	}

	clientID := int64(claims["client_id"].(float64))

	// 检查 Headscale 客户端
	if a.headscaleClient == nil {
		c.JSON(http.StatusServiceUnavailable, TailscaleAuthResponse{
			Success: false,
			Message: "Tailscale 服务未配置",
		})
		return
	}

	// 创建预认证密钥（临时节点，Desktop 断开后自动清理）
	authKeyExpiry := 24 * time.Hour
	user := a.config.Tailscale.User
	authKey, err := a.headscaleClient.CreatePreAuthKey(c.Request.Context(), user, authKeyExpiry, true)
	if err != nil {
		logger.Errorf("创建 Tailscale 预认证密钥失败: %v", err)
		c.JSON(http.StatusInternalServerError, TailscaleAuthResponse{
			Success: false,
			Message: "创建认证密钥失败",
		})
		return
	}

	logger.Infof("为 Client %d 创建 Tailscale 预认证密钥", clientID)

	c.JSON(http.StatusOK, TailscaleAuthResponse{
		Success:    true,
		ControlURL: a.config.Tailscale.HeadscaleURL,
		AuthKey:    authKey.Key,
		DerpURL:    a.config.Tailscale.HeadscaleURL + "/derp",
	})
}

// DisconnectTailscale 断开 Tailscale 连接
func (a *ClientAuthAPI) DisconnectTailscale(c *gin.Context) {
	// 从 Authorization header 获取 token
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

	// 验证 Token
	token, err := jwt.Parse(parts[1], func(token *jwt.Token) (interface{}, error) {
		return []byte(a.config.Security.JWTSecret), nil
	})

	if err != nil || !token.Valid {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "Token 无效或已过期",
		})
		return
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "Token 格式错误",
		})
		return
	}

	clientID := int64(claims["client_id"].(float64))

	// 清除 Client 的 Tailscale IP
	var client model.Client
	if err := db.DB.First(&client, clientID).Error; err == nil {
		if client.TailscaleIP != "" && a.headscaleClient != nil {
			// 尝试从 Headscale 删除节点
			node, err := a.headscaleClient.GetNodeByIP(c.Request.Context(), client.TailscaleIP)
			if err == nil && node != nil {
				if err := a.headscaleClient.DeleteNode(c.Request.Context(), node.ID); err != nil {
					logger.Warnf("从 Headscale 删除节点失败: %v", err)
				}
			}
		}

		client.TailscaleIP = ""
		db.DB.Save(&client)
	}

	logger.Infof("Client %d 断开 Tailscale 连接", clientID)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "已断开 Tailscale 连接",
	})
}

// GetServicesV2 获取服务列表（Tailscale 版本）
func (a *ClientAuthAPI) GetServicesV2(c *gin.Context) {
	// 从 Authorization header 获取 token
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

	// 验证 Token
	token, err := jwt.Parse(parts[1], func(token *jwt.Token) (interface{}, error) {
		return []byte(a.config.Security.JWTSecret), nil
	})

	if err != nil || !token.Valid {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "Token 无效或已过期",
		})
		return
	}

	// 查询所有运行中的端口映射服务
	var services []model.ProxyService
	if err := db.DB.Preload("Agent").Where("status = ?", model.ProxyStatusRunning).Find(&services).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "查询服务失败",
		})
		return
	}

	// 构建响应
	type ServiceV2Info struct {
		ID          int64  `json:"id"`
		Name        string `json:"name"`
		AgentName   string `json:"agent_name"`
		TailscaleIP string `json:"tailscale_ip"`
		ListenPort  int    `json:"listen_port"`
		TargetAddr  string `json:"target_addr"`
		Status      string `json:"status"`
	}

	result := make([]ServiceV2Info, 0, len(services))
	for _, svc := range services {
		info := ServiceV2Info{
			ID:         svc.ID,
			Name:       svc.Name,
			ListenPort: svc.ListenPort,
			TargetAddr: svc.TargetAddr,
			Status:     svc.Status,
		}
		if svc.Agent != nil {
			info.AgentName = svc.Agent.AgentName
			info.TailscaleIP = svc.Agent.TailscaleIP
		}
		result = append(result, info)
	}

	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"services": result,
	})
}
