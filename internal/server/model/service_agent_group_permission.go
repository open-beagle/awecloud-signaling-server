package model

import "time"

// ServiceAgentGroupPermission 服务-代理分组授权模型
// 记录 ProxyService 与 AgentGroup 的授权关系，用于代理授权的分组授权
type ServiceAgentGroupPermission struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	ServiceID string    `gorm:"size:36;not null;index;uniqueIndex:uk_sagp" json:"service_id"` // 服务 ID (UUID)
	GroupID   int64     `gorm:"not null;index;uniqueIndex:uk_sagp" json:"group_id"`           // 代理分组 ID
	GrantedAt time.Time `json:"granted_at"`                                                   // 授权时间

	// 关联
	Service *ProxyService `gorm:"foreignKey:ServiceID" json:"service,omitempty"`
	Group   *AgentGroup   `gorm:"foreignKey:GroupID" json:"group,omitempty"`
}

func (ServiceAgentGroupPermission) TableName() string {
	return "service_agent_group_permission"
}
