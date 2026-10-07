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
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

// ACLAPI 授权管理 API
type ACLAPI struct {
	config         *config.ServerConfig
	aclSync        aclSyncer
	desktopService DesktopServiceInterface
}

// DesktopServiceInterface Desktop 服务接口（用于权限变更推送）
type DesktopServiceInterface interface {
	NotifyAllDesktopsDataChange(dataType pb.DesktopDataType)
}

// NewACLAPI 创建 ACLAPI
func NewACLAPI(cfg *config.ServerConfig) *ACLAPI {
	api := &ACLAPI{config: cfg}

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

// SetDesktopService 设置 Desktop 服务（用于权限变更推送）
func (a *ACLAPI) SetDesktopService(desktopService DesktopServiceInterface) {
	a.desktopService = desktopService
}

// notifyDesktopDataChange 通知 Desktop 数据变更（辅助方法）
func (a *ACLAPI) notifyDesktopDataChange(dataType pb.DesktopDataType) {
	if a.desktopService != nil {
		go a.desktopService.NotifyAllDesktopsDataChange(dataType)
	}
}

// ========== 服务授权 ==========

// ServiceACLListItem 服务授权列表项
type ServiceACLListItem struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Alias      string    `json:"alias"`
	UserID     uint64    `json:"user_id"`
	UserName   string    `json:"user_name"`
	SourceAddr string    `json:"source_addr"`
	UserCount  int64     `json:"user_count"`  // 已授权用户数
	GroupCount int64     `json:"group_count"` // 已授权分组数
	CreatedAt  time.Time `json:"created_at"`
}

// ListServiceACL 获取服务授权列表
func (a *ACLAPI) ListServiceACL(c *gin.Context) {
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

	query := db.DB.WithContext(ctx).Model(&model.ProxyService{}).Preload("User")
	if search != "" {
		query = query.Where("name LIKE ? OR alias LIKE ?", "%"+search+"%", "%"+search+"%")
	}

	var total int64
	query.Count(&total)

	var services []model.ProxyService
	offset := (page - 1) * size
	if err := query.Order("created_at DESC").Offset(offset).Limit(size).Find(&services).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询失败"))
		return
	}

	// 查询每个服务的授权数量
	var userCounts []struct {
		ServiceID string `gorm:"column:service_id"`
		Count     int64  `gorm:"column:count"`
	}
	db.DB.WithContext(ctx).Model(&model.AclServiceUserPermission{}).
		Select("service_id, COUNT(*) as count").
		Group("service_id").Find(&userCounts)

	userCountMap := make(map[string]int64)
	for _, uc := range userCounts {
		userCountMap[uc.ServiceID] = uc.Count
	}

	var groupCounts []struct {
		ServiceID string `gorm:"column:service_id"`
		Count     int64  `gorm:"column:count"`
	}
	db.DB.WithContext(ctx).Model(&model.AclServiceGroupPermission{}).
		Select("service_id, COUNT(*) as count").
		Group("service_id").Find(&groupCounts)

	groupCountMap := make(map[string]int64)
	for _, gc := range groupCounts {
		groupCountMap[gc.ServiceID] = gc.Count
	}

	result := make([]ServiceACLListItem, len(services))
	for i, svc := range services {
		userName := ""
		if svc.User != nil {
			userName = svc.User.Name
		}

		result[i] = ServiceACLListItem{
			ID:         svc.ID,
			Name:       svc.Name,
			Alias:      svc.Alias,
			UserID:     svc.UserID,
			UserName:   userName,
			SourceAddr: svc.SourceAddr,
			UserCount:  userCountMap[svc.ID],
			GroupCount: groupCountMap[svc.ID],
			CreatedAt:  svc.CreatedAt,
		}
	}

	c.JSON(http.StatusOK, NewPagedResponse(result, total, page, size))
}

// ServiceACLDetail 服务授权详情
type ServiceACLDetail struct {
	ID         string              `json:"id"`
	Name       string              `json:"name"`
	Alias      string              `json:"alias"`
	UserID     uint64              `json:"user_id"`
	UserName   string              `json:"user_name"`
	SourceAddr string              `json:"source_addr"`
	TargetAddr string              `json:"target_addr"`
	Users      []ACLPermissionItem `json:"users"`
	Groups     []ACLPermissionItem `json:"groups"`
}

// ACLPermissionItem 授权项
type ACLPermissionItem struct {
	ID        uint64    `json:"id"`
	Name      string    `json:"name"`
	Alias     string    `json:"alias"`
	GrantedAt time.Time `json:"granted_at"`
}

// GetServiceACL 获取服务授权详情
func (a *ACLAPI) GetServiceACL(c *gin.Context) {
	ctx := c.Request.Context()
	serviceID := c.Param("id")

	var service model.ProxyService
	if err := db.DB.WithContext(ctx).Preload("User").First(&service, "id = ?", serviceID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("服务不存在"))
		return
	}

	// 查询已授权用户
	var userPerms []model.AclServiceUserPermission
	db.DB.WithContext(ctx).Preload("User").Where("service_id = ?", serviceID).Find(&userPerms)

	users := make([]ACLPermissionItem, 0, len(userPerms))
	for _, p := range userPerms {
		if p.User != nil {
			users = append(users, ACLPermissionItem{
				ID:        p.User.ID,
				Name:      p.User.Name,
				Alias:     p.User.Alias,
				GrantedAt: p.GrantedAt,
			})
		}
	}

	// 查询已授权分组
	var groupPerms []model.AclServiceGroupPermission
	db.DB.WithContext(ctx).Preload("Group").Where("service_id = ?", serviceID).Find(&groupPerms)

	groups := make([]ACLPermissionItem, 0, len(groupPerms))
	for _, p := range groupPerms {
		if p.Group != nil {
			groups = append(groups, ACLPermissionItem{
				ID:        uint64(p.Group.ID),
				Name:      p.Group.Name,
				Alias:     p.Group.Alias,
				GrantedAt: p.GrantedAt,
			})
		}
	}

	userName := ""
	if service.User != nil {
		userName = service.User.Name
	}

	result := ServiceACLDetail{
		ID:         service.ID,
		Name:       service.Name,
		Alias:      service.Alias,
		UserID:     service.UserID,
		UserName:   userName,
		SourceAddr: service.SourceAddr,
		TargetAddr: service.TargetAddr,
		Users:      users,
		Groups:     groups,
	}

	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

// AddACLUsersRequest 添加用户授权请求（支持批量）
type AddACLUsersRequest struct {
	UserIDs []uint64 `json:"user_ids" binding:"required,min=1"`
}

// AddServiceACLUsers 添加服务用户授权
func (a *ACLAPI) AddServiceACLUsers(c *gin.Context) {
	a.writeACL(c, "service", false, false)
}

// AddACLGroupsRequest 添加分组授权请求（支持批量）
type AddACLGroupsRequest struct {
	GroupIDs []int64 `json:"group_ids" binding:"required,min=1"`
}

// AddServiceACLGroups 添加服务分组授权
func (a *ACLAPI) AddServiceACLGroups(c *gin.Context) {
	a.writeACL(c, "service", true, false)
}

// RemoveServiceACLUser 撤销服务用户授权
func (a *ACLAPI) RemoveServiceACLUser(c *gin.Context) {
	a.writeACL(c, "service", false, true)
}

// RemoveServiceACLGroup 撤销服务分组授权
func (a *ACLAPI) RemoveServiceACLGroup(c *gin.Context) {
	a.writeACL(c, "service", true, true)
}

// ========== 用户授权 ==========

// UserACLListItem 用户授权列表项
type UserACLListItem struct {
	ID         uint64    `json:"id"`
	Name       string    `json:"name"`
	Alias      string    `json:"alias"`
	Role       string    `json:"role"`
	UserCount  int64     `json:"user_count"`
	GroupCount int64     `json:"group_count"`
	CreatedAt  time.Time `json:"created_at"`
}

// ListUserACL 获取用户授权列表（仅 Agent 角色）
func (a *ACLAPI) ListUserACL(c *gin.Context) {
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

	// 只查询 Agent 角色的用户
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

	// 查询每个用户的授权数量
	var userCounts []struct {
		TargetUserID uint64 `gorm:"column:target_user_id"`
		Count        int64  `gorm:"column:count"`
	}
	db.DB.WithContext(ctx).Model(&model.AclUserUserPermission{}).
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
	db.DB.WithContext(ctx).Model(&model.AclUserGroupPermission{}).
		Select("target_user_id, COUNT(*) as count").
		Group("target_user_id").Find(&groupCounts)

	groupCountMap := make(map[uint64]int64)
	for _, gc := range groupCounts {
		groupCountMap[gc.TargetUserID] = gc.Count
	}

	result := make([]UserACLListItem, len(users))
	for i, user := range users {
		result[i] = UserACLListItem{
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

// UserACLDetail 用户授权详情
type UserACLDetail struct {
	ID     uint64              `json:"id"`
	Name   string              `json:"name"`
	Alias  string              `json:"alias"`
	Role   string              `json:"role"`
	Users  []ACLPermissionItem `json:"users"`
	Groups []ACLPermissionItem `json:"groups"`
}

// GetUserACL 获取用户授权详情
func (a *ACLAPI) GetUserACL(c *gin.Context) {
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
	var userPerms []model.AclUserUserPermission
	db.DB.WithContext(ctx).Preload("GrantedUser").Where("target_user_id = ?", targetUserID).Find(&userPerms)

	users := make([]ACLPermissionItem, 0, len(userPerms))
	for _, p := range userPerms {
		if p.GrantedUser != nil {
			users = append(users, ACLPermissionItem{
				ID:        p.GrantedUser.ID,
				Name:      p.GrantedUser.Name,
				Alias:     p.GrantedUser.Alias,
				GrantedAt: p.GrantedAt,
			})
		}
	}

	// 查询已授权分组
	var groupPerms []model.AclUserGroupPermission
	db.DB.WithContext(ctx).Preload("Group").Where("target_user_id = ?", targetUserID).Find(&groupPerms)

	groups := make([]ACLPermissionItem, 0, len(groupPerms))
	for _, p := range groupPerms {
		if p.Group != nil {
			groups = append(groups, ACLPermissionItem{
				ID:        uint64(p.Group.ID),
				Name:      p.Group.Name,
				Alias:     p.Group.Alias,
				GrantedAt: p.GrantedAt,
			})
		}
	}

	result := UserACLDetail{
		ID:     targetUser.ID,
		Name:   targetUser.Name,
		Alias:  targetUser.Alias,
		Role:   string(targetUser.Role),
		Users:  users,
		Groups: groups,
	}

	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

// AddUserACLUsers 添加用户授权（用户级别）
func (a *ACLAPI) AddUserACLUsers(c *gin.Context) {
	a.writeACL(c, "user", false, false)
}

// AddUserACLGroups 添加用户授权（分组级别）
func (a *ACLAPI) AddUserACLGroups(c *gin.Context) {
	a.writeACL(c, "user", true, false)
}

// RemoveUserACLUser 撤销用户授权（用户级别）
func (a *ACLAPI) RemoveUserACLUser(c *gin.Context) {
	a.writeACL(c, "user", false, true)
}

// RemoveUserACLGroup 撤销用户授权（分组级别）
func (a *ACLAPI) RemoveUserACLGroup(c *gin.Context) {
	a.writeACL(c, "user", true, true)
}

// ========== 分组授权 ==========

// GroupACLListItem 分组授权列表项
type GroupACLListItem struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Alias       string    `json:"alias"`
	MemberCount int64     `json:"member_count"`
	UserCount   int64     `json:"user_count"`
	GroupCount  int64     `json:"group_count"`
	CreatedAt   time.Time `json:"created_at"`
}

// ListGroupACL 获取分组授权列表
func (a *ACLAPI) ListGroupACL(c *gin.Context) {
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

	query := db.DB.WithContext(ctx).Model(&model.Group{})
	if search != "" {
		query = query.Where("name LIKE ? OR alias LIKE ?", "%"+search+"%", "%"+search+"%")
	}

	var total int64
	query.Count(&total)

	var groups []model.Group
	offset := (page - 1) * size
	if err := query.Order("created_at DESC").Offset(offset).Limit(size).Find(&groups).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询失败"))
		return
	}

	// 查询成员数量
	var memberCounts []struct {
		GroupID int64 `gorm:"column:group_id"`
		Count   int64 `gorm:"column:count"`
	}
	db.DB.WithContext(ctx).Model(&model.GroupMember{}).
		Select("group_id, COUNT(*) as count").
		Group("group_id").Find(&memberCounts)

	memberCountMap := make(map[int64]int64)
	for _, mc := range memberCounts {
		memberCountMap[mc.GroupID] = mc.Count
	}

	// 查询授权数量
	var userCounts []struct {
		TargetGroupID int64 `gorm:"column:target_group_id"`
		Count         int64 `gorm:"column:count"`
	}
	db.DB.WithContext(ctx).Model(&model.AclGroupUserPermission{}).
		Select("target_group_id, COUNT(*) as count").
		Group("target_group_id").Find(&userCounts)

	userCountMap := make(map[int64]int64)
	for _, uc := range userCounts {
		userCountMap[uc.TargetGroupID] = uc.Count
	}

	var groupCounts []struct {
		TargetGroupID int64 `gorm:"column:target_group_id"`
		Count         int64 `gorm:"column:count"`
	}
	db.DB.WithContext(ctx).Model(&model.AclGroupGroupPermission{}).
		Select("target_group_id, COUNT(*) as count").
		Group("target_group_id").Find(&groupCounts)

	groupCountMap := make(map[int64]int64)
	for _, gc := range groupCounts {
		groupCountMap[gc.TargetGroupID] = gc.Count
	}

	result := make([]GroupACLListItem, len(groups))
	for i, group := range groups {
		result[i] = GroupACLListItem{
			ID:          group.ID,
			Name:        group.Name,
			Alias:       group.Alias,
			MemberCount: memberCountMap[group.ID],
			UserCount:   userCountMap[group.ID],
			GroupCount:  groupCountMap[group.ID],
			CreatedAt:   group.CreatedAt,
		}
	}

	c.JSON(http.StatusOK, NewPagedResponse(result, total, page, size))
}

// GroupACLDetail 分组授权详情
type GroupACLDetail struct {
	ID     int64               `json:"id"`
	Name   string              `json:"name"`
	Alias  string              `json:"alias"`
	Users  []ACLPermissionItem `json:"users"`
	Groups []ACLPermissionItem `json:"groups"`
}

// GetGroupACL 获取分组授权详情
func (a *ACLAPI) GetGroupACL(c *gin.Context) {
	ctx := c.Request.Context()
	targetGroupID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	var targetGroup model.Group
	if err := db.DB.WithContext(ctx).First(&targetGroup, targetGroupID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("分组不存在"))
		return
	}

	// 查询已授权用户
	var userPerms []model.AclGroupUserPermission
	db.DB.WithContext(ctx).Preload("User").Where("target_group_id = ?", targetGroupID).Find(&userPerms)

	users := make([]ACLPermissionItem, 0, len(userPerms))
	for _, p := range userPerms {
		if p.User != nil {
			users = append(users, ACLPermissionItem{
				ID:        p.User.ID,
				Name:      p.User.Name,
				Alias:     p.User.Alias,
				GrantedAt: p.GrantedAt,
			})
		}
	}

	// 查询已授权分组
	var groupPerms []model.AclGroupGroupPermission
	db.DB.WithContext(ctx).Preload("GrantedGroup").Where("target_group_id = ?", targetGroupID).Find(&groupPerms)

	groups := make([]ACLPermissionItem, 0, len(groupPerms))
	for _, p := range groupPerms {
		if p.GrantedGroup != nil {
			groups = append(groups, ACLPermissionItem{
				ID:        uint64(p.GrantedGroup.ID),
				Name:      p.GrantedGroup.Name,
				Alias:     p.GrantedGroup.Alias,
				GrantedAt: p.GrantedAt,
			})
		}
	}

	result := GroupACLDetail{
		ID:     targetGroup.ID,
		Name:   targetGroup.Name,
		Alias:  targetGroup.Alias,
		Users:  users,
		Groups: groups,
	}

	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

// AddGroupACLUsers 添加分组授权（用户级别）
func (a *ACLAPI) AddGroupACLUsers(c *gin.Context) {
	a.writeACL(c, "group", false, false)
}

// AddGroupACLGroups 添加分组授权（分组级别）
func (a *ACLAPI) AddGroupACLGroups(c *gin.Context) {
	a.writeACL(c, "group", true, false)
}

// RemoveGroupACLUser 撤销分组授权（用户级别）
func (a *ACLAPI) RemoveGroupACLUser(c *gin.Context) {
	a.writeACL(c, "group", false, true)
}

// RemoveGroupACLGroup 撤销分组授权（分组级别）
func (a *ACLAPI) RemoveGroupACLGroup(c *gin.Context) {
	a.writeACL(c, "group", true, true)
}

// ========== SSH 授权 ==========

// SSHACLListItem SSH 授权列表项
type SSHACLListItem struct {
	ID         uint64    `json:"id"`
	Name       string    `json:"name"`
	Alias      string    `json:"alias"`
	Role       string    `json:"role"`
	SSHEnabled bool      `json:"ssh_enabled"`
	UserCount  int64     `json:"user_count"`
	GroupCount int64     `json:"group_count"`
	CreatedAt  time.Time `json:"created_at"`
}

// ListSSHACL 获取 SSH 授权列表（仅启用 SSH 的 Agent）
func (a *ACLAPI) ListSSHACL(c *gin.Context) {
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

	// 查询启用 SSH 的 Agent 用户 + 所有 Client 用户
	query := db.DB.WithContext(ctx).Model(&model.User{}).Where(
		"(role = ? AND ssh_enabled = ?) OR role = ?",
		model.UserRoleAgent, true, model.UserRoleClient,
	)
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
	db.DB.WithContext(ctx).Model(&model.AclSSHUserPermission{}).
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
	db.DB.WithContext(ctx).Model(&model.AclSSHGroupPermission{}).
		Select("target_user_id, COUNT(*) as count").
		Group("target_user_id").Find(&groupCounts)

	groupCountMap := make(map[uint64]int64)
	for _, gc := range groupCounts {
		groupCountMap[gc.TargetUserID] = gc.Count
	}

	result := make([]SSHACLListItem, len(users))
	for i, user := range users {
		result[i] = SSHACLListItem{
			ID:         user.ID,
			Name:       user.Name,
			Alias:      user.Alias,
			Role:       string(user.Role),
			SSHEnabled: user.SSHEnabled,
			UserCount:  userCountMap[user.ID],
			GroupCount: groupCountMap[user.ID],
			CreatedAt:  user.CreatedAt,
		}
	}

	c.JSON(http.StatusOK, NewPagedResponse(result, total, page, size))
}

// SSHACLPermissionItem SSH 授权项
type SSHACLPermissionItem struct {
	ID        uint64    `json:"id"`
	Name      string    `json:"name"`
	Alias     string    `json:"alias"`
	SSHUsers  []string  `json:"ssh_users"`
	Enabled   bool      `json:"enabled"`
	GrantedAt time.Time `json:"granted_at"`
}

// SSHACLDetail SSH 授权详情
type SSHACLDetail struct {
	ID         uint64                 `json:"id"`
	Name       string                 `json:"name"`
	Alias      string                 `json:"alias"`
	Role       string                 `json:"role"`
	SSHEnabled bool                   `json:"ssh_enabled"`
	Users      []SSHACLPermissionItem `json:"users"`
	Groups     []SSHACLPermissionItem `json:"groups"`
}

// GetSSHACL 获取 SSH 授权详情
func (a *ACLAPI) GetSSHACL(c *gin.Context) {
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
	var userPerms []model.AclSSHUserPermission
	db.DB.WithContext(ctx).Preload("User").Where("target_user_id = ?", targetUserID).Find(&userPerms)

	users := make([]SSHACLPermissionItem, 0, len(userPerms))
	for _, p := range userPerms {
		if p.User != nil {
			users = append(users, SSHACLPermissionItem{
				ID:        p.User.ID,
				Name:      p.User.Name,
				Alias:     p.User.Alias,
				SSHUsers:  parseSSHUsers(p.SSHUsers),
				Enabled:   p.Enabled,
				GrantedAt: p.GrantedAt,
			})
		}
	}

	// 查询已授权分组
	var groupPerms []model.AclSSHGroupPermission
	db.DB.WithContext(ctx).Preload("Group").Where("target_user_id = ?", targetUserID).Find(&groupPerms)

	groups := make([]SSHACLPermissionItem, 0, len(groupPerms))
	for _, p := range groupPerms {
		if p.Group != nil {
			groups = append(groups, SSHACLPermissionItem{
				ID:        uint64(p.Group.ID),
				Name:      p.Group.Name,
				Alias:     p.Group.Alias,
				SSHUsers:  parseSSHUsers(p.SSHUsers),
				Enabled:   p.Enabled,
				GrantedAt: p.GrantedAt,
			})
		}
	}

	result := SSHACLDetail{
		ID:         targetUser.ID,
		Name:       targetUser.Name,
		Alias:      targetUser.Alias,
		Role:       string(targetUser.Role),
		SSHEnabled: targetUser.SSHEnabled,
		Users:      users,
		Groups:     groups,
	}

	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

// AddSSHACLUsersRequest 添加 SSH 用户授权请求
type AddSSHACLUsersRequest struct {
	UserIDs  []uint64 `json:"user_ids" binding:"required,min=1"`
	SSHUsers []string `json:"ssh_users" binding:"required,min=1"` // Linux 用户名列表
}

// AddSSHACLUsers 添加 SSH 用户授权
func (a *ACLAPI) AddSSHACLUsers(c *gin.Context) {
	a.writeACL(c, "ssh", false, false)
}

// AddSSHACLGroupsRequest 添加 SSH 分组授权请求
type AddSSHACLGroupsRequest struct {
	GroupIDs []int64  `json:"group_ids" binding:"required,min=1"`
	SSHUsers []string `json:"ssh_users" binding:"required,min=1"`
}

// AddSSHACLGroups 添加 SSH 分组授权
func (a *ACLAPI) AddSSHACLGroups(c *gin.Context) {
	a.writeACL(c, "ssh", true, false)
}

// RemoveSSHACLUser 撤销 SSH 用户授权
func (a *ACLAPI) RemoveSSHACLUser(c *gin.Context) {
	a.writeACL(c, "ssh", false, true)
}

// RemoveSSHACLGroup 撤销 SSH 分组授权
func (a *ACLAPI) RemoveSSHACLGroup(c *gin.Context) {
	a.writeACL(c, "ssh", true, true)
}

// parseSSHUsers 解析 SSH 用户列表 JSON
func parseSSHUsers(jsonStr string) []string {
	if jsonStr == "" {
		return []string{}
	}
	var users []string
	// 简单解析 JSON 数组
	// 格式: ["root", "admin"]
	if len(jsonStr) > 2 && jsonStr[0] == '[' && jsonStr[len(jsonStr)-1] == ']' {
		// 去掉括号和引号，按逗号分割
		inner := jsonStr[1 : len(jsonStr)-1]
		if inner != "" {
			for _, part := range splitAndTrim(inner, ',') {
				if len(part) > 2 && part[0] == '"' && part[len(part)-1] == '"' {
					users = append(users, part[1:len(part)-1])
				}
			}
		}
	}
	return users
}

// formatSSHUsers 格式化 SSH 用户列表为 JSON
func formatSSHUsers(users []string) string {
	if len(users) == 0 {
		return "[]"
	}
	result := "["
	for i, u := range users {
		if i > 0 {
			result += ","
		}
		result += "\"" + u + "\""
	}
	result += "]"
	return result
}

// splitAndTrim 分割字符串并去除空白
func splitAndTrim(s string, sep rune) []string {
	var result []string
	var current string
	inQuote := false
	for _, ch := range s {
		if ch == '"' {
			inQuote = !inQuote
			current += string(ch)
		} else if ch == sep && !inQuote {
			if current != "" {
				result = append(result, current)
			}
			current = ""
		} else {
			current += string(ch)
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}
