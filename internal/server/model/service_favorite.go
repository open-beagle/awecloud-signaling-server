package model

import "time"

// ServiceFavorite 服务收藏模型
// 存储用户收藏的服务列表
type ServiceFavorite struct {
	ID             int64     `gorm:"primaryKey" json:"id"`
	ClientID       int64     `gorm:"index;not null" json:"client_id"`        // 客户端 ID
	STCPInstanceID int64     `gorm:"index;not null" json:"stcp_instance_id"` // 端口映射服务 ID
	LocalPort      int       `gorm:"default:0" json:"local_port"`            // 本地端口（可选）
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (ServiceFavorite) TableName() string {
	return "service_favorites"
}
