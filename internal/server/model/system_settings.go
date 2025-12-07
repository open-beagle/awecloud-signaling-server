package model

import "time"

// SystemSettings 系统设置（Key-Value存储）
type SystemSettings struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	SettingKey   string    `gorm:"type:varchar(100);uniqueIndex;not null" json:"setting_key"` // 设置键
	SettingValue string    `gorm:"type:text" json:"setting_value"`                            // 设置值
	Description  string    `gorm:"type:text" json:"description"`                              // 描述
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// TableName 指定表名
func (SystemSettings) TableName() string {
	return "system_settings"
}
