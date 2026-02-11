package model

import "time"

// AclK8SServiceUserPermission K8SService 授权 - 用户级别
// 授权某个用户访问某个 Agent 发现的 K8S Service
type AclK8SServiceUserPermission struct {
	ID           int64     `gorm:"primaryKey" json:"id"`
	TargetUserID uint64    `gorm:"not null;index;uniqueIndex:uk_ak8ssup" json:"target_user_id"` // 目标用户 ID（Agent）
	UserID       uint64    `gorm:"not null;index;uniqueIndex:uk_ak8ssup" json:"user_id"`        // 被授权用户 ID
	Namespaces   string    `gorm:"type:text;not null" json:"namespaces"`                        // 允许的命名空间（JSON 数组，空数组表示全部）
	ServiceNames string    `gorm:"type:text;not null" json:"service_names"`                     // 允许的 Service 名称（JSON 数组，空数组表示全部）
	Enabled      bool      `gorm:"default:true" json:"enabled"`                                 // 是否启用
	GrantedAt    time.Time `json:"granted_at"`                                                  // 授权时间

	// 关联
	TargetUser *User `gorm:"foreignKey:TargetUserID" json:"target_user,omitempty"`
	User       *User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

func (AclK8SServiceUserPermission) TableName() string {
	return "acl_k8s_service_user_permission"
}

// AclK8SServiceGroupPermission K8SService 授权 - 分组级别
// 授权某个分组访问某个 Agent 发现的 K8S Service
type AclK8SServiceGroupPermission struct {
	ID           int64     `gorm:"primaryKey" json:"id"`
	TargetUserID uint64    `gorm:"not null;index;uniqueIndex:uk_ak8ssgp" json:"target_user_id"` // 目标用户 ID（Agent）
	GroupID      int64     `gorm:"not null;index;uniqueIndex:uk_ak8ssgp" json:"group_id"`       // 被授权分组 ID
	Namespaces   string    `gorm:"type:text;not null" json:"namespaces"`                        // 允许的命名空间（JSON 数组，空数组表示全部）
	ServiceNames string    `gorm:"type:text;not null" json:"service_names"`                     // 允许的 Service 名称（JSON 数组，空数组表示全部）
	Enabled      bool      `gorm:"default:true" json:"enabled"`                                 // 是否启用
	GrantedAt    time.Time `json:"granted_at"`                                                  // 授权时间

	// 关联
	TargetUser *User  `gorm:"foreignKey:TargetUserID" json:"target_user,omitempty"`
	Group      *Group `gorm:"foreignKey:GroupID" json:"group,omitempty"`
}

func (AclK8SServiceGroupPermission) TableName() string {
	return "acl_k8s_service_group_permission"
}
