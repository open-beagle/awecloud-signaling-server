package model

import "time"

// Agent 代理模型
// 与 Headscale 集成，使用 Headscale User ID 作为主键
type Agent struct {
	ID            uint64     `gorm:"primaryKey" json:"id"`                      // Headscale User ID
	Name          string     `gorm:"uniqueIndex;size:100;not null" json:"name"` // 唯一名称
	Alias         string     `gorm:"size:100" json:"alias"`                     // 别名（显示名称）
	SecretHash    string     `gorm:"size:255;not null" json:"-"`                // 密钥哈希（不序列化）
	Version       string     `gorm:"size:50" json:"version"`                    // Agent 版本
	SystemInfo    string     `gorm:"type:text" json:"system_info"`              // 系统信息 JSON
	LastHeartbeat *time.Time `json:"last_heartbeat"`                            // 最后心跳时间
	NodeID        uint64     `json:"node_id"`                                   // Headscale Node ID
	IP            string     `gorm:"size:50" json:"ip"`                         // 隧道 IP
	SSHEnabled    bool       `gorm:"default:false" json:"ssh_enabled"`          // 是否启用 SSH

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SystemInfoData 系统信息结构（用于 JSON 解析）
type SystemInfoData struct {
	OS        string `json:"os"`
	OSVersion string `json:"os_version"`
	Arch      string `json:"arch"`
	Hostname  string `json:"hostname"`
	CPU       string `json:"cpu"`
	CPUCores  int    `json:"cpu_cores"`
	MemoryGB  int    `json:"memory_gb"`
}

func (Agent) TableName() string {
	return "agent"
}
