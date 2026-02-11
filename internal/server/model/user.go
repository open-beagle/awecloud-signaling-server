package model

import "time"

// UserRole 用户角色
type UserRole string

const (
	UserRoleAgent  UserRole = "agent"  // 代理角色
	UserRoleClient UserRole = "client" // 客户端角色
)

// UserSource 用户来源
type UserSource string

const (
	UserSourceManual UserSource = "manual" // 管理员手动创建
	UserSourceLogto  UserSource = "logto"  // Logto 上游注册
)

// User 用户模型
// 统一 Agent 和 Client，通过 Role 区分
type User struct {
	ID           uint64     `gorm:"primaryKey;autoIncrement" json:"id"`        // 自增主键
	HeadscaleUID uint64     `gorm:"index" json:"headscale_uid,omitempty"`      // Headscale User ID
	Name         string     `gorm:"uniqueIndex;size:100;not null" json:"name"` // 唯一名称
	Alias        string     `gorm:"size:100" json:"alias"`                     // 别名（显示名称）
	Role         UserRole   `gorm:"size:20;not null;index" json:"role"`        // 角色：agent / client
	SecretHash   string     `gorm:"size:255;not null" json:"-"`                // 密钥哈希（不序列化）
	SSHEnabled   bool       `gorm:"default:false" json:"ssh_enabled"`          // 是否启用 SSH（Agent 和 Client 都支持）
	Enabled      bool       `gorm:"default:true" json:"enabled"`               // 是否启用（Logto 注册默认 false，需管理员审批）
	Source       UserSource `gorm:"size:20;default:'manual'" json:"source"`    // 来源：manual / logto
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`

	// Agent 远程能力配置（Server 远程控制，nil=未设置，由 Agent 本地配置决定）
	// 注意：SSHEnabled 保留在 User 表（bool 类型，始终有值），K8S/SVC 配置已迁移到 Node 表

	// 关联
	Nodes []*Node `gorm:"foreignKey:UserID" json:"nodes,omitempty"`
}

func (User) TableName() string {
	return "user"
}
