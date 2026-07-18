package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

// ReconcileOutcome describes the result of matching one runtime candidate.
type ReconcileOutcome string

const (
	ReconcilePublished ReconcileOutcome = "published"
	ReconcileRefreshed ReconcileOutcome = "refreshed"
	ReconcilePending   ReconcileOutcome = "pending_claim"
	ReconcileConflict  ReconcileOutcome = "conflict"
	ReconcileStale     ReconcileOutcome = "stale"
)

// ReconcileResult keeps the state transition visible to both HTTP and
// heartbeat callers without exposing a Kubernetes credential or address.
type ReconcileResult struct {
	Candidate model.DiscoveryCandidate
	Resource  *model.Resource
	Target    *model.ResourceTarget
	Outcome   ReconcileOutcome
	Reason    string
}

// ResourceReconciliationService matches untrusted Agent observations to
// trusted Workspace bindings. It is deliberately independent of HTTP so the
// same rules run after a heartbeat and from the admin retry endpoint.
type ResourceReconciliationService struct {
	db *gorm.DB
}

func NewResourceReconciliationService(database *gorm.DB) *ResourceReconciliationService {
	return &ResourceReconciliationService{db: database}
}

// ReconcileWorkspace retries all non-rejected observations for one trusted
// Workspace. It is called after a Workspace binding changes so an Agent does
// not need to report the same Pod again before it becomes publishable.
func (s *ResourceReconciliationService) ReconcileWorkspace(ctx context.Context, providerID, workspaceID string) (int, error) {
	var candidates []model.DiscoveryCandidate
	if err := s.db.WithContext(ctx).
		Where("provider_hint = ? AND workspace_hint = ? AND status <> ?", providerID, workspaceID, model.DiscoveryCandidateRejected).
		Order("observed_at ASC").Find(&candidates).Error; err != nil {
		return 0, fmt.Errorf("查询 Workspace Candidate 失败: %w", err)
	}

	return s.reconcileCandidates(ctx, candidates)
}

// ReconcileProviderTenant retries all Workspaces under one provider customer.
// This matters when a ProviderTenantBinding is restored after a temporary
// integration or customer mapping outage.
func (s *ResourceReconciliationService) ReconcileProviderTenant(ctx context.Context, providerID, externalTenantID string) (int, error) {
	var bindings []model.WorkspaceBinding
	if err := s.db.WithContext(ctx).
		Where("provider_id = ? AND external_tenant_id = ?", providerID, externalTenantID).
		Find(&bindings).Error; err != nil {
		return 0, fmt.Errorf("查询 Provider Workspace Binding 失败: %w", err)
	}

	count := 0
	for _, binding := range bindings {
		matched, err := s.ReconcileWorkspace(ctx, binding.ProviderID, binding.ExternalWorkspaceID)
		count += matched
		if err != nil {
			return count, err
		}
	}
	return count, nil
}

func (s *ResourceReconciliationService) reconcileCandidates(ctx context.Context, candidates []model.DiscoveryCandidate) (int, error) {
	count := 0
	for _, candidate := range candidates {
		if _, err := s.ReconcileCandidate(ctx, candidate.ID); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

// ExpireCandidates applies the lease boundary even when an Agent stops
// sending heartbeats. Explicitly rejected candidates remain rejected so an
// operator decision is not overwritten by maintenance.
func (s *ResourceReconciliationService) ExpireCandidates(ctx context.Context, now time.Time) (int64, error) {
	var candidates []model.DiscoveryCandidate
	if err := s.db.WithContext(ctx).
		Where("lease_expires_at IS NOT NULL AND lease_expires_at < ? AND status IN ?", now, []model.DiscoveryCandidateStatus{
			model.DiscoveryCandidateObserved, model.DiscoveryCandidatePendingClaim, model.DiscoveryCandidatePublished,
		}).Find(&candidates).Error; err != nil {
		return 0, fmt.Errorf("查询过期 Candidate 失败: %w", err)
	}

	var count int64
	for _, candidate := range candidates {
		wasPublished := candidate.Status == model.DiscoveryCandidatePublished
		resourceID := candidate.ResourceID
		err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(&candidate).Updates(map[string]interface{}{
				"status":          model.DiscoveryCandidateStale,
				"conflict_reason": "Agent 观测租约已过期",
			}).Error; err != nil {
				return err
			}
			if wasPublished && resourceID != "" {
				if err := tx.Model(&model.Resource{}).
					Where("id = ? AND state IN ?", resourceID, []model.ResourceState{model.ResourceStateAvailable, model.ResourceStateDegraded}).
					Update("state", model.ResourceStatePending).Error; err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return count, fmt.Errorf("标记过期 Candidate 失败: candidate_id=%s: %w", candidate.ID, err)
		}
		count++
	}
	return count, nil
}

// StartPeriodicMaintenance starts the lease maintenance loop owned by the
// Server lifecycle. Binding changes and heartbeats handle reconciliation;
// this loop only handles observations that stop arriving altogether.
func (s *ResourceReconciliationService) StartPeriodicMaintenance(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	logger.Info("启动 ContainerSSH Candidate 租约维护任务，间隔 1 分钟")
	for {
		select {
		case <-ctx.Done():
			logger.Info("ContainerSSH Candidate 租约维护任务已停止")
			return
		case now := <-ticker.C:
			if count, err := s.ExpireCandidates(ctx, now); err != nil {
				logger.Warnf("ContainerSSH Candidate 租约维护失败: %v", err)
			} else if count > 0 {
				logger.Infof("ContainerSSH Candidate 租约过期: count=%d", count)
			}
		}
	}
}

// ReconcileCandidate is idempotent for an unchanged runtime target. A new
// ResourceTarget revision is created only when the target identity or ready
// state changes.
func (s *ResourceReconciliationService) ReconcileCandidate(ctx context.Context, candidateID string) (*ReconcileResult, error) {
	var candidate model.DiscoveryCandidate
	if err := s.db.WithContext(ctx).First(&candidate, "id = ?", candidateID).Error; err != nil {
		return nil, err
	}

	result := &ReconcileResult{Candidate: candidate}
	if candidate.Status == model.DiscoveryCandidateRejected {
		result.Outcome = ReconcileConflict
		result.Reason = "已拒绝候选不能重新发布"
		return result, nil
	}
	if candidate.LeaseExpiresAt != nil && candidate.LeaseExpiresAt.Before(time.Now()) {
		return s.updateCandidateState(ctx, candidate, model.DiscoveryCandidateStale, "Agent 观测租约已过期", ReconcileStale)
	}
	if strings.TrimSpace(candidate.ProviderHint) == "" || strings.TrimSpace(candidate.WorkspaceHint) == "" {
		return s.updateCandidateState(ctx, candidate, model.DiscoveryCandidatePendingClaim, "缺少可信 Provider 或 Workspace Hint", ReconcilePending)
	}

	var binding model.WorkspaceBinding
	err := s.db.WithContext(ctx).Where("provider_id = ? AND external_workspace_id = ?", candidate.ProviderHint, candidate.WorkspaceHint).First(&binding).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return s.updateCandidateState(ctx, candidate, model.DiscoveryCandidatePendingClaim, "未找到可信 Workspace Binding", ReconcilePending)
	}
	if err != nil {
		return nil, fmt.Errorf("查询 Workspace Binding 失败: %w", err)
	}
	if binding.Status != model.WorkspaceBindingActive {
		return s.updateCandidateState(ctx, candidate, model.DiscoveryCandidateConflict, "Workspace Binding 已停止或撤销", ReconcileConflict)
	}
	if binding.ExpiresAt != nil && binding.ExpiresAt.Before(time.Now()) {
		return s.updateCandidateState(ctx, candidate, model.DiscoveryCandidateConflict, "Workspace Binding 已过期", ReconcileConflict)
	}
	if candidate.GenerationHint != 0 && candidate.GenerationHint != binding.Generation {
		return s.updateCandidateState(ctx, candidate, model.DiscoveryCandidateConflict, "Workspace generation 与可信绑定不一致", ReconcileConflict)
	}

	var providerBinding model.ProviderTenantBinding
	err = s.db.WithContext(ctx).Where(
		"provider_id = ? AND external_tenant_id = ? AND tenant_id = ? AND status = ?",
		binding.ProviderID, binding.ExternalTenantID, binding.TenantID, model.ProviderBindingActive,
	).First(&providerBinding).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return s.updateCandidateState(ctx, candidate, model.DiscoveryCandidateConflict, "Provider Tenant Binding 不可用", ReconcileConflict)
	}
	if err != nil {
		return nil, fmt.Errorf("查询 Provider Tenant Binding 失败: %w", err)
	}

	var resource model.Resource
	if err := s.db.WithContext(ctx).First(&resource, "id = ?", binding.ResourceID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return s.updateCandidateState(ctx, candidate, model.DiscoveryCandidateConflict, "Workspace Resource 不存在", ReconcileConflict)
		}
		return nil, fmt.Errorf("查询 Workspace Resource 失败: %w", err)
	}
	if resource.TenantID != binding.TenantID || resource.Type != model.ResourceTypeContainerSSH {
		return s.updateCandidateState(ctx, candidate, model.DiscoveryCandidateConflict, "Workspace Resource 客户或类型不一致", ReconcileConflict)
	}

	if err := s.validateAgent(ctx, candidate.AgentNodeID); err != nil {
		return s.updateCandidateState(ctx, candidate, model.DiscoveryCandidateConflict, err.Error(), ReconcileConflict)
	}

	var target *model.ResourceTarget
	outcome := ReconcilePublished
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current model.ResourceTarget
		currentErr := tx.Where("resource_id = ?", resource.ID).Order("revision DESC").First(&current).Error
		if currentErr != nil && !errors.Is(currentErr, gorm.ErrRecordNotFound) {
			return fmt.Errorf("查询当前 Resource Target 失败: %w", currentErr)
		}

		if currentErr == nil && sameRuntimeTarget(&current, &candidate) {
			current.ObservedAt = time.Now()
			if err := tx.Model(&current).Updates(map[string]interface{}{"observed_at": current.ObservedAt}).Error; err != nil {
				return fmt.Errorf("刷新 Resource Target 失败: %w", err)
			}
			target = &current
			outcome = ReconcileRefreshed
		} else {
			var conflict model.ResourceTarget
			conflictErr := tx.Where("pod_uid = ? AND container_name = ? AND resource_id <> ?", candidate.PodUID, candidate.ContainerName, resource.ID).First(&conflict).Error
			if conflictErr == nil {
				return &reconcileConflictError{reason: "Pod UID 和容器已绑定其他资源"}
			}
			if conflictErr != nil && !errors.Is(conflictErr, gorm.ErrRecordNotFound) {
				return fmt.Errorf("检查 Resource Target 冲突失败: %w", conflictErr)
			}

			revision := resource.TargetRevision + 1
			if currentErr == nil && current.Revision >= revision {
				revision = current.Revision + 1
			}
			newTarget := &model.ResourceTarget{
				ResourceID: resource.ID, Revision: revision, AgentNodeID: candidate.AgentNodeID,
				ClusterID: candidate.ClusterID, Namespace: candidate.Namespace, PodName: candidate.PodName,
				PodUID: candidate.PodUID, ContainerName: candidate.ContainerName, Ready: candidate.Ready,
				ObservedAt: time.Now(),
			}
			if err := tx.Create(newTarget).Error; err != nil {
				return fmt.Errorf("创建 Resource Target 失败: %w", err)
			}
			target = newTarget
		}

		state := model.ResourceStateDegraded
		if candidate.Ready {
			state = model.ResourceStateAvailable
		}
		updates := map[string]interface{}{
			"agent_node_id": candidate.AgentNodeID, "cluster_id": candidate.ClusterID,
			"namespace": candidate.Namespace, "pod_name": candidate.PodName, "pod_uid": candidate.PodUID,
			"container_name": candidate.ContainerName, "state": state,
		}
		if outcome == ReconcilePublished {
			updates["target_revision"] = target.Revision
		}
		if err := EnsureContainerSSHPort(tx, &resource); err != nil {
			return fmt.Errorf("分配 ContainerSSH 端口失败: %w", err)
		}
		if err := tx.Model(&resource).Updates(updates).Error; err != nil {
			return fmt.Errorf("更新 Workspace Resource 失败: %w", err)
		}

		candidate.Status = model.DiscoveryCandidatePublished
		candidate.ResourceID = resource.ID
		candidate.ConflictReason = ""
		if err := tx.Save(&candidate).Error; err != nil {
			return fmt.Errorf("保存已发布 Candidate 失败: %w", err)
		}
		return nil
	})
	if err != nil {
		var conflictErr *reconcileConflictError
		if errors.As(err, &conflictErr) {
			return s.updateCandidateState(ctx, candidate, model.DiscoveryCandidateConflict, conflictErr.reason, ReconcileConflict)
		}
		return nil, err
	}

	resource.TargetRevision = target.Revision
	resource.AgentNodeID = candidate.AgentNodeID
	resource.ClusterID = candidate.ClusterID
	resource.Namespace = candidate.Namespace
	resource.PodName = candidate.PodName
	resource.PodUID = candidate.PodUID
	resource.ContainerName = candidate.ContainerName
	if candidate.Ready {
		resource.State = model.ResourceStateAvailable
	} else {
		resource.State = model.ResourceStateDegraded
	}
	result.Candidate = candidate
	result.Resource = &resource
	result.Target = target
	result.Outcome = outcome
	return result, nil
}

func (s *ResourceReconciliationService) validateAgent(ctx context.Context, nodeID uint64) error {
	var node model.Node
	if err := s.db.WithContext(ctx).Where("id = ? AND type = ?", nodeID, model.NodeTypeAgent).First(&node).Error; err != nil {
		return errors.New("Agent 不存在或类型无效")
	}
	return nil
}

func (s *ResourceReconciliationService) updateCandidateState(ctx context.Context, candidate model.DiscoveryCandidate, status model.DiscoveryCandidateStatus, reason string, outcome ReconcileOutcome) (*ReconcileResult, error) {
	candidate.Status = status
	candidate.ConflictReason = reason
	if err := s.db.WithContext(ctx).Save(&candidate).Error; err != nil {
		return nil, fmt.Errorf("更新 Candidate 状态失败: %w", err)
	}
	return &ReconcileResult{Candidate: candidate, Outcome: outcome, Reason: reason}, nil
}

func sameRuntimeTarget(target *model.ResourceTarget, candidate *model.DiscoveryCandidate) bool {
	return target.AgentNodeID == candidate.AgentNodeID &&
		target.ClusterID == candidate.ClusterID &&
		target.Namespace == candidate.Namespace &&
		target.PodName == candidate.PodName &&
		target.PodUID == candidate.PodUID &&
		target.ContainerName == candidate.ContainerName &&
		target.Ready == candidate.Ready
}

type reconcileConflictError struct {
	reason string
}

func (e *reconcileConflictError) Error() string { return e.reason }
