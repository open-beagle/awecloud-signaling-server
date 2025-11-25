package model

import "time"

type ClientPermission struct {
	ID             int64     `gorm:"primaryKey" json:"id"`
	ClientID       int64     `gorm:"not null;index" json:"client_id"`
	STCPInstanceID int64     `gorm:"not null;index" json:"stcp_instance_id"`
	CreatedAt      time.Time `json:"created_at"`

	// 关联
	Client       *Client       `gorm:"foreignKey:ClientID" json:"client,omitempty"`
	STCPInstance *STCPInstance `gorm:"foreignKey:STCPInstanceID" json:"stcp_instance,omitempty"`
}

func (ClientPermission) TableName() string {
	return "client_permissions"
}
