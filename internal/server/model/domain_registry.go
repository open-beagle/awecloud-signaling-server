package model

import "time"

// DomainType 域名类型
type DomainType string

const (
	DomainTypeAgentSSH          DomainType = "agent_ssh"           // Agent 本机 SSH
	DomainTypeAgentK8SAPI       DomainType = "agent_k8sapi"        // Agent 本机 K8S API
	DomainTypeAgentK8SService   DomainType = "agent_k8s_service"   // Agent 本机 K8S Service
	DomainTypeAgentService      DomainType = "agent_service"       // Agent 手动端口映射
	DomainTypeEndpointSSH       DomainType = "endpoint_ssh"        // Endpoint SSH 跳跃
	DomainTypeEndpointK8SAPI    DomainType = "endpoint_k8sapi"     // Endpoint K8S API 跳跃
	DomainTypeEndpointK8SService DomainType = "endpoint_k8s_service" // Endpoint K8S Service 跳跃
)

// DomainStatus 域名状态
type DomainStatus string

const (
	DomainStatusOnline  DomainStatus = "online"
	DomainStatusOffline DomainStatus = "offline"
)

// DomainRegistry 域名注册表
// 记录域名到 Agent/Endpoint 的映射关系
type DomainRegistry struct {
	ID          int64        `gorm:"primaryKey" json:"id"`
	Domain      string       `gorm:"size:255;not null;uniqueIndex" json:"domain"`                // 完整域名（如 pg.yygl.beijing.k8s）
	Type        DomainType   `gorm:"size:30;not null;index" json:"type"`                         // 域名类型
	AgentUserID uint64       `gorm:"not null;index" json:"agent_user_id"`                        // 所属 Agent 的 User ID
	EndpointID  string       `gorm:"size:36" json:"endpoint_id,omitempty"`                       // 关联的 Endpoint ID（Endpoint 类型时）
	TargetIP    string       `gorm:"size:50" json:"target_ip,omitempty"`                         // 目标 IP
	TargetPort  int          `json:"target_port,omitempty"`                                      // 目标端口
	Namespace   string       `gorm:"size:100" json:"namespace,omitempty"`                        // K8S 命名空间（K8S 类型时）
	ServiceName string       `gorm:"size:100" json:"service_name,omitempty"`                     // K8S Service 名称（K8S 类型时）
	Status      DomainStatus `gorm:"size:20;not null;default:'online'" json:"status"`            // online/offline
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`

	// 关联
	AgentUser *User `gorm:"foreignKey:AgentUserID" json:"agent_user,omitempty"`
}

func (DomainRegistry) TableName() string {
	return "domain_registry"
}
