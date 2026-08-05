package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func TestProviderSupplyReconciliationExpiresTechnicalResourceLease(t *testing.T) {
	fixture := newProviderSupplyFixture(t)
	agent := fixture.createBoundAgent(t, "expired-agent", 1001)
	expiresAt := fixture.now.Add(-time.Minute)
	require.NoError(t, fixture.database.Model(&model.TechnicalResource{}).Where("id = ?", agent.ID).Updates(map[string]any{
		"health_state": model.ResourceHealthOnline, "lease_expires_at": expiresAt,
	}).Error)

	result, err := NewProviderSupplyReconciliationService(fixture.database).ReconcileExpiredEvidence(context.Background(), fixture.now)
	require.NoError(t, err)
	require.Equal(t, int64(1), result.ExpiredTechnicalResources)
	require.NoError(t, fixture.database.First(agent, "id = ?", agent.ID).Error)
	require.Equal(t, model.ResourceHealthOffline, agent.HealthState)
}

func TestProviderSupplyCompleteSnapshotReconcilesNamespaceLeaseAndScope(t *testing.T) {
	fixture := newProviderSupplyFixture(t)
	fixture.createBoundAgent(t, "agent-reconciliation", 1001)
	credential := TechnicalResourceCredential{
		SourceType: model.TechnicalResourceBindingLegacyNode, SourceID: "1001", CredentialRevision: 1,
	}
	candidate := reportSupplyCandidate(t, fixture, credential, "epoch-reconciliation", 1, "snapshot-initial", `{
		"cluster_uid":"cluster-reconciliation",
		"display_name":"Reconciliation Cluster",
		"namespaces":[
			{"uid":"namespace-retained","name":"retained-before","labels":{"team":"platform"},"status":"Active"},
			{"uid":"namespace-missing","name":"missing","labels":{"team":"legacy"},"status":"Active"}
		]
	}`)
	accepted, err := fixture.service.AcceptSupplyCandidate(context.Background(), fixture.authorization, AcceptSupplyCandidateInput{
		CandidateID: candidate.ID, ExpectedRowVersion: candidate.RowVersion, Reason: "accept reconciliation fixture",
	})
	require.NoError(t, err)
	resource, err := fixture.service.SetPlatformResourceLifecycle(context.Background(), fixture.authorization, SetPlatformResourceLifecycleInput{
		ResourceID: accepted.Resource.ID, TargetState: model.PlatformResourceActive,
		ExpectedRowVersion: accepted.Resource.RowVersion, Reason: "activate reconciliation resource",
	})
	require.NoError(t, err)
	_, err = fixture.service.SetResourceScopeLifecycle(context.Background(), fixture.authorization, SetResourceScopeLifecycleInput{
		ScopeID: accepted.ClusterScope.ID, TargetState: model.ResourceScopeActive,
		ExpectedRowVersion: accepted.ClusterScope.RowVersion, Reason: "activate reconciliation cluster",
	})
	require.NoError(t, err)

	var missingObservation, retainedObservation model.NamespaceObservation
	require.NoError(t, fixture.database.Where("cluster_resource_id = ? AND namespace_uid = ?", resource.ID, "namespace-missing").First(&missingObservation).Error)
	require.NoError(t, fixture.database.Where("cluster_resource_id = ? AND namespace_uid = ?", resource.ID, "namespace-retained").First(&retainedObservation).Error)
	var missingScope, retainedScope model.ResourceScope
	require.NoError(t, fixture.database.First(&missingScope, "namespace_observation_id = ?", missingObservation.ID).Error)
	require.NoError(t, fixture.database.First(&retainedScope, "namespace_observation_id = ?", retainedObservation.ID).Error)
	for _, scope := range []*model.ResourceScope{&missingScope, &retainedScope} {
		updated, updateErr := fixture.service.SetResourceScopeLifecycle(context.Background(), fixture.authorization, SetResourceScopeLifecycleInput{
			ScopeID: scope.ID, TargetState: model.ResourceScopeActive, ExpectedRowVersion: scope.RowVersion, Reason: "activate namespace evidence",
		})
		require.NoError(t, updateErr)
		*scope = *updated
	}
	marked, err := fixture.service.MarkResourceScopeAllocatable(context.Background(), fixture.authorization, MarkResourceScopeAllocatableInput{
		ScopeID: missingScope.ID, ExpectedRowVersion: missingScope.RowVersion, Reason: "mark missing fixture allocatable",
	})
	require.NoError(t, err)
	missingScope = *marked.Scope
	require.Equal(t, 1, marked.Resource.AllocatableScopeCount)

	secondObservedAt := fixture.now.Add(time.Minute)
	fixture.service.now = func() time.Time { return secondObservedAt }
	updatedCandidate := reportSupplyCandidate(t, fixture, credential, "epoch-reconciliation", 2, "snapshot-updated", `{
		"cluster_uid":"cluster-reconciliation",
		"display_name":"Reconciliation Cluster",
		"namespaces":[
			{"uid":"namespace-retained","name":"retained-after","labels":{"team":"platform"},"status":"Active"},
			{"uid":"namespace-new","name":"new","labels":{"environment":"test"},"status":"Active"}
		]
	}`)
	require.Equal(t, model.SupplyCandidateLinked, updatedCandidate.ReviewState)
	require.NoError(t, fixture.database.First(&retainedObservation, "id = ?", retainedObservation.ID).Error)
	require.Equal(t, "retained-after", retainedObservation.Name)
	require.Equal(t, int64(2), retainedObservation.Revision)
	require.Equal(t, model.NamespaceObservationObserved, retainedObservation.State)
	require.NoError(t, fixture.database.First(&retainedScope, "id = ?", retainedScope.ID).Error)
	require.Equal(t, retainedObservation.Revision, retainedScope.EvidenceRevision)
	require.Equal(t, model.ResourceScopeActive, retainedScope.LifecycleState)

	var newObservation model.NamespaceObservation
	require.NoError(t, fixture.database.Where("cluster_resource_id = ? AND namespace_uid = ?", resource.ID, "namespace-new").First(&newObservation).Error)
	var newScope model.ResourceScope
	require.NoError(t, fixture.database.First(&newScope, "namespace_observation_id = ?", newObservation.ID).Error)
	require.Equal(t, model.ResourceScopeDraft, newScope.LifecycleState)
	require.NoError(t, fixture.database.First(&missingObservation, "id = ?", missingObservation.ID).Error)
	require.Equal(t, model.NamespaceObservationObserved, missingObservation.State)

	reconciler := NewProviderSupplyReconciliationService(fixture.database)
	result, err := reconciler.ReconcileExpiredEvidence(context.Background(), fixture.now.Add(10*time.Minute+30*time.Second))
	require.NoError(t, err)
	require.Equal(t, int64(1), result.StaleObservations)
	require.Equal(t, int64(1), result.SuspendedScopes)
	require.Equal(t, int64(1), result.UpdatedResources)
	require.NoError(t, fixture.database.First(&missingObservation, "id = ?", missingObservation.ID).Error)
	require.Equal(t, model.NamespaceObservationStale, missingObservation.State)
	require.NoError(t, fixture.database.First(&missingScope, "id = ?", missingScope.ID).Error)
	require.Equal(t, model.ResourceScopeSuspended, missingScope.LifecycleState)
	require.Equal(t, missingObservation.Revision, missingScope.EvidenceRevision)
	require.NoError(t, fixture.database.First(&retainedObservation, "id = ?", retainedObservation.ID).Error)
	require.Equal(t, model.NamespaceObservationObserved, retainedObservation.State)
	require.NoError(t, fixture.database.First(&resource, "id = ?", resource.ID).Error)
	require.Zero(t, resource.AllocatableScopeCount)
	require.Equal(t, model.ResourceHealthOnline, resource.HealthState)

	var outboxCount, auditCount int64
	require.NoError(t, fixture.database.Model(&model.OutboxEvent{}).Where("consumer = ?", "provider_supply_projection").Count(&outboxCount).Error)
	require.GreaterOrEqual(t, outboxCount, int64(3))
	require.NoError(t, fixture.database.Model(&model.AuditLog{}).Where("action_type = ?", providerSupplyReconciliationAction).Count(&auditCount).Error)
	require.Equal(t, int64(1), auditCount)

	secondResult, err := reconciler.ReconcileExpiredEvidence(context.Background(), fixture.now.Add(10*time.Minute+30*time.Second))
	require.NoError(t, err)
	require.Zero(t, secondResult.StaleObservations)
	require.Zero(t, secondResult.SuspendedScopes)
	var secondOutboxCount int64
	require.NoError(t, fixture.database.Model(&model.OutboxEvent{}).Count(&secondOutboxCount).Error)
	require.Equal(t, outboxCount, secondOutboxCount)
}

func TestProviderSupplyCrossProviderConflictExpiryPreservesLinkedSource(t *testing.T) {
	fixture := newProviderSupplyFixture(t)
	agentA := fixture.createBoundAgent(t, "agent-conflict-a", 1001)
	candidateA := reportSupplyCandidate(t, fixture, TechnicalResourceCredential{
		SourceType: model.TechnicalResourceBindingLegacyNode, SourceID: "1001", CredentialRevision: 1,
	}, "epoch-conflict-a", 1, "snapshot-conflict-a", `{
		"cluster_uid":"cluster-cross-provider-expiry",
		"display_name":"Conflict Cluster A",
		"namespaces":[{"uid":"namespace-conflict","name":"workloads","labels":{},"status":"Active"}]
	}`)
	accepted, err := fixture.service.AcceptSupplyCandidate(context.Background(), fixture.authorization, AcceptSupplyCandidateInput{
		CandidateID: candidateA.ID, ExpectedRowVersion: candidateA.RowVersion, Reason: "accept before conflict",
	})
	require.NoError(t, err)
	require.Equal(t, agentA.ID, accepted.Candidate.TechnicalResourceID)

	otherAuthorization := createOtherProviderAuthorization(t, fixture)
	createBoundAgentForProvider(t, fixture, otherAuthorization, "agent-conflict-b", 1002)
	candidateB := reportSupplyCandidate(t, fixture, TechnicalResourceCredential{
		SourceType: model.TechnicalResourceBindingLegacyNode, SourceID: "1002", CredentialRevision: 1,
	}, "epoch-conflict-b", 1, "snapshot-conflict-b", `{
		"cluster_uid":"cluster-cross-provider-expiry",
		"display_name":"Conflict Cluster B",
		"namespaces":[{"uid":"namespace-conflict","name":"workloads","labels":{},"status":"Active"}]
	}`)
	require.NoError(t, fixture.database.First(&candidateA, "id = ?", candidateA.ID).Error)
	require.Equal(t, model.SupplyCandidateLinked, candidateA.ReviewState)
	require.Equal(t, supplyConflictCrossProvider, candidateA.ConflictCode)
	var resource model.PlatformResource
	require.NoError(t, fixture.database.First(&resource, "id = ?", accepted.Resource.ID).Error)
	require.Equal(t, model.ResourceHealthDegraded, resource.HealthState)

	bExpired := fixture.now.Add(time.Second)
	require.NoError(t, fixture.database.Model(&model.SupplyCandidate{}).Where("id = ?", candidateB.ID).Updates(map[string]any{
		"lease_expires_at": bExpired,
	}).Error)
	reconciler := NewProviderSupplyReconciliationService(fixture.database)
	_, err = reconciler.ReconcileExpiredEvidence(context.Background(), fixture.now.Add(2*time.Second))
	require.NoError(t, err)
	require.NoError(t, fixture.database.First(&candidateA, "id = ?", candidateA.ID).Error)
	require.Equal(t, model.SupplyCandidateLinked, candidateA.ReviewState)
	require.Equal(t, model.SupplyIdentityStrong, candidateA.IdentityQuality)
	require.Empty(t, candidateA.ConflictCode)
	require.Empty(t, candidateA.OpaqueConflictID)
	require.NoError(t, fixture.database.First(&resource, "id = ?", accepted.Resource.ID).Error)
	require.Equal(t, model.ResourceHealthOnline, resource.HealthState)
}

func TestProviderSupplyStaleEvidenceCannotActivate(t *testing.T) {
	fixture := newProviderSupplyFixture(t)
	accepted := createLifecycleSupplyResource(t, fixture)
	resource, clusterScope, namespaceScope := activateLifecycleResourceAndNamespace(t, fixture, accepted)
	require.NotNil(t, namespaceScope.NamespaceObservationID)
	require.NoError(t, fixture.database.Model(&model.ResourceScope{}).Where("id = ?", namespaceScope.ID).Updates(map[string]any{
		"lifecycle_state": model.ResourceScopeSuspended,
	}).Error)
	require.NoError(t, fixture.database.Model(&model.NamespaceObservation{}).Where("id = ?", *namespaceScope.NamespaceObservationID).Updates(map[string]any{
		"state": model.NamespaceObservationStale, "revision": gorm.Expr("revision + 1"),
	}).Error)
	var observation model.NamespaceObservation
	require.NoError(t, fixture.database.First(&observation, "id = ?", *namespaceScope.NamespaceObservationID).Error)
	require.NoError(t, fixture.database.Model(&model.ResourceScope{}).Where("id = ?", namespaceScope.ID).Updates(map[string]any{
		"evidence_revision": observation.Revision,
	}).Error)
	_, err := fixture.service.SetResourceScopeLifecycle(context.Background(), fixture.authorization, SetResourceScopeLifecycleInput{
		ScopeID: namespaceScope.ID, TargetState: model.ResourceScopeActive,
		ExpectedRowVersion: namespaceScope.RowVersion, Reason: "stale evidence must not activate",
	})
	require.ErrorIs(t, err, ErrResourceScopeStateTransition)
	require.Equal(t, model.PlatformResourceActive, resource.LifecycleState)
	require.Equal(t, model.ResourceScopeActive, clusterScope.LifecycleState)
}

func TestProviderSupplyReconciliationRollsBackWithoutAuditPersistence(t *testing.T) {
	fixture := newProviderSupplyFixture(t)
	accepted := createLifecycleSupplyResource(t, fixture)
	_, _, namespaceScope := activateLifecycleResourceAndNamespace(t, fixture, accepted)
	require.NotNil(t, namespaceScope.NamespaceObservationID)

	leaseExpiresAt := fixture.now.Add(time.Second)
	require.NoError(t, fixture.database.Model(&model.NamespaceObservation{}).
		Where("id = ?", *namespaceScope.NamespaceObservationID).
		Update("lease_expires_at", leaseExpiresAt).Error)
	var outboxBefore int64
	require.NoError(t, fixture.database.Model(&model.OutboxEvent{}).Count(&outboxBefore).Error)
	require.NoError(t, fixture.database.Migrator().DropTable(&model.AuditLog{}))

	_, err := NewProviderSupplyReconciliationService(fixture.database).
		ReconcileExpiredEvidence(context.Background(), fixture.now.Add(2*time.Second))
	require.Error(t, err)

	var observation model.NamespaceObservation
	require.NoError(t, fixture.database.First(&observation, "id = ?", *namespaceScope.NamespaceObservationID).Error)
	require.Equal(t, model.NamespaceObservationObserved, observation.State)
	var persistedScope model.ResourceScope
	require.NoError(t, fixture.database.First(&persistedScope, "id = ?", namespaceScope.ID).Error)
	require.Equal(t, model.ResourceScopeActive, persistedScope.LifecycleState)
	var outboxAfter int64
	require.NoError(t, fixture.database.Model(&model.OutboxEvent{}).Count(&outboxAfter).Error)
	require.Equal(t, outboxBefore, outboxAfter)
}
