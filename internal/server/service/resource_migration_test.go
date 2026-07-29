package service

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func TestResourceMigrationBatchAndManualMapping(t *testing.T) {
	database := newResourceDeliveryServiceDB(t)
	service := NewResourceMigrationService(database)
	ctx := context.Background()

	batch, err := service.CreateBatch(ctx, CreateMigrationBatchInput{
		Kind: "compat_inventory", SourceFingerprint: strings.Repeat("a", 64), RequestID: "request-1", TotalCount: 1,
	})
	require.NoError(t, err)
	require.Equal(t, model.MigrationBatchDraft, batch.Status)

	running, err := service.TransitionBatch(ctx, batch.ID, 1, model.MigrationBatchRunning, MigrationBatchTransition{})
	require.NoError(t, err)
	require.EqualValues(t, 2, running.RowVersion)
	require.NotNil(t, running.StartedAt)
	_, err = service.TransitionBatch(ctx, batch.ID, 1, model.MigrationBatchFailed, MigrationBatchTransition{})
	require.ErrorIs(t, err, ErrMigrationVersionConflict)

	mapping, err := service.UpsertSource(ctx, UpsertMigrationSourceInput{
		BatchID: batch.ID, SourceType: "resource", SourceID: "legacy-1", SourceRevision: "3",
		Classification: model.MigrationClassificationManual, EvidenceHash: strings.Repeat("b", 64),
		EvidenceSummary: "tenant\x00 mapping\n requires review",
	})
	require.NoError(t, err)
	require.Equal(t, "tenant mapping requires review", mapping.EvidenceSummary)

	confirmed, err := service.TransitionSource(ctx, mapping.ID, mapping.RowVersion, model.MigrationSourceConfirmed, "tenant_resource", "resource-1", "", "")
	require.NoError(t, err)
	require.Equal(t, model.MigrationSourceConfirmed, confirmed.Status)

	_, err = service.UpsertSource(ctx, UpsertMigrationSourceInput{
		BatchID: batch.ID, SourceType: "resource", SourceID: "legacy-1", SourceRevision: "3",
		Classification: model.MigrationClassificationManual, EvidenceHash: strings.Repeat("c", 64), EvidenceSummary: "changed",
	})
	require.ErrorIs(t, err, ErrInvalidMigrationState)
	var unchanged model.MigrationSourceMapping
	require.NoError(t, database.First(&unchanged, "id = ?", confirmed.ID).Error)
	require.Equal(t, strings.Repeat("b", 64), unchanged.EvidenceHash)
	require.Equal(t, "tenant mapping requires review", unchanged.EvidenceSummary)

	completed, err := service.TransitionBatch(ctx, batch.ID, running.RowVersion, model.MigrationBatchCompleted, MigrationBatchTransition{
		ProcessedCount: 1, SucceededCount: 1, ManifestHash: strings.Repeat("d", 64),
	})
	require.NoError(t, err)
	require.Equal(t, model.MigrationBatchCompleted, completed.Status)
	require.NotNil(t, completed.FinishedAt)
}

func TestResourceMigrationRejectsUnsafeTransitions(t *testing.T) {
	database := newResourceDeliveryServiceDB(t)
	service := NewResourceMigrationService(database)
	ctx := context.Background()
	batch, err := service.CreateBatch(ctx, CreateMigrationBatchInput{
		Kind: "inventory", SourceFingerprint: strings.Repeat("a", 64), RequestID: "request-1", TotalCount: 2,
	})
	require.NoError(t, err)

	_, err = service.TransitionBatch(ctx, batch.ID, 1, model.MigrationBatchCompleted, MigrationBatchTransition{ProcessedCount: 2})
	require.ErrorIs(t, err, ErrInvalidMigrationState)
	running, err := service.TransitionBatch(ctx, batch.ID, 1, model.MigrationBatchRunning, MigrationBatchTransition{})
	require.NoError(t, err)
	_, err = service.TransitionBatch(ctx, batch.ID, running.RowVersion, model.MigrationBatchCompleted, MigrationBatchTransition{ProcessedCount: 1})
	require.ErrorIs(t, err, ErrInvalidDeliveryInput)

	mapping, err := service.UpsertSource(ctx, UpsertMigrationSourceInput{
		BatchID: batch.ID, SourceType: "resource", SourceID: "1", SourceRevision: "1", Classification: model.MigrationClassificationAutomatic,
	})
	require.NoError(t, err)
	_, err = service.TransitionSource(ctx, mapping.ID, mapping.RowVersion, model.MigrationSourceConfirmed, "tenant_resource", "r1", "", "")
	require.ErrorIs(t, err, ErrInvalidMigrationState)
}
