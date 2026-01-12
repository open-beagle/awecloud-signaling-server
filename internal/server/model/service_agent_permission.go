package model

import "time"

// ServiceAgentPermission 服务-代理授权模型
// 记录 ProxyService 与 Agent 的授权关系，用于代理授权
type ServiceAgentPermission struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	ServiceID string    `gorm:"size:36;not null;index;uniqueIndex:uk_sap" json:"service_id"` // 服务 ID (UUID)
	AgentID   uint64    `gorm:"not null;index;uniqueIndex:uk_sap" json:"agent_id"`           // Agent ID (Headscale User ID)
	GrantedAt time.Time `json:"granted_at"`                                                  // 授权时间

	// 关联
	Service *ProxyService `gorm:"foreignKey:ServiceID" json:"service,omitempty"`
	Agent   *Agent        `gorm:"foreignKey:AgentID" json:"agent,omitempty"`
}

func (ServiceAgentPermission) TableName() string {
	return "service_agent_permission"
}
