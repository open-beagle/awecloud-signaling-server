package service

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func reportSupplyCandidate(t *testing.T, fixture providerSupplyFixture, credential TechnicalResourceCredential, epoch string, sequence int64, snapshotID, clusterJSON string) model.SupplyCandidate {
	t.Helper()
	payload, hash := inventoryPayload(t, fmt.Sprintf(`{"kubernetes_clusters":[%s]}`, clusterJSON))
	ack, err := fixture.service.ReceiveSupplyInventoryBatch(context.Background(), inventoryInput(
		credential, payload, hash, epoch, sequence, snapshotID, 0, 1,
	))
	require.NoError(t, err)
	require.True(t, ack.SnapshotCommitted)

	var candidate model.SupplyCandidate
	require.NoError(t, fixture.database.Where("technical_resource_id = ?", ack.TechnicalResourceID).First(&candidate).Error)
	return candidate
}

func createOtherProviderAuthorization(t *testing.T, fixture providerSupplyFixture) *ManagementAuthorizationContext {
	t.Helper()
	require.NoError(t, fixture.database.Create(&model.AdminProviderMembership{
		ID: uuid.NewString(), UserID: fixture.actor.ID, ProviderID: fixture.otherProvider.ID,
		Role: model.ProviderManagementRoleOperator, Enabled: true, ValidFrom: fixture.now.Add(-time.Minute),
		PermissionRevision: 1, CreatedByUserID: fixture.actor.ID, Reason: "other Provider fixture", RowVersion: 1,
	}).Error)
	authorization, err := ResolveManagementContext(
		fixture.database, fixture.actor.ID, model.ManagementScopeProvider, fixture.otherProvider.ID, fixture.now, false,
	)
	require.NoError(t, err)
	return authorization
}

func createBoundAgentForProvider(t *testing.T, fixture providerSupplyFixture, authorization *ManagementAuthorizationContext, stableKey string, nodeID uint64) *model.TechnicalResource {
	t.Helper()
	resource, err := fixture.service.CreateTechnicalResource(context.Background(), authorization, CreateTechnicalResourceInput{
		Type: model.TechnicalResourceAgent, StableKey: stableKey, CredentialRevision: 1, RuntimeName: stableKey, DomainLabel: stableKey,
	})
	require.NoError(t, err)
	require.NoError(t, fixture.database.Model(&model.Node{}).Where("id = ?", nodeID).Update("user_id", resource.RuntimeUserID).Error)
	if nodeID == 1001 {
		require.NoError(t, fixture.database.Model(&model.Endpoint{}).Where("id = ?", "legacy-endpoint-a").Update("user_id", resource.RuntimeUserID).Error)
	}
	bound, err := fixture.service.BindTechnicalResource(context.Background(), authorization, BindTechnicalResourceInput{
		TechnicalResourceID: resource.ID, SourceType: model.TechnicalResourceBindingLegacyNode,
		SourceID: strconv.FormatUint(nodeID, 10), ExpectedResourceVersion: resource.RowVersion, Reason: "Provider fixture binding",
	})
	require.NoError(t, err)
	return bound.TechnicalResource
}

func TestEnsureLegacyHostPlatformResourceRefreshesExistingLease(t *testing.T) {
	fixture := newProviderSupplyFixture(t)
	agent := fixture.createBoundAgent(t, "agent-host-refresh", 1001)
	var node model.Node
	require.NoError(t, fixture.database.First(&node, 1001).Error)

	firstAt := fixture.now
	require.NoError(t, EnsureLegacyHostPlatformResource(fixture.database, agent, &node, fixture.actor.ID, firstAt))
	stableKey := LegacyHostStableKey(model.TechnicalResourceBindingLegacyNode, fmt.Sprint(node.ID))
	var first model.SupplyCandidate
	require.NoError(t, fixture.database.First(&first, "technical_resource_id = ? AND resource_type = ? AND stable_key = ?", agent.ID, model.SupplyResourceHost, stableKey).Error)

	secondAt := firstAt.Add(5 * time.Minute)
	require.NoError(t, EnsureLegacyHostPlatformResource(fixture.database, agent, &node, fixture.actor.ID, secondAt))
	var second model.SupplyCandidate
	require.NoError(t, fixture.database.First(&second, "id = ?", first.ID).Error)
	require.True(t, second.LastObservedAt.After(first.LastObservedAt))
	require.True(t, second.LeaseExpiresAt.After(first.LeaseExpiresAt))
}

func TestSupplyInventoryReconcilesWithActiveLegacyHostResource(t *testing.T) {
	fixture := newProviderSupplyFixture(t)
	agent := fixture.createBoundAgent(t, "agent-host-and-kubernetes", 1001)
	var node model.Node
	require.NoError(t, fixture.database.First(&node, 1001).Error)
	require.NoError(t, EnsureLegacyHostPlatformResource(fixture.database, agent, &node, fixture.actor.ID, fixture.now))

	payload, hash := inventoryPayload(t, `{"kubernetes_clusters":[{
		"cluster_uid":"cluster-host-and-kubernetes",
		"display_name":"Host Kubernetes",
		"namespaces":[]
	}]}`)
	ack, err := fixture.service.ReceiveSupplyInventoryBatch(context.Background(), inventoryInput(
		TechnicalResourceCredential{SourceType: model.TechnicalResourceBindingLegacyNode, SourceID: "1001", CredentialRevision: 1},
		payload, hash, "epoch-host-and-kubernetes", 1, "snapshot-host-and-kubernetes", 0, 1,
	))
	require.NoError(t, err)
	require.True(t, ack.SnapshotCommitted)

	var host model.PlatformResource
	require.NoError(t, fixture.database.First(&host, "provider_id = ? AND type = ?", fixture.provider.ID, model.SupplyResourceHost).Error)
	require.Equal(t, model.ResourceHealthOnline, host.HealthState)
}

func TestSupplyCandidateProjectionUpdateAndRejectedDecision(t *testing.T) {
	fixture := newProviderSupplyFixture(t)
	agent := fixture.createBoundAgent(t, "agent-candidate-update", 1001)
	credential := TechnicalResourceCredential{
		SourceType: model.TechnicalResourceBindingLegacyNode, SourceID: "1001", CredentialRevision: 1,
	}

	first := reportSupplyCandidate(t, fixture, credential, "epoch-a", 1, "snapshot-a", `{
		"cluster_uid":"cluster-uid-a",
		"display_name":"Cluster A",
		"capabilities":["watch","list","watch"],
		"namespaces":[{"uid":"namespace-uid-a","name":"team-a","labels":{"environment":"prod","ignored":"drop"},"status":"Active"}]
	}`)
	require.Equal(t, fixture.provider.ID, first.ProviderID)
	require.Equal(t, agent.ID, first.TechnicalResourceID)
	require.Equal(t, model.SupplyIdentityStrong, first.IdentityQuality)
	require.Equal(t, model.SupplyCandidatePendingReview, first.ReviewState)
	require.Empty(t, first.ConflictCode)
	require.Len(t, first.StableKey, 64)
	require.NotEqual(t, "cluster-uid-a", first.StableKey)
	require.Contains(t, first.ObservationSnapshot, `"environment":"prod"`)
	require.NotContains(t, first.ObservationSnapshot, "ignored")
	require.Contains(t, first.ObservationSnapshot, `"capabilities":["list","watch"]`)

	second := reportSupplyCandidate(t, fixture, credential, "epoch-a", 2, "snapshot-b", `{
		"cluster_uid":"cluster-uid-a",
		"display_name":"Cluster A renamed",
		"namespaces":[{"uid":"namespace-uid-a","name":"team-a","labels":{"team":"platform"},"status":"Active"}]
	}`)
	require.Equal(t, first.ID, second.ID)
	require.Equal(t, first.StableKey, second.StableKey)
	require.Greater(t, second.RowVersion, first.RowVersion)
	require.Contains(t, second.ObservationSnapshot, "Cluster A renamed")

	rejected, err := fixture.service.RejectSupplyCandidate(context.Background(), fixture.authorization, RejectSupplyCandidateInput{
		CandidateID: second.ID, ExpectedRowVersion: second.RowVersion, Reason: "not managed by this Provider",
	})
	require.NoError(t, err)
	require.Equal(t, model.SupplyCandidateRejected, rejected.ReviewState)
	require.NotNil(t, rejected.ReviewedAt)
	require.NotNil(t, rejected.ReviewedByUserID)

	observedAgain := reportSupplyCandidate(t, fixture, credential, "epoch-a", 3, "snapshot-c", `{
		"cluster_uid":"cluster-uid-a",
		"display_name":"Cluster A observed again",
		"namespaces":[]
	}`)
	require.Equal(t, rejected.ID, observedAgain.ID)
	require.Equal(t, model.SupplyCandidateRejected, observedAgain.ReviewState)
	require.Equal(t, rejected.ReviewedAt, observedAgain.ReviewedAt)
	require.Equal(t, rejected.ReviewedByUserID, observedAgain.ReviewedByUserID)
}

func TestSupplyCandidateInsufficientIdentityCannotBeAccepted(t *testing.T) {
	fixture := newProviderSupplyFixture(t)
	fixture.createBoundAgent(t, "agent-candidate-insufficient", 1001)
	credential := TechnicalResourceCredential{
		SourceType: model.TechnicalResourceBindingLegacyNode, SourceID: "1001", CredentialRevision: 1,
	}
	candidate := reportSupplyCandidate(t, fixture, credential, "epoch-a", 1, "snapshot-a", `{
		"display_name":"Name is not identity",
		"ca_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"namespaces":[]
	}`)
	require.Equal(t, model.SupplyIdentityInsufficient, candidate.IdentityQuality)
	require.Equal(t, supplyConflictInsufficientClusterIdentity, candidate.ConflictCode)
	require.Equal(t, model.SupplyCandidatePendingReview, candidate.ReviewState)

	_, err := fixture.service.AcceptSupplyCandidate(context.Background(), fixture.authorization, AcceptSupplyCandidateInput{
		CandidateID: candidate.ID, ExpectedRowVersion: candidate.RowVersion, Reason: "attempt weak identity acceptance",
	})
	require.ErrorIs(t, err, ErrProviderSupplyConflict)
	var resourceCount int64
	require.NoError(t, fixture.database.Model(&model.PlatformResource{}).Count(&resourceCount).Error)
	require.Zero(t, resourceCount)
}

func TestSupplyCandidateCrossProviderConflictIsOpaqueAndIsolated(t *testing.T) {
	fixture := newProviderSupplyFixture(t)
	fixture.createBoundAgent(t, "agent-provider-a", 1001)
	authorizationB := createOtherProviderAuthorization(t, fixture)
	createBoundAgentForProvider(t, fixture, authorizationB, "agent-provider-b", 1002)

	candidateA := reportSupplyCandidate(t, fixture, TechnicalResourceCredential{
		SourceType: model.TechnicalResourceBindingLegacyNode, SourceID: "1001", CredentialRevision: 1,
	}, "epoch-a", 1, "snapshot-a", `{"cluster_uid":"shared-cluster-uid","display_name":"Provider A name","namespaces":[]}`)
	candidateB := reportSupplyCandidate(t, fixture, TechnicalResourceCredential{
		SourceType: model.TechnicalResourceBindingLegacyNode, SourceID: "1002", CredentialRevision: 1,
	}, "epoch-b", 1, "snapshot-b", `{"cluster_uid":"shared-cluster-uid","display_name":"Provider B name","namespaces":[]}`)
	require.NoError(t, fixture.database.First(&candidateA, "id = ?", candidateA.ID).Error)
	require.NoError(t, fixture.database.First(&candidateB, "id = ?", candidateB.ID).Error)

	for _, candidate := range []model.SupplyCandidate{candidateA, candidateB} {
		require.Equal(t, model.SupplyIdentityCollision, candidate.IdentityQuality)
		require.Equal(t, model.SupplyCandidateConflict, candidate.ReviewState)
		require.Equal(t, supplyConflictCrossProvider, candidate.ConflictCode)
		require.Len(t, candidate.OpaqueConflictID, 64)
	}
	require.Equal(t, candidateA.OpaqueConflictID, candidateB.OpaqueConflictID)
	require.NotContains(t, candidateA.OpaqueConflictID, fixture.otherProvider.ID)

	_, err := fixture.service.AcceptSupplyCandidate(context.Background(), fixture.authorization, AcceptSupplyCandidateInput{
		CandidateID: candidateB.ID, ExpectedRowVersion: candidateB.RowVersion, Reason: "cross Provider object ID",
	})
	require.ErrorIs(t, err, ErrProviderSupplyObjectNotFound)
	_, err = fixture.service.AcceptSupplyCandidate(context.Background(), fixture.authorization, AcceptSupplyCandidateInput{
		CandidateID: candidateA.ID, ExpectedRowVersion: candidateA.RowVersion, Reason: "unresolved identity conflict",
	})
	require.ErrorIs(t, err, ErrProviderSupplyConflict)
}

func TestAcceptSupplyCandidatesCreatesDraftResourceScopesAndMultipleSources(t *testing.T) {
	fixture := newProviderSupplyFixture(t)
	agentA := fixture.createBoundAgent(t, "agent-source-a", 1001)
	agentB := fixture.createBoundAgent(t, "agent-source-b", 1002)
	require.NotEqual(t, agentA.ID, agentB.ID)

	candidateA := reportSupplyCandidate(t, fixture, TechnicalResourceCredential{
		SourceType: model.TechnicalResourceBindingLegacyNode, SourceID: "1001", CredentialRevision: 1,
	}, "epoch-a", 1, "snapshot-a", `{
		"cluster_uid":"cluster-multi-source",
		"display_name":"Observed cluster",
		"namespaces":[
			{"uid":"namespace-stable","name":"before-rename","labels":{"owner":"team-a","secret-label":"drop"},"status":"Active"},
			{"uid":"namespace-second","name":"second","labels":{"app.kubernetes.io/name":"api"},"status":"Active"}
		]
	}`)
	candidateB := reportSupplyCandidate(t, fixture, TechnicalResourceCredential{
		SourceType: model.TechnicalResourceBindingLegacyNode, SourceID: "1002", CredentialRevision: 1,
	}, "epoch-b", 1, "snapshot-b", `{
		"cluster_uid":"cluster-multi-source",
		"display_name":"Second source name",
		"namespaces":[
			{"uid":"namespace-stable","name":"after-rename","labels":{"owner":"team-b"},"status":"Active"},
			{"uid":"namespace-second","name":"second","labels":{"app.kubernetes.io/name":"api"},"status":"Active"}
		]
	}`)

	acceptedA, err := fixture.service.AcceptSupplyCandidate(context.Background(), fixture.authorization, AcceptSupplyCandidateInput{
		CandidateID: candidateA.ID, ExpectedRowVersion: candidateA.RowVersion,
		DisplayName: "Managed Cluster", Reason: "accept primary source",
	})
	require.NoError(t, err)
	require.Equal(t, model.SupplyCandidateLinked, acceptedA.Candidate.ReviewState)
	require.Equal(t, model.PlatformResourceDraft, acceptedA.Resource.LifecycleState)
	require.Equal(t, "Managed Cluster", acceptedA.Resource.DisplayName)
	require.Zero(t, acceptedA.Resource.AllocatableScopeCount)
	require.True(t, acceptedA.Source.IsPrimary)
	require.Equal(t, model.ResourceScopeDraft, acceptedA.ClusterScope.LifecycleState)
	require.Equal(t, model.ResourceScopeIsolationNone, acceptedA.ClusterScope.IsolationMode)
	require.Len(t, acceptedA.NamespaceScopes, 2)
	for _, scope := range acceptedA.NamespaceScopes {
		require.Equal(t, model.ResourceScopeDraft, scope.LifecycleState)
		require.Equal(t, model.ResourceScopeIsolationNamespaceIsolated, scope.IsolationMode)
		require.NotNil(t, scope.ParentID)
		require.Equal(t, acceptedA.ClusterScope.ID, *scope.ParentID)
	}

	var beforeRename model.NamespaceObservation
	require.NoError(t, fixture.database.Where(
		"cluster_resource_id = ? AND namespace_uid = ?", acceptedA.Resource.ID, "namespace-stable",
	).First(&beforeRename).Error)
	require.Equal(t, "before-rename", beforeRename.Name)
	require.Equal(t, int64(1), beforeRename.Revision)
	require.NotContains(t, beforeRename.LabelSnapshot, "secret-label")
	var beforeRenameScope model.ResourceScope
	require.NoError(t, fixture.database.First(&beforeRenameScope, "namespace_observation_id = ?", beforeRename.ID).Error)

	acceptedB, err := fixture.service.AcceptSupplyCandidate(context.Background(), fixture.authorization, AcceptSupplyCandidateInput{
		CandidateID: candidateB.ID, ExpectedRowVersion: candidateB.RowVersion, Reason: "accept secondary source",
	})
	require.NoError(t, err)
	require.Equal(t, acceptedA.Resource.ID, acceptedB.Resource.ID)
	require.False(t, acceptedB.Source.IsPrimary)
	require.Equal(t, acceptedA.ClusterScope.ID, acceptedB.ClusterScope.ID)

	var afterRename model.NamespaceObservation
	require.NoError(t, fixture.database.First(&afterRename, "id = ?", beforeRename.ID).Error)
	require.Equal(t, beforeRename.ID, afterRename.ID)
	require.Equal(t, "after-rename", afterRename.Name)
	require.Equal(t, int64(2), afterRename.Revision)
	var afterRenameScope model.ResourceScope
	require.NoError(t, fixture.database.First(&afterRenameScope, "id = ?", beforeRenameScope.ID).Error)
	require.Equal(t, beforeRenameScope.ID, afterRenameScope.ID)
	require.Equal(t, afterRename.Revision, afterRenameScope.EvidenceRevision)
	require.Greater(t, afterRenameScope.RowVersion, beforeRenameScope.RowVersion)

	var resourceCount, sourceCount, clusterScopeCount, namespaceScopeCount, observationCount int64
	require.NoError(t, fixture.database.Model(&model.PlatformResource{}).Count(&resourceCount).Error)
	require.NoError(t, fixture.database.Model(&model.PlatformResourceSource{}).Count(&sourceCount).Error)
	require.NoError(t, fixture.database.Model(&model.ResourceScope{}).Where("type = ?", model.ResourceScopeCluster).Count(&clusterScopeCount).Error)
	require.NoError(t, fixture.database.Model(&model.ResourceScope{}).Where("type = ?", model.ResourceScopeNamespace).Count(&namespaceScopeCount).Error)
	require.NoError(t, fixture.database.Model(&model.NamespaceObservation{}).Count(&observationCount).Error)
	require.Equal(t, int64(1), resourceCount)
	require.Equal(t, int64(2), sourceCount)
	require.Equal(t, int64(1), clusterScopeCount)
	require.Equal(t, int64(2), namespaceScopeCount)
	require.Equal(t, int64(2), observationCount)
}
