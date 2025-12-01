package model

import "time"

// STCPAccess STCP访问控制表（重命名自ClientPermission）
type STCPAccess struct {
	ID             int64     `gorm:"primaryKey" json:"id"`
	STCPInstanceID int64     `gorm:"not null;index:idx_stcp_client" json:"stcp_instance_id"`
	ClientID       int64     `gorm:"not null;index:idx_stcp_client" json:"client_id"`
	CreatedAt      time.Time `json:"created_at"`

	// 关联
	STCPInstance *STCPInstance `gorm:"foreignKey:STCPInstanceID" json:"stcp_instance,omitempty"`
	Client       *Client       `gorm:"foreignKey:ClientID" json:"client"`
}

func (STCPAccess) TableName() string {
	return "stcp_accesses"
}
