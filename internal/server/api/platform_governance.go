package api

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

// PlatformGovernanceAPI exposes cross-scope governance directories. These
// reads never grant access to the referenced Provider or Tenant.
type PlatformGovernanceAPI struct{}

func NewPlatformGovernanceAPI() *PlatformGovernanceAPI { return &PlatformGovernanceAPI{} }

type platformOrganizationItem struct {
	ID                        string                    `json:"id"`
	ScopeType                 string                    `json:"scope_type"`
	Key                       string                    `json:"key"`
	Name                      string                    `json:"name"`
	DomainLabel               string                    `json:"domain_label,omitempty"`
	DomainScope               model.ProviderDomainScope `json:"domain_scope,omitempty"`
	Status                    string                    `json:"status"`
	ManagementMembershipCount int64                     `json:"management_membership_count"`
	BusinessMemberCount       int64                     `json:"business_member_count"`
	TechnicalResourceCount    int64                     `json:"technical_resource_count"`
	ResourceCount             int64                     `json:"resource_count"`
	ScopeCount                int64                     `json:"scope_count"`
	Revision                  int64                     `json:"revision"`
	RowVersion                int64                     `json:"row_version"`
	CreatedAt                 time.Time                 `json:"created_at"`
	UpdatedAt                 time.Time                 `json:"updated_at"`
}

func (a *PlatformGovernanceAPI) ListOrganizations(c *gin.Context) {
	page, size := pageParams(c)
	scopeType := strings.TrimSpace(c.Query("scope_type"))
	if scopeType != "" && scopeType != string(model.ManagementScopeProvider) && scopeType != string(model.ManagementScopeTenant) {
		codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "组织类型筛选无效")
		return
	}
	status := strings.TrimSpace(c.Query("status"))
	if status != "" && status != "active" && status != "suspended" && status != "retired" {
		codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "组织状态筛选无效")
		return
	}

	search := strings.TrimSpace(c.Query("search"))
	now := time.Now()
	limit := page * size
	items := make([]platformOrganizationItem, 0, limit*2)
	var total int64

	if scopeType == "" || scopeType == string(model.ManagementScopeProvider) {
		query := platformProviderOrganizationQuery(c, search, status)
		var count int64
		if err := query.Count(&count).Error; err != nil {
			codedError(c, http.StatusInternalServerError, "PLATFORM_ORGANIZATION_QUERY_FAILED", "查询 Provider 组织失败")
			return
		}
		total += count
		if count > 0 {
			var providerItems []platformOrganizationItem
			if err := query.Select(`organization.id, 'provider' AS scope_type, organization.key,
				organization.display_name AS name, organization.domain_scope, organization.domain_label, organization.status,
				(SELECT COUNT(*) FROM admin_provider_membership AS membership JOIN user AS member_user ON member_user.id = membership.user_id
					WHERE membership.provider_id = organization.id AND membership.enabled = ? AND member_user.enabled = ?
					AND membership.valid_from <= ? AND (membership.expires_at IS NULL OR membership.expires_at > ?)) AS management_membership_count,
				0 AS business_member_count,
				(SELECT COUNT(*) FROM technical_resource WHERE technical_resource.provider_id = organization.id) AS technical_resource_count,
				(SELECT COUNT(*) FROM platform_resource WHERE platform_resource.provider_id = organization.id) AS resource_count,
				(SELECT COUNT(*) FROM resource_scope WHERE resource_scope.provider_id = organization.id) AS scope_count,
				organization.revision, organization.row_version, organization.created_at, organization.updated_at`, true, true, now, now).
				Order("organization.updated_at DESC").Order("organization.id ASC").Limit(limit).Scan(&providerItems).Error; err != nil {
				codedError(c, http.StatusInternalServerError, "PLATFORM_ORGANIZATION_QUERY_FAILED", "查询 Provider 组织失败")
				return
			}
			items = append(items, providerItems...)
		}
	}

	if scopeType == "" || scopeType == string(model.ManagementScopeTenant) {
		query := platformTenantOrganizationQuery(c, search, status)
		var count int64
		if err := query.Count(&count).Error; err != nil {
			codedError(c, http.StatusInternalServerError, "PLATFORM_ORGANIZATION_QUERY_FAILED", "查询 Tenant 组织失败")
			return
		}
		total += count
		if count > 0 {
			var tenantItems []platformOrganizationItem
			if err := query.Select(`organization.id, 'tenant' AS scope_type, organization.key, organization.name, organization.status,
				(SELECT COUNT(*) FROM user_tenant_management_membership AS membership JOIN user AS member_user ON member_user.id = membership.user_id
					WHERE membership.tenant_id = organization.id AND membership.enabled = ? AND member_user.enabled = ?
					AND membership.valid_from <= ? AND (membership.expires_at IS NULL OR membership.expires_at > ?)) AS management_membership_count,
				(SELECT COUNT(*) FROM tenant_membership AS membership JOIN user AS business_user ON business_user.id = membership.user_id
					WHERE membership.tenant_id = organization.id AND membership.enabled = ? AND business_user.enabled = ?
					AND (membership.expires_at IS NULL OR membership.expires_at > ?)) AS business_member_count,
				0 AS technical_resource_count,
				(SELECT COUNT(*) FROM tenant_resource WHERE tenant_resource.tenant_id = organization.id) AS resource_count,
				0 AS scope_count, organization.revision, organization.row_version, organization.created_at, organization.updated_at`, true, true, now, now, true, true, now).
				Order("organization.updated_at DESC").Order("organization.id ASC").Limit(limit).Scan(&tenantItems).Error; err != nil {
				codedError(c, http.StatusInternalServerError, "PLATFORM_ORGANIZATION_QUERY_FAILED", "查询 Tenant 组织失败")
				return
			}
			items = append(items, tenantItems...)
		}
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].UpdatedAt.Equal(items[j].UpdatedAt) {
			if items[i].ScopeType == items[j].ScopeType {
				return items[i].ID < items[j].ID
			}
			return items[i].ScopeType < items[j].ScopeType
		}
		return items[i].UpdatedAt.After(items[j].UpdatedAt)
	})
	offset := (page - 1) * size
	if offset >= len(items) {
		items = []platformOrganizationItem{}
	} else {
		end := offset + size
		if end > len(items) {
			end = len(items)
		}
		items = items[offset:end]
	}
	c.JSON(http.StatusOK, NewPagedResponse(items, total, page, size))
}

func platformProviderOrganizationQuery(c *gin.Context, search, status string) *gorm.DB {
	query := db.DB.WithContext(c.Request.Context()).Table("resource_provider AS organization")
	if search != "" {
		pattern := "%" + search + "%"
		query = query.Where("organization.key LIKE ? OR organization.display_name LIKE ? OR organization.domain_label LIKE ?", pattern, pattern, pattern)
	}
	if status != "" {
		query = query.Where("organization.status = ?", status)
	}
	return query
}

func platformTenantOrganizationQuery(c *gin.Context, search, status string) *gorm.DB {
	query := db.DB.WithContext(c.Request.Context()).Table("tenant AS organization")
	if search != "" {
		pattern := "%" + search + "%"
		query = query.Where("organization.key LIKE ? OR organization.name LIKE ?", pattern, pattern)
	}
	if status != "" {
		query = query.Where("organization.status = ?", status)
	}
	return query
}

type platformManagementMembershipItem struct {
	ID                 string     `json:"id"`
	ScopeType          string     `json:"scope_type"`
	ScopeID            string     `json:"scope_id"`
	ScopeKey           string     `json:"scope_key"`
	ScopeName          string     `json:"scope_name"`
	ScopeStatus        string     `json:"scope_status"`
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

type platformAuditItem struct {
	ID                  int64     `json:"id"`
	ActorAdminID        int64     `json:"actor_admin_id"`
	ActorUsername       string    `json:"actor_username"`
	ActorUserID         uint64    `json:"actor_user_id"`
	ActorUserName       string    `json:"actor_user_name"`
	EffectiveUserID     uint64    `json:"effective_user_id"`
	EffectiveUserName   string    `json:"effective_user_name"`
	SimulationSessionID string    `json:"simulation_session_id"`
	ScopeType           string    `json:"scope_type"`
	ScopeID             string    `json:"scope_id"`
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

func (a *PlatformGovernanceAPI) ListAuditLogs(c *gin.Context) {
	page, size := pageParams(c)
	scopeType := strings.TrimSpace(c.Query("scope_type"))
	if scopeType != "" && scopeType != string(model.ManagementScopePlatform) && scopeType != string(model.ManagementScopeProvider) && scopeType != string(model.ManagementScopeTenant) {
		codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "审计 Scope 类型筛选无效")
		return
	}
	simulation := strings.TrimSpace(c.Query("simulation"))
	if simulation != "" && simulation != "true" && simulation != "false" {
		codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "用户模拟筛选无效")
		return
	}

	query := db.DB.WithContext(c.Request.Context()).Table("audit_log AS audit").
		Joins("LEFT JOIN user AS actor_user ON actor_user.id = audit.actor_user_id").
		Joins("LEFT JOIN user_identity_profile AS actor_profile ON actor_profile.user_id = audit.actor_user_id").
		Joins("LEFT JOIN user AS effective_user ON effective_user.id = audit.effective_user_id").
		Joins("LEFT JOIN user_identity_profile AS effective_profile ON effective_profile.user_id = audit.effective_user_id")
	if scopeType != "" {
		query = query.Where("audit.scope_type = ?", scopeType)
	}
	if actionType := strings.TrimSpace(c.Query("action_type")); actionType != "" {
		query = query.Where("audit.action_type = ?", actionType)
	}
	if simulation == "true" {
		query = query.Where("audit.simulation_session_id <> ''")
	} else if simulation == "false" {
		query = query.Where("audit.simulation_session_id = ''")
	}
	if search := strings.TrimSpace(c.Query("search")); search != "" {
		pattern := "%" + search + "%"
		query = query.Where(`audit.actor_username LIKE ? OR actor_user.name LIKE ? OR actor_profile.display_name LIKE ?
			OR effective_user.name LIKE ? OR effective_profile.display_name LIKE ? OR audit.target_name LIKE ?
			OR audit.target_id LIKE ? OR audit.request_id LIKE ? OR audit.scope_id LIKE ?`,
			pattern, pattern, pattern, pattern, pattern, pattern, pattern, pattern, pattern)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		codedError(c, http.StatusInternalServerError, "PLATFORM_AUDIT_QUERY_FAILED", "查询平台审计失败")
		return
	}
	var items []platformAuditItem
	if err := query.Select(`audit.id, audit.actor_admin_id, audit.actor_username, audit.actor_user_id,
		COALESCE(NULLIF(actor_profile.display_name, ''), NULLIF(actor_user.alias, ''), actor_user.name, audit.actor_username) AS actor_user_name,
		audit.effective_user_id,
		COALESCE(NULLIF(effective_profile.display_name, ''), NULLIF(effective_user.alias, ''), effective_user.name) AS effective_user_name,
		audit.simulation_session_id, audit.scope_type, audit.scope_id, audit.required_permission,
		audit.permission_revision, audit.action_type, audit.target_type, audit.target_id, audit.target_name,
		audit.request_id, audit.source_ip, audit.detail, audit.created_at`).
		Order("audit.created_at DESC").Order("audit.id DESC").Offset((page - 1) * size).Limit(size).Scan(&items).Error; err != nil {
		codedError(c, http.StatusInternalServerError, "PLATFORM_AUDIT_QUERY_FAILED", "查询平台审计失败")
		return
	}
	c.JSON(http.StatusOK, NewPagedResponse(items, total, page, size))
}

func (a *PlatformGovernanceAPI) ListManagementMemberships(c *gin.Context) {
	page, size := pageParams(c)
	scopeType := strings.TrimSpace(c.Query("scope_type"))
	if scopeType != "" && scopeType != string(model.ManagementScopeProvider) && scopeType != string(model.ManagementScopeTenant) {
		codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "管理授权 Scope 类型筛选无效")
		return
	}
	role := strings.TrimSpace(c.Query("role"))
	if role != "" && !validPlatformGovernanceRole(role) {
		codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "管理授权角色筛选无效")
		return
	}
	state := strings.TrimSpace(c.Query("state"))
	if state != "" && state != "active" && state != "disabled" && state != "scheduled" && state != "expired" {
		codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "管理授权状态筛选无效")
		return
	}

	search := strings.TrimSpace(c.Query("search"))
	now := time.Now()
	limit := page * size
	items := make([]platformManagementMembershipItem, 0, limit*2)
	var total int64

	if scopeType == "" || scopeType == string(model.ManagementScopeProvider) {
		query := platformProviderMembershipQuery(c, search, role, state, now)
		var count int64
		if err := query.Count(&count).Error; err != nil {
			codedError(c, http.StatusInternalServerError, "PLATFORM_MEMBERSHIP_QUERY_FAILED", "查询 Provider 管理授权失败")
			return
		}
		total += count
		if count > 0 {
			var providerItems []platformManagementMembershipItem
			if err := query.Select(`membership.id, 'provider' AS scope_type, membership.provider_id AS scope_id,
				provider.key AS scope_key, provider.display_name AS scope_name, provider.status AS scope_status,
				membership.user_id, user.name AS username,
				COALESCE(NULLIF(profile.display_name, ''), NULLIF(user.alias, ''), user.name) AS display_name,
				user.enabled AS user_enabled, membership.role, membership.enabled, membership.valid_from,
				membership.expires_at, membership.permission_revision, membership.reason, membership.row_version,
				membership.created_at, membership.updated_at`).
				Order("membership.updated_at DESC").Order("membership.id ASC").Limit(limit).Scan(&providerItems).Error; err != nil {
				codedError(c, http.StatusInternalServerError, "PLATFORM_MEMBERSHIP_QUERY_FAILED", "查询 Provider 管理授权失败")
				return
			}
			items = append(items, providerItems...)
		}
	}

	if scopeType == "" || scopeType == string(model.ManagementScopeTenant) {
		query := platformTenantMembershipQuery(c, search, role, state, now)
		var count int64
		if err := query.Count(&count).Error; err != nil {
			codedError(c, http.StatusInternalServerError, "PLATFORM_MEMBERSHIP_QUERY_FAILED", "查询 Tenant 管理授权失败")
			return
		}
		total += count
		if count > 0 {
			var tenantItems []platformManagementMembershipItem
			if err := query.Select(`membership.id, 'tenant' AS scope_type, membership.tenant_id AS scope_id,
				tenant.key AS scope_key, tenant.name AS scope_name, tenant.status AS scope_status,
				membership.user_id, user.name AS username,
				COALESCE(NULLIF(profile.display_name, ''), NULLIF(user.alias, ''), user.name) AS display_name,
				user.enabled AS user_enabled, membership.role, membership.enabled, membership.valid_from,
				membership.expires_at, membership.permission_revision, membership.reason, membership.row_version,
				membership.created_at, membership.updated_at`).
				Order("membership.updated_at DESC").Order("membership.id ASC").Limit(limit).Scan(&tenantItems).Error; err != nil {
				codedError(c, http.StatusInternalServerError, "PLATFORM_MEMBERSHIP_QUERY_FAILED", "查询 Tenant 管理授权失败")
				return
			}
			items = append(items, tenantItems...)
		}
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].UpdatedAt.Equal(items[j].UpdatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].UpdatedAt.After(items[j].UpdatedAt)
	})
	offset := (page - 1) * size
	if offset >= len(items) {
		items = []platformManagementMembershipItem{}
	} else {
		end := offset + size
		if end > len(items) {
			end = len(items)
		}
		items = items[offset:end]
	}
	c.JSON(http.StatusOK, NewPagedResponse(items, total, page, size))
}

func platformProviderMembershipQuery(c *gin.Context, search, role, state string, now time.Time) *gorm.DB {
	query := db.DB.WithContext(c.Request.Context()).Table("admin_provider_membership AS membership").
		Joins("JOIN user ON user.id = membership.user_id").
		Joins("LEFT JOIN user_identity_profile AS profile ON profile.user_id = membership.user_id").
		Joins("JOIN resource_provider AS provider ON provider.id = membership.provider_id")
	if search != "" {
		pattern := "%" + search + "%"
		query = query.Where("user.name LIKE ? OR user.alias LIKE ? OR profile.display_name LIKE ? OR provider.key LIKE ? OR provider.display_name LIKE ?", pattern, pattern, pattern, pattern, pattern)
	}
	if role != "" {
		query = query.Where("membership.role = ?", role)
	}
	return filterPlatformMembershipState(query, state, now)
}

func platformTenantMembershipQuery(c *gin.Context, search, role, state string, now time.Time) *gorm.DB {
	query := db.DB.WithContext(c.Request.Context()).Table("user_tenant_management_membership AS membership").
		Joins("JOIN user ON user.id = membership.user_id").
		Joins("LEFT JOIN user_identity_profile AS profile ON profile.user_id = membership.user_id").
		Joins("JOIN tenant ON tenant.id = membership.tenant_id")
	if search != "" {
		pattern := "%" + search + "%"
		query = query.Where("user.name LIKE ? OR user.alias LIKE ? OR profile.display_name LIKE ? OR tenant.key LIKE ? OR tenant.name LIKE ?", pattern, pattern, pattern, pattern, pattern)
	}
	if role != "" {
		query = query.Where("membership.role = ?", role)
	}
	return filterPlatformMembershipState(query, state, now)
}

func filterPlatformMembershipState(query *gorm.DB, state string, now time.Time) *gorm.DB {
	switch state {
	case "active":
		return query.Where("membership.enabled = ? AND user.enabled = ? AND membership.valid_from <= ? AND (membership.expires_at IS NULL OR membership.expires_at > ?)", true, true, now, now)
	case "disabled":
		return query.Where("membership.enabled = ? OR user.enabled = ?", false, false)
	case "scheduled":
		return query.Where("membership.enabled = ? AND user.enabled = ? AND membership.valid_from > ?", true, true, now)
	case "expired":
		return query.Where("membership.enabled = ? AND user.enabled = ? AND membership.expires_at IS NOT NULL AND membership.expires_at <= ?", true, true, now)
	default:
		return query
	}
}

func validPlatformGovernanceRole(role string) bool {
	switch role {
	case string(model.ProviderManagementRoleAdmin), string(model.ProviderManagementRoleOperator), string(model.ProviderManagementRoleViewer),
		string(model.TenantManagementRoleAdmin), string(model.TenantManagementRoleSecurityAuditor), string(model.TenantManagementRoleViewer):
		return true
	default:
		return false
	}
}
