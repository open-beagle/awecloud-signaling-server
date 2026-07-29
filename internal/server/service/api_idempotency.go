package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

var (
	ErrIdempotencyKeyReused      = errors.New("idempotency key reused with different request")
	ErrIdempotencyInProgress     = errors.New("idempotency request is in progress")
	ErrIdempotencyRecoveryNeeded = errors.New("idempotency request requires business recovery")
	ErrIdempotencyStateConflict  = errors.New("idempotency record state conflict")
)

type APIIdempotencyService struct {
	db             *gorm.DB
	responsePolicy map[string]JSONFieldPolicy
	processingTTL  time.Duration
	retention      time.Duration
	now            func() time.Time
}

func NewAPIIdempotencyService(database *gorm.DB, policies map[string]JSONFieldPolicy, processingTTL, retention time.Duration) *APIIdempotencyService {
	cloned := make(map[string]JSONFieldPolicy, len(policies))
	for route, policy := range policies {
		cloned[route] = policy
	}
	if processingTTL <= 0 {
		processingTTL = 5 * time.Minute
	}
	if retention <= 0 {
		retention = 24 * time.Hour
	}
	return &APIIdempotencyService{db: database, responsePolicy: cloned, processingTTL: processingTTL, retention: retention, now: time.Now}
}

type BeginIdempotencyInput struct {
	ActorType string
	ActorID   string
	ScopeType string
	ScopeID   string
	Method    string
	Route     string
	Key       string
	Body      []byte
}

type IdempotencyBeginResult struct {
	Record *model.APIIdempotencyRecord
	Replay bool
}

func (s *APIIdempotencyService) Begin(ctx context.Context, input BeginIdempotencyInput) (*IdempotencyBeginResult, error) {
	for _, field := range []struct {
		name  string
		value string
		max   int
	}{
		{"actor_type", input.ActorType, 32}, {"actor_id", input.ActorID, 100},
		{"scope_type", input.ScopeType, 32}, {"scope_id", input.ScopeID, 100},
		{"method", input.Method, 10}, {"route", input.Route, 200}, {"idempotency_key", input.Key, 128},
	} {
		if err := validateRequired(field.name, field.value, field.max); err != nil {
			return nil, err
		}
	}
	method := strings.ToUpper(strings.TrimSpace(input.Method))
	if !requestCanMutate(method) {
		return nil, fmt.Errorf("%w: idempotency is only valid for mutating methods", ErrInvalidDeliveryInput)
	}
	route := strings.TrimSpace(input.Route)
	if !strings.HasPrefix(route, "/") || strings.ContainsAny(route, "?#") {
		return nil, fmt.Errorf("%w: route must be a normalized route template", ErrInvalidDeliveryInput)
	}
	if !validMetadataKey(input.Key) {
		return nil, fmt.Errorf("%w: invalid idempotency key", ErrInvalidDeliveryInput)
	}

	canonicalBody, err := canonicalJSONObject(input.Body)
	if err != nil {
		return nil, err
	}
	keyHash := sha256Hex([]byte(strings.TrimSpace(input.Key)))
	requestHash := sha256Hex([]byte(strings.Join([]string{
		strings.TrimSpace(input.ActorType), strings.TrimSpace(input.ActorID), strings.TrimSpace(input.ScopeType), strings.TrimSpace(input.ScopeID), method, route, string(canonicalBody),
	}, "\x00")))
	now := s.now().UTC()
	record := &model.APIIdempotencyRecord{
		ID:          uuid.NewString(),
		ActorType:   strings.TrimSpace(input.ActorType),
		ActorID:     strings.TrimSpace(input.ActorID),
		ScopeType:   strings.TrimSpace(input.ScopeType),
		ScopeID:     strings.TrimSpace(input.ScopeID),
		Method:      method,
		Route:       route,
		KeyHash:     keyHash,
		RequestHash: requestHash,
		Status:      model.APIIdempotencyProcessing,
		ExpiresAt:   now.Add(s.processingTTL),
	}
	result := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "actor_type"}, {Name: "actor_id"}, {Name: "scope_type"}, {Name: "scope_id"}, {Name: "method"}, {Name: "route"}, {Name: "key_hash"}},
		DoNothing: true,
	}).Create(record)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 1 {
		return &IdempotencyBeginResult{Record: record}, nil
	}

	var existing model.APIIdempotencyRecord
	if err := s.db.WithContext(ctx).
		Where("actor_type = ? AND actor_id = ? AND scope_type = ? AND scope_id = ? AND method = ? AND route = ? AND key_hash = ?",
			record.ActorType, record.ActorID, record.ScopeType, record.ScopeID, record.Method, record.Route, record.KeyHash).
		First(&existing).Error; err != nil {
		return nil, err
	}
	record = &existing
	if (record.Status == model.APIIdempotencyCompleted || record.Status == model.APIIdempotencyFailed) && !record.ExpiresAt.After(now) {
		deleted := s.db.WithContext(ctx).
			Where("id = ? AND status IN ? AND expires_at <= ?", record.ID, []model.APIIdempotencyStatus{model.APIIdempotencyCompleted, model.APIIdempotencyFailed}, now).
			Delete(&model.APIIdempotencyRecord{})
		if deleted.Error != nil {
			return nil, deleted.Error
		}
		if deleted.RowsAffected == 1 {
			return s.Begin(ctx, input)
		}
		return nil, ErrIdempotencyStateConflict
	}
	if record.RequestHash != requestHash {
		return nil, ErrIdempotencyKeyReused
	}
	switch record.Status {
	case model.APIIdempotencyCompleted, model.APIIdempotencyFailed:
		return &IdempotencyBeginResult{Record: record, Replay: true}, nil
	case model.APIIdempotencyProcessing:
		if record.ExpiresAt.After(now) {
			return nil, ErrIdempotencyInProgress
		}
		return &IdempotencyBeginResult{Record: record}, ErrIdempotencyRecoveryNeeded
	default:
		return nil, ErrIdempotencyStateConflict
	}
}

type CompleteIdempotencyInput struct {
	RecordID       string
	RequestHash    string
	Status         model.APIIdempotencyStatus
	ResponseStatus int
	ResponseBody   []byte
	ErrorCode      string
}

// Complete uses the caller's business transaction so the business result and
// replay record become visible atomically.
func (s *APIIdempotencyService) Complete(tx *gorm.DB, input CompleteIdempotencyInput) (*model.APIIdempotencyRecord, error) {
	if tx == nil {
		return nil, fmt.Errorf("%w: transaction handle is required", ErrInvalidDeliveryInput)
	}
	if input.Status != model.APIIdempotencyCompleted && input.Status != model.APIIdempotencyFailed {
		return nil, fmt.Errorf("%w: final idempotency status is required", ErrInvalidDeliveryInput)
	}
	if err := validateRequired("record_id", input.RecordID, 36); err != nil {
		return nil, err
	}
	if err := validateOptionalSHA256("request_hash", input.RequestHash); err != nil || input.RequestHash == "" {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("%w: request_hash is required", ErrInvalidDeliveryInput)
	}
	if len(input.ErrorCode) > 100 {
		return nil, fmt.Errorf("%w: error_code exceeds 100 bytes", ErrInvalidDeliveryInput)
	}
	if input.ResponseStatus < 100 || input.ResponseStatus > 599 {
		return nil, fmt.Errorf("%w: invalid response status", ErrInvalidDeliveryInput)
	}

	var record model.APIIdempotencyRecord
	if err := tx.First(&record, "id = ?", input.RecordID).Error; err != nil {
		return nil, err
	}
	if record.Status != model.APIIdempotencyProcessing || record.RequestHash != input.RequestHash {
		return nil, ErrIdempotencyStateConflict
	}
	policy, ok := s.responsePolicy[record.Method+" "+record.Route]
	if !ok {
		return nil, fmt.Errorf("%w: response policy is not declared for %s %s", ErrInvalidDeliveryInput, record.Method, record.Route)
	}
	body, err := policy.Validate(input.ResponseBody)
	if err != nil {
		return nil, err
	}
	if input.Status == model.APIIdempotencyFailed && strings.TrimSpace(input.ErrorCode) == "" {
		return nil, fmt.Errorf("%w: failed result requires error_code", ErrInvalidDeliveryInput)
	}

	now := s.now().UTC()
	result := tx.Model(&model.APIIdempotencyRecord{}).
		Where("id = ? AND request_hash = ? AND status = ?", record.ID, input.RequestHash, model.APIIdempotencyProcessing).
		Updates(map[string]any{
			"status":          input.Status,
			"response_status": input.ResponseStatus,
			"response_body":   string(body),
			"error_code":      strings.TrimSpace(input.ErrorCode),
			"expires_at":      now.Add(s.retention),
		})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, ErrIdempotencyStateConflict
	}
	if err := tx.First(&record, "id = ?", record.ID).Error; err != nil {
		return nil, err
	}
	return &record, nil
}

func validMetadataKey(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 || strings.Contains(value, ",") {
		return false
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || strings.ContainsRune("-_.:", char) {
			continue
		}
		return false
	}
	return true
}

func canonicalJSONObject(body []byte) ([]byte, error) {
	if len(body) == 0 {
		body = []byte("{}")
	}
	if len(body) > maxDeliveryJSONBytes {
		return nil, fmt.Errorf("%w: request JSON exceeds %d bytes", ErrInvalidDeliveryInput, maxDeliveryJSONBytes)
	}
	value, err := decodeJSONObject(body)
	if err != nil {
		return nil, err
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("%w: canonicalize request JSON: %v", ErrInvalidDeliveryInput, err)
	}
	return canonical, nil
}

func requestCanMutate(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}
