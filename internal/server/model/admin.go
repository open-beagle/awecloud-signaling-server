package model

import "time"

// Admin 管理员模型
type Admin struct {
	ID           int64     `gorm:"primaryKey" json:"id"`
	Username     string    `gorm:"uniqueIndex;size:50;not null" json:"username"` // 用户名，唯一索引
	PasswordHash string    `gorm:"size:255;not null" json:"-"`                   // 密码（加密存储）
	Role         string    `gorm:"size:20;not null;default:'admin'" json:"role"` // 角色：admin / viewer
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (Admin) TableName() string {
	return "admin"
}
