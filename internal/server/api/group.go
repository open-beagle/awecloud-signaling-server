package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

type GroupAPI struct{}

func NewGroupAPI() *GroupAPI {
	return &GroupAPI{}
}

// CreateGroupRequest 创建组请求
type CreateGroupRequest struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
}

// UpdateGroupRequest 更新组请求
type UpdateGroupRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// AddMemberRequest 添加成员请求
type AddMemberRequest struct {
	ClientID int64  `json:"client_id" binding:"required"`
	Role     string `json:"role"` // 'admin' or 'member', default 'member'
}

// GetGroups 获取所有组
func (a *GroupAPI) GetGroups(c *gin.Context) {
	var groups []model.Group
	if err := db.DB.Find(&groups).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "查询失败",
		})
		return
	}

	// 查询每个组的成员数量
	type GroupWithCount struct {
		model.Group
		MemberCount int64 `json:"member_count"`
	}

	result := make([]GroupWithCount, 0, len(groups))
	for _, group := range groups {
		var count int64
		db.DB.Model(&model.GroupMember{}).Where("group_id = ?", group.ID).Count(&count)
		result = append(result, GroupWithCount{
			Group:       group,
			MemberCount: count,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}

// CreateGroup 创建组
func (a *GroupAPI) CreateGroup(c *gin.Context) {
	var req CreateGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "请求参数错误",
		})
		return
	}

	group := &model.Group{
		Name:        req.Name,
		Description: req.Description,
	}

	if err := db.DB.Create(group).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "创建失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    group,
	})
}

// UpdateGroup 更新组
func (a *GroupAPI) UpdateGroup(c *gin.Context) {
	id := c.Param("id")

	var group model.Group
	if err := db.DB.First(&group, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   "组不存在",
		})
		return
	}

	var req UpdateGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "请求参数错误",
		})
		return
	}

	if req.Name != "" {
		group.Name = req.Name
	}
	if req.Description != "" {
		group.Description = req.Description
	}

	if err := db.DB.Save(&group).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "更新失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "更新成功",
	})
}

// DeleteGroup 删除组
func (a *GroupAPI) DeleteGroup(c *gin.Context) {
	id := c.Param("id")

	if err := db.DB.Delete(&model.Group{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "删除失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "删除成功",
	})
}

// GetGroupMembers 获取组成员
func (a *GroupAPI) GetGroupMembers(c *gin.Context) {
	groupID := c.Param("id")

	var members []model.GroupMember
	if err := db.DB.Preload("Client").Where("group_id = ?", groupID).Find(&members).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "查询失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    members,
	})
}

// AddGroupMember 添加组成员
func (a *GroupAPI) AddGroupMember(c *gin.Context) {
	groupID := c.Param("id")

	var req AddMemberRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "请求参数错误",
		})
		return
	}

	// 检查组是否存在
	var group model.Group
	if err := db.DB.First(&group, groupID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   "组不存在",
		})
		return
	}

	// 检查客户端是否存在
	var client model.Client
	if err := db.DB.First(&client, req.ClientID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   "客户端不存在",
		})
		return
	}

	// 设置默认角色
	role := req.Role
	if role == "" {
		role = "member"
	}

	member := &model.GroupMember{
		GroupID:  group.ID,
		ClientID: req.ClientID,
		Role:     role,
	}

	if err := db.DB.Create(member).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "添加失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "添加成功",
	})
}

// RemoveGroupMember 移除组成员
func (a *GroupAPI) RemoveGroupMember(c *gin.Context) {
	groupID := c.Param("id")
	clientID := c.Param("client_id")

	if err := db.DB.Where("group_id = ? AND client_id = ?", groupID, clientID).
		Delete(&model.GroupMember{}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "移除失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "移除成功",
	})
}
