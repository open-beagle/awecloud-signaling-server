package model

import "time"

// SystemConfig 系统配置
type SystemConfig struct {
	ID                uint   `gorm:"primaryKey" json:"id"`
	ClientDownloadURL string `gorm:"type:text" json:"client_download_url"`                        // 客户端下载地址
	DesktopMinVersion string `gorm:"type:varchar(20);default:'1.0.0'" json:"desktop_min_version"` // Desktop 最低支持版本

	// Tailscale 网段配置（可在 Web 界面动态修改，覆盖配置文件默认值）
	AgentCIDR      string `gorm:"type:varchar(20);default:'100.64.0.0/16'" json:"agent_cidr"`      // Agent 网段
	AgentIPStart   string `gorm:"type:varchar(20);default:'100.64.0.1'" json:"agent_ip_start"`     // Agent IP 起始
	AgentIPEnd     string `gorm:"type:varchar(20);default:'100.64.255.254'" json:"agent_ip_end"`   // Agent IP 结束
	DesktopCIDR    string `gorm:"type:varchar(20);default:'100.65.0.0/16'" json:"desktop_cidr"`    // Desktop 网段
	DesktopIPStart string `gorm:"type:varchar(20);default:'100.65.0.1'" json:"desktop_ip_start"`   // Desktop IP 起始
	DesktopIPEnd   string `gorm:"type:varchar(20);default:'100.65.255.254'" json:"desktop_ip_end"` // Desktop IP 结束
	ServerCIDR     string `gorm:"type:varchar(20);default:'100.66.0.0/16'" json:"server_cidr"`     // Server 网段
	ServerIPStart  string `gorm:"type:varchar(20);default:'100.66.0.1'" json:"server_ip_start"`    // Server IP 起始
	ServerIPEnd    string `gorm:"type:varchar(20);default:'100.66.255.254'" json:"server_ip_end"`  // Server IP 结束

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName 指定表名
func (SystemConfig) TableName() string {
	return "system_config"
}
