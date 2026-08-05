package model

import "time"

type AuthenticationProviderType string

const (
	AuthenticationProviderLegacyUser  AuthenticationProviderType = "legacy_user"
	AuthenticationProviderLegacyAdmin AuthenticationProviderType = "legacy_admin"
	AuthenticationProviderOIDC        AuthenticationProviderType = "oidc"
)

type ProviderStatus string

const (
	ProviderStatusActive    ProviderStatus = "active"
	ProviderStatusSuspended ProviderStatus = "suspended"
	ProviderStatusRetired   ProviderStatus = "retired"
)

type ProviderManagementRole string

const (
	ProviderManagementRoleAdmin    ProviderManagementRole = "provider_admin"
	ProviderManagementRoleOperator ProviderManagementRole = "provider_operator"
	ProviderManagementRoleViewer   ProviderManagementRole = "provider_viewer"
)

type ManagementScopeType string

const (
	ManagementScopePlatform ManagementScopeType = "platform"
	ManagementScopeProvider ManagementScopeType = "provider"
	ManagementScopeTenant   ManagementScopeType = "tenant"
)

type UserSimulationScopeType string

const (
	UserSimulationScopeProvider UserSimulationScopeType = "provider"
	UserSimulationScopeTenant   UserSimulationScopeType = "tenant"
)

type UserSimulationSessionStatus string

const (
	UserSimulationSessionActive  UserSimulationSessionStatus = "active"
	UserSimulationSessionRevoked UserSimulationSessionStatus = "revoked"
	UserSimulationSessionExpired UserSimulationSessionStatus = "expired"
)

// UserIdentityProfile is the additive target identity attached to a legacy
// User. It is populated only after an identity mapping is confirmed.
type UserIdentityProfile struct {
	UserID       uint64    `gorm:"primaryKey;autoIncrement:false" json:"user_id"`
	Username     string    `gorm:"size:100;not null;uniqueIndex" json:"username"`
	DisplayName  string    `gorm:"size:200;not null" json:"display_name"`
	Enabled      bool      `gorm:"not null;index" json:"enabled"`
	AuthRevision int64     `gorm:"not null;default:1;check:chk_user_identity_auth_revision,auth_revision > 0" json:"auth_revision"`
	RowVersion   int64     `gorm:"not null;default:1;check:chk_user_identity_row_version,row_version > 0" json:"row_version"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`

	User *User `gorm:"foreignKey:UserID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
}

func (UserIdentityProfile) TableName() string { return "user_identity_profile" }

type UserAuthenticationLink struct {
	ID                 string                     `gorm:"primaryKey;size:36" json:"id"`
	UserID             uint64                     `gorm:"not null;index" json:"user_id"`
	ProviderType       AuthenticationProviderType `gorm:"size:32;not null;uniqueIndex:uk_user_auth_provider_subject,priority:1;check:chk_user_auth_provider_type,provider_type IN ('legacy_user','legacy_admin','oidc')" json:"provider_type"`
	ProviderSubject    string                     `gorm:"size:200;not null;uniqueIndex:uk_user_auth_provider_subject,priority:2" json:"provider_subject"`
	CredentialRevision int64                      `gorm:"not null;default:1;check:chk_user_auth_credential_revision,credential_revision > 0" json:"credential_revision"`
	Enabled            bool                       `gorm:"not null;index" json:"enabled"`
	RowVersion         int64                      `gorm:"not null;default:1;check:chk_user_auth_row_version,row_version > 0" json:"row_version"`
	CreatedAt          time.Time                  `json:"created_at"`
	UpdatedAt          time.Time                  `json:"updated_at"`

	User *User `gorm:"foreignKey:UserID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
}

func (UserAuthenticationLink) TableName() string { return "user_authentication_link" }

type PlatformRoleMembership struct {
	ID                 string       `gorm:"primaryKey;size:36" json:"id"`
	UserID             uint64       `gorm:"not null;uniqueIndex;index" json:"user_id"`
	Role               PlatformRole `gorm:"size:32;not null;index;check:chk_platform_role_membership_role,role IN ('platform_admin','platform_viewer')" json:"role"`
	Enabled            bool         `gorm:"not null;index" json:"enabled"`
	ValidFrom          time.Time    `gorm:"not null;index" json:"valid_from"`
	ExpiresAt          *time.Time   `gorm:"index;check:chk_platform_role_membership_interval,expires_at IS NULL OR expires_at > valid_from" json:"expires_at,omitempty"`
	PermissionRevision int64        `gorm:"not null;default:1;check:chk_platform_role_permission_revision,permission_revision > 0" json:"permission_revision"`
	CreatedByUserID    uint64       `gorm:"not null;index" json:"created_by_user_id"`
	Reason             string       `gorm:"size:500;not null" json:"reason"`
	RowVersion         int64        `gorm:"not null;default:1;check:chk_platform_role_row_version,row_version > 0" json:"row_version"`
	CreatedAt          time.Time    `json:"created_at"`
	UpdatedAt          time.Time    `json:"updated_at"`

	User          *User `gorm:"foreignKey:UserID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	CreatedByUser *User `gorm:"foreignKey:CreatedByUserID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
}

func (PlatformRoleMembership) TableName() string { return "platform_role_membership" }

type ResourceProvider struct {
	ID          string              `gorm:"primaryKey;size:36" json:"id"`
	Key         string              `gorm:"size:100;not null;uniqueIndex" json:"key"`
	DisplayName string              `gorm:"size:200;not null" json:"display_name"`
	DomainScope ProviderDomainScope `gorm:"size:16;not null;default:'named';index;check:chk_resource_provider_domain_scope,domain_scope IN ('root','named')" json:"domain_scope"`
	DomainLabel string              `gorm:"size:63;not null;default:'';index;check:chk_resource_provider_domain_shape,(domain_scope = 'root' AND domain_label = '') OR (domain_scope = 'named' AND domain_label <> '')" json:"domain_label"`
	Status      ProviderStatus      `gorm:"size:20;not null;default:'active';index;check:chk_resource_provider_status,status IN ('active','suspended','retired')" json:"status"`
	Revision    int64               `gorm:"not null;default:1;check:chk_resource_provider_revision,revision > 0" json:"revision"`
	RowVersion  int64               `gorm:"not null;default:1;check:chk_resource_provider_row_version,row_version > 0" json:"row_version"`
	CreatedAt   time.Time           `json:"created_at"`
	UpdatedAt   time.Time           `json:"updated_at"`
}

type ProviderDomainScope string

const (
	ProviderDomainRoot  ProviderDomainScope = "root"
	ProviderDomainNamed ProviderDomainScope = "named"
)

func (ResourceProvider) TableName() string { return "resource_provider" }

type AdminProviderMembership struct {
	ID                 string                 `gorm:"primaryKey;size:36" json:"id"`
	UserID             uint64                 `gorm:"not null;uniqueIndex:uk_admin_provider,priority:1;index" json:"user_id"`
	ProviderID         string                 `gorm:"size:36;not null;uniqueIndex:uk_admin_provider,priority:2;index" json:"provider_id"`
	Role               ProviderManagementRole `gorm:"size:32;not null;index;check:chk_admin_provider_role,role IN ('provider_admin','provider_operator','provider_viewer')" json:"role"`
	Enabled            bool                   `gorm:"not null;index" json:"enabled"`
	ValidFrom          time.Time              `gorm:"not null;index" json:"valid_from"`
	ExpiresAt          *time.Time             `gorm:"index;check:chk_admin_provider_interval,expires_at IS NULL OR expires_at > valid_from" json:"expires_at,omitempty"`
	PermissionRevision int64                  `gorm:"not null;default:1;check:chk_admin_provider_permission_revision,permission_revision > 0" json:"permission_revision"`
	CreatedByUserID    uint64                 `gorm:"not null;index" json:"created_by_user_id"`
	Reason             string                 `gorm:"size:500;not null" json:"reason"`
	RowVersion         int64                  `gorm:"not null;default:1;check:chk_admin_provider_row_version,row_version > 0" json:"row_version"`
	CreatedAt          time.Time              `json:"created_at"`
	UpdatedAt          time.Time              `json:"updated_at"`

	User          *User             `gorm:"foreignKey:UserID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	Provider      *ResourceProvider `gorm:"foreignKey:ProviderID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	CreatedByUser *User             `gorm:"foreignKey:CreatedByUserID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
}

func (AdminProviderMembership) TableName() string { return "admin_provider_membership" }

// UserTenantManagementMembership is intentionally separate from the legacy
// AdminTenantMembership, whose subject is Admin rather than User.
type UserTenantManagementMembership struct {
	ID                 string               `gorm:"primaryKey;size:36" json:"id"`
	UserID             uint64               `gorm:"not null;uniqueIndex:uk_user_tenant_management,priority:1;index" json:"user_id"`
	TenantID           string               `gorm:"size:36;not null;uniqueIndex:uk_user_tenant_management,priority:2;index" json:"tenant_id"`
	Role               TenantManagementRole `gorm:"size:32;not null;index;check:chk_user_tenant_management_role,role IN ('tenant_admin','security_auditor','tenant_viewer')" json:"role"`
	Enabled            bool                 `gorm:"not null;index" json:"enabled"`
	ValidFrom          time.Time            `gorm:"not null;index" json:"valid_from"`
	ExpiresAt          *time.Time           `gorm:"index;check:chk_user_tenant_management_interval,expires_at IS NULL OR expires_at > valid_from" json:"expires_at,omitempty"`
	PermissionRevision int64                `gorm:"not null;default:1;check:chk_user_tenant_permission_revision,permission_revision > 0" json:"permission_revision"`
	CreatedByUserID    uint64               `gorm:"not null;index" json:"created_by_user_id"`
	Reason             string               `gorm:"size:500;not null" json:"reason"`
	RowVersion         int64                `gorm:"not null;default:1;check:chk_user_tenant_management_row_version,row_version > 0" json:"row_version"`
	CreatedAt          time.Time            `json:"created_at"`
	UpdatedAt          time.Time            `json:"updated_at"`

	User          *User   `gorm:"foreignKey:UserID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	Tenant        *Tenant `gorm:"foreignKey:TenantID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	CreatedByUser *User   `gorm:"foreignKey:CreatedByUserID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
}

func (UserTenantManagementMembership) TableName() string {
	return "user_tenant_management_membership"
}

type UserSimulationSession struct {
	ID                 string                      `gorm:"primaryKey;size:36" json:"id"`
	ActorUserID        uint64                      `gorm:"not null;index" json:"actor_user_id"`
	EffectiveUserID    uint64                      `gorm:"not null;index" json:"effective_user_id"`
	ScopeType          UserSimulationScopeType     `gorm:"size:20;not null;index;check:chk_user_simulation_scope_type,scope_type IN ('provider','tenant')" json:"scope_type"`
	ScopeID            string                      `gorm:"size:36;not null;index" json:"scope_id"`
	Reason             string                      `gorm:"size:500;not null" json:"reason"`
	Status             UserSimulationSessionStatus `gorm:"size:20;not null;default:'active';index;check:chk_user_simulation_status,status IN ('active','revoked','expired');check:chk_user_simulation_end_state,(status = 'active' AND ended_at IS NULL) OR (status IN ('revoked','expired') AND ended_at IS NOT NULL)" json:"status"`
	StartedAt          time.Time                   `gorm:"not null;index" json:"started_at"`
	ExpiresAt          time.Time                   `gorm:"not null;index;check:chk_user_simulation_interval,expires_at > started_at" json:"expires_at"`
	EndedAt            *time.Time                  `gorm:"check:chk_user_simulation_ended_at,ended_at IS NULL OR ended_at >= started_at" json:"ended_at,omitempty"`
	EndReason          string                      `gorm:"size:100" json:"end_reason,omitempty"`
	CreatedRequestID   string                      `gorm:"size:64;not null;index" json:"created_request_id"`
	PermissionRevision int64                       `gorm:"not null;default:1;check:chk_user_simulation_permission_revision,permission_revision > 0" json:"permission_revision"`
	RowVersion         int64                       `gorm:"not null;default:1;check:chk_user_simulation_row_version,row_version > 0" json:"row_version"`
	CreatedAt          time.Time                   `json:"created_at"`
	UpdatedAt          time.Time                   `json:"updated_at"`

	ActorUser     *User `gorm:"foreignKey:ActorUserID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	EffectiveUser *User `gorm:"foreignKey:EffectiveUserID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
}

func (UserSimulationSession) TableName() string { return "user_simulation_session" }
