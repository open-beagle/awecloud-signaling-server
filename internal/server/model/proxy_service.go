package model

import "time"

// ProxyService 端口映射服务
// 替代原有的 STCPInstance 和 TCPService
type ProxyService struct {
	ID          int64     `gorm:"primaryKey" json:"id"`
	Name        string    `gorm:"uniqueIndex;size:100;not null" json:"name"` // 服务名称
	AgentID     int64     `gorm:"index;not null" json:"agent_id"`            // 所属 Agent ID
	ListenPort  int       `gorm:"not null" json:"listen_port"`               // 监听端口
	TargetAddr  string    `gorm:"size:255;not null" json:"target_addr"`      // 目标地址，如 192.168.1.100:3306
	Status      string    `gorm:"size:20;default:stopped" json:"status"`     // 状态：running/stopped/error
	Connections int       `gorm:"default:0" json:"connections"`              // 当前连接数
	BytesIn     int64     `gorm:"default:0" json:"bytes_in"`                 // 入站流量（字节）
	BytesOut    int64     `gorm:"default:0" json:"bytes_out"`                // 出站流量（字节）
	Remark      string    `gorm:"size:500" json:"remark"`                    // 备注
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`

	// 关联
	Agent *Agent `gorm:"foreignKey:AgentID" json:"agent,omitempty"`
}

func (ProxyService) TableName() string {
	return "proxy_services"
}

// ProxyService 状态常量
const (
	ProxyStatusRunning = "running"
	ProxyStatusStopped = "stopped"
	ProxyStatusError   = "error"
)
