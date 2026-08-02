package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

// TenantGovernanceAPI exposes the unified management identities assigned to
// the current Tenant. It does not read or create business TenantMemberships.
type TenantGovernanceAPI struct{}

func NewTenantGovernanceAPI() *TenantGovernanceAPI { return &TenantGovernanceAPI{} }

type tenantManagementMembershipItem struct {
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

func (a *TenantGovernanceAPI) ListManagementMemberships(c *gin.Context) {
	_, tenantID, ok := currentTenantAuthorization(c)
	if !ok {
		return
	}
	page, size := pageParams(c)
	now := time.Now()
	query := db.DB.WithContext(c.Request.Context()).Table("user_tenant_management_membership AS membership").
		Joins("JOIN user ON user.id = membership.user_id").
		Joins("LEFT JOIN user_identity_profile AS profile ON profile.user_id = membership.user_id").
		Where("membership.tenant_id = ?", tenantID)
	if search := strings.TrimSpace(c.Query("search")); search != "" {
		pattern := "%" + search + "%"
		query = query.Where("user.name LIKE ? OR user.alias LIKE ? OR profile.display_name LIKE ?", pattern, pattern, pattern)
	}
	if role := strings.TrimSpace(c.Query("role")); role != "" {
		if role != string(model.TenantManagementRoleAdmin) && role != string(model.TenantManagementRoleSecurityAuditor) && role != string(model.TenantManagementRoleViewer) {
			codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "Tenant 管理角色筛选无效")
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
		codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "Tenant 管理状态筛选无效")
		return
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		codedError(c, http.StatusInternalServerError, "TENANT_MANAGEMENT_MEMBERSHIP_QUERY_FAILED", "查询 Tenant 管理员失败")
		return
	}
	var items []tenantManagementMembershipItem
	if err := query.Select(`membership.id, membership.user_id, user.name AS username,
		COALESCE(NULLIF(profile.display_name, ''), NULLIF(user.alias, ''), user.name) AS display_name,
		user.enabled AS user_enabled, membership.role, membership.enabled, membership.valid_from,
		membership.expires_at, membership.permission_revision, membership.reason, membership.row_version,
		membership.created_at, membership.updated_at`).
		Order("membership.updated_at DESC").Order("membership.id ASC").
		Offset((page - 1) * size).Limit(size).Scan(&items).Error; err != nil {
		codedError(c, http.StatusInternalServerError, "TENANT_MANAGEMENT_MEMBERSHIP_QUERY_FAILED", "查询 Tenant 管理员失败")
		return
	}
	c.JSON(http.StatusOK, NewPagedResponse(items, total, page, size))
}
