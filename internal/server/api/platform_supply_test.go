package api

import (
	"encoding/json"
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

func TestPlatformSupplyConflictAPIIsScopedAggregatedAndFiltered(t *testing.T) {
	fixture, login := prepareProviderSupplyAPIFixture(t, config.FeatureFlagsSection{})
	management := fixture.router.Group("/api/v1/management")
	management.Use(AuthMiddleware(fixture.config.Security.JWTSecret, false))
	management.Use(RequireFeatureFlag(config.FeatureFlagsSection{ManagementContextV2: true}, config.FeatureManagementContextV2, false))
	management.Use(UnifiedManagementIdentityMiddleware())
	management.GET("/platform/supply-conflicts", RequireManagementPermission(service.PermissionPlatformResourcesRead), NewPlatformSupplyAPI().ListConflicts)

	now := time.Now().UTC()
	technicalA := model.TechnicalResource{
		ID: uuid.NewString(), ProviderID: fixture.provider.ID, Type: model.TechnicalResourceAgent,
		StableKey: "platform-conflict-agent-a", LifecycleState: model.TechnicalResourceRegistered,
		HealthState: model.ResourceHealthOnline, CredentialRevision: 1, ConfigRevision: 1, RowVersion: 1,
	}
	technicalB := model.TechnicalResource{
		ID: uuid.NewString(), ProviderID: fixture.otherProvider.ID, Type: model.TechnicalResourceAgent,
		StableKey: "platform-conflict-agent-b", LifecycleState: model.TechnicalResourceRegistered,
		HealthState: model.ResourceHealthOnline, CredentialRevision: 1, ConfigRevision: 1, RowVersion: 1,
	}
	require.NoError(t, fixture.database.Create(&technicalA).Error)
	require.NoError(t, fixture.database.Create(&technicalB).Error)

	conflictKubernetes := strings.Repeat("a", 64)
	conflictHost := strings.Repeat("b", 64)
	expiredConflict := strings.Repeat("c", 64)
	stableKubernetes := "secret-stable-kubernetes"
	stableHost := "secret-stable-host"
	candidates := []model.SupplyCandidate{
		platformConflictCandidate(technicalA, model.SupplyResourceKubernetes, stableKubernetes, conflictKubernetes, now, now.Add(time.Hour), "secret-snapshot-a"),
		platformConflictCandidate(technicalB, model.SupplyResourceKubernetes, stableKubernetes, conflictKubernetes, now, now.Add(time.Hour), "secret-snapshot-b"),
		platformConflictCandidate(technicalA, model.SupplyResourceHost, stableHost, conflictHost, now.Add(time.Minute), now.Add(time.Hour), "secret-snapshot-c"),
		platformConflictCandidate(technicalB, model.SupplyResourceHost, stableHost, conflictHost, now.Add(time.Minute), now.Add(time.Hour), "secret-snapshot-d"),
		platformConflictCandidate(technicalA, model.SupplyResourceHost, "expired-secret", expiredConflict, now.Add(-2*time.Hour), now.Add(-time.Hour), "expired-secret-snapshot"),
		platformConflictCandidate(technicalB, model.SupplyResourceHost, "expired-secret", expiredConflict, now.Add(-2*time.Hour), now.Add(-time.Hour), "expired-secret-snapshot"),
	}
	for index := range candidates {
		require.NoError(t, fixture.database.Create(&candidates[index]).Error)
	}

	request := func(path string, scope model.ManagementScopeType, scopeID string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+login.Token)
		req.Header.Set(HeaderManagementScopeType, string(scope))
		if scopeID != "" {
			req.Header.Set(HeaderManagementScopeID, scopeID)
		}
		response := httptest.NewRecorder()
		fixture.router.ServeHTTP(response, req)
		return response
	}

	response := request("/api/v1/management/platform/supply-conflicts", model.ManagementScopePlatform, "")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var page struct {
		Data  []map[string]any `json:"data"`
		Total int64            `json:"total"`
		Page  int              `json:"page"`
		Size  int              `json:"size"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &page))
	require.Equal(t, int64(2), page.Total)
	require.Len(t, page.Data, 2)
	for _, item := range page.Data {
		require.Len(t, item, 4)
		require.Contains(t, item, "opaque_conflict_id")
		require.Contains(t, item, "resource_type")
		require.Equal(t, float64(2), item["provider_count"])
		require.Equal(t, float64(2), item["candidate_count"])
	}
	for _, secret := range []string{
		technicalA.ID, technicalB.ID, fixture.provider.ID, fixture.otherProvider.ID,
		candidates[0].ID, candidates[1].ID, candidates[2].ID, candidates[3].ID, candidates[4].ID, candidates[5].ID,
		stableKubernetes, stableHost, "expired-secret", "secret-snapshot", expiredConflict,
	} {
		require.NotContains(t, response.Body.String(), secret)
	}

	filtered := request("/api/v1/management/platform/supply-conflicts?type=kubernetes&search="+conflictKubernetes[:12]+"&page=1&size=1", model.ManagementScopePlatform, "")
	require.Equal(t, http.StatusOK, filtered.Code, filtered.Body.String())
	require.NoError(t, json.Unmarshal(filtered.Body.Bytes(), &page))
	require.Equal(t, int64(1), page.Total)
	require.Len(t, page.Data, 1)
	require.Equal(t, conflictKubernetes, page.Data[0]["opaque_conflict_id"])

	secondPage := request("/api/v1/management/platform/supply-conflicts?page=2&size=1", model.ManagementScopePlatform, "")
	require.Equal(t, http.StatusOK, secondPage.Code, secondPage.Body.String())
	require.NoError(t, json.Unmarshal(secondPage.Body.Bytes(), &page))
	require.Equal(t, int64(2), page.Total)
	require.Len(t, page.Data, 1)

	unknown := request("/api/v1/management/platform/supply-conflicts?provider_id=secret", model.ManagementScopePlatform, "")
	require.Equal(t, http.StatusBadRequest, unknown.Code, unknown.Body.String())
	invalidType := request("/api/v1/management/platform/supply-conflicts?type=database", model.ManagementScopePlatform, "")
	require.Equal(t, http.StatusBadRequest, invalidType.Code, invalidType.Body.String())
	providerScoped := request("/api/v1/management/platform/supply-conflicts", model.ManagementScopeProvider, fixture.provider.ID)
	require.Equal(t, http.StatusForbidden, providerScoped.Code, providerScoped.Body.String())
}

func platformConflictCandidate(technical model.TechnicalResource, resourceType model.SupplyResourceType, stableKey, opaqueID string, observedAt, leaseExpiresAt time.Time, snapshot string) model.SupplyCandidate {
	return model.SupplyCandidate{
		ID: uuid.NewString(), ProviderID: technical.ProviderID, TechnicalResourceID: technical.ID,
		ResourceType: resourceType, StableKey: stableKey, IdentityQuality: model.SupplyIdentityCollision,
		PayloadHash: strings.Repeat("d", 64), ObservationSnapshot: snapshot,
		FirstObservedAt: observedAt, LastObservedAt: observedAt, LeaseExpiresAt: leaseExpiresAt,
		ReviewState: model.SupplyCandidateConflict, ConflictCode: "CROSS_PROVIDER_SUPPLY_IDENTITY_CONFLICT",
		OpaqueConflictID: opaqueID, RowVersion: 1,
	}
}
