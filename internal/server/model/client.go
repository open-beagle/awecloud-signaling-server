package model

import "time"

type Client struct {
	ID           int64     `gorm:"primaryKey" json:"id"`
	ClientID     string    `gorm:"uniqueIndex;size:100;not null" json:"client_id"`
	ClientSecret string    `gorm:"size:255;not null" json:"client_secret,omitempty"`
	TunnelToken  string    `gorm:"size:255" json:"tunnel_token,omitempty"` // 每个Client独立的隧道Token
	Enabled      bool      `gorm:"default:true" json:"enabled"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (Client) TableName() string {
	return "clients"
}
