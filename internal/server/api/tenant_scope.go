package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func selectedTenantID(c *gin.Context) (string, bool) {
	header := strings.TrimSpace(c.GetHeader("X-Tenant-ID"))
	query := strings.TrimSpace(c.Query("tenant_id"))
	if header != "" && query != "" && header != query {
		codedError(c, http.StatusBadRequest, ErrorCodeTenantContextConflict, "Tenant 上下文与查询条件不一致")
		return "", false
	}
	if header != "" {
		return header, true
	}
	return query, true
}

func currentAdmin(c *gin.Context) (*model.Admin, bool, bool) {
	adminID := getAdminIDFromContext(c)
	if adminID == 0 {
		// AuthMiddleware uses ID 0 only for the explicit localhost debug path;
		// unit handlers also omit middleware. Production JWTs always have an ID.
		return &model.Admin{Role: "admin"}, true, true
	}
	var admin model.Admin
	if err := db.DB.WithContext(c.Request.Context()).Where("id = ? AND enabled = ?", adminID, true).First(&admin).Error; err != nil {
		c.JSON(http.StatusUnauthorized, NewErrorResponse("管理员身份无效"))
		return nil, false, false
	}
	return &admin, false, true
}

func requirePlatformAccess(c *gin.Context, write bool) bool {
	admin, debug, ok := currentAdmin(c)
	if !ok {
		return false
	}
	if debug {
		return true
	}
	if !admin.Enabled {
		codedError(c, http.StatusForbidden, ErrorCodeAdminDisabled, "管理员身份已停用")
		return false
	}
	role := model.NormalizePlatformRole(admin.Role)
	if role == model.PlatformRoleAdmin || (!write && role == model.PlatformRoleViewer) {
		return true
	}
	codedError(c, http.StatusForbidden, ErrorCodePlatformPermissionDenied, "需要平台管理权限")
	return false
}

func requirePlatformAdmin(c *gin.Context) bool {
	admin, debug, ok := currentAdmin(c)
	if !ok {
		return false
	}
	if debug {
		c.JSON(http.StatusUnauthorized, NewErrorResponse("管理账号操作必须使用 Platform Admin 身份认证"))
		return false
	}
	if admin.Role == "admin" {
		return true
	}
	c.JSON(http.StatusForbidden, NewErrorResponse("需要 Platform Admin 权限"))
	return false
}

func requireTenantAccess(c *gin.Context, tenantID string, write bool) bool {
	permission := PermissionTenantOverviewRead
	if write {
		permission = PermissionTenantSettingsWrite
	}
	return requireTenantPermission(c, tenantID, permission)
}

func requireTenantPermission(c *gin.Context, tenantID, permission string) bool {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		codedError(c, http.StatusBadRequest, ErrorCodeTenantContextRequired, "必须指定租户上下文")
		return false
	}
	admin, debug, ok := currentAdmin(c)
	if !ok {
		return false
	}
	if debug {
		return true
	}
	if !admin.Enabled {
		codedError(c, http.StatusForbidden, ErrorCodeAdminDisabled, "管理员身份已停用")
		return false
	}
	headerTenantID := strings.TrimSpace(c.GetHeader("X-Tenant-ID"))
	if headerTenantID == "" {
		codedError(c, http.StatusBadRequest, ErrorCodeTenantContextRequired, "租户级请求必须携带 X-Tenant-ID")
		return false
	}
	selected, ok := selectedTenantID(c)
	if !ok {
		return false
	}
	if selected != tenantID {
		codedError(c, http.StatusForbidden, ErrorCodeTenantContextConflict, "不能访问其他租户的对象")
		return false
	}

	var tenant model.Tenant
	if err := db.DB.WithContext(c.Request.Context()).First(&tenant, "id = ?", tenantID).Error; err != nil {
		codedError(c, http.StatusNotFound, ErrorCodeTenantObjectNotFound, "当前租户范围内对象不存在")
		return false
	}
	if tenant.Status == model.TenantStatusSuspended && tenantPermissionIsWrite(permission) {
		codedError(c, http.StatusConflict, ErrorCodeTenantSuspended, "租户已暂停，不能执行写操作")
		return false
	}

	now := time.Now()
	var membership model.AdminTenantMembership
	if err := db.DB.WithContext(c.Request.Context()).Where(
		"admin_id = ? AND tenant_id = ? AND enabled = ? AND (expires_at IS NULL OR expires_at > ?)",
		admin.ID, tenantID, true, now,
	).First(&membership).Error; err != nil {
		codedError(c, http.StatusForbidden, ErrorCodeTenantContextUnavailable, "管理员没有有效的租户管理角色")
		return false
	}
	if _, allowed := tenantRoleHasPermission(membership.Role, permission); !allowed {
		codedError(c, http.StatusForbidden, ErrorCodeTenantPermissionDenied, "当前租户角色不能执行此操作")
		return false
	}
	c.Set("audit_tenant_id", tenantID)
	c.Set("audit_tenant_role", string(model.NormalizeTenantManagementRole(membership.Role)))
	c.Set("audit_required_permission", permission)
	c.Set("audit_permission_revision", membership.PermissionRevision)
	return true
}

// tenantObjectScope authorizes the selected Tenant before a detail or mutation
// query is allowed to resolve an object ID. The unrestricted result exists only
// for the explicit localhost debug path and unit handlers without auth.
func tenantObjectScope(c *gin.Context, permission string) (tenantID string, unrestricted bool, ok bool) {
	selected, selectedOK := selectedTenantID(c)
	if !selectedOK {
		return "", false, false
	}
	if getAdminIDFromContext(c) == 0 {
		if selected == "" {
			return "", true, true
		}
		return selected, false, requireTenantPermission(c, selected, permission)
	}
	if selected == "" {
		codedError(c, http.StatusBadRequest, ErrorCodeTenantContextRequired, "租户级请求必须携带 X-Tenant-ID")
		return "", false, false
	}
	if !requireTenantPermission(c, selected, permission) {
		return "", false, false
	}
	return selected, false, true
}

func tenantReadScope(c *gin.Context, permission string) ([]string, bool, bool) {
	selected, ok := selectedTenantID(c)
	if !ok {
		return nil, false, false
	}
	admin, debug, ok := currentAdmin(c)
	if !ok {
		return nil, false, false
	}
	if debug {
		if selected != "" {
			return []string{selected}, false, true
		}
		return nil, true, true
	}
	if !admin.Enabled {
		codedError(c, http.StatusForbidden, ErrorCodeAdminDisabled, "管理员身份已停用")
		return nil, false, false
	}
	if selected != "" {
		if !requireTenantPermission(c, selected, permission) {
			return nil, false, false
		}
		return []string{selected}, false, true
	}
	platformRole := model.NormalizePlatformRole(admin.Role)
	if platformRole == model.PlatformRoleAdmin || platformRole == model.PlatformRoleViewer {
		return nil, true, true
	}
	query := db.DB.WithContext(c.Request.Context()).Model(&model.AdminTenantMembership{}).
		Where("admin_id = ? AND enabled = ? AND (expires_at IS NULL OR expires_at > ?)", admin.ID, true, time.Now())
	var memberships []model.AdminTenantMembership
	if err := query.Find(&memberships).Error; err != nil || len(memberships) == 0 {
		c.JSON(http.StatusForbidden, NewErrorResponse("管理员没有可访问的 Tenant"))
		return nil, false, false
	}
	ids := make([]string, 0, len(memberships))
	for _, membership := range memberships {
		if _, allowed := tenantRoleHasPermission(membership.Role, permission); allowed {
			ids = append(ids, membership.TenantID)
		}
	}
	if len(ids) == 0 {
		codedError(c, http.StatusForbidden, ErrorCodeTenantPermissionDenied, "当前租户角色不能执行此操作")
		return nil, false, false
	}
	return ids, false, true
}
