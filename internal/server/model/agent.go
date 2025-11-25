package model

import "time"

type Agent struct {
	ID            int64      `gorm:"primaryKey" json:"id"`
	AgentName     string     `gorm:"uniqueIndex;size:100;not null" json:"agent_name"`
	AgentToken    string     `gorm:"size:255;not null" json:"agent_token,omitempty"`
	Status        string     `gorm:"size:20;default:offline" json:"status"` // online, offline
	LastHeartbeat *time.Time `json:"last_heartbeat"`
	CreatedAt     time.Time  `json:"created_at"`
}

func (Agent) TableName() string {
	return "agents"
}
