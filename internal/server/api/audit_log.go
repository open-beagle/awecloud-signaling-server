package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

// AuditLogAPI 审计日志 API
type AuditLogAPI struct{}

// NewAuditLogAPI 创建 AuditLogAPI
func NewAuditLogAPI() *AuditLogAPI {
	return &AuditLogAPI{}
}

// AuditLogItem 审计日志项
type AuditLogItem struct {
	ID         int64     `json:"id"`
	ActionType string    `json:"action_type"`
	ActorName  string    `json:"actor_name"`
	TargetName string    `json:"target_name"`
	Detail     string    `json:"detail"`
	CreatedAt  time.Time `json:"created_at"`
}

// QueryAuditLogs 查询审计日志
func (a *AuditLogAPI) QueryAuditLogs(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	actionType := c.Query("action_type")
	userID, _ := strconv.ParseInt(c.Query("user_id"), 10, 64)
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")

	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}

	query := db.DB.Model(&model.AuditLog{})

	// 按操作类型筛选
	if actionType != "" {
		query = query.Where("action_type = ?", actionType)
	}

	// 按操作者 ID 筛选
	if userID > 0 {
		query = query.Where("user_id = ?", userID)
	}

	// 按日期范围筛选
	if startDate != "" {
		t, err := time.Parse("2006-01-02", startDate)
		if err == nil {
			query = query.Where("created_at >= ?", t)
		}
	}
	if endDate != "" {
		t, err := time.Parse("2006-01-02", endDate)
		if err == nil {
			// 结束日期加一天，包含当天
			query = query.Where("created_at < ?", t.AddDate(0, 0, 1))
		}
	}

	var total int64
	query.Count(&total)

	var logs []model.AuditLog
	offset := (page - 1) * size
	if err := query.Order("created_at DESC").Offset(offset).Limit(size).Find(&logs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询失败"))
		return
	}

	// 查询管理员名称（目前只支持 admin 类型）
	userIDs := make([]int64, 0, len(logs))
	for _, log := range logs {
		if log.UserID > 0 && log.UserType == "admin" {
			userIDs = append(userIDs, log.UserID)
		}
	}

	adminMap := make(map[int64]string)
	if len(userIDs) > 0 {
		var admins []model.Admin
		db.DB.Where("id IN ?", userIDs).Find(&admins)
		for _, admin := range admins {
			adminMap[admin.ID] = admin.Username
		}
	}

	result := make([]AuditLogItem, len(logs))
	for i, log := range logs {
		actorName := ""
		if log.UserType == "admin" {
			actorName = adminMap[log.UserID]
		}
		// 未来可扩展 desktop 类型的用户名查询

		result[i] = AuditLogItem{
			ID:         log.ID,
			ActionType: log.ActionType,
			ActorName:  actorName,
			TargetName: log.TargetName,
			Detail:     log.Detail,
			CreatedAt:  log.CreatedAt,
		}
	}

	c.JSON(http.StatusOK, NewPagedResponse(result, total, page, size))
}

// GetActionTypes 获取操作类型列表
func (a *AuditLogAPI) GetActionTypes(c *gin.Context) {
	actionTypes := []map[string]string{
		{"value": model.ActionCreateAgent, "label": "创建代理"},
		{"value": model.ActionDeleteAgent, "label": "删除代理"},
		{"value": model.ActionCreateService, "label": "创建服务"},
		{"value": model.ActionDeleteService, "label": "删除服务"},
		{"value": model.ActionGrantDesktop, "label": "桌面授权"},
		{"value": model.ActionRevokeDesktop, "label": "撤销桌面授权"},
		{"value": model.ActionGrantAgent, "label": "代理授权"},
		{"value": model.ActionRevokeAgent, "label": "撤销代理授权"},
		{"value": model.ActionCreatePortForward, "label": "创建端口访问"},
		{"value": model.ActionDeletePortForward, "label": "删除端口访问"},
		{"value": model.ActionCreateClientGroup, "label": "创建用户分组"},
		{"value": model.ActionDeleteClientGroup, "label": "删除用户分组"},
		{"value": model.ActionCreateAgentGroup, "label": "创建代理分组"},
		{"value": model.ActionDeleteAgentGroup, "label": "删除代理分组"},
	}

	c.JSON(http.StatusOK, NewSuccessResponse(actionTypes))
}

// AdminOption 管理员选项
type AdminOption struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

// GetUsers 获取操作用户列表（用于筛选）
func (a *AuditLogAPI) GetUsers(c *gin.Context) {
	var admins []model.Admin
	if err := db.DB.Select("id, username").Find(&admins).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询失败"))
		return
	}

	result := make([]AdminOption, len(admins))
	for i, admin := range admins {
		result[i] = AdminOption{
			ID:       admin.ID,
			Username: admin.Username,
		}
	}

	c.JSON(http.StatusOK, NewSuccessResponse(result))
}
