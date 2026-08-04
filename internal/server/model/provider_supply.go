package model

import "time"

type TechnicalResourceType string

const (
	TechnicalResourceAgent    TechnicalResourceType = "agent"
	TechnicalResourceEndpoint TechnicalResourceType = "endpoint"
)

type TechnicalResourceLifecycleState string

const (
	TechnicalResourcePending    TechnicalResourceLifecycleState = "pending"
	TechnicalResourceRegistered TechnicalResourceLifecycleState = "registered"
	TechnicalResourceDisabled   TechnicalResourceLifecycleState = "disabled"
	TechnicalResourceRetired    TechnicalResourceLifecycleState = "retired"
	TechnicalResourceDeleted    TechnicalResourceLifecycleState = "deleted"
)

type ResourceHealthState string

const (
	ResourceHealthUnknown  ResourceHealthState = "unknown"
	ResourceHealthOnline   ResourceHealthState = "online"
	ResourceHealthDegraded ResourceHealthState = "degraded"
	ResourceHealthOffline  ResourceHealthState = "offline"
)

type TechnicalResource struct {
	ID                 string                          `gorm:"primaryKey;size:36;uniqueIndex:uk_technical_resource_provider_id,priority:2" json:"id"`
	ProviderID         string                          `gorm:"size:36;not null;uniqueIndex:uk_technical_resource_identity,priority:1;uniqueIndex:uk_technical_resource_provider_id,priority:1;index" json:"provider_id"`
	Type               TechnicalResourceType           `gorm:"size:20;not null;uniqueIndex:uk_technical_resource_identity,priority:2;index;check:chk_technical_resource_type,type IN ('agent','endpoint')" json:"type"`
	StableKey          string                          `gorm:"size:128;not null;uniqueIndex:uk_technical_resource_identity,priority:3" json:"stable_key"`
	ParentID           *string                         `gorm:"size:36;index;check:chk_technical_resource_parent,(type = 'agent' AND parent_id IS NULL) OR (type = 'endpoint' AND parent_id IS NOT NULL)" json:"parent_id,omitempty"`
	LifecycleState     TechnicalResourceLifecycleState `gorm:"size:20;not null;default:'pending';index;check:chk_technical_resource_lifecycle,lifecycle_state IN ('pending','registered','disabled','retired')" json:"lifecycle_state"`
	HealthState        ResourceHealthState             `gorm:"size:20;not null;default:'unknown';index;check:chk_technical_resource_health,health_state IN ('unknown','online','degraded','offline')" json:"health_state"`
	CredentialRevision int64                           `gorm:"not null;default:1;check:chk_technical_resource_credential_revision,credential_revision > 0" json:"credential_revision"`
	RuntimeUserID      uint64                          `gorm:"not null;default:0;index" json:"-"`
	SourceEpoch        string                          `gorm:"size:36" json:"source_epoch,omitempty"`
	LastSequence       int64                           `gorm:"not null;default:0;check:chk_technical_resource_last_sequence,last_sequence >= 0" json:"last_sequence"`
	LastPayloadHash    string                          `gorm:"size:64" json:"last_payload_hash,omitempty"`
	LastReceivedAt     *time.Time                      `gorm:"index" json:"last_received_at,omitempty"`
	LeaseExpiresAt     *time.Time                      `gorm:"index" json:"lease_expires_at,omitempty"`
	ConfigRevision     int64                           `gorm:"not null;default:1;check:chk_technical_resource_config_revision,config_revision > 0" json:"config_revision"`
	ObservedRevision   int64                           `gorm:"not null;default:0;check:chk_technical_resource_observed_revision,observed_revision >= 0" json:"observed_revision"`
	RowVersion         int64                           `gorm:"not null;default:1;check:chk_technical_resource_row_version,row_version > 0" json:"row_version"`
	CreatedAt          time.Time                       `json:"created_at"`
	UpdatedAt          time.Time                       `json:"updated_at"`
	DeletedAt          *time.Time                      `gorm:"index" json:"deleted_at,omitempty"`

	Provider *ResourceProvider  `gorm:"foreignKey:ProviderID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	Parent   *TechnicalResource `gorm:"foreignKey:ProviderID,ParentID;references:ProviderID,ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
}

func (TechnicalResource) TableName() string { return "technical_resource" }

type TechnicalResourceBindingSourceType string

const (
	TechnicalResourceBindingLegacyNode     TechnicalResourceBindingSourceType = "legacy_node"
	TechnicalResourceBindingLegacyEndpoint TechnicalResourceBindingSourceType = "legacy_endpoint"
)

type TechnicalResourceBinding struct {
	ID                  string                             `gorm:"primaryKey;size:36" json:"id"`
	TechnicalResourceID string                             `gorm:"size:36;not null;uniqueIndex:uk_active_technical_resource_binding,where:enabled = true;index" json:"technical_resource_id"`
	SourceType          TechnicalResourceBindingSourceType `gorm:"size:32;not null;uniqueIndex:uk_technical_resource_binding_source,priority:1;check:chk_technical_resource_binding_source_type,source_type IN ('legacy_node','legacy_endpoint')" json:"source_type"`
	SourceID            string                             `gorm:"size:100;not null;uniqueIndex:uk_technical_resource_binding_source,priority:2" json:"source_id"`
	CredentialRevision  int64                              `gorm:"not null;check:chk_technical_resource_binding_credential_revision,credential_revision > 0" json:"credential_revision"`
	Enabled             bool                               `gorm:"not null;default:true;index" json:"enabled"`
	BoundByUserID       uint64                             `gorm:"not null;index" json:"bound_by_user_id"`
	Reason              string                             `gorm:"size:500;not null" json:"reason"`
	RowVersion          int64                              `gorm:"not null;default:1;check:chk_technical_resource_binding_row_version,row_version > 0" json:"row_version"`
	CreatedAt           time.Time                          `json:"created_at"`
	UpdatedAt           time.Time                          `json:"updated_at"`

	TechnicalResource *TechnicalResource `gorm:"foreignKey:TechnicalResourceID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	BoundByUser       *User              `gorm:"foreignKey:BoundByUserID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
}

func (TechnicalResourceBinding) TableName() string { return "technical_resource_binding" }

type SupplyInventoryReceiptStatus string

const (
	SupplyInventoryReceiptStaging   SupplyInventoryReceiptStatus = "staging"
	SupplyInventoryReceiptCommitted SupplyInventoryReceiptStatus = "committed"
	SupplyInventoryReceiptRejected  SupplyInventoryReceiptStatus = "rejected"
)

type SupplyInventoryReceipt struct {
	ID                  string                       `gorm:"primaryKey;size:36" json:"id"`
	TechnicalResourceID string                       `gorm:"size:36;not null;uniqueIndex:uk_supply_inventory_sequence,priority:1;uniqueIndex:uk_supply_inventory_batch,priority:1;index" json:"technical_resource_id"`
	SourceEpoch         string                       `gorm:"size:36;not null;uniqueIndex:uk_supply_inventory_sequence,priority:2;uniqueIndex:uk_supply_inventory_batch,priority:2" json:"source_epoch"`
	Sequence            int64                        `gorm:"not null;uniqueIndex:uk_supply_inventory_sequence,priority:3;check:chk_supply_inventory_sequence,sequence > 0" json:"sequence"`
	SchemaVersion       int                          `gorm:"not null;check:chk_supply_inventory_schema_version,schema_version > 0" json:"schema_version"`
	SnapshotID          string                       `gorm:"size:36;not null;uniqueIndex:uk_supply_inventory_batch,priority:3;index" json:"snapshot_id"`
	BatchIndex          int                          `gorm:"not null;uniqueIndex:uk_supply_inventory_batch,priority:4;check:chk_supply_inventory_batch_index,batch_index >= 0 AND batch_index < batch_count" json:"batch_index"`
	BatchCount          int                          `gorm:"not null;check:chk_supply_inventory_batch_count,batch_count > 0" json:"batch_count"`
	PayloadHash         string                       `gorm:"size:64;not null;check:chk_supply_inventory_payload_hash,length(payload_hash) = 64" json:"payload_hash"`
	CanonicalPayload    string                       `gorm:"type:text;check:chk_supply_inventory_payload_size,length(CAST(canonical_payload AS BLOB)) <= 1048576" json:"-"`
	ReceivedAt          time.Time                    `gorm:"not null;index" json:"received_at"`
	Status              SupplyInventoryReceiptStatus `gorm:"size:20;not null;default:'staging';index;check:chk_supply_inventory_receipt_status,status IN ('staging','committed','rejected');check:chk_supply_inventory_committed_at,(status = 'committed' AND committed_at IS NOT NULL) OR (status IN ('staging','rejected') AND committed_at IS NULL)" json:"status"`
	ResultCode          string                       `gorm:"size:100;not null" json:"result_code"`
	CommittedAt         *time.Time                   `json:"committed_at,omitempty"`
	PayloadDeleteAfter  *time.Time                   `gorm:"index" json:"payload_delete_after,omitempty"`
	CreatedAt           time.Time                    `json:"created_at"`
	UpdatedAt           time.Time                    `json:"updated_at"`

	TechnicalResource *TechnicalResource `gorm:"foreignKey:TechnicalResourceID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
}

func (SupplyInventoryReceipt) TableName() string { return "supply_inventory_receipt" }

type SupplyResourceType string

const (
	SupplyResourceHost       SupplyResourceType = "host"
	SupplyResourceKubernetes SupplyResourceType = "kubernetes"
)

type SupplyIdentityQuality string

const (
	SupplyIdentityStrong       SupplyIdentityQuality = "strong"
	SupplyIdentityInsufficient SupplyIdentityQuality = "insufficient"
	SupplyIdentityCollision    SupplyIdentityQuality = "collision"
)

type SupplyCandidateReviewState string

const (
	SupplyCandidateObserved      SupplyCandidateReviewState = "observed"
	SupplyCandidatePendingReview SupplyCandidateReviewState = "pending_review"
	SupplyCandidateAccepted      SupplyCandidateReviewState = "accepted"
	SupplyCandidateLinked        SupplyCandidateReviewState = "linked"
	SupplyCandidateConflict      SupplyCandidateReviewState = "conflict"
	SupplyCandidateRejected      SupplyCandidateReviewState = "rejected"
)

type SupplyCandidate struct {
	ID                  string                     `gorm:"primaryKey;size:36;uniqueIndex:uk_supply_candidate_provider_id,priority:2" json:"id"`
	ProviderID          string                     `gorm:"size:36;not null;uniqueIndex:uk_supply_candidate_provider_id,priority:1;index" json:"provider_id"`
	TechnicalResourceID string                     `gorm:"size:36;not null;uniqueIndex:uk_supply_candidate_identity,priority:1;index" json:"technical_resource_id"`
	ResourceType        SupplyResourceType         `gorm:"size:20;not null;uniqueIndex:uk_supply_candidate_identity,priority:2;index;check:chk_supply_candidate_resource_type,resource_type IN ('host','kubernetes')" json:"resource_type"`
	StableKey           string                     `gorm:"size:128;not null;uniqueIndex:uk_supply_candidate_identity,priority:3;index" json:"stable_key"`
	IdentityQuality     SupplyIdentityQuality      `gorm:"size:20;not null;index;check:chk_supply_candidate_identity_quality,identity_quality IN ('strong','insufficient','collision')" json:"identity_quality"`
	PayloadHash         string                     `gorm:"size:64;not null;check:chk_supply_candidate_payload_hash,length(payload_hash) = 64" json:"payload_hash"`
	ObservationSnapshot string                     `gorm:"type:text;check:chk_supply_candidate_snapshot_size,length(CAST(observation_snapshot AS BLOB)) <= 65536" json:"-"`
	FirstObservedAt     time.Time                  `gorm:"not null" json:"first_observed_at"`
	LastObservedAt      time.Time                  `gorm:"not null;index;check:chk_supply_candidate_observed_interval,last_observed_at >= first_observed_at" json:"last_observed_at"`
	LeaseExpiresAt      time.Time                  `gorm:"not null;index;check:chk_supply_candidate_lease,lease_expires_at > last_observed_at" json:"lease_expires_at"`
	ReviewState         SupplyCandidateReviewState `gorm:"size:20;not null;default:'observed';index;check:chk_supply_candidate_review_state,review_state IN ('observed','pending_review','accepted','linked','conflict','rejected')" json:"review_state"`
	ConflictCode        string                     `gorm:"size:100;index" json:"conflict_code,omitempty"`
	OpaqueConflictID    string                     `gorm:"size:64;index" json:"opaque_conflict_id,omitempty"`
	ReviewedByUserID    *uint64                    `gorm:"index" json:"reviewed_by_user_id,omitempty"`
	ReviewedAt          *time.Time                 `json:"reviewed_at,omitempty"`
	RowVersion          int64                      `gorm:"not null;default:1;check:chk_supply_candidate_row_version,row_version > 0" json:"row_version"`
	CreatedAt           time.Time                  `json:"created_at"`
	UpdatedAt           time.Time                  `json:"updated_at"`

	Provider          *ResourceProvider  `gorm:"foreignKey:ProviderID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	TechnicalResource *TechnicalResource `gorm:"foreignKey:ProviderID,TechnicalResourceID;references:ProviderID,ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	ReviewedByUser    *User              `gorm:"foreignKey:ReviewedByUserID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
}

func (SupplyCandidate) TableName() string { return "supply_candidate" }

type PlatformResourceLifecycleState string

const (
	PlatformResourceDraft     PlatformResourceLifecycleState = "draft"
	PlatformResourceActive    PlatformResourceLifecycleState = "active"
	PlatformResourceSuspended PlatformResourceLifecycleState = "suspended"
	PlatformResourceRetired   PlatformResourceLifecycleState = "retired"
)

type PlatformResource struct {
	ID                    string                         `gorm:"primaryKey;size:36;uniqueIndex:uk_platform_resource_provider_id,priority:2" json:"id"`
	ProviderID            string                         `gorm:"size:36;not null;uniqueIndex:uk_platform_resource_identity,priority:1;uniqueIndex:uk_platform_resource_provider_id,priority:1;index" json:"provider_id"`
	Type                  SupplyResourceType             `gorm:"size:20;not null;uniqueIndex:uk_platform_resource_identity,priority:2;index;check:chk_platform_resource_type,type IN ('host','kubernetes')" json:"type"`
	StableKey             string                         `gorm:"size:128;not null;uniqueIndex:uk_platform_resource_identity,priority:3" json:"stable_key"`
	DisplayName           string                         `gorm:"size:200;not null" json:"display_name"`
	LifecycleState        PlatformResourceLifecycleState `gorm:"size:20;not null;default:'draft';index;check:chk_platform_resource_lifecycle,lifecycle_state IN ('draft','active','suspended','retired')" json:"lifecycle_state"`
	HealthState           ResourceHealthState            `gorm:"size:20;not null;default:'unknown';index;check:chk_platform_resource_health,health_state IN ('unknown','online','degraded','offline')" json:"health_state"`
	CapabilityRevision    int64                          `gorm:"not null;default:1;check:chk_platform_resource_capability_revision,capability_revision > 0" json:"capability_revision"`
	AllocatableScopeCount int                            `gorm:"not null;default:0;check:chk_platform_resource_allocatable_scope_count,allocatable_scope_count >= 0" json:"allocatable_scope_count"`
	RowVersion            int64                          `gorm:"not null;default:1;check:chk_platform_resource_row_version,row_version > 0" json:"row_version"`
	CreatedAt             time.Time                      `json:"created_at"`
	UpdatedAt             time.Time                      `json:"updated_at"`

	Provider *ResourceProvider `gorm:"foreignKey:ProviderID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
}

func (PlatformResource) TableName() string { return "platform_resource" }

type PlatformResourceSource struct {
	ID                 string    `gorm:"primaryKey;size:36" json:"id"`
	ProviderID         string    `gorm:"size:36;not null;index" json:"provider_id"`
	PlatformResourceID string    `gorm:"size:36;not null;index;uniqueIndex:uk_primary_platform_resource_source,where:is_primary = true" json:"platform_resource_id"`
	SupplyCandidateID  string    `gorm:"size:36;not null;uniqueIndex" json:"supply_candidate_id"`
	IsPrimary          bool      `gorm:"not null;default:false;index" json:"is_primary"`
	LinkedAt           time.Time `gorm:"not null" json:"linked_at"`
	LastConfirmedAt    time.Time `gorm:"not null;index;check:chk_platform_resource_source_confirmed,last_confirmed_at >= linked_at" json:"last_confirmed_at"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`

	PlatformResource *PlatformResource `gorm:"foreignKey:ProviderID,PlatformResourceID;references:ProviderID,ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	SupplyCandidate  *SupplyCandidate  `gorm:"foreignKey:ProviderID,SupplyCandidateID;references:ProviderID,ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
}

func (PlatformResourceSource) TableName() string { return "platform_resource_source" }

type NamespaceObservationState string

const (
	NamespaceObservationObserved NamespaceObservationState = "observed"
	NamespaceObservationStale    NamespaceObservationState = "stale"
)

type NamespaceObservation struct {
	ID                string                    `gorm:"primaryKey;size:36;uniqueIndex:uk_namespace_observation_resource_id,priority:3" json:"id"`
	ProviderID        string                    `gorm:"size:36;not null;uniqueIndex:uk_namespace_observation_resource_id,priority:1;index" json:"provider_id"`
	ClusterResourceID string                    `gorm:"size:36;not null;uniqueIndex:uk_namespace_observation_identity,priority:1;uniqueIndex:uk_namespace_observation_resource_id,priority:2;index" json:"cluster_resource_id"`
	NamespaceUID      string                    `gorm:"size:128;not null;uniqueIndex:uk_namespace_observation_identity,priority:2" json:"namespace_uid"`
	Name              string                    `gorm:"size:253;not null;index" json:"name"`
	LabelSnapshot     string                    `gorm:"type:text;check:chk_namespace_observation_labels_size,length(CAST(label_snapshot AS BLOB)) <= 16384" json:"label_snapshot"`
	Revision          int64                     `gorm:"not null;default:1;check:chk_namespace_observation_revision,revision > 0" json:"revision"`
	ObservedAt        time.Time                 `gorm:"not null;index" json:"observed_at"`
	LeaseExpiresAt    time.Time                 `gorm:"not null;index;check:chk_namespace_observation_lease,lease_expires_at > observed_at" json:"lease_expires_at"`
	State             NamespaceObservationState `gorm:"size:20;not null;default:'observed';index;check:chk_namespace_observation_state,state IN ('observed','stale')" json:"state"`
	CreatedAt         time.Time                 `json:"created_at"`
	UpdatedAt         time.Time                 `json:"updated_at"`

	ClusterResource *PlatformResource `gorm:"foreignKey:ProviderID,ClusterResourceID;references:ProviderID,ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
}

func (NamespaceObservation) TableName() string { return "namespace_observation" }

type ResourceScopeType string

const (
	ResourceScopeCluster   ResourceScopeType = "cluster"
	ResourceScopeNamespace ResourceScopeType = "namespace"
)

type ResourceScopeLifecycleState string

const (
	ResourceScopeDraft       ResourceScopeLifecycleState = "draft"
	ResourceScopeActive      ResourceScopeLifecycleState = "active"
	ResourceScopeAllocatable ResourceScopeLifecycleState = "allocatable"
	ResourceScopeSuspended   ResourceScopeLifecycleState = "suspended"
	ResourceScopeRetired     ResourceScopeLifecycleState = "retired"
)

type ResourceScopeIsolationMode string

const (
	ResourceScopeIsolationNone              ResourceScopeIsolationMode = ""
	ResourceScopeIsolationNamespaceIsolated ResourceScopeIsolationMode = "namespace_isolated"
	ResourceScopeIsolationReviewedShared    ResourceScopeIsolationMode = "reviewed_shared"
)

type ResourceScope struct {
	ID                     string                      `gorm:"primaryKey;size:36;uniqueIndex:uk_resource_scope_provider_resource_id,priority:3" json:"id"`
	ProviderID             string                      `gorm:"size:36;not null;uniqueIndex:uk_resource_scope_provider_resource_id,priority:1;index" json:"provider_id"`
	PlatformResourceID     string                      `gorm:"size:36;not null;uniqueIndex:uk_resource_scope_identity,priority:1;uniqueIndex:uk_resource_scope_provider_resource_id,priority:2;index" json:"platform_resource_id"`
	Type                   ResourceScopeType           `gorm:"size:20;not null;uniqueIndex:uk_resource_scope_identity,priority:2;index;check:chk_resource_scope_type,type IN ('cluster','namespace')" json:"type"`
	StableKey              string                      `gorm:"size:128;not null;uniqueIndex:uk_resource_scope_identity,priority:3" json:"stable_key"`
	ParentID               *string                     `gorm:"size:36;index" json:"parent_id,omitempty"`
	NamespaceObservationID *string                     `gorm:"size:36;uniqueIndex;index" json:"namespace_observation_id,omitempty"`
	LifecycleState         ResourceScopeLifecycleState `gorm:"size:20;not null;default:'draft';index;check:chk_resource_scope_lifecycle,lifecycle_state IN ('draft','active','allocatable','suspended','retired')" json:"lifecycle_state"`
	IsolationMode          ResourceScopeIsolationMode  `gorm:"size:32;not null;default:'';check:chk_resource_scope_shape,(type = 'cluster' AND parent_id IS NULL AND namespace_observation_id IS NULL AND isolation_mode = '') OR (type = 'namespace' AND parent_id IS NOT NULL AND namespace_observation_id IS NOT NULL AND isolation_mode IN ('namespace_isolated','reviewed_shared'))" json:"isolation_mode"`
	ConfigRevision         int64                       `gorm:"not null;default:1;check:chk_resource_scope_config_revision,config_revision > 0" json:"config_revision"`
	EvidenceRevision       int64                       `gorm:"not null;default:1;check:chk_resource_scope_evidence_revision,evidence_revision > 0" json:"evidence_revision"`
	RowVersion             int64                       `gorm:"not null;default:1;check:chk_resource_scope_row_version,row_version > 0" json:"row_version"`
	CreatedAt              time.Time                   `json:"created_at"`
	UpdatedAt              time.Time                   `json:"updated_at"`

	PlatformResource     *PlatformResource     `gorm:"foreignKey:ProviderID,PlatformResourceID;references:ProviderID,ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	Parent               *ResourceScope        `gorm:"foreignKey:ProviderID,PlatformResourceID,ParentID;references:ProviderID,PlatformResourceID,ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	NamespaceObservation *NamespaceObservation `gorm:"foreignKey:ProviderID,PlatformResourceID,NamespaceObservationID;references:ProviderID,ClusterResourceID,ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
}

func (ResourceScope) TableName() string { return "resource_scope" }
