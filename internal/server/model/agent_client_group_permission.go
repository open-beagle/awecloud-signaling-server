package model

import "time"

// AgentClientGroupPermission Agent-ClientGroup 授权模型
// 记录 Agent 与 ClientGroup 的授权关系，授权后分组内所有 Client 可访问 Agent 下所有服务
type AgentClientGroupPermission struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	AgentID   uint64    `gorm:"not null;index;uniqueIndex:uk_acgp" json:"agent_id"` // Agent ID
	GroupID   int64     `gorm:"not null;index;uniqueIndex:uk_acgp" json:"group_id"` // ClientGroup ID
	GrantedAt time.Time `json:"granted_at"`                                         // 授权时间

	// 关联
	Agent *Agent       `gorm:"foreignKey:AgentID" json:"agent,omitempty"`
	Group *ClientGroup `gorm:"foreignKey:GroupID" json:"group,omitempty"`
}

func (AgentClientGroupPermission) TableName() string {
	return "agent_client_group_permission"
}
