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

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/service"
)

type platformAllocationAPIFixture struct {
	managementContextAPIFixture
	login LoginResponse
	scope model.ResourceScope
}

func preparePlatformAllocationAPIFixture(t *testing.T, flags config.FeatureFlagsSection) platformAllocationAPIFixture {
	t.Helper()
	fixture := newManagementContextAPIFixture(t)
	require.NoError(t, fixture.database.AutoMigrate(
		&model.Node{}, &model.TechnicalResource{}, &model.TechnicalResourceBinding{}, &model.SupplyCandidate{},
		&model.PlatformResource{}, &model.PlatformResourceSource{}, &model.NamespaceObservation{}, &model.ResourceScope{},
		&model.ResourceAllocation{}, &model.ResourceAllocationItem{}, &model.OutboxEvent{},
	))
	now := time.Now().UTC()
	node := model.Node{ID: 9001, UserID: fixture.user.ID, Name: "allocation-agent", Type: model.NodeTypeAgent}
	require.NoError(t, fixture.database.Create(&node).Error)
	technical := model.TechnicalResource{
		ID: uuid.NewString(), ProviderID: fixture.provider.ID, Type: model.TechnicalResourceAgent, StableKey: "allocation-agent", DomainLabel: "allocation-agent",
		LifecycleState: model.TechnicalResourceRegistered, HealthState: model.ResourceHealthOnline,
		CredentialRevision: 1, ConfigRevision: 1, RowVersion: 1,
	}
	require.NoError(t, fixture.database.Create(&technical).Error)
	require.NoError(t, fixture.database.Create(&model.TechnicalResourceBinding{
		ID: uuid.NewString(), TechnicalResourceID: technical.ID, SourceType: model.TechnicalResourceBindingLegacyNode,
		SourceID: "9001", CredentialRevision: 1, Enabled: true, BoundByUserID: fixture.user.ID,
		Reason: "allocation API fixture", RowVersion: 1,
	}).Error)
	clusterStableKey := fmt.Sprintf("%x", sha256.Sum256([]byte("kubernetes-cluster-v1:cluster_uid\x00allocation-cluster")))
	candidate := model.SupplyCandidate{
		ID: uuid.NewString(), ProviderID: fixture.provider.ID, TechnicalResourceID: technical.ID,
		ResourceType: model.SupplyResourceKubernetes, StableKey: clusterStableKey, IdentityQuality: model.SupplyIdentityStrong,
		PayloadHash: strings.Repeat("a", 64), FirstObservedAt: now.Add(-time.Minute), LastObservedAt: now,
		LeaseExpiresAt: now.Add(time.Hour), ReviewState: model.SupplyCandidateLinked, RowVersion: 1,
	}
	require.NoError(t, fixture.database.Create(&candidate).Error)
	resource := model.PlatformResource{
		ID: uuid.NewString(), ProviderID: fixture.provider.ID, Type: model.SupplyResourceKubernetes,
		StableKey: clusterStableKey, DisplayName: "Allocation Cluster", LifecycleState: model.PlatformResourceActive,
		HealthState: model.ResourceHealthOnline, CapabilityRevision: 1, AllocatableScopeCount: 1, RowVersion: 1,
	}
	require.NoError(t, fixture.database.Create(&resource).Error)
	require.NoError(t, fixture.database.Create(&model.PlatformResourceSource{
		ID: uuid.NewString(), ProviderID: fixture.provider.ID, PlatformResourceID: resource.ID,
		SupplyCandidateID: candidate.ID, IsPrimary: true, LinkedAt: now, LastConfirmedAt: now,
	}).Error)
	observation := model.NamespaceObservation{
		ID: uuid.NewString(), ProviderID: fixture.provider.ID, ClusterResourceID: resource.ID,
		NamespaceUID: "allocation-namespace", Name: "workloads", Revision: 1, ObservedAt: now,
		LeaseExpiresAt: now.Add(time.Hour), State: model.NamespaceObservationObserved,
	}
	require.NoError(t, fixture.database.Create(&observation).Error)
	cluster := model.ResourceScope{
		ID: uuid.NewString(), ProviderID: fixture.provider.ID, PlatformResourceID: resource.ID,
		Type: model.ResourceScopeCluster, StableKey: resource.StableKey, LifecycleState: model.ResourceScopeActive,
		IsolationMode: model.ResourceScopeIsolationNone, ConfigRevision: 1, EvidenceRevision: 1, RowVersion: 1,
	}
	require.NoError(t, fixture.database.Create(&cluster).Error)
	namespaceStableKey := fmt.Sprintf("%x", sha256.Sum256([]byte("kubernetes-namespace-v1\x00"+resource.ID+"\x00"+observation.NamespaceUID)))
	scope := model.ResourceScope{
		ID: uuid.NewString(), ProviderID: fixture.provider.ID, PlatformResourceID: resource.ID,
		Type: model.ResourceScopeNamespace, StableKey: namespaceStableKey, ParentID: &cluster.ID,
		NamespaceObservationID: &observation.ID, LifecycleState: model.ResourceScopeAllocatable,
		IsolationMode: model.ResourceScopeIsolationNamespaceIsolated, ConfigRevision: 1, EvidenceRevision: 1, RowVersion: 1,
	}
	require.NoError(t, fixture.database.Create(&scope).Error)

	management := fixture.router.Group("/api/v1/management")
	management.Use(AuthMiddleware(fixture.config.Security.JWTSecret, false))
	management.Use(RequireFeatureFlag(config.FeatureFlagsSection{ManagementContextV2: true}, config.FeatureManagementContextV2, false))
	management.Use(UnifiedManagementIdentityMiddleware())
	platform := management.Group("/platform")
	allocationAPI := NewPlatformAllocationAPI()
	platform.GET("/allocations", RequireManagementPermission(service.PermissionPlatformAllocationsRead), allocationAPI.List)
	platform.GET("/allocations/:id", RequireManagementPermission(service.PermissionPlatformAllocationsRead), allocationAPI.Get)
	writeMiddleware := []gin.HandlerFunc{
		RequireManagementPermission(service.PermissionPlatformAllocationsWrite),
		RequireFeatureFlag(flags, config.FeatureResourceModelWrite, true),
		RequireFeatureFlag(flags, config.FeatureResourceAllocation, true),
	}
	platform.POST("/allocations", append(writeMiddleware, RequireIdempotencyKey(), allocationAPI.Create)...)
	platform.POST("/allocations/:id/activate", append(writeMiddleware, RequireIfMatch(), RequireIdempotencyKey(), allocationAPI.Activate)...)
	platform.POST("/allocations/:id/revoke", append(writeMiddleware, RequireIfMatch(), RequireIdempotencyKey(), allocationAPI.Revoke)...)
	return platformAllocationAPIFixture{managementContextAPIFixture: fixture, login: fixture.login(t, fixture.admin.Username), scope: scope}
}

func platformAllocationAPIRequest(fixture platformAllocationAPIFixture, method, path, body string, headers map[string]string, scope model.ManagementScopeType, scopeID string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+fixture.login.Token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(HeaderManagementScopeType, string(scope))
	if scopeID != "" {
		request.Header.Set(HeaderManagementScopeID, scopeID)
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response := httptest.NewRecorder()
	fixture.router.ServeHTTP(response, request)
	return response
}

func platformAllocationCreateBody(fixture platformAllocationAPIFixture, mode string) string {
	return fmt.Sprintf(`{"tenant_id":%q,"mode":%q,"scope_id":%q,"valid_from":%q,"expires_at":%q,"contract_ref":"contract-api"}`,
		fixture.tenant.ID, mode, fixture.scope.ID, time.Now().UTC().Add(-time.Minute).Format(time.RFC3339), time.Now().UTC().Add(time.Hour).Format(time.RFC3339))
}

func TestPlatformAllocationAPICreateActivateAndReplayAreTransactional(t *testing.T) {
	fixture := preparePlatformAllocationAPIFixture(t, config.FeatureFlagsSection{ResourceModelWrite: true, ResourceAllocation: true})
	body := platformAllocationCreateBody(fixture, string(model.ResourceAllocationLeased))
	createHeaders := map[string]string{HeaderIdempotencyKey: "allocation-create-api"}
	created := platformAllocationAPIRequest(fixture, http.MethodPost, platformAllocationCreateRoute, body, createHeaders, model.ManagementScopePlatform, "")
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	require.Equal(t, `"1"`, created.Header().Get("ETag"))
	replayed := platformAllocationAPIRequest(fixture, http.MethodPost, platformAllocationCreateRoute, body, createHeaders, model.ManagementScopePlatform, "")
	require.Equal(t, http.StatusCreated, replayed.Code, replayed.Body.String())
	require.JSONEq(t, created.Body.String(), replayed.Body.String())

	var response struct {
		Data struct {
			Result model.ResourceAllocation `json:"result"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &response))
	allocationID := response.Data.Result.ID
	require.NotEmpty(t, allocationID)

	activateHeaders := map[string]string{HeaderIdempotencyKey: "allocation-activate-api", HeaderIfMatch: `"1"`}
	activated := platformAllocationAPIRequest(fixture, http.MethodPost, "/api/v1/management/platform/allocations/"+allocationID+"/activate", `{"reason":"activate API allocation"}`, activateHeaders, model.ManagementScopePlatform, "")
	require.Equal(t, http.StatusOK, activated.Code, activated.Body.String())
	require.Equal(t, `"2"`, activated.Header().Get("ETag"))
	activateReplay := platformAllocationAPIRequest(fixture, http.MethodPost, "/api/v1/management/platform/allocations/"+allocationID+"/activate", `{"reason":"activate API allocation"}`, activateHeaders, model.ManagementScopePlatform, "")
	require.Equal(t, http.StatusOK, activateReplay.Code, activateReplay.Body.String())
	require.JSONEq(t, activated.Body.String(), activateReplay.Body.String())

	var allocationCount, itemCount, outboxCount, auditCount, idempotencyCount int64
	require.NoError(t, fixture.database.Model(&model.ResourceAllocation{}).Count(&allocationCount).Error)
	require.NoError(t, fixture.database.Model(&model.ResourceAllocationItem{}).Count(&itemCount).Error)
	require.NoError(t, fixture.database.Model(&model.OutboxEvent{}).Count(&outboxCount).Error)
	require.NoError(t, fixture.database.Model(&model.AuditLog{}).Where("target_type = ?", "resource_allocation").Count(&auditCount).Error)
	require.NoError(t, fixture.database.Model(&model.APIIdempotencyRecord{}).Count(&idempotencyCount).Error)
	require.Equal(t, int64(1), allocationCount)
	require.Equal(t, int64(1), itemCount)
	require.Equal(t, int64(2), outboxCount)
	require.Equal(t, int64(2), auditCount)
	require.Equal(t, int64(2), idempotencyCount)
	var createAudit model.AuditLog
	require.NoError(t, fixture.database.Where("action_type = ? AND target_id = ?", "create_resource_allocation", allocationID).First(&createAudit).Error)
	require.Equal(t, fixture.user.ID, createAudit.ActorUserID)
	require.Equal(t, fixture.user.ID, createAudit.EffectiveUserID)
	require.Equal(t, string(model.ManagementScopePlatform), createAudit.ScopeType)
	require.Empty(t, createAudit.ScopeID)
	require.Equal(t, int64(7), createAudit.PermissionRevision)
	var createdEvent model.OutboxEvent
	require.NoError(t, fixture.database.Where("aggregate_id = ? AND event_type = ?", allocationID, "resource_allocation.created").First(&createdEvent).Error)
	require.Equal(t, service.PlatformAllocationOutboxConsumer, createdEvent.Consumer)
	require.NotContains(t, createdEvent.Payload, "contract_ref")
	require.NotContains(t, createdEvent.Payload, "reason")

	detail := platformAllocationAPIRequest(fixture, http.MethodGet, "/api/v1/management/platform/allocations/"+allocationID, "", nil, model.ManagementScopePlatform, "")
	require.Equal(t, http.StatusOK, detail.Code, detail.Body.String())
	require.Equal(t, `"2"`, detail.Header().Get("ETag"))
	list := platformAllocationAPIRequest(fixture, http.MethodGet, "/api/v1/management/platform/allocations?tenant_id="+fixture.tenant.ID, "", nil, model.ManagementScopePlatform, "")
	require.Equal(t, http.StatusOK, list.Code, list.Body.String())
	require.Contains(t, list.Body.String(), allocationID)
}

func TestPlatformAllocationAPIFailsClosedOnFlagsContextAndInput(t *testing.T) {
	t.Run("allocation flag disabled", func(t *testing.T) {
		fixture := preparePlatformAllocationAPIFixture(t, config.FeatureFlagsSection{ResourceModelWrite: true})
		response := platformAllocationAPIRequest(fixture, http.MethodPost, platformAllocationCreateRoute,
			platformAllocationCreateBody(fixture, string(model.ResourceAllocationLeased)), map[string]string{HeaderIdempotencyKey: "allocation-disabled"}, model.ManagementScopePlatform, "")
		require.Equal(t, http.StatusServiceUnavailable, response.Code, response.Body.String())
		assertResponseErrorCode(t, response, ErrorCodeFeatureDisabled)
		var count int64
		require.NoError(t, fixture.database.Model(&model.ResourceAllocation{}).Count(&count).Error)
		require.Zero(t, count)
	})

	t.Run("wrong context and strict input", func(t *testing.T) {
		fixture := preparePlatformAllocationAPIFixture(t, config.FeatureFlagsSection{ResourceModelWrite: true, ResourceAllocation: true})
		body := platformAllocationCreateBody(fixture, string(model.ResourceAllocationLeased))
		provider := platformAllocationAPIRequest(fixture, http.MethodPost, platformAllocationCreateRoute, body,
			map[string]string{HeaderIdempotencyKey: "allocation-provider-context"}, model.ManagementScopeProvider, fixture.provider.ID)
		require.Equal(t, http.StatusForbidden, provider.Code, provider.Body.String())

		missingKey := platformAllocationAPIRequest(fixture, http.MethodPost, platformAllocationCreateRoute, body, nil, model.ManagementScopePlatform, "")
		require.Equal(t, http.StatusBadRequest, missingKey.Code, missingKey.Body.String())
		assertResponseErrorCode(t, missingKey, ErrorCodeIdempotencyKeyRequired)

		unknown := strings.TrimSuffix(body, "}") + `,"items":[{"scope_id":"forbidden"}]}`
		unknownResponse := platformAllocationAPIRequest(fixture, http.MethodPost, platformAllocationCreateRoute, unknown,
			map[string]string{HeaderIdempotencyKey: "allocation-unknown-field"}, model.ManagementScopePlatform, "")
		require.Equal(t, http.StatusBadRequest, unknownResponse.Code, unknownResponse.Body.String())

		shared := platformAllocationAPIRequest(fixture, http.MethodPost, platformAllocationCreateRoute,
			platformAllocationCreateBody(fixture, string(model.ResourceAllocationShared)), map[string]string{HeaderIdempotencyKey: "allocation-shared"}, model.ManagementScopePlatform, "")
		require.Equal(t, http.StatusUnprocessableEntity, shared.Code, shared.Body.String())
		assertResponseErrorCode(t, shared, ErrorCodePlatformAllocationMode)
	})

	t.Run("Platform viewer is read only", func(t *testing.T) {
		fixture := preparePlatformAllocationAPIFixture(t, config.FeatureFlagsSection{ResourceModelWrite: true, ResourceAllocation: true})
		require.NoError(t, fixture.database.Model(&model.PlatformRoleMembership{}).Where("user_id = ?", fixture.user.ID).
			Updates(map[string]any{"role": model.PlatformRoleViewer, "permission_revision": gorm.Expr("permission_revision + 1")}).Error)
		list := platformAllocationAPIRequest(fixture, http.MethodGet, "/api/v1/management/platform/allocations", "", nil, model.ManagementScopePlatform, "")
		require.Equal(t, http.StatusOK, list.Code, list.Body.String())
		write := platformAllocationAPIRequest(fixture, http.MethodPost, platformAllocationCreateRoute,
			platformAllocationCreateBody(fixture, string(model.ResourceAllocationLeased)), map[string]string{HeaderIdempotencyKey: "allocation-viewer"}, model.ManagementScopePlatform, "")
		require.Equal(t, http.StatusForbidden, write.Code, write.Body.String())
	})

	t.Run("outbox failure rolls back business and audit", func(t *testing.T) {
		fixture := preparePlatformAllocationAPIFixture(t, config.FeatureFlagsSection{ResourceModelWrite: true, ResourceAllocation: true})
		require.NoError(t, fixture.database.Migrator().DropTable(&model.OutboxEvent{}))
		response := platformAllocationAPIRequest(fixture, http.MethodPost, platformAllocationCreateRoute,
			platformAllocationCreateBody(fixture, string(model.ResourceAllocationLeased)), map[string]string{HeaderIdempotencyKey: "allocation-outbox-failure"}, model.ManagementScopePlatform, "")
		require.Equal(t, http.StatusServiceUnavailable, response.Code, response.Body.String())
		assertResponseErrorCode(t, response, ErrorCodePlatformAllocationOutboxFailed)
		var allocationCount, itemCount, auditCount int64
		require.NoError(t, fixture.database.Model(&model.ResourceAllocation{}).Count(&allocationCount).Error)
		require.NoError(t, fixture.database.Model(&model.ResourceAllocationItem{}).Count(&itemCount).Error)
		require.NoError(t, fixture.database.Model(&model.AuditLog{}).Where("target_type = ?", "resource_allocation").Count(&auditCount).Error)
		require.Zero(t, allocationCount)
		require.Zero(t, itemCount)
		require.Zero(t, auditCount)
	})
}
