package model

import "time"

// ServicePermission 服务访问权限（Desktop 额外授权）
// 用于额外的细粒度授权（如临时授权某人访问 private 服务）
type ServicePermission struct {
	ID        int64      `gorm:"primaryKey" json:"id"`
	ServiceID int64      `gorm:"index;not null" json:"service_id"` // 服务 ID
	ClientID  int64      `gorm:"index;not null" json:"client_id"`  // 被授权的 Client ID
	GrantedBy int64      `gorm:"not null" json:"granted_by"`       // 授权人（Admin ID）
	GrantedAt time.Time  `gorm:"not null" json:"granted_at"`       // 授权时间
	ExpiresAt *time.Time `json:"expires_at"`                       // 过期时间（可选）
	CreatedAt time.Time  `json:"created_at"`

	// 关联
	Service *ProxyService `gorm:"foreignKey:ServiceID" json:"service,omitempty"`
	Client  *Client       `gorm:"foreignKey:ClientID" json:"client,omitempty"`
}

func (ServicePermission) TableName() string {
	return "service_permissions"
}

// IsExpired 检查权限是否已过期
func (p *ServicePermission) IsExpired() bool {
	if p.ExpiresAt == nil {
		return false // 永久授权
	}
	return time.Now().After(*p.ExpiresAt)
}
