package model

import "time"

// ProxyService 端口映射服务
// Agent 提供的端口映射服务，用于通过隧道暴露内部服务
type ProxyService struct {
	ID         string    `gorm:"primaryKey;size:36" json:"id"`                                // UUID
	Name       string    `gorm:"size:100;not null;uniqueIndex:uk_proxy_service" json:"name"`  // 服务名称
	Alias      string    `gorm:"size:100" json:"alias"`                                       // 别名
	AgentID    uint64    `gorm:"index;not null;uniqueIndex:uk_proxy_service" json:"agent_id"` // 所属 Agent ID (Headscale User ID)
	TargetAddr string    `gorm:"size:255;not null" json:"target_addr"`                        // 目标地址，如 192.168.1.100:3306
	ListenAddr string    `gorm:"size:255;not null" json:"listen_addr"`                        // 监听地址
	Enabled    bool      `gorm:"default:true" json:"enabled"`                                 // 是否启用
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`

	// 关联
	Agent *Agent `gorm:"foreignKey:AgentID" json:"agent,omitempty"`
}

func (ProxyService) TableName() string {
	return "proxy_service"
}
