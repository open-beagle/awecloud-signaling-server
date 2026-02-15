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

// ========== Endpoint SSH 授权 ==========

// EndpointSSHACLListItem Endpoint SSH 授权列表项
type EndpointSSHACLListItem struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Alias      string    `json:"alias"`
	AgentID    uint64    `json:"agent_id"`
	AgentName  string    `json:"agent_name"`
	Host       string    `json:"host"`
	Port       int       `json:"port"`
	Status     string    `json:"status"`
	UserCount  int64     `json:"user_count"`
	GroupCount int64     `json:"group_count"`
	CreatedAt  time.Time `json:"created_at"`
}

// ListEndpointSSHACL 获取 Endpoint SSH 授权列表
func (a *ACLAPI) ListEndpointSSHACL(c *gin.Context) {
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

	query := db.DB.WithContext(ctx).Model(&model.EndpointSSH{}).Where("revoked = ?", false)
	if search != "" {
		query = query.Where("name LIKE ? OR alias LIKE ?", "%"+search+"%", "%"+search+"%")
	}

	var total int64
	query.Count(&total)

	var endpoints []model.EndpointSSH
	offset := (page - 1) * size
	if err := query.Preload("User").Order("created_at DESC").Offset(offset).Limit(size).Find(&endpoints).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询失败"))
		return
	}

	// 查询授权数量
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
		db.DB.WithContext(ctx).Model(&model.AclEndpointSSHUserPermission{}).
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
		db.DB.WithContext(ctx).Model(&model.AclEndpointSSHGroupPermission{}).
			Select("endpoint_id, COUNT(*) as count").
			Where("endpoint_id IN ?", endpointIDs).
			Group("endpoint_id").Find(&groupCounts)
		for _, gc := range groupCounts {
			groupCountMap[gc.EndpointID] = gc.Count
		}
	}

	result := make([]EndpointSSHACLListItem, len(endpoints))
	for i, ep := range endpoints {
		agentName := ""
		if ep.User != nil {
			agentName = ep.User.Name
		}
		result[i] = EndpointSSHACLListItem{
			ID:         ep.ID,
			Name:       ep.Name,
			Alias:      ep.Alias,
			AgentID:    ep.UserID,
			AgentName:  agentName,
			Host:       ep.Host,
			Port:       ep.Port,
			Status:     ep.Status,
			UserCount:  userCountMap[ep.ID],
			GroupCount: groupCountMap[ep.ID],
			CreatedAt:  ep.CreatedAt,
		}
	}

	c.JSON(http.StatusOK, NewPagedResponse(result, total, page, size))
}

// EndpointSSHACLPermissionItem Endpoint SSH 授权项
type EndpointSSHACLPermissionItem struct {
	ID        uint64    `json:"id"`
	Name      string    `json:"name"`
	Alias     string    `json:"alias"`
	SSHUsers  []string  `json:"ssh_users"`
	Enabled   bool      `json:"enabled"`
	GrantedAt time.Time `json:"granted_at"`
}

// EndpointSSHACLDetail Endpoint SSH 授权详情
type EndpointSSHACLDetail struct {
	ID        string                         `json:"id"`
	Name      string                         `json:"name"`
	Alias     string                         `json:"alias"`
	AgentID   uint64                         `json:"agent_id"`
	AgentName string                         `json:"agent_name"`
	Host      string                         `json:"host"`
	Port      int                            `json:"port"`
	Status    string                         `json:"status"`
	Users     []EndpointSSHACLPermissionItem `json:"users"`
	Groups    []EndpointSSHACLPermissionItem `json:"groups"`
}

// GetEndpointSSHACL 获取 Endpoint SSH 授权详情
func (a *ACLAPI) GetEndpointSSHACL(c *gin.Context) {
	ctx := c.Request.Context()
	endpointID := c.Param("id")

	var endpoint model.EndpointSSH
	if err := db.DB.WithContext(ctx).Preload("User").First(&endpoint, "id = ?", endpointID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Endpoint 不存在"))
		return
	}

	// 查询已授权用户
	var userPerms []model.AclEndpointSSHUserPermission
	db.DB.WithContext(ctx).Preload("User").Where("endpoint_id = ?", endpointID).Find(&userPerms)

	users := make([]EndpointSSHACLPermissionItem, 0, len(userPerms))
	for _, p := range userPerms {
		if p.User != nil {
			users = append(users, EndpointSSHACLPermissionItem{
				ID:        p.User.ID,
				Name:      p.User.Name,
				Alias:     p.User.Alias,
				SSHUsers:  parseJSONStringArray(p.SSHUsers),
				Enabled:   p.Enabled,
				GrantedAt: p.GrantedAt,
			})
		}
	}

	// 查询已授权分组
	var groupPerms []model.AclEndpointSSHGroupPermission
	db.DB.WithContext(ctx).Preload("Group").Where("endpoint_id = ?", endpointID).Find(&groupPerms)

	groups := make([]EndpointSSHACLPermissionItem, 0, len(groupPerms))
	for _, p := range groupPerms {
		if p.Group != nil {
			groups = append(groups, EndpointSSHACLPermissionItem{
				ID:        uint64(p.Group.ID),
				Name:      p.Group.Name,
				Alias:     p.Group.Alias,
				SSHUsers:  parseJSONStringArray(p.SSHUsers),
				Enabled:   p.Enabled,
				GrantedAt: p.GrantedAt,
			})
		}
	}

	agentName := ""
	if endpoint.User != nil {
		agentName = endpoint.User.Name
	}

	result := EndpointSSHACLDetail{
		ID:        endpoint.ID,
		Name:      endpoint.Name,
		Alias:     endpoint.Alias,
		AgentID:   endpoint.UserID,
		AgentName: agentName,
		Host:      endpoint.Host,
		Port:      endpoint.Port,
		Status:    endpoint.Status,
		Users:     users,
		Groups:    groups,
	}

	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

// AddEndpointSSHACLUsersRequest 添加 Endpoint SSH 用户授权请求
type AddEndpointSSHACLUsersRequest struct {
	UserIDs  []uint64 `json:"user_ids" binding:"required,min=1"`
	SSHUsers []string `json:"ssh_users"` // 允许的 SSH 登录用户名
}

// AddEndpointSSHACLUsers 添加 Endpoint SSH 用户授权
func (a *ACLAPI) AddEndpointSSHACLUsers(c *gin.Context) {
	ctx := c.Request.Context()
	endpointID := c.Param("id")

	var req AddEndpointSSHACLUsersRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	var endpoint model.EndpointSSH
	if err := db.DB.WithContext(ctx).First(&endpoint, "id = ?", endpointID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Endpoint 不存在"))
		return
	}

	sshUsersJSON := formatJSONStringArray(req.SSHUsers)
	now := time.Now()

	for _, userID := range req.UserIDs {
		var existing model.AclEndpointSSHUserPermission
		if err := db.DB.WithContext(ctx).Where("endpoint_id = ? AND user_id = ?", endpointID, userID).First(&existing).Error; err == nil {
			existing.SSHUsers = sshUsersJSON
			existing.Enabled = true
			db.DB.WithContext(ctx).Save(&existing)
			continue
		}

		perm := &model.AclEndpointSSHUserPermission{
			EndpointID: endpointID,
			UserID:     userID,
			SSHUsers:   sshUsersJSON,
			Enabled:    true,
			GrantedAt:  now,
		}
		db.DB.WithContext(ctx).Create(perm)
	}

	logger.Infof("添加 Endpoint SSH 用户授权: endpoint_id=%s, user_ids=%v", endpointID, req.UserIDs)
	c.JSON(http.StatusOK, NewSuccessMessageResponse("授权成功", nil))
}

// AddEndpointSSHACLGroupsRequest 添加 Endpoint SSH 分组授权请求
type AddEndpointSSHACLGroupsRequest struct {
	GroupIDs []int64  `json:"group_ids" binding:"required,min=1"`
	SSHUsers []string `json:"ssh_users"` // 允许的 SSH 登录用户名
}

// AddEndpointSSHACLGroups 添加 Endpoint SSH 分组授权
func (a *ACLAPI) AddEndpointSSHACLGroups(c *gin.Context) {
	ctx := c.Request.Context()
	endpointID := c.Param("id")

	var req AddEndpointSSHACLGroupsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	var endpoint model.EndpointSSH
	if err := db.DB.WithContext(ctx).First(&endpoint, "id = ?", endpointID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Endpoint 不存在"))
		return
	}

	sshUsersJSON := formatJSONStringArray(req.SSHUsers)
	now := time.Now()

	for _, groupID := range req.GroupIDs {
		var existing model.AclEndpointSSHGroupPermission
		if err := db.DB.WithContext(ctx).Where("endpoint_id = ? AND group_id = ?", endpointID, groupID).First(&existing).Error; err == nil {
			existing.SSHUsers = sshUsersJSON
			existing.Enabled = true
			db.DB.WithContext(ctx).Save(&existing)
			continue
		}

		perm := &model.AclEndpointSSHGroupPermission{
			EndpointID: endpointID,
			GroupID:    groupID,
			SSHUsers:   sshUsersJSON,
			Enabled:    true,
			GrantedAt:  now,
		}
		db.DB.WithContext(ctx).Create(perm)
	}

	logger.Infof("添加 Endpoint SSH 分组授权: endpoint_id=%s, group_ids=%v", endpointID, req.GroupIDs)
	c.JSON(http.StatusOK, NewSuccessMessageResponse("授权成功", nil))
}

// RemoveEndpointSSHACLUser 撤销 Endpoint SSH 用户授权
func (a *ACLAPI) RemoveEndpointSSHACLUser(c *gin.Context) {
	ctx := c.Request.Context()
	endpointID := c.Param("id")
	userID, _ := strconv.ParseUint(c.Param("uid"), 10, 64)

	result := db.DB.WithContext(ctx).Where("endpoint_id = ? AND user_id = ?", endpointID, userID).Delete(&model.AclEndpointSSHUserPermission{})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("删除失败"))
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, NewErrorResponse("授权不存在"))
		return
	}

	logger.Infof("撤销 Endpoint SSH 用户授权: endpoint_id=%s, user_id=%d", endpointID, userID)
	c.JSON(http.StatusOK, NewSuccessMessageResponse("撤销成功", nil))
}

// RemoveEndpointSSHACLGroup 撤销 Endpoint SSH 分组授权
func (a *ACLAPI) RemoveEndpointSSHACLGroup(c *gin.Context) {
	ctx := c.Request.Context()
	endpointID := c.Param("id")
	groupID, _ := strconv.ParseInt(c.Param("gid"), 10, 64)

	result := db.DB.WithContext(ctx).Where("endpoint_id = ? AND group_id = ?", endpointID, groupID).Delete(&model.AclEndpointSSHGroupPermission{})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("删除失败"))
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, NewErrorResponse("授权不存在"))
		return
	}

	logger.Infof("撤销 Endpoint SSH 分组授权: endpoint_id=%s, group_id=%d", endpointID, groupID)
	c.JSON(http.StatusOK, NewSuccessMessageResponse("撤销成功", nil))
}
