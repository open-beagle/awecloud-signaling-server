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
	configNotify ConfigNotifier // 配置变更通知接口
}

// ConfigNotifier 配置变更通知接口
type ConfigNotifier interface {
	NotifyConfigChange()
}

// NewProxyServiceAPI 创建 ProxyServiceAPI
func NewProxyServiceAPI(cfg *config.ServerConfig) *ProxyServiceAPI {
	api := &ProxyServiceAPI{config: cfg}

	// 初始化 ACL 同步服务
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
	ID              string `json:"id"`
	Name            string `json:"name"`
	AgentID         uint64 `json:"agent_id"`
	AgentName       string `json:"agent_name"`
	SourceAddr      string `json:"source_addr"`
	TargetAddr      string `json:"target_addr"`
	Enabled         bool   `json:"enabled"`
	DisplayStatus   string `json:"display_status"` // 合并后的显示状态
	ErrorMsg        string `json:"error_msg,omitempty"`
	ClientCount     int64  `json:"client_count"`
	GroupCount      int64  `json:"group_count"`
	AgentCount      int64  `json:"agent_count"`
	AgentGroupCount int64  `json:"agent_group_count"`
}

// List 获取服务列表
func (a *ProxyServiceAPI) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	agentIDStr := c.Query("agent_id")

	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}

	query := db.DB.Model(&model.ProxyService{}).Preload("Agent")
	if agentIDStr != "" {
		agentID, _ := strconv.ParseUint(agentIDStr, 10, 64)
		query = query.Where("agent_id = ?", agentID)
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
	var clientCounts []struct {
		ServiceID string `gorm:"column:service_id"`
		Count     int64  `gorm:"column:count"`
	}
	db.DB.Model(&model.ServiceClientPermission{}).
		Select("service_id, COUNT(*) as count").
		Group("service_id").Find(&clientCounts)

	clientCountMap := make(map[string]int64)
	for _, cc := range clientCounts {
		clientCountMap[cc.ServiceID] = cc.Count
	}

	// 查询每个服务的授权分组数
	var groupCounts []struct {
		ServiceID string `gorm:"column:service_id"`
		Count     int64  `gorm:"column:count"`
	}
	db.DB.Model(&model.ServiceClientGroupPermission{}).
		Select("service_id, COUNT(*) as count").
		Group("service_id").Find(&groupCounts)

	groupCountMap := make(map[string]int64)
	for _, gc := range groupCounts {
		groupCountMap[gc.ServiceID] = gc.Count
	}

	// 查询每个服务的授权 Agent 数
	var agentCounts []struct {
		ServiceID string `gorm:"column:service_id"`
		Count     int64  `gorm:"column:count"`
	}
	db.DB.Model(&model.ServiceAgentPermission{}).
		Select("service_id, COUNT(*) as count").
		Group("service_id").Find(&agentCounts)

	agentCountMap := make(map[string]int64)
	for _, ac := range agentCounts {
		agentCountMap[ac.ServiceID] = ac.Count
	}

	// 查询每个服务的授权 Agent 分组数
	var agentGroupCounts []struct {
		ServiceID string `gorm:"column:service_id"`
		Count     int64  `gorm:"column:count"`
	}
	db.DB.Model(&model.ServiceAgentGroupPermission{}).
		Select("service_id, COUNT(*) as count").
		Group("service_id").Find(&agentGroupCounts)

	agentGroupCountMap := make(map[string]int64)
	for _, agc := range agentGroupCounts {
		agentGroupCountMap[agc.ServiceID] = agc.Count
	}

	result := make([]ServiceListItem, len(services))
	for i, svc := range services {
		// 计算 Agent 在线状态
		agentOnline := false
		if svc.Agent != nil && svc.Agent.LastHeartbeat != nil {
			agentOnline = time.Since(*svc.Agent.LastHeartbeat) < 60*time.Second
		}

		// 获取运行时状态并计算显示状态
		runtimeStatus := cache.GetProxyServiceStatus(svc.ID)
		displayStatus, errorMsg := cache.GetDisplayStatus(svc.Enabled, agentOnline, runtimeStatus)

		item := ServiceListItem{
			ID:              svc.ID,
			Name:            svc.Name,
			AgentID:         svc.AgentID,
			SourceAddr:      svc.SourceAddr,
			TargetAddr:      svc.TargetAddr,
			Enabled:         svc.Enabled,
			DisplayStatus:   displayStatus,
			ErrorMsg:        errorMsg,
			ClientCount:     clientCountMap[svc.ID],
			GroupCount:      groupCountMap[svc.ID],
			AgentCount:      agentCountMap[svc.ID],
			AgentGroupCount: agentGroupCountMap[svc.ID],
		}
		if svc.Agent != nil {
			item.AgentName = svc.Agent.Name
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
	AgentID       uint64    `json:"agent_id"`
	AgentName     string    `json:"agent_name"`
	SourceAddr    string    `json:"source_addr"`
	TargetAddr    string    `json:"target_addr"`
	Enabled       bool      `json:"enabled"`
	DisplayStatus string    `json:"display_status"` // 合并后的显示状态
	ErrorMsg      string    `json:"error_msg,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

// Get 获取服务详情
func (a *ProxyServiceAPI) Get(c *gin.Context) {
	id := c.Param("id")

	var service model.ProxyService
	if err := db.DB.Preload("Agent").First(&service, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("服务不存在"))
		return
	}

	// 计算 Agent 在线状态
	agentOnline := false
	if service.Agent != nil && service.Agent.LastHeartbeat != nil {
		agentOnline = time.Since(*service.Agent.LastHeartbeat) < 60*time.Second
	}

	// 获取运行时状态并计算显示状态
	runtimeStatus := cache.GetProxyServiceStatus(service.ID)
	displayStatus, errorMsg := cache.GetDisplayStatus(service.Enabled, agentOnline, runtimeStatus)

	result := ServiceDetail{
		ID:            service.ID,
		Name:          service.Name,
		Alias:         service.Alias,
		AgentID:       service.AgentID,
		SourceAddr:    service.SourceAddr,
		TargetAddr:    service.TargetAddr,
		Enabled:       service.Enabled,
		DisplayStatus: displayStatus,
		ErrorMsg:      errorMsg,
		CreatedAt:     service.CreatedAt,
	}
	if service.Agent != nil {
		result.AgentName = service.Agent.Name
	}

	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

// CreateServiceRequest 创建服务请求
type CreateServiceRequest struct {
	AgentID    uint64 `json:"agent_id" binding:"required"`
	Name       string `json:"name" binding:"required"`
	Alias      string `json:"alias"`
	SourceAddr string `json:"source_addr" binding:"required"`
	TargetAddr string `json:"target_addr" binding:"required"`
}

// Create 创建服务
func (a *ProxyServiceAPI) Create(c *gin.Context) {
	var req CreateServiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	// 验证 Agent 存在
	var agent model.Agent
	if err := db.DB.First(&agent, req.AgentID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Agent 不存在"))
		return
	}

	// 检查名称是否已存在（同一 Agent 下）
	var existing model.ProxyService
	if err := db.DB.Where("agent_id = ? AND name = ?", req.AgentID, req.Name).First(&existing).Error; err == nil {
		c.JSON(http.StatusConflict, NewErrorResponse("服务名称已存在"))
		return
	}

	service := &model.ProxyService{
		ID:         uuid.New().String(),
		Name:       req.Name,
		Alias:      req.Alias,
		AgentID:    req.AgentID,
		SourceAddr: req.SourceAddr,
		TargetAddr: req.TargetAddr,
		Enabled:    true,
	}

	if err := db.DB.Create(service).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("创建失败: "+err.Error()))
		return
	}

	// 初始化运行时状态为 pending
	cache.UpdateProxyServiceStatus(service.ID, cache.ServiceStatusPending, "", "")

	logger.Infof("创建服务: id=%s, name=%s, agent_id=%d", service.ID, service.Name, service.AgentID)
	recordAuditLog(c, model.ActionCreateService, "service", service.ID, service.Name, nil)

	// 通知配置变更
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
	id := c.Param("id")

	var req UpdateServiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	var service model.ProxyService
	if err := db.DB.First(&service, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("服务不存在"))
		return
	}

	// 只更新提供的字段
	updates := make(map[string]interface{})
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

	if err := db.DB.Model(&service).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("更新失败"))
		return
	}

	logger.Infof("更新服务: id=%s", id)
	recordAuditLog(c, model.ActionUpdateService, "service", id, service.Name, nil)

	// 通知配置变更
	if a.configNotify != nil {
		a.configNotify.NotifyConfigChange()
	}

	c.JSON(http.StatusOK, NewSuccessMessageResponse("更新成功", nil))
}

// Delete 删除服务
func (a *ProxyServiceAPI) Delete(c *gin.Context) {
	id := c.Param("id")

	var service model.ProxyService
	if err := db.DB.First(&service, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("服务不存在"))
		return
	}

	// 删除相关权限
	db.DB.Where("service_id = ?", id).Delete(&model.ServiceClientPermission{})
	db.DB.Where("service_id = ?", id).Delete(&model.ServiceClientGroupPermission{})
	db.DB.Where("service_id = ?", id).Delete(&model.ServiceAgentPermission{})
	db.DB.Where("service_id = ?", id).Delete(&model.ServiceAgentGroupPermission{})

	// 删除服务
	if err := db.DB.Delete(&service).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("删除失败"))
		return
	}

	// 清理运行时状态缓存
	cache.DeleteProxyServiceStatus(id)

	// 同步 ACL
	if a.aclSync != nil {
		go func() {
			if err := a.aclSync.SyncACL(nil); err != nil {
				logger.Warnf("同步 ACL 失败: %v", err)
			}
		}()
	}

	logger.Infof("删除服务: id=%s, name=%s", id, service.Name)
	recordAuditLog(c, model.ActionDeleteService, "service", id, service.Name, nil)

	// 通知配置变更
	if a.configNotify != nil {
		a.configNotify.NotifyConfigChange()
	}

	c.JSON(http.StatusOK, NewSuccessMessageResponse("删除成功", nil))
}

// Toggle 启用/禁用服务
func (a *ProxyServiceAPI) Toggle(c *gin.Context) {
	id := c.Param("id")

	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	var service model.ProxyService
	if err := db.DB.First(&service, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("服务不存在"))
		return
	}

	// 更新启用状态
	if err := db.DB.Model(&service).Update("enabled", req.Enabled).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("更新失败"))
		return
	}

	// 如果是启用操作，将运行时状态设置为 pending
	if req.Enabled {
		cache.UpdateProxyServiceStatus(id, cache.ServiceStatusPending, "", "")
	}

	logger.Infof("切换服务状态: id=%s, enabled=%v", id, req.Enabled)
	recordAuditLog(c, model.ActionUpdateService, "service", id, service.Name, nil)

	// 通知配置变更
	if a.configNotify != nil {
		a.configNotify.NotifyConfigChange()
	}

	c.JSON(http.StatusOK, NewSuccessMessageResponse("操作成功", nil))
}

// Retry 重试错误状态的服务
func (a *ProxyServiceAPI) Retry(c *gin.Context) {
	id := c.Param("id")

	var service model.ProxyService
	if err := db.DB.First(&service, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("服务不存在"))
		return
	}

	// 检查运行时状态是否为错误
	runtimeStatus := cache.GetProxyServiceStatus(id)
	if runtimeStatus == nil || runtimeStatus.Status != cache.ServiceStatusError {
		c.JSON(http.StatusBadRequest, NewErrorResponse("只有错误状态的服务才能重试"))
		return
	}

	// 重置运行时状态为 pending
	cache.UpdateProxyServiceStatus(id, cache.ServiceStatusPending, "", "")

	logger.Infof("重试服务: id=%s", id)
	recordAuditLog(c, model.ActionUpdateService, "service", id, service.Name, nil)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("重试成功", nil))
}
