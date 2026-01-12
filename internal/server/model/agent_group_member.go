package model

import "time"

// AgentGroupMember 代理分组成员关系模型
// 记录 Agent 与 AgentGroup 的多对多关系
type AgentGroupMember struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	GroupID   int64     `gorm:"not null;index;uniqueIndex:uk_agm" json:"group_id"` // 分组 ID
	AgentID   uint64    `gorm:"not null;index;uniqueIndex:uk_agm" json:"agent_id"` // Agent ID (Headscale User ID)
	CreatedAt time.Time `json:"created_at"`

	// 关联
	Group *AgentGroup `gorm:"foreignKey:GroupID" json:"group,omitempty"`
	Agent *Agent      `gorm:"foreignKey:AgentID" json:"agent,omitempty"`
}

func (AgentGroupMember) TableName() string {
	return "agent_group_member"
}
