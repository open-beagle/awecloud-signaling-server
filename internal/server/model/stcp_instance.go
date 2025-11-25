package model

import "time"

type STCPInstance struct {
	ID           int64     `gorm:"primaryKey" json:"id"`
	AgentID      int64     `gorm:"not null;index" json:"agent_id"`
	InstanceName string    `gorm:"size:100;not null" json:"instance_name"`
	ServiceType  string    `gorm:"size:20;not null" json:"service_type"` // tcp, udp
	LocalIP      string    `gorm:"size:50;not null" json:"local_ip"`
	LocalPort    int       `gorm:"not null" json:"local_port"`
	SecretKey    string    `gorm:"size:255;not null" json:"secret_key"`
	ServerName   string    `gorm:"uniqueIndex;size:200;not null" json:"server_name"`
	Status       string    `gorm:"size:20;default:inactive" json:"status"` // active, inactive
	CreatedAt    time.Time `json:"created_at"`

	// 关联
	Agent *Agent `gorm:"foreignKey:AgentID" json:"agent,omitempty"`
}

func (STCPInstance) TableName() string {
	return "stcp_instances"
}
