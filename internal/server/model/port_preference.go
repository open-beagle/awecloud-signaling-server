package model

import "time"

// PortPreference 端口偏好模型
// 存储用户对特定服务的本地端口偏好设置
type PortPreference struct {
	ID             int64     `gorm:"primaryKey" json:"id"`
	ClientID       int64     `gorm:"index;not null" json:"client_id"`        // 客户端 ID
	STCPInstanceID int64     `gorm:"index;not null" json:"stcp_instance_id"` // 端口映射服务 ID
	PreferredPort  int       `gorm:"not null" json:"preferred_port"`         // 偏好的本地端口
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (PortPreference) TableName() string {
	return "port_preferences"
}
