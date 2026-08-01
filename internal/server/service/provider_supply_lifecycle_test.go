package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func createLifecycleSupplyResource(t *testing.T, fixture providerSupplyFixture) *AcceptSupplyCandidateResult {
	t.Helper()
	fixture.createBoundAgent(t, "agent-lifecycle", 1001)
	candidate := reportSupplyCandidate(t, fixture, TechnicalResourceCredential{
		SourceType: model.TechnicalResourceBindingLegacyNode, SourceID: "1001", CredentialRevision: 1,
	}, "epoch-a", 1, "snapshot-a", `{
		"cluster_uid":"cluster-lifecycle",
		"display_name":"Lifecycle Cluster",
		"namespaces":[
			{"uid":"namespace-lifecycle","name":"workloads","labels":{"environment":"test"},"status":"Active"},
			{"uid":"namespace-draft","name":"draft-only","labels":{},"status":"Active"}
		]
	}`)
	accepted, err := fixture.service.AcceptSupplyCandidate(context.Background(), fixture.authorization, AcceptSupplyCandidateInput{
		CandidateID: candidate.ID, ExpectedRowVersion: candidate.RowVersion, Reason: "lifecycle fixture acceptance",
	})
	require.NoError(t, err)
	return accepted
}

func activateLifecycleResourceAndNamespace(t *testing.T, fixture providerSupplyFixture, accepted *AcceptSupplyCandidateResult) (*model.PlatformResource, *model.ResourceScope, *model.ResourceScope) {
	t.Helper()
	resource, err := fixture.service.SetPlatformResourceLifecycle(context.Background(), fixture.authorization, SetPlatformResourceLifecycleInput{
		ResourceID: accepted.Resource.ID, TargetState: model.PlatformResourceActive,
		ExpectedRowVersion: accepted.Resource.RowVersion, Reason: "activate managed resource",
	})
	require.NoError(t, err)
	clusterScope, err := fixture.service.SetResourceScopeLifecycle(context.Background(), fixture.authorization, SetResourceScopeLifecycleInput{
		ScopeID: accepted.ClusterScope.ID, TargetState: model.ResourceScopeActive,
		ExpectedRowVersion: accepted.ClusterScope.RowVersion, Reason: "activate cluster scope",
	})
	require.NoError(t, err)
	namespaceScope, err := fixture.service.SetResourceScopeLifecycle(context.Background(), fixture.authorization, SetResourceScopeLifecycleInput{
		ScopeID: accepted.NamespaceScopes[0].ID, TargetState: model.ResourceScopeActive,
		ExpectedRowVersion: accepted.NamespaceScopes[0].RowVersion, Reason: "activate namespace scope",
	})
	require.NoError(t, err)
	return resource, clusterScope, namespaceScope
}

func TestProviderSupplyResourceAndScopeLifecycleFailClosed(t *testing.T) {
	fixture := newProviderSupplyFixture(t)
	accepted := createLifecycleSupplyResource(t, fixture)

	_, err := fixture.service.SetResourceScopeLifecycle(context.Background(), fixture.authorization, SetResourceScopeLifecycleInput{
		ScopeID: accepted.NamespaceScopes[0].ID, TargetState: model.ResourceScopeActive,
		ExpectedRowVersion: accepted.NamespaceScopes[0].RowVersion, Reason: "parent resource is still draft",
	})
	require.ErrorIs(t, err, ErrResourceScopeStateTransition)

	resource, clusterScope, namespaceScope := activateLifecycleResourceAndNamespace(t, fixture, accepted)
	marked, err := fixture.service.MarkResourceScopeAllocatable(context.Background(), fixture.authorization, MarkResourceScopeAllocatableInput{
		ScopeID: namespaceScope.ID, ExpectedRowVersion: namespaceScope.RowVersion, Reason: "publish namespace capacity",
	})
	require.NoError(t, err)
	require.Equal(t, model.ResourceScopeAllocatable, marked.Scope.LifecycleState)
	require.Equal(t, 1, marked.Resource.AllocatableScopeCount)
	require.Greater(t, marked.Resource.CapabilityRevision, resource.CapabilityRevision)

	suspendedScope, err := fixture.service.SetResourceScopeLifecycle(context.Background(), fixture.authorization, SetResourceScopeLifecycleInput{
		ScopeID: marked.Scope.ID, TargetState: model.ResourceScopeSuspended,
		ExpectedRowVersion: marked.Scope.RowVersion, Reason: "temporarily withdraw namespace",
	})
	require.NoError(t, err)
	var persistedResource model.PlatformResource
	require.NoError(t, fixture.database.First(&persistedResource, "id = ?", resource.ID).Error)
	require.Zero(t, persistedResource.AllocatableScopeCount)

	resumedScope, err := fixture.service.SetResourceScopeLifecycle(context.Background(), fixture.authorization, SetResourceScopeLifecycleInput{
		ScopeID: suspendedScope.ID, TargetState: model.ResourceScopeActive,
		ExpectedRowVersion: suspendedScope.RowVersion, Reason: "resume namespace review",
	})
	require.NoError(t, err)
	require.Equal(t, model.ResourceScopeActive, resumedScope.LifecycleState)
	require.NoError(t, fixture.database.First(&persistedResource, "id = ?", resource.ID).Error)
	require.Zero(t, persistedResource.AllocatableScopeCount)

	markedAgain, err := fixture.service.MarkResourceScopeAllocatable(context.Background(), fixture.authorization, MarkResourceScopeAllocatableInput{
		ScopeID: resumedScope.ID, ExpectedRowVersion: resumedScope.RowVersion, Reason: "publish namespace after review",
	})
	require.NoError(t, err)

	suspendedResource, err := fixture.service.SetPlatformResourceLifecycle(context.Background(), fixture.authorization, SetPlatformResourceLifecycleInput{
		ResourceID: resource.ID, TargetState: model.PlatformResourceSuspended,
		ExpectedRowVersion: markedAgain.Resource.RowVersion, Reason: "resource maintenance",
	})
	require.NoError(t, err)
	require.Zero(t, suspendedResource.AllocatableScopeCount)
	for _, scopeID := range []string{clusterScope.ID, namespaceScope.ID} {
		var scope model.ResourceScope
		require.NoError(t, fixture.database.First(&scope, "id = ?", scopeID).Error)
		require.Equal(t, model.ResourceScopeSuspended, scope.LifecycleState)
	}
	var draftScope model.ResourceScope
	require.NoError(t, fixture.database.First(&draftScope, "id = ?", accepted.NamespaceScopes[1].ID).Error)
	require.Equal(t, model.ResourceScopeDraft, draftScope.LifecycleState)

	resumedResource, err := fixture.service.SetPlatformResourceLifecycle(context.Background(), fixture.authorization, SetPlatformResourceLifecycleInput{
		ResourceID: resource.ID, TargetState: model.PlatformResourceActive,
		ExpectedRowVersion: suspendedResource.RowVersion, Reason: "resource maintenance complete",
	})
	require.NoError(t, err)
	require.Zero(t, resumedResource.AllocatableScopeCount)
	require.NoError(t, fixture.database.First(&namespaceScope, "id = ?", namespaceScope.ID).Error)
	require.Equal(t, model.ResourceScopeSuspended, namespaceScope.LifecycleState)

	clusterScope, err = fixture.service.SetResourceScopeLifecycle(context.Background(), fixture.authorization, SetResourceScopeLifecycleInput{
		ScopeID: clusterScope.ID, TargetState: model.ResourceScopeActive,
		ExpectedRowVersion: loadScopeRowVersion(t, fixture, clusterScope.ID), Reason: "resume cluster review",
	})
	require.NoError(t, err)
	namespaceScope, err = fixture.service.SetResourceScopeLifecycle(context.Background(), fixture.authorization, SetResourceScopeLifecycleInput{
		ScopeID: namespaceScope.ID, TargetState: model.ResourceScopeActive,
		ExpectedRowVersion: namespaceScope.RowVersion, Reason: "resume namespace review",
	})
	require.NoError(t, err)
	markedAgain, err = fixture.service.MarkResourceScopeAllocatable(context.Background(), fixture.authorization, MarkResourceScopeAllocatableInput{
		ScopeID: namespaceScope.ID, ExpectedRowVersion: namespaceScope.RowVersion, Reason: "republish namespace capacity",
	})
	require.NoError(t, err)

	retiredCluster, err := fixture.service.SetResourceScopeLifecycle(context.Background(), fixture.authorization, SetResourceScopeLifecycleInput{
		ScopeID: clusterScope.ID, TargetState: model.ResourceScopeRetired,
		ExpectedRowVersion: clusterScope.RowVersion, Reason: "retire cluster boundary",
	})
	require.NoError(t, err)
	require.Equal(t, model.ResourceScopeRetired, retiredCluster.LifecycleState)
	for _, scopeID := range []string{namespaceScope.ID, draftScope.ID} {
		var scope model.ResourceScope
		require.NoError(t, fixture.database.First(&scope, "id = ?", scopeID).Error)
		require.Equal(t, model.ResourceScopeRetired, scope.LifecycleState)
	}
	require.NoError(t, fixture.database.First(&persistedResource, "id = ?", resource.ID).Error)
	require.Zero(t, persistedResource.AllocatableScopeCount)
	require.Greater(t, persistedResource.RowVersion, markedAgain.Resource.RowVersion)

	retiredResource, err := fixture.service.SetPlatformResourceLifecycle(context.Background(), fixture.authorization, SetPlatformResourceLifecycleInput{
		ResourceID: resource.ID, TargetState: model.PlatformResourceRetired,
		ExpectedRowVersion: persistedResource.RowVersion, Reason: "retire managed cluster",
	})
	require.NoError(t, err)
	require.Equal(t, model.PlatformResourceRetired, retiredResource.LifecycleState)
	_, err = fixture.service.SetPlatformResourceLifecycle(context.Background(), fixture.authorization, SetPlatformResourceLifecycleInput{
		ResourceID: resource.ID, TargetState: model.PlatformResourceActive,
		ExpectedRowVersion: retiredResource.RowVersion, Reason: "terminal state must not resume",
	})
	require.ErrorIs(t, err, ErrPlatformResourceStateTransition)
}

func TestMarkResourceScopeAllocatablePrerequisites(t *testing.T) {
	t.Run("cluster scope", func(t *testing.T) {
		fixture := newProviderSupplyFixture(t)
		accepted := createLifecycleSupplyResource(t, fixture)
		_, clusterScope, _ := activateLifecycleResourceAndNamespace(t, fixture, accepted)
		marked, err := fixture.service.MarkResourceScopeAllocatable(context.Background(), fixture.authorization, MarkResourceScopeAllocatableInput{
			ScopeID: clusterScope.ID, ExpectedRowVersion: clusterScope.RowVersion, Reason: "publish whole cluster capacity",
		})
		require.NoError(t, err)
		require.Equal(t, model.ResourceScopeAllocatable, marked.Scope.LifecycleState)
		require.Equal(t, 1, marked.Resource.AllocatableScopeCount)
	})

	t.Run("expired source lease", func(t *testing.T) {
		fixture := newProviderSupplyFixture(t)
		accepted := createLifecycleSupplyResource(t, fixture)
		_, _, namespaceScope := activateLifecycleResourceAndNamespace(t, fixture, accepted)
		fixture.service.now = func() time.Time { return fixture.now.Add(11 * time.Minute) }
		_, err := fixture.service.MarkResourceScopeAllocatable(context.Background(), fixture.authorization, MarkResourceScopeAllocatableInput{
			ScopeID: namespaceScope.ID, ExpectedRowVersion: namespaceScope.RowVersion, Reason: "expired source must fail closed",
		})
		require.ErrorIs(t, err, ErrResourceScopeNotAllocatable)
	})

	t.Run("identity conflict", func(t *testing.T) {
		fixture := newProviderSupplyFixture(t)
		accepted := createLifecycleSupplyResource(t, fixture)
		_, _, namespaceScope := activateLifecycleResourceAndNamespace(t, fixture, accepted)
		require.NoError(t, fixture.database.Model(&model.SupplyCandidate{}).Where("id = ?", accepted.Candidate.ID).Updates(map[string]any{
			"identity_quality": model.SupplyIdentityCollision,
			"conflict_code":    supplyConflictCrossProvider,
			"review_state":     model.SupplyCandidateConflict,
		}).Error)
		_, err := fixture.service.MarkResourceScopeAllocatable(context.Background(), fixture.authorization, MarkResourceScopeAllocatableInput{
			ScopeID: namespaceScope.ID, ExpectedRowVersion: namespaceScope.RowVersion, Reason: "conflicted source must fail closed",
		})
		require.ErrorIs(t, err, ErrResourceScopeNotAllocatable)
	})

	t.Run("disabled source binding", func(t *testing.T) {
		fixture := newProviderSupplyFixture(t)
		accepted := createLifecycleSupplyResource(t, fixture)
		_, _, namespaceScope := activateLifecycleResourceAndNamespace(t, fixture, accepted)
		require.NoError(t, fixture.database.Model(&model.TechnicalResourceBinding{}).
			Where("technical_resource_id = ?", accepted.Candidate.TechnicalResourceID).Update("enabled", false).Error)
		_, err := fixture.service.MarkResourceScopeAllocatable(context.Background(), fixture.authorization, MarkResourceScopeAllocatableInput{
			ScopeID: namespaceScope.ID, ExpectedRowVersion: namespaceScope.RowVersion, Reason: "disabled binding must fail closed",
		})
		require.ErrorIs(t, err, ErrResourceScopeNotAllocatable)
	})

	t.Run("observation drift and isolation mode", func(t *testing.T) {
		fixture := newProviderSupplyFixture(t)
		accepted := createLifecycleSupplyResource(t, fixture)
		_, _, namespaceScope := activateLifecycleResourceAndNamespace(t, fixture, accepted)
		require.NoError(t, fixture.database.Model(&model.NamespaceObservation{}).
			Where("id = ?", *namespaceScope.NamespaceObservationID).Update("revision", gorm.Expr("revision + 1")).Error)
		_, err := fixture.service.MarkResourceScopeAllocatable(context.Background(), fixture.authorization, MarkResourceScopeAllocatableInput{
			ScopeID: namespaceScope.ID, ExpectedRowVersion: namespaceScope.RowVersion, Reason: "stale evidence revision",
		})
		require.ErrorIs(t, err, ErrResourceScopeNotAllocatable)

		var observation model.NamespaceObservation
		require.NoError(t, fixture.database.First(&observation, "id = ?", *namespaceScope.NamespaceObservationID).Error)
		require.NoError(t, fixture.database.Model(&model.ResourceScope{}).Where("id = ?", namespaceScope.ID).Updates(map[string]any{
			"evidence_revision": observation.Revision,
			"isolation_mode":    model.ResourceScopeIsolationReviewedShared,
		}).Error)
		_, err = fixture.service.MarkResourceScopeAllocatable(context.Background(), fixture.authorization, MarkResourceScopeAllocatableInput{
			ScopeID: namespaceScope.ID, ExpectedRowVersion: namespaceScope.RowVersion, Reason: "reviewed shared is reserved",
		})
		require.ErrorIs(t, err, ErrResourceScopeNotAllocatable)
	})

	t.Run("provider isolation and stale version", func(t *testing.T) {
		fixture := newProviderSupplyFixture(t)
		accepted := createLifecycleSupplyResource(t, fixture)
		_, _, namespaceScope := activateLifecycleResourceAndNamespace(t, fixture, accepted)
		otherAuthorization := createOtherProviderAuthorization(t, fixture)
		_, err := fixture.service.MarkResourceScopeAllocatable(context.Background(), otherAuthorization, MarkResourceScopeAllocatableInput{
			ScopeID: namespaceScope.ID, ExpectedRowVersion: namespaceScope.RowVersion, Reason: "foreign Provider object ID",
		})
		require.ErrorIs(t, err, ErrProviderSupplyObjectNotFound)
		_, err = fixture.service.MarkResourceScopeAllocatable(context.Background(), fixture.authorization, MarkResourceScopeAllocatableInput{
			ScopeID: namespaceScope.ID, ExpectedRowVersion: namespaceScope.RowVersion + 1, Reason: "stale If-Match",
		})
		require.ErrorIs(t, err, ErrProviderSupplyVersionConflict)
	})
}

func loadScopeRowVersion(t *testing.T, fixture providerSupplyFixture, scopeID string) int64 {
	t.Helper()
	var scope model.ResourceScope
	require.NoError(t, fixture.database.First(&scope, "id = ?", scopeID).Error)
	return scope.RowVersion
}
