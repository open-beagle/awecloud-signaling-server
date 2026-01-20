package model

import "time"

// ProxyService 端口映射服务
// Agent 用户提供的端口映射服务，用于通过隧道暴露内部服务
type ProxyService struct {
	ID         string    `gorm:"primaryKey;size:36" json:"id"`                                                // UUID
	Name       string    `gorm:"size:100;not null;uniqueIndex:uk_proxy_name_user" json:"name"`                // 服务名称
	Alias      string    `gorm:"size:100" json:"alias"`                                                       // 别名
	UserID     uint64    `gorm:"index:idx_proxy_user;not null;uniqueIndex:uk_proxy_name_user" json:"user_id"` // 所属用户（Agent 角色），外键
	SourceAddr string    `gorm:"size:255;not null;column:source_addr" json:"source_addr"`                     // 源地址（VPN IP:端口，如 100.64.0.1:80）
	TargetAddr string    `gorm:"size:255;not null" json:"target_addr"`                                        // 目标地址（局域网地址，如 192.168.1.10:80）
	Enabled    bool      `gorm:"not null" json:"enabled"`                                                     // 是否启用（管理员控制）
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`

	// 关联
	User *User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

func (ProxyService) TableName() string {
	return "proxy_service"
}
