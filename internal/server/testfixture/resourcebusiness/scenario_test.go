package resourcebusiness

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/stretchr/testify/require"
)

func TestCatalogContainsValidAnonymousScenarios(t *testing.T) {
	require.Equal(t, []string{
		ScenarioEmpty,
		ScenarioSingleProviderTenant,
		ScenarioProviderIdentityConflict,
		ScenarioDualTenantScopes,
		ScenarioScopeTimeConflict,
		ScenarioAdminUserSameName,
		ScenarioLegacyAmbiguity,
		ScenarioRuntimeEvolution,
	}, Names())

	for _, name := range Names() {
		t.Run(name, func(t *testing.T) {
			scenario, err := Load(name)
			require.NoError(t, err)
			require.NoError(t, Validate(scenario))
			require.False(t, ContainsSensitiveFixtureText(scenario))
		})
	}
	_, err := Load("not-registered")
	require.Error(t, err)
}

func TestScenarioFactsCoverM0DMatrix(t *testing.T) {
	empty := MustLoad(ScenarioEmpty)
	require.Empty(t, empty.Providers)
	require.Empty(t, empty.Tenants)

	normal := MustLoad(ScenarioSingleProviderTenant)
	require.Len(t, normal.Providers, 1)
	require.Len(t, normal.TechnicalResources, 1)
	require.Len(t, normal.SupplyInventories, 1)
	require.Equal(t, normal.TechnicalResources[0].ID, normal.SupplyInventories[0].TechnicalResourceID)
	require.Len(t, normal.Tenants, 1)
	require.True(t, normal.Sessions[0].ExpectedPermitted)

	providerConflict := MustLoad(ScenarioProviderIdentityConflict)
	require.Len(t, providerConflict.Providers, 2)
	require.Equal(t, providerConflict.Providers[0].StableIdentity, providerConflict.Providers[1].StableIdentity)
	require.Equal(t, providerConflict.SupplyInventories[0].ClusterUID, providerConflict.SupplyInventories[1].ClusterUID)
	require.True(t, HasIssue(providerConflict, "PROVIDER_STABLE_IDENTITY_CONFLICT"))
	require.True(t, HasIssue(providerConflict, "CROSS_PROVIDER_SUPPLY_IDENTITY_CONFLICT"))

	dualTenant := MustLoad(ScenarioDualTenantScopes)
	require.Len(t, dualTenant.Tenants, 2)
	require.NotEqual(t, dualTenant.Scopes[0].TenantID, dualTenant.Scopes[1].TenantID)
	require.NotEqual(t, dualTenant.Scopes[0].Namespace, dualTenant.Scopes[1].Namespace)

	scopeConflict := MustLoad(ScenarioScopeTimeConflict)
	require.Equal(t, "cluster", scopeConflict.Scopes[0].Kind)
	require.Equal(t, scopeConflict.Scopes[0].ID, scopeConflict.Scopes[1].ParentID)
	require.True(t, scopeConflict.Scopes[1].StartsAt.Before(scopeConflict.Scopes[0].EndsAt))

	sameName := MustLoad(ScenarioAdminUserSameName)
	require.Equal(t, sameName.Identities[0].NameToken, sameName.Identities[1].NameToken)
	require.NotEqual(t, sameName.Identities[0].ProofToken, sameName.Identities[1].ProofToken)

	legacy := MustLoad(ScenarioLegacyAmbiguity)
	require.True(t, legacy.Grants[0].Ambiguous)
	require.False(t, legacy.Grants[0].Traceable)
	require.Equal(t, "tenant_admin", legacy.Grants[0].LegacyRole)

	runtime := MustLoad(ScenarioRuntimeEvolution)
	require.Equal(t, runtime.Workloads[0].StableKey, normal.Workloads[0].StableKey)
	require.NotEqual(t, runtime.Workloads[0].PreviousPodUID, runtime.Workloads[0].PodUID)
	require.Equal(t, []int{443, 8443, 9443}, []int{
		runtime.Services[0].Ports[0].Number,
		runtime.Services[0].Ports[1].Number,
		runtime.Services[0].Ports[2].Number,
	})
	require.NotEqual(t, runtime.Services[0].Ports[0].ResourceID, runtime.Services[0].Ports[1].ResourceID)
	require.NotEqual(t, runtime.Services[0].Ports[1].StableKey, runtime.Services[0].Ports[2].StableKey)
	require.Equal(t, "allocation-root-a", runtime.Allocations[1].RenewedFromID)
	require.Empty(t, runtime.Allocations[2].RenewedFromID)
	require.Equal(t, "allocation-root-a", runtime.ResourceSources[0].EntitlementLineageID)
	require.Equal(t, "allocation-renewal-a", runtime.ResourceSources[0].AllocationID)
	require.Equal(t, runtime.TargetRevisions[0].WorkloadStableKey, runtime.TargetRevisions[1].WorkloadStableKey)
	require.NotEqual(t, runtime.TargetRevisions[0].PodUID, runtime.TargetRevisions[1].PodUID)
	for _, session := range runtime.Sessions {
		require.False(t, session.ExpectedPermitted)
		require.NotEmpty(t, session.BlockReason)
		require.Equal(t, runtime.ResourceSources[0].ID, session.ResourceSourceID)
		require.Equal(t, runtime.TargetRevisions[1].ID, session.TargetRevisionID)
		require.Equal(t, runtime.Grants[0].ID, session.GrantID)
	}
}

func TestLoadReturnsIndependentScenarioCopies(t *testing.T) {
	first := MustLoad(ScenarioRuntimeEvolution)
	second := MustLoad(ScenarioRuntimeEvolution)
	first.Services[0].Ports[0].Number = 1
	first.SupplyInventories[0].NamespaceUIDs[0] = "changed"
	first.Issues[0] = "changed"
	require.Equal(t, 443, second.Services[0].Ports[0].Number)
	require.Equal(t, "namespace-uid-a", second.SupplyInventories[0].NamespaceUIDs[0])
	require.NotEqual(t, first.Issues[0], second.Issues[0])
}

func TestCreateCompatibilityDatabaseUsesExplicitNewPath(t *testing.T) {
	for _, name := range Names() {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "fixture.db")
			require.NoError(t, CreateCompatibilityDatabase(path, MustLoad(name)))
			info, err := os.Stat(path)
			require.NoError(t, err)
			require.Greater(t, info.Size(), int64(0))

			database, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro")
			require.NoError(t, err)
			var tableCount int
			require.NoError(t, database.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table'`).Scan(&tableCount))
			require.Equal(t, len(compatibilitySchema), tableCount)
			require.NoError(t, database.Close())

			require.Error(t, CreateCompatibilityDatabase(path, MustLoad(name)))
		})
	}
}
