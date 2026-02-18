package model

import "time"

// AclEndpointK8SServiceUserPermission Endpoint K8SService 授权 - 用户级别
// 授权某个用户通过 Agent 跳跃到 Endpoint K8S Service
type AclEndpointK8SServiceUserPermission struct {
	ID           int64     `gorm:"primaryKey" json:"id"`
	EndpointID   string    `gorm:"size:36;not null;index;uniqueIndex:uk_aek8ssup" json:"endpoint_id"` // 目标 Endpoint K8SService 的 ID
	UserID       uint64    `gorm:"not null;index;uniqueIndex:uk_aek8ssup" json:"user_id"`             // 被授权用户 ID
	Namespaces   string    `gorm:"type:text;not null" json:"namespaces"`                              // 允许的命名空间（JSON 数组，空数组表示全部）
	ServiceNames string    `gorm:"type:text;not null" json:"service_names"`                           // 允许的 Service 名称（JSON 数组，空数组表示全部）
	Enabled      bool      `gorm:"default:true" json:"enabled"`                                       // 是否启用
	GrantedAt    time.Time `json:"granted_at"`                                                        // 授权时间

	// 关联
	Endpoint *Endpoint `gorm:"foreignKey:EndpointID" json:"endpoint,omitempty"`
	User     *User     `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

func (AclEndpointK8SServiceUserPermission) TableName() string {
	return "acl_endpoint_k8sservice_user_permission"
}

// AclEndpointK8SServiceGroupPermission Endpoint K8SService 授权 - 分组级别
// 授权某个分组通过 Agent 跳跃到 Endpoint K8S Service
type AclEndpointK8SServiceGroupPermission struct {
	ID           int64     `gorm:"primaryKey" json:"id"`
	EndpointID   string    `gorm:"size:36;not null;index;uniqueIndex:uk_aek8ssgp" json:"endpoint_id"` // 目标 Endpoint K8SService 的 ID
	GroupID      int64     `gorm:"not null;index;uniqueIndex:uk_aek8ssgp" json:"group_id"`            // 被授权分组 ID
	Namespaces   string    `gorm:"type:text;not null" json:"namespaces"`                              // 允许的命名空间（JSON 数组，空数组表示全部）
	ServiceNames string    `gorm:"type:text;not null" json:"service_names"`                           // 允许的 Service 名称（JSON 数组，空数组表示全部）
	Enabled      bool      `gorm:"default:true" json:"enabled"`                                       // 是否启用
	GrantedAt    time.Time `json:"granted_at"`                                                        // 授权时间

	// 关联
	Endpoint *Endpoint `gorm:"foreignKey:EndpointID" json:"endpoint,omitempty"`
	Group    *Group    `gorm:"foreignKey:GroupID" json:"group,omitempty"`
}

func (AclEndpointK8SServiceGroupPermission) TableName() string {
	return "acl_endpoint_k8sservice_group_permission"
}
