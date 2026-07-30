package service

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

type PlatformAllocationListInput struct {
	TenantID   string
	ProviderID string
	ResourceID string
	ScopeID    string
	Mode       string
	State      string
	Search     string
	ValidAt    *time.Time
	Page       int
	PageSize   int
}

type PlatformAllocationListResult struct {
	Items []model.ResourceAllocation `json:"items"`
	Total int64                      `json:"total"`
}

func (s *PlatformAllocationService) List(ctx context.Context, authorization *ManagementAuthorizationContext, input PlatformAllocationListInput) (*PlatformAllocationListResult, error) {
	if s == nil || s.db == nil {
		return nil, ErrPlatformAllocationInvalidInput
	}
	input.TenantID = strings.TrimSpace(input.TenantID)
	input.ProviderID = strings.TrimSpace(input.ProviderID)
	input.ResourceID = strings.TrimSpace(input.ResourceID)
	input.ScopeID = strings.TrimSpace(input.ScopeID)
	input.Mode = strings.TrimSpace(input.Mode)
	input.State = strings.TrimSpace(input.State)
	input.Search = strings.TrimSpace(input.Search)
	if input.Page < 1 || input.PageSize < 1 || input.PageSize > 100 || len(input.Search) > 200 ||
		len(input.TenantID) > 36 || len(input.ProviderID) > 36 || len(input.ResourceID) > 36 || len(input.ScopeID) > 36 {
		return nil, ErrPlatformAllocationInvalidInput
	}
	if input.Mode != "" && !model.ResourceAllocationMode(input.Mode).Valid() {
		return nil, ErrPlatformAllocationInvalidInput
	}
	if input.State != "" && !model.ResourceAllocationState(input.State).Valid() {
		return nil, ErrPlatformAllocationInvalidInput
	}
	now := s.now().UTC()
	if err := reauthorizePlatformPermission(s.db.WithContext(ctx), authorization, PermissionPlatformAllocationsRead, now); err != nil {
		return nil, err
	}

	query := func() *gorm.DB {
		return s.db.WithContext(ctx).Model(&model.ResourceAllocation{}).
			Joins("JOIN resource_allocation_item AS allocation_item ON allocation_item.allocation_id = resource_allocation.id").
			Joins("JOIN resource_scope AS allocation_scope ON allocation_scope.id = allocation_item.scope_id").
			Joins("JOIN tenant AS allocation_tenant ON allocation_tenant.id = resource_allocation.tenant_id")
	}
	if input.TenantID != "" {
		previous := query
		query = func() *gorm.DB { return previous().Where("resource_allocation.tenant_id = ?", input.TenantID) }
	}
	if input.ProviderID != "" {
		previous := query
		query = func() *gorm.DB { return previous().Where("allocation_scope.provider_id = ?", input.ProviderID) }
	}
	if input.ResourceID != "" {
		previous := query
		query = func() *gorm.DB {
			return previous().Where("allocation_scope.platform_resource_id = ?", input.ResourceID)
		}
	}
	if input.ScopeID != "" {
		previous := query
		query = func() *gorm.DB { return previous().Where("allocation_item.scope_id = ?", input.ScopeID) }
	}
	if input.Mode != "" {
		previous := query
		query = func() *gorm.DB { return previous().Where("resource_allocation.mode = ?", input.Mode) }
	}
	if input.State != "" {
		previous := query
		query = func() *gorm.DB { return previous().Where("resource_allocation.state = ?", input.State) }
	}
	if input.Search != "" {
		search := "%" + escapeProviderLike(input.Search) + "%"
		previous := query
		query = func() *gorm.DB {
			return previous().Where("resource_allocation.id LIKE ? ESCAPE '\\' OR resource_allocation.contract_ref LIKE ? ESCAPE '\\' OR allocation_tenant.name LIKE ? ESCAPE '\\'", search, search, search)
		}
	}
	if input.ValidAt != nil {
		validAt := input.ValidAt.UTC()
		previous := query
		query = func() *gorm.DB {
			return previous().Where("julianday(resource_allocation.valid_from) <= julianday(?) AND (resource_allocation.expires_at IS NULL OR julianday(resource_allocation.expires_at) > julianday(?))", validAt, validAt)
		}
	}

	result := &PlatformAllocationListResult{Items: []model.ResourceAllocation{}}
	if err := query().Distinct("resource_allocation.id").Count(&result.Total).Error; err != nil {
		return nil, err
	}
	if err := query().Select("resource_allocation.*").Distinct().Preload("Items").
		Order("resource_allocation.updated_at DESC, resource_allocation.id ASC").
		Offset((input.Page - 1) * input.PageSize).Limit(input.PageSize).Find(&result.Items).Error; err != nil {
		return nil, err
	}
	return result, nil
}

func (s *PlatformAllocationService) Get(ctx context.Context, authorization *ManagementAuthorizationContext, allocationID string) (*model.ResourceAllocation, error) {
	if s == nil || s.db == nil || validateRequired("allocation_id", strings.TrimSpace(allocationID), 36) != nil {
		return nil, ErrPlatformAllocationInvalidInput
	}
	now := s.now().UTC()
	if err := reauthorizePlatformPermission(s.db.WithContext(ctx), authorization, PermissionPlatformAllocationsRead, now); err != nil {
		return nil, err
	}
	return loadPlatformAllocation(s.db.WithContext(ctx), strings.TrimSpace(allocationID))
}
