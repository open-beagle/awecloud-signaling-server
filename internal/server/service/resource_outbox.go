package service

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

var (
	ErrOutboxEventTypeUndeclared = errors.New("outbox event type has no payload policy")
	ErrOutboxEventKeyReused      = errors.New("outbox event key reused with different event")
	ErrOutboxLeaseLost           = errors.New("outbox lease is no longer valid")
	ErrOutboxNoEvent             = errors.New("no outbox event is available")
)

type ResourceOutboxService struct {
	db       *gorm.DB
	policies map[string]JSONFieldPolicy
	now      func() time.Time
}

func NewResourceOutboxService(database *gorm.DB, policies map[string]JSONFieldPolicy) *ResourceOutboxService {
	cloned := make(map[string]JSONFieldPolicy, len(policies))
	for eventType, policy := range policies {
		cloned[eventType] = policy
	}
	return &ResourceOutboxService{db: database, policies: cloned, now: time.Now}
}

type AppendOutboxEventInput struct {
	Consumer          string
	EventType         string
	AggregateType     string
	AggregateID       string
	AggregateRevision int64
	EventKey          string
	Payload           []byte
	RequestID         string
	AvailableAt       time.Time
	MaxAttempts       int
}

// Append writes an event using the caller's transaction. It never falls back
// to the service database handle.
func (s *ResourceOutboxService) Append(tx *gorm.DB, input AppendOutboxEventInput) (*model.OutboxEvent, error) {
	if tx == nil {
		return nil, fmt.Errorf("%w: transaction handle is required", ErrInvalidDeliveryInput)
	}
	for _, field := range []struct {
		name  string
		value string
		max   int
	}{
		{"consumer", input.Consumer, 100}, {"event_type", input.EventType, 100},
		{"aggregate_type", input.AggregateType, 64}, {"aggregate_id", input.AggregateID, 100},
		{"event_key", input.EventKey, 100}, {"request_id", input.RequestID, 64},
	} {
		if err := validateRequired(field.name, field.value, field.max); err != nil {
			return nil, err
		}
	}
	if input.AggregateRevision <= 0 {
		return nil, fmt.Errorf("%w: aggregate_revision must be positive", ErrInvalidDeliveryInput)
	}
	policy, ok := s.policies[input.EventType]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrOutboxEventTypeUndeclared, input.EventType)
	}
	payload, err := policy.Validate(input.Payload)
	if err != nil {
		return nil, err
	}
	if input.MaxAttempts <= 0 {
		input.MaxAttempts = 5
	}
	if input.AvailableAt.IsZero() {
		input.AvailableAt = s.now().UTC()
	}

	event := &model.OutboxEvent{
		ID:                uuid.NewString(),
		Consumer:          strings.TrimSpace(input.Consumer),
		EventType:         strings.TrimSpace(input.EventType),
		AggregateType:     strings.TrimSpace(input.AggregateType),
		AggregateID:       strings.TrimSpace(input.AggregateID),
		AggregateRevision: input.AggregateRevision,
		EventKey:          strings.TrimSpace(input.EventKey),
		Payload:           string(payload),
		PayloadHash:       sha256Hex(payload),
		RequestID:         strings.TrimSpace(input.RequestID),
		Status:            model.OutboxEventPending,
		AvailableAt:       input.AvailableAt.UTC(),
		MaxAttempts:       input.MaxAttempts,
	}
	result := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "consumer"}, {Name: "event_key"}},
		DoNothing: true,
	}).Create(event)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 1 {
		return event, nil
	}

	var existing model.OutboxEvent
	if err := tx.Where("consumer = ? AND event_key = ?", event.Consumer, event.EventKey).First(&existing).Error; err != nil {
		return nil, err
	}
	if existing.EventType != event.EventType || existing.AggregateType != event.AggregateType || existing.AggregateID != event.AggregateID ||
		existing.AggregateRevision != event.AggregateRevision || existing.PayloadHash != event.PayloadHash {
		return nil, ErrOutboxEventKeyReused
	}
	return &existing, nil
}

func (s *ResourceOutboxService) Claim(ctx context.Context, consumer, owner string, leaseDuration time.Duration) (*model.OutboxEvent, error) {
	if err := validateRequired("consumer", consumer, 100); err != nil {
		return nil, err
	}
	if err := validateRequired("lease_owner", owner, 100); err != nil {
		return nil, err
	}
	if leaseDuration <= 0 {
		return nil, fmt.Errorf("%w: lease duration must be positive", ErrInvalidDeliveryInput)
	}
	now := s.now().UTC()
	for attempt := 0; attempt < 16; attempt++ {
		var candidate model.OutboxEvent
		result := s.db.WithContext(ctx).Raw(`
			SELECT id FROM (
				SELECT id, available_at, created_at FROM (
					SELECT id, available_at, created_at
					FROM outbox_event
					WHERE consumer = ? AND status = ? AND attempt_count < max_attempts AND available_at <= ?
					ORDER BY available_at ASC, created_at ASC, id ASC
					LIMIT 1
				)
				UNION ALL
				SELECT id, available_at, created_at FROM (
					SELECT id, available_at, created_at
					FROM outbox_event
					WHERE consumer = ? AND status = ? AND attempt_count < max_attempts
						AND lease_expires_at IS NOT NULL AND lease_expires_at <= ?
					ORDER BY available_at ASC, created_at ASC, id ASC
					LIMIT 1
				)
			)
			ORDER BY available_at ASC, created_at ASC, id ASC
			LIMIT 1`,
			consumer, model.OutboxEventPending, now,
			consumer, model.OutboxEventProcessing, now,
		).Scan(&candidate)
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected == 0 {
			if err := s.deadLetterExpiredAttempts(ctx, consumer, now); err != nil {
				return nil, err
			}
			return nil, ErrOutboxNoEvent
		}

		leaseToken := uuid.NewString()
		leaseExpiresAt := now.Add(leaseDuration)
		result = s.db.WithContext(ctx).Model(&model.OutboxEvent{}).
			Where("id = ? AND consumer = ? AND attempt_count < max_attempts AND ((status = ? AND available_at <= ?) OR (status = ? AND lease_expires_at IS NOT NULL AND lease_expires_at <= ?))",
				candidate.ID, consumer, model.OutboxEventPending, now, model.OutboxEventProcessing, now).
			Updates(map[string]any{
				"status":           model.OutboxEventProcessing,
				"lease_owner":      owner,
				"lease_token":      leaseToken,
				"lease_expires_at": leaseExpiresAt,
				"attempt_count":    gorm.Expr("attempt_count + 1"),
			})
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected == 0 {
			continue
		}

		var claimed model.OutboxEvent
		if err := s.db.WithContext(ctx).Where("id = ? AND lease_token = ?", candidate.ID, leaseToken).First(&claimed).Error; err != nil {
			return nil, err
		}
		return &claimed, nil
	}
	return nil, ErrOutboxNoEvent
}

func (s *ResourceOutboxService) deadLetterExpiredAttempts(ctx context.Context, consumer string, now time.Time) error {
	summary := "event exhausted attempts after lease expiry"
	return s.db.WithContext(ctx).Model(&model.OutboxEvent{}).
		Where("consumer = ? AND attempt_count >= max_attempts AND (status = ? OR (status = ? AND lease_expires_at IS NOT NULL AND lease_expires_at <= ?))",
			consumer, model.OutboxEventPending, model.OutboxEventProcessing, now).
		Updates(map[string]any{
			"status":             model.OutboxEventDeadLetter,
			"last_error_code":    "LEASE_EXPIRED_MAX_ATTEMPTS",
			"last_error_summary": summary,
			"dead_letter_at":     now,
			"lease_owner":        "",
			"lease_token":        "",
			"lease_expires_at":   nil,
		}).Error
}

type OutboxAggregateRef struct {
	EventID           string
	Consumer          string
	AggregateType     string
	AggregateID       string
	AggregateRevision int64
}

// OutboxEffect receives only the aggregate reference. Consumers must reread
// authoritative state instead of applying the event payload incrementally.
type OutboxEffect func(tx *gorm.DB, aggregate OutboxAggregateRef) error

func (s *ResourceOutboxService) Complete(ctx context.Context, eventID, leaseToken string, effect OutboxEffect) (bool, error) {
	if effect == nil {
		return false, fmt.Errorf("%w: outbox effect is required", ErrInvalidDeliveryInput)
	}
	now := s.now().UTC()
	executed := false
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var event model.OutboxEvent
		if err := tx.Where("id = ? AND status = ? AND lease_token = ? AND lease_expires_at > ?", eventID, model.OutboxEventProcessing, leaseToken, now).First(&event).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrOutboxLeaseLost
			}
			return err
		}
		effectiveEvent := event
		var latest model.OutboxEvent
		err := tx.Where("consumer = ? AND aggregate_type = ? AND aggregate_id = ? AND status = ? AND attempt_count < max_attempts AND available_at <= ? AND aggregate_revision > ?",
			event.Consumer, event.AggregateType, event.AggregateID, model.OutboxEventPending, now, event.AggregateRevision).
			Order("aggregate_revision DESC, created_at DESC, id DESC").First(&latest).Error
		if err == nil {
			effectiveEvent = latest
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		checkpoint := model.ConsumerRevision{
			ID:            uuid.NewString(),
			Consumer:      effectiveEvent.Consumer,
			AggregateType: effectiveEvent.AggregateType,
			AggregateID:   effectiveEvent.AggregateID,
			LastRevision:  effectiveEvent.AggregateRevision,
			LastEventID:   effectiveEvent.ID,
			RowVersion:    1,
			UpdatedAt:     now,
		}
		result := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "consumer"}, {Name: "aggregate_type"}, {Name: "aggregate_id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"last_revision": event.AggregateRevision,
				"last_event_id": event.ID,
				"row_version":   gorm.Expr("row_version + 1"),
				"updated_at":    now,
			}),
			Where: clause.Where{Exprs: []clause.Expression{
				clause.Expr{SQL: "consumer_revision.last_revision < excluded.last_revision"},
			}},
		}).Create(&checkpoint)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 1 {
			if err := effect(tx, OutboxAggregateRef{
				EventID: effectiveEvent.ID, Consumer: effectiveEvent.Consumer, AggregateType: effectiveEvent.AggregateType,
				AggregateID: effectiveEvent.AggregateID, AggregateRevision: effectiveEvent.AggregateRevision,
			}); err != nil {
				return err
			}
			executed = true
		}

		result = tx.Model(&model.OutboxEvent{}).
			Where("id = ? AND status = ? AND lease_token = ? AND lease_expires_at > ?", event.ID, model.OutboxEventProcessing, leaseToken, now).
			Updates(map[string]any{
				"status":           model.OutboxEventProcessed,
				"processed_at":     now,
				"lease_owner":      "",
				"lease_token":      "",
				"lease_expires_at": nil,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrOutboxLeaseLost
		}
		if effectiveEvent.ID != event.ID {
			result = tx.Model(&model.OutboxEvent{}).
				Where("consumer = ? AND aggregate_type = ? AND aggregate_id = ? AND status = ? AND attempt_count < max_attempts AND available_at <= ? AND aggregate_revision <= ?",
					event.Consumer, event.AggregateType, event.AggregateID, model.OutboxEventPending, now, effectiveEvent.AggregateRevision).
				Updates(map[string]any{
					"status":           model.OutboxEventProcessed,
					"processed_at":     now,
					"lease_owner":      "",
					"lease_token":      "",
					"lease_expires_at": nil,
				})
			if result.Error != nil {
				return result.Error
			}
		}
		return nil
	})
	return executed, err
}

type OutboxFailure struct {
	Retryable bool
	Code      string
	Summary   string
	BaseDelay time.Duration
	MaxDelay  time.Duration
}

func (s *ResourceOutboxService) Fail(ctx context.Context, eventID, leaseToken string, failure OutboxFailure) error {
	if err := validateRequired("error_code", failure.Code, 100); err != nil {
		return err
	}
	if failure.BaseDelay <= 0 {
		failure.BaseDelay = time.Second
	}
	if failure.MaxDelay <= 0 {
		failure.MaxDelay = time.Hour
	}
	if failure.MaxDelay < failure.BaseDelay {
		return fmt.Errorf("%w: max retry delay is less than base delay", ErrInvalidDeliveryInput)
	}

	now := s.now().UTC()
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var event model.OutboxEvent
		if err := tx.Where("id = ? AND status = ? AND lease_token = ? AND lease_expires_at > ?", eventID, model.OutboxEventProcessing, leaseToken, now).First(&event).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrOutboxLeaseLost
			}
			return err
		}

		updates := map[string]any{
			"last_error_code":    strings.TrimSpace(failure.Code),
			"last_error_summary": sanitizeSummary(failure.Summary),
			"lease_owner":        "",
			"lease_token":        "",
			"lease_expires_at":   nil,
		}
		if !failure.Retryable || event.AttemptCount >= event.MaxAttempts {
			updates["status"] = model.OutboxEventDeadLetter
			updates["dead_letter_at"] = now
		} else {
			updates["status"] = model.OutboxEventPending
			updates["available_at"] = now.Add(retryDelay(event.AttemptCount, failure.BaseDelay, failure.MaxDelay))
		}
		result := tx.Model(&model.OutboxEvent{}).
			Where("id = ? AND status = ? AND lease_token = ? AND lease_expires_at > ?", event.ID, model.OutboxEventProcessing, leaseToken, now).
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrOutboxLeaseLost
		}
		return nil
	})
}

func retryDelay(attempt int, baseDelay, maxDelay time.Duration) time.Duration {
	shift := attempt - 1
	if shift < 0 {
		shift = 0
	}
	if shift > 30 {
		shift = 30
	}
	delay := baseDelay
	for i := 0; i < shift && delay < maxDelay; i++ {
		if delay > maxDelay/2 {
			delay = maxDelay
			break
		}
		delay *= 2
	}
	if delay > maxDelay {
		delay = maxDelay
	}
	jitterLimit := delay / 4
	if jitterLimit <= 0 || delay == maxDelay {
		return delay
	}
	random, err := cryptorand.Int(cryptorand.Reader, big.NewInt(int64(jitterLimit)+1))
	if err != nil {
		return delay
	}
	if delay+time.Duration(random.Int64()) > maxDelay {
		return maxDelay
	}
	return delay + time.Duration(random.Int64())
}
