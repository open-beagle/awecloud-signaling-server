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

// AgentPermissionAPI Agent 级别授权管理 API
type AgentPermissionAPI struct {
	config  *config.ServerConfig
	aclSync *headscale.ACLSyncService
}

// NewAgentPermissionAPI 创建 AgentPermissionAPI
func NewAgentPermissionAPI(cfg *config.ServerConfig) *AgentPermissionAPI {
	api := &AgentPermissionAPI{config: cfg}

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

// syncACL 异步同步 ACL
func (a *AgentPermissionAPI) syncACL() {
	if a.aclSync != nil {
		go func() {
			if err := a.aclSync.SyncACL(nil); err != nil {
				logger.Warnf("同步 ACL 失败: %v", err)
			}
		}()
	}
}

// ========== Agent 授权统计 ==========

// AgentAuthStats Agent 授权统计
type AgentAuthStats struct {
	ID               uint64 `json:"id"`
	Name             string `json:"name"`
	Alias            string `json:"alias"`
	TailscaleIP      string `json:"tailscale_ip"`
	ServiceCount     int64  `json:"service_count"`
	ClientCount      int64  `json:"client_count"`
	ClientGroupCount int64  `json:"client_group_count"`
	AgentCount       int64  `json:"agent_count"`
	AgentGroupCount  int64  `json:"agent_group_count"`
}

// GetAgentAuthStats 获取 Agent 列表（带授权统计）
func (a *AgentPermissionAPI) GetAgentAuthStats(c *gin.Context) {
	var agents []model.Agent
	if err := db.DB.Find(&agents).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询失败"))
		return
	}

	result := make([]AgentAuthStats, 0, len(agents))
	for _, agent := range agents {
		stats := AgentAuthStats{
			ID:          agent.ID,
			Name:        agent.Name,
			Alias:       agent.Alias,
			TailscaleIP: agent.IP,
		}

		// 统计服务数量
		db.DB.Model(&model.ProxyService{}).Where("agent_id = ?", agent.ID).Count(&stats.ServiceCount)

		// 统计 Client 授权数量
		db.DB.Model(&model.AgentClientPermission{}).Where("agent_id = ?", agent.ID).Count(&stats.ClientCount)

		// 统计 ClientGroup 授权数量
		db.DB.Model(&model.AgentClientGroupPermission{}).Where("agent_id = ?", agent.ID).Count(&stats.ClientGroupCount)

		// 统计 Agent 授权数量（作为目标 Agent）
		db.DB.Model(&model.AgentAgentPermission{}).Where("target_agent_id = ?", agent.ID).Count(&stats.AgentCount)

		// 统计 AgentGroup 授权数量
		db.DB.Model(&model.AgentAgentGroupPermission{}).Where("target_agent_id = ?", agent.ID).Count(&stats.AgentGroupCount)

		result = append(result, stats)
	}

	c.JSON(http.StatusOK, NewPagedResponse(result, int64(len(result)), 1, len(result)))
}

// ========== Agent-Client 授权 ==========

// AgentAuthorizedClient 已授权 Client
type AgentAuthorizedClient struct {
	ID         int64     `json:"id"`          // 授权记录 ID
	ClientID   uint64    `json:"client_id"`   // Client ID
	ClientName string    `json:"client_name"` // Client 名称
	GrantedAt  time.Time `json:"granted_at"`  // 授权时间
}

// GetClientPermissions 获取 Agent 的 Client 授权列表
func (a *AgentPermissionAPI) GetClientPermissions(c *gin.Context) {
	agentID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 Agent ID"))
		return
	}

	var perms []model.AgentClientPermission
	if err := db.DB.Preload("Client").Where("agent_id = ?", agentID).Find(&perms).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询失败"))
		return
	}

	result := make([]AgentAuthorizedClient, 0, len(perms))
	for _, p := range perms {
		if p.Client != nil {
			result = append(result, AgentAuthorizedClient{
				ID:         p.ID,
				ClientID:   p.Client.ID,
				ClientName: p.Client.Name,
				GrantedAt:  p.GrantedAt,
			})
		}
	}

	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

// AddClientPermissionRequest 添加 Client 授权请求
type AddClientPermissionRequest struct {
	ClientID uint64 `json:"client_id" binding:"required"`
}

// AddClientPermission 添加 Client 授权
func (a *AgentPermissionAPI) AddClientPermission(c *gin.Context) {
	agentID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 Agent ID"))
		return
	}

	var req AddClientPermissionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	// 验证 Agent 存在
	var agent model.Agent
	if err := db.DB.First(&agent, agentID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Agent 不存在"))
		return
	}

	// 验证 Client 存在
	var client model.Client
	if err := db.DB.First(&client, req.ClientID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Client 不存在"))
		return
	}

	// 检查是否已存在
	var existing model.AgentClientPermission
	if err := db.DB.Where("agent_id = ? AND client_id = ?", agentID, req.ClientID).First(&existing).Error; err == nil {
		c.JSON(http.StatusConflict, NewErrorResponse("授权已存在"))
		return
	}

	perm := &model.AgentClientPermission{
		AgentID:   agentID,
		ClientID:  req.ClientID,
		GrantedAt: time.Now(),
	}

	if err := db.DB.Create(perm).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("添加失败"))
		return
	}

	a.syncACL()

	logger.Infof("添加 Agent-Client 授权: agent_id=%d, client_id=%d", agentID, req.ClientID)
	recordAuditLog(c, model.ActionGrantDesktop, "agent", strconv.FormatUint(agentID, 10), agent.Name, map[string]interface{}{
		"client_id":   req.ClientID,
		"client_name": client.Name,
		"level":       "agent",
	})

	c.JSON(http.StatusOK, NewSuccessMessageResponse("添加成功", nil))
}

// RemoveClientPermission 移除 Client 授权
func (a *AgentPermissionAPI) RemoveClientPermission(c *gin.Context) {
	agentID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 Agent ID"))
		return
	}

	permID, err := strconv.ParseInt(c.Param("pid"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的授权 ID"))
		return
	}

	result := db.DB.Where("id = ? AND agent_id = ?", permID, agentID).Delete(&model.AgentClientPermission{})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("删除失败"))
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, NewErrorResponse("授权不存在"))
		return
	}

	a.syncACL()

	logger.Infof("移除 Agent-Client 授权: agent_id=%d, perm_id=%d", agentID, permID)
	recordAuditLog(c, model.ActionRevokeDesktop, "agent", strconv.FormatUint(agentID, 10), "", map[string]interface{}{
		"perm_id": permID,
		"level":   "agent",
	})

	c.JSON(http.StatusOK, NewSuccessMessageResponse("移除成功", nil))
}

// ========== Agent-ClientGroup 授权 ==========

// AgentAuthorizedClientGroup 已授权 ClientGroup
type AgentAuthorizedClientGroup struct {
	ID          int64     `json:"id"`           // 授权记录 ID
	GroupID     int64     `json:"group_id"`     // 分组 ID
	Name        string    `json:"name"`         // 分组名称
	Alias       string    `json:"alias"`        // 分组别名
	MemberCount int64     `json:"member_count"` // 成员数量
	GrantedAt   time.Time `json:"granted_at"`   // 授权时间
}

// GetClientGroupPermissions 获取 Agent 的 ClientGroup 授权列表
func (a *AgentPermissionAPI) GetClientGroupPermissions(c *gin.Context) {
	agentID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 Agent ID"))
		return
	}

	var perms []model.AgentClientGroupPermission
	if err := db.DB.Preload("Group").Where("agent_id = ?", agentID).Find(&perms).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询失败"))
		return
	}

	result := make([]AgentAuthorizedClientGroup, 0, len(perms))
	for _, p := range perms {
		if p.Group != nil {
			var memberCount int64
			db.DB.Model(&model.ClientGroupMember{}).Where("group_id = ?", p.Group.ID).Count(&memberCount)

			result = append(result, AgentAuthorizedClientGroup{
				ID:          p.ID,
				GroupID:     p.Group.ID,
				Name:        p.Group.Name,
				Alias:       p.Group.Alias,
				MemberCount: memberCount,
				GrantedAt:   p.GrantedAt,
			})
		}
	}

	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

// AddClientGroupPermissionRequest 添加 ClientGroup 授权请求
type AddClientGroupPermissionRequest struct {
	GroupID int64 `json:"group_id" binding:"required"`
}

// AddClientGroupPermission 添加 ClientGroup 授权
func (a *AgentPermissionAPI) AddClientGroupPermission(c *gin.Context) {
	agentID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 Agent ID"))
		return
	}

	var req AddClientGroupPermissionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	// 验证 Agent 存在
	var agent model.Agent
	if err := db.DB.First(&agent, agentID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Agent 不存在"))
		return
	}

	// 验证分组存在
	var group model.ClientGroup
	if err := db.DB.First(&group, req.GroupID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("分组不存在"))
		return
	}

	// 检查是否已存在
	var existing model.AgentClientGroupPermission
	if err := db.DB.Where("agent_id = ? AND group_id = ?", agentID, req.GroupID).First(&existing).Error; err == nil {
		c.JSON(http.StatusConflict, NewErrorResponse("授权已存在"))
		return
	}

	perm := &model.AgentClientGroupPermission{
		AgentID:   agentID,
		GroupID:   req.GroupID,
		GrantedAt: time.Now(),
	}

	if err := db.DB.Create(perm).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("添加失败"))
		return
	}

	a.syncACL()

	logger.Infof("添加 Agent-ClientGroup 授权: agent_id=%d, group_id=%d", agentID, req.GroupID)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("添加成功", nil))
}

// RemoveClientGroupPermission 移除 ClientGroup 授权
func (a *AgentPermissionAPI) RemoveClientGroupPermission(c *gin.Context) {
	agentID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 Agent ID"))
		return
	}

	permID, err := strconv.ParseInt(c.Param("pid"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的授权 ID"))
		return
	}

	result := db.DB.Where("id = ? AND agent_id = ?", permID, agentID).Delete(&model.AgentClientGroupPermission{})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("删除失败"))
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, NewErrorResponse("授权不存在"))
		return
	}

	a.syncACL()

	logger.Infof("移除 Agent-ClientGroup 授权: agent_id=%d, perm_id=%d", agentID, permID)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("移除成功", nil))
}

// ========== Agent-Agent 授权 ==========

// AgentAuthorizedAgent 已授权 Agent
type AgentAuthorizedAgent struct {
	ID          int64     `json:"id"`           // 授权记录 ID
	SourceID    uint64    `json:"source_id"`    // 源 Agent ID
	SourceName  string    `json:"source_name"`  // 源 Agent 名称
	SourceAlias string    `json:"source_alias"` // 源 Agent 别名
	SourceIP    string    `json:"source_ip"`    // 源 Agent IP
	GrantedAt   time.Time `json:"granted_at"`   // 授权时间
}

// GetAgentPermissions 获取 Agent 的 Agent 授权列表
func (a *AgentPermissionAPI) GetAgentPermissions(c *gin.Context) {
	agentID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 Agent ID"))
		return
	}

	var perms []model.AgentAgentPermission
	if err := db.DB.Preload("SourceAgent").Where("target_agent_id = ?", agentID).Find(&perms).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询失败"))
		return
	}

	result := make([]AgentAuthorizedAgent, 0, len(perms))
	for _, p := range perms {
		if p.SourceAgent != nil {
			result = append(result, AgentAuthorizedAgent{
				ID:          p.ID,
				SourceID:    p.SourceAgent.ID,
				SourceName:  p.SourceAgent.Name,
				SourceAlias: p.SourceAgent.Alias,
				SourceIP:    p.SourceAgent.IP,
				GrantedAt:   p.GrantedAt,
			})
		}
	}

	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

// AddAgentPermissionRequest 添加 Agent 授权请求
type AddAgentPermissionRequest struct {
	SourceAgentID uint64 `json:"source_agent_id" binding:"required"`
}

// AddAgentPermission 添加 Agent 授权
func (a *AgentPermissionAPI) AddAgentPermission(c *gin.Context) {
	targetAgentID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 Agent ID"))
		return
	}

	var req AddAgentPermissionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	// 不能授权给自己
	if targetAgentID == req.SourceAgentID {
		c.JSON(http.StatusBadRequest, NewErrorResponse("不能授权给自己"))
		return
	}

	// 验证目标 Agent 存在
	var targetAgent model.Agent
	if err := db.DB.First(&targetAgent, targetAgentID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("目标 Agent 不存在"))
		return
	}

	// 验证源 Agent 存在
	var sourceAgent model.Agent
	if err := db.DB.First(&sourceAgent, req.SourceAgentID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("源 Agent 不存在"))
		return
	}

	// 检查是否已存在
	var existing model.AgentAgentPermission
	if err := db.DB.Where("target_agent_id = ? AND source_agent_id = ?", targetAgentID, req.SourceAgentID).First(&existing).Error; err == nil {
		c.JSON(http.StatusConflict, NewErrorResponse("授权已存在"))
		return
	}

	perm := &model.AgentAgentPermission{
		TargetAgentID: targetAgentID,
		SourceAgentID: req.SourceAgentID,
		GrantedAt:     time.Now(),
	}

	if err := db.DB.Create(perm).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("添加失败"))
		return
	}

	a.syncACL()

	logger.Infof("添加 Agent-Agent 授权: target_agent_id=%d, source_agent_id=%d", targetAgentID, req.SourceAgentID)
	recordAuditLog(c, model.ActionGrantAgent, "agent", strconv.FormatUint(targetAgentID, 10), targetAgent.Name, map[string]interface{}{
		"source_agent_id":   req.SourceAgentID,
		"source_agent_name": sourceAgent.Name,
		"level":             "agent",
	})

	c.JSON(http.StatusOK, NewSuccessMessageResponse("添加成功", nil))
}

// RemoveAgentPermission 移除 Agent 授权
func (a *AgentPermissionAPI) RemoveAgentPermission(c *gin.Context) {
	targetAgentID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 Agent ID"))
		return
	}

	permID, err := strconv.ParseInt(c.Param("pid"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的授权 ID"))
		return
	}

	result := db.DB.Where("id = ? AND target_agent_id = ?", permID, targetAgentID).Delete(&model.AgentAgentPermission{})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("删除失败"))
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, NewErrorResponse("授权不存在"))
		return
	}

	a.syncACL()

	logger.Infof("移除 Agent-Agent 授权: target_agent_id=%d, perm_id=%d", targetAgentID, permID)
	recordAuditLog(c, model.ActionRevokeAgent, "agent", strconv.FormatUint(targetAgentID, 10), "", map[string]interface{}{
		"perm_id": permID,
		"level":   "agent",
	})

	c.JSON(http.StatusOK, NewSuccessMessageResponse("移除成功", nil))
}

// ========== Agent-AgentGroup 授权 ==========

// AgentAuthorizedAgentGroup 已授权 AgentGroup
type AgentAuthorizedAgentGroup struct {
	ID          int64     `json:"id"`           // 授权记录 ID
	GroupID     int64     `json:"group_id"`     // 分组 ID
	Name        string    `json:"name"`         // 分组名称
	Alias       string    `json:"alias"`        // 分组别名
	MemberCount int64     `json:"member_count"` // 成员数量
	GrantedAt   time.Time `json:"granted_at"`   // 授权时间
}

// GetAgentGroupPermissions 获取 Agent 的 AgentGroup 授权列表
func (a *AgentPermissionAPI) GetAgentGroupPermissions(c *gin.Context) {
	agentID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 Agent ID"))
		return
	}

	var perms []model.AgentAgentGroupPermission
	if err := db.DB.Preload("Group").Where("target_agent_id = ?", agentID).Find(&perms).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询失败"))
		return
	}

	result := make([]AgentAuthorizedAgentGroup, 0, len(perms))
	for _, p := range perms {
		if p.Group != nil {
			var memberCount int64
			db.DB.Model(&model.AgentGroupMember{}).Where("group_id = ?", p.Group.ID).Count(&memberCount)

			result = append(result, AgentAuthorizedAgentGroup{
				ID:          p.ID,
				GroupID:     p.Group.ID,
				Name:        p.Group.Name,
				Alias:       p.Group.Alias,
				MemberCount: memberCount,
				GrantedAt:   p.GrantedAt,
			})
		}
	}

	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

// AddAgentGroupPermissionRequest 添加 AgentGroup 授权请求
type AddAgentGroupPermissionRequest struct {
	GroupID int64 `json:"group_id" binding:"required"`
}

// AddAgentGroupPermission 添加 AgentGroup 授权
func (a *AgentPermissionAPI) AddAgentGroupPermission(c *gin.Context) {
	targetAgentID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 Agent ID"))
		return
	}

	var req AddAgentGroupPermissionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	// 验证 Agent 存在
	var agent model.Agent
	if err := db.DB.First(&agent, targetAgentID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Agent 不存在"))
		return
	}

	// 验证分组存在
	var group model.AgentGroup
	if err := db.DB.First(&group, req.GroupID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("分组不存在"))
		return
	}

	// 检查是否已存在
	var existing model.AgentAgentGroupPermission
	if err := db.DB.Where("target_agent_id = ? AND group_id = ?", targetAgentID, req.GroupID).First(&existing).Error; err == nil {
		c.JSON(http.StatusConflict, NewErrorResponse("授权已存在"))
		return
	}

	perm := &model.AgentAgentGroupPermission{
		TargetAgentID: targetAgentID,
		GroupID:       req.GroupID,
		GrantedAt:     time.Now(),
	}

	if err := db.DB.Create(perm).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("添加失败"))
		return
	}

	a.syncACL()

	logger.Infof("添加 Agent-AgentGroup 授权: target_agent_id=%d, group_id=%d", targetAgentID, req.GroupID)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("添加成功", nil))
}

// RemoveAgentGroupPermission 移除 AgentGroup 授权
func (a *AgentPermissionAPI) RemoveAgentGroupPermission(c *gin.Context) {
	targetAgentID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 Agent ID"))
		return
	}

	permID, err := strconv.ParseInt(c.Param("pid"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的授权 ID"))
		return
	}

	result := db.DB.Where("id = ? AND target_agent_id = ?", permID, targetAgentID).Delete(&model.AgentAgentGroupPermission{})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("删除失败"))
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, NewErrorResponse("授权不存在"))
		return
	}

	a.syncACL()

	logger.Infof("移除 Agent-AgentGroup 授权: target_agent_id=%d, perm_id=%d", targetAgentID, permID)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("移除成功", nil))
}
