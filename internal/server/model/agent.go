package model

import "time"

type Agent struct {
	ID            int64      `gorm:"primaryKey" json:"id"`
	AgentName     string     `gorm:"uniqueIndex;size:100;not null" json:"agent_name"`
	AgentToken    string     `gorm:"size:255;not null" json:"agent_token,omitempty"`
	Description   string     `gorm:"size:500" json:"description"`
	Status        string     `gorm:"size:20;default:offline" json:"status"` // online, offline
	Version       string     `gorm:"size:50" json:"version"`                // Agent版本
	LastHeartbeat *time.Time `json:"last_heartbeat"`

	// Tailscale 相关字段
	TailscaleIP    string     `gorm:"size:50" json:"tailscale_ip"`           // Tailscale IP，如 100.64.0.10
	TsConnected    bool       `gorm:"default:false" json:"ts_connected"`     // Tailscale 连接状态
	TsConnType     string     `gorm:"size:20" json:"ts_conn_type"`           // 连接方式：p2p / derp
	TsRegisteredAt *time.Time `json:"ts_registered_at"`                      // Tailscale 注册时间
	TsNodeKey      string     `gorm:"size:255" json:"ts_node_key,omitempty"` // Tailscale 节点密钥（内部使用）

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (Agent) TableName() string {
	return "agents"
}
