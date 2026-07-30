package model

import "time"

type ResourceAllocationMode string

const (
	ResourceAllocationAssigned ResourceAllocationMode = "assigned"
	ResourceAllocationLeased   ResourceAllocationMode = "leased"
	ResourceAllocationShared   ResourceAllocationMode = "shared"
)

func (m ResourceAllocationMode) Valid() bool {
	return m == ResourceAllocationAssigned || m == ResourceAllocationLeased || m == ResourceAllocationShared
}

type ResourceAllocationState string

const (
	ResourceAllocationDraft     ResourceAllocationState = "draft"
	ResourceAllocationScheduled ResourceAllocationState = "scheduled"
	ResourceAllocationActive    ResourceAllocationState = "active"
	ResourceAllocationSuspended ResourceAllocationState = "suspended"
	ResourceAllocationExpired   ResourceAllocationState = "expired"
	ResourceAllocationRevoked   ResourceAllocationState = "revoked"
)

func (s ResourceAllocationState) Valid() bool {
	switch s {
	case ResourceAllocationDraft, ResourceAllocationScheduled, ResourceAllocationActive,
		ResourceAllocationSuspended, ResourceAllocationExpired, ResourceAllocationRevoked:
		return true
	default:
		return false
	}
}

func (s ResourceAllocationState) OccupiesScope() bool {
	return s == ResourceAllocationScheduled || s == ResourceAllocationActive || s == ResourceAllocationSuspended
}

func (s ResourceAllocationState) Terminal() bool {
	return s == ResourceAllocationExpired || s == ResourceAllocationRevoked
}

type ResourceAllocation struct {
	ID                 string                  `gorm:"primaryKey;size:36" json:"id"`
	TenantID           string                  `gorm:"size:36;not null;index:idx_resource_allocation_tenant_state_window,priority:1" json:"tenant_id"`
	Mode               ResourceAllocationMode  `gorm:"size:20;not null;index;check:chk_resource_allocation_mode,mode IN ('assigned','leased','shared');check:chk_resource_allocation_lease,mode <> 'leased' OR expires_at IS NOT NULL" json:"mode"`
	ValidFrom          time.Time               `gorm:"not null;index:idx_resource_allocation_tenant_state_window,priority:3" json:"valid_from"`
	ExpiresAt          *time.Time              `gorm:"index:idx_resource_allocation_tenant_state_window,priority:4;index:idx_resource_allocation_expiry,priority:2;check:chk_resource_allocation_window,expires_at IS NULL OR expires_at > valid_from" json:"expires_at,omitempty"`
	ContractRef        string                  `gorm:"size:200" json:"contract_ref,omitempty"`
	State              ResourceAllocationState `gorm:"size:20;not null;default:'draft';index:idx_resource_allocation_tenant_state_window,priority:2;index:idx_resource_allocation_expiry,priority:1;check:chk_resource_allocation_state,state IN ('draft','scheduled','active','suspended','expired','revoked')" json:"state"`
	RowVersion         int64                   `gorm:"not null;default:1;check:chk_resource_allocation_row_version,row_version > 0" json:"row_version"`
	CreatedByUserID    uint64                  `gorm:"not null;index" json:"created_by_user_id"`
	ActivatedByUserID  *uint64                 `gorm:"index" json:"activated_by_user_id,omitempty"`
	ActivatedAt        *time.Time              `gorm:"check:chk_resource_allocation_activation,(activated_by_user_id IS NULL AND activated_at IS NULL) OR (activated_by_user_id IS NOT NULL AND activated_at IS NOT NULL)" json:"activated_at,omitempty"`
	TerminatedByUserID *uint64                 `gorm:"index" json:"terminated_by_user_id,omitempty"`
	TerminatedAt       *time.Time              `json:"terminated_at,omitempty"`
	TerminationReason  string                  `gorm:"size:500;not null;default:'';check:chk_resource_allocation_termination,(state = 'revoked' AND terminated_by_user_id IS NOT NULL AND terminated_at IS NOT NULL AND termination_reason <> '') OR (state = 'expired' AND terminated_by_user_id IS NULL AND terminated_at IS NOT NULL AND termination_reason <> '') OR (state NOT IN ('revoked','expired') AND terminated_by_user_id IS NULL AND terminated_at IS NULL AND termination_reason = '')" json:"termination_reason,omitempty"`
	RenewedFromID      *string                 `gorm:"size:36;index" json:"renewed_from_id,omitempty"`
	CreatedAt          time.Time               `json:"created_at"`
	UpdatedAt          time.Time               `json:"updated_at"`

	Tenant       *Tenant                  `gorm:"foreignKey:TenantID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	CreatedBy    *User                    `gorm:"foreignKey:CreatedByUserID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	ActivatedBy  *User                    `gorm:"foreignKey:ActivatedByUserID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	TerminatedBy *User                    `gorm:"foreignKey:TerminatedByUserID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	RenewedFrom  *ResourceAllocation      `gorm:"foreignKey:RenewedFromID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	Items        []ResourceAllocationItem `gorm:"foreignKey:AllocationID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"items,omitempty"`
}

func (ResourceAllocation) TableName() string { return "resource_allocation" }

type ResourceAllocationItem struct {
	ID                      string    `gorm:"primaryKey;size:36" json:"id"`
	AllocationID            string    `gorm:"size:36;not null;uniqueIndex:uk_resource_allocation_item,priority:1;index" json:"allocation_id"`
	ScopeID                 string    `gorm:"size:36;not null;uniqueIndex:uk_resource_allocation_item,priority:2;index" json:"scope_id"`
	ScopeRowVersionSnapshot int64     `gorm:"not null;check:chk_resource_allocation_item_scope_version,scope_row_version_snapshot > 0" json:"scope_row_version_snapshot"`
	CreatedAt               time.Time `json:"created_at"`

	Allocation *ResourceAllocation `gorm:"foreignKey:AllocationID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	Scope      *ResourceScope      `gorm:"foreignKey:ScopeID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
}

func (ResourceAllocationItem) TableName() string { return "resource_allocation_item" }
