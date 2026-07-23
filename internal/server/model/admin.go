package model

import "time"

type PlatformRole string

const (
	PlatformRoleAdmin  PlatformRole = "platform_admin"
	PlatformRoleViewer PlatformRole = "platform_viewer"
	PlatformRoleNone   PlatformRole = "none"
)

type TenantManagementRole string

const (
	TenantManagementRoleAdmin           TenantManagementRole = "tenant_admin"
	TenantManagementRoleSecurityAuditor TenantManagementRole = "security_auditor"
	TenantManagementRoleViewer          TenantManagementRole = "tenant_viewer"
)

func NormalizePlatformRole(role string) PlatformRole {
	switch role {
	case "admin", string(PlatformRoleAdmin):
		return PlatformRoleAdmin
	case "viewer", string(PlatformRoleViewer):
		return PlatformRoleViewer
	default:
		return PlatformRoleNone
	}
}

func NormalizeTenantManagementRole(role string) TenantManagementRole {
	switch role {
	case string(TenantManagementRoleAdmin):
		return TenantManagementRoleAdmin
	case string(TenantManagementRoleSecurityAuditor):
		return TenantManagementRoleSecurityAuditor
	case "viewer", string(TenantManagementRoleViewer):
		return TenantManagementRoleViewer
	default:
		return ""
	}
}

// Admin 管理员模型
type Admin struct {
	ID           int64     `gorm:"primaryKey" json:"id"`
	Username     string    `gorm:"uniqueIndex;size:50;not null" json:"username"` // 用户名，唯一索引
	PasswordHash string    `gorm:"size:255;not null" json:"-"`                   // 密码（加密存储）
	Role         string    `gorm:"size:20;not null;default:'admin'" json:"role"` // admin / viewer / tenant_admin
	Enabled      bool      `gorm:"not null;default:true;index" json:"enabled"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// AdminTenantMembership scopes a management-console identity to one Tenant.
// Business TenantMembership remains dedicated to Desktop users and grants.
type AdminTenantMembership struct {
	ID                 int64      `gorm:"primaryKey" json:"id"`
	AdminID            int64      `gorm:"not null;uniqueIndex:uk_admin_tenant,priority:1;index" json:"admin_id"`
	TenantID           string     `gorm:"size:36;not null;uniqueIndex:uk_admin_tenant,priority:2;index" json:"tenant_id"`
	Role               string     `gorm:"size:30;not null;default:'tenant_viewer'" json:"role"`
	Enabled            bool       `gorm:"not null;default:true;index" json:"enabled"`
	ExpiresAt          *time.Time `gorm:"index" json:"expires_at,omitempty"`
	PermissionRevision int64      `gorm:"not null;default:1" json:"permission_revision"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

func (AdminTenantMembership) TableName() string { return "admin_tenant_membership" }

func (Admin) TableName() string {
	return "admin"
}
