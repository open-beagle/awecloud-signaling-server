package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	"gorm.io/gorm"
)

type memoryTenantResourceCandidate struct {
	view       TenantResourceView
	projection workloadProjection
	targets    []workloadProjection
	snapshot   workloadSnapshot
	allocation model.ResourceAllocation
	item       model.ResourceAllocationItem
	lineage    model.ResourceAllocation
	scope      model.ResourceScope
	namespace  model.NamespaceObservation
}

func tenantResourceIdentityKey(resourceType model.TenantResourceType, stableKey, lineageID string) string {
	return string(resourceType) + "\x00" + stableKey + "\x00" + lineageID
}

func memoryCandidateID(tenantID string, resourceType model.TenantResourceType, stableKey, lineageID string) string {
	value := tenantID + "\x00" + tenantResourceIdentityKey(resourceType, stableKey, lineageID)
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(value)).String()
}

func (s *TenantResourceService) memoryCandidates(ctx context.Context, tenantID string, at time.Time) ([]memoryTenantResourceCandidate, error) {
	if s.snapshots == nil {
		return nil, ErrTenantResourceInvalidInput
	}
	var allocations []model.ResourceAllocation
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND state = ? AND valid_from <= ? AND (expires_at IS NULL OR expires_at > ?)",
		tenantID, model.ResourceAllocationActive, at, at).Order("valid_from ASC, id ASC").Find(&allocations).Error; err != nil {
		return nil, err
	}
	type entitlement struct {
		allocation model.ResourceAllocation
		item       model.ResourceAllocationItem
		lineage    model.ResourceAllocation
		scope      model.ResourceScope
		namespace  model.NamespaceObservation
	}
	entitlements := make(map[string][]entitlement)
	for i := range allocations {
		var items []model.ResourceAllocationItem
		if err := s.db.WithContext(ctx).Where("allocation_id = ?", allocations[i].ID).Order("id ASC").Find(&items).Error; err != nil {
			return nil, err
		}
		for j := range items {
			scope, err := loadAllocatableNamespaceScope(s.db.WithContext(ctx), items[j].ScopeID, at)
			if err != nil {
				continue
			}
			if scope.NamespaceObservationID == nil {
				continue
			}
			var namespace model.NamespaceObservation
			if err := s.db.WithContext(ctx).First(&namespace, "id = ?", *scope.NamespaceObservationID).Error; err != nil {
				continue
			}
			lineage, err := resolveAllocationLineageRoot(s.db.WithContext(ctx), &allocations[i], scope.ID)
			if err != nil {
				return nil, err
			}
			entitlements[scope.ID] = append(entitlements[scope.ID], entitlement{
				allocation: allocations[i], item: items[j], lineage: *lineage, scope: *scope, namespace: namespace,
			})
		}
	}

	var persisted []model.TenantResource
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&persisted).Error; err != nil {
		return nil, err
	}
	existing := make(map[string]struct{}, len(persisted))
	for i := range persisted {
		existing[tenantResourceIdentityKey(persisted[i].Type, persisted[i].StableKey, persisted[i].EntitlementLineageID)] = struct{}{}
	}

	candidatesByID := make(map[string]memoryTenantResourceCandidate)
	for _, snapshot := range s.snapshots.current(at) {
		for _, entitlement := range entitlements[snapshot.NamespaceScopeID] {
			for _, projection := range snapshot.Projections {
				if !workloadExposureAllowed(projection.Labels) || projection.IdentityQuality == model.WorkloadIdentityInsufficient {
					continue
				}
				resourceType := model.TenantResourceContainerService
				if snapshot.Kind == model.WorkloadObservationContainer {
					resourceType = model.TenantResourceContainerSSH
				}
				identity := tenantResourceIdentityKey(resourceType, projection.StableKey, entitlement.lineage.ID)
				if _, found := existing[identity]; found {
					continue
				}
				id := memoryCandidateID(tenantID, resourceType, projection.StableKey, entitlement.lineage.ID)
				candidate := memoryTenantResourceCandidate{
					projection: projection, snapshot: snapshot, allocation: entitlement.allocation, item: entitlement.item,
					targets: []workloadProjection{projection},
					lineage: entitlement.lineage, scope: entitlement.scope, namespace: entitlement.namespace,
					view: TenantResourceView{
						ResourceID: id, Type: string(resourceType), DisplayName: workloadDisplayName(snapshot.Kind, projection.Target),
						VisibilityState: model.TenantResourcePending, AvailabilityState: model.TenantResourceUnavailable,
						Revision: snapshot.Sequence, RowVersion: snapshot.Sequence, NamespaceScopeID: snapshot.NamespaceScopeID,
						NamespaceName: snapshot.NamespaceName, TargetRevision: 1, ObservationRevision: snapshot.Sequence,
						Ready: projection.Ready, IdentityQuality: projection.IdentityQuality,
						CreatedAt: snapshot.ReceivedAt, UpdatedAt: snapshot.ReceivedAt,
					},
				}
				if projection.Ready {
					candidate.view.AvailabilityState = model.TenantResourceAvailable
				}
				if err := applyMemoryCandidateTarget(&candidate.view, snapshot.Kind, projection.Target, true); err != nil {
					continue
				}
				prior, found := candidatesByID[id]
				if found && prior.snapshot.SourceTechnicalResourceID == snapshot.SourceTechnicalResourceID &&
					prior.snapshot.NamespaceScopeID == snapshot.NamespaceScopeID && prior.snapshot.Kind == snapshot.Kind &&
					prior.snapshot.Sequence == snapshot.Sequence {
					duplicate := false
					for _, target := range prior.targets {
						if target.Target == projection.Target {
							duplicate = true
							break
						}
					}
					if !duplicate {
						prior.targets = append(prior.targets, projection)
						sort.Slice(prior.targets, func(i, j int) bool { return prior.targets[i].Target < prior.targets[j].Target })
						candidatesByID[id] = prior
					}
					continue
				}
				if !found || snapshot.ReceivedAt.After(prior.snapshot.ReceivedAt) {
					candidatesByID[id] = candidate
				}
			}
		}
	}
	result := make([]memoryTenantResourceCandidate, 0, len(candidatesByID))
	for _, candidate := range candidatesByID {
		result = append(result, candidate)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].view.CreatedAt.Equal(result[j].view.CreatedAt) {
			return result[i].view.ResourceID > result[j].view.ResourceID
		}
		return result[i].view.CreatedAt.After(result[j].view.CreatedAt)
	})
	return result, nil
}

func applyMemoryCandidateTarget(view *TenantResourceView, kind model.WorkloadObservationKind, targetJSON string, includeRuntimeIdentity bool) error {
	if kind == model.WorkloadObservationServicePort {
		var target struct {
			ServiceUID  string `json:"service_uid"`
			ServiceName string `json:"service_name"`
			PortName    string `json:"port_name"`
			Protocol    string `json:"protocol"`
			PortNumber  int    `json:"port_number"`
		}
		if err := decodeWorkloadTarget(targetJSON, &target); err != nil {
			return err
		}
		view.ServiceUID, view.ServiceName, view.PortName = target.ServiceUID, target.ServiceName, target.PortName
		view.PortNumber, view.Protocol = target.PortNumber, target.Protocol
		return nil
	}
	var target struct {
		WorkloadUID   string   `json:"workload_uid"`
		WorkloadKind  string   `json:"workload_kind"`
		WorkloadName  string   `json:"workload_name"`
		PodUID        string   `json:"pod_uid"`
		PodName       string   `json:"pod_name"`
		ContainerName string   `json:"container_name"`
		SSHUsers      []string `json:"ssh_users"`
	}
	if err := decodeWorkloadTarget(targetJSON, &target); err != nil {
		return err
	}
	view.WorkloadUID, view.WorkloadKind, view.WorkloadName = target.WorkloadUID, target.WorkloadKind, target.WorkloadName
	view.PodUID, view.PodName, view.ContainerName, view.SSHUsers = target.PodUID, target.PodName, target.ContainerName, target.SSHUsers
	if !includeRuntimeIdentity {
		view.PodUID, view.PodName = "", ""
	}
	return nil
}

func decodeWorkloadTarget(targetJSON string, target any) error {
	if err := json.Unmarshal([]byte(targetJSON), target); err != nil {
		return ErrTenantResourceTargetNotTrusted
	}
	return nil
}

func filterMemoryCandidates(candidates []memoryTenantResourceCandidate, input TenantResourceListInput) []memoryTenantResourceCandidate {
	result := make([]memoryTenantResourceCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		view := candidate.view
		if input.Type != "" && view.Type != input.Type || input.Availability != "" && string(view.AvailabilityState) != input.Availability {
			continue
		}
		if input.Namespace != "" && view.NamespaceScopeID != input.Namespace && !strings.Contains(view.NamespaceName, input.Namespace) {
			continue
		}
		if input.Query != "" && !strings.Contains(strings.ToLower(view.DisplayName), strings.ToLower(input.Query)) {
			continue
		}
		result = append(result, candidate)
	}
	return result
}

func (s *TenantResourceService) memoryCandidate(ctx context.Context, tenantID, resourceID string, at time.Time) (*memoryTenantResourceCandidate, error) {
	candidates, err := s.memoryCandidates(ctx, tenantID, at)
	if err != nil {
		return nil, err
	}
	for i := range candidates {
		if candidates[i].view.ResourceID == resourceID {
			return &candidates[i], nil
		}
	}
	return nil, ErrTenantResourceNotFound
}

func candidateCursorIndex(candidates []memoryTenantResourceCandidate, cursor string) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	for i := range candidates {
		if candidates[i].view.ResourceID == cursor {
			return i + 1, nil
		}
	}
	return 0, fmt.Errorf("%w: cursor", ErrTenantResourceNotFound)
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
