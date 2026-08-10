package model

import "time"

// NodeType 设备类型
type NodeType string

const (
	NodeTypeAgent   NodeType = "agent"   // Agent 设备
	NodeTypeDesktop NodeType = "desktop" // Desktop 设备
)

// Node 设备模型
// 统一 Agent 设备和 Desktop 设备
type Node struct {
	ID                   uint64     `gorm:"primaryKey;autoIncrement" json:"id"`                                               // 自增主键
	UserID               uint64     `gorm:"not null;index;uniqueIndex:uk_node_user_type_name,priority:1" json:"user_id"`      // 所属用户 ID
	Name                 string     `gorm:"size:100;not null;uniqueIndex:uk_node_user_type_name,priority:3" json:"name"`      // 设备名称
	Type                 NodeType   `gorm:"size:20;not null;index;uniqueIndex:uk_node_user_type_name,priority:2" json:"type"` // 类型：agent / desktop
	HeadscaleNodeID      uint64     `gorm:"index" json:"headscale_node_id"`                                                   // Headscale Node ID（外部系统 ID）
	IP                   string     `gorm:"size:50" json:"ip"`                                                                // 隧道 IP
	Version              string     `gorm:"size:50" json:"version"`                                                           // 版本号
	CommitID             string     `gorm:"size:40" json:"commit_id"`                                                         // Git SHA
	BinarySHA256         string     `gorm:"size:64" json:"binary_sha256"`                                                     // 可执行文件 SHA256 摘要
	UpdaterProtocol      string     `gorm:"size:16" json:"updater_protocol"`                                                  // 支持的 updater 协议版本
	ContainerSSHProtocol string     `gorm:"size:16" json:"container_ssh_protocol"`                                            // 支持的 ContainerSSH 协议版本
	Hostname             string     `gorm:"size:100" json:"hostname"`                                                         // 主机名
	HostDomainLabel      string     `gorm:"size:63;not null;default:'';index" json:"host_domain_label"`                       // SSH 主机域名标识
	SystemInfo           string     `gorm:"type:text" json:"system_info"`                                                     // 系统信息 JSON
	SecretHash           string     `gorm:"size:255" json:"-"`                                                                // 设备认证密钥哈希（Desktop 用）
	LastHeartbeat        *time.Time `json:"last_heartbeat"`                                                                   // 最后心跳时间
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`

	// Agent 远程能力配置（Server 远程控制，nil=未设置，由 Agent 本地配置决定）
	// 每个 Node 独立配置，互不影响
	K8SEnabled        *bool  `gorm:"column:k8s_enabled" json:"k8s_enabled,omitempty"`                        // K8S API 能力开关
	K8SListenPort     *int   `gorm:"column:k8s_listen_port" json:"k8s_listen_port,omitempty"`                // K8S API tsnet 监听端口
	K8SApiServer      string `gorm:"column:k8s_api_server;size:255" json:"k8s_api_server,omitempty"`         // K8S API Server 地址
	SVCEnabled        *bool  `gorm:"column:svc_enabled" json:"svc_enabled,omitempty"`                        // K8S Service 能力开关
	SVCLabelSelector  string `gorm:"column:svc_label_selector;size:255" json:"svc_label_selector,omitempty"` // K8S Service 标签选择器
	SVCNamespaces     string `gorm:"column:svc_namespaces;size:1024" json:"svc_namespaces,omitempty"`        // K8S Service 命名空间列表 JSON
	SVCListenPortBase *int   `gorm:"column:svc_listen_port_base" json:"svc_listen_port_base,omitempty"`      // K8S Service gRPC 监听端口

	// Endpoint 功能配置（Server 远程控制）
	EndpointEnabled    *bool  `gorm:"column:endpoint_enabled" json:"endpoint_enabled,omitempty"`          // Endpoint 功能开关
	EndpointAddress    string `gorm:"column:endpoint_address;size:255" json:"endpoint_address,omitempty"` // Endpoint 所在网络可达的 Agent 地址
	EndpointListenPort *int   `gorm:"column:endpoint_listen_port" json:"endpoint_listen_port,omitempty"`  // Endpoint 内网 gRPC 监听端口（默认 50052）
	EndpointToken      string `gorm:"column:endpoint_token;size:255" json:"-"`                            // Endpoint 注册令牌（不序列化到 JSON）

	// 关联
	User *User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

// NodeSystemInfo 设备系统信息结构（用于 JSON 解析）
type NodeSystemInfo struct {
	OS        string `json:"os"`         // 操作系统
	OSVersion string `json:"os_version"` // 系统版本
	Arch      string `json:"arch"`       // CPU 架构
	Hostname  string `json:"hostname"`   // 主机名
	CPU       string `json:"cpu"`        // CPU 型号
	CPUCores  int    `json:"cpu_cores"`  // CPU 核心数
	MemoryGB  int    `json:"memory_gb"`  // 内存大小（GB）
}

func (Node) TableName() string {
	return "node"
}
