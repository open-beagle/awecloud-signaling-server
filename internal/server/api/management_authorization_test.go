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
		{name: "tenant cannot create tenant", admin: admins["tenant"].ID, method: http.MethodPost, path: "/api/v1/admin/tenants", want: http.StatusForbidden},
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
	router.Use(AuthMiddleware(secret), ManagementAuthorizationMiddleware())
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
