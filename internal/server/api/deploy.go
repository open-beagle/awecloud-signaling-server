package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/headscale"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

// DeployAPI 统一部署 Token API（合并原 AgentDeployAPI 和 ClientTokenAPI）
type DeployAPI struct {
	config   *config.ServerConfig
	hsClient *headscale.Client
}

// NewDeployAPI 创建 DeployAPI
func NewDeployAPI(cfg *config.ServerConfig) *DeployAPI {
	api := &DeployAPI{config: cfg}

	if cfg.Tailscale.HeadscaleURL != "" && cfg.Tailscale.HeadscaleAPIKey != "" {
		client, err := headscale.NewClient(headscale.Config{
			URL:    cfg.Tailscale.HeadscaleURL,
			APIKey: cfg.Tailscale.HeadscaleAPIKey,
		})
		if err != nil {
			logger.Warnf("初始化 DeployAPI Headscale 客户端失败: %v", err)
		} else {
			api.hsClient = client
		}
	}

	return api
}

// --- 请求/响应结构 ---

// CreateDeployTokenRequest 创建部署 Token 请求
type CreateDeployTokenRequest struct {
	Name string `json:"name" binding:"required"` // Token 名称/备注
}

// CreateDeployTokenResponse 创建部署 Token 响应
type CreateDeployTokenResponse struct {
	Token          string  `json:"token"`                     // Token
	Name           string  `json:"name"`                      // Token 名称
	ExpiresAt      *string `json:"expires_at,omitempty"`      // 过期时间（Agent 有，Client 无）
	InstallCommand string  `json:"install_command,omitempty"` // 安装命令（Agent 专用）
	EnvConfig      string  `json:"env_config,omitempty"`      // 环境变量配置（Client 专用）
}

// DeployTokenListItem 部署 Token 列表项
type DeployTokenListItem struct {
	ID                uint64     `json:"id"`
	Name              string     `json:"name"`
	Status            string     `json:"status"`
	DeviceFingerprint string     `json:"device_fingerprint,omitempty"`
	SSHEnabled        bool       `json:"ssh_enabled"`
	CreatedBy         uint64     `json:"created_by"`
	CreatedByName     string     `json:"created_by_name,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	ExpiresAt         *time.Time `json:"expires_at,omitempty"`
	BoundAt           *time.Time `json:"bound_at,omitempty"`
	LastUsedAt        *time.Time `json:"last_used_at,omitempty"`
}

// RegisterRequest 统一注册请求
type RegisterRequest struct {
	Token             string `json:"token" binding:"required"`              // 部署 Token
	DeviceFingerprint string `json:"device_fingerprint" binding:"required"` // 设备指纹（SHA256(hostname)）
}

// RegisterResponse 统一注册响应
type RegisterResponse struct {
	Message      string                 `json:"message"`
	UserRole     string                 `json:"user_role"`               // 用户角色：agent / client
	UserID       uint64                 `json:"user_id"`                 // 用户 ID
	Config       map[string]interface{} `json:"config,omitempty"`        // 配置信息
	HeadscaleURL string                 `json:"headscale_url,omitempty"` // Headscale 地址
	AuthKey      string                 `json:"auth_key,omitempty"`      // Headscale 认证密钥
	UserName     string                 `json:"user_name,omitempty"`     // 用户名
	DeviceName   string                 `json:"device_name,omitempty"`   // 设备名称（Node.Name，用于域名注册）
}

// --- Token 管理 API ---

// CreateDeployToken 创建部署 Token（支持 ID 或用户名）
func (a *DeployAPI) CreateDeployToken(c *gin.Context) {
	ctx := c.Request.Context()

	identifier := c.Param("id")

	var req CreateDeployTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("名称不能为空"))
		return
	}

	// 查找用户（支持 ID 或用户名）
	var user model.User
	var err error
	if id, parseErr := strconv.ParseUint(identifier, 10, 64); parseErr == nil {
		err = db.DB.WithContext(ctx).First(&user, id).Error
	} else {
		err = db.DB.WithContext(ctx).Where("name = ?", identifier).First(&user).Error
	}
	if err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("用户不存在"))
		return
	}
	if user.Role != model.UserRoleClient {
		c.JSON(http.StatusConflict, NewErrorResponse("Agent 部署 Token 请从资源方技术资源管理"))
		return
	}

	// 生成 Token
	token, err := generateDeployToken(64)
	if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("生成 Token 失败"))
		return
	}

	adminID := getAdminIDFromContext(c)

	deployToken := &model.DeployToken{
		Token:     token,
		UserID:    user.ID,
		Name:      req.Name,
		Status:    model.DeployTokenStatusPending,
		CreatedBy: uint64(adminID),
		ExpiresAt: nil,
	}

	if err := db.DB.WithContext(ctx).Create(deployToken).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("创建 Token 失败"))
		return
	}

	// 构建响应
	serverAddr := a.getServerAddr(c)

	resp := CreateDeployTokenResponse{
		Token: token,
		Name:  req.Name,
	}

	resp.InstallCommand = fmt.Sprintf(
		"curl -fsSL %s/api/v1/download/install_signal.sh | bash -s -- -t %s -s %s",
		serverAddr, token, serverAddr,
	)
	resp.EnvConfig = "SIGNAL_TOKEN=" + token + "\n" +
		"SIGNAL_SERVER=" + serverAddr

	logger.Infof("创建部署 Token: user_id=%d, user_name=%s, role=%s, name=%s", user.ID, user.Name, user.Role, req.Name)

	c.JSON(http.StatusOK, NewSuccessResponse(resp))
}

// ListDeployTokens 查询部署 Token 列表（支持 ID 或用户名）
func (a *DeployAPI) ListDeployTokens(c *gin.Context) {
	ctx := c.Request.Context()

	identifier := c.Param("id")

	// 查找用户
	var user model.User
	var err error
	if id, parseErr := strconv.ParseUint(identifier, 10, 64); parseErr == nil {
		err = db.DB.WithContext(ctx).First(&user, id).Error
	} else {
		err = db.DB.WithContext(ctx).Where("name = ?", identifier).First(&user).Error
	}
	if err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("用户不存在"))
		return
	}
	if user.Role != model.UserRoleClient {
		c.JSON(http.StatusConflict, NewErrorResponse("Agent 部署 Token 请从资源方技术资源管理"))
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

	// 更新过期状态（仅对有过期时间的 pending Token）
	db.DB.WithContext(ctx).Model(&model.DeployToken{}).
		Where("user_id = ? AND status = ? AND expires_at IS NOT NULL AND expires_at < ?",
			user.ID, model.DeployTokenStatusPending, time.Now()).
		Update("status", model.DeployTokenStatusRevoked)

	query := db.DB.WithContext(ctx).Model(&model.DeployToken{}).
		Where("user_id = ?", user.ID)

	var total int64
	query.Count(&total)

	var tokens []model.DeployToken
	offset := (page - 1) * size
	if err := query.Order("created_at DESC").Offset(offset).Limit(size).Find(&tokens).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询失败"))
		return
	}

	// 填充创建人名称
	result := make([]DeployTokenListItem, len(tokens))
	for i, t := range tokens {
		item := DeployTokenListItem{
			ID:                t.ID,
			Name:              t.Name,
			Status:            string(t.Status),
			DeviceFingerprint: t.DeviceFingerprint,
			SSHEnabled:        t.SSHEnabled,
			CreatedBy:         t.CreatedBy,
			CreatedAt:         t.CreatedAt,
			ExpiresAt:         t.ExpiresAt,
			BoundAt:           t.BoundAt,
			LastUsedAt:        t.LastUsedAt,
		}
		var admin model.Admin
		if err := db.DB.WithContext(ctx).First(&admin, t.CreatedBy).Error; err == nil {
			item.CreatedByName = admin.Username
		}
		result[i] = item
	}

	c.JSON(http.StatusOK, NewPagedResponse(result, total, page, size))
}

// GetDeployCommand 获取部署命令（用于复制）
func (a *DeployAPI) GetDeployCommand(c *gin.Context) {
	ctx := c.Request.Context()

	tokenID, err := strconv.ParseUint(c.Param("token_id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 Token ID"))
		return
	}

	var deployToken model.DeployToken
	if err := db.DB.WithContext(ctx).Preload("User").First(&deployToken, tokenID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Token 不存在"))
		return
	}
	if deployToken.User == nil || deployToken.User.Role != model.UserRoleClient {
		c.JSON(http.StatusConflict, NewErrorResponse("Agent 部署 Token 请从资源方技术资源管理"))
		return
	}

	serverAddr := a.getServerAddr(c)

	result := map[string]string{}

	result["install_command"] = fmt.Sprintf(
		"curl -fsSL %s/api/v1/download/install_signal.sh | bash -s -- -t %s -s %s",
		serverAddr, deployToken.Token, serverAddr,
	)
	result["env_config"] = "SIGNAL_TOKEN=" + deployToken.Token + "\n" +
		"SIGNAL_SERVER=" + serverAddr

	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

// RevokeDeployToken 撤销/删除部署 Token
func (a *DeployAPI) RevokeDeployToken(c *gin.Context) {
	ctx := c.Request.Context()

	tokenID, err := strconv.ParseUint(c.Param("token_id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 Token ID"))
		return
	}

	var deployToken model.DeployToken
	if err := db.DB.WithContext(ctx).Preload("User").First(&deployToken, tokenID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Token 不存在"))
		return
	}
	if deployToken.User == nil || deployToken.User.Role != model.UserRoleClient {
		c.JSON(http.StatusConflict, NewErrorResponse("Agent 部署 Token 请从资源方技术资源管理"))
		return
	}

	// 如果 Token 已经被撤销或过期,直接删除
	if deployToken.Status == model.DeployTokenStatusRevoked || deployToken.IsExpired() {
		if err := db.DB.WithContext(ctx).Delete(&deployToken).Error; err != nil {
			c.JSON(http.StatusInternalServerError, NewErrorResponse("删除失败"))
			return
		}
		logger.Infof("删除失效的部署 Token: id=%d, status=%s", tokenID, deployToken.Status)
		c.JSON(http.StatusOK, NewSuccessMessageResponse("删除成功", nil))
		return
	}

	// 否则先撤销
	deployToken.Revoke()
	if err := db.DB.WithContext(ctx).Save(&deployToken).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("撤销失败"))
		return
	}

	logger.Infof("撤销部署 Token: id=%d", tokenID)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("撤销成功", nil))
}

// --- 统一注册 API ---

// Register 统一注册接口（POST /api/v1/register）
// 根据 User.Role 分支 Headscale 逻辑：
//   - Agent: Headscale 用户 agent-{name}，无 Tag
//   - Client: Headscale 用户 client-{name}，带 Tag
func (a *DeployAPI) Register(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	ctx := c.Request.Context()
	var resourceToken model.TechnicalResourceDeployToken
	if err := db.DB.WithContext(ctx).Where("token = ?", req.Token).First(&resourceToken).Error; err == nil {
		a.registerTechnicalResource(c, req, &resourceToken)
		return
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询部署 Token 失败"))
		return
	}
	// 资源 Token 未命中时继续兼容 User 所属的 Desktop Token。
	var deployToken model.DeployToken
	if err := db.DB.WithContext(ctx).Where("token = ?", req.Token).First(&deployToken).Error; err != nil {
		c.JSON(http.StatusUnauthorized, NewErrorResponse("无效的 Token"))
		return
	}

	// 检查 Token 是否可用
	canUse, errMsg := deployToken.CanUse(req.DeviceFingerprint)
	if !canUse {
		c.JSON(http.StatusForbidden, NewErrorResponse(errMsg))
		return
	}

	// 获取关联用户
	var user model.User
	if err := db.DB.WithContext(ctx).First(&user, deployToken.UserID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("用户不存在"))
		return
	}

	// 检查用户是否启用
	if !user.Enabled {
		c.JSON(http.StatusForbidden, NewErrorResponse("用户已禁用"))
		return
	}
	if user.Role != model.UserRoleClient {
		c.JSON(http.StatusForbidden, NewErrorResponse("旧 Agent Token 已停止准入，请重新生成资源 Token"))
		return
	}

	isFirstUse := deployToken.Status == model.DeployTokenStatusPending

	// 绑定设备
	if isFirstUse {
		deployToken.Bind(req.DeviceFingerprint)
	} else {
		deployToken.UpdateLastUsed()
	}

	if err := db.DB.WithContext(ctx).Save(&deployToken).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("更新 Token 失败"))
		return
	}

	// 创建或更新 Node（使用 DeployToken 名称）
	var node model.Node
	nodeType := model.NodeTypeAgent
	if user.Role == model.UserRoleClient {
		nodeType = model.NodeTypeDesktop
	}

	// 使用 DeployToken 名称作为 Node 名称
	nodeName := deployToken.Name

	if err := db.DB.WithContext(ctx).Where("user_id = ? AND type = ?", user.ID, nodeType).First(&node).Error; err != nil {
		// Node 不存在，创建新 Node
		node = model.Node{
			UserID: user.ID,
			Name:   nodeName,
			Type:   nodeType,
		}
		if err := db.DB.WithContext(ctx).Create(&node).Error; err != nil {
			logger.Errorf("创建 Node 失败: user_id=%d, name=%s, err=%v", user.ID, nodeName, err)
		} else {
			logger.Infof("注册时创建 Node: user_id=%d, name=%s, type=%s", user.ID, nodeName, nodeType)
		}
	} else {
		// Node 已存在，更新名称（使用 DeployToken 名称）
		if node.Name != nodeName {
			node.Name = nodeName
			if err := db.DB.WithContext(ctx).Save(&node).Error; err != nil {
				logger.Errorf("更新 Node 名称失败: node_id=%d, new_name=%s, err=%v", node.ID, nodeName, err)
			} else {
				logger.Infof("注册时更新 Node 名称: node_id=%d, old_name=%s, new_name=%s", node.ID, node.Name, nodeName)
			}
		}
	}

	// 根据 User.Role 分支 Headscale 逻辑
	var authKey string
	var headscaleURL string

	if a.hsClient != nil {
		headscaleURL = a.config.Tailscale.HeadscalePublicURL
		if headscaleURL == "" {
			headscaleURL = a.config.Tailscale.HeadscaleURL
		}

		switch user.Role {
		case model.UserRoleAgent:
			// Agent: Headscale 用户 agent-{name}，无 Tag
			authKey = a.createAgentAuthKey(ctx, user.Name)

		case model.UserRoleClient:
			// Client: Headscale 用户 client-{name}，带 Tag
			authKey = a.createClientAuthKey(ctx, &user)
		}
	}

	// 构建响应
	resp := RegisterResponse{
		UserRole:     string(user.Role),
		UserID:       user.ID,
		HeadscaleURL: headscaleURL,
		AuthKey:      authKey,
		UserName:     user.Name,
		DeviceName:   node.Name, // 返回 Node.Name（即 DeployToken.Name）供 Agent 使用
	}

	switch user.Role {
	case model.UserRoleAgent:
		resp.Message = "Agent 注册成功"
		resp.Config = map[string]interface{}{
			"server": map[string]interface{}{
				"address": a.config.Server.PublicURL,
			},
		}
		if !isFirstUse {
			resp.Message = "Agent 升级成功"
		}

	case model.UserRoleClient:
		resp.Message = "Client 注册成功"
		if !isFirstUse {
			resp.Message = "Client 重连成功"
		}
	}

	logger.Infof("统一注册成功: user=%s, role=%s, token=%s, first_use=%v",
		user.Name, user.Role, deployToken.Name, isFirstUse)

	c.JSON(http.StatusOK, NewSuccessResponse(resp))
}

func (a *DeployAPI) registerTechnicalResource(c *gin.Context, req RegisterRequest, token *model.TechnicalResourceDeployToken) {
	ctx := c.Request.Context()
	now := time.Now().UTC()
	if !token.CanConsume(now) {
		c.JSON(http.StatusForbidden, NewErrorResponse("Token 已消费、撤销或过期，请重新生成"))
		return
	}
	var resource model.TechnicalResource
	var user model.User
	var node model.Node
	err := db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id = ? AND status = ?", token.ID, model.TechnicalResourceDeployTokenPending).First(token).Error; err != nil {
			return err
		}
		if !token.CanConsume(now) {
			return gorm.ErrInvalidData
		}
		if err := tx.Where("id = ? AND type = ? AND lifecycle_state IN ?", token.TechnicalResourceID, model.TechnicalResourceAgent, []model.TechnicalResourceLifecycleState{model.TechnicalResourcePending, model.TechnicalResourceRegistered}).First(&resource).Error; err != nil {
			return err
		}
		if resource.RuntimeUserID != 0 && resource.RuntimeUserID != token.RuntimeUserID {
			return gorm.ErrInvalidData
		}
		if err := tx.Where("id = ? AND role = ? AND enabled = ?", token.RuntimeUserID, model.UserRoleAgent, true).First(&user).Error; err != nil {
			return err
		}
		err := tx.Where("user_id = ? AND type = ?", user.ID, model.NodeTypeAgent).First(&node).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			node = model.Node{UserID: user.ID, Name: token.Name, Type: model.NodeTypeAgent}
			if err := tx.Create(&node).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else if node.Name != token.Name {
			if err := tx.Model(&node).Update("name", token.Name).Error; err != nil {
				return err
			}
			node.Name = token.Name
		}
		var existingBinding model.TechnicalResourceBinding
		bindingErr := tx.Where("source_type = ? AND source_id = ?", model.TechnicalResourceBindingLegacyNode, strconv.FormatUint(node.ID, 10)).First(&existingBinding).Error
		if bindingErr != nil && !errors.Is(bindingErr, gorm.ErrRecordNotFound) {
			return bindingErr
		}
		if resource.LifecycleState == model.TechnicalResourceRegistered {
			if bindingErr != nil || existingBinding.TechnicalResourceID != resource.ID || !existingBinding.Enabled {
				return gorm.ErrDuplicatedKey
			}
		} else if bindingErr == nil {
			return gorm.ErrDuplicatedKey
		}
		if resource.LifecycleState == model.TechnicalResourcePending {
			binding := model.TechnicalResourceBinding{
				ID: uuid.NewString(), TechnicalResourceID: resource.ID, SourceType: model.TechnicalResourceBindingLegacyNode,
				SourceID: strconv.FormatUint(node.ID, 10), CredentialRevision: resource.CredentialRevision, Enabled: true,
				BoundByUserID: token.CreatedByUserID, Reason: "resource deploy token consumed", RowVersion: 1,
			}
			if err := tx.Create(&binding).Error; err != nil {
				return err
			}
			updated := tx.Model(&model.TechnicalResource{}).Where("id = ? AND lifecycle_state = ? AND row_version = ?", resource.ID, model.TechnicalResourcePending, resource.RowVersion).
				Updates(map[string]any{"runtime_user_id": user.ID, "lifecycle_state": model.TechnicalResourceRegistered, "row_version": gorm.Expr("row_version + 1")})
			if updated.Error != nil {
				return updated.Error
			}
			if updated.RowsAffected != 1 {
				return gorm.ErrInvalidTransaction
			}
		}
		consumed := tx.Model(&model.TechnicalResourceDeployToken{}).Where("id = ? AND status = ?", token.ID, model.TechnicalResourceDeployTokenPending).
			Updates(map[string]any{"status": model.TechnicalResourceDeployTokenConsumed, "device_fingerprint": req.DeviceFingerprint, "consumed_at": now})
		if consumed.Error != nil {
			return consumed.Error
		}
		if consumed.RowsAffected != 1 {
			return gorm.ErrInvalidTransaction
		}
		return nil
	})
	if err != nil {
		status := http.StatusConflict
		if errors.Is(err, gorm.ErrRecordNotFound) {
			status = http.StatusForbidden
		}
		c.JSON(status, NewErrorResponse("资源 Token 无法完成注册，请重新生成"))
		return
	}
	headscaleURL, authKey := "", ""
	if a.hsClient != nil {
		headscaleURL = a.config.Tailscale.HeadscalePublicURL
		if headscaleURL == "" {
			headscaleURL = a.config.Tailscale.HeadscaleURL
		}
		authKey = a.createAgentAuthKey(ctx, user.Name)
	}
	logger.Infof("技术资源注册成功: resource_id=%s, node_id=%d", resource.ID, node.ID)
	c.JSON(http.StatusOK, NewSuccessResponse(RegisterResponse{
		Message: "Agent 注册成功", UserRole: string(user.Role), UserID: user.ID,
		HeadscaleURL: headscaleURL, AuthKey: authKey, UserName: user.Name, DeviceName: node.Name,
		Config: map[string]interface{}{"server": map[string]interface{}{"address": a.config.Server.PublicURL}},
	}))
}

// RegisterCompat 旧版 Agent 注册兼容接口（POST /api/v1/agent/register）
// 内部转发到统一注册逻辑，输出 deprecation 警告
func (a *DeployAPI) RegisterCompat(c *gin.Context) {
	logger.Warnf("[DEPRECATED] POST /api/v1/agent/register 已弃用，请使用 POST /api/v1/register")
	a.Register(c)
}

// createAgentAuthKey 为 Agent 创建 Headscale AuthKey
func (a *DeployAPI) createAgentAuthKey(ctx context.Context, userName string) string {
	hsUserName := fmt.Sprintf("agent-%s", userName)

	user, err := a.hsClient.GetOrCreateUser(ctx, hsUserName)
	if err != nil {
		logger.Warnf("获取或创建 Headscale 用户失败: %v", err)
		return ""
	}

	key, err := a.hsClient.CreatePreAuthKey(ctx, user.Id, 24*time.Hour, false)
	if err != nil {
		logger.Warnf("创建 Headscale AuthKey 失败: %v", err)
		return ""
	}

	return key.Key
}

// createClientAuthKey 为 Client 创建 Headscale AuthKey（带 Tag）
func (a *DeployAPI) createClientAuthKey(ctx context.Context, user *model.User) string {
	hsUserName := fmt.Sprintf("client-%s", user.Name)

	hsUser, err := a.hsClient.GetOrCreateUser(ctx, hsUserName)
	if err != nil {
		logger.Errorf("获取/创建 Headscale 用户失败: %v", err)
		return ""
	}

	// 构建 Tag 列表（与 Desktop 共享同一套 Tag）
	tags := []string{fmt.Sprintf("tag:client-%s", user.Name)}

	// 查询用户所属分组，添加分组 Tag
	var groupMembers []model.GroupMember
	if err := db.DB.WithContext(ctx).Preload("Group").Where("user_id = ?", user.ID).Find(&groupMembers).Error; err == nil {
		for _, gm := range groupMembers {
			if gm.Group != nil {
				tags = append(tags, fmt.Sprintf("tag:group-%s", gm.Group.Name))
			}
		}
	}

	// 创建 PreAuthKey（ephemeral=false，保持节点稳定）
	authKey, err := a.hsClient.CreatePreAuthKeyWithTags(ctx, hsUser.Id, 24*time.Hour, false, tags)
	if err != nil {
		logger.Errorf("创建 Client PreAuthKey 失败: %v", err)
		return ""
	}

	return authKey.Key
}

// getServerAddr 获取 Server 外部访问地址
// 优先使用配置的 PublicURL，否则从请求 Header 推断
func (a *DeployAPI) getServerAddr(c *gin.Context) string {
	if a.config.Server.PublicURL != "" {
		return a.config.Server.PublicURL
	}

	// 从请求推断：优先 X-Forwarded 系列 Header（反向代理场景）
	scheme := c.GetHeader("X-Forwarded-Proto")
	if scheme == "" {
		if c.Request.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}

	host := c.GetHeader("X-Forwarded-Host")
	if host == "" {
		host = c.Request.Host
	}

	return scheme + "://" + host
}

// generateDeployToken 生成部署 Token
func generateDeployToken(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
