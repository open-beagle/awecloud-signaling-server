package model

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newProviderSupplyModelDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared&_pragma=foreign_keys(1)", uuid.NewString())
	database, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(
		&User{},
		&ResourceProvider{},
		&TechnicalResource{},
		&TechnicalResourceBinding{},
		&SupplyInventoryReceipt{},
		&SupplyCandidate{},
		&PlatformResource{},
		&PlatformResourceSource{},
		&NamespaceObservation{},
		&ResourceScope{},
	))
	return database
}

func seedProviderSupplyPrincipals(t *testing.T, database *gorm.DB) {
	t.Helper()
	require.NoError(t, database.Create(&User{ID: 1, Name: "provider-admin", Role: UserRoleClient, SecretHash: "fixture-hash", Enabled: true}).Error)
	providers := []ResourceProvider{
		{ID: "provider-a", Key: "provider-a", DisplayName: "Provider A", DomainScope: ProviderDomainNamed, DomainLabel: "provider-a", Status: ProviderStatusActive, Revision: 1, RowVersion: 1},
		{ID: "provider-b", Key: "provider-b", DisplayName: "Provider B", DomainScope: ProviderDomainNamed, DomainLabel: "provider-b", Status: ProviderStatusActive, Revision: 1, RowVersion: 1},
	}
	require.NoError(t, database.Create(&providers).Error)
}

func TestProviderSupplyTechnicalResourceAndBindingConstraints(t *testing.T) {
	database := newProviderSupplyModelDB(t)
	seedProviderSupplyPrincipals(t, database)

	agentA := TechnicalResource{
		ID: "agent-a", ProviderID: "provider-a", Type: TechnicalResourceAgent, StableKey: "agent-stable-a",
		DomainLabel:    "agent-stable-a",
		LifecycleState: TechnicalResourceRegistered, HealthState: ResourceHealthOnline, CredentialRevision: 1,
		ConfigRevision: 1, ObservedRevision: 1, RowVersion: 1,
	}
	require.NoError(t, database.Create(&agentA).Error)

	duplicateStableKey := agentA
	duplicateStableKey.ID = "agent-a-duplicate"
	require.Error(t, database.Create(&duplicateStableKey).Error)

	agentB := agentA
	agentB.ID, agentB.ProviderID, agentB.DomainLabel = "agent-b", "provider-b", "agent-b"
	require.NoError(t, database.Create(&agentB).Error)

	endpointAID := "endpoint-a"
	endpointA := TechnicalResource{
		ID: endpointAID, ProviderID: "provider-a", Type: TechnicalResourceEndpoint, StableKey: "endpoint-stable-a", ParentID: &agentA.ID,
		LifecycleState: TechnicalResourceRegistered, HealthState: ResourceHealthOnline, CredentialRevision: 1,
		ConfigRevision: 1, ObservedRevision: 1, RowVersion: 1,
	}
	require.NoError(t, database.Create(&endpointA).Error)

	endpointWithoutParent := endpointA
	endpointWithoutParent.ID, endpointWithoutParent.StableKey, endpointWithoutParent.ParentID = "endpoint-no-parent", "endpoint-no-parent", nil
	require.Error(t, database.Create(&endpointWithoutParent).Error)

	crossProviderParent := endpointA
	crossProviderParent.ID, crossProviderParent.ProviderID, crossProviderParent.StableKey = "endpoint-b", "provider-b", "endpoint-stable-b"
	require.Error(t, database.Create(&crossProviderParent).Error)

	binding := TechnicalResourceBinding{
		ID: "binding-a", TechnicalResourceID: agentA.ID, SourceType: TechnicalResourceBindingLegacyNode, SourceID: "1001",
		CredentialRevision: 1, Enabled: true, BoundByUserID: 1, Reason: "anonymous fixture binding", RowVersion: 1,
	}
	require.NoError(t, database.Create(&binding).Error)

	duplicateSource := binding
	duplicateSource.ID, duplicateSource.TechnicalResourceID = "binding-duplicate-source", endpointAID
	require.Error(t, database.Create(&duplicateSource).Error)

	duplicateActiveBinding := binding
	duplicateActiveBinding.ID, duplicateActiveBinding.SourceID = "binding-duplicate-active", "1002"
	require.NoError(t, database.Create(&duplicateActiveBinding).Error)

	require.NoError(t, database.Model(&binding).Update("enabled", false).Error)
	replacementBinding := binding
	replacementBinding.ID, replacementBinding.SourceID, replacementBinding.Enabled = "binding-replacement", "1003", true
	require.NoError(t, database.Create(&replacementBinding).Error)
}

func TestProviderSupplyInventoryAndCandidateConstraints(t *testing.T) {
	database := newProviderSupplyModelDB(t)
	seedProviderSupplyPrincipals(t, database)
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)

	agents := []TechnicalResource{
		{ID: "agent-a", ProviderID: "provider-a", Type: TechnicalResourceAgent, StableKey: "agent-a", DomainLabel: "agent-a", LifecycleState: TechnicalResourceRegistered, HealthState: ResourceHealthOnline, CredentialRevision: 1, ConfigRevision: 1, RowVersion: 1},
		{ID: "agent-b", ProviderID: "provider-b", Type: TechnicalResourceAgent, StableKey: "agent-b", DomainLabel: "agent-b", LifecycleState: TechnicalResourceRegistered, HealthState: ResourceHealthOnline, CredentialRevision: 1, ConfigRevision: 1, RowVersion: 1},
	}
	require.NoError(t, database.Create(&agents).Error)

	receipt := SupplyInventoryReceipt{
		ID: "receipt-a", TechnicalResourceID: "agent-a", SourceEpoch: "epoch-a", Sequence: 1, SchemaVersion: 1,
		SnapshotID: "snapshot-a", BatchIndex: 0, BatchCount: 2, PayloadHash: strings.Repeat("a", 64),
		CanonicalPayload: `{}`, ReceivedAt: now, Status: SupplyInventoryReceiptStaging, ResultCode: "BATCH_STAGED",
	}
	require.NoError(t, database.Create(&receipt).Error)

	duplicateSequence := receipt
	duplicateSequence.ID, duplicateSequence.SnapshotID, duplicateSequence.BatchIndex = "receipt-duplicate-sequence", "snapshot-b", 0
	require.Error(t, database.Create(&duplicateSequence).Error)

	duplicateBatch := receipt
	duplicateBatch.ID, duplicateBatch.Sequence = "receipt-duplicate-batch", 2
	require.Error(t, database.Create(&duplicateBatch).Error)

	invalidBatch := receipt
	invalidBatch.ID, invalidBatch.Sequence, invalidBatch.BatchIndex = "receipt-invalid-batch", 3, 2
	require.Error(t, database.Create(&invalidBatch).Error)

	candidateA := SupplyCandidate{
		ID: "candidate-a", ProviderID: "provider-a", TechnicalResourceID: "agent-a", ResourceType: SupplyResourceKubernetes,
		StableKey: "cluster-stable", IdentityQuality: SupplyIdentityStrong, PayloadHash: strings.Repeat("b", 64),
		ObservationSnapshot: `{}`, FirstObservedAt: now, LastObservedAt: now, LeaseExpiresAt: now.Add(time.Minute),
		ReviewState: SupplyCandidatePendingReview, RowVersion: 1,
	}
	require.NoError(t, database.Create(&candidateA).Error)

	duplicateCandidate := candidateA
	duplicateCandidate.ID = "candidate-a-duplicate"
	require.Error(t, database.Create(&duplicateCandidate).Error)

	crossProviderSource := candidateA
	crossProviderSource.ID, crossProviderSource.ProviderID, crossProviderSource.StableKey = "candidate-cross-provider", "provider-b", "cluster-cross-provider"
	require.Error(t, database.Create(&crossProviderSource).Error)

	candidateB := candidateA
	candidateB.ID, candidateB.ProviderID, candidateB.TechnicalResourceID = "candidate-b", "provider-b", "agent-b"
	require.NoError(t, database.Create(&candidateB).Error)
}

func TestProviderSupplyResourceSourceAndScopeConstraints(t *testing.T) {
	database := newProviderSupplyModelDB(t)
	seedProviderSupplyPrincipals(t, database)
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)

	agents := []TechnicalResource{
		{ID: "agent-a", ProviderID: "provider-a", Type: TechnicalResourceAgent, StableKey: "agent-a", DomainLabel: "agent-a", LifecycleState: TechnicalResourceRegistered, HealthState: ResourceHealthOnline, CredentialRevision: 1, ConfigRevision: 1, RowVersion: 1},
		{ID: "agent-b", ProviderID: "provider-b", Type: TechnicalResourceAgent, StableKey: "agent-b", DomainLabel: "agent-b", LifecycleState: TechnicalResourceRegistered, HealthState: ResourceHealthOnline, CredentialRevision: 1, ConfigRevision: 1, RowVersion: 1},
	}
	require.NoError(t, database.Create(&agents).Error)
	candidates := []SupplyCandidate{
		{ID: "candidate-a", ProviderID: "provider-a", TechnicalResourceID: "agent-a", ResourceType: SupplyResourceKubernetes, StableKey: "cluster-a", IdentityQuality: SupplyIdentityStrong, PayloadHash: strings.Repeat("a", 64), FirstObservedAt: now, LastObservedAt: now, LeaseExpiresAt: now.Add(time.Minute), ReviewState: SupplyCandidateAccepted, RowVersion: 1},
		{ID: "candidate-a-2", ProviderID: "provider-a", TechnicalResourceID: "agent-a", ResourceType: SupplyResourceKubernetes, StableKey: "cluster-a-source-2", IdentityQuality: SupplyIdentityStrong, PayloadHash: strings.Repeat("b", 64), FirstObservedAt: now, LastObservedAt: now, LeaseExpiresAt: now.Add(time.Minute), ReviewState: SupplyCandidateAccepted, RowVersion: 1},
		{ID: "candidate-b", ProviderID: "provider-b", TechnicalResourceID: "agent-b", ResourceType: SupplyResourceKubernetes, StableKey: "cluster-a", IdentityQuality: SupplyIdentityStrong, PayloadHash: strings.Repeat("c", 64), FirstObservedAt: now, LastObservedAt: now, LeaseExpiresAt: now.Add(time.Minute), ReviewState: SupplyCandidateAccepted, RowVersion: 1},
	}
	require.NoError(t, database.Create(&candidates).Error)

	resources := []PlatformResource{
		{ID: "resource-a", ProviderID: "provider-a", Type: SupplyResourceKubernetes, StableKey: "cluster-a", DisplayName: "Cluster A", LifecycleState: PlatformResourceDraft, HealthState: ResourceHealthOnline, CapabilityRevision: 1, RowVersion: 1},
		{ID: "resource-a-2", ProviderID: "provider-a", Type: SupplyResourceKubernetes, StableKey: "cluster-a-2", DisplayName: "Cluster A2", LifecycleState: PlatformResourceDraft, HealthState: ResourceHealthOnline, CapabilityRevision: 1, RowVersion: 1},
		{ID: "resource-b", ProviderID: "provider-b", Type: SupplyResourceKubernetes, StableKey: "cluster-a", DisplayName: "Cluster B", LifecycleState: PlatformResourceDraft, HealthState: ResourceHealthOnline, CapabilityRevision: 1, RowVersion: 1},
	}
	require.NoError(t, database.Create(&resources).Error)
	duplicateResource := resources[0]
	duplicateResource.ID = "resource-a-duplicate"
	require.Error(t, database.Create(&duplicateResource).Error)

	primarySource := PlatformResourceSource{ID: "source-a", ProviderID: "provider-a", PlatformResourceID: "resource-a", SupplyCandidateID: "candidate-a", IsPrimary: true, LinkedAt: now, LastConfirmedAt: now}
	require.NoError(t, database.Create(&primarySource).Error)
	secondPrimary := PlatformResourceSource{ID: "source-a-2", ProviderID: "provider-a", PlatformResourceID: "resource-a", SupplyCandidateID: "candidate-a-2", IsPrimary: true, LinkedAt: now, LastConfirmedAt: now}
	require.Error(t, database.Create(&secondPrimary).Error)
	secondPrimary.IsPrimary = false
	require.NoError(t, database.Create(&secondPrimary).Error)
	crossProviderSource := PlatformResourceSource{ID: "source-cross-provider", ProviderID: "provider-a", PlatformResourceID: "resource-a-2", SupplyCandidateID: "candidate-b", LinkedAt: now, LastConfirmedAt: now}
	require.Error(t, database.Create(&crossProviderSource).Error)

	observations := []NamespaceObservation{
		{ID: "observation-a", ProviderID: "provider-a", ClusterResourceID: "resource-a", NamespaceUID: "namespace-uid", Name: "namespace-a", Revision: 1, ObservedAt: now, LeaseExpiresAt: now.Add(time.Minute), State: NamespaceObservationObserved},
		{ID: "observation-a-2", ProviderID: "provider-a", ClusterResourceID: "resource-a-2", NamespaceUID: "namespace-uid", Name: "namespace-a", Revision: 1, ObservedAt: now, LeaseExpiresAt: now.Add(time.Minute), State: NamespaceObservationObserved},
	}
	require.NoError(t, database.Create(&observations).Error)
	duplicateObservation := observations[0]
	duplicateObservation.ID = "observation-a-duplicate"
	require.Error(t, database.Create(&duplicateObservation).Error)

	clusterScopeA := ResourceScope{ID: "scope-cluster-a", ProviderID: "provider-a", PlatformResourceID: "resource-a", Type: ResourceScopeCluster, StableKey: "cluster-a", LifecycleState: ResourceScopeDraft, IsolationMode: ResourceScopeIsolationNone, ConfigRevision: 1, EvidenceRevision: 1, RowVersion: 1}
	require.NoError(t, database.Create(&clusterScopeA).Error)
	namespaceScopeA := ResourceScope{ID: "scope-namespace-a", ProviderID: "provider-a", PlatformResourceID: "resource-a", Type: ResourceScopeNamespace, StableKey: "namespace-uid", ParentID: &clusterScopeA.ID, NamespaceObservationID: &observations[0].ID, LifecycleState: ResourceScopeDraft, IsolationMode: ResourceScopeIsolationNamespaceIsolated, ConfigRevision: 1, EvidenceRevision: 1, RowVersion: 1}
	require.NoError(t, database.Create(&namespaceScopeA).Error)

	invalidClusterShape := clusterScopeA
	invalidClusterShape.ID, invalidClusterShape.StableKey, invalidClusterShape.ParentID = "scope-invalid-cluster", "invalid-cluster", &clusterScopeA.ID
	require.Error(t, database.Create(&invalidClusterShape).Error)

	clusterScopeA2 := ResourceScope{ID: "scope-cluster-a-2", ProviderID: "provider-a", PlatformResourceID: "resource-a-2", Type: ResourceScopeCluster, StableKey: "cluster-a-2", LifecycleState: ResourceScopeDraft, IsolationMode: ResourceScopeIsolationNone, ConfigRevision: 1, EvidenceRevision: 1, RowVersion: 1}
	require.NoError(t, database.Create(&clusterScopeA2).Error)
	crossResourceParent := namespaceScopeA
	crossResourceParent.ID, crossResourceParent.PlatformResourceID, crossResourceParent.StableKey, crossResourceParent.NamespaceObservationID = "scope-cross-resource-parent", "resource-a-2", "namespace-cross-parent", &observations[1].ID
	require.Error(t, database.Create(&crossResourceParent).Error)
	crossResourceObservation := namespaceScopeA
	crossResourceObservation.ID, crossResourceObservation.PlatformResourceID, crossResourceObservation.StableKey, crossResourceObservation.ParentID = "scope-cross-resource-observation", "resource-a-2", "namespace-cross-observation", &clusterScopeA2.ID
	require.Error(t, database.Create(&crossResourceObservation).Error)
}
