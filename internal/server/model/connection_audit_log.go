package model

import (
	"time"
)

// ConnectionAuditLog 连接审计日志表
type ConnectionAuditLog struct {
	ID                int64     `gorm:"primaryKey" json:"id"`
	ClientPKID        int64     `gorm:"column:client_id;not null;index:idx_audit_logs_client_id" json:"client_id"`
	STCPInstancePKID  int64     `gorm:"column:stcp_instance_id;not null;index:idx_audit_logs_instance_id" json:"stcp_instance_id"`
	Action            string    `gorm:"type:varchar(20);not null;index:idx_audit_logs_action" json:"action"` // connect, disconnect
	LocalPort         int       `json:"local_port"`
	DeviceFingerprint string    `gorm:"type:text" json:"device_fingerprint"`
	DeviceInfo        string    `gorm:"type:text" json:"device_info"` // JSON格式存储设备信息
	IPAddress         string    `gorm:"type:varchar(45)" json:"ip_address"`
	ServerAddress     string    `gorm:"type:varchar(100)" json:"server_address"` // Desktop连接的Server地址
	Success           bool      `gorm:"not null;index:idx_audit_logs_success" json:"success"`
	ErrorMessage      string    `gorm:"type:text" json:"error_message"`
	CreatedAt         time.Time `gorm:"autoCreateTime;index:idx_audit_logs_created_at" json:"created_at"`

	// 关联
	Client Client `gorm:"foreignKey:ClientPKID;references:ID" json:"-"`
}

// TableName 指定表名
func (ConnectionAuditLog) TableName() string {
	return "connection_audit_logs"
}
