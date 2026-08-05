package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/service"
)

type managementContextAPIFixture struct {
	database      *gorm.DB
	config        *config.ServerConfig
	router        *gin.Engine
	admin         model.Admin
	unmappedAdmin model.Admin
	user          model.User
	provider      model.ResourceProvider
	otherProvider model.ResourceProvider
	tenant        model.Tenant
}

func newManagementContextAPIFixture(t *testing.T) managementContextAPIFixture {
	t.Helper()
	oldDB := db.DB
	t.Cleanup(func() { db.DB = oldDB })
	database, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared&_pragma=foreign_keys(1)", uuid.NewString())), &gorm.Config{})
	require.NoError(t, err)
	db.DB = database
	require.NoError(t, database.AutoMigrate(
		&model.Admin{}, &model.AdminTenantMembership{}, &model.User{}, &model.UserIdentityProfile{}, &model.UserAuthenticationLink{},
		&model.PlatformRoleMembership{}, &model.ResourceProvider{}, &model.AdminProviderMembership{},
		&model.Tenant{}, &model.TenantMembership{}, &model.UserTenantManagementMembership{},
		&model.UserSimulationSession{}, &model.APIIdempotencyRecord{}, &model.AuditLog{},
	))
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("fixture-password"), bcrypt.MinCost)
	require.NoError(t, err)
	admin := model.Admin{Username: "mapped-admin", PasswordHash: string(passwordHash), Role: "admin", Enabled: true}
	unmappedAdmin := model.Admin{Username: "unmapped-admin", PasswordHash: string(passwordHash), Role: "admin", Enabled: true}
	require.NoError(t, database.Create(&admin).Error)
	require.NoError(t, database.Create(&unmappedAdmin).Error)
	user := model.User{Name: "unified-management-user", Role: model.UserRoleClient, SecretHash: "fixture", Enabled: true}
	require.NoError(t, database.Create(&user).Error)
	require.NoError(t, database.Create(&model.UserIdentityProfile{
		UserID: user.ID, Username: "unified-admin", DisplayName: "Unified Admin", Enabled: true, AuthRevision: 4, RowVersion: 1,
	}).Error)
	require.NoError(t, database.Create(&model.UserAuthenticationLink{
		ID: uuid.NewString(), UserID: user.ID, ProviderType: model.AuthenticationProviderLegacyAdmin,
		ProviderSubject: strconv.FormatInt(admin.ID, 10), CredentialRevision: 3, Enabled: true, RowVersion: 1,
	}).Error)
	now := time.Now().Add(-time.Minute)
	require.NoError(t, database.Create(&model.PlatformRoleMembership{
		ID: uuid.NewString(), UserID: user.ID, Role: model.PlatformRoleAdmin, Enabled: true, ValidFrom: now,
		PermissionRevision: 7, CreatedByUserID: user.ID, Reason: "fixture", RowVersion: 1,
	}).Error)
	provider := model.ResourceProvider{ID: uuid.NewString(), Key: "provider-a", DisplayName: "Provider A", DomainScope: model.ProviderDomainNamed, DomainLabel: "provider-a", Status: model.ProviderStatusActive, Revision: 1, RowVersion: 1}
	otherProvider := model.ResourceProvider{ID: uuid.NewString(), Key: "provider-b", DisplayName: "Provider B", DomainScope: model.ProviderDomainNamed, DomainLabel: "provider-b", Status: model.ProviderStatusActive, Revision: 1, RowVersion: 1}
	require.NoError(t, database.Create(&provider).Error)
	require.NoError(t, database.Create(&otherProvider).Error)
	require.NoError(t, database.Create(&model.AdminProviderMembership{
		ID: uuid.NewString(), UserID: user.ID, ProviderID: provider.ID, Role: model.ProviderManagementRoleAdmin,
		Enabled: true, ValidFrom: now, PermissionRevision: 5, CreatedByUserID: user.ID, Reason: "fixture", RowVersion: 1,
	}).Error)
	tenant := model.Tenant{ID: uuid.NewString(), Key: "tenant-a", Name: "Tenant A", Status: model.TenantStatusActive}
	require.NoError(t, database.Create(&tenant).Error)
	require.NoError(t, database.Create(&model.UserTenantManagementMembership{
		ID: uuid.NewString(), UserID: user.ID, TenantID: tenant.ID, Role: model.TenantManagementRoleViewer,
		Enabled: true, ValidFrom: now, PermissionRevision: 2, CreatedByUserID: user.ID, Reason: "fixture", RowVersion: 1,
	}).Error)

	cfg := &config.ServerConfig{Security: config.SecuritySection{JWTSecret: "management-context-test-secret", JWTExpireHours: 1}}
	router := gin.New()
	router.Use(RequestMetadataMiddleware())
	adminAPI := NewAdminAPI(cfg)
	router.POST("/login", adminAPI.Login)
	legacyAuthenticated := router.Group("")
	legacyAuthenticated.Use(AuthMiddleware(cfg.Security.JWTSecret, false))
	legacyAuthenticated.PUT("/password", ForbidUserSimulation(), adminAPI.ChangePassword)
	legacyAuthenticated.POST("/legacy-audit", func(c *gin.Context) {
		recordAuditLog(c.Request.Context(), c, "legacy_business_write", "fixture", "fixture-1", "Fixture", nil)
		c.Status(http.StatusNoContent)
	})
	management := router.Group("/management")
	management.Use(AuthMiddleware(cfg.Security.JWTSecret, false))
	management.Use(UnifiedManagementIdentityMiddleware())
	contextAPI := NewManagementContextAPI()
	management.GET("/contexts", contextAPI.List)
	management.GET("/contexts/current", contextAPI.Current)
	userSimulationAPI := NewUserSimulationAPI(8)
	management.GET("/user-simulations", RequireManagementPermission(service.PermissionPlatformUserSimulationsRead), userSimulationAPI.List)
	management.POST("/user-simulations", ForbidUserSimulation(), RequireManagementPermission(service.PermissionPlatformUserSimulationsWrite), RequireIdempotencyKey(), userSimulationAPI.Create)
	management.POST("/user-simulations/:id/revoke", RequireManagementPermission(service.PermissionPlatformUserSimulationsWrite), RequireIfMatch(), userSimulationAPI.Revoke)
	management.GET("/test/tenant-resources", RequireManagementPermission(service.PermissionTenantResourcesRead), func(c *gin.Context) {
		context, _ := currentManagementAuthorization(c)
		c.JSON(http.StatusOK, NewSuccessResponse(managementContextView(context)))
	})
	management.POST("/test/tenant-resources", RequireManagementPermission(service.PermissionTenantResourcesWrite), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	return managementContextAPIFixture{
		database: database, config: cfg, router: router, admin: admin, unmappedAdmin: unmappedAdmin,
		user: user, provider: provider, otherProvider: otherProvider, tenant: tenant,
	}
}

func (fixture managementContextAPIFixture) login(t *testing.T, username string) LoginResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewBufferString(`{"username":"`+username+`","password":"fixture-password"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	fixture.router.ServeHTTP(resp, req)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	var body struct {
		Data LoginResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	return body.Data
}

func (fixture managementContextAPIFixture) managementRequest(method, path, token string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp := httptest.NewRecorder()
	fixture.router.ServeHTTP(resp, req)
	return resp
}

func TestManagementContextV2UsesExplicitUnifiedIdentityAndScope(t *testing.T) {
	fixture := newManagementContextAPIFixture(t)
	login := fixture.login(t, fixture.admin.Username)
	require.Equal(t, fixture.user.ID, login.Admin.UnifiedUserID)
	require.Equal(t, int64(4), login.Admin.AuthRevision)
	require.Equal(t, int64(3), login.Admin.CredentialRevision)

	list := fixture.managementRequest(http.MethodGet, "/management/contexts", login.Token, nil)
	require.Equal(t, http.StatusOK, list.Code, list.Body.String())
	var listBody struct {
		Data []managementContextResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(list.Body.Bytes(), &listBody))
	require.Len(t, listBody.Data, 3)

	current := fixture.managementRequest(http.MethodGet, "/management/contexts/current", login.Token, map[string]string{
		HeaderManagementScopeType: string(model.ManagementScopeProvider), HeaderManagementScopeID: fixture.provider.ID,
	})
	require.Equal(t, http.StatusOK, current.Code, current.Body.String())
	var currentBody struct {
		Data managementContextResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(current.Body.Bytes(), &currentBody))
	require.Equal(t, fixture.provider.ID, currentBody.Data.ScopeID)
	require.Contains(t, currentBody.Data.Permissions, "provider.resources.write")
}

func TestManagementContextV2RejectsCrossScopeIDsAndLegacyHeader(t *testing.T) {
	fixture := newManagementContextAPIFixture(t)
	login := fixture.login(t, fixture.admin.Username)

	requestID := "request-from-provider-a"
	crossScope := fixture.managementRequest(http.MethodGet, "/management/contexts/current", login.Token, map[string]string{
		HeaderManagementScopeType: string(model.ManagementScopeProvider),
		HeaderManagementScopeID:   fixture.otherProvider.ID,
		HeaderRequestID:           requestID,
	})
	require.Equal(t, http.StatusNotFound, crossScope.Code, crossScope.Body.String())
	var errorBody Response
	require.NoError(t, json.Unmarshal(crossScope.Body.Bytes(), &errorBody))
	require.Equal(t, ErrorCodeManagementObjectMissing, errorBody.Code)
	require.Equal(t, requestID, errorBody.RequestID)

	legacyProviderID := fixture.provider.Key
	require.NotEqual(t, fixture.provider.ID, legacyProviderID)
	legacyScope := fixture.managementRequest(http.MethodGet, "/management/contexts/current", login.Token, map[string]string{
		HeaderManagementScopeType: string(model.ManagementScopeProvider),
		HeaderManagementScopeID:   legacyProviderID,
	})
	require.Equal(t, http.StatusNotFound, legacyScope.Code, legacyScope.Body.String())
	require.NoError(t, json.Unmarshal(legacyScope.Body.Bytes(), &errorBody))
	require.Equal(t, ErrorCodeManagementObjectMissing, errorBody.Code)

	conflict := fixture.managementRequest(http.MethodGet, "/management/contexts/current", login.Token, map[string]string{
		HeaderManagementScopeType: string(model.ManagementScopeTenant),
		HeaderManagementScopeID:   fixture.tenant.ID,
		"X-Tenant-ID":             fixture.tenant.ID,
	})
	require.Equal(t, http.StatusBadRequest, conflict.Code, conflict.Body.String())
	require.NoError(t, json.Unmarshal(conflict.Body.Bytes(), &errorBody))
	require.Equal(t, ErrorCodeManagementScopeConflict, errorBody.Code)
}

func TestManagementContextV2RejectsUnmappedAndStaleCredentials(t *testing.T) {
	fixture := newManagementContextAPIFixture(t)
	unmappedLogin := fixture.login(t, fixture.unmappedAdmin.Username)
	require.Zero(t, unmappedLogin.Admin.UnifiedUserID)
	unmapped := fixture.managementRequest(http.MethodGet, "/management/contexts", unmappedLogin.Token, nil)
	require.Equal(t, http.StatusUnauthorized, unmapped.Code, unmapped.Body.String())

	login := fixture.login(t, fixture.admin.Username)
	require.NoError(t, fixture.database.Model(&model.UserIdentityProfile{}).Where("user_id = ?", fixture.user.ID).Update("auth_revision", 5).Error)
	stale := fixture.managementRequest(http.MethodGet, "/management/contexts", login.Token, nil)
	require.Equal(t, http.StatusUnauthorized, stale.Code, stale.Body.String())
	var errorBody Response
	require.NoError(t, json.Unmarshal(stale.Body.Bytes(), &errorBody))
	require.Equal(t, ErrorCodeAuthRevisionStale, errorBody.Code)
}

func TestManagementContextV2CredentialRevisionInvalidatesOldToken(t *testing.T) {
	fixture := newManagementContextAPIFixture(t)
	login := fixture.login(t, fixture.admin.Username)
	changeReq := httptest.NewRequest(http.MethodPut, "/password", bytes.NewBufferString(`{"old_password":"fixture-password","new_password":"updated-fixture-password"}`))
	changeReq.Header.Set("Authorization", "Bearer "+login.Token)
	changeReq.Header.Set("Content-Type", "application/json")
	changeResp := httptest.NewRecorder()
	fixture.router.ServeHTTP(changeResp, changeReq)
	require.Equal(t, http.StatusOK, changeResp.Code, changeResp.Body.String())

	stale := fixture.managementRequest(http.MethodGet, "/management/contexts", login.Token, nil)
	require.Equal(t, http.StatusUnauthorized, stale.Code, stale.Body.String())
	var errorBody Response
	require.NoError(t, json.Unmarshal(stale.Body.Bytes(), &errorBody))
	require.Equal(t, ErrorCodeAuthRevisionStale, errorBody.Code)

	var link model.UserAuthenticationLink
	require.NoError(t, fixture.database.Where("provider_type = ? AND provider_subject = ?",
		model.AuthenticationProviderLegacyAdmin, strconv.FormatInt(fixture.admin.ID, 10)).First(&link).Error)
	require.Equal(t, int64(4), link.CredentialRevision)
}

func TestLegacyAuthenticatedWritePersistsUnifiedActorAndEffectiveUser(t *testing.T) {
	fixture := newManagementContextAPIFixture(t)
	login := fixture.login(t, fixture.admin.Username)
	response := fixture.managementRequest(http.MethodPost, "/legacy-audit", login.Token, nil)
	require.Equal(t, http.StatusNoContent, response.Code, response.Body.String())

	var audit model.AuditLog
	require.NoError(t, fixture.database.First(&audit, "action_type = ?", "legacy_business_write").Error)
	require.Equal(t, fixture.admin.ID, audit.ActorAdminID)
	require.Equal(t, fixture.user.ID, audit.ActorUserID)
	require.Equal(t, fixture.user.ID, audit.EffectiveUserID)
}

func TestManagementContextV2FeatureFlagFailsClosedBeforeIdentityMapping(t *testing.T) {
	fixture := newManagementContextAPIFixture(t)
	login := fixture.login(t, fixture.admin.Username)
	router := gin.New()
	router.Use(RequestMetadataMiddleware())
	router.Use(AuthMiddleware(fixture.config.Security.JWTSecret, false))
	router.Use(RequireFeatureFlag(config.FeatureFlagsSection{}, config.FeatureManagementContextV2, false))
	router.Use(UnifiedManagementIdentityMiddleware())
	router.GET("/management/contexts", NewManagementContextAPI().List)
	req := httptest.NewRequest(http.MethodGet, "/management/contexts", nil)
	req.Header.Set("Authorization", "Bearer "+login.Token)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	require.Equal(t, http.StatusServiceUnavailable, resp.Code, resp.Body.String())
}
