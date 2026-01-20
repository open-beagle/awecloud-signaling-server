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
	configNotify ConfigNotifier
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
	UserID          uint64 `json:"user_id" binding:"required"`
	TargetServiceID string `json:"target_service_id" binding:"required"`
	SourceAddr      string `json:"source_addr" binding:"required"`
}

// Create 创建端口转发
func (a *PortForwardAPI) Create(c *gin.Context) {
	ctx := c.Request.Context()
	var req CreateForwardRequest
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
		c.JSON(http.StatusBadRequest, NewErrorResponse("只有 Agent 用户可以创建端口转发"))
		return
	}

	// 验证目标服务存在
	var targetService model.ProxyService
	if err := db.DB.WithContext(ctx).First(&targetService, "id = ?", req.TargetServiceID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("目标服务不存在"))
		return
	}

	// 检查是否已存在相同的转发配置
	var existing model.PortForward
	if err := db.DB.WithContext(ctx).Where("user_id = ? AND source_addr = ?", req.UserID, req.SourceAddr).First(&existing).Error; err == nil {
		c.JSON(http.StatusConflict, NewErrorResponse("该源地址已被使用"))
		return
	}

	targetAddr := targetService.SourceAddr

	forward := &model.PortForward{
		ID:              uuid.New().String(),
		UserID:          req.UserID,
		TargetServiceID: req.TargetServiceID,
		SourceAddr:      req.SourceAddr,
		TargetAddr:      targetAddr,
		Enabled:         true,
	}

	if err := db.DB.WithContext(ctx).Create(forward).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("创建失败: "+err.Error()))
		return
	}

	cache.UpdatePortForwardStatus(forward.ID, cache.ServiceStatusPending, "", "")

	logger.Infof("创建端口转发: id=%s, user_id=%d, target_service_id=%s", forward.ID, forward.UserID, forward.TargetServiceID)
	recordAuditLog(ctx, c, model.ActionCreatePortForward, "port_forward", forward.ID, "", nil)

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
	ctx := c.Request.Context()
	id := c.Param("id")

	var req UpdateForwardRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	var forward model.PortForward
	if err := db.DB.WithContext(ctx).First(&forward, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("端口转发不存在"))
		return
	}

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

	if err := db.DB.WithContext(ctx).Model(&forward).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("更新失败"))
		return
	}

	logger.Infof("更新端口转发: id=%s", id)
	recordAuditLog(ctx, c, model.ActionUpdatePortForward, "port_forward", id, "", nil)

	if a.configNotify != nil {
		a.configNotify.NotifyConfigChange()
	}

	c.JSON(http.StatusOK, NewSuccessMessageResponse("更新成功", nil))
}

// Toggle 启用/禁用端口转发
func (a *PortForwardAPI) Toggle(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	var forward model.PortForward
	if err := db.DB.WithContext(ctx).First(&forward, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("端口转发不存在"))
		return
	}

	if err := db.DB.WithContext(ctx).Model(&forward).Update("enabled", req.Enabled).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("更新失败"))
		return
	}

	if req.Enabled {
		cache.UpdatePortForwardStatus(id, cache.ServiceStatusPending, "", "")
	}

	logger.Infof("切换端口转发状态: id=%s, enabled=%v", id, req.Enabled)
	recordAuditLog(ctx, c, model.ActionTogglePortForward, "port_forward", id, "", nil)

	if a.configNotify != nil {
		a.configNotify.NotifyConfigChange()
	}

	c.JSON(http.StatusOK, NewSuccessMessageResponse("操作成功", nil))
}

// Retry 重试错误状态的端口转发
func (a *PortForwardAPI) Retry(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	var forward model.PortForward
	if err := db.DB.WithContext(ctx).First(&forward, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("端口转发不存在"))
		return
	}

	runtimeStatus := cache.GetPortForwardStatus(id)
	if runtimeStatus == nil || runtimeStatus.Status != cache.ServiceStatusError {
		c.JSON(http.StatusBadRequest, NewErrorResponse("只有错误状态的端口转发才能重试"))
		return
	}

	cache.UpdatePortForwardStatus(id, cache.ServiceStatusPending, "", "")

	logger.Infof("重试端口转发: id=%s", id)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("重试成功", nil))
}

// Delete 删除端口转发
func (a *PortForwardAPI) Delete(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	var forward model.PortForward
	if err := db.DB.WithContext(ctx).First(&forward, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("端口转发不存在"))
		return
	}

	if err := db.DB.WithContext(ctx).Delete(&forward).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("删除失败"))
		return
	}

	cache.DeletePortForwardStatus(id)

	logger.Infof("删除端口转发: id=%s", id)
	recordAuditLog(ctx, c, model.ActionDeletePortForward, "port_forward", id, "", nil)

	if a.configNotify != nil {
		a.configNotify.NotifyConfigChange()
	}

	c.JSON(http.StatusOK, NewSuccessMessageResponse("删除成功", nil))
}
