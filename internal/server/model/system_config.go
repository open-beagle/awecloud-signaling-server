package model

import "time"

// SystemConfig 系统配置表
// 存储系统级别的配置项，可在 Web 界面动态修改
type SystemConfig struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	Key       string    `gorm:"uniqueIndex;size:100;not null" json:"key"` // 配置键，唯一索引
	Value     string    `gorm:"type:text;not null" json:"value"`          // 配置值
	UpdatedAt time.Time `json:"updated_at"`
}

func (SystemConfig) TableName() string {
	return "system_config"
}

// 系统配置键常量
const (
	ConfigClientDownloadURL  = "client_download_url"   // 客户端下载地址
	ConfigDesktopMinVersion  = "desktop_min_version"   // 客户端最低版本
	ConfigHeadscalePublicURL = "headscale_public_url"  // 隧道公网地址
	ConfigStunPort           = "stun_port"             // STUN 端口
	ConfigIPPrefix           = "ip_prefix"             // IP 地址段
	ConfigAuthKeyExpiryHours = "auth_key_expiry_hours" // 预认证密钥有效期（小时）
)
