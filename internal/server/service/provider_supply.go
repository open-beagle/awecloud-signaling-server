package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

var (
	ErrProviderSupplyInvalidInput       = errors.New("invalid Provider supply input")
	ErrProviderSupplyObjectNotFound     = errors.New("Provider supply object not found")
	ErrProviderSupplyConflict           = errors.New("Provider supply object conflict")
	ErrProviderSupplyVersionConflict    = errors.New("Provider supply row version conflict")
	ErrTechnicalResourceUnbound         = errors.New("TECHNICAL_RESOURCE_UNBOUND")
	ErrTechnicalResourceDisabled        = errors.New("TECHNICAL_RESOURCE_DISABLED")
	ErrTechnicalResourceRetired         = errors.New("TECHNICAL_RESOURCE_RETIRED")
	ErrTechnicalResourceStateTransition = errors.New("invalid TechnicalResource state transition")
	ErrCredentialRevisionStale          = errors.New("CREDENTIAL_REVISION_STALE")
)

type ProviderSupplyService struct {
	db  *gorm.DB
	now func() time.Time
}

func NewProviderSupplyService(database *gorm.DB) *ProviderSupplyService {
	return &ProviderSupplyService{db: database, now: time.Now}
}

type CreateTechnicalResourceInput struct {
	Type               model.TechnicalResourceType
	StableKey          string
	ParentID           string
	CredentialRevision int64
}

func (s *ProviderSupplyService) CreateTechnicalResource(ctx context.Context, authorization *ManagementAuthorizationContext, input CreateTechnicalResourceInput) (*model.TechnicalResource, error) {
	if s == nil || s.db == nil {
		return nil, ErrProviderSupplyInvalidInput
	}
	input.StableKey = strings.TrimSpace(input.StableKey)
	input.ParentID = strings.TrimSpace(input.ParentID)
	if err := validateRequired("stable_key", input.StableKey, 128); err != nil || input.CredentialRevision <= 0 {
		return nil, ErrProviderSupplyInvalidInput
	}
	if input.Type != model.TechnicalResourceAgent && input.Type != model.TechnicalResourceEndpoint {
		return nil, ErrProviderSupplyInvalidInput
	}
	if (input.Type == model.TechnicalResourceAgent && input.ParentID != "") ||
		(input.Type == model.TechnicalResourceEndpoint && input.ParentID == "") {
		return nil, ErrProviderSupplyInvalidInput
	}

	now := s.now().UTC()
	resource := &model.TechnicalResource{
		ID: uuid.NewString(), Type: input.Type, StableKey: input.StableKey,
		LifecycleState: model.TechnicalResourcePending, HealthState: model.ResourceHealthUnknown,
		CredentialRevision: input.CredentialRevision, ConfigRevision: 1, ObservedRevision: 0, RowVersion: 1,
	}
	if input.ParentID != "" {
		resource.ParentID = &input.ParentID
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		providerID, err := reauthorizeProviderPermission(tx, authorization, PermissionProviderTechnicalResourcesWrite, now)
		if err != nil {
			return err
		}
		resource.ProviderID = providerID
		if resource.ParentID != nil {
			var parent model.TechnicalResource
			if err := tx.Where("provider_id = ? AND id = ? AND type = ? AND lifecycle_state = ?",
				providerID, *resource.ParentID, model.TechnicalResourceAgent, model.TechnicalResourceRegistered).First(&parent).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return ErrProviderSupplyObjectNotFound
				}
				return err
			}
		}
		if err := tx.Create(resource).Error; err != nil {
			if isDatabaseConstraintError(err) {
				return ErrProviderSupplyConflict
			}
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return resource, nil
}

type BindTechnicalResourceInput struct {
	TechnicalResourceID     string
	SourceType              model.TechnicalResourceBindingSourceType
	SourceID                string
	ExpectedResourceVersion int64
	Reason                  string
}

type BindTechnicalResourceResult struct {
	TechnicalResource *model.TechnicalResource        `json:"technical_resource"`
	Binding           *model.TechnicalResourceBinding `json:"binding"`
}

func (s *ProviderSupplyService) BindTechnicalResource(ctx context.Context, authorization *ManagementAuthorizationContext, input BindTechnicalResourceInput) (*BindTechnicalResourceResult, error) {
	if s == nil || s.db == nil {
		return nil, ErrProviderSupplyInvalidInput
	}
	input.TechnicalResourceID = strings.TrimSpace(input.TechnicalResourceID)
	input.SourceID = strings.TrimSpace(input.SourceID)
	input.Reason = strings.TrimSpace(input.Reason)
	if validateRequired("technical_resource_id", input.TechnicalResourceID, 36) != nil ||
		validateRequired("source_id", input.SourceID, 100) != nil ||
		validateRequired("reason", input.Reason, 500) != nil || input.ExpectedResourceVersion <= 0 {
		return nil, ErrProviderSupplyInvalidInput
	}
	if input.SourceType != model.TechnicalResourceBindingLegacyNode && input.SourceType != model.TechnicalResourceBindingLegacyEndpoint {
		return nil, ErrProviderSupplyInvalidInput
	}

	now := s.now().UTC()
	result := &BindTechnicalResourceResult{}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		providerID, err := reauthorizeProviderPermission(tx, authorization, PermissionProviderTechnicalResourcesWrite, now)
		if err != nil {
			return err
		}
		var resource model.TechnicalResource
		if err := tx.Where("provider_id = ? AND id = ?", providerID, input.TechnicalResourceID).First(&resource).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrProviderSupplyObjectNotFound
			}
			return err
		}
		if resource.RowVersion != input.ExpectedResourceVersion {
			return ErrProviderSupplyVersionConflict
		}
		if resource.LifecycleState != model.TechnicalResourcePending && resource.LifecycleState != model.TechnicalResourceRegistered {
			return ErrTechnicalResourceStateTransition
		}
		if err := validateLegacyTechnicalResourceSource(tx, &resource, input.SourceType, input.SourceID); err != nil {
			return err
		}
		var activeBindingCount int64
		if err := tx.Model(&model.TechnicalResourceBinding{}).
			Where("technical_resource_id = ? AND enabled = ?", resource.ID, true).Count(&activeBindingCount).Error; err != nil {
			return err
		}
		if activeBindingCount != 0 {
			return ErrProviderSupplyConflict
		}

		binding := &model.TechnicalResourceBinding{
			ID: uuid.NewString(), TechnicalResourceID: resource.ID, SourceType: input.SourceType, SourceID: input.SourceID,
			CredentialRevision: resource.CredentialRevision, Enabled: true, BoundByUserID: authorization.EffectiveUserID,
			Reason: input.Reason, RowVersion: 1,
		}
		if err := tx.Create(binding).Error; err != nil {
			if isDatabaseConstraintError(err) {
				return ErrProviderSupplyConflict
			}
			return err
		}
		updated := tx.Model(&model.TechnicalResource{}).
			Where("provider_id = ? AND id = ? AND row_version = ? AND lifecycle_state IN ?",
				providerID, resource.ID, resource.RowVersion,
				[]model.TechnicalResourceLifecycleState{model.TechnicalResourcePending, model.TechnicalResourceRegistered}).
			Updates(map[string]any{
				"lifecycle_state": model.TechnicalResourceRegistered,
				"config_revision": gorm.Expr("config_revision + 1"),
				"row_version":     gorm.Expr("row_version + 1"),
			})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrProviderSupplyVersionConflict
		}
		if err := tx.First(&resource, "provider_id = ? AND id = ?", providerID, resource.ID).Error; err != nil {
			return err
		}
		result.TechnicalResource = &resource
		result.Binding = binding
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

type SetTechnicalResourceLifecycleInput struct {
	TechnicalResourceID string
	TargetState         model.TechnicalResourceLifecycleState
	ExpectedRowVersion  int64
	Reason              string
}

func (s *ProviderSupplyService) SetTechnicalResourceLifecycle(ctx context.Context, authorization *ManagementAuthorizationContext, input SetTechnicalResourceLifecycleInput) (*model.TechnicalResource, error) {
	if s == nil || s.db == nil {
		return nil, ErrProviderSupplyInvalidInput
	}
	input.TechnicalResourceID = strings.TrimSpace(input.TechnicalResourceID)
	input.Reason = strings.TrimSpace(input.Reason)
	if validateRequired("technical_resource_id", input.TechnicalResourceID, 36) != nil ||
		validateRequired("reason", input.Reason, 500) != nil || input.ExpectedRowVersion <= 0 {
		return nil, ErrProviderSupplyInvalidInput
	}
	if input.TargetState != model.TechnicalResourceRegistered && input.TargetState != model.TechnicalResourceDisabled && input.TargetState != model.TechnicalResourceRetired {
		return nil, ErrProviderSupplyInvalidInput
	}

	now := s.now().UTC()
	var resource model.TechnicalResource
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		providerID, err := reauthorizeProviderPermission(tx, authorization, PermissionProviderTechnicalResourcesWrite, now)
		if err != nil {
			return err
		}
		if err := tx.Where("provider_id = ? AND id = ?", providerID, input.TechnicalResourceID).First(&resource).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrProviderSupplyObjectNotFound
			}
			return err
		}
		if resource.RowVersion != input.ExpectedRowVersion {
			return ErrProviderSupplyVersionConflict
		}
		if !validTechnicalResourceTransition(resource.LifecycleState, input.TargetState) {
			return ErrTechnicalResourceStateTransition
		}
		if input.TargetState == model.TechnicalResourceRegistered {
			var activeBindings int64
			if err := tx.Model(&model.TechnicalResourceBinding{}).
				Where("technical_resource_id = ? AND enabled = ?", resource.ID, true).Count(&activeBindings).Error; err != nil {
				return err
			}
			if activeBindings != 1 {
				return ErrTechnicalResourceUnbound
			}
		}
		updated := tx.Model(&model.TechnicalResource{}).
			Where("provider_id = ? AND id = ? AND row_version = ? AND lifecycle_state = ?", providerID, resource.ID, resource.RowVersion, resource.LifecycleState).
			Updates(map[string]any{
				"lifecycle_state": input.TargetState,
				"config_revision": gorm.Expr("config_revision + 1"),
				"row_version":     gorm.Expr("row_version + 1"),
			})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrProviderSupplyVersionConflict
		}
		if input.TargetState == model.TechnicalResourceRetired {
			if err := tx.Model(&model.TechnicalResourceBinding{}).
				Where("technical_resource_id = ? AND enabled = ?", resource.ID, true).
				Updates(map[string]any{"enabled": false, "row_version": gorm.Expr("row_version + 1")}).Error; err != nil {
				return err
			}
		}
		return tx.First(&resource, "provider_id = ? AND id = ?", providerID, resource.ID).Error
	})
	if err != nil {
		return nil, err
	}
	return &resource, nil
}

type TechnicalResourceCredential struct {
	SourceType         model.TechnicalResourceBindingSourceType
	SourceID           string
	CredentialRevision int64
}

func (s *ProviderSupplyService) RecordTechnicalResourceHeartbeat(ctx context.Context, credential TechnicalResourceCredential, leaseDuration time.Duration) (*model.TechnicalResource, error) {
	if s == nil || s.db == nil || leaseDuration <= 0 || leaseDuration > 24*time.Hour {
		return nil, ErrProviderSupplyInvalidInput
	}
	now := s.now().UTC()
	var resource *model.TechnicalResource
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		resolved, err := resolveAuthenticatedTechnicalResource(tx, credential)
		if err != nil {
			return err
		}
		resource = resolved
		if err := requireReportingLifecycle(resource); err != nil {
			return err
		}
		updated := tx.Model(&model.TechnicalResource{}).
			Where("id = ? AND provider_id = ? AND credential_revision = ? AND lifecycle_state = ?",
				resource.ID, resource.ProviderID, credential.CredentialRevision, model.TechnicalResourceRegistered).
			Updates(map[string]any{
				"health_state":      model.ResourceHealthOnline,
				"last_received_at":  now,
				"lease_expires_at":  now.Add(leaseDuration),
				"observed_revision": gorm.Expr("observed_revision + 1"),
			})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrTechnicalResourceDisabled
		}
		return tx.First(resource, "id = ?", resource.ID).Error
	})
	if err != nil {
		return nil, err
	}
	return resource, nil
}

func (s *ProviderSupplyService) ExpireTechnicalResourceLeases(ctx context.Context, at time.Time) (int64, error) {
	if s == nil || s.db == nil || at.IsZero() {
		return 0, ErrProviderSupplyInvalidInput
	}
	result := s.db.WithContext(ctx).Model(&model.TechnicalResource{}).
		Where("lifecycle_state <> ? AND lease_expires_at IS NOT NULL AND julianday(lease_expires_at) <= julianday(?) AND health_state <> ?",
			model.TechnicalResourceRetired, at.UTC(), model.ResourceHealthOffline).
		Updates(map[string]any{
			"health_state":      model.ResourceHealthOffline,
			"observed_revision": gorm.Expr("observed_revision + 1"),
		})
	return result.RowsAffected, result.Error
}

func reauthorizeProviderPermission(tx *gorm.DB, authorization *ManagementAuthorizationContext, permission string, at time.Time) (string, error) {
	if tx == nil || authorization == nil || authorization.ScopeType != model.ManagementScopeProvider ||
		strings.TrimSpace(authorization.ScopeID) == "" || authorization.ActorUserID == 0 || authorization.EffectiveUserID == 0 ||
		authorization.PermissionRevision <= 0 || at.IsZero() {
		return "", ErrManagementPermissionDenied
	}
	var current *ManagementAuthorizationContext
	var err error
	if authorization.SimulationSessionID != "" {
		_, current, err = ResolveUserSimulationSession(tx, authorization.SimulationSessionID, authorization.ActorUserID, at)
	} else {
		current, err = ResolveManagementContext(tx, authorization.EffectiveUserID, model.ManagementScopeProvider, authorization.ScopeID, at, false)
	}
	if err != nil || current == nil || current.ScopeID != authorization.ScopeID ||
		current.ActorUserID != authorization.ActorUserID || current.EffectiveUserID != authorization.EffectiveUserID ||
		current.PermissionRevision != authorization.PermissionRevision {
		return "", ErrManagementPermissionDenied
	}
	if err := AuthorizeManagementPermission(current, permission); err != nil {
		return "", err
	}
	return current.ScopeID, nil
}

func validateLegacyTechnicalResourceSource(tx *gorm.DB, resource *model.TechnicalResource, sourceType model.TechnicalResourceBindingSourceType, sourceID string) error {
	if resource == nil {
		return ErrProviderSupplyInvalidInput
	}
	switch resource.Type {
	case model.TechnicalResourceAgent:
		if sourceType != model.TechnicalResourceBindingLegacyNode {
			return ErrProviderSupplyInvalidInput
		}
		nodeID, err := strconv.ParseUint(sourceID, 10, 64)
		if err != nil || strconv.FormatUint(nodeID, 10) != sourceID {
			return ErrProviderSupplyInvalidInput
		}
		var node model.Node
		if err := tx.Where("id = ? AND type = ?", nodeID, model.NodeTypeAgent).First(&node).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrProviderSupplyObjectNotFound
			}
			return err
		}
	case model.TechnicalResourceEndpoint:
		if sourceType != model.TechnicalResourceBindingLegacyEndpoint || resource.ParentID == nil {
			return ErrProviderSupplyInvalidInput
		}
		var endpoint model.Endpoint
		if err := tx.First(&endpoint, "id = ?", sourceID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrProviderSupplyObjectNotFound
			}
			return err
		}
		parentBinding, err := loadActiveTechnicalResourceBinding(tx, *resource.ParentID)
		if err != nil {
			return err
		}
		if parentBinding.SourceType != model.TechnicalResourceBindingLegacyNode {
			return ErrProviderSupplyConflict
		}
		parentNodeID, err := strconv.ParseUint(parentBinding.SourceID, 10, 64)
		if err != nil {
			return ErrProviderSupplyConflict
		}
		var parentNode model.Node
		if err := tx.First(&parentNode, "id = ? AND type = ?", parentNodeID, model.NodeTypeAgent).Error; err != nil {
			return ErrProviderSupplyConflict
		}
		if endpoint.UserID != parentNode.UserID {
			return ErrProviderSupplyConflict
		}
	default:
		return ErrProviderSupplyInvalidInput
	}
	return nil
}

func resolveAuthenticatedTechnicalResource(tx *gorm.DB, credential TechnicalResourceCredential) (*model.TechnicalResource, error) {
	credential.SourceID = strings.TrimSpace(credential.SourceID)
	if tx == nil || credential.CredentialRevision <= 0 || validateRequired("source_id", credential.SourceID, 100) != nil ||
		(credential.SourceType != model.TechnicalResourceBindingLegacyNode && credential.SourceType != model.TechnicalResourceBindingLegacyEndpoint) {
		return nil, ErrProviderSupplyInvalidInput
	}
	var binding model.TechnicalResourceBinding
	if err := tx.Where("source_type = ? AND source_id = ? AND enabled = ?", credential.SourceType, credential.SourceID, true).First(&binding).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTechnicalResourceUnbound
		}
		return nil, err
	}
	var resource model.TechnicalResource
	if err := tx.First(&resource, "id = ?", binding.TechnicalResourceID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTechnicalResourceUnbound
		}
		return nil, err
	}
	if binding.CredentialRevision != credential.CredentialRevision || resource.CredentialRevision != credential.CredentialRevision {
		return nil, ErrCredentialRevisionStale
	}
	return &resource, nil
}

func loadActiveTechnicalResourceBinding(tx *gorm.DB, technicalResourceID string) (*model.TechnicalResourceBinding, error) {
	var binding model.TechnicalResourceBinding
	if err := tx.Where("technical_resource_id = ? AND enabled = ?", technicalResourceID, true).First(&binding).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTechnicalResourceUnbound
		}
		return nil, err
	}
	return &binding, nil
}

func requireReportingLifecycle(resource *model.TechnicalResource) error {
	if resource == nil {
		return ErrTechnicalResourceUnbound
	}
	switch resource.LifecycleState {
	case model.TechnicalResourceRegistered:
		return nil
	case model.TechnicalResourceDisabled:
		return ErrTechnicalResourceDisabled
	case model.TechnicalResourceRetired:
		return ErrTechnicalResourceRetired
	default:
		return ErrTechnicalResourceUnbound
	}
}

func validTechnicalResourceTransition(current, target model.TechnicalResourceLifecycleState) bool {
	switch target {
	case model.TechnicalResourceDisabled:
		return current == model.TechnicalResourceRegistered
	case model.TechnicalResourceRegistered:
		return current == model.TechnicalResourceDisabled
	case model.TechnicalResourceRetired:
		return current == model.TechnicalResourcePending || current == model.TechnicalResourceRegistered || current == model.TechnicalResourceDisabled
	default:
		return false
	}
}

func isDatabaseConstraintError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "constraint") || strings.Contains(message, "unique") || strings.Contains(message, "s2_") || strings.Contains(message, "s3_")
}
