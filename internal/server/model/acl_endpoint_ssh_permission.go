package model

import "time"

// AclEndpointSSHUserPermission Endpoint SSH 授权 - 用户级别
// 授权某个用户通过 Agent 跳跃到 Endpoint SSH
type AclEndpointSSHUserPermission struct {
	ID         int64     `gorm:"primaryKey" json:"id"`
	EndpointID string    `gorm:"size:36;not null;index;uniqueIndex:uk_aesshup" json:"endpoint_id"` // 目标 Endpoint SSH 的 ID
	UserID     uint64    `gorm:"not null;index;uniqueIndex:uk_aesshup" json:"user_id"`             // 被授权用户 ID
	SSHUsers   string    `gorm:"type:text;not null" json:"ssh_users"`                              // 允许的 SSH 登录用户名列表（JSON 数组）
	Enabled    bool      `gorm:"default:true" json:"enabled"`                                      // 是否启用
	GrantedAt  time.Time `json:"granted_at"`                                                       // 授权时间

	// 关联
	Endpoint *Endpoint `gorm:"foreignKey:EndpointID" json:"endpoint,omitempty"`
	User     *User     `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

func (AclEndpointSSHUserPermission) TableName() string {
	return "acl_endpoint_ssh_user_permission"
}

// AclEndpointSSHGroupPermission Endpoint SSH 授权 - 分组级别
// 授权某个分组通过 Agent 跳跃到 Endpoint SSH
type AclEndpointSSHGroupPermission struct {
	ID         int64     `gorm:"primaryKey" json:"id"`
	EndpointID string    `gorm:"size:36;not null;index;uniqueIndex:uk_aesshgp" json:"endpoint_id"` // 目标 Endpoint 的 ID
	GroupID    int64     `gorm:"not null;index;uniqueIndex:uk_aesshgp" json:"group_id"`            // 被授权分组 ID
	SSHUsers   string    `gorm:"type:text;not null" json:"ssh_users"`                              // 允许的 SSH 登录用户名列表（JSON 数组）
	Enabled    bool      `gorm:"default:true" json:"enabled"`                                      // 是否启用
	GrantedAt  time.Time `json:"granted_at"`                                                       // 授权时间

	// 关联
	Endpoint *Endpoint `gorm:"foreignKey:EndpointID" json:"endpoint,omitempty"`
	Group    *Group    `gorm:"foreignKey:GroupID" json:"group,omitempty"`
}

func (AclEndpointSSHGroupPermission) TableName() string {
	return "acl_endpoint_ssh_group_permission"
}
