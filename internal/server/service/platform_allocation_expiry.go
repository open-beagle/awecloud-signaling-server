package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

const platformAllocationExpiryReason = "validity_window_elapsed"

type PlatformAllocationExpiryService struct {
	db  *gorm.DB
	now func() time.Time
}

func NewPlatformAllocationExpiryService(database *gorm.DB) *PlatformAllocationExpiryService {
	return &PlatformAllocationExpiryService{db: database, now: time.Now}
}

func (s *PlatformAllocationExpiryService) ExpireDue(ctx context.Context, limit int) (int, error) {
	if s == nil || s.db == nil || limit < 1 || limit > 1000 {
		return 0, ErrPlatformAllocationInvalidInput
	}
	now := s.now().UTC()
	var candidates []model.ResourceAllocation
	if err := s.db.WithContext(ctx).Select("id", "row_version", "state", "expires_at").
		Where("state IN ? AND expires_at IS NOT NULL AND julianday(expires_at) <= julianday(?)",
			[]model.ResourceAllocationState{model.ResourceAllocationScheduled, model.ResourceAllocationActive, model.ResourceAllocationSuspended}, now).
		Order("expires_at ASC, id ASC").Limit(limit).Find(&candidates).Error; err != nil {
		return 0, err
	}

	expired := 0
	for _, candidate := range candidates {
		requestID := fmt.Sprintf("allocation-expiry:%s:%d", candidate.ID, candidate.RowVersion+1)
		didExpire := false
		err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			current, err := loadPlatformAllocation(tx, candidate.ID)
			if err != nil {
				if err == ErrPlatformAllocationObjectNotFound {
					return nil
				}
				return err
			}
			if !current.State.OccupiesScope() || current.RowVersion != candidate.RowVersion || current.ExpiresAt == nil || current.ExpiresAt.After(now) {
				return nil
			}
			updated := tx.Model(&model.ResourceAllocation{}).
				Where("id = ? AND row_version = ? AND state = ? AND expires_at IS NOT NULL AND julianday(expires_at) <= julianday(?)",
					current.ID, current.RowVersion, current.State, now).
				Updates(map[string]any{
					"state": model.ResourceAllocationExpired, "row_version": gorm.Expr("row_version + 1"),
					"terminated_at": now, "termination_reason": platformAllocationExpiryReason,
				})
			if updated.Error != nil {
				return mapPlatformAllocationConstraint(updated.Error)
			}
			if updated.RowsAffected == 0 {
				return nil
			}
			allocation, err := loadPlatformAllocation(tx, current.ID)
			if err != nil {
				return err
			}
			if err := AppendPlatformAllocationOutbox(tx, s.db, allocation, "resource_allocation.expired", requestID, now); err != nil {
				return err
			}
			detail, err := json.Marshal(map[string]any{
				"reason": platformAllocationExpiryReason, "tenant_id": allocation.TenantID,
				"state": allocation.State, "row_version": allocation.RowVersion,
			})
			if err != nil {
				return err
			}
			audit := model.AuditLog{
				UserType: "system", ActorUsername: "system:resource_allocation_expiry",
				ScopeType: string(model.ManagementScopePlatform), RequestID: requestID,
				ActionType: "expire_resource_allocation", TargetType: "resource_allocation",
				TargetID: allocation.ID, TargetName: allocation.ID, Detail: string(detail),
			}
			if err := tx.Create(&audit).Error; err != nil {
				return err
			}
			didExpire = true
			return nil
		})
		if err != nil {
			return expired, err
		}
		if didExpire {
			expired++
		}
	}
	return expired, nil
}

func (s *PlatformAllocationExpiryService) StartPeriodicExpiration(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	logger.Info("启动 Platform Allocation 到期任务，间隔 1 分钟")
	for {
		select {
		case <-ctx.Done():
			logger.Info("Platform Allocation 到期任务已停止")
			return
		case <-ticker.C:
			count, err := s.ExpireDue(ctx, 100)
			if err != nil {
				logger.Warnf("Platform Allocation 到期处理失败: %v", err)
			} else if count > 0 {
				logger.Infof("Platform Allocation 已到期: count=%d", count)
			}
		}
	}
}
