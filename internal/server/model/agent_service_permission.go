package model

import "time"

// AgentServicePermission Agent 服务访问权限
// 用于 Agent 间访问授权（如外部 Agent 访问内网 Agent 的服务）
// 同组 Agent 默认可互访，不需要此表记录
// 无分组 Agent 需要通过此表显式授权
type AgentServicePermission struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	AgentID   int64     `gorm:"index;not null" json:"agent_id"`   // 被授权的 Agent ID（访问方）
	ServiceID int64     `gorm:"index;not null" json:"service_id"` // 服务 ID（被访问的服务）
	GrantedBy int64     `gorm:"not null" json:"granted_by"`       // 授权人（Admin ID）
	GrantedAt time.Time `gorm:"not null" json:"granted_at"`       // 授权时间
	CreatedAt time.Time `json:"created_at"`

	// 关联
	Agent   *Agent        `gorm:"foreignKey:AgentID" json:"agent,omitempty"`
	Service *ProxyService `gorm:"foreignKey:ServiceID" json:"service,omitempty"`
}

func (AgentServicePermission) TableName() string {
	return "agent_service_permissions"
}
