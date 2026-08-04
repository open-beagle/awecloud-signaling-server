package model

import "time"

type TechnicalResourceDeployTokenStatus string

const (
	TechnicalResourceDeployTokenPending  TechnicalResourceDeployTokenStatus = "pending"
	TechnicalResourceDeployTokenConsumed TechnicalResourceDeployTokenStatus = "consumed"
	TechnicalResourceDeployTokenRevoked  TechnicalResourceDeployTokenStatus = "revoked"
)

// TechnicalResourceDeployToken is an Agent/Endpoint admission credential. The
// runtime user is execution compatibility data, not the credential owner.
type TechnicalResourceDeployToken struct {
	ID                  string                             `gorm:"primaryKey;size:36" json:"id"`
	TechnicalResourceID string                             `gorm:"size:36;not null;index" json:"technical_resource_id"`
	Token               string                             `gorm:"size:500;not null;uniqueIndex" json:"-"`
	Name                string                             `gorm:"size:100;not null" json:"name"`
	RuntimeUserID       uint64                             `gorm:"not null;index" json:"-"`
	Status              TechnicalResourceDeployTokenStatus `gorm:"size:20;not null;default:'pending';index" json:"status"`
	DeviceFingerprint   string                             `gorm:"size:255" json:"device_fingerprint,omitempty"`
	ExpiresAt           *time.Time                         `json:"expires_at,omitempty"`
	ConsumedAt          *time.Time                         `json:"consumed_at,omitempty"`
	RevokedAt           *time.Time                         `json:"revoked_at,omitempty"`
	CreatedByUserID     uint64                             `gorm:"not null;index" json:"created_by_user_id"`
	CreatedAt           time.Time                          `json:"created_at"`
	UpdatedAt           time.Time                          `json:"updated_at"`

	TechnicalResource *TechnicalResource `gorm:"foreignKey:TechnicalResourceID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
}

func (TechnicalResourceDeployToken) TableName() string { return "technical_resource_deploy_token" }

func (t *TechnicalResourceDeployToken) CanConsume(now time.Time) bool {
	return t != nil && t.Status == TechnicalResourceDeployTokenPending && (t.ExpiresAt == nil || !now.After(*t.ExpiresAt))
}
