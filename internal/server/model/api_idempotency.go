package model

import "time"

type APIIdempotencyStatus string

const (
	APIIdempotencyProcessing APIIdempotencyStatus = "processing"
	APIIdempotencyCompleted  APIIdempotencyStatus = "completed"
	APIIdempotencyFailed     APIIdempotencyStatus = "failed"
)

func (s APIIdempotencyStatus) Valid() bool {
	return s == APIIdempotencyProcessing || s == APIIdempotencyCompleted || s == APIIdempotencyFailed
}

type APIIdempotencyRecord struct {
	ID             string               `gorm:"primaryKey;size:36" json:"id"`
	ActorType      string               `gorm:"size:32;not null;uniqueIndex:uk_api_idempotency,priority:1" json:"actor_type"`
	ActorID        string               `gorm:"size:100;not null;uniqueIndex:uk_api_idempotency,priority:2" json:"actor_id"`
	ScopeType      string               `gorm:"size:32;not null;uniqueIndex:uk_api_idempotency,priority:3" json:"scope_type"`
	ScopeID        string               `gorm:"size:100;not null;uniqueIndex:uk_api_idempotency,priority:4" json:"scope_id"`
	Method         string               `gorm:"size:10;not null;uniqueIndex:uk_api_idempotency,priority:5" json:"method"`
	Route          string               `gorm:"size:200;not null;uniqueIndex:uk_api_idempotency,priority:6" json:"route"`
	KeyHash        string               `gorm:"size:64;not null;uniqueIndex:uk_api_idempotency,priority:7" json:"key_hash"`
	RequestHash    string               `gorm:"size:64;not null" json:"request_hash"`
	Status         APIIdempotencyStatus `gorm:"size:20;not null;default:'processing';index;check:chk_api_idempotency_status,status IN ('processing','completed','failed')" json:"status"`
	ResponseStatus int                  `gorm:"not null;default:0;check:chk_api_idempotency_response_status,response_status >= 0 AND response_status <= 599" json:"response_status,omitempty"`
	ResponseBody   string               `gorm:"type:text;check:chk_api_idempotency_response_size,length(CAST(response_body AS BLOB)) <= 65536" json:"-"`
	ErrorCode      string               `gorm:"size:100" json:"error_code,omitempty"`
	ExpiresAt      time.Time            `gorm:"not null;index" json:"expires_at"`
	CreatedAt      time.Time            `json:"created_at"`
	UpdatedAt      time.Time            `json:"updated_at"`
}

func (APIIdempotencyRecord) TableName() string { return "api_idempotency_record" }
