package model

import "time"

// ClientGroupMember 用户分组成员关系模型
// 记录 Client 与 ClientGroup 的多对多关系
type ClientGroupMember struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	GroupID   int64     `gorm:"not null;index;uniqueIndex:uk_cgm" json:"group_id"`  // 分组 ID
	ClientID  uint64    `gorm:"not null;index;uniqueIndex:uk_cgm" json:"client_id"` // Client ID (Headscale User ID)
	CreatedAt time.Time `json:"created_at"`

	// 关联
	Group  *ClientGroup `gorm:"foreignKey:GroupID" json:"group,omitempty"`
	Client *Client      `gorm:"foreignKey:ClientID" json:"client,omitempty"`
}

func (ClientGroupMember) TableName() string {
	return "client_group_member"
}
