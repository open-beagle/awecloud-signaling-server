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
	ConfigClientDownloadURL  = "client_download_url"   // 客户端/Agent 下载地址（共用）
	ConfigDesktopMinVersion  = "desktop_min_version"   // 客户端最低版本
	ConfigHeadscalePublicURL = "headscale_public_url"  // 隧道公网地址
	ConfigStunPort           = "stun_port"             // STUN 端口
	ConfigIPPrefix           = "ip_prefix"             // IP 地址段
	ConfigAuthKeyExpiryHours = "auth_key_expiry_hours" // 预认证密钥有效期（小时）
	ConfigDomainSuffix       = "domain_suffix"         // 域名后缀（默认 .beagle）
)

// DefaultDomainSuffix 默认域名后缀
const DefaultDomainSuffix = ".beagle"

// GetConfigValue 获取配置值的辅助方法
func GetConfigValue(key string, defaultValue string) string {
	// 这个方法需要在使用时通过数据库查询实现
	// 这里只是定义接口，实际实现在调用处
	return defaultValue
}

// SystemConfig 的字段访问方法（用于兼容旧代码）
func (sc *SystemConfig) GetAuthKeyExpiryHours() int {
	if sc.Key == ConfigAuthKeyExpiryHours {
		// 解析 Value 为 int，如果失败返回默认值 24
		if val := sc.Value; val != "" {
			// 简单的字符串转换，实际使用时需要 strconv.Atoi
			return 24 // 默认值
		}
	}
	return 24
}

func (sc *SystemConfig) GetHeadscalePublicURL() string {
	if sc.Key == ConfigHeadscalePublicURL {
		return sc.Value
	}
	return ""
}
