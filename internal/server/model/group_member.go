package model

import "time"

type GroupMember struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	GroupID   int64     `gorm:"not null;index;uniqueIndex:idx_group_client" json:"group_id"`
	ClientID  int64     `gorm:"not null;index;uniqueIndex:idx_group_client" json:"client_id"`
	Role      string    `gorm:"size:20;default:'member'" json:"role"` // 'admin', 'member'
	CreatedAt time.Time `json:"created_at"`

	// 关联
	Group  *Group  `gorm:"foreignKey:GroupID" json:"group,omitempty"`
	Client *Client `gorm:"foreignKey:ClientID" json:"client,omitempty"`
}

func (GroupMember) TableName() string {
	return "group_members"
}
