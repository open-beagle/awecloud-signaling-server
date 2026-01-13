package api

import (
	"context"
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

// GroupAPI 分组管理 API
type GroupAPI struct {
	config   *config.ServerConfig
	hsClient *headscale.Client
	aclSync  *headscale.ACLSyncService
}

// NewGroupAPI 创建 GroupAPI
func NewGroupAPI(cfg *config.ServerConfig) *GroupAPI {
	api := &GroupAPI{config: cfg}

	if cfg.Tailscale.HeadscaleURL != "" && cfg.Tailscale.HeadscaleAPIKey != "" {
		client, err := headscale.NewClient(headscale.Config{
			URL:    cfg.Tailscale.HeadscaleURL,
			APIKey: cfg.Tailscale.HeadscaleAPIKey,
		})
		if err != nil {
			logger.Warnf("初始化 Headscale 客户端失败: %v", err)
		} else {
			api.hsClient = client
			api.aclSync = headscale.NewACLSyncService(client)
		}
	}

	return api
}

// ========== 用户分组 ==========

// ClientGroupListItem 用户分组列表项
type ClientGroupListItem struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Alias       string    `json:"alias"`
	MemberCount int64     `json:"member_count"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

// GetClientGroup 获取单个用户分组
func (a *GroupAPI) GetClientGroup(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	var group model.ClientGroup
	if err := db.DB.First(&group, id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("分组不存在"))
		return
	}

	// 查询成员数量
	var memberCount int64
	db.DB.Model(&model.ClientGroupMember{}).Where("group_id = ?", id).Count(&memberCount)

	result := ClientGroupListItem{
		ID:          group.ID,
		Name:        group.Name,
		Alias:       group.Alias,
		MemberCount: memberCount,
		Description: group.Description,
		CreatedAt:   group.CreatedAt,
	}

	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

// ListClientGroups 获取用户分组列表
func (a *GroupAPI) ListClientGroups(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))

	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}

	var total int64
	db.DB.Model(&model.ClientGroup{}).Count(&total)

	var groups []model.ClientGroup
	offset := (page - 1) * size
	if err := db.DB.Order("created_at DESC").Offset(offset).Limit(size).Find(&groups).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询失败"))
		return
	}

	// 查询每个分组的成员数量
	var memberCounts []struct {
		GroupID int64 `gorm:"column:group_id"`
		Count   int64 `gorm:"column:count"`
	}
	db.DB.Model(&model.ClientGroupMember{}).
		Select("group_id, COUNT(*) as count").
		Group("group_id").Find(&memberCounts)

	countMap := make(map[int64]int64)
	for _, mc := range memberCounts {
		countMap[mc.GroupID] = mc.Count
	}

	result := make([]ClientGroupListItem, len(groups))
	for i, g := range groups {
		result[i] = ClientGroupListItem{
			ID:          g.ID,
			Name:        g.Name,
			Alias:       g.Alias,
			MemberCount: countMap[g.ID],
			Description: g.Description,
			CreatedAt:   g.CreatedAt,
		}
	}

	c.JSON(http.StatusOK, NewPagedResponse(result, total, page, size))
}

// CreateClientGroupRequest 创建用户分组请求
type CreateClientGroupRequest struct {
	Name        string `json:"name" binding:"required"`
	Alias       string `json:"alias"`
	Description string `json:"description"`
}

// CreateClientGroup 创建用户分组
func (a *GroupAPI) CreateClientGroup(c *gin.Context) {
	var req CreateClientGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	// 检查名称是否已存在
	var existing model.ClientGroup
	if err := db.DB.Where("name = ?", req.Name).First(&existing).Error; err == nil {
		c.JSON(http.StatusConflict, NewErrorResponse("分组名称已存在"))
		return
	}

	group := &model.ClientGroup{
		Name:        req.Name,
		Alias:       req.Alias,
		Description: req.Description,
	}

	if err := db.DB.Create(group).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("创建失败"))
		return
	}

	logger.Infof("创建用户分组: id=%d, name=%s", group.ID, group.Name)
	recordAuditLog(c, model.ActionCreateClientGroup, "client_group", strconv.FormatInt(group.ID, 10), group.Name, nil)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("创建成功", group))
}

// UpdateClientGroupRequest 更新用户分组请求
type UpdateClientGroupRequest struct {
	Alias       string `json:"alias"`
	Description string `json:"description"`
}

// UpdateClientGroup 更新用户分组
func (a *GroupAPI) UpdateClientGroup(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	var req UpdateClientGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	var group model.ClientGroup
	if err := db.DB.First(&group, id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("分组不存在"))
		return
	}

	group.Alias = req.Alias
	group.Description = req.Description

	if err := db.DB.Save(&group).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("更新失败"))
		return
	}

	logger.Infof("更新用户分组: id=%d", id)
	recordAuditLog(c, model.ActionUpdateClientGroup, "client_group", strconv.FormatInt(id, 10), group.Name, nil)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("更新成功", nil))
}

// DeleteClientGroup 删除用户分组
func (a *GroupAPI) DeleteClientGroup(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	var group model.ClientGroup
	if err := db.DB.First(&group, id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("分组不存在"))
		return
	}

	// 获取所有成员，移除 Tag
	if a.hsClient != nil {
		var members []model.ClientGroupMember
		db.DB.Preload("Client").Where("group_id = ?", id).Find(&members)

		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()

		for _, m := range members {
			if m.Client != nil {
				// 查询该 Client 的所有 Desktop
				var desktops []model.Desktop
				db.DB.Where("client_id = ?", m.ClientID).Find(&desktops)
				for _, d := range desktops {
					if d.ID > 0 {
						// 移除分组 Tag（简化处理，实际需要获取现有 Tags 并移除）
						_ = a.hsClient.SetTags(ctx, d.ID, []string{})
					}
				}
			}
		}
	}

	// 删除分组权限
	db.DB.Where("group_id = ?", id).Delete(&model.ServiceClientGroupPermission{})

	// 删除分组成员
	db.DB.Where("group_id = ?", id).Delete(&model.ClientGroupMember{})

	// 删除分组
	if err := db.DB.Delete(&group).Error; err != nil {
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

	logger.Infof("删除用户分组: id=%d, name=%s", id, group.Name)
	recordAuditLog(c, model.ActionDeleteClientGroup, "client_group", strconv.FormatInt(id, 10), group.Name, nil)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("删除成功", nil))
}

// ClientGroupMemberItem 用户分组成员项
type ClientGroupMemberItem struct {
	ID       uint64    `json:"id"`
	Name     string    `json:"name"`
	Alias    string    `json:"alias"`
	JoinedAt time.Time `json:"joined_at"`
}

// ClientGroupMembersResponse 用户分组成员响应
type ClientGroupMembersResponse struct {
	Group   ClientGroupListItem     `json:"group"`
	Members []ClientGroupMemberItem `json:"members"`
}

// GetClientGroupMembers 获取用户分组成员
func (a *GroupAPI) GetClientGroupMembers(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	// 获取分组信息
	var group model.ClientGroup
	if err := db.DB.First(&group, id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("分组不存在"))
		return
	}

	var members []model.ClientGroupMember
	if err := db.DB.Preload("Client").Where("group_id = ?", id).Find(&members).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询失败"))
		return
	}

	result := make([]ClientGroupMemberItem, 0, len(members))
	for _, m := range members {
		if m.Client != nil {
			result = append(result, ClientGroupMemberItem{
				ID:       m.Client.ID,
				Name:     m.Client.Name,
				Alias:    m.Client.Alias,
				JoinedAt: m.CreatedAt,
			})
		}
	}

	response := ClientGroupMembersResponse{
		Group: ClientGroupListItem{
			ID:          group.ID,
			Name:        group.Name,
			Alias:       group.Alias,
			MemberCount: int64(len(result)),
			Description: group.Description,
			CreatedAt:   group.CreatedAt,
		},
		Members: result,
	}

	c.JSON(http.StatusOK, NewSuccessResponse(response))
}

// AddClientGroupMemberRequest 添加用户分组成员请求
type AddClientGroupMemberRequest struct {
	ClientID uint64 `json:"client_id" binding:"required"`
}

// AddClientGroupMember 添加用户分组成员
func (a *GroupAPI) AddClientGroupMember(c *gin.Context) {
	groupID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的分组 ID"))
		return
	}

	var req AddClientGroupMemberRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	// 验证分组存在
	var group model.ClientGroup
	if err := db.DB.First(&group, groupID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("分组不存在"))
		return
	}

	// 验证 Client 存在
	var client model.Client
	if err := db.DB.First(&client, req.ClientID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Client 不存在"))
		return
	}

	// 检查是否已是成员
	var existing model.ClientGroupMember
	if err := db.DB.Where("group_id = ? AND client_id = ?", groupID, req.ClientID).First(&existing).Error; err == nil {
		c.JSON(http.StatusConflict, NewErrorResponse("已是分组成员"))
		return
	}

	member := &model.ClientGroupMember{
		GroupID:  groupID,
		ClientID: req.ClientID,
	}

	if err := db.DB.Create(member).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("添加失败"))
		return
	}

	// 给该 Client 的所有 Desktop 添加分组 Tag
	if a.hsClient != nil {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()

		var desktops []model.Desktop
		db.DB.Where("client_id = ?", req.ClientID).Find(&desktops)
		// 使用分组名称生成 Tag，格式: tag:desktop-group-{group.name}
		newTag := "tag:desktop-group-" + group.Name
		// 身份 Tag，格式: tag:desktop-{client.name}
		identityTag := "tag:desktop-" + client.Name

		for _, d := range desktops {
			if d.ID > 0 {
				// 获取现有 Tag
				node, err := a.hsClient.GetNode(ctx, d.ID)
				if err != nil {
					logger.Warnf("获取节点 %d 失败: %v", d.ID, err)
					continue
				}

				tags := node.ForcedTags
				tagsChanged := false

				// 确保有身份 Tag
				hasIdentityTag := false
				for _, t := range tags {
					if t == identityTag {
						hasIdentityTag = true
						break
					}
				}
				if !hasIdentityTag {
					tags = append(tags, identityTag)
					tagsChanged = true
				}

				// 检查是否已有分组 Tag
				hasGroupTag := false
				for _, t := range tags {
					if t == newTag {
						hasGroupTag = true
						break
					}
				}
				if !hasGroupTag {
					tags = append(tags, newTag)
					tagsChanged = true
				}

				// 更新 Tag
				if tagsChanged {
					if err := a.hsClient.SetTags(ctx, d.ID, tags); err != nil {
						logger.Warnf("设置节点 %d Tag 失败: %v", d.ID, err)
					} else {
						logger.Infof("节点 %d 添加 Tag: %s", d.ID, newTag)
					}
				}
			}
		}
	}

	logger.Infof("添加用户分组成员: group_id=%d, client_id=%d", groupID, req.ClientID)
	recordAuditLog(c, model.ActionAddGroupMember, "client_group", strconv.FormatInt(groupID, 10), group.Name, map[string]interface{}{
		"client_id":   req.ClientID,
		"client_name": client.Name,
	})

	c.JSON(http.StatusOK, NewSuccessMessageResponse("添加成功", nil))
}

// RemoveClientGroupMember 移除用户分组成员
func (a *GroupAPI) RemoveClientGroupMember(c *gin.Context) {
	groupID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的分组 ID"))
		return
	}

	clientID, err := strconv.ParseUint(c.Param("cid"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 Client ID"))
		return
	}

	result := db.DB.Where("group_id = ? AND client_id = ?", groupID, clientID).Delete(&model.ClientGroupMember{})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("删除失败"))
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, NewErrorResponse("成员不存在"))
		return
	}

	// 移除该 Client 的所有 Desktop 的分组 Tag
	if a.hsClient != nil {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()

		// 获取分组信息
		var group model.ClientGroup
		if err := db.DB.First(&group, groupID).Error; err == nil {
			var desktops []model.Desktop
			db.DB.Where("client_id = ?", clientID).Find(&desktops)
			// 使用分组名称生成 Tag，格式: tag:desktop-group-{group.name}
			tagToRemove := "tag:desktop-group-" + group.Name

			for _, d := range desktops {
				if d.ID > 0 {
					// 获取现有 Tag
					node, err := a.hsClient.GetNode(ctx, d.ID)
					if err != nil {
						logger.Warnf("获取节点 %d 失败: %v", d.ID, err)
						continue
					}

					// 移除指定 Tag，保留其他 Tag（包括身份 Tag）
					newTags := []string{}
					for _, t := range node.ForcedTags {
						if t != tagToRemove {
							newTags = append(newTags, t)
						}
					}

					if err := a.hsClient.SetTags(ctx, d.ID, newTags); err != nil {
						logger.Warnf("设置节点 %d Tag 失败: %v", d.ID, err)
					} else {
						logger.Infof("节点 %d 移除 Tag: %s", d.ID, tagToRemove)
					}
				}
			}
		}
	}

	logger.Infof("移除用户分组成员: group_id=%d, client_id=%d", groupID, clientID)
	recordAuditLog(c, model.ActionRemoveGroupMember, "client_group", strconv.FormatInt(groupID, 10), "", map[string]interface{}{
		"client_id": clientID,
	})

	c.JSON(http.StatusOK, NewSuccessMessageResponse("移除成功", nil))
}

// ========== 代理分组 ==========

// AgentGroupListItem 代理分组列表项
type AgentGroupListItem struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Alias       string    `json:"alias"`
	MemberCount int64     `json:"member_count"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

// GetAgentGroup 获取单个代理分组
func (a *GroupAPI) GetAgentGroup(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	var group model.AgentGroup
	if err := db.DB.First(&group, id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("分组不存在"))
		return
	}

	// 查询成员数量
	var memberCount int64
	db.DB.Model(&model.AgentGroupMember{}).Where("group_id = ?", id).Count(&memberCount)

	result := AgentGroupListItem{
		ID:          group.ID,
		Name:        group.Name,
		Alias:       group.Alias,
		MemberCount: memberCount,
		Description: group.Description,
		CreatedAt:   group.CreatedAt,
	}

	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

// ListAgentGroups 获取代理分组列表
func (a *GroupAPI) ListAgentGroups(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))

	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}

	var total int64
	db.DB.Model(&model.AgentGroup{}).Count(&total)

	var groups []model.AgentGroup
	offset := (page - 1) * size
	if err := db.DB.Order("created_at DESC").Offset(offset).Limit(size).Find(&groups).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询失败"))
		return
	}

	var memberCounts []struct {
		GroupID int64 `gorm:"column:group_id"`
		Count   int64 `gorm:"column:count"`
	}
	db.DB.Model(&model.AgentGroupMember{}).
		Select("group_id, COUNT(*) as count").
		Group("group_id").Find(&memberCounts)

	countMap := make(map[int64]int64)
	for _, mc := range memberCounts {
		countMap[mc.GroupID] = mc.Count
	}

	result := make([]AgentGroupListItem, len(groups))
	for i, g := range groups {
		result[i] = AgentGroupListItem{
			ID:          g.ID,
			Name:        g.Name,
			Alias:       g.Alias,
			MemberCount: countMap[g.ID],
			Description: g.Description,
			CreatedAt:   g.CreatedAt,
		}
	}

	c.JSON(http.StatusOK, NewPagedResponse(result, total, page, size))
}

// CreateAgentGroupRequest 创建代理分组请求
type CreateAgentGroupRequest struct {
	Name        string `json:"name" binding:"required"`
	Alias       string `json:"alias"`
	Description string `json:"description"`
}

// CreateAgentGroup 创建代理分组
func (a *GroupAPI) CreateAgentGroup(c *gin.Context) {
	var req CreateAgentGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	var existing model.AgentGroup
	if err := db.DB.Where("name = ?", req.Name).First(&existing).Error; err == nil {
		c.JSON(http.StatusConflict, NewErrorResponse("分组名称已存在"))
		return
	}

	group := &model.AgentGroup{
		Name:        req.Name,
		Alias:       req.Alias,
		Description: req.Description,
	}

	if err := db.DB.Create(group).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("创建失败"))
		return
	}

	logger.Infof("创建代理分组: id=%d, name=%s", group.ID, group.Name)
	recordAuditLog(c, model.ActionCreateAgentGroup, "agent_group", strconv.FormatInt(group.ID, 10), group.Name, nil)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("创建成功", group))
}

// UpdateAgentGroupRequest 更新代理分组请求
type UpdateAgentGroupRequest struct {
	Alias       string `json:"alias"`
	Description string `json:"description"`
}

// UpdateAgentGroup 更新代理分组
func (a *GroupAPI) UpdateAgentGroup(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	var req UpdateAgentGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	var group model.AgentGroup
	if err := db.DB.First(&group, id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("分组不存在"))
		return
	}

	group.Alias = req.Alias
	group.Description = req.Description

	if err := db.DB.Save(&group).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("更新失败"))
		return
	}

	logger.Infof("更新代理分组: id=%d", id)
	recordAuditLog(c, model.ActionUpdateAgentGroup, "agent_group", strconv.FormatInt(id, 10), group.Name, nil)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("更新成功", nil))
}

// DeleteAgentGroup 删除代理分组
func (a *GroupAPI) DeleteAgentGroup(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	var group model.AgentGroup
	if err := db.DB.First(&group, id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("分组不存在"))
		return
	}

	// 移除所有成员的 Tag
	if a.hsClient != nil {
		var members []model.AgentGroupMember
		db.DB.Preload("Agent").Where("group_id = ?", id).Find(&members)

		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()

		for _, m := range members {
			if m.Agent != nil && m.Agent.NodeID > 0 {
				_ = a.hsClient.SetTags(ctx, m.Agent.NodeID, []string{})
			}
		}
	}

	// 删除分组权限
	db.DB.Where("group_id = ?", id).Delete(&model.ServiceAgentGroupPermission{})

	// 删除分组成员
	db.DB.Where("group_id = ?", id).Delete(&model.AgentGroupMember{})

	// 删除分组
	if err := db.DB.Delete(&group).Error; err != nil {
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

	logger.Infof("删除代理分组: id=%d, name=%s", id, group.Name)
	recordAuditLog(c, model.ActionDeleteAgentGroup, "agent_group", strconv.FormatInt(id, 10), group.Name, nil)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("删除成功", nil))
}

// AgentGroupMemberItem 代理分组成员项
type AgentGroupMemberItem struct {
	ID       uint64    `json:"id"`
	Name     string    `json:"name"`
	Alias    string    `json:"alias"`
	JoinedAt time.Time `json:"joined_at"`
}

// AgentGroupMembersResponse 代理分组成员响应
type AgentGroupMembersResponse struct {
	Group   AgentGroupListItem     `json:"group"`
	Members []AgentGroupMemberItem `json:"members"`
}

// GetAgentGroupMembers 获取代理分组成员
func (a *GroupAPI) GetAgentGroupMembers(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	// 获取分组信息
	var group model.AgentGroup
	if err := db.DB.First(&group, id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("分组不存在"))
		return
	}

	var members []model.AgentGroupMember
	if err := db.DB.Preload("Agent").Where("group_id = ?", id).Find(&members).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询失败"))
		return
	}

	result := make([]AgentGroupMemberItem, 0, len(members))
	for _, m := range members {
		if m.Agent != nil {
			result = append(result, AgentGroupMemberItem{
				ID:       m.Agent.ID,
				Name:     m.Agent.Name,
				Alias:    m.Agent.Alias,
				JoinedAt: m.CreatedAt,
			})
		}
	}

	response := AgentGroupMembersResponse{
		Group: AgentGroupListItem{
			ID:          group.ID,
			Name:        group.Name,
			Alias:       group.Alias,
			MemberCount: int64(len(result)),
			Description: group.Description,
			CreatedAt:   group.CreatedAt,
		},
		Members: result,
	}

	c.JSON(http.StatusOK, NewSuccessResponse(response))
}

// AddAgentGroupMemberRequest 添加代理分组成员请求
type AddAgentGroupMemberRequest struct {
	AgentID uint64 `json:"agent_id" binding:"required"`
}

// AddAgentGroupMember 添加代理分组成员
func (a *GroupAPI) AddAgentGroupMember(c *gin.Context) {
	groupID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的分组 ID"))
		return
	}

	var req AddAgentGroupMemberRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	var group model.AgentGroup
	if err := db.DB.First(&group, groupID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("分组不存在"))
		return
	}

	var agent model.Agent
	if err := db.DB.First(&agent, req.AgentID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Agent 不存在"))
		return
	}

	var existing model.AgentGroupMember
	if err := db.DB.Where("group_id = ? AND agent_id = ?", groupID, req.AgentID).First(&existing).Error; err == nil {
		c.JSON(http.StatusConflict, NewErrorResponse("已是分组成员"))
		return
	}

	member := &model.AgentGroupMember{
		GroupID: groupID,
		AgentID: req.AgentID,
	}

	if err := db.DB.Create(member).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("添加失败"))
		return
	}

	// 给 Agent Node 添加分组 Tag
	if a.hsClient != nil && agent.NodeID > 0 {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()
		// 使用分组名称生成 Tag，格式: tag:agent-group-{group.name}
		newTag := "tag:agent-group-" + group.Name
		// 身份 Tag，格式: tag:agent-{agent.name}
		identityTag := "tag:agent-" + agent.Name

		// 获取现有 Tag
		node, err := a.hsClient.GetNode(ctx, agent.NodeID)
		if err != nil {
			logger.Warnf("获取节点 %d 失败: %v", agent.NodeID, err)
		} else {
			tags := node.ForcedTags
			tagsChanged := false

			// 确保有身份 Tag
			hasIdentityTag := false
			for _, t := range tags {
				if t == identityTag {
					hasIdentityTag = true
					break
				}
			}
			if !hasIdentityTag {
				tags = append(tags, identityTag)
				tagsChanged = true
			}

			// 检查是否已有分组 Tag
			hasGroupTag := false
			for _, t := range tags {
				if t == newTag {
					hasGroupTag = true
					break
				}
			}
			if !hasGroupTag {
				tags = append(tags, newTag)
				tagsChanged = true
			}

			// 更新 Tag
			if tagsChanged {
				if err := a.hsClient.SetTags(ctx, agent.NodeID, tags); err != nil {
					logger.Warnf("设置节点 %d Tag 失败: %v", agent.NodeID, err)
				} else {
					logger.Infof("节点 %d 添加 Tag: %v", agent.NodeID, tags)
				}
			}
		}
	}

	logger.Infof("添加代理分组成员: group_id=%d, agent_id=%d", groupID, req.AgentID)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("添加成功", nil))
}

// RemoveAgentGroupMember 移除代理分组成员
func (a *GroupAPI) RemoveAgentGroupMember(c *gin.Context) {
	groupID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的分组 ID"))
		return
	}

	agentID, err := strconv.ParseUint(c.Param("aid"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 Agent ID"))
		return
	}

	// 获取 Agent 信息用于移除 Tag
	var agent model.Agent
	db.DB.First(&agent, agentID)

	result := db.DB.Where("group_id = ? AND agent_id = ?", groupID, agentID).Delete(&model.AgentGroupMember{})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("删除失败"))
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, NewErrorResponse("成员不存在"))
		return
	}

	// 移除 Agent Node 的分组 Tag（只移除指定 Tag，保留其他 Tag）
	if a.hsClient != nil && agent.NodeID > 0 {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()

		// 获取分组信息
		var group model.AgentGroup
		if err := db.DB.First(&group, groupID).Error; err == nil {
			// 使用分组名称生成 Tag
			tagToRemove := "tag:agent-group-" + group.Name

			// 获取现有 Tag
			node, err := a.hsClient.GetNode(ctx, agent.NodeID)
			if err != nil {
				logger.Warnf("获取节点 %d 失败: %v", agent.NodeID, err)
			} else {
				// 过滤掉要移除的 Tag，保留其他 Tag（包括身份 Tag）
				var newTags []string
				for _, t := range node.ForcedTags {
					if t != tagToRemove {
						newTags = append(newTags, t)
					}
				}

				// 设置新的 Tag 列表
				if err := a.hsClient.SetTags(ctx, agent.NodeID, newTags); err != nil {
					logger.Warnf("设置节点 %d Tag 失败: %v", agent.NodeID, err)
				} else {
					logger.Infof("节点 %d 移除 Tag: %s, 剩余 Tag: %v", agent.NodeID, tagToRemove, newTags)
				}
			}
		}
	}

	logger.Infof("移除代理分组成员: group_id=%d, agent_id=%d", groupID, agentID)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("移除成功", nil))
}
