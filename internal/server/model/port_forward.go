package model

import "time"

// PortForward 端口转发配置
// Agent 访问其他 Agent 服务的端口转发配置
type PortForward struct {
	ID              string    `gorm:"primaryKey;size:36" json:"id"`                                                                // UUID
	AgentID         uint64    `gorm:"index;not null;uniqueIndex:uk_forward_source_agent" json:"agent_id"`                          // 所属 Agent ID (Headscale User ID)
	TargetServiceID string    `gorm:"size:36;not null" json:"target_service_id"`                                                   // 目标服务 ID (ProxyService UUID)
	SourceAddr      string    `gorm:"size:255;not null;column:source_addr;uniqueIndex:uk_forward_source_agent" json:"source_addr"` // 源地址（局域网 IP:端口，如 192.168.1.100:13306）
	TargetAddr      string    `gorm:"size:255;not null" json:"target_addr"`                                                        // 目标地址（VPN 地址，如 100.64.0.1:3306）
	Enabled         bool      `gorm:"default:true" json:"enabled"`                                                                 // 是否启用（管理员控制）
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`

	// 关联
	Agent         *Agent        `gorm:"foreignKey:AgentID" json:"agent,omitempty"`
	TargetService *ProxyService `gorm:"foreignKey:TargetServiceID" json:"target_service,omitempty"`
}

func (PortForward) TableName() string {
	return "port_forward"
}
