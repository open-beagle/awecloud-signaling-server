package model

import "time"

// Visitor 端口访问服务
// Agent 在局域网 IP 上监听端口，将流量通过 Tailscale VPN 转发到其他节点暴露的服务
type Visitor struct {
	ID              int64  `gorm:"primaryKey" json:"id"`
	Name            string `gorm:"size:100;not null" json:"name"`           // Visitor 名称
	AgentID         int64  `gorm:"index;not null" json:"agent_id"`          // 访问方 Agent ID
	ListenPort      int    `gorm:"not null" json:"listen_port"`             // 本地监听端口
	TargetServiceID int64  `gorm:"index;not null" json:"target_service_id"` // 目标服务 ID
	TargetAddr      string `gorm:"size:128;not null" json:"target_addr"`    // 目标地址（冗余，便于查询）
	Status          string `gorm:"size:20;default:stopped" json:"status"`   // 状态：running/stopped/error
	Connections     int    `gorm:"default:0" json:"connections"`            // 当前连接数
	BytesIn         int64  `gorm:"default:0" json:"bytes_in"`               // 入站流量（字节）
	BytesOut        int64  `gorm:"default:0" json:"bytes_out"`              // 出站流量（字节）

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// 关联
	Agent         *Agent        `gorm:"foreignKey:AgentID" json:"agent,omitempty"`
	TargetService *ProxyService `gorm:"foreignKey:TargetServiceID" json:"target_service,omitempty"`
}

func (Visitor) TableName() string {
	return "visitors"
}

// VisitorWithDetails Visitor 详情（包含关联数据）
type VisitorWithDetails struct {
	Visitor
	TargetAgentName   string `json:"target_agent_name"`
	TargetServiceName string `json:"target_service_name"`
}

// Visitor 状态常量
const (
	VisitorStatusRunning = "running"
	VisitorStatusStopped = "stopped"
	VisitorStatusError   = "error"
)
