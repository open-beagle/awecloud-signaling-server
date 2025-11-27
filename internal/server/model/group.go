package model

import "time"

type Group struct {
	ID          int64     `gorm:"primaryKey" json:"id"`
	Name        string    `gorm:"uniqueIndex;size:100;not null" json:"name"`
	Description string    `gorm:"type:text" json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`

	// 关联
	Members []*GroupMember `gorm:"foreignKey:GroupID" json:"members,omitempty"`
}

func (Group) TableName() string {
	return "groups"
}
