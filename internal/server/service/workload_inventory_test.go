package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

type workloadInventoryFixture struct {
	platformAllocationFixture
	workload      *WorkloadInventoryService
	projection    *TenantResourceProjectionService
	credential    TechnicalResourceCredential
	technical     model.TechnicalResource
	allocation    model.ResourceAllocation
	namespaceUID  string
	namespaceName string
	clusterKey    string
}

func newWorkloadInventoryFixture(t *testing.T) workloadInventoryFixture {
	t.Helper()
	fixture := newPlatformAllocationFixture(t)
	require.NoError(t, fixture.database.AutoMigrate(
		&model.WorkloadInventoryReceipt{}, &model.WorkloadInventoryBatch{}, &model.WorkloadObservation{}, &model.WorkloadObservationSource{},
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
	require.NoError(t, fixture.database.Model(&model.SupplyCandidate{}).Where("id = ?", candidate.ID).
		Update("observation_snapshot", string(updatedSnapshot)).Error)

	var technical model.TechnicalResource
	require.NoError(t, fixture.database.First(&technical, "id = ?", candidate.TechnicalResourceID).Error)
	draft, err := fixture.service.CreateDraft(context.Background(), fixture.authorization, CreatePlatformAllocationInput{
		TenantID: fixture.tenantA.ID, Mode: model.ResourceAllocationAssigned, ScopeID: fixture.scopeA.ID,
		ValidFrom: fixture.now.Add(-time.Minute), ContractRef: "workload projection fixture",
	})
	require.NoError(t, err)
	active, err := fixture.service.Activate(context.Background(), fixture.authorization, PlatformAllocationActionInput{
		AllocationID: draft.ID, ExpectedRowVersion: draft.RowVersion, Reason: "activate workload fixture",
	})
	require.NoError(t, err)

	workload := NewWorkloadInventoryService(fixture.database)
	workload.now = func() time.Time { return fixture.now }
	projection := NewTenantResourceProjectionService(fixture.database)
	projection.now = func() time.Time { return fixture.now }
	return workloadInventoryFixture{
		platformAllocationFixture: fixture, workload: workload, projection: projection, technical: technical,
		credential: TechnicalResourceCredential{
			SourceType: model.TechnicalResourceBindingLegacyNode, SourceID: "1001", CredentialRevision: technical.CredentialRevision,
		},
		allocation: *active, namespaceUID: namespace.NamespaceUID, namespaceName: namespace.Name, clusterKey: candidate.StableKey,
	}
}

func (f workloadInventoryFixture) servicePayload(t *testing.T, clusterIP, protocol string, ready bool) ([]byte, string) {
	t.Helper()
	payload := []byte(`{"service_ports":[{"service_uid":"service-api","service_name":"api","cluster_ip":"` + clusterIP + `","port_name":"https","port_number":443,"protocol":"` + protocol + `","ready":` + boolJSON(ready) + `,"labels_allowlist":{"signal.beagle.io/expose":"true","app.kubernetes.io/name":"api"}}]}`)
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

func (f workloadInventoryFixture) input(payload []byte, hash, epoch string, sequence int64, snapshot string, batchIndex, batchCount int) ReceiveWorkloadInventoryBatchInput {
	return ReceiveWorkloadInventoryBatchInput{
		AuthenticatedSource: f.credential, SourceCredentialRevision: f.technical.CredentialRevision,
		SchemaVersion: 1, SourceEpoch: epoch, Sequence: sequence, SnapshotID: snapshot,
		BatchIndex: batchIndex, BatchCount: batchCount, ClusterIdentityDigest: f.clusterKey,
		NamespaceUID: f.namespaceUID, NamespaceName: "workloads", Kind: model.WorkloadObservationServicePort,
		ObservedAt: f.now, PayloadHash: hash, Payload: payload,
	}
}

func TestWorkloadInventorySequenceReplayAndPendingProjection(t *testing.T) {
	fixture := newWorkloadInventoryFixture(t)
	payload, hash := fixture.servicePayload(t, "10.96.0.10", "TCP", true)

	ack, err := fixture.workload.ReceiveBatch(context.Background(), fixture.input(payload, hash, "epoch-a", 1, "snapshot-a", 0, 1))
	require.NoError(t, err)
	require.True(t, ack.Committed)
	require.Equal(t, WorkloadInventoryResultAccepted, ack.ResultCode)

	replay, err := fixture.workload.ReceiveBatch(context.Background(), fixture.input(payload, hash, "epoch-a", 1, "snapshot-a", 0, 1))
	require.NoError(t, err)
	require.True(t, replay.Replayed)
	require.Equal(t, WorkloadInventoryResultReplayed, replay.ResultCode)

	_, err = fixture.workload.ReceiveBatch(context.Background(), fixture.input(payload, hash, "epoch-a", 3, "snapshot-gap", 0, 1))
	require.ErrorIs(t, err, ErrWorkloadSequenceGap)

	processed, err := fixture.projection.Drain(context.Background(), 10)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	var resource model.TenantResource
	require.NoError(t, fixture.database.Where("tenant_id = ? AND type = ?", fixture.tenantA.ID, model.TenantResourceContainerService).First(&resource).Error)
	require.Equal(t, model.TenantResourcePending, resource.VisibilityState)
	require.Equal(t, model.TenantResourceAvailable, resource.AvailabilityState)
	require.Equal(t, fixture.allocation.ID, resource.EntitlementLineageID)

	var source model.TenantResourceSource
	require.NoError(t, fixture.database.First(&source, "tenant_resource_id = ?", resource.ID).Error)
	require.Equal(t, fixture.allocation.Items[0].ID, source.AllocationItemID)
	var target model.TenantResourceTargetRevision
	require.NoError(t, fixture.database.First(&target, "tenant_resource_source_id = ?", source.ID).Error)
	require.Equal(t, fixture.technical.ID, target.SourceTechnicalResourceID)
	require.Contains(t, target.TargetSnapshot, `"port_number":443`)
	require.Contains(t, target.TargetSnapshot, `"namespace_uid":"`+fixture.namespaceUID+`"`)
	require.Contains(t, target.TargetSnapshot, `"namespace_name":"`+fixture.namespaceName+`"`)

	var otherTenantCount int64
	require.NoError(t, fixture.database.Model(&model.TenantResource{}).Where("tenant_id = ?", fixture.tenantB.ID).Count(&otherTenantCount).Error)
	require.Zero(t, otherTenantCount)
}

func TestWorkloadInventoryRejectsForbiddenUnsupportedAndIncompletePayloads(t *testing.T) {
	fixture := newWorkloadInventoryFixture(t)
	udp := []byte(`{"service_ports":[{"service_uid":"service-a","service_name":"api","cluster_ip":"10.0.0.1","port_name":"dns","port_number":53,"protocol":"UDP","ready":true,"labels_allowlist":{"signal.beagle.io/expose":"true"}}]}`)
	_, err := canonicalizeWorkloadInventoryPayload(model.WorkloadObservationServicePort, udp)
	require.ErrorIs(t, err, ErrWorkloadProtocolUnsupported)

	forbidden := []byte(`{"service_ports":[{"service_uid":"service-a","service_name":"api","cluster_ip":"10.0.0.1","port_name":"https","port_number":443,"protocol":"TCP","ready":true,"labels_allowlist":{"tenant_id":"tenant-b","token":"secret"}}]}`)
	_, err = canonicalizeWorkloadInventoryPayload(model.WorkloadObservationServicePort, forbidden)
	require.ErrorIs(t, err, ErrWorkloadPayloadForbidden)

	payload, hash := fixture.servicePayload(t, "10.96.0.10", "TCP", true)
	_, err = fixture.workload.ReceiveBatch(context.Background(), fixture.input(payload, strings.Repeat("0", 64), "epoch-a", 1, "snapshot-hash", 0, 1))
	require.ErrorIs(t, err, ErrWorkloadPayloadHashMismatch)

	staged, err := fixture.workload.ReceiveBatch(context.Background(), fixture.input(payload, hash, "epoch-a", 1, "snapshot-incomplete", 0, 2))
	require.NoError(t, err)
	require.False(t, staged.Committed)
	purged, err := fixture.workload.PurgeExpiredPayloads(context.Background(), fixture.now.Add(workloadInventoryPayloadRetention+time.Second))
	require.NoError(t, err)
	require.Equal(t, int64(1), purged)
	_, err = fixture.workload.ReceiveBatch(context.Background(), fixture.input(payload, hash, "epoch-a", 2, "snapshot-incomplete", 1, 2))
	require.ErrorIs(t, err, ErrWorkloadSnapshotIncomplete)
}

func TestWorkloadInventoryMultiSourceLeaseAndNewEpoch(t *testing.T) {
	fixture := newWorkloadInventoryFixture(t)
	payload, hash := fixture.servicePayload(t, "10.96.0.10", "TCP", true)
	_, err := fixture.workload.ReceiveBatch(context.Background(), fixture.input(payload, hash, "epoch-agent", 1, "snapshot-agent", 0, 1))
	require.NoError(t, err)

	endpoint, err := fixture.providerSupplyFixture.service.CreateTechnicalResource(context.Background(), fixture.providerSupplyFixture.authorization, CreateTechnicalResourceInput{
		Type: model.TechnicalResourceEndpoint, StableKey: "endpoint-workload", ParentID: fixture.technical.ID, CredentialRevision: 1,
	})
	require.NoError(t, err)
	bound, err := fixture.providerSupplyFixture.service.BindTechnicalResource(context.Background(), fixture.providerSupplyFixture.authorization, BindTechnicalResourceInput{
		TechnicalResourceID: endpoint.ID, SourceType: model.TechnicalResourceBindingLegacyEndpoint, SourceID: "legacy-endpoint-a",
		ExpectedResourceVersion: endpoint.RowVersion, Reason: "workload endpoint binding",
	})
	require.NoError(t, err)
	require.NoError(t, fixture.database.Model(&model.Endpoint{}).Where("id = ?", "legacy-endpoint-a").Update("k8sservice_enabled", true).Error)

	secondNow := fixture.now.Add(30 * time.Second)
	fixture.workload.now = func() time.Time { return secondNow }
	endpointInput := fixture.input(payload, hash, "epoch-endpoint", 1, "snapshot-endpoint", 0, 1)
	endpointInput.SourceTechnicalResourceID = bound.TechnicalResource.ID
	endpointInput.SourceCredentialRevision = bound.TechnicalResource.CredentialRevision
	_, err = fixture.workload.ReceiveBatch(context.Background(), endpointInput)
	require.NoError(t, err)

	var observation model.WorkloadObservation
	require.NoError(t, fixture.database.First(&observation).Error)
	var sourceCount int64
	require.NoError(t, fixture.database.Model(&model.WorkloadObservationSource{}).Where("workload_observation_id = ?", observation.ID).Count(&sourceCount).Error)
	require.Equal(t, int64(2), sourceCount)
	processed, err := fixture.projection.Drain(context.Background(), 10)
	require.NoError(t, err)
	require.Greater(t, processed, 0)
	var resource model.TenantResource
	require.NoError(t, fixture.database.Where("tenant_id = ? AND type = ?", fixture.tenantA.ID, model.TenantResourceContainerService).First(&resource).Error)
	require.Equal(t, model.TenantResourceAvailable, resource.AvailabilityState)
	observedRevision, resourceRevision := observation.ObservedRevision, resource.Revision

	reconciler := NewWorkloadReconciliationService(fixture.database)
	reconcileAt := fixture.now.Add(2*time.Minute + 15*time.Second)
	result, err := reconciler.Reconcile(context.Background(), reconcileAt)
	require.NoError(t, err)
	require.Equal(t, int64(1), result.StaleSources)
	fixture.projection.now = func() time.Time { return reconcileAt }
	processed, err = fixture.projection.Drain(context.Background(), 10)
	require.NoError(t, err)
	require.Greater(t, processed, 0)
	require.NoError(t, fixture.database.First(&observation, "id = ?", observation.ID).Error)
	require.NotEqual(t, model.WorkloadObservationStale, observation.State)
	require.True(t, observation.Ready)
	require.Equal(t, observedRevision, observation.ObservedRevision)
	require.NoError(t, fixture.database.First(&resource, "id = ?", resource.ID).Error)
	require.Equal(t, model.TenantResourceDegraded, resource.AvailabilityState)
	require.Greater(t, resource.Revision, resourceRevision)

	restartedAt := reconcileAt.Add(15 * time.Second)
	fixture.workload.now = func() time.Time { return restartedAt }
	newEpoch := endpointInput
	newEpoch.SourceEpoch, newEpoch.Sequence, newEpoch.SnapshotID = "epoch-endpoint-restarted", 1, "snapshot-endpoint-restarted"
	newEpoch.ObservedAt = restartedAt
	_, err = fixture.workload.ReceiveBatch(context.Background(), newEpoch)
	require.NoError(t, err)
	oldEpoch := endpointInput
	oldEpoch.Sequence, oldEpoch.SnapshotID = 2, "snapshot-old-epoch"
	_, err = fixture.workload.ReceiveBatch(context.Background(), oldEpoch)
	require.ErrorIs(t, err, ErrWorkloadSourceEpochStale)
}

func TestWorkloadInventoryReconciliationIgnoresSourceWithoutCurrentCapability(t *testing.T) {
	fixture := newWorkloadInventoryFixture(t)
	payload, hash := fixture.servicePayload(t, "10.96.0.10", "TCP", true)
	_, err := fixture.workload.ReceiveBatch(context.Background(), fixture.input(payload, hash, "epoch-agent", 1, "snapshot-agent", 0, 1))
	require.NoError(t, err)

	endpoint, err := fixture.providerSupplyFixture.service.CreateTechnicalResource(context.Background(), fixture.providerSupplyFixture.authorization, CreateTechnicalResourceInput{
		Type: model.TechnicalResourceEndpoint, StableKey: "endpoint-capability", ParentID: fixture.technical.ID, CredentialRevision: 1,
	})
	require.NoError(t, err)
	bound, err := fixture.providerSupplyFixture.service.BindTechnicalResource(context.Background(), fixture.providerSupplyFixture.authorization, BindTechnicalResourceInput{
		TechnicalResourceID: endpoint.ID, SourceType: model.TechnicalResourceBindingLegacyEndpoint, SourceID: "legacy-endpoint-a",
		ExpectedResourceVersion: endpoint.RowVersion, Reason: "workload endpoint capability binding",
	})
	require.NoError(t, err)
	require.NoError(t, fixture.database.Model(&model.Endpoint{}).Where("id = ?", "legacy-endpoint-a").Update("k8sservice_enabled", true).Error)

	endpointAt := fixture.now.Add(30 * time.Second)
	fixture.workload.now = func() time.Time { return endpointAt }
	fixture.projection.now = func() time.Time { return endpointAt }
	endpointInput := fixture.input(payload, hash, "epoch-endpoint", 1, "snapshot-endpoint", 0, 1)
	endpointInput.SourceTechnicalResourceID = bound.TechnicalResource.ID
	endpointInput.SourceCredentialRevision = bound.TechnicalResource.CredentialRevision
	endpointInput.ObservedAt = endpointAt
	_, err = fixture.workload.ReceiveBatch(context.Background(), endpointInput)
	require.NoError(t, err)
	processed, err := fixture.projection.Drain(context.Background(), 10)
	require.NoError(t, err)
	require.Greater(t, processed, 0)

	var observation model.WorkloadObservation
	require.NoError(t, fixture.database.First(&observation).Error)
	observedRevision := observation.ObservedRevision
	var resource model.TenantResource
	require.NoError(t, fixture.database.Where("tenant_id = ? AND type = ?", fixture.tenantA.ID, model.TenantResourceContainerService).First(&resource).Error)
	var source model.TenantResourceSource
	require.NoError(t, fixture.database.First(&source, "tenant_resource_id = ?", resource.ID).Error)
	var latest model.TenantResourceTargetRevision
	require.NoError(t, fixture.database.Where("tenant_resource_source_id = ?", source.ID).Order("revision DESC").First(&latest).Error)
	require.Equal(t, bound.TechnicalResource.ID, latest.SourceTechnicalResourceID)

	require.NoError(t, fixture.database.Model(&model.Endpoint{}).Where("id = ?", "legacy-endpoint-a").Update("k8sservice_enabled", false).Error)
	reconcileAt := endpointAt.Add(time.Second)
	reconciler := NewWorkloadReconciliationService(fixture.database)
	result, err := reconciler.Reconcile(context.Background(), reconcileAt)
	require.NoError(t, err)
	require.Equal(t, int64(1), result.UpdatedObservations)
	fixture.projection.now = func() time.Time { return reconcileAt }
	processed, err = fixture.projection.Drain(context.Background(), 10)
	require.NoError(t, err)
	require.Greater(t, processed, 0)

	require.NoError(t, fixture.database.First(&observation, "id = ?", observation.ID).Error)
	require.Equal(t, observedRevision, observation.ObservedRevision)
	require.Equal(t, model.WorkloadObservationEligible, observation.State)
	require.NoError(t, fixture.database.First(&resource, "id = ?", resource.ID).Error)
	require.Equal(t, model.TenantResourceDegraded, resource.AvailabilityState)
	latest = model.TenantResourceTargetRevision{}
	require.NoError(t, fixture.database.Where("tenant_resource_source_id = ?", source.ID).Order("revision DESC").First(&latest).Error)
	require.Equal(t, fixture.technical.ID, latest.SourceTechnicalResourceID)
}

func TestWorkloadInventoryReadinessAndLeaseDoNotAdvanceObservedRevision(t *testing.T) {
	fixture := newWorkloadInventoryFixture(t)
	readyPayload, readyHash := fixture.servicePayload(t, "10.96.0.10", "TCP", true)
	_, err := fixture.workload.ReceiveBatch(context.Background(), fixture.input(readyPayload, readyHash, "epoch-ready", 1, "snapshot-ready", 0, 1))
	require.NoError(t, err)
	processed, err := fixture.projection.Drain(context.Background(), 10)
	require.NoError(t, err)
	require.Equal(t, 1, processed)

	var observation model.WorkloadObservation
	require.NoError(t, fixture.database.First(&observation).Error)
	observedRevision := observation.ObservedRevision
	var resource model.TenantResource
	require.NoError(t, fixture.database.Where("tenant_id = ? AND type = ?", fixture.tenantA.ID, model.TenantResourceContainerService).First(&resource).Error)
	require.Equal(t, model.TenantResourceAvailable, resource.AvailabilityState)
	availableRevision := resource.Revision

	notReadyAt := fixture.now.Add(30 * time.Second)
	fixture.workload.now = func() time.Time { return notReadyAt }
	fixture.projection.now = func() time.Time { return notReadyAt }
	notReadyPayload, notReadyHash := fixture.servicePayload(t, "10.96.0.10", "TCP", false)
	notReady := fixture.input(notReadyPayload, notReadyHash, "epoch-ready", 2, "snapshot-not-ready", 0, 1)
	notReady.ObservedAt = notReadyAt
	_, err = fixture.workload.ReceiveBatch(context.Background(), notReady)
	require.NoError(t, err)
	processed, err = fixture.projection.Drain(context.Background(), 10)
	require.NoError(t, err)
	require.Equal(t, 1, processed)

	require.NoError(t, fixture.database.First(&observation, "id = ?", observation.ID).Error)
	require.Equal(t, observedRevision, observation.ObservedRevision)
	require.False(t, observation.Ready)
	require.NoError(t, fixture.database.First(&resource, "id = ?", resource.ID).Error)
	require.Equal(t, model.TenantResourceUnavailable, resource.AvailabilityState)
	require.Greater(t, resource.Revision, availableRevision)
	degradedRevision := resource.Revision

	readyAgainAt := fixture.now.Add(time.Minute)
	fixture.workload.now = func() time.Time { return readyAgainAt }
	fixture.projection.now = func() time.Time { return readyAgainAt }
	readyAgain := fixture.input(readyPayload, readyHash, "epoch-ready", 3, "snapshot-ready-again", 0, 1)
	readyAgain.ObservedAt = readyAgainAt
	_, err = fixture.workload.ReceiveBatch(context.Background(), readyAgain)
	require.NoError(t, err)
	processed, err = fixture.projection.Drain(context.Background(), 10)
	require.NoError(t, err)
	require.Equal(t, 1, processed)

	require.NoError(t, fixture.database.First(&observation, "id = ?", observation.ID).Error)
	require.Equal(t, observedRevision, observation.ObservedRevision)
	require.True(t, observation.Ready)
	require.NoError(t, fixture.database.First(&resource, "id = ?", resource.ID).Error)
	require.Equal(t, model.TenantResourceAvailable, resource.AvailabilityState)
	require.Greater(t, resource.Revision, degradedRevision)
	readyAgainRevision := resource.Revision

	expiredAt := readyAgainAt.Add(workloadInventoryLeaseDuration + time.Second)
	reconciler := NewWorkloadReconciliationService(fixture.database)
	result, err := reconciler.Reconcile(context.Background(), expiredAt)
	require.NoError(t, err)
	require.Equal(t, int64(1), result.StaleSources)
	fixture.projection.now = func() time.Time { return expiredAt }
	processed, err = fixture.projection.Drain(context.Background(), 10)
	require.NoError(t, err)
	require.Equal(t, 1, processed)

	require.NoError(t, fixture.database.First(&observation, "id = ?", observation.ID).Error)
	require.Equal(t, observedRevision, observation.ObservedRevision)
	require.Equal(t, model.WorkloadObservationStale, observation.State)
	require.NoError(t, fixture.database.First(&resource, "id = ?", resource.ID).Error)
	require.Equal(t, model.TenantResourceUnavailable, resource.AvailabilityState)
	require.Greater(t, resource.Revision, readyAgainRevision)
}

func TestWorkloadContainerPodRebuildKeepsStableTenantResource(t *testing.T) {
	fixture := newWorkloadInventoryFixture(t)
	containerPayload := func(podUID, podName string) ([]byte, string) {
		raw := []byte(`{"containers":[{"workload_uid":"deployment-api","workload_kind":"Deployment","workload_name":"api","pod_uid":"` + podUID + `","pod_name":"` + podName + `","container_name":"app","ready":true,"labels_allowlist":{"signal.beagle.io/expose":"true"},"ssh_users":["code"]}]}`)
		canonical, err := canonicalizeWorkloadInventoryPayload(model.WorkloadObservationContainer, raw)
		require.NoError(t, err)
		return canonical, sha256Hex(canonical)
	}
	input := func(payload []byte, hash string, sequence int64, snapshot string) ReceiveWorkloadInventoryBatchInput {
		result := fixture.input(payload, hash, "epoch-container", sequence, snapshot, 0, 1)
		result.Kind = model.WorkloadObservationContainer
		return result
	}

	firstPayload, firstHash := containerPayload("pod-a", "api-a")
	_, err := fixture.workload.ReceiveBatch(context.Background(), input(firstPayload, firstHash, 1, "container-a"))
	require.NoError(t, err)
	processed, err := fixture.projection.Drain(context.Background(), 10)
	require.NoError(t, err)
	require.Equal(t, 1, processed)

	var resource model.TenantResource
	require.NoError(t, fixture.database.Where("tenant_id = ? AND type = ?", fixture.tenantA.ID, model.TenantResourceContainerSSH).First(&resource).Error)
	var source model.TenantResourceSource
	require.NoError(t, fixture.database.First(&source, "tenant_resource_id = ?", resource.ID).Error)

	secondNow := fixture.now.Add(time.Minute)
	fixture.workload.now = func() time.Time { return secondNow }
	fixture.projection.now = func() time.Time { return secondNow }
	secondPayload, secondHash := containerPayload("pod-b", "api-b")
	second := input(secondPayload, secondHash, 2, "container-b")
	second.ObservedAt = secondNow
	_, err = fixture.workload.ReceiveBatch(context.Background(), second)
	require.NoError(t, err)
	processed, err = fixture.projection.Drain(context.Background(), 10)
	require.NoError(t, err)
	require.Equal(t, 1, processed)

	var resourceCount, targetCount int64
	require.NoError(t, fixture.database.Model(&model.TenantResource{}).Where("id = ?", resource.ID).Count(&resourceCount).Error)
	require.Equal(t, int64(1), resourceCount)
	require.NoError(t, fixture.database.Model(&model.TenantResourceTargetRevision{}).Where("tenant_resource_source_id = ?", source.ID).Count(&targetCount).Error)
	require.Equal(t, int64(2), targetCount)
	var latest model.TenantResourceTargetRevision
	require.NoError(t, fixture.database.Where("tenant_resource_source_id = ?", source.ID).Order("revision DESC").First(&latest).Error)
	require.Contains(t, latest.TargetSnapshot, `"pod_uid":"pod-b"`)
}
