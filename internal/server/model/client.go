package model

import "time"

// Client 客户端模型
// 与 Headscale 集成，使用 Headscale User ID 作为主键
type Client struct {
	ID         uint64    `gorm:"primaryKey" json:"id"`                      // Headscale User ID
	Name       string    `gorm:"uniqueIndex;size:100;not null" json:"name"` // 用户名，唯一索引
	Alias      string    `gorm:"size:100" json:"alias"`                     // 用户别名（如：张三）
	SecretHash string    `gorm:"size:255;not null" json:"-"`                // 认证密钥哈希（bcrypt）
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (Client) TableName() string {
	return "client"
}
