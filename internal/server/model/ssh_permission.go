package model

import "time"

// SSHClientPermission Desktop → Agent SSH 授权
// 一条记录表示一个 Desktop 用户可以 SSH 到一个 Agent
type SSHClientPermission struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	ClientID  uint64    `gorm:"index;not null" json:"client_id"`     // Desktop/Client ID
	AgentID   uint64    `gorm:"index;not null" json:"agent_id"`      // 目标 Agent ID
	SSHUsers  string    `gorm:"type:text;not null" json:"ssh_users"` // 批准的 Linux 用户名列表（JSON 数组）
	Enabled   bool      `gorm:"default:true" json:"enabled"`         // 是否启用
	CreatedAt time.Time `json:"created_at"`

	// 关联
	Client *Client `gorm:"foreignKey:ClientID" json:"client,omitempty"`
	Agent  *Agent  `gorm:"foreignKey:AgentID" json:"agent,omitempty"`
}

func (SSHClientPermission) TableName() string {
	return "ssh_client_permissions"
}

// SSHClientGroupPermission Desktop 分组 → Agent SSH 授权
// 一条记录表示一个 Desktop 分组内的所有用户可以 SSH 到一个 Agent
type SSHClientGroupPermission struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	GroupID   int64     `gorm:"index;not null" json:"group_id"`      // Desktop 分组 ID
	AgentID   uint64    `gorm:"index;not null" json:"agent_id"`      // 目标 Agent ID
	SSHUsers  string    `gorm:"type:text;not null" json:"ssh_users"` // 批准的 Linux 用户名列表（JSON 数组）
	Enabled   bool      `gorm:"default:true" json:"enabled"`         // 是否启用
	CreatedAt time.Time `json:"created_at"`

	// 关联
	Group *ClientGroup `gorm:"foreignKey:GroupID" json:"group,omitempty"`
	Agent *Agent       `gorm:"foreignKey:AgentID" json:"agent,omitempty"`
}

func (SSHClientGroupPermission) TableName() string {
	return "ssh_client_group_permissions"
}
