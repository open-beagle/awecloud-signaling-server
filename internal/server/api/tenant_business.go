package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

type TenantBusinessAPI struct{}

func NewTenantBusinessAPI() *TenantBusinessAPI { return &TenantBusinessAPI{} }

type TenantMemberDeviceItem struct {
	NodeID        uint64     `json:"node_id"`
	UserID        uint64     `json:"user_id"`
	UserName      string     `json:"user_name"`
	UserAlias     string     `json:"user_alias,omitempty"`
	DeviceName    string     `json:"device_name"`
	Hostname      string     `json:"hostname,omitempty"`
	IP            string     `json:"ip,omitempty"`
	Version       string     `json:"version,omitempty"`
	LastHeartbeat *time.Time `json:"last_heartbeat,omitempty"`
	Online        bool       `json:"online"`
}

type TenantAuditItem struct {
	ID                 int64     `json:"id"`
	ActorAdminID       int64     `json:"actor_admin_id"`
	ActorUsername      string    `json:"actor_username"`
	PlatformRole       string    `json:"platform_role"`
	TenantRole         string    `json:"tenant_role"`
	RequiredPermission string    `json:"required_permission"`
	PermissionRevision int64     `json:"permission_revision"`
	ActionType         string    `json:"action_type"`
	TargetType         string    `json:"target_type"`
	TargetID           string    `json:"target_id"`
	TargetName         string    `json:"target_name"`
	RequestID          string    `json:"request_id"`
	SourceIP           string    `json:"source_ip"`
	Detail             string    `json:"detail,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
}

func (a *TenantBusinessAPI) ListMemberDevices(c *gin.Context) {
	tenantID := strings.TrimSpace(c.Param("id"))
	if !requireTenantPermission(c, tenantID, PermissionTenantDevicesRead) {
		return
	}
	page, size := pageParams(c)
	now := time.Now()
	base := db.DB.WithContext(c.Request.Context()).Table("node AS n").
		Joins("JOIN user AS u ON u.id = n.user_id").
		Joins("JOIN tenant_membership AS tm ON tm.user_id = u.id").
		Where("tm.tenant_id = ? AND tm.enabled = ? AND u.enabled = ? AND n.type = ? AND (tm.expires_at IS NULL OR tm.expires_at > ?)", tenantID, true, true, model.NodeTypeDesktop, now)
	if search := strings.TrimSpace(c.Query("search")); search != "" {
		pattern := "%" + search + "%"
		base = base.Where("u.name LIKE ? OR u.alias LIKE ? OR n.name LIKE ? OR n.hostname LIKE ?", pattern, pattern, pattern, pattern)
	}
	var total int64
	if err := base.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询成员设备失败"))
		return
	}
	type row struct {
		NodeID        uint64
		UserID        uint64
		UserName      string
		UserAlias     string
		DeviceName    string
		Hostname      string
		IP            string
		Version       string
		LastHeartbeat *time.Time
	}
	var rows []row
	if err := base.Select("n.id AS node_id, u.id AS user_id, u.name AS user_name, u.alias AS user_alias, n.name AS device_name, n.hostname, n.ip, n.version, n.last_heartbeat").
		Order(gorm.Expr("CASE WHEN n.last_heartbeat > ? THEN 0 ELSE 1 END ASC", now.Add(-2*time.Minute))).
		Order("n.last_heartbeat DESC").
		Order("n.id ASC").
		Offset((page - 1) * size).Limit(size).Scan(&rows).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询成员设备失败"))
		return
	}
	items := make([]TenantMemberDeviceItem, 0, len(rows))
	for _, value := range rows {
		items = append(items, TenantMemberDeviceItem{
			NodeID: value.NodeID, UserID: value.UserID, UserName: value.UserName, UserAlias: value.UserAlias,
			DeviceName: value.DeviceName, Hostname: value.Hostname, IP: value.IP, Version: value.Version,
			LastHeartbeat: value.LastHeartbeat, Online: value.LastHeartbeat != nil && value.LastHeartbeat.After(now.Add(-2*time.Minute)),
		})
	}
	c.JSON(http.StatusOK, NewPagedResponse(items, total, page, size))
}

func (a *TenantBusinessAPI) ListAuditLogs(c *gin.Context) {
	tenantID := strings.TrimSpace(c.Param("id"))
	if !requireTenantPermission(c, tenantID, PermissionTenantAuditRead) {
		return
	}
	page, size := pageParams(c)
	query := db.DB.WithContext(c.Request.Context()).Model(&model.AuditLog{}).Where("tenant_id = ?", tenantID)
	if actionType := strings.TrimSpace(c.Query("action_type")); actionType != "" {
		query = query.Where("action_type = ?", actionType)
	}
	if search := strings.TrimSpace(c.Query("search")); search != "" {
		pattern := "%" + search + "%"
		query = query.Where("actor_username LIKE ? OR target_name LIKE ? OR target_id LIKE ? OR request_id LIKE ?", pattern, pattern, pattern, pattern)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询租户审计失败"))
		return
	}
	var logs []model.AuditLog
	if err := query.Order("created_at DESC").Offset((page - 1) * size).Limit(size).Find(&logs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询租户审计失败"))
		return
	}
	items := make([]TenantAuditItem, 0, len(logs))
	for _, log := range logs {
		items = append(items, TenantAuditItem{
			ID: log.ID, ActorAdminID: log.ActorAdminID, ActorUsername: log.ActorUsername, PlatformRole: log.PlatformRole,
			TenantRole: log.TenantRole, RequiredPermission: log.RequiredPermission, PermissionRevision: log.PermissionRevision,
			ActionType: log.ActionType, TargetType: log.TargetType, TargetID: log.TargetID, TargetName: log.TargetName,
			RequestID: log.RequestID, SourceIP: log.SourceIP, Detail: log.Detail, CreatedAt: log.CreatedAt,
		})
	}
	c.JSON(http.StatusOK, NewPagedResponse(items, total, page, size))
}
