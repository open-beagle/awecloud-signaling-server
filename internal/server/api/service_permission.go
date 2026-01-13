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
	config  *config.ServerConfig
	aclSync *headscale.ACLSyncService
}

// NewServicePermissionAPI 创建 ServicePermissionAPI
func NewServicePermissionAPI(cfg *config.ServerConfig) *ServicePermissionAPI {
	api := &ServicePermissionAPI{config: cfg}

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

// ========== 全局权限查询 ==========

// ServicePermissionItem 服务权限项（用于全局列表）
type ServicePermissionItem struct {
	ID          int64     `json:"id"`
	ServiceID   string    `json:"service_id"`
	ServiceName string    `json:"service_name"`
	AgentID     uint64    `json:"agent_id"`
	AgentName   string    `json:"agent_name"`
	ClientID    uint64    `json:"client_id"`
	ClientName  string    `json:"client_name"`
	GrantedAt   time.Time `json:"granted_at"`
}

// GetAllClientPermissions 获取所有服务的用户授权列表
func (a *ServicePermissionAPI) GetAllClientPermissions(c *gin.Context) {
	var perms []model.ServiceClientPermission
	if err := db.DB.Preload("Client").Preload("Service").Preload("Service.Agent").Find(&perms).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询失败"))
		return
	}

	result := make([]ServicePermissionItem, 0, len(perms))
	for _, p := range perms {
		item := ServicePermissionItem{
			ID:        p.ID,
			ServiceID: p.ServiceID,
			GrantedAt: p.GrantedAt,
		}
		if p.Service != nil {
			item.ServiceName = p.Service.Name
			if p.Service.Agent != nil {
				item.AgentID = p.Service.Agent.ID
				item.AgentName = p.Service.Agent.Name
			}
		}
		if p.Client != nil {
			item.ClientID = p.Client.ID
			item.ClientName = p.Client.Name
		}
		result = append(result, item)
	}

	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

// AgentPermissionItem Agent 授权项（用于全局列表）
type AgentPermissionItem struct {
	ID              int64     `json:"id"`
	ServiceID       string    `json:"service_id"`
	ServiceName     string    `json:"service_name"`
	OwnerAgentID    uint64    `json:"owner_agent_id"`
	OwnerAgentName  string    `json:"owner_agent_name"`
	AccessAgentID   uint64    `json:"access_agent_id"`
	AccessAgentName string    `json:"access_agent_name"`
	GrantedAt       time.Time `json:"granted_at"`
}

// GetAllAgentPermissions 获取所有服务的 Agent 授权列表
func (a *ServicePermissionAPI) GetAllAgentPermissions(c *gin.Context) {
	var perms []model.ServiceAgentPermission
	if err := db.DB.Preload("Agent").Preload("Service").Preload("Service.Agent").Find(&perms).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询失败"))
		return
	}

	result := make([]AgentPermissionItem, 0, len(perms))
	for _, p := range perms {
		item := AgentPermissionItem{
			ID:        p.ID,
			ServiceID: p.ServiceID,
			GrantedAt: p.GrantedAt,
		}
		if p.Service != nil {
			item.ServiceName = p.Service.Name
			if p.Service.Agent != nil {
				item.OwnerAgentID = p.Service.Agent.ID
				item.OwnerAgentName = p.Service.Agent.Name
			}
		}
		if p.Agent != nil {
			item.AccessAgentID = p.Agent.ID
			item.AccessAgentName = p.Agent.Name
		}
		result = append(result, item)
	}

	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

// ========== 桌面授权 - 用户授权 ==========

// AuthorizedClient 已授权用户
type AuthorizedClient struct {
	ID        uint64    `json:"id"`
	Name      string    `json:"name"`
	Alias     string    `json:"alias"`
	GrantedAt time.Time `json:"granted_at"`
}

// GetClients 获取服务的用户授权列表
func (a *ServicePermissionAPI) GetClients(c *gin.Context) {
	serviceID := c.Param("id")

	var perms []model.ServiceClientPermission
	if err := db.DB.Preload("Client").Where("service_id = ?", serviceID).Find(&perms).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询失败"))
		return
	}

	result := make([]AuthorizedClient, 0, len(perms))
	for _, p := range perms {
		if p.Client != nil {
			result = append(result, AuthorizedClient{
				ID:        p.Client.ID,
				Name:      p.Client.Name,
				Alias:     p.Client.Alias,
				GrantedAt: p.GrantedAt,
			})
		}
	}

	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

// AddClientRequest 添加用户授权请求
type AddClientRequest struct {
	ClientID uint64 `json:"client_id" binding:"required"`
}

// AddClient 添加用户授权
func (a *ServicePermissionAPI) AddClient(c *gin.Context) {
	serviceID := c.Param("id")

	var req AddClientRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	// 验证服务存在
	var service model.ProxyService
	if err := db.DB.First(&service, "id = ?", serviceID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("服务不存在"))
		return
	}

	// 验证 Client 存在
	var client model.Client
	if err := db.DB.First(&client, req.ClientID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Client 不存在"))
		return
	}

	// 检查是否已存在
	var existing model.ServiceClientPermission
	if err := db.DB.Where("service_id = ? AND client_id = ?", serviceID, req.ClientID).First(&existing).Error; err == nil {
		c.JSON(http.StatusConflict, NewErrorResponse("授权已存在"))
		return
	}

	perm := &model.ServiceClientPermission{
		ServiceID: serviceID,
		ClientID:  req.ClientID,
		GrantedAt: time.Now(),
	}

	if err := db.DB.Create(perm).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("添加失败"))
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

	logger.Infof("添加用户授权: service_id=%s, client_id=%d", serviceID, req.ClientID)
	recordAuditLog(c, model.ActionGrantDesktop, "service", serviceID, service.Name, map[string]interface{}{
		"client_id":   req.ClientID,
		"client_name": client.Name,
	})

	c.JSON(http.StatusOK, NewSuccessMessageResponse("添加成功", nil))
}

// RemoveClient 移除用户授权
func (a *ServicePermissionAPI) RemoveClient(c *gin.Context) {
	serviceID := c.Param("id")
	clientID, err := strconv.ParseUint(c.Param("cid"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 Client ID"))
		return
	}

	result := db.DB.Where("service_id = ? AND client_id = ?", serviceID, clientID).Delete(&model.ServiceClientPermission{})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("删除失败"))
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, NewErrorResponse("授权不存在"))
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

	logger.Infof("移除用户授权: service_id=%s, client_id=%d", serviceID, clientID)
	recordAuditLog(c, model.ActionRevokeDesktop, "service", serviceID, "", map[string]interface{}{
		"client_id": clientID,
	})

	c.JSON(http.StatusOK, NewSuccessMessageResponse("移除成功", nil))
}

// ========== 桌面授权 - 用户分组授权 ==========

// AuthorizedClientGroup 已授权用户分组
type AuthorizedClientGroup struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Alias       string    `json:"alias"`
	MemberCount int64     `json:"member_count"`
	GrantedAt   time.Time `json:"granted_at"`
}

// GetClientGroups 获取服务的用户分组授权列表
func (a *ServicePermissionAPI) GetClientGroups(c *gin.Context) {
	serviceID := c.Param("id")

	var perms []model.ServiceClientGroupPermission
	if err := db.DB.Preload("Group").Where("service_id = ?", serviceID).Find(&perms).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询失败"))
		return
	}

	result := make([]AuthorizedClientGroup, 0, len(perms))
	for _, p := range perms {
		if p.Group != nil {
			var memberCount int64
			db.DB.Model(&model.ClientGroupMember{}).Where("group_id = ?", p.Group.ID).Count(&memberCount)

			result = append(result, AuthorizedClientGroup{
				ID:          p.Group.ID,
				Name:        p.Group.Name,
				Alias:       p.Group.Alias,
				MemberCount: memberCount,
				GrantedAt:   p.GrantedAt,
			})
		}
	}

	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

// AddClientGroupRequest 添加用户分组授权请求
type AddClientGroupRequest struct {
	GroupID int64 `json:"group_id" binding:"required"`
}

// AddClientGroup 添加用户分组授权
func (a *ServicePermissionAPI) AddClientGroup(c *gin.Context) {
	serviceID := c.Param("id")

	var req AddClientGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	// 验证服务存在
	var service model.ProxyService
	if err := db.DB.First(&service, "id = ?", serviceID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("服务不存在"))
		return
	}

	// 验证分组存在
	var group model.ClientGroup
	if err := db.DB.First(&group, req.GroupID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("分组不存在"))
		return
	}

	// 检查是否已存在
	var existing model.ServiceClientGroupPermission
	if err := db.DB.Where("service_id = ? AND group_id = ?", serviceID, req.GroupID).First(&existing).Error; err == nil {
		c.JSON(http.StatusConflict, NewErrorResponse("授权已存在"))
		return
	}

	perm := &model.ServiceClientGroupPermission{
		ServiceID: serviceID,
		GroupID:   req.GroupID,
		GrantedAt: time.Now(),
	}

	if err := db.DB.Create(perm).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("添加失败"))
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

	logger.Infof("添加用户分组授权: service_id=%s, group_id=%d", serviceID, req.GroupID)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("添加成功", nil))
}

// RemoveClientGroup 移除用户分组授权
func (a *ServicePermissionAPI) RemoveClientGroup(c *gin.Context) {
	serviceID := c.Param("id")
	groupID, err := strconv.ParseInt(c.Param("gid"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的分组 ID"))
		return
	}

	result := db.DB.Where("service_id = ? AND group_id = ?", serviceID, groupID).Delete(&model.ServiceClientGroupPermission{})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("删除失败"))
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, NewErrorResponse("授权不存在"))
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

	logger.Infof("移除用户分组授权: service_id=%s, group_id=%d", serviceID, groupID)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("移除成功", nil))
}

// ========== 代理授权 - Agent 授权 ==========

// AuthorizedAgent 已授权 Agent
type AuthorizedAgent struct {
	ID        uint64    `json:"id"`
	Name      string    `json:"name"`
	Alias     string    `json:"alias"`
	GrantedAt time.Time `json:"granted_at"`
}

// GetAgents 获取服务的 Agent 授权列表
func (a *ServicePermissionAPI) GetAgents(c *gin.Context) {
	serviceID := c.Param("id")

	var perms []model.ServiceAgentPermission
	if err := db.DB.Preload("Agent").Where("service_id = ?", serviceID).Find(&perms).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询失败"))
		return
	}

	result := make([]AuthorizedAgent, 0, len(perms))
	for _, p := range perms {
		if p.Agent != nil {
			result = append(result, AuthorizedAgent{
				ID:        p.Agent.ID,
				Name:      p.Agent.Name,
				Alias:     p.Agent.Alias,
				GrantedAt: p.GrantedAt,
			})
		}
	}

	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

// AddAgentRequest 添加 Agent 授权请求
type AddAgentRequest struct {
	AgentID uint64 `json:"agent_id" binding:"required"`
}

// AddAgent 添加 Agent 授权
func (a *ServicePermissionAPI) AddAgent(c *gin.Context) {
	serviceID := c.Param("id")

	var req AddAgentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	// 验证服务存在
	var service model.ProxyService
	if err := db.DB.First(&service, "id = ?", serviceID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("服务不存在"))
		return
	}

	// 验证 Agent 存在
	var agent model.Agent
	if err := db.DB.First(&agent, req.AgentID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Agent 不存在"))
		return
	}

	// 检查是否已存在
	var existing model.ServiceAgentPermission
	if err := db.DB.Where("service_id = ? AND agent_id = ?", serviceID, req.AgentID).First(&existing).Error; err == nil {
		c.JSON(http.StatusConflict, NewErrorResponse("授权已存在"))
		return
	}

	perm := &model.ServiceAgentPermission{
		ServiceID: serviceID,
		AgentID:   req.AgentID,
		GrantedAt: time.Now(),
	}

	if err := db.DB.Create(perm).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("添加失败"))
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

	logger.Infof("添加 Agent 授权: service_id=%s, agent_id=%d", serviceID, req.AgentID)
	recordAuditLog(c, model.ActionGrantAgent, "service", serviceID, service.Name, map[string]interface{}{
		"agent_id":   req.AgentID,
		"agent_name": agent.Name,
	})

	c.JSON(http.StatusOK, NewSuccessMessageResponse("添加成功", nil))
}

// RemoveAgent 移除 Agent 授权
func (a *ServicePermissionAPI) RemoveAgent(c *gin.Context) {
	serviceID := c.Param("id")
	agentID, err := strconv.ParseUint(c.Param("aid"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 Agent ID"))
		return
	}

	result := db.DB.Where("service_id = ? AND agent_id = ?", serviceID, agentID).Delete(&model.ServiceAgentPermission{})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("删除失败"))
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, NewErrorResponse("授权不存在"))
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

	logger.Infof("移除 Agent 授权: service_id=%s, agent_id=%d", serviceID, agentID)
	recordAuditLog(c, model.ActionRevokeAgent, "service", serviceID, "", map[string]interface{}{
		"agent_id": agentID,
	})

	c.JSON(http.StatusOK, NewSuccessMessageResponse("移除成功", nil))
}

// ========== 代理授权 - Agent 分组授权 ==========

// AuthorizedAgentGroup 已授权 Agent 分组
type AuthorizedAgentGroup struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Alias       string    `json:"alias"`
	MemberCount int64     `json:"member_count"`
	GrantedAt   time.Time `json:"granted_at"`
}

// GetAgentGroups 获取服务的 Agent 分组授权列表
func (a *ServicePermissionAPI) GetAgentGroups(c *gin.Context) {
	serviceID := c.Param("id")

	var perms []model.ServiceAgentGroupPermission
	if err := db.DB.Preload("Group").Where("service_id = ?", serviceID).Find(&perms).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询失败"))
		return
	}

	result := make([]AuthorizedAgentGroup, 0, len(perms))
	for _, p := range perms {
		if p.Group != nil {
			var memberCount int64
			db.DB.Model(&model.AgentGroupMember{}).Where("group_id = ?", p.Group.ID).Count(&memberCount)

			result = append(result, AuthorizedAgentGroup{
				ID:          p.Group.ID,
				Name:        p.Group.Name,
				Alias:       p.Group.Alias,
				MemberCount: memberCount,
				GrantedAt:   p.GrantedAt,
			})
		}
	}

	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

// AddAgentGroupRequest 添加 Agent 分组授权请求
type AddAgentGroupRequest struct {
	GroupID int64 `json:"group_id" binding:"required"`
}

// AddAgentGroup 添加 Agent 分组授权
func (a *ServicePermissionAPI) AddAgentGroup(c *gin.Context) {
	serviceID := c.Param("id")

	var req AddAgentGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	// 验证服务存在
	var service model.ProxyService
	if err := db.DB.First(&service, "id = ?", serviceID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("服务不存在"))
		return
	}

	// 验证分组存在
	var group model.AgentGroup
	if err := db.DB.First(&group, req.GroupID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("分组不存在"))
		return
	}

	// 检查是否已存在
	var existing model.ServiceAgentGroupPermission
	if err := db.DB.Where("service_id = ? AND group_id = ?", serviceID, req.GroupID).First(&existing).Error; err == nil {
		c.JSON(http.StatusConflict, NewErrorResponse("授权已存在"))
		return
	}

	perm := &model.ServiceAgentGroupPermission{
		ServiceID: serviceID,
		GroupID:   req.GroupID,
		GrantedAt: time.Now(),
	}

	if err := db.DB.Create(perm).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("添加失败"))
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

	logger.Infof("添加 Agent 分组授权: service_id=%s, group_id=%d", serviceID, req.GroupID)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("添加成功", nil))
}

// RemoveAgentGroup 移除 Agent 分组授权
func (a *ServicePermissionAPI) RemoveAgentGroup(c *gin.Context) {
	serviceID := c.Param("id")
	groupID, err := strconv.ParseInt(c.Param("gid"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的分组 ID"))
		return
	}

	result := db.DB.Where("service_id = ? AND group_id = ?", serviceID, groupID).Delete(&model.ServiceAgentGroupPermission{})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("删除失败"))
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, NewErrorResponse("授权不存在"))
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

	logger.Infof("移除 Agent 分组授权: service_id=%s, group_id=%d", serviceID, groupID)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("移除成功", nil))
}
