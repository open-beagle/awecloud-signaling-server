package model

import "time"

type Client struct {
	ID           int64  `gorm:"primaryKey" json:"id"`
	ClientID     string `gorm:"uniqueIndex;size:100;not null" json:"client_id"`
	ClientSecret string `gorm:"size:255;not null" json:"-"`             // 不序列化到 JSON
	TunnelToken  string `gorm:"size:255" json:"tunnel_token,omitempty"` // 每个Client独立的隧道Token
	Enabled      bool   `gorm:"default:true" json:"enabled"`

	// Tailscale 相关字段
	TailscaleIP string `gorm:"size:50" json:"tailscale_ip"` // Tailscale IP (100.65.x.x 网段)

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (Client) TableName() string {
	return "clients"
}

// DesktopInstance Desktop 实例（支持多设备）
// 同一员工可在多设备登录，每设备独立 IP
type DesktopInstance struct {
	ID                int64     `gorm:"primaryKey" json:"id"`
	ClientID          int64     `gorm:"index;not null" json:"client_id"`                   // 所属 Client
	DeviceToken       string    `gorm:"uniqueIndex;size:255;not null" json:"device_token"` // 设备 Token
	DeviceFingerprint string    `gorm:"size:255;index" json:"device_fingerprint"`          // 设备指纹
	DeviceName        string    `gorm:"size:100" json:"device_name"`                       // 设备名称
	TailscaleIP       string    `gorm:"size:50" json:"tailscale_ip"`                       // 固定 Tailscale IP
	TailscaleNodeID   string    `gorm:"size:255" json:"tailscale_node_id,omitempty"`       // Headscale 节点 ID
	Online            bool      `gorm:"default:false" json:"online"`                       // 在线状态
	LastSeenAt        time.Time `json:"last_seen_at"`                                      // 最后在线时间
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`

	// 关联
	Client *Client `gorm:"foreignKey:ClientID" json:"client,omitempty"`
}

func (DesktopInstance) TableName() string {
	return "desktop_instances"
}

// DesktopService Desktop 暴露的服务（如 RDP、SMB 等）
type DesktopService struct {
	ID                int64     `gorm:"primaryKey" json:"id"`
	DesktopInstanceID int64     `gorm:"index;not null" json:"desktop_instance_id"` // 所属 Desktop 实例
	Name              string    `gorm:"size:100;not null" json:"name"`             // 服务名称
	Port              int       `gorm:"not null" json:"port"`                      // 监听端口
	Protocol          string    `gorm:"size:10;default:tcp" json:"protocol"`       // tcp/udp
	Description       string    `gorm:"size:500" json:"description"`               // 描述
	AllowSelf         bool      `gorm:"default:true" json:"allow_self"`            // 是否允许自己的其他设备访问
	AllowAll          bool      `gorm:"default:false" json:"allow_all"`            // 是否允许所有人访问
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`

	// 关联
	DesktopInstance *DesktopInstance `gorm:"foreignKey:DesktopInstanceID" json:"desktop_instance,omitempty"`
}

func (DesktopService) TableName() string {
	return "desktop_services"
}
