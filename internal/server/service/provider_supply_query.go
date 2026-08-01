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
	Items []model.TechnicalResource `json:"items"`
	Total int64                     `json:"total"`
}

type TechnicalResourceDetail struct {
	Resource *model.TechnicalResource         `json:"resource"`
	Bindings []model.TechnicalResourceBinding `json:"bindings"`
}

type SupplyCandidateListResult struct {
	Items []model.SupplyCandidate `json:"items"`
	Total int64                   `json:"total"`
}

type PlatformResourceListResult struct {
	Items []model.PlatformResource `json:"items"`
	Total int64                    `json:"total"`
}

type PlatformResourceDetail struct {
	Resource *model.PlatformResource        `json:"resource"`
	Sources  []model.PlatformResourceSource `json:"sources"`
	Scopes   []model.ResourceScope          `json:"scopes"`
}

type ResourceScopeListInput struct {
	ProviderSupplyListInput
	PlatformResourceID string
}

type ResourceScopeListResult struct {
	Items []model.ResourceScope `json:"items"`
	Total int64                 `json:"total"`
}

type ResourceScopeDetail struct {
	Scope       *model.ResourceScope        `json:"scope"`
	Observation *model.NamespaceObservation `json:"observation,omitempty"`
}

func (s *ProviderSupplyService) ListTechnicalResources(ctx context.Context, authorization *ManagementAuthorizationContext, input ProviderSupplyListInput) (*TechnicalResourceListResult, error) {
	providerID, query, err := s.providerReadQuery(ctx, authorization, &input, PermissionProviderTechnicalResourcesRead)
	if err != nil {
		return nil, err
	}
	if input.Type != "" {
		typeValue := model.TechnicalResourceType(input.Type)
		if typeValue != model.TechnicalResourceAgent && typeValue != model.TechnicalResourceEndpoint {
			return nil, ErrProviderSupplyInvalidInput
		}
		query = query.Where("type = ?", typeValue)
	}
	if input.State != "" {
		state := model.TechnicalResourceLifecycleState(input.State)
		if state != model.TechnicalResourcePending && state != model.TechnicalResourceRegistered &&
			state != model.TechnicalResourceDisabled && state != model.TechnicalResourceRetired {
			return nil, ErrProviderSupplyInvalidInput
		}
		query = query.Where("lifecycle_state = ?", state)
	}
	if input.Search != "" {
		query = query.Where("stable_key LIKE ? ESCAPE '\\'", "%"+escapeProviderLike(input.Search)+"%")
	}
	result := &TechnicalResourceListResult{Items: []model.TechnicalResource{}}
	query = query.Model(&model.TechnicalResource{}).Where("provider_id = ?", providerID)
	if err := query.Count(&result.Total).Error; err != nil {
		return nil, err
	}
	if err := query.Order("created_at DESC, id ASC").Offset((input.Page - 1) * input.PageSize).Limit(input.PageSize).Find(&result.Items).Error; err != nil {
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
	var resource model.TechnicalResource
	if err := query.Where("provider_id = ? AND id = ?", providerID, resourceID).First(&resource).Error; err != nil {
		return nil, providerSupplyNotFound(err)
	}
	bindings := []model.TechnicalResourceBinding{}
	if err := query.Where("technical_resource_id = ?", resource.ID).Order("created_at DESC, id ASC").Find(&bindings).Error; err != nil {
		return nil, err
	}
	return &TechnicalResourceDetail{Resource: &resource, Bindings: bindings}, nil
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
	}
	if input.Search != "" {
		pattern := "%" + escapeProviderLike(input.Search) + "%"
		query = query.Where("display_name LIKE ? ESCAPE '\\' OR stable_key LIKE ? ESCAPE '\\'", pattern, pattern)
	}
	result := &PlatformResourceListResult{Items: []model.PlatformResource{}}
	query = query.Model(&model.PlatformResource{}).Where("provider_id = ?", providerID)
	if err := query.Count(&result.Total).Error; err != nil {
		return nil, err
	}
	if err := query.Order("created_at DESC, id ASC").Offset((input.Page - 1) * input.PageSize).Limit(input.PageSize).Find(&result.Items).Error; err != nil {
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
	var resource model.PlatformResource
	if err := query.Where("provider_id = ? AND id = ?", providerID, resourceID).First(&resource).Error; err != nil {
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
		query = query.Where("platform_resource_id = ?", input.PlatformResourceID)
	}
	if input.Type != "" {
		typeValue := model.ResourceScopeType(input.Type)
		if typeValue != model.ResourceScopeCluster && typeValue != model.ResourceScopeNamespace {
			return nil, ErrProviderSupplyInvalidInput
		}
		query = query.Where("type = ?", typeValue)
	}
	if input.State != "" {
		state := model.ResourceScopeLifecycleState(input.State)
		switch state {
		case model.ResourceScopeDraft, model.ResourceScopeActive, model.ResourceScopeAllocatable,
			model.ResourceScopeSuspended, model.ResourceScopeRetired:
		default:
			return nil, ErrProviderSupplyInvalidInput
		}
		query = query.Where("lifecycle_state = ?", state)
	}
	if input.Search != "" {
		query = query.Where("stable_key LIKE ? ESCAPE '\\'", "%"+escapeProviderLike(input.Search)+"%")
	}
	result := &ResourceScopeListResult{Items: []model.ResourceScope{}}
	query = query.Model(&model.ResourceScope{}).Where("provider_id = ?", providerID)
	if err := query.Count(&result.Total).Error; err != nil {
		return nil, err
	}
	if err := query.Order("type ASC, created_at DESC, id ASC").Offset((input.Page - 1) * input.PageSize).Limit(input.PageSize).Find(&result.Items).Error; err != nil {
		return nil, err
	}
	return result, nil
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
