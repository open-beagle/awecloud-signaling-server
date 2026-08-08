package model

import "time"

type WorkloadInventoryReceiptStatus string

const (
	WorkloadInventoryReceiptStaging   WorkloadInventoryReceiptStatus = "staging"
	WorkloadInventoryReceiptCommitted WorkloadInventoryReceiptStatus = "committed"
	WorkloadInventoryReceiptRejected  WorkloadInventoryReceiptStatus = "rejected"
)

type WorkloadObservationKind string

const (
	WorkloadObservationServicePort WorkloadObservationKind = "service_port"
	WorkloadObservationContainer   WorkloadObservationKind = "container"
)

func (k WorkloadObservationKind) Valid() bool {
	return k == WorkloadObservationServicePort || k == WorkloadObservationContainer
}

type WorkloadIdentityQuality string

const (
	WorkloadIdentityStrong       WorkloadIdentityQuality = "strong"
	WorkloadIdentityEphemeral    WorkloadIdentityQuality = "ephemeral"
	WorkloadIdentityInsufficient WorkloadIdentityQuality = "insufficient"
)

type WorkloadObservationState string

const (
	WorkloadObservationObserved WorkloadObservationState = "observed"
	WorkloadObservationEligible WorkloadObservationState = "eligible"
	WorkloadObservationConflict WorkloadObservationState = "conflict"
	WorkloadObservationStale    WorkloadObservationState = "stale"
)

type WorkloadObservationSourceState string

const (
	WorkloadObservationSourceObserved WorkloadObservationSourceState = "observed"
	WorkloadObservationSourceConflict WorkloadObservationSourceState = "conflict"
	WorkloadObservationSourceStale    WorkloadObservationSourceState = "stale"
)

type WorkloadInventoryReceipt struct {
	ID                        string                         `gorm:"primaryKey;size:36" json:"id"`
	SourceTechnicalResourceID string                         `gorm:"size:36;not null;uniqueIndex:uk_workload_inventory_sequence,priority:1;uniqueIndex:uk_workload_inventory_batch,priority:1;index" json:"source_technical_resource_id"`
	SourceEpoch               string                         `gorm:"size:36;not null;uniqueIndex:uk_workload_inventory_sequence,priority:2" json:"source_epoch"`
	Sequence                  int64                          `gorm:"not null;uniqueIndex:uk_workload_inventory_sequence,priority:3;check:chk_workload_inventory_sequence,sequence > 0" json:"sequence"`
	SchemaVersion             int                            `gorm:"not null;check:chk_workload_inventory_schema_version,schema_version > 0" json:"schema_version"`
	SnapshotID                string                         `gorm:"size:36;not null;uniqueIndex:uk_workload_inventory_batch,priority:2;index" json:"snapshot_id"`
	BatchIndex                int                            `gorm:"not null;uniqueIndex:uk_workload_inventory_batch,priority:3;check:chk_workload_inventory_batch_index,batch_index >= 0 AND batch_index < batch_count" json:"batch_index"`
	BatchCount                int                            `gorm:"not null;check:chk_workload_inventory_batch_count,batch_count > 0" json:"batch_count"`
	ClusterIdentityDigest     string                         `gorm:"size:64;not null;check:chk_workload_inventory_cluster_digest,length(cluster_identity_digest) = 64" json:"cluster_identity_digest"`
	NamespaceUID              string                         `gorm:"size:100;not null;index" json:"namespace_uid"`
	NamespaceName             string                         `gorm:"size:200" json:"namespace_name,omitempty"`
	Kind                      WorkloadObservationKind        `gorm:"size:20;not null;index;check:chk_workload_inventory_kind,kind IN ('service_port','container')" json:"kind"`
	PayloadHash               string                         `gorm:"size:64;not null;check:chk_workload_inventory_payload_hash,length(payload_hash) = 64" json:"payload_hash"`
	ObservedAt                time.Time                      `gorm:"not null" json:"observed_at"`
	ReceivedAt                time.Time                      `gorm:"not null;index" json:"received_at"`
	LeaseExpiresAt            time.Time                      `gorm:"not null;index;check:chk_workload_inventory_lease,lease_expires_at > received_at" json:"lease_expires_at"`
	Status                    WorkloadInventoryReceiptStatus `gorm:"size:20;not null;default:'staging';index;check:chk_workload_inventory_status,status IN ('staging','committed','rejected');check:chk_workload_inventory_committed,(status = 'committed' AND committed_at IS NOT NULL) OR (status IN ('staging','rejected') AND committed_at IS NULL)" json:"status"`
	ResultCode                string                         `gorm:"size:100;not null" json:"result_code"`
	Retryable                 bool                           `gorm:"not null;default:false" json:"retryable"`
	CommittedAt               *time.Time                     `json:"committed_at,omitempty"`
	PayloadDeleteAfter        *time.Time                     `gorm:"index" json:"payload_delete_after,omitempty"`
	CreatedAt                 time.Time                      `json:"created_at"`
	UpdatedAt                 time.Time                      `json:"updated_at"`

	SourceTechnicalResource *TechnicalResource      `gorm:"foreignKey:SourceTechnicalResourceID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	Batch                   *WorkloadInventoryBatch `gorm:"foreignKey:ReceiptID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
}

func (WorkloadInventoryReceipt) TableName() string { return "workload_inventory_receipt" }

type WorkloadInventoryBatch struct {
	ID               string    `gorm:"primaryKey;size:36" json:"id"`
	ReceiptID        string    `gorm:"size:36;not null;uniqueIndex" json:"receipt_id"`
	CanonicalPayload string    `gorm:"type:text;not null;check:chk_workload_inventory_batch_payload_size,length(CAST(canonical_payload AS BLOB)) <= 1048576" json:"-"`
	CreatedAt        time.Time `json:"created_at"`

	Receipt *WorkloadInventoryReceipt `gorm:"foreignKey:ReceiptID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
}

func (WorkloadInventoryBatch) TableName() string { return "workload_inventory_batch" }

type WorkloadObservation struct {
	ID               string                   `gorm:"primaryKey;size:36" json:"id"`
	NamespaceScopeID string                   `gorm:"size:36;not null;uniqueIndex:uk_workload_observation_identity,priority:1;index" json:"namespace_scope_id"`
	Kind             WorkloadObservationKind  `gorm:"size:20;not null;uniqueIndex:uk_workload_observation_identity,priority:2;index;check:chk_workload_observation_kind,kind IN ('service_port','container')" json:"kind"`
	StableKey        string                   `gorm:"size:64;not null;uniqueIndex:uk_workload_observation_identity,priority:3;check:chk_workload_observation_stable_key,length(stable_key) = 64" json:"stable_key"`
	IdentityQuality  WorkloadIdentityQuality  `gorm:"size:20;not null;index;check:chk_workload_observation_identity_quality,identity_quality IN ('strong','ephemeral','insufficient')" json:"identity_quality"`
	State            WorkloadObservationState `gorm:"size:20;not null;default:'observed';index:idx_workload_observation_scope_kind_state,priority:3;check:chk_workload_observation_state,state IN ('observed','eligible','conflict','stale')" json:"state"`
	Ready            bool                     `gorm:"not null;default:false" json:"ready"`
	ObservedRevision int64                    `gorm:"not null;default:1;check:chk_workload_observation_revision,observed_revision > 0" json:"observed_revision"`
	LabelSnapshot    string                   `gorm:"type:text;not null;default:'{}';check:chk_workload_observation_labels_size,length(CAST(label_snapshot AS BLOB)) <= 65536" json:"-"`
	FirstObservedAt  time.Time                `gorm:"not null" json:"first_observed_at"`
	LastObservedAt   time.Time                `gorm:"not null;index;check:chk_workload_observation_interval,last_observed_at >= first_observed_at" json:"last_observed_at"`
	LeaseExpiresAt   time.Time                `gorm:"not null;index;check:chk_workload_observation_lease,lease_expires_at > last_observed_at" json:"lease_expires_at"`
	RowVersion       int64                    `gorm:"not null;default:1;check:chk_workload_observation_row_version,row_version > 0" json:"row_version"`
	CreatedAt        time.Time                `json:"created_at"`
	UpdatedAt        time.Time                `json:"updated_at"`

	NamespaceScope *ResourceScope              `gorm:"foreignKey:NamespaceScopeID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	Sources        []WorkloadObservationSource `gorm:"foreignKey:WorkloadObservationID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"sources,omitempty"`
}

func (WorkloadObservation) TableName() string { return "workload_observation" }

type WorkloadObservationSource struct {
	ID                        string                         `gorm:"primaryKey;size:36" json:"id"`
	WorkloadObservationID     string                         `gorm:"size:36;not null;uniqueIndex:uk_workload_observation_source,priority:1;index" json:"workload_observation_id"`
	SourceTechnicalResourceID string                         `gorm:"size:36;not null;uniqueIndex:uk_workload_observation_source,priority:2;index" json:"source_technical_resource_id"`
	SourceEpoch               string                         `gorm:"size:36;not null" json:"source_epoch"`
	Sequence                  int64                          `gorm:"not null;check:chk_workload_observation_source_sequence,sequence > 0" json:"sequence"`
	PayloadHash               string                         `gorm:"size:64;not null;check:chk_workload_observation_source_hash,length(payload_hash) = 64" json:"payload_hash"`
	State                     WorkloadObservationSourceState `gorm:"size:20;not null;default:'observed';index;check:chk_workload_observation_source_state,state IN ('observed','conflict','stale')" json:"state"`
	Ready                     bool                           `gorm:"not null;default:false" json:"ready"`
	TargetSnapshot            string                         `gorm:"type:text;not null;check:chk_workload_observation_source_target_size,length(CAST(target_snapshot AS BLOB)) <= 65536" json:"-"`
	ObservedAt                time.Time                      `gorm:"not null" json:"observed_at"`
	ReceivedAt                time.Time                      `gorm:"not null;index" json:"received_at"`
	LeaseExpiresAt            time.Time                      `gorm:"not null;index;check:chk_workload_observation_source_lease,lease_expires_at > received_at" json:"lease_expires_at"`
	SourceRevision            int64                          `gorm:"not null;default:1;check:chk_workload_observation_source_revision,source_revision > 0" json:"source_revision"`
	RowVersion                int64                          `gorm:"not null;default:1;check:chk_workload_observation_source_row_version,row_version > 0" json:"row_version"`
	CreatedAt                 time.Time                      `json:"created_at"`
	UpdatedAt                 time.Time                      `json:"updated_at"`

	WorkloadObservation     *WorkloadObservation `gorm:"foreignKey:WorkloadObservationID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	SourceTechnicalResource *TechnicalResource   `gorm:"foreignKey:SourceTechnicalResourceID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
}

func (WorkloadObservationSource) TableName() string { return "workload_observation_source" }

type TenantResourceType string

const (
	TenantResourceContainerService TenantResourceType = "container_service"
	TenantResourceContainerSSH     TenantResourceType = "container_ssh"
)

func (t TenantResourceType) Valid() bool {
	return t == TenantResourceContainerService || t == TenantResourceContainerSSH
}

type TenantResourceVisibilityState string

const (
	TenantResourcePending TenantResourceVisibilityState = "pending"
	TenantResourceVisible TenantResourceVisibilityState = "visible"
	TenantResourceHidden  TenantResourceVisibilityState = "hidden"
	TenantResourceRetired TenantResourceVisibilityState = "retired"
)

type TenantResourceAvailabilityState string

const (
	TenantResourceUnknown     TenantResourceAvailabilityState = "unknown"
	TenantResourceAvailable   TenantResourceAvailabilityState = "available"
	TenantResourceDegraded    TenantResourceAvailabilityState = "degraded"
	TenantResourceUnavailable TenantResourceAvailabilityState = "unavailable"
)

type TenantResource struct {
	ID                   string                          `gorm:"primaryKey;size:36" json:"id"`
	TenantID             string                          `gorm:"size:36;not null;uniqueIndex:uk_tenant_resource_identity,priority:1;index:idx_tenant_resource_catalog,priority:1" json:"tenant_id"`
	Type                 TenantResourceType              `gorm:"size:30;not null;uniqueIndex:uk_tenant_resource_identity,priority:2;index:idx_tenant_resource_catalog,priority:2;check:chk_tenant_resource_type,type IN ('container_service','container_ssh')" json:"type"`
	StableKey            string                          `gorm:"size:64;not null;uniqueIndex:uk_tenant_resource_identity,priority:3;check:chk_tenant_resource_stable_key,length(stable_key) = 64" json:"stable_key"`
	EntitlementLineageID string                          `gorm:"size:36;not null;uniqueIndex:uk_tenant_resource_identity,priority:4;index" json:"entitlement_lineage_id"`
	DisplayName          string                          `gorm:"size:200;not null" json:"display_name"`
	Description          string                          `gorm:"size:1000" json:"description,omitempty"`
	VisibilityState      TenantResourceVisibilityState   `gorm:"size:20;not null;default:'pending';index:idx_tenant_resource_catalog,priority:3;check:chk_tenant_resource_visibility,visibility_state IN ('pending','visible','hidden','retired')" json:"visibility_state"`
	AvailabilityState    TenantResourceAvailabilityState `gorm:"size:20;not null;default:'unknown';index:idx_tenant_resource_catalog,priority:4;check:chk_tenant_resource_availability,availability_state IN ('unknown','available','degraded','unavailable')" json:"availability_state"`
	Revision             int64                           `gorm:"not null;default:1;check:chk_tenant_resource_revision,revision > 0" json:"revision"`
	RowVersion           int64                           `gorm:"not null;default:1;check:chk_tenant_resource_row_version,row_version > 0" json:"row_version"`
	CreatedAt            time.Time                       `json:"created_at"`
	UpdatedAt            time.Time                       `json:"updated_at"`

	Tenant             *Tenant                `gorm:"foreignKey:TenantID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	EntitlementLineage *ResourceAllocation    `gorm:"foreignKey:EntitlementLineageID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	Sources            []TenantResourceSource `gorm:"foreignKey:TenantResourceID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"sources,omitempty"`
}

func (TenantResource) TableName() string { return "tenant_resource" }

type TenantResourceSource struct {
	ID                    string     `gorm:"primaryKey;size:36" json:"id"`
	TenantResourceID      string     `gorm:"size:36;not null;uniqueIndex:uk_tenant_resource_source,priority:1;index" json:"tenant_resource_id"`
	AllocationItemID      string     `gorm:"size:36;not null;uniqueIndex:uk_tenant_resource_source,priority:2;index" json:"allocation_item_id"`
	WorkloadObservationID string     `gorm:"size:36;not null;uniqueIndex:uk_tenant_resource_source,priority:3;index" json:"workload_observation_id"`
	Enabled               bool       `gorm:"not null;default:true;index" json:"enabled"`
	EnabledAt             time.Time  `gorm:"not null" json:"enabled_at"`
	DisabledAt            *time.Time `json:"disabled_at,omitempty"`
	DisabledReason        string     `gorm:"size:500;not null;default:'';check:chk_tenant_resource_source_disable,(enabled = 1 AND disabled_at IS NULL AND disabled_reason = '') OR (enabled = 0 AND disabled_at IS NOT NULL AND disabled_reason <> '')" json:"disabled_reason,omitempty"`
	SourceRevision        int64      `gorm:"not null;default:1;check:chk_tenant_resource_source_revision,source_revision > 0" json:"source_revision"`
	RowVersion            int64      `gorm:"not null;default:1;check:chk_tenant_resource_source_row_version,row_version > 0" json:"row_version"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`

	TenantResource      *TenantResource         `gorm:"foreignKey:TenantResourceID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	AllocationItem      *ResourceAllocationItem `gorm:"foreignKey:AllocationItemID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	WorkloadObservation *WorkloadObservation    `gorm:"foreignKey:WorkloadObservationID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
}

func (TenantResourceSource) TableName() string { return "tenant_resource_source" }

type TenantResourceReviewDecisionType string

const (
	TenantResourceReviewPublished TenantResourceReviewDecisionType = "published"
	TenantResourceReviewRejected  TenantResourceReviewDecisionType = "rejected"
)

type TenantResourceReviewDecision struct {
	ID                  string                           `gorm:"primaryKey;size:36" json:"id"`
	TenantResourceID    string                           `gorm:"size:36;not null;uniqueIndex:uk_tenant_resource_review,priority:1;index" json:"tenant_resource_id"`
	ObservationRevision int64                            `gorm:"not null;uniqueIndex:uk_tenant_resource_review,priority:2;check:chk_tenant_resource_review_revision,observation_revision > 0" json:"observation_revision"`
	Decision            TenantResourceReviewDecisionType `gorm:"size:20;not null;check:chk_tenant_resource_review_decision,decision IN ('published','rejected')" json:"decision"`
	ActorUserID         uint64                           `gorm:"not null;index" json:"actor_user_id"`
	EffectiveUserID     uint64                           `gorm:"not null;index" json:"effective_user_id"`
	SimulationSessionID *string                          `gorm:"size:36;index" json:"simulation_session_id,omitempty"`
	Reason              string                           `gorm:"size:500;not null" json:"reason"`
	CreatedAt           time.Time                        `json:"created_at"`

	TenantResource    *TenantResource        `gorm:"foreignKey:TenantResourceID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	ActorUser         *User                  `gorm:"foreignKey:ActorUserID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	EffectiveUser     *User                  `gorm:"foreignKey:EffectiveUserID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	SimulationSession *UserSimulationSession `gorm:"foreignKey:SimulationSessionID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
}

func (TenantResourceReviewDecision) TableName() string { return "tenant_resource_review_decision" }

type TenantResourceTargetRevision struct {
	ID                        string                  `gorm:"primaryKey;size:36" json:"id"`
	TenantResourceSourceID    string                  `gorm:"size:36;not null;uniqueIndex:uk_resource_target_revision_v2,priority:1;index" json:"tenant_resource_source_id"`
	Revision                  int64                   `gorm:"not null;uniqueIndex:uk_resource_target_revision_v2,priority:2;check:chk_resource_target_revision_v2_revision,revision > 0" json:"revision"`
	TargetType                WorkloadObservationKind `gorm:"size:20;not null;check:chk_resource_target_revision_v2_type,target_type IN ('service_port','container')" json:"target_type"`
	TargetSnapshot            string                  `gorm:"type:text;not null;check:chk_resource_target_revision_v2_snapshot_size,length(CAST(target_snapshot AS BLOB)) <= 65536" json:"-"`
	SourceTechnicalResourceID string                  `gorm:"size:36;not null;index" json:"source_technical_resource_id"`
	AccessTechnicalResourceID string                  `gorm:"size:36;not null;index" json:"access_technical_resource_id"`
	Ready                     bool                    `gorm:"not null;default:false" json:"ready"`
	ObservedAt                time.Time               `gorm:"not null" json:"observed_at"`
	SupersededAt              *time.Time              `gorm:"index" json:"superseded_at,omitempty"`
	ObservationRevision       int64                   `gorm:"not null;check:chk_resource_target_revision_v2_observation_revision,observation_revision > 0" json:"observation_revision"`
	SourceRevision            int64                   `gorm:"not null;check:chk_resource_target_revision_v2_source_revision,source_revision > 0" json:"source_revision"`
	CreatedAt                 time.Time               `json:"created_at"`

	TenantResourceSource    *TenantResourceSource `gorm:"foreignKey:TenantResourceSourceID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	SourceTechnicalResource *TechnicalResource    `gorm:"foreignKey:SourceTechnicalResourceID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	AccessTechnicalResource *TechnicalResource    `gorm:"foreignKey:AccessTechnicalResourceID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
}

func (TenantResourceTargetRevision) TableName() string { return "resource_target_revision_v2" }

type TenantAccessGrantSubjectType string

const (
	TenantAccessGrantSubjectUser  TenantAccessGrantSubjectType = "user"
	TenantAccessGrantSubjectGroup TenantAccessGrantSubjectType = "group"
)

type TenantAccessGrantStatus string

const (
	TenantAccessGrantEnabled   TenantAccessGrantStatus = "enabled"
	TenantAccessGrantSuspended TenantAccessGrantStatus = "suspended"
	TenantAccessGrantRevoked   TenantAccessGrantStatus = "revoked"
	TenantAccessGrantExpired   TenantAccessGrantStatus = "expired"
)

type TenantAccessGrant struct {
	ID                string                       `gorm:"primaryKey;size:36" json:"id"`
	TenantID          string                       `gorm:"size:36;not null;index" json:"tenant_id"`
	TenantResourceID  string                       `gorm:"size:36;not null;uniqueIndex:uk_active_tenant_access_grant,where:status = 'enabled' OR status = 'suspended',priority:1;index" json:"tenant_resource_id"`
	SubjectType       TenantAccessGrantSubjectType `gorm:"size:20;not null;uniqueIndex:uk_active_tenant_access_grant,where:status = 'enabled' OR status = 'suspended',priority:2;check:chk_tenant_access_grant_subject_type,subject_type IN ('user','group')" json:"subject_type"`
	SubjectKey        string                       `gorm:"size:40;not null;uniqueIndex:uk_active_tenant_access_grant,where:status = 'enabled' OR status = 'suspended',priority:3" json:"subject_key"`
	SubjectUserID     *uint64                      `gorm:"index;check:chk_tenant_access_grant_subject,(subject_type = 'user' AND subject_user_id IS NOT NULL AND subject_group_id IS NULL) OR (subject_type = 'group' AND subject_user_id IS NULL AND subject_group_id IS NOT NULL)" json:"subject_user_id,omitempty"`
	SubjectGroupID    *int64                       `gorm:"index" json:"subject_group_id,omitempty"`
	Actions           string                       `gorm:"type:text;not null;check:chk_tenant_access_grant_actions,length(CAST(actions AS BLOB)) <= 4096" json:"actions"`
	ValidFrom         time.Time                    `gorm:"not null" json:"valid_from"`
	ExpiresAt         *time.Time                   `gorm:"index;check:chk_tenant_access_grant_window,expires_at IS NULL OR expires_at > valid_from" json:"expires_at,omitempty"`
	MaxSessionSeconds int                          `gorm:"not null;default:28800;check:chk_tenant_access_grant_max_session,max_session_seconds > 0" json:"max_session_seconds"`
	Status            TenantAccessGrantStatus      `gorm:"size:20;not null;default:'enabled';index;check:chk_tenant_access_grant_status,status IN ('enabled','suspended','revoked','expired')" json:"status"`
	Revision          int64                        `gorm:"not null;default:1;check:chk_tenant_access_grant_revision,revision > 0" json:"revision"`
	RowVersion        int64                        `gorm:"not null;default:1;check:chk_tenant_access_grant_row_version,row_version > 0" json:"row_version"`
	CreatedByUserID   uint64                       `gorm:"not null;index" json:"created_by_user_id"`
	RevokedByUserID   *uint64                      `gorm:"index" json:"revoked_by_user_id,omitempty"`
	RevokedAt         *time.Time                   `json:"revoked_at,omitempty"`
	RevokeReason      string                       `gorm:"size:500;not null;default:'';check:chk_tenant_access_grant_revoke,(status = 'revoked' AND revoked_by_user_id IS NOT NULL AND revoked_at IS NOT NULL AND revoke_reason <> '') OR (status <> 'revoked' AND revoked_by_user_id IS NULL AND revoked_at IS NULL AND revoke_reason = '')" json:"revoke_reason,omitempty"`
	CreatedAt         time.Time                    `json:"created_at"`
	UpdatedAt         time.Time                    `json:"updated_at"`

	Tenant         *Tenant         `gorm:"foreignKey:TenantID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	TenantResource *TenantResource `gorm:"foreignKey:TenantResourceID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	SubjectUser    *User           `gorm:"foreignKey:SubjectUserID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	SubjectGroup   *Group          `gorm:"foreignKey:SubjectGroupID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	CreatedByUser  *User           `gorm:"foreignKey:CreatedByUserID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	RevokedByUser  *User           `gorm:"foreignKey:RevokedByUserID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
}

func (TenantAccessGrant) TableName() string { return "tenant_access_grant" }

type TenantAccessGrantEvent struct {
	ID                  string    `gorm:"primaryKey;size:36" json:"id"`
	GrantID             string    `gorm:"size:36;not null;uniqueIndex:uk_tenant_access_grant_event,priority:1;index" json:"grant_id"`
	GrantRevision       int64     `gorm:"not null;uniqueIndex:uk_tenant_access_grant_event,priority:2;check:chk_tenant_access_grant_event_revision,grant_revision > 0" json:"grant_revision"`
	EventType           string    `gorm:"size:40;not null" json:"event_type"`
	ActorUserID         uint64    `gorm:"not null;index" json:"actor_user_id"`
	EffectiveUserID     uint64    `gorm:"not null;index" json:"effective_user_id"`
	SimulationSessionID *string   `gorm:"size:36;index" json:"simulation_session_id,omitempty"`
	RequestID           string    `gorm:"size:100;not null;index" json:"request_id"`
	Reason              string    `gorm:"size:500" json:"reason,omitempty"`
	Snapshot            string    `gorm:"type:text;not null;check:chk_tenant_access_grant_event_snapshot_size,length(CAST(snapshot AS BLOB)) <= 16384" json:"-"`
	OccurredAt          time.Time `gorm:"not null;index" json:"occurred_at"`
	CreatedAt           time.Time `json:"created_at"`

	Grant             *TenantAccessGrant     `gorm:"foreignKey:GrantID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	ActorUser         *User                  `gorm:"foreignKey:ActorUserID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	EffectiveUser     *User                  `gorm:"foreignKey:EffectiveUserID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	SimulationSession *UserSimulationSession `gorm:"foreignKey:SimulationSessionID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
}

func (TenantAccessGrantEvent) TableName() string { return "tenant_access_grant_event" }

type ResourceSessionType string

const (
	ResourceSessionContainerSSH     ResourceSessionType = "container_ssh"
	ResourceSessionContainerService ResourceSessionType = "container_service"
)

type ResourceSessionStatus string

const (
	ResourceSessionAuthorizing ResourceSessionStatus = "authorizing"
	ResourceSessionActive      ResourceSessionStatus = "active"
	ResourceSessionEnding      ResourceSessionStatus = "ending"
	ResourceSessionEnded       ResourceSessionStatus = "ended"
	ResourceSessionTerminated  ResourceSessionStatus = "terminated"
	ResourceSessionRejected    ResourceSessionStatus = "rejected"
)

type ResourceSession struct {
	ID                        string                `gorm:"primaryKey;size:36" json:"id"`
	TenantID                  string                `gorm:"size:36;not null;index" json:"tenant_id"`
	TenantResourceID          string                `gorm:"size:36;not null;index" json:"tenant_resource_id"`
	TenantResourceSourceID    string                `gorm:"size:36;not null;index" json:"tenant_resource_source_id"`
	TargetRevisionID          string                `gorm:"size:36;not null;index" json:"target_revision_id"`
	AllocationID              string                `gorm:"size:36;not null;index" json:"allocation_id"`
	AllocationItemID          string                `gorm:"size:36;not null;index" json:"allocation_item_id"`
	GrantID                   string                `gorm:"size:36;not null;index" json:"grant_id"`
	GrantRevision             int64                 `gorm:"not null;check:chk_resource_session_grant_revision,grant_revision > 0" json:"grant_revision"`
	UserID                    uint64                `gorm:"not null;index" json:"user_id"`
	TenantMembershipID        int64                 `gorm:"not null;index" json:"tenant_membership_id"`
	DeviceID                  uint64                `gorm:"not null;index" json:"device_id"`
	ActorUserID               uint64                `gorm:"not null;index" json:"actor_user_id"`
	EffectiveUserID           uint64                `gorm:"not null;index" json:"effective_user_id"`
	SimulationSessionID       *string               `gorm:"size:36;index" json:"simulation_session_id,omitempty"`
	SessionType               ResourceSessionType   `gorm:"size:30;not null;index;check:chk_resource_session_type,session_type IN ('container_ssh','container_service')" json:"session_type"`
	Action                    string                `gorm:"size:30;not null" json:"action"`
	AccessTechnicalResourceID string                `gorm:"size:36;not null;index" json:"access_technical_resource_id"`
	AuthorizationRevision     int64                 `gorm:"not null;check:chk_resource_session_authorization_revision,authorization_revision > 0" json:"authorization_revision"`
	ValidUntil                time.Time             `gorm:"not null;index;check:chk_resource_session_valid_until,valid_until > started_at" json:"valid_until"`
	Status                    ResourceSessionStatus `gorm:"size:20;not null;default:'authorizing';index;check:chk_resource_session_status,status IN ('authorizing','active','ending','ended','terminated','rejected')" json:"status"`
	RequestID                 string                `gorm:"size:100;not null;index" json:"request_id"`
	TraceID                   string                `gorm:"size:100;index" json:"trace_id,omitempty"`
	StartedAt                 time.Time             `gorm:"not null;index" json:"started_at"`
	ConnectedAt               *time.Time            `json:"connected_at,omitempty"`
	EndedAt                   *time.Time            `json:"ended_at,omitempty"`
	Result                    string                `gorm:"size:30" json:"result,omitempty"`
	CloseReason               string                `gorm:"size:500" json:"close_reason,omitempty"`
	DisconnectAcknowledgedAt  *time.Time            `json:"disconnect_acknowledged_at,omitempty"`
	RowVersion                int64                 `gorm:"not null;default:1;check:chk_resource_session_row_version,row_version > 0" json:"row_version"`
	CreatedAt                 time.Time             `json:"created_at"`
	UpdatedAt                 time.Time             `json:"updated_at"`

	Tenant                  *Tenant                       `gorm:"foreignKey:TenantID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	TenantResource          *TenantResource               `gorm:"foreignKey:TenantResourceID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	TenantResourceSource    *TenantResourceSource         `gorm:"foreignKey:TenantResourceSourceID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	TargetRevision          *TenantResourceTargetRevision `gorm:"foreignKey:TargetRevisionID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	Allocation              *ResourceAllocation           `gorm:"foreignKey:AllocationID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	AllocationItem          *ResourceAllocationItem       `gorm:"foreignKey:AllocationItemID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	Grant                   *TenantAccessGrant            `gorm:"foreignKey:GrantID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	User                    *User                         `gorm:"foreignKey:UserID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	TenantMembership        *TenantMembership             `gorm:"foreignKey:TenantMembershipID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	Device                  *Node                         `gorm:"foreignKey:DeviceID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	ActorUser               *User                         `gorm:"foreignKey:ActorUserID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	EffectiveUser           *User                         `gorm:"foreignKey:EffectiveUserID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	SimulationSession       *UserSimulationSession        `gorm:"foreignKey:SimulationSessionID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	AccessTechnicalResource *TechnicalResource            `gorm:"foreignKey:AccessTechnicalResourceID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
}

func (ResourceSession) TableName() string { return "resource_session" }

type ResourceSessionEventType string

const (
	ResourceSessionEventAccepted   ResourceSessionEventType = "accepted"
	ResourceSessionEventConnected  ResourceSessionEventType = "connected"
	ResourceSessionEventEnded      ResourceSessionEventType = "ended"
	ResourceSessionEventTerminated ResourceSessionEventType = "terminated"
	ResourceSessionEventFailed     ResourceSessionEventType = "failed"
)

type ResourceSessionEvent struct {
	ID                        string                   `gorm:"primaryKey;size:36" json:"id"`
	EventID                   string                   `gorm:"size:36;not null;uniqueIndex:uk_resource_session_event_source,priority:2" json:"event_id"`
	SourceTechnicalResourceID string                   `gorm:"size:36;not null;uniqueIndex:uk_resource_session_event_source,priority:1;index" json:"source_technical_resource_id"`
	SessionID                 string                   `gorm:"size:36;not null;uniqueIndex:uk_resource_session_event_sequence,priority:1;index" json:"session_id"`
	SourceSequence            int64                    `gorm:"not null;uniqueIndex:uk_resource_session_event_sequence,priority:2;check:chk_resource_session_event_sequence,source_sequence > 0" json:"source_sequence"`
	EventType                 ResourceSessionEventType `gorm:"size:20;not null;check:chk_resource_session_event_type,event_type IN ('accepted','connected','ended','terminated','failed')" json:"event_type"`
	OccurredAt                time.Time                `gorm:"not null;index" json:"occurred_at"`
	ReceivedAt                time.Time                `gorm:"not null;index" json:"received_at"`
	ResultCode                string                   `gorm:"size:100" json:"result_code,omitempty"`
	Payload                   string                   `gorm:"type:text;not null;default:'{}';check:chk_resource_session_event_payload_size,length(CAST(payload AS BLOB)) <= 16384" json:"-"`
	CreatedAt                 time.Time                `json:"created_at"`

	SourceTechnicalResource *TechnicalResource `gorm:"foreignKey:SourceTechnicalResourceID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	Session                 *ResourceSession   `gorm:"foreignKey:SessionID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
}

func (ResourceSessionEvent) TableName() string { return "resource_session_event" }

type ResourceSessionTerminationStatus string

const (
	ResourceSessionTerminationPending      ResourceSessionTerminationStatus = "pending"
	ResourceSessionTerminationDelivered    ResourceSessionTerminationStatus = "delivered"
	ResourceSessionTerminationAcknowledged ResourceSessionTerminationStatus = "acknowledged"
)

type ResourceSessionTermination struct {
	ID              string                           `gorm:"primaryKey;size:36" json:"id"`
	SessionID       string                           `gorm:"size:36;not null;uniqueIndex:uk_resource_session_termination,priority:1;index" json:"session_id"`
	CommandRevision int64                            `gorm:"not null;uniqueIndex:uk_resource_session_termination,priority:2;check:chk_resource_session_termination_revision,command_revision > 0" json:"command_revision"`
	ReasonCode      string                           `gorm:"size:100;not null" json:"reason_code"`
	Reason          string                           `gorm:"size:500;not null" json:"reason"`
	Status          ResourceSessionTerminationStatus `gorm:"size:20;not null;default:'pending';index;check:chk_resource_session_termination_status,status IN ('pending','delivered','acknowledged')" json:"status"`
	DeliveredAt     *time.Time                       `json:"delivered_at,omitempty"`
	AcknowledgedAt  *time.Time                       `json:"acknowledged_at,omitempty"`
	RetryCount      int                              `gorm:"not null;default:0;check:chk_resource_session_termination_retry,retry_count >= 0" json:"retry_count"`
	NextAttemptAt   *time.Time                       `gorm:"index" json:"next_attempt_at,omitempty"`
	CreatedAt       time.Time                        `json:"created_at"`
	UpdatedAt       time.Time                        `json:"updated_at"`

	Session *ResourceSession `gorm:"foreignKey:SessionID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
}

func (ResourceSessionTermination) TableName() string { return "resource_session_termination" }
