package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

const (
	ErrorCodeAuthRequired             = "AUTH_REQUIRED"
	ErrorCodeAdminDisabled            = "ADMIN_DISABLED"
	ErrorCodePlatformPermissionDenied = "PLATFORM_PERMISSION_DENIED"
	ErrorCodeTenantContextRequired    = "TENANT_CONTEXT_REQUIRED"
	ErrorCodeTenantContextConflict    = "TENANT_CONTEXT_CONFLICT"
	ErrorCodeTenantContextUnavailable = "TENANT_CONTEXT_UNAVAILABLE"
	ErrorCodeTenantPermissionDenied   = "TENANT_PERMISSION_DENIED"
	ErrorCodeTenantSuspended          = "TENANT_SUSPENDED"
	ErrorCodeTenantObjectNotFound     = "TENANT_OBJECT_NOT_FOUND"
	ErrorCodePermissionRevisionStale  = "PERMISSION_REVISION_STALE"
)

type TenantContextItem struct {
	TenantID           string                     `json:"tenant_id"`
	TenantKey          string                     `json:"tenant_key"`
	TenantName         string                     `json:"tenant_name"`
	TenantStatus       model.TenantStatus         `json:"tenant_status"`
	ManagementRole     model.TenantManagementRole `json:"management_role"`
	Permissions        []string                   `json:"permissions"`
	PermissionRevision int64                      `json:"permission_revision"`
	ExpiresAt          *time.Time                 `json:"expires_at,omitempty"`
}

type TenantContextAPI struct{}

func NewTenantContextAPI() *TenantContextAPI { return &TenantContextAPI{} }

func codedError(c *gin.Context, status int, code, message string) {
	c.JSON(status, NewCodedErrorResponse(code, message, requestID(c)))
}

func (a *TenantContextAPI) List(c *gin.Context) {
	admin, ok := enabledCurrentAdmin(c)
	if !ok {
		return
	}
	contexts, err := loadTenantContexts(c, admin.ID, "")
	if err != nil {
		codedError(c, http.StatusInternalServerError, "TENANT_CONTEXT_QUERY_FAILED", "查询租户上下文失败")
		return
	}
	c.JSON(http.StatusOK, NewSuccessResponse(contexts))
}

func (a *TenantContextAPI) Get(c *gin.Context) {
	admin, ok := enabledCurrentAdmin(c)
	if !ok {
		return
	}
	tenantID := strings.TrimSpace(c.Param("id"))
	if tenantID == "" {
		codedError(c, http.StatusBadRequest, ErrorCodeTenantContextUnavailable, "必须指定租户")
		return
	}
	contexts, err := loadTenantContexts(c, admin.ID, tenantID)
	if err != nil {
		codedError(c, http.StatusInternalServerError, "TENANT_CONTEXT_QUERY_FAILED", "查询租户上下文失败")
		return
	}
	if len(contexts) == 0 {
		codedError(c, http.StatusForbidden, ErrorCodeTenantContextUnavailable, "管理员没有有效的租户管理角色")
		return
	}
	c.JSON(http.StatusOK, NewSuccessResponse(contexts[0]))
}

func enabledCurrentAdmin(c *gin.Context) (*model.Admin, bool) {
	adminID := getAdminIDFromContext(c)
	if adminID == 0 {
		codedError(c, http.StatusUnauthorized, ErrorCodeAuthRequired, "管理员身份无效")
		return nil, false
	}
	var admin model.Admin
	if err := db.DB.WithContext(c.Request.Context()).First(&admin, adminID).Error; err != nil {
		codedError(c, http.StatusUnauthorized, ErrorCodeAuthRequired, "管理员身份无效")
		return nil, false
	}
	if !admin.Enabled {
		codedError(c, http.StatusForbidden, ErrorCodeAdminDisabled, "管理员身份已停用")
		return nil, false
	}
	return &admin, true
}

func loadTenantContexts(c *gin.Context, adminID int64, tenantID string) ([]TenantContextItem, error) {
	now := time.Now()
	query := db.DB.WithContext(c.Request.Context()).Model(&model.AdminTenantMembership{}).
		Where("admin_id = ? AND enabled = ? AND (expires_at IS NULL OR expires_at > ?)", adminID, true, now)
	if tenantID != "" {
		query = query.Where("tenant_id = ?", tenantID)
	}
	var memberships []model.AdminTenantMembership
	if err := query.Order("tenant_id ASC").Find(&memberships).Error; err != nil {
		return nil, err
	}
	if len(memberships) == 0 {
		return []TenantContextItem{}, nil
	}

	tenantIDs := make([]string, 0, len(memberships))
	for _, membership := range memberships {
		tenantIDs = append(tenantIDs, membership.TenantID)
	}
	var tenants []model.Tenant
	if err := db.DB.WithContext(c.Request.Context()).Where("id IN ?", tenantIDs).Find(&tenants).Error; err != nil {
		return nil, err
	}
	tenantByID := make(map[string]model.Tenant, len(tenants))
	for _, tenant := range tenants {
		tenantByID[tenant.ID] = tenant
	}

	contexts := make([]TenantContextItem, 0, len(memberships))
	for _, membership := range memberships {
		tenant, exists := tenantByID[membership.TenantID]
		if !exists {
			continue
		}
		permissions, role, valid := permissionsForTenantRole(membership.Role)
		if !valid {
			continue
		}
		revision := membership.PermissionRevision
		if revision < 1 {
			revision = 1
		}
		contexts = append(contexts, TenantContextItem{
			TenantID:           tenant.ID,
			TenantKey:          tenant.Key,
			TenantName:         tenant.Name,
			TenantStatus:       tenant.Status,
			ManagementRole:     role,
			Permissions:        permissions,
			PermissionRevision: revision,
			ExpiresAt:          membership.ExpiresAt,
		})
	}
	return contexts, nil
}
