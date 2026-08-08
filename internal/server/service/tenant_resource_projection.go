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

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

type TenantResourceProjectionService struct {
	db     *gorm.DB
	now    func() time.Time
	outbox *ResourceOutboxService
}

func NewTenantResourceProjectionService(database *gorm.DB) *TenantResourceProjectionService {
	policies := PlatformAllocationOutboxPolicies()
	for eventType, policy := range workloadProjectionOutboxPolicies {
		policies[eventType] = policy
	}
	return &TenantResourceProjectionService{
		db: database, now: time.Now, outbox: NewResourceOutboxService(database, policies),
	}
}

func (s *TenantResourceProjectionService) ProcessNext(ctx context.Context) (bool, error) {
	if s == nil || s.db == nil || s.outbox == nil {
		return false, ErrWorkloadInventoryInvalidInput
	}
	event, err := s.outbox.Claim(ctx, TenantResourceProjectionConsumer, "tenant-resource-projection", workloadProjectionLeaseDuration)
	if errors.Is(err, ErrOutboxNoEvent) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	executed, err := s.outbox.Complete(ctx, event.ID, event.LeaseToken, func(tx *gorm.DB, aggregate OutboxAggregateRef) error {
		now := s.now().UTC()
		switch aggregate.AggregateType {
		case "workload_observation":
			return reconcileTenantResourcesForObservation(tx, aggregate.AggregateID, now, aggregate.EventID)
		case "resource_allocation":
			return reconcileTenantResourcesForAllocation(tx, aggregate.AggregateID, now, aggregate.EventID)
		default:
			return ErrWorkloadInventoryInvalidInput
		}
	})
	if err != nil {
		_ = s.outbox.Fail(ctx, event.ID, event.LeaseToken, OutboxFailure{
			Retryable: true, Code: "TENANT_RESOURCE_PROJECTION_FAILED", Summary: err.Error(), BaseDelay: time.Second, MaxDelay: time.Minute,
		})
		return false, err
	}
	return executed, nil
}

func (s *TenantResourceProjectionService) Drain(ctx context.Context, limit int) (int, error) {
	if limit <= 0 || limit > 10000 {
		return 0, ErrWorkloadInventoryInvalidInput
	}
	processed := 0
	for processed < limit {
		ok, err := s.ProcessNext(ctx)
		if err != nil {
			return processed, err
		}
		if !ok {
			return processed, nil
		}
		processed++
	}
	return processed, nil
}

func reconcileTenantResourcesForAllocation(tx *gorm.DB, allocationID string, now time.Time, requestID string) error {
	var items []model.ResourceAllocationItem
	if err := tx.Where("allocation_id = ?", allocationID).Find(&items).Error; err != nil {
		return err
	}
	for i := range items {
		var observations []model.WorkloadObservation
		if err := tx.Where("namespace_scope_id = ?", items[i].ScopeID).Find(&observations).Error; err != nil {
			return err
		}
		for j := range observations {
			if _, err := refreshWorkloadObservationAggregate(tx, observations[j].ID, now, false); err != nil {
				return err
			}
			if err := reconcileTenantResourcesForObservation(tx, observations[j].ID, now, requestID); err != nil {
				return err
			}
		}
	}
	return nil
}

func reconcileTenantResourcesForObservation(tx *gorm.DB, observationID string, now time.Time, requestID string) error {
	var observation model.WorkloadObservation
	if err := tx.First(&observation, "id = ?", observationID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	var allocations []model.ResourceAllocation
	if observation.State == model.WorkloadObservationEligible {
		if err := tx.Table("resource_allocation AS allocation").Select("allocation.*").
			Joins("JOIN resource_allocation_item AS item ON item.allocation_id = allocation.id").
			Joins("JOIN tenant ON tenant.id = allocation.tenant_id").
			Where("item.scope_id = ? AND allocation.state = ? AND allocation.valid_from <= ? AND (allocation.expires_at IS NULL OR allocation.expires_at > ?)", observation.NamespaceScopeID, model.ResourceAllocationActive, now, now).
			Where("tenant.status = ?", model.TenantStatusActive).Order("allocation.valid_from ASC, allocation.id ASC").Find(&allocations).Error; err != nil {
			return err
		}
	}
	activeItems := make(map[string]struct{})
	for i := range allocations {
		var item model.ResourceAllocationItem
		if err := tx.Where("allocation_id = ? AND scope_id = ?", allocations[i].ID, observation.NamespaceScopeID).First(&item).Error; err != nil {
			return err
		}
		activeItems[item.ID] = struct{}{}
		if err := projectTenantResourceForAllocation(tx, &observation, &allocations[i], &item, now, requestID); err != nil {
			return err
		}
	}
	return disableInactiveTenantResourceSources(tx, &observation, activeItems, now, requestID)
}

func projectTenantResourceForAllocation(tx *gorm.DB, observation *model.WorkloadObservation, allocation *model.ResourceAllocation, item *model.ResourceAllocationItem, now time.Time, requestID string) error {
	root, err := resolveAllocationLineageRoot(tx, allocation, observation.NamespaceScopeID)
	if err != nil {
		return err
	}
	resourceType := model.TenantResourceContainerService
	if observation.Kind == model.WorkloadObservationContainer {
		resourceType = model.TenantResourceContainerSSH
	}
	selected, err := selectWorkloadObservationSource(tx, observation.ID, now)
	if err != nil {
		return err
	}
	if selected == nil {
		return nil
	}
	if resourceType == model.TenantResourceContainerSSH {
		if _, err := containerSSHUsersFromTargetSnapshot(selected.TargetSnapshot); err != nil {
			return ErrWorkloadInventoryInvalidInput
		}
	}

	var resource model.TenantResource
	err = tx.Where("tenant_id = ? AND type = ? AND stable_key = ? AND entitlement_lineage_id = ?", allocation.TenantID, resourceType, observation.StableKey, root.ID).First(&resource).Error
	created := false
	if errors.Is(err, gorm.ErrRecordNotFound) {
		resource = model.TenantResource{
			ID: uuid.NewString(), TenantID: allocation.TenantID, Type: resourceType, StableKey: observation.StableKey,
			EntitlementLineageID: root.ID, DisplayName: workloadDisplayName(observation.Kind, selected.TargetSnapshot),
			VisibilityState: model.TenantResourcePending, AvailabilityState: model.TenantResourceUnknown,
			Revision: 1, RowVersion: 1,
		}
		if err := tx.Create(&resource).Error; err != nil {
			return err
		}
		created = true
	} else if err != nil {
		return err
	} else if resource.VisibilityState == model.TenantResourceRetired {
		return nil
	}
	var resourceSource model.TenantResourceSource
	err = tx.Where("tenant_resource_id = ? AND allocation_item_id = ? AND workload_observation_id = ?", resource.ID, item.ID, observation.ID).First(&resourceSource).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		resourceSource = model.TenantResourceSource{
			ID: uuid.NewString(), TenantResourceID: resource.ID, AllocationItemID: item.ID, WorkloadObservationID: observation.ID,
			Enabled: true, EnabledAt: now, SourceRevision: 1, RowVersion: 1,
		}
		if err := tx.Create(&resourceSource).Error; err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else {
		if !resourceSource.Enabled {
			result := tx.Model(&model.TenantResourceSource{}).Where("id = ? AND row_version = ?", resourceSource.ID, resourceSource.RowVersion).
				Updates(map[string]any{"enabled": true, "enabled_at": now, "disabled_at": nil, "disabled_reason": "", "row_version": gorm.Expr("row_version + 1")})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrWorkloadSequenceConflict
			}
			resourceSource.Enabled, resourceSource.EnabledAt, resourceSource.DisabledAt, resourceSource.DisabledReason = true, now, nil, ""
			resourceSource.RowVersion++
		}
	}
	if err := appendTenantResourceTarget(tx, observation, &resourceSource, selected, now); err != nil {
		return err
	}
	changed, err := refreshTenantResourceAvailability(tx, &resource, now)
	if err != nil {
		return err
	}
	if created || changed {
		return recordTenantResourceProjectionAudit(tx, requestID, &resource, observation, created, now)
	}
	return nil
}

func resolveAllocationLineageRoot(tx *gorm.DB, allocation *model.ResourceAllocation, scopeID string) (*model.ResourceAllocation, error) {
	if allocation == nil {
		return nil, ErrWorkloadInventoryInvalidInput
	}
	current := *allocation
	seen := make(map[string]struct{})
	for current.RenewedFromID != nil {
		if _, exists := seen[current.ID]; exists {
			return nil, ErrWorkloadSequenceConflict
		}
		seen[current.ID] = struct{}{}
		var parent model.ResourceAllocation
		if err := tx.First(&parent, "id = ? AND tenant_id = ?", *current.RenewedFromID, allocation.TenantID).Error; err != nil {
			return nil, ErrWorkloadSequenceConflict
		}
		var itemCount int64
		if err := tx.Model(&model.ResourceAllocationItem{}).Where("allocation_id = ? AND scope_id = ?", parent.ID, scopeID).Count(&itemCount).Error; err != nil {
			return nil, err
		}
		if itemCount != 1 {
			return nil, ErrWorkloadSequenceConflict
		}
		current = parent
	}
	return &current, nil
}

func selectWorkloadObservationSource(tx *gorm.DB, observationID string, now time.Time) (*model.WorkloadObservationSource, error) {
	var observation model.WorkloadObservation
	if err := tx.First(&observation, "id = ?", observationID).Error; err != nil {
		return nil, err
	}
	sources, err := currentWorkloadObservationSources(tx, &observation, now)
	if err != nil {
		return nil, err
	}
	if len(sources) == 0 {
		return nil, nil
	}
	return &sources[0], nil
}

func appendTenantResourceTarget(tx *gorm.DB, observation *model.WorkloadObservation, resourceSource *model.TenantResourceSource, evidence *model.WorkloadObservationSource, now time.Time) error {
	var latest model.TenantResourceTargetRevision
	err := tx.Where("tenant_resource_source_id = ?", resourceSource.ID).Order("revision DESC").First(&latest).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if !evidence.Ready {
		return nil
	}
	unchanged := err == nil && latest.TargetSnapshot == evidence.TargetSnapshot && latest.SourceTechnicalResourceID == evidence.SourceTechnicalResourceID &&
		latest.AccessTechnicalResourceID == evidence.SourceTechnicalResourceID && latest.Ready &&
		latest.ObservationRevision == observation.ObservedRevision && latest.SourceRevision == resourceSource.SourceRevision && latest.SupersededAt == nil
	if unchanged {
		return nil
	}
	nextRevision := int64(1)
	if err == nil {
		nextRevision = latest.Revision + 1
		if latest.SupersededAt == nil {
			if err := tx.Model(&model.TenantResourceTargetRevision{}).Where("id = ? AND superseded_at IS NULL", latest.ID).Update("superseded_at", now).Error; err != nil {
				return err
			}
		}
		advanced := tx.Model(&model.TenantResourceSource{}).Where("id = ? AND row_version = ?", resourceSource.ID, resourceSource.RowVersion).
			Updates(map[string]any{"source_revision": gorm.Expr("source_revision + 1"), "row_version": gorm.Expr("row_version + 1")})
		if advanced.Error != nil {
			return advanced.Error
		}
		if advanced.RowsAffected != 1 {
			return ErrWorkloadSequenceConflict
		}
		resourceSource.SourceRevision++
		resourceSource.RowVersion++
	}
	target := model.TenantResourceTargetRevision{
		ID: uuid.NewString(), TenantResourceSourceID: resourceSource.ID, Revision: nextRevision, TargetType: observation.Kind,
		TargetSnapshot: evidence.TargetSnapshot, SourceTechnicalResourceID: evidence.SourceTechnicalResourceID,
		AccessTechnicalResourceID: evidence.SourceTechnicalResourceID, Ready: true, ObservedAt: evidence.ObservedAt,
		ObservationRevision: observation.ObservedRevision, SourceRevision: resourceSource.SourceRevision, CreatedAt: now,
	}
	return tx.Create(&target).Error
}

func disableInactiveTenantResourceSources(tx *gorm.DB, observation *model.WorkloadObservation, activeItems map[string]struct{}, now time.Time, requestID string) error {
	var sources []model.TenantResourceSource
	if err := tx.Where("workload_observation_id = ? AND enabled = ?", observation.ID, true).Find(&sources).Error; err != nil {
		return err
	}
	resourceIDs := make(map[string]struct{})
	for i := range sources {
		if _, active := activeItems[sources[i].AllocationItemID]; active && observation.State == model.WorkloadObservationEligible {
			continue
		}
		reason := "WORKLOAD_NOT_ELIGIBLE"
		if observation.State == model.WorkloadObservationEligible {
			reason = "ALLOCATION_NOT_ACTIVE"
		}
		result := tx.Model(&model.TenantResourceSource{}).Where("id = ? AND row_version = ?", sources[i].ID, sources[i].RowVersion).
			Updates(map[string]any{
				"enabled": false, "disabled_at": now, "disabled_reason": reason,
				"row_version": gorm.Expr("row_version + 1"),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrWorkloadSequenceConflict
		}
		resourceIDs[sources[i].TenantResourceID] = struct{}{}
	}
	ids := make([]string, 0, len(resourceIDs))
	for id := range resourceIDs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		var resource model.TenantResource
		if err := tx.First(&resource, "id = ?", id).Error; err != nil {
			return err
		}
		changed, err := refreshTenantResourceAvailability(tx, &resource, now)
		if err != nil {
			return err
		}
		if changed {
			if err := recordTenantResourceProjectionAudit(tx, requestID, &resource, observation, false, now); err != nil {
				return err
			}
		}
	}
	return nil
}

func refreshTenantResourceAvailability(tx *gorm.DB, resource *model.TenantResource, now time.Time) (bool, error) {
	var readyCount int64
	base := tx.Table("tenant_resource_source AS source").
		Joins("JOIN resource_allocation_item AS item ON item.id = source.allocation_item_id").
		Joins("JOIN resource_allocation AS allocation ON allocation.id = item.allocation_id").
		Joins("JOIN workload_observation AS observation ON observation.id = source.workload_observation_id").
		Where("source.tenant_resource_id = ? AND source.enabled = ?", resource.ID, true).
		Where("allocation.state = ? AND allocation.valid_from <= ? AND (allocation.expires_at IS NULL OR allocation.expires_at > ?)", model.ResourceAllocationActive, now, now).
		Where("observation.state = ?", model.WorkloadObservationEligible)
	if err := base.Where("observation.ready = ?", true).Where(`EXISTS (
		SELECT 1 FROM resource_target_revision_v2 target
		WHERE target.tenant_resource_source_id = source.id AND target.superseded_at IS NULL AND target.ready = true
	)`).Count(&readyCount).Error; err != nil {
		return false, err
	}
	target := model.TenantResourceUnavailable
	if readyCount > 0 {
		target = model.TenantResourceAvailable
		var observations []model.WorkloadObservation
		if err := tx.Table("workload_observation AS observation").Select("DISTINCT observation.*").
			Joins("JOIN tenant_resource_source AS source ON source.workload_observation_id = observation.id").
			Joins("JOIN resource_allocation_item AS item ON item.id = source.allocation_item_id").
			Joins("JOIN resource_allocation AS allocation ON allocation.id = item.allocation_id").
			Where("source.tenant_resource_id = ? AND source.enabled = ?", resource.ID, true).
			Where("allocation.state = ? AND allocation.valid_from <= ? AND (allocation.expires_at IS NULL OR allocation.expires_at > ?)", model.ResourceAllocationActive, now, now).
			Where("observation.state = ?", model.WorkloadObservationEligible).
			Find(&observations).Error; err != nil {
			return false, err
		}
		for i := range observations {
			var evidenceCount int64
			if err := tx.Model(&model.WorkloadObservationSource{}).Where("workload_observation_id = ?", observations[i].ID).Count(&evidenceCount).Error; err != nil {
				return false, err
			}
			current, err := currentWorkloadObservationSources(tx, &observations[i], now)
			if err != nil {
				return false, err
			}
			degraded := int64(len(current)) < evidenceCount
			for j := range current {
				degraded = degraded || !current[j].Ready
			}
			if degraded {
				target = model.TenantResourceDegraded
				break
			}
		}
	}
	if resource.AvailabilityState == target {
		return false, nil
	}
	result := tx.Model(&model.TenantResource{}).Where("id = ? AND row_version = ?", resource.ID, resource.RowVersion).
		Updates(map[string]any{"availability_state": target, "revision": gorm.Expr("revision + 1"), "row_version": gorm.Expr("row_version + 1")})
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected != 1 {
		return false, ErrWorkloadSequenceConflict
	}
	resource.AvailabilityState = target
	resource.Revision++
	resource.RowVersion++
	return true, nil
}

func workloadDisplayName(kind model.WorkloadObservationKind, targetJSON string) string {
	var target map[string]any
	if json.Unmarshal([]byte(targetJSON), &target) != nil {
		return "Workload"
	}
	if kind == model.WorkloadObservationServicePort {
		return fmt.Sprintf("%v:%v", target["service_name"], target["port_number"])
	}
	workloadName := strings.TrimSpace(fmt.Sprint(target["workload_name"]))
	if workloadName == "" || workloadName == "<nil>" {
		workloadName = strings.TrimSpace(fmt.Sprint(target["pod_name"]))
	}
	return workloadName + "/" + strings.TrimSpace(fmt.Sprint(target["container_name"]))
}

func recordTenantResourceProjectionAudit(tx *gorm.DB, requestID string, resource *model.TenantResource, observation *model.WorkloadObservation, created bool, now time.Time) error {
	if strings.TrimSpace(requestID) == "" {
		requestID = uuid.NewString()
	}
	action := "reconcile_tenant_resource"
	if created {
		action = "create_pending_tenant_resource"
	}
	detail, err := json.Marshal(map[string]any{
		"tenant_id": resource.TenantID, "resource_revision": resource.Revision,
		"observation_id": observation.ID, "observation_revision": observation.ObservedRevision,
		"reconciled_at": now,
	})
	if err != nil {
		return err
	}
	return tx.Create(&model.AuditLog{
		UserType: "system", ScopeType: string(model.ManagementScopeTenant), ScopeID: resource.TenantID,
		RequiredPermission: "system.tenant_resource_projection", RequestID: requestID,
		ActionType: action, TargetType: "tenant_resource", TargetID: resource.ID, TargetName: resource.DisplayName,
		Detail: string(detail),
	}).Error
}
