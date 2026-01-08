package api

import (
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

// AgentServicePermissionAPI Agent 服务权限管理 API
type AgentServicePermissionAPI struct {
	aclSync *headscale.ACLSyncService
}

// NewAgentServicePermissionAPI 创建 AgentServicePermissionAPI
func NewAgentServicePermissionAPI(cfg *config.ServerConfig) *AgentServicePermissionAPI {
	api := &AgentServicePermissionAPI{}

	// 初始化 ACL 同步服务
	if cfg.Tailscale.HeadscaleURL != "" && cfg.Tailscale.HeadscaleAPIKey != "" {
		client := headscale.NewClient(headscale.Config{
			URL:    cfg.Tailscale.HeadscaleURL,
			APIKey: cfg.Tailscale.HeadscaleAPIKey,
		})
		api.aclSync = headscale.NewACLSyncService(client)
	}

	return api
}

// AgentPermissionResponse API 响应
type AgentPermissionResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

// AgentServicePermissionInfo Agent 服务权限信息
type AgentServicePermissionInfo struct {
	ID          int64     `json:"id"`
	AgentID     int64     `json:"agent_id"`
	AgentName   string    `json:"agent_name"`
	AgentIP     string    `json:"agent_ip"`
	ServiceID   int64     `json:"service_id"`
	ServiceName string    `json:"service_name"`
	ServiceAddr string    `json:"service_addr"` // IP:Port
	GrantedBy   int64     `json:"granted_by"`
	GrantedAt   time.Time `json:"granted_at"`
}

// ListAgentServicePermissions 获取 Agent 服务权限列表
func (a *AgentServicePermissionAPI) ListAgentServicePermissions(c *gin.Context) {
	var perms []model.AgentServicePermission
	if err := db.DB.Preload("Agent").Preload("Service").Preload("Service.Agent").
		Order("granted_at DESC").
		Find(&perms).Error; err != nil {
		c.JSON(http.StatusInternalServerError, AgentPermissionResponse{
			Success: false,
			Message: "查询失败",
		})
		return
	}

	// 转换为响应格式
	result := make([]AgentServicePermissionInfo, 0, len(perms))
	for _, p := range perms {
		info := AgentServicePermissionInfo{
			ID:        p.ID,
			AgentID:   p.AgentID,
			ServiceID: p.ServiceID,
			GrantedBy: p.GrantedBy,
			GrantedAt: p.GrantedAt,
		}
		if p.Agent != nil {
			info.AgentName = p.Agent.AgentName
			info.AgentIP = p.Agent.TailscaleIP
		}
		if p.Service != nil {
			info.ServiceName = p.Service.Name
			if p.Service.Agent != nil {
				info.ServiceAddr = p.Service.Agent.TailscaleIP + ":" + strconv.Itoa(p.Service.ListenPort)
			}
		}
		result = append(result, info)
	}

	c.JSON(http.StatusOK, AgentPermissionResponse{
		Success: true,
		Data:    result,
	})
}

// AddAgentServicePermissionRequest 添加 Agent 服务权限请求
type AddAgentServicePermissionRequest struct {
	AgentID    int64   `json:"agent_id" binding:"required"`
	ServiceIDs []int64 `json:"service_ids" binding:"required"` // 支持批量授权
}

// AddAgentServicePermission 添加 Agent 服务权限
func (a *AgentServicePermissionAPI) AddAgentServicePermission(c *gin.Context) {
	var req AddAgentServicePermissionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, AgentPermissionResponse{
			Success: false,
			Message: "请求参数错误",
		})
		return
	}

	// 验证 Agent 存在
	var agent model.Agent
	if err := db.DB.First(&agent, req.AgentID).Error; err != nil {
		c.JSON(http.StatusNotFound, AgentPermissionResponse{
			Success: false,
			Message: "Agent 不存在",
		})
		return
	}

	// 获取当前管理员 ID
	adminID := getAdminIDFromContext(c)

	// 批量添加权限
	for _, serviceID := range req.ServiceIDs {
		// 验证服务存在
		var service model.ProxyService
		if err := db.DB.First(&service, serviceID).Error; err != nil {
			logger.Warnf("服务不存在: %d", serviceID)
			continue
		}

		// 检查是否已存在权限
		var existing model.AgentServicePermission
		if err := db.DB.Where("agent_id = ? AND service_id = ?", req.AgentID, serviceID).First(&existing).Error; err == nil {
			logger.Debugf("权限已存在: agent_id=%d, service_id=%d", req.AgentID, serviceID)
			continue
		}

		// 添加权限
		if a.aclSync != nil {
			if err := a.aclSync.AddAgentServicePermission(c.Request.Context(), req.AgentID, serviceID, adminID); err != nil {
				logger.Errorf("添加权限失败: %v", err)
				continue
			}
		} else {
			perm := &model.AgentServicePermission{
				AgentID:   req.AgentID,
				ServiceID: serviceID,
				GrantedBy: adminID,
				GrantedAt: time.Now(),
			}
			if err := db.DB.Create(perm).Error; err != nil {
				logger.Errorf("添加权限失败: %v", err)
				continue
			}
		}

		logger.Infof("添加 Agent 服务权限: agent_id=%d, service_id=%d", req.AgentID, serviceID)
	}

	c.JSON(http.StatusOK, AgentPermissionResponse{
		Success: true,
		Message: "添加成功",
	})
}

// RemoveAgentServicePermission 删除 Agent 服务权限
func (a *AgentServicePermissionAPI) RemoveAgentServicePermission(c *gin.Context) {
	permID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, AgentPermissionResponse{
			Success: false,
			Message: "无效的权限 ID",
		})
		return
	}

	// 验证权限存在
	var perm model.AgentServicePermission
	if err := db.DB.First(&perm, permID).Error; err != nil {
		c.JSON(http.StatusNotFound, AgentPermissionResponse{
			Success: false,
			Message: "权限不存在",
		})
		return
	}

	// 删除权限
	if a.aclSync != nil {
		if err := a.aclSync.RemoveAgentServicePermission(c.Request.Context(), permID); err != nil {
			c.JSON(http.StatusInternalServerError, AgentPermissionResponse{
				Success: false,
				Message: "删除权限失败: " + err.Error(),
			})
			return
		}
	} else {
		if err := db.DB.Delete(&model.AgentServicePermission{}, permID).Error; err != nil {
			c.JSON(http.StatusInternalServerError, AgentPermissionResponse{
				Success: false,
				Message: "删除权限失败: " + err.Error(),
			})
			return
		}
	}

	logger.Infof("删除 Agent 服务权限: id=%d", permID)

	c.JSON(http.StatusOK, AgentPermissionResponse{
		Success: true,
		Message: "删除成功",
	})
}
