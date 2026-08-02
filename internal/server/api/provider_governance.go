package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

// ProviderGovernanceAPI exposes read-only governance data for the Provider
// selected by the management authorization middleware.
type ProviderGovernanceAPI struct{}

func NewProviderGovernanceAPI() *ProviderGovernanceAPI { return &ProviderGovernanceAPI{} }

type providerMembershipItem struct {
	ID                 string     `json:"id"`
	UserID             uint64     `json:"user_id"`
	Username           string     `json:"username"`
	DisplayName        string     `json:"display_name"`
	UserEnabled        bool       `json:"user_enabled"`
	Role               string     `json:"role"`
	Enabled            bool       `json:"enabled"`
	ValidFrom          time.Time  `json:"valid_from"`
	ExpiresAt          *time.Time `json:"expires_at,omitempty"`
	PermissionRevision int64      `json:"permission_revision"`
	Reason             string     `json:"reason"`
	RowVersion         int64      `json:"row_version"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

type providerAuditItem struct {
	ID                  int64     `json:"id"`
	ActorAdminID        int64     `json:"actor_admin_id"`
	ActorUsername       string    `json:"actor_username"`
	ActorUserID         uint64    `json:"actor_user_id"`
	EffectiveUserID     uint64    `json:"effective_user_id"`
	SimulationSessionID string    `json:"simulation_session_id"`
	RequiredPermission  string    `json:"required_permission"`
	PermissionRevision  int64     `json:"permission_revision"`
	ActionType          string    `json:"action_type"`
	TargetType          string    `json:"target_type"`
	TargetID            string    `json:"target_id"`
	TargetName          string    `json:"target_name"`
	RequestID           string    `json:"request_id"`
	SourceIP            string    `json:"source_ip"`
	Detail              string    `json:"detail,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
}

func (a *ProviderGovernanceAPI) ListMemberships(c *gin.Context) {
	authorization, ok := currentManagementAuthorization(c)
	if !ok {
		return
	}
	page, size := pageParams(c)
	now := time.Now()
	query := db.DB.WithContext(c.Request.Context()).Table("admin_provider_membership AS membership").
		Joins("JOIN user ON user.id = membership.user_id").
		Joins("LEFT JOIN user_identity_profile AS profile ON profile.user_id = membership.user_id").
		Where("membership.provider_id = ?", authorization.ScopeID)
	if search := strings.TrimSpace(c.Query("search")); search != "" {
		pattern := "%" + search + "%"
		query = query.Where("user.name LIKE ? OR user.alias LIKE ? OR profile.display_name LIKE ?", pattern, pattern, pattern)
	}
	if role := strings.TrimSpace(c.Query("role")); role != "" {
		if role != string(model.ProviderManagementRoleAdmin) && role != string(model.ProviderManagementRoleOperator) && role != string(model.ProviderManagementRoleViewer) {
			codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "Provider 管理角色筛选无效")
			return
		}
		query = query.Where("membership.role = ?", role)
	}
	switch state := strings.TrimSpace(c.Query("state")); state {
	case "":
	case "active":
		query = query.Where("membership.enabled = ? AND user.enabled = ? AND membership.valid_from <= ? AND (membership.expires_at IS NULL OR membership.expires_at > ?)", true, true, now, now)
	case "disabled":
		query = query.Where("membership.enabled = ? OR user.enabled = ?", false, false)
	case "scheduled":
		query = query.Where("membership.enabled = ? AND user.enabled = ? AND membership.valid_from > ?", true, true, now)
	case "expired":
		query = query.Where("membership.enabled = ? AND user.enabled = ? AND membership.expires_at IS NOT NULL AND membership.expires_at <= ?", true, true, now)
	default:
		codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "Provider 管理状态筛选无效")
		return
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		codedError(c, http.StatusInternalServerError, "PROVIDER_MEMBERSHIP_QUERY_FAILED", "查询 Provider 管理员失败")
		return
	}
	var items []providerMembershipItem
	if err := query.Select(`membership.id, membership.user_id, user.name AS username,
		COALESCE(NULLIF(profile.display_name, ''), NULLIF(user.alias, ''), user.name) AS display_name,
		user.enabled AS user_enabled, membership.role, membership.enabled, membership.valid_from,
		membership.expires_at, membership.permission_revision, membership.reason, membership.row_version,
		membership.created_at, membership.updated_at`).
		Order("membership.updated_at DESC").Order("membership.id ASC").
		Offset((page - 1) * size).Limit(size).Scan(&items).Error; err != nil {
		codedError(c, http.StatusInternalServerError, "PROVIDER_MEMBERSHIP_QUERY_FAILED", "查询 Provider 管理员失败")
		return
	}
	c.JSON(http.StatusOK, NewPagedResponse(items, total, page, size))
}

func (a *ProviderGovernanceAPI) ListAuditLogs(c *gin.Context) {
	authorization, ok := currentManagementAuthorization(c)
	if !ok {
		return
	}
	page, size := pageParams(c)
	query := db.DB.WithContext(c.Request.Context()).Model(&model.AuditLog{}).
		Where("scope_type = ? AND scope_id = ?", model.ManagementScopeProvider, authorization.ScopeID)
	if actionType := strings.TrimSpace(c.Query("action_type")); actionType != "" {
		query = query.Where("action_type = ?", actionType)
	}
	if search := strings.TrimSpace(c.Query("search")); search != "" {
		pattern := "%" + search + "%"
		query = query.Where("actor_username LIKE ? OR target_name LIKE ? OR target_id LIKE ? OR request_id LIKE ?", pattern, pattern, pattern, pattern)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		codedError(c, http.StatusInternalServerError, "PROVIDER_AUDIT_QUERY_FAILED", "查询 Provider 审计失败")
		return
	}
	var logs []model.AuditLog
	if err := query.Order("created_at DESC").Order("id DESC").Offset((page - 1) * size).Limit(size).Find(&logs).Error; err != nil {
		codedError(c, http.StatusInternalServerError, "PROVIDER_AUDIT_QUERY_FAILED", "查询 Provider 审计失败")
		return
	}
	items := make([]providerAuditItem, 0, len(logs))
	for _, log := range logs {
		items = append(items, providerAuditItem{
			ID: log.ID, ActorAdminID: log.ActorAdminID, ActorUsername: log.ActorUsername,
			ActorUserID: log.ActorUserID, EffectiveUserID: log.EffectiveUserID,
			SimulationSessionID: log.SimulationSessionID, RequiredPermission: log.RequiredPermission,
			PermissionRevision: log.PermissionRevision, ActionType: log.ActionType, TargetType: log.TargetType,
			TargetID: log.TargetID, TargetName: log.TargetName, RequestID: log.RequestID,
			SourceIP: log.SourceIP, Detail: log.Detail, CreatedAt: log.CreatedAt,
		})
	}
	c.JSON(http.StatusOK, NewPagedResponse(items, total, page, size))
}
