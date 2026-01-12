package model

import "time"

// AgentGroup 代理分组模型
// 用于将多个 Agent 组织在一起，便于批量授权
type AgentGroup struct {
	ID          int64     `gorm:"primaryKey" json:"id"`
	Name        string    `gorm:"uniqueIndex;size:100;not null" json:"name"` // 唯一名称
	Alias       string    `gorm:"size:100" json:"alias"`                     // 别名（显示名称）
	Description string    `gorm:"size:500" json:"description"`               // 描述
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`

	// 关联
	Members []*AgentGroupMember `gorm:"foreignKey:GroupID" json:"members,omitempty"`
}

func (AgentGroup) TableName() string {
	return "agent_group"
}
