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
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func TestTenantPermissionRevalidatesRoleStatusAndContextEveryRequest(t *testing.T) {
	oldDB := db.DB
	t.Cleanup(func() { db.DB = oldDB })
	database, err := gorm.Open(sqlite.Open("file:tenant_permission_revalidation_test?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	db.DB = database
	require.NoError(t, database.AutoMigrate(&model.Admin{}, &model.AdminTenantMembership{}, &model.Tenant{}))

	admin := model.Admin{Username: "auditor-revalidation", PasswordHash: "test", Role: "admin", Enabled: true}
	active := model.Tenant{ID: uuid.NewString(), Key: "permission-active", Name: "Permission Active", Status: model.TenantStatusActive}
	suspended := model.Tenant{ID: uuid.NewString(), Key: "permission-suspended", Name: "Permission Suspended", Status: model.TenantStatusSuspended}
	require.NoError(t, database.Create(&admin).Error)
	require.NoError(t, database.Create(&active).Error)
	require.NoError(t, database.Create(&suspended).Error)
	activeMembership := model.AdminTenantMembership{AdminID: admin.ID, TenantID: active.ID, Role: string(model.TenantManagementRoleSecurityAuditor), Enabled: true}
	suspendedMembership := model.AdminTenantMembership{AdminID: admin.ID, TenantID: suspended.ID, Role: string(model.TenantManagementRoleAdmin), Enabled: true}
	require.NoError(t, database.Create(&activeMembership).Error)
	require.NoError(t, database.Create(&suspendedMembership).Error)

	contextFor := func(headerTenantID, queryTenantID string) (*gin.Context, *httptest.ResponseRecorder) {
		response := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(response)
		path := "/"
		if queryTenantID != "" {
			path += "?tenant_id=" + queryTenantID
		}
		ctx.Request = httptest.NewRequest(http.MethodGet, path, nil)
		ctx.Set("admin_id", admin.ID)
		if headerTenantID != "" {
			ctx.Request.Header.Set("X-Tenant-ID", headerTenantID)
		}
		return ctx, response
	}
	assertCode := func(response *httptest.ResponseRecorder, expected string) {
		var body Response
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
		require.Equal(t, expected, body.Code)
		require.NotEmpty(t, body.RequestID)
	}

	ctx, _ := contextFor(active.ID, "")
	require.True(t, requireTenantPermission(ctx, active.ID, PermissionTenantResourcesRead))
	ctx, response := contextFor(active.ID, "")
	require.False(t, requireTenantPermission(ctx, active.ID, PermissionTenantMembersRead))
	require.Equal(t, http.StatusForbidden, response.Code)
	assertCode(response, ErrorCodeTenantPermissionDenied)

	ctx, response = contextFor(active.ID, suspended.ID)
	require.False(t, requireTenantPermission(ctx, active.ID, PermissionTenantResourcesRead))
	require.Equal(t, http.StatusBadRequest, response.Code)
	assertCode(response, ErrorCodeTenantContextConflict)

	ctx, _ = contextFor(suspended.ID, "")
	require.True(t, requireTenantPermission(ctx, suspended.ID, PermissionTenantResourcesRead))
	ctx, response = contextFor(suspended.ID, "")
	require.False(t, requireTenantPermission(ctx, suspended.ID, PermissionTenantResourcesWrite))
	require.Equal(t, http.StatusConflict, response.Code)
	assertCode(response, ErrorCodeTenantSuspended)

	require.NoError(t, database.Model(&activeMembership).Update("enabled", false).Error)
	ctx, response = contextFor(active.ID, "")
	require.False(t, requireTenantPermission(ctx, active.ID, PermissionTenantResourcesRead), "revocation must take effect on the next request")
	assertCode(response, ErrorCodeTenantContextUnavailable)

	expired := time.Now().Add(-time.Minute)
	require.NoError(t, database.Model(&activeMembership).Updates(map[string]interface{}{"enabled": true, "expires_at": expired}).Error)
	ctx, response = contextFor(active.ID, "")
	require.False(t, requireTenantPermission(ctx, active.ID, PermissionTenantResourcesRead))
	assertCode(response, ErrorCodeTenantContextUnavailable)
}

func TestTenantScopeSeparatesPlatformViewerAndTenantAdmin(t *testing.T) {
	oldDB := db.DB
	t.Cleanup(func() { db.DB = oldDB })
	database, err := gorm.Open(sqlite.Open("file:tenant_scope_test?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	db.DB = database
	require.NoError(t, database.AutoMigrate(&model.Admin{}, &model.AdminTenantMembership{}, &model.Tenant{}))

	platform := model.Admin{Username: "platform", PasswordHash: "test", Role: "admin"}
	viewer := model.Admin{Username: "viewer", PasswordHash: "test", Role: "viewer"}
	tenantAdmin := model.Admin{Username: "tenant-admin", PasswordHash: "test", Role: "tenant_admin"}
	require.NoError(t, database.Create(&platform).Error)
	require.NoError(t, database.Create(&viewer).Error)
	require.NoError(t, database.Create(&tenantAdmin).Error)
	tenantA := model.Tenant{ID: uuid.NewString(), Key: "scope-a", Name: "Scope A", Status: model.TenantStatusActive}
	tenantB := model.Tenant{ID: uuid.NewString(), Key: "scope-b", Name: "Scope B", Status: model.TenantStatusActive}
	require.NoError(t, database.Create(&tenantA).Error)
	require.NoError(t, database.Create(&tenantB).Error)
	require.NoError(t, database.Create(&model.AdminTenantMembership{AdminID: platform.ID, TenantID: tenantA.ID, Role: string(model.TenantManagementRoleAdmin), Enabled: true}).Error)
	require.NoError(t, database.Create(&model.AdminTenantMembership{AdminID: viewer.ID, TenantID: tenantA.ID, Role: string(model.TenantManagementRoleViewer), Enabled: true}).Error)
	require.NoError(t, database.Create(&model.AdminTenantMembership{AdminID: tenantAdmin.ID, TenantID: tenantA.ID, Role: string(model.TenantManagementRoleAdmin), Enabled: true}).Error)

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
	require.False(t, requireTenantAccess(ctx, tenantA.ID, true), "platform writes require explicit Tenant context")
	ctx, _ = contextFor(platform.ID, tenantA.ID)
	require.True(t, requireTenantAccess(ctx, tenantA.ID, true))
	ctx, _ = contextFor(platform.ID, tenantB.ID)
	require.False(t, requireTenantAccess(ctx, tenantB.ID, false), "platform identity alone does not grant tenant access")
	ctx, _ = contextFor(viewer.ID, tenantA.ID)
	require.False(t, requireTenantAccess(ctx, tenantA.ID, true))
	ctx, _ = contextFor(viewer.ID, tenantA.ID)
	require.True(t, requireTenantAccess(ctx, tenantA.ID, false))
	ctx, _ = contextFor(tenantAdmin.ID, tenantA.ID)
	require.True(t, requireTenantAccess(ctx, tenantA.ID, true))
	ctx, _ = contextFor(tenantAdmin.ID, tenantB.ID)
	require.False(t, requireTenantAccess(ctx, tenantB.ID, false))

	ctx, _ = contextFor(tenantAdmin.ID, "")
	ids, unrestricted, ok := tenantReadScope(ctx, PermissionTenantOverviewRead)
	require.True(t, ok)
	require.False(t, unrestricted)
	require.Equal(t, []string{tenantA.ID}, ids)
}

func TestTenantMemberListDoesNotExposeAnotherTenant(t *testing.T) {
	oldDB := db.DB
	t.Cleanup(func() { db.DB = oldDB })
	database, err := gorm.Open(sqlite.Open("file:tenant_member_scope_test?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	db.DB = database
	require.NoError(t, database.AutoMigrate(&model.Admin{}, &model.AdminTenantMembership{}, &model.User{}, &model.Tenant{}, &model.TenantMembership{}))

	admin := model.Admin{Username: "tenant-admin-members", PasswordHash: "test", Role: "tenant_admin"}
	require.NoError(t, database.Create(&admin).Error)
	tenantA := model.Tenant{ID: uuid.NewString(), Key: "member-scope-a", Name: "Member Scope A", Status: model.TenantStatusActive}
	tenantB := model.Tenant{ID: uuid.NewString(), Key: "member-scope-b", Name: "Member Scope B", Status: model.TenantStatusActive}
	require.NoError(t, database.Create(&tenantA).Error)
	require.NoError(t, database.Create(&tenantB).Error)
	require.NoError(t, database.Create(&model.AdminTenantMembership{AdminID: admin.ID, TenantID: tenantA.ID, Role: "tenant_admin", Enabled: true}).Error)
	userA := model.User{Name: "tenant-a-user", Role: model.UserRoleClient, SecretHash: "test", Enabled: true}
	userB := model.User{Name: "tenant-b-user", Role: model.UserRoleClient, SecretHash: "test", Enabled: true}
	require.NoError(t, database.Create(&userA).Error)
	require.NoError(t, database.Create(&userB).Error)
	require.NoError(t, database.Create(&model.TenantMembership{TenantID: tenantA.ID, UserID: userA.ID, Enabled: true}).Error)
	require.NoError(t, database.Create(&model.TenantMembership{TenantID: tenantB.ID, UserID: userB.ID, Enabled: true}).Error)

	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("admin_id", admin.ID) })
	router.GET("/tenants/:id/members", NewUnifiedResourceAPI().ListTenantMembers)
	router.POST("/tenants/:id/members", NewUnifiedResourceAPI().AddTenantMember)

	legacyRoleReq := httptest.NewRequest(http.MethodPost, "/tenants/"+tenantA.ID+"/members", bytes.NewBufferString(`{"user_id":`+strconv.FormatUint(userA.ID, 10)+`,"role":"tenant_admin"}`))
	legacyRoleReq.Header.Set("Content-Type", "application/json")
	legacyRoleReq.Header.Set("X-Tenant-ID", tenantA.ID)
	legacyRoleResp := httptest.NewRecorder()
	router.ServeHTTP(legacyRoleResp, legacyRoleReq)
	require.Equal(t, http.StatusBadRequest, legacyRoleResp.Code)

	allowedReq := httptest.NewRequest(http.MethodGet, "/tenants/"+tenantA.ID+"/members", nil)
	allowedReq.Header.Set("X-Tenant-ID", tenantA.ID)
	allowedResp := httptest.NewRecorder()
	router.ServeHTTP(allowedResp, allowedReq)
	require.Equal(t, http.StatusOK, allowedResp.Code)
	var body struct {
		Data []tenantMemberListItem `json:"data"`
	}
	require.NoError(t, json.Unmarshal(allowedResp.Body.Bytes(), &body))
	require.Len(t, body.Data, 1)
	require.Equal(t, userA.ID, body.Data[0].UserID)

	deniedReq := httptest.NewRequest(http.MethodGet, "/tenants/"+tenantB.ID+"/members", nil)
	deniedReq.Header.Set("X-Tenant-ID", tenantB.ID)
	deniedResp := httptest.NewRecorder()
	router.ServeHTTP(deniedResp, deniedReq)
	require.Equal(t, http.StatusForbidden, deniedResp.Code)
}

func TestTenantMemberDisableRestorePreservesMembership(t *testing.T) {
	oldDB := db.DB
	t.Cleanup(func() { db.DB = oldDB })
	database, err := gorm.Open(sqlite.Open("file:tenant_member_lifecycle_test?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	db.DB = database
	require.NoError(t, database.AutoMigrate(&model.Admin{}, &model.AdminTenantMembership{}, &model.User{}, &model.Tenant{}, &model.TenantMembership{}, &model.AuditLog{}))

	tenantA := model.Tenant{ID: uuid.NewString(), Key: "tenant-lifecycle-a", Name: "Tenant Lifecycle A", Status: model.TenantStatusActive}
	tenantB := model.Tenant{ID: uuid.NewString(), Key: "tenant-lifecycle-b", Name: "Tenant Lifecycle B", Status: model.TenantStatusActive}
	admin := model.Admin{Username: "tenant-member-lifecycle", PasswordHash: "test", Role: "tenant_admin"}
	user := model.User{Name: "lifecycle-user", Role: model.UserRoleClient, SecretHash: "test", Enabled: true}
	require.NoError(t, database.Create(&tenantA).Error)
	require.NoError(t, database.Create(&tenantB).Error)
	require.NoError(t, database.Create(&admin).Error)
	require.NoError(t, database.Create(&user).Error)
	require.NoError(t, database.Create(&model.AdminTenantMembership{AdminID: admin.ID, TenantID: tenantA.ID, Role: "tenant_admin", Enabled: true}).Error)
	require.NoError(t, database.Create(&model.TenantMembership{TenantID: tenantA.ID, UserID: user.ID, Role: "member", Enabled: true}).Error)

	api := NewUnifiedResourceAPI()
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("admin_id", admin.ID) })
	router.POST("/tenants/:id/members", api.AddTenantMember)
	router.POST("/tenants/:id/members/:user_id/disable", api.DisableTenantMember)

	disableReq := httptest.NewRequest(http.MethodPost, "/tenants/"+tenantA.ID+"/members/"+strconv.FormatUint(user.ID, 10)+"/disable", nil)
	disableReq.Header.Set("X-Tenant-ID", tenantA.ID)
	disableResp := httptest.NewRecorder()
	router.ServeHTTP(disableResp, disableReq)
	require.Equal(t, http.StatusOK, disableResp.Code)
	var membership model.TenantMembership
	require.NoError(t, database.First(&membership, "tenant_id = ? AND user_id = ?", tenantA.ID, user.ID).Error)
	require.False(t, membership.Enabled)
	var membershipCount int64
	require.NoError(t, database.Model(&model.TenantMembership{}).Where("tenant_id = ? AND user_id = ?", tenantA.ID, user.ID).Count(&membershipCount).Error)
	require.Equal(t, int64(1), membershipCount)

	crossTenantReq := httptest.NewRequest(http.MethodPost, "/tenants/"+tenantB.ID+"/members/"+strconv.FormatUint(user.ID, 10)+"/disable", nil)
	crossTenantReq.Header.Set("X-Tenant-ID", tenantB.ID)
	crossTenantResp := httptest.NewRecorder()
	router.ServeHTTP(crossTenantResp, crossTenantReq)
	require.Equal(t, http.StatusForbidden, crossTenantResp.Code)

	restoreBody := bytes.NewBufferString(`{"user_id":` + strconv.FormatUint(user.ID, 10) + `,"role":"member"}`)
	restoreReq := httptest.NewRequest(http.MethodPost, "/tenants/"+tenantA.ID+"/members", restoreBody)
	restoreReq.Header.Set("Content-Type", "application/json")
	restoreReq.Header.Set("X-Tenant-ID", tenantA.ID)
	restoreResp := httptest.NewRecorder()
	router.ServeHTTP(restoreResp, restoreReq)
	require.Equal(t, http.StatusCreated, restoreResp.Code)
	require.NoError(t, database.First(&membership, "tenant_id = ? AND user_id = ?", tenantA.ID, user.ID).Error)
	require.True(t, membership.Enabled)
	var audit model.AuditLog
	require.NoError(t, database.Where("action_type = ?", "disable_tenant_member").First(&audit).Error)
	require.Equal(t, admin.ID, audit.ActorAdminID)
	require.Equal(t, admin.Username, audit.ActorUsername)
	require.Equal(t, tenantA.ID, audit.TenantID)
	require.Equal(t, string(model.TenantManagementRoleAdmin), audit.TenantRole)
	require.Equal(t, PermissionTenantMembersWrite, audit.RequiredPermission)
	require.Equal(t, int64(1), audit.PermissionRevision)
	require.NotEmpty(t, audit.RequestID)
	require.NotEmpty(t, audit.SourceIP)
}
