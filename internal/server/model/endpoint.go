package model

import "time"

// Endpoint 统一 Endpoint 数据模型
// 一个 Endpoint 节点对应一行记录，SSH/K8SAPI/K8SService 是能力开关字段
// Endpoint 由 Agent 自动发现上报，不支持手动创建
type Endpoint struct {
	ID              string `gorm:"primaryKey;size:36" json:"id"`            // UUID（Server 生成）
	UserID          uint64 `gorm:"not null;index" json:"user_id"`           // 所属 Agent 的 User ID
	Name            string `gorm:"size:100;not null" json:"name"`           // 名称（Endpoint 上报）
	Alias           string `gorm:"size:100" json:"alias"`                   // 别名（Server 可修改）
	Version         string `gorm:"size:50" json:"version"`                  // Endpoint 版本
	UpdaterProtocol string `gorm:"size:16" json:"updater_protocol"`         // Endpoint 支持的 updater 协议版本
	OS              string `gorm:"size:32" json:"os"`                       // Endpoint 操作系统
	Arch            string `gorm:"size:32" json:"arch"`                     // Endpoint CPU 架构
	Status          string `gorm:"size:20;default:'offline'" json:"status"` // 状态：online/offline
	Revoked         bool   `gorm:"default:false" json:"revoked"`            // 是否已注销

	// SSH 能力
	SSHEnabled bool   `gorm:"default:false" json:"ssh_enabled"`                 // SSH 能力开关
	SSHUsers   string `gorm:"type:text;not null;default:'[]'" json:"ssh_users"` // 允许的 SSH 用户名列表（JSON 数组）
	SSHPort    uint16 `gorm:"default:0" json:"ssh_port"`                        // SSH 代理端口（0 表示未分配）

	// K8S API 能力
	K8SAPIEnabled   bool   `gorm:"column:k8sapi_enabled;default:false" json:"k8sapi_enabled"`  // K8S API 能力开关
	K8SAPIApiServer string `gorm:"column:k8sapi_api_server;size:255" json:"k8sapi_api_server"` // K8S API Server 地址
	K8SAPIPort      uint16 `gorm:"column:k8sapi_port;default:0" json:"k8sapi_port"`            // K8SAPI 代理端口（0 表示未分配）

	// K8S Service 能力
	K8SServiceEnabled       bool   `gorm:"column:k8sservice_enabled;default:false" json:"k8sservice_enabled"`          // K8S Service 能力开关
	K8SServiceLabelSelector string `gorm:"column:k8sservice_label_selector;size:255" json:"k8sservice_label_selector"` // 标签选择器
	K8SServiceNamespaces    string `gorm:"column:k8sservice_namespaces;type:text" json:"k8sservice_namespaces"`        // 命名空间（JSON 数组）

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// 关联
	User *User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

func (Endpoint) TableName() string {
	return "endpoint"
}
