package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func newIdempotencyForTest(database *gorm.DB) *APIIdempotencyService {
	return NewAPIIdempotencyService(database, map[string]JSONFieldPolicy{
		"POST /resources": NewJSONFieldPolicy("id", "state", "error"),
	}, time.Minute, time.Hour)
}

func idempotencyInput(body string) BeginIdempotencyInput {
	return BeginIdempotencyInput{
		ActorType: "user", ActorID: "user-1", ScopeType: "tenant", ScopeID: "tenant-1",
		Method: "POST", Route: "/resources", Key: "request-key-1", Body: []byte(body),
	}
}

func TestAPIIdempotencyReplayConflictAndRawKeyProtection(t *testing.T) {
	database := newResourceDeliveryServiceDB(t)
	service := newIdempotencyForTest(database)
	clock := time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return clock }
	ctx := context.Background()

	begin, err := service.Begin(ctx, idempotencyInput(`{"name":"resource","enabled":true}`))
	require.NoError(t, err)
	require.False(t, begin.Replay)
	require.NotEqual(t, "request-key-1", begin.Record.KeyHash)
	require.NotContains(t, begin.Record.KeyHash, "request-key-1")

	err = database.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&resourceDeliveryProjection{Key: "idempotent-business", Value: 1}).Error; err != nil {
			return err
		}
		_, completeErr := service.Complete(tx, CompleteIdempotencyInput{
			RecordID: begin.Record.ID, RequestHash: begin.Record.RequestHash, Status: model.APIIdempotencyCompleted,
			ResponseStatus: 201, ResponseBody: []byte(`{"state":"created","id":"resource-1"}`),
		})
		return completeErr
	})
	require.NoError(t, err)

	replay, err := service.Begin(ctx, idempotencyInput(`{ "enabled": true, "name": "resource" }`))
	require.NoError(t, err)
	require.True(t, replay.Replay)
	require.Equal(t, 201, replay.Record.ResponseStatus)
	require.JSONEq(t, `{"id":"resource-1","state":"created"}`, replay.Record.ResponseBody)

	_, err = service.Begin(ctx, idempotencyInput(`{"name":"different","enabled":true}`))
	require.ErrorIs(t, err, ErrIdempotencyKeyReused)

	var persisted model.APIIdempotencyRecord
	require.NoError(t, database.First(&persisted, "id = ?", begin.Record.ID).Error)
	require.NotEqual(t, "request-key-1", persisted.KeyHash)

	clock = clock.Add(2 * time.Hour)
	reused, err := service.Begin(ctx, idempotencyInput(`{"name":"different","enabled":true}`))
	require.NoError(t, err)
	require.NotEqual(t, begin.Record.ID, reused.Record.ID)
	require.False(t, reused.Replay)
}

func TestAPIIdempotencyInProgressExpiryAndTransactionRollback(t *testing.T) {
	database := newResourceDeliveryServiceDB(t)
	service := newIdempotencyForTest(database)
	clock := time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return clock }
	ctx := context.Background()

	begin, err := service.Begin(ctx, idempotencyInput(`{"name":"resource"}`))
	require.NoError(t, err)
	_, err = service.Begin(ctx, idempotencyInput(`{"name":"resource"}`))
	require.ErrorIs(t, err, ErrIdempotencyInProgress)

	rollbackErr := errors.New("rollback")
	err = database.Transaction(func(tx *gorm.DB) error {
		_, completeErr := service.Complete(tx, CompleteIdempotencyInput{
			RecordID: begin.Record.ID, RequestHash: begin.Record.RequestHash, Status: model.APIIdempotencyCompleted,
			ResponseStatus: 201, ResponseBody: []byte(`{"id":"resource-1","state":"created"}`),
		})
		require.NoError(t, completeErr)
		return rollbackErr
	})
	require.ErrorIs(t, err, rollbackErr)
	var persisted model.APIIdempotencyRecord
	require.NoError(t, database.First(&persisted, "id = ?", begin.Record.ID).Error)
	require.Equal(t, model.APIIdempotencyProcessing, persisted.Status)

	clock = clock.Add(2 * time.Minute)
	recovery, err := service.Begin(ctx, idempotencyInput(`{"name":"resource"}`))
	require.ErrorIs(t, err, ErrIdempotencyRecoveryNeeded)
	require.NotNil(t, recovery)
	require.Equal(t, begin.Record.ID, recovery.Record.ID)
}

func TestAPIIdempotencyResponsePolicyFailsClosed(t *testing.T) {
	database := newResourceDeliveryServiceDB(t)
	service := newIdempotencyForTest(database)
	begin, err := service.Begin(context.Background(), idempotencyInput(`{"name":"resource"}`))
	require.NoError(t, err)

	for _, response := range [][]byte{
		[]byte(`{"id":"resource-1","token":"secret"}`),
		[]byte(`{"id":"resource-1","error":"token=secret-value"}`),
		[]byte(`{"id":"resource-1","unknown":true}`),
	} {
		err := database.Transaction(func(tx *gorm.DB) error {
			_, completeErr := service.Complete(tx, CompleteIdempotencyInput{
				RecordID: begin.Record.ID, RequestHash: begin.Record.RequestHash, Status: model.APIIdempotencyCompleted,
				ResponseStatus: 201, ResponseBody: response,
			})
			return completeErr
		})
		require.Error(t, err)
	}

	var persisted model.APIIdempotencyRecord
	require.NoError(t, database.First(&persisted, "id = ?", begin.Record.ID).Error)
	require.Equal(t, model.APIIdempotencyProcessing, persisted.Status)
	require.Empty(t, persisted.ResponseBody)
}
