package api

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/headscale"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

// AgentDeployAPI Agent 部署 API
type AgentDeployAPI struct {
	config   *config.ServerConfig
	hsClient *headscale.Client
}

// NewAgentDeployAPI 创建 AgentDeployAPI
func NewAgentDeployAPI(cfg *config.ServerConfig) *AgentDeployAPI {
	api := &AgentDeployAPI{config: cfg}

	if cfg.Tailscale.HeadscaleURL != "" && cfg.Tailscale.HeadscaleAPIKey != "" {
		client, err := headscale.NewClient(headscale.Config{
			URL:    cfg.Tailscale.HeadscaleURL,
			APIKey: cfg.Tailscale.HeadscaleAPIKey,
		})
		if err != nil {
			logger.Warnf("初始化 Headscale 客户端失败: %v", err)
		} else {
			api.hsClient = client
		}
	}

	return api
}

// CreateDeployTokenRequest 生成部署 Token 请求
type CreateDeployTokenRequest struct {
	DeviceName string `json:"device_name" binding:"required"` // 设备名称（必填）
}

// CreateDeployTokenResponse 生成部署 Token 响应
type CreateDeployTokenResponse struct {
	Token          string `json:"token"`
	ExpiresAt      string `json:"expires_at"`
	InstallCommand string `json:"install_command"`
}

// CreateDeployToken 生成部署 Token（支持 ID 或用户名）
func (a *AgentDeployAPI) CreateDeployToken(c *gin.Context) {
	ctx := c.Request.Context()

	// 获取 Agent 用户 ID 或用户名
	identifier := c.Param("id")

	var req CreateDeployTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("设备名称不能为空"))
		return
	}

	// 验证用户存在且为 Agent 角色
	var user model.User
	var err error

	// 尝试解析为 ID
	if id, parseErr := strconv.ParseUint(identifier, 10, 64); parseErr == nil {
		err = db.DB.WithContext(ctx).First(&user, id).Error
	} else {
		err = db.DB.WithContext(ctx).Where("name = ?", identifier).First(&user).Error
	}

	if err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("用户不存在"))
		return
	}
	if user.Role != model.UserRoleAgent {
		c.JSON(http.StatusBadRequest, NewErrorResponse("仅支持 Agent 用户"))
		return
	}

	// 生成 Token
	token, err := generateDeployToken(64)
	if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("生成 Token 失败"))
		return
	}

	// 获取管理员 ID
	adminID := getAdminIDFromContext(c)

	// 创建部署 Token 记录
	expiresAt := time.Now().Add(24 * time.Hour)
	deployToken := &model.AgentDeployToken{
		Token:      token,
		UserID:     user.ID,
		DeviceName: req.DeviceName,
		Status:     model.AgentDeployTokenStatusPending,
		CreatedBy:  uint64(adminID),
		ExpiresAt:  expiresAt,
	}

	if err := db.DB.WithContext(ctx).Create(deployToken).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("创建 Token 失败"))
		return
	}

	// 生成安装命令
	serverAddr := a.config.Server.PublicURL
	if serverAddr == "" {
		serverAddr = fmt.Sprintf("http://localhost:%d", a.config.Web.ListenPort)
	}
	installCommand := fmt.Sprintf(
		"curl -fsSL %s/api/v1/download/install.sh | sudo bash -s -- \\\n  -n %s \\\n  -d %s \\\n  -t %s \\\n  -s %s",
		serverAddr, user.Name, req.DeviceName, token, serverAddr,
	)

	logger.Infof("生成部署 Token: user_id=%d, user_name=%s, device_name=%s", user.ID, user.Name, req.DeviceName)

	c.JSON(http.StatusOK, NewSuccessResponse(CreateDeployTokenResponse{
		Token:          token,
		ExpiresAt:      expiresAt.Format(time.RFC3339),
		InstallCommand: installCommand,
	}))
}

// DeployTokenListItem 部署 Token 列表项
type DeployTokenListItem struct {
	ID                uint64     `json:"id"`
	DeviceName        string     `json:"device_name"`
	Status            string     `json:"status"`
	DeviceFingerprint string     `json:"device_fingerprint,omitempty"`
	CreatedBy         uint64     `json:"created_by"`
	CreatedByName     string     `json:"created_by_name,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	ExpiresAt         time.Time  `json:"expires_at"`
	BoundAt           *time.Time `json:"bound_at,omitempty"`
	LastUsedAt        *time.Time `json:"last_used_at,omitempty"`
}

// ListDeployTokens 查询部署 Token 历史（支持 ID 或用户名）
func (a *AgentDeployAPI) ListDeployTokens(c *gin.Context) {
	ctx := c.Request.Context()

	identifier := c.Param("id")

	// 查找用户
	var user model.User
	var err error

	// 尝试解析为 ID
	if id, parseErr := strconv.ParseUint(identifier, 10, 64); parseErr == nil {
		err = db.DB.WithContext(ctx).First(&user, id).Error
	} else {
		err = db.DB.WithContext(ctx).Where("name = ?", identifier).First(&user).Error
	}

	if err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("用户不存在"))
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))

	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}

	// 先更新过期状态
	db.DB.WithContext(ctx).Model(&model.AgentDeployToken{}).
		Where("user_id = ? AND status = ? AND expires_at < ?", user.ID, model.AgentDeployTokenStatusPending, time.Now()).
		Update("status", model.AgentDeployTokenStatusExpired)

	query := db.DB.WithContext(ctx).Model(&model.AgentDeployToken{}).
		Where("user_id = ?", user.ID).
		Preload("CreatedByAdmin")

	var total int64
	query.Count(&total)

	var tokens []model.AgentDeployToken
	offset := (page - 1) * size
	if err := query.Order("created_at DESC").Offset(offset).Limit(size).Find(&tokens).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询失败"))
		return
	}

	result := make([]DeployTokenListItem, len(tokens))
	for i, t := range tokens {
		item := DeployTokenListItem{
			ID:                t.ID,
			DeviceName:        t.DeviceName,
			Status:            string(t.Status),
			DeviceFingerprint: t.DeviceFingerprint,
			CreatedBy:         t.CreatedBy,
			CreatedAt:         t.CreatedAt,
			ExpiresAt:         t.ExpiresAt,
			BoundAt:           t.BoundAt,
			LastUsedAt:        t.LastUsedAt,
		}
		if t.CreatedByAdmin != nil {
			item.CreatedByName = t.CreatedByAdmin.Username
		}
		result[i] = item
	}

	c.JSON(http.StatusOK, NewPagedResponse(result, total, page, size))
}

// GetDeployCommand 获取部署命令（用于复制）
func (a *AgentDeployAPI) GetDeployCommand(c *gin.Context) {
	ctx := c.Request.Context()

	tokenID, err := strconv.ParseUint(c.Param("token_id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 Token ID"))
		return
	}

	var deployToken model.AgentDeployToken
	if err := db.DB.WithContext(ctx).Preload("User").First(&deployToken, tokenID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Token 不存在"))
		return
	}

	if deployToken.Status != model.AgentDeployTokenStatusPending {
		c.JSON(http.StatusBadRequest, NewErrorResponse("Token 已使用或已过期"))
		return
	}

	serverAddr := a.config.Server.PublicURL
	if serverAddr == "" {
		serverAddr = fmt.Sprintf("http://localhost:%d", a.config.Web.ListenPort)
	}

	userName := ""
	if deployToken.User != nil {
		userName = deployToken.User.Name
	}

	installCommand := fmt.Sprintf(
		"curl -fsSL %s/api/v1/download/install.sh | sudo bash -s -- \\\n  -n %s \\\n  -d %s \\\n  -t %s \\\n  -s %s",
		serverAddr, userName, deployToken.DeviceName, deployToken.Token, serverAddr,
	)

	c.JSON(http.StatusOK, NewSuccessResponse(map[string]string{
		"install_command": installCommand,
	}))
}

// RevokeDeployToken 撤销部署 Token
func (a *AgentDeployAPI) RevokeDeployToken(c *gin.Context) {
	ctx := c.Request.Context()

	tokenID, err := strconv.ParseUint(c.Param("token_id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 Token ID"))
		return
	}

	var deployToken model.AgentDeployToken
	if err := db.DB.WithContext(ctx).First(&deployToken, tokenID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Token 不存在"))
		return
	}

	if deployToken.Status != model.AgentDeployTokenStatusPending {
		c.JSON(http.StatusBadRequest, NewErrorResponse("仅可撤销待使用的 Token"))
		return
	}

	deployToken.Status = model.AgentDeployTokenStatusExpired
	if err := db.DB.WithContext(ctx).Save(&deployToken).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("撤销失败"))
		return
	}

	logger.Infof("撤销部署 Token: id=%d", tokenID)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("撤销成功", nil))
}

// AgentRegisterRequest Agent 注册请求
type AgentRegisterRequest struct {
	Token             string `json:"token" binding:"required"`
	DeviceFingerprint string `json:"device_fingerprint" binding:"required"`
}

// AgentRegisterResponse Agent 注册响应
type AgentRegisterResponse struct {
	Message      string                 `json:"message"`
	Config       map[string]interface{} `json:"config"`
	HeadscaleURL string                 `json:"headscale_url,omitempty"`
	AuthKey      string                 `json:"auth_key,omitempty"`
}

// Register Agent 注册（验证 Token 并绑定设备）
func (a *AgentDeployAPI) Register(c *gin.Context) {
	ctx := c.Request.Context()

	var req AgentRegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	// 查找 Token
	var deployToken model.AgentDeployToken
	if err := db.DB.WithContext(ctx).Preload("User").Where("token = ?", req.Token).First(&deployToken).Error; err != nil {
		c.JSON(http.StatusUnauthorized, NewErrorResponse("无效的 Token"))
		return
	}

	// 检查 Token 是否可用
	canUse, errMsg := deployToken.CanUse(req.DeviceFingerprint)
	if !canUse {
		c.JSON(http.StatusForbidden, NewErrorResponse(errMsg))
		return
	}

	now := time.Now()
	isFirstUse := deployToken.Status == model.AgentDeployTokenStatusPending

	// 首次使用：绑定设备指纹
	if isFirstUse {
		deployToken.Status = model.AgentDeployTokenStatusBound
		deployToken.DeviceFingerprint = req.DeviceFingerprint
		deployToken.BoundAt = &now
	}
	deployToken.LastUsedAt = &now

	if err := db.DB.WithContext(ctx).Save(&deployToken).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("更新 Token 失败"))
		return
	}

	// 生成 Headscale AuthKey
	var authKey string
	var headscaleURL string
	if a.hsClient != nil && deployToken.User != nil {
		headscaleURL = a.config.Tailscale.HeadscaleURL
		userName := fmt.Sprintf("agent-%s", deployToken.User.Name)

		// 获取用户 ID
		user, err := a.hsClient.GetUserByName(ctx, userName)
		if err != nil {
			logger.Warnf("获取 Headscale 用户失败: %v", err)
		} else if user != nil {
			key, err := a.hsClient.CreatePreAuthKey(ctx, user.Id, 24*time.Hour, false)
			if err != nil {
				logger.Warnf("创建 Headscale AuthKey 失败: %v", err)
			} else {
				authKey = key.Key
			}
		}
	}

	// 构建配置
	agentConfig := map[string]interface{}{
		"agent": map[string]interface{}{
			"name":   deployToken.User.Name,
			"device": deployToken.DeviceName,
		},
		"server": map[string]interface{}{
			"address": a.config.Server.PublicURL,
		},
	}

	message := "部署成功，Token 已绑定设备"
	if !isFirstUse {
		message = "升级成功"
	}

	logger.Infof("Agent 注册成功: user=%s, device=%s, first_use=%v", deployToken.User.Name, deployToken.DeviceName, isFirstUse)

	c.JSON(http.StatusOK, NewSuccessResponse(AgentRegisterResponse{
		Message:      message,
		Config:       agentConfig,
		HeadscaleURL: headscaleURL,
		AuthKey:      authKey,
	}))
}

// generateDeployToken 生成部署 Token
func generateDeployToken(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
