package model

import "time"

type STCPInstance struct {
	ID           int64     `gorm:"primaryKey" json:"id"`
	InstanceName string    `gorm:"uniqueIndex;size:100;not null" json:"instance_name"`
	AgentID      int64     `gorm:"not null;index" json:"agent_id"`
	SecretKey    string    `gorm:"size:255;not null" json:"secret_key"`
	LocalIP      string    `gorm:"size:50;not null" json:"local_ip"`
	LocalPort    int       `gorm:"not null" json:"local_port"`
	Description  string    `gorm:"type:text" json:"description"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`

	// 关联
	Agent *Agent `gorm:"foreignKey:AgentID" json:"agent,omitempty"`
}

func (STCPInstance) TableName() string {
	return "stcp_instances"
}
