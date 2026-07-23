package model

import "time"

const (
	LegacySourceAgentNode = "agent_node"
	LegacySourceEndpoint  = "endpoint"
)

// LegacyResourceClaim records an explicit Tenant assignment for an existing
// legacy access object. It does not create a Resource or alter legacy access.
type LegacyResourceClaim struct {
	ID          string    `gorm:"primaryKey;size:36" json:"id"`
	SourceType  string    `gorm:"size:30;not null;uniqueIndex:uk_legacy_source,priority:1;index" json:"source_type"`
	SourceID    string    `gorm:"size:100;not null;uniqueIndex:uk_legacy_source,priority:2" json:"source_id"`
	TenantID    string    `gorm:"size:36;not null;index" json:"tenant_id"`
	Status      string    `gorm:"size:20;not null;default:'active';index" json:"status"`
	ClaimedBy   int64     `gorm:"index" json:"claimed_by"`
	ClaimReason string    `gorm:"size:500" json:"claim_reason,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (LegacyResourceClaim) TableName() string { return "legacy_resource_claim" }
