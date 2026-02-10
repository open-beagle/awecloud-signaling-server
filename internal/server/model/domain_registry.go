package model

import "time"

// DomainType 域名能力类型
type DomainType string

const (
	DomainTypeSSH    DomainType = "ssh"    // SSH 能力
	DomainTypeK8SAPI DomainType = "k8sapi" // K8S API 能力
	DomainTypeK8SSVC DomainType = "k8ssvc" // K8S Service 能力
)

// DomainStatus 域名状态
type DomainStatus string

const (
	DomainStatusOnline  DomainStatus = "online"
	DomainStatusOffline DomainStatus = "offline"
)

// DomainRegistry 域名注册表
// 记录域名到 Node/Endpoint 的映射关系
// 同一域名可有多条记录（不同 node_id），支持负载均衡
type DomainRegistry struct {
	ID          int64        `gorm:"primaryKey" json:"id"`
	Domain      string       `gorm:"size:255;not null;index" json:"domain"`                      // 完整域名（如 beagle-242.beijing.beagle）
	Type        DomainType   `gorm:"size:30;not null;index" json:"type"`                         // 能力类型：ssh / k8sapi / k8ssvc
	UserID      uint64       `gorm:"not null;index" json:"user_id"`                              // 所属 User ID（Agent User 或 Client User）
	NodeID      uint64       `gorm:"index" json:"node_id,omitempty"`                             // 关联的 Node ID（Node 注册时）
	EndpointID  string       `gorm:"size:36" json:"endpoint_id,omitempty"`                       // 关联的 Endpoint ID（Endpoint 注册时）
	TargetIP    string       `gorm:"size:50" json:"target_ip,omitempty"`                         // 目标 IP（Node 的 Tailscale IP 或 ClusterIP）
	TargetPort  int          `json:"target_port,omitempty"`                                      // 目标端口
	Namespace   string       `gorm:"size:100" json:"namespace,omitempty"`                        // K8S 命名空间（k8ssvc 类型时）
	ServiceName string       `gorm:"size:100" json:"service_name,omitempty"`                     // K8S Service 名称（k8ssvc 类型时）
	Status      DomainStatus `gorm:"size:20;not null;default:'online'" json:"status"`            // online/offline
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`

	// 关联
	User *User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

func (DomainRegistry) TableName() string {
	return "domain_registry"
}
