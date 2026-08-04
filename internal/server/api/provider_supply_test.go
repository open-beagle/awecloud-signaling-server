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

func prepareProviderSupplyAPIFixture(t *testing.T, flags config.FeatureFlagsSection) (managementContextAPIFixture, LoginResponse) {
	t.Helper()
	fixture := newManagementContextAPIFixture(t)
	require.NoError(t, fixture.database.AutoMigrate(
		&model.TechnicalResource{}, &model.TechnicalResourceBinding{}, &model.SupplyInventoryReceipt{},
		&model.SupplyCandidate{}, &model.PlatformResource{}, &model.PlatformResourceSource{},
		&model.NamespaceObservation{}, &model.ResourceScope{}, &model.OutboxEvent{},
	))
	var providerMemberships int64
	require.NoError(t, fixture.database.Model(&model.AdminProviderMembership{}).
		Where("user_id = ? AND provider_id = ?", fixture.user.ID, fixture.provider.ID).Count(&providerMemberships).Error)
	if providerMemberships == 0 {
		require.NoError(t, fixture.database.Create(&model.AdminProviderMembership{
			ID: uuid.NewString(), UserID: fixture.user.ID, ProviderID: fixture.provider.ID,
			Role: model.ProviderManagementRoleAdmin, Enabled: true, ValidFrom: time.Now().Add(-time.Minute),
			PermissionRevision: 5, CreatedByUserID: fixture.user.ID, Reason: "Provider API fixture", RowVersion: 1,
		}).Error)
	}
	var currentMembership model.AdminProviderMembership
	require.NoError(t, fixture.database.Where("user_id = ? AND provider_id = ? AND enabled = ? AND valid_from <= ?",
		fixture.user.ID, fixture.provider.ID, true, time.Now()).First(&currentMembership).Error)

	management := fixture.router.Group("/api/v1/management")
	management.Use(AuthMiddleware(fixture.config.Security.JWTSecret, false))
	management.Use(RequireFeatureFlag(config.FeatureFlagsSection{ManagementContextV2: true}, config.FeatureManagementContextV2, false))
	management.Use(UnifiedManagementIdentityMiddleware())
	provider := management.Group("/provider")
	providerAPI := NewProviderSupplyAPI()
	providerGovernanceAPI := NewProviderGovernanceAPI()
	provider.GET("/memberships", RequireManagementPermission(service.PermissionProviderMembershipsRead), providerGovernanceAPI.ListMemberships)
	provider.GET("/audit-logs", RequireManagementPermission(service.PermissionProviderAuditRead), providerGovernanceAPI.ListAuditLogs)
	provider.GET("/technical-resources/:id", RequireManagementPermission(service.PermissionProviderTechnicalResourcesRead), providerAPI.GetTechnicalResource)
	provider.GET("/resources", RequireManagementPermission(service.PermissionProviderResourcesRead), providerAPI.ListPlatformResources)
	provider.GET("/resources/:id", RequireManagementPermission(service.PermissionProviderResourcesRead), providerAPI.GetPlatformResource)
	provider.GET("/scopes", RequireManagementPermission(service.PermissionProviderResourcesRead), providerAPI.ListResourceScopes)
	provider.GET("/scopes/:id", RequireManagementPermission(service.PermissionProviderResourcesRead), RequireManagementPermission(service.PermissionProviderIsolationEvidenceRead), providerAPI.GetResourceScope)
	provider.POST("/resources/:id/suspend", RequireManagementPermission(service.PermissionProviderResourcesWrite), RequireFeatureFlag(flags, config.FeatureResourceModelWrite, true), RequireIfMatch(), providerAPI.SetPlatformResourceLifecycle(model.PlatformResourceSuspended, "suspend_platform_resource"))
	provider.POST("/technical-resources", RequireManagementPermission(service.PermissionProviderTechnicalResourcesWrite), RequireFeatureFlag(flags, config.FeatureResourceModelWrite, true), RequireIdempotencyKey(), providerAPI.CreateTechnicalResource)
	provider.POST("/supply-candidates/:id/accept", RequireManagementPermission(service.PermissionProviderResourcesWrite), RequireFeatureFlag(flags, config.FeatureResourceModelWrite, true), RequireIfMatch(), RequireIdempotencyKey(), providerAPI.AcceptSupplyCandidate)
	return fixture, fixture.login(t, fixture.admin.Username)
}

func TestProviderGovernanceAPIReadsOnlyCurrentProvider(t *testing.T) {
	fixture, login := prepareProviderSupplyAPIFixture(t, config.FeatureFlagsSection{})
	now := time.Now().UTC()
	otherUser := model.User{Name: "other-provider-admin", Alias: "Other Provider Admin", Role: model.UserRoleClient, SecretHash: "fixture", Enabled: true}
	require.NoError(t, fixture.database.Create(&otherUser).Error)
	require.NoError(t, fixture.database.Create(&model.UserIdentityProfile{
		UserID: otherUser.ID, Username: "other-provider-admin", DisplayName: "Other Provider Admin",
		Enabled: true, AuthRevision: 1, RowVersion: 1,
	}).Error)
	require.NoError(t, fixture.database.Create(&model.AdminProviderMembership{
		ID: uuid.NewString(), UserID: otherUser.ID, ProviderID: fixture.otherProvider.ID,
		Role: model.ProviderManagementRoleViewer, Enabled: true, ValidFrom: now.Add(-time.Minute),
		PermissionRevision: 1, CreatedByUserID: fixture.user.ID, Reason: "other provider only", RowVersion: 1,
	}).Error)
	require.NoError(t, fixture.database.Create(&model.AuditLog{
		ScopeType: string(model.ManagementScopeProvider), ScopeID: fixture.provider.ID,
		ActorUsername: "mapped-admin", ActorUserID: fixture.user.ID, EffectiveUserID: fixture.user.ID,
		RequiredPermission: service.PermissionProviderResourcesWrite, PermissionRevision: 5,
		ActionType: "provider_a_action", TargetType: "platform_resource", TargetID: "resource-a", TargetName: "Resource A",
		RequestID: "provider-a-request",
	}).Error)
	require.NoError(t, fixture.database.Create(&model.AuditLog{
		ScopeType: string(model.ManagementScopeProvider), ScopeID: fixture.otherProvider.ID,
		ActorUsername: "other-provider-admin", ActorUserID: otherUser.ID, EffectiveUserID: otherUser.ID,
		RequiredPermission: service.PermissionProviderResourcesWrite, PermissionRevision: 1,
		ActionType: "provider_b_action", TargetType: "platform_resource", TargetID: "resource-b", TargetName: "Resource B",
		RequestID: "provider-b-request",
	}).Error)

	memberships := providerAPIRequest(fixture, login, http.MethodGet, "/api/v1/management/provider/memberships?size=100", fixture.provider.ID, "", nil)
	require.Equal(t, http.StatusOK, memberships.Code, memberships.Body.String())
	require.Contains(t, memberships.Body.String(), fixture.user.Name)
	require.NotContains(t, memberships.Body.String(), otherUser.Name)

	audit := providerAPIRequest(fixture, login, http.MethodGet, "/api/v1/management/provider/audit-logs?size=100", fixture.provider.ID, "", nil)
	require.Equal(t, http.StatusOK, audit.Code, audit.Body.String())
	require.Contains(t, audit.Body.String(), "provider-a-request")
	require.NotContains(t, audit.Body.String(), "provider-b-request")

	invalidState := providerAPIRequest(fixture, login, http.MethodGet, "/api/v1/management/provider/memberships?state=unknown", fixture.provider.ID, "", nil)
	require.Equal(t, http.StatusBadRequest, invalidState.Code, invalidState.Body.String())
}

func providerAPIRequest(fixture managementContextAPIFixture, login LoginResponse, method, path, providerID, body string, headers map[string]string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+login.Token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(HeaderManagementScopeType, string(model.ManagementScopeProvider))
	request.Header.Set(HeaderManagementScopeID, providerID)
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response := httptest.NewRecorder()
	fixture.router.ServeHTTP(response, request)
	return response
}

func TestProviderSupplyAPIReadsOnlyCurrentProvider(t *testing.T) {
	fixture, login := prepareProviderSupplyAPIFixture(t, config.FeatureFlagsSection{ResourceModelWrite: true})
	now := time.Now().UTC()
	resourceA := model.PlatformResource{
		ID: uuid.NewString(), ProviderID: fixture.provider.ID, Type: model.SupplyResourceKubernetes,
		StableKey: "cluster-api-a", DisplayName: "Cluster API A", LifecycleState: model.PlatformResourceActive,
		HealthState: model.ResourceHealthOnline, CapabilityRevision: 1, RowVersion: 1,
	}
	resourceB := model.PlatformResource{
		ID: uuid.NewString(), ProviderID: fixture.otherProvider.ID, Type: model.SupplyResourceKubernetes,
		StableKey: "cluster-api-b", DisplayName: "Cluster API B", LifecycleState: model.PlatformResourceActive,
		HealthState: model.ResourceHealthOnline, CapabilityRevision: 1, RowVersion: 1,
	}
	require.NoError(t, fixture.database.Create(&resourceA).Error)
	require.NoError(t, fixture.database.Create(&resourceB).Error)
	scopeA := model.ResourceScope{
		ID: uuid.NewString(), ProviderID: fixture.provider.ID, PlatformResourceID: resourceA.ID,
		Type: model.ResourceScopeCluster, StableKey: "cluster-scope-api-a", LifecycleState: model.ResourceScopeActive,
		ConfigRevision: 1, EvidenceRevision: 1, RowVersion: 1,
	}
	scopeB := model.ResourceScope{
		ID: uuid.NewString(), ProviderID: fixture.otherProvider.ID, PlatformResourceID: resourceB.ID,
		Type: model.ResourceScopeCluster, StableKey: "cluster-scope-api-b", LifecycleState: model.ResourceScopeActive,
		ConfigRevision: 1, EvidenceRevision: 1, RowVersion: 1,
	}
	require.NoError(t, fixture.database.Create(&scopeA).Error)
	require.NoError(t, fixture.database.Create(&scopeB).Error)

	list := providerAPIRequest(fixture, login, http.MethodGet, "/api/v1/management/provider/resources?size=100", fixture.provider.ID, "", nil)
	require.Equal(t, http.StatusOK, list.Code, list.Body.String())
	var listBody PagedResponse
	require.NoError(t, json.Unmarshal(list.Body.Bytes(), &listBody))
	require.Equal(t, int64(1), listBody.Total)
	require.NotContains(t, list.Body.String(), resourceB.ID)
	scopeList := providerAPIRequest(fixture, login, http.MethodGet, "/api/v1/management/provider/scopes?size=100", fixture.provider.ID, "", nil)
	require.Equal(t, http.StatusOK, scopeList.Code, scopeList.Body.String())
	require.Contains(t, scopeList.Body.String(), scopeA.ID)
	require.NotContains(t, scopeList.Body.String(), scopeB.ID)
	detail := providerAPIRequest(fixture, login, http.MethodGet, "/api/v1/management/provider/resources/"+resourceA.ID, fixture.provider.ID, "", nil)
	require.Equal(t, http.StatusOK, detail.Code, detail.Body.String())
	var detailBody map[string]any
	require.NoError(t, json.Unmarshal(detail.Body.Bytes(), &detailBody))
	detailData, ok := detailBody["data"].(map[string]any)
	require.True(t, ok, detail.Body.String())
	require.Contains(t, detailData, "resource")
	require.Contains(t, detailData, "sources")
	require.Contains(t, detailData, "scopes")
	require.NotContains(t, detailData, "Resource")
	require.NotContains(t, detailData, "Sources")
	require.NotContains(t, detailData, "Scopes")

	foreign := providerAPIRequest(fixture, login, http.MethodGet, "/api/v1/management/provider/resources/"+resourceB.ID, fixture.provider.ID, "", nil)
	require.Equal(t, http.StatusNotFound, foreign.Code, foreign.Body.String())
	assertResponseErrorCode(t, foreign, ErrorCodeManagementObjectMissing)

	invalidFilter := providerAPIRequest(fixture, login, http.MethodGet, "/api/v1/management/provider/resources?state=unknown", fixture.provider.ID, "", nil)
	require.Equal(t, http.StatusBadRequest, invalidFilter.Code, invalidFilter.Body.String())

	require.NoError(t, fixture.database.Create(&model.AdminProviderMembership{
		ID: uuid.NewString(), UserID: fixture.user.ID, ProviderID: fixture.otherProvider.ID,
		Role: model.ProviderManagementRoleViewer, Enabled: true, ValidFrom: now.Add(-time.Minute),
		PermissionRevision: 1, CreatedByUserID: fixture.user.ID, Reason: "Provider B API fixture", RowVersion: 1,
	}).Error)
	otherList := providerAPIRequest(fixture, login, http.MethodGet, "/api/v1/management/provider/resources?size=100", fixture.otherProvider.ID, "", nil)
	require.Equal(t, http.StatusOK, otherList.Code, otherList.Body.String())
	require.Contains(t, otherList.Body.String(), resourceB.ID)
	require.NotContains(t, otherList.Body.String(), resourceA.ID)
}

func TestProviderSupplyAPICreateIsIdempotentAndTransactional(t *testing.T) {
	fixture, login := prepareProviderSupplyAPIFixture(t, config.FeatureFlagsSection{ResourceModelWrite: true})
	body := `{"type":"agent","stable_key":"agent-api-a","runtime_name":"agent-api-a","credential_revision":1,"reason":"register API agent"}`
	headers := map[string]string{HeaderIdempotencyKey: "provider-create-agent-a"}
	created := providerAPIRequest(fixture, login, http.MethodPost, providerTechnicalResourceCreateRoute, fixture.provider.ID, body, headers)
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	require.Equal(t, `"1"`, created.Header().Get("ETag"))

	replayed := providerAPIRequest(fixture, login, http.MethodPost, providerTechnicalResourceCreateRoute, fixture.provider.ID,
		`{"reason":"register API agent","credential_revision":1,"runtime_name":"agent-api-a","stable_key":"agent-api-a","type":"agent"}`, headers)
	require.Equal(t, http.StatusCreated, replayed.Code, replayed.Body.String())
	require.JSONEq(t, created.Body.String(), replayed.Body.String())
	require.Equal(t, `"1"`, replayed.Header().Get("ETag"))

	var resourceCount, outboxCount, auditCount, idempotencyCount int64
	require.NoError(t, fixture.database.Model(&model.TechnicalResource{}).Count(&resourceCount).Error)
	require.NoError(t, fixture.database.Model(&model.OutboxEvent{}).Count(&outboxCount).Error)
	require.NoError(t, fixture.database.Model(&model.AuditLog{}).Where("action_type = ?", "create_technical_resource").Count(&auditCount).Error)
	require.NoError(t, fixture.database.Model(&model.APIIdempotencyRecord{}).Count(&idempotencyCount).Error)
	require.Equal(t, int64(1), resourceCount)
	require.Equal(t, int64(1), outboxCount)
	require.Equal(t, int64(1), auditCount)
	require.Equal(t, int64(1), idempotencyCount)

	reused := providerAPIRequest(fixture, login, http.MethodPost, providerTechnicalResourceCreateRoute, fixture.provider.ID,
		`{"type":"agent","stable_key":"agent-api-other","runtime_name":"agent-api-other","credential_revision":1,"reason":"different request"}`, headers)
	require.Equal(t, http.StatusConflict, reused.Code, reused.Body.String())
	assertResponseErrorCode(t, reused, ErrorCodeIdempotencyKeyReused)

	for _, forbiddenBody := range []string{
		`{"type":"agent","stable_key":"agent-provider-id","credential_revision":1,"reason":"bad","provider_id":"foreign"}`,
		`{"type":"agent","stable_key":"agent-tenant-id","credential_revision":1,"reason":"bad","tenant_id":"tenant"}`,
	} {
		response := providerAPIRequest(fixture, login, http.MethodPost, providerTechnicalResourceCreateRoute, fixture.provider.ID,
			forbiddenBody, map[string]string{HeaderIdempotencyKey: uuid.NewString()})
		require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
		assertResponseErrorCode(t, response, ErrorCodeInvalidArgument)
	}
}

func TestProviderSupplyAPIAuditFailureRollsBackMutationAndOutbox(t *testing.T) {
	fixture, login := prepareProviderSupplyAPIFixture(t, config.FeatureFlagsSection{ResourceModelWrite: true})
	require.NoError(t, fixture.database.Migrator().DropTable(&model.AuditLog{}))

	response := providerAPIRequest(fixture, login, http.MethodPost, providerTechnicalResourceCreateRoute, fixture.provider.ID,
		`{"type":"agent","stable_key":"agent-audit-failure","runtime_name":"agent-audit-failure","credential_revision":1,"reason":"verify atomic rollback"}`,
		map[string]string{HeaderIdempotencyKey: "provider-audit-failure"})
	require.Equal(t, http.StatusServiceUnavailable, response.Code, response.Body.String())
	assertResponseErrorCode(t, response, ErrorCodeProviderSupplyAuditFailed)

	var resourceCount, outboxCount int64
	require.NoError(t, fixture.database.Model(&model.TechnicalResource{}).Count(&resourceCount).Error)
	require.NoError(t, fixture.database.Model(&model.OutboxEvent{}).Count(&outboxCount).Error)
	require.Zero(t, resourceCount)
	require.Zero(t, outboxCount)

	var record model.APIIdempotencyRecord
	require.NoError(t, fixture.database.First(&record).Error)
	require.Equal(t, model.APIIdempotencyProcessing, record.Status)
}

func TestProviderSupplyAPIStateRequiresIfMatchAndWritesAuditOutbox(t *testing.T) {
	fixture, login := prepareProviderSupplyAPIFixture(t, config.FeatureFlagsSection{ResourceModelWrite: true})
	resource := model.PlatformResource{
		ID: uuid.NewString(), ProviderID: fixture.provider.ID, Type: model.SupplyResourceKubernetes,
		StableKey: "cluster-state-api", DisplayName: "Cluster State API", LifecycleState: model.PlatformResourceActive,
		HealthState: model.ResourceHealthOnline, CapabilityRevision: 1, RowVersion: 1,
	}
	require.NoError(t, fixture.database.Create(&resource).Error)
	path := "/api/v1/management/provider/resources/" + resource.ID + "/suspend"
	body := `{"reason":"API maintenance"}`

	missing := providerAPIRequest(fixture, login, http.MethodPost, path, fixture.provider.ID, body, nil)
	require.Equal(t, http.StatusPreconditionRequired, missing.Code, missing.Body.String())

	stale := providerAPIRequest(fixture, login, http.MethodPost, path, fixture.provider.ID, body, map[string]string{HeaderIfMatch: `"2"`})
	require.Equal(t, http.StatusConflict, stale.Code, stale.Body.String())
	assertResponseErrorCode(t, stale, ErrorCodeProviderSupplyVersion)

	unknown := providerAPIRequest(fixture, login, http.MethodPost, path, fixture.provider.ID,
		`{"reason":"API maintenance","tenant_id":"forbidden"}`, map[string]string{HeaderIfMatch: `"1"`})
	require.Equal(t, http.StatusBadRequest, unknown.Code, unknown.Body.String())

	updated := providerAPIRequest(fixture, login, http.MethodPost, path, fixture.provider.ID, body, map[string]string{HeaderIfMatch: `"1"`})
	require.Equal(t, http.StatusOK, updated.Code, updated.Body.String())
	require.Equal(t, `"2"`, updated.Header().Get("ETag"))

	var persisted model.PlatformResource
	require.NoError(t, fixture.database.First(&persisted, "id = ?", resource.ID).Error)
	require.Equal(t, model.PlatformResourceSuspended, persisted.LifecycleState)
	require.Equal(t, int64(2), persisted.RowVersion)
	var outbox model.OutboxEvent
	require.NoError(t, fixture.database.First(&outbox, "aggregate_id = ?", resource.ID).Error)
	require.Equal(t, int64(2), outbox.AggregateRevision)
	var audit model.AuditLog
	require.NoError(t, fixture.database.First(&audit, "action_type = ?", "suspend_platform_resource").Error)
	require.Equal(t, fixture.user.ID, audit.ActorUserID)
	require.Equal(t, fixture.user.ID, audit.EffectiveUserID)
	require.Equal(t, fixture.provider.ID, audit.ScopeID)
}

func TestProviderSupplyAPIAcceptCandidateCreatesDraftScopesOnce(t *testing.T) {
	fixture, login := prepareProviderSupplyAPIFixture(t, config.FeatureFlagsSection{ResourceModelWrite: true})
	now := time.Now().UTC()
	technical := model.TechnicalResource{
		ID: uuid.NewString(), ProviderID: fixture.provider.ID, Type: model.TechnicalResourceAgent,
		StableKey: "candidate-api-agent", LifecycleState: model.TechnicalResourceRegistered,
		HealthState: model.ResourceHealthOnline, CredentialRevision: 1, ConfigRevision: 1, RowVersion: 1,
	}
	require.NoError(t, fixture.database.Create(&technical).Error)
	clusterUID := "candidate-api-cluster"
	stableKey := fmt.Sprintf("%x", sha256.Sum256([]byte("kubernetes-cluster-v1:cluster_uid\x00"+clusterUID)))
	candidate := model.SupplyCandidate{
		ID: uuid.NewString(), ProviderID: fixture.provider.ID, TechnicalResourceID: technical.ID,
		ResourceType: model.SupplyResourceKubernetes, StableKey: stableKey, IdentityQuality: model.SupplyIdentityStrong,
		PayloadHash: strings.Repeat("a", 64), ObservationSnapshot: `{"cluster_uid":"candidate-api-cluster","display_name":"Candidate API Cluster","namespaces":[{"uid":"candidate-api-namespace","name":"workloads","labels":{"environment":"test"},"status":"Active"}]}`,
		FirstObservedAt: now.Add(-time.Minute), LastObservedAt: now, LeaseExpiresAt: now.Add(10 * time.Minute),
		ReviewState: model.SupplyCandidatePendingReview, RowVersion: 1,
	}
	require.NoError(t, fixture.database.Create(&candidate).Error)
	path := "/api/v1/management/provider/supply-candidates/" + candidate.ID + "/accept"
	headers := map[string]string{HeaderIfMatch: `"1"`, HeaderIdempotencyKey: "accept-candidate-api"}
	body := `{"display_name":"Managed API Cluster","reason":"accept verified candidate"}`

	accepted := providerAPIRequest(fixture, login, http.MethodPost, path, fixture.provider.ID, body, headers)
	require.Equal(t, http.StatusOK, accepted.Code, accepted.Body.String())
	require.Equal(t, `"2"`, accepted.Header().Get("ETag"))
	replayed := providerAPIRequest(fixture, login, http.MethodPost, path, fixture.provider.ID, body, headers)
	require.Equal(t, http.StatusOK, replayed.Code, replayed.Body.String())
	require.JSONEq(t, accepted.Body.String(), replayed.Body.String())

	var persistedCandidate model.SupplyCandidate
	require.NoError(t, fixture.database.First(&persistedCandidate, "id = ?", candidate.ID).Error)
	require.Equal(t, model.SupplyCandidateLinked, persistedCandidate.ReviewState)
	require.Equal(t, int64(2), persistedCandidate.RowVersion)
	var resourceCount, sourceCount, scopeCount, outboxCount, auditCount int64
	require.NoError(t, fixture.database.Model(&model.PlatformResource{}).Count(&resourceCount).Error)
	require.NoError(t, fixture.database.Model(&model.PlatformResourceSource{}).Count(&sourceCount).Error)
	require.NoError(t, fixture.database.Model(&model.ResourceScope{}).Count(&scopeCount).Error)
	require.NoError(t, fixture.database.Model(&model.OutboxEvent{}).Where("aggregate_id = ?", candidate.ID).Count(&outboxCount).Error)
	require.NoError(t, fixture.database.Model(&model.AuditLog{}).Where("action_type = ?", "accept_supply_candidate").Count(&auditCount).Error)
	require.Equal(t, int64(1), resourceCount)
	require.Equal(t, int64(1), sourceCount)
	require.Equal(t, int64(2), scopeCount)
	require.Equal(t, int64(1), outboxCount)
	require.Equal(t, int64(1), auditCount)
}

func TestProviderSupplyAPIWriteFlagAndCreateHeadersFailClosed(t *testing.T) {
	t.Run("resource write flag disabled", func(t *testing.T) {
		fixture, login := prepareProviderSupplyAPIFixture(t, config.FeatureFlagsSection{})
		response := providerAPIRequest(fixture, login, http.MethodPost, providerTechnicalResourceCreateRoute, fixture.provider.ID,
			`{"type":"agent","stable_key":"disabled-agent","runtime_name":"disabled-agent","credential_revision":1,"reason":"must stay disabled"}`, nil)
		require.Equal(t, http.StatusServiceUnavailable, response.Code, response.Body.String())
		assertResponseErrorCode(t, response, ErrorCodeFeatureDisabled)
		var resources int64
		require.NoError(t, fixture.database.Model(&model.TechnicalResource{}).Count(&resources).Error)
		require.Zero(t, resources)
		var audit model.AuditLog
		require.NoError(t, fixture.database.First(&audit, "action_type = ?", "feature_flag_write_rejected").Error)
		require.Equal(t, fixture.provider.ID, audit.ScopeID)
	})

	t.Run("accept requires both precondition and idempotency key", func(t *testing.T) {
		fixture, login := prepareProviderSupplyAPIFixture(t, config.FeatureFlagsSection{ResourceModelWrite: true})
		path := "/api/v1/management/provider/supply-candidates/" + uuid.NewString() + "/accept"
		response := providerAPIRequest(fixture, login, http.MethodPost, path, fixture.provider.ID,
			`{"reason":"accept candidate"}`, map[string]string{HeaderIfMatch: `"1"`})
		require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
		assertResponseErrorCode(t, response, ErrorCodeIdempotencyKeyRequired)
	})
}
