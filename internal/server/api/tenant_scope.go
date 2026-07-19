package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func selectedTenantID(c *gin.Context) (string, bool) {
	header := strings.TrimSpace(c.GetHeader("X-Tenant-ID"))
	query := strings.TrimSpace(c.Query("tenant_id"))
	if header != "" && query != "" && header != query {
		c.JSON(http.StatusBadRequest, NewErrorResponse("Tenant 上下文与查询条件不一致"))
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
	if err := db.DB.WithContext(c.Request.Context()).First(&admin, adminID).Error; err != nil {
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
	if debug || admin.Role == "admin" || (!write && admin.Role == "viewer") {
		return true
	}
	c.JSON(http.StatusForbidden, NewErrorResponse("需要 Platform Admin 权限"))
	return false
}

func requireTenantAccess(c *gin.Context, tenantID string, write bool) bool {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, NewErrorResponse("必须指定 Tenant 上下文"))
		return false
	}
	selected, ok := selectedTenantID(c)
	if !ok {
		return false
	}
	admin, debug, ok := currentAdmin(c)
	if !ok {
		return false
	}
	if debug {
		return true
	}
	if selected != "" && selected != tenantID {
		c.JSON(http.StatusForbidden, NewErrorResponse("不能访问其他 Tenant"))
		return false
	}
	if admin.Role == "admin" {
		if write && selected == "" {
			c.JSON(http.StatusBadRequest, NewErrorResponse("写操作必须进入明确 Tenant 上下文"))
			return false
		}
		return true
	}
	if admin.Role == "viewer" {
		if write {
			c.JSON(http.StatusForbidden, NewErrorResponse("只读管理员不能执行写操作"))
			return false
		}
		return true
	}
	if selected == "" && write {
		c.JSON(http.StatusBadRequest, NewErrorResponse("写操作必须进入明确 Tenant 上下文"))
		return false
	}
	var membership model.AdminTenantMembership
	if err := db.DB.WithContext(c.Request.Context()).Where("admin_id = ? AND tenant_id = ? AND enabled = ?", admin.ID, tenantID, true).First(&membership).Error; err != nil {
		c.JSON(http.StatusForbidden, NewErrorResponse("管理员不属于该 Tenant"))
		return false
	}
	if write && membership.Role != "tenant_admin" {
		c.JSON(http.StatusForbidden, NewErrorResponse("需要 Tenant Admin 权限"))
		return false
	}
	return true
}

func tenantReadScope(c *gin.Context) ([]string, bool, bool) {
	selected, ok := selectedTenantID(c)
	if !ok {
		return nil, false, false
	}
	admin, debug, ok := currentAdmin(c)
	if !ok {
		return nil, false, false
	}
	if debug || admin.Role == "admin" || admin.Role == "viewer" {
		if selected != "" {
			return []string{selected}, false, true
		}
		return nil, true, true
	}
	query := db.DB.WithContext(c.Request.Context()).Model(&model.AdminTenantMembership{}).
		Where("admin_id = ? AND enabled = ?", admin.ID, true)
	if selected != "" {
		query = query.Where("tenant_id = ?", selected)
	}
	var memberships []model.AdminTenantMembership
	if err := query.Find(&memberships).Error; err != nil || len(memberships) == 0 {
		c.JSON(http.StatusForbidden, NewErrorResponse("管理员没有可访问的 Tenant"))
		return nil, false, false
	}
	ids := make([]string, 0, len(memberships))
	for _, membership := range memberships {
		ids = append(ids, membership.TenantID)
	}
	return ids, false, true
}
