package model

import "time"

// Visitor 状态常量
const (
	VisitorStatusStopped = "stopped"
	VisitorStatusRunning = "running"
)

// Visitor 访问者模型
// 用于 Agent 用户访问其他 Agent 提供的服务
type Visitor struct {
	ID              int64     `gorm:"primaryKey" json:"id"`
	Name            string    `gorm:"size:100;not null" json:"name"`           // 名称
	UserID          int64     `gorm:"index;not null" json:"user_id"`           // 所属用户 ID（Agent 角色）
	ListenPort      int       `gorm:"not null" json:"listen_port"`             // 本地监听端口
	TargetServiceID int64     `gorm:"index;not null" json:"target_service_id"` // 目标服务 ID
	TargetAddr      string    `gorm:"size:255" json:"target_addr"`             // 目标地址（Tailscale IP:Port）
	Status          string    `gorm:"size:20;default:'stopped'" json:"status"` // 状态：stopped/running
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`

	// 关联
	User *User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

func (Visitor) TableName() string {
	return "visitor"
}

// VisitorWithDetails 带详情的 Visitor
type VisitorWithDetails struct {
	Visitor
	TargetServiceName string `json:"target_service_name"`
	TargetUserName    string `json:"target_user_name"`
}
