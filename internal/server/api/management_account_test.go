package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

type managementAccountTestEnv struct {
	database *gorm.DB
	router   *gin.Engine
	platform model.Admin
	tenantA  model.Tenant
	tenantB  model.Tenant
}

func newManagementAccountTestEnv(t *testing.T) managementAccountTestEnv {
	t.Helper()
	oldDB := db.DB
	t.Cleanup(func() { db.DB = oldDB })
	database, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	db.DB = database
	require.NoError(t, database.AutoMigrate(
		&model.Admin{}, &model.AdminTenantMembership{}, &model.User{}, &model.Tenant{},
		&model.UserIdentityProfile{}, &model.UserAuthenticationLink{}, &model.PlatformRoleMembership{},
		&model.UserTenantManagementMembership{}, &model.AuditLog{},
	))

	platform := model.Admin{Username: "platform-admin", PasswordHash: "test", Role: managementRoleAdmin, Enabled: true}
	tenantA := model.Tenant{ID: uuid.NewString(), Key: "acceptance-a", Name: "Acceptance A", Status: model.TenantStatusActive}
	tenantB := model.Tenant{ID: uuid.NewString(), Key: "acceptance-b", Name: "Acceptance B", Status: model.TenantStatusActive}
	require.NoError(t, database.Create(&platform).Error)
	require.NoError(t, database.Create(&tenantA).Error)
	require.NoError(t, database.Create(&tenantB).Error)

	managementAPI := NewManagementAccountAPI()
	router := gin.New()
	router.Use(func(c *gin.Context) {
		id, _ := strconv.ParseInt(c.GetHeader("X-Test-Admin-ID"), 10, 64)
		c.Set("admin_id", id)
	})
	router.GET("/management-accounts", managementAPI.List)
	router.POST("/management-accounts", managementAPI.Create)
	router.POST("/management-accounts/:id/password", managementAPI.ResetPassword)
	router.POST("/management-accounts/:id/enable", managementAPI.Enable)
	router.POST("/management-accounts/:id/disable", managementAPI.Disable)
	router.GET("/management-accounts/:id/tenant-memberships", managementAPI.ListTenantMemberships)
	router.POST("/management-accounts/:id/tenant-memberships", managementAPI.BindTenant)
	router.POST("/management-accounts/:id/tenant-memberships/:tenant_id/disable", managementAPI.DisableTenantMembership)

	return managementAccountTestEnv{database: database, router: router, platform: platform, tenantA: tenantA, tenantB: tenantB}
}

func managementRequest(t *testing.T, router http.Handler, adminID int64, method, path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var requestBody *bytes.Reader
	if body == nil {
		requestBody = bytes.NewReader(nil)
	} else {
		data, err := json.Marshal(body)
		require.NoError(t, err)
		requestBody = bytes.NewReader(data)
	}
	req := httptest.NewRequest(method, path, requestBody)
	req.Header.Set("Content-Type", "application/json")
	if adminID != 0 {
		req.Header.Set("X-Test-Admin-ID", strconv.FormatInt(adminID, 10))
	}
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	return resp
}

func TestManagementAccountCreateIsScopedAndDoesNotLeakPassword(t *testing.T) {
	env := newManagementAccountTestEnv(t)
	const password = "acceptance-secret-2026"
	createBody := map[string]interface{}{
		"username": "acceptance-tenant-admin",
		"password": password,
		"role":     managementRoleTenantAdmin,
		"tenant_memberships": []map[string]string{{
			"tenant_id": env.tenantA.ID,
			"role":      managementRoleTenantAdmin,
		}},
	}
	resp := managementRequest(t, env.router, env.platform.ID, http.MethodPost, "/management-accounts", createBody)
	require.Equal(t, http.StatusCreated, resp.Code, resp.Body.String())
	require.NotContains(t, resp.Body.String(), password)
	require.NotContains(t, resp.Body.String(), "password_hash")

	var created model.Admin
	require.NoError(t, env.database.First(&created, "username = ?", "acceptance-tenant-admin").Error)
	require.True(t, created.Enabled)
	require.Equal(t, managementRoleTenantAdmin, created.Role)
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(created.PasswordHash), []byte(password)))
	var membership model.AdminTenantMembership
	require.NoError(t, env.database.First(&membership, "admin_id = ? AND tenant_id = ?", created.ID, env.tenantA.ID).Error)
	require.True(t, membership.Enabled)
	require.Equal(t, managementRoleTenantAdmin, membership.Role)
	var link model.UserAuthenticationLink
	require.NoError(t, env.database.Where("provider_type = ? AND provider_subject = ?",
		model.AuthenticationProviderLegacyAdmin, strconv.FormatInt(created.ID, 10)).First(&link).Error)
	var unifiedMembership model.UserTenantManagementMembership
	require.NoError(t, env.database.First(&unifiedMembership, "user_id = ? AND tenant_id = ?", link.UserID, env.tenantA.ID).Error)
	require.True(t, unifiedMembership.Enabled)
	require.Equal(t, model.TenantManagementRoleAdmin, unifiedMembership.Role)

	var audit model.AuditLog
	require.NoError(t, env.database.First(&audit, "action_type = ?", "create_management_account").Error)
	require.NotContains(t, audit.Detail, password)
	require.NotContains(t, strings.ToLower(audit.Detail), "password")

	missingScope := managementRequest(t, env.router, env.platform.ID, http.MethodPost, "/management-accounts", map[string]string{
		"username": "unscoped-tenant-admin", "password": password, "role": managementRoleTenantAdmin,
	})
	require.Equal(t, http.StatusBadRequest, missingScope.Code)
	var unscopedCount int64
	require.NoError(t, env.database.Model(&model.Admin{}).Where("username = ?", "unscoped-tenant-admin").Count(&unscopedCount).Error)
	require.Zero(t, unscopedCount)

	viewer := model.Admin{Username: "platform-viewer", PasswordHash: "test", Role: managementRoleViewer, Enabled: true}
	require.NoError(t, env.database.Create(&viewer).Error)
	viewerList := managementRequest(t, env.router, viewer.ID, http.MethodGet, "/management-accounts", nil)
	require.Equal(t, http.StatusForbidden, viewerList.Code)
	localhostBypass := managementRequest(t, env.router, 0, http.MethodGet, "/management-accounts", nil)
	require.Equal(t, http.StatusUnauthorized, localhostBypass.Code)
}

func TestManagementAccountTenantBindingDisableAndRestore(t *testing.T) {
	env := newManagementAccountTestEnv(t)
	account := model.Admin{Username: "tenant-binding-admin", PasswordHash: "test", Role: managementRoleTenantAdmin, Enabled: true}
	require.NoError(t, env.database.Create(&account).Error)
	require.NoError(t, env.database.Create(&model.AdminTenantMembership{
		AdminID: account.ID, TenantID: env.tenantA.ID, Role: managementRoleTenantAdmin, Enabled: true,
	}).Error)

	bind := managementRequest(t, env.router, env.platform.ID, http.MethodPost,
		"/management-accounts/"+strconv.FormatInt(account.ID, 10)+"/tenant-memberships",
		map[string]string{"tenant_id": env.tenantB.ID, "role": managementRoleViewer})
	require.Equal(t, http.StatusCreated, bind.Code, bind.Body.String())

	disable := managementRequest(t, env.router, env.platform.ID, http.MethodPost,
		"/management-accounts/"+strconv.FormatInt(account.ID, 10)+"/tenant-memberships/"+env.tenantB.ID+"/disable", nil)
	require.Equal(t, http.StatusOK, disable.Code, disable.Body.String())
	var membership model.AdminTenantMembership
	require.NoError(t, env.database.First(&membership, "admin_id = ? AND tenant_id = ?", account.ID, env.tenantB.ID).Error)
	require.False(t, membership.Enabled)
	var link model.UserAuthenticationLink
	require.NoError(t, env.database.Where("provider_type = ? AND provider_subject = ?",
		model.AuthenticationProviderLegacyAdmin, strconv.FormatInt(account.ID, 10)).First(&link).Error)
	var unifiedMembership model.UserTenantManagementMembership
	require.NoError(t, env.database.First(&unifiedMembership, "user_id = ? AND tenant_id = ?", link.UserID, env.tenantB.ID).Error)
	require.False(t, unifiedMembership.Enabled)
	disabledRevision := unifiedMembership.PermissionRevision

	restore := managementRequest(t, env.router, env.platform.ID, http.MethodPost,
		"/management-accounts/"+strconv.FormatInt(account.ID, 10)+"/tenant-memberships",
		map[string]string{"tenant_id": env.tenantB.ID, "role": managementRoleTenantAdmin})
	require.Equal(t, http.StatusCreated, restore.Code, restore.Body.String())
	require.NoError(t, env.database.First(&membership, "admin_id = ? AND tenant_id = ?", account.ID, env.tenantB.ID).Error)
	require.True(t, membership.Enabled)
	require.Equal(t, managementRoleTenantAdmin, membership.Role)
	require.NoError(t, env.database.First(&unifiedMembership, "user_id = ? AND tenant_id = ?", link.UserID, env.tenantB.ID).Error)
	require.True(t, unifiedMembership.Enabled)
	require.Equal(t, model.TenantManagementRoleAdmin, unifiedMembership.Role)
	require.Greater(t, unifiedMembership.PermissionRevision, disabledRevision)
	var count int64
	require.NoError(t, env.database.Model(&model.AdminTenantMembership{}).Where("admin_id = ? AND tenant_id = ?", account.ID, env.tenantB.ID).Count(&count).Error)
	require.Equal(t, int64(1), count)
}

func TestManagementAccountLifecycleGuardsAndPasswordReset(t *testing.T) {
	env := newManagementAccountTestEnv(t)
	selfDisable := managementRequest(t, env.router, env.platform.ID, http.MethodPost,
		"/management-accounts/"+strconv.FormatInt(env.platform.ID, 10)+"/disable", nil)
	require.Equal(t, http.StatusConflict, selfDisable.Code)

	lastAdminDisable := managementRequest(t, env.router, 0, http.MethodPost,
		"/management-accounts/"+strconv.FormatInt(env.platform.ID, 10)+"/disable", nil)
	require.Equal(t, http.StatusUnauthorized, lastAdminDisable.Code)

	secondPlatform := model.Admin{Username: "second-platform", PasswordHash: "test", Role: managementRoleAdmin, Enabled: true}
	require.NoError(t, env.database.Create(&secondPlatform).Error)
	disable := managementRequest(t, env.router, secondPlatform.ID, http.MethodPost,
		"/management-accounts/"+strconv.FormatInt(env.platform.ID, 10)+"/disable", nil)
	require.Equal(t, http.StatusOK, disable.Code, disable.Body.String())
	require.NoError(t, env.database.First(&env.platform, env.platform.ID).Error)
	require.False(t, env.platform.Enabled)

	disabledTokenRequest := managementRequest(t, env.router, env.platform.ID, http.MethodGet, "/management-accounts", nil)
	require.Equal(t, http.StatusUnauthorized, disabledTokenRequest.Code)

	const newPassword = "replacement-secret-2026"
	reset := managementRequest(t, env.router, secondPlatform.ID, http.MethodPost,
		"/management-accounts/"+strconv.FormatInt(env.platform.ID, 10)+"/password", map[string]string{"password": newPassword})
	require.Equal(t, http.StatusOK, reset.Code, reset.Body.String())
	require.NotContains(t, reset.Body.String(), newPassword)
	require.NoError(t, env.database.First(&env.platform, env.platform.ID).Error)
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(env.platform.PasswordHash), []byte(newPassword)))

	var audit model.AuditLog
	require.NoError(t, env.database.First(&audit, "action_type = ?", "reset_management_account_password").Error)
	require.NotContains(t, audit.Detail, newPassword)
}

func TestDisabledManagementAccountCannotLogin(t *testing.T) {
	env := newManagementAccountTestEnv(t)
	const password = "disabled-secret-2026"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	require.NoError(t, err)
	disabled := model.Admin{
		Username: "disabled-viewer", PasswordHash: string(hash), Role: managementRoleViewer, Enabled: true,
	}
	require.NoError(t, env.database.Create(&disabled).Error)
	require.NoError(t, env.database.Model(&disabled).Update("enabled", false).Error)

	router := gin.New()
	router.POST("/login", NewAdminAPI(&config.ServerConfig{Security: config.SecuritySection{JWTSecret: "test-secret", JWTExpireHours: 1}}).Login)
	resp := managementRequest(t, router, 0, http.MethodPost, "/login", map[string]string{
		"username": disabled.Username, "password": password,
	})
	require.Equal(t, http.StatusUnauthorized, resp.Code)
	require.NotContains(t, resp.Body.String(), password)
}
