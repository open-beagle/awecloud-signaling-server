package model

import "time"

// AclUserUserPermission 用户授权 - 用户级别
// 授权某个用户访问某个 Agent 用户的所有端口
type AclUserUserPermission struct {
	ID            int64     `gorm:"primaryKey" json:"id"`
	TargetUserID  uint64    `gorm:"not null;index;uniqueIndex:uk_auup" json:"target_user_id"`  // 目标用户 ID（被访问的 Agent）
	GrantedUserID uint64    `gorm:"not null;index;uniqueIndex:uk_auup" json:"granted_user_id"` // 被授权用户 ID
	GrantedAt     time.Time `json:"granted_at"`                                                // 授权时间

	// 关联
	TargetUser  *User `gorm:"foreignKey:TargetUserID" json:"target_user,omitempty"`
	GrantedUser *User `gorm:"foreignKey:GrantedUserID" json:"granted_user,omitempty"`
}

func (AclUserUserPermission) TableName() string {
	return "acl_user_user_permission"
}

// AclUserGroupPermission 用户授权 - 分组级别
// 授权某个分组访问某个 Agent 用户的所有端口
type AclUserGroupPermission struct {
	ID           int64     `gorm:"primaryKey" json:"id"`
	TargetUserID uint64    `gorm:"not null;index;uniqueIndex:uk_augp" json:"target_user_id"` // 目标用户 ID（被访问的 Agent）
	GroupID      int64     `gorm:"not null;index;uniqueIndex:uk_augp" json:"group_id"`       // 被授权分组 ID
	GrantedAt    time.Time `json:"granted_at"`                                               // 授权时间

	// 关联
	TargetUser *User  `gorm:"foreignKey:TargetUserID" json:"target_user,omitempty"`
	Group      *Group `gorm:"foreignKey:GroupID" json:"group,omitempty"`
}

func (AclUserGroupPermission) TableName() string {
	return "acl_user_group_permission"
}
