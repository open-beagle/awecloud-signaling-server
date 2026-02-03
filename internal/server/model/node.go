package model

import "time"

// NodeType 设备类型
type NodeType string

const (
	NodeTypeAgent   NodeType = "agent"   // Agent 设备
	NodeTypeDesktop NodeType = "desktop" // Desktop 设备
)

// Node 设备模型
// 统一 Agent 设备和 Desktop 设备
type Node struct {
	ID              uint64     `gorm:"primaryKey;autoIncrement" json:"id"`                                               // 自增主键
	UserID          uint64     `gorm:"not null;index;uniqueIndex:uk_node_user_type_name,priority:1" json:"user_id"`      // 所属用户 ID
	Name            string     `gorm:"size:100;not null;uniqueIndex:uk_node_user_type_name,priority:3" json:"name"`      // 设备名称
	Type            NodeType   `gorm:"size:20;not null;index;uniqueIndex:uk_node_user_type_name,priority:2" json:"type"` // 类型：agent / desktop
	HeadscaleNodeID uint64     `gorm:"index" json:"headscale_node_id"`                                                   // Headscale Node ID（外部系统 ID）
	IP              string     `gorm:"size:50" json:"ip"`                                                                // 隧道 IP
	Version         string     `gorm:"size:50" json:"version"`                                                           // 版本号
	Hostname        string     `gorm:"size:100" json:"hostname"`                                                         // 主机名
	SystemInfo      string     `gorm:"type:text" json:"system_info"`                                                     // 系统信息 JSON
	SecretHash      string     `gorm:"size:255" json:"-"`                                                                // 设备认证密钥哈希（Desktop 用）
	LastHeartbeat   *time.Time `json:"last_heartbeat"`                                                                   // 最后心跳时间
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`

	// 关联
	User *User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

// NodeSystemInfo 设备系统信息结构（用于 JSON 解析）
type NodeSystemInfo struct {
	OS        string `json:"os"`         // 操作系统
	OSVersion string `json:"os_version"` // 系统版本
	Arch      string `json:"arch"`       // CPU 架构
	Hostname  string `json:"hostname"`   // 主机名
	CPU       string `json:"cpu"`        // CPU 型号
	CPUCores  int    `json:"cpu_cores"`  // CPU 核心数
	MemoryGB  int    `json:"memory_gb"`  // 内存大小（GB）
}

func (Node) TableName() string {
	return "node"
}
