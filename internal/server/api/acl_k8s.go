package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

// ========== K8S API 授权 ==========

// K8SACLListItem K8S API 授权列表项
type K8SACLListItem struct {
	ID         uint64    `json:"id"`
	Name       string    `json:"name"`
	Alias      string    `json:"alias"`
	Role       string    `json:"role"`
	K8SEnabled bool      `json:"k8s_enabled"` // 是否启用 K8S API（从 node 表查询）
	UserCount  int64     `json:"user_count"`
	GroupCount int64     `json:"group_count"`
	CreatedAt  time.Time `json:"created_at"`
}

// ListK8SACL 获取 K8S API 授权列表（仅 Agent 角色）
func (a *ACLAPI) ListK8SACL(c *gin.Context) {
	ctx := c.Request.Context()
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	search := c.Query("search")

	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}

	query := db.DB.WithContext(ctx).Model(&model.User{}).Where("role = ?", model.UserRoleAgent)
	if search != "" {
		query = query.Where("name LIKE ? OR alias LIKE ?", "%"+search+"%", "%"+search+"%")
	}

	var total int64
	query.Count(&total)

	var users []model.User
	offset := (page - 1) * size
	if err := query.Order("created_at DESC").Offset(offset).Limit(size).Find(&users).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询失败"))
		return
	}

	// 查询每个用户的 K8S 启用状态（从 node 表）
	var k8sStatuses []struct {
		UserID     uint64 `gorm:"column:user_id"`
		K8SEnabled bool   `gorm:"column:k8s_enabled"`
	}
	db.DB.WithContext(ctx).Model(&model.Node{}).
		Select("user_id, COALESCE(k8s_enabled, 0) as k8s_enabled").
		Where("type = ? AND user_id IN (?)", model.NodeTypeAgent, getUserIDs(users)).
		Group("user_id").
		Find(&k8sStatuses)

	k8sEnabledMap := make(map[uint64]bool)
	for _, status := range k8sStatuses {
		k8sEnabledMap[status.UserID] = status.K8SEnabled
	}

	// 查询授权数量
	var userCounts []struct {
		TargetUserID uint64 `gorm:"column:target_user_id"`
		Count        int64  `gorm:"column:count"`
	}
	db.DB.WithContext(ctx).Model(&model.AclK8SUserPermission{}).
		Select("target_user_id, COUNT(*) as count").
		Group("target_user_id").Find(&userCounts)

	userCountMap := make(map[uint64]int64)
	for _, uc := range userCounts {
		userCountMap[uc.TargetUserID] = uc.Count
	}

	var groupCounts []struct {
		TargetUserID uint64 `gorm:"column:target_user_id"`
		Count        int64  `gorm:"column:count"`
	}
	db.DB.WithContext(ctx).Model(&model.AclK8SGroupPermission{}).
		Select("target_user_id, COUNT(*) as count").
		Group("target_user_id").Find(&groupCounts)

	groupCountMap := make(map[uint64]int64)
	for _, gc := range groupCounts {
		groupCountMap[gc.TargetUserID] = gc.Count
	}

	result := make([]K8SACLListItem, len(users))
	for i, user := range users {
		result[i] = K8SACLListItem{
			ID:         user.ID,
			Name:       user.Name,
			Alias:      user.Alias,
			Role:       string(user.Role),
			K8SEnabled: k8sEnabledMap[user.ID],
			UserCount:  userCountMap[user.ID],
			GroupCount: groupCountMap[user.ID],
			CreatedAt:  user.CreatedAt,
		}
	}

	c.JSON(http.StatusOK, NewPagedResponse(result, total, page, size))
}

// K8SACLPermissionItem K8S API 授权项
type K8SACLPermissionItem struct {
	ID         uint64    `json:"id"`
	Name       string    `json:"name"`
	Alias      string    `json:"alias"`
	K8SGroups  []string  `json:"k8s_groups"`
	Namespaces []string  `json:"namespaces"`
	Enabled    bool      `json:"enabled"`
	GrantedAt  time.Time `json:"granted_at"`
}

// K8SACLDetail K8S API 授权详情
type K8SACLDetail struct {
	ID         uint64                 `json:"id"`
	Name       string                 `json:"name"`
	Alias      string                 `json:"alias"`
	Role       string                 `json:"role"`
	K8SEnabled bool                   `json:"k8s_enabled"` // 是否启用 K8S API
	Users      []K8SACLPermissionItem `json:"users"`
	Groups     []K8SACLPermissionItem `json:"groups"`
}

// GetK8SACL 获取 K8S API 授权详情
func (a *ACLAPI) GetK8SACL(c *gin.Context) {
	ctx := c.Request.Context()
	targetUserID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	var targetUser model.User
	if err := db.DB.WithContext(ctx).First(&targetUser, targetUserID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("用户不存在"))
		return
	}

	// 查询该用户的 K8S 启用状态
	var node model.Node
	k8sEnabled := false
	if err := db.DB.WithContext(ctx).Where("user_id = ? AND type = ?", targetUserID, model.NodeTypeAgent).First(&node).Error; err == nil {
		if node.K8SEnabled != nil {
			k8sEnabled = *node.K8SEnabled
		}
	}

	// 查询已授权用户
	var userPerms []model.AclK8SUserPermission
	db.DB.WithContext(ctx).Preload("User").Where("target_user_id = ?", targetUserID).Find(&userPerms)

	users := make([]K8SACLPermissionItem, 0, len(userPerms))
	for _, p := range userPerms {
		if p.User != nil {
			users = append(users, K8SACLPermissionItem{
				ID:         p.User.ID,
				Name:       p.User.Name,
				Alias:      p.User.Alias,
				K8SGroups:  parseJSONStringArray(p.K8SGroups),
				Namespaces: parseJSONStringArray(p.Namespaces),
				Enabled:    p.Enabled,
				GrantedAt:  p.GrantedAt,
			})
		}
	}

	// 查询已授权分组
	var groupPerms []model.AclK8SGroupPermission
	db.DB.WithContext(ctx).Preload("Group").Where("target_user_id = ?", targetUserID).Find(&groupPerms)

	groups := make([]K8SACLPermissionItem, 0, len(groupPerms))
	for _, p := range groupPerms {
		if p.Group != nil {
			groups = append(groups, K8SACLPermissionItem{
				ID:         uint64(p.Group.ID),
				Name:       p.Group.Name,
				Alias:      p.Group.Alias,
				K8SGroups:  parseJSONStringArray(p.K8SGroups),
				Namespaces: parseJSONStringArray(p.Namespaces),
				Enabled:    p.Enabled,
				GrantedAt:  p.GrantedAt,
			})
		}
	}

	result := K8SACLDetail{
		ID:         targetUser.ID,
		Name:       targetUser.Name,
		Alias:      targetUser.Alias,
		Role:       string(targetUser.Role),
		K8SEnabled: k8sEnabled,
		Users:      users,
		Groups:     groups,
	}

	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

// AddK8SACLUsersRequest 添加 K8S 用户授权请求
type AddK8SACLUsersRequest struct {
	UserIDs    []uint64 `json:"user_ids" binding:"required,min=1"`
	K8SGroups  []string `json:"k8s_groups" binding:"required,min=1"` // K8S Impersonation 分组
	Namespaces []string `json:"namespaces"`                          // 允许的命名空间（空表示全部）
}

// AddK8SACLUsers 添加 K8S 用户授权
func (a *ACLAPI) AddK8SACLUsers(c *gin.Context) {
	ctx := c.Request.Context()
	targetUserID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	var req AddK8SACLUsersRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	var targetUser model.User
	if err := db.DB.WithContext(ctx).First(&targetUser, targetUserID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("用户不存在"))
		return
	}

	k8sGroupsJSON := formatJSONStringArray(req.K8SGroups)
	namespacesJSON := formatJSONStringArray(req.Namespaces)
	now := time.Now()

	for _, userID := range req.UserIDs {
		var existing model.AclK8SUserPermission
		if err := db.DB.WithContext(ctx).Where("target_user_id = ? AND user_id = ?", targetUserID, userID).First(&existing).Error; err == nil {
			// 更新现有授权
			existing.K8SGroups = k8sGroupsJSON
			existing.Namespaces = namespacesJSON
			existing.Enabled = true
			db.DB.WithContext(ctx).Save(&existing)
			continue
		}

		perm := &model.AclK8SUserPermission{
			TargetUserID: targetUserID,
			UserID:       userID,
			K8SGroups:    k8sGroupsJSON,
			Namespaces:   namespacesJSON,
			Enabled:      true,
			GrantedAt:    now,
		}
		db.DB.WithContext(ctx).Create(perm)
	}

	logger.Infof("添加 K8S 用户授权: target_user_id=%d, user_ids=%v", targetUserID, req.UserIDs)
	
	// 推送权限变更到 Desktop 客户端（K8S API 权限 → 全部数据）
	a.notifyDesktopDataChange(pb.DesktopDataType_DESKTOP_DATA_TYPE_ALL)
	
	c.JSON(http.StatusOK, NewSuccessMessageResponse("授权成功", nil))
}

// AddK8SACLGroupsRequest 添加 K8S 分组授权请求
type AddK8SACLGroupsRequest struct {
	GroupIDs   []int64  `json:"group_ids" binding:"required,min=1"`
	K8SGroups  []string `json:"k8s_groups" binding:"required,min=1"`
	Namespaces []string `json:"namespaces"`
}

// AddK8SACLGroups 添加 K8S 分组授权
func (a *ACLAPI) AddK8SACLGroups(c *gin.Context) {
	ctx := c.Request.Context()
	targetUserID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	var req AddK8SACLGroupsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	var targetUser model.User
	if err := db.DB.WithContext(ctx).First(&targetUser, targetUserID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("用户不存在"))
		return
	}

	k8sGroupsJSON := formatJSONStringArray(req.K8SGroups)
	namespacesJSON := formatJSONStringArray(req.Namespaces)
	now := time.Now()

	for _, groupID := range req.GroupIDs {
		var existing model.AclK8SGroupPermission
		if err := db.DB.WithContext(ctx).Where("target_user_id = ? AND group_id = ?", targetUserID, groupID).First(&existing).Error; err == nil {
			existing.K8SGroups = k8sGroupsJSON
			existing.Namespaces = namespacesJSON
			existing.Enabled = true
			db.DB.WithContext(ctx).Save(&existing)
			continue
		}

		perm := &model.AclK8SGroupPermission{
			TargetUserID: targetUserID,
			GroupID:      groupID,
			K8SGroups:    k8sGroupsJSON,
			Namespaces:   namespacesJSON,
			Enabled:      true,
			GrantedAt:    now,
		}
		db.DB.WithContext(ctx).Create(perm)
	}

	logger.Infof("添加 K8S 分组授权: target_user_id=%d, group_ids=%v", targetUserID, req.GroupIDs)
	c.JSON(http.StatusOK, NewSuccessMessageResponse("授权成功", nil))
}

// RemoveK8SACLUser 撤销 K8S 用户授权
func (a *ACLAPI) RemoveK8SACLUser(c *gin.Context) {
	ctx := c.Request.Context()
	targetUserID, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	userID, _ := strconv.ParseUint(c.Param("uid"), 10, 64)

	result := db.DB.WithContext(ctx).Where("target_user_id = ? AND user_id = ?", targetUserID, userID).Delete(&model.AclK8SUserPermission{})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("删除失败"))
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, NewErrorResponse("授权不存在"))
		return
	}

	logger.Infof("撤销 K8S 用户授权: target_user_id=%d, user_id=%d", targetUserID, userID)
	c.JSON(http.StatusOK, NewSuccessMessageResponse("撤销成功", nil))
}

// RemoveK8SACLGroup 撤销 K8S 分组授权
func (a *ACLAPI) RemoveK8SACLGroup(c *gin.Context) {
	ctx := c.Request.Context()
	targetUserID, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	groupID, _ := strconv.ParseInt(c.Param("gid"), 10, 64)

	result := db.DB.WithContext(ctx).Where("target_user_id = ? AND group_id = ?", targetUserID, groupID).Delete(&model.AclK8SGroupPermission{})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("删除失败"))
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, NewErrorResponse("授权不存在"))
		return
	}

	logger.Infof("撤销 K8S 分组授权: target_user_id=%d, group_id=%d", targetUserID, groupID)
	c.JSON(http.StatusOK, NewSuccessMessageResponse("撤销成功", nil))
}

// ========== JSON 数组工具函数 ==========

// getUserIDs 从用户列表提取 ID 列表
func getUserIDs(users []model.User) []uint64 {
	ids := make([]uint64, len(users))
	for i, u := range users {
		ids[i] = u.ID
	}
	return ids
}

// parseJSONStringArray 解析 JSON 字符串数组
func parseJSONStringArray(jsonStr string) []string {
	if jsonStr == "" || jsonStr == "[]" {
		return []string{}
	}
	var result []string
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return []string{}
	}
	return result
}

// formatJSONStringArray 格式化字符串数组为 JSON
func formatJSONStringArray(arr []string) string {
	if arr == nil {
		arr = []string{}
	}
	data, _ := json.Marshal(arr)
	return string(data)
}

// ========== Endpoint K8SAPI 授权（兼容路由） ==========
// P10/P11 重构后，Endpoint 级别权限已统一为 Agent 级别
// 以下方法保留前端兼容，内部通过 Endpoint ID 映射到 Agent ID 操作 Agent 级别权限表

// EndpointK8SAPIACLListItem Endpoint K8SAPI 授权列表项（兼容前端）
type EndpointK8SAPIACLListItem struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Alias      string    `json:"alias"`
	AgentID    uint64    `json:"agent_id"`
	AgentName  string    `json:"agent_name"`
	APIServer  string    `json:"api_server"`
	Status     string    `json:"status"`
	UserCount  int64     `json:"user_count"`
	GroupCount int64     `json:"group_count"`
	CreatedAt  time.Time `json:"created_at"`
}

// ListEndpointK8SAPIACL 获取 Endpoint K8SAPI 授权列表（兼容路由）
func (a *ACLAPI) ListEndpointK8SAPIACL(c *gin.Context) {
	ctx := c.Request.Context()
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	search := c.Query("search")

	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}

	query := db.DB.WithContext(ctx).Model(&model.Endpoint{}).Where("revoked = ? AND k8sapi_enabled = ?", false, true)
	if search != "" {
		query = query.Where("name LIKE ? OR alias LIKE ?", "%"+search+"%", "%"+search+"%")
	}

	var total int64
	query.Count(&total)

	var endpoints []model.Endpoint
	offset := (page - 1) * size
	if err := query.Preload("User").Order("created_at DESC").Offset(offset).Limit(size).Find(&endpoints).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询失败"))
		return
	}

	userCountMap := make(map[string]int64)
	groupCountMap := make(map[string]int64)

	if len(endpoints) > 0 {
		endpointToAgent := make(map[string]uint64)
		agentIDs := make([]uint64, 0, len(endpoints))
		for _, ep := range endpoints {
			endpointToAgent[ep.ID] = ep.UserID
			agentIDs = append(agentIDs, ep.UserID)
		}

		var userCounts []struct {
			TargetUserID uint64 `gorm:"column:target_user_id"`
			Count        int64  `gorm:"column:count"`
		}
		db.DB.WithContext(ctx).Model(&model.AclK8SUserPermission{}).
			Select("target_user_id, COUNT(*) as count").
			Where("target_user_id IN ?", agentIDs).
			Group("target_user_id").Find(&userCounts)

		agentUserCount := make(map[uint64]int64)
		for _, uc := range userCounts {
			agentUserCount[uc.TargetUserID] = uc.Count
		}
		for epID, agentID := range endpointToAgent {
			userCountMap[epID] = agentUserCount[agentID]
		}

		var groupCounts []struct {
			TargetUserID uint64 `gorm:"column:target_user_id"`
			Count        int64  `gorm:"column:count"`
		}
		db.DB.WithContext(ctx).Model(&model.AclK8SGroupPermission{}).
			Select("target_user_id, COUNT(*) as count").
			Where("target_user_id IN ?", agentIDs).
			Group("target_user_id").Find(&groupCounts)

		agentGroupCount := make(map[uint64]int64)
		for _, gc := range groupCounts {
			agentGroupCount[gc.TargetUserID] = gc.Count
		}
		for epID, agentID := range endpointToAgent {
			groupCountMap[epID] = agentGroupCount[agentID]
		}
	}

	result := make([]EndpointK8SAPIACLListItem, len(endpoints))
	for i, ep := range endpoints {
		agentName := ""
		if ep.User != nil {
			agentName = ep.User.Name
		}
		result[i] = EndpointK8SAPIACLListItem{
			ID:         ep.ID,
			Name:       ep.Name,
			Alias:      ep.Alias,
			AgentID:    ep.UserID,
			AgentName:  agentName,
			APIServer:  ep.K8SAPIApiServer,
			Status:     ep.Status,
			UserCount:  userCountMap[ep.ID],
			GroupCount: groupCountMap[ep.ID],
			CreatedAt:  ep.CreatedAt,
		}
	}

	c.JSON(http.StatusOK, NewPagedResponse(result, total, page, size))
}

// EndpointK8SAPIACLPermissionItem Endpoint K8SAPI 授权项（兼容前端）
type EndpointK8SAPIACLPermissionItem struct {
	ID         uint64    `json:"id"`
	Name       string    `json:"name"`
	Alias      string    `json:"alias"`
	K8SGroups  []string  `json:"k8s_groups"`
	Namespaces []string  `json:"namespaces"`
	Enabled    bool      `json:"enabled"`
	GrantedAt  time.Time `json:"granted_at"`
}

// EndpointK8SAPIACLDetail Endpoint K8SAPI 授权详情（兼容前端）
type EndpointK8SAPIACLDetail struct {
	ID        string                            `json:"id"`
	Name      string                            `json:"name"`
	Alias     string                            `json:"alias"`
	AgentID   uint64                            `json:"agent_id"`
	AgentName string                            `json:"agent_name"`
	APIServer string                            `json:"api_server"`
	Status    string                            `json:"status"`
	Users     []EndpointK8SAPIACLPermissionItem `json:"users"`
	Groups    []EndpointK8SAPIACLPermissionItem `json:"groups"`
}

// GetEndpointK8SAPIACL 获取 Endpoint K8SAPI 授权详情（兼容路由）
func (a *ACLAPI) GetEndpointK8SAPIACL(c *gin.Context) {
	ctx := c.Request.Context()
	endpointID := c.Param("id")

	var endpoint model.Endpoint
	if err := db.DB.WithContext(ctx).Preload("User").First(&endpoint, "id = ?", endpointID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Endpoint 不存在"))
		return
	}

	agentID := endpoint.UserID

	var userPerms []model.AclK8SUserPermission
	db.DB.WithContext(ctx).Preload("User").Where("target_user_id = ?", agentID).Find(&userPerms)

	users := make([]EndpointK8SAPIACLPermissionItem, 0, len(userPerms))
	for _, p := range userPerms {
		if p.User != nil {
			users = append(users, EndpointK8SAPIACLPermissionItem{
				ID:         p.User.ID,
				Name:       p.User.Name,
				Alias:      p.User.Alias,
				K8SGroups:  parseJSONStringArray(p.K8SGroups),
				Namespaces: parseJSONStringArray(p.Namespaces),
				Enabled:    p.Enabled,
				GrantedAt:  p.GrantedAt,
			})
		}
	}

	var groupPerms []model.AclK8SGroupPermission
	db.DB.WithContext(ctx).Preload("Group").Where("target_user_id = ?", agentID).Find(&groupPerms)

	groups := make([]EndpointK8SAPIACLPermissionItem, 0, len(groupPerms))
	for _, p := range groupPerms {
		if p.Group != nil {
			groups = append(groups, EndpointK8SAPIACLPermissionItem{
				ID:         uint64(p.Group.ID),
				Name:       p.Group.Name,
				Alias:      p.Group.Alias,
				K8SGroups:  parseJSONStringArray(p.K8SGroups),
				Namespaces: parseJSONStringArray(p.Namespaces),
				Enabled:    p.Enabled,
				GrantedAt:  p.GrantedAt,
			})
		}
	}

	agentName := ""
	if endpoint.User != nil {
		agentName = endpoint.User.Name
	}

	result := EndpointK8SAPIACLDetail{
		ID:        endpoint.ID,
		Name:      endpoint.Name,
		Alias:     endpoint.Alias,
		AgentID:   endpoint.UserID,
		AgentName: agentName,
		APIServer: endpoint.K8SAPIApiServer,
		Status:    endpoint.Status,
		Users:     users,
		Groups:    groups,
	}

	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

// AddEndpointK8SAPIACLUsersRequest 添加 Endpoint K8SAPI 用户授权请求（兼容前端）
type AddEndpointK8SAPIACLUsersRequest struct {
	UserIDs    []uint64 `json:"user_ids" binding:"required,min=1"`
	K8SGroups  []string `json:"k8s_groups"`
	Namespaces []string `json:"namespaces"`
}

// AddEndpointK8SAPIACLUsers 添加 Endpoint K8SAPI 用户授权（兼容路由）
func (a *ACLAPI) AddEndpointK8SAPIACLUsers(c *gin.Context) {
	ctx := c.Request.Context()
	endpointID := c.Param("id")

	var req AddEndpointK8SAPIACLUsersRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	var endpoint model.Endpoint
	if err := db.DB.WithContext(ctx).First(&endpoint, "id = ?", endpointID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Endpoint 不存在"))
		return
	}

	agentID := endpoint.UserID
	k8sGroupsJSON := formatJSONStringArray(req.K8SGroups)
	namespacesJSON := formatJSONStringArray(req.Namespaces)
	now := time.Now()

	for _, userID := range req.UserIDs {
		var existing model.AclK8SUserPermission
		if err := db.DB.WithContext(ctx).Where("target_user_id = ? AND user_id = ?", agentID, userID).First(&existing).Error; err == nil {
			existing.K8SGroups = k8sGroupsJSON
			existing.Namespaces = namespacesJSON
			existing.Enabled = true
			db.DB.WithContext(ctx).Save(&existing)
			continue
		}

		perm := &model.AclK8SUserPermission{
			TargetUserID: agentID,
			UserID:       userID,
			K8SGroups:    k8sGroupsJSON,
			Namespaces:   namespacesJSON,
			Enabled:      true,
			GrantedAt:    now,
		}
		db.DB.WithContext(ctx).Create(perm)
	}

	logger.Infof("添加 K8SAPI 用户授权 (Endpoint兼容路由): endpoint_id=%s, agent_id=%d, user_ids=%v", endpointID, agentID, req.UserIDs)
	c.JSON(http.StatusOK, NewSuccessMessageResponse("授权成功", nil))
}

// AddEndpointK8SAPIACLGroupsRequest 添加 Endpoint K8SAPI 分组授权请求（兼容前端）
type AddEndpointK8SAPIACLGroupsRequest struct {
	GroupIDs   []int64  `json:"group_ids" binding:"required,min=1"`
	K8SGroups  []string `json:"k8s_groups"`
	Namespaces []string `json:"namespaces"`
}

// AddEndpointK8SAPIACLGroups 添加 Endpoint K8SAPI 分组授权（兼容路由）
func (a *ACLAPI) AddEndpointK8SAPIACLGroups(c *gin.Context) {
	ctx := c.Request.Context()
	endpointID := c.Param("id")

	var req AddEndpointK8SAPIACLGroupsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	var endpoint model.Endpoint
	if err := db.DB.WithContext(ctx).First(&endpoint, "id = ?", endpointID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Endpoint 不存在"))
		return
	}

	agentID := endpoint.UserID
	k8sGroupsJSON := formatJSONStringArray(req.K8SGroups)
	namespacesJSON := formatJSONStringArray(req.Namespaces)
	now := time.Now()

	for _, groupID := range req.GroupIDs {
		var existing model.AclK8SGroupPermission
		if err := db.DB.WithContext(ctx).Where("target_user_id = ? AND group_id = ?", agentID, groupID).First(&existing).Error; err == nil {
			existing.K8SGroups = k8sGroupsJSON
			existing.Namespaces = namespacesJSON
			existing.Enabled = true
			db.DB.WithContext(ctx).Save(&existing)
			continue
		}

		perm := &model.AclK8SGroupPermission{
			TargetUserID: agentID,
			GroupID:      groupID,
			K8SGroups:    k8sGroupsJSON,
			Namespaces:   namespacesJSON,
			Enabled:      true,
			GrantedAt:    now,
		}
		db.DB.WithContext(ctx).Create(perm)
	}

	logger.Infof("添加 K8SAPI 分组授权 (Endpoint兼容路由): endpoint_id=%s, agent_id=%d, group_ids=%v", endpointID, agentID, req.GroupIDs)
	c.JSON(http.StatusOK, NewSuccessMessageResponse("授权成功", nil))
}

// RemoveEndpointK8SAPIACLUser 撤销 Endpoint K8SAPI 用户授权（兼容路由）
func (a *ACLAPI) RemoveEndpointK8SAPIACLUser(c *gin.Context) {
	ctx := c.Request.Context()
	endpointID := c.Param("id")
	userID, _ := strconv.ParseUint(c.Param("uid"), 10, 64)

	var endpoint model.Endpoint
	if err := db.DB.WithContext(ctx).First(&endpoint, "id = ?", endpointID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Endpoint 不存在"))
		return
	}

	agentID := endpoint.UserID
	result := db.DB.WithContext(ctx).Where("target_user_id = ? AND user_id = ?", agentID, userID).Delete(&model.AclK8SUserPermission{})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("删除失败"))
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, NewErrorResponse("授权不存在"))
		return
	}

	logger.Infof("撤销 K8SAPI 用户授权 (Endpoint兼容路由): endpoint_id=%s, agent_id=%d, user_id=%d", endpointID, agentID, userID)
	c.JSON(http.StatusOK, NewSuccessMessageResponse("撤销成功", nil))
}

// RemoveEndpointK8SAPIACLGroup 撤销 Endpoint K8SAPI 分组授权（兼容路由）
func (a *ACLAPI) RemoveEndpointK8SAPIACLGroup(c *gin.Context) {
	ctx := c.Request.Context()
	endpointID := c.Param("id")
	groupID, _ := strconv.ParseInt(c.Param("gid"), 10, 64)

	var endpoint model.Endpoint
	if err := db.DB.WithContext(ctx).First(&endpoint, "id = ?", endpointID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Endpoint 不存在"))
		return
	}

	agentID := endpoint.UserID
	result := db.DB.WithContext(ctx).Where("target_user_id = ? AND group_id = ?", agentID, groupID).Delete(&model.AclK8SGroupPermission{})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("删除失败"))
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, NewErrorResponse("授权不存在"))
		return
	}

	logger.Infof("撤销 K8SAPI 分组授权 (Endpoint兼容路由): endpoint_id=%s, agent_id=%d, group_id=%d", endpointID, agentID, groupID)
	c.JSON(http.StatusOK, NewSuccessMessageResponse("撤销成功", nil))
}
