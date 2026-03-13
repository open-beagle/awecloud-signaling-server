package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/cache"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/headscale"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

// ProxyServiceAPI 端口映射服务 API
type ProxyServiceAPI struct {
	config       *config.ServerConfig
	aclSync      *headscale.ACLSyncService
	configNotify ConfigNotifier
}

// ConfigNotifier 配置变更通知接口
type ConfigNotifier interface {
	NotifyConfigChange()
}

// NewProxyServiceAPI 创建 ProxyServiceAPI
func NewProxyServiceAPI(cfg *config.ServerConfig) *ProxyServiceAPI {
	api := &ProxyServiceAPI{config: cfg}

	if cfg.Tailscale.HeadscaleURL != "" && cfg.Tailscale.HeadscaleAPIKey != "" {
		client, err := headscale.NewClient(headscale.Config{
			URL:    cfg.Tailscale.HeadscaleURL,
			APIKey: cfg.Tailscale.HeadscaleAPIKey,
		})
		if err != nil {
			logger.Warnf("初始化 Headscale 客户端失败: %v", err)
		} else {
			api.aclSync = headscale.NewACLSyncService(client)
		}
	}

	return api
}

// SetConfigNotifier 设置配置变更通知器
func (a *ProxyServiceAPI) SetConfigNotifier(notifier ConfigNotifier) {
	a.configNotify = notifier
}

// ServiceListItem 服务列表项
type ServiceListItem struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	UserID        uint64 `json:"user_id"`
	UserName      string `json:"user_name"`
	SourceAddr    string `json:"source_addr"`
	TargetAddr    string `json:"target_addr"`
	Enabled       bool   `json:"enabled"`
	DisplayStatus string `json:"display_status"`
	ErrorMsg      string `json:"error_msg,omitempty"`
	UserCount     int64  `json:"user_count"`
	GroupCount    int64  `json:"group_count"`
}

// List 获取服务列表
func (a *ProxyServiceAPI) List(c *gin.Context) {
	ctx := c.Request.Context()
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	userIDStr := c.Query("user_id")

	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}

	query := db.DB.WithContext(ctx).Model(&model.ProxyService{}).Preload("User")
	if userIDStr != "" {
		userID, _ := strconv.ParseUint(userIDStr, 10, 64)
		query = query.Where("user_id = ?", userID)
	}

	var total int64
	query.Count(&total)

	var services []model.ProxyService
	offset := (page - 1) * size
	if err := query.Order("created_at DESC").Offset(offset).Limit(size).Find(&services).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询失败"))
		return
	}

	// 查询每个服务的授权用户数
	var userCounts []struct {
		ServiceID string `gorm:"column:service_id"`
		Count     int64  `gorm:"column:count"`
	}
	db.DB.WithContext(ctx).Model(&model.AclServiceUserPermission{}).
		Select("service_id, COUNT(*) as count").
		Group("service_id").Find(&userCounts)

	userCountMap := make(map[string]int64)
	for _, uc := range userCounts {
		userCountMap[uc.ServiceID] = uc.Count
	}

	// 查询每个服务的授权分组数
	var groupCounts []struct {
		ServiceID string `gorm:"column:service_id"`
		Count     int64  `gorm:"column:count"`
	}
	db.DB.WithContext(ctx).Model(&model.AclServiceGroupPermission{}).
		Select("service_id, COUNT(*) as count").
		Group("service_id").Find(&groupCounts)

	groupCountMap := make(map[string]int64)
	for _, gc := range groupCounts {
		groupCountMap[gc.ServiceID] = gc.Count
	}

	// 查询 User 的 Node 在线状态
	var nodes []model.Node
	db.DB.WithContext(ctx).Where("type = ?", model.NodeTypeAgent).Find(&nodes)
	nodeOnlineMap := make(map[uint64]bool)
	for _, node := range nodes {
		if node.LastHeartbeat != nil && time.Since(*node.LastHeartbeat) < 60*time.Second {
			nodeOnlineMap[node.UserID] = true
		}
	}

	result := make([]ServiceListItem, len(services))
	for i, svc := range services {
		userOnline := nodeOnlineMap[svc.UserID]
		runtimeStatus := cache.GetProxyServiceStatus(svc.ID)
		displayStatus, errorMsg := cache.GetDisplayStatus(svc.Enabled, userOnline, runtimeStatus)

		item := ServiceListItem{
			ID:            svc.ID,
			Name:          svc.Name,
			UserID:        svc.UserID,
			SourceAddr:    svc.SourceAddr,
			TargetAddr:    svc.TargetAddr,
			Enabled:       svc.Enabled,
			DisplayStatus: displayStatus,
			ErrorMsg:      errorMsg,
			UserCount:     userCountMap[svc.ID],
			GroupCount:    groupCountMap[svc.ID],
		}
		if svc.User != nil {
			item.UserName = svc.User.Name
		}
		result[i] = item
	}

	c.JSON(http.StatusOK, NewPagedResponse(result, total, page, size))
}

// ServiceDetail 服务详情
type ServiceDetail struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Alias         string    `json:"alias"`
	UserID        uint64    `json:"user_id"`
	UserName      string    `json:"user_name"`
	SourceAddr    string    `json:"source_addr"`
	TargetAddr    string    `json:"target_addr"`
	Enabled       bool      `json:"enabled"`
	DisplayStatus string    `json:"display_status"`
	ErrorMsg      string    `json:"error_msg,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

// Get 获取服务详情
func (a *ProxyServiceAPI) Get(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	var service model.ProxyService
	if err := db.DB.WithContext(ctx).Preload("User").First(&service, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("服务不存在"))
		return
	}

	// 查询 User 的 Node 在线状态
	userOnline := false
	var node model.Node
	if err := db.DB.WithContext(ctx).Where("user_id = ? AND type = ?", service.UserID, model.NodeTypeAgent).First(&node).Error; err == nil {
		if node.LastHeartbeat != nil && time.Since(*node.LastHeartbeat) < 60*time.Second {
			userOnline = true
		}
	}

	runtimeStatus := cache.GetProxyServiceStatus(service.ID)
	displayStatus, errorMsg := cache.GetDisplayStatus(service.Enabled, userOnline, runtimeStatus)

	result := ServiceDetail{
		ID:            service.ID,
		Name:          service.Name,
		Alias:         service.Alias,
		UserID:        service.UserID,
		SourceAddr:    service.SourceAddr,
		TargetAddr:    service.TargetAddr,
		Enabled:       service.Enabled,
		DisplayStatus: displayStatus,
		ErrorMsg:      errorMsg,
		CreatedAt:     service.CreatedAt,
	}
	if service.User != nil {
		result.UserName = service.User.Name
	}

	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

// CreateServiceRequest 创建服务请求
type CreateServiceRequest struct {
	UserID     uint64 `json:"user_id" binding:"required"`
	Name       string `json:"name" binding:"required"`
	Alias      string `json:"alias"`
	SourceAddr string `json:"source_addr" binding:"required"`
	TargetAddr string `json:"target_addr" binding:"required"`
}

// Create 创建服务
func (a *ProxyServiceAPI) Create(c *gin.Context) {
	ctx := c.Request.Context()
	var req CreateServiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	// 验证 User 存在且为 Agent 角色
	var user model.User
	if err := db.DB.WithContext(ctx).First(&user, req.UserID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("用户不存在"))
		return
	}
	if user.Role != model.UserRoleAgent {
		c.JSON(http.StatusBadRequest, NewErrorResponse("只有 Agent 用户可以创建服务"))
		return
	}

	// 检查名称是否已存在（同一 User 下）
	var existing model.ProxyService
	if err := db.DB.WithContext(ctx).Where("user_id = ? AND name = ?", req.UserID, req.Name).First(&existing).Error; err == nil {
		c.JSON(http.StatusConflict, NewErrorResponse("服务名称已存在"))
		return
	}

	service := &model.ProxyService{
		ID:         uuid.New().String(),
		Name:       req.Name,
		Alias:      req.Alias,
		UserID:     req.UserID,
		SourceAddr: req.SourceAddr,
		TargetAddr: req.TargetAddr,
		Enabled:    true,
	}

	if err := db.DB.WithContext(ctx).Create(service).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("创建失败: "+err.Error()))
		return
	}

	cache.UpdateProxyServiceStatus(service.ID, cache.ServiceStatusPending, "", "")

	logger.Infof("创建服务: id=%s, name=%s, user_id=%d", service.ID, service.Name, service.UserID)
	recordAuditLog(ctx, c, model.ActionCreateService, "service", service.ID, service.Name, nil)

	if a.configNotify != nil {
		a.configNotify.NotifyConfigChange()
	}

	c.JSON(http.StatusOK, NewSuccessMessageResponse("创建成功", service))
}

// UpdateServiceRequest 更新服务请求
type UpdateServiceRequest struct {
	Alias      string `json:"alias"`
	SourceAddr string `json:"source_addr"`
	TargetAddr string `json:"target_addr"`
	Enabled    *bool  `json:"enabled"`
}

// Update 更新服务
func (a *ProxyServiceAPI) Update(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	var req UpdateServiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	var service model.ProxyService
	if err := db.DB.WithContext(ctx).First(&service, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("服务不存在"))
		return
	}

	updates := make(map[string]any)
	if req.Alias != "" {
		updates["alias"] = req.Alias
	}
	if req.SourceAddr != "" {
		updates["source_addr"] = req.SourceAddr
	}
	if req.TargetAddr != "" {
		updates["target_addr"] = req.TargetAddr
	}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}

	if len(updates) == 0 {
		c.JSON(http.StatusBadRequest, NewErrorResponse("没有需要更新的字段"))
		return
	}

	if err := db.DB.WithContext(ctx).Model(&service).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("更新失败"))
		return
	}

	logger.Infof("更新服务: id=%s", id)
	recordAuditLog(ctx, c, model.ActionUpdateService, "service", id, service.Name, nil)

	if a.configNotify != nil {
		a.configNotify.NotifyConfigChange()
	}

	c.JSON(http.StatusOK, NewSuccessMessageResponse("更新成功", nil))
}

// Delete 删除服务
func (a *ProxyServiceAPI) Delete(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	var service model.ProxyService
	if err := db.DB.WithContext(ctx).First(&service, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("服务不存在"))
		return
	}

	// 删除相关权限
	db.DB.WithContext(ctx).Where("service_id = ?", id).Delete(&model.AclServiceUserPermission{})
	db.DB.WithContext(ctx).Where("service_id = ?", id).Delete(&model.AclServiceGroupPermission{})

	if err := db.DB.WithContext(ctx).Delete(&service).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("删除失败"))
		return
	}

	cache.DeleteProxyServiceStatus(id)

	if a.aclSync != nil {
		go func() {
			if err := a.aclSync.FullSync(nil); err != nil {
				logger.Warnf("同步 ACL 失败: %v", err)
			}
		}()
	}

	logger.Infof("删除服务: id=%s, name=%s", id, service.Name)
	recordAuditLog(ctx, c, model.ActionDeleteService, "service", id, service.Name, nil)

	if a.configNotify != nil {
		a.configNotify.NotifyConfigChange()
	}

	c.JSON(http.StatusOK, NewSuccessMessageResponse("删除成功", nil))
}

// Toggle 启用/禁用服务
func (a *ProxyServiceAPI) Toggle(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	var service model.ProxyService
	if err := db.DB.WithContext(ctx).First(&service, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("服务不存在"))
		return
	}

	if err := db.DB.WithContext(ctx).Model(&service).Update("enabled", req.Enabled).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("更新失败"))
		return
	}

	if req.Enabled {
		cache.UpdateProxyServiceStatus(id, cache.ServiceStatusPending, "", "")
	}

	logger.Infof("切换服务状态: id=%s, enabled=%v", id, req.Enabled)
	recordAuditLog(ctx, c, model.ActionToggleService, "service", id, service.Name, nil)

	if a.configNotify != nil {
		a.configNotify.NotifyConfigChange()
	}

	c.JSON(http.StatusOK, NewSuccessMessageResponse("操作成功", nil))
}

// Retry 重试错误状态的服务
func (a *ProxyServiceAPI) Retry(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	var service model.ProxyService
	if err := db.DB.WithContext(ctx).First(&service, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("服务不存在"))
		return
	}

	runtimeStatus := cache.GetProxyServiceStatus(id)
	if runtimeStatus == nil || runtimeStatus.Status != cache.ServiceStatusError {
		c.JSON(http.StatusBadRequest, NewErrorResponse("只有错误状态的服务才能重试"))
		return
	}

	cache.UpdateProxyServiceStatus(id, cache.ServiceStatusPending, "", "")

	logger.Infof("重试服务: id=%s", id)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("重试成功", nil))
}
