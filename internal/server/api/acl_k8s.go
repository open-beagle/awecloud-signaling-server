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
)

// ========== K8S 授权 ==========

// K8SACLListItem K8S 授权列表项
type K8SACLListItem struct {
	ID         uint64    `json:"id"`
	Name       string    `json:"name"`
	Alias      string    `json:"alias"`
	Role       string    `json:"role"`
	UserCount  int64     `json:"user_count"`
	GroupCount int64     `json:"group_count"`
	CreatedAt  time.Time `json:"created_at"`
}

// ListK8SACL 获取 K8S 授权列表（仅 Agent 角色）
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
			UserCount:  userCountMap[user.ID],
			GroupCount: groupCountMap[user.ID],
			CreatedAt:  user.CreatedAt,
		}
	}

	c.JSON(http.StatusOK, NewPagedResponse(result, total, page, size))
}

// K8SACLPermissionItem K8S 授权项
type K8SACLPermissionItem struct {
	ID         uint64    `json:"id"`
	Name       string    `json:"name"`
	Alias      string    `json:"alias"`
	K8SGroups  []string  `json:"k8s_groups"`
	Namespaces []string  `json:"namespaces"`
	Enabled    bool      `json:"enabled"`
	GrantedAt  time.Time `json:"granted_at"`
}

// K8SACLDetail K8S 授权详情
type K8SACLDetail struct {
	ID     uint64                 `json:"id"`
	Name   string                 `json:"name"`
	Alias  string                 `json:"alias"`
	Role   string                 `json:"role"`
	Users  []K8SACLPermissionItem `json:"users"`
	Groups []K8SACLPermissionItem `json:"groups"`
}

// GetK8SACL 获取 K8S 授权详情
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
		ID:     targetUser.ID,
		Name:   targetUser.Name,
		Alias:  targetUser.Alias,
		Role:   string(targetUser.Role),
		Users:  users,
		Groups: groups,
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
