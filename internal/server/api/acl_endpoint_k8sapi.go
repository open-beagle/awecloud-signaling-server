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

// ========== Endpoint K8SAPI 授权 ==========

// EndpointK8SAPIACLListItem Endpoint K8SAPI 授权列表项
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

// ListEndpointK8SAPIACL 获取 Endpoint K8SAPI 授权列表
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

	query := db.DB.WithContext(ctx).Model(&model.EndpointK8SAPI{}).Where("revoked = ?", false)
	if search != "" {
		query = query.Where("name LIKE ? OR alias LIKE ?", "%"+search+"%", "%"+search+"%")
	}

	var total int64
	query.Count(&total)

	var endpoints []model.EndpointK8SAPI
	offset := (page - 1) * size
	if err := query.Preload("User").Order("created_at DESC").Offset(offset).Limit(size).Find(&endpoints).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询失败"))
		return
	}

	endpointIDs := make([]string, len(endpoints))
	for i, ep := range endpoints {
		endpointIDs[i] = ep.ID
	}

	userCountMap := make(map[string]int64)
	groupCountMap := make(map[string]int64)

	if len(endpointIDs) > 0 {
		var userCounts []struct {
			EndpointID string `gorm:"column:endpoint_id"`
			Count      int64  `gorm:"column:count"`
		}
		db.DB.WithContext(ctx).Model(&model.AclEndpointK8SAPIUserPermission{}).
			Select("endpoint_id, COUNT(*) as count").
			Where("endpoint_id IN ?", endpointIDs).
			Group("endpoint_id").Find(&userCounts)
		for _, uc := range userCounts {
			userCountMap[uc.EndpointID] = uc.Count
		}

		var groupCounts []struct {
			EndpointID string `gorm:"column:endpoint_id"`
			Count      int64  `gorm:"column:count"`
		}
		db.DB.WithContext(ctx).Model(&model.AclEndpointK8SAPIGroupPermission{}).
			Select("endpoint_id, COUNT(*) as count").
			Where("endpoint_id IN ?", endpointIDs).
			Group("endpoint_id").Find(&groupCounts)
		for _, gc := range groupCounts {
			groupCountMap[gc.EndpointID] = gc.Count
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
			APIServer:  ep.APIServer,
			Status:     ep.Status,
			UserCount:  userCountMap[ep.ID],
			GroupCount: groupCountMap[ep.ID],
			CreatedAt:  ep.CreatedAt,
		}
	}

	c.JSON(http.StatusOK, NewPagedResponse(result, total, page, size))
}

// EndpointK8SAPIACLPermissionItem Endpoint K8SAPI 授权项
type EndpointK8SAPIACLPermissionItem struct {
	ID         uint64    `json:"id"`
	Name       string    `json:"name"`
	Alias      string    `json:"alias"`
	K8SGroups  []string  `json:"k8s_groups"`
	Namespaces []string  `json:"namespaces"`
	Enabled    bool      `json:"enabled"`
	GrantedAt  time.Time `json:"granted_at"`
}

// EndpointK8SAPIACLDetail Endpoint K8SAPI 授权详情
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

// GetEndpointK8SAPIACL 获取 Endpoint K8SAPI 授权详情
func (a *ACLAPI) GetEndpointK8SAPIACL(c *gin.Context) {
	ctx := c.Request.Context()
	endpointID := c.Param("id")

	var endpoint model.EndpointK8SAPI
	if err := db.DB.WithContext(ctx).Preload("User").First(&endpoint, "id = ?", endpointID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Endpoint 不存在"))
		return
	}

	// 查询已授权用户
	var userPerms []model.AclEndpointK8SAPIUserPermission
	db.DB.WithContext(ctx).Preload("User").Where("endpoint_id = ?", endpointID).Find(&userPerms)

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

	// 查询已授权分组
	var groupPerms []model.AclEndpointK8SAPIGroupPermission
	db.DB.WithContext(ctx).Preload("Group").Where("endpoint_id = ?", endpointID).Find(&groupPerms)

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
		APIServer: endpoint.APIServer,
		Status:    endpoint.Status,
		Users:     users,
		Groups:    groups,
	}

	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

// AddEndpointK8SAPIACLUsersRequest 添加 Endpoint K8SAPI 用户授权请求
type AddEndpointK8SAPIACLUsersRequest struct {
	UserIDs    []uint64 `json:"user_ids" binding:"required,min=1"`
	K8SGroups  []string `json:"k8s_groups"` // K8S Impersonation 分组
	Namespaces []string `json:"namespaces"` // 允许的命名空间（空表示全部）
}

// AddEndpointK8SAPIACLUsers 添加 Endpoint K8SAPI 用户授权
func (a *ACLAPI) AddEndpointK8SAPIACLUsers(c *gin.Context) {
	ctx := c.Request.Context()
	endpointID := c.Param("id")

	var req AddEndpointK8SAPIACLUsersRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	var endpoint model.EndpointK8SAPI
	if err := db.DB.WithContext(ctx).First(&endpoint, "id = ?", endpointID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Endpoint 不存在"))
		return
	}

	k8sGroupsJSON := formatJSONStringArray(req.K8SGroups)
	namespacesJSON := formatJSONStringArray(req.Namespaces)
	now := time.Now()

	for _, userID := range req.UserIDs {
		var existing model.AclEndpointK8SAPIUserPermission
		if err := db.DB.WithContext(ctx).Where("endpoint_id = ? AND user_id = ?", endpointID, userID).First(&existing).Error; err == nil {
			existing.K8SGroups = k8sGroupsJSON
			existing.Namespaces = namespacesJSON
			existing.Enabled = true
			db.DB.WithContext(ctx).Save(&existing)
			continue
		}

		perm := &model.AclEndpointK8SAPIUserPermission{
			EndpointID: endpointID,
			UserID:     userID,
			K8SGroups:  k8sGroupsJSON,
			Namespaces: namespacesJSON,
			Enabled:    true,
			GrantedAt:  now,
		}
		db.DB.WithContext(ctx).Create(perm)
	}

	logger.Infof("添加 Endpoint K8SAPI 用户授权: endpoint_id=%s, user_ids=%v", endpointID, req.UserIDs)
	c.JSON(http.StatusOK, NewSuccessMessageResponse("授权成功", nil))
}

// AddEndpointK8SAPIACLGroupsRequest 添加 Endpoint K8SAPI 分组授权请求
type AddEndpointK8SAPIACLGroupsRequest struct {
	GroupIDs   []int64  `json:"group_ids" binding:"required,min=1"`
	K8SGroups  []string `json:"k8s_groups"`
	Namespaces []string `json:"namespaces"`
}

// AddEndpointK8SAPIACLGroups 添加 Endpoint K8SAPI 分组授权
func (a *ACLAPI) AddEndpointK8SAPIACLGroups(c *gin.Context) {
	ctx := c.Request.Context()
	endpointID := c.Param("id")

	var req AddEndpointK8SAPIACLGroupsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	var endpoint model.EndpointK8SAPI
	if err := db.DB.WithContext(ctx).First(&endpoint, "id = ?", endpointID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Endpoint 不存在"))
		return
	}

	k8sGroupsJSON := formatJSONStringArray(req.K8SGroups)
	namespacesJSON := formatJSONStringArray(req.Namespaces)
	now := time.Now()

	for _, groupID := range req.GroupIDs {
		var existing model.AclEndpointK8SAPIGroupPermission
		if err := db.DB.WithContext(ctx).Where("endpoint_id = ? AND group_id = ?", endpointID, groupID).First(&existing).Error; err == nil {
			existing.K8SGroups = k8sGroupsJSON
			existing.Namespaces = namespacesJSON
			existing.Enabled = true
			db.DB.WithContext(ctx).Save(&existing)
			continue
		}

		perm := &model.AclEndpointK8SAPIGroupPermission{
			EndpointID: endpointID,
			GroupID:    groupID,
			K8SGroups:  k8sGroupsJSON,
			Namespaces: namespacesJSON,
			Enabled:    true,
			GrantedAt:  now,
		}
		db.DB.WithContext(ctx).Create(perm)
	}

	logger.Infof("添加 Endpoint K8SAPI 分组授权: endpoint_id=%s, group_ids=%v", endpointID, req.GroupIDs)
	c.JSON(http.StatusOK, NewSuccessMessageResponse("授权成功", nil))
}

// RemoveEndpointK8SAPIACLUser 撤销 Endpoint K8SAPI 用户授权
func (a *ACLAPI) RemoveEndpointK8SAPIACLUser(c *gin.Context) {
	ctx := c.Request.Context()
	endpointID := c.Param("id")
	userID, _ := strconv.ParseUint(c.Param("uid"), 10, 64)

	result := db.DB.WithContext(ctx).Where("endpoint_id = ? AND user_id = ?", endpointID, userID).Delete(&model.AclEndpointK8SAPIUserPermission{})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("删除失败"))
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, NewErrorResponse("授权不存在"))
		return
	}

	logger.Infof("撤销 Endpoint K8SAPI 用户授权: endpoint_id=%s, user_id=%d", endpointID, userID)
	c.JSON(http.StatusOK, NewSuccessMessageResponse("撤销成功", nil))
}

// RemoveEndpointK8SAPIACLGroup 撤销 Endpoint K8SAPI 分组授权
func (a *ACLAPI) RemoveEndpointK8SAPIACLGroup(c *gin.Context) {
	ctx := c.Request.Context()
	endpointID := c.Param("id")
	groupID, _ := strconv.ParseInt(c.Param("gid"), 10, 64)

	result := db.DB.WithContext(ctx).Where("endpoint_id = ? AND group_id = ?", endpointID, groupID).Delete(&model.AclEndpointK8SAPIGroupPermission{})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("删除失败"))
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, NewErrorResponse("授权不存在"))
		return
	}

	logger.Infof("撤销 Endpoint K8SAPI 分组授权: endpoint_id=%s, group_id=%d", endpointID, groupID)
	c.JSON(http.StatusOK, NewSuccessMessageResponse("撤销成功", nil))
}
