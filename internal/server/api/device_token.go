package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/auth"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

// DeviceTokenAPI 设备令牌 API
type DeviceTokenAPI struct {
	config *config.ServerConfig
}

// NewDeviceTokenAPI 创建 DeviceTokenAPI
func NewDeviceTokenAPI(cfg *config.ServerConfig) *DeviceTokenAPI {
	return &DeviceTokenAPI{config: cfg}
}

// LoginWithSecretRequest 使用 Secret 登录请求
type LoginWithSecretRequest struct {
	Name              string          `json:"name" binding:"required"`
	Secret            string          `json:"secret" binding:"required"`
	DeviceFingerprint string          `json:"device_fingerprint" binding:"required"`
	DeviceInfo        auth.DeviceInfo `json:"device_info" binding:"required"`
}

// LoginWithSecretResponse 使用 Secret 登录响应
type LoginWithSecretResponse struct {
	Success      bool   `json:"success"`
	DeviceToken  string `json:"device_token,omitempty"`
	ExpiresAt    string `json:"expires_at,omitempty"`
	JWTToken     string `json:"jwt_token,omitempty"`
	JWTExpiresIn int    `json:"jwt_expires_in,omitempty"`
	Message      string `json:"message,omitempty"`
}

// LoginWithTokenRequest 使用 Device Token 登录请求
type LoginWithTokenRequest struct {
	Name              string          `json:"name" binding:"required"`
	DeviceToken       string          `json:"device_token" binding:"required"`
	DeviceFingerprint string          `json:"device_fingerprint" binding:"required"`
	DeviceInfo        auth.DeviceInfo `json:"device_info"`
}

// LoginWithTokenResponse 使用 Device Token 登录响应
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

// DeviceTokenInfo 设备 Token 信息
type DeviceTokenInfo struct {
	DeviceToken string          `json:"device_token"`
	DeviceInfo  auth.DeviceInfo `json:"device_info"`
	CreatedAt   string          `json:"created_at"`
	LastUsedAt  string          `json:"last_used_at"`
	ExpiresAt   string          `json:"expires_at"`
	Revoked     bool            `json:"revoked"`
	IsCurrent   bool            `json:"is_current"`
}

// LoginWithSecret 使用 Secret 登录并获取 Device Token
func (a *DeviceTokenAPI) LoginWithSecret(c *gin.Context) {
	var req LoginWithSecretRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, LoginWithSecretResponse{
			Success: false,
			Message: "请求参数错误",
		})
		return
	}

	// 查询 Client
	var client model.Client
	if err := db.DB.Where("name = ?", req.Name).First(&client).Error; err != nil {
		c.JSON(http.StatusUnauthorized, LoginWithSecretResponse{
			Success: false,
			Message: "用户名或密钥错误",
		})
		return
	}

	// 验证密钥
	if err := bcrypt.CompareHashAndPassword([]byte(client.SecretHash), []byte(req.Secret)); err != nil {
		c.JSON(http.StatusUnauthorized, LoginWithSecretResponse{
			Success: false,
			Message: "用户名或密钥错误",
		})
		return
	}

	// 创建 Device Token
	deviceToken, err := auth.CreateDeviceToken(db.DB, int64(client.ID), req.DeviceInfo)
	if err != nil {
		logger.Warnf("创建 Device Token 失败: %v", err)
		c.JSON(http.StatusInternalServerError, LoginWithSecretResponse{
			Success: false,
			Message: "创建 Device Token 失败",
		})
		return
	}

	// 生成 JWT Token
	jwtExpiresIn := a.config.Security.JWTExpireHours * 3600
	jwtToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"client_id":    client.ID,
		"device_token": deviceToken.DeviceToken,
		"exp":          time.Now().Add(time.Hour * time.Duration(a.config.Security.JWTExpireHours)).Unix(),
	})

	jwtTokenString, err := jwtToken.SignedString([]byte(a.config.Security.JWTSecret))
	if err != nil {
		c.JSON(http.StatusInternalServerError, LoginWithSecretResponse{
			Success: false,
			Message: "生成 JWT Token 失败",
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

// LoginWithToken 使用 Device Token 登录
func (a *DeviceTokenAPI) LoginWithToken(c *gin.Context) {
	var req LoginWithTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, LoginWithTokenResponse{
			Success: false,
			Message: "请求参数错误",
		})
		return
	}

	// 查询 Client
	var client model.Client
	if err := db.DB.Where("name = ?", req.Name).First(&client).Error; err != nil {
		c.JSON(http.StatusUnauthorized, LoginWithTokenResponse{
			Success: false,
			Message: "用户名错误",
		})
		return
	}

	// 验证 Device Token
	deviceToken, err := auth.ValidateDeviceToken(db.DB, int64(client.ID), req.DeviceToken, req.DeviceInfo)
	if err != nil {
		logger.Warnf("验证 Device Token 失败: %v", err)
		c.JSON(http.StatusUnauthorized, LoginWithTokenResponse{
			Success: false,
			Message: err.Error(),
		})
		return
	}

	// 生成 JWT Token
	jwtExpiresIn := a.config.Security.JWTExpireHours * 3600
	jwtToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"client_id":    client.ID,
		"device_token": deviceToken.DeviceToken,
		"exp":          time.Now().Add(time.Hour * time.Duration(a.config.Security.JWTExpireHours)).Unix(),
	})

	jwtTokenString, err := jwtToken.SignedString([]byte(a.config.Security.JWTSecret))
	if err != nil {
		c.JSON(http.StatusInternalServerError, LoginWithTokenResponse{
			Success: false,
			Message: "生成 JWT Token 失败",
		})
		return
	}

	logger.Infof("Device Token 验证成功: client_id=%d", client.ID)

	c.JSON(http.StatusOK, LoginWithTokenResponse{
		Success:      true,
		JWTToken:     jwtTokenString,
		JWTExpiresIn: jwtExpiresIn,
	})
}

// ListDevices 列出用户已登录的设备
func (a *DeviceTokenAPI) ListDevices(c *gin.Context) {
	// 从 JWT 获取 client_id
	clientIDRaw, exists := c.Get("client_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, DeviceListResponse{
			Success: false,
			Message: "未认证",
		})
		return
	}

	// 转换 client_id 为 int64
	var clientID int64
	switch v := clientIDRaw.(type) {
	case float64:
		clientID = int64(v)
	case int64:
		clientID = v
	case uint64:
		clientID = int64(v)
	case int:
		clientID = int64(v)
	default:
		logger.Warnf("无效的 client_id 类型: %T", clientIDRaw)
		c.JSON(http.StatusInternalServerError, DeviceListResponse{
			Success: false,
			Message: "内部错误",
		})
		return
	}

	// 获取当前使用的 JWT token
	authHeader := c.GetHeader("Authorization")
	currentJWT := ""
	if strings.HasPrefix(authHeader, "Bearer ") {
		currentJWT = strings.TrimPrefix(authHeader, "Bearer ")
	}

	// 查询设备列表
	tokens, err := auth.ListDeviceTokens(db.DB, clientID)
	if err != nil {
		logger.Warnf("查询设备列表失败: %v", err)
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

		// 判断是否是当前设备
		isCurrent := false
		if currentJWT != "" {
			jwtToken, err := jwt.Parse(currentJWT, func(t *jwt.Token) (interface{}, error) {
				return []byte(a.config.Security.JWTSecret), nil
			})
			if err == nil && jwtToken.Valid {
				if claims, ok := jwtToken.Claims.(jwt.MapClaims); ok {
					if deviceToken, exists := claims["device_token"]; exists {
						isCurrent = (deviceToken == token.DeviceToken)
					}
				}
			}
		}

		devices = append(devices, DeviceTokenInfo{
			DeviceToken: token.DeviceToken,
			DeviceInfo:  deviceInfo,
			CreatedAt:   token.CreatedAt.Format(time.RFC3339),
			LastUsedAt:  token.LastUsedAt.Format(time.RFC3339),
			ExpiresAt:   token.ExpiresAt.Format(time.RFC3339),
			Revoked:     token.Revoked,
			IsCurrent:   isCurrent,
		})
	}

	c.JSON(http.StatusOK, DeviceListResponse{
		Success: true,
		Devices: devices,
	})
}

// OfflineDevice 让设备下线（撤销 Token）
func (a *DeviceTokenAPI) OfflineDevice(c *gin.Context) {
	deviceToken := c.Param("device_token")
	if deviceToken == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "缺少 device_token 参数",
		})
		return
	}

	// 从 JWT 获取 client_id
	clientIDRaw, exists := c.Get("client_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "未认证",
		})
		return
	}

	var clientID int64
	switch v := clientIDRaw.(type) {
	case float64:
		clientID = int64(v)
	case uint64:
		clientID = int64(v)
	default:
		clientID = int64(v.(int))
	}

	// 撤销 Token
	if err := auth.RevokeDeviceToken(db.DB, clientID, deviceToken); err != nil {
		logger.Warnf("撤销 Device Token 失败: %v", err)
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
			"message": "缺少 device_token 参数",
		})
		return
	}

	// 从 JWT 获取 client_id
	clientIDRaw, exists := c.Get("client_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "未认证",
		})
		return
	}

	var clientID int64
	switch v := clientIDRaw.(type) {
	case float64:
		clientID = int64(v)
	case uint64:
		clientID = int64(v)
	default:
		clientID = int64(v.(int))
	}

	// 删除 Token
	if err := auth.DeleteDeviceToken(db.DB, clientID, deviceToken); err != nil {
		logger.Warnf("删除 Device Token 失败: %v", err)
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
