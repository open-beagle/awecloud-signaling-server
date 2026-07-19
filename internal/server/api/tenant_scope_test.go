package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func TestTenantScopeSeparatesPlatformViewerAndTenantAdmin(t *testing.T) {
	oldDB := db.DB
	t.Cleanup(func() { db.DB = oldDB })
	database, err := gorm.Open(sqlite.Open("file:tenant_scope_test?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	db.DB = database
	require.NoError(t, database.AutoMigrate(&model.Admin{}, &model.AdminTenantMembership{}))

	platform := model.Admin{Username: "platform", PasswordHash: "test", Role: "admin"}
	viewer := model.Admin{Username: "viewer", PasswordHash: "test", Role: "viewer"}
	tenantAdmin := model.Admin{Username: "tenant-admin", PasswordHash: "test", Role: "tenant_admin"}
	require.NoError(t, database.Create(&platform).Error)
	require.NoError(t, database.Create(&viewer).Error)
	require.NoError(t, database.Create(&tenantAdmin).Error)
	tenantA, tenantB := uuid.NewString(), uuid.NewString()
	require.NoError(t, database.Create(&model.AdminTenantMembership{AdminID: tenantAdmin.ID, TenantID: tenantA, Role: "tenant_admin", Enabled: true}).Error)

	contextFor := func(adminID int64, tenantID string) (*gin.Context, *httptest.ResponseRecorder) {
		response := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(response)
		ctx.Request = httptest.NewRequest("GET", "/", nil)
		ctx.Set("admin_id", adminID)
		if tenantID != "" {
			ctx.Request.Header.Set("X-Tenant-ID", tenantID)
		}
		return ctx, response
	}

	ctx, _ := contextFor(platform.ID, "")
	require.False(t, requireTenantAccess(ctx, tenantA, true), "platform writes require explicit Tenant context")
	ctx, _ = contextFor(platform.ID, tenantA)
	require.True(t, requireTenantAccess(ctx, tenantA, true))
	ctx, _ = contextFor(viewer.ID, tenantA)
	require.False(t, requireTenantAccess(ctx, tenantA, true))
	require.True(t, requireTenantAccess(ctx, tenantA, false))
	ctx, _ = contextFor(tenantAdmin.ID, tenantA)
	require.True(t, requireTenantAccess(ctx, tenantA, true))
	ctx, _ = contextFor(tenantAdmin.ID, tenantB)
	require.False(t, requireTenantAccess(ctx, tenantB, false))

	ctx, _ = contextFor(tenantAdmin.ID, "")
	ids, unrestricted, ok := tenantReadScope(ctx)
	require.True(t, ok)
	require.False(t, unrestricted)
	require.Equal(t, []string{tenantA}, ids)
}

func TestTenantMemberListDoesNotExposeAnotherTenant(t *testing.T) {
	oldDB := db.DB
	t.Cleanup(func() { db.DB = oldDB })
	database, err := gorm.Open(sqlite.Open("file:tenant_member_scope_test?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	db.DB = database
	require.NoError(t, database.AutoMigrate(&model.Admin{}, &model.AdminTenantMembership{}, &model.User{}, &model.TenantMembership{}))

	admin := model.Admin{Username: "tenant-admin-members", PasswordHash: "test", Role: "tenant_admin"}
	require.NoError(t, database.Create(&admin).Error)
	tenantA, tenantB := uuid.NewString(), uuid.NewString()
	require.NoError(t, database.Create(&model.AdminTenantMembership{AdminID: admin.ID, TenantID: tenantA, Role: "tenant_admin", Enabled: true}).Error)
	userA := model.User{Name: "tenant-a-user", Role: model.UserRoleClient, SecretHash: "test", Enabled: true}
	userB := model.User{Name: "tenant-b-user", Role: model.UserRoleClient, SecretHash: "test", Enabled: true}
	require.NoError(t, database.Create(&userA).Error)
	require.NoError(t, database.Create(&userB).Error)
	require.NoError(t, database.Create(&model.TenantMembership{TenantID: tenantA, UserID: userA.ID, Enabled: true}).Error)
	require.NoError(t, database.Create(&model.TenantMembership{TenantID: tenantB, UserID: userB.ID, Enabled: true}).Error)

	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("admin_id", admin.ID) })
	router.GET("/tenants/:id/members", NewUnifiedResourceAPI().ListTenantMembers)

	allowedReq := httptest.NewRequest(http.MethodGet, "/tenants/"+tenantA+"/members", nil)
	allowedReq.Header.Set("X-Tenant-ID", tenantA)
	allowedResp := httptest.NewRecorder()
	router.ServeHTTP(allowedResp, allowedReq)
	require.Equal(t, http.StatusOK, allowedResp.Code)
	var body struct {
		Data []tenantMemberListItem `json:"data"`
	}
	require.NoError(t, json.Unmarshal(allowedResp.Body.Bytes(), &body))
	require.Len(t, body.Data, 1)
	require.Equal(t, userA.ID, body.Data[0].UserID)

	deniedReq := httptest.NewRequest(http.MethodGet, "/tenants/"+tenantB+"/members", nil)
	deniedReq.Header.Set("X-Tenant-ID", tenantB)
	deniedResp := httptest.NewRecorder()
	router.ServeHTTP(deniedResp, deniedReq)
	require.Equal(t, http.StatusForbidden, deniedResp.Code)
}
