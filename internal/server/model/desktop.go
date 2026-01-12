package model

import "time"

// Desktop 桌面设备模型
// 与 Headscale 集成，使用 Headscale Node ID 作为主键
// 一个 Client 可以拥有多个 Desktop 设备
type Desktop struct {
	ID         uint64     `gorm:"primaryKey" json:"id"`            // Headscale Node ID
	ClientID   uint64     `gorm:"index;not null" json:"client_id"` // 所属 Client ID
	Name       string     `gorm:"size:100;not null" json:"name"`   // 设备名称，Desktop 收集的主机名
	Alias      string     `gorm:"size:100" json:"alias"`           // 设备别名
	SecretHash string     `gorm:"size:255;not null" json:"-"`      // 设备认证密钥哈希（bcrypt）
	SystemInfo string     `gorm:"type:text" json:"system_info"`    // 系统信息 JSON
	IP         string     `gorm:"size:50" json:"ip"`               // 隧道 IP
	LastOnline *time.Time `json:"last_online"`                     // 最后在线时间
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// DesktopSystemInfo 桌面系统信息结构（用于 JSON 解析）
type DesktopSystemInfo struct {
	OS        string `json:"os"`         // 操作系统：windows / darwin / linux
	OSVersion string `json:"os_version"` // 系统版本
	Arch      string `json:"arch"`       // CPU 架构：amd64 / arm64
	Hostname  string `json:"hostname"`   // 主机名
	CPU       string `json:"cpu"`        // CPU 型号
	CPUCores  int    `json:"cpu_cores"`  // CPU 核心数
	MemoryGB  int    `json:"memory_gb"`  // 内存大小（GB）
}

func (Desktop) TableName() string {
	return "desktop"
}
