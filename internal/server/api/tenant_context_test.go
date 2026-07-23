package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func TestTenantContextsUseMembershipRoleEvenForPlatformAdmin(t *testing.T) {
	oldDB := db.DB
	t.Cleanup(func() { db.DB = oldDB })
	database, err := gorm.Open(sqlite.Open("file:tenant_context_catalog_test?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	db.DB = database
	require.NoError(t, database.AutoMigrate(&model.Admin{}, &model.AdminTenantMembership{}, &model.Tenant{}))

	admin := model.Admin{Username: "platform-with-scoped-tenants", PasswordHash: "test", Role: "admin", Enabled: true}
	require.NoError(t, database.Create(&admin).Error)
	tenantA := model.Tenant{ID: uuid.NewString(), Key: "tenant-a", Name: "Tenant A", Status: model.TenantStatusActive}
	tenantB := model.Tenant{ID: uuid.NewString(), Key: "tenant-b", Name: "Tenant B", Status: model.TenantStatusSuspended}
	tenantC := model.Tenant{ID: uuid.NewString(), Key: "tenant-c", Name: "Tenant C", Status: model.TenantStatusActive}
	require.NoError(t, database.Create(&tenantA).Error)
	require.NoError(t, database.Create(&tenantB).Error)
	require.NoError(t, database.Create(&tenantC).Error)
	require.NoError(t, database.Create(&model.AdminTenantMembership{AdminID: admin.ID, TenantID: tenantA.ID, Role: "tenant_admin", Enabled: true, PermissionRevision: 3}).Error)
	require.NoError(t, database.Create(&model.AdminTenantMembership{AdminID: admin.ID, TenantID: tenantB.ID, Role: "security_auditor", Enabled: true, PermissionRevision: 7}).Error)

	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("admin_id", admin.ID) })
	api := NewTenantContextAPI()
	router.GET("/tenant-contexts", api.List)
	router.GET("/tenant-contexts/:id", api.Get)

	listResponse := httptest.NewRecorder()
	router.ServeHTTP(listResponse, httptest.NewRequest(http.MethodGet, "/tenant-contexts", nil))
	require.Equal(t, http.StatusOK, listResponse.Code)
	var listBody struct {
		Data []TenantContextItem `json:"data"`
	}
	require.NoError(t, json.Unmarshal(listResponse.Body.Bytes(), &listBody))
	require.Len(t, listBody.Data, 2)

	contextByTenant := map[string]TenantContextItem{}
	for _, item := range listBody.Data {
		contextByTenant[item.TenantID] = item
	}
	require.Equal(t, model.TenantManagementRoleAdmin, contextByTenant[tenantA.ID].ManagementRole)
	require.Contains(t, contextByTenant[tenantA.ID].Permissions, PermissionTenantMembersWrite)
	require.Equal(t, int64(3), contextByTenant[tenantA.ID].PermissionRevision)
	require.Equal(t, model.TenantManagementRoleSecurityAuditor, contextByTenant[tenantB.ID].ManagementRole)
	require.Equal(t, model.TenantStatusSuspended, contextByTenant[tenantB.ID].TenantStatus)
	require.Contains(t, contextByTenant[tenantB.ID].Permissions, PermissionTenantAuditRead)
	require.NotContains(t, contextByTenant[tenantB.ID].Permissions, PermissionTenantMembersWrite)
	require.NotContains(t, contextByTenant, tenantC.ID, "platform role must not create a tenant-side context without membership")

	detailResponse := httptest.NewRecorder()
	router.ServeHTTP(detailResponse, httptest.NewRequest(http.MethodGet, "/tenant-contexts/"+tenantB.ID, nil))
	require.Equal(t, http.StatusOK, detailResponse.Code)

	deniedResponse := httptest.NewRecorder()
	router.ServeHTTP(deniedResponse, httptest.NewRequest(http.MethodGet, "/tenant-contexts/"+tenantC.ID, nil))
	require.Equal(t, http.StatusForbidden, deniedResponse.Code)
	var deniedBody Response
	require.NoError(t, json.Unmarshal(deniedResponse.Body.Bytes(), &deniedBody))
	require.Equal(t, ErrorCodeTenantContextUnavailable, deniedBody.Code)
	require.NotEmpty(t, deniedBody.RequestID)
}

func TestTenantContextsExcludeDisabledExpiredAndUnknownRoles(t *testing.T) {
	oldDB := db.DB
	t.Cleanup(func() { db.DB = oldDB })
	database, err := gorm.Open(sqlite.Open("file:tenant_context_validity_test?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	db.DB = database
	require.NoError(t, database.AutoMigrate(&model.Admin{}, &model.AdminTenantMembership{}, &model.Tenant{}))

	admin := model.Admin{Username: "tenant-context-validity", PasswordHash: "test", Role: "tenant_admin", Enabled: true}
	require.NoError(t, database.Create(&admin).Error)
	expiredAt := time.Now().Add(-time.Minute)
	tenantRoles := []struct {
		key       string
		role      string
		enabled   bool
		expiresAt *time.Time
	}{
		{key: "valid-viewer", role: "viewer", enabled: true},
		{key: "disabled", role: "tenant_admin", enabled: false},
		{key: "expired", role: "tenant_admin", enabled: true, expiresAt: &expiredAt},
		{key: "unknown", role: "owner", enabled: true},
	}
	for _, entry := range tenantRoles {
		tenant := model.Tenant{ID: uuid.NewString(), Key: entry.key, Name: entry.key, Status: model.TenantStatusActive}
		require.NoError(t, database.Create(&tenant).Error)
		membership := model.AdminTenantMembership{AdminID: admin.ID, TenantID: tenant.ID, Role: entry.role, Enabled: true, ExpiresAt: entry.expiresAt}
		require.NoError(t, database.Create(&membership).Error)
		if !entry.enabled {
			require.NoError(t, database.Model(&membership).Update("enabled", false).Error)
		}
	}

	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("admin_id", admin.ID) })
	router.GET("/tenant-contexts", NewTenantContextAPI().List)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/tenant-contexts", nil))
	require.Equal(t, http.StatusOK, response.Code)
	var body struct {
		Data []TenantContextItem `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	require.Len(t, body.Data, 1)
	require.Equal(t, model.TenantManagementRoleViewer, body.Data[0].ManagementRole)
	require.Equal(t, int64(1), body.Data[0].PermissionRevision)
}

func TestTenantContextsRejectDisabledAdmin(t *testing.T) {
	oldDB := db.DB
	t.Cleanup(func() { db.DB = oldDB })
	database, err := gorm.Open(sqlite.Open("file:tenant_context_disabled_admin_test?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	db.DB = database
	require.NoError(t, database.AutoMigrate(&model.Admin{}, &model.AdminTenantMembership{}, &model.Tenant{}))

	admin := model.Admin{Username: "disabled-admin", PasswordHash: "test", Role: "admin", Enabled: true}
	require.NoError(t, database.Create(&admin).Error)
	require.NoError(t, database.Model(&admin).Update("enabled", false).Error)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("admin_id", admin.ID) })
	router.GET("/tenant-contexts", NewTenantContextAPI().List)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/tenant-contexts", nil))
	require.Equal(t, http.StatusForbidden, response.Code)
	var body Response
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	require.Equal(t, ErrorCodeAdminDisabled, body.Code)
}
