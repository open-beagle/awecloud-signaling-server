package model

import "time"

type Client struct {
	ID           int64      `gorm:"primaryKey" json:"id"`
	ClientID     string     `gorm:"uniqueIndex;size:100;not null" json:"client_id"`
	ClientSecret string     `gorm:"size:255;not null" json:"client_secret,omitempty"`
	Status       string     `gorm:"size:20;default:active" json:"status"` // active, disabled
	IsOnline     bool       `gorm:"default:false" json:"is_online"`
	LastSeen     *time.Time `json:"last_seen"`
	CreatedAt    time.Time  `json:"created_at"`
}

func (Client) TableName() string {
	return "clients"
}
