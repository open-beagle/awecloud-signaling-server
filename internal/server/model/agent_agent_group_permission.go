package model

import "time"

// AgentAgentGroupPermission Agent-AgentGroup 授权模型
// 记录 Agent 与 AgentGroup 的授权关系，授权后分组内所有 Agent 可访问目标 Agent 下所有服务
type AgentAgentGroupPermission struct {
	ID            int64     `gorm:"primaryKey" json:"id"`
	TargetAgentID uint64    `gorm:"not null;index;uniqueIndex:uk_aagp" json:"target_agent_id"` // 目标 Agent ID
	GroupID       int64     `gorm:"not null;index;uniqueIndex:uk_aagp" json:"group_id"`        // AgentGroup ID
	GrantedAt     time.Time `json:"granted_at"`                                                // 授权时间

	// 关联
	TargetAgent *Agent      `gorm:"foreignKey:TargetAgentID" json:"target_agent,omitempty"`
	Group       *AgentGroup `gorm:"foreignKey:GroupID" json:"group,omitempty"`
}

func (AgentAgentGroupPermission) TableName() string {
	return "agent_agent_group_permission"
}
