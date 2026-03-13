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

// GroupAPINew 统一分组管理 API
type GroupAPINew struct {
	config  *config.ServerConfig
	aclSync *headscale.ACLSyncService
}

// NewGroupAPINew 创建 GroupAPINew
func NewGroupAPINew(cfg *config.ServerConfig) *GroupAPINew {
	api := &GroupAPINew{config: cfg}

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

// GroupListItem 分组列表项
type GroupListItem struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Alias       string    `json:"alias"`
	Description string    `json:"description"`
	MemberCount int64     `json:"member_count"`
	CreatedAt   time.Time `json:"created_at"`
}

// List 获取分组列表
func (a *GroupAPINew) List(c *gin.Context) {
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

	// 查询每个分组的成员数量
	var memberCounts []struct {
		GroupID int64 `gorm:"column:group_id"`
		Count   int64 `gorm:"column:count"`
	}
	db.DB.WithContext(ctx).Model(&model.GroupMember{}).
		Select("group_id, COUNT(*) as count").
		Group("group_id").Find(&memberCounts)

	countMap := make(map[int64]int64)
	for _, mc := range memberCounts {
		countMap[mc.GroupID] = mc.Count
	}

	result := make([]GroupListItem, len(groups))
	for i, g := range groups {
		result[i] = GroupListItem{
			ID:          g.ID,
			Name:        g.Name,
			Alias:       g.Alias,
			Description: g.Description,
			MemberCount: countMap[g.ID],
			CreatedAt:   g.CreatedAt,
		}
	}

	c.JSON(http.StatusOK, NewPagedResponse(result, total, page, size))
}

// GroupDetail 分组详情
type GroupDetail struct {
	ID          int64             `json:"id"`
	Name        string            `json:"name"`
	Alias       string            `json:"alias"`
	Description string            `json:"description"`
	MemberCount int64             `json:"member_count"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	Members     []GroupMemberItem `json:"members"`
}

// GroupMemberItem 分组成员项
type GroupMemberItem struct {
	ID       uint64    `json:"id"`
	Name     string    `json:"name"`
	Alias    string    `json:"alias"`
	Role     string    `json:"role"`
	JoinedAt time.Time `json:"joined_at"`
}

// Get 获取分组详情
func (a *GroupAPINew) Get(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	var group model.Group
	if err := db.DB.WithContext(ctx).First(&group, id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("分组不存在"))
		return
	}

	// 查询成员列表
	var members []model.GroupMember
	db.DB.WithContext(ctx).Preload("User").Where("group_id = ?", id).Find(&members)

	memberItems := make([]GroupMemberItem, 0, len(members))
	for _, m := range members {
		if m.User != nil {
			memberItems = append(memberItems, GroupMemberItem{
				ID:       m.User.ID,
				Name:     m.User.Name,
				Alias:    m.User.Alias,
				Role:     string(m.User.Role),
				JoinedAt: m.CreatedAt,
			})
		}
	}

	result := GroupDetail{
		ID:          group.ID,
		Name:        group.Name,
		Alias:       group.Alias,
		Description: group.Description,
		MemberCount: int64(len(memberItems)),
		CreatedAt:   group.CreatedAt,
		UpdatedAt:   group.UpdatedAt,
		Members:     memberItems,
	}

	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

// CreateGroupRequest 创建分组请求
type CreateGroupRequest struct {
	Name        string `json:"name" binding:"required"`
	Alias       string `json:"alias"`
	Description string `json:"description"`
}

// Create 创建分组
func (a *GroupAPINew) Create(c *gin.Context) {
	ctx := c.Request.Context()
	var req CreateGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	// 检查名称是否已存在
	var existing model.Group
	if err := db.DB.WithContext(ctx).Where("name = ?", req.Name).First(&existing).Error; err == nil {
		c.JSON(http.StatusConflict, NewErrorResponse("分组名称已存在"))
		return
	}

	group := &model.Group{
		Name:        req.Name,
		Alias:       req.Alias,
		Description: req.Description,
	}

	if err := db.DB.WithContext(ctx).Create(group).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("创建失败"))
		return
	}

	logger.Infof("创建分组: id=%d, name=%s", group.ID, group.Name)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("创建成功", group))
}

// UpdateGroupRequest 更新分组请求
type UpdateGroupRequest struct {
	Alias       string `json:"alias"`
	Description string `json:"description"`
}

// Update 更新分组
func (a *GroupAPINew) Update(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	var req UpdateGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	var group model.Group
	if err := db.DB.WithContext(ctx).First(&group, id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("分组不存在"))
		return
	}

	group.Alias = req.Alias
	group.Description = req.Description

	if err := db.DB.WithContext(ctx).Save(&group).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("更新失败"))
		return
	}

	logger.Infof("更新分组: id=%d", id)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("更新成功", nil))
}

// Delete 删除分组
func (a *GroupAPINew) Delete(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	var group model.Group
	if err := db.DB.WithContext(ctx).First(&group, id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("分组不存在"))
		return
	}

	// 删除分组相关的授权
	db.DB.WithContext(ctx).Where("group_id = ?", id).Delete(&model.AclServiceGroupPermission{})
	db.DB.WithContext(ctx).Where("group_id = ?", id).Delete(&model.AclUserGroupPermission{})
	db.DB.WithContext(ctx).Where("group_id = ?", id).Delete(&model.AclGroupGroupPermission{})
	db.DB.WithContext(ctx).Where("target_group_id = ?", id).Delete(&model.AclGroupUserPermission{})
	db.DB.WithContext(ctx).Where("target_group_id = ?", id).Delete(&model.AclGroupGroupPermission{})
	db.DB.WithContext(ctx).Where("group_id = ?", id).Delete(&model.AclSSHGroupPermission{})

	// 删除分组成员
	db.DB.WithContext(ctx).Where("group_id = ?", id).Delete(&model.GroupMember{})

	// 删除分组
	if err := db.DB.WithContext(ctx).Delete(&group).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("删除失败"))
		return
	}

	// 同步 ACL
	if a.aclSync != nil {
		go func() {
			if err := a.aclSync.FullSync(nil); err != nil {
				logger.Warnf("同步 ACL 失败: %v", err)
			}
		}()
	}

	logger.Infof("删除分组: id=%d, name=%s", id, group.Name)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("删除成功", nil))
}

// GetMembers 获取分组成员列表
func (a *GroupAPINew) GetMembers(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	var group model.Group
	if err := db.DB.WithContext(ctx).First(&group, id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("分组不存在"))
		return
	}

	var members []model.GroupMember
	if err := db.DB.WithContext(ctx).Preload("User").Where("group_id = ?", id).Find(&members).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询失败"))
		return
	}

	c.JSON(http.StatusOK, NewSuccessResponse(members))
}

// AddMemberRequest 添加成员请求
type AddMemberRequest struct {
	UserIDs []uint64 `json:"user_ids" binding:"required,min=1"`
}

// AddMembers 添加分组成员（支持批量）
func (a *GroupAPINew) AddMembers(c *gin.Context) {
	ctx := c.Request.Context()
	groupID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的分组 ID"))
		return
	}

	var req AddMemberRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	// 验证分组存在
	var group model.Group
	if err := db.DB.WithContext(ctx).First(&group, groupID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("分组不存在"))
		return
	}

	// 批量添加成员
	for _, userID := range req.UserIDs {
		// 验证用户存在
		var user model.User
		if err := db.DB.WithContext(ctx).First(&user, userID).Error; err != nil {
			continue // 用户不存在，跳过
		}

		// 检查是否已是成员
		var existing model.GroupMember
		if err := db.DB.WithContext(ctx).Where("group_id = ? AND user_id = ?", groupID, userID).First(&existing).Error; err == nil {
			continue // 已是成员，跳过
		}

		member := &model.GroupMember{
			GroupID: groupID,
			UserID:  userID,
		}
		db.DB.WithContext(ctx).Create(member)
	}

	// 同步 ACL
	if a.aclSync != nil {
		go func() {
			if err := a.aclSync.FullSync(nil); err != nil {
				logger.Warnf("同步 ACL 失败: %v", err)
			}
		}()
	}

	logger.Infof("添加分组成员: group_id=%d, user_ids=%v", groupID, req.UserIDs)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("添加成功", nil))
}

// RemoveMember 移除分组成员
func (a *GroupAPINew) RemoveMember(c *gin.Context) {
	ctx := c.Request.Context()
	groupID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的分组 ID"))
		return
	}

	userID, err := strconv.ParseUint(c.Param("uid"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的用户 ID"))
		return
	}

	result := db.DB.WithContext(ctx).Where("group_id = ? AND user_id = ?", groupID, userID).Delete(&model.GroupMember{})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("删除失败"))
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, NewErrorResponse("成员不存在"))
		return
	}

	// 同步 ACL
	if a.aclSync != nil {
		go func() {
			if err := a.aclSync.FullSync(nil); err != nil {
				logger.Warnf("同步 ACL 失败: %v", err)
			}
		}()
	}

	logger.Infof("移除分组成员: group_id=%d, user_id=%d", groupID, userID)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("移除成功", nil))
}
