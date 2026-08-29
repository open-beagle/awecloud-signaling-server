package api

import (
	"bytes"
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

func TestTenantResourceScopeRejectsCrossObjectRequestAndQueryInputs(t *testing.T) {
	oldDB := db.DB
	t.Cleanup(func() { db.DB = oldDB })
	database, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	db.DB = database
	require.NoError(t, database.AutoMigrate(
		&model.Admin{}, &model.AdminTenantMembership{}, &model.User{}, &model.Tenant{}, &model.TenantMembership{},
		&model.Group{}, &model.Resource{}, &model.ResourceTarget{}, &model.AccessGrant{}, &model.ContainerSession{},
		&model.WorkspaceBinding{}, &model.AuditLog{},
	))

	tenantA := model.Tenant{ID: uuid.NewString(), Key: "scope-a", Name: "Scope A", Status: model.TenantStatusActive}
	tenantB := model.Tenant{ID: uuid.NewString(), Key: "scope-b", Name: "Scope B", Status: model.TenantStatusActive}
	adminA := model.Admin{Username: "scope-admin-a", PasswordHash: "test", Role: "tenant_admin", Enabled: true}
	userA := model.User{Name: "scope-user-a", Alias: "Scope User A", Role: model.UserRoleClient, SecretHash: "test", Enabled: true}
	userB := model.User{Name: "scope-user-b-private", Alias: "Scope User B Private", Role: model.UserRoleClient, SecretHash: "test", Enabled: true}
	for _, value := range []any{&tenantA, &tenantB, &adminA, &userA, &userB} {
		require.NoError(t, database.Create(value).Error)
	}
	require.NoError(t, database.Create(&model.AdminTenantMembership{
		AdminID: adminA.ID, TenantID: tenantA.ID, Role: string(model.TenantManagementRoleAdmin), Enabled: true, PermissionRevision: 5,
	}).Error)
	require.NoError(t, database.Create(&model.TenantMembership{TenantID: tenantA.ID, UserID: userA.ID, Role: "member", Enabled: true}).Error)
	require.NoError(t, database.Create(&model.TenantMembership{TenantID: tenantB.ID, UserID: userB.ID, Role: "member", Enabled: true}).Error)

	resourceA1 := model.Resource{ID: uuid.NewString(), TenantID: tenantA.ID, Type: model.ResourceTypeContainerSSH, DisplayName: "Scope A Resource 1", State: model.ResourceStateAvailable}
	resourceA2 := model.Resource{ID: uuid.NewString(), TenantID: tenantA.ID, Type: model.ResourceTypeContainerSSH, DisplayName: "Scope A Resource 2", State: model.ResourceStateAvailable}
	resourceB := model.Resource{ID: uuid.NewString(), TenantID: tenantB.ID, Type: model.ResourceTypeContainerSSH, DisplayName: "Scope B Private Resource", State: model.ResourceStateAvailable}
	for _, resource := range []*model.Resource{&resourceA1, &resourceA2, &resourceB} {
		require.NoError(t, database.Create(resource).Error)
	}
	grantB := model.AccessGrant{
		ID: uuid.NewString(), TenantID: tenantB.ID, ResourceID: resourceB.ID, SubjectType: "user", SubjectUserID: userB.ID,
		Actions: `["shell"]`, ValidFrom: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Hour),
		MaxSessionSeconds: 3600, Revision: 1, Status: "enabled",
	}
	require.NoError(t, database.Create(&grantB).Error)
	sessionB := model.ContainerSession{
		ID: uuid.NewString(), TenantID: tenantB.ID, UserID: userB.ID, ActorUserID: userB.ID, EffectiveUserID: userB.ID,
		ResourceID: resourceB.ID, Status: model.ContainerSessionActive, StartedAt: time.Now(),
	}
	require.NoError(t, database.Create(&sessionB).Error)
	require.NoError(t, database.Create(&model.AuditLog{
		TenantID: tenantB.ID, ActionType: "scope_b_private_action", TargetType: "resource", TargetID: resourceB.ID,
		TargetName: resourceB.DisplayName, RequestID: "scope-b-private-request",
	}).Error)

	resourceAPI := NewUnifiedResourceAPI()
	sessionAPI := NewContainerSessionAPI()
	tenantBusinessAPI := NewTenantBusinessAPI()
	router := gin.New()
	router.Use(RequestMetadataMiddleware())
	router.Use(func(c *gin.Context) { c.Set("admin_id", adminA.ID) })
	router.GET("/resources", resourceAPI.List)
	router.GET("/resources/summary", resourceAPI.Summary)
	router.GET("/resources/:id", resourceAPI.Get)
	router.GET("/resources/:id/events", resourceAPI.ListEvents)
	router.GET("/resources/:id/grants", resourceAPI.ListGrants)
	router.POST("/resources/:id/grants", resourceAPI.CreateGrant)
	router.POST("/resources/:id/targets", resourceAPI.ObserveTarget)
	router.GET("/grants", resourceAPI.ListAllGrants)
	router.POST("/grants/:id/revoke", resourceAPI.RevokeGrant)
	router.GET("/sessions", sessionAPI.List)
	router.GET("/sessions/:id", sessionAPI.Get)
	router.POST("/sessions/:id/revoke", sessionAPI.Revoke)
	router.GET("/tenants/:id/audit-logs", tenantBusinessAPI.ListAuditLogs)

	request := func(method, path string, body []byte) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		req.Header.Set("X-Tenant-ID", tenantA.ID)
		req.Header.Set(HeaderRequestID, "scope-b-private-request")
		if len(body) > 0 {
			req.Header.Set("Content-Type", "application/json")
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}
	assertEmptyPage := func(response *httptest.ResponseRecorder) {
		t.Helper()
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var body struct {
			Total int64             `json:"total"`
			Data  []json.RawMessage `json:"data"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
		require.Zero(t, body.Total)
		require.Empty(t, body.Data)
		require.NotContains(t, response.Body.String(), resourceB.DisplayName)
	}
	assertScopedNotFound := func(response *httptest.ResponseRecorder) {
		t.Helper()
		require.Equal(t, http.StatusNotFound, response.Code, response.Body.String())
		var body Response
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
		require.Equal(t, ErrorCodeTenantObjectNotFound, body.Code)
		require.Equal(t, "scope-b-private-request", body.RequestID)
		require.NotContains(t, response.Body.String(), resourceB.DisplayName)
		require.NotContains(t, response.Body.String(), "scope_b_private_action")
	}

	page := request(http.MethodGet, "/resources?page=1&size=1", nil)
	require.Equal(t, http.StatusOK, page.Code, page.Body.String())
	var pageBody struct {
		Total int64              `json:"total"`
		Data  []resourceListItem `json:"data"`
	}
	require.NoError(t, json.Unmarshal(page.Body.Bytes(), &pageBody))
	require.Equal(t, int64(2), pageBody.Total)
	require.Len(t, pageBody.Data, 1)
	require.Equal(t, tenantA.ID, pageBody.Data[0].TenantID)

	assertEmptyPage(request(http.MethodGet, "/resources?search=Scope+B+Private", nil))
	assertEmptyPage(request(http.MethodGet, "/grants?resource_id="+resourceB.ID, nil))
	assertEmptyPage(request(http.MethodGet, "/sessions?resource_id="+resourceB.ID, nil))
	assertEmptyPage(request(http.MethodGet, "/tenants/"+tenantA.ID+"/audit-logs?search=scope-b-private-request&page=1&size=1", nil))

	for _, response := range []*httptest.ResponseRecorder{
		request(http.MethodGet, "/resources/"+resourceB.ID, nil),
		request(http.MethodGet, "/resources/"+resourceB.ID+"/events?page=1&size=1", nil),
		request(http.MethodGet, "/resources/"+resourceB.ID+"/grants", nil),
		request(http.MethodPost, "/resources/"+resourceB.ID+"/grants", []byte(`{"subject_user_id":1}`)),
		request(http.MethodPost, "/resources/"+resourceB.ID+"/targets", []byte(`{}`)),
		request(http.MethodPost, "/grants/"+grantB.ID+"/revoke", nil),
		request(http.MethodGet, "/sessions/"+sessionB.ID, nil),
		request(http.MethodPost, "/sessions/"+sessionB.ID+"/revoke", nil),
	} {
		assertScopedNotFound(response)
	}

	require.NoError(t, database.First(&grantB, "id = ?", grantB.ID).Error)
	require.Equal(t, "enabled", grantB.Status)
	require.NoError(t, database.First(&sessionB, "id = ?", sessionB.ID).Error)
	require.Equal(t, model.ContainerSessionActive, sessionB.Status)
}
