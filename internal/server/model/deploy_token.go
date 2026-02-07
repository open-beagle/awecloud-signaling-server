package model

import (
	"time"

	"gorm.io/gorm"
)

// DeployTokenStatus 部署 Token 状态
type DeployTokenStatus string

const (
	DeployTokenStatusPending DeployTokenStatus = "pending" // 待使用
	DeployTokenStatusBound   DeployTokenStatus = "bound"   // 已绑定
	DeployTokenStatusRevoked DeployTokenStatus = "revoked" // 已撤销
)

// DeployToken 统一部署 Token 模型
// 合并了原 AgentDeployToken 和 ClientToken，Agent 和 Client 共用同一套 Token 机制。
// User.Role 决定注册时的 Headscale 行为（agent → agent-{name}，client → client-{name}）。
//
// 设备绑定说明：
// 统一使用 hostname 的 SHA256 哈希作为设备指纹。
// Token 首次使用后绑定 hostname，其他设备无法复用。
type DeployToken struct {
	ID                uint64            `gorm:"primaryKey;autoIncrement" json:"id"`
	Token             string            `gorm:"size:500;not null;uniqueIndex" json:"-"`          // 部署 Token（不返回给前端）
	UserID            uint64            `gorm:"not null;index" json:"user_id"`                   // 关联的用户 ID
	Name              string            `gorm:"size:100;not null" json:"name"`                   // Token 名称/备注
	Status            DeployTokenStatus `gorm:"size:20;not null;default:pending" json:"status"`  // 状态
	DeviceFingerprint string            `gorm:"size:255" json:"device_fingerprint"`              // 绑定的设备指纹（SHA256(hostname)）
	DeviceName        string            `gorm:"size:100" json:"device_name"`                     // 设备名称（hostname）
	SSHEnabled        bool              `gorm:"default:false" json:"ssh_enabled"`                // 是否启用 SSH
	SSHUsers          string            `gorm:"size:500" json:"ssh_users"`                       // SSH 用户名列表（JSON 数组，Client 角色专用）
	ExpiresAt         *time.Time        `json:"expires_at"`                                      // 首次使用截止时间（可选，Agent 默认 24h，Client 无限制）
	CreatedBy         uint64            `gorm:"not null" json:"created_by"`                      // 创建人（管理员 ID）
	CreatedAt         time.Time         `json:"created_at"`                                      // 创建时间
	BoundAt           *time.Time        `json:"bound_at"`                                        // 绑定时间
	LastUsedAt        *time.Time        `json:"last_used_at"`                                    // 最后使用时间
	NodeID            *uint64           `gorm:"index" json:"node_id"`                            // 关联的 Headscale Node ID

	// 关联
	User           *User  `gorm:"foreignKey:UserID" json:"user,omitempty"`
	CreatedByAdmin *Admin `gorm:"foreignKey:CreatedBy" json:"created_by_admin,omitempty"`

	// 非数据库字段
	CreatedByName string `json:"created_by_name" gorm:"-"` // 创建人名称
	UserName      string `json:"user_name" gorm:"-"`       // 用户名称
}

// TableName 表名
func (DeployToken) TableName() string {
	return "deploy_tokens"
}

// BeforeCreate 创建前钩子
func (t *DeployToken) BeforeCreate(tx *gorm.DB) error {
	t.CreatedAt = time.Now()
	if t.Status == "" {
		t.Status = DeployTokenStatusPending
	}
	return nil
}

// IsExpired 检查 Token 是否过期（仅对有过期时间的 pending 状态有效）
func (t *DeployToken) IsExpired() bool {
	if t.Status != DeployTokenStatusPending {
		return false
	}
	if t.ExpiresAt == nil {
		return false // 无过期时间限制
	}
	return time.Now().After(*t.ExpiresAt)
}

// CanUse 检查 Token 是否可用
func (t *DeployToken) CanUse(deviceFingerprint string) (bool, string) {
	switch t.Status {
	case DeployTokenStatusPending:
		if t.IsExpired() {
			return false, "Token 已过期，请重新生成"
		}
		return true, ""
	case DeployTokenStatusBound:
		if t.DeviceFingerprint != deviceFingerprint {
			return false, "Token 已绑定其他设备，请生成新 Token"
		}
		return true, ""
	case DeployTokenStatusRevoked:
		return false, "Token 已被撤销"
	default:
		return false, "Token 状态异常"
	}
}

// Bind 绑定设备
func (t *DeployToken) Bind(fingerprint, deviceName string) {
	now := time.Now()
	t.Status = DeployTokenStatusBound
	t.DeviceFingerprint = fingerprint
	t.DeviceName = deviceName
	t.BoundAt = &now
	t.LastUsedAt = &now
}

// UpdateLastUsed 更新最后使用时间
func (t *DeployToken) UpdateLastUsed() {
	now := time.Now()
	t.LastUsedAt = &now
}

// Revoke 撤销 Token
func (t *DeployToken) Revoke() {
	t.Status = DeployTokenStatusRevoked
}
