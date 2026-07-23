package model

import "time"

// DiscoveryCandidateStatus describes a runtime observation before it is
// reconciled with a trusted Provider Workspace binding.
type DiscoveryCandidateStatus string

const (
	DiscoveryCandidateObserved     DiscoveryCandidateStatus = "observed"
	DiscoveryCandidatePendingClaim DiscoveryCandidateStatus = "pending_claim"
	DiscoveryCandidatePublished    DiscoveryCandidateStatus = "published"
	DiscoveryCandidateConflict     DiscoveryCandidateStatus = "conflict"
	DiscoveryCandidateStale        DiscoveryCandidateStatus = "stale"
	DiscoveryCandidateRejected     DiscoveryCandidateStatus = "rejected"
)

// TenantStatus controls whether a customer may receive new access grants.
type TenantStatus string

const (
	TenantStatusActive    TenantStatus = "active"
	TenantStatusSuspended TenantStatus = "suspended"
)

// Tenant is the authorization boundary for customer-owned resources.
// A shared Agent or Kubernetes cluster may serve resources from many tenants.
type Tenant struct {
	ID        string       `gorm:"primaryKey;size:36" json:"id"`
	Key       string       `gorm:"size:100;not null;uniqueIndex" json:"key"`
	Name      string       `gorm:"size:200;not null" json:"name"`
	Status    TenantStatus `gorm:"size:20;not null;default:'active';index" json:"status"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
}

func (Tenant) TableName() string { return "tenant" }

// TenantMembership connects a global User identity to a customer.
type TenantMembership struct {
	ID        int64      `gorm:"primaryKey" json:"id"`
	TenantID  string     `gorm:"size:36;not null;uniqueIndex:uk_tenant_membership,priority:1;index" json:"tenant_id"`
	UserID    uint64     `gorm:"not null;uniqueIndex:uk_tenant_membership,priority:2;index" json:"user_id"`
	Role      string     `gorm:"size:30;not null;default:'member'" json:"role"`
	Enabled   bool       `gorm:"not null;default:true;index" json:"enabled"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

func (TenantMembership) TableName() string { return "tenant_membership" }

// ProviderBindingStatus controls whether a trusted external customer may
// publish Workspace resources into a Tenant.
type ProviderBindingStatus string

const (
	ProviderBindingActive  ProviderBindingStatus = "active"
	ProviderBindingRevoked ProviderBindingStatus = "revoked"
)

// ProviderTenantBinding maps a provider-owned customer identity to Beagle's
// internal Tenant. The external ID is the only customer identity accepted
// from a provider integration.
type ProviderTenantBinding struct {
	ID               string                `gorm:"primaryKey;size:36" json:"id"`
	ProviderID       string                `gorm:"size:100;not null;uniqueIndex:uk_provider_external_tenant,priority:1;index" json:"provider_id"`
	ExternalTenantID string                `gorm:"size:200;not null;uniqueIndex:uk_provider_external_tenant,priority:2" json:"external_tenant_id"`
	TenantID         string                `gorm:"size:36;not null;index" json:"tenant_id"`
	Status           ProviderBindingStatus `gorm:"size:20;not null;default:'active';index" json:"status"`
	CreatedAt        time.Time             `json:"created_at"`
	UpdatedAt        time.Time             `json:"updated_at"`
}

func (ProviderTenantBinding) TableName() string { return "provider_tenant_binding" }

type WorkspaceBindingStatus string

const (
	WorkspaceBindingActive  WorkspaceBindingStatus = "active"
	WorkspaceBindingStopped WorkspaceBindingStatus = "stopped"
	WorkspaceBindingRevoked WorkspaceBindingStatus = "revoked"
)

// WorkspaceBinding is the trusted business identity behind a ContainerSSH
// Resource. Runtime Pod facts remain in ResourceTarget and Candidate.
type WorkspaceBinding struct {
	ID                  string                 `gorm:"primaryKey;size:36" json:"id"`
	ProviderID          string                 `gorm:"size:100;not null;uniqueIndex:uk_provider_workspace,priority:1;index" json:"provider_id"`
	ExternalTenantID    string                 `gorm:"size:200;not null;index" json:"external_tenant_id"`
	ExternalWorkspaceID string                 `gorm:"size:200;not null;uniqueIndex:uk_provider_workspace,priority:2;index" json:"external_workspace_id"`
	TenantID            string                 `gorm:"size:36;not null;index" json:"tenant_id"`
	OwnerUserID         uint64                 `gorm:"index" json:"owner_user_id,omitempty"`
	ResourceID          string                 `gorm:"size:36;not null;uniqueIndex" json:"resource_id"`
	Generation          int64                  `gorm:"not null;default:1" json:"generation"`
	Status              WorkspaceBindingStatus `gorm:"size:20;not null;default:'active';index" json:"status"`
	ExpiresAt           *time.Time             `gorm:"index" json:"expires_at,omitempty"`
	CreatedAt           time.Time              `json:"created_at"`
	UpdatedAt           time.Time              `json:"updated_at"`
}

func (WorkspaceBinding) TableName() string { return "workspace_binding" }

// ResourceType is the user-visible access capability.
type ResourceType string

const (
	ResourceTypeHostSSH         ResourceType = "host_ssh"
	ResourceTypeContainerSSH    ResourceType = "container_ssh"
	ResourceTypeKubernetesAPI   ResourceType = "kubernetes_api"
	ResourceTypeDatabaseService ResourceType = "database_service"
	ResourceTypeTCPService      ResourceType = "tcp_service"
)

// ResourceState is intentionally separate from the provider/runtime state.
type ResourceState string

const (
	ResourceStatePending   ResourceState = "pending"
	ResourceStateAvailable ResourceState = "available"
	ResourceStateDegraded  ResourceState = "degraded"
	ResourceStateDraining  ResourceState = "draining"
	ResourceStateStopped   ResourceState = "stopped"
	ResourceStateRevoked   ResourceState = "revoked"
)

// Resource is the stable business object shown in the management console.
// Runtime fields are retained for the first migration slice and are mirrored
// into ResourceTarget once Agent reconciliation is enabled.
type Resource struct {
	ID                  string        `gorm:"primaryKey;size:36" json:"id"`
	TenantID            string        `gorm:"size:36;not null;index" json:"tenant_id"`
	Type                ResourceType  `gorm:"size:30;not null;index" json:"type"`
	DisplayName         string        `gorm:"size:200;not null" json:"display_name"`
	ProviderID          string        `gorm:"size:100;index" json:"provider_id,omitempty"`
	ExternalWorkspaceID string        `gorm:"size:200;index" json:"external_workspace_id,omitempty"`
	OwnerUserID         uint64        `gorm:"index" json:"owner_user_id,omitempty"`
	OwnerGroupID        *int64        `gorm:"index" json:"owner_group_id,omitempty"`
	AgentNodeID         uint64        `gorm:"index;uniqueIndex:uk_agent_container_ssh_port,where:container_ssh_port > 0,priority:1" json:"agent_node_id,omitempty"`
	ClusterID           string        `gorm:"size:200" json:"cluster_id,omitempty"`
	Namespace           string        `gorm:"size:200" json:"namespace,omitempty"`
	PodName             string        `gorm:"size:253" json:"pod_name,omitempty"`
	PodUID              string        `gorm:"size:100;index" json:"pod_uid,omitempty"`
	ContainerName       string        `gorm:"size:200" json:"container_name,omitempty"`
	ContainerSSHPort    uint16        `gorm:"index;uniqueIndex:uk_agent_container_ssh_port,where:container_ssh_port > 0,priority:2" json:"container_ssh_port,omitempty"`
	ShellProfileID      string        `gorm:"size:36" json:"shell_profile_id,omitempty"`
	TargetRevision      int64         `gorm:"not null;default:0" json:"target_revision"`
	State               ResourceState `gorm:"size:20;not null;default:'pending';index" json:"state"`
	ExpiresAt           *time.Time    `gorm:"index" json:"expires_at,omitempty"`
	CreatedAt           time.Time     `json:"created_at"`
	UpdatedAt           time.Time     `json:"updated_at"`
}

func (Resource) TableName() string { return "resource" }

// ResourceTarget represents an observed, immutable runtime target revision.
type ResourceTarget struct {
	ID            int64     `gorm:"primaryKey" json:"id"`
	ResourceID    string    `gorm:"size:36;not null;uniqueIndex:uk_resource_target_revision,priority:1;index" json:"resource_id"`
	Revision      int64     `gorm:"not null;uniqueIndex:uk_resource_target_revision,priority:2" json:"revision"`
	AgentNodeID   uint64    `gorm:"index" json:"agent_node_id,omitempty"`
	ClusterID     string    `gorm:"size:200" json:"cluster_id,omitempty"`
	Namespace     string    `gorm:"size:200" json:"namespace,omitempty"`
	PodName       string    `gorm:"size:253" json:"pod_name,omitempty"`
	PodUID        string    `gorm:"size:100;not null;index" json:"pod_uid"`
	ContainerName string    `gorm:"size:200;not null" json:"container_name"`
	Ready         bool      `gorm:"not null;default:false" json:"ready"`
	ObservedAt    time.Time `json:"observed_at"`
	CreatedAt     time.Time `json:"created_at"`
}

func (ResourceTarget) TableName() string { return "resource_target" }

// AccessGrant is the unified resource-level authorization record.
type AccessGrant struct {
	ID                string    `gorm:"primaryKey;size:36" json:"id"`
	TenantID          string    `gorm:"size:36;not null;index" json:"tenant_id"`
	ResourceID        string    `gorm:"size:36;not null;index" json:"resource_id"`
	SubjectType       string    `gorm:"size:20;not null" json:"subject_type"` // user / group
	SubjectUserID     uint64    `gorm:"index" json:"subject_user_id,omitempty"`
	SubjectGroupID    *int64    `gorm:"index" json:"subject_group_id,omitempty"`
	Actions           string    `gorm:"type:text;not null" json:"actions"` // JSON array
	ShellProfileID    string    `gorm:"size:36" json:"shell_profile_id,omitempty"`
	ValidFrom         time.Time `gorm:"not null" json:"valid_from"`
	ExpiresAt         time.Time `gorm:"not null;index" json:"expires_at"`
	MaxSessionSeconds int       `gorm:"not null;default:28800" json:"max_session_seconds"`
	Revision          int64     `gorm:"not null;default:1" json:"revision"`
	Status            string    `gorm:"size:20;not null;default:'enabled';index" json:"status"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

func (AccessGrant) TableName() string { return "access_grant" }

// DiscoveryCandidate is an untrusted runtime observation reported by an
// Agent. It never grants access and never creates a Resource by itself.
// WorkspaceHint and LabelSnapshot are evidence for reconciliation only; they
// are deliberately not TenantID or OwnerUserID.
type DiscoveryCandidate struct {
	ID             string                   `gorm:"primaryKey;size:36" json:"id"`
	AgentNodeID    uint64                   `gorm:"not null;index:uk_candidate_runtime,priority:1" json:"agent_node_id"`
	ProviderHint   string                   `gorm:"size:100;index" json:"provider_hint,omitempty"`
	ClusterID      string                   `gorm:"size:200;index" json:"cluster_id,omitempty"`
	Namespace      string                   `gorm:"size:200;not null;index" json:"namespace"`
	PodName        string                   `gorm:"size:253" json:"pod_name,omitempty"`
	PodUID         string                   `gorm:"size:100;not null;index:uk_candidate_runtime,priority:2" json:"pod_uid"`
	ContainerName  string                   `gorm:"size:200;not null;index:uk_candidate_runtime,priority:3" json:"container_name"`
	WorkspaceHint  string                   `gorm:"size:200;index" json:"workspace_hint,omitempty"`
	GenerationHint int64                    `gorm:"not null;default:0" json:"generation_hint,omitempty"`
	Ready          bool                     `gorm:"not null;default:false" json:"ready"`
	Status         DiscoveryCandidateStatus `gorm:"size:20;not null;default:'observed';index" json:"status"`
	ConflictReason string                   `gorm:"size:500" json:"conflict_reason,omitempty"`
	LabelSnapshot  string                   `gorm:"type:text" json:"label_snapshot,omitempty"`
	ObservedAt     time.Time                `gorm:"index" json:"observed_at"`
	LeaseExpiresAt *time.Time               `gorm:"index" json:"lease_expires_at,omitempty"`
	ResourceID     string                   `gorm:"size:36;index" json:"resource_id,omitempty"`
	CreatedAt      time.Time                `json:"created_at"`
	UpdatedAt      time.Time                `json:"updated_at"`
}

func (DiscoveryCandidate) TableName() string { return "discovery_candidate" }
