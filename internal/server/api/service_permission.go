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

// ServicePermissionAPI 服务权限管理 API
type ServicePermissionAPI struct {
	aclSync *headscale.ACLSyncService
}

// NewServicePermissionAPI 创建 ServicePermissionAPI
func NewServicePermissionAPI(cfg *config.ServerConfig) *ServicePermissionAPI {
	api := &ServicePermissionAPI{}

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

// PermissionResponse API 响应
type PermissionResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

// ServicePermissionInfo 服务权限信息
type ServicePermissionInfo struct {
	ID          int64      `json:"id"`
	ServiceID   int64      `json:"service_id"`
	ServiceName string     `json:"service_name"`
	ClientID    int64      `json:"client_id"`
	ClientName  string     `json:"client_name"`
	GrantedBy   int64      `json:"granted_by"`
	GrantedAt   time.Time  `json:"granted_at"`
	ExpiresAt   *time.Time `json:"expires_at"`
}

// ListServicePermissions 获取服务的权限列表
func (a *ServicePermissionAPI) ListServicePermissions(c *gin.Context) {
	serviceID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, PermissionResponse{
			Success: false,
			Message: "无效的服务 ID",
		})
		return
	}

	var perms []model.ServicePermission
	if err := db.DB.Preload("Service").Preload("Client").
		Where("service_id = ?", serviceID).
		Order("granted_at DESC").
		Find(&perms).Error; err != nil {
		c.JSON(http.StatusInternalServerError, PermissionResponse{
			Success: false,
			Message: "查询失败",
		})
		return
	}

	// 转换为响应格式
	result := make([]ServicePermissionInfo, 0, len(perms))
	for _, p := range perms {
		info := ServicePermissionInfo{
			ID:        p.ID,
			ServiceID: p.ServiceID,
			ClientID:  p.ClientID,
			GrantedBy: p.GrantedBy,
			GrantedAt: p.GrantedAt,
			ExpiresAt: p.ExpiresAt,
		}
		if p.Service != nil {
			info.ServiceName = p.Service.Name
		}
		if p.Client != nil {
			info.ClientName = p.Client.ClientID
		}
		result = append(result, info)
	}

	c.JSON(http.StatusOK, PermissionResponse{
		Success: true,
		Data:    result,
	})
}

// AddServicePermissionRequest 添加服务权限请求
type AddServicePermissionRequest struct {
	ClientID  int64  `json:"client_id" binding:"required"`
	ExpiresAt string `json:"expires_at"` // RFC3339 格式，可选
}

// AddServicePermission 添加服务权限
func (a *ServicePermissionAPI) AddServicePermission(c *gin.Context) {
	serviceID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, PermissionResponse{
			Success: false,
			Message: "无效的服务 ID",
		})
		return
	}

	var req AddServicePermissionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, PermissionResponse{
			Success: false,
			Message: "请求参数错误",
		})
		return
	}

	// 验证服务存在
	var service model.ProxyService
	if err := db.DB.First(&service, serviceID).Error; err != nil {
		c.JSON(http.StatusNotFound, PermissionResponse{
			Success: false,
			Message: "服务不存在",
		})
		return
	}

	// 验证 Client 存在
	var client model.Client
	if err := db.DB.First(&client, req.ClientID).Error; err != nil {
		c.JSON(http.StatusNotFound, PermissionResponse{
			Success: false,
			Message: "Client 不存在",
		})
		return
	}

	// 检查是否已存在权限
	var existing model.ServicePermission
	if err := db.DB.Where("service_id = ? AND client_id = ?", serviceID, req.ClientID).First(&existing).Error; err == nil {
		c.JSON(http.StatusConflict, PermissionResponse{
			Success: false,
			Message: "权限已存在",
		})
		return
	}

	// 解析过期时间
	var expiresAt *time.Time
	if req.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, req.ExpiresAt)
		if err != nil {
			c.JSON(http.StatusBadRequest, PermissionResponse{
				Success: false,
				Message: "过期时间格式错误",
			})
			return
		}
		expiresAt = &t
	}

	// 获取当前管理员 ID
	adminID := getAdminIDFromContext(c)

	// 添加权限
	if a.aclSync != nil {
		if err := a.aclSync.AddServicePermission(c.Request.Context(), serviceID, req.ClientID, adminID, expiresAt); err != nil {
			c.JSON(http.StatusInternalServerError, PermissionResponse{
				Success: false,
				Message: "添加权限失败: " + err.Error(),
			})
			return
		}
	} else {
		// 没有 ACL 同步服务，直接创建记录
		perm := &model.ServicePermission{
			ServiceID: serviceID,
			ClientID:  req.ClientID,
			GrantedBy: adminID,
			GrantedAt: time.Now(),
			ExpiresAt: expiresAt,
		}
		if err := db.DB.Create(perm).Error; err != nil {
			c.JSON(http.StatusInternalServerError, PermissionResponse{
				Success: false,
				Message: "添加权限失败: " + err.Error(),
			})
			return
		}
	}

	logger.Infof("添加服务权限: service_id=%d, client_id=%d, granted_by=%d", serviceID, req.ClientID, adminID)

	c.JSON(http.StatusOK, PermissionResponse{
		Success: true,
		Message: "添加成功",
	})
}

// RemoveServicePermission 删除服务权限
func (a *ServicePermissionAPI) RemoveServicePermission(c *gin.Context) {
	permID, err := strconv.ParseInt(c.Param("pid"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, PermissionResponse{
			Success: false,
			Message: "无效的权限 ID",
		})
		return
	}

	// 验证权限存在
	var perm model.ServicePermission
	if err := db.DB.First(&perm, permID).Error; err != nil {
		c.JSON(http.StatusNotFound, PermissionResponse{
			Success: false,
			Message: "权限不存在",
		})
		return
	}

	// 删除权限
	if a.aclSync != nil {
		if err := a.aclSync.RemoveServicePermission(c.Request.Context(), permID); err != nil {
			c.JSON(http.StatusInternalServerError, PermissionResponse{
				Success: false,
				Message: "删除权限失败: " + err.Error(),
			})
			return
		}
	} else {
		if err := db.DB.Delete(&model.ServicePermission{}, permID).Error; err != nil {
			c.JSON(http.StatusInternalServerError, PermissionResponse{
				Success: false,
				Message: "删除权限失败: " + err.Error(),
			})
			return
		}
	}

	logger.Infof("删除服务权限: id=%d", permID)

	c.JSON(http.StatusOK, PermissionResponse{
		Success: true,
		Message: "删除成功",
	})
}

// UpdateAccessTypeRequest 更新访问类型请求
type UpdateAccessTypeRequest struct {
	AccessType string `json:"access_type" binding:"required"` // public, private, group
	GroupID    *int64 `json:"group_id"`                       // group 类型时必填
}

// UpdateServiceAccessType 更新服务访问类型
func (a *ServicePermissionAPI) UpdateServiceAccessType(c *gin.Context) {
	serviceID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, PermissionResponse{
			Success: false,
			Message: "无效的服务 ID",
		})
		return
	}

	var req UpdateAccessTypeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, PermissionResponse{
			Success: false,
			Message: "请求参数错误",
		})
		return
	}

	// 验证访问类型
	if req.AccessType != model.AccessTypePublic &&
		req.AccessType != model.AccessTypePrivate &&
		req.AccessType != model.AccessTypeGroup {
		c.JSON(http.StatusBadRequest, PermissionResponse{
			Success: false,
			Message: "无效的访问类型",
		})
		return
	}

	// group 类型需要指定组
	if req.AccessType == model.AccessTypeGroup && req.GroupID == nil {
		c.JSON(http.StatusBadRequest, PermissionResponse{
			Success: false,
			Message: "group 类型需要指定组 ID",
		})
		return
	}

	// 验证服务存在
	var service model.ProxyService
	if err := db.DB.First(&service, serviceID).Error; err != nil {
		c.JSON(http.StatusNotFound, PermissionResponse{
			Success: false,
			Message: "服务不存在",
		})
		return
	}

	// 更新访问类型
	if a.aclSync != nil {
		if err := a.aclSync.UpdateServiceAccessType(c.Request.Context(), serviceID, req.AccessType, req.GroupID); err != nil {
			c.JSON(http.StatusInternalServerError, PermissionResponse{
				Success: false,
				Message: "更新失败: " + err.Error(),
			})
			return
		}
	} else {
		updates := map[string]interface{}{
			"access_type": req.AccessType,
			"group_id":    req.GroupID,
		}
		if err := db.DB.Model(&model.ProxyService{}).Where("id = ?", serviceID).Updates(updates).Error; err != nil {
			c.JSON(http.StatusInternalServerError, PermissionResponse{
				Success: false,
				Message: "更新失败: " + err.Error(),
			})
			return
		}
	}

	logger.Infof("更新服务访问类型: service_id=%d, access_type=%s", serviceID, req.AccessType)

	c.JSON(http.StatusOK, PermissionResponse{
		Success: true,
		Message: "更新成功",
	})
}

// getAdminIDFromContext 从上下文获取管理员 ID
func getAdminIDFromContext(c *gin.Context) int64 {
	if adminID, exists := c.Get("admin_id"); exists {
		if id, ok := adminID.(int64); ok {
			return id
		}
	}
	return 0
}
