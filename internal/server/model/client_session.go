package model

import "time"

// ClientSession Client会话表
type ClientSession struct {
	ID           int64     `gorm:"primaryKey" json:"id"`
	ClientID     int64     `gorm:"not null;index" json:"client_id"`
	SessionToken string    `gorm:"uniqueIndex;size:255;not null" json:"session_token"`
	ExpiresAt    time.Time `gorm:"not null;index" json:"expires_at"`
	CreatedAt    time.Time `json:"created_at"`

	// 关联
	Client *Client `gorm:"foreignKey:ClientID" json:"client,omitempty"`
}

func (ClientSession) TableName() string {
	return "client_sessions"
}
