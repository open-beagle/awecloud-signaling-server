package api

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/service"
)

type tenantResourceAPIFixture struct {
	managementContextAPIFixture
	login        LoginResponse
	resource     model.TenantResource
	otherTenant  model.Tenant
	other        model.TenantResource
	grant        model.TenantAccessGrant
	otherGrant   model.TenantAccessGrant
	session      model.ResourceSession
	otherSession model.ResourceSession
	desktop      model.Node
}

func prepareTenantResourceAPIFixture(t *testing.T) tenantResourceAPIFixture {
	t.Helper()
	fixture := newManagementContextAPIFixture(t)
	require.NoError(t, fixture.database.AutoMigrate(
		&model.Node{}, &model.Group{}, &model.GroupMember{},
		&model.TechnicalResource{}, &model.TechnicalResourceBinding{}, &model.SupplyCandidate{},
		&model.PlatformResource{}, &model.PlatformResourceSource{}, &model.NamespaceObservation{}, &model.ResourceScope{},
		&model.ResourceAllocation{}, &model.ResourceAllocationItem{},
		&model.WorkloadObservation{}, &model.WorkloadObservationSource{},
		&model.TenantResource{}, &model.TenantResourceSource{}, &model.TenantResourceReviewDecision{}, &model.TenantResourceTargetRevision{},
		&model.TenantAccessGrant{}, &model.TenantAccessGrantEvent{},
		&model.ResourceSession{}, &model.ResourceSessionEvent{}, &model.ResourceSessionTermination{},
		&model.OutboxEvent{}, &model.AuditLog{},
	))
	now := time.Now().UTC()
	require.NoError(t, fixture.database.Model(&model.UserTenantManagementMembership{}).
		Where("user_id = ? AND tenant_id = ?", fixture.user.ID, fixture.tenant.ID).
		Updates(map[string]any{"role": model.TenantManagementRoleAdmin, "permission_revision": int64(3)}).Error)
	require.NoError(t, fixture.database.Create(&model.TenantMembership{
		ID: 9001, TenantID: fixture.tenant.ID, UserID: fixture.user.ID, Role: "member", Enabled: true,
	}).Error)
	desktop := model.Node{ID: 9101, UserID: fixture.user.ID, Name: "tenant-desktop", Type: model.NodeTypeDesktop, LastHeartbeat: &now}
	agentNode := model.Node{ID: 9102, UserID: fixture.user.ID, Name: "tenant-agent", Type: model.NodeTypeAgent, LastHeartbeat: &now}
	require.NoError(t, fixture.database.Create(&[]model.Node{desktop, agentNode}).Error)

	technical := model.TechnicalResource{
		ID: uuid.NewString(), ProviderID: fixture.provider.ID, Type: model.TechnicalResourceAgent, StableKey: "tenant-api-agent",
		LifecycleState: model.TechnicalResourceRegistered, HealthState: model.ResourceHealthOnline,
		CredentialRevision: 1, ConfigRevision: 1, RowVersion: 1,
	}
	require.NoError(t, fixture.database.Create(&technical).Error)
	require.NoError(t, fixture.database.Create(&model.TechnicalResourceBinding{
		ID: uuid.NewString(), TechnicalResourceID: technical.ID, SourceType: model.TechnicalResourceBindingLegacyNode,
		SourceID: fmt.Sprint(agentNode.ID), CredentialRevision: 1, Enabled: true, BoundByUserID: fixture.user.ID,
		Reason: "Tenant API fixture", RowVersion: 1,
	}).Error)
	clusterStableKey := tenantTestDigest("kubernetes-cluster-v1", "tenant-api-cluster")
	candidate := model.SupplyCandidate{
		ID: uuid.NewString(), ProviderID: fixture.provider.ID, TechnicalResourceID: technical.ID,
		ResourceType: model.SupplyResourceKubernetes, StableKey: clusterStableKey, IdentityQuality: model.SupplyIdentityStrong,
		PayloadHash: strings.Repeat("a", 64), ObservationSnapshot: `{"capabilities":["workload_inventory_v1"]}`,
		FirstObservedAt: now.Add(-time.Minute), LastObservedAt: now, LeaseExpiresAt: now.Add(time.Hour),
		ReviewState: model.SupplyCandidateLinked, RowVersion: 1,
	}
	require.NoError(t, fixture.database.Create(&candidate).Error)
	platformResource := model.PlatformResource{
		ID: uuid.NewString(), ProviderID: fixture.provider.ID, Type: model.SupplyResourceKubernetes,
		StableKey: clusterStableKey, DisplayName: "Tenant API Cluster", LifecycleState: model.PlatformResourceActive,
		HealthState: model.ResourceHealthOnline, CapabilityRevision: 1, AllocatableScopeCount: 1, RowVersion: 1,
	}
	require.NoError(t, fixture.database.Create(&platformResource).Error)
	require.NoError(t, fixture.database.Create(&model.PlatformResourceSource{
		ID: uuid.NewString(), ProviderID: fixture.provider.ID, PlatformResourceID: platformResource.ID,
		SupplyCandidateID: candidate.ID, IsPrimary: true, LinkedAt: now, LastConfirmedAt: now,
	}).Error)
	namespaceObservation := model.NamespaceObservation{
		ID: uuid.NewString(), ProviderID: fixture.provider.ID, ClusterResourceID: platformResource.ID,
		NamespaceUID: "tenant-api-namespace-uid", Name: "tenant-workloads", Revision: 1,
		ObservedAt: now, LeaseExpiresAt: now.Add(time.Hour), State: model.NamespaceObservationObserved,
	}
	require.NoError(t, fixture.database.Create(&namespaceObservation).Error)
	clusterScope := model.ResourceScope{
		ID: uuid.NewString(), ProviderID: fixture.provider.ID, PlatformResourceID: platformResource.ID,
		Type: model.ResourceScopeCluster, StableKey: clusterStableKey, LifecycleState: model.ResourceScopeActive,
		IsolationMode: model.ResourceScopeIsolationNone, ConfigRevision: 1, EvidenceRevision: 1, RowVersion: 1,
	}
	require.NoError(t, fixture.database.Create(&clusterScope).Error)
	namespaceScope := model.ResourceScope{
		ID: uuid.NewString(), ProviderID: fixture.provider.ID, PlatformResourceID: platformResource.ID,
		Type: model.ResourceScopeNamespace, StableKey: tenantTestDigest("kubernetes-namespace-v1", platformResource.ID+"\x00"+namespaceObservation.NamespaceUID),
		ParentID: &clusterScope.ID, NamespaceObservationID: &namespaceObservation.ID,
		LifecycleState: model.ResourceScopeAllocatable, IsolationMode: model.ResourceScopeIsolationNamespaceIsolated,
		ConfigRevision: 1, EvidenceRevision: 1, RowVersion: 1,
	}
	require.NoError(t, fixture.database.Create(&namespaceScope).Error)
	expiresAt := now.Add(time.Hour)
	allocation := model.ResourceAllocation{
		ID: uuid.NewString(), TenantID: fixture.tenant.ID, Mode: model.ResourceAllocationLeased,
		ValidFrom: now.Add(-time.Minute), ExpiresAt: &expiresAt, State: model.ResourceAllocationActive,
		RowVersion: 1, CreatedByUserID: fixture.user.ID,
	}
	require.NoError(t, fixture.database.Create(&allocation).Error)
	item := model.ResourceAllocationItem{ID: uuid.NewString(), AllocationID: allocation.ID, ScopeID: namespaceScope.ID, ScopeRowVersionSnapshot: 1}
	require.NoError(t, fixture.database.Create(&item).Error)
	observation := model.WorkloadObservation{
		ID: uuid.NewString(), NamespaceScopeID: namespaceScope.ID, Kind: model.WorkloadObservationServicePort,
		StableKey: strings.Repeat("b", 64), IdentityQuality: model.WorkloadIdentityStrong,
		State: model.WorkloadObservationEligible, Ready: true, ObservedRevision: 1, LabelSnapshot: `{}`,
		FirstObservedAt: now, LastObservedAt: now, LeaseExpiresAt: now.Add(time.Hour), RowVersion: 1,
	}
	require.NoError(t, fixture.database.Create(&observation).Error)
	targetSnapshot := `{"namespace_uid":"tenant-api-namespace-uid","namespace_name":"tenant-workloads","service_uid":"service-api","service_name":"api","cluster_ip":"10.0.0.10","port_name":"https","port_number":443,"protocol":"TCP","labels_allowlist":{"beagle.io/expose":"true"}}`
	evidence := model.WorkloadObservationSource{
		ID: uuid.NewString(), WorkloadObservationID: observation.ID, SourceTechnicalResourceID: technical.ID,
		SourceEpoch: uuid.NewString(), Sequence: 1, PayloadHash: strings.Repeat("c", 64), State: model.WorkloadObservationSourceObserved,
		Ready: true, TargetSnapshot: targetSnapshot, ObservedAt: now, ReceivedAt: now,
		LeaseExpiresAt: now.Add(time.Hour), SourceRevision: 1, RowVersion: 1,
	}
	require.NoError(t, fixture.database.Create(&evidence).Error)
	resource := model.TenantResource{
		ID: uuid.NewString(), TenantID: fixture.tenant.ID, Type: model.TenantResourceContainerService,
		StableKey: strings.Repeat("d", 64), EntitlementLineageID: allocation.ID, DisplayName: "api:443",
		VisibilityState: model.TenantResourcePending, AvailabilityState: model.TenantResourceAvailable, Revision: 1, RowVersion: 1,
	}
	require.NoError(t, fixture.database.Create(&resource).Error)
	source := model.TenantResourceSource{
		ID: uuid.NewString(), TenantResourceID: resource.ID, AllocationItemID: item.ID,
		WorkloadObservationID: observation.ID, Enabled: true, EnabledAt: now, SourceRevision: 1, RowVersion: 1,
	}
	require.NoError(t, fixture.database.Create(&source).Error)
	target := model.TenantResourceTargetRevision{
		ID: uuid.NewString(), TenantResourceSourceID: source.ID, Revision: 1,
		TargetType: model.WorkloadObservationServicePort, TargetSnapshot: targetSnapshot,
		SourceTechnicalResourceID: technical.ID, AccessTechnicalResourceID: technical.ID,
		Ready: true, ObservedAt: now, ObservationRevision: 1, SourceRevision: 1,
	}
	require.NoError(t, fixture.database.Create(&target).Error)

	otherTenant := model.Tenant{ID: uuid.NewString(), Key: "tenant-b-api", Name: "Tenant B API", Status: model.TenantStatusActive}
	require.NoError(t, fixture.database.Create(&otherTenant).Error)
	otherAllocation := allocation
	otherAllocation.ID = uuid.NewString()
	otherAllocation.TenantID = otherTenant.ID
	require.NoError(t, fixture.database.Create(&otherAllocation).Error)
	other := resource
	other.ID = uuid.NewString()
	other.TenantID = otherTenant.ID
	other.StableKey = strings.Repeat("e", 64)
	other.EntitlementLineageID = otherAllocation.ID
	require.NoError(t, fixture.database.Create(&other).Error)
	flags := config.FeatureFlagsSection{ManagementContextV2: true, TenantResourceReadV2: true, ResourceModelWrite: true, SessionAuthorizationV2: true}
	management := fixture.router.Group("/api/v1/management")
	management.Use(AuthMiddleware(fixture.config.Security.JWTSecret, false))
	management.Use(RequireFeatureFlag(flags, config.FeatureManagementContextV2, false))
	management.Use(UnifiedManagementIdentityMiddleware())
	tenant := management.Group("/tenants/:tenant_id")
	tenant.Use(RequireFeatureFlag(flags, config.FeatureTenantResourceReadV2, false))
	resourceAPI := NewTenantResourceManagementAPI()
	grantAPI := NewTenantAccessGrantAPI()
	sessionAPI := NewResourceSessionManagementAPI()
	tenant.GET("/resource-candidates", RequireManagementPermission(service.PermissionTenantResourcesRead), resourceAPI.ListCandidates)
	tenant.GET("/resource-candidates/:resource_id", RequireManagementPermission(service.PermissionTenantResourcesRead), resourceAPI.GetCandidate)
	tenant.POST("/resource-candidates/:resource_id/publish", RequireManagementPermission(service.PermissionTenantResourcesWrite), RequireFeatureFlag(flags, config.FeatureResourceModelWrite, true), RequireIfMatch(), RequireIdempotencyKey(), resourceAPI.PublishCandidate)
	tenant.POST("/resource-candidates/:resource_id/reject", RequireManagementPermission(service.PermissionTenantResourcesWrite), RequireFeatureFlag(flags, config.FeatureResourceModelWrite, true), resourceAPI.RejectCandidate)
	tenant.GET("/resources", RequireManagementPermission(service.PermissionTenantResourcesRead), resourceAPI.ListResources)
	tenant.GET("/resources/:id", RequireManagementPermission(service.PermissionTenantResourcesRead), resourceAPI.GetResource)
	tenant.GET("/grants", RequireManagementPermission(service.PermissionTenantGrantsRead), grantAPI.List)
	tenant.GET("/grants/:id", RequireManagementPermission(service.PermissionTenantGrantsRead), grantAPI.Get)
	tenant.POST("/grants", RequireManagementPermission(service.PermissionTenantGrantsWrite), RequireFeatureFlag(flags, config.FeatureResourceModelWrite, true), RequireIdempotencyKey(), grantAPI.Create)
	tenant.PATCH("/grants/:id", RequireManagementPermission(service.PermissionTenantGrantsWrite), RequireFeatureFlag(flags, config.FeatureResourceModelWrite, true), RequireIfMatch(), grantAPI.Update)
	tenant.POST("/grants/:id/suspend", RequireManagementPermission(service.PermissionTenantGrantsWrite), RequireFeatureFlag(flags, config.FeatureResourceModelWrite, true), RequireIfMatch(), RequireIdempotencyKey(), grantAPI.Suspend)
	tenant.POST("/grants/:id/resume", RequireManagementPermission(service.PermissionTenantGrantsWrite), RequireFeatureFlag(flags, config.FeatureResourceModelWrite, true), RequireIfMatch(), RequireIdempotencyKey(), grantAPI.Resume)
	tenant.POST("/grants/:id/revoke", RequireManagementPermission(service.PermissionTenantGrantsWrite), RequireFeatureFlag(flags, config.FeatureResourceModelWrite, true), RequireIfMatch(), RequireIdempotencyKey(), grantAPI.Revoke)
	tenant.GET("/sessions", RequireManagementPermission(service.PermissionTenantSessionsRead), sessionAPI.List)
	tenant.GET("/sessions/:id", RequireManagementPermission(service.PermissionTenantSessionsRead), sessionAPI.Get)
	tenant.POST("/sessions", RequireManagementPermission(service.PermissionTenantSessionsRead), RequireFeatureFlag(flags, config.FeatureSessionAuthorizationV2, true), RequireIdempotencyKey(), sessionAPI.Create)
	tenant.POST("/sessions/:id/terminate", RequireManagementPermission(service.PermissionTenantSessionsTerminate), RequireFeatureFlag(flags, config.FeatureSessionAuthorizationV2, true), RequireIfMatch(), RequireIdempotencyKey(), sessionAPI.Terminate)

	return tenantResourceAPIFixture{
		managementContextAPIFixture: fixture, login: fixture.login(t, fixture.admin.Username),
		resource: resource, otherTenant: otherTenant, other: other, desktop: desktop,
	}
}

func seedTenantResourceIsolationRecords(t *testing.T, fixture *tenantResourceAPIFixture) {
	t.Helper()
	now := time.Now().UTC()
	expiresAt := now.Add(time.Hour)
	var source model.TenantResourceSource
	require.NoError(t, fixture.database.Where("tenant_resource_id = ?", fixture.resource.ID).First(&source).Error)
	var target model.TenantResourceTargetRevision
	require.NoError(t, fixture.database.Where("tenant_resource_source_id = ?", source.ID).First(&target).Error)
	var allocation model.ResourceAllocation
	require.NoError(t, fixture.database.First(&allocation, "id = ?", fixture.resource.EntitlementLineageID).Error)
	var otherAllocation model.ResourceAllocation
	require.NoError(t, fixture.database.First(&otherAllocation, "id = ?", fixture.other.EntitlementLineageID).Error)
	var item model.ResourceAllocationItem
	require.NoError(t, fixture.database.First(&item, "id = ?", source.AllocationItemID).Error)
	fixture.grant = model.TenantAccessGrant{
		ID: uuid.NewString(), TenantID: fixture.tenant.ID, TenantResourceID: fixture.resource.ID,
		SubjectType: model.TenantAccessGrantSubjectUser, SubjectKey: fmt.Sprint(fixture.user.ID), SubjectUserID: &fixture.user.ID,
		Actions: `["connect"]`, ValidFrom: now.Add(-time.Minute), ExpiresAt: &expiresAt, MaxSessionSeconds: 3600,
		Status: model.TenantAccessGrantEnabled, Revision: 1, RowVersion: 1, CreatedByUserID: fixture.user.ID,
	}
	fixture.otherGrant = fixture.grant
	fixture.otherGrant.ID = uuid.NewString()
	fixture.otherGrant.TenantID = fixture.otherTenant.ID
	fixture.otherGrant.TenantResourceID = fixture.other.ID
	require.NoError(t, fixture.database.Create(&[]model.TenantAccessGrant{fixture.grant, fixture.otherGrant}).Error)
	fixture.session = model.ResourceSession{
		ID: uuid.NewString(), TenantID: fixture.tenant.ID, TenantResourceID: fixture.resource.ID,
		TenantResourceSourceID: source.ID, TargetRevisionID: target.ID, AllocationID: allocation.ID,
		AllocationItemID: item.ID, GrantID: fixture.grant.ID, GrantRevision: 1, UserID: fixture.user.ID,
		TenantMembershipID: 9001, DeviceID: fixture.desktop.ID, ActorUserID: fixture.user.ID, EffectiveUserID: fixture.user.ID,
		SessionType: model.ResourceSessionContainerService, Action: "connect", AccessTechnicalResourceID: target.AccessTechnicalResourceID,
		AuthorizationRevision: 1, ValidUntil: now.Add(30 * time.Second), Status: model.ResourceSessionAuthorizing,
		RequestID: "tenant-a-session-request", StartedAt: now, RowVersion: 1,
	}
	fixture.otherSession = fixture.session
	fixture.otherSession.ID = uuid.NewString()
	fixture.otherSession.TenantID = fixture.otherTenant.ID
	fixture.otherSession.TenantResourceID = fixture.other.ID
	fixture.otherSession.AllocationID = otherAllocation.ID
	fixture.otherSession.GrantID = fixture.otherGrant.ID
	fixture.otherSession.RequestID = "tenant-b-private-request"
	require.NoError(t, fixture.database.Create(&[]model.ResourceSession{fixture.session, fixture.otherSession}).Error)
}

func tenantTestDigest(domain, value string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(domain+"\x00"+strings.TrimSpace(value))))
}

func tenantAPIRequest(fixture tenantResourceAPIFixture, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+fixture.login.Token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(HeaderManagementScopeType, string(model.ManagementScopeTenant))
	request.Header.Set(HeaderManagementScopeID, fixture.tenant.ID)
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response := httptest.NewRecorder()
	fixture.router.ServeHTTP(response, request)
	return response
}

func TestTenantResourceAPIIsScopeFirstAndRedactsTrustedTargets(t *testing.T) {
	fixture := prepareTenantResourceAPIFixture(t)
	seedTenantResourceIsolationRecords(t, &fixture)
	list := tenantAPIRequest(fixture, http.MethodGet, "/api/v1/management/tenants/"+fixture.tenant.ID+"/resource-candidates", "", nil)
	require.Equal(t, http.StatusOK, list.Code, list.Body.String())
	require.Contains(t, list.Body.String(), fixture.resource.ID)
	require.NotContains(t, list.Body.String(), fixture.other.ID)
	require.NotContains(t, list.Body.String(), "10.0.0.10")
	require.NotContains(t, list.Body.String(), "labels_allowlist")
	require.NotContains(t, list.Body.String(), fixture.provider.ID)

	foreign := tenantAPIRequest(fixture, http.MethodGet, "/api/v1/management/tenants/"+fixture.tenant.ID+"/resource-candidates/"+fixture.other.ID, "", nil)
	require.Equal(t, http.StatusNotFound, foreign.Code, foreign.Body.String())
	assertResponseErrorCode(t, foreign, "TENANT_RESOURCE_NOT_FOUND")

	pathMismatch := tenantAPIRequest(fixture, http.MethodGet, "/api/v1/management/tenants/"+fixture.otherTenant.ID+"/resource-candidates/"+fixture.other.ID, "", nil)
	require.Equal(t, http.StatusNotFound, pathMismatch.Code, pathMismatch.Body.String())
	assertResponseErrorCode(t, pathMismatch, ErrorCodeManagementObjectMissing)

	require.NoError(t, fixture.database.Model(&model.TenantResource{}).
		Where("id IN ?", []string{fixture.resource.ID, fixture.other.ID}).
		Update("visibility_state", model.TenantResourceVisible).Error)
	resources := tenantAPIRequest(fixture, http.MethodGet, "/api/v1/management/tenants/"+fixture.tenant.ID+"/resources?limit=1&query=api", "", nil)
	require.Equal(t, http.StatusOK, resources.Code, resources.Body.String())
	require.Contains(t, resources.Body.String(), fixture.resource.ID)
	require.NotContains(t, resources.Body.String(), fixture.other.ID)
	foreignResource := tenantAPIRequest(fixture, http.MethodGet, "/api/v1/management/tenants/"+fixture.tenant.ID+"/resources/"+fixture.other.ID, "", nil)
	require.Equal(t, http.StatusNotFound, foreignResource.Code, foreignResource.Body.String())
	assertResponseErrorCode(t, foreignResource, "TENANT_RESOURCE_NOT_FOUND")

	grants := tenantAPIRequest(fixture, http.MethodGet, "/api/v1/management/tenants/"+fixture.tenant.ID+"/grants?page=1&size=1", "", nil)
	require.Equal(t, http.StatusOK, grants.Code, grants.Body.String())
	require.Contains(t, grants.Body.String(), fixture.grant.ID)
	require.NotContains(t, grants.Body.String(), fixture.otherGrant.ID)
	foreignGrantFilter := tenantAPIRequest(fixture, http.MethodGet, "/api/v1/management/tenants/"+fixture.tenant.ID+"/grants?resource_id="+fixture.other.ID, "", nil)
	require.Equal(t, http.StatusOK, foreignGrantFilter.Code, foreignGrantFilter.Body.String())
	require.NotContains(t, foreignGrantFilter.Body.String(), fixture.otherGrant.ID)
	foreignGrant := tenantAPIRequest(fixture, http.MethodGet, "/api/v1/management/tenants/"+fixture.tenant.ID+"/grants/"+fixture.otherGrant.ID, "", nil)
	require.Equal(t, http.StatusNotFound, foreignGrant.Code, foreignGrant.Body.String())

	sessions := tenantAPIRequest(fixture, http.MethodGet, "/api/v1/management/tenants/"+fixture.tenant.ID+"/sessions?page=1&size=1", "", nil)
	require.Equal(t, http.StatusOK, sessions.Code, sessions.Body.String())
	require.Contains(t, sessions.Body.String(), fixture.session.ID)
	require.NotContains(t, sessions.Body.String(), fixture.otherSession.ID)
	require.NotContains(t, sessions.Body.String(), fixture.otherSession.RequestID)
	foreignSessionFilter := tenantAPIRequest(fixture, http.MethodGet, "/api/v1/management/tenants/"+fixture.tenant.ID+"/sessions?resource_id="+fixture.other.ID, "", nil)
	require.Equal(t, http.StatusOK, foreignSessionFilter.Code, foreignSessionFilter.Body.String())
	require.NotContains(t, foreignSessionFilter.Body.String(), fixture.otherSession.ID)
	foreignSession := tenantAPIRequest(fixture, http.MethodGet, "/api/v1/management/tenants/"+fixture.tenant.ID+"/sessions/"+fixture.otherSession.ID, "", nil)
	require.Equal(t, http.StatusNotFound, foreignSession.Code, foreignSession.Body.String())

	foreignGrantUpdate := tenantAPIRequest(fixture, http.MethodPatch, "/api/v1/management/tenants/"+fixture.tenant.ID+"/grants/"+fixture.otherGrant.ID, `{"max_session_seconds":1800}`, map[string]string{HeaderIfMatch: `"1"`})
	require.Equal(t, http.StatusNotFound, foreignGrantUpdate.Code, foreignGrantUpdate.Body.String())
	foreignSessionTermination := tenantAPIRequest(fixture, http.MethodPost, "/api/v1/management/tenants/"+fixture.tenant.ID+"/sessions/"+fixture.otherSession.ID+"/terminate", `{"reason":"cross tenant"}`, map[string]string{HeaderIfMatch: `"1"`, HeaderIdempotencyKey: "cross-tenant-termination"})
	require.Equal(t, http.StatusNotFound, foreignSessionTermination.Code, foreignSessionTermination.Body.String())
}

func TestTenantResourcePublishGrantSessionAndTerminationAreIdempotent(t *testing.T) {
	fixture := prepareTenantResourceAPIFixture(t)
	publishPath := "/api/v1/management/tenants/" + fixture.tenant.ID + "/resource-candidates/" + fixture.resource.ID + "/publish"
	publishBody := `{"observation_revision":1,"reason":"approved"}`
	publishHeaders := map[string]string{HeaderIfMatch: `"1"`, HeaderIdempotencyKey: "publish-resource"}
	published := tenantAPIRequest(fixture, http.MethodPost, publishPath, publishBody, publishHeaders)
	require.Equal(t, http.StatusOK, published.Code, published.Body.String())
	require.Equal(t, `"2"`, published.Header().Get("ETag"))
	replayedPublish := tenantAPIRequest(fixture, http.MethodPost, publishPath, publishBody, publishHeaders)
	require.Equal(t, http.StatusOK, replayedPublish.Code, replayedPublish.Body.String())
	require.JSONEq(t, published.Body.String(), replayedPublish.Body.String())

	grantPath := "/api/v1/management/tenants/" + fixture.tenant.ID + "/grants"
	grantBody := fmt.Sprintf(`{"resource_id":%q,"subject":{"type":"user","user_id":%d},"actions":["connect"],"max_session_seconds":3600}`,
		fixture.resource.ID, fixture.user.ID)
	grantHeaders := map[string]string{HeaderIdempotencyKey: "create-grant"}
	createdGrant := tenantAPIRequest(fixture, http.MethodPost, grantPath, grantBody, grantHeaders)
	require.Equal(t, http.StatusCreated, createdGrant.Code, createdGrant.Body.String())
	replayedGrant := tenantAPIRequest(fixture, http.MethodPost, grantPath, grantBody, grantHeaders)
	require.Equal(t, http.StatusCreated, replayedGrant.Code, replayedGrant.Body.String())
	require.JSONEq(t, createdGrant.Body.String(), replayedGrant.Body.String())
	var grantResponse struct {
		Data struct {
			Result service.TenantGrantView `json:"result"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(createdGrant.Body.Bytes(), &grantResponse))
	require.NotEmpty(t, grantResponse.Data.Result.ID)

	sessionPath := "/api/v1/management/tenants/" + fixture.tenant.ID + "/sessions"
	sessionBody := fmt.Sprintf(`{"resource_id":%q,"action":"connect","device_id":%d,"client_capability":"resource_session_v2"}`,
		fixture.resource.ID, fixture.desktop.ID)
	sessionHeaders := map[string]string{HeaderIdempotencyKey: "create-session"}
	createdSession := tenantAPIRequest(fixture, http.MethodPost, sessionPath, sessionBody, sessionHeaders)
	require.Equal(t, http.StatusCreated, createdSession.Code, createdSession.Body.String())
	var sessionResponse struct {
		Data struct {
			Result service.ResourceSessionView `json:"result"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(createdSession.Body.Bytes(), &sessionResponse))
	require.NotEmpty(t, sessionResponse.Data.Result.ID)
	require.Equal(t, model.ResourceSessionAuthorizing, sessionResponse.Data.Result.Status)

	terminatePath := sessionPath + "/" + sessionResponse.Data.Result.ID + "/terminate"
	terminateHeaders := map[string]string{HeaderIfMatch: `"1"`, HeaderIdempotencyKey: "terminate-session"}
	terminated := tenantAPIRequest(fixture, http.MethodPost, terminatePath, `{"reason":"operator requested"}`, terminateHeaders)
	require.Equal(t, http.StatusOK, terminated.Code, terminated.Body.String())
	replayedTermination := tenantAPIRequest(fixture, http.MethodPost, terminatePath, `{"reason":"operator requested"}`, terminateHeaders)
	require.Equal(t, http.StatusOK, replayedTermination.Code, replayedTermination.Body.String())
	require.JSONEq(t, terminated.Body.String(), replayedTermination.Body.String())

	var terminationCount, grantEventCount, outboxCount, auditCount int64
	require.NoError(t, fixture.database.Model(&model.ResourceSessionTermination{}).Where("session_id = ?", sessionResponse.Data.Result.ID).Count(&terminationCount).Error)
	require.NoError(t, fixture.database.Model(&model.TenantAccessGrantEvent{}).Where("grant_id = ?", grantResponse.Data.Result.ID).Count(&grantEventCount).Error)
	require.NoError(t, fixture.database.Model(&model.OutboxEvent{}).Where("consumer = ?", service.TenantAuthorizationOutboxConsumer).Count(&outboxCount).Error)
	require.NoError(t, fixture.database.Model(&model.AuditLog{}).Where("tenant_id = ?", fixture.tenant.ID).Count(&auditCount).Error)
	require.Equal(t, int64(1), terminationCount)
	require.Equal(t, int64(1), grantEventCount)
	require.Equal(t, int64(4), outboxCount)
	require.Equal(t, int64(4), auditCount)
}

func TestTenantResourceRejectHidesOnlyCurrentCandidateRevision(t *testing.T) {
	fixture := prepareTenantResourceAPIFixture(t)
	path := "/api/v1/management/tenants/" + fixture.tenant.ID + "/resource-candidates/" + fixture.resource.ID + "/reject"
	rejected := tenantAPIRequest(fixture, http.MethodPost, path, `{"observation_revision":1,"reason":"not intended for Tenant access"}`, nil)
	require.Equal(t, http.StatusOK, rejected.Code, rejected.Body.String())
	require.Equal(t, `"2"`, rejected.Header().Get("ETag"))

	list := tenantAPIRequest(fixture, http.MethodGet, "/api/v1/management/tenants/"+fixture.tenant.ID+"/resource-candidates", "", nil)
	require.Equal(t, http.StatusOK, list.Code, list.Body.String())
	require.NotContains(t, list.Body.String(), fixture.resource.ID)
	detail := tenantAPIRequest(fixture, http.MethodGet, "/api/v1/management/tenants/"+fixture.tenant.ID+"/resource-candidates/"+fixture.resource.ID, "", nil)
	require.Equal(t, http.StatusOK, detail.Code, detail.Body.String())
	require.Contains(t, detail.Body.String(), fixture.resource.ID)

	repeated := tenantAPIRequest(fixture, http.MethodPost, path, `{"observation_revision":1,"reason":"repeat"}`, nil)
	require.Equal(t, http.StatusConflict, repeated.Code, repeated.Body.String())
	assertResponseErrorCode(t, repeated, "TENANT_RESOURCE_REVIEW_STALE")
}

func TestTenantResourcePublishReportsStaleObservationBeforeTargetUnavailable(t *testing.T) {
	fixture := prepareTenantResourceAPIFixture(t)
	var source model.TenantResourceSource
	require.NoError(t, fixture.database.First(&source, "tenant_resource_id = ?", fixture.resource.ID).Error)
	require.NoError(t, fixture.database.Model(&model.WorkloadObservation{}).Where("id = ?", source.WorkloadObservationID).
		Updates(map[string]any{"observed_revision": int64(2), "row_version": int64(2)}).Error)

	path := "/api/v1/management/tenants/" + fixture.tenant.ID + "/resource-candidates/" + fixture.resource.ID + "/publish"
	response := tenantAPIRequest(fixture, http.MethodPost, path, `{"observation_revision":1,"reason":"stale review"}`,
		map[string]string{HeaderIfMatch: `"1"`, HeaderIdempotencyKey: "publish-stale-observation"})
	require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
	assertResponseErrorCode(t, response, "TENANT_RESOURCE_REVIEW_STALE")

	var resource model.TenantResource
	require.NoError(t, fixture.database.First(&resource, "id = ?", fixture.resource.ID).Error)
	require.Equal(t, model.TenantResourcePending, resource.VisibilityState)
	var reviewCount, outboxCount int64
	require.NoError(t, fixture.database.Model(&model.TenantResourceReviewDecision{}).Where("tenant_resource_id = ?", resource.ID).Count(&reviewCount).Error)
	require.NoError(t, fixture.database.Model(&model.OutboxEvent{}).Where("aggregate_id = ?", resource.ID).Count(&outboxCount).Error)
	require.Zero(t, reviewCount)
	require.Zero(t, outboxCount)
}

func TestTenantResourcePublishRollsBackBusinessOutboxAndReviewWhenAuditFails(t *testing.T) {
	fixture := prepareTenantResourceAPIFixture(t)
	require.NoError(t, fixture.database.Migrator().DropTable(&model.AuditLog{}))
	path := "/api/v1/management/tenants/" + fixture.tenant.ID + "/resource-candidates/" + fixture.resource.ID + "/publish"
	response := tenantAPIRequest(fixture, http.MethodPost, path, `{"observation_revision":1,"reason":"audit must be atomic"}`,
		map[string]string{HeaderIfMatch: `"1"`, HeaderIdempotencyKey: "publish-audit-failure"})
	require.Equal(t, http.StatusInternalServerError, response.Code, response.Body.String())

	var resource model.TenantResource
	require.NoError(t, fixture.database.First(&resource, "id = ?", fixture.resource.ID).Error)
	require.Equal(t, model.TenantResourcePending, resource.VisibilityState)
	require.Equal(t, int64(1), resource.Revision)
	var reviewCount, outboxCount int64
	require.NoError(t, fixture.database.Model(&model.TenantResourceReviewDecision{}).Where("tenant_resource_id = ?", resource.ID).Count(&reviewCount).Error)
	require.NoError(t, fixture.database.Model(&model.OutboxEvent{}).Where("aggregate_id = ?", resource.ID).Count(&outboxCount).Error)
	require.Zero(t, reviewCount)
	require.Zero(t, outboxCount)
}

func TestTenantGrantSuspensionEndsSessionsAndRevokedGrantNeverResumes(t *testing.T) {
	fixture := prepareTenantResourceAPIFixture(t)
	publishPath := "/api/v1/management/tenants/" + fixture.tenant.ID + "/resource-candidates/" + fixture.resource.ID + "/publish"
	require.Equal(t, http.StatusOK, tenantAPIRequest(fixture, http.MethodPost, publishPath,
		`{"observation_revision":1,"reason":"approved"}`, map[string]string{HeaderIfMatch: `"1"`, HeaderIdempotencyKey: "grant-flow-publish"}).Code)

	grantPath := "/api/v1/management/tenants/" + fixture.tenant.ID + "/grants"
	grantBody := fmt.Sprintf(`{"resource_id":%q,"subject":{"type":"user","user_id":%d},"actions":["connect"],"max_session_seconds":3600}`,
		fixture.resource.ID, fixture.user.ID)
	createdGrant := tenantAPIRequest(fixture, http.MethodPost, grantPath, grantBody, map[string]string{HeaderIdempotencyKey: "grant-flow-create"})
	require.Equal(t, http.StatusCreated, createdGrant.Code, createdGrant.Body.String())
	var grantResponse struct {
		Data struct {
			Result service.TenantGrantView `json:"result"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(createdGrant.Body.Bytes(), &grantResponse))
	grantID := grantResponse.Data.Result.ID

	sessionPath := "/api/v1/management/tenants/" + fixture.tenant.ID + "/sessions"
	sessionBody := fmt.Sprintf(`{"resource_id":%q,"action":"connect","device_id":%d,"client_capability":"resource_session_v2"}`,
		fixture.resource.ID, fixture.desktop.ID)
	createdSession := tenantAPIRequest(fixture, http.MethodPost, sessionPath, sessionBody, map[string]string{HeaderIdempotencyKey: "grant-flow-session"})
	require.Equal(t, http.StatusCreated, createdSession.Code, createdSession.Body.String())
	var sessionResponse struct {
		Data struct {
			Result service.ResourceSessionView `json:"result"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(createdSession.Body.Bytes(), &sessionResponse))

	suspendHeaders := map[string]string{HeaderIfMatch: `"1"`, HeaderIdempotencyKey: "grant-flow-suspend"}
	suspend := tenantAPIRequest(fixture, http.MethodPost, grantPath+"/"+grantID+"/suspend", `{"reason":"security review"}`, suspendHeaders)
	require.Equal(t, http.StatusOK, suspend.Code, suspend.Body.String())
	replayedSuspend := tenantAPIRequest(fixture, http.MethodPost, grantPath+"/"+grantID+"/suspend", `{"reason":"security review"}`, suspendHeaders)
	require.Equal(t, http.StatusOK, replayedSuspend.Code, replayedSuspend.Body.String())
	require.JSONEq(t, suspend.Body.String(), replayedSuspend.Body.String())
	var session model.ResourceSession
	require.NoError(t, fixture.database.First(&session, "id = ?", sessionResponse.Data.Result.ID).Error)
	require.Equal(t, model.ResourceSessionEnding, session.Status)
	require.Equal(t, int64(2), session.RowVersion)
	var terminationCount int64
	require.NoError(t, fixture.database.Model(&model.ResourceSessionTermination{}).Where("session_id = ?", session.ID).Count(&terminationCount).Error)
	require.Equal(t, int64(1), terminationCount)

	resume := tenantAPIRequest(fixture, http.MethodPost, grantPath+"/"+grantID+"/resume", `{"reason":"review complete"}`,
		map[string]string{HeaderIfMatch: `"2"`, HeaderIdempotencyKey: "grant-flow-resume"})
	require.Equal(t, http.StatusOK, resume.Code, resume.Body.String())
	revoke := tenantAPIRequest(fixture, http.MethodPost, grantPath+"/"+grantID+"/revoke", `{"reason":"access removed"}`,
		map[string]string{HeaderIfMatch: `"3"`, HeaderIdempotencyKey: "grant-flow-revoke"})
	require.Equal(t, http.StatusOK, revoke.Code, revoke.Body.String())
	resumeRevoked := tenantAPIRequest(fixture, http.MethodPost, grantPath+"/"+grantID+"/resume", `{"reason":"must stay revoked"}`,
		map[string]string{HeaderIfMatch: `"4"`, HeaderIdempotencyKey: "grant-flow-resume-revoked"})
	require.Equal(t, http.StatusConflict, resumeRevoked.Code, resumeRevoked.Body.String())
	assertResponseErrorCode(t, resumeRevoked, "TENANT_GRANT_STATE_TRANSITION_INVALID")

	var grant model.TenantAccessGrant
	require.NoError(t, fixture.database.First(&grant, "id = ?", grantID).Error)
	require.Equal(t, model.TenantAccessGrantRevoked, grant.Status)
	require.Equal(t, int64(4), grant.Revision)
}
