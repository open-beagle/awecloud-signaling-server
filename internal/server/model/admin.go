package model

import "time"

// Admin 管理员模型
type Admin struct {
	ID           int64     `gorm:"primaryKey" json:"id"`
	Username     string    `gorm:"uniqueIndex;size:50;not null" json:"username"` // 用户名，唯一索引
	PasswordHash string    `gorm:"size:255;not null" json:"-"`                   // 密码（加密存储）
	Role         string    `gorm:"size:20;not null;default:'admin'" json:"role"` // admin / viewer / tenant_admin
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// AdminTenantMembership scopes a management-console identity to one Tenant.
// Business TenantMembership remains dedicated to Desktop users and grants.
type AdminTenantMembership struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	AdminID   int64     `gorm:"not null;uniqueIndex:uk_admin_tenant,priority:1;index" json:"admin_id"`
	TenantID  string    `gorm:"size:36;not null;uniqueIndex:uk_admin_tenant,priority:2;index" json:"tenant_id"`
	Role      string    `gorm:"size:30;not null;default:'viewer'" json:"role"`
	Enabled   bool      `gorm:"not null;default:true;index" json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (AdminTenantMembership) TableName() string { return "admin_tenant_membership" }

func (Admin) TableName() string {
	return "admin"
}
