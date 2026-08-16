package service

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

const providerSupplyMaxPageSize = 100

type ProviderSupplyListInput struct {
	Search   string
	Type     string
	State    string
	Page     int
	PageSize int
}

type TechnicalResourceListResult struct {
	Items []TechnicalResourceView `json:"items"`
	Total int64                   `json:"total"`
}

type TechnicalResourceDetail struct {
	Resource            *TechnicalResourceView           `json:"resource"`
	Bindings            []model.TechnicalResourceBinding `json:"bindings"`
	Endpoints           []TechnicalResourceView          `json:"endpoints"`
	AffectedDomainCount int64                            `json:"affected_domain_count"`
	ActiveSessionCount  int64                            `json:"active_session_count"`
}

// TechnicalResourceView projects the Provider-owned identity together with its
// current legacy runtime binding. Runtime fields are read-only compatibility
// data; Provider ownership continues to come exclusively from TechnicalResource.
type TechnicalResourceView struct {
	model.TechnicalResource
	DisplayName           string `gorm:"column:display_name" json:"display_name"`
	Hostname              string `gorm:"column:hostname" json:"hostname"`
	HostDomainLabel       string `gorm:"column:host_domain_label" json:"host_domain_label"`
	DomainNamespace       string `gorm:"column:domain_namespace" json:"domain_namespace"`
	HostnameSource        string `gorm:"column:hostname_source" json:"hostname_source,omitempty"`
	ParentHostname        string `gorm:"column:parent_hostname" json:"parent_hostname,omitempty"`
	Version               string `gorm:"column:version" json:"version,omitempty"`
	CommitID              string `gorm:"column:commit_id" json:"commit_id,omitempty"`
	CommitDate            string `gorm:"column:commit_date" json:"commit_date,omitempty"`
	BinarySHA256          string `gorm:"column:binary_sha256" json:"binary_sha256,omitempty"`
	UpdaterProtocol       string `gorm:"column:updater_protocol" json:"updater_protocol,omitempty"`
	SSHEnabled            bool   `gorm:"column:ssh_enabled" json:"ssh_enabled"`
	ContainerSSHEnabled   bool   `gorm:"column:container_ssh_enabled" json:"container_ssh_enabled"`
	K8SEnabled            bool   `gorm:"column:k8s_enabled" json:"k8s_enabled"`
	SVCEnabled            bool   `gorm:"column:svc_enabled" json:"svc_enabled"`
	EndpointAccessEnabled bool   `gorm:"column:endpoint_access_enabled" json:"endpoint_access_enabled"`
	EndpointCount         int64  `gorm:"column:endpoint_count" json:"endpoint_count"`
}

type SupplyCandidateListResult struct {
	Items []model.SupplyCandidate `json:"items"`
	Total int64                   `json:"total"`
}

type PlatformResourceListResult struct {
	Items []PlatformResourceView `json:"items"`
	Total int64                  `json:"total"`
}

type PlatformResourceDetail struct {
	Resource *PlatformResourceView          `json:"resource"`
	Sources  []model.PlatformResourceSource `json:"sources"`
	Scopes   []model.ResourceScope          `json:"scopes"`
}

type PlatformResourceView struct {
	model.PlatformResource
	AccessDomain    string `gorm:"column:access_domain" json:"access_domain"`
	HostDomainLabel string `gorm:"column:host_domain_label" json:"host_domain_label,omitempty"`
	SourceNodeID    uint64 `gorm:"column:source_node_id" json:"source_node_id,omitempty"`
}

type ResourceScopeListInput struct {
	ProviderSupplyListInput
	PlatformResourceID string
}

type ResourceScopeListResult struct {
	Items []ResourceScopeView `json:"items"`
	Total int64               `json:"total"`
}

type ResourceScopeDetail struct {
	Scope       *model.ResourceScope        `json:"scope"`
	Observation *model.NamespaceObservation `json:"observation,omitempty"`
}

type ResourceScopeView struct {
	model.ResourceScope
	PlatformResourceDisplayName  string `gorm:"column:platform_resource_display_name" json:"platform_resource_display_name"`
	PlatformResourceStableKey    string `gorm:"column:platform_resource_stable_key" json:"platform_resource_stable_key"`
	PlatformResourceAccessDomain string `gorm:"column:platform_resource_access_domain" json:"platform_resource_access_domain"`
}

func (s *ProviderSupplyService) ListTechnicalResources(ctx context.Context, authorization *ManagementAuthorizationContext, input ProviderSupplyListInput) (*TechnicalResourceListResult, error) {
	providerID, query, err := s.providerReadQuery(ctx, authorization, &input, PermissionProviderTechnicalResourcesRead)
	if err != nil {
		return nil, err
	}
	if input.Type != "" {
		return nil, ErrProviderSupplyInvalidInput
	}
	query = query.Where("technical_resource.type = ? AND technical_resource.deleted_at IS NULL", model.TechnicalResourceAgent)
	if input.State != "" {
		state := model.TechnicalResourceLifecycleState(input.State)
		if state != model.TechnicalResourcePending && state != model.TechnicalResourceRegistered &&
			state != model.TechnicalResourceDisabled && state != model.TechnicalResourceRetired {
			return nil, ErrProviderSupplyInvalidInput
		}
		query = query.Where("technical_resource.lifecycle_state = ?", state)
	}
	query = technicalResourceProjectionQuery(query)
	if input.Search != "" {
		pattern := "%" + escapeProviderLike(input.Search) + "%"
		query = query.Where(`(technical_resource.stable_key LIKE ? ESCAPE '\'
			OR runtime_user.alias LIKE ? ESCAPE '\'
			OR agent_node.hostname LIKE ? ESCAPE '\'
			OR agent_node.name LIKE ? ESCAPE '\'
			OR agent_node.host_domain_label LIKE ? ESCAPE '\')`, pattern, pattern, pattern, pattern, pattern)
	}
	result := &TechnicalResourceListResult{Items: []TechnicalResourceView{}}
	query = query.Where("technical_resource.provider_id = ?", providerID)
	if err := query.Count(&result.Total).Error; err != nil {
		return nil, err
	}
	if err := query.Select(technicalResourceProjectionSelect()).
		Order("technical_resource.created_at DESC, technical_resource.id ASC").
		Offset((input.Page - 1) * input.PageSize).Limit(input.PageSize).Scan(&result.Items).Error; err != nil {
		return nil, err
	}
	return result, nil
}

func (s *ProviderSupplyService) GetTechnicalResource(ctx context.Context, authorization *ManagementAuthorizationContext, resourceID string) (*TechnicalResourceDetail, error) {
	input := ProviderSupplyListInput{}
	providerID, query, err := s.providerReadQuery(ctx, authorization, &input, PermissionProviderTechnicalResourcesRead)
	if err != nil {
		return nil, err
	}
	resourceID = strings.TrimSpace(resourceID)
	if validateRequired("technical_resource_id", resourceID, 36) != nil {
		return nil, ErrProviderSupplyInvalidInput
	}
	var resource TechnicalResourceView
	if err := technicalResourceProjectionQuery(query).
		Select(technicalResourceProjectionSelect()).
		Where("technical_resource.provider_id = ? AND technical_resource.id = ? AND technical_resource.deleted_at IS NULL", providerID, resourceID).
		Take(&resource).Error; err != nil {
		return nil, providerSupplyNotFound(err)
	}
	bindings := []model.TechnicalResourceBinding{}
	if err := query.Where("technical_resource_id = ?", resource.ID).Order("created_at DESC, id ASC").Find(&bindings).Error; err != nil {
		return nil, err
	}
	endpoints := []TechnicalResourceView{}
	if resource.Type == model.TechnicalResourceAgent {
		if err := technicalResourceProjectionQuery(query).
			Select(technicalResourceProjectionSelect()).
			Where("technical_resource.provider_id = ? AND technical_resource.parent_id = ? AND technical_resource.type = ? AND technical_resource.deleted_at IS NULL",
				providerID, resource.ID, model.TechnicalResourceEndpoint).
			Order("technical_resource.created_at ASC, technical_resource.id ASC").
			Scan(&endpoints).Error; err != nil {
			return nil, err
		}
	}
	var affectedDomainCount int64
	if err := s.db.WithContext(ctx).Model(&model.DomainRegistry{}).Where("agent_resource_id = ?", resource.ID).Count(&affectedDomainCount).Error; err != nil {
		return nil, err
	}
	var activeSessionCount int64
	if err := s.db.WithContext(ctx).Model(&model.ResourceSession{}).
		Where("status IN ?", []model.ResourceSessionStatus{model.ResourceSessionAuthorizing, model.ResourceSessionActive}).
		Where(`access_technical_resource_id = ? OR access_technical_resource_id IN (
			SELECT id FROM technical_resource WHERE parent_id = ? AND deleted_at IS NULL
		)`, resource.ID, resource.ID).
		Count(&activeSessionCount).Error; err != nil {
		return nil, err
	}
	return &TechnicalResourceDetail{
		Resource: &resource, Bindings: bindings, Endpoints: endpoints,
		AffectedDomainCount: affectedDomainCount, ActiveSessionCount: activeSessionCount,
	}, nil
}

func technicalResourceProjectionQuery(query *gorm.DB) *gorm.DB {
	return query.Table("technical_resource").
		Joins("JOIN resource_provider projection_provider ON projection_provider.id = technical_resource.provider_id").
		Joins(`LEFT JOIN technical_resource_binding active_binding
			ON active_binding.id = (SELECT selected_binding.id FROM technical_resource_binding selected_binding
				WHERE selected_binding.technical_resource_id = technical_resource.id AND selected_binding.enabled = ?
				ORDER BY selected_binding.created_at DESC, selected_binding.id ASC LIMIT 1)`, true).
		Joins(`LEFT JOIN node agent_node
			ON technical_resource.type = ? AND active_binding.source_type = ?
			AND CAST(agent_node.id AS TEXT) = active_binding.source_id`, model.TechnicalResourceAgent, model.TechnicalResourceBindingLegacyNode).
		Joins(`LEFT JOIN user runtime_user ON runtime_user.id = technical_resource.runtime_user_id`).
		Joins(`LEFT JOIN user agent_user ON agent_user.id = agent_node.user_id`).
		Joins(`LEFT JOIN endpoint bound_endpoint
			ON technical_resource.type = ? AND active_binding.source_type = ?
			AND bound_endpoint.id = active_binding.source_id`, model.TechnicalResourceEndpoint, model.TechnicalResourceBindingLegacyEndpoint).
		Joins(`LEFT JOIN technical_resource parent_resource ON parent_resource.id = technical_resource.parent_id`).
		Joins(`LEFT JOIN technical_resource_binding parent_binding
			ON parent_binding.id = (SELECT selected_parent_binding.id FROM technical_resource_binding selected_parent_binding
				WHERE selected_parent_binding.technical_resource_id = parent_resource.id AND selected_parent_binding.enabled = ?
				ORDER BY selected_parent_binding.created_at DESC, selected_parent_binding.id ASC LIMIT 1)`, true).
		Joins(`LEFT JOIN node parent_node
			ON parent_binding.source_type = ? AND CAST(parent_node.id AS TEXT) = parent_binding.source_id`, model.TechnicalResourceBindingLegacyNode)
}

func technicalResourceProjectionSelect() string {
	return `technical_resource.*,
		CASE WHEN technical_resource.type = 'agent'
			THEN COALESCE(NULLIF(runtime_user.alias, ''), technical_resource.domain_label)
			ELSE COALESCE(NULLIF(bound_endpoint.alias, ''), bound_endpoint.name, '') END AS display_name,
		CASE WHEN technical_resource.type = 'agent' AND projection_provider.domain_scope = 'root' THEN technical_resource.domain_label
			WHEN technical_resource.type = 'agent' THEN technical_resource.domain_label || '.' || projection_provider.domain_label
			WHEN technical_resource.type = 'endpoint' AND projection_provider.domain_scope = 'root' THEN parent_resource.domain_label
			WHEN technical_resource.type = 'endpoint' THEN parent_resource.domain_label || '.' || projection_provider.domain_label
			ELSE '' END AS domain_namespace,
		CASE WHEN technical_resource.type = 'agent'
			THEN COALESCE(NULLIF(agent_node.hostname, ''), agent_node.name, '')
			ELSE COALESCE(bound_endpoint.name, '') END AS hostname,
		CASE WHEN technical_resource.type = 'agent' THEN COALESCE(agent_node.host_domain_label, '')
			ELSE COALESCE(bound_endpoint.host_domain_label, '') END AS host_domain_label,
		CASE WHEN technical_resource.type = 'agent' AND COALESCE(agent_node.hostname, '') <> '' THEN 'reported'
			WHEN technical_resource.type = 'agent' AND agent_node.id IS NOT NULL THEN 'legacy_name'
			WHEN technical_resource.type = 'endpoint' AND bound_endpoint.id IS NOT NULL THEN 'legacy_name'
			ELSE '' END AS hostname_source,
		COALESCE(NULLIF(parent_node.hostname, ''), parent_node.name, '') AS parent_hostname,
		CASE WHEN technical_resource.type = 'agent' THEN COALESCE(agent_node.version, '')
			ELSE COALESCE(bound_endpoint.version, '') END AS version,
		CASE WHEN technical_resource.type = 'agent' THEN COALESCE(agent_node.commit_id, '')
			ELSE '' END AS commit_id,
		CASE WHEN technical_resource.type = 'agent' AND agent_node.commit_date IS NOT NULL
			THEN strftime('%Y-%m-%dT%H:%M:%SZ', agent_node.commit_date)
			ELSE '' END AS commit_date,
		CASE WHEN technical_resource.type = 'agent' THEN COALESCE(agent_node.binary_sha256, '')
			ELSE '' END AS binary_sha256,
		CASE WHEN technical_resource.type = 'agent' THEN COALESCE(agent_node.updater_protocol, '')
			ELSE COALESCE(bound_endpoint.updater_protocol, '') END AS updater_protocol,
		CASE WHEN technical_resource.type = 'agent' THEN COALESCE(agent_user.ssh_enabled, false)
			ELSE COALESCE(bound_endpoint.ssh_enabled, false) END AS ssh_enabled,
		CASE WHEN technical_resource.type = 'agent' THEN COALESCE(agent_node.container_ssh_protocol, '') <> ''
			ELSE false END AS container_ssh_enabled,
		CASE WHEN technical_resource.type = 'agent' THEN COALESCE(agent_node.k8s_enabled, false)
			ELSE COALESCE(bound_endpoint.k8sapi_enabled, false) END AS k8s_enabled,
		CASE WHEN technical_resource.type = 'agent' THEN COALESCE(agent_node.svc_enabled, false)
			ELSE COALESCE(bound_endpoint.k8sservice_enabled, false) END AS svc_enabled,
		CASE WHEN technical_resource.type = 'agent' THEN COALESCE(agent_node.endpoint_enabled, false)
			ELSE false END AS endpoint_access_enabled,
		CASE WHEN technical_resource.type = 'agent' THEN (
			SELECT COUNT(*) FROM technical_resource child_endpoint
			WHERE child_endpoint.parent_id = technical_resource.id
				AND child_endpoint.provider_id = technical_resource.provider_id
				AND child_endpoint.type = 'endpoint' AND child_endpoint.deleted_at IS NULL
		) ELSE 0 END AS endpoint_count`
}

func (s *ProviderSupplyService) ListSupplyCandidates(ctx context.Context, authorization *ManagementAuthorizationContext, input ProviderSupplyListInput) (*SupplyCandidateListResult, error) {
	providerID, query, err := s.providerReadQuery(ctx, authorization, &input, PermissionProviderResourcesRead)
	if err != nil {
		return nil, err
	}
	if input.Type != "" {
		typeValue := model.SupplyResourceType(input.Type)
		if typeValue != model.SupplyResourceKubernetes && typeValue != model.SupplyResourceHost {
			return nil, ErrProviderSupplyInvalidInput
		}
		query = query.Where("resource_type = ?", typeValue)
	}
	if input.State != "" {
		state := model.SupplyCandidateReviewState(input.State)
		switch state {
		case model.SupplyCandidateObserved, model.SupplyCandidatePendingReview, model.SupplyCandidateAccepted,
			model.SupplyCandidateLinked, model.SupplyCandidateConflict, model.SupplyCandidateRejected:
		default:
			return nil, ErrProviderSupplyInvalidInput
		}
		query = query.Where("review_state = ?", state)
	}
	if input.Search != "" {
		pattern := "%" + escapeProviderLike(input.Search) + "%"
		query = query.Where("stable_key LIKE ? ESCAPE '\\' OR conflict_code LIKE ? ESCAPE '\\'", pattern, pattern)
	}
	result := &SupplyCandidateListResult{Items: []model.SupplyCandidate{}}
	query = query.Model(&model.SupplyCandidate{}).Where("provider_id = ?", providerID)
	if err := query.Count(&result.Total).Error; err != nil {
		return nil, err
	}
	if err := query.Order("last_observed_at DESC, id ASC").Offset((input.Page - 1) * input.PageSize).Limit(input.PageSize).Find(&result.Items).Error; err != nil {
		return nil, err
	}
	return result, nil
}

func (s *ProviderSupplyService) GetSupplyCandidate(ctx context.Context, authorization *ManagementAuthorizationContext, candidateID string) (*model.SupplyCandidate, error) {
	input := ProviderSupplyListInput{}
	providerID, query, err := s.providerReadQuery(ctx, authorization, &input, PermissionProviderResourcesRead)
	if err != nil {
		return nil, err
	}
	candidateID = strings.TrimSpace(candidateID)
	if validateRequired("candidate_id", candidateID, 36) != nil {
		return nil, ErrProviderSupplyInvalidInput
	}
	var candidate model.SupplyCandidate
	if err := query.Where("provider_id = ? AND id = ?", providerID, candidateID).First(&candidate).Error; err != nil {
		return nil, providerSupplyNotFound(err)
	}
	return &candidate, nil
}

func (s *ProviderSupplyService) ListPlatformResources(ctx context.Context, authorization *ManagementAuthorizationContext, input ProviderSupplyListInput) (*PlatformResourceListResult, error) {
	providerID, query, err := s.providerReadQuery(ctx, authorization, &input, PermissionProviderResourcesRead)
	if err != nil {
		return nil, err
	}
	if input.Type != "" {
		typeValue := model.SupplyResourceType(input.Type)
		if typeValue != model.SupplyResourceKubernetes && typeValue != model.SupplyResourceHost {
			return nil, ErrProviderSupplyInvalidInput
		}
		query = query.Where("type = ?", typeValue)
	}
	if input.State != "" {
		state := model.PlatformResourceLifecycleState(input.State)
		if state != model.PlatformResourceDraft && state != model.PlatformResourceActive &&
			state != model.PlatformResourceSuspended && state != model.PlatformResourceRetired {
			return nil, ErrProviderSupplyInvalidInput
		}
		query = query.Where("lifecycle_state = ?", state)
	} else {
		query = query.Where("lifecycle_state <> ?", model.PlatformResourceRetired)
	}
	if input.Search != "" {
		pattern := "%" + escapeProviderLike(input.Search) + "%"
		query = query.Where("display_name LIKE ? ESCAPE '\\' OR stable_key LIKE ? ESCAPE '\\' OR "+platformResourceAccessDomainSubquery()+" LIKE ? ESCAPE '\\'", pattern, pattern, pattern)
	}
	result := &PlatformResourceListResult{Items: []PlatformResourceView{}}
	query = query.Model(&model.PlatformResource{}).Where("provider_id = ?", providerID)
	if err := query.Count(&result.Total).Error; err != nil {
		return nil, err
	}
	if err := query.Select(platformResourceProjectionSelect()).
		Order("created_at DESC, id ASC").
		Offset((input.Page - 1) * input.PageSize).Limit(input.PageSize).Scan(&result.Items).Error; err != nil {
		return nil, err
	}
	return result, nil
}

func (s *ProviderSupplyService) GetPlatformResource(ctx context.Context, authorization *ManagementAuthorizationContext, resourceID string) (*PlatformResourceDetail, error) {
	input := ProviderSupplyListInput{}
	providerID, query, err := s.providerReadQuery(ctx, authorization, &input, PermissionProviderResourcesRead)
	if err != nil {
		return nil, err
	}
	resourceID = strings.TrimSpace(resourceID)
	if validateRequired("resource_id", resourceID, 36) != nil {
		return nil, ErrProviderSupplyInvalidInput
	}
	var resource PlatformResourceView
	if err := query.Model(&model.PlatformResource{}).
		Select(platformResourceProjectionSelect()).
		Where("provider_id = ? AND id = ?", providerID, resourceID).
		Take(&resource).Error; err != nil {
		return nil, providerSupplyNotFound(err)
	}
	sources := []model.PlatformResourceSource{}
	if err := query.Where("provider_id = ? AND platform_resource_id = ?", providerID, resource.ID).
		Order("is_primary DESC, created_at ASC, id ASC").Find(&sources).Error; err != nil {
		return nil, err
	}
	scopes := []model.ResourceScope{}
	if err := query.Where("provider_id = ? AND platform_resource_id = ?", providerID, resource.ID).
		Order("type ASC, created_at ASC, id ASC").Find(&scopes).Error; err != nil {
		return nil, err
	}
	return &PlatformResourceDetail{Resource: &resource, Sources: sources, Scopes: scopes}, nil
}

func platformResourceProjectionSelect() string {
	return "platform_resource.*, " + platformResourceAccessDomainSubquery() + " AS access_domain, " +
		platformResourceHostDomainLabelSubquery() + " AS host_domain_label, " +
		platformResourceSourceNodeIDSubquery() + " AS source_node_id"
}

func platformResourceAccessDomainSubquery() string {
	return `COALESCE((
		SELECT domain_registry.domain
		FROM platform_resource_source access_source
		JOIN supply_candidate access_candidate
			ON access_candidate.id = access_source.supply_candidate_id
			AND access_candidate.provider_id = access_source.provider_id
		JOIN technical_resource_binding access_binding
			ON access_binding.technical_resource_id = access_candidate.technical_resource_id
			AND access_binding.enabled = true
		JOIN domain_registry
			ON domain_registry.provider_id = platform_resource.provider_id
			AND domain_registry.status = 'online'
		WHERE access_source.provider_id = platform_resource.provider_id
			AND access_source.platform_resource_id = platform_resource.id
			AND (
				(platform_resource.type = 'host'
					AND access_binding.source_type = 'legacy_node'
					AND access_candidate.stable_key = 'legacy-host-legacy_node:' || access_binding.source_id
					AND domain_registry.type = 'ssh'
					AND domain_registry.resource_kind = 'node'
					AND domain_registry.resource_id = access_binding.source_id)
				OR (platform_resource.type = 'kubernetes'
					AND access_binding.source_type = 'legacy_node'
					AND domain_registry.type = 'k8sapi'
					AND domain_registry.resource_kind = 'kubernetes'
					AND domain_registry.resource_id = access_binding.source_id)
				OR (platform_resource.type = 'kubernetes'
					AND access_binding.source_type = 'legacy_endpoint'
					AND domain_registry.type = 'k8sapi'
					AND domain_registry.resource_kind = 'endpoint'
					AND domain_registry.resource_id = access_binding.source_id)
			)
		ORDER BY access_source.is_primary DESC, domain_registry.status DESC, domain_registry.domain ASC, domain_registry.id ASC
		LIMIT 1
	), '')`
}

func platformResourceHostDomainLabelSubquery() string {
	return `COALESCE((
		SELECT node.host_domain_label
		FROM platform_resource_source access_source
		JOIN supply_candidate access_candidate
			ON access_candidate.id = access_source.supply_candidate_id
			AND access_candidate.provider_id = access_source.provider_id
		JOIN technical_resource_binding access_binding
			ON access_binding.technical_resource_id = access_candidate.technical_resource_id
			AND access_binding.enabled = true
			AND access_binding.source_type = 'legacy_node'
			AND access_candidate.stable_key = 'legacy-host-legacy_node:' || access_binding.source_id
		JOIN node ON node.id = CAST(access_binding.source_id AS INTEGER)
		WHERE access_source.provider_id = platform_resource.provider_id
			AND access_source.platform_resource_id = platform_resource.id
			AND platform_resource.type = 'host'
		ORDER BY access_source.is_primary DESC, access_source.created_at ASC, access_source.id ASC
		LIMIT 1
	), '')`
}

func platformResourceSourceNodeIDSubquery() string {
	return `COALESCE((
		SELECT CAST(access_binding.source_id AS INTEGER)
		FROM platform_resource_source access_source
		JOIN supply_candidate access_candidate
			ON access_candidate.id = access_source.supply_candidate_id
			AND access_candidate.provider_id = access_source.provider_id
		JOIN technical_resource_binding access_binding
			ON access_binding.technical_resource_id = access_candidate.technical_resource_id
			AND access_binding.enabled = true
			AND access_binding.source_type = 'legacy_node'
			AND access_candidate.stable_key = 'legacy-host-legacy_node:' || access_binding.source_id
		WHERE access_source.provider_id = platform_resource.provider_id
			AND access_source.platform_resource_id = platform_resource.id
			AND platform_resource.type = 'host'
		ORDER BY access_source.is_primary DESC, access_source.created_at ASC, access_source.id ASC
		LIMIT 1
	), 0)`
}

func (s *ProviderSupplyService) ListResourceScopes(ctx context.Context, authorization *ManagementAuthorizationContext, input ResourceScopeListInput) (*ResourceScopeListResult, error) {
	input.PlatformResourceID = strings.TrimSpace(input.PlatformResourceID)
	providerID, query, err := s.providerReadQuery(ctx, authorization, &input.ProviderSupplyListInput, PermissionProviderResourcesRead)
	if err != nil {
		return nil, err
	}
	if input.PlatformResourceID != "" && validateRequired("resource_id", input.PlatformResourceID, 36) != nil {
		return nil, ErrProviderSupplyInvalidInput
	}
	if input.PlatformResourceID != "" {
		var count int64
		if err := s.db.WithContext(ctx).Model(&model.PlatformResource{}).
			Where("provider_id = ? AND id = ?", providerID, input.PlatformResourceID).Count(&count).Error; err != nil {
			return nil, err
		}
		if count != 1 {
			return nil, ErrProviderSupplyObjectNotFound
		}
		query = query.Where("resource_scope.platform_resource_id = ?", input.PlatformResourceID)
	}
	if input.Type != "" {
		typeValue := model.ResourceScopeType(input.Type)
		if typeValue != model.ResourceScopeCluster && typeValue != model.ResourceScopeNamespace {
			return nil, ErrProviderSupplyInvalidInput
		}
		query = query.Where("resource_scope.type = ?", typeValue)
	}
	if input.State != "" {
		state := model.ResourceScopeLifecycleState(input.State)
		switch state {
		case model.ResourceScopeDraft, model.ResourceScopeActive, model.ResourceScopeAllocatable,
			model.ResourceScopeSuspended, model.ResourceScopeRetired:
		default:
			return nil, ErrProviderSupplyInvalidInput
		}
		query = query.Where("resource_scope.lifecycle_state = ?", state)
	}
	if input.Search != "" {
		query = query.Where("resource_scope.stable_key LIKE ? ESCAPE '\\'", "%"+escapeProviderLike(input.Search)+"%")
	}
	result := &ResourceScopeListResult{Items: []ResourceScopeView{}}
	query = query.Table("resource_scope").
		Joins("JOIN platform_resource ON platform_resource.provider_id = resource_scope.provider_id AND platform_resource.id = resource_scope.platform_resource_id").
		Where("resource_scope.provider_id = ?", providerID)
	if err := query.Count(&result.Total).Error; err != nil {
		return nil, err
	}
	if err := query.Select(resourceScopeProjectionSelect()).
		Order("resource_scope.type ASC, resource_scope.created_at DESC, resource_scope.id ASC").
		Offset((input.Page - 1) * input.PageSize).Limit(input.PageSize).Scan(&result.Items).Error; err != nil {
		return nil, err
	}
	return result, nil
}

func resourceScopeProjectionSelect() string {
	return "resource_scope.*, platform_resource.display_name AS platform_resource_display_name, platform_resource.stable_key AS platform_resource_stable_key, " +
		platformResourceAccessDomainSubquery() + " AS platform_resource_access_domain"
}

func (s *ProviderSupplyService) GetResourceScope(ctx context.Context, authorization *ManagementAuthorizationContext, scopeID string) (*ResourceScopeDetail, error) {
	input := ProviderSupplyListInput{}
	providerID, query, err := s.providerReadQuery(ctx, authorization, &input, PermissionProviderResourcesRead)
	if err != nil {
		return nil, err
	}
	scopeID = strings.TrimSpace(scopeID)
	if validateRequired("scope_id", scopeID, 36) != nil {
		return nil, ErrProviderSupplyInvalidInput
	}
	var scope model.ResourceScope
	if err := query.Where("provider_id = ? AND id = ?", providerID, scopeID).First(&scope).Error; err != nil {
		return nil, providerSupplyNotFound(err)
	}
	detail := &ResourceScopeDetail{Scope: &scope}
	if scope.NamespaceObservationID != nil {
		evidenceProviderID, err := reauthorizeProviderPermission(query, authorization, PermissionProviderIsolationEvidenceRead, s.now().UTC())
		if err != nil {
			return nil, err
		}
		if evidenceProviderID != providerID {
			return nil, ErrManagementPermissionDenied
		}
		var observation model.NamespaceObservation
		if err := query.Where("provider_id = ? AND cluster_resource_id = ? AND id = ?", providerID, scope.PlatformResourceID, *scope.NamespaceObservationID).
			First(&observation).Error; err != nil {
			return nil, providerSupplyNotFound(err)
		}
		detail.Observation = &observation
	}
	return detail, nil
}

func (s *ProviderSupplyService) providerReadQuery(ctx context.Context, authorization *ManagementAuthorizationContext, input *ProviderSupplyListInput, permission string) (string, *gorm.DB, error) {
	if s == nil || s.db == nil {
		return "", nil, ErrProviderSupplyInvalidInput
	}
	if input == nil {
		return "", nil, ErrProviderSupplyInvalidInput
	}
	input.Search = strings.TrimSpace(input.Search)
	input.Type = strings.TrimSpace(input.Type)
	input.State = strings.TrimSpace(input.State)
	if len(input.Search) > 200 || len(input.Type) > 32 || len(input.State) > 32 {
		return "", nil, ErrProviderSupplyInvalidInput
	}
	if input.Page == 0 {
		input.Page = 1
	}
	if input.PageSize == 0 {
		input.PageSize = 20
	}
	if input.Page < 1 || input.PageSize < 1 || input.PageSize > providerSupplyMaxPageSize {
		return "", nil, ErrProviderSupplyInvalidInput
	}
	query := s.db.WithContext(ctx)
	providerID, err := reauthorizeProviderPermission(query, authorization, permission, s.now().UTC())
	if err != nil {
		return "", nil, err
	}
	return providerID, query, nil
}

func providerSupplyNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrProviderSupplyObjectNotFound
	}
	return err
}

func escapeProviderLike(value string) string {
	return strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(strings.TrimSpace(value))
}
