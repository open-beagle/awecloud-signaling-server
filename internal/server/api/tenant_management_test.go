package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	serverdb "github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func setupTenantManagementTest(t *testing.T) (*model.Admin, *model.Admin, *model.Tenant) {
	t.Helper()
	database, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(
		&model.Admin{}, &model.AdminTenantMembership{}, &model.Tenant{}, &model.TenantMembership{}, &model.AuditLog{},
	))
	serverdb.DB = database
	platform := &model.Admin{Username: "platform-owner", PasswordHash: "test", Role: "admin", Enabled: true}
	tenantAdmin := &model.Admin{Username: "tenant-operator", PasswordHash: "test", Role: "tenant_admin", Enabled: true}
	tenant := &model.Tenant{ID: "tenant-management-a", Key: "tenant-a", Name: "租户 A", Status: model.TenantStatusActive}
	require.NoError(t, database.Create(platform).Error)
	require.NoError(t, database.Create(tenantAdmin).Error)
	require.NoError(t, database.Create(tenant).Error)
	require.NoError(t, database.Create(&model.AdminTenantMembership{
		AdminID: tenantAdmin.ID, TenantID: tenant.ID, Role: string(model.TenantManagementRoleAdmin), Enabled: true, PermissionRevision: 1,
	}).Error)
	return platform, tenantAdmin, tenant
}

func tenantManagementRouter(adminID int64, register func(*gin.Engine)) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("admin_id", adminID); c.Next() })
	register(router)
	return router
}

func TestTenantSettingsReadUpdateAndAudit(t *testing.T) {
	_, tenantAdmin, tenant := setupTenantManagementTest(t)
	api := NewTenantSettingsAPI()
	router := tenantManagementRouter(tenantAdmin.ID, func(router *gin.Engine) {
		router.GET("/tenants/:id/settings", api.Get)
		router.PUT("/tenants/:id/settings", api.Update)
	})

	getRequest := httptest.NewRequest(http.MethodGet, "/tenants/"+tenant.ID+"/settings", nil)
	getRequest.Header.Set("X-Tenant-ID", tenant.ID)
	getResponse := httptest.NewRecorder()
	router.ServeHTTP(getResponse, getRequest)
	require.Equal(t, http.StatusOK, getResponse.Code)
	require.Contains(t, getResponse.Body.String(), `"key":"tenant-a"`)

	body, err := json.Marshal(map[string]string{"name": "租户 A（新名称）"})
	require.NoError(t, err)
	updateRequest := httptest.NewRequest(http.MethodPut, "/tenants/"+tenant.ID+"/settings", bytes.NewReader(body))
	updateRequest.Header.Set("Content-Type", "application/json")
	updateRequest.Header.Set("X-Tenant-ID", tenant.ID)
	updateRequest.Header.Set("X-Request-ID", "settings-request")
	updateResponse := httptest.NewRecorder()
	router.ServeHTTP(updateResponse, updateRequest)
	require.Equal(t, http.StatusOK, updateResponse.Code)

	var updated model.Tenant
	require.NoError(t, serverdb.DB.First(&updated, "id = ?", tenant.ID).Error)
	require.Equal(t, "租户 A（新名称）", updated.Name)
	require.Equal(t, tenant.Key, updated.Key)
	require.Equal(t, model.TenantStatusActive, updated.Status)
	var audit model.AuditLog
	require.NoError(t, serverdb.DB.Where("action_type = ?", "update_tenant_settings").First(&audit).Error)
	require.Equal(t, tenantAdmin.ID, audit.ActorAdminID)
	require.Equal(t, tenant.ID, audit.TenantID)
	require.Equal(t, PermissionTenantSettingsWrite, audit.RequiredPermission)
	require.Equal(t, "settings-request", audit.RequestID)
}

func TestTenantSettingsViewerCannotWrite(t *testing.T) {
	_, tenantAdmin, tenant := setupTenantManagementTest(t)
	require.NoError(t, serverdb.DB.Model(&model.AdminTenantMembership{}).
		Where("admin_id = ? AND tenant_id = ?", tenantAdmin.ID, tenant.ID).
		Updates(map[string]interface{}{"role": string(model.TenantManagementRoleViewer), "permission_revision": 2}).Error)
	api := NewTenantSettingsAPI()
	router := tenantManagementRouter(tenantAdmin.ID, func(router *gin.Engine) { router.PUT("/tenants/:id/settings", api.Update) })
	request := httptest.NewRequest(http.MethodPut, "/tenants/"+tenant.ID+"/settings", bytes.NewBufferString(`{"name":"越权名称"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Tenant-ID", tenant.ID)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusForbidden, response.Code)
	require.Contains(t, response.Body.String(), ErrorCodeTenantPermissionDenied)
}

func TestTenantAdminMembershipLifecycleDoesNotCreateBusinessMember(t *testing.T) {
	platform, tenantAdmin, tenant := setupTenantManagementTest(t)
	api := NewTenantAdminMembershipAPI()
	router := tenantManagementRouter(platform.ID, func(router *gin.Engine) {
		router.POST("/tenant-admin-memberships", api.Create)
		router.PUT("/tenant-admin-memberships/:id", api.Update)
		router.GET("/tenant-admin-memberships", api.List)
	})

	newAdmin := model.Admin{Username: "scoped-auditor", PasswordHash: "test", Role: "tenant_admin", Enabled: true}
	require.NoError(t, serverdb.DB.Create(&newAdmin).Error)
	createBody := bytes.NewBufferString(`{"admin_id":` + jsonNumber(newAdmin.ID) + `,"tenant_id":"` + tenant.ID + `","role":"security_auditor","enabled":true}`)
	createRequest := httptest.NewRequest(http.MethodPost, "/tenant-admin-memberships", createBody)
	createRequest.Header.Set("Content-Type", "application/json")
	createResponse := httptest.NewRecorder()
	router.ServeHTTP(createResponse, createRequest)
	require.Equal(t, http.StatusCreated, createResponse.Code)

	var membership model.AdminTenantMembership
	require.NoError(t, serverdb.DB.Where("admin_id = ? AND tenant_id = ?", newAdmin.ID, tenant.ID).First(&membership).Error)
	require.Equal(t, string(model.TenantManagementRoleSecurityAuditor), membership.Role)
	require.Equal(t, int64(1), membership.PermissionRevision)
	var businessMemberCount int64
	require.NoError(t, serverdb.DB.Model(&model.TenantMembership{}).Count(&businessMemberCount).Error)
	require.Zero(t, businessMemberCount)

	expiresAt := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	updateBody := bytes.NewBufferString(`{"role":"tenant_viewer","enabled":false,"expires_at":"` + expiresAt + `"}`)
	updateRequest := httptest.NewRequest(http.MethodPut, "/tenant-admin-memberships/"+jsonNumber(membership.ID), updateBody)
	updateRequest.Header.Set("Content-Type", "application/json")
	updateResponse := httptest.NewRecorder()
	router.ServeHTTP(updateResponse, updateRequest)
	require.Equal(t, http.StatusOK, updateResponse.Code)
	require.NoError(t, serverdb.DB.First(&membership, membership.ID).Error)
	require.Equal(t, string(model.TenantManagementRoleViewer), membership.Role)
	require.False(t, membership.Enabled)
	require.Equal(t, int64(2), membership.PermissionRevision)

	listResponse := httptest.NewRecorder()
	router.ServeHTTP(listResponse, httptest.NewRequest(http.MethodGet, "/tenant-admin-memberships?search=scoped-auditor", nil))
	require.Equal(t, http.StatusOK, listResponse.Code)
	require.Contains(t, listResponse.Body.String(), "scoped-auditor")
	require.NotContains(t, listResponse.Body.String(), tenantAdmin.Username)
}

func jsonNumber(value int64) string { return strconv.FormatInt(value, 10) }
