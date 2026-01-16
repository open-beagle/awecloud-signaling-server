package model

import "time"

// AgentAgentPermission Agent-Agent 授权模型
// 记录 Agent 与 Agent 的授权关系，授权后源 Agent 可访问目标 Agent 下所有服务
type AgentAgentPermission struct {
	ID            int64     `gorm:"primaryKey" json:"id"`
	TargetAgentID uint64    `gorm:"not null;index;uniqueIndex:uk_aap" json:"target_agent_id"` // 目标 Agent ID
	SourceAgentID uint64    `gorm:"not null;index;uniqueIndex:uk_aap" json:"source_agent_id"` // 源 Agent ID
	GrantedAt     time.Time `json:"granted_at"`                                               // 授权时间

	// 关联
	TargetAgent *Agent `gorm:"foreignKey:TargetAgentID" json:"target_agent,omitempty"`
	SourceAgent *Agent `gorm:"foreignKey:SourceAgentID" json:"source_agent,omitempty"`
}

func (AgentAgentPermission) TableName() string {
	return "agent_agent_permission"
}
