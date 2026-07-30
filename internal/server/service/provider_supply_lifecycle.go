package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

var (
	ErrPlatformResourceStateTransition = errors.New("invalid PlatformResource state transition")
	ErrResourceScopeStateTransition    = errors.New("invalid ResourceScope state transition")
	ErrResourceScopeNotAllocatable     = errors.New("ResourceScope allocatable prerequisites are not satisfied")
)

type SetPlatformResourceLifecycleInput struct {
	ResourceID         string
	TargetState        model.PlatformResourceLifecycleState
	ExpectedRowVersion int64
	Reason             string
}

func (s *ProviderSupplyService) SetPlatformResourceLifecycle(ctx context.Context, authorization *ManagementAuthorizationContext, input SetPlatformResourceLifecycleInput) (*model.PlatformResource, error) {
	if s == nil || s.db == nil {
		return nil, ErrProviderSupplyInvalidInput
	}
	input.ResourceID = strings.TrimSpace(input.ResourceID)
	input.Reason = strings.TrimSpace(input.Reason)
	if validateRequired("resource_id", input.ResourceID, 36) != nil || validateRequired("reason", input.Reason, 500) != nil ||
		input.ExpectedRowVersion <= 0 || !validPlatformResourceTarget(input.TargetState) {
		return nil, ErrProviderSupplyInvalidInput
	}

	now := s.now().UTC()
	var resource model.PlatformResource
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		providerID, err := reauthorizeProviderPermission(tx, authorization, PermissionProviderResourcesWrite, now)
		if err != nil {
			return err
		}
		if err := tx.Where("provider_id = ? AND id = ?", providerID, input.ResourceID).First(&resource).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrProviderSupplyObjectNotFound
			}
			return err
		}
		if resource.RowVersion != input.ExpectedRowVersion {
			return ErrProviderSupplyVersionConflict
		}
		if !validPlatformResourceTransition(resource.LifecycleState, input.TargetState) {
			return ErrPlatformResourceStateTransition
		}
		if resource.LifecycleState == model.PlatformResourceDraft && input.TargetState == model.PlatformResourceActive {
			var sourceCount int64
			if err := tx.Model(&model.PlatformResourceSource{}).
				Where("provider_id = ? AND platform_resource_id = ?", providerID, resource.ID).Count(&sourceCount).Error; err != nil {
				return err
			}
			if sourceCount == 0 {
				return ErrProviderSupplyConflict
			}
		}

		updates := map[string]any{
			"lifecycle_state": input.TargetState,
			"row_version":     gorm.Expr("row_version + 1"),
		}
		switch input.TargetState {
		case model.PlatformResourceSuspended:
			if err := cascadeResourceScopes(tx, providerID, resource.ID, model.ResourceScopeSuspended); err != nil {
				return err
			}
			updates["allocatable_scope_count"] = 0
			updates["capability_revision"] = gorm.Expr("capability_revision + 1")
		case model.PlatformResourceRetired:
			if err := cascadeResourceScopes(tx, providerID, resource.ID, model.ResourceScopeRetired); err != nil {
				return err
			}
			updates["allocatable_scope_count"] = 0
			updates["capability_revision"] = gorm.Expr("capability_revision + 1")
		}
		updated := tx.Model(&model.PlatformResource{}).
			Where("provider_id = ? AND id = ? AND row_version = ? AND lifecycle_state = ?",
				providerID, resource.ID, resource.RowVersion, resource.LifecycleState).
			Updates(updates)
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrProviderSupplyVersionConflict
		}
		return tx.Where("provider_id = ? AND id = ?", providerID, resource.ID).First(&resource).Error
	})
	if err != nil {
		return nil, err
	}
	return &resource, nil
}

type SetResourceScopeLifecycleInput struct {
	ScopeID            string
	TargetState        model.ResourceScopeLifecycleState
	ExpectedRowVersion int64
	Reason             string
}

func (s *ProviderSupplyService) SetResourceScopeLifecycle(ctx context.Context, authorization *ManagementAuthorizationContext, input SetResourceScopeLifecycleInput) (*model.ResourceScope, error) {
	if s == nil || s.db == nil {
		return nil, ErrProviderSupplyInvalidInput
	}
	input.ScopeID = strings.TrimSpace(input.ScopeID)
	input.Reason = strings.TrimSpace(input.Reason)
	if validateRequired("scope_id", input.ScopeID, 36) != nil || validateRequired("reason", input.Reason, 500) != nil ||
		input.ExpectedRowVersion <= 0 || !validResourceScopeTarget(input.TargetState) {
		return nil, ErrProviderSupplyInvalidInput
	}

	now := s.now().UTC()
	var scope model.ResourceScope
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		providerID, err := reauthorizeProviderPermission(tx, authorization, PermissionProviderResourcesWrite, now)
		if err != nil {
			return err
		}
		if err := tx.Where("provider_id = ? AND id = ?", providerID, input.ScopeID).First(&scope).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrProviderSupplyObjectNotFound
			}
			return err
		}
		if scope.RowVersion != input.ExpectedRowVersion {
			return ErrProviderSupplyVersionConflict
		}
		if !validResourceScopeTransition(scope.LifecycleState, input.TargetState) {
			return ErrResourceScopeStateTransition
		}
		var resource model.PlatformResource
		if err := tx.Where("provider_id = ? AND id = ?", providerID, scope.PlatformResourceID).First(&resource).Error; err != nil {
			return ErrProviderSupplyConflict
		}
		if input.TargetState == model.ResourceScopeActive {
			if resource.LifecycleState != model.PlatformResourceActive || !scopeParentAvailable(tx, &scope) {
				return ErrResourceScopeStateTransition
			}
		}
		if scope.Type == model.ResourceScopeCluster {
			switch input.TargetState {
			case model.ResourceScopeSuspended:
				if err := cascadeChildScopes(tx, providerID, resource.ID, scope.ID, model.ResourceScopeSuspended); err != nil {
					return err
				}
			case model.ResourceScopeRetired:
				if err := cascadeChildScopes(tx, providerID, resource.ID, scope.ID, model.ResourceScopeRetired); err != nil {
					return err
				}
			}
		}
		updated := tx.Model(&model.ResourceScope{}).
			Where("provider_id = ? AND id = ? AND row_version = ? AND lifecycle_state = ?",
				providerID, scope.ID, scope.RowVersion, scope.LifecycleState).
			Updates(map[string]any{
				"lifecycle_state": input.TargetState,
				"row_version":     gorm.Expr("row_version + 1"),
			})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrProviderSupplyVersionConflict
		}
		if err := syncPlatformResourceAllocatableScopeCount(tx, providerID, resource.ID); err != nil {
			return err
		}
		return tx.Where("provider_id = ? AND id = ?", providerID, scope.ID).First(&scope).Error
	})
	if err != nil {
		return nil, err
	}
	return &scope, nil
}

type MarkResourceScopeAllocatableInput struct {
	ScopeID            string
	ExpectedRowVersion int64
	Reason             string
}

type MarkResourceScopeAllocatableResult struct {
	Scope    *model.ResourceScope    `json:"scope"`
	Resource *model.PlatformResource `json:"resource"`
}

func (s *ProviderSupplyService) MarkResourceScopeAllocatable(ctx context.Context, authorization *ManagementAuthorizationContext, input MarkResourceScopeAllocatableInput) (*MarkResourceScopeAllocatableResult, error) {
	if s == nil || s.db == nil {
		return nil, ErrProviderSupplyInvalidInput
	}
	input.ScopeID = strings.TrimSpace(input.ScopeID)
	input.Reason = strings.TrimSpace(input.Reason)
	if validateRequired("scope_id", input.ScopeID, 36) != nil || validateRequired("reason", input.Reason, 500) != nil || input.ExpectedRowVersion <= 0 {
		return nil, ErrProviderSupplyInvalidInput
	}

	now := s.now().UTC()
	result := &MarkResourceScopeAllocatableResult{}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		providerID, err := reauthorizeProviderPermission(tx, authorization, PermissionProviderResourcesWrite, now)
		if err != nil {
			return err
		}
		var scope model.ResourceScope
		if err := tx.Where("provider_id = ? AND id = ?", providerID, input.ScopeID).First(&scope).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrProviderSupplyObjectNotFound
			}
			return err
		}
		if scope.RowVersion != input.ExpectedRowVersion {
			return ErrProviderSupplyVersionConflict
		}
		if scope.LifecycleState != model.ResourceScopeActive {
			return ErrResourceScopeStateTransition
		}
		var resource model.PlatformResource
		if err := tx.Where("provider_id = ? AND id = ?", providerID, scope.PlatformResourceID).First(&resource).Error; err != nil {
			return ErrResourceScopeNotAllocatable
		}
		if resource.Type != model.SupplyResourceKubernetes || resource.LifecycleState != model.PlatformResourceActive {
			return ErrResourceScopeNotAllocatable
		}
		if err := validateAllocatableResourceSource(tx, &resource, now); err != nil {
			return err
		}
		if err := validateAllocatableScopeEvidence(tx, &resource, &scope, now); err != nil {
			return err
		}
		updated := tx.Model(&model.ResourceScope{}).
			Where("provider_id = ? AND id = ? AND row_version = ? AND lifecycle_state = ?",
				providerID, scope.ID, scope.RowVersion, model.ResourceScopeActive).
			Updates(map[string]any{
				"lifecycle_state": model.ResourceScopeAllocatable,
				"row_version":     gorm.Expr("row_version + 1"),
			})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrProviderSupplyVersionConflict
		}
		if err := syncPlatformResourceAllocatableScopeCount(tx, providerID, resource.ID); err != nil {
			return err
		}
		if err := tx.Where("provider_id = ? AND id = ?", providerID, scope.ID).First(&scope).Error; err != nil {
			return err
		}
		if err := tx.Where("provider_id = ? AND id = ?", providerID, resource.ID).First(&resource).Error; err != nil {
			return err
		}
		result.Scope, result.Resource = &scope, &resource
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func validPlatformResourceTarget(target model.PlatformResourceLifecycleState) bool {
	return target == model.PlatformResourceActive || target == model.PlatformResourceSuspended || target == model.PlatformResourceRetired
}

func validPlatformResourceTransition(current, target model.PlatformResourceLifecycleState) bool {
	switch target {
	case model.PlatformResourceActive:
		return current == model.PlatformResourceDraft || current == model.PlatformResourceSuspended
	case model.PlatformResourceSuspended:
		return current == model.PlatformResourceActive
	case model.PlatformResourceRetired:
		return current == model.PlatformResourceDraft || current == model.PlatformResourceActive || current == model.PlatformResourceSuspended
	default:
		return false
	}
}

func validResourceScopeTarget(target model.ResourceScopeLifecycleState) bool {
	return target == model.ResourceScopeActive || target == model.ResourceScopeSuspended || target == model.ResourceScopeRetired
}

func validResourceScopeTransition(current, target model.ResourceScopeLifecycleState) bool {
	switch target {
	case model.ResourceScopeActive:
		return current == model.ResourceScopeDraft || current == model.ResourceScopeSuspended
	case model.ResourceScopeSuspended:
		return current == model.ResourceScopeActive || current == model.ResourceScopeAllocatable
	case model.ResourceScopeRetired:
		return current == model.ResourceScopeDraft || current == model.ResourceScopeActive ||
			current == model.ResourceScopeAllocatable || current == model.ResourceScopeSuspended
	default:
		return false
	}
}

func cascadeResourceScopes(tx *gorm.DB, providerID, resourceID string, target model.ResourceScopeLifecycleState) error {
	query := tx.Model(&model.ResourceScope{}).Where("provider_id = ? AND platform_resource_id = ?", providerID, resourceID)
	switch target {
	case model.ResourceScopeSuspended:
		query = query.Where("lifecycle_state IN ?", []model.ResourceScopeLifecycleState{model.ResourceScopeActive, model.ResourceScopeAllocatable})
	case model.ResourceScopeRetired:
		query = query.Where("lifecycle_state <> ?", model.ResourceScopeRetired)
	default:
		return ErrProviderSupplyInvalidInput
	}
	return query.Updates(map[string]any{"lifecycle_state": target, "row_version": gorm.Expr("row_version + 1")}).Error
}

func cascadeChildScopes(tx *gorm.DB, providerID, resourceID, parentID string, target model.ResourceScopeLifecycleState) error {
	query := tx.Model(&model.ResourceScope{}).
		Where("provider_id = ? AND platform_resource_id = ? AND parent_id = ?", providerID, resourceID, parentID)
	switch target {
	case model.ResourceScopeSuspended:
		query = query.Where("lifecycle_state IN ?", []model.ResourceScopeLifecycleState{model.ResourceScopeActive, model.ResourceScopeAllocatable})
	case model.ResourceScopeRetired:
		query = query.Where("lifecycle_state <> ?", model.ResourceScopeRetired)
	default:
		return ErrProviderSupplyInvalidInput
	}
	return query.Updates(map[string]any{"lifecycle_state": target, "row_version": gorm.Expr("row_version + 1")}).Error
}

func scopeParentAvailable(tx *gorm.DB, scope *model.ResourceScope) bool {
	if scope == nil {
		return false
	}
	if scope.Type == model.ResourceScopeCluster {
		return scope.ParentID == nil && scope.NamespaceObservationID == nil
	}
	if scope.Type != model.ResourceScopeNamespace || scope.ParentID == nil || scope.NamespaceObservationID == nil {
		return false
	}
	var count int64
	if err := tx.Model(&model.ResourceScope{}).
		Where("provider_id = ? AND platform_resource_id = ? AND id = ? AND type = ? AND lifecycle_state IN ?",
			scope.ProviderID, scope.PlatformResourceID, *scope.ParentID, model.ResourceScopeCluster,
			[]model.ResourceScopeLifecycleState{model.ResourceScopeActive, model.ResourceScopeAllocatable}).Count(&count).Error; err != nil {
		return false
	}
	return count == 1
}

func validateAllocatableResourceSource(tx *gorm.DB, resource *model.PlatformResource, now time.Time) error {
	var count int64
	err := tx.Table("platform_resource_source AS source").
		Joins("JOIN supply_candidate AS candidate ON candidate.id = source.supply_candidate_id AND candidate.provider_id = source.provider_id").
		Joins("JOIN technical_resource AS technical ON technical.id = candidate.technical_resource_id AND technical.provider_id = candidate.provider_id").
		Where("source.provider_id = ? AND source.platform_resource_id = ?", resource.ProviderID, resource.ID).
		Where("candidate.resource_type = ? AND candidate.stable_key = ?", resource.Type, resource.StableKey).
		Where("candidate.review_state = ? AND candidate.identity_quality = ? AND candidate.conflict_code = '' AND candidate.lease_expires_at > ?",
			model.SupplyCandidateLinked, model.SupplyIdentityStrong, now).
		Where("technical.lifecycle_state = ?", model.TechnicalResourceRegistered).
		Where(`EXISTS (
			SELECT 1 FROM technical_resource_binding binding
			WHERE binding.technical_resource_id = technical.id
				AND binding.enabled = true
				AND binding.credential_revision = technical.credential_revision
		)`).Count(&count).Error
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrResourceScopeNotAllocatable
	}
	return nil
}

func validateAllocatableScopeEvidence(tx *gorm.DB, resource *model.PlatformResource, scope *model.ResourceScope, now time.Time) error {
	if resource == nil || scope == nil || scope.ProviderID != resource.ProviderID || scope.PlatformResourceID != resource.ID {
		return ErrResourceScopeNotAllocatable
	}
	switch scope.Type {
	case model.ResourceScopeCluster:
		if scope.ParentID != nil || scope.NamespaceObservationID != nil || scope.IsolationMode != model.ResourceScopeIsolationNone || scope.StableKey != resource.StableKey {
			return ErrResourceScopeNotAllocatable
		}
	case model.ResourceScopeNamespace:
		if scope.ParentID == nil || scope.NamespaceObservationID == nil || scope.IsolationMode != model.ResourceScopeIsolationNamespaceIsolated {
			return ErrResourceScopeNotAllocatable
		}
		var parent model.ResourceScope
		if err := tx.Where("provider_id = ? AND platform_resource_id = ? AND id = ? AND type = ? AND lifecycle_state IN ?",
			resource.ProviderID, resource.ID, *scope.ParentID, model.ResourceScopeCluster,
			[]model.ResourceScopeLifecycleState{model.ResourceScopeActive, model.ResourceScopeAllocatable}).First(&parent).Error; err != nil {
			return ErrResourceScopeNotAllocatable
		}
		var observation model.NamespaceObservation
		if err := tx.Where("provider_id = ? AND cluster_resource_id = ? AND id = ? AND state = ? AND lease_expires_at > ?",
			resource.ProviderID, resource.ID, *scope.NamespaceObservationID, model.NamespaceObservationObserved, now).First(&observation).Error; err != nil {
			return ErrResourceScopeNotAllocatable
		}
		expectedStableKey := supplyStableDigest("kubernetes-namespace-v1", resource.ID+"\x00"+observation.NamespaceUID)
		if scope.StableKey != expectedStableKey || scope.EvidenceRevision != observation.Revision {
			return ErrResourceScopeNotAllocatable
		}
	default:
		return ErrResourceScopeNotAllocatable
	}
	return nil
}

func syncPlatformResourceAllocatableScopeCount(tx *gorm.DB, providerID, resourceID string) error {
	var resource model.PlatformResource
	if err := tx.Where("provider_id = ? AND id = ?", providerID, resourceID).First(&resource).Error; err != nil {
		return err
	}
	var count int64
	if err := tx.Model(&model.ResourceScope{}).
		Where("provider_id = ? AND platform_resource_id = ? AND lifecycle_state = ?", providerID, resourceID, model.ResourceScopeAllocatable).
		Count(&count).Error; err != nil {
		return err
	}
	if resource.AllocatableScopeCount == int(count) {
		return nil
	}
	updated := tx.Model(&model.PlatformResource{}).
		Where("provider_id = ? AND id = ? AND row_version = ?", providerID, resourceID, resource.RowVersion).
		Updates(map[string]any{
			"allocatable_scope_count": int(count),
			"capability_revision":     gorm.Expr("capability_revision + 1"),
			"row_version":             gorm.Expr("row_version + 1"),
		})
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected != 1 {
		return ErrProviderSupplyVersionConflict
	}
	return nil
}
