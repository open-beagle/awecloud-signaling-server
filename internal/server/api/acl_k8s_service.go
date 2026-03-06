package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

// ========== K8S Service 授权 ==========

// K8SServiceACLListItem K8S Service 授权列表项
type K8SServiceACLListItem struct {
	ID         uint64    `json:"id"`
	Name       string    `json:"name"`
	Alias      string    `json:"alias"`
	Role       string    `json:"role"`
	UserCount  int64     `json:"user_count"`
	GroupCount int64     `json:"group_count"`
	CreatedAt  time.Time `json:"created_at"`
}

// ListK8SServiceACL 获取 K8S Service 授权列表（仅 Agent 角色）
func (a *ACLAPI) ListK8SServiceACL(c *gin.Context) {
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

	// 查询授权数量
	var userCounts []struct {
		TargetUserID uint64 `gorm:"column:target_user_id"`
		Count        int64  `gorm:"column:count"`
	}
	db.DB.WithContext(ctx).Model(&model.AclK8SServiceUserPermission{}).
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
	db.DB.WithContext(ctx).Model(&model.AclK8SServiceGroupPermission{}).
		Select("target_user_id, COUNT(*) as count").
		Group("target_user_id").Find(&groupCounts)

	groupCountMap := make(map[uint64]int64)
	for _, gc := range groupCounts {
		groupCountMap[gc.TargetUserID] = gc.Count
	}

	result := make([]K8SServiceACLListItem, len(users))
	for i, user := range users {
		result[i] = K8SServiceACLListItem{
			ID:         user.ID,
			Name:       user.Name,
			Alias:      user.Alias,
			Role:       string(user.Role),
			UserCount:  userCountMap[user.ID],
			GroupCount: groupCountMap[user.ID],
			CreatedAt:  user.CreatedAt,
		}
	}

	c.JSON(http.StatusOK, NewPagedResponse(result, total, page, size))
}

// K8SServiceACLPermissionItem K8S Service 授权项
type K8SServiceACLPermissionItem struct {
	ID           uint64    `json:"id"`
	Name         string    `json:"name"`
	Alias        string    `json:"alias"`
	Namespaces   []string  `json:"namespaces"`
	ServiceNames []string  `json:"service_names"`
	Enabled      bool      `json:"enabled"`
	GrantedAt    time.Time `json:"granted_at"`
}

// K8SServiceACLDetail K8S Service 授权详情
type K8SServiceACLDetail struct {
	ID     uint64                        `json:"id"`
	Name   string                        `json:"name"`
	Alias  string                        `json:"alias"`
	Role   string                        `json:"role"`
	Users  []K8SServiceACLPermissionItem `json:"users"`
	Groups []K8SServiceACLPermissionItem `json:"groups"`
}

// GetK8SServiceACL 获取 K8S Service 授权详情
func (a *ACLAPI) GetK8SServiceACL(c *gin.Context) {
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

	// 查询已授权用户
	var userPerms []model.AclK8SServiceUserPermission
	db.DB.WithContext(ctx).Preload("User").Where("target_user_id = ?", targetUserID).Find(&userPerms)

	users := make([]K8SServiceACLPermissionItem, 0, len(userPerms))
	for _, p := range userPerms {
		if p.User != nil {
			users = append(users, K8SServiceACLPermissionItem{
				ID:           p.User.ID,
				Name:         p.User.Name,
				Alias:        p.User.Alias,
				Namespaces:   parseJSONStringArray(p.Namespaces),
				ServiceNames: parseJSONStringArray(p.ServiceNames),
				Enabled:      p.Enabled,
				GrantedAt:    p.GrantedAt,
			})
		}
	}

	// 查询已授权分组
	var groupPerms []model.AclK8SServiceGroupPermission
	db.DB.WithContext(ctx).Preload("Group").Where("target_user_id = ?", targetUserID).Find(&groupPerms)

	groups := make([]K8SServiceACLPermissionItem, 0, len(groupPerms))
	for _, p := range groupPerms {
		if p.Group != nil {
			groups = append(groups, K8SServiceACLPermissionItem{
				ID:           uint64(p.Group.ID),
				Name:         p.Group.Name,
				Alias:        p.Group.Alias,
				Namespaces:   parseJSONStringArray(p.Namespaces),
				ServiceNames: parseJSONStringArray(p.ServiceNames),
				Enabled:      p.Enabled,
				GrantedAt:    p.GrantedAt,
			})
		}
	}

	result := K8SServiceACLDetail{
		ID:     targetUser.ID,
		Name:   targetUser.Name,
		Alias:  targetUser.Alias,
		Role:   string(targetUser.Role),
		Users:  users,
		Groups: groups,
	}

	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

// AddK8SServiceACLUsersRequest 添加 K8SService 用户授权请求
type AddK8SServiceACLUsersRequest struct {
	UserIDs      []uint64 `json:"user_ids" binding:"required,min=1"`
	Namespaces   []string `json:"namespaces"`    // 允许的命名空间（空表示全部）
	ServiceNames []string `json:"service_names"` // 允许的 Service 名称（空表示全部）
}

// AddK8SServiceACLUsers 添加 K8SService 用户授权
func (a *ACLAPI) AddK8SServiceACLUsers(c *gin.Context) {
	ctx := c.Request.Context()
	targetUserID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	var req AddK8SServiceACLUsersRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	var targetUser model.User
	if err := db.DB.WithContext(ctx).First(&targetUser, targetUserID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("用户不存在"))
		return
	}

	namespacesJSON := formatJSONStringArray(req.Namespaces)
	serviceNamesJSON := formatJSONStringArray(req.ServiceNames)
	now := time.Now()

	for _, userID := range req.UserIDs {
		var existing model.AclK8SServiceUserPermission
		if err := db.DB.WithContext(ctx).Where("target_user_id = ? AND user_id = ?", targetUserID, userID).First(&existing).Error; err == nil {
			existing.Namespaces = namespacesJSON
			existing.ServiceNames = serviceNamesJSON
			existing.Enabled = true
			db.DB.WithContext(ctx).Save(&existing)
			continue
		}

		perm := &model.AclK8SServiceUserPermission{
			TargetUserID: targetUserID,
			UserID:       userID,
			Namespaces:   namespacesJSON,
			ServiceNames: serviceNamesJSON,
			Enabled:      true,
			GrantedAt:    now,
		}
		db.DB.WithContext(ctx).Create(perm)
	}

	logger.Infof("添加 K8SService 用户授权: target_user_id=%d, user_ids=%v", targetUserID, req.UserIDs)
	c.JSON(http.StatusOK, NewSuccessMessageResponse("授权成功", nil))
}

// AddK8SServiceACLGroupsRequest 添加 K8SService 分组授权请求
type AddK8SServiceACLGroupsRequest struct {
	GroupIDs     []int64  `json:"group_ids" binding:"required,min=1"`
	Namespaces   []string `json:"namespaces"`
	ServiceNames []string `json:"service_names"`
}

// AddK8SServiceACLGroups 添加 K8SService 分组授权
func (a *ACLAPI) AddK8SServiceACLGroups(c *gin.Context) {
	ctx := c.Request.Context()
	targetUserID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	var req AddK8SServiceACLGroupsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	var targetUser model.User
	if err := db.DB.WithContext(ctx).First(&targetUser, targetUserID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("用户不存在"))
		return
	}

	namespacesJSON := formatJSONStringArray(req.Namespaces)
	serviceNamesJSON := formatJSONStringArray(req.ServiceNames)
	now := time.Now()

	for _, groupID := range req.GroupIDs {
		var existing model.AclK8SServiceGroupPermission
		if err := db.DB.WithContext(ctx).Where("target_user_id = ? AND group_id = ?", targetUserID, groupID).First(&existing).Error; err == nil {
			existing.Namespaces = namespacesJSON
			existing.ServiceNames = serviceNamesJSON
			existing.Enabled = true
			db.DB.WithContext(ctx).Save(&existing)
			continue
		}

		perm := &model.AclK8SServiceGroupPermission{
			TargetUserID: targetUserID,
			GroupID:      groupID,
			Namespaces:   namespacesJSON,
			ServiceNames: serviceNamesJSON,
			Enabled:      true,
			GrantedAt:    now,
		}
		db.DB.WithContext(ctx).Create(perm)
	}

	logger.Infof("添加 K8SService 分组授权: target_user_id=%d, group_ids=%v", targetUserID, req.GroupIDs)
	c.JSON(http.StatusOK, NewSuccessMessageResponse("授权成功", nil))
}

// RemoveK8SServiceACLUser 撤销 K8SService 用户授权
func (a *ACLAPI) RemoveK8SServiceACLUser(c *gin.Context) {
	ctx := c.Request.Context()
	targetUserID, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	userID, _ := strconv.ParseUint(c.Param("uid"), 10, 64)

	result := db.DB.WithContext(ctx).Where("target_user_id = ? AND user_id = ?", targetUserID, userID).Delete(&model.AclK8SServiceUserPermission{})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("删除失败"))
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, NewErrorResponse("授权不存在"))
		return
	}

	logger.Infof("撤销 K8SService 用户授权: target_user_id=%d, user_id=%d", targetUserID, userID)
	c.JSON(http.StatusOK, NewSuccessMessageResponse("撤销成功", nil))
}

// RemoveK8SServiceACLGroup 撤销 K8SService 分组授权
func (a *ACLAPI) RemoveK8SServiceACLGroup(c *gin.Context) {
	ctx := c.Request.Context()
	targetUserID, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	groupID, _ := strconv.ParseInt(c.Param("gid"), 10, 64)

	result := db.DB.WithContext(ctx).Where("target_user_id = ? AND group_id = ?", targetUserID, groupID).Delete(&model.AclK8SServiceGroupPermission{})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("删除失败"))
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, NewErrorResponse("授权不存在"))
		return
	}

	logger.Infof("撤销 K8SService 分组授权: target_user_id=%d, group_id=%d", targetUserID, groupID)
	c.JSON(http.StatusOK, NewSuccessMessageResponse("撤销成功", nil))
}

// ========== Endpoint K8SService 授权（兼容路由） ==========
// P10 重构后，Endpoint 级别权限已统一为 Agent 级别
// 以下方法保留前端兼容，内部通过 Endpoint ID 映射到 Agent ID 操作 Agent 级别权限表

// EndpointK8SServiceACLListItem Endpoint K8SService 授权列表项（兼容前端）
type EndpointK8SServiceACLListItem struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Alias      string    `json:"alias"`
	AgentID    uint64    `json:"agent_id"`
	AgentName  string    `json:"agent_name"`
	Status     string    `json:"status"`
	UserCount  int64     `json:"user_count"`
	GroupCount int64     `json:"group_count"`
	CreatedAt  time.Time `json:"created_at"`
}

// ListEndpointK8SServiceACL 获取 Endpoint K8SService 授权列表（兼容路由）
func (a *ACLAPI) ListEndpointK8SServiceACL(c *gin.Context) {
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

	query := db.DB.WithContext(ctx).Model(&model.Endpoint{}).Where("revoked = ? AND k8sservice_enabled = ?", false, true)
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
		db.DB.WithContext(ctx).Model(&model.AclK8SServiceUserPermission{}).
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
		db.DB.WithContext(ctx).Model(&model.AclK8SServiceGroupPermission{}).
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

	result := make([]EndpointK8SServiceACLListItem, len(endpoints))
	for i, ep := range endpoints {
		agentName := ""
		if ep.User != nil {
			agentName = ep.User.Name
		}
		result[i] = EndpointK8SServiceACLListItem{
			ID:         ep.ID,
			Name:       ep.Name,
			Alias:      ep.Alias,
			AgentID:    ep.UserID,
			AgentName:  agentName,
			Status:     ep.Status,
			UserCount:  userCountMap[ep.ID],
			GroupCount: groupCountMap[ep.ID],
			CreatedAt:  ep.CreatedAt,
		}
	}

	c.JSON(http.StatusOK, NewPagedResponse(result, total, page, size))
}

// EndpointK8SServiceACLPermissionItem Endpoint K8SService 授权项（兼容前端）
type EndpointK8SServiceACLPermissionItem struct {
	ID           uint64    `json:"id"`
	Name         string    `json:"name"`
	Alias        string    `json:"alias"`
	Namespaces   []string  `json:"namespaces"`
	ServiceNames []string  `json:"service_names"`
	Enabled      bool      `json:"enabled"`
	GrantedAt    time.Time `json:"granted_at"`
}

// EndpointK8SServiceACLDetail Endpoint K8SService 授权详情（兼容前端）
type EndpointK8SServiceACLDetail struct {
	ID        string                                `json:"id"`
	Name      string                                `json:"name"`
	Alias     string                                `json:"alias"`
	AgentID   uint64                                `json:"agent_id"`
	AgentName string                                `json:"agent_name"`
	Status    string                                `json:"status"`
	Users     []EndpointK8SServiceACLPermissionItem `json:"users"`
	Groups    []EndpointK8SServiceACLPermissionItem `json:"groups"`
}

// GetEndpointK8SServiceACL 获取 Endpoint K8SService 授权详情（兼容路由）
func (a *ACLAPI) GetEndpointK8SServiceACL(c *gin.Context) {
	ctx := c.Request.Context()
	endpointID := c.Param("id")

	var endpoint model.Endpoint
	if err := db.DB.WithContext(ctx).Preload("User").First(&endpoint, "id = ?", endpointID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Endpoint 不存在"))
		return
	}

	agentID := endpoint.UserID

	var userPerms []model.AclK8SServiceUserPermission
	db.DB.WithContext(ctx).Preload("User").Where("target_user_id = ?", agentID).Find(&userPerms)

	users := make([]EndpointK8SServiceACLPermissionItem, 0, len(userPerms))
	for _, p := range userPerms {
		if p.User != nil {
			users = append(users, EndpointK8SServiceACLPermissionItem{
				ID:           p.User.ID,
				Name:         p.User.Name,
				Alias:        p.User.Alias,
				Namespaces:   parseJSONStringArray(p.Namespaces),
				ServiceNames: parseJSONStringArray(p.ServiceNames),
				Enabled:      p.Enabled,
				GrantedAt:    p.GrantedAt,
			})
		}
	}

	var groupPerms []model.AclK8SServiceGroupPermission
	db.DB.WithContext(ctx).Preload("Group").Where("target_user_id = ?", agentID).Find(&groupPerms)

	groups := make([]EndpointK8SServiceACLPermissionItem, 0, len(groupPerms))
	for _, p := range groupPerms {
		if p.Group != nil {
			groups = append(groups, EndpointK8SServiceACLPermissionItem{
				ID:           uint64(p.Group.ID),
				Name:         p.Group.Name,
				Alias:        p.Group.Alias,
				Namespaces:   parseJSONStringArray(p.Namespaces),
				ServiceNames: parseJSONStringArray(p.ServiceNames),
				Enabled:      p.Enabled,
				GrantedAt:    p.GrantedAt,
			})
		}
	}

	agentName := ""
	if endpoint.User != nil {
		agentName = endpoint.User.Name
	}

	result := EndpointK8SServiceACLDetail{
		ID:        endpoint.ID,
		Name:      endpoint.Name,
		Alias:     endpoint.Alias,
		AgentID:   endpoint.UserID,
		AgentName: agentName,
		Status:    endpoint.Status,
		Users:     users,
		Groups:    groups,
	}

	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

// AddEndpointK8SServiceACLUsersRequest 添加 Endpoint K8SService 用户授权请求（兼容前端）
type AddEndpointK8SServiceACLUsersRequest struct {
	UserIDs      []uint64 `json:"user_ids" binding:"required,min=1"`
	Namespaces   []string `json:"namespaces"`
	ServiceNames []string `json:"service_names"`
}

// AddEndpointK8SServiceACLUsers 添加 Endpoint K8SService 用户授权（兼容路由）
func (a *ACLAPI) AddEndpointK8SServiceACLUsers(c *gin.Context) {
	ctx := c.Request.Context()
	endpointID := c.Param("id")

	var req AddEndpointK8SServiceACLUsersRequest
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
	namespacesJSON := formatJSONStringArray(req.Namespaces)
	serviceNamesJSON := formatJSONStringArray(req.ServiceNames)
	now := time.Now()

	for _, userID := range req.UserIDs {
		var existing model.AclK8SServiceUserPermission
		if err := db.DB.WithContext(ctx).Where("target_user_id = ? AND user_id = ?", agentID, userID).First(&existing).Error; err == nil {
			existing.Namespaces = namespacesJSON
			existing.ServiceNames = serviceNamesJSON
			existing.Enabled = true
			db.DB.WithContext(ctx).Save(&existing)
			continue
		}

		perm := &model.AclK8SServiceUserPermission{
			TargetUserID: agentID,
			UserID:       userID,
			Namespaces:   namespacesJSON,
			ServiceNames: serviceNamesJSON,
			Enabled:      true,
			GrantedAt:    now,
		}
		db.DB.WithContext(ctx).Create(perm)
	}

	logger.Infof("添加 K8SService 用户授权 (Endpoint兼容路由): endpoint_id=%s, agent_id=%d, user_ids=%v", endpointID, agentID, req.UserIDs)
	c.JSON(http.StatusOK, NewSuccessMessageResponse("授权成功", nil))
}

// AddEndpointK8SServiceACLGroupsRequest 添加 Endpoint K8SService 分组授权请求（兼容前端）
type AddEndpointK8SServiceACLGroupsRequest struct {
	GroupIDs     []int64  `json:"group_ids" binding:"required,min=1"`
	Namespaces   []string `json:"namespaces"`
	ServiceNames []string `json:"service_names"`
}

// AddEndpointK8SServiceACLGroups 添加 Endpoint K8SService 分组授权（兼容路由）
func (a *ACLAPI) AddEndpointK8SServiceACLGroups(c *gin.Context) {
	ctx := c.Request.Context()
	endpointID := c.Param("id")

	var req AddEndpointK8SServiceACLGroupsRequest
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
	namespacesJSON := formatJSONStringArray(req.Namespaces)
	serviceNamesJSON := formatJSONStringArray(req.ServiceNames)
	now := time.Now()

	for _, groupID := range req.GroupIDs {
		var existing model.AclK8SServiceGroupPermission
		if err := db.DB.WithContext(ctx).Where("target_user_id = ? AND group_id = ?", agentID, groupID).First(&existing).Error; err == nil {
			existing.Namespaces = namespacesJSON
			existing.ServiceNames = serviceNamesJSON
			existing.Enabled = true
			db.DB.WithContext(ctx).Save(&existing)
			continue
		}

		perm := &model.AclK8SServiceGroupPermission{
			TargetUserID: agentID,
			GroupID:      groupID,
			Namespaces:   namespacesJSON,
			ServiceNames: serviceNamesJSON,
			Enabled:      true,
			GrantedAt:    now,
		}
		db.DB.WithContext(ctx).Create(perm)
	}

	logger.Infof("添加 K8SService 分组授权 (Endpoint兼容路由): endpoint_id=%s, agent_id=%d, group_ids=%v", endpointID, agentID, req.GroupIDs)
	c.JSON(http.StatusOK, NewSuccessMessageResponse("授权成功", nil))
}

// RemoveEndpointK8SServiceACLUser 撤销 Endpoint K8SService 用户授权（兼容路由）
func (a *ACLAPI) RemoveEndpointK8SServiceACLUser(c *gin.Context) {
	ctx := c.Request.Context()
	endpointID := c.Param("id")
	userID, _ := strconv.ParseUint(c.Param("uid"), 10, 64)

	var endpoint model.Endpoint
	if err := db.DB.WithContext(ctx).First(&endpoint, "id = ?", endpointID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Endpoint 不存在"))
		return
	}

	agentID := endpoint.UserID
	result := db.DB.WithContext(ctx).Where("target_user_id = ? AND user_id = ?", agentID, userID).Delete(&model.AclK8SServiceUserPermission{})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("删除失败"))
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, NewErrorResponse("授权不存在"))
		return
	}

	logger.Infof("撤销 K8SService 用户授权 (Endpoint兼容路由): endpoint_id=%s, agent_id=%d, user_id=%d", endpointID, agentID, userID)
	c.JSON(http.StatusOK, NewSuccessMessageResponse("撤销成功", nil))
}

// RemoveEndpointK8SServiceACLGroup 撤销 Endpoint K8SService 分组授权（兼容路由）
func (a *ACLAPI) RemoveEndpointK8SServiceACLGroup(c *gin.Context) {
	ctx := c.Request.Context()
	endpointID := c.Param("id")
	groupID, _ := strconv.ParseInt(c.Param("gid"), 10, 64)

	var endpoint model.Endpoint
	if err := db.DB.WithContext(ctx).First(&endpoint, "id = ?", endpointID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Endpoint 不存在"))
		return
	}

	agentID := endpoint.UserID
	result := db.DB.WithContext(ctx).Where("target_user_id = ? AND group_id = ?", agentID, groupID).Delete(&model.AclK8SServiceGroupPermission{})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("删除失败"))
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, NewErrorResponse("授权不存在"))
		return
	}

	logger.Infof("撤销 K8SService 分组授权 (Endpoint兼容路由): endpoint_id=%s, agent_id=%d, group_id=%d", endpointID, agentID, groupID)
	c.JSON(http.StatusOK, NewSuccessMessageResponse("撤销成功", nil))
}
