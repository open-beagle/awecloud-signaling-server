package model

import "time"

// ServiceClientGroupPermission 服务-用户分组授权模型
// 记录 ProxyService 与 ClientGroup 的授权关系，用于桌面授权的分组授权
type ServiceClientGroupPermission struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	ServiceID string    `gorm:"size:36;not null;index;uniqueIndex:uk_scgp" json:"service_id"` // 服务 ID (UUID)
	GroupID   int64     `gorm:"not null;index;uniqueIndex:uk_scgp" json:"group_id"`           // 用户分组 ID
	GrantedAt time.Time `json:"granted_at"`                                                   // 授权时间

	// 关联
	Service *ProxyService `gorm:"foreignKey:ServiceID" json:"service,omitempty"`
	Group   *ClientGroup  `gorm:"foreignKey:GroupID" json:"group,omitempty"`
}

func (ServiceClientGroupPermission) TableName() string {
	return "service_client_group_permission"
}
