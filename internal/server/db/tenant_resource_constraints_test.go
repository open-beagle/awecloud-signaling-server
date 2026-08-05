package db

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

type tenantResourceConstraintFixture struct {
	now            time.Time
	admin          model.User
	member         model.User
	otherMember    model.User
	tenantA        model.Tenant
	tenantB        model.Tenant
	membership     model.TenantMembership
	desktop        model.Node
	otherDesktop   model.Node
	agentA         model.TechnicalResource
	agentB         model.TechnicalResource
	namespaceA     model.ResourceScope
	namespaceB     model.ResourceScope
	rootAllocation model.ResourceAllocation
	renewal        model.ResourceAllocation
	unrelated      model.ResourceAllocation
	renewalItem    model.ResourceAllocationItem
	unrelatedItem  model.ResourceAllocationItem
	observation    model.WorkloadObservation
	evidence       model.WorkloadObservationSource
	resource       model.TenantResource
	source         model.TenantResourceSource
	target         model.TenantResourceTargetRevision
	grant          model.TenantAccessGrant
	groupGrant     model.TenantAccessGrant
}

func newTenantResourceConstraintFixture(t *testing.T) tenantResourceConstraintFixture {
	t.Helper()
	original := DB
	t.Cleanup(func() { DB = original })
	require.NoError(t, InitDB(config.DatabaseSection{Type: "sqlite", Path: filepath.Join(t.TempDir(), "signal.db")}))
	t.Cleanup(func() {
		if current, err := DB.DB(); err == nil {
			_ = current.Close()
		}
	})

	var foreignKeysEnabled int
	require.NoError(t, DB.Raw("PRAGMA foreign_keys").Scan(&foreignKeysEnabled).Error)
	require.Zero(t, foreignKeysEnabled)
	var triggerCount int64
	require.NoError(t, DB.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger' AND name LIKE 'trg_s4_%'").Scan(&triggerCount).Error)
	require.Equal(t, int64(len(tenantResourceTriggers)), triggerCount)

	now := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	admin := model.User{ID: 1, Name: "tenant-admin", Role: model.UserRoleClient, SecretHash: "fixture", Enabled: true}
	member := model.User{ID: 2, Name: "tenant-member", Role: model.UserRoleClient, SecretHash: "fixture", Enabled: true}
	otherMember := model.User{ID: 3, Name: "other-member", Role: model.UserRoleClient, SecretHash: "fixture", Enabled: true}
	require.NoError(t, DB.Create(&[]model.User{admin, member, otherMember}).Error)

	tenantA := model.Tenant{ID: "tenant-a", Key: "tenant-a", Name: "Tenant A", Status: model.TenantStatusActive}
	tenantB := model.Tenant{ID: "tenant-b", Key: "tenant-b", Name: "Tenant B", Status: model.TenantStatusActive}
	require.NoError(t, DB.Create(&[]model.Tenant{tenantA, tenantB}).Error)
	membership := model.TenantMembership{ID: 101, TenantID: tenantA.ID, UserID: member.ID, Role: "member", Enabled: true}
	otherMembership := model.TenantMembership{ID: 102, TenantID: tenantB.ID, UserID: otherMember.ID, Role: "member", Enabled: true}
	require.NoError(t, DB.Create(&[]model.TenantMembership{membership, otherMembership}).Error)
	desktop := model.Node{ID: 2001, UserID: member.ID, Name: "member-desktop", Type: model.NodeTypeDesktop}
	otherDesktop := model.Node{ID: 2002, UserID: otherMember.ID, Name: "other-desktop", Type: model.NodeTypeDesktop}
	require.NoError(t, DB.Create(&[]model.Node{desktop, otherDesktop}).Error)
	require.NoError(t, DB.Create(&[]model.Group{
		{ID: 301, TenantID: tenantA.ID, Name: "tenant-a-members"},
		{ID: 302, TenantID: tenantB.ID, Name: "tenant-b-members"},
	}).Error)
	require.NoError(t, DB.Create(&model.GroupMember{ID: 401, GroupID: 301, UserID: member.ID}).Error)

	providerA := model.ResourceProvider{ID: "provider-a", Key: "provider-a", DisplayName: "Provider A", DomainScope: model.ProviderDomainNamed, DomainLabel: "provider-a", Status: model.ProviderStatusActive, Revision: 1, RowVersion: 1}
	providerB := model.ResourceProvider{ID: "provider-b", Key: "provider-b", DisplayName: "Provider B", DomainScope: model.ProviderDomainNamed, DomainLabel: "provider-b", Status: model.ProviderStatusActive, Revision: 1, RowVersion: 1}
	require.NoError(t, DB.Create(&[]model.ResourceProvider{providerA, providerB}).Error)
	clusterA := model.PlatformResource{ID: "cluster-a", ProviderID: providerA.ID, Type: model.SupplyResourceKubernetes, StableKey: "cluster-a", DisplayName: "Cluster A", LifecycleState: model.PlatformResourceActive, HealthState: model.ResourceHealthOnline, CapabilityRevision: 1, RowVersion: 1}
	clusterB := model.PlatformResource{ID: "cluster-b", ProviderID: providerB.ID, Type: model.SupplyResourceKubernetes, StableKey: "cluster-b", DisplayName: "Cluster B", LifecycleState: model.PlatformResourceActive, HealthState: model.ResourceHealthOnline, CapabilityRevision: 1, RowVersion: 1}
	require.NoError(t, DB.Create(&[]model.PlatformResource{clusterA, clusterB}).Error)
	namespaceObservationA := model.NamespaceObservation{ID: "namespace-observation-a", ProviderID: providerA.ID, ClusterResourceID: clusterA.ID, NamespaceUID: "namespace-uid-a", Name: "namespace-a", Revision: 1, ObservedAt: now, LeaseExpiresAt: now.Add(24 * time.Hour), State: model.NamespaceObservationObserved}
	namespaceObservationB := model.NamespaceObservation{ID: "namespace-observation-b", ProviderID: providerB.ID, ClusterResourceID: clusterB.ID, NamespaceUID: "namespace-uid-b", Name: "namespace-b", Revision: 1, ObservedAt: now, LeaseExpiresAt: now.Add(24 * time.Hour), State: model.NamespaceObservationObserved}
	require.NoError(t, DB.Create(&[]model.NamespaceObservation{namespaceObservationA, namespaceObservationB}).Error)
	clusterScopeA := model.ResourceScope{ID: "scope-cluster-a", ProviderID: providerA.ID, PlatformResourceID: clusterA.ID, Type: model.ResourceScopeCluster, StableKey: "cluster-a", LifecycleState: model.ResourceScopeAllocatable, IsolationMode: model.ResourceScopeIsolationNone, ConfigRevision: 1, EvidenceRevision: 1, RowVersion: 1}
	clusterScopeB := model.ResourceScope{ID: "scope-cluster-b", ProviderID: providerB.ID, PlatformResourceID: clusterB.ID, Type: model.ResourceScopeCluster, StableKey: "cluster-b", LifecycleState: model.ResourceScopeAllocatable, IsolationMode: model.ResourceScopeIsolationNone, ConfigRevision: 1, EvidenceRevision: 1, RowVersion: 1}
	require.NoError(t, DB.Create(&[]model.ResourceScope{clusterScopeA, clusterScopeB}).Error)
	namespaceA := model.ResourceScope{ID: "scope-namespace-a", ProviderID: providerA.ID, PlatformResourceID: clusterA.ID, Type: model.ResourceScopeNamespace, StableKey: "namespace-a", ParentID: &clusterScopeA.ID, NamespaceObservationID: &namespaceObservationA.ID, LifecycleState: model.ResourceScopeAllocatable, IsolationMode: model.ResourceScopeIsolationNamespaceIsolated, ConfigRevision: 1, EvidenceRevision: 1, RowVersion: 1}
	namespaceB := model.ResourceScope{ID: "scope-namespace-b", ProviderID: providerB.ID, PlatformResourceID: clusterB.ID, Type: model.ResourceScopeNamespace, StableKey: "namespace-b", ParentID: &clusterScopeB.ID, NamespaceObservationID: &namespaceObservationB.ID, LifecycleState: model.ResourceScopeAllocatable, IsolationMode: model.ResourceScopeIsolationNamespaceIsolated, ConfigRevision: 1, EvidenceRevision: 1, RowVersion: 1}
	require.NoError(t, DB.Create(&[]model.ResourceScope{namespaceA, namespaceB}).Error)

	agentA := model.TechnicalResource{ID: "agent-a", ProviderID: providerA.ID, Type: model.TechnicalResourceAgent, StableKey: "agent-a", DomainLabel: "agent-a", LifecycleState: model.TechnicalResourceRegistered, HealthState: model.ResourceHealthOnline, CredentialRevision: 1, ConfigRevision: 1, RowVersion: 1}
	agentB := model.TechnicalResource{ID: "agent-b", ProviderID: providerB.ID, Type: model.TechnicalResourceAgent, StableKey: "agent-b", DomainLabel: "agent-b", LifecycleState: model.TechnicalResourceRegistered, HealthState: model.ResourceHealthOnline, CredentialRevision: 1, ConfigRevision: 1, RowVersion: 1}
	require.NoError(t, DB.Create(&[]model.TechnicalResource{agentA, agentB}).Error)
	candidateA := model.SupplyCandidate{ID: "candidate-cluster-a", ProviderID: providerA.ID, TechnicalResourceID: agentA.ID, ResourceType: model.SupplyResourceKubernetes, StableKey: "cluster-a", IdentityQuality: model.SupplyIdentityStrong, PayloadHash: strings.Repeat("8", 64), FirstObservedAt: now, LastObservedAt: now, LeaseExpiresAt: now.Add(time.Hour), ReviewState: model.SupplyCandidateAccepted, RowVersion: 1}
	candidateB := model.SupplyCandidate{ID: "candidate-cluster-b", ProviderID: providerB.ID, TechnicalResourceID: agentB.ID, ResourceType: model.SupplyResourceKubernetes, StableKey: "cluster-b", IdentityQuality: model.SupplyIdentityStrong, PayloadHash: strings.Repeat("9", 64), FirstObservedAt: now, LastObservedAt: now, LeaseExpiresAt: now.Add(time.Hour), ReviewState: model.SupplyCandidateAccepted, RowVersion: 1}
	require.NoError(t, DB.Create(&[]model.SupplyCandidate{candidateA, candidateB}).Error)
	require.NoError(t, DB.Create(&[]model.PlatformResourceSource{
		{ID: "platform-source-a", ProviderID: providerA.ID, PlatformResourceID: clusterA.ID, SupplyCandidateID: candidateA.ID, LinkedAt: now, LastConfirmedAt: now},
		{ID: "platform-source-b", ProviderID: providerB.ID, PlatformResourceID: clusterB.ID, SupplyCandidateID: candidateB.ID, LinkedAt: now, LastConfirmedAt: now},
	}).Error)

	rootAllocation, _ := createS4DraftAllocation(t, "allocation-root", tenantA.ID, namespaceA, admin.ID, nil, now.Add(-25*time.Hour))
	renewal, renewalItem := createS4DraftAllocation(t, "allocation-renewal", tenantA.ID, namespaceA, admin.ID, &rootAllocation.ID, now.Add(-time.Hour))
	unrelated, unrelatedItem := createS4DraftAllocation(t, "allocation-unrelated", tenantA.ID, namespaceA, admin.ID, nil, now.Add(48*time.Hour))
	require.NoError(t, DB.Model(&model.ResourceAllocation{}).Where("id = ?", renewal.ID).Updates(map[string]any{
		"state": model.ResourceAllocationActive, "row_version": int64(2),
	}).Error)
	renewal.State = model.ResourceAllocationActive
	renewal.RowVersion = 2

	observation := model.WorkloadObservation{
		ID: "workload-service-443", NamespaceScopeID: namespaceA.ID, Kind: model.WorkloadObservationServicePort,
		StableKey: strings.Repeat("a", 64), IdentityQuality: model.WorkloadIdentityStrong,
		State: model.WorkloadObservationEligible, Ready: true, ObservedRevision: 1, LabelSnapshot: "{}",
		FirstObservedAt: now, LastObservedAt: now, LeaseExpiresAt: now.Add(time.Minute), RowVersion: 1,
	}
	require.NoError(t, DB.Create(&observation).Error)
	evidence := model.WorkloadObservationSource{
		ID: "workload-source-a", WorkloadObservationID: observation.ID, SourceTechnicalResourceID: agentA.ID,
		SourceEpoch: "epoch-a", Sequence: 1, PayloadHash: strings.Repeat("b", 64),
		State: model.WorkloadObservationSourceObserved, Ready: true, TargetSnapshot: `{"service_uid":"service-a","port":443}`,
		ObservedAt: now, ReceivedAt: now, LeaseExpiresAt: now.Add(time.Minute), SourceRevision: 1, RowVersion: 1,
	}
	require.NoError(t, DB.Create(&evidence).Error)
	resource := model.TenantResource{
		ID: "tenant-resource-service-443", TenantID: tenantA.ID, Type: model.TenantResourceContainerService,
		StableKey: strings.Repeat("c", 64), EntitlementLineageID: rootAllocation.ID, DisplayName: "service-a:443",
		VisibilityState: model.TenantResourceVisible, AvailabilityState: model.TenantResourceAvailable, Revision: 1, RowVersion: 1,
	}
	require.NoError(t, DB.Create(&resource).Error)
	source := model.TenantResourceSource{
		ID: "tenant-resource-source-a", TenantResourceID: resource.ID, AllocationItemID: renewalItem.ID,
		WorkloadObservationID: observation.ID, Enabled: true, EnabledAt: now, SourceRevision: 1, RowVersion: 1,
	}
	require.NoError(t, DB.Create(&source).Error)
	target := model.TenantResourceTargetRevision{
		ID: "target-revision-1", TenantResourceSourceID: source.ID, Revision: 1,
		TargetType: model.WorkloadObservationServicePort, TargetSnapshot: evidence.TargetSnapshot,
		SourceTechnicalResourceID: agentA.ID, AccessTechnicalResourceID: agentA.ID, Ready: true,
		ObservedAt: now, ObservationRevision: 1, SourceRevision: 1,
	}
	require.NoError(t, DB.Create(&target).Error)
	grant := model.TenantAccessGrant{
		ID: "grant-user-a", TenantID: tenantA.ID, TenantResourceID: resource.ID,
		SubjectType: model.TenantAccessGrantSubjectUser, SubjectKey: fmt.Sprint(member.ID), SubjectUserID: &member.ID,
		Actions: `["connect"]`, ValidFrom: now.Add(-time.Minute), MaxSessionSeconds: 3600,
		Status: model.TenantAccessGrantEnabled, Revision: 1, RowVersion: 1, CreatedByUserID: admin.ID,
	}
	require.NoError(t, DB.Create(&grant).Error)
	groupID := int64(301)
	groupGrant := grant
	groupGrant.ID = "grant-group-a"
	groupGrant.SubjectType = model.TenantAccessGrantSubjectGroup
	groupGrant.SubjectKey = fmt.Sprint(groupID)
	groupGrant.SubjectUserID = nil
	groupGrant.SubjectGroupID = &groupID
	require.NoError(t, DB.Create(&groupGrant).Error)

	return tenantResourceConstraintFixture{
		now: now, admin: admin, member: member, otherMember: otherMember, tenantA: tenantA, tenantB: tenantB,
		membership: membership, desktop: desktop, otherDesktop: otherDesktop, agentA: agentA, agentB: agentB,
		namespaceA: namespaceA, namespaceB: namespaceB, rootAllocation: rootAllocation, renewal: renewal,
		unrelated: unrelated, renewalItem: renewalItem, unrelatedItem: unrelatedItem, observation: observation,
		evidence: evidence, resource: resource, source: source, target: target, grant: grant, groupGrant: groupGrant,
	}
}

func createS4DraftAllocation(t *testing.T, id, tenantID string, scope model.ResourceScope, actorID uint64, renewedFromID *string, validFrom time.Time) (model.ResourceAllocation, model.ResourceAllocationItem) {
	t.Helper()
	expiresAt := validFrom.Add(24 * time.Hour)
	allocation := model.ResourceAllocation{
		ID: id, TenantID: tenantID, Mode: model.ResourceAllocationLeased, ValidFrom: validFrom,
		ExpiresAt: &expiresAt, State: model.ResourceAllocationDraft, RowVersion: 1,
		CreatedByUserID: actorID, RenewedFromID: renewedFromID,
	}
	require.NoError(t, DB.Create(&allocation).Error)
	item := model.ResourceAllocationItem{ID: id + "-item", AllocationID: id, ScopeID: scope.ID, ScopeRowVersionSnapshot: scope.RowVersion}
	require.NoError(t, DB.Create(&item).Error)
	return allocation, item
}

func (f tenantResourceConstraintFixture) session(id string, grant model.TenantAccessGrant) model.ResourceSession {
	return model.ResourceSession{
		ID: id, TenantID: f.tenantA.ID, TenantResourceID: f.resource.ID, TenantResourceSourceID: f.source.ID,
		TargetRevisionID: f.target.ID, AllocationID: f.renewal.ID, AllocationItemID: f.renewalItem.ID,
		GrantID: grant.ID, GrantRevision: grant.Revision, UserID: f.member.ID,
		TenantMembershipID: f.membership.ID, DeviceID: f.desktop.ID,
		ActorUserID: f.member.ID, EffectiveUserID: f.member.ID,
		SessionType: model.ResourceSessionContainerService, Action: "connect",
		AccessTechnicalResourceID: f.agentA.ID, AuthorizationRevision: 1,
		ValidUntil: f.now.Add(30 * time.Second), Status: model.ResourceSessionAuthorizing,
		RequestID: "request-" + id, StartedAt: f.now, RowVersion: 1,
	}
}

func requireS4ConstraintError(t *testing.T, err error, code string) {
	t.Helper()
	require.Error(t, err)
	require.Contains(t, err.Error(), code)
}

func TestTenantResourceConstraintsPreserveTrustedResourceAndSessionChain(t *testing.T) {
	fixture := newTenantResourceConstraintFixture(t)

	t.Run("inventory and provider evidence", func(t *testing.T) {
		receipt := model.WorkloadInventoryReceipt{
			ID: "receipt-a", SourceTechnicalResourceID: fixture.agentA.ID, SourceEpoch: "epoch-a", Sequence: 1,
			SchemaVersion: 1, SnapshotID: "snapshot-a", BatchIndex: 0, BatchCount: 1,
			ClusterIdentityDigest: strings.Repeat("d", 64), NamespaceUID: "namespace-uid-a",
			Kind: model.WorkloadObservationServicePort, PayloadHash: strings.Repeat("e", 64),
			ObservedAt: fixture.now, ReceivedAt: fixture.now, Status: model.WorkloadInventoryReceiptStaging,
			LeaseExpiresAt: fixture.now.Add(30 * time.Second), ResultCode: "BATCH_STAGED",
		}
		require.NoError(t, DB.Create(&receipt).Error)
		requireS4ConstraintError(t, DB.Delete(&receipt).Error, "S4_WORKLOAD_RECEIPT_DELETE_FORBIDDEN")
		missingReceiptBatch := model.WorkloadInventoryBatch{ID: "batch-missing", ReceiptID: "missing", CanonicalPayload: "{}"}
		requireS4ConstraintError(t, DB.Create(&missingReceiptBatch).Error, "S4_WORKLOAD_RECEIPT_NOT_FOUND")

		crossProvider := fixture.evidence
		crossProvider.ID = "workload-source-cross-provider"
		crossProvider.SourceTechnicalResourceID = fixture.agentB.ID
		requireS4ConstraintError(t, DB.Create(&crossProvider).Error, "S4_WORKLOAD_SOURCE_PROVIDER_MISMATCH")
		unlinkedAgent := model.TechnicalResource{ID: "agent-a-unlinked", ProviderID: fixture.agentA.ProviderID, Type: model.TechnicalResourceAgent, StableKey: "agent-a-unlinked", DomainLabel: "agent-a-unlinked", LifecycleState: model.TechnicalResourceRegistered, HealthState: model.ResourceHealthOnline, CredentialRevision: 1, ConfigRevision: 1, RowVersion: 1}
		require.NoError(t, DB.Create(&unlinkedAgent).Error)
		sameProviderWrongSupply := fixture.evidence
		sameProviderWrongSupply.ID = "workload-source-wrong-supply"
		sameProviderWrongSupply.SourceTechnicalResourceID = unlinkedAgent.ID
		requireS4ConstraintError(t, DB.Create(&sameProviderWrongSupply).Error, "S4_WORKLOAD_SOURCE_PROVIDER_MISMATCH")
	})

	t.Run("allocation lineage and scope", func(t *testing.T) {
		crossTenantResource := fixture.resource
		crossTenantResource.ID = "tenant-resource-cross-tenant"
		crossTenantResource.TenantID = fixture.tenantB.ID
		crossTenantResource.StableKey = strings.Repeat("f", 64)
		requireS4ConstraintError(t, DB.Create(&crossTenantResource).Error, "S4_RESOURCE_LINEAGE_ROOT_MISMATCH")

		unrelatedSource := fixture.source
		unrelatedSource.ID = "tenant-resource-source-unrelated"
		unrelatedSource.AllocationItemID = fixture.unrelatedItem.ID
		requireS4ConstraintError(t, DB.Create(&unrelatedSource).Error, "S4_RESOURCE_SOURCE_LINEAGE_MISMATCH")

		otherObservation := fixture.observation
		otherObservation.ID = "workload-other-scope"
		otherObservation.NamespaceScopeID = fixture.namespaceB.ID
		otherObservation.StableKey = strings.Repeat("1", 64)
		require.NoError(t, DB.Create(&otherObservation).Error)
		wrongScopeSource := fixture.source
		wrongScopeSource.ID = "tenant-resource-source-wrong-scope"
		wrongScopeSource.WorkloadObservationID = otherObservation.ID
		requireS4ConstraintError(t, DB.Create(&wrongScopeSource).Error, "S4_RESOURCE_SOURCE_CHAIN_MISMATCH")

		_, crossScopeItem := createS4DraftAllocation(t, "allocation-cross-scope-renewal", fixture.tenantA.ID, fixture.namespaceB, fixture.admin.ID, &fixture.rootAllocation.ID, fixture.now)
		crossScopeRenewalSource := fixture.source
		crossScopeRenewalSource.ID = "tenant-resource-source-cross-scope-renewal"
		crossScopeRenewalSource.AllocationItemID = crossScopeItem.ID
		crossScopeRenewalSource.WorkloadObservationID = otherObservation.ID
		requireS4ConstraintError(t, DB.Create(&crossScopeRenewalSource).Error, "S4_RESOURCE_SOURCE_LINEAGE_MISMATCH")
	})

	t.Run("target revisions append without changing resource identity", func(t *testing.T) {
		invalidSequence := fixture.target
		invalidSequence.ID = "target-revision-3"
		invalidSequence.Revision = 3
		requireS4ConstraintError(t, DB.Create(&invalidSequence).Error, "S4_TARGET_REVISION_SEQUENCE_INVALID")

		require.NoError(t, DB.Model(&model.WorkloadObservation{}).Where("id = ?", fixture.observation.ID).Updates(map[string]any{
			"observed_revision": int64(1), "row_version": int64(2),
			"last_observed_at": fixture.now.Add(time.Second), "lease_expires_at": fixture.now.Add(time.Minute),
		}).Error)
		require.NoError(t, DB.Model(&model.WorkloadObservationSource{}).Where("id = ?", fixture.evidence.ID).Updates(map[string]any{
			"sequence": int64(2), "received_at": fixture.now.Add(time.Second),
			"lease_expires_at": fixture.now.Add(time.Minute), "source_revision": int64(1), "row_version": int64(2),
		}).Error)
		require.NoError(t, DB.Model(&model.WorkloadObservation{}).Where("id = ?", fixture.observation.ID).Updates(map[string]any{
			"observed_revision": int64(2), "row_version": int64(3),
			"last_observed_at": fixture.now.Add(2 * time.Second), "lease_expires_at": fixture.now.Add(time.Minute),
		}).Error)
		require.NoError(t, DB.Model(&model.WorkloadObservationSource{}).Where("id = ?", fixture.evidence.ID).Updates(map[string]any{
			"sequence": int64(2), "payload_hash": strings.Repeat("2", 64),
			"target_snapshot": `{"service_uid":"service-a","port":443,"endpoint":"new"}`,
			"observed_at":     fixture.now.Add(2 * time.Second), "received_at": fixture.now.Add(2 * time.Second),
			"lease_expires_at": fixture.now.Add(time.Minute), "source_revision": int64(2), "row_version": int64(3),
		}).Error)
		require.NoError(t, DB.Model(&model.TenantResourceSource{}).Where("id = ?", fixture.source.ID).Updates(map[string]any{
			"source_revision": int64(2), "row_version": int64(2),
		}).Error)
		supersededAt := fixture.now.Add(time.Second)
		require.NoError(t, DB.Model(&model.TenantResourceTargetRevision{}).Where("id = ?", fixture.target.ID).Update("superseded_at", supersededAt).Error)
		nextTarget := fixture.target
		nextTarget.ID = "target-revision-2"
		nextTarget.Revision = 2
		nextTarget.TargetSnapshot = `{"service_uid":"service-a","port":443,"endpoint":"new"}`
		nextTarget.ObservedAt = fixture.now.Add(time.Second)
		nextTarget.ObservationRevision = 2
		nextTarget.SourceRevision = 2
		nextTarget.SupersededAt = nil
		require.NoError(t, DB.Create(&nextTarget).Error)

		var resourceCount int64
		require.NoError(t, DB.Model(&model.TenantResource{}).Where("id = ?", fixture.resource.ID).Count(&resourceCount).Error)
		require.Equal(t, int64(1), resourceCount)
	})

	t.Run("grant subject tenant boundaries", func(t *testing.T) {
		otherUserID := fixture.otherMember.ID
		badUserGrant := fixture.grant
		badUserGrant.ID = "grant-wrong-membership"
		badUserGrant.SubjectKey = fmt.Sprint(otherUserID)
		badUserGrant.SubjectUserID = &otherUserID
		requireS4ConstraintError(t, DB.Create(&badUserGrant).Error, "S4_GRANT_USER_MEMBERSHIP_MISMATCH")

		otherGroupID := int64(302)
		badGroupGrant := fixture.groupGrant
		badGroupGrant.ID = "grant-wrong-group"
		badGroupGrant.SubjectKey = fmt.Sprint(otherGroupID)
		badGroupGrant.SubjectGroupID = &otherGroupID
		requireS4ConstraintError(t, DB.Create(&badGroupGrant).Error, "S4_GRANT_GROUP_TENANT_MISMATCH")
		requireS4ConstraintError(t, DB.Delete(&fixture.grant).Error, "S4_GRANT_DELETE_FORBIDDEN")
	})

	t.Run("session chain events and durable history", func(t *testing.T) {
		session := fixture.session("session-user", fixture.grant)
		require.NoError(t, DB.Create(&session).Error)
		groupSession := fixture.session("session-group", fixture.groupGrant)
		require.NoError(t, DB.Create(&groupSession).Error)
		require.NoError(t, DB.Model(&model.ResourceSession{}).Where("id = ?", groupSession.ID).Updates(map[string]any{
			"valid_until": fixture.now.Add(time.Minute), "row_version": int64(2),
		}).Error)
		requireS4ConstraintError(t, DB.Model(&model.ResourceSession{}).Where("id = ?", groupSession.ID).Updates(map[string]any{
			"authorization_revision": int64(2), "row_version": int64(3),
		}).Error, "S4_RESOURCE_SESSION_IDENTITY_IMMUTABLE")

		wrongDevice := fixture.session("session-wrong-device", fixture.grant)
		wrongDevice.DeviceID = fixture.otherDesktop.ID
		requireS4ConstraintError(t, DB.Create(&wrongDevice).Error, "S4_RESOURCE_SESSION_CHAIN_MISMATCH")
		requireS4ConstraintError(t, DB.Model(&model.ResourceSession{}).Where("id = ?", session.ID).Updates(map[string]any{
			"device_id": fixture.otherDesktop.ID, "row_version": int64(2),
		}).Error, "S4_RESOURCE_SESSION_IDENTITY_IMMUTABLE")
		require.NoError(t, DB.Model(&model.ResourceSession{}).Where("id = ?", session.ID).Updates(map[string]any{
			"status": model.ResourceSessionActive, "row_version": int64(2),
		}).Error)
		requireS4ConstraintError(t, DB.Model(&model.ResourceSession{}).Where("id = ?", session.ID).Updates(map[string]any{
			"status": model.ResourceSessionRejected, "row_version": int64(3),
		}).Error, "S4_RESOURCE_SESSION_STATUS_TRANSITION_INVALID")

		wrongSourceEvent := model.ResourceSessionEvent{
			ID: "event-wrong-source", EventID: "event-id-wrong-source", SourceTechnicalResourceID: fixture.agentB.ID,
			SessionID: session.ID, SourceSequence: 1, EventType: model.ResourceSessionEventConnected,
			OccurredAt: fixture.now, ReceivedAt: fixture.now, Payload: "{}",
		}
		requireS4ConstraintError(t, DB.Create(&wrongSourceEvent).Error, "S4_SESSION_EVENT_SOURCE_MISMATCH")
		event := wrongSourceEvent
		event.ID = "event-connected"
		event.EventID = "event-id-connected"
		event.SourceTechnicalResourceID = fixture.agentA.ID
		require.NoError(t, DB.Create(&event).Error)

		termination := model.ResourceSessionTermination{
			ID: "termination-1", SessionID: session.ID, CommandRevision: 1,
			ReasonCode: "GRANT_REVOKED", Reason: "grant revoked", Status: model.ResourceSessionTerminationPending,
		}
		require.NoError(t, DB.Create(&termination).Error)
		invalidTermination := termination
		invalidTermination.ID = "termination-3"
		invalidTermination.CommandRevision = 3
		requireS4ConstraintError(t, DB.Create(&invalidTermination).Error, "S4_SESSION_TERMINATION_SEQUENCE_INVALID")

		requireS4ConstraintError(t, DB.Delete(&event).Error, "S4_SESSION_EVENT_DELETE_FORBIDDEN")
		requireS4ConstraintError(t, DB.Delete(&session).Error, "S4_RESOURCE_SESSION_DELETE_FORBIDDEN")
		requireS4ConstraintError(t, DB.Delete(&fixture.source).Error, "S4_TENANT_RESOURCE_SOURCE_DELETE_FORBIDDEN")
		requireS4ConstraintError(t, DB.Delete(&fixture.target).Error, "S4_TARGET_REVISION_DELETE_FORBIDDEN")
	})
}
