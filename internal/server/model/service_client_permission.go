package model

import "time"

// ServiceClientPermission 服务-用户授权模型
// 记录 ProxyService 与 Client 的授权关系，用于桌面授权
type ServiceClientPermission struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	ServiceID string    `gorm:"size:36;not null;index;uniqueIndex:uk_scp" json:"service_id"` // 服务 ID (UUID)
	ClientID  uint64    `gorm:"not null;index;uniqueIndex:uk_scp" json:"client_id"`          // Client ID (Headscale User ID)
	GrantedAt time.Time `json:"granted_at"`                                                  // 授权时间

	// 关联
	Service *ProxyService `gorm:"foreignKey:ServiceID" json:"service,omitempty"`
	Client  *Client       `gorm:"foreignKey:ClientID" json:"client,omitempty"`
}

func (ServiceClientPermission) TableName() string {
	return "service_client_permission"
}
