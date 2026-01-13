package model

import "time"

// DeviceToken 设备令牌模型
// 用于 Desktop 设备的持久化认证，绑定设备指纹
type DeviceToken struct {
	ID                int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	ClientID          int64     `gorm:"index;not null" json:"client_id"`                   // 所属 Client ID
	DeviceToken       string    `gorm:"uniqueIndex;size:100;not null" json:"device_token"` // 设备令牌
	DeviceFingerprint string    `gorm:"index;size:255;not null" json:"device_fingerprint"` // 设备指纹
	DeviceInfo        string    `gorm:"type:text" json:"device_info"`                      // 设备信息 JSON
	LastUsedAt        time.Time `json:"last_used_at"`                                      // 最后使用时间
	ExpiresAt         time.Time `json:"expires_at"`                                        // 过期时间
	Revoked           bool      `gorm:"default:false" json:"revoked"`                      // 是否已撤销
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

func (DeviceToken) TableName() string {
	return "device_token"
}
