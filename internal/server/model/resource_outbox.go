package model

import "time"

type OutboxEventStatus string

const (
	OutboxEventPending    OutboxEventStatus = "pending"
	OutboxEventProcessing OutboxEventStatus = "processing"
	OutboxEventProcessed  OutboxEventStatus = "processed"
	OutboxEventDeadLetter OutboxEventStatus = "dead_letter"
)

func (s OutboxEventStatus) Valid() bool {
	switch s {
	case OutboxEventPending, OutboxEventProcessing, OutboxEventProcessed, OutboxEventDeadLetter:
		return true
	default:
		return false
	}
}

type OutboxEvent struct {
	ID                string            `gorm:"primaryKey;size:36" json:"id"`
	Consumer          string            `gorm:"size:100;not null;uniqueIndex:uk_outbox_consumer_key,priority:1;index:idx_outbox_claim,priority:1;index:idx_outbox_processing_lease,priority:1" json:"consumer"`
	EventType         string            `gorm:"size:100;not null;index" json:"event_type"`
	AggregateType     string            `gorm:"size:64;not null;index:idx_outbox_aggregate,priority:1" json:"aggregate_type"`
	AggregateID       string            `gorm:"size:100;not null;index:idx_outbox_aggregate,priority:2" json:"aggregate_id"`
	AggregateRevision int64             `gorm:"not null;index:idx_outbox_aggregate,priority:3;check:chk_outbox_revision,aggregate_revision > 0" json:"aggregate_revision"`
	EventKey          string            `gorm:"size:100;not null;uniqueIndex:uk_outbox_consumer_key,priority:2" json:"event_key"`
	Payload           string            `gorm:"type:text;not null;check:chk_outbox_payload_size,length(CAST(payload AS BLOB)) <= 65536" json:"-"`
	PayloadHash       string            `gorm:"size:64;not null" json:"payload_hash"`
	RequestID         string            `gorm:"size:64;not null;index" json:"request_id"`
	Status            OutboxEventStatus `gorm:"size:20;not null;default:'pending';index:idx_outbox_claim,priority:2;index:idx_outbox_processing_lease,priority:2;check:chk_outbox_status,status IN ('pending','processing','processed','dead_letter')" json:"status"`
	AvailableAt       time.Time         `gorm:"not null;index:idx_outbox_claim,priority:3" json:"available_at"`
	LeaseOwner        string            `gorm:"size:100" json:"lease_owner,omitempty"`
	LeaseToken        string            `gorm:"size:36" json:"-"`
	LeaseExpiresAt    *time.Time        `gorm:"index;index:idx_outbox_processing_lease,priority:3" json:"lease_expires_at,omitempty"`
	AttemptCount      int               `gorm:"not null;default:0;check:chk_outbox_attempt_count,attempt_count >= 0" json:"attempt_count"`
	MaxAttempts       int               `gorm:"not null;default:5;check:chk_outbox_max_attempts,max_attempts > 0" json:"max_attempts"`
	LastErrorCode     string            `gorm:"size:100" json:"last_error_code,omitempty"`
	LastErrorSummary  string            `gorm:"type:text;check:chk_outbox_error_size,length(CAST(last_error_summary AS BLOB)) <= 512" json:"last_error_summary,omitempty"`
	ProcessedAt       *time.Time        `json:"processed_at,omitempty"`
	DeadLetterAt      *time.Time        `json:"dead_letter_at,omitempty"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
}

func (OutboxEvent) TableName() string { return "outbox_event" }

type ConsumerRevision struct {
	ID            string    `gorm:"primaryKey;size:36" json:"id"`
	Consumer      string    `gorm:"size:100;not null;uniqueIndex:uk_consumer_revision,priority:1" json:"consumer"`
	AggregateType string    `gorm:"size:64;not null;uniqueIndex:uk_consumer_revision,priority:2" json:"aggregate_type"`
	AggregateID   string    `gorm:"size:100;not null;uniqueIndex:uk_consumer_revision,priority:3" json:"aggregate_id"`
	LastRevision  int64     `gorm:"not null;check:chk_consumer_last_revision,last_revision > 0" json:"last_revision"`
	LastEventID   string    `gorm:"size:36;not null" json:"last_event_id"`
	RowVersion    int64     `gorm:"not null;default:1;check:chk_consumer_row_version,row_version > 0" json:"row_version"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (ConsumerRevision) TableName() string { return "consumer_revision" }
