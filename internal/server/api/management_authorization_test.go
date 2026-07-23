package api

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func TestManagementAuthorizationRoleMatrix(t *testing.T) {
	oldDB := db.DB
	t.Cleanup(func() { db.DB = oldDB })
	database, err := gorm.Open(sqlite.Open("file:management_authorization_test?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	db.DB = database
	require.NoError(t, database.AutoMigrate(&model.Admin{}))

	admins := map[string]model.Admin{
		"platform": {Username: "platform-route", PasswordHash: "test", Role: "admin"},
		"viewer":   {Username: "viewer-route", PasswordHash: "test", Role: "viewer"},
		"tenant":   {Username: "tenant-route", PasswordHash: "test", Role: "tenant_admin"},
		"none":     {Username: "none-route", PasswordHash: "test", Role: "none"},
	}
	for key, admin := range admins {
		require.NoError(t, database.Create(&admin).Error)
		admins[key] = admin
	}

	router := gin.New()
	router.Use(func(c *gin.Context) {
		id, _ := strconv.ParseInt(c.GetHeader("X-Test-Admin-ID"), 10, 64)
		c.Set("admin_id", id)
	})
	router.Use(ManagementAuthorizationMiddleware())
	router.Any("/*path", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	tests := []struct {
		name   string
		admin  int64
		method string
		path   string
		want   int
	}{
		{name: "platform legacy write", admin: admins["platform"].ID, method: http.MethodPost, path: "/api/v1/admin/users", want: http.StatusNoContent},
		{name: "viewer legacy read", admin: admins["viewer"].ID, method: http.MethodGet, path: "/api/v1/admin/nodes", want: http.StatusNoContent},
		{name: "viewer management accounts denied", admin: admins["viewer"].ID, method: http.MethodGet, path: "/api/v1/admin/management-accounts", want: http.StatusForbidden},
		{name: "viewer legacy write denied", admin: admins["viewer"].ID, method: http.MethodDelete, path: "/api/v1/admin/nodes/1", want: http.StatusForbidden},
		{name: "viewer own password", admin: admins["viewer"].ID, method: http.MethodPut, path: "/api/v1/admin/auth/password", want: http.StatusNoContent},
		{name: "tenant resource grant", admin: admins["tenant"].ID, method: http.MethodPost, path: "/api/v1/admin/resources/resource-a/grants", want: http.StatusNoContent},
		{name: "tenant resource events", admin: admins["tenant"].ID, method: http.MethodGet, path: "/api/v1/admin/resources/resource-a/events", want: http.StatusNoContent},
		{name: "tenant revoke grant", admin: admins["tenant"].ID, method: http.MethodPost, path: "/api/v1/admin/grants/grant-a/revoke", want: http.StatusNoContent},
		{name: "tenant list grants", admin: admins["tenant"].ID, method: http.MethodGet, path: "/api/v1/admin/grants", want: http.StatusNoContent},
		{name: "tenant list sessions", admin: admins["tenant"].ID, method: http.MethodGet, path: "/api/v1/admin/sessions", want: http.StatusNoContent},
		{name: "tenant group membership", admin: admins["tenant"].ID, method: http.MethodPost, path: "/api/v1/admin/groups/7/members", want: http.StatusNoContent},
		{name: "tenant member directory", admin: admins["tenant"].ID, method: http.MethodGet, path: "/api/v1/admin/tenants/tenant-a/members", want: http.StatusNoContent},
		{name: "tenant disable member", admin: admins["tenant"].ID, method: http.MethodPost, path: "/api/v1/admin/tenants/tenant-a/members/7/disable", want: http.StatusNoContent},
		{name: "tenant member devices", admin: admins["tenant"].ID, method: http.MethodGet, path: "/api/v1/admin/tenants/tenant-a/member-devices", want: http.StatusNoContent},
		{name: "tenant audit logs", admin: admins["tenant"].ID, method: http.MethodGet, path: "/api/v1/admin/tenants/tenant-a/audit-logs", want: http.StatusNoContent},
		{name: "tenant settings read", admin: admins["tenant"].ID, method: http.MethodGet, path: "/api/v1/admin/tenants/tenant-a/settings", want: http.StatusNoContent},
		{name: "tenant settings write", admin: admins["tenant"].ID, method: http.MethodPut, path: "/api/v1/admin/tenants/tenant-a/settings", want: http.StatusNoContent},
		{name: "tenant overview", admin: admins["tenant"].ID, method: http.MethodGet, path: "/api/v1/admin/tenants/tenant-a/overview", want: http.StatusNoContent},
		{name: "tenant platform overview denied", admin: admins["tenant"].ID, method: http.MethodGet, path: "/api/v1/admin/overview/platform", want: http.StatusForbidden},
		{name: "tenant context catalog", admin: admins["tenant"].ID, method: http.MethodGet, path: "/api/v1/admin/tenant-contexts", want: http.StatusNoContent},
		{name: "tenant context detail", admin: admins["tenant"].ID, method: http.MethodGet, path: "/api/v1/admin/tenant-contexts/tenant-a", want: http.StatusNoContent},
		{name: "canonical none tenant context", admin: admins["none"].ID, method: http.MethodGet, path: "/api/v1/admin/tenant-contexts", want: http.StatusNoContent},
		{name: "canonical none platform admins denied", admin: admins["none"].ID, method: http.MethodGet, path: "/api/v1/admin/platform-admins", want: http.StatusForbidden},
		{name: "tenant context write denied", admin: admins["tenant"].ID, method: http.MethodPost, path: "/api/v1/admin/tenant-contexts", want: http.StatusForbidden},
		{name: "tenant cannot create tenant", admin: admins["tenant"].ID, method: http.MethodPost, path: "/api/v1/admin/tenants", want: http.StatusForbidden},
		{name: "tenant cannot manage admin memberships", admin: admins["tenant"].ID, method: http.MethodGet, path: "/api/v1/admin/tenant-admin-memberships", want: http.StatusForbidden},
		{name: "tenant legacy users denied", admin: admins["tenant"].ID, method: http.MethodGet, path: "/api/v1/admin/users", want: http.StatusForbidden},
		{name: "tenant candidates denied", admin: admins["tenant"].ID, method: http.MethodGet, path: "/api/v1/admin/resource-candidates", want: http.StatusForbidden},
		{name: "tenant legacy claims denied", admin: admins["tenant"].ID, method: http.MethodGet, path: "/api/v1/admin/legacy-resource-claims", want: http.StatusForbidden},
		{name: "tenant legacy resource discovery denied", admin: admins["tenant"].ID, method: http.MethodGet, path: "/api/v1/admin/resources/k8s-services", want: http.StatusForbidden},
		{name: "localhost debug compatibility", admin: 0, method: http.MethodDelete, path: "/api/v1/admin/nodes/1", want: http.StatusNoContent},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set("X-Test-Admin-ID", strconv.FormatInt(tc.admin, 10))
			resp := httptest.NewRecorder()
			router.ServeHTTP(resp, req)
			require.Equal(t, tc.want, resp.Code)
		})
	}
}

func TestManagementAuthorizationWithSignedJWT(t *testing.T) {
	oldDB := db.DB
	t.Cleanup(func() { db.DB = oldDB })
	database, err := gorm.Open(sqlite.Open("file:management_authorization_jwt_test?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	db.DB = database
	require.NoError(t, database.AutoMigrate(&model.Admin{}))
	platform := model.Admin{Username: "jwt-platform", PasswordHash: "test", Role: "admin"}
	tenantAdmin := model.Admin{Username: "jwt-tenant", PasswordHash: "test", Role: "tenant_admin"}
	require.NoError(t, database.Create(&platform).Error)
	require.NoError(t, database.Create(&tenantAdmin).Error)

	const secret = "management-role-test-secret"
	router := gin.New()
	router.Use(AuthMiddleware(secret, false), ManagementAuthorizationMiddleware())
	router.POST("/api/v1/admin/users", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	tokenFor := func(admin model.Admin) string {
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"admin_id": admin.ID, "username": admin.Username, "exp": time.Now().Add(time.Hour).Unix(),
		})
		signed, err := token.SignedString([]byte(secret))
		require.NoError(t, err)
		return signed
	}
	request := func(admin model.Admin) int {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users", nil)
		req.RemoteAddr = "192.0.2.10:12345"
		req.Header.Set("Authorization", "Bearer "+tokenFor(admin))
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)
		return resp.Code
	}
	require.Equal(t, http.StatusNoContent, request(platform))
	require.Equal(t, http.StatusForbidden, request(tenantAdmin))
}

func TestLocalhostAdminDebugRequiresExplicitEnablement(t *testing.T) {
	request := func(enabled bool) int {
		router := gin.New()
		router.Use(AuthMiddleware("localhost-debug-test", enabled))
		router.GET("/api/v1/admin/ping", func(c *gin.Context) { c.Status(http.StatusNoContent) })
		req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/ping", nil)
		req.RemoteAddr = "127.0.0.1:12345"
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)
		return resp.Code
	}
	require.Equal(t, http.StatusUnauthorized, request(false))
	require.Equal(t, http.StatusNoContent, request(true))
}
