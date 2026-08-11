package service

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

var (
	ErrTenantResourceInvalidInput         = errors.New("invalid Tenant resource input")
	ErrTenantResourceNotFound             = errors.New("Tenant resource not found")
	ErrTenantResourceRevisionConflict     = errors.New("Tenant resource revision conflict")
	ErrTenantResourceUpstreamUnavailable  = errors.New("Tenant resource upstream is unavailable")
	ErrTenantResourceReviewStale          = errors.New("Tenant resource review is stale")
	ErrTenantResourceTargetNotTrusted     = errors.New("Tenant resource target is not trusted")
	ErrTenantResourceStateTransition      = errors.New("Tenant resource state transition is invalid")
	ErrTenantResourceCrossTenantReference = errors.New("Tenant resource reference is outside the Tenant")
)

const tenantResourceDefaultLimit = 20
const tenantResourceMaxLimit = 100

type TenantResourceListInput struct {
	Type         string
	Visibility   string
	Availability string
	Namespace    string
	Query        string
	Cursor       string
	Limit        int
	Candidates   bool
}

type TenantResourceListResult struct {
	Items      []TenantResourceView `json:"items"`
	NextCursor string               `json:"next_cursor,omitempty"`
}

type TenantResourceView struct {
	ResourceID          string                                `json:"resource_id"`
	Type                string                                `json:"type"`
	DisplayName         string                                `json:"display_name"`
	Description         string                                `json:"description,omitempty"`
	VisibilityState     model.TenantResourceVisibilityState   `json:"visibility_state"`
	AvailabilityState   model.TenantResourceAvailabilityState `json:"availability_state"`
	Revision            int64                                 `json:"revision"`
	RowVersion          int64                                 `json:"row_version"`
	NamespaceScopeID    string                                `json:"namespace_scope_id,omitempty"`
	NamespaceName       string                                `json:"namespace_name,omitempty"`
	TargetRevision      int64                                 `json:"target_revision,omitempty"`
	ObservationRevision int64                                 `json:"observation_revision,omitempty"`
	Ready               bool                                  `json:"ready"`
	ServiceUID          string                                `json:"service_uid,omitempty"`
	ServiceName         string                                `json:"service_name,omitempty"`
	PortName            string                                `json:"port_name,omitempty"`
	PortNumber          int                                   `json:"port_number,omitempty"`
	Protocol            string                                `json:"protocol,omitempty"`
	WorkloadUID         string                                `json:"workload_uid,omitempty"`
	WorkloadKind        string                                `json:"workload_kind,omitempty"`
	WorkloadName        string                                `json:"workload_name,omitempty"`
	PodUID              string                                `json:"pod_uid,omitempty"`
	PodName             string                                `json:"pod_name,omitempty"`
	ContainerName       string                                `json:"container_name,omitempty"`
	IdentityQuality     model.WorkloadIdentityQuality         `json:"identity_quality,omitempty"`
	AgentNodeID         uint64                                `json:"agent_node_id,omitempty"`
	SSHDomain           string                                `json:"ssh_domain,omitempty"`
	TargetIP            string                                `json:"target_ip,omitempty"`
	TargetPort          int                                   `json:"target_port,omitempty"`
	SSHUsers            []string                              `json:"ssh_users,omitempty"`
	CreatedAt           time.Time                             `json:"created_at"`
	UpdatedAt           time.Time                             `json:"updated_at"`
}

type tenantResourceChain struct {
	Resource    model.TenantResource
	Source      model.TenantResourceSource
	Observation model.WorkloadObservation
	Evidence    model.WorkloadObservationSource
	Target      model.TenantResourceTargetRevision
	Allocation  model.ResourceAllocation
	Item        model.ResourceAllocationItem
	Scope       model.ResourceScope
	Namespace   model.NamespaceObservation
}

type TenantResourceService struct {
	db        *gorm.DB
	now       func() time.Time
	snapshots *WorkloadSnapshotStore
}

func NewTenantResourceService(database *gorm.DB, snapshots *WorkloadSnapshotStore) *TenantResourceService {
	return &TenantResourceService{db: database, now: time.Now, snapshots: snapshots}
}

func (s *TenantResourceService) List(ctx context.Context, authorization *ManagementAuthorizationContext, tenantID string, input TenantResourceListInput) (*TenantResourceListResult, error) {
	if s == nil || s.db == nil {
		return nil, ErrTenantResourceInvalidInput
	}
	tenantID = strings.TrimSpace(tenantID)
	input.Type = strings.TrimSpace(input.Type)
	input.Visibility = strings.TrimSpace(input.Visibility)
	input.Availability = strings.TrimSpace(input.Availability)
	input.Namespace = strings.TrimSpace(input.Namespace)
	input.Query = strings.TrimSpace(input.Query)
	input.Cursor = strings.TrimSpace(input.Cursor)
	if input.Limit == 0 {
		input.Limit = tenantResourceDefaultLimit
	}
	if validateRequired("tenant_id", tenantID, 36) != nil || input.Limit < 1 || input.Limit > tenantResourceMaxLimit ||
		len(input.Namespace) > 253 || len(input.Query) > 200 || len(input.Cursor) > 36 {
		return nil, ErrTenantResourceInvalidInput
	}
	if input.Type != "" && input.Type != string(model.ResourceTypeHostSSH) && !model.TenantResourceType(input.Type).Valid() {
		return nil, ErrTenantResourceInvalidInput
	}
	if input.Visibility != "" && !validTenantResourceVisibility(model.TenantResourceVisibilityState(input.Visibility)) {
		return nil, ErrTenantResourceInvalidInput
	}
	if input.Availability != "" && !validTenantResourceAvailability(model.TenantResourceAvailabilityState(input.Availability)) {
		return nil, ErrTenantResourceInvalidInput
	}

	now := s.now().UTC()
	if err := reauthorizeTenantPermission(s.db.WithContext(ctx), authorization, tenantID, PermissionTenantResourcesRead, now); err != nil {
		return nil, err
	}
	if input.Type == string(model.ResourceTypeHostSSH) {
		return s.listHostSSHResources(ctx, tenantID, input)
	}
	if input.Candidates {
		candidates, err := s.memoryCandidates(ctx, tenantID, now)
		if err != nil {
			return nil, err
		}
		candidates = filterMemoryCandidates(candidates, input)
		start, err := candidateCursorIndex(candidates, input.Cursor)
		if err != nil {
			return nil, err
		}
		result := &TenantResourceListResult{Items: []TenantResourceView{}}
		if start >= len(candidates) {
			return result, nil
		}
		end := min(start+input.Limit, len(candidates))
		result.Items = make([]TenantResourceView, 0, end-start)
		for i := start; i < end; i++ {
			result.Items = append(result.Items, candidates[i].view)
		}
		if end < len(candidates) {
			result.NextCursor = candidates[end-1].view.ResourceID
		}
		return result, nil
	}
	query := s.db.WithContext(ctx).Model(&model.TenantResource{}).Where("tenant_id = ?", tenantID)
	query = query.Where("visibility_state <> ?", model.TenantResourcePending)
	if input.Type != "" {
		query = query.Where("type = ?", input.Type)
	}
	if input.Visibility != "" {
		query = query.Where("visibility_state = ?", input.Visibility)
	}
	if input.Availability != "" {
		query = query.Where("availability_state = ?", input.Availability)
	}
	if input.Namespace != "" {
		pattern := "%" + escapeProviderLike(input.Namespace) + "%"
		query = query.Where(`EXISTS (
			SELECT 1 FROM tenant_resource_source source
			JOIN resource_allocation_item item ON item.id = source.allocation_item_id
			JOIN resource_scope scope ON scope.id = item.scope_id
			JOIN namespace_observation namespace ON namespace.id = scope.namespace_observation_id
			WHERE source.tenant_resource_id = tenant_resource.id
				AND (scope.id = ? OR namespace.name LIKE ? ESCAPE '\\')
		)`, input.Namespace, pattern)
	}
	if input.Query != "" {
		pattern := "%" + escapeProviderLike(input.Query) + "%"
		query = query.Where("display_name LIKE ? ESCAPE '\\' OR description LIKE ? ESCAPE '\\'", pattern, pattern)
	}
	if input.Cursor != "" {
		var cursor model.TenantResource
		if err := s.db.WithContext(ctx).Select("id", "created_at").Where("tenant_id = ? AND id = ?", tenantID, input.Cursor).First(&cursor).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, ErrTenantResourceNotFound
			}
			return nil, err
		}
		query = query.Where("created_at < ? OR (created_at = ? AND id < ?)", cursor.CreatedAt, cursor.CreatedAt, cursor.ID)
	}

	var resources []model.TenantResource
	if err := query.Order("created_at DESC, id DESC").Limit(input.Limit + 1).Find(&resources).Error; err != nil {
		return nil, err
	}
	result := &TenantResourceListResult{Items: make([]TenantResourceView, 0, min(len(resources), input.Limit))}
	if len(resources) > input.Limit {
		resources = resources[:input.Limit]
		result.NextCursor = resources[len(resources)-1].ID
	}
	for i := range resources {
		view, err := tenantResourceView(s.db.WithContext(ctx), &resources[i], now, false)
		if err != nil {
			return nil, err
		}
		result.Items = append(result.Items, *view)
	}
	if !input.Candidates && input.Type == "" && result.NextCursor == "" && len(result.Items) < input.Limit {
		hostInput := input
		hostInput.Limit = input.Limit - len(result.Items)
		hosts, err := s.listHostSSHResources(ctx, tenantID, hostInput)
		if err != nil {
			return nil, err
		}
		result.Items = append(result.Items, hosts.Items...)
	}
	return result, nil
}

func (s *TenantResourceService) Get(ctx context.Context, authorization *ManagementAuthorizationContext, tenantID, resourceID string, candidate bool) (*TenantResourceView, error) {
	if s == nil || s.db == nil {
		return nil, ErrTenantResourceInvalidInput
	}
	tenantID, resourceID = strings.TrimSpace(tenantID), strings.TrimSpace(resourceID)
	if validateRequired("tenant_id", tenantID, 36) != nil || validateRequired("resource_id", resourceID, 36) != nil {
		return nil, ErrTenantResourceInvalidInput
	}
	now := s.now().UTC()
	if err := reauthorizeTenantPermission(s.db.WithContext(ctx), authorization, tenantID, PermissionTenantResourcesRead, now); err != nil {
		return nil, err
	}
	if candidate {
		memoryCandidate, err := s.memoryCandidate(ctx, tenantID, resourceID, now)
		if err != nil {
			return nil, err
		}
		view := memoryCandidate.view
		return &view, nil
	}
	query := s.db.WithContext(ctx).Where("tenant_id = ? AND id = ?", tenantID, resourceID)
	query = query.Where("visibility_state <> ?", model.TenantResourcePending)
	var resource model.TenantResource
	if err := query.First(&resource).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if !candidate {
				return s.getHostSSHResourceView(ctx, tenantID, resourceID)
			}
			return nil, ErrTenantResourceNotFound
		}
		return nil, err
	}
	return tenantResourceView(s.db.WithContext(ctx), &resource, now, true)
}

func (s *TenantResourceService) listHostSSHResources(ctx context.Context, tenantID string, input TenantResourceListInput) (*TenantResourceListResult, error) {
	if input.Candidates {
		return &TenantResourceListResult{Items: []TenantResourceView{}}, nil
	}
	query := s.db.WithContext(ctx).Model(&model.Resource{}).Where("tenant_id = ? AND type = ?", tenantID, model.ResourceTypeHostSSH)
	query = applyHostSSHResourceFilters(query, input)
	var resources []model.Resource
	if err := query.Order("created_at DESC, id DESC").Limit(input.Limit).Find(&resources).Error; err != nil {
		return nil, err
	}
	result := &TenantResourceListResult{Items: make([]TenantResourceView, 0, len(resources))}
	for i := range resources {
		view := s.hostSSHResourceView(ctx, &resources[i])
		result.Items = append(result.Items, view)
	}
	return result, nil
}

func (s *TenantResourceService) getHostSSHResourceView(ctx context.Context, tenantID, resourceID string) (*TenantResourceView, error) {
	var resource model.Resource
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND id = ? AND type = ?", tenantID, resourceID, model.ResourceTypeHostSSH).First(&resource).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTenantResourceNotFound
		}
		return nil, err
	}
	view := s.hostSSHResourceView(ctx, &resource)
	return &view, nil
}

func applyHostSSHResourceFilters(query *gorm.DB, input TenantResourceListInput) *gorm.DB {
	switch model.TenantResourceVisibilityState(input.Visibility) {
	case model.TenantResourceVisible:
		query = query.Where("state NOT IN ?", []model.ResourceState{model.ResourceStatePending, model.ResourceStateRevoked})
	case model.TenantResourcePending:
		query = query.Where("state = ?", model.ResourceStatePending)
	case model.TenantResourceRetired:
		query = query.Where("state = ?", model.ResourceStateRevoked)
	}
	switch model.TenantResourceAvailabilityState(input.Availability) {
	case model.TenantResourceAvailable:
		query = query.Where("state = ?", model.ResourceStateAvailable)
	case model.TenantResourceDegraded:
		query = query.Where("state = ?", model.ResourceStateDegraded)
	case model.TenantResourceUnavailable:
		query = query.Where("state IN ?", []model.ResourceState{model.ResourceStateDraining, model.ResourceStateStopped, model.ResourceStateRevoked})
	case model.TenantResourceUnknown:
		query = query.Where("state = ?", model.ResourceStatePending)
	}
	if input.Query != "" {
		pattern := "%" + escapeProviderLike(input.Query) + "%"
		query = query.Where("display_name LIKE ? ESCAPE '\\' OR id LIKE ? ESCAPE '\\'", pattern, pattern)
	}
	return query
}

func (s *TenantResourceService) hostSSHResourceView(ctx context.Context, resource *model.Resource) TenantResourceView {
	visibility, availability := hostSSHResourceStates(resource.State)
	view := TenantResourceView{
		ResourceID: resource.ID, Type: string(model.ResourceTypeHostSSH), DisplayName: resource.DisplayName,
		VisibilityState: visibility, AvailabilityState: availability, Revision: max(resource.TargetRevision, 1),
		RowVersion: max(resource.TargetRevision, 1), AgentNodeID: resource.AgentNodeID, Ready: availability == model.TenantResourceAvailable,
		CreatedAt: resource.CreatedAt, UpdatedAt: resource.UpdatedAt,
	}
	var domain model.DomainRegistry
	nodeID := strconv.FormatUint(resource.AgentNodeID, 10)
	if err := s.db.WithContext(ctx).Where("type = ? AND resource_kind = ? AND resource_id = ?", model.DomainTypeSSH, model.DomainResourceNode, nodeID).
		Or("type = ? AND node_id = ?", model.DomainTypeSSH, resource.AgentNodeID).
		Order("updated_at DESC, id DESC").First(&domain).Error; err == nil {
		view.SSHDomain = domain.Domain
		view.TargetIP = domain.TargetIP
		view.TargetPort = domain.TargetPort
		view.SSHUsers = domain.GetSSHUsers()
	}
	return view
}

func hostSSHResourceStates(state model.ResourceState) (model.TenantResourceVisibilityState, model.TenantResourceAvailabilityState) {
	switch state {
	case model.ResourceStateAvailable:
		return model.TenantResourceVisible, model.TenantResourceAvailable
	case model.ResourceStateDegraded:
		return model.TenantResourceVisible, model.TenantResourceDegraded
	case model.ResourceStateRevoked:
		return model.TenantResourceRetired, model.TenantResourceUnavailable
	case model.ResourceStatePending:
		return model.TenantResourcePending, model.TenantResourceUnknown
	default:
		return model.TenantResourceVisible, model.TenantResourceUnavailable
	}
}

type ReviewTenantResourceInput struct {
	TenantID            string
	ResourceID          string
	ExpectedRowVersion  int64
	ObservationRevision int64
	Reason              string
	Publish             bool
}

func (s *TenantResourceService) Review(ctx context.Context, authorization *ManagementAuthorizationContext, input ReviewTenantResourceInput) (*model.TenantResource, error) {
	if s == nil || s.db == nil {
		return nil, ErrTenantResourceInvalidInput
	}
	input.TenantID, input.ResourceID, input.Reason = strings.TrimSpace(input.TenantID), strings.TrimSpace(input.ResourceID), strings.TrimSpace(input.Reason)
	if validateRequired("tenant_id", input.TenantID, 36) != nil || validateRequired("resource_id", input.ResourceID, 36) != nil ||
		(input.Publish && input.ExpectedRowVersion <= 0) || input.ObservationRevision <= 0 || len(input.Reason) > 500 || (!input.Publish && input.Reason == "") {
		return nil, ErrTenantResourceInvalidInput
	}
	now := s.now().UTC()
	var resource model.TenantResource
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := reauthorizeTenantPermission(tx, authorization, input.TenantID, PermissionTenantResourcesWrite, now); err != nil {
			return err
		}
		var persisted model.TenantResource
		if err := tx.Where("tenant_id = ? AND id = ?", input.TenantID, input.ResourceID).First(&persisted).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			candidateService := &TenantResourceService{db: tx, now: s.now, snapshots: s.snapshots}
			candidate, candidateErr := candidateService.memoryCandidate(ctx, input.TenantID, input.ResourceID, now)
			if candidateErr != nil {
				return candidateErr
			}
			if !input.Publish && input.ExpectedRowVersion == 0 {
				input.ExpectedRowVersion = candidate.view.RowVersion
			}
			if candidate.view.RowVersion != input.ExpectedRowVersion || candidate.view.ObservationRevision != input.ObservationRevision {
				return ErrTenantResourceReviewStale
			}
			if err := materializeMemoryCandidate(tx, candidate, now); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		chain, err := loadTenantResourceChain(tx, input.TenantID, input.ResourceID, now, false)
		if err != nil {
			return err
		}
		if chain.Source.ID == "" || chain.Observation.ID == "" {
			return ErrTenantResourceUpstreamUnavailable
		}
		if input.ExpectedRowVersion == 0 {
			input.ExpectedRowVersion = chain.Resource.RowVersion
		}
		if chain.Resource.RowVersion != input.ExpectedRowVersion {
			return ErrTenantResourceRevisionConflict
		}
		if chain.Resource.VisibilityState != model.TenantResourcePending {
			return ErrTenantResourceStateTransition
		}
		if chain.Observation.ObservedRevision != input.ObservationRevision {
			return ErrTenantResourceReviewStale
		}
		if input.Publish {
			chain, err = loadTenantResourceChain(tx, input.TenantID, input.ResourceID, now, true)
			if err != nil {
				return err
			}
			if chain.Observation.ObservedRevision != input.ObservationRevision || chain.Target.ObservationRevision != input.ObservationRevision {
				return ErrTenantResourceReviewStale
			}
			if err := ensureContainerSSHBusinessDomainUnique(ctx, tx, chain, now); err != nil {
				return err
			}
			if chain.Resource.Type == model.TenantResourceContainerSSH {
				if _, err := containerSSHUsersFromTargetSnapshot(chain.Target.TargetSnapshot); err != nil {
					return ErrTenantResourceUpstreamUnavailable
				}
			}
		}
		decisionType := model.TenantResourceReviewRejected
		visibility := model.TenantResourcePending
		if input.Publish {
			decisionType = model.TenantResourceReviewPublished
			visibility = model.TenantResourceVisible
		}
		var simulationID *string
		if authorization.SimulationSessionID != "" {
			value := authorization.SimulationSessionID
			simulationID = &value
		}
		decision := model.TenantResourceReviewDecision{
			ID: uuid.NewString(), TenantResourceID: chain.Resource.ID, ObservationRevision: input.ObservationRevision,
			Decision: decisionType, ActorUserID: authorization.ActorUserID, EffectiveUserID: authorization.EffectiveUserID,
			SimulationSessionID: simulationID, Reason: input.Reason, CreatedAt: now,
		}
		if err := tx.Create(&decision).Error; err != nil {
			if strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "unique") {
				return ErrTenantResourceReviewStale
			}
			return mapTenantResourceConstraint(err)
		}
		updated := tx.Model(&model.TenantResource{}).
			Where("tenant_id = ? AND id = ? AND row_version = ? AND visibility_state = ?", input.TenantID, input.ResourceID, input.ExpectedRowVersion, model.TenantResourcePending).
			Updates(map[string]any{"visibility_state": visibility, "revision": gorm.Expr("revision + 1"), "row_version": gorm.Expr("row_version + 1")})
		if updated.Error != nil {
			return mapTenantResourceConstraint(updated.Error)
		}
		if updated.RowsAffected != 1 {
			return ErrTenantResourceRevisionConflict
		}
		return tx.Where("tenant_id = ? AND id = ?", input.TenantID, input.ResourceID).First(&resource).Error
	})
	return &resource, err
}

func materializeMemoryCandidate(tx *gorm.DB, candidate *memoryTenantResourceCandidate, now time.Time) error {
	if tx == nil || candidate == nil || candidate.view.ResourceID == "" {
		return ErrTenantResourceInvalidInput
	}
	observation := model.WorkloadObservation{
		ID:               uuid.NewSHA1(uuid.NameSpaceOID, []byte(candidate.view.ResourceID+"\x00observation")).String(),
		NamespaceScopeID: candidate.scope.ID, Kind: candidate.snapshot.Kind, StableKey: candidate.projection.StableKey,
		IdentityQuality: candidate.projection.IdentityQuality, State: model.WorkloadObservationEligible,
		Ready: candidate.projection.Ready, ObservedRevision: candidate.snapshot.Sequence,
		LabelSnapshot: candidate.projection.Labels, FirstObservedAt: candidate.snapshot.ObservedAt,
		LastObservedAt: candidate.snapshot.ReceivedAt, LeaseExpiresAt: candidate.snapshot.LeaseExpiresAt, RowVersion: 1,
	}
	if err := tx.Create(&observation).Error; err != nil {
		return err
	}
	evidence := model.WorkloadObservationSource{
		ID: uuid.NewString(), WorkloadObservationID: observation.ID,
		SourceTechnicalResourceID: candidate.snapshot.SourceTechnicalResourceID,
		SourceEpoch:               candidate.snapshot.SourceEpoch, Sequence: candidate.snapshot.Sequence,
		PayloadHash: candidate.projection.PayloadHash, State: model.WorkloadObservationSourceObserved,
		Ready: candidate.projection.Ready, TargetSnapshot: candidate.projection.Target,
		ObservedAt: candidate.snapshot.ObservedAt, ReceivedAt: candidate.snapshot.ReceivedAt,
		LeaseExpiresAt: candidate.snapshot.LeaseExpiresAt, SourceRevision: 1, RowVersion: 1,
	}
	if err := tx.Create(&evidence).Error; err != nil {
		return err
	}
	resourceType := model.TenantResourceType(candidate.view.Type)
	resource := model.TenantResource{
		ID: candidate.view.ResourceID, TenantID: candidate.allocation.TenantID, Type: resourceType,
		StableKey: candidate.projection.StableKey, EntitlementLineageID: candidate.lineage.ID,
		DisplayName: candidate.view.DisplayName, VisibilityState: model.TenantResourcePending,
		AvailabilityState: candidate.view.AvailabilityState, Revision: candidate.view.Revision, RowVersion: candidate.view.RowVersion,
	}
	if err := tx.Create(&resource).Error; err != nil {
		return err
	}
	source := model.TenantResourceSource{
		ID: uuid.NewString(), TenantResourceID: resource.ID, AllocationItemID: candidate.item.ID,
		WorkloadObservationID: observation.ID, Enabled: true, EnabledAt: now, SourceRevision: 1, RowVersion: 1,
	}
	if err := tx.Create(&source).Error; err != nil {
		return err
	}
	targets := candidate.targets
	if len(targets) == 0 {
		targets = []workloadProjection{candidate.projection}
	}
	for i, projection := range targets {
		target := model.TenantResourceTargetRevision{
			ID: uuid.NewString(), TenantResourceSourceID: source.ID, Revision: int64(i + 1), TargetType: candidate.snapshot.Kind,
			TargetSnapshot: projection.Target, SourceTechnicalResourceID: candidate.snapshot.SourceTechnicalResourceID,
			AccessTechnicalResourceID: candidate.snapshot.SourceTechnicalResourceID, Ready: projection.Ready,
			ObservedAt: candidate.snapshot.ObservedAt, ObservationRevision: candidate.snapshot.Sequence,
			SourceRevision: source.SourceRevision, CreatedAt: now,
		}
		if err := tx.Create(&target).Error; err != nil {
			return err
		}
	}
	return nil
}

type UpdateTenantResourceInput struct {
	TenantID           string
	ResourceID         string
	ExpectedRowVersion int64
	DisplayName        *string
	Description        *string
}

func (s *TenantResourceService) Update(ctx context.Context, authorization *ManagementAuthorizationContext, input UpdateTenantResourceInput) (*model.TenantResource, error) {
	if s == nil || s.db == nil || input.DisplayName == nil && input.Description == nil {
		return nil, ErrTenantResourceInvalidInput
	}
	input.TenantID, input.ResourceID = strings.TrimSpace(input.TenantID), strings.TrimSpace(input.ResourceID)
	if validateRequired("tenant_id", input.TenantID, 36) != nil || validateRequired("resource_id", input.ResourceID, 36) != nil || input.ExpectedRowVersion <= 0 {
		return nil, ErrTenantResourceInvalidInput
	}
	updates := map[string]any{"revision": gorm.Expr("revision + 1"), "row_version": gorm.Expr("row_version + 1")}
	if input.DisplayName != nil {
		value := strings.TrimSpace(*input.DisplayName)
		if validateRequired("display_name", value, 200) != nil {
			return nil, ErrTenantResourceInvalidInput
		}
		updates["display_name"] = value
	}
	if input.Description != nil {
		value := strings.TrimSpace(*input.Description)
		if len(value) > 1000 {
			return nil, ErrTenantResourceInvalidInput
		}
		updates["description"] = value
	}
	now := s.now().UTC()
	var resource model.TenantResource
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := reauthorizeTenantPermission(tx, authorization, input.TenantID, PermissionTenantResourcesWrite, now); err != nil {
			return err
		}
		var current model.TenantResource
		if err := tx.Where("tenant_id = ? AND id = ? AND visibility_state <> ?", input.TenantID, input.ResourceID, model.TenantResourcePending).First(&current).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrTenantResourceNotFound
			}
			return err
		}
		if current.RowVersion != input.ExpectedRowVersion {
			return ErrTenantResourceRevisionConflict
		}
		result := tx.Model(&model.TenantResource{}).Where("tenant_id = ? AND id = ? AND row_version = ?", input.TenantID, input.ResourceID, input.ExpectedRowVersion).Updates(updates)
		if result.Error != nil {
			return mapTenantResourceConstraint(result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrTenantResourceRevisionConflict
		}
		return tx.Where("tenant_id = ? AND id = ?", input.TenantID, input.ResourceID).First(&resource).Error
	})
	return &resource, err
}

func (s *TenantResourceService) SetVisibility(ctx context.Context, authorization *ManagementAuthorizationContext, tenantID, resourceID string, expectedRowVersion int64, target model.TenantResourceVisibilityState) (*model.TenantResource, error) {
	if s == nil || s.db == nil || (target != model.TenantResourceHidden && target != model.TenantResourceVisible) {
		return nil, ErrTenantResourceInvalidInput
	}
	tenantID, resourceID = strings.TrimSpace(tenantID), strings.TrimSpace(resourceID)
	if validateRequired("tenant_id", tenantID, 36) != nil || validateRequired("resource_id", resourceID, 36) != nil || expectedRowVersion <= 0 {
		return nil, ErrTenantResourceInvalidInput
	}
	now := s.now().UTC()
	var resource model.TenantResource
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := reauthorizeTenantPermission(tx, authorization, tenantID, PermissionTenantResourcesWrite, now); err != nil {
			return err
		}
		var current model.TenantResource
		if err := tx.Where("tenant_id = ? AND id = ?", tenantID, resourceID).First(&current).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrTenantResourceNotFound
			}
			return err
		}
		if current.RowVersion != expectedRowVersion {
			return ErrTenantResourceRevisionConflict
		}
		valid := (current.VisibilityState == model.TenantResourceVisible && target == model.TenantResourceHidden) ||
			(current.VisibilityState == model.TenantResourceHidden && target == model.TenantResourceVisible)
		if !valid {
			return ErrTenantResourceStateTransition
		}
		if target == model.TenantResourceVisible {
			chain, err := loadTenantResourceChain(tx, tenantID, resourceID, now, true)
			if err != nil {
				return err
			}
			if err := ensureContainerSSHBusinessDomainUnique(ctx, tx, chain, now); err != nil {
				return err
			}
		}
		result := tx.Model(&model.TenantResource{}).Where("tenant_id = ? AND id = ? AND row_version = ? AND visibility_state = ?", tenantID, resourceID, expectedRowVersion, current.VisibilityState).
			Updates(map[string]any{"visibility_state": target, "revision": gorm.Expr("revision + 1"), "row_version": gorm.Expr("row_version + 1")})
		if result.Error != nil {
			return mapTenantResourceConstraint(result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrTenantResourceRevisionConflict
		}
		return tx.Where("tenant_id = ? AND id = ?", tenantID, resourceID).First(&resource).Error
	})
	return &resource, err
}

func ensureContainerSSHBusinessDomainUnique(ctx context.Context, tx *gorm.DB, chain *tenantResourceChain, now time.Time) error {
	if chain == nil || chain.Resource.Type != model.TenantResourceContainerSSH {
		return nil
	}
	domain, err := ContainerSSHBusinessDomain(ctx, tx, chain.Target.AccessTechnicalResourceID, chain.Target.TargetSnapshot)
	if err != nil {
		return err
	}
	var visible []model.TenantResource
	if err := tx.Where("type = ? AND visibility_state = ? AND id <> ?", model.TenantResourceContainerSSH, model.TenantResourceVisible, chain.Resource.ID).Find(&visible).Error; err != nil {
		return err
	}
	for i := range visible {
		other, err := loadTenantResourceChain(tx, visible[i].TenantID, visible[i].ID, now, false)
		if err != nil || other.Target.ID == "" {
			continue
		}
		otherDomain, err := ContainerSSHBusinessDomain(ctx, tx, other.Target.AccessTechnicalResourceID, other.Target.TargetSnapshot)
		if err != nil {
			continue
		}
		if otherDomain == domain && visible[i].StableKey != chain.Resource.StableKey {
			return ErrContainerSSHBusinessDomainConflict
		}
	}
	return nil
}

func reauthorizeTenantPermission(tx *gorm.DB, authorization *ManagementAuthorizationContext, tenantID, permission string, at time.Time) error {
	if tx == nil || authorization == nil || authorization.ScopeType != model.ManagementScopeTenant || authorization.ScopeID != tenantID ||
		authorization.ActorUserID == 0 || authorization.EffectiveUserID == 0 || authorization.PermissionRevision <= 0 || at.IsZero() {
		return ErrManagementPermissionDenied
	}
	var current *ManagementAuthorizationContext
	var err error
	if authorization.SimulationSessionID == "" {
		if authorization.ActorUserID != authorization.EffectiveUserID {
			return ErrManagementPermissionDenied
		}
		current, err = ResolveManagementContext(tx, authorization.EffectiveUserID, model.ManagementScopeTenant, tenantID, at, false)
	} else {
		_, current, err = ResolveUserSimulationSession(tx, authorization.SimulationSessionID, authorization.ActorUserID, at)
	}
	if err != nil || current == nil || current.ScopeType != model.ManagementScopeTenant || current.ScopeID != tenantID ||
		current.ActorUserID != authorization.ActorUserID || current.EffectiveUserID != authorization.EffectiveUserID ||
		current.PermissionRevision != authorization.PermissionRevision || current.SimulationSessionID != authorization.SimulationSessionID {
		return ErrManagementPermissionDenied
	}
	return AuthorizeManagementPermission(current, permission)
}

func loadTenantResourceChain(tx *gorm.DB, tenantID, resourceID string, now time.Time, requireTrusted bool) (*tenantResourceChain, error) {
	return loadTenantResourceChainForTarget(tx, tenantID, resourceID, "", now, requireTrusted)
}

func loadTenantResourceChainForTarget(tx *gorm.DB, tenantID, resourceID, targetRevisionID string, now time.Time, requireTrusted bool) (*tenantResourceChain, error) {
	var chain tenantResourceChain
	targetUntrusted := false
	if err := tx.Where("tenant_id = ? AND id = ?", tenantID, resourceID).First(&chain.Resource).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTenantResourceNotFound
		}
		return nil, err
	}
	var sources []model.TenantResourceSource
	query := tx.Where("tenant_resource_id = ?", chain.Resource.ID)
	if requireTrusted {
		query = query.Where("enabled = ?", true)
	}
	if err := query.Order("enabled DESC, source_revision DESC, id ASC").Find(&sources).Error; err != nil {
		return nil, err
	}
	for i := range sources {
		candidate := tenantResourceChain{Resource: chain.Resource, Source: sources[i]}
		if err := tx.First(&candidate.Observation, "id = ?", sources[i].WorkloadObservationID).Error; err != nil {
			continue
		}
		if err := tx.First(&candidate.Item, "id = ?", sources[i].AllocationItemID).Error; err != nil {
			continue
		}
		if err := tx.First(&candidate.Allocation, "id = ? AND tenant_id = ?", candidate.Item.AllocationID, tenantID).Error; err != nil {
			continue
		}
		if err := tx.First(&candidate.Scope, "id = ?", candidate.Item.ScopeID).Error; err != nil {
			continue
		}
		if candidate.Scope.NamespaceObservationID != nil {
			_ = tx.First(&candidate.Namespace, "id = ?", *candidate.Scope.NamespaceObservationID).Error
		}
		targetQuery := tx.Where("tenant_resource_source_id = ? AND superseded_at IS NULL", sources[i].ID)
		if targetRevisionID != "" {
			targetQuery = targetQuery.Where("id = ?", targetRevisionID)
		}
		targetQuery = targetQuery.Order("revision DESC")
		if err := targetQuery.First(&candidate.Target).Error; err != nil {
			if requireTrusted {
				continue
			}
			return &candidate, nil
		}
		if !requireTrusted {
			return &candidate, nil
		}
		if !validTenantTargetSnapshot(candidate.Resource.Type, candidate.Target.TargetType, candidate.Target.TargetSnapshot) {
			targetUntrusted = true
			continue
		}
		if !sources[i].Enabled || candidate.Allocation.State != model.ResourceAllocationActive || candidate.Allocation.ValidFrom.After(now) ||
			(candidate.Allocation.ExpiresAt != nil && !candidate.Allocation.ExpiresAt.After(now)) ||
			!candidate.Target.Ready || candidate.Target.ObservationRevision != candidate.Observation.ObservedRevision || candidate.Target.SourceRevision != sources[i].SourceRevision {
			continue
		}
		if _, err := loadAllocatableNamespaceScope(tx, candidate.Scope.ID, now); err != nil {
			continue
		}
		if err := tx.Where("workload_observation_id = ? AND source_technical_resource_id = ?",
			candidate.Observation.ID, candidate.Target.SourceTechnicalResourceID).First(&candidate.Evidence).Error; err != nil {
			continue
		}
		var technical model.TechnicalResource
		if err := tx.First(&technical, "id = ?", candidate.Target.AccessTechnicalResourceID).Error; err != nil {
			continue
		}
		valid, err := workloadTechnicalResourceBindingCurrent(tx, &technical)
		if err != nil {
			return nil, err
		}
		if !valid {
			continue
		}
		capable, err := workloadSourceHasCapability(tx, &technical, candidate.Scope.PlatformResourceID, now)
		if err != nil {
			return nil, err
		}
		if !capable {
			continue
		}
		return &candidate, nil
	}
	if requireTrusted {
		if targetUntrusted {
			return nil, ErrTenantResourceTargetNotTrusted
		}
		return nil, ErrTenantResourceUpstreamUnavailable
	}
	return &chain, nil
}

func validTenantTargetSnapshot(resourceType model.TenantResourceType, targetType model.WorkloadObservationKind, snapshot string) bool {
	switch resourceType {
	case model.TenantResourceContainerService:
		if targetType != model.WorkloadObservationServicePort {
			return false
		}
		var target struct {
			ServiceUID  string `json:"service_uid"`
			ServiceName string `json:"service_name"`
			PortNumber  int    `json:"port_number"`
			Protocol    string `json:"protocol"`
		}
		return json.Unmarshal([]byte(snapshot), &target) == nil && strings.TrimSpace(target.ServiceUID) != "" &&
			strings.TrimSpace(target.ServiceName) != "" && target.PortNumber > 0 && target.PortNumber <= 65535 && target.Protocol == "TCP"
	case model.TenantResourceContainerSSH:
		if targetType != model.WorkloadObservationContainer {
			return false
		}
		var target struct {
			PodUID        string `json:"pod_uid"`
			PodName       string `json:"pod_name"`
			ContainerName string `json:"container_name"`
		}
		return json.Unmarshal([]byte(snapshot), &target) == nil && strings.TrimSpace(target.PodUID) != "" &&
			strings.TrimSpace(target.PodName) != "" && strings.TrimSpace(target.ContainerName) != ""
	default:
		return false
	}
}

func tenantResourceView(tx *gorm.DB, resource *model.TenantResource, now time.Time, includeRuntimeIdentity bool) (*TenantResourceView, error) {
	view := &TenantResourceView{
		ResourceID: resource.ID, Type: string(resource.Type), DisplayName: resource.DisplayName, Description: resource.Description,
		VisibilityState: resource.VisibilityState, AvailabilityState: resource.AvailabilityState,
		Revision: resource.Revision, RowVersion: resource.RowVersion, CreatedAt: resource.CreatedAt, UpdatedAt: resource.UpdatedAt,
	}
	chain, err := loadTenantResourceChain(tx, resource.TenantID, resource.ID, now, false)
	if err != nil {
		return nil, err
	}
	view.NamespaceScopeID = chain.Scope.ID
	view.NamespaceName = chain.Namespace.Name
	view.ObservationRevision = chain.Observation.ObservedRevision
	view.TargetRevision = chain.Target.Revision
	view.Ready = chain.Target.Ready && chain.Target.SupersededAt == nil
	if chain.Target.ID == "" {
		return view, nil
	}
	if resource.Type == model.TenantResourceContainerSSH {
		view.SSHUsers, err = containerSSHUsersFromTargetSnapshot(chain.Target.TargetSnapshot)
		if err != nil {
			return nil, ErrTenantResourceInvalidInput
		}
	}
	if resource.Type == model.TenantResourceContainerService {
		var target struct {
			ServiceUID    string `json:"service_uid"`
			ServiceName   string `json:"service_name"`
			PortName      string `json:"port_name"`
			PortNumber    int    `json:"port_number"`
			Protocol      string `json:"protocol"`
			NamespaceName string `json:"namespace_name"`
		}
		if err := json.Unmarshal([]byte(chain.Target.TargetSnapshot), &target); err != nil {
			return nil, ErrTenantResourceTargetNotTrusted
		}
		if view.NamespaceName == "" {
			view.NamespaceName = target.NamespaceName
		}
		view.ServiceUID, view.ServiceName = target.ServiceUID, target.ServiceName
		view.PortName, view.PortNumber, view.Protocol = target.PortName, target.PortNumber, target.Protocol
	} else {
		var target struct {
			WorkloadUID   string `json:"workload_uid"`
			WorkloadKind  string `json:"workload_kind"`
			WorkloadName  string `json:"workload_name"`
			PodUID        string `json:"pod_uid"`
			PodName       string `json:"pod_name"`
			ContainerName string `json:"container_name"`
			NamespaceName string `json:"namespace_name"`
		}
		if err := json.Unmarshal([]byte(chain.Target.TargetSnapshot), &target); err != nil {
			return nil, ErrTenantResourceTargetNotTrusted
		}
		if view.NamespaceName == "" {
			view.NamespaceName = target.NamespaceName
		}
		if !includeRuntimeIdentity {
			target.PodUID = ""
			target.PodName = ""
		}
		view.WorkloadUID, view.WorkloadKind, view.WorkloadName = target.WorkloadUID, target.WorkloadKind, target.WorkloadName
		view.PodUID, view.PodName, view.ContainerName = target.PodUID, target.PodName, target.ContainerName
		view.IdentityQuality = chain.Observation.IdentityQuality
	}
	return view, nil
}

func containerSSHUsersFromTargetSnapshot(snapshot string) ([]string, error) {
	var target struct {
		SSHUsers []string `json:"ssh_users"`
	}
	if json.Unmarshal([]byte(snapshot), &target) != nil || len(target.SSHUsers) != 1 {
		return nil, ErrTenantResourceInvalidInput
	}
	user := strings.TrimSpace(target.SSHUsers[0])
	if !validContainerSSHUser(user) {
		return nil, ErrTenantResourceInvalidInput
	}
	return []string{user}, nil
}

func validContainerSSHUser(user string) bool {
	if len(user) == 0 || len(user) > 32 || user[0] == '-' {
		return false
	}
	for _, char := range user {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_' || char == '-' {
			continue
		}
		return false
	}
	return true
}

func validTenantResourceVisibility(value model.TenantResourceVisibilityState) bool {
	return value == model.TenantResourcePending || value == model.TenantResourceVisible || value == model.TenantResourceHidden || value == model.TenantResourceRetired
}

func validTenantResourceAvailability(value model.TenantResourceAvailabilityState) bool {
	return value == model.TenantResourceUnknown || value == model.TenantResourceAvailable || value == model.TenantResourceDegraded || value == model.TenantResourceUnavailable
}

func mapTenantResourceConstraint(err error) error {
	if err == nil {
		return nil
	}
	message := err.Error()
	switch {
	case strings.Contains(message, "S4_TENANT_RESOURCE_VERSION_INVALID"):
		return ErrTenantResourceRevisionConflict
	case strings.Contains(message, "S4_REVIEW_OBSERVATION_REVISION_MISMATCH"):
		return ErrTenantResourceReviewStale
	case strings.Contains(message, "S4_TARGET_REVISION"):
		return ErrTenantResourceTargetNotTrusted
	case strings.Contains(message, "S4_RESOURCE_"):
		return ErrTenantResourceUpstreamUnavailable
	case isDatabaseConstraintError(err):
		return ErrTenantResourceInvalidInput
	default:
		return err
	}
}
