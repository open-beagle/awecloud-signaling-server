package model

import "time"

type Agent struct {
	ID            int64      `gorm:"primaryKey" json:"id"`
	AgentName     string     `gorm:"uniqueIndex;size:100;not null" json:"agent_name"`
	AgentToken    string     `gorm:"size:255;not null" json:"agent_token,omitempty"`
	Description   string     `gorm:"size:500" json:"description"`
	Status        string     `gorm:"size:20;default:offline" json:"status"` // online, offline
	Version       string     `gorm:"size:50" json:"version"`                // Agent版本
	LastHeartbeat *time.Time `json:"last_heartbeat"`

	// Tailscale 相关字段
	TailscaleIP    string     `gorm:"size:50" json:"tailscale_ip"`           // Tailscale IP，如 100.64.0.10
	TsConnected    bool       `gorm:"default:false" json:"ts_connected"`     // Tailscale 连接状态
	TsConnType     string     `gorm:"size:20" json:"ts_conn_type"`           // 连接方式：p2p / derp
	TsRegisteredAt *time.Time `json:"ts_registered_at"`                      // Tailscale 注册时间
	TsNodeKey      string     `gorm:"size:255" json:"ts_node_key,omitempty"` // Tailscale 节点密钥（内部使用）

	// 分组管理（安全架构）
	// 相同 GroupName 的 Agent 自动归为一组，同组 Agent 可以互访所有端口
	// 空字符串表示无分组，该 Agent 只能访问显式授权的服务
	GroupName string `gorm:"size:100;index" json:"group_name"` // 分组名称

	// 网络信息（Agent 心跳上报）
	LanIP        string `gorm:"size:50" json:"lan_ip"`        // 局域网 IP
	LanGateway   string `gorm:"size:50" json:"lan_gateway"`   // 网关地址
	LanInterface string `gorm:"size:50" json:"lan_interface"` // 网卡名称
	RuntimeEnv   string `gorm:"size:20" json:"runtime_env"`   // 运行环境: native/docker/kubernetes
	Hostname     string `gorm:"size:128" json:"hostname"`     // 主机名

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// NetworkInfo 网络信息（用于 API 响应）
type NetworkInfo struct {
	LanIP        string `json:"lan_ip"`
	LanGateway   string `json:"lan_gateway"`
	LanInterface string `json:"lan_interface"`
	RuntimeEnv   string `json:"runtime_env"`
	Hostname     string `json:"hostname"`
}

// GetNetworkInfo 获取网络信息
func (a *Agent) GetNetworkInfo() *NetworkInfo {
	if a.LanIP == "" {
		return nil
	}
	return &NetworkInfo{
		LanIP:        a.LanIP,
		LanGateway:   a.LanGateway,
		LanInterface: a.LanInterface,
		RuntimeEnv:   a.RuntimeEnv,
		Hostname:     a.Hostname,
	}
}

func (Agent) TableName() string {
	return "agents"
}
