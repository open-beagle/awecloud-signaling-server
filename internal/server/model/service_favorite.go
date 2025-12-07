package model

import (
	"time"
)

// ServiceFavorite 服务收藏表
type ServiceFavorite struct {
	ID             int64     `gorm:"primaryKey" json:"id"`
	ClientID       int64     `gorm:"column:client_id;not null;index:idx_service_favorites_client_id;uniqueIndex:idx_client_instance" json:"client_id"`
	STCPInstanceID int64     `gorm:"column:stcp_instance_id;not null;index:idx_service_favorites_instance_id;uniqueIndex:idx_client_instance" json:"stcp_instance_id"`
	LocalPort      int       `gorm:"default:0" json:"local_port"` // 用户偏好的本地端口
	CreatedAt      time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time `gorm:"autoUpdateTime" json:"updated_at"`

	// 关联
	// 使用 ClientPKID 避免与 Client.ClientID 字段名冲突
	Client       Client       `gorm:"foreignKey:ClientID;references:ID;constraint:OnDelete:CASCADE" json:"-"`
	STCPInstance STCPInstance `gorm:"foreignKey:STCPInstanceID;references:ID;constraint:OnDelete:CASCADE" json:"-"`
}

// TableName 指定表名
func (ServiceFavorite) TableName() string {
	return "service_favorites"
}
