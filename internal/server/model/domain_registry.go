package model

import (
	"encoding/json"
	"time"
)

// DomainType 域名能力类型
type DomainType string

const (
	DomainTypeSSH    DomainType = "ssh"    // SSH 能力
	DomainTypeK8SAPI DomainType = "k8sapi" // K8S API 能力
)

// DomainStatus 域名状态
type DomainStatus string

const (
	DomainStatusOnline  DomainStatus = "online"
	DomainStatusOffline DomainStatus = "offline"
)

// DomainRegistry 域名注册表
// 记录域名到 Node/Endpoint 的映射关系
// 唯一约束：
// - (domain, node_id) 当 node_id > 0 且 endpoint_id = ” 时唯一
// - (domain, endpoint_id) 当 endpoint_id != ” 时唯一
// 注意：GORM 不支持部分唯一索引，需要在数据库迁移中手动创建
type DomainRegistry struct {
	ID              int64              `gorm:"primaryKey" json:"id"`
	Domain          string             `gorm:"size:255;not null;index" json:"domain"`           // 完整域名（如 beagle-242.beijing.beagle）
	Type            DomainType         `gorm:"size:30;not null;index" json:"type"`              // 能力类型：ssh / k8sapi
	UserID          uint64             `gorm:"not null;index" json:"user_id"`                   // 所属 User ID（Agent User 或 Client User）
	ProviderID      string             `gorm:"size:36;not null;index" json:"provider_id"`       // 所属 Provider
	AgentResourceID string             `gorm:"size:36;not null;index" json:"agent_resource_id"` // 所属逻辑 Agent 技术资源
	ResourceKind    DomainResourceKind `gorm:"size:16;not null;index" json:"resource_kind"`     // 具体资源类型
	ResourceID      string             `gorm:"size:100;not null;index" json:"resource_id"`      // 具体资源 ID
	NodeID          uint64             `gorm:"index" json:"node_id,omitempty"`                  // 关联的 Node ID（Node 注册时）
	EndpointID      string             `gorm:"size:36" json:"endpoint_id,omitempty"`            // 关联的 Endpoint ID（Endpoint 注册时）
	TargetIP        string             `gorm:"size:50" json:"target_ip,omitempty"`              // 目标 IP（Agent 的 Tailscale IP）
	TargetPort      int                `json:"target_port,omitempty"`                           // 目标端口（Desktop 通过 Tailscale 连接的端口）
	SshUsers        string             `gorm:"type:text" json:"ssh_users,omitempty"`            // SSH 用户列表（ssh 类型时，JSON 数组，如 "[\"root\",\"deploy\"]"）
	Status          DomainStatus       `gorm:"size:20;not null;default:'online'" json:"status"` // online/offline
	CreatedAt       time.Time          `json:"created_at"`
	UpdatedAt       time.Time          `json:"updated_at"`

	// 关联
	User     *User     `gorm:"foreignKey:UserID" json:"user,omitempty"`
	Node     *Node     `gorm:"foreignKey:NodeID" json:"node,omitempty"`
	Endpoint *Endpoint `gorm:"foreignKey:EndpointID;references:ID" json:"endpoint,omitempty"`
}

type DomainResourceKind string

const (
	DomainResourceNode       DomainResourceKind = "node"
	DomainResourceEndpoint   DomainResourceKind = "endpoint"
	DomainResourceKubernetes DomainResourceKind = "kubernetes"
)

func (DomainRegistry) TableName() string {
	return "domain_registry"
}

// GetSSHUsers 解析 SSH 用户列表（从 JSON 字符串）
func (d *DomainRegistry) GetSSHUsers() []string {
	if d.SshUsers == "" || d.SshUsers == "[]" {
		return nil
	}
	var users []string
	if err := json.Unmarshal([]byte(d.SshUsers), &users); err != nil {
		return nil
	}
	return users
}
