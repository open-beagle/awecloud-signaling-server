package model

import "time"

// AclEndpointK8SAPIUserPermission Endpoint K8SAPI 授权 - 用户级别
// 授权某个用户通过 Agent 跳跃到 Endpoint K8S API
type AclEndpointK8SAPIUserPermission struct {
	ID         int64     `gorm:"primaryKey" json:"id"`
	EndpointID string    `gorm:"size:36;not null;index;uniqueIndex:uk_aek8saup" json:"endpoint_id"` // 目标 Endpoint K8SAPI 的 ID
	UserID     uint64    `gorm:"not null;index;uniqueIndex:uk_aek8saup" json:"user_id"`             // 被授权用户 ID
	K8SGroups  string    `gorm:"type:text;not null" json:"k8s_groups"`                              // K8S Impersonation 分组（JSON 数组）
	Namespaces string    `gorm:"type:text;not null" json:"namespaces"`                              // 允许的命名空间（JSON 数组，空数组表示全部）
	Enabled    bool      `gorm:"default:true" json:"enabled"`                                       // 是否启用
	GrantedAt  time.Time `json:"granted_at"`                                                        // 授权时间

	// 关联
	Endpoint *EndpointK8SAPI `gorm:"foreignKey:EndpointID" json:"endpoint,omitempty"`
	User     *User           `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

func (AclEndpointK8SAPIUserPermission) TableName() string {
	return "acl_endpoint_k8sapi_user_permission"
}

// AclEndpointK8SAPIGroupPermission Endpoint K8SAPI 授权 - 分组级别
// 授权某个分组通过 Agent 跳跃到 Endpoint K8S API
type AclEndpointK8SAPIGroupPermission struct {
	ID         int64     `gorm:"primaryKey" json:"id"`
	EndpointID string    `gorm:"size:36;not null;index;uniqueIndex:uk_aek8sagp" json:"endpoint_id"` // 目标 Endpoint K8SAPI 的 ID
	GroupID    int64     `gorm:"not null;index;uniqueIndex:uk_aek8sagp" json:"group_id"`            // 被授权分组 ID
	K8SGroups  string    `gorm:"type:text;not null" json:"k8s_groups"`                              // K8S Impersonation 分组（JSON 数组）
	Namespaces string    `gorm:"type:text;not null" json:"namespaces"`                              // 允许的命名空间（JSON 数组，空数组表示全部）
	Enabled    bool      `gorm:"default:true" json:"enabled"`                                       // 是否启用
	GrantedAt  time.Time `json:"granted_at"`                                                        // 授权时间

	// 关联
	Endpoint *EndpointK8SAPI `gorm:"foreignKey:EndpointID" json:"endpoint,omitempty"`
	Group    *Group          `gorm:"foreignKey:GroupID" json:"group,omitempty"`
}

func (AclEndpointK8SAPIGroupPermission) TableName() string {
	return "acl_endpoint_k8sapi_group_permission"
}
