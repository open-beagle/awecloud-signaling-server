package model

import "time"

// AclGroupUserPermission 分组授权 - 用户级别
// 授权某个用户访问某个分组标记的所有端口
type AclGroupUserPermission struct {
	ID            int64     `gorm:"primaryKey" json:"id"`
	TargetGroupID int64     `gorm:"not null;index;uniqueIndex:uk_agup" json:"target_group_id"` // 目标分组 ID（被访问的分组）
	UserID        uint64    `gorm:"not null;index;uniqueIndex:uk_agup" json:"user_id"`         // 被授权用户 ID
	GrantedAt     time.Time `json:"granted_at"`                                                // 授权时间

	// 关联
	TargetGroup *Group `gorm:"foreignKey:TargetGroupID" json:"target_group,omitempty"`
	User        *User  `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

func (AclGroupUserPermission) TableName() string {
	return "acl_group_user_permission"
}

// AclGroupGroupPermission 分组授权 - 分组级别
// 授权某个分组访问某个分组标记的所有端口
type AclGroupGroupPermission struct {
	ID            int64     `gorm:"primaryKey" json:"id"`
	TargetGroupID int64     `gorm:"not null;index;uniqueIndex:uk_aggp" json:"target_group_id"` // 目标分组 ID（被访问的分组）
	GroupID       int64     `gorm:"not null;index;uniqueIndex:uk_aggp" json:"group_id"`        // 被授权分组 ID
	GrantedAt     time.Time `json:"granted_at"`                                                // 授权时间

	// 关联
	TargetGroup  *Group `gorm:"foreignKey:TargetGroupID" json:"target_group,omitempty"`
	GrantedGroup *Group `gorm:"foreignKey:GroupID" json:"granted_group,omitempty"`
}

func (AclGroupGroupPermission) TableName() string {
	return "acl_group_group_permission"
}
