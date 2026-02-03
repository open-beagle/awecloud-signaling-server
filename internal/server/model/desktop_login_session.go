package model

import (
	"time"
)

// DesktopLoginSessionStatus 登录会话状态
type DesktopLoginSessionStatus string

const (
	// DesktopLoginSessionStatusPending 待登录
	DesktopLoginSessionStatusPending DesktopLoginSessionStatus = "pending"
	// DesktopLoginSessionStatusCompleted 已完成
	DesktopLoginSessionStatusCompleted DesktopLoginSessionStatus = "completed"
	// DesktopLoginSessionStatusExpired 已过期
	DesktopLoginSessionStatusExpired DesktopLoginSessionStatus = "expired"
	// DesktopLoginSessionStatusFailed 失败
	DesktopLoginSessionStatusFailed DesktopLoginSessionStatus = "failed"
)

// DesktopLoginSession Desktop 登录会话
type DesktopLoginSession struct {
	ID                uint64                    `gorm:"primaryKey;autoIncrement" json:"id"`
	SessionID         string                    `gorm:"type:varchar(64);uniqueIndex;not null" json:"session_id"`   // 会话 ID（UUID）
	DeviceFingerprint string                    `gorm:"type:varchar(128);not null" json:"device_fingerprint"`      // 设备指纹
	DeviceName        string                    `gorm:"type:varchar(128)" json:"device_name"`                      // 设备名称
	UsernameHint      string                    `gorm:"type:varchar(128)" json:"username_hint"`                    // 预填用户名
	Status            DesktopLoginSessionStatus `gorm:"type:varchar(32);default:'pending';not null" json:"status"` // 状态
	UserID            uint64                    `gorm:"default:0" json:"user_id"`                                  // 登录成功后关联的用户 ID
	Token             string                    `gorm:"type:text" json:"-"`                                        // 生成的访问 Token（不返回给前端）
	LogtoState        string                    `gorm:"type:varchar(128)" json:"-"`                                // Logto OAuth state 参数
	LogtoCodeVerifier string                    `gorm:"type:varchar(128)" json:"-"`                                // Logto PKCE code_verifier
	CreatedAt         time.Time                 `gorm:"autoCreateTime" json:"created_at"`                          // 创建时间
	ExpiresAt         time.Time                 `gorm:"not null" json:"expires_at"`                                // 过期时间
	CompletedAt       *time.Time                `json:"completed_at,omitempty"`                                    // 完成时间
	ErrorMessage      string                    `gorm:"type:varchar(512)" json:"error_message,omitempty"`          // 错误信息
}

// TableName 表名
func (DesktopLoginSession) TableName() string {
	return "desktop_login_sessions"
}

// IsExpired 检查会话是否过期
func (s *DesktopLoginSession) IsExpired() bool {
	return time.Now().After(s.ExpiresAt)
}

// IsPending 检查会话是否待登录
func (s *DesktopLoginSession) IsPending() bool {
	return s.Status == DesktopLoginSessionStatusPending
}

// Complete 完成登录
func (s *DesktopLoginSession) Complete(userID uint64, token string) {
	now := time.Now()
	s.Status = DesktopLoginSessionStatusCompleted
	s.UserID = userID
	s.Token = token
	s.CompletedAt = &now
}

// Fail 登录失败
func (s *DesktopLoginSession) Fail(errMsg string) {
	now := time.Now()
	s.Status = DesktopLoginSessionStatusFailed
	s.ErrorMessage = errMsg
	s.CompletedAt = &now
}

// Expire 标记过期
func (s *DesktopLoginSession) Expire() {
	s.Status = DesktopLoginSessionStatusExpired
}
