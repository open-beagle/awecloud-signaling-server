package model

import (
	"time"

	"gorm.io/gorm"
)

// ClientTokenStatus Client Token 状态
type ClientTokenStatus string

const (
	ClientTokenStatusPending ClientTokenStatus = "pending" // 待使用
	ClientTokenStatusBound   ClientTokenStatus = "bound"   // 已绑定
)

// ClientToken Client Token 模型，用于 CloudIDE/kubectl 等客户端
type ClientToken struct {
	ID                uint              `json:"id" gorm:"primaryKey"`
	Token             string            `json:"token" gorm:"uniqueIndex;size:512"`     // Token（加密存储，前缀 ct_）
	UserID            uint              `json:"user_id" gorm:"index"`                  // 关联的用户 ID
	Name              string            `json:"name" gorm:"size:100"`                  // Token 名称
	Status            ClientTokenStatus `json:"status" gorm:"size:20;default:pending"` // 状态
	DeviceFingerprint string            `json:"device_fingerprint" gorm:"size:255"`    // 绑定的设备指纹
	DeviceName        string            `json:"device_name" gorm:"size:100"`           // 设备名称
	CreatedAt         time.Time         `json:"created_at"`                            // 创建时间
	BoundAt           *time.Time        `json:"bound_at"`                              // 绑定时间
	LastUsedAt        *time.Time        `json:"last_used_at"`                          // 最后使用时间
	NodeID            *uint64           `json:"node_id"`                               // 关联的 Headscale Node ID
	CreatedBy         uint              `json:"created_by"`                            // 创建人 ID
	CreatedByName     string            `json:"created_by_name" gorm:"-"`              // 创建人名称（非数据库字段）
	UserName          string            `json:"user_name" gorm:"-"`                    // 用户名称（非数据库字段）
}

// TableName 表名
func (ClientToken) TableName() string {
	return "client_tokens"
}

// BeforeCreate 创建前钩子
func (t *ClientToken) BeforeCreate(tx *gorm.DB) error {
	t.CreatedAt = time.Now()
	t.Status = ClientTokenStatusPending
	return nil
}

// CanUse 检查 Token 是否可用
func (t *ClientToken) CanUse(fingerprint string) (bool, string) {
	switch t.Status {
	case ClientTokenStatusPending:
		// 待使用状态，任何设备都可以使用
		return true, ""
	case ClientTokenStatusBound:
		// 已绑定状态，只有相同设备指纹才能使用
		if t.DeviceFingerprint == fingerprint {
			return true, ""
		}
		return false, "Token 已绑定其他设备，请生成新 Token"
	default:
		return false, "Token 状态无效"
	}
}

// Bind 绑定设备
func (t *ClientToken) Bind(fingerprint, deviceName string) {
	now := time.Now()
	t.Status = ClientTokenStatusBound
	t.DeviceFingerprint = fingerprint
	t.DeviceName = deviceName
	t.BoundAt = &now
	t.LastUsedAt = &now
}

// UpdateLastUsed 更新最后使用时间
func (t *ClientToken) UpdateLastUsed() {
	now := time.Now()
	t.LastUsedAt = &now
}
