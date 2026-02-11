package model

import "time"

// EndpointSSH SSH Endpoint 数据模型
type EndpointSSH struct {
	ID        string    `gorm:"primaryKey;size:36" json:"id"`                          // UUID
	UserID    uint64    `gorm:"not null;index" json:"user_id"`                         // 所属 Agent 的 User ID
	Name      string    `gorm:"size:100;not null" json:"name"`                         // 名称
	Alias     string    `gorm:"size:100" json:"alias"`                                 // 别名
	Host      string    `gorm:"size:255;not null" json:"host"`                         // SSH 目标地址
	Port      int       `gorm:"not null;default:22" json:"port"`                       // SSH 端口
	SSHUsers  string    `gorm:"type:text;not null" json:"ssh_users"`                   // 允许的 SSH 用户名列表（JSON 数组）
	Enabled   bool      `gorm:"default:true" json:"enabled"`                           // 是否启用
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// 关联
	User *User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

func (EndpointSSH) TableName() string {
	return "endpoint_ssh"
}

// EndpointK8SAPI K8SAPI Endpoint 数据模型
type EndpointK8SAPI struct {
	ID            string    `gorm:"primaryKey;size:36" json:"id"`                          // UUID
	UserID        uint64    `gorm:"not null;index" json:"user_id"`                         // 所属 Agent 的 User ID
	Name          string    `gorm:"size:100;not null" json:"name"`                         // 名称
	Alias         string    `gorm:"size:100" json:"alias"`                                 // 别名
	APIServer     string    `gorm:"size:255;not null" json:"api_server"`                   // K8S API Server 地址
	KubeconfigRef string    `gorm:"size:255" json:"kubeconfig_ref"`                        // kubeconfig 引用
	Enabled       bool      `gorm:"default:true" json:"enabled"`                           // 是否启用
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`

	// 关联
	User *User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

func (EndpointK8SAPI) TableName() string {
	return "endpoint_k8sapi"
}

// EndpointK8SService K8SService Endpoint 数据模型
type EndpointK8SService struct {
	ID          string    `gorm:"primaryKey;size:36" json:"id"`                          // UUID
	UserID      uint64    `gorm:"not null;index" json:"user_id"`                         // 所属 Agent 的 User ID
	Name        string    `gorm:"size:100;not null" json:"name"`                         // 名称
	Alias       string    `gorm:"size:100" json:"alias"`                                 // 别名
	Namespace   string    `gorm:"size:100;not null" json:"namespace"`                    // K8S 命名空间
	ServiceName string    `gorm:"size:100;not null" json:"service_name"`                 // K8S Service 名称
	TargetPort  int       `gorm:"not null" json:"target_port"`                           // 目标端口
	Enabled     bool      `gorm:"default:true" json:"enabled"`                           // 是否启用
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`

	// 关联
	User *User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

func (EndpointK8SService) TableName() string {
	return "endpoint_k8sservice"
}
