package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

const (
	SupplyInventoryResultBatchStaged        = "BATCH_STAGED"
	SupplyInventoryResultSnapshotCommitted  = "SNAPSHOT_COMMITTED"
	SupplyInventoryResultSnapshotIncomplete = "SNAPSHOT_INCOMPLETE"
	maxSupplyInventoryPayloadBytes          = 1024 * 1024
	maxSupplyInventoryBatchCount            = 1024
)

var (
	ErrSourceEpochStale          = errors.New("SOURCE_EPOCH_STALE")
	ErrSourceSequenceConflict    = errors.New("SOURCE_SEQUENCE_CONFLICT")
	ErrSourceSequenceOutOfOrder  = errors.New("SOURCE_SEQUENCE_OUT_OF_ORDER")
	ErrSnapshotMetadataConflict  = errors.New("SNAPSHOT_METADATA_CONFLICT")
	ErrSupplyPayloadHashMismatch = errors.New("PAYLOAD_HASH_MISMATCH")
)

type ReceiveSupplyInventoryBatchInput struct {
	AuthenticatedSource       TechnicalResourceCredential
	SourceTechnicalResourceID string
	SourceCredentialRevision  int64
	SchemaVersion             int
	SourceEpoch               string
	Sequence                  int64
	SnapshotID                string
	BatchIndex                int
	BatchCount                int
	PayloadHash               string
	Payload                   []byte
}

type SupplyInventoryAck struct {
	TechnicalResourceID string
	AcceptedSequence    int64
	SnapshotID          string
	ResultCode          string
	Replay              bool
	SnapshotCommitted   bool
}

func (s *ProviderSupplyService) ReceiveSupplyInventoryBatch(ctx context.Context, input ReceiveSupplyInventoryBatchInput) (*SupplyInventoryAck, error) {
	if s == nil || s.db == nil {
		return nil, ErrProviderSupplyInvalidInput
	}
	input.SourceTechnicalResourceID = strings.TrimSpace(input.SourceTechnicalResourceID)
	input.SourceEpoch = strings.TrimSpace(input.SourceEpoch)
	input.SnapshotID = strings.TrimSpace(input.SnapshotID)
	input.PayloadHash = strings.ToLower(strings.TrimSpace(input.PayloadHash))
	if input.SchemaVersion <= 0 || input.Sequence <= 0 || input.BatchCount <= 0 || input.BatchCount > maxSupplyInventoryBatchCount ||
		input.BatchIndex < 0 || input.BatchIndex >= input.BatchCount ||
		validateRequired("source_epoch", input.SourceEpoch, 36) != nil ||
		validateRequired("snapshot_id", input.SnapshotID, 36) != nil ||
		validateOptionalSHA256("payload_hash", input.PayloadHash) != nil || input.PayloadHash == "" {
		return nil, ErrProviderSupplyInvalidInput
	}
	canonicalPayload, err := canonicalizeSupplyInventoryPayload(input.Payload)
	if err != nil {
		return nil, err
	}
	computedHash := sha256Hex(canonicalPayload)
	if computedHash != input.PayloadHash {
		return nil, ErrSupplyPayloadHashMismatch
	}

	now := s.now().UTC()
	var ack *SupplyInventoryAck
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		resource, err := resolveSupplyInventorySource(tx, input)
		if err != nil {
			return err
		}

		var existing model.SupplyInventoryReceipt
		err = tx.Where("technical_resource_id = ? AND source_epoch = ? AND sequence = ?", resource.ID, input.SourceEpoch, input.Sequence).
			First(&existing).Error
		if err == nil {
			if existing.PayloadHash != input.PayloadHash || existing.SchemaVersion != input.SchemaVersion ||
				existing.SnapshotID != input.SnapshotID || existing.BatchIndex != input.BatchIndex || existing.BatchCount != input.BatchCount {
				return ErrSourceSequenceConflict
			}
			ack = inventoryAckFromReceipt(&existing, true)
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		if resource.SourceEpoch == input.SourceEpoch {
			if input.Sequence != resource.LastSequence+1 {
				return ErrSourceSequenceOutOfOrder
			}
		} else {
			var knownEpochCount int64
			if err := tx.Model(&model.SupplyInventoryReceipt{}).
				Where("technical_resource_id = ? AND source_epoch = ?", resource.ID, input.SourceEpoch).
				Count(&knownEpochCount).Error; err != nil {
				return err
			}
			if knownEpochCount > 0 || input.Sequence != 1 {
				return ErrSourceEpochStale
			}
		}

		var snapshotReceipts []model.SupplyInventoryReceipt
		if err := tx.Where("technical_resource_id = ? AND source_epoch = ? AND snapshot_id = ?",
			resource.ID, input.SourceEpoch, input.SnapshotID).Find(&snapshotReceipts).Error; err != nil {
			return err
		}
		for _, receipt := range snapshotReceipts {
			if receipt.SchemaVersion != input.SchemaVersion || receipt.BatchCount != input.BatchCount ||
				receipt.Status != model.SupplyInventoryReceiptStaging {
				return ErrSnapshotMetadataConflict
			}
		}

		receipt := &model.SupplyInventoryReceipt{
			ID: uuid.NewString(), TechnicalResourceID: resource.ID, SourceEpoch: input.SourceEpoch, Sequence: input.Sequence,
			SchemaVersion: input.SchemaVersion, SnapshotID: input.SnapshotID, BatchIndex: input.BatchIndex, BatchCount: input.BatchCount,
			PayloadHash: input.PayloadHash, CanonicalPayload: string(canonicalPayload), ReceivedAt: now,
			Status: model.SupplyInventoryReceiptStaging, ResultCode: SupplyInventoryResultBatchStaged,
			PayloadDeleteAfter: timePointer(now.Add(24 * time.Hour)),
		}
		if err := tx.Create(receipt).Error; err != nil {
			if isDatabaseConstraintError(err) {
				return ErrSourceSequenceConflict
			}
			return err
		}

		updated := tx.Model(&model.TechnicalResource{}).
			Where("id = ? AND provider_id = ? AND source_epoch = ? AND last_sequence = ? AND lifecycle_state = ?",
				resource.ID, resource.ProviderID, resource.SourceEpoch, resource.LastSequence, model.TechnicalResourceRegistered).
			Updates(map[string]any{
				"source_epoch":      input.SourceEpoch,
				"last_sequence":     input.Sequence,
				"last_payload_hash": input.PayloadHash,
				"last_received_at":  now,
			})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrSourceSequenceConflict
		}

		snapshotReceipts = append(snapshotReceipts, *receipt)
		if len(snapshotReceipts) == input.BatchCount {
			if err := projectSupplyCandidatesFromSnapshot(tx, resource, snapshotReceipts, now); err != nil {
				return err
			}
			committedAt := now
			result := tx.Model(&model.SupplyInventoryReceipt{}).
				Where("technical_resource_id = ? AND source_epoch = ? AND snapshot_id = ? AND status = ?",
					resource.ID, input.SourceEpoch, input.SnapshotID, model.SupplyInventoryReceiptStaging).
				Updates(map[string]any{
					"status":       model.SupplyInventoryReceiptCommitted,
					"result_code":  SupplyInventoryResultSnapshotCommitted,
					"committed_at": committedAt,
				})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != int64(input.BatchCount) {
				return ErrSnapshotMetadataConflict
			}
			receipt.Status = model.SupplyInventoryReceiptCommitted
			receipt.ResultCode = SupplyInventoryResultSnapshotCommitted
			receipt.CommittedAt = &committedAt
		}
		ack = inventoryAckFromReceipt(receipt, false)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ack, nil
}

func (s *ProviderSupplyService) PurgeExpiredSupplyInventoryPayloads(ctx context.Context, at time.Time) (int64, error) {
	if s == nil || s.db == nil || at.IsZero() {
		return 0, ErrProviderSupplyInvalidInput
	}
	var purged int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		expiredStaging := tx.Model(&model.SupplyInventoryReceipt{}).
			Where("payload_delete_after IS NOT NULL AND payload_delete_after <= ? AND canonical_payload <> '' AND status = ?",
				at.UTC(), model.SupplyInventoryReceiptStaging).
			Updates(map[string]any{
				"status":               model.SupplyInventoryReceiptRejected,
				"result_code":          SupplyInventoryResultSnapshotIncomplete,
				"canonical_payload":    "",
				"payload_delete_after": nil,
			})
		if expiredStaging.Error != nil {
			return expiredStaging.Error
		}
		purged += expiredStaging.RowsAffected
		expiredFinal := tx.Model(&model.SupplyInventoryReceipt{}).
			Where("payload_delete_after IS NOT NULL AND payload_delete_after <= ? AND canonical_payload <> '' AND status IN ?",
				at.UTC(), []model.SupplyInventoryReceiptStatus{model.SupplyInventoryReceiptCommitted, model.SupplyInventoryReceiptRejected}).
			Updates(map[string]any{"canonical_payload": "", "payload_delete_after": nil})
		if expiredFinal.Error != nil {
			return expiredFinal.Error
		}
		purged += expiredFinal.RowsAffected
		return nil
	})
	return purged, err
}

func resolveSupplyInventorySource(tx *gorm.DB, input ReceiveSupplyInventoryBatchInput) (*model.TechnicalResource, error) {
	direct, err := resolveAuthenticatedTechnicalResource(tx, input.AuthenticatedSource)
	if err != nil {
		return nil, err
	}
	if err := requireReportingLifecycle(direct); err != nil {
		return nil, err
	}
	if input.SourceTechnicalResourceID == "" {
		if input.SourceCredentialRevision != 0 && input.SourceCredentialRevision != direct.CredentialRevision {
			return nil, ErrCredentialRevisionStale
		}
		return direct, nil
	}
	if direct.Type != model.TechnicalResourceAgent || input.SourceCredentialRevision <= 0 {
		return nil, ErrTechnicalResourceUnbound
	}
	var source model.TechnicalResource
	if err := tx.Where("id = ? AND provider_id = ? AND parent_id = ? AND type = ?",
		input.SourceTechnicalResourceID, direct.ProviderID, direct.ID, model.TechnicalResourceEndpoint).First(&source).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTechnicalResourceUnbound
		}
		return nil, err
	}
	if source.CredentialRevision != input.SourceCredentialRevision {
		return nil, ErrCredentialRevisionStale
	}
	binding, err := loadActiveTechnicalResourceBinding(tx, source.ID)
	if err != nil {
		return nil, err
	}
	if binding.CredentialRevision != input.SourceCredentialRevision {
		return nil, ErrCredentialRevisionStale
	}
	if err := requireReportingLifecycle(&source); err != nil {
		return nil, err
	}
	return &source, nil
}

func canonicalizeSupplyInventoryPayload(payload []byte) ([]byte, error) {
	if len(payload) == 0 || len(payload) > maxSupplyInventoryPayloadBytes {
		return nil, ErrProviderSupplyInvalidInput
	}
	value, err := decodeJSONObject(payload)
	if err != nil {
		return nil, err
	}
	if len(value) != 1 {
		return nil, ErrProviderSupplyInvalidInput
	}
	clusters, ok := value["kubernetes_clusters"].([]any)
	if !ok || clusters == nil {
		return nil, ErrProviderSupplyInvalidInput
	}
	if field, found := findSensitiveJSONField(value); found {
		return nil, fmt.Errorf("%w: %s", ErrSensitiveJSONField, field)
	}
	if field, found := findSupplyAuthorityField(value); found {
		return nil, fmt.Errorf("%w: authority field %s is not accepted", ErrProviderSupplyInvalidInput, field)
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("%w: canonicalize Supply Inventory: %v", ErrProviderSupplyInvalidInput, err)
	}
	if len(canonical) > maxSupplyInventoryPayloadBytes {
		return nil, ErrProviderSupplyInvalidInput
	}
	return canonical, nil
}

func findSupplyAuthorityField(value any) (string, bool) {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			normalized := strings.NewReplacer("-", "", "_", "", ".", "").Replace(strings.ToLower(key))
			switch normalized {
			case "providerid", "tenantid", "allocationid":
				return key, true
			}
			if field, found := findSupplyAuthorityField(nested); found {
				return key + "." + field, true
			}
		}
	case []any:
		for _, nested := range typed {
			if field, found := findSupplyAuthorityField(nested); found {
				return field, true
			}
		}
	}
	return "", false
}

func inventoryAckFromReceipt(receipt *model.SupplyInventoryReceipt, replay bool) *SupplyInventoryAck {
	return &SupplyInventoryAck{
		TechnicalResourceID: receipt.TechnicalResourceID,
		AcceptedSequence:    receipt.Sequence,
		SnapshotID:          receipt.SnapshotID,
		ResultCode:          receipt.ResultCode,
		Replay:              replay,
		SnapshotCommitted:   receipt.Status == model.SupplyInventoryReceiptCommitted,
	}
}

func timePointer(value time.Time) *time.Time {
	return &value
}
