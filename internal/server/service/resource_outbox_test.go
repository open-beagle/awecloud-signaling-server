package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

type resourceDeliveryProjection struct {
	Key   string `gorm:"primaryKey;size:100"`
	Value int
}

func (resourceDeliveryProjection) TableName() string { return "resource_delivery_test_projection" }

func newResourceDeliveryServiceDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:" + uuid.NewString() + "?mode=memory&cache=shared&_pragma=busy_timeout(5000)"
	database, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := database.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(8)
	require.NoError(t, database.AutoMigrate(
		&model.MigrationBatch{},
		&model.MigrationSourceMapping{},
		&model.APIIdempotencyRecord{},
		&model.OutboxEvent{},
		&model.ConsumerRevision{},
		&resourceDeliveryProjection{},
	))
	return database
}

func newOutboxForTest(database *gorm.DB) *ResourceOutboxService {
	return NewResourceOutboxService(database, map[string]JSONFieldPolicy{
		"resource.changed.v1": NewJSONFieldPolicy("name", "state"),
	})
}

func appendOutboxForTest(t *testing.T, database *gorm.DB, service *ResourceOutboxService, revision int64, key string, available time.Time, maxAttempts int) *model.OutboxEvent {
	t.Helper()
	var event *model.OutboxEvent
	require.NoError(t, database.Transaction(func(tx *gorm.DB) error {
		var err error
		event, err = service.Append(tx, AppendOutboxEventInput{
			Consumer: "resource_projection", EventType: "resource.changed.v1", AggregateType: "resource", AggregateID: "resource-1",
			AggregateRevision: revision, EventKey: key, Payload: []byte(fmt.Sprintf(`{"name":"resource","state":"r%d"}`, revision)),
			RequestID: "request-1", AvailableAt: available, MaxAttempts: maxAttempts,
		})
		return err
	}))
	return event
}

func TestOutboxProducerSharesBusinessTransactionAndIsDeterministic(t *testing.T) {
	database := newResourceDeliveryServiceDB(t)
	outbox := newOutboxForTest(database)
	ctx := context.Background()

	errRollback := errors.New("rollback")
	err := database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		require.NoError(t, tx.Create(&resourceDeliveryProjection{Key: "rolled-back", Value: 1}).Error)
		_, appendErr := outbox.Append(tx, AppendOutboxEventInput{
			Consumer: "resource_projection", EventType: "resource.changed.v1", AggregateType: "resource", AggregateID: "resource-1",
			AggregateRevision: 1, EventKey: "rollback-event", Payload: []byte(`{"name":"resource","state":"ready"}`), RequestID: "request-1",
		})
		require.NoError(t, appendErr)
		return errRollback
	})
	require.ErrorIs(t, err, errRollback)
	var count int64
	require.NoError(t, database.Model(&model.OutboxEvent{}).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, database.Model(&resourceDeliveryProjection{}).Count(&count).Error)
	require.Zero(t, count)

	event := appendOutboxForTest(t, database, outbox, 1, "stable-event", time.Time{}, 3)
	var repeated *model.OutboxEvent
	require.NoError(t, database.Transaction(func(tx *gorm.DB) error {
		var repeatErr error
		repeated, repeatErr = outbox.Append(tx, AppendOutboxEventInput{
			Consumer: "resource_projection", EventType: "resource.changed.v1", AggregateType: "resource", AggregateID: "resource-1",
			AggregateRevision: 1, EventKey: "stable-event", Payload: []byte(`{ "state":"r1", "name":"resource" }`), RequestID: "request-1",
		})
		return repeatErr
	}))
	require.Equal(t, event.ID, repeated.ID)
	err = database.Transaction(func(tx *gorm.DB) error {
		_, collisionErr := outbox.Append(tx, AppendOutboxEventInput{
			Consumer: "resource_projection", EventType: "resource.changed.v1", AggregateType: "resource", AggregateID: "resource-1",
			AggregateRevision: 2, EventKey: "stable-event", Payload: []byte(`{"name":"resource","state":"r2"}`), RequestID: "request-1",
		})
		return collisionErr
	})
	require.ErrorIs(t, err, ErrOutboxEventKeyReused)
}

func TestOutboxPayloadPolicyRejectsUnknownAndSensitiveFields(t *testing.T) {
	database := newResourceDeliveryServiceDB(t)
	outbox := newOutboxForTest(database)

	for _, payload := range [][]byte{
		[]byte(`{"name":"resource","unknown":true}`),
		[]byte(`{"name":"resource","state":{"token":"secret"}}`),
		[]byte(`{"name":"resource","state":"token=secret-value"}`),
		[]byte(`null`),
		[]byte(`{"name":"` + strings.Repeat("x", 64*1024) + `"}`),
	} {
		err := database.Transaction(func(tx *gorm.DB) error {
			_, err := outbox.Append(tx, AppendOutboxEventInput{
				Consumer: "resource_projection", EventType: "resource.changed.v1", AggregateType: "resource", AggregateID: "resource-1",
				AggregateRevision: 1, EventKey: uuid.NewString(), Payload: payload, RequestID: "request-1",
			})
			return err
		})
		require.Error(t, err)
	}
}

func TestOutboxClaimLeaseRecoveryAndRevisionOrdering(t *testing.T) {
	database := newResourceDeliveryServiceDB(t)
	outbox := newOutboxForTest(database)
	clock := time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC)
	outbox.now = func() time.Time { return clock }

	appendOutboxForTest(t, database, outbox, 2, "revision-2", clock, 3)
	appendOutboxForTest(t, database, outbox, 1, "revision-1", clock.Add(time.Second), 3)

	first, err := outbox.Claim(context.Background(), "resource_projection", "worker-a", time.Minute)
	require.NoError(t, err)
	clock = clock.Add(2 * time.Minute)
	reclaimed, err := outbox.Claim(context.Background(), "resource_projection", "worker-b", time.Minute)
	require.NoError(t, err)
	require.Equal(t, first.ID, reclaimed.ID)
	_, err = outbox.Complete(context.Background(), first.ID, first.LeaseToken, func(tx *gorm.DB, aggregate OutboxAggregateRef) error { return nil })
	require.ErrorIs(t, err, ErrOutboxLeaseLost)

	executed, err := outbox.Complete(context.Background(), reclaimed.ID, reclaimed.LeaseToken, func(tx *gorm.DB, aggregate OutboxAggregateRef) error {
		return tx.Create(&resourceDeliveryProjection{Key: aggregate.AggregateID, Value: int(aggregate.AggregateRevision)}).Error
	})
	require.NoError(t, err)
	require.True(t, executed)

	clock = clock.Add(time.Second)
	late, err := outbox.Claim(context.Background(), "resource_projection", "worker-b", time.Minute)
	require.NoError(t, err)
	executed, err = outbox.Complete(context.Background(), late.ID, late.LeaseToken, func(tx *gorm.DB, aggregate OutboxAggregateRef) error {
		return tx.Model(&resourceDeliveryProjection{}).Where("key = ?", aggregate.AggregateID).Update("value", aggregate.AggregateRevision).Error
	})
	require.NoError(t, err)
	require.False(t, executed)

	var projection resourceDeliveryProjection
	require.NoError(t, database.First(&projection, "key = ?", "resource-1").Error)
	require.Equal(t, 2, projection.Value)
	var checkpoint model.ConsumerRevision
	require.NoError(t, database.First(&checkpoint, "consumer = ? AND aggregate_id = ?", "resource_projection", "resource-1").Error)
	require.EqualValues(t, 2, checkpoint.LastRevision)
}

func TestOutboxEffectFailureRollsBackCheckpointAndSideEffect(t *testing.T) {
	database := newResourceDeliveryServiceDB(t)
	outbox := newOutboxForTest(database)
	clock := time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC)
	outbox.now = func() time.Time { return clock }
	event := appendOutboxForTest(t, database, outbox, 1, "effect-failure", clock, 3)
	claimed, err := outbox.Claim(context.Background(), event.Consumer, "worker", time.Minute)
	require.NoError(t, err)

	effectErr := errors.New("effect failed")
	_, err = outbox.Complete(context.Background(), claimed.ID, claimed.LeaseToken, func(tx *gorm.DB, aggregate OutboxAggregateRef) error {
		require.NoError(t, tx.Create(&resourceDeliveryProjection{Key: "failed-effect", Value: 1}).Error)
		return effectErr
	})
	require.ErrorIs(t, err, effectErr)
	var count int64
	require.NoError(t, database.Model(&resourceDeliveryProjection{}).Where("key = ?", "failed-effect").Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, database.Model(&model.ConsumerRevision{}).Count(&count).Error)
	require.Zero(t, count)
	var persisted model.OutboxEvent
	require.NoError(t, database.First(&persisted, "id = ?", claimed.ID).Error)
	require.Equal(t, model.OutboxEventProcessing, persisted.Status)
}

func TestOutboxCompletionCoalescesReadyAggregateRevisions(t *testing.T) {
	database := newResourceDeliveryServiceDB(t)
	outbox := newOutboxForTest(database)
	clock := time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC)
	outbox.now = func() time.Time { return clock }
	appendOutboxForTest(t, database, outbox, 1, "coalesce-1", clock, 3)
	appendOutboxForTest(t, database, outbox, 2, "coalesce-2", clock, 3)
	appendOutboxForTest(t, database, outbox, 3, "coalesce-3", clock, 3)

	claimed, err := outbox.Claim(context.Background(), "resource_projection", "worker", time.Minute)
	require.NoError(t, err)
	var appliedRevision int64
	executed, err := outbox.Complete(context.Background(), claimed.ID, claimed.LeaseToken, func(tx *gorm.DB, aggregate OutboxAggregateRef) error {
		appliedRevision = aggregate.AggregateRevision
		return nil
	})
	require.NoError(t, err)
	require.True(t, executed)
	require.EqualValues(t, 3, appliedRevision)

	var processed int64
	require.NoError(t, database.Model(&model.OutboxEvent{}).Where("status = ?", model.OutboxEventProcessed).Count(&processed).Error)
	require.EqualValues(t, 3, processed)
	_, err = outbox.Claim(context.Background(), "resource_projection", "worker", time.Minute)
	require.ErrorIs(t, err, ErrOutboxNoEvent)
	var checkpoint model.ConsumerRevision
	require.NoError(t, database.First(&checkpoint, "consumer = ? AND aggregate_id = ?", "resource_projection", "resource-1").Error)
	require.EqualValues(t, 3, checkpoint.LastRevision)
}

func TestOutboxRetryAndDeadLetter(t *testing.T) {
	database := newResourceDeliveryServiceDB(t)
	outbox := newOutboxForTest(database)
	clock := time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC)
	outbox.now = func() time.Time { return clock }
	event := appendOutboxForTest(t, database, outbox, 1, "retry", clock, 2)

	claimed, err := outbox.Claim(context.Background(), event.Consumer, "worker", time.Minute)
	require.NoError(t, err)
	require.NoError(t, outbox.Fail(context.Background(), claimed.ID, claimed.LeaseToken, OutboxFailure{
		Retryable: true, Code: "TEMPORARY", Summary: "temporary\x00 failure token=secret-value Authorization: Bearer abc postgres://user:pass@db", BaseDelay: time.Second, MaxDelay: time.Minute,
	}))
	var pending model.OutboxEvent
	require.NoError(t, database.First(&pending, "id = ?", event.ID).Error)
	require.Equal(t, model.OutboxEventPending, pending.Status)
	require.NotContains(t, pending.LastErrorSummary, "secret-value")
	require.NotContains(t, pending.LastErrorSummary, "Bearer abc")
	require.NotContains(t, pending.LastErrorSummary, "user:pass")
	require.Contains(t, pending.LastErrorSummary, "[REDACTED]")
	require.False(t, pending.AvailableAt.Before(clock.Add(time.Second)))
	require.False(t, pending.AvailableAt.After(clock.Add(1250*time.Millisecond)))

	clock = pending.AvailableAt.Add(time.Millisecond)
	claimed, err = outbox.Claim(context.Background(), event.Consumer, "worker", time.Minute)
	require.NoError(t, err)
	require.NoError(t, outbox.Fail(context.Background(), claimed.ID, claimed.LeaseToken, OutboxFailure{
		Retryable: true, Code: "TEMPORARY", Summary: strings.Repeat("x", 700), BaseDelay: time.Second, MaxDelay: time.Minute,
	}))
	var dead model.OutboxEvent
	require.NoError(t, database.First(&dead, "id = ?", event.ID).Error)
	require.Equal(t, model.OutboxEventDeadLetter, dead.Status)
	require.Len(t, dead.LastErrorSummary, maxSummaryBytes)
	require.NotNil(t, dead.DeadLetterAt)
	require.NotContains(t, dead.LastErrorSummary, dead.Payload)
}

func TestOutboxClaimSweepsExhaustedEventsWhenQueueIsIdle(t *testing.T) {
	database := newResourceDeliveryServiceDB(t)
	outbox := newOutboxForTest(database)
	clock := time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC)
	outbox.now = func() time.Time { return clock }
	event := appendOutboxForTest(t, database, outbox, 1, "exhausted", clock, 1)

	require.NoError(t, database.Model(&model.OutboxEvent{}).Where("id = ?", event.ID).
		Updates(map[string]any{"attempt_count": 1, "status": model.OutboxEventPending}).Error)
	_, err := outbox.Claim(context.Background(), event.Consumer, "worker", time.Minute)
	require.ErrorIs(t, err, ErrOutboxNoEvent)

	var persisted model.OutboxEvent
	require.NoError(t, database.First(&persisted, "id = ?", event.ID).Error)
	require.Equal(t, model.OutboxEventDeadLetter, persisted.Status)
	require.Equal(t, "LEASE_EXPIRED_MAX_ATTEMPTS", persisted.LastErrorCode)
	require.NotNil(t, persisted.DeadLetterAt)
}

func TestOutboxConcurrentClaimHasSingleWinner(t *testing.T) {
	database := newResourceDeliveryServiceDB(t)
	outbox := newOutboxForTest(database)
	clock := time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC)
	outbox.now = func() time.Time { return clock }
	appendOutboxForTest(t, database, outbox, 1, "concurrent", clock, 3)

	const workers = 8
	var wg sync.WaitGroup
	winners := make(chan *model.OutboxEvent, workers)
	errorsSeen := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			event, err := outbox.Claim(context.Background(), "resource_projection", fmt.Sprintf("worker-%d", worker), time.Minute)
			if err == nil {
				winners <- event
				return
			}
			if !errors.Is(err, ErrOutboxNoEvent) {
				errorsSeen <- err
			}
		}(i)
	}
	wg.Wait()
	close(winners)
	close(errorsSeen)
	require.Empty(t, errorsSeen)
	require.Len(t, winners, 1)
}
