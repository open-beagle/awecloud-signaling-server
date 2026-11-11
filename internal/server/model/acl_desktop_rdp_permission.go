package model

import "time"

// AclDesktopRDPPermission Desktop 远程桌面（3389）跨账号显式临时授权
// 隐私约束：必须满足目标设备 node.RDPAllowShare == true 且 node.RDPAdminDisabled == false，且强制包含 ExpiresAt。
type AclDesktopRDPPermission struct {
	ID              uint64     `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID        string     `gorm:"size:64;not null;index:idx_rdp_perm_tenant_target" json:"tenant_id"`
	TargetNodeID    uint64     `gorm:"not null;index:idx_rdp_perm_tenant_target;index:idx_rdp_perm_target_enabled" json:"target_node_id"`
	OwnerUserID     uint64     `gorm:"not null;index" json:"owner_user_id"`
	GranteeUserID   uint64     `gorm:"not null;index" json:"grantee_user_id"`
	CreatedByUserID uint64     `gorm:"not null" json:"created_by_user_id"`
	CreatedByName   string     `gorm:"size:100" json:"created_by_name"`
	Reason          string     `gorm:"size:255" json:"reason"`
	Enabled         bool       `gorm:"not null;default:true;index:idx_rdp_perm_target_enabled" json:"enabled"`
	ExpiresAt       time.Time  `gorm:"not null;index" json:"expires_at"`
	RevokedAt       *time.Time `json:"revoked_at,omitempty"`
	RevokeReason    string     `gorm:"size:255" json:"revoke_reason,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`

	TargetNode  *Node `gorm:"foreignKey:TargetNodeID" json:"target_node,omitempty"`
	OwnerUser   *User `gorm:"foreignKey:OwnerUserID" json:"owner_user,omitempty"`
	GranteeUser *User `gorm:"foreignKey:GranteeUserID" json:"grantee_user,omitempty"`
}

// IsActiveAt 判断授权在指定时刻是否处于有效生效期
func (p AclDesktopRDPPermission) IsActiveAt(now time.Time) bool {
	return p.Enabled && p.RevokedAt == nil && !p.ExpiresAt.IsZero() && now.Before(p.ExpiresAt)
}

func (AclDesktopRDPPermission) TableName() string {
	return "acl_desktop_rdp_permission"
}

const (
	RDPSessionEventStarted  = "started"
	RDPSessionEventEnded    = "ended"
	RDPSessionEventRejected = "rejected"
)

// DesktopRDPSessionAudit 被控端上报的远程桌面入站连接会话审计记录
type DesktopRDPSessionAudit struct {
	ID                    uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	SessionUUID           string    `gorm:"size:64;not null;index" json:"session_uuid"`
	TargetNodeID          uint64    `gorm:"not null;index" json:"target_node_id"`
	OwnerUserID           uint64    `gorm:"not null;index" json:"owner_user_id"`
	EventType             string    `gorm:"size:32;not null;index" json:"event_type"`
	SourceHeadscaleNodeID uint64    `gorm:"index" json:"source_headscale_node_id"`
	SourceNodeID          uint64    `gorm:"index" json:"source_node_id"`
	SourceUserName        string    `gorm:"size:100" json:"source_user_name"`
	SourceDeviceName      string    `gorm:"size:100" json:"source_device_name"`
	SourceIP              string    `gorm:"size:64" json:"source_ip"`
	GrantID               uint64    `gorm:"index" json:"grant_id"`
	CloseReason           string    `gorm:"size:100" json:"close_reason"`
	DurationMs            int64     `json:"duration_ms"`
	OccurredAt            time.Time `gorm:"not null;index" json:"occurred_at"`
	CreatedAt             time.Time `json:"created_at"`
}

func (DesktopRDPSessionAudit) TableName() string {
	return "desktop_rdp_session_audit"
}
