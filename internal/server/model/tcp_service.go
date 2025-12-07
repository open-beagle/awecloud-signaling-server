package model

import (
	"time"

	"gorm.io/gorm"
)

// TCPService TCP服务实例
type TCPService struct {
	ID            uint           `json:"id" gorm:"primaryKey"`
	ServiceName   string         `json:"service_name" gorm:"uniqueIndex;not null"`
	AgentID       uint           `json:"agent_id" gorm:"not null;index"`
	LocalIP       string         `json:"local_ip" gorm:"not null"`
	LocalPort     int            `json:"local_port" gorm:"not null"`
	RemotePort    int            `json:"remote_port" gorm:"not null;index"`
	Description   string         `json:"description"`
	Enabled       bool           `json:"enabled" gorm:"default:false;index"`
	AccessControl string         `json:"access_control" gorm:"default:'public'"`
	IPWhitelist   string         `json:"ip_whitelist" gorm:"type:text"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	DeletedAt     gorm.DeletedAt `json:"-" gorm:"index"`
}

// TableName 指定表名
func (TCPService) TableName() string {
	return "tcp_services"
}

// TCPServiceWithAgent TCP服务实例（包含Agent信息）
type TCPServiceWithAgent struct {
	TCPService
	AgentName string `json:"agent_name" gorm:"-"`
}

// TCPServiceAccessLog TCP服务访问日志
type TCPServiceAccessLog struct {
	ID              uint      `json:"id" gorm:"primaryKey"`
	TCPServiceID    uint      `json:"tcp_service_id" gorm:"not null;index"`
	ClientIP        string    `json:"client_ip" gorm:"not null;index"`
	Action          string    `json:"action" gorm:"not null;index"`
	BytesSent       int64     `json:"bytes_sent" gorm:"default:0"`
	BytesReceived   int64     `json:"bytes_received" gorm:"default:0"`
	DurationSeconds int       `json:"duration_seconds"`
	CreatedAt       time.Time `json:"created_at" gorm:"index"`
}

// TableName 指定表名
func (TCPServiceAccessLog) TableName() string {
	return "tcp_service_access_logs"
}
