package model

import "time"

// AclSSHUserPermission SSH 授权 - 用户级别
// 授权某个用户 SSH 访问某个 Agent
type AclSSHUserPermission struct {
	ID           int64     `gorm:"primaryKey" json:"id"`
	TargetUserID uint64    `gorm:"not null;index;uniqueIndex:uk_asshup" json:"target_user_id"` // 目标用户 ID（被 SSH 的 Agent）
	UserID       uint64    `gorm:"not null;index;uniqueIndex:uk_asshup" json:"user_id"`        // 被授权用户 ID
	SSHUsers     string    `gorm:"type:text;not null" json:"ssh_users"`                        // 批准的 Linux 用户名列表（JSON 数组）
	Enabled      bool      `gorm:"default:true" json:"enabled"`                                // 是否启用
	GrantedAt    time.Time `json:"granted_at"`                                                 // 授权时间

	// 关联
	TargetUser *User `gorm:"foreignKey:TargetUserID" json:"target_user,omitempty"`
	User       *User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

func (AclSSHUserPermission) TableName() string {
	return "acl_ssh_user_permission"
}

// AclSSHGroupPermission SSH 授权 - 分组级别
// 授权某个分组 SSH 访问某个 Agent
type AclSSHGroupPermission struct {
	ID           int64     `gorm:"primaryKey" json:"id"`
	TargetUserID uint64    `gorm:"not null;index;uniqueIndex:uk_asshgp" json:"target_user_id"` // 目标用户 ID（被 SSH 的 Agent）
	GroupID      int64     `gorm:"not null;index;uniqueIndex:uk_asshgp" json:"group_id"`       // 被授权分组 ID
	SSHUsers     string    `gorm:"type:text;not null" json:"ssh_users"`                        // 批准的 Linux 用户名列表（JSON 数组）
	Enabled      bool      `gorm:"default:true" json:"enabled"`                                // 是否启用
	GrantedAt    time.Time `json:"granted_at"`                                                 // 授权时间

	// 关联
	TargetUser *User  `gorm:"foreignKey:TargetUserID" json:"target_user,omitempty"`
	Group      *Group `gorm:"foreignKey:GroupID" json:"group,omitempty"`
}

func (AclSSHGroupPermission) TableName() string {
	return "acl_ssh_group_permission"
}
