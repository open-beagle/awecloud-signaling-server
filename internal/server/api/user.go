package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/cache"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	grpcserver "github.com/open-beagle/awecloud-signaling-server/internal/server/grpc"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/headscale"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

// UserAPI 用户管理 API
type UserAPI struct {
	config       *config.ServerConfig
	hsClient     *headscale.Client
	agentService *grpcserver.AgentServiceServer
}

// NewUserAPI 创建 UserAPI
func NewUserAPI(cfg *config.ServerConfig) *UserAPI {
	api := &UserAPI{config: cfg}

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

// SetAgentService 设置 AgentService（用于获取实时状态）
func (a *UserAPI) SetAgentService(service *grpcserver.AgentServiceServer) {
	a.agentService = service
}

// UserListItem 用户列表项
type UserListItem struct {
	ID           uint64     `json:"id"`
	Name         string     `json:"name"`
	Alias        string     `json:"alias"`
	Role         string     `json:"role"`
	NodeCount    int64      `json:"node_count"`    // 设备数量
	OnlineCount  int64      `json:"online_count"`  // 在线设备数量
	ServiceCount int64      `json:"service_count"` // 服务数量（仅 Agent）
	GroupCount   int64      `json:"group_count"`   // 分组数量
	Status       string     `json:"status"`
	SSHEnabled   bool       `json:"ssh_enabled"` // SSH 是否启用（仅 Agent）
	LastOnline   *time.Time `json:"last_online"`
	CreatedAt    time.Time  `json:"created_at"`
}

// List 获取用户列表
func (a *UserAPI) List(c *gin.Context) {
	ctx := c.Request.Context()
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	search := c.Query("search")
	role := c.Query("role") // 筛选角色：agent / client

	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}

	query := db.DB.WithContext(ctx).Model(&model.User{})
	if search != "" {
		query = query.Where("name LIKE ? OR alias LIKE ?", "%"+search+"%", "%"+search+"%")
	}
	if role != "" {
		query = query.Where("role = ?", role)
	}

	var total int64
	query.Count(&total)

	var users []model.User
	offset := (page - 1) * size
	if err := query.Order("created_at DESC").Offset(offset).Limit(size).Find(&users).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询失败"))
		return
	}

	// 查询每个用户的设备数量和在线数量
	type NodeStats struct {
		UserID      uint64     `gorm:"column:user_id"`
		Count       int64      `gorm:"column:count"`
		OnlineCount int64      `gorm:"column:online_count"`
		LastOnline  *time.Time `gorm:"column:last_online"`
	}
	var nodeStats []NodeStats
	db.DB.WithContext(ctx).Model(&model.Node{}).
		Select("user_id, COUNT(*) as count, SUM(CASE WHEN last_heartbeat > datetime('now', '-60 seconds') THEN 1 ELSE 0 END) as online_count, MAX(last_heartbeat) as last_online").
		Group("user_id").Find(&nodeStats)

	nodeStatsMap := make(map[uint64]NodeStats)
	for _, ns := range nodeStats {
		nodeStatsMap[ns.UserID] = ns
	}

	// 查询每个用户的服务数量（仅 Agent）
	var serviceCounts []struct {
		UserID uint64 `gorm:"column:user_id"`
		Count  int64  `gorm:"column:count"`
	}
	db.DB.WithContext(ctx).Model(&model.ProxyService{}).
		Select("user_id, COUNT(*) as count").
		Group("user_id").Find(&serviceCounts)

	serviceCountMap := make(map[uint64]int64)
	for _, sc := range serviceCounts {
		serviceCountMap[sc.UserID] = sc.Count
	}

	// 查询每个用户的分组数量
	var groupCounts []struct {
		UserID uint64 `gorm:"column:user_id"`
		Count  int64  `gorm:"column:count"`
	}
	db.DB.WithContext(ctx).Model(&model.GroupMember{}).
		Select("user_id, COUNT(*) as count").
		Group("user_id").Find(&groupCounts)

	groupCountMap := make(map[uint64]int64)
	for _, gc := range groupCounts {
		groupCountMap[gc.UserID] = gc.Count
	}

	result := make([]UserListItem, len(users))
	for i, user := range users {
		stats := nodeStatsMap[user.ID]
		status := "offline"
		if stats.OnlineCount > 0 {
			status = "online"
		}

		result[i] = UserListItem{
			ID:           user.ID,
			Name:         user.Name,
			Alias:        user.Alias,
			Role:         string(user.Role),
			NodeCount:    stats.Count,
			OnlineCount:  stats.OnlineCount,
			ServiceCount: serviceCountMap[user.ID],
			GroupCount:   groupCountMap[user.ID],
			Status:       status,
			SSHEnabled:   user.SSHEnabled,
			LastOnline:   stats.LastOnline,
			CreatedAt:    user.CreatedAt,
		}
	}

	c.JSON(http.StatusOK, NewPagedResponse(result, total, page, size))
}

// UserDetail 用户详情
type UserDetail struct {
	ID         uint64            `json:"id"`
	Name       string            `json:"name"`
	Alias      string            `json:"alias"`
	Role       string            `json:"role"`
	SSHEnabled bool              `json:"ssh_enabled"`
	CreatedAt  time.Time         `json:"created_at"`
	UpdatedAt  time.Time         `json:"updated_at"`
	Nodes      []UserNodeItem    `json:"nodes"`    // 设备列表
	Services   []UserServiceItem `json:"services"` // 服务列表（仅 Agent）
}

// UserNodeItem 用户设备项
type UserNodeItem struct {
	ID            uint64     `json:"id"`
	Name          string     `json:"name"`
	Type          string     `json:"type"`
	IP            string     `json:"ip"`
	Version       string     `json:"version"`
	Status        string     `json:"status"`
	LastHeartbeat *time.Time `json:"last_heartbeat"`
}

// UserServiceItem 用户服务项
type UserServiceItem struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Alias         string `json:"alias"`
	SourceAddr    string `json:"source_addr"`
	TargetAddr    string `json:"target_addr"`
	Enabled       bool   `json:"enabled"`
	DisplayStatus string `json:"display_status"`
	ErrorMsg      string `json:"error_msg,omitempty"`
}

// Get 获取用户详情
func (a *UserAPI) Get(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	var user model.User
	if err := db.DB.WithContext(ctx).First(&user, id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("用户不存在"))
		return
	}

	// 查询设备列表
	var nodes []model.Node
	db.DB.WithContext(ctx).Where("user_id = ?", id).Find(&nodes)

	now := time.Now()
	nodeItems := make([]UserNodeItem, len(nodes))
	for i, node := range nodes {
		status := "offline"
		if node.LastHeartbeat != nil && now.Sub(*node.LastHeartbeat) < 60*time.Second {
			status = "online"
		}
		nodeItems[i] = UserNodeItem{
			ID:            node.ID,
			Name:          node.Name,
			Type:          string(node.Type),
			IP:            node.IP,
			Version:       node.Version,
			Status:        status,
			LastHeartbeat: node.LastHeartbeat,
		}
	}

	// 查询服务列表（仅 Agent 角色）
	var serviceItems []UserServiceItem
	if user.Role == model.UserRoleAgent {
		var services []model.ProxyService
		db.DB.WithContext(ctx).Where("user_id = ?", id).Find(&services)

		// 判断用户是否在线（任一设备在线即为在线）
		userOnline := false
		for _, node := range nodes {
			if node.LastHeartbeat != nil && now.Sub(*node.LastHeartbeat) < 60*time.Second {
				userOnline = true
				break
			}
		}

		serviceItems = make([]UserServiceItem, len(services))
		for i, svc := range services {
			runtimeStatus := cache.GetProxyServiceStatus(svc.ID)
			displayStatus, errorMsg := cache.GetDisplayStatus(svc.Enabled, userOnline, runtimeStatus)

			serviceItems[i] = UserServiceItem{
				ID:            svc.ID,
				Name:          svc.Name,
				Alias:         svc.Alias,
				SourceAddr:    svc.SourceAddr,
				TargetAddr:    svc.TargetAddr,
				Enabled:       svc.Enabled,
				DisplayStatus: displayStatus,
				ErrorMsg:      errorMsg,
			}
		}
	}

	result := UserDetail{
		ID:         user.ID,
		Name:       user.Name,
		Alias:      user.Alias,
		Role:       string(user.Role),
		SSHEnabled: user.SSHEnabled,
		CreatedAt:  user.CreatedAt,
		UpdatedAt:  user.UpdatedAt,
		Nodes:      nodeItems,
		Services:   serviceItems,
	}

	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

// CreateUserRequest 创建用户请求
type CreateUserRequest struct {
	Name  string `json:"name" binding:"required"`
	Alias string `json:"alias"`
	Role  string `json:"role" binding:"required,oneof=agent client"`
}

// CreateUserResponse 创建用户响应
type CreateUserResponse struct {
	ID     uint64 `json:"id"`
	Name   string `json:"name"`
	Secret string `json:"secret"`
}

// Create 创建用户
func (a *UserAPI) Create(c *gin.Context) {
	ctx := c.Request.Context()
	var req CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	// 检查名称是否已存在
	var existing model.User
	if err := db.DB.WithContext(ctx).Where("name = ?", req.Name).First(&existing).Error; err == nil {
		c.JSON(http.StatusConflict, NewErrorResponse("用户名称已存在"))
		return
	}

	// 生成密钥
	secret, err := generateUserSecret(32)
	if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("生成密钥失败"))
		return
	}

	// 哈希密钥
	secretHash, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("加密密钥失败"))
		return
	}

	// 在 Headscale 创建 User
	var userID uint64
	if a.hsClient != nil {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()

		// User 命名规则: {role}-{name}
		userName := fmt.Sprintf("%s-%s", req.Role, req.Name)
		user, err := a.hsClient.CreateUser(ctx, userName)
		if err != nil {
			logger.Warnf("Headscale 创建用户失败: %v", err)
		} else {
			userID = user.Id
		}
	}

	// 创建用户
	user := &model.User{
		Name:       req.Name,
		Alias:      req.Alias,
		Role:       model.UserRole(req.Role),
		SecretHash: string(secretHash),
	}
	if userID > 0 {
		user.HeadscaleUID = userID
	}

	if err := db.DB.WithContext(ctx).Create(user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("创建失败: "+err.Error()))
		return
	}

	logger.Infof("创建用户: id=%d, name=%s, role=%s", user.ID, user.Name, user.Role)

	// 记录审计日志
	actionType := model.ActionCreateAgent
	if req.Role == "client" {
		actionType = model.ActionCreateClient
	}
	recordAuditLog(ctx, c, actionType, "user", strconv.FormatUint(user.ID, 10), user.Name, nil)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("创建成功", CreateUserResponse{
		ID:     user.ID,
		Name:   user.Name,
		Secret: secret,
	}))
}

// UpdateUserRequest 更新用户请求
type UpdateUserRequest struct {
	Alias string `json:"alias"`
}

// Update 更新用户
func (a *UserAPI) Update(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	var req UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	var user model.User
	if err := db.DB.WithContext(ctx).First(&user, id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("用户不存在"))
		return
	}

	user.Alias = req.Alias

	if err := db.DB.WithContext(ctx).Save(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("更新失败"))
		return
	}

	logger.Infof("更新用户: id=%d", id)

	actionType := model.ActionUpdateAgent
	if user.Role == model.UserRoleClient {
		actionType = model.ActionUpdateClient
	}
	recordAuditLog(ctx, c, actionType, "user", strconv.FormatUint(id, 10), user.Name, nil)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("更新成功", nil))
}

// UpdateUserSSHRequest 更新用户 SSH 配置请求
type UpdateUserSSHRequest struct {
	Enabled bool `json:"enabled"`
}

// UpdateSSH 更新用户 SSH 配置（仅 Agent）
func (a *UserAPI) UpdateSSH(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	var req UpdateUserSSHRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	var user model.User
	if err := db.DB.WithContext(ctx).First(&user, id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("用户不存在"))
		return
	}

	if user.Role != model.UserRoleAgent {
		c.JSON(http.StatusBadRequest, NewErrorResponse("仅 Agent 用户支持 SSH 配置"))
		return
	}

	oldEnabled := user.SSHEnabled
	user.SSHEnabled = req.Enabled

	if err := db.DB.WithContext(ctx).Save(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("更新失败"))
		return
	}

	logger.Infof("更新用户 SSH 配置: id=%d, enabled=%v", id, req.Enabled)

	detail := map[string]interface{}{
		"old_enabled": oldEnabled,
		"new_enabled": req.Enabled,
	}
	recordAuditLog(ctx, c, model.ActionUpdateAgent, "user", strconv.FormatUint(id, 10), user.Name, detail)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("SSH 配置更新成功", nil))
}

// Delete 删除用户
func (a *UserAPI) Delete(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	var user model.User
	if err := db.DB.WithContext(ctx).First(&user, id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("用户不存在"))
		return
	}

	// 在 Headscale 删除 Node 和 User
	if a.hsClient != nil {
		hsCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		// 删除所有关联的 Node
		var nodes []model.Node
		db.DB.WithContext(ctx).Where("user_id = ?", id).Find(&nodes)
		for _, node := range nodes {
			if node.ID > 0 {
				_ = a.hsClient.DeleteNode(hsCtx, node.ID)
			}
		}

		// 删除 User
		userName := fmt.Sprintf("%s-%s", user.Role, user.Name)
		_ = a.hsClient.DeleteUser(hsCtx, userName)
	}

	// 删除相关数据
	db.DB.WithContext(ctx).Where("user_id = ?", id).Delete(&model.Node{})
	db.DB.WithContext(ctx).Where("user_id = ?", id).Delete(&model.ProxyService{})
	db.DB.WithContext(ctx).Where("user_id = ?", id).Delete(&model.PortForward{})
	db.DB.WithContext(ctx).Where("user_id = ?", id).Delete(&model.GroupMember{})

	// 删除用户
	if err := db.DB.WithContext(ctx).Delete(&model.User{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("删除失败"))
		return
	}

	logger.Infof("删除用户: id=%d, name=%s", id, user.Name)

	actionType := model.ActionDeleteAgent
	if user.Role == model.UserRoleClient {
		actionType = model.ActionDeleteClient
	}
	recordAuditLog(ctx, c, actionType, "user", strconv.FormatUint(id, 10), user.Name, nil)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("删除成功", nil))
}

// RegenerateSecret 重新生成用户密钥
func (a *UserAPI) RegenerateSecret(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	var user model.User
	if err := db.DB.WithContext(ctx).First(&user, id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("用户不存在"))
		return
	}

	secret, err := generateUserSecret(32)
	if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("生成密钥失败"))
		return
	}

	secretHash, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("加密密钥失败"))
		return
	}

	user.SecretHash = string(secretHash)
	if err := db.DB.WithContext(ctx).Save(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("更新失败"))
		return
	}

	logger.Infof("重置用户密钥: id=%d", id)

	actionType := model.ActionResetAgentSecret
	if user.Role == model.UserRoleClient {
		actionType = model.ActionResetClientSecret
	}
	recordAuditLog(ctx, c, actionType, "user", strconv.FormatUint(id, 10), user.Name, nil)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("密钥重置成功", map[string]string{
		"secret": secret,
	}))
}

// generateUserSecret 生成随机密钥
func generateUserSecret(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
