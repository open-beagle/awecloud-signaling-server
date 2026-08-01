package service

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

type PlatformSupplyConflictListInput struct {
	Search   string
	Type     string
	Page     int
	PageSize int
}

type PlatformSupplyConflictSummary struct {
	OpaqueConflictID string                   `json:"opaque_conflict_id"`
	ResourceType     model.SupplyResourceType `json:"resource_type"`
	ProviderCount    int64                    `json:"provider_count"`
	CandidateCount   int64                    `json:"candidate_count"`
}

type PlatformSupplyConflictListResult struct {
	Items []PlatformSupplyConflictSummary `json:"items"`
	Total int64                           `json:"total"`
}

type PlatformSupplyGovernanceService struct {
	db  *gorm.DB
	now func() time.Time
}

func NewPlatformSupplyGovernanceService(database *gorm.DB) *PlatformSupplyGovernanceService {
	return &PlatformSupplyGovernanceService{db: database, now: time.Now}
}

func (s *PlatformSupplyGovernanceService) ListSupplyConflicts(ctx context.Context, authorization *ManagementAuthorizationContext, input PlatformSupplyConflictListInput) (*PlatformSupplyConflictListResult, error) {
	if s == nil || s.db == nil {
		return nil, ErrProviderSupplyInvalidInput
	}
	input.Search = strings.TrimSpace(input.Search)
	input.Type = strings.TrimSpace(input.Type)
	if len(input.Search) > 200 || len(input.Type) > 32 {
		return nil, ErrProviderSupplyInvalidInput
	}
	if input.Page == 0 {
		input.Page = 1
	}
	if input.PageSize == 0 {
		input.PageSize = 20
	}
	if input.Page < 1 || input.PageSize < 1 || input.PageSize > providerSupplyMaxPageSize {
		return nil, ErrProviderSupplyInvalidInput
	}

	now := s.now().UTC()
	if err := reauthorizePlatformPermission(s.db.WithContext(ctx), authorization, PermissionPlatformResourcesRead, now); err != nil {
		return nil, err
	}
	query := func() *gorm.DB {
		return s.db.WithContext(ctx).Model(&model.SupplyCandidate{}).
			Where("conflict_code = ? AND opaque_conflict_id <> ''", supplyConflictCrossProvider).
			Where("julianday(lease_expires_at) > julianday(?)", now)
	}
	if input.Type != "" {
		resourceType := model.SupplyResourceType(input.Type)
		if resourceType != model.SupplyResourceKubernetes && resourceType != model.SupplyResourceHost {
			return nil, ErrProviderSupplyInvalidInput
		}
		previous := query
		query = func() *gorm.DB { return previous().Where("resource_type = ?", resourceType) }
	}
	if input.Search != "" {
		search := "%" + escapeProviderLike(input.Search) + "%"
		previous := query
		query = func() *gorm.DB { return previous().Where("opaque_conflict_id LIKE ? ESCAPE '\\'", search) }
	}

	result := &PlatformSupplyConflictListResult{Items: []PlatformSupplyConflictSummary{}}
	if err := query().Distinct("opaque_conflict_id").Count(&result.Total).Error; err != nil {
		return nil, err
	}
	if err := query().Select(`opaque_conflict_id, resource_type,
		COUNT(DISTINCT provider_id) AS provider_count,
		COUNT(*) AS candidate_count`).
		Group("opaque_conflict_id, resource_type").
		Order("MAX(julianday(last_observed_at)) DESC, opaque_conflict_id ASC").
		Offset((input.Page - 1) * input.PageSize).Limit(input.PageSize).
		Scan(&result.Items).Error; err != nil {
		return nil, err
	}
	return result, nil
}

func reauthorizePlatformPermission(tx *gorm.DB, authorization *ManagementAuthorizationContext, permission string, at time.Time) error {
	if tx == nil || authorization == nil || authorization.ScopeType != model.ManagementScopePlatform || authorization.ScopeID != "" ||
		authorization.ActorUserID == 0 || authorization.EffectiveUserID == 0 || authorization.PermissionRevision <= 0 ||
		authorization.SimulationSessionID != "" || at.IsZero() {
		return ErrManagementPermissionDenied
	}
	current, err := ResolveManagementContext(tx, authorization.EffectiveUserID, model.ManagementScopePlatform, "", at, false)
	if err != nil || current == nil || current.ActorUserID != authorization.ActorUserID || current.EffectiveUserID != authorization.EffectiveUserID ||
		current.PermissionRevision != authorization.PermissionRevision {
		return ErrManagementPermissionDenied
	}
	return AuthorizeManagementPermission(current, permission)
}
