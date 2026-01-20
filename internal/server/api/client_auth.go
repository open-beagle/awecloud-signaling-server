package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/headscale"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

// ClientAuthAPI Client 认证 API
type ClientAuthAPI struct {
	config          *config.ServerConfig
	headscaleClient *headscale.Client
}

// NewClientAuthAPI 创建 ClientAuthAPI
func NewClientAuthAPI(cfg *config.ServerConfig) *ClientAuthAPI {
	api := &ClientAuthAPI{config: cfg}

	// 初始化 Headscale 客户端
	if cfg.Tailscale.HeadscaleURL != "" && cfg.Tailscale.HeadscaleAPIKey != "" {
		client, err := headscale.NewClient(headscale.Config{
			URL:    cfg.Tailscale.HeadscaleURL,
			APIKey: cfg.Tailscale.HeadscaleAPIKey,
		})
		if err != nil {
			logger.Warnf("初始化 Headscale 客户端失败: %v", err)
		} else {
			api.headscaleClient = client
		}
	}

	return api
}

// ClientAuthRequest Client 认证请求
type ClientAuthRequest struct {
	Name   string `json:"name" binding:"required"`
	Secret string `json:"secret" binding:"required"`
}

// ClientAuthResponse Client 认证响应
type ClientAuthResponse struct {
	Success   bool   `json:"success"`
	Token     string `json:"token,omitempty"`
	ExpiresIn int    `json:"expires_in,omitempty"`
	Message   string `json:"message,omitempty"`
}

// Auth Client 认证
func (a *ClientAuthAPI) Auth(c *gin.Context) {
	ctx := c.Request.Context()
	var req ClientAuthRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ClientAuthResponse{
			Success: false,
			Message: "请求参数错误",
		})
		return
	}

	// 查询 Client（role = client 的 User）
	var user model.User
	if err := db.DB.WithContext(ctx).Where("name = ? AND role = ?", req.Name, model.UserRoleClient).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, ClientAuthResponse{
			Success: false,
			Message: "用户名或密钥错误",
		})
		return
	}

	// 验证密钥
	if err := bcrypt.CompareHashAndPassword([]byte(user.SecretHash), []byte(req.Secret)); err != nil {
		c.JSON(http.StatusUnauthorized, ClientAuthResponse{
			Success: false,
			Message: "用户名或密钥错误",
		})
		return
	}

	// 生成 JWT Token
	expiresIn := a.config.Security.JWTExpireHours * 3600
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"client_id": user.ID,
		"exp":       time.Now().Add(time.Hour * time.Duration(a.config.Security.JWTExpireHours)).Unix(),
	})

	tokenString, err := token.SignedString([]byte(a.config.Security.JWTSecret))
	if err != nil {
		c.JSON(http.StatusInternalServerError, ClientAuthResponse{
			Success: false,
			Message: "生成 Token 失败",
		})
		return
	}

	c.JSON(http.StatusOK, ClientAuthResponse{
		Success:   true,
		Token:     tokenString,
		ExpiresIn: expiresIn,
	})
}

// GetTunnelConfig 获取隧道配置
func (a *ClientAuthAPI) GetTunnelConfig(c *gin.Context) {
	// 验证 Token
	if _, err := a.validateToken(c); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"server_url": a.config.Tailscale.HeadscalePublicURL,
	})
}

// ServiceInfo 服务信息
type ServiceInfo struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	AgentName  string `json:"agent_name"`
	SourceAddr string `json:"source_addr"`
}

// ServicesResponse 服务列表响应
type ServicesResponse struct {
	Success  bool          `json:"success"`
	Services []ServiceInfo `json:"services,omitempty"`
	Message  string        `json:"message,omitempty"`
}

// GetServices 获取服务列表
func (a *ClientAuthAPI) GetServices(c *gin.Context) {
	ctx := c.Request.Context()
	// 验证 Token
	if _, err := a.validateToken(c); err != nil {
		c.JSON(http.StatusUnauthorized, ServicesResponse{
			Success: false,
			Message: err.Error(),
		})
		return
	}

	// 查询所有启用的端口映射服务
	var proxyServices []model.ProxyService
	if err := db.DB.WithContext(ctx).Preload("User").Where("enabled = ?", true).Find(&proxyServices).Error; err != nil {
		c.JSON(http.StatusInternalServerError, ServicesResponse{
			Success: false,
			Message: "查询服务失败",
		})
		return
	}

	// 构建服务列表
	services := make([]ServiceInfo, 0, len(proxyServices))
	for _, svc := range proxyServices {
		info := ServiceInfo{
			ID:         svc.ID,
			Name:       svc.Name,
			SourceAddr: svc.SourceAddr,
		}
		if svc.User != nil {
			info.AgentName = svc.User.Name
		}
		services = append(services, info)
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
}

// GetTailscaleAuth 获取 Tailscale 认证信息
func (a *ClientAuthAPI) GetTailscaleAuth(c *gin.Context) {
	ctx := c.Request.Context()
	// 验证 Token
	userID, err := a.validateToken(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, TailscaleAuthResponse{
			Success: false,
			Message: err.Error(),
		})
		return
	}

	// 获取 User 信息（Client 角色）
	var user model.User
	if err := db.DB.WithContext(ctx).First(&user, userID).Error; err != nil {
		c.JSON(http.StatusNotFound, TailscaleAuthResponse{
			Success: false,
			Message: "Client 不存在",
		})
		return
	}

	// 检查 Headscale 客户端
	if a.headscaleClient == nil {
		c.JSON(http.StatusServiceUnavailable, TailscaleAuthResponse{
			Success: false,
			Message: "Tailscale 服务未配置",
		})
		return
	}

	// 为每个 Client 创建独立的 Headscale User
	// User 命名规则: desktop-{user.name}，参见 docs/design_headscale_integration.md
	userName := fmt.Sprintf("desktop-%s", user.Name)

	// 获取或创建 User
	hsUser, err := a.headscaleClient.GetOrCreateUser(ctx, userName)
	if err != nil {
		logger.Errorf("获取或创建 Headscale User 失败: %v", err)
		c.JSON(http.StatusInternalServerError, TailscaleAuthResponse{
			Success: false,
			Message: "创建用户失败",
		})
		return
	}

	// 构建 Tags 列表
	// 身份 Tag: tag:desktop-{user.name}，参见 docs/design_headscale_integration.md 第 10 节
	tags := []string{fmt.Sprintf("tag:desktop-%s", user.Name)}

	// 查询 User 所属的分组，添加分组 Tag
	var groupMembers []model.GroupMember
	if err := db.DB.WithContext(ctx).Preload("Group").Where("user_id = ?", userID).Find(&groupMembers).Error; err == nil {
		for _, gm := range groupMembers {
			if gm.Group != nil {
				// 分组 Tag: tag:desktop-group-{group.name}
				tags = append(tags, fmt.Sprintf("tag:desktop-group-%s", gm.Group.Name))
			}
		}
	}

	// 创建预认证密钥（带 Tags）
	authKeyExpiry := 24 * time.Hour
	authKey, err := a.headscaleClient.CreatePreAuthKeyWithTags(ctx, hsUser.Id, authKeyExpiry, true, tags)
	if err != nil {
		logger.Errorf("创建 Tailscale 预认证密钥失败: %v", err)
		c.JSON(http.StatusInternalServerError, TailscaleAuthResponse{
			Success: false,
			Message: "创建认证密钥失败",
		})
		return
	}

	logger.Infof("为 Client %d 创建 Tailscale 预认证密钥（User: %s）", userID, userName)

	c.JSON(http.StatusOK, TailscaleAuthResponse{
		Success:    true,
		ControlURL: a.config.Tailscale.HeadscalePublicURL,
		AuthKey:    authKey.Key,
	})
}

// DisconnectTailscale 断开 Tailscale 连接
func (a *ClientAuthAPI) DisconnectTailscale(c *gin.Context) {
	ctx := c.Request.Context()
	// 验证 Token
	userID, err := a.validateToken(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	// 获取 User 的所有 Desktop 设备（Node type = desktop）
	var nodes []model.Node
	if err := db.DB.WithContext(ctx).Where("user_id = ? AND type = ?", userID, model.NodeTypeDesktop).Find(&nodes).Error; err == nil {
		for _, n := range nodes {
			if n.ID > 0 && a.headscaleClient != nil {
				// 过期节点而不是删除
				if err := a.headscaleClient.ExpireNode(ctx, n.ID); err != nil {
					logger.Warnf("过期 Headscale 节点失败: %v", err)
				}
			}
		}
	}

	logger.Infof("Client %d 断开 Tailscale 连接", userID)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "已断开 Tailscale 连接",
	})
}

// GetServicesV2 获取服务列表（带 Agent IP）
func (a *ClientAuthAPI) GetServicesV2(c *gin.Context) {
	ctx := c.Request.Context()
	// 验证 Token
	if _, err := a.validateToken(c); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	// 查询所有启用的端口映射服务
	var services []model.ProxyService
	if err := db.DB.WithContext(ctx).Preload("User").Where("enabled = ?", true).Find(&services).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "查询服务失败",
		})
		return
	}

	// 构建响应
	type ServiceV2Info struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		AgentName  string `json:"agent_name"`
		AgentIP    string `json:"agent_ip"`
		SourceAddr string `json:"source_addr"`
		TargetAddr string `json:"target_addr"`
	}

	result := make([]ServiceV2Info, 0, len(services))
	for _, svc := range services {
		info := ServiceV2Info{
			ID:         svc.ID,
			Name:       svc.Name,
			SourceAddr: svc.SourceAddr,
			TargetAddr: svc.TargetAddr,
		}
		if svc.User != nil {
			info.AgentName = svc.User.Name
			// 获取 Agent 的 Node IP（需要查询 Node 表）
			var node model.Node
			if err := db.DB.WithContext(ctx).Where("user_id = ? AND type = ?", svc.UserID, model.NodeTypeAgent).First(&node).Error; err == nil {
				info.AgentIP = node.IP
			}
		}
		result = append(result, info)
	}

	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"services": result,
	})
}

// validateToken 验证 JWT Token，返回 user_id（client_id）
func (a *ClientAuthAPI) validateToken(c *gin.Context) (uint64, error) {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		return 0, fmt.Errorf("未提供认证信息")
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || parts[0] != "Bearer" {
		return 0, fmt.Errorf("认证格式错误")
	}

	token, err := jwt.Parse(parts[1], func(token *jwt.Token) (interface{}, error) {
		return []byte(a.config.Security.JWTSecret), nil
	})

	if err != nil || !token.Valid {
		return 0, fmt.Errorf("Token 无效或已过期")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return 0, fmt.Errorf("Token 格式错误")
	}

	userID := uint64(claims["client_id"].(float64))
	return userID, nil
}
