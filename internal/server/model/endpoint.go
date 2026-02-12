package model

import "time"

// EndpointSSH SSH Endpoint 数据模型
// Endpoint 由 Agent 自动发现上报，不支持手动创建
type EndpointSSH struct {
	ID        string    `gorm:"primaryKey;size:36" json:"id"`                          // UUID（Server 生成）
	UserID    uint64    `gorm:"not null;index" json:"user_id"`                         // 所属 Agent 的 User ID
	Name      string    `gorm:"size:100;not null" json:"name"`                         // 名称（Endpoint 上报）
	Alias     string    `gorm:"size:100" json:"alias"`                                 // 别名（Server 可修改）
	Host      string    `gorm:"size:255;not null" json:"host"`                         // 内网地址（Endpoint 上报）
	Port      int       `gorm:"not null;default:22" json:"port"`                       // SSH 端口（Endpoint 上报）
	SSHUsers  string    `gorm:"type:text;not null" json:"ssh_users"`                   // 允许的 SSH 用户名列表（JSON 数组，Server 可修改）
	Status    string    `gorm:"size:20;default:'offline'" json:"status"`               // 状态：online/offline（Agent 上报）
	Enabled   bool      `gorm:"default:true" json:"enabled"`                           // 是否启用（Server 可修改）
	Revoked   bool      `gorm:"default:false" json:"revoked"`                          // 是否已注销
	CreatedAt time.Time `json:"created_at"`                                            // 首次发现时间
	UpdatedAt time.Time `json:"updated_at"`

	// 关联
	User *User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

func (EndpointSSH) TableName() string {
	return "endpoint_ssh"
}

// EndpointK8SAPI K8SAPI Endpoint 数据模型
// Endpoint 由 Agent 自动发现上报，不支持手动创建
type EndpointK8SAPI struct {
	ID        string    `gorm:"primaryKey;size:36" json:"id"`                          // UUID（Server 生成）
	UserID    uint64    `gorm:"not null;index" json:"user_id"`                         // 所属 Agent 的 User ID
	Name      string    `gorm:"size:100;not null" json:"name"`                         // 集群名称（Endpoint 上报）
	Alias     string    `gorm:"size:100" json:"alias"`                                 // 别名（Server 可修改）
	APIServer string    `gorm:"size:255;not null" json:"api_server"`                   // K8S API Server 地址（Endpoint 上报）
	Status    string    `gorm:"size:20;default:'offline'" json:"status"`               // 状态：online/offline（Agent 上报）
	Enabled   bool      `gorm:"default:true" json:"enabled"`                           // 是否启用（Server 可修改）
	Revoked   bool      `gorm:"default:false" json:"revoked"`                          // 是否已注销
	CreatedAt time.Time `json:"created_at"`                                            // 首次发现时间
	UpdatedAt time.Time `json:"updated_at"`

	// 关联
	User *User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

func (EndpointK8SAPI) TableName() string {
	return "endpoint_k8sapi"
}

// EndpointK8SService K8SService Endpoint 数据模型
// Endpoint 由 Agent 自动发现上报，不支持手动创建
type EndpointK8SService struct {
	ID        string    `gorm:"primaryKey;size:36" json:"id"`                          // UUID（Server 生成）
	UserID    uint64    `gorm:"not null;index" json:"user_id"`                         // 所属 Agent 的 User ID
	Name      string    `gorm:"size:100;not null" json:"name"`                         // 集群名称（Endpoint 上报）
	Alias     string    `gorm:"size:100" json:"alias"`                                 // 别名（Server 可修改）
	Status    string    `gorm:"size:20;default:'offline'" json:"status"`               // 状态：online/offline（Agent 上报）
	Enabled   bool      `gorm:"default:true" json:"enabled"`                           // 是否启用（Server 可修改）
	Revoked   bool      `gorm:"default:false" json:"revoked"`                          // 是否已注销
	CreatedAt time.Time `json:"created_at"`                                            // 首次发现时间
	UpdatedAt time.Time `json:"updated_at"`

	// 关联
	User *User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

func (EndpointK8SService) TableName() string {
	return "endpoint_k8sservice"
}
