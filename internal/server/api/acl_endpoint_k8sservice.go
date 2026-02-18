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

// ========== Endpoint K8SService 授权 ==========

// EndpointK8SServiceACLListItem Endpoint K8SService 授权列表项
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

// ListEndpointK8SServiceACL 获取 Endpoint K8SService 授权列表
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
		db.DB.WithContext(ctx).Model(&model.AclEndpointK8SServiceUserPermission{}).
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
		db.DB.WithContext(ctx).Model(&model.AclEndpointK8SServiceGroupPermission{}).
			Select("endpoint_id, COUNT(*) as count").
			Where("endpoint_id IN ?", endpointIDs).
			Group("endpoint_id").Find(&groupCounts)
		for _, gc := range groupCounts {
			groupCountMap[gc.EndpointID] = gc.Count
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

// EndpointK8SServiceACLPermissionItem Endpoint K8SService 授权项
type EndpointK8SServiceACLPermissionItem struct {
	ID           uint64    `json:"id"`
	Name         string    `json:"name"`
	Alias        string    `json:"alias"`
	Namespaces   []string  `json:"namespaces"`
	ServiceNames []string  `json:"service_names"`
	Enabled      bool      `json:"enabled"`
	GrantedAt    time.Time `json:"granted_at"`
}

// EndpointK8SServiceACLDetail Endpoint K8SService 授权详情
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

// GetEndpointK8SServiceACL 获取 Endpoint K8SService 授权详情
func (a *ACLAPI) GetEndpointK8SServiceACL(c *gin.Context) {
	ctx := c.Request.Context()
	endpointID := c.Param("id")

	var endpoint model.Endpoint
	if err := db.DB.WithContext(ctx).Preload("User").First(&endpoint, "id = ?", endpointID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Endpoint 不存在"))
		return
	}

	// 查询已授权用户
	var userPerms []model.AclEndpointK8SServiceUserPermission
	db.DB.WithContext(ctx).Preload("User").Where("endpoint_id = ?", endpointID).Find(&userPerms)

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

	// 查询已授权分组
	var groupPerms []model.AclEndpointK8SServiceGroupPermission
	db.DB.WithContext(ctx).Preload("Group").Where("endpoint_id = ?", endpointID).Find(&groupPerms)

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

// AddEndpointK8SServiceACLUsersRequest 添加 Endpoint K8SService 用户授权请求
type AddEndpointK8SServiceACLUsersRequest struct {
	UserIDs      []uint64 `json:"user_ids" binding:"required,min=1"`
	Namespaces   []string `json:"namespaces"`    // 允许的命名空间（空表示全部）
	ServiceNames []string `json:"service_names"` // 允许的 Service 名称（空表示全部）
}

// AddEndpointK8SServiceACLUsers 添加 Endpoint K8SService 用户授权
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

	namespacesJSON := formatJSONStringArray(req.Namespaces)
	serviceNamesJSON := formatJSONStringArray(req.ServiceNames)
	now := time.Now()

	for _, userID := range req.UserIDs {
		var existing model.AclEndpointK8SServiceUserPermission
		if err := db.DB.WithContext(ctx).Where("endpoint_id = ? AND user_id = ?", endpointID, userID).First(&existing).Error; err == nil {
			existing.Namespaces = namespacesJSON
			existing.ServiceNames = serviceNamesJSON
			existing.Enabled = true
			db.DB.WithContext(ctx).Save(&existing)
			continue
		}

		perm := &model.AclEndpointK8SServiceUserPermission{
			EndpointID:   endpointID,
			UserID:       userID,
			Namespaces:   namespacesJSON,
			ServiceNames: serviceNamesJSON,
			Enabled:      true,
			GrantedAt:    now,
		}
		db.DB.WithContext(ctx).Create(perm)
	}

	logger.Infof("添加 Endpoint K8SService 用户授权: endpoint_id=%s, user_ids=%v", endpointID, req.UserIDs)
	c.JSON(http.StatusOK, NewSuccessMessageResponse("授权成功", nil))
}

// AddEndpointK8SServiceACLGroupsRequest 添加 Endpoint K8SService 分组授权请求
type AddEndpointK8SServiceACLGroupsRequest struct {
	GroupIDs     []int64  `json:"group_ids" binding:"required,min=1"`
	Namespaces   []string `json:"namespaces"`
	ServiceNames []string `json:"service_names"`
}

// AddEndpointK8SServiceACLGroups 添加 Endpoint K8SService 分组授权
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

	namespacesJSON := formatJSONStringArray(req.Namespaces)
	serviceNamesJSON := formatJSONStringArray(req.ServiceNames)
	now := time.Now()

	for _, groupID := range req.GroupIDs {
		var existing model.AclEndpointK8SServiceGroupPermission
		if err := db.DB.WithContext(ctx).Where("endpoint_id = ? AND group_id = ?", endpointID, groupID).First(&existing).Error; err == nil {
			existing.Namespaces = namespacesJSON
			existing.ServiceNames = serviceNamesJSON
			existing.Enabled = true
			db.DB.WithContext(ctx).Save(&existing)
			continue
		}

		perm := &model.AclEndpointK8SServiceGroupPermission{
			EndpointID:   endpointID,
			GroupID:      groupID,
			Namespaces:   namespacesJSON,
			ServiceNames: serviceNamesJSON,
			Enabled:      true,
			GrantedAt:    now,
		}
		db.DB.WithContext(ctx).Create(perm)
	}

	logger.Infof("添加 Endpoint K8SService 分组授权: endpoint_id=%s, group_ids=%v", endpointID, req.GroupIDs)
	c.JSON(http.StatusOK, NewSuccessMessageResponse("授权成功", nil))
}

// RemoveEndpointK8SServiceACLUser 撤销 Endpoint K8SService 用户授权
func (a *ACLAPI) RemoveEndpointK8SServiceACLUser(c *gin.Context) {
	ctx := c.Request.Context()
	endpointID := c.Param("id")
	userID, _ := strconv.ParseUint(c.Param("uid"), 10, 64)

	result := db.DB.WithContext(ctx).Where("endpoint_id = ? AND user_id = ?", endpointID, userID).Delete(&model.AclEndpointK8SServiceUserPermission{})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("删除失败"))
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, NewErrorResponse("授权不存在"))
		return
	}

	logger.Infof("撤销 Endpoint K8SService 用户授权: endpoint_id=%s, user_id=%d", endpointID, userID)
	c.JSON(http.StatusOK, NewSuccessMessageResponse("撤销成功", nil))
}

// RemoveEndpointK8SServiceACLGroup 撤销 Endpoint K8SService 分组授权
func (a *ACLAPI) RemoveEndpointK8SServiceACLGroup(c *gin.Context) {
	ctx := c.Request.Context()
	endpointID := c.Param("id")
	groupID, _ := strconv.ParseInt(c.Param("gid"), 10, 64)

	result := db.DB.WithContext(ctx).Where("endpoint_id = ? AND group_id = ?", endpointID, groupID).Delete(&model.AclEndpointK8SServiceGroupPermission{})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("删除失败"))
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, NewErrorResponse("授权不存在"))
		return
	}

	logger.Infof("撤销 Endpoint K8SService 分组授权: endpoint_id=%s, group_id=%d", endpointID, groupID)
	c.JSON(http.StatusOK, NewSuccessMessageResponse("撤销成功", nil))
}
