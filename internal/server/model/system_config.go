package model

import "time"

// SystemConfig 系统配置
type SystemConfig struct {
	ID                uint      `gorm:"primaryKey" json:"id"`
	ClientDownloadURL string    `gorm:"type:text" json:"client_download_url"` // 客户端下载地址
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// TableName 指定表名
func (SystemConfig) TableName() string {
	return "system_config"
}
