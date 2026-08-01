package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

const (
	TenantResourceProjectionConsumer = "tenant_resource_projection"
	workloadReconciliationInterval   = 30 * time.Second
	workloadProjectionLeaseDuration  = time.Minute
)

var workloadProjectionOutboxPolicies = map[string]JSONFieldPolicy{
	"workload_observation.changed": NewJSONFieldPolicy("observation_id", "namespace_scope_id", "state", "observed_revision", "row_version"),
}

func projectWorkloadSnapshot(tx *gorm.DB, source *model.TechnicalResource, scope *model.ResourceScope, receipts []model.WorkloadInventoryReceipt, now time.Time) error {
	if tx == nil || source == nil || scope == nil || len(receipts) == 0 || now.IsZero() {
		return ErrWorkloadInventoryInvalidInput
	}
	if scope.NamespaceObservationID == nil {
		return ErrWorkloadScopeNotTrusted
	}
	var namespace model.NamespaceObservation
	if err := tx.Where("id = ? AND provider_id = ? AND cluster_resource_id = ?", *scope.NamespaceObservationID, scope.ProviderID, scope.PlatformResourceID).
		First(&namespace).Error; err != nil {
		return ErrWorkloadScopeNotTrusted
	}
	sort.Slice(receipts, func(i, j int) bool { return receipts[i].BatchIndex < receipts[j].BatchIndex })
	projections := make(map[string]workloadProjection)
	for i := range receipts {
		var batch model.WorkloadInventoryBatch
		if err := tx.Where("receipt_id = ?", receipts[i].ID).First(&batch).Error; err != nil {
			return err
		}
		items, err := decodeWorkloadDocument(receipts[i].Kind, batch.CanonicalPayload)
		if err != nil {
			return err
		}
		for _, item := range items {
			if prior, exists := projections[item.StableKey]; exists {
				if prior.PayloadHash != item.PayloadHash {
					return ErrWorkloadSequenceConflict
				}
				continue
			}
			projections[item.StableKey] = item
		}
	}

	latest := receipts[len(receipts)-1]
	type aggregateChange struct{ material, force bool }
	touched := make(map[string]aggregateChange)
	stableKeys := make([]string, 0, len(projections))
	for stableKey := range projections {
		stableKeys = append(stableKeys, stableKey)
	}
	sort.Strings(stableKeys)
	for _, stableKey := range stableKeys {
		projection := projections[stableKey]
		trustedTarget, err := workloadTargetWithTrustedNamespace(projection.Target, &namespace)
		if err != nil {
			return err
		}
		projection.Target = trustedTarget
		observationID, materialChanged, err := upsertWorkloadProjection(tx, source, scope, latest, projection, now)
		if err != nil {
			return err
		}
		change := touched[observationID]
		change.material = change.material || materialChanged
		touched[observationID] = change
	}

	var previous []model.WorkloadObservationSource
	if err := tx.Table("workload_observation_source AS evidence").Select("evidence.*").
		Joins("JOIN workload_observation AS observation ON observation.id = evidence.workload_observation_id").
		Where("evidence.source_technical_resource_id = ? AND observation.namespace_scope_id = ? AND observation.kind = ?", source.ID, scope.ID, latest.Kind).
		Find(&previous).Error; err != nil {
		return err
	}
	for i := range previous {
		var observation model.WorkloadObservation
		if err := tx.First(&observation, "id = ?", previous[i].WorkloadObservationID).Error; err != nil {
			return err
		}
		if _, present := projections[observation.StableKey]; present || previous[i].State == model.WorkloadObservationSourceStale {
			continue
		}
		updated := tx.Model(&model.WorkloadObservationSource{}).
			Where("id = ? AND row_version = ?", previous[i].ID, previous[i].RowVersion).
			Updates(map[string]any{
				"state": model.WorkloadObservationSourceStale, "source_revision": gorm.Expr("source_revision + 1"),
				"row_version": gorm.Expr("row_version + 1"),
			})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrWorkloadSequenceConflict
		}
		change := touched[observation.ID]
		change.force = true
		touched[observation.ID] = change
	}

	observationIDs := make([]string, 0, len(touched))
	for id := range touched {
		observationIDs = append(observationIDs, id)
	}
	sort.Strings(observationIDs)
	for _, id := range observationIDs {
		change := touched[id]
		changed, err := refreshWorkloadObservationAggregate(tx, id, now, change.material, change.force)
		if err != nil {
			return err
		}
		if changed {
			var observation model.WorkloadObservation
			if err := tx.First(&observation, "id = ?", id).Error; err != nil {
				return err
			}
			if err := appendWorkloadObservationEvent(tx, &observation, now); err != nil {
				return err
			}
		}
	}
	return nil
}

func workloadTargetWithTrustedNamespace(targetJSON string, namespace *model.NamespaceObservation) (string, error) {
	if namespace == nil || strings.TrimSpace(namespace.NamespaceUID) == "" || strings.TrimSpace(namespace.Name) == "" {
		return "", ErrWorkloadScopeNotTrusted
	}
	var target map[string]any
	if err := json.Unmarshal([]byte(targetJSON), &target); err != nil {
		return "", ErrWorkloadInventoryInvalidInput
	}
	target["namespace_uid"] = namespace.NamespaceUID
	target["namespace_name"] = namespace.Name
	canonical, err := json.Marshal(target)
	if err != nil {
		return "", ErrWorkloadInventoryInvalidInput
	}
	return string(canonical), nil
}

func upsertWorkloadProjection(tx *gorm.DB, source *model.TechnicalResource, scope *model.ResourceScope, receipt model.WorkloadInventoryReceipt, projection workloadProjection, now time.Time) (string, bool, error) {
	var observation model.WorkloadObservation
	err := tx.Where("namespace_scope_id = ? AND kind = ? AND stable_key = ?", scope.ID, receipt.Kind, projection.StableKey).First(&observation).Error
	created := false
	if errors.Is(err, gorm.ErrRecordNotFound) {
		observation = model.WorkloadObservation{
			ID: uuid.NewString(), NamespaceScopeID: scope.ID, Kind: receipt.Kind, StableKey: projection.StableKey,
			IdentityQuality: projection.IdentityQuality, State: model.WorkloadObservationObserved, Ready: projection.Ready,
			ObservedRevision: 1, LabelSnapshot: projection.Labels, FirstObservedAt: now, LastObservedAt: now,
			LeaseExpiresAt: receipt.LeaseExpiresAt, RowVersion: 1,
		}
		if err := tx.Create(&observation).Error; err != nil {
			return "", false, err
		}
		created = true
	} else if err != nil {
		return "", false, err
	} else if observation.IdentityQuality != projection.IdentityQuality {
		return "", false, ErrWorkloadSequenceConflict
	}

	var evidence model.WorkloadObservationSource
	err = tx.Where("workload_observation_id = ? AND source_technical_resource_id = ?", observation.ID, source.ID).First(&evidence).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		evidence = model.WorkloadObservationSource{
			ID: uuid.NewString(), WorkloadObservationID: observation.ID, SourceTechnicalResourceID: source.ID,
			SourceEpoch: receipt.SourceEpoch, Sequence: receipt.Sequence, PayloadHash: projection.PayloadHash,
			State: model.WorkloadObservationSourceObserved, Ready: projection.Ready, TargetSnapshot: projection.Target,
			ObservedAt: receipt.ObservedAt, ReceivedAt: now, LeaseExpiresAt: receipt.LeaseExpiresAt,
			SourceRevision: 1, RowVersion: 1,
		}
		if err := tx.Create(&evidence).Error; err != nil {
			return "", false, err
		}
		return observation.ID, !created, nil
	}
	if err != nil {
		return "", false, err
	}
	sourceChanged := evidence.PayloadHash != projection.PayloadHash || evidence.TargetSnapshot != projection.Target ||
		evidence.Ready != projection.Ready || evidence.State != model.WorkloadObservationSourceObserved
	materialChanged := evidence.TargetSnapshot != projection.Target
	updates := map[string]any{
		"source_epoch": receipt.SourceEpoch, "sequence": receipt.Sequence, "payload_hash": projection.PayloadHash,
		"state": model.WorkloadObservationSourceObserved, "ready": projection.Ready, "target_snapshot": projection.Target,
		"observed_at": receipt.ObservedAt, "received_at": now, "lease_expires_at": receipt.LeaseExpiresAt,
		"row_version": gorm.Expr("row_version + 1"),
	}
	if sourceChanged {
		updates["source_revision"] = gorm.Expr("source_revision + 1")
	}
	updated := tx.Model(&model.WorkloadObservationSource{}).Where("id = ? AND row_version = ?", evidence.ID, evidence.RowVersion).Updates(updates)
	if updated.Error != nil {
		return "", false, updated.Error
	}
	if updated.RowsAffected != 1 {
		return "", false, ErrWorkloadSequenceConflict
	}
	return observation.ID, created || materialChanged || observation.LabelSnapshot != projection.Labels, nil
}

func refreshWorkloadObservationAggregate(tx *gorm.DB, observationID string, now time.Time, materialChanged bool, forceChange ...bool) (bool, error) {
	var observation model.WorkloadObservation
	if err := tx.First(&observation, "id = ?", observationID).Error; err != nil {
		return false, err
	}
	live, err := currentWorkloadObservationSources(tx, &observation, now)
	if err != nil {
		return false, err
	}
	state := model.WorkloadObservationStale
	ready := false
	labels := observation.LabelSnapshot
	lastObservedAt, leaseExpiresAt := observation.LastObservedAt, observation.LeaseExpiresAt
	conflict := false
	if len(live) > 0 {
		state = model.WorkloadObservationObserved
		labels = sourceLabelsFromPayloadHash(tx, live[0], labels)
		lastObservedAt, leaseExpiresAt = live[0].ReceivedAt, live[0].LeaseExpiresAt
		baseTarget := live[0].TargetSnapshot
		for i := range live {
			ready = ready || live[i].Ready
			if live[i].ReceivedAt.After(lastObservedAt) {
				lastObservedAt = live[i].ReceivedAt
			}
			if live[i].LeaseExpiresAt.After(leaseExpiresAt) {
				leaseExpiresAt = live[i].LeaseExpiresAt
			}
			if live[i].TargetSnapshot != baseTarget {
				conflict = true
			}
		}
		if conflict {
			state = model.WorkloadObservationConflict
		} else {
			eligible, err := workloadObservationHasActiveEntitlement(tx, &observation, labels, now)
			if err != nil {
				return false, err
			}
			if eligible && observation.IdentityQuality != model.WorkloadIdentityInsufficient {
				state = model.WorkloadObservationEligible
			}
		}
	}
	if labels != observation.LabelSnapshot {
		materialChanged = true
	}
	forced := len(forceChange) > 0 && forceChange[0]
	changed := state != observation.State || ready != observation.Ready || labels != observation.LabelSnapshot ||
		!lastObservedAt.Equal(observation.LastObservedAt) || !leaseExpiresAt.Equal(observation.LeaseExpiresAt) || materialChanged
	changed = changed || forced
	if !changed {
		return false, nil
	}
	updates := map[string]any{
		"state": state, "ready": ready, "label_snapshot": labels, "last_observed_at": lastObservedAt,
		"lease_expires_at": leaseExpiresAt, "row_version": gorm.Expr("row_version + 1"),
	}
	if materialChanged {
		updates["observed_revision"] = gorm.Expr("observed_revision + 1")
	}
	result := tx.Model(&model.WorkloadObservation{}).Where("id = ? AND row_version = ?", observation.ID, observation.RowVersion).Updates(updates)
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected != 1 {
		return false, ErrWorkloadSequenceConflict
	}
	return true, nil
}

func currentWorkloadObservationSources(tx *gorm.DB, observation *model.WorkloadObservation, now time.Time) ([]model.WorkloadObservationSource, error) {
	if tx == nil || observation == nil || now.IsZero() {
		return nil, ErrWorkloadInventoryInvalidInput
	}
	var scope model.ResourceScope
	if err := tx.First(&scope, "id = ?", observation.NamespaceScopeID).Error; err != nil {
		return nil, err
	}
	var sources []model.WorkloadObservationSource
	if err := tx.Where("workload_observation_id = ? AND state = ? AND lease_expires_at > ?", observation.ID, model.WorkloadObservationSourceObserved, now).
		Order("ready DESC, received_at DESC, source_technical_resource_id ASC").Find(&sources).Error; err != nil {
		return nil, err
	}
	current := make([]model.WorkloadObservationSource, 0, len(sources))
	for i := range sources {
		var technical model.TechnicalResource
		if err := tx.First(&technical, "id = ?", sources[i].SourceTechnicalResourceID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return nil, err
		}
		valid, err := workloadTechnicalResourceBindingCurrent(tx, &technical)
		if err != nil {
			return nil, err
		}
		if !valid {
			continue
		}
		if technical.ParentID != nil {
			var parent model.TechnicalResource
			if err := tx.First(&parent, "id = ?", *technical.ParentID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					continue
				}
				return nil, err
			}
			valid, err = workloadTechnicalResourceBindingCurrent(tx, &parent)
			if err != nil {
				return nil, err
			}
			if !valid {
				continue
			}
		}
		capable, err := workloadSourceHasCapability(tx, &technical, scope.PlatformResourceID, now)
		if err != nil {
			return nil, err
		}
		if capable {
			current = append(current, sources[i])
		}
	}
	return current, nil
}

func workloadTechnicalResourceBindingCurrent(tx *gorm.DB, technical *model.TechnicalResource) (bool, error) {
	if technical == nil || technical.LifecycleState != model.TechnicalResourceRegistered ||
		(technical.Type != model.TechnicalResourceAgent && technical.Type != model.TechnicalResourceEndpoint) {
		return false, nil
	}
	binding, err := loadActiveTechnicalResourceBinding(tx, technical.ID)
	if errors.Is(err, ErrTechnicalResourceUnbound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return binding.CredentialRevision == technical.CredentialRevision, nil
}

// Source payload hashes are deliberately not reversible. Labels are read from
// the latest committed batch only while processing that batch; existing labels
// remain authoritative for lease-only aggregation.
func sourceLabelsFromPayloadHash(_ *gorm.DB, source model.WorkloadObservationSource, fallback string) string {
	var target struct {
		Labels map[string]string `json:"labels_allowlist"`
	}
	if json.Unmarshal([]byte(source.TargetSnapshot), &target) != nil || target.Labels == nil {
		return fallback
	}
	canonical, err := json.Marshal(target.Labels)
	if err != nil {
		return fallback
	}
	return string(canonical)
}

func workloadObservationHasActiveEntitlement(tx *gorm.DB, observation *model.WorkloadObservation, labels string, now time.Time) (bool, error) {
	if observation == nil || !workloadExposureAllowed(labels) {
		return false, nil
	}
	var count int64
	err := tx.Table("resource_allocation_item AS item").
		Joins("JOIN resource_allocation AS allocation ON allocation.id = item.allocation_id").
		Joins("JOIN tenant ON tenant.id = allocation.tenant_id").
		Joins("JOIN resource_scope AS scope ON scope.id = item.scope_id").
		Joins("JOIN platform_resource AS resource ON resource.id = scope.platform_resource_id AND resource.provider_id = scope.provider_id").
		Joins("JOIN resource_provider AS provider ON provider.id = resource.provider_id").
		Joins("JOIN namespace_observation AS namespace ON namespace.id = scope.namespace_observation_id").
		Where("item.scope_id = ? AND allocation.state = ? AND allocation.valid_from <= ? AND (allocation.expires_at IS NULL OR allocation.expires_at > ?)", observation.NamespaceScopeID, model.ResourceAllocationActive, now, now).
		Where("tenant.status = ? AND provider.status = ? AND scope.lifecycle_state = ? AND resource.lifecycle_state = ?", model.TenantStatusActive, model.ProviderStatusActive, model.ResourceScopeAllocatable, model.PlatformResourceActive).
		Where("namespace.state = ? AND namespace.lease_expires_at > ?", model.NamespaceObservationObserved, now).
		Where(`EXISTS (
			SELECT 1 FROM workload_observation_source evidence
			JOIN technical_resource technical ON technical.id = evidence.source_technical_resource_id
			JOIN technical_resource_binding binding ON binding.technical_resource_id = technical.id
			WHERE evidence.workload_observation_id = ? AND evidence.state = ? AND evidence.lease_expires_at > ?
				AND technical.lifecycle_state = ? AND binding.enabled = true
				AND binding.credential_revision = technical.credential_revision
		)`, observation.ID, model.WorkloadObservationSourceObserved, now, model.TechnicalResourceRegistered).
		Count(&count).Error
	return count > 0, err
}

func workloadExposureAllowed(labelsJSON string) bool {
	var labels map[string]string
	if json.Unmarshal([]byte(labelsJSON), &labels) != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(labels[workloadExposeLabel]), "true")
}

func appendWorkloadObservationEvent(tx *gorm.DB, observation *model.WorkloadObservation, now time.Time) error {
	payload, err := json.Marshal(map[string]any{
		"observation_id": observation.ID, "namespace_scope_id": observation.NamespaceScopeID,
		"state": observation.State, "observed_revision": observation.ObservedRevision, "row_version": observation.RowVersion,
	})
	if err != nil {
		return err
	}
	_, err = NewResourceOutboxService(tx, workloadProjectionOutboxPolicies).Append(tx, AppendOutboxEventInput{
		Consumer: TenantResourceProjectionConsumer, EventType: "workload_observation.changed", AggregateType: "workload_observation",
		AggregateID: observation.ID, AggregateRevision: observation.RowVersion,
		EventKey: fmt.Sprintf("workload_observation:%s:%d", observation.ID, observation.RowVersion), Payload: payload,
		RequestID: uuid.NewString(), AvailableAt: now,
	})
	return err
}

type WorkloadReconciliationResult struct {
	StaleSources        int64
	UpdatedObservations int64
	PurgedPayloads      int64
}

type WorkloadReconciliationService struct {
	db         *gorm.DB
	now        func() time.Time
	projection *TenantResourceProjectionService
}

func NewWorkloadReconciliationService(database *gorm.DB) *WorkloadReconciliationService {
	return &WorkloadReconciliationService{db: database, now: time.Now, projection: NewTenantResourceProjectionService(database)}
}

func (s *WorkloadReconciliationService) Reconcile(ctx context.Context, at time.Time) (*WorkloadReconciliationResult, error) {
	if s == nil || s.db == nil || at.IsZero() {
		return nil, ErrWorkloadInventoryInvalidInput
	}
	at = at.UTC()
	result := &WorkloadReconciliationResult{}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var expired []model.WorkloadObservationSource
		if err := tx.Where("state = ? AND lease_expires_at <= ?", model.WorkloadObservationSourceObserved, at).Find(&expired).Error; err != nil {
			return err
		}
		touched := make(map[string]struct{})
		for i := range expired {
			updated := tx.Model(&model.WorkloadObservationSource{}).Where("id = ? AND row_version = ?", expired[i].ID, expired[i].RowVersion).
				Updates(map[string]any{"state": model.WorkloadObservationSourceStale, "source_revision": gorm.Expr("source_revision + 1"), "row_version": gorm.Expr("row_version + 1")})
			if updated.Error != nil {
				return updated.Error
			}
			result.StaleSources += updated.RowsAffected
			touched[expired[i].WorkloadObservationID] = struct{}{}
		}
		for observationID := range touched {
			changed, err := refreshWorkloadObservationAggregate(tx, observationID, at, false, true)
			if err != nil {
				return err
			}
			if changed {
				var observation model.WorkloadObservation
				if err := tx.First(&observation, "id = ?", observationID).Error; err != nil {
					return err
				}
				if err := appendWorkloadObservationEvent(tx, &observation, at); err != nil {
					return err
				}
				result.UpdatedObservations++
			}
		}
		var allObservationIDs []string
		if err := tx.Model(&model.WorkloadObservation{}).Pluck("id", &allObservationIDs).Error; err != nil {
			return err
		}
		for _, observationID := range allObservationIDs {
			if _, already := touched[observationID]; already {
				continue
			}
			changed, err := refreshWorkloadObservationAggregate(tx, observationID, at, false)
			if err != nil {
				return err
			}
			if changed {
				var observation model.WorkloadObservation
				if err := tx.First(&observation, "id = ?", observationID).Error; err != nil {
					return err
				}
				if err := appendWorkloadObservationEvent(tx, &observation, at); err != nil {
					return err
				}
				result.UpdatedObservations++
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	purged, err := NewWorkloadInventoryService(s.db).PurgeExpiredPayloads(ctx, at)
	if err != nil {
		return nil, err
	}
	result.PurgedPayloads = purged
	return result, nil
}

func (s *WorkloadReconciliationService) StartPeriodicMaintenance(ctx context.Context) {
	ticker := time.NewTicker(workloadReconciliationInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := s.Reconcile(ctx, s.now().UTC()); err != nil {
				logger.Errorf("Workload Inventory lease reconciliation failed: %v", err)
			}
			for i := 0; i < 100; i++ {
				processed, err := s.projection.ProcessNext(ctx)
				if err != nil {
					logger.Errorf("Tenant Resource projection failed: %v", err)
					break
				}
				if !processed {
					break
				}
			}
		}
	}
}
