package model

import "time"

type MigrationBatchStatus string

const (
	MigrationBatchDraft     MigrationBatchStatus = "draft"
	MigrationBatchRunning   MigrationBatchStatus = "running"
	MigrationBatchCompleted MigrationBatchStatus = "completed"
	MigrationBatchFailed    MigrationBatchStatus = "failed"
	MigrationBatchCancelled MigrationBatchStatus = "cancelled"
)

func (s MigrationBatchStatus) Valid() bool {
	switch s {
	case MigrationBatchDraft, MigrationBatchRunning, MigrationBatchCompleted, MigrationBatchFailed, MigrationBatchCancelled:
		return true
	default:
		return false
	}
}

func (s MigrationBatchStatus) Terminal() bool {
	return s == MigrationBatchCompleted || s == MigrationBatchFailed || s == MigrationBatchCancelled
}

type MigrationBatch struct {
	ID                string               `gorm:"primaryKey;size:36" json:"id"`
	Kind              string               `gorm:"size:64;not null;index" json:"kind"`
	SourceFingerprint string               `gorm:"size:64;not null" json:"source_fingerprint"`
	ManifestHash      string               `gorm:"size:64" json:"manifest_hash,omitempty"`
	Status            MigrationBatchStatus `gorm:"size:20;not null;default:'draft';index;check:chk_migration_batch_status,status IN ('draft','running','completed','failed','cancelled')" json:"status"`
	TotalCount        int64                `gorm:"not null;default:0;check:chk_migration_batch_total,total_count >= 0" json:"total_count"`
	ProcessedCount    int64                `gorm:"not null;default:0;check:chk_migration_batch_processed,processed_count >= 0" json:"processed_count"`
	SucceededCount    int64                `gorm:"not null;default:0;check:chk_migration_batch_succeeded,succeeded_count >= 0" json:"succeeded_count"`
	FailedCount       int64                `gorm:"not null;default:0;check:chk_migration_batch_failed,failed_count >= 0" json:"failed_count"`
	RowVersion        int64                `gorm:"not null;default:1;check:chk_migration_batch_version,row_version > 0" json:"row_version"`
	RequestID         string               `gorm:"size:64;not null;index" json:"request_id"`
	StartedAt         *time.Time           `json:"started_at,omitempty"`
	FinishedAt        *time.Time           `json:"finished_at,omitempty"`
	CreatedAt         time.Time            `json:"created_at"`
	UpdatedAt         time.Time            `json:"updated_at"`
}

func (MigrationBatch) TableName() string { return "migration_batch" }

type MigrationClassification string

const (
	MigrationClassificationAutomatic     MigrationClassification = "automatic"
	MigrationClassificationManual        MigrationClassification = "manual"
	MigrationClassificationCompatibility MigrationClassification = "compatibility"
	MigrationClassificationInvalid       MigrationClassification = "invalid"
)

func (c MigrationClassification) Valid() bool {
	switch c {
	case MigrationClassificationAutomatic, MigrationClassificationManual, MigrationClassificationCompatibility, MigrationClassificationInvalid:
		return true
	default:
		return false
	}
}

type MigrationSourceStatus string

const (
	MigrationSourceCandidate MigrationSourceStatus = "candidate"
	MigrationSourceConfirmed MigrationSourceStatus = "confirmed"
	MigrationSourceMigrated  MigrationSourceStatus = "migrated"
	MigrationSourceSkipped   MigrationSourceStatus = "skipped"
	MigrationSourceFailed    MigrationSourceStatus = "failed"
)

func (s MigrationSourceStatus) Valid() bool {
	switch s {
	case MigrationSourceCandidate, MigrationSourceConfirmed, MigrationSourceMigrated, MigrationSourceSkipped, MigrationSourceFailed:
		return true
	default:
		return false
	}
}

type MigrationSourceMapping struct {
	ID              string                  `gorm:"primaryKey;size:36" json:"id"`
	BatchID         string                  `gorm:"size:36;not null;uniqueIndex:uk_migration_source,priority:1;index" json:"batch_id"`
	SourceType      string                  `gorm:"size:64;not null;uniqueIndex:uk_migration_source,priority:2;index" json:"source_type"`
	SourceID        string                  `gorm:"size:200;not null;uniqueIndex:uk_migration_source,priority:3" json:"source_id"`
	SourceRevision  string                  `gorm:"size:100;not null;uniqueIndex:uk_migration_source,priority:4" json:"source_revision"`
	TargetType      string                  `gorm:"size:64;index" json:"target_type,omitempty"`
	TargetID        string                  `gorm:"size:100;index" json:"target_id,omitempty"`
	Classification  MigrationClassification `gorm:"size:20;not null;index;check:chk_migration_source_classification,classification IN ('automatic','manual','compatibility','invalid')" json:"classification"`
	Status          MigrationSourceStatus   `gorm:"size:20;not null;default:'candidate';index;check:chk_migration_source_status,status IN ('candidate','confirmed','migrated','skipped','failed')" json:"status"`
	EvidenceHash    string                  `gorm:"size:64" json:"evidence_hash,omitempty"`
	EvidenceSummary string                  `gorm:"type:text;check:chk_migration_evidence_size,length(CAST(evidence_summary AS BLOB)) <= 512" json:"evidence_summary,omitempty"`
	ErrorCode       string                  `gorm:"size:100" json:"error_code,omitempty"`
	ErrorSummary    string                  `gorm:"type:text;check:chk_migration_error_size,length(CAST(error_summary AS BLOB)) <= 512" json:"error_summary,omitempty"`
	RowVersion      int64                   `gorm:"not null;default:1;check:chk_migration_source_version,row_version > 0" json:"row_version"`
	CreatedAt       time.Time               `json:"created_at"`
	UpdatedAt       time.Time               `json:"updated_at"`

	Batch *MigrationBatch `gorm:"foreignKey:BatchID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
}

func (MigrationSourceMapping) TableName() string { return "migration_source_mapping" }
