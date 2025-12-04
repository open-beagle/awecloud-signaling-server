package model

import (
	"time"
)

// ServiceFavorite 服务收藏表
type ServiceFavorite struct {
	ID             int64     `gorm:"primaryKey" json:"id"`
	ClientID       int64     `gorm:"not null;index:idx_service_favorites_client_id" json:"client_id"`
	STCPInstanceID int64     `gorm:"not null;index:idx_service_favorites_instance_id;uniqueIndex:idx_client_instance" json:"stcp_instance_id"`
	LocalPort      int       `gorm:"default:0" json:"local_port"` // 用户偏好的本地端口
	CreatedAt      time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time `gorm:"autoUpdateTime" json:"updated_at"`

	// 关联
	Client       Client       `gorm:"foreignKey:ClientID;constraint:OnDelete:CASCADE" json:"-"`
	STCPInstance STCPInstance `gorm:"foreignKey:STCPInstanceID;constraint:OnDelete:CASCADE" json:"-"`
}

// TableName 指定表名
func (ServiceFavorite) TableName() string {
	return "service_favorites"
}
