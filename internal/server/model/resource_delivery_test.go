package model

import (
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newResourceDeliveryModelDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(
		&MigrationBatch{},
		&MigrationSourceMapping{},
		&APIIdempotencyRecord{},
		&OutboxEvent{},
		&ConsumerRevision{},
	))
	return database
}

func TestResourceDeliveryModelConstraints(t *testing.T) {
	database := newResourceDeliveryModelDB(t)
	now := time.Now().UTC()

	batch := MigrationBatch{ID: uuid.NewString(), Kind: "inventory", SourceFingerprint: strings.Repeat("a", 64), Status: MigrationBatchDraft, RowVersion: 1, RequestID: uuid.NewString()}
	require.NoError(t, database.Create(&batch).Error)

	duplicateA := MigrationSourceMapping{ID: uuid.NewString(), BatchID: batch.ID, SourceType: "resource", SourceID: "1", SourceRevision: "1", Classification: MigrationClassificationManual, Status: MigrationSourceCandidate, RowVersion: 1}
	require.NoError(t, database.Create(&duplicateA).Error)
	duplicateB := duplicateA
	duplicateB.ID = uuid.NewString()
	require.Error(t, database.Create(&duplicateB).Error)

	invalidRevision := OutboxEvent{
		ID: uuid.NewString(), Consumer: "projection", EventType: "resource.changed.v1", AggregateType: "resource", AggregateID: "r1",
		AggregateRevision: 0, EventKey: "event-1", Payload: "{}", PayloadHash: strings.Repeat("b", 64), RequestID: uuid.NewString(),
		Status: OutboxEventPending, AvailableAt: now, MaxAttempts: 3,
	}
	require.Error(t, database.Create(&invalidRevision).Error)

	oversizedPayload := invalidRevision
	oversizedPayload.ID = uuid.NewString()
	oversizedPayload.EventKey = "event-2"
	oversizedPayload.AggregateRevision = 1
	oversizedPayload.Payload = strings.Repeat("x", 64*1024+1)
	require.Error(t, database.Create(&oversizedPayload).Error)

	invalidStatus := APIIdempotencyRecord{
		ID: uuid.NewString(), ActorType: "user", ActorID: "1", ScopeType: "tenant", ScopeID: "t1", Method: "POST", Route: "/resources",
		KeyHash: strings.Repeat("c", 64), RequestHash: strings.Repeat("d", 64), Status: APIIdempotencyStatus("unknown"), ExpiresAt: now.Add(time.Hour),
	}
	require.Error(t, database.Create(&invalidStatus).Error)
}
