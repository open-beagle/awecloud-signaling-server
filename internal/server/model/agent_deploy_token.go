package model

import "time"

// AgentDeployTokenStatus 部署 Token 状态
type AgentDeployTokenStatus string

const (
	AgentDeployTokenStatusPending AgentDeployTokenStatus = "pending" // 待使用
	AgentDeployTokenStatusBound   AgentDeployTokenStatus = "bound"   // 已绑定
	AgentDeployTokenStatusExpired AgentDeployTokenStatus = "expired" // 已过期
)

// AgentDeployToken Agent 部署 Token
type AgentDeployToken struct {
	ID                uint64                 `gorm:"primaryKey;autoIncrement" json:"id"`
	Token             string                 `gorm:"size:500;not null;uniqueIndex" json:"-"`         // 部署 Token（加密存储，不返回给前端）
	UserID            uint64                 `gorm:"not null;index" json:"user_id"`                  // 关联的 Agent 用户 ID
	DeviceName        string                 `gorm:"size:100;not null" json:"device_name"`           // 设备名称
	Status            AgentDeployTokenStatus `gorm:"size:20;not null;default:pending" json:"status"` // 状态
	DeviceFingerprint string                 `gorm:"size:255" json:"device_fingerprint"`             // 绑定的设备指纹
	CreatedBy         uint64                 `gorm:"not null" json:"created_by"`                     // 创建人（管理员 ID）
	CreatedAt         time.Time              `json:"created_at"`
	ExpiresAt         time.Time              `json:"expires_at"`           // 首次部署截止时间（24小时）
	BoundAt           *time.Time             `json:"bound_at"`             // 绑定时间
	LastUsedAt        *time.Time             `json:"last_used_at"`         // 最后使用时间
	NodeID            *uint64                `gorm:"index" json:"node_id"` // 关联的 Node ID

	// 关联
	User           *User  `gorm:"foreignKey:UserID" json:"user,omitempty"`
	CreatedByAdmin *Admin `gorm:"foreignKey:CreatedBy" json:"created_by_admin,omitempty"`
	Node           *Node  `gorm:"foreignKey:NodeID" json:"node,omitempty"`
}

func (AgentDeployToken) TableName() string {
	return "agent_deploy_token"
}

// IsExpired 检查 Token 是否过期（仅对 pending 状态有效）
func (t *AgentDeployToken) IsExpired() bool {
	if t.Status != AgentDeployTokenStatusPending {
		return false
	}
	return time.Now().After(t.ExpiresAt)
}

// CanUse 检查 Token 是否可用
func (t *AgentDeployToken) CanUse(deviceFingerprint string) (bool, string) {
	switch t.Status {
	case AgentDeployTokenStatusPending:
		if t.IsExpired() {
			return false, "Token 已过期，请重新生成"
		}
		return true, ""
	case AgentDeployTokenStatusBound:
		if t.DeviceFingerprint != deviceFingerprint {
			return false, "Token 已绑定其他设备，请生成新 Token"
		}
		return true, ""
	case AgentDeployTokenStatusExpired:
		return false, "Token 已过期，请重新生成"
	default:
		return false, "Token 状态异常"
	}
}
