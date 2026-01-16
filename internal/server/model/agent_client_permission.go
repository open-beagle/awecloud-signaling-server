package model

import "time"

// AgentClientPermission Agent-Client 授权模型
// 记录 Agent 与 Client 的授权关系，授权后 Client 可访问 Agent 下所有服务
type AgentClientPermission struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	AgentID   uint64    `gorm:"not null;index;uniqueIndex:uk_acp" json:"agent_id"`  // Agent ID
	ClientID  uint64    `gorm:"not null;index;uniqueIndex:uk_acp" json:"client_id"` // Client ID
	GrantedAt time.Time `json:"granted_at"`                                         // 授权时间

	// 关联
	Agent  *Agent  `gorm:"foreignKey:AgentID" json:"agent,omitempty"`
	Client *Client `gorm:"foreignKey:ClientID" json:"client,omitempty"`
}

func (AgentClientPermission) TableName() string {
	return "agent_client_permission"
}
