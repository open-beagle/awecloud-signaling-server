package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func inventoryPayload(t *testing.T, value string) ([]byte, string) {
	t.Helper()
	payload := []byte(value)
	canonical, err := canonicalizeSupplyInventoryPayload(payload)
	require.NoError(t, err)
	return payload, sha256Hex(canonical)
}

func inventoryInput(credential TechnicalResourceCredential, payload []byte, payloadHash, epoch string, sequence int64, snapshot string, batchIndex, batchCount int) ReceiveSupplyInventoryBatchInput {
	return ReceiveSupplyInventoryBatchInput{
		AuthenticatedSource: credential, SchemaVersion: 1, SourceEpoch: epoch, Sequence: sequence,
		SnapshotID: snapshot, BatchIndex: batchIndex, BatchCount: batchCount, PayloadHash: payloadHash, Payload: payload,
	}
}

func TestSupplyInventorySequenceReplayConflictAndSnapshotCommit(t *testing.T) {
	fixture := newProviderSupplyFixture(t)
	agent := fixture.createBoundAgent(t, "agent-inventory", 1001)
	credential := TechnicalResourceCredential{SourceType: model.TechnicalResourceBindingLegacyNode, SourceID: "1001", CredentialRevision: 1}
	payloadA, hashA := inventoryPayload(t, `{"kubernetes_clusters":[{"cluster_uid":"cluster-a","namespaces":[]}]}`)
	payloadB, hashB := inventoryPayload(t, `{"kubernetes_clusters":[{"cluster_uid":"cluster-a","namespaces":[{"uid":"namespace-a"}]}]}`)
	ctx := context.Background()

	_, err := fixture.service.ReceiveSupplyInventoryBatch(ctx, inventoryInput(credential, payloadA, hashA, "epoch-a", 2, "snapshot-a", 0, 2))
	require.ErrorIs(t, err, ErrSourceEpochStale)

	first, err := fixture.service.ReceiveSupplyInventoryBatch(ctx, inventoryInput(credential, payloadA, hashA, "epoch-a", 1, "snapshot-a", 0, 2))
	require.NoError(t, err)
	require.Equal(t, SupplyInventoryResultBatchStaged, first.ResultCode)
	require.False(t, first.SnapshotCommitted)
	require.Equal(t, agent.ID, first.TechnicalResourceID)

	replay, err := fixture.service.ReceiveSupplyInventoryBatch(ctx, inventoryInput(credential, payloadA, hashA, "epoch-a", 1, "snapshot-a", 0, 2))
	require.NoError(t, err)
	require.True(t, replay.Replay)
	require.False(t, replay.SnapshotCommitted)
	changedMetadata := inventoryInput(credential, payloadA, hashA, "epoch-a", 1, "snapshot-other", 0, 2)
	_, err = fixture.service.ReceiveSupplyInventoryBatch(ctx, changedMetadata)
	require.ErrorIs(t, err, ErrSourceSequenceConflict)

	_, err = fixture.service.ReceiveSupplyInventoryBatch(ctx, inventoryInput(credential, payloadB, hashB, "epoch-a", 1, "snapshot-a", 0, 2))
	require.ErrorIs(t, err, ErrSourceSequenceConflict)
	_, err = fixture.service.ReceiveSupplyInventoryBatch(ctx, inventoryInput(credential, payloadB, hashB, "epoch-a", 3, "snapshot-a", 1, 2))
	require.ErrorIs(t, err, ErrSourceSequenceOutOfOrder)

	committed, err := fixture.service.ReceiveSupplyInventoryBatch(ctx, inventoryInput(credential, payloadB, hashB, "epoch-a", 2, "snapshot-a", 1, 2))
	require.NoError(t, err)
	require.True(t, committed.SnapshotCommitted)
	require.Equal(t, SupplyInventoryResultSnapshotCommitted, committed.ResultCode)
	var committedCount int64
	require.NoError(t, fixture.database.Model(&model.SupplyInventoryReceipt{}).
		Where("technical_resource_id = ? AND snapshot_id = ? AND status = ?", agent.ID, "snapshot-a", model.SupplyInventoryReceiptCommitted).
		Count(&committedCount).Error)
	require.Equal(t, int64(2), committedCount)

	committedReplay, err := fixture.service.ReceiveSupplyInventoryBatch(ctx, inventoryInput(credential, payloadB, hashB, "epoch-a", 2, "snapshot-a", 1, 2))
	require.NoError(t, err)
	require.True(t, committedReplay.Replay)
	require.True(t, committedReplay.SnapshotCommitted)

	newEpoch, err := fixture.service.ReceiveSupplyInventoryBatch(ctx, inventoryInput(credential, payloadA, hashA, "epoch-b", 1, "snapshot-b", 0, 1))
	require.NoError(t, err)
	require.True(t, newEpoch.SnapshotCommitted)
	_, err = fixture.service.ReceiveSupplyInventoryBatch(ctx, inventoryInput(credential, payloadA, hashA, "epoch-a", 3, "snapshot-old", 0, 1))
	require.ErrorIs(t, err, ErrSourceEpochStale)

	var persisted model.TechnicalResource
	require.NoError(t, fixture.database.First(&persisted, "id = ?", agent.ID).Error)
	require.Equal(t, "epoch-b", persisted.SourceEpoch)
	require.Equal(t, int64(1), persisted.LastSequence)
	require.Zero(t, persisted.ObservedRevision)
}

func TestSupplyInventoryRejectsMetadataPayloadAndDisabledSource(t *testing.T) {
	fixture := newProviderSupplyFixture(t)
	agent := fixture.createBoundAgent(t, "agent-inventory-validation", 1001)
	credential := TechnicalResourceCredential{SourceType: model.TechnicalResourceBindingLegacyNode, SourceID: "1001", CredentialRevision: 1}
	payload, hash := inventoryPayload(t, `{"kubernetes_clusters":[]}`)
	ctx := context.Background()

	badHash := inventoryInput(credential, payload, hash, "epoch-a", 1, "snapshot-a", 0, 1)
	badHash.PayloadHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	_, err := fixture.service.ReceiveSupplyInventoryBatch(ctx, badHash)
	require.ErrorIs(t, err, ErrSupplyPayloadHashMismatch)

	unknownField := inventoryInput(credential, []byte(`{"kubernetes_clusters":[],"provider_id":"provider-b"}`), hash, "epoch-a", 1, "snapshot-a", 0, 1)
	_, err = fixture.service.ReceiveSupplyInventoryBatch(ctx, unknownField)
	require.ErrorIs(t, err, ErrProviderSupplyInvalidInput)
	nestedAuthorityPayload := []byte(`{"kubernetes_clusters":[{"cluster_uid":"cluster-a","tenant_id":"tenant-a"}]}`)
	nestedAuthorityInput := inventoryInput(credential, nestedAuthorityPayload, hash, "epoch-a", 1, "snapshot-a", 0, 1)
	_, err = fixture.service.ReceiveSupplyInventoryBatch(ctx, nestedAuthorityInput)
	require.ErrorIs(t, err, ErrProviderSupplyInvalidInput)

	sensitivePayload := []byte(`{"kubernetes_clusters":[{"token":"fixture-value"}]}`)
	sensitiveInput := inventoryInput(credential, sensitivePayload, hash, "epoch-a", 1, "snapshot-a", 0, 1)
	_, err = fixture.service.ReceiveSupplyInventoryBatch(ctx, sensitiveInput)
	require.True(t, errors.Is(err, ErrSensitiveJSONField))

	first := inventoryInput(credential, payload, hash, "epoch-a", 1, "snapshot-a", 0, 2)
	_, err = fixture.service.ReceiveSupplyInventoryBatch(ctx, first)
	require.NoError(t, err)
	metadataConflict := inventoryInput(credential, payload, hash, "epoch-a", 2, "snapshot-a", 1, 3)
	_, err = fixture.service.ReceiveSupplyInventoryBatch(ctx, metadataConflict)
	require.ErrorIs(t, err, ErrSnapshotMetadataConflict)
	var persisted model.TechnicalResource
	require.NoError(t, fixture.database.First(&persisted, "id = ?", agent.ID).Error)
	require.Equal(t, int64(1), persisted.LastSequence)

	disabled, err := fixture.service.SetTechnicalResourceLifecycle(ctx, fixture.authorization, SetTechnicalResourceLifecycleInput{
		TechnicalResourceID: agent.ID, TargetState: model.TechnicalResourceDisabled,
		ExpectedRowVersion: agent.RowVersion, Reason: "maintenance",
	})
	require.NoError(t, err)
	_, err = fixture.service.ReceiveSupplyInventoryBatch(ctx, inventoryInput(credential, payload, hash, "epoch-a", 2, "snapshot-a", 1, 2))
	require.ErrorIs(t, err, ErrTechnicalResourceDisabled)
	_, err = fixture.service.SetTechnicalResourceLifecycle(ctx, fixture.authorization, SetTechnicalResourceLifecycleInput{
		TechnicalResourceID: agent.ID, TargetState: model.TechnicalResourceRegistered,
		ExpectedRowVersion: disabled.RowVersion, Reason: "maintenance complete",
	})
	require.NoError(t, err)

	fixture.service.now = func() time.Time { return fixture.now.Add(25 * time.Hour) }
	purged, err := fixture.service.PurgeExpiredSupplyInventoryPayloads(ctx, fixture.now.Add(25*time.Hour))
	require.NoError(t, err)
	require.Equal(t, int64(1), purged)
	var receipt model.SupplyInventoryReceipt
	require.NoError(t, fixture.database.First(&receipt, "technical_resource_id = ? AND source_epoch = ? AND sequence = ?", agent.ID, "epoch-a", 1).Error)
	require.Empty(t, receipt.CanonicalPayload)
	require.Equal(t, model.SupplyInventoryReceiptRejected, receipt.Status)
	require.Equal(t, SupplyInventoryResultSnapshotIncomplete, receipt.ResultCode)
	_, err = fixture.service.ReceiveSupplyInventoryBatch(ctx, inventoryInput(credential, payload, hash, "epoch-a", 2, "snapshot-a", 1, 2))
	require.ErrorIs(t, err, ErrSnapshotMetadataConflict)
	require.NoError(t, fixture.database.First(&persisted, "id = ?", agent.ID).Error)
	require.Equal(t, int64(1), persisted.LastSequence)
}

func TestSupplyInventoryEndpointForwardingIsBoundToParentAgent(t *testing.T) {
	fixture := newProviderSupplyFixture(t)
	agentA := fixture.createBoundAgent(t, "agent-forward-a", 1001)
	agentB := fixture.createBoundAgent(t, "agent-forward-b", 1002)
	ctx := context.Background()
	require.NoError(t, fixture.database.Model(&model.Endpoint{}).
		Where("id = ?", "legacy-endpoint-a").
		Update("user_id", agentB.RuntimeUserID).Error)
	endpoint, err := fixture.service.CreateTechnicalResource(ctx, fixture.authorization, CreateTechnicalResourceInput{
		Type: model.TechnicalResourceEndpoint, StableKey: "endpoint-forward", ParentID: agentB.ID, CredentialRevision: 1,
	})
	require.NoError(t, err)
	bound, err := fixture.service.BindTechnicalResource(ctx, fixture.authorization, BindTechnicalResourceInput{
		TechnicalResourceID: endpoint.ID, SourceType: model.TechnicalResourceBindingLegacyEndpoint,
		SourceID: "legacy-endpoint-a", ExpectedResourceVersion: endpoint.RowVersion, Reason: "explicit Endpoint binding",
	})
	require.NoError(t, err)

	payload, hash := inventoryPayload(t, `{"kubernetes_clusters":[]}`)
	forwarded := inventoryInput(
		TechnicalResourceCredential{SourceType: model.TechnicalResourceBindingLegacyNode, SourceID: "1001", CredentialRevision: 1},
		payload, hash, "epoch-a", 1, "snapshot-a", 0, 1,
	)
	forwarded.SourceTechnicalResourceID = bound.TechnicalResource.ID
	forwarded.SourceCredentialRevision = 1
	_, err = fixture.service.ReceiveSupplyInventoryBatch(ctx, forwarded)
	require.ErrorIs(t, err, ErrTechnicalResourceUnbound)

	forwarded.AuthenticatedSource.SourceID = "1002"
	ack, err := fixture.service.ReceiveSupplyInventoryBatch(ctx, forwarded)
	require.NoError(t, err)
	require.Equal(t, bound.TechnicalResource.ID, ack.TechnicalResourceID)
	require.NotEqual(t, agentA.ID, ack.TechnicalResourceID)
}
