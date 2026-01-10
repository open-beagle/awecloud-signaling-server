package model

import "time"

// AgentTailscaleState Agent 的 Tailscale 状态存储
// 用于集中存储 Agent 的 Tailscale 节点状态，支持无状态化部署
type AgentTailscaleState struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	AgentID   int64     `gorm:"uniqueIndex;not null" json:"agent_id"` // 外键关联 agents.id
	StateData []byte    `gorm:"type:blob" json:"-"`                   // Tailscale 状态数据（压缩后）
	UpdatedAt time.Time `json:"updated_at"`
	CreatedAt time.Time `json:"created_at"`
}

func (AgentTailscaleState) TableName() string {
	return "agent_tailscale_states"
}
