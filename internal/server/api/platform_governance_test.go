package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/service"
)

func TestPlatformGovernanceAPIListsUnifiedProviderAndTenantOrganizations(t *testing.T) {
	fixture := newManagementContextAPIFixture(t)
	for _, statement := range []string{
		"CREATE TABLE technical_resource (id TEXT PRIMARY KEY, provider_id TEXT NOT NULL)",
		"CREATE TABLE platform_resource (id TEXT PRIMARY KEY, provider_id TEXT NOT NULL)",
		"CREATE TABLE resource_scope (id TEXT PRIMARY KEY, provider_id TEXT NOT NULL)",
		"CREATE TABLE tenant_resource (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL)",
	} {
		require.NoError(t, fixture.database.Exec(statement).Error)
	}
	require.NoError(t, fixture.database.Exec("INSERT INTO technical_resource (id, provider_id) VALUES (?, ?)", "technical-a", fixture.provider.ID).Error)
	require.NoError(t, fixture.database.Exec("INSERT INTO platform_resource (id, provider_id) VALUES (?, ?)", "resource-a", fixture.provider.ID).Error)
	require.NoError(t, fixture.database.Exec("INSERT INTO resource_scope (id, provider_id) VALUES (?, ?)", "scope-a", fixture.provider.ID).Error)
	require.NoError(t, fixture.database.Exec("INSERT INTO tenant_resource (id, tenant_id) VALUES (?, ?)", "tenant-resource-a", fixture.tenant.ID).Error)
	require.NoError(t, fixture.database.Create(&model.TenantMembership{
		TenantID: fixture.tenant.ID, UserID: fixture.user.ID, Role: "member", Enabled: true,
	}).Error)

	login := fixture.login(t, fixture.admin.Username)
	group := fixture.router.Group("/platform-governance")
	group.Use(AuthMiddleware(fixture.config.Security.JWTSecret, false))
	group.Use(UnifiedManagementIdentityMiddleware())
	group.GET("/organizations", RequireManagementPermission(service.PermissionPlatformOrganizationsRead), NewPlatformGovernanceAPI().ListOrganizations)

	headers := map[string]string{HeaderManagementScopeType: string(model.ManagementScopePlatform)}
	response := fixture.managementRequest(http.MethodGet, "/platform-governance/organizations?size=100", login.Token, headers)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var body struct {
		Data  []platformOrganizationItem `json:"data"`
		Total int64                      `json:"total"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	require.EqualValues(t, 3, body.Total)
	require.Len(t, body.Data, 3)

	byID := make(map[string]platformOrganizationItem, len(body.Data))
	for _, item := range body.Data {
		byID[item.ID] = item
	}
	provider := byID[fixture.provider.ID]
	require.Equal(t, "provider", provider.ScopeType)
	require.EqualValues(t, 1, provider.ManagementMembershipCount)
	require.EqualValues(t, 1, provider.TechnicalResourceCount)
	require.EqualValues(t, 1, provider.ResourceCount)
	require.EqualValues(t, 1, provider.ScopeCount)
	tenant := byID[fixture.tenant.ID]
	require.Equal(t, "tenant", tenant.ScopeType)
	require.EqualValues(t, 1, tenant.ManagementMembershipCount)
	require.EqualValues(t, 1, tenant.BusinessMemberCount)
	require.EqualValues(t, 1, tenant.ResourceCount)

	providerOnly := fixture.managementRequest(http.MethodGet, "/platform-governance/organizations?scope_type=provider&search=Provider+A&size=100", login.Token, headers)
	require.Equal(t, http.StatusOK, providerOnly.Code, providerOnly.Body.String())
	require.Contains(t, providerOnly.Body.String(), fixture.provider.ID)
	require.NotContains(t, providerOnly.Body.String(), fixture.tenant.ID)

	retiredTenant := fixture.managementRequest(http.MethodGet, "/platform-governance/organizations?scope_type=tenant&status=retired", login.Token, headers)
	require.Equal(t, http.StatusOK, retiredTenant.Code, retiredTenant.Body.String())
	require.Contains(t, retiredTenant.Body.String(), `"total":0`)

	pageOne := fixture.managementRequest(http.MethodGet, "/platform-governance/organizations?page=1&size=1", login.Token, headers)
	pageTwo := fixture.managementRequest(http.MethodGet, "/platform-governance/organizations?page=2&size=1", login.Token, headers)
	require.Equal(t, http.StatusOK, pageOne.Code, pageOne.Body.String())
	require.Equal(t, http.StatusOK, pageTwo.Code, pageTwo.Body.String())
	require.NotEqual(t, pageOne.Body.String(), pageTwo.Body.String())

	invalid := fixture.managementRequest(http.MethodGet, "/platform-governance/organizations?status=deleted", login.Token, headers)
	require.Equal(t, http.StatusBadRequest, invalid.Code, invalid.Body.String())
}

func TestPlatformGovernanceAPIMutatesUnifiedManagementMemberships(t *testing.T) {
	fixture := newManagementContextAPIFixture(t)
	login := fixture.login(t, fixture.admin.Username)
	group := fixture.router.Group("/platform-governance")
	group.Use(AuthMiddleware(fixture.config.Security.JWTSecret, false))
	group.Use(UnifiedManagementIdentityMiddleware())
	api := NewPlatformGovernanceAPI()
	group.POST("/management-memberships/providers/:id", ForbidUserSimulation(), RequireManagementPermission(service.PermissionPlatformMembershipsWrite), RequireIdempotencyKey(), api.CreateProviderMembership)
	group.POST("/management-memberships/tenants/:id", ForbidUserSimulation(), RequireManagementPermission(service.PermissionPlatformMembershipsWrite), RequireIdempotencyKey(), api.CreateTenantMembership)
	group.PATCH("/management-memberships/tenants/:id/:membership_id", ForbidUserSimulation(), RequireManagementPermission(service.PermissionPlatformMembershipsWrite), RequireIfMatch(), api.UpdateTenantMembership)

	request := func(method, path, body string, extra map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+login.Token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(HeaderManagementScopeType, string(model.ManagementScopePlatform))
		for key, value := range extra {
			req.Header.Set(key, value)
		}
		response := httptest.NewRecorder()
		fixture.router.ServeHTTP(response, req)
		return response
	}

	otherUser := model.User{Name: "membership-target", Role: model.UserRoleClient, SecretHash: "fixture", Enabled: true}
	require.NoError(t, fixture.database.Create(&otherUser).Error)
	expiresAt := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339Nano)
	createBody := `{"user_id":` + strconv.FormatUint(otherUser.ID, 10) + `,"role":"tenant_admin","valid_from":"` + time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano) + `","expires_at":"` + expiresAt + `","reason":"incident ownership"}`
	headers := map[string]string{HeaderIdempotencyKey: "tenant-membership-create"}
	created := request(http.MethodPost, "/platform-governance/management-memberships/tenants/"+fixture.tenant.ID, createBody, headers)
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	require.Equal(t, `"1"`, created.Header().Get("ETag"))
	var createResponse struct {
		Data platformMembershipMutationResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &createResponse))
	require.Equal(t, otherUser.ID, createResponse.Data.UserID)
	require.Equal(t, string(model.ManagementScopeTenant), createResponse.Data.ScopeType)

	replayed := request(http.MethodPost, "/platform-governance/management-memberships/tenants/"+fixture.tenant.ID, createBody, headers)
	require.Equal(t, http.StatusCreated, replayed.Code, replayed.Body.String())
	require.JSONEq(t, created.Body.String(), replayed.Body.String())
	var membershipCount int64
	require.NoError(t, fixture.database.Model(&model.UserTenantManagementMembership{}).Where("user_id = ? AND tenant_id = ?", otherUser.ID, fixture.tenant.ID).Count(&membershipCount).Error)
	require.EqualValues(t, 1, membershipCount)

	updateBody := `{"role":"security_auditor","enabled":false,"reason":"scheduled rotation"}`
	updated := request(http.MethodPatch, "/platform-governance/management-memberships/tenants/"+fixture.tenant.ID+"/"+createResponse.Data.ID, updateBody, map[string]string{HeaderIfMatch: "1"})
	require.Equal(t, http.StatusOK, updated.Code, updated.Body.String())
	var updateResponse struct {
		Data platformMembershipMutationResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(updated.Body.Bytes(), &updateResponse))
	require.Equal(t, "security_auditor", updateResponse.Data.Role)
	require.False(t, updateResponse.Data.Enabled)
	require.EqualValues(t, 2, updateResponse.Data.RowVersion)
	require.EqualValues(t, 2, updateResponse.Data.PermissionRevision)

	stale := request(http.MethodPatch, "/platform-governance/management-memberships/tenants/"+fixture.tenant.ID+"/"+createResponse.Data.ID, updateBody, map[string]string{HeaderIfMatch: "1"})
	require.Equal(t, http.StatusConflict, stale.Code, stale.Body.String())

	selfWithoutExpiry := request(http.MethodPost, "/platform-governance/management-memberships/providers/"+fixture.otherProvider.ID,
		`{"user_id":`+strconv.FormatUint(fixture.user.ID, 10)+`,"role":"provider_viewer","reason":"self support"}`,
		map[string]string{HeaderIdempotencyKey: "provider-self-no-expiry"})
	require.Equal(t, http.StatusBadRequest, selfWithoutExpiry.Code, selfWithoutExpiry.Body.String())

	var createAudit, updateAudit int64
	require.NoError(t, fixture.database.Model(&model.AuditLog{}).Where("action_type = ?", "create_tenant_management_membership").Count(&createAudit).Error)
	require.NoError(t, fixture.database.Model(&model.AuditLog{}).Where("action_type = ?", "update_tenant_management_membership").Count(&updateAudit).Error)
	require.EqualValues(t, 1, createAudit)
	require.EqualValues(t, 1, updateAudit)
}

func TestPlatformGovernanceMembershipWritesRejectPlatformViewer(t *testing.T) {
	fixture := newManagementContextAPIFixture(t)
	require.NoError(t, fixture.database.Model(&model.PlatformRoleMembership{}).Where("user_id = ?", fixture.user.ID).Updates(map[string]any{
		"role": model.PlatformRoleViewer, "permission_revision": gorm.Expr("permission_revision + 1"), "row_version": gorm.Expr("row_version + 1"),
	}).Error)
	login := fixture.login(t, fixture.admin.Username)
	group := fixture.router.Group("/platform-governance-viewer")
	group.Use(AuthMiddleware(fixture.config.Security.JWTSecret, false))
	group.Use(UnifiedManagementIdentityMiddleware())
	group.POST("/management-memberships/tenants/:id", ForbidUserSimulation(), RequireManagementPermission(service.PermissionPlatformMembershipsWrite), RequireIdempotencyKey(), NewPlatformGovernanceAPI().CreateTenantMembership)

	req := httptest.NewRequest(http.MethodPost, "/platform-governance-viewer/management-memberships/tenants/"+fixture.tenant.ID, bytes.NewBufferString(`{"user_id":1,"role":"tenant_viewer","reason":"denied"}`))
	req.Header.Set("Authorization", "Bearer "+login.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(HeaderManagementScopeType, string(model.ManagementScopePlatform))
	req.Header.Set(HeaderIdempotencyKey, "viewer-denied")
	response := httptest.NewRecorder()
	fixture.router.ServeHTTP(response, req)
	require.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
}

func TestPlatformGovernanceAPIMutatesOrganizations(t *testing.T) {
	fixture := newManagementContextAPIFixture(t)
	require.NoError(t, fixture.database.AutoMigrate(&model.TechnicalResource{}, &model.DomainRegistry{}, &model.SystemConfig{}))
	login := fixture.login(t, fixture.admin.Username)
	group := fixture.router.Group("/platform-organizations")
	group.Use(AuthMiddleware(fixture.config.Security.JWTSecret, false))
	group.Use(UnifiedManagementIdentityMiddleware())
	api := NewPlatformGovernanceAPI()
	group.POST("/providers", ForbidUserSimulation(), RequireManagementPermission(service.PermissionPlatformOrganizationsWrite), RequireIdempotencyKey(), api.CreateProvider)
	group.PATCH("/providers/:id", ForbidUserSimulation(), RequireManagementPermission(service.PermissionPlatformOrganizationsWrite), RequireIfMatch(), api.UpdateProvider)
	group.POST("/tenants", ForbidUserSimulation(), RequireManagementPermission(service.PermissionPlatformOrganizationsWrite), RequireIdempotencyKey(), api.CreateTenant)
	group.PATCH("/tenants/:id", ForbidUserSimulation(), RequireManagementPermission(service.PermissionPlatformOrganizationsWrite), RequireIfMatch(), api.UpdateTenant)
	group.POST("/tenants/:id/suspend", ForbidUserSimulation(), RequireManagementPermission(service.PermissionPlatformOrganizationsWrite), RequireIdempotencyKey(), RequireIfMatch(), api.SuspendTenant)
	group.POST("/tenants/:id/resume", ForbidUserSimulation(), RequireManagementPermission(service.PermissionPlatformOrganizationsWrite), RequireIdempotencyKey(), RequireIfMatch(), api.ResumeTenant)

	request := func(method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+login.Token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(HeaderManagementScopeType, string(model.ManagementScopePlatform))
		for key, value := range headers {
			req.Header.Set(key, value)
		}
		response := httptest.NewRecorder()
		fixture.router.ServeHTTP(response, req)
		return response
	}

	createBody := `{"key":"audit-team","name":"审计团队","reason":"ORG-1001 新租户建档"}`
	missingDomain := request(http.MethodPost, "/platform-organizations/providers", `{"key":"missing-domain","name":"缺少域名标识","reason":"ORG-0999 校验必填"}`, map[string]string{HeaderIdempotencyKey: "create-missing-domain"})
	require.Equal(t, http.StatusBadRequest, missingDomain.Code, missingDomain.Body.String())
	providerCreated := request(http.MethodPost, "/platform-organizations/providers", `{"key":"edge-provider","name":"边缘资源服务商","domain_label":"edge-north","reason":"ORG-1000 新 Provider 建档"}`, map[string]string{HeaderIdempotencyKey: "create-edge-provider"})
	require.Equal(t, http.StatusCreated, providerCreated.Code, providerCreated.Body.String())
	require.Contains(t, providerCreated.Body.String(), `"scope_type":"provider"`)
	var providerCreatedBody struct {
		Data platformOrganizationMutationResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(providerCreated.Body.Bytes(), &providerCreatedBody))
	require.Equal(t, "edge-north", providerCreatedBody.Data.DomainLabel)
	mismatch := request(http.MethodPatch, "/platform-organizations/providers/"+providerCreatedBody.Data.ID, `{"name":"边缘资源服务商","domain_label":"edge-south","domain_change_confirmation":"edge-wrong","reason":"ORG-1003 域名调整"}`, map[string]string{HeaderIfMatch: "1"})
	require.Equal(t, http.StatusBadRequest, mismatch.Code, mismatch.Body.String())
	providerUpdated := request(http.MethodPatch, "/platform-organizations/providers/"+providerCreatedBody.Data.ID, `{"name":"边缘资源服务商","domain_label":"edge-south","domain_change_confirmation":"edge-south","reason":"ORG-1003 域名调整"}`, map[string]string{HeaderIfMatch: "1"})
	require.Equal(t, http.StatusOK, providerUpdated.Code, providerUpdated.Body.String())
	require.Contains(t, providerUpdated.Body.String(), `"domain_label":"edge-south"`)
	domainConflict := request(http.MethodPost, "/platform-organizations/providers", `{"key":"edge-provider-2","name":"第二资源服务商","domain_label":"EDGE-SOUTH","reason":"ORG-1004 新 Provider 建档"}`, map[string]string{HeaderIdempotencyKey: "create-edge-provider-2"})
	require.Equal(t, http.StatusConflict, domainConflict.Code, domainConflict.Body.String())
	created := request(http.MethodPost, "/platform-organizations/tenants", createBody, map[string]string{HeaderIdempotencyKey: "create-audit-team"})
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	var createdBody struct {
		Data platformOrganizationMutationResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &createdBody))
	require.Equal(t, "tenant", createdBody.Data.ScopeType)
	require.EqualValues(t, 1, createdBody.Data.RowVersion)
	replayed := request(http.MethodPost, "/platform-organizations/tenants", createBody, map[string]string{HeaderIdempotencyKey: "create-audit-team"})
	require.Equal(t, http.StatusCreated, replayed.Code, replayed.Body.String())
	require.JSONEq(t, created.Body.String(), replayed.Body.String())

	updated := request(http.MethodPatch, "/platform-organizations/tenants/"+createdBody.Data.ID, `{"name":"审计与响应团队","reason":"ORG-1002 名称校正"}`, map[string]string{HeaderIfMatch: "1"})
	require.Equal(t, http.StatusOK, updated.Code, updated.Body.String())
	var updatedBody struct {
		Data platformOrganizationMutationResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(updated.Body.Bytes(), &updatedBody))
	require.EqualValues(t, 2, updatedBody.Data.Revision)
	require.EqualValues(t, 2, updatedBody.Data.RowVersion)

	suspended := request(http.MethodPost, "/platform-organizations/tenants/"+createdBody.Data.ID+"/suspend", `{"reason":"INC-2201 暂停新会话"}`, map[string]string{HeaderIdempotencyKey: "suspend-audit-team", HeaderIfMatch: "2"})
	require.Equal(t, http.StatusOK, suspended.Code, suspended.Body.String())
	var suspendedBody struct {
		Data platformOrganizationMutationResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(suspended.Body.Bytes(), &suspendedBody))
	require.Equal(t, "suspended", suspendedBody.Data.Status)
	require.EqualValues(t, 3, suspendedBody.Data.RowVersion)

	stale := request(http.MethodPost, "/platform-organizations/tenants/"+createdBody.Data.ID+"/resume", `{"reason":"INC-2201 stale"}`, map[string]string{HeaderIdempotencyKey: "resume-stale", HeaderIfMatch: "2"})
	require.Equal(t, http.StatusConflict, stale.Code, stale.Body.String())
	resumed := request(http.MethodPost, "/platform-organizations/tenants/"+createdBody.Data.ID+"/resume", `{"reason":"INC-2201 风险解除"}`, map[string]string{HeaderIdempotencyKey: "resume-audit-team", HeaderIfMatch: "3"})
	require.Equal(t, http.StatusOK, resumed.Code, resumed.Body.String())
	require.Contains(t, resumed.Body.String(), `"status":"active"`)

	var count int64
	require.NoError(t, fixture.database.Model(&model.Tenant{}).Where("key = ?", "audit-team").Count(&count).Error)
	require.EqualValues(t, 1, count)
	for _, action := range []string{"create_provider", "update_provider", "create_tenant", "update_tenant", "suspend_tenant", "resume_tenant"} {
		require.NoError(t, fixture.database.Model(&model.AuditLog{}).Where("action_type = ?", action).Count(&count).Error)
		require.EqualValues(t, 1, count, action)
	}
}

func TestPlatformGovernanceAPIListsUnifiedProviderAndTenantMemberships(t *testing.T) {
	fixture := newManagementContextAPIFixture(t)
	login := fixture.login(t, fixture.admin.Username)

	group := fixture.router.Group("/platform-governance")
	group.Use(AuthMiddleware(fixture.config.Security.JWTSecret, false))
	group.Use(UnifiedManagementIdentityMiddleware())
	group.GET("/memberships", RequireManagementPermission(service.PermissionPlatformMembershipsRead), NewPlatformGovernanceAPI().ListManagementMemberships)

	headers := map[string]string{HeaderManagementScopeType: string(model.ManagementScopePlatform)}
	response := fixture.managementRequest(http.MethodGet, "/platform-governance/memberships?size=100", login.Token, headers)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var body struct {
		Data  []platformManagementMembershipItem `json:"data"`
		Total int64                              `json:"total"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	require.EqualValues(t, 2, body.Total)
	require.Len(t, body.Data, 2)
	require.ElementsMatch(t, []string{"provider", "tenant"}, []string{body.Data[0].ScopeType, body.Data[1].ScopeType})
	for _, item := range body.Data {
		require.Equal(t, fixture.user.ID, item.UserID)
		require.Equal(t, fixture.user.Name, item.Username)
		require.NotEmpty(t, item.ScopeID)
		require.NotEmpty(t, item.ScopeName)
	}

	providerOnly := fixture.managementRequest(http.MethodGet, "/platform-governance/memberships?scope_type=provider&size=100", login.Token, headers)
	require.Equal(t, http.StatusOK, providerOnly.Code, providerOnly.Body.String())
	require.Contains(t, providerOnly.Body.String(), fixture.provider.ID)
	require.NotContains(t, providerOnly.Body.String(), fixture.tenant.ID)

	tenantSearch := fixture.managementRequest(http.MethodGet, "/platform-governance/memberships?search=Tenant+A&size=100", login.Token, headers)
	require.Equal(t, http.StatusOK, tenantSearch.Code, tenantSearch.Body.String())
	require.Contains(t, tenantSearch.Body.String(), fixture.tenant.ID)
	require.NotContains(t, tenantSearch.Body.String(), fixture.provider.ID)

	pageOne := fixture.managementRequest(http.MethodGet, "/platform-governance/memberships?page=1&size=1", login.Token, headers)
	pageTwo := fixture.managementRequest(http.MethodGet, "/platform-governance/memberships?page=2&size=1", login.Token, headers)
	require.Equal(t, http.StatusOK, pageOne.Code, pageOne.Body.String())
	require.Equal(t, http.StatusOK, pageTwo.Code, pageTwo.Body.String())
	require.NotEqual(t, pageOne.Body.String(), pageTwo.Body.String())

	invalid := fixture.managementRequest(http.MethodGet, "/platform-governance/memberships?scope_type=platform", login.Token, headers)
	require.Equal(t, http.StatusBadRequest, invalid.Code, invalid.Body.String())
}

func TestPlatformGovernanceAPIListsCrossScopeAuditWithIdentityEvidence(t *testing.T) {
	fixture := newManagementContextAPIFixture(t)
	simulationID := "simulation-fixture"
	require.NoError(t, fixture.database.Create(&model.AuditLog{
		ActorAdminID: fixture.admin.ID, ActorUsername: fixture.admin.Username,
		ActorUserID: fixture.user.ID, EffectiveUserID: fixture.user.ID,
		ScopeType: string(model.ManagementScopePlatform), RequiredPermission: service.PermissionPlatformOrganizationsRead,
		PermissionRevision: 7, ActionType: "read_platform_organizations", TargetType: "organization_directory",
		TargetID: "platform", TargetName: "Platform organizations", RequestID: "request-platform", SourceIP: "127.0.0.1",
	}).Error)
	require.NoError(t, fixture.database.Create(&model.AuditLog{
		ActorAdminID: fixture.admin.ID, ActorUsername: fixture.admin.Username,
		ActorUserID: fixture.user.ID, EffectiveUserID: fixture.user.ID, SimulationSessionID: simulationID,
		ScopeType: string(model.ManagementScopeProvider), ScopeID: fixture.provider.ID,
		RequiredPermission: service.PermissionProviderResourcesRead, PermissionRevision: 5,
		ActionType: "accept_supply_candidate", TargetType: "supply_candidate", TargetID: "candidate-a",
		TargetName: "Candidate A", RequestID: "request-simulation", SourceIP: "127.0.0.1",
	}).Error)

	login := fixture.login(t, fixture.admin.Username)
	group := fixture.router.Group("/platform-governance")
	group.Use(AuthMiddleware(fixture.config.Security.JWTSecret, false))
	group.Use(UnifiedManagementIdentityMiddleware())
	group.GET("/audit-logs", RequireManagementPermission(service.PermissionPlatformAuditRead), NewPlatformGovernanceAPI().ListAuditLogs)
	headers := map[string]string{HeaderManagementScopeType: string(model.ManagementScopePlatform)}

	response := fixture.managementRequest(http.MethodGet, "/platform-governance/audit-logs?size=100", login.Token, headers)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var body struct {
		Data  []platformAuditItem `json:"data"`
		Total int64               `json:"total"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	require.EqualValues(t, 2, body.Total)
	require.Len(t, body.Data, 2)
	for _, item := range body.Data {
		require.Equal(t, fixture.user.ID, item.ActorUserID)
		require.Equal(t, fixture.user.ID, item.EffectiveUserID)
		require.Equal(t, "Unified Admin", item.ActorUserName)
		require.Equal(t, "Unified Admin", item.EffectiveUserName)
		require.NotEmpty(t, item.RequestID)
		require.NotEmpty(t, item.RequiredPermission)
	}

	simulated := fixture.managementRequest(http.MethodGet, "/platform-governance/audit-logs?simulation=true&scope_type=provider&search=request-simulation", login.Token, headers)
	require.Equal(t, http.StatusOK, simulated.Code, simulated.Body.String())
	require.Contains(t, simulated.Body.String(), simulationID)
	require.NotContains(t, simulated.Body.String(), "request-platform")

	realIdentity := fixture.managementRequest(http.MethodGet, "/platform-governance/audit-logs?simulation=false", login.Token, headers)
	require.Equal(t, http.StatusOK, realIdentity.Code, realIdentity.Body.String())
	require.Contains(t, realIdentity.Body.String(), "request-platform")
	require.NotContains(t, realIdentity.Body.String(), simulationID)

	invalid := fixture.managementRequest(http.MethodGet, "/platform-governance/audit-logs?scope_type=unknown", login.Token, headers)
	require.Equal(t, http.StatusBadRequest, invalid.Code, invalid.Body.String())
}
