package model

import (
	"time"

	"gorm.io/gorm"
)

// STCPVisitor STCP访问（visitor端）
type STCPVisitor struct {
	ID          uint           `json:"id" gorm:"primaryKey"`
	VisitorName string         `json:"visitor_name" gorm:"uniqueIndex:idx_agent_visitor;not null"`
	AgentName   string         `json:"agent_name" gorm:"uniqueIndex:idx_agent_visitor;not null;index"`
	ServerName  string         `json:"server_name" gorm:"not null"`
	SecretKey   string         `json:"secret_key" gorm:"not null"`
	BindAddr    string         `json:"bind_addr" gorm:"default:'127.0.0.1'"`
	BindPort    int            `json:"bind_port" gorm:"not null"`
	Description string         `json:"description"`
	Enabled     bool           `json:"enabled" gorm:"default:false;index"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `json:"-" gorm:"index"`
}

// TableName 指定表名
func (STCPVisitor) TableName() string {
	return "stcp_visitors"
}

// STCPVisitorWithAgent STCP访问（包含Agent信息）
// 注意：AgentName已经在STCPVisitor中，这里保留此类型以保持一致性
type STCPVisitorWithAgent struct {
	STCPVisitor
}
