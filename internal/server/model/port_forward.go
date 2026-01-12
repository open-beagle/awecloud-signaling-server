package model

import "time"

// PortForward 端口转发配置
// Agent 访问其他 Agent 服务的端口转发配置
type PortForward struct {
	ID              string    `gorm:"primaryKey;size:36" json:"id"`                               // UUID
	Name            string    `gorm:"size:100;not null;uniqueIndex:uk_port_forward" json:"name"`  // 名称
	Alias           string    `gorm:"size:100" json:"alias"`                                      // 别名
	AgentID         uint64    `gorm:"index;not null;uniqueIndex:uk_port_forward" json:"agent_id"` // 所属 Agent ID (Headscale User ID)
	TargetServiceID string    `gorm:"size:36;not null" json:"target_service_id"`                  // 目标服务 ID (ProxyService UUID)
	TargetAddr      string    `gorm:"size:255;not null" json:"target_addr"`                       // 目标地址
	ListenAddr      string    `gorm:"size:255;not null" json:"listen_addr"`                       // 监听地址
	Enabled         bool      `gorm:"default:true" json:"enabled"`                                // 是否启用
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`

	// 关联
	Agent         *Agent        `gorm:"foreignKey:AgentID" json:"agent,omitempty"`
	TargetService *ProxyService `gorm:"foreignKey:TargetServiceID" json:"target_service,omitempty"`
}

func (PortForward) TableName() string {
	return "port_forward"
}
