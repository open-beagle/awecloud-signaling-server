package model

import (
	"time"
)

// DeviceToken 设备令牌表
type DeviceToken struct {
	ID                int64     `gorm:"primaryKey" json:"id"`
	ClientID          int64     `gorm:"not null;index:idx_device_tokens_client_id" json:"client_id"`
	DeviceToken       string    `gorm:"type:text;not null;uniqueIndex:idx_device_tokens_token" json:"device_token"`
	DeviceFingerprint string    `gorm:"type:text;not null;index:idx_device_tokens_fingerprint" json:"device_fingerprint"`
	DeviceInfo        string    `gorm:"type:text" json:"device_info"` // JSON格式存储设备信息
	CreatedAt         time.Time `gorm:"autoCreateTime" json:"created_at"`
	LastUsedAt        time.Time `json:"last_used_at"`
	ExpiresAt         time.Time `gorm:"not null;index:idx_device_tokens_expires_at" json:"expires_at"`
	Revoked           bool      `gorm:"default:false;index:idx_device_tokens_revoked" json:"revoked"`

	// 关联
	Client Client `gorm:"foreignKey:ClientID;constraint:OnDelete:CASCADE" json:"-"`
}

// TableName 指定表名
func (DeviceToken) TableName() string {
	return "device_tokens"
}
