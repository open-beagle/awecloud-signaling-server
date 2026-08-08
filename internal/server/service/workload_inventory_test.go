package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

type workloadInventoryFixture struct {
	platformAllocationFixture
	workload      *WorkloadInventoryService
	snapshots     *WorkloadSnapshotStore
	credential    TechnicalResourceCredential
	technical     model.TechnicalResource
	allocation    model.ResourceAllocation
	tenantAuth    *ManagementAuthorizationContext
	namespaceUID  string
	namespaceName string
	clusterKey    string
}

func newWorkloadInventoryFixture(t *testing.T) workloadInventoryFixture {
	t.Helper()
	fixture := newPlatformAllocationFixture(t)
	require.NoError(t, fixture.database.AutoMigrate(
		&model.UserTenantManagementMembership{},
		&model.WorkloadObservation{}, &model.WorkloadObservationSource{},
		&model.TenantResource{}, &model.TenantResourceSource{}, &model.TenantResourceReviewDecision{}, &model.TenantResourceTargetRevision{},
		&model.ConsumerRevision{},
	))
	var namespace model.NamespaceObservation
	require.NoError(t, fixture.database.First(&namespace, "id = ?", *fixture.scopeA.NamespaceObservationID).Error)
	var candidate model.SupplyCandidate
	require.NoError(t, fixture.database.Table("supply_candidate AS candidate").Select("candidate.*").
		Joins("JOIN platform_resource_source AS source ON source.supply_candidate_id = candidate.id").
		Where("source.platform_resource_id = ?", fixture.scopeA.PlatformResourceID).First(&candidate).Error)
	var snapshot map[string]any
	require.NoError(t, json.Unmarshal([]byte(candidate.ObservationSnapshot), &snapshot))
	snapshot["capabilities"] = []string{"workload_inventory_v1"}
	updatedSnapshot, err := json.Marshal(snapshot)
	require.NoError(t, err)
	require.NoError(t, fixture.database.Model(&model.SupplyCandidate{}).Where("id = ?", candidate.ID).Update("observation_snapshot", string(updatedSnapshot)).Error)
	var technical model.TechnicalResource
	require.NoError(t, fixture.database.First(&technical, "id = ?", candidate.TechnicalResourceID).Error)
	draft, err := fixture.service.CreateDraft(context.Background(), fixture.authorization, CreatePlatformAllocationInput{
		TenantID: fixture.tenantA.ID, Mode: model.ResourceAllocationAssigned, ScopeID: fixture.scopeA.ID,
		ValidFrom: fixture.now.Add(-time.Minute), ContractRef: "workload memory fixture",
	})
	require.NoError(t, err)
	active, err := fixture.service.Activate(context.Background(), fixture.authorization, PlatformAllocationActionInput{
		AllocationID: draft.ID, ExpectedRowVersion: draft.RowVersion, Reason: "activate workload fixture",
	})
	require.NoError(t, err)
	require.NoError(t, fixture.database.Create(&model.UserTenantManagementMembership{
		ID: uuid.NewString(), UserID: fixture.authorization.EffectiveUserID, TenantID: fixture.tenantA.ID,
		Role: model.TenantManagementRoleAdmin, Enabled: true, ValidFrom: fixture.now.Add(-time.Hour),
		PermissionRevision: 1, CreatedByUserID: fixture.authorization.EffectiveUserID, Reason: "test", RowVersion: 1,
	}).Error)
	tenantAuth, err := ResolveManagementContext(fixture.database, fixture.authorization.EffectiveUserID, model.ManagementScopeTenant, fixture.tenantA.ID, fixture.now, false)
	require.NoError(t, err)
	snapshots := NewWorkloadSnapshotStore()
	workload := NewWorkloadInventoryService(fixture.database, snapshots)
	workload.now = func() time.Time { return fixture.now }
	return workloadInventoryFixture{
		platformAllocationFixture: fixture, workload: workload, snapshots: snapshots, technical: technical,
		credential: TechnicalResourceCredential{SourceType: model.TechnicalResourceBindingLegacyNode, SourceID: "1001", CredentialRevision: technical.CredentialRevision},
		allocation: *active, tenantAuth: tenantAuth, namespaceUID: namespace.NamespaceUID, namespaceName: namespace.Name, clusterKey: candidate.StableKey,
	}
}

func (f workloadInventoryFixture) servicePayload(t *testing.T, clusterIP string, ready bool) ([]byte, string) {
	t.Helper()
	payload := []byte(`{"service_ports":[{"service_uid":"service-api","service_name":"api","cluster_ip":"` + clusterIP + `","port_name":"https","port_number":443,"protocol":"TCP","ready":` + boolJSON(ready) + `,"labels_allowlist":{"signal.beagle.io/expose":"true","app.kubernetes.io/name":"api"}}]}`)
	canonical, err := canonicalizeWorkloadInventoryPayload(model.WorkloadObservationServicePort, payload)
	require.NoError(t, err)
	return canonical, sha256Hex(canonical)
}

func boolJSON(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func (f workloadInventoryFixture) input(payload []byte, hash, epoch string, sequence int64, snapshot string) ReceiveWorkloadInventoryBatchInput {
	return ReceiveWorkloadInventoryBatchInput{
		AuthenticatedSource: f.credential, SourceCredentialRevision: f.technical.CredentialRevision,
		SchemaVersion: 1, SourceEpoch: epoch, Sequence: sequence, SnapshotID: snapshot, BatchIndex: 0, BatchCount: 1,
		ClusterIdentityDigest: f.clusterKey, NamespaceUID: f.namespaceUID, NamespaceName: f.namespaceName,
		Kind: model.WorkloadObservationServicePort, ObservedAt: f.now, PayloadHash: hash, Payload: payload,
	}
}

func TestWorkloadInventoryRefreshStaysInMemory(t *testing.T) {
	fixture := newWorkloadInventoryFixture(t)
	payload, hash := fixture.servicePayload(t, "10.96.0.10", true)
	ack, err := fixture.workload.ReceiveBatch(context.Background(), fixture.input(payload, hash, "epoch-a", 1, "snapshot-a"))
	require.NoError(t, err)
	require.True(t, ack.Committed)
	require.False(t, ack.Replayed)

	replay, err := fixture.workload.ReceiveBatch(context.Background(), fixture.input(payload, hash, "epoch-a", 1, "snapshot-retry"))
	require.NoError(t, err)
	require.True(t, replay.Replayed)
	_, err = fixture.workload.ReceiveBatch(context.Background(), fixture.input(payload, hash, "epoch-a", 3, "snapshot-gap"))
	require.ErrorIs(t, err, ErrWorkloadSequenceGap)

	for _, table := range []any{&model.WorkloadObservation{}, &model.WorkloadObservationSource{}, &model.TenantResource{}, &model.OutboxEvent{}} {
		var count int64
		require.NoError(t, fixture.database.Model(table).Count(&count).Error)
		require.Zero(t, count)
	}
}

func TestWorkloadSnapshotStoreAcceptsAgentSequenceAfterServerRestart(t *testing.T) {
	store := NewWorkloadSnapshotStore()
	snapshot := workloadSnapshot{
		SourceTechnicalResourceID: "agent-a", NamespaceScopeID: "scope-a", Kind: model.WorkloadObservationServicePort,
		SourceEpoch: "epoch-a", Sequence: 42, SnapshotID: "snapshot-a", ReceivedAt: time.Now().UTC(),
		LeaseExpiresAt: time.Now().UTC().Add(time.Minute),
	}
	replayed, err := store.replace(snapshot, strings.Repeat("a", 64))
	require.NoError(t, err)
	require.False(t, replayed)
	require.Len(t, store.current(time.Now().UTC()), 1)

	snapshot.SourceEpoch, snapshot.Sequence = "epoch-b", 1
	_, err = store.replace(snapshot, strings.Repeat("b", 64))
	require.NoError(t, err)
	snapshot.SourceEpoch, snapshot.Sequence = "epoch-a", 43
	_, err = store.replace(snapshot, strings.Repeat("c", 64))
	require.ErrorIs(t, err, ErrWorkloadSourceEpochStale)
}

func TestWorkloadInventoryBuildsTenantCandidatesFromMemory(t *testing.T) {
	fixture := newWorkloadInventoryFixture(t)
	payload, hash := fixture.servicePayload(t, "10.96.0.10", true)
	_, err := fixture.workload.ReceiveBatch(context.Background(), fixture.input(payload, hash, "epoch-a", 1, "snapshot-a"))
	require.NoError(t, err)
	resources := NewTenantResourceService(fixture.database, fixture.snapshots)
	resources.now = func() time.Time { return fixture.now }
	result, err := resources.List(context.Background(), fixture.tenantAuth, fixture.tenantA.ID, TenantResourceListInput{
		Type: string(model.TenantResourceContainerService), Candidates: true, Limit: 100,
	})
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	require.Equal(t, "api:443", result.Items[0].DisplayName)
	require.Equal(t, fixture.namespaceName, result.Items[0].NamespaceName)
	require.Equal(t, 443, result.Items[0].PortNumber)
}

func TestPublishingCandidatePersistsStaticResourceOnlyOnce(t *testing.T) {
	fixture := newWorkloadInventoryFixture(t)
	payload, hash := fixture.servicePayload(t, "10.96.0.10", true)
	_, err := fixture.workload.ReceiveBatch(context.Background(), fixture.input(payload, hash, "epoch-a", 1, "snapshot-a"))
	require.NoError(t, err)
	resources := NewTenantResourceService(fixture.database, fixture.snapshots)
	resources.now = func() time.Time { return fixture.now }
	list, err := resources.List(context.Background(), fixture.tenantAuth, fixture.tenantA.ID, TenantResourceListInput{Candidates: true, Limit: 100})
	require.NoError(t, err)
	require.Len(t, list.Items, 1)
	candidate := list.Items[0]
	published, err := resources.Review(context.Background(), fixture.tenantAuth, ReviewTenantResourceInput{
		TenantID: fixture.tenantA.ID, ResourceID: candidate.ResourceID, ExpectedRowVersion: candidate.RowVersion,
		ObservationRevision: candidate.ObservationRevision, Reason: "publish static service", Publish: true,
	})
	require.NoError(t, err)
	require.Equal(t, model.TenantResourceVisible, published.VisibilityState)

	changed, changedHash := fixture.servicePayload(t, "10.96.0.11", true)
	fixture.now = fixture.now.Add(time.Minute)
	fixture.workload.now = func() time.Time { return fixture.now }
	_, err = fixture.workload.ReceiveBatch(context.Background(), fixture.input(changed, changedHash, "epoch-a", 2, "snapshot-b"))
	require.NoError(t, err)
	for _, table := range []any{&model.WorkloadObservation{}, &model.WorkloadObservationSource{}, &model.TenantResource{}, &model.TenantResourceSource{}, &model.TenantResourceTargetRevision{}} {
		var count int64
		require.NoError(t, fixture.database.Model(table).Count(&count).Error)
		require.EqualValues(t, 1, count)
	}
	var target model.TenantResourceTargetRevision
	require.NoError(t, fixture.database.First(&target).Error)
	require.Contains(t, target.TargetSnapshot, `"cluster_ip":"10.96.0.10"`)
	restarted := NewTenantResourceService(fixture.database, NewWorkloadSnapshotStore())
	restarted.now = func() time.Time { return fixture.now }
	view, err := restarted.Get(context.Background(), fixture.tenantAuth, fixture.tenantA.ID, published.ID, false)
	require.NoError(t, err)
	require.Equal(t, published.ID, view.ResourceID)
	_, err = loadTenantResourceChain(fixture.database, fixture.tenantA.ID, published.ID, fixture.now, true)
	require.NoError(t, err)
}
