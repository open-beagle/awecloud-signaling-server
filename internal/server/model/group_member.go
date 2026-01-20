package model

import "time"

// GroupMember 分组成员关系模型
// 记录 User 与 Group 的多对多关系
type GroupMember struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	GroupID   int64     `gorm:"not null;index;uniqueIndex:uk_gm" json:"group_id"` // 分组 ID
	UserID    uint64    `gorm:"not null;index;uniqueIndex:uk_gm" json:"user_id"`  // 用户 ID (Headscale User ID)
	CreatedAt time.Time `json:"created_at"`

	// 关联
	Group *Group `gorm:"foreignKey:GroupID" json:"group,omitempty"`
	User  *User  `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

func (GroupMember) TableName() string {
	return "group_member"
}
