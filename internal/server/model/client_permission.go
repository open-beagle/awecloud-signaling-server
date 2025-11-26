package model

import "time"

// STCPAccess STCP访问控制表（重命名自ClientPermission）
type STCPAccess struct {
	ID             int64     `gorm:"primaryKey" json:"id"`
	STCPInstanceID int64     `gorm:"not null;index" json:"stcp_instance_id"`
	ClientID       int64     `gorm:"not null;index" json:"client_id"`
	GrantedAt      time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"granted_at"`

	// 关联
	STCPInstance *STCPInstance `gorm:"foreignKey:STCPInstanceID" json:"stcp_instance,omitempty"`
	Client       *Client       `gorm:"foreignKey:ClientID" json:"client,omitempty"`
}

func (STCPAccess) TableName() string {
	return "stcp_access"
}
