package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/headscale"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

// ProxyServiceAPI 端口映射服务 API
type ProxyServiceAPI struct {
	config  *config.ServerConfig
	aclSync *headscale.ACLSyncService
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

// ServiceListItem 服务列表项
type ServiceListItem struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	AgentID         uint64 `json:"agent_id"`
	AgentName       string `json:"agent_name"`
	TargetAddr      string `json:"target_addr"`
	ListenAddr      string `json:"listen_addr"`
	Enabled         bool   `json:"enabled"`
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
		// 构建完整的监听地址（Agent IP + 端口）
		listenAddr := svc.ListenAddr
		if svc.Agent != nil && svc.Agent.IP != "" && listenAddr != "" {
			// 如果 ListenAddr 是 :port 格式，添加 Agent IP
			if len(listenAddr) > 0 && listenAddr[0] == ':' {
				listenAddr = svc.Agent.IP + listenAddr
			}
		}

		item := ServiceListItem{
			ID:              svc.ID,
			Name:            svc.Name,
			AgentID:         svc.AgentID,
			TargetAddr:      svc.TargetAddr,
			ListenAddr:      listenAddr,
			Enabled:         svc.Enabled,
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
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Alias      string    `json:"alias"`
	AgentID    uint64    `json:"agent_id"`
	AgentName  string    `json:"agent_name"`
	TargetAddr string    `json:"target_addr"`
	ListenAddr string    `json:"listen_addr"`
	Enabled    bool      `json:"enabled"`
	CreatedAt  time.Time `json:"created_at"`
}

// Get 获取服务详情
func (a *ProxyServiceAPI) Get(c *gin.Context) {
	id := c.Param("id")

	var service model.ProxyService
	if err := db.DB.Preload("Agent").First(&service, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("服务不存在"))
		return
	}

	result := ServiceDetail{
		ID:         service.ID,
		Name:       service.Name,
		Alias:      service.Alias,
		AgentID:    service.AgentID,
		TargetAddr: service.TargetAddr,
		ListenAddr: service.ListenAddr,
		Enabled:    service.Enabled,
		CreatedAt:  service.CreatedAt,
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
	TargetAddr string `json:"target_addr" binding:"required"`
	ListenAddr string `json:"listen_addr" binding:"required"`
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
		TargetAddr: req.TargetAddr,
		ListenAddr: req.ListenAddr,
		Enabled:    true,
	}

	if err := db.DB.Create(service).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("创建失败: "+err.Error()))
		return
	}

	logger.Infof("创建服务: id=%s, name=%s, agent_id=%d", service.ID, service.Name, service.AgentID)
	recordAuditLog(c, model.ActionCreateService, "service", service.ID, service.Name, nil)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("创建成功", service))
}

// UpdateServiceRequest 更新服务请求
type UpdateServiceRequest struct {
	Alias      string `json:"alias"`
	TargetAddr string `json:"target_addr"`
	ListenAddr string `json:"listen_addr"`
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
	if req.TargetAddr != "" {
		updates["target_addr"] = req.TargetAddr
	}
	if req.ListenAddr != "" {
		updates["listen_addr"] = req.ListenAddr
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

	c.JSON(http.StatusOK, NewSuccessMessageResponse("删除成功", nil))
}
