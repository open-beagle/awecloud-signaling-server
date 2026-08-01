package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

var (
	ErrPlatformAllocationInvalidInput      = errors.New("invalid Platform allocation input")
	ErrPlatformAllocationObjectNotFound    = errors.New("Platform allocation not found")
	ErrPlatformAllocationVersionConflict   = errors.New("Platform allocation row version conflict")
	ErrPlatformAllocationStateTransition   = errors.New("invalid Platform allocation state transition")
	ErrPlatformAllocationModeUnsupported   = errors.New("Platform allocation mode is unsupported")
	ErrPlatformAllocationTimeInvalid       = errors.New("Platform allocation time window is invalid")
	ErrPlatformAllocationTenantNotActive   = errors.New("Platform allocation Tenant is not active")
	ErrPlatformAllocationScopeUnavailable  = errors.New("Platform allocation Scope is not allocatable")
	ErrPlatformAllocationItemPolicy        = errors.New("Platform allocation Item violates first-version policy")
	ErrPlatformAllocationScopeConflict     = errors.New("Platform allocation Scope conflicts with an existing allocation")
	ErrPlatformAllocationHierarchyConflict = errors.New("Platform allocation Scope hierarchy conflicts with an existing allocation")
	ErrPlatformAllocationReasonRequired    = errors.New("Platform allocation reason is required")
)

type PlatformAllocationService struct {
	db  *gorm.DB
	now func() time.Time
}

func NewPlatformAllocationService(database *gorm.DB) *PlatformAllocationService {
	return &PlatformAllocationService{db: database, now: time.Now}
}

type CreatePlatformAllocationInput struct {
	TenantID    string
	Mode        model.ResourceAllocationMode
	ScopeID     string
	ValidFrom   time.Time
	ExpiresAt   *time.Time
	ContractRef string
	RenewedFrom *string
}

func (s *PlatformAllocationService) CreateDraft(ctx context.Context, authorization *ManagementAuthorizationContext, input CreatePlatformAllocationInput) (*model.ResourceAllocation, error) {
	if s == nil || s.db == nil {
		return nil, ErrPlatformAllocationInvalidInput
	}
	if err := normalizeAllocationDraftInput(&input); err != nil {
		return nil, err
	}
	now := s.now().UTC()
	allocation := &model.ResourceAllocation{
		ID: uuid.NewString(), TenantID: input.TenantID, Mode: input.Mode, ValidFrom: input.ValidFrom,
		ExpiresAt: input.ExpiresAt, ContractRef: input.ContractRef, State: model.ResourceAllocationDraft,
		RowVersion: 1, RenewedFromID: input.RenewedFrom,
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := reauthorizePlatformPermission(tx, authorization, PermissionPlatformAllocationsWrite, now); err != nil {
			return err
		}
		allocation.CreatedByUserID = authorization.EffectiveUserID
		if err := requireActiveAllocationTenant(tx, input.TenantID); err != nil {
			return err
		}
		scope, err := loadAllocatableNamespaceScope(tx, input.ScopeID, now)
		if err != nil {
			return err
		}
		if input.RenewedFrom != nil {
			var count int64
			if err := tx.Model(&model.ResourceAllocation{}).Where("id = ?", *input.RenewedFrom).Count(&count).Error; err != nil {
				return err
			}
			if count != 1 {
				return ErrPlatformAllocationObjectNotFound
			}
		}
		if err := tx.Create(allocation).Error; err != nil {
			return mapPlatformAllocationConstraint(err)
		}
		item := model.ResourceAllocationItem{
			ID: uuid.NewString(), AllocationID: allocation.ID, ScopeID: scope.ID,
			ScopeRowVersionSnapshot: scope.RowVersion,
		}
		if err := tx.Create(&item).Error; err != nil {
			return mapPlatformAllocationConstraint(err)
		}
		allocation.Items = []model.ResourceAllocationItem{item}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return allocation, nil
}

type UpdatePlatformAllocationDraftInput struct {
	AllocationID       string
	ExpectedRowVersion int64
	TenantID           string
	Mode               model.ResourceAllocationMode
	ScopeID            string
	ValidFrom          time.Time
	ExpiresAt          *time.Time
	ContractRef        string
}

func (s *PlatformAllocationService) UpdateDraft(ctx context.Context, authorization *ManagementAuthorizationContext, input UpdatePlatformAllocationDraftInput) (*model.ResourceAllocation, error) {
	if s == nil || s.db == nil || input.ExpectedRowVersion <= 0 || validateRequired("allocation_id", strings.TrimSpace(input.AllocationID), 36) != nil {
		return nil, ErrPlatformAllocationInvalidInput
	}
	draftInput := CreatePlatformAllocationInput{
		TenantID: input.TenantID, Mode: input.Mode, ScopeID: input.ScopeID, ValidFrom: input.ValidFrom,
		ExpiresAt: input.ExpiresAt, ContractRef: input.ContractRef,
	}
	if err := normalizeAllocationDraftInput(&draftInput); err != nil {
		return nil, err
	}
	now := s.now().UTC()
	var allocation model.ResourceAllocation
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := reauthorizePlatformPermission(tx, authorization, PermissionPlatformAllocationsWrite, now); err != nil {
			return err
		}
		current, err := loadPlatformAllocation(tx, strings.TrimSpace(input.AllocationID))
		if err != nil {
			return err
		}
		if current.RowVersion != input.ExpectedRowVersion {
			return ErrPlatformAllocationVersionConflict
		}
		if current.State != model.ResourceAllocationDraft {
			return ErrPlatformAllocationStateTransition
		}
		if len(current.Items) != 1 {
			return ErrPlatformAllocationItemPolicy
		}
		if err := requireActiveAllocationTenant(tx, draftInput.TenantID); err != nil {
			return err
		}
		scope, err := loadAllocatableNamespaceScope(tx, draftInput.ScopeID, now)
		if err != nil {
			return err
		}
		updated := tx.Model(&model.ResourceAllocation{}).
			Where("id = ? AND row_version = ? AND state = ?", current.ID, current.RowVersion, model.ResourceAllocationDraft).
			Updates(map[string]any{
				"tenant_id": draftInput.TenantID, "mode": draftInput.Mode, "valid_from": draftInput.ValidFrom,
				"expires_at": draftInput.ExpiresAt, "contract_ref": draftInput.ContractRef,
				"row_version": gorm.Expr("row_version + 1"),
			})
		if updated.Error != nil {
			return mapPlatformAllocationConstraint(updated.Error)
		}
		if updated.RowsAffected != 1 {
			return ErrPlatformAllocationVersionConflict
		}
		itemUpdated := tx.Model(&model.ResourceAllocationItem{}).
			Where("id = ? AND allocation_id = ?", current.Items[0].ID, current.ID).
			Updates(map[string]any{"scope_id": scope.ID, "scope_row_version_snapshot": scope.RowVersion})
		if itemUpdated.Error != nil {
			return mapPlatformAllocationConstraint(itemUpdated.Error)
		}
		if itemUpdated.RowsAffected != 1 {
			return ErrPlatformAllocationItemPolicy
		}
		result, err := loadPlatformAllocation(tx, current.ID)
		if err == nil {
			allocation = *result
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return &allocation, nil
}

type PlatformAllocationActionInput struct {
	AllocationID       string
	ExpectedRowVersion int64
	Reason             string
}

func (s *PlatformAllocationService) Schedule(ctx context.Context, authorization *ManagementAuthorizationContext, input PlatformAllocationActionInput) (*model.ResourceAllocation, error) {
	return s.transition(ctx, authorization, input, model.ResourceAllocationScheduled)
}

func (s *PlatformAllocationService) Activate(ctx context.Context, authorization *ManagementAuthorizationContext, input PlatformAllocationActionInput) (*model.ResourceAllocation, error) {
	return s.transition(ctx, authorization, input, model.ResourceAllocationActive)
}

func (s *PlatformAllocationService) Suspend(ctx context.Context, authorization *ManagementAuthorizationContext, input PlatformAllocationActionInput) (*model.ResourceAllocation, error) {
	return s.transition(ctx, authorization, input, model.ResourceAllocationSuspended)
}

func (s *PlatformAllocationService) Resume(ctx context.Context, authorization *ManagementAuthorizationContext, input PlatformAllocationActionInput) (*model.ResourceAllocation, error) {
	return s.transition(ctx, authorization, input, model.ResourceAllocationActive)
}

func (s *PlatformAllocationService) Revoke(ctx context.Context, authorization *ManagementAuthorizationContext, input PlatformAllocationActionInput) (*model.ResourceAllocation, error) {
	return s.transition(ctx, authorization, input, model.ResourceAllocationRevoked)
}

func (s *PlatformAllocationService) transition(ctx context.Context, authorization *ManagementAuthorizationContext, input PlatformAllocationActionInput, target model.ResourceAllocationState) (*model.ResourceAllocation, error) {
	if s == nil || s.db == nil {
		return nil, ErrPlatformAllocationInvalidInput
	}
	input.AllocationID = strings.TrimSpace(input.AllocationID)
	input.Reason = strings.TrimSpace(input.Reason)
	if validateRequired("allocation_id", input.AllocationID, 36) != nil || input.ExpectedRowVersion <= 0 {
		return nil, ErrPlatformAllocationInvalidInput
	}
	if validateRequired("reason", input.Reason, 500) != nil {
		return nil, ErrPlatformAllocationReasonRequired
	}
	now := s.now().UTC()
	var allocation model.ResourceAllocation
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := reauthorizePlatformPermission(tx, authorization, PermissionPlatformAllocationsWrite, now); err != nil {
			return err
		}
		current, err := loadPlatformAllocation(tx, input.AllocationID)
		if err != nil {
			return err
		}
		if current.RowVersion != input.ExpectedRowVersion {
			return ErrPlatformAllocationVersionConflict
		}
		if !validPlatformAllocationTransition(current.State, target) {
			return ErrPlatformAllocationStateTransition
		}

		updates := map[string]any{"state": target, "row_version": gorm.Expr("row_version + 1")}
		switch target {
		case model.ResourceAllocationScheduled:
			if !current.ValidFrom.After(now) {
				return ErrPlatformAllocationTimeInvalid
			}
			if err := validateAllocationForOccupancy(tx, current, now, false); err != nil {
				return err
			}
		case model.ResourceAllocationActive:
			if current.ValidFrom.After(now) || (current.ExpiresAt != nil && !current.ExpiresAt.After(now)) {
				return ErrPlatformAllocationTimeInvalid
			}
			if err := validateAllocationForOccupancy(tx, current, now, true); err != nil {
				return err
			}
			if current.ActivatedAt == nil {
				updates["activated_by_user_id"] = authorization.EffectiveUserID
				updates["activated_at"] = now
			}
		case model.ResourceAllocationSuspended:
		case model.ResourceAllocationRevoked:
			updates["terminated_by_user_id"] = authorization.EffectiveUserID
			updates["terminated_at"] = now
			updates["termination_reason"] = input.Reason
		default:
			return ErrPlatformAllocationStateTransition
		}

		updated := tx.Model(&model.ResourceAllocation{}).
			Where("id = ? AND row_version = ? AND state = ?", current.ID, current.RowVersion, current.State).
			Updates(updates)
		if updated.Error != nil {
			return mapPlatformAllocationConstraint(updated.Error)
		}
		if updated.RowsAffected != 1 {
			return ErrPlatformAllocationVersionConflict
		}
		result, err := loadPlatformAllocation(tx, current.ID)
		if err == nil {
			allocation = *result
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return &allocation, nil
}

type RenewPlatformAllocationInput struct {
	AllocationID       string
	ExpectedRowVersion int64
	ValidFrom          time.Time
	ExpiresAt          *time.Time
	ContractRef        *string
	Reason             string
}

func (s *PlatformAllocationService) Renew(ctx context.Context, authorization *ManagementAuthorizationContext, input RenewPlatformAllocationInput) (*model.ResourceAllocation, error) {
	if s == nil || s.db == nil {
		return nil, ErrPlatformAllocationInvalidInput
	}
	input.AllocationID = strings.TrimSpace(input.AllocationID)
	input.Reason = strings.TrimSpace(input.Reason)
	if validateRequired("allocation_id", input.AllocationID, 36) != nil || input.ExpectedRowVersion <= 0 {
		return nil, ErrPlatformAllocationInvalidInput
	}
	if validateRequired("reason", input.Reason, 500) != nil {
		return nil, ErrPlatformAllocationReasonRequired
	}
	now := s.now().UTC()
	var renewed *model.ResourceAllocation
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := reauthorizePlatformPermission(tx, authorization, PermissionPlatformAllocationsWrite, now); err != nil {
			return err
		}
		current, err := loadPlatformAllocation(tx, input.AllocationID)
		if err != nil {
			return err
		}
		if current.RowVersion != input.ExpectedRowVersion {
			return ErrPlatformAllocationVersionConflict
		}
		if current.State != model.ResourceAllocationActive && current.State != model.ResourceAllocationSuspended &&
			current.State != model.ResourceAllocationExpired && current.State != model.ResourceAllocationRevoked {
			return ErrPlatformAllocationStateTransition
		}
		if len(current.Items) != 1 {
			return ErrPlatformAllocationItemPolicy
		}
		contractRef := current.ContractRef
		if input.ContractRef != nil {
			contractRef = strings.TrimSpace(*input.ContractRef)
		}
		renewedFrom := current.ID
		createInput := CreatePlatformAllocationInput{
			TenantID: current.TenantID, Mode: current.Mode, ScopeID: current.Items[0].ScopeID,
			ValidFrom: input.ValidFrom, ExpiresAt: input.ExpiresAt, ContractRef: contractRef, RenewedFrom: &renewedFrom,
		}
		if err := normalizeAllocationDraftInput(&createInput); err != nil {
			return err
		}
		if err := requireActiveAllocationTenant(tx, createInput.TenantID); err != nil {
			return err
		}
		scope, err := loadAllocatableNamespaceScope(tx, createInput.ScopeID, now)
		if err != nil {
			return err
		}
		candidate := &model.ResourceAllocation{
			ID: uuid.NewString(), TenantID: createInput.TenantID, Mode: createInput.Mode, ValidFrom: createInput.ValidFrom,
			ExpiresAt: createInput.ExpiresAt, ContractRef: createInput.ContractRef, State: model.ResourceAllocationDraft,
			RowVersion: 1, CreatedByUserID: authorization.EffectiveUserID, RenewedFromID: &renewedFrom,
		}
		if err := tx.Create(candidate).Error; err != nil {
			return mapPlatformAllocationConstraint(err)
		}
		item := model.ResourceAllocationItem{ID: uuid.NewString(), AllocationID: candidate.ID, ScopeID: scope.ID, ScopeRowVersionSnapshot: scope.RowVersion}
		if err := tx.Create(&item).Error; err != nil {
			return mapPlatformAllocationConstraint(err)
		}
		candidate.Items = []model.ResourceAllocationItem{item}
		renewed = candidate
		return nil
	})
	if err != nil {
		return nil, err
	}
	return renewed, nil
}

func normalizeAllocationDraftInput(input *CreatePlatformAllocationInput) error {
	if input == nil {
		return ErrPlatformAllocationInvalidInput
	}
	input.TenantID = strings.TrimSpace(input.TenantID)
	input.ScopeID = strings.TrimSpace(input.ScopeID)
	input.ContractRef = strings.TrimSpace(input.ContractRef)
	if validateRequired("tenant_id", input.TenantID, 36) != nil || validateRequired("scope_id", input.ScopeID, 36) != nil || len(input.ContractRef) > 200 || input.ValidFrom.IsZero() {
		return ErrPlatformAllocationInvalidInput
	}
	if input.Mode == model.ResourceAllocationShared || !input.Mode.Valid() {
		return ErrPlatformAllocationModeUnsupported
	}
	input.ValidFrom = input.ValidFrom.UTC()
	if input.ExpiresAt != nil {
		expiresAt := input.ExpiresAt.UTC()
		input.ExpiresAt = &expiresAt
		if !expiresAt.After(input.ValidFrom) {
			return ErrPlatformAllocationTimeInvalid
		}
	}
	if input.Mode == model.ResourceAllocationLeased && input.ExpiresAt == nil {
		return ErrPlatformAllocationTimeInvalid
	}
	return nil
}

func validPlatformAllocationTransition(current, target model.ResourceAllocationState) bool {
	switch target {
	case model.ResourceAllocationScheduled:
		return current == model.ResourceAllocationDraft
	case model.ResourceAllocationActive:
		return current == model.ResourceAllocationDraft || current == model.ResourceAllocationScheduled || current == model.ResourceAllocationSuspended
	case model.ResourceAllocationSuspended:
		return current == model.ResourceAllocationActive
	case model.ResourceAllocationRevoked:
		return current == model.ResourceAllocationDraft || current == model.ResourceAllocationScheduled || current == model.ResourceAllocationActive || current == model.ResourceAllocationSuspended
	default:
		return false
	}
}

func validateAllocationForOccupancy(tx *gorm.DB, allocation *model.ResourceAllocation, now time.Time, requireCurrentWindow bool) error {
	if allocation == nil || len(allocation.Items) != 1 {
		return ErrPlatformAllocationItemPolicy
	}
	if allocation.Mode == model.ResourceAllocationShared || !allocation.Mode.Valid() {
		return ErrPlatformAllocationModeUnsupported
	}
	if requireCurrentWindow && (allocation.ValidFrom.After(now) || (allocation.ExpiresAt != nil && !allocation.ExpiresAt.After(now))) {
		return ErrPlatformAllocationTimeInvalid
	}
	if err := requireActiveAllocationTenant(tx, allocation.TenantID); err != nil {
		return err
	}
	scope, err := loadAllocatableNamespaceScope(tx, allocation.Items[0].ScopeID, now)
	if err != nil {
		return err
	}
	return checkPlatformAllocationConflicts(tx, allocation.ID, scope, allocation.ValidFrom, allocation.ExpiresAt)
}

func requireActiveAllocationTenant(tx *gorm.DB, tenantID string) error {
	var count int64
	if err := tx.Model(&model.Tenant{}).Where("id = ? AND status = ?", tenantID, model.TenantStatusActive).Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return ErrPlatformAllocationTenantNotActive
	}
	return nil
}

func loadAllocatableNamespaceScope(tx *gorm.DB, scopeID string, now time.Time) (*model.ResourceScope, error) {
	var scope model.ResourceScope
	if err := tx.Where("id = ? AND type = ? AND lifecycle_state = ?", scopeID, model.ResourceScopeNamespace, model.ResourceScopeAllocatable).First(&scope).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrPlatformAllocationScopeUnavailable
		}
		return nil, err
	}
	var resource model.PlatformResource
	if err := tx.Where("provider_id = ? AND id = ? AND lifecycle_state = ?", scope.ProviderID, scope.PlatformResourceID, model.PlatformResourceActive).First(&resource).Error; err != nil {
		return nil, ErrPlatformAllocationScopeUnavailable
	}
	var providerCount int64
	if err := tx.Model(&model.ResourceProvider{}).Where("id = ? AND status = ?", scope.ProviderID, model.ProviderStatusActive).Count(&providerCount).Error; err != nil {
		return nil, err
	}
	if providerCount != 1 {
		return nil, ErrPlatformAllocationScopeUnavailable
	}
	if err := validateAllocatableResourceSource(tx, &resource, now); err != nil {
		if errors.Is(err, ErrResourceScopeNotAllocatable) {
			return nil, ErrPlatformAllocationScopeUnavailable
		}
		return nil, err
	}
	if err := validateAllocatableScopeEvidence(tx, &resource, &scope, now); err != nil {
		if errors.Is(err, ErrResourceScopeNotAllocatable) {
			return nil, ErrPlatformAllocationScopeUnavailable
		}
		return nil, err
	}
	return &scope, nil
}

func checkPlatformAllocationConflicts(tx *gorm.DB, allocationID string, scope *model.ResourceScope, validFrom time.Time, expiresAt *time.Time) error {
	if scope == nil {
		return ErrPlatformAllocationScopeUnavailable
	}
	query := func() *gorm.DB {
		return tx.Table("resource_allocation_item AS occupied_item").
			Joins("JOIN resource_allocation AS occupied ON occupied.id = occupied_item.allocation_id").
			Where("occupied.id <> ? AND occupied.state IN ?", allocationID,
				[]model.ResourceAllocationState{model.ResourceAllocationScheduled, model.ResourceAllocationActive, model.ResourceAllocationSuspended}).
			Where("julianday(occupied.valid_from) < COALESCE(julianday(?), 5373484.499999)", expiresAt).
			Where("julianday(?) < COALESCE(julianday(occupied.expires_at), 5373484.499999)", validFrom)
	}
	var sameScope int64
	if err := query().Where("occupied_item.scope_id = ?", scope.ID).Count(&sameScope).Error; err != nil {
		return err
	}
	if sameScope != 0 {
		return ErrPlatformAllocationScopeConflict
	}
	var hierarchy int64
	if err := query().
		Joins("JOIN resource_scope AS occupied_scope ON occupied_scope.id = occupied_item.scope_id").
		Where("occupied_item.scope_id <> ? AND (occupied_scope.parent_id = ? OR occupied_scope.id = ?)", scope.ID, scope.ID, scope.ParentID).
		Count(&hierarchy).Error; err != nil {
		return err
	}
	if hierarchy != 0 {
		return ErrPlatformAllocationHierarchyConflict
	}
	return nil
}

func loadPlatformAllocation(tx *gorm.DB, allocationID string) (*model.ResourceAllocation, error) {
	var allocation model.ResourceAllocation
	if err := tx.Preload("Items").First(&allocation, "id = ?", allocationID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrPlatformAllocationObjectNotFound
		}
		return nil, err
	}
	return &allocation, nil
}

func mapPlatformAllocationConstraint(err error) error {
	if err == nil {
		return nil
	}
	message := err.Error()
	switch {
	case strings.Contains(message, "S3_ALLOCATION_SCOPE_CONFLICT"):
		return ErrPlatformAllocationScopeConflict
	case strings.Contains(message, "S3_ALLOCATION_HIERARCHY_CONFLICT"):
		return ErrPlatformAllocationHierarchyConflict
	case strings.Contains(message, "S3_ALLOCATION_STATE_TRANSITION_INVALID"), strings.Contains(message, "S3_ALLOCATION_IMMUTABLE"):
		return ErrPlatformAllocationStateTransition
	case strings.Contains(message, "S3_ALLOCATION_VERSION_INVALID"):
		return ErrPlatformAllocationVersionConflict
	case strings.Contains(message, "S3_ALLOCATION_SCOPE_NOT_FOUND_OR_STALE"):
		return ErrPlatformAllocationScopeUnavailable
	case strings.Contains(message, "S3_ALLOCATION_TENANT_NOT_FOUND"):
		return ErrPlatformAllocationTenantNotActive
	case strings.Contains(message, "S3_ALLOCATION_ITEM"):
		return ErrPlatformAllocationItemPolicy
	case isDatabaseConstraintError(err):
		return ErrPlatformAllocationInvalidInput
	default:
		return err
	}
}
