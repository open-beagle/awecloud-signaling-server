package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

var (
	ErrMigrationVersionConflict = errors.New("migration row version conflict")
	ErrInvalidMigrationState    = errors.New("invalid migration state transition")
)

type ResourceMigrationService struct {
	db  *gorm.DB
	now func() time.Time
}

func NewResourceMigrationService(database *gorm.DB) *ResourceMigrationService {
	return &ResourceMigrationService{db: database, now: time.Now}
}

type CreateMigrationBatchInput struct {
	Kind              string
	SourceFingerprint string
	ManifestHash      string
	RequestID         string
	TotalCount        int64
}

func (s *ResourceMigrationService) CreateBatch(ctx context.Context, input CreateMigrationBatchInput) (*model.MigrationBatch, error) {
	if err := validateRequired("kind", input.Kind, 64); err != nil {
		return nil, err
	}
	if err := validateRequired("source_fingerprint", input.SourceFingerprint, 64); err != nil {
		return nil, err
	}
	if err := validateOptionalSHA256("source_fingerprint", input.SourceFingerprint); err != nil {
		return nil, err
	}
	if err := validateOptionalSHA256("manifest_hash", input.ManifestHash); err != nil {
		return nil, err
	}
	if err := validateRequired("request_id", input.RequestID, 64); err != nil {
		return nil, err
	}
	if input.TotalCount < 0 {
		return nil, fmt.Errorf("%w: total_count cannot be negative", ErrInvalidDeliveryInput)
	}

	batch := &model.MigrationBatch{
		ID:                uuid.NewString(),
		Kind:              strings.TrimSpace(input.Kind),
		SourceFingerprint: strings.ToLower(input.SourceFingerprint),
		ManifestHash:      strings.ToLower(input.ManifestHash),
		Status:            model.MigrationBatchDraft,
		TotalCount:        input.TotalCount,
		RowVersion:        1,
		RequestID:         input.RequestID,
	}
	if err := s.db.WithContext(ctx).Create(batch).Error; err != nil {
		return nil, err
	}
	return batch, nil
}

type MigrationBatchTransition struct {
	ProcessedCount int64
	SucceededCount int64
	FailedCount    int64
	ManifestHash   string
}

func (s *ResourceMigrationService) TransitionBatch(ctx context.Context, id string, expectedVersion int64, next model.MigrationBatchStatus, counts MigrationBatchTransition) (*model.MigrationBatch, error) {
	if expectedVersion <= 0 || !next.Valid() {
		return nil, fmt.Errorf("%w: invalid batch revision or status", ErrInvalidDeliveryInput)
	}
	if counts.ProcessedCount < 0 || counts.SucceededCount < 0 || counts.FailedCount < 0 || counts.SucceededCount+counts.FailedCount > counts.ProcessedCount {
		return nil, fmt.Errorf("%w: invalid migration counts", ErrInvalidDeliveryInput)
	}
	if err := validateOptionalSHA256("manifest_hash", counts.ManifestHash); err != nil {
		return nil, err
	}

	now := s.now().UTC()
	var updated model.MigrationBatch
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current model.MigrationBatch
		if err := tx.First(&current, "id = ?", id).Error; err != nil {
			return err
		}
		if current.RowVersion != expectedVersion {
			return ErrMigrationVersionConflict
		}
		if !validBatchTransition(current.Status, next) {
			return ErrInvalidMigrationState
		}
		if counts.ProcessedCount > current.TotalCount {
			return fmt.Errorf("%w: processed_count exceeds total_count", ErrInvalidDeliveryInput)
		}
		if next == model.MigrationBatchCompleted && counts.ProcessedCount != current.TotalCount {
			return fmt.Errorf("%w: completed batch must process every source", ErrInvalidDeliveryInput)
		}

		updates := map[string]any{
			"status":          next,
			"processed_count": counts.ProcessedCount,
			"succeeded_count": counts.SucceededCount,
			"failed_count":    counts.FailedCount,
			"row_version":     gorm.Expr("row_version + 1"),
		}
		if counts.ManifestHash != "" {
			updates["manifest_hash"] = strings.ToLower(counts.ManifestHash)
		}
		if next == model.MigrationBatchRunning && current.StartedAt == nil {
			updates["started_at"] = now
		}
		if next.Terminal() {
			updates["finished_at"] = now
		}

		result := tx.Model(&model.MigrationBatch{}).
			Where("id = ? AND row_version = ? AND status = ?", id, expectedVersion, current.Status).
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrMigrationVersionConflict
		}
		return tx.First(&updated, "id = ?", id).Error
	})
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

func validBatchTransition(current, next model.MigrationBatchStatus) bool {
	if current == next {
		return false
	}
	switch current {
	case model.MigrationBatchDraft:
		return next == model.MigrationBatchRunning || next == model.MigrationBatchCancelled
	case model.MigrationBatchRunning:
		return next == model.MigrationBatchCompleted || next == model.MigrationBatchFailed || next == model.MigrationBatchCancelled
	default:
		return false
	}
}

type UpsertMigrationSourceInput struct {
	BatchID         string
	SourceType      string
	SourceID        string
	SourceRevision  string
	TargetType      string
	TargetID        string
	Classification  model.MigrationClassification
	EvidenceHash    string
	EvidenceSummary string
}

func (s *ResourceMigrationService) UpsertSource(ctx context.Context, input UpsertMigrationSourceInput) (*model.MigrationSourceMapping, error) {
	for _, field := range []struct {
		name  string
		value string
		max   int
	}{
		{"batch_id", input.BatchID, 36}, {"source_type", input.SourceType, 64},
		{"source_id", input.SourceID, 200}, {"source_revision", input.SourceRevision, 100},
	} {
		if err := validateRequired(field.name, field.value, field.max); err != nil {
			return nil, err
		}
	}
	if len(input.TargetType) > 64 || len(input.TargetID) > 100 {
		return nil, fmt.Errorf("%w: migration target exceeds its size limit", ErrInvalidDeliveryInput)
	}
	if !input.Classification.Valid() {
		return nil, fmt.Errorf("%w: invalid migration classification", ErrInvalidDeliveryInput)
	}
	if err := validateOptionalSHA256("evidence_hash", input.EvidenceHash); err != nil {
		return nil, err
	}
	summary := sanitizeSummary(input.EvidenceSummary)
	mapping := model.MigrationSourceMapping{
		ID:              uuid.NewString(),
		BatchID:         input.BatchID,
		SourceType:      strings.TrimSpace(input.SourceType),
		SourceID:        strings.TrimSpace(input.SourceID),
		SourceRevision:  strings.TrimSpace(input.SourceRevision),
		TargetType:      strings.TrimSpace(input.TargetType),
		TargetID:        strings.TrimSpace(input.TargetID),
		Classification:  input.Classification,
		Status:          model.MigrationSourceCandidate,
		EvidenceHash:    strings.ToLower(input.EvidenceHash),
		EvidenceSummary: summary,
		RowVersion:      1,
	}

	err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "batch_id"}, {Name: "source_type"}, {Name: "source_id"}, {Name: "source_revision"}},
		DoUpdates: clause.Assignments(map[string]any{
			"target_type":      mapping.TargetType,
			"target_id":        mapping.TargetID,
			"classification":   mapping.Classification,
			"evidence_hash":    mapping.EvidenceHash,
			"evidence_summary": mapping.EvidenceSummary,
			"row_version":      gorm.Expr("row_version + 1"),
			"updated_at":       s.now().UTC(),
		}),
		Where: clause.Where{Exprs: []clause.Expression{clause.Eq{Column: clause.Column{Name: "status"}, Value: model.MigrationSourceCandidate}}},
	}).Create(&mapping).Error
	if err != nil {
		return nil, err
	}
	var persisted model.MigrationSourceMapping
	if err := s.db.WithContext(ctx).
		Where("batch_id = ? AND source_type = ? AND source_id = ? AND source_revision = ?", input.BatchID, mapping.SourceType, mapping.SourceID, mapping.SourceRevision).
		First(&persisted).Error; err != nil {
		return nil, err
	}
	if persisted.Status != model.MigrationSourceCandidate {
		return nil, ErrInvalidMigrationState
	}
	return &persisted, nil
}

func (s *ResourceMigrationService) TransitionSource(ctx context.Context, id string, expectedVersion int64, next model.MigrationSourceStatus, targetType, targetID, errorCode, errorSummary string) (*model.MigrationSourceMapping, error) {
	if expectedVersion <= 0 || !next.Valid() {
		return nil, fmt.Errorf("%w: invalid source revision or status", ErrInvalidDeliveryInput)
	}
	if len(targetType) > 64 || len(targetID) > 100 || len(errorCode) > 100 {
		return nil, fmt.Errorf("%w: migration result exceeds its size limit", ErrInvalidDeliveryInput)
	}
	var updated model.MigrationSourceMapping
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current model.MigrationSourceMapping
		if err := tx.First(&current, "id = ?", id).Error; err != nil {
			return err
		}
		if current.RowVersion != expectedVersion {
			return ErrMigrationVersionConflict
		}
		if !validSourceTransition(current, next) {
			return ErrInvalidMigrationState
		}
		if next == model.MigrationSourceConfirmed && (strings.TrimSpace(targetType) == "" || strings.TrimSpace(targetID) == "") {
			return fmt.Errorf("%w: confirmed source requires a target", ErrInvalidDeliveryInput)
		}

		updates := map[string]any{
			"status":        next,
			"target_type":   strings.TrimSpace(targetType),
			"target_id":     strings.TrimSpace(targetID),
			"error_code":    strings.TrimSpace(errorCode),
			"error_summary": sanitizeSummary(errorSummary),
			"row_version":   gorm.Expr("row_version + 1"),
		}
		result := tx.Model(&model.MigrationSourceMapping{}).
			Where("id = ? AND row_version = ? AND status = ?", id, expectedVersion, current.Status).
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrMigrationVersionConflict
		}
		return tx.First(&updated, "id = ?", id).Error
	})
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

func validSourceTransition(current model.MigrationSourceMapping, next model.MigrationSourceStatus) bool {
	switch current.Status {
	case model.MigrationSourceCandidate:
		if next == model.MigrationSourceConfirmed {
			return current.Classification == model.MigrationClassificationManual
		}
		return next == model.MigrationSourceMigrated || next == model.MigrationSourceSkipped || next == model.MigrationSourceFailed
	case model.MigrationSourceConfirmed:
		return next == model.MigrationSourceMigrated || next == model.MigrationSourceSkipped || next == model.MigrationSourceFailed
	default:
		return false
	}
}
