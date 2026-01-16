package api

import (
	"encoding/json"
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

// SSHPermissionAPI SSH 授权管理 API
type SSHPermissionAPI struct {
	config  *config.ServerConfig
	aclSync *headscale.ACLSyncService
}

// NewSSHPermissionAPI 创建 SSHPermissionAPI
func NewSSHPermissionAPI(cfg *config.ServerConfig) *SSHPermissionAPI {
	api := &SSHPermissionAPI{config: cfg}

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

// ========== Desktop -> Agent SSH 授权 ==========

// SSHClientPermissionItem SSH 授权项
type SSHClientPermissionItem struct {
	ID         int64    `json:"id"`
	ClientID   uint64   `json:"client_id"`
	ClientName string   `json:"client_name"`
	AgentID    uint64   `json:"agent_id"`
	AgentName  string   `json:"agent_name"`
	SSHUsers   []string `json:"ssh_users"`
	Enabled    bool     `json:"enabled"`
	CreatedAt  string   `json:"created_at"`
}

// ListClientPermissions 获取所有 Desktop -> Agent SSH 授权
func (a *SSHPermissionAPI) ListClientPermissions(c *gin.Context) {
	var perms []model.SSHClientPermission
	if err := db.DB.Preload("Client").Preload("Agent").Find(&perms).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询失败"))
		return
	}

	result := make([]SSHClientPermissionItem, 0, len(perms))
	for _, p := range perms {
		item := SSHClientPermissionItem{
			ID:        p.ID,
			ClientID:  p.ClientID,
			AgentID:   p.AgentID,
			SSHUsers:  parseSSHUsersJSON(p.SSHUsers),
			Enabled:   p.Enabled,
			CreatedAt: p.CreatedAt.Format(time.RFC3339),
		}
		if p.Client != nil {
			item.ClientName = p.Client.Name
		}
		if p.Agent != nil {
			item.AgentName = p.Agent.Name
		}
		result = append(result, item)
	}

	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

// CreateClientPermissionRequest 创建 SSH 授权请求
type CreateClientPermissionRequest struct {
	ClientID uint64   `json:"client_id" binding:"required"`
	AgentID  uint64   `json:"agent_id" binding:"required"`
	SSHUsers []string `json:"ssh_users" binding:"required"`
}

// CreateClientPermission 创建 Desktop -> Agent SSH 授权
func (a *SSHPermissionAPI) CreateClientPermission(c *gin.Context) {
	var req CreateClientPermissionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	// 验证 Client 存在
	var client model.Client
	if err := db.DB.First(&client, req.ClientID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Client 不存在"))
		return
	}

	// 验证 Agent 存在
	var agent model.Agent
	if err := db.DB.First(&agent, req.AgentID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Agent 不存在"))
		return
	}

	// 检查是否已存在
	var existing model.SSHClientPermission
	if err := db.DB.Where("client_id = ? AND agent_id = ?", req.ClientID, req.AgentID).First(&existing).Error; err == nil {
		c.JSON(http.StatusConflict, NewErrorResponse("授权已存在"))
		return
	}

	// 序列化 SSHUsers
	sshUsersJSON, _ := json.Marshal(req.SSHUsers)

	perm := &model.SSHClientPermission{
		ClientID:  req.ClientID,
		AgentID:   req.AgentID,
		SSHUsers:  string(sshUsersJSON),
		Enabled:   true,
		CreatedAt: time.Now(),
	}

	if err := db.DB.Create(perm).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("创建失败"))
		return
	}

	// 同步 ACL
	a.syncACL()

	logger.Infof("创建 SSH 授权: client_id=%d, agent_id=%d, ssh_users=%v", req.ClientID, req.AgentID, req.SSHUsers)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("创建成功", map[string]interface{}{"id": perm.ID}))
}

// UpdateClientPermissionRequest 更新 SSH 授权请求
type UpdateClientPermissionRequest struct {
	SSHUsers []string `json:"ssh_users"`
	Enabled  *bool    `json:"enabled"`
}

// UpdateClientPermission 更新 Desktop -> Agent SSH 授权
func (a *SSHPermissionAPI) UpdateClientPermission(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	var req UpdateClientPermissionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	var perm model.SSHClientPermission
	if err := db.DB.First(&perm, id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("授权不存在"))
		return
	}

	// 更新字段
	updates := make(map[string]interface{})
	if req.SSHUsers != nil {
		sshUsersJSON, _ := json.Marshal(req.SSHUsers)
		updates["ssh_users"] = string(sshUsersJSON)
	}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}

	if len(updates) > 0 {
		if err := db.DB.Model(&perm).Updates(updates).Error; err != nil {
			c.JSON(http.StatusInternalServerError, NewErrorResponse("更新失败"))
			return
		}
	}

	// 同步 ACL
	a.syncACL()

	logger.Infof("更新 SSH 授权: id=%d", id)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("更新成功", nil))
}

// DeleteClientPermission 删除 Desktop -> Agent SSH 授权
func (a *SSHPermissionAPI) DeleteClientPermission(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	result := db.DB.Delete(&model.SSHClientPermission{}, id)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("删除失败"))
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, NewErrorResponse("授权不存在"))
		return
	}

	// 同步 ACL
	a.syncACL()

	logger.Infof("删除 SSH 授权: id=%d", id)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("删除成功", nil))
}

// ========== Desktop 分组 -> Agent SSH 授权 ==========

// SSHClientGroupPermissionItem SSH 分组授权项
type SSHClientGroupPermissionItem struct {
	ID        int64    `json:"id"`
	GroupID   int64    `json:"group_id"`
	GroupName string   `json:"group_name"`
	AgentID   uint64   `json:"agent_id"`
	AgentName string   `json:"agent_name"`
	SSHUsers  []string `json:"ssh_users"`
	Enabled   bool     `json:"enabled"`
	CreatedAt string   `json:"created_at"`
}

// ListClientGroupPermissions 获取所有 Desktop 分组 -> Agent SSH 授权
func (a *SSHPermissionAPI) ListClientGroupPermissions(c *gin.Context) {
	var perms []model.SSHClientGroupPermission
	if err := db.DB.Preload("Group").Preload("Agent").Find(&perms).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询失败"))
		return
	}

	result := make([]SSHClientGroupPermissionItem, 0, len(perms))
	for _, p := range perms {
		item := SSHClientGroupPermissionItem{
			ID:        p.ID,
			GroupID:   p.GroupID,
			AgentID:   p.AgentID,
			SSHUsers:  parseSSHUsersJSON(p.SSHUsers),
			Enabled:   p.Enabled,
			CreatedAt: p.CreatedAt.Format(time.RFC3339),
		}
		if p.Group != nil {
			item.GroupName = p.Group.Name
		}
		if p.Agent != nil {
			item.AgentName = p.Agent.Name
		}
		result = append(result, item)
	}

	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

// CreateClientGroupPermissionRequest 创建 SSH 分组授权请求
type CreateClientGroupPermissionRequest struct {
	GroupID  int64    `json:"group_id" binding:"required"`
	AgentID  uint64   `json:"agent_id" binding:"required"`
	SSHUsers []string `json:"ssh_users" binding:"required"`
}

// CreateClientGroupPermission 创建 Desktop 分组 -> Agent SSH 授权
func (a *SSHPermissionAPI) CreateClientGroupPermission(c *gin.Context) {
	var req CreateClientGroupPermissionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	// 验证分组存在
	var group model.ClientGroup
	if err := db.DB.First(&group, req.GroupID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("分组不存在"))
		return
	}

	// 验证 Agent 存在
	var agent model.Agent
	if err := db.DB.First(&agent, req.AgentID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Agent 不存在"))
		return
	}

	// 检查是否已存在
	var existing model.SSHClientGroupPermission
	if err := db.DB.Where("group_id = ? AND agent_id = ?", req.GroupID, req.AgentID).First(&existing).Error; err == nil {
		c.JSON(http.StatusConflict, NewErrorResponse("授权已存在"))
		return
	}

	// 序列化 SSHUsers
	sshUsersJSON, _ := json.Marshal(req.SSHUsers)

	perm := &model.SSHClientGroupPermission{
		GroupID:   req.GroupID,
		AgentID:   req.AgentID,
		SSHUsers:  string(sshUsersJSON),
		Enabled:   true,
		CreatedAt: time.Now(),
	}

	if err := db.DB.Create(perm).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("创建失败"))
		return
	}

	// 同步 ACL
	a.syncACL()

	logger.Infof("创建 SSH 分组授权: group_id=%d, agent_id=%d, ssh_users=%v", req.GroupID, req.AgentID, req.SSHUsers)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("创建成功", map[string]interface{}{"id": perm.ID}))
}

// UpdateClientGroupPermission 更新 Desktop 分组 -> Agent SSH 授权
func (a *SSHPermissionAPI) UpdateClientGroupPermission(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	var req UpdateClientPermissionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	var perm model.SSHClientGroupPermission
	if err := db.DB.First(&perm, id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("授权不存在"))
		return
	}

	// 更新字段
	updates := make(map[string]interface{})
	if req.SSHUsers != nil {
		sshUsersJSON, _ := json.Marshal(req.SSHUsers)
		updates["ssh_users"] = string(sshUsersJSON)
	}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}

	if len(updates) > 0 {
		if err := db.DB.Model(&perm).Updates(updates).Error; err != nil {
			c.JSON(http.StatusInternalServerError, NewErrorResponse("更新失败"))
			return
		}
	}

	// 同步 ACL
	a.syncACL()

	logger.Infof("更新 SSH 分组授权: id=%d", id)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("更新成功", nil))
}

// DeleteClientGroupPermission 删除 Desktop 分组 -> Agent SSH 授权
func (a *SSHPermissionAPI) DeleteClientGroupPermission(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	result := db.DB.Delete(&model.SSHClientGroupPermission{}, id)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("删除失败"))
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, NewErrorResponse("授权不存在"))
		return
	}

	// 同步 ACL
	a.syncACL()

	logger.Infof("删除 SSH 分组授权: id=%d", id)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("删除成功", nil))
}

// ========== Agent SSH 统计 ==========

// AgentSSHStatsItem Agent SSH 统计项
type AgentSSHStatsItem struct {
	ID               uint64 `json:"id"`
	Name             string `json:"name"`
	Alias            string `json:"alias"`
	TailscaleIP      string `json:"tailscale_ip"`
	ClientGroupCount int64  `json:"client_group_count"`
	ClientCount      int64  `json:"client_count"`
}

// GetAgentSSHStats 获取所有 Agent 的 SSH 授权统计
func (a *SSHPermissionAPI) GetAgentSSHStats(c *gin.Context) {
	// 获取所有启用 SSH 的 Agent
	var agents []model.Agent
	if err := db.DB.Where("ssh_enabled = ?", true).Find(&agents).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询失败"))
		return
	}

	result := make([]AgentSSHStatsItem, 0, len(agents))
	for _, agent := range agents {
		// 统计用户授权数
		var clientCount int64
		db.DB.Model(&model.SSHClientPermission{}).Where("agent_id = ? AND enabled = ?", agent.ID, true).Count(&clientCount)

		// 统计分组授权数
		var groupCount int64
		db.DB.Model(&model.SSHClientGroupPermission{}).Where("agent_id = ? AND enabled = ?", agent.ID, true).Count(&groupCount)

		result = append(result, AgentSSHStatsItem{
			ID:               agent.ID,
			Name:             agent.Name,
			Alias:            agent.Alias,
			TailscaleIP:      agent.IP,
			ClientGroupCount: groupCount,
			ClientCount:      clientCount,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
		"total":   len(result),
	})
}

// GetAgentSSHPermissions 获取指定 Agent 的 SSH 授权详情
func (a *SSHPermissionAPI) GetAgentSSHPermissions(c *gin.Context) {
	agentID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 Agent ID"))
		return
	}

	// 验证 Agent 存在
	var agent model.Agent
	if err := db.DB.First(&agent, agentID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Agent 不存在"))
		return
	}

	// 获取用户授权
	var clientPerms []model.SSHClientPermission
	db.DB.Preload("Client").Where("agent_id = ?", agentID).Find(&clientPerms)

	clientPermItems := make([]map[string]interface{}, 0, len(clientPerms))
	for _, p := range clientPerms {
		item := map[string]interface{}{
			"id":         p.ID,
			"client_id":  p.ClientID,
			"ssh_users":  parseSSHUsersJSON(p.SSHUsers),
			"enabled":    p.Enabled,
			"created_at": p.CreatedAt.Format(time.RFC3339),
		}
		if p.Client != nil {
			item["client_name"] = p.Client.Name
			item["client_ip"] = "" // Client 没有 IP 字段，留空
		}
		clientPermItems = append(clientPermItems, item)
	}

	// 获取分组授权
	var groupPerms []model.SSHClientGroupPermission
	db.DB.Preload("Group").Where("agent_id = ?", agentID).Find(&groupPerms)

	groupPermItems := make([]map[string]interface{}, 0, len(groupPerms))
	for _, p := range groupPerms {
		item := map[string]interface{}{
			"id":         p.ID,
			"group_id":   p.GroupID,
			"ssh_users":  parseSSHUsersJSON(p.SSHUsers),
			"enabled":    p.Enabled,
			"created_at": p.CreatedAt.Format(time.RFC3339),
		}
		if p.Group != nil {
			item["group_name"] = p.Group.Name
			// 统计分组成员数
			var memberCount int64
			db.DB.Model(&model.ClientGroupMember{}).Where("group_id = ?", p.GroupID).Count(&memberCount)
			item["member_count"] = memberCount
		}
		groupPermItems = append(groupPermItems, item)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"agent": gin.H{
				"id":           agent.ID,
				"name":         agent.Name,
				"alias":        agent.Alias,
				"tailscale_ip": agent.IP,
			},
			"client_permissions": clientPermItems,
			"group_permissions":  groupPermItems,
		},
	})
}

// ========== 辅助函数 ==========

// syncACL 同步 ACL 到 Headscale
func (a *SSHPermissionAPI) syncACL() {
	if a.aclSync != nil {
		go func() {
			if err := a.aclSync.SyncACL(nil); err != nil {
				logger.Warnf("同步 ACL 失败: %v", err)
			}
		}()
	}
}

// parseSSHUsersJSON 解析 SSHUsers JSON 字符串
func parseSSHUsersJSON(jsonStr string) []string {
	if jsonStr == "" {
		return []string{}
	}
	var users []string
	if err := json.Unmarshal([]byte(jsonStr), &users); err != nil {
		return []string{}
	}
	return users
}
