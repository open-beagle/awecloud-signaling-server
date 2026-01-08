package model

import (
	"time"
)

// PortPreference 端口偏好表（废弃，保留兼容）
type PortPreference struct {
	ID             int64     `gorm:"primaryKey" json:"id"`
	ClientID       int64     `gorm:"not null;index:idx_port_preferences_client_id" json:"client_id"`
	STCPInstanceID int64     `gorm:"not null;index:idx_port_preferences_instance_id" json:"stcp_instance_id"`
	PreferredPort  int       `gorm:"not null" json:"preferred_port"`
	CreatedAt      time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time `gorm:"autoUpdateTime" json:"updated_at"`

	// 关联
	Client Client `gorm:"foreignKey:ClientID;constraint:OnDelete:CASCADE" json:"-"`
}

// TableName 指定表名
func (PortPreference) TableName() string {
	return "port_preferences"
}
