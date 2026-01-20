package model

import "time"

// Group 统一分组模型
// 合并 AgentGroup 和 ClientGroup，统一管理所有用户分组
type Group struct {
	ID          int64     `gorm:"primaryKey" json:"id"`
	Name        string    `gorm:"uniqueIndex;size:100;not null" json:"name"` // 唯一名称
	Alias       string    `gorm:"size:100" json:"alias"`                     // 别名（显示名称）
	Description string    `gorm:"size:500" json:"description"`               // 描述
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`

	// 关联
	Members []*GroupMember `gorm:"foreignKey:GroupID" json:"members,omitempty"`
}

func (Group) TableName() string {
	return "group"
}
