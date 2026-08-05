package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

const (
	providerSupplyReconciliationInterval = time.Minute
	providerSupplyReconciliationAction   = "reconcile_provider_supply_lease"
)

var providerSupplyReconciliationOutboxPolicies = map[string]JSONFieldPolicy{
	"provider_supply.changed": NewJSONFieldPolicy("provider_id", "aggregate_type", "aggregate_id", "row_version", "action"),
}

type ProviderSupplyReconciliationResult struct {
	ExpiredTechnicalResources int64
	StaleObservations         int64
	SuspendedScopes           int64
	UpdatedResources          int64
}

type ProviderSupplyReconciliationService struct {
	db  *gorm.DB
	now func() time.Time
}

func NewProviderSupplyReconciliationService(database *gorm.DB) *ProviderSupplyReconciliationService {
	return &ProviderSupplyReconciliationService{db: database, now: time.Now}
}

// ReconcileExpiredEvidence applies lease expiry without deleting historical
// candidates, observations, or scopes. A later valid snapshot may refresh the
// evidence, but suspended scopes still require an explicit Provider resume.
func (s *ProviderSupplyReconciliationService) ReconcileExpiredEvidence(ctx context.Context, at time.Time) (*ProviderSupplyReconciliationResult, error) {
	if s == nil || s.db == nil || at.IsZero() {
		return nil, ErrProviderSupplyInvalidInput
	}
	at = at.UTC()
	result := &ProviderSupplyReconciliationResult{}
	expiredTechnicalResources, err := NewProviderSupplyService(s.db).ExpireTechnicalResourceLeases(ctx, at)
	if err != nil {
		return nil, fmt.Errorf("expire TechnicalResource leases: %w", err)
	}
	result.ExpiredTechnicalResources = expiredTechnicalResources

	conflictKeys, err := s.expiredCrossProviderConflictKeys(ctx, at)
	if err != nil {
		return nil, err
	}
	for _, key := range conflictKeys {
		if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := reconcileCrossProviderSupplyConflict(tx, key.ResourceType, key.StableKey, at); err != nil {
				return err
			}
			return reconcileLinkedPlatformResourcesForStableKey(tx, key.ResourceType, key.StableKey, at)
		}); err != nil {
			return nil, fmt.Errorf("reconcile expired cross-Provider conflict: %w", err)
		}
	}

	resourceRefs, err := s.resourcesWithExpiredCandidates(ctx, at)
	if err != nil {
		return nil, err
	}
	for _, ref := range resourceRefs {
		requestID := uuid.NewString()
		if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var before model.PlatformResource
			if err := tx.Where("provider_id = ? AND id = ?", ref.ProviderID, ref.ResourceID).First(&before).Error; err != nil {
				return err
			}
			if err := reconcilePlatformResourceEvidence(tx, ref.ProviderID, ref.ResourceID, at); err != nil {
				return err
			}
			var after model.PlatformResource
			if err := tx.Where("provider_id = ? AND id = ?", ref.ProviderID, ref.ResourceID).First(&after).Error; err != nil {
				return err
			}
			if after.RowVersion == before.RowVersion {
				return nil
			}
			if err := appendProviderSupplyReconciliationEvent(tx, requestID, after.ProviderID, "platform_resource", after.ID, after.RowVersion, providerSupplyReconciliationAction); err != nil {
				return err
			}
			if err := recordProviderSupplyReconciliationAudit(tx, requestID, after.ProviderID, "platform_resource", after.ID, after.RowVersion, at); err != nil {
				return err
			}
			result.UpdatedResources++
			return nil
		}); err != nil {
			return nil, fmt.Errorf("reconcile PlatformResource %s: %w", ref.ResourceID, err)
		}
	}

	var expired []model.NamespaceObservation
	if err := s.db.WithContext(ctx).
		Where("state = ? AND julianday(lease_expires_at) <= julianday(?)", model.NamespaceObservationObserved, at).
		Order("lease_expires_at ASC, id ASC").Find(&expired).Error; err != nil {
		return nil, fmt.Errorf("query expired Namespace observations: %w", err)
	}
	for _, candidate := range expired {
		requestID := uuid.NewString()
		if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var observation model.NamespaceObservation
			if err := tx.Where("provider_id = ? AND id = ?", candidate.ProviderID, candidate.ID).First(&observation).Error; err != nil {
				return err
			}
			updated := tx.Model(&model.NamespaceObservation{}).
				Where("provider_id = ? AND id = ? AND state = ? AND julianday(lease_expires_at) <= julianday(?)",
					observation.ProviderID, observation.ID, model.NamespaceObservationObserved, at).
				Updates(map[string]any{
					"state":    model.NamespaceObservationStale,
					"revision": gorm.Expr("revision + 1"),
				})
			if updated.Error != nil {
				return updated.Error
			}
			if updated.RowsAffected == 0 {
				return nil
			}
			if err := tx.Where("provider_id = ? AND id = ?", observation.ProviderID, observation.ID).First(&observation).Error; err != nil {
				return err
			}
			result.StaleObservations++
			if err := appendProviderSupplyReconciliationEvent(tx, requestID, observation.ProviderID, "namespace_observation", observation.ID, observation.Revision, providerSupplyReconciliationAction); err != nil {
				return err
			}

			var scope model.ResourceScope
			err := tx.Where("provider_id = ? AND platform_resource_id = ? AND namespace_observation_id = ?",
				observation.ProviderID, observation.ClusterResourceID, observation.ID).First(&scope).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if err == nil && scope.LifecycleState != model.ResourceScopeRetired {
				updates := map[string]any{
					"evidence_revision": observation.Revision,
					"row_version":       gorm.Expr("row_version + 1"),
				}
				if scope.LifecycleState == model.ResourceScopeActive || scope.LifecycleState == model.ResourceScopeAllocatable {
					updates["lifecycle_state"] = model.ResourceScopeSuspended
					result.SuspendedScopes++
				}
				if err := tx.Model(&model.ResourceScope{}).Where("provider_id = ? AND id = ? AND row_version = ?", scope.ProviderID, scope.ID, scope.RowVersion).
					Updates(updates).Error; err != nil {
					return err
				}
				if err := tx.Where("provider_id = ? AND id = ?", scope.ProviderID, scope.ID).First(&scope).Error; err != nil {
					return err
				}
				if err := appendProviderSupplyReconciliationEvent(tx, requestID, scope.ProviderID, "resource_scope", scope.ID, scope.RowVersion, providerSupplyReconciliationAction); err != nil {
					return err
				}
			}

			var resourceBefore model.PlatformResource
			if err := tx.Where("provider_id = ? AND id = ?", observation.ProviderID, observation.ClusterResourceID).First(&resourceBefore).Error; err != nil {
				return err
			}
			if err := syncPlatformResourceAllocatableScopeCount(tx, resourceBefore.ProviderID, resourceBefore.ID); err != nil {
				return err
			}
			if err := refreshPlatformResourceHealth(tx, resourceBefore.ProviderID, resourceBefore.ID, at); err != nil {
				return err
			}
			var resourceAfter model.PlatformResource
			if err := tx.Where("provider_id = ? AND id = ?", resourceBefore.ProviderID, resourceBefore.ID).First(&resourceAfter).Error; err != nil {
				return err
			}
			if resourceAfter.RowVersion != resourceBefore.RowVersion {
				if err := appendProviderSupplyReconciliationEvent(tx, requestID, resourceAfter.ProviderID, "platform_resource", resourceAfter.ID, resourceAfter.RowVersion, providerSupplyReconciliationAction); err != nil {
					return err
				}
				result.UpdatedResources++
			}
			return recordProviderSupplyReconciliationAudit(tx, requestID, observation.ProviderID, "namespace_observation", observation.ID, observation.Revision, at)
		}); err != nil {
			return nil, fmt.Errorf("expire Namespace observation %s: %w", candidate.ID, err)
		}
	}
	return result, nil
}

func (s *ProviderSupplyReconciliationService) StartPeriodicMaintenance(ctx context.Context) {
	ticker := time.NewTicker(providerSupplyReconciliationInterval)
	defer ticker.Stop()
	logger.Info("启动 Provider Supply 租约对账任务，间隔 1 分钟")
	for {
		select {
		case <-ctx.Done():
			logger.Info("Provider Supply 租约对账任务已停止")
			return
		case at := <-ticker.C:
			result, err := s.ReconcileExpiredEvidence(ctx, at)
			if err != nil {
				logger.Warnf("Provider Supply 租约对账失败: %v", err)
				continue
			}
			if result.ExpiredTechnicalResources > 0 || result.StaleObservations > 0 || result.SuspendedScopes > 0 || result.UpdatedResources > 0 {
				logger.Infof("Provider Supply 租约对账完成: expired_technical_resources=%d stale_observations=%d suspended_scopes=%d updated_resources=%d",
					result.ExpiredTechnicalResources, result.StaleObservations, result.SuspendedScopes, result.UpdatedResources)
			}
		}
	}
}

type providerSupplyConflictKey struct {
	ResourceType model.SupplyResourceType
	StableKey    string
}

func (s *ProviderSupplyReconciliationService) expiredCrossProviderConflictKeys(ctx context.Context, at time.Time) ([]providerSupplyConflictKey, error) {
	var keys []providerSupplyConflictKey
	err := s.db.WithContext(ctx).Model(&model.SupplyCandidate{}).
		Select("DISTINCT resource_type, stable_key").
		Where("conflict_code = ? AND julianday(lease_expires_at) <= julianday(?)", supplyConflictCrossProvider, at).
		Scan(&keys).Error
	return keys, err
}

type providerSupplyResourceRef struct {
	ProviderID string
	ResourceID string
}

func (s *ProviderSupplyReconciliationService) resourcesWithExpiredCandidates(ctx context.Context, at time.Time) ([]providerSupplyResourceRef, error) {
	var refs []providerSupplyResourceRef
	err := s.db.WithContext(ctx).Table("platform_resource_source AS source").
		Select("DISTINCT source.provider_id, source.platform_resource_id AS resource_id").
		Joins("JOIN supply_candidate AS candidate ON candidate.provider_id = source.provider_id AND candidate.id = source.supply_candidate_id").
		Where("julianday(candidate.lease_expires_at) <= julianday(?)", at).
		Order("source.provider_id ASC, source.platform_resource_id ASC").Scan(&refs).Error
	return refs, err
}

func reconcileLinkedPlatformResourcesForSource(tx *gorm.DB, source *model.TechnicalResource, at time.Time) error {
	if tx == nil || source == nil {
		return ErrProviderSupplyInvalidInput
	}
	var refs []providerSupplyResourceRef
	if err := tx.Table("platform_resource_source AS resource_source").
		Select("DISTINCT resource_source.provider_id, resource_source.platform_resource_id AS resource_id").
		Joins("JOIN supply_candidate AS candidate ON candidate.provider_id = resource_source.provider_id AND candidate.id = resource_source.supply_candidate_id").
		Where("candidate.provider_id = ? AND candidate.technical_resource_id = ?", source.ProviderID, source.ID).
		Scan(&refs).Error; err != nil {
		return err
	}
	for _, ref := range refs {
		if err := reconcilePlatformResourceEvidence(tx, ref.ProviderID, ref.ResourceID, at); err != nil {
			return err
		}
	}
	return nil
}

func reconcileLinkedPlatformResourcesForStableKey(tx *gorm.DB, resourceType model.SupplyResourceType, stableKey string, at time.Time) error {
	var refs []providerSupplyResourceRef
	if err := tx.Table("platform_resource_source AS resource_source").
		Select("DISTINCT resource_source.provider_id, resource_source.platform_resource_id AS resource_id").
		Joins("JOIN supply_candidate AS candidate ON candidate.provider_id = resource_source.provider_id AND candidate.id = resource_source.supply_candidate_id").
		Where("candidate.resource_type = ? AND candidate.stable_key = ?", resourceType, stableKey).
		Scan(&refs).Error; err != nil {
		return err
	}
	for _, ref := range refs {
		if err := reconcilePlatformResourceEvidence(tx, ref.ProviderID, ref.ResourceID, at); err != nil {
			return err
		}
	}
	return nil
}

func reconcilePlatformResourceEvidence(tx *gorm.DB, providerID, resourceID string, at time.Time) error {
	var resource model.PlatformResource
	if err := tx.Where("provider_id = ? AND id = ?", providerID, resourceID).First(&resource).Error; err != nil {
		return err
	}
	if resource.LifecycleState == model.PlatformResourceRetired {
		return refreshPlatformResourceHealth(tx, providerID, resourceID, at)
	}

	var candidates []model.SupplyCandidate
	if err := tx.Model(&model.SupplyCandidate{}).
		Select("supply_candidate.*").
		Joins("JOIN platform_resource_source AS source ON source.provider_id = supply_candidate.provider_id AND source.supply_candidate_id = supply_candidate.id").
		Where("source.provider_id = ? AND source.platform_resource_id = ?", providerID, resourceID).
		Where("supply_candidate.review_state = ? AND supply_candidate.identity_quality = ? AND supply_candidate.conflict_code = ''", model.SupplyCandidateLinked, model.SupplyIdentityStrong).
		Where("julianday(supply_candidate.lease_expires_at) > julianday(?)", at).
		Order("supply_candidate.last_observed_at DESC, supply_candidate.id ASC").Find(&candidates).Error; err != nil {
		return err
	}

	type observedNamespace struct {
		evidence   supplyNamespaceEvidence
		observedAt time.Time
	}
	namespaces := make(map[string]observedNamespace)
	for _, candidate := range candidates {
		var evidence supplyClusterEvidence
		if err := json.Unmarshal([]byte(candidate.ObservationSnapshot), &evidence); err != nil {
			return ErrProviderSupplyInvalidInput
		}
		projection, err := normalizeSupplyCandidateProjection(candidate.TechnicalResourceID, evidence)
		if err != nil || projection.StableKey != resource.StableKey || projection.Quality != model.SupplyIdentityStrong {
			return ErrProviderSupplyConflict
		}
		for _, namespace := range projection.Evidence.Namespaces {
			if _, exists := namespaces[namespace.UID]; !exists {
				namespaces[namespace.UID] = observedNamespace{evidence: namespace, observedAt: candidate.LastObservedAt.UTC()}
			}
		}
		if err := tx.Model(&model.PlatformResourceSource{}).
			Where("provider_id = ? AND platform_resource_id = ? AND supply_candidate_id = ?", providerID, resourceID, candidate.ID).
			Update("last_confirmed_at", candidate.LastObservedAt.UTC()).Error; err != nil {
			return err
		}
	}

	if len(namespaces) > 0 {
		clusterScope, err := findOrCreateClusterScope(tx, &resource)
		if err != nil {
			return err
		}
		uids := make([]string, 0, len(namespaces))
		for uid := range namespaces {
			uids = append(uids, uid)
		}
		sort.Strings(uids)
		for _, uid := range uids {
			observed := namespaces[uid]
			observation, err := upsertNamespaceObservation(tx, &resource, observed.evidence, observed.observedAt)
			if err != nil {
				return err
			}
			if _, err := findOrCreateNamespaceScope(tx, &resource, clusterScope, observation); err != nil {
				return err
			}
		}
	}
	return refreshPlatformResourceHealth(tx, providerID, resourceID, at)
}

func refreshPlatformResourceHealth(tx *gorm.DB, providerID, resourceID string, at time.Time) error {
	var resource model.PlatformResource
	if err := tx.Where("provider_id = ? AND id = ?", providerID, resourceID).First(&resource).Error; err != nil {
		return err
	}
	base := func() *gorm.DB {
		return tx.Table("platform_resource_source AS source").
			Joins("JOIN supply_candidate AS candidate ON candidate.provider_id = source.provider_id AND candidate.id = source.supply_candidate_id").
			Where("source.provider_id = ? AND source.platform_resource_id = ?", providerID, resourceID).
			Where("julianday(candidate.lease_expires_at) > julianday(?)", at)
	}
	var liveCount, validCount int64
	if err := base().Count(&liveCount).Error; err != nil {
		return err
	}
	if err := base().
		Joins("JOIN technical_resource AS technical ON technical.provider_id = candidate.provider_id AND technical.id = candidate.technical_resource_id").
		Where("candidate.review_state = ? AND candidate.identity_quality = ? AND candidate.conflict_code = ''", model.SupplyCandidateLinked, model.SupplyIdentityStrong).
		Where("technical.lifecycle_state = ?", model.TechnicalResourceRegistered).
		Where(`EXISTS (
			SELECT 1 FROM technical_resource_binding AS binding
			WHERE binding.technical_resource_id = technical.id
				AND binding.enabled = true
				AND binding.credential_revision = technical.credential_revision
		)`).Count(&validCount).Error; err != nil {
		return err
	}
	target := model.ResourceHealthOffline
	if validCount > 0 {
		target = model.ResourceHealthOnline
	} else if liveCount > 0 {
		target = model.ResourceHealthDegraded
	}
	if resource.HealthState == target {
		return nil
	}
	return tx.Model(&model.PlatformResource{}).Where("provider_id = ? AND id = ? AND row_version = ?", providerID, resourceID, resource.RowVersion).
		Updates(map[string]any{"health_state": target, "row_version": gorm.Expr("row_version + 1")}).Error
}

func appendProviderSupplyReconciliationEvent(tx *gorm.DB, requestID, providerID, aggregateType, aggregateID string, rowVersion int64, action string) error {
	payload, err := json.Marshal(map[string]any{
		"provider_id": providerID, "aggregate_type": aggregateType, "aggregate_id": aggregateID,
		"row_version": rowVersion, "action": action,
	})
	if err != nil {
		return err
	}
	_, err = NewResourceOutboxService(tx, providerSupplyReconciliationOutboxPolicies).Append(tx, AppendOutboxEventInput{
		Consumer: "provider_supply_projection", EventType: "provider_supply.changed",
		AggregateType: aggregateType, AggregateID: aggregateID, AggregateRevision: rowVersion,
		EventKey: fmt.Sprintf("%s:%s:%d", aggregateType, aggregateID, rowVersion), Payload: payload, RequestID: requestID,
	})
	return err
}

func recordProviderSupplyReconciliationAudit(tx *gorm.DB, requestID, providerID, targetType, targetID string, rowVersion int64, at time.Time) error {
	detail, err := json.Marshal(map[string]any{"row_version": rowVersion, "reconciled_at": at.UTC()})
	if err != nil {
		return err
	}
	return tx.Create(&model.AuditLog{
		UserType: "system", ScopeType: string(model.ManagementScopeProvider), ScopeID: providerID,
		RequiredPermission: "system.resource_reconciliation", RequestID: requestID,
		ActionType: providerSupplyReconciliationAction, TargetType: targetType, TargetID: targetID, TargetName: targetID,
		Detail: string(detail),
	}).Error
}
