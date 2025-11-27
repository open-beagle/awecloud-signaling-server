package api

import (
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/auth"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

type DeviceTokenAPI struct {
	config *config.ServerConfig
}

func NewDeviceTokenAPI(cfg *config.ServerConfig) *DeviceTokenAPI {
	return &DeviceTokenAPI{config: cfg}
}

// LoginWithSecretRequest 使用Secret登录请求
type LoginWithSecretRequest struct {
	ClientID          string          `json:"client_id" binding:"required"`
	ClientSecret      string          `json:"client_secret" binding:"required"`
	DeviceFingerprint string          `json:"device_fingerprint" binding:"required"`
	DeviceInfo        auth.DeviceInfo `json:"device_info" binding:"required"`
}

// LoginWithSecretResponse 使用Secret登录响应
type LoginWithSecretResponse struct {
	Success      bool   `json:"success"`
	DeviceToken  string `json:"device_token,omitempty"`
	ExpiresAt    string `json:"expires_at,omitempty"`
	JWTToken     string `json:"jwt_token,omitempty"`
	JWTExpiresIn int    `json:"jwt_expires_in,omitempty"`
	Message      string `json:"message,omitempty"`
}

// LoginWithTokenRequest 使用Device Token登录请求
type LoginWithTokenRequest struct {
	ClientID          string `json:"client_id" binding:"required"`
	DeviceToken       string `json:"device_token" binding:"required"`
	DeviceFingerprint string `json:"device_fingerprint" binding:"required"`
}

// LoginWithTokenResponse 使用Device Token登录响应
type LoginWithTokenResponse struct {
	Success      bool   `json:"success"`
	JWTToken     string `json:"jwt_token,omitempty"`
	JWTExpiresIn int    `json:"jwt_expires_in,omitempty"`
	Message      string `json:"message,omitempty"`
}

// DeviceListResponse 设备列表响应
type DeviceListResponse struct {
	Success bool              `json:"success"`
	Devices []DeviceTokenInfo `json:"devices,omitempty"`
	Message string            `json:"message,omitempty"`
}

// DeviceTokenInfo 设备Token信息
type DeviceTokenInfo struct {
	DeviceToken string          `json:"device_token"`
	DeviceInfo  auth.DeviceInfo `json:"device_info"`
	CreatedAt   string          `json:"created_at"`
	LastUsedAt  string          `json:"last_used_at"`
	ExpiresAt   string          `json:"expires_at"`
	Revoked     bool            `json:"revoked"`
	IsCurrent   bool            `json:"is_current"`
}

// LoginWithSecret 使用Secret登录并获取Device Token
func (a *DeviceTokenAPI) LoginWithSecret(c *gin.Context) {
	var req LoginWithSecretRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, LoginWithSecretResponse{
			Success: false,
			Message: "请求参数错误",
		})
		return
	}

	// 查询Client
	var client model.Client
	if err := db.DB.Where("client_id = ?", req.ClientID).First(&client).Error; err != nil {
		c.JSON(http.StatusUnauthorized, LoginWithSecretResponse{
			Success: false,
			Message: "Client ID或Secret错误",
		})
		return
	}

	// 验证Secret
	if client.ClientSecret != req.ClientSecret {
		c.JSON(http.StatusUnauthorized, LoginWithSecretResponse{
			Success: false,
			Message: "Client ID或Secret错误",
		})
		return
	}

	// 检查状态
	if !client.Enabled {
		c.JSON(http.StatusForbidden, LoginWithSecretResponse{
			Success: false,
			Message: "Client已被禁用",
		})
		return
	}

	// 创建Device Token
	deviceToken, err := auth.CreateDeviceToken(db.DB, client.ID, req.DeviceInfo)
	if err != nil {
		log.Printf("创建Device Token失败: %v", err)
		c.JSON(http.StatusInternalServerError, LoginWithSecretResponse{
			Success: false,
			Message: "创建Device Token失败",
		})
		return
	}

	// 生成JWT Token
	jwtExpiresIn := a.config.Security.JWTExpireHours * 3600
	jwtToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"client_id": client.ID,
		"exp":       time.Now().Add(time.Hour * time.Duration(a.config.Security.JWTExpireHours)).Unix(),
	})

	jwtTokenString, err := jwtToken.SignedString([]byte(a.config.Security.JWTSecret))
	if err != nil {
		c.JSON(http.StatusInternalServerError, LoginWithSecretResponse{
			Success: false,
			Message: "生成JWT Token失败",
		})
		return
	}

	c.JSON(http.StatusOK, LoginWithSecretResponse{
		Success:      true,
		DeviceToken:  deviceToken.DeviceToken,
		ExpiresAt:    deviceToken.ExpiresAt.Format(time.RFC3339),
		JWTToken:     jwtTokenString,
		JWTExpiresIn: jwtExpiresIn,
	})
}

// LoginWithToken 使用Device Token登录
func (a *DeviceTokenAPI) LoginWithToken(c *gin.Context) {
	var req LoginWithTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, LoginWithTokenResponse{
			Success: false,
			Message: "请求参数错误",
		})
		return
	}

	// 查询Client
	var client model.Client
	if err := db.DB.Where("client_id = ?", req.ClientID).First(&client).Error; err != nil {
		c.JSON(http.StatusUnauthorized, LoginWithTokenResponse{
			Success: false,
			Message: "Client ID错误",
		})
		return
	}

	// 检查状态
	if !client.Enabled {
		c.JSON(http.StatusForbidden, LoginWithTokenResponse{
			Success: false,
			Message: "Client已被禁用",
		})
		return
	}

	// 解析设备信息（从fingerprint推导，实际应该从请求中获取完整的DeviceInfo）
	// 这里简化处理，实际应该要求客户端提供完整的DeviceInfo
	deviceInfo := auth.DeviceInfo{
		// 注意：这里需要客户端提供完整的设备信息
		// 暂时使用空结构，后续需要修改API设计
	}

	// 验证Device Token
	deviceToken, err := auth.ValidateDeviceToken(db.DB, client.ID, req.DeviceToken, deviceInfo)
	if err != nil {
		log.Printf("验证Device Token失败: %v", err)
		c.JSON(http.StatusUnauthorized, LoginWithTokenResponse{
			Success: false,
			Message: err.Error(),
		})
		return
	}

	// 生成JWT Token
	jwtExpiresIn := a.config.Security.JWTExpireHours * 3600
	jwtToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"client_id": client.ID,
		"exp":       time.Now().Add(time.Hour * time.Duration(a.config.Security.JWTExpireHours)).Unix(),
	})

	jwtTokenString, err := jwtToken.SignedString([]byte(a.config.Security.JWTSecret))
	if err != nil {
		c.JSON(http.StatusInternalServerError, LoginWithTokenResponse{
			Success: false,
			Message: "生成JWT Token失败",
		})
		return
	}

	log.Printf("Device Token验证成功: client_id=%d, device_token=%s", client.ID, deviceToken.DeviceToken)

	c.JSON(http.StatusOK, LoginWithTokenResponse{
		Success:      true,
		JWTToken:     jwtTokenString,
		JWTExpiresIn: jwtExpiresIn,
	})
}

// ListDevices 列出用户已登录的设备
func (a *DeviceTokenAPI) ListDevices(c *gin.Context) {
	// 从JWT获取client_id
	clientID, exists := c.Get("client_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, DeviceListResponse{
			Success: false,
			Message: "未认证",
		})
		return
	}

	// 获取当前使用的token（如果有）
	currentToken := c.GetHeader("X-Device-Token")

	// 查询设备列表
	tokens, err := auth.ListDeviceTokens(db.DB, int64(clientID.(float64)))
	if err != nil {
		log.Printf("查询设备列表失败: %v", err)
		c.JSON(http.StatusInternalServerError, DeviceListResponse{
			Success: false,
			Message: "查询设备列表失败",
		})
		return
	}

	// 构建响应
	devices := make([]DeviceTokenInfo, 0, len(tokens))
	for _, token := range tokens {
		deviceInfo, _ := auth.DeviceInfoFromJSON(token.DeviceInfo)
		devices = append(devices, DeviceTokenInfo{
			DeviceToken: token.DeviceToken,
			DeviceInfo:  deviceInfo,
			CreatedAt:   token.CreatedAt.Format(time.RFC3339),
			LastUsedAt:  token.LastUsedAt.Format(time.RFC3339),
			ExpiresAt:   token.ExpiresAt.Format(time.RFC3339),
			Revoked:     token.Revoked,
			IsCurrent:   token.DeviceToken == currentToken,
		})
	}

	c.JSON(http.StatusOK, DeviceListResponse{
		Success: true,
		Devices: devices,
	})
}

// OfflineDevice 让设备下线（撤销Token）
func (a *DeviceTokenAPI) OfflineDevice(c *gin.Context) {
	deviceToken := c.Param("device_token")
	if deviceToken == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "缺少device_token参数",
		})
		return
	}

	// 从JWT获取client_id
	clientID, exists := c.Get("client_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "未认证",
		})
		return
	}

	// 撤销Token
	if err := auth.RevokeDeviceToken(db.DB, int64(clientID.(float64)), deviceToken); err != nil {
		log.Printf("撤销Device Token失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "设备已下线",
	})
}

// DeleteDevice 删除设备记录
func (a *DeviceTokenAPI) DeleteDevice(c *gin.Context) {
	deviceToken := c.Param("device_token")
	if deviceToken == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "缺少device_token参数",
		})
		return
	}

	// 从JWT获取client_id
	clientID, exists := c.Get("client_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "未认证",
		})
		return
	}

	// 删除Token
	if err := auth.DeleteDeviceToken(db.DB, int64(clientID.(float64)), deviceToken); err != nil {
		log.Printf("删除Device Token失败: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "设备记录已删除",
	})
}
