package model

import "time"

// AclK8SUserPermission K8S API 授权 - 用户级别
// 授权某个用户通过 K8S API 访问某个 Agent 的 K8S 集群
type AclK8SUserPermission struct {
	ID           int64     `gorm:"primaryKey" json:"id"`
	TargetUserID uint64    `gorm:"not null;index;uniqueIndex:uk_ak8sup" json:"target_user_id"` // 目标用户 ID（Agent）
	UserID       uint64    `gorm:"not null;index;uniqueIndex:uk_ak8sup" json:"user_id"`        // 被授权用户 ID
	K8SGroups    string    `gorm:"type:text;not null" json:"k8s_groups"`                       // K8S Impersonation 分组（JSON 数组，如 ["system:masters"]）
	Namespaces   string    `gorm:"type:text;not null" json:"namespaces"`                       // 允许的命名空间（JSON 数组，空数组表示全部）
	Enabled      bool      `gorm:"default:true" json:"enabled"`                                // 是否启用
	GrantedAt    time.Time `json:"granted_at"`                                                 // 授权时间

	// 关联
	TargetUser *User `gorm:"foreignKey:TargetUserID" json:"target_user,omitempty"`
	User       *User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

func (AclK8SUserPermission) TableName() string {
	return "acl_k8s_user_permission"
}

// AclK8SGroupPermission K8S API 授权 - 分组级别
// 授权某个分组通过 K8S API 访问某个 Agent 的 K8S 集群
type AclK8SGroupPermission struct {
	ID           int64     `gorm:"primaryKey" json:"id"`
	TargetUserID uint64    `gorm:"not null;index;uniqueIndex:uk_ak8sgp" json:"target_user_id"` // 目标用户 ID（Agent）
	GroupID      int64     `gorm:"not null;index;uniqueIndex:uk_ak8sgp" json:"group_id"`       // 被授权分组 ID
	K8SGroups    string    `gorm:"type:text;not null" json:"k8s_groups"`                       // K8S Impersonation 分组（JSON 数组）
	Namespaces   string    `gorm:"type:text;not null" json:"namespaces"`                       // 允许的命名空间（JSON 数组，空数组表示全部）
	Enabled      bool      `gorm:"default:true" json:"enabled"`                                // 是否启用
	GrantedAt    time.Time `json:"granted_at"`                                                 // 授权时间

	// 关联
	TargetUser *User  `gorm:"foreignKey:TargetUserID" json:"target_user,omitempty"`
	Group      *Group `gorm:"foreignKey:GroupID" json:"group,omitempty"`
}

func (AclK8SGroupPermission) TableName() string {
	return "acl_k8s_group_permission"
}
