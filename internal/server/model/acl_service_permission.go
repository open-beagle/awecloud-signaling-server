package model

import "time"

// AclServiceUserPermission 服务授权 - 用户级别
// 授权某个用户访问某个服务
type AclServiceUserPermission struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	ServiceID string    `gorm:"size:36;not null;index;uniqueIndex:uk_asup" json:"service_id"` // 服务 ID (UUID)
	UserID    uint64    `gorm:"not null;index;uniqueIndex:uk_asup" json:"user_id"`            // 被授权用户 ID
	GrantedAt time.Time `json:"granted_at"`                                                   // 授权时间

	// 关联
	Service *ProxyService `gorm:"foreignKey:ServiceID" json:"service,omitempty"`
	User    *User         `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

func (AclServiceUserPermission) TableName() string {
	return "acl_service_user_permission"
}

// AclServiceGroupPermission 服务授权 - 分组级别
// 授权某个分组访问某个服务
type AclServiceGroupPermission struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	ServiceID string    `gorm:"size:36;not null;index;uniqueIndex:uk_asgp" json:"service_id"` // 服务 ID (UUID)
	GroupID   int64     `gorm:"not null;index;uniqueIndex:uk_asgp" json:"group_id"`           // 被授权分组 ID
	GrantedAt time.Time `json:"granted_at"`                                                   // 授权时间

	// 关联
	Service *ProxyService `gorm:"foreignKey:ServiceID" json:"service,omitempty"`
	Group   *Group        `gorm:"foreignKey:GroupID" json:"group,omitempty"`
}

func (AclServiceGroupPermission) TableName() string {
	return "acl_service_group_permission"
}
