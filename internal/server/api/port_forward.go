package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/cache"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

// PortForwardAPI 端口转发 API
type PortForwardAPI struct {
	config       *config.ServerConfig
	configNotify ConfigNotifier // 配置变更通知接口
}

// NewPortForwardAPI 创建 PortForwardAPI
func NewPortForwardAPI(cfg *config.ServerConfig) *PortForwardAPI {
	return &PortForwardAPI{config: cfg}
}

// SetConfigNotifier 设置配置变更通知器
func (a *PortForwardAPI) SetConfigNotifier(notifier ConfigNotifier) {
	a.configNotify = notifier
}

// CreateForwardRequest 创建端口转发请求
type CreateForwardRequest struct {
	AgentID         uint64 `json:"agent_id" binding:"required"`
	TargetServiceID string `json:"target_service_id" binding:"required"`
	SourceAddr      string `json:"source_addr" binding:"required"`
}

// Create 创建端口转发
func (a *PortForwardAPI) Create(c *gin.Context) {
	var req CreateForwardRequest
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

	// 验证目标服务存在
	var targetService model.ProxyService
	if err := db.DB.First(&targetService, "id = ?", req.TargetServiceID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("目标服务不存在"))
		return
	}

	// 检查是否已存在相同的转发配置
	var existing model.PortForward
	if err := db.DB.Where("agent_id = ? AND source_addr = ?", req.AgentID, req.SourceAddr).First(&existing).Error; err == nil {
		c.JSON(http.StatusConflict, NewErrorResponse("该源地址已被使用"))
		return
	}

	// 获取目标服务的 VPN 地址作为 target_addr
	targetAddr := targetService.SourceAddr

	forward := &model.PortForward{
		ID:              uuid.New().String(),
		AgentID:         req.AgentID,
		TargetServiceID: req.TargetServiceID,
		SourceAddr:      req.SourceAddr,
		TargetAddr:      targetAddr,
		Enabled:         true,
	}

	if err := db.DB.Create(forward).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("创建失败: "+err.Error()))
		return
	}

	// 初始化运行时状态为 pending
	cache.UpdatePortForwardStatus(forward.ID, cache.ServiceStatusPending, "", "")

	logger.Infof("创建端口转发: id=%s, agent_id=%d, target_service_id=%s", forward.ID, forward.AgentID, forward.TargetServiceID)
	recordAuditLog(c, model.ActionCreateService, "port_forward", forward.ID, "", nil)

	// 通知配置变更
	if a.configNotify != nil {
		a.configNotify.NotifyConfigChange()
	}

	c.JSON(http.StatusOK, NewSuccessMessageResponse("创建成功", forward))
}

// UpdateForwardRequest 更新端口转发请求
type UpdateForwardRequest struct {
	SourceAddr string `json:"source_addr"`
	Enabled    *bool  `json:"enabled"`
}

// Update 更新端口转发
func (a *PortForwardAPI) Update(c *gin.Context) {
	id := c.Param("id")

	var req UpdateForwardRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	var forward model.PortForward
	if err := db.DB.First(&forward, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("端口转发不存在"))
		return
	}

	// 只更新提供的字段
	updates := make(map[string]interface{})
	if req.SourceAddr != "" {
		updates["source_addr"] = req.SourceAddr
	}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}

	if len(updates) == 0 {
		c.JSON(http.StatusBadRequest, NewErrorResponse("没有需要更新的字段"))
		return
	}

	if err := db.DB.Model(&forward).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("更新失败"))
		return
	}

	logger.Infof("更新端口转发: id=%s", id)
	recordAuditLog(c, model.ActionUpdateService, "port_forward", id, "", nil)

	// 通知配置变更
	if a.configNotify != nil {
		a.configNotify.NotifyConfigChange()
	}

	c.JSON(http.StatusOK, NewSuccessMessageResponse("更新成功", nil))
}

// Toggle 启用/禁用端口转发
func (a *PortForwardAPI) Toggle(c *gin.Context) {
	id := c.Param("id")

	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	var forward model.PortForward
	if err := db.DB.First(&forward, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("端口转发不存在"))
		return
	}

	// 更新启用状态
	if err := db.DB.Model(&forward).Update("enabled", req.Enabled).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("更新失败"))
		return
	}

	// 如果是启用操作，将运行时状态设置为 pending
	if req.Enabled {
		cache.UpdatePortForwardStatus(id, cache.ServiceStatusPending, "", "")
	}

	logger.Infof("切换端口转发状态: id=%s, enabled=%v", id, req.Enabled)
	recordAuditLog(c, model.ActionUpdateService, "port_forward", id, "", nil)

	// 通知配置变更
	if a.configNotify != nil {
		a.configNotify.NotifyConfigChange()
	}

	c.JSON(http.StatusOK, NewSuccessMessageResponse("操作成功", nil))
}

// Retry 重试错误状态的端口转发
func (a *PortForwardAPI) Retry(c *gin.Context) {
	id := c.Param("id")

	var forward model.PortForward
	if err := db.DB.First(&forward, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("端口转发不存在"))
		return
	}

	// 检查运行时状态是否为错误
	runtimeStatus := cache.GetPortForwardStatus(id)
	if runtimeStatus == nil || runtimeStatus.Status != cache.ServiceStatusError {
		c.JSON(http.StatusBadRequest, NewErrorResponse("只有错误状态的端口转发才能重试"))
		return
	}

	// 重置运行时状态为 pending
	cache.UpdatePortForwardStatus(id, cache.ServiceStatusPending, "", "")

	logger.Infof("重试端口转发: id=%s", id)
	recordAuditLog(c, model.ActionUpdateService, "port_forward", id, "", nil)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("重试成功", nil))
}

// Delete 删除端口转发
func (a *PortForwardAPI) Delete(c *gin.Context) {
	id := c.Param("id")

	var forward model.PortForward
	if err := db.DB.First(&forward, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("端口转发不存在"))
		return
	}

	if err := db.DB.Delete(&forward).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("删除失败"))
		return
	}

	// 清理运行时状态缓存
	cache.DeletePortForwardStatus(id)

	logger.Infof("删除端口转发: id=%s", id)
	recordAuditLog(c, model.ActionDeleteService, "port_forward", id, "", nil)

	// 通知配置变更
	if a.configNotify != nil {
		a.configNotify.NotifyConfigChange()
	}

	c.JSON(http.StatusOK, NewSuccessMessageResponse("删除成功", nil))
}
