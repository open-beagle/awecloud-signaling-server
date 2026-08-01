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

const (
	tenantGrantExpiryReasonCode             = "GRANT_EXPIRED"
	tenantGrantExpiryReason                 = "grant validity window elapsed"
	sessionAuthorizationDisabledReasonCode  = "SESSION_AUTHORIZATION_V2_DISABLED"
	sessionAuthorizationDisabledReason      = "session authorization v2 is disabled"
	tenantAuthorizationMaintenanceInterval  = time.Minute
	tenantAuthorizationMaintenanceBatchSize = 100
)

type TenantAuthorizationMaintenanceService struct {
	db  *gorm.DB
	now func() time.Time
}

func NewTenantAuthorizationMaintenanceService(database *gorm.DB) *TenantAuthorizationMaintenanceService {
	return &TenantAuthorizationMaintenanceService{db: database, now: time.Now}
}

func (s *TenantAuthorizationMaintenanceService) ExpireDueGrants(ctx context.Context, limit int) (int, error) {
	if s == nil || s.db == nil || limit < 1 || limit > 1000 {
		return 0, ErrTenantGrantInvalidInput
	}
	now := s.now().UTC()
	var candidates []model.TenantAccessGrant
	if err := s.db.WithContext(ctx).Select("id", "tenant_id", "row_version", "status", "expires_at").
		Where("status IN ? AND expires_at IS NOT NULL AND julianday(expires_at) <= julianday(?)",
			[]model.TenantAccessGrantStatus{model.TenantAccessGrantEnabled, model.TenantAccessGrantSuspended}, now).
		Order("expires_at ASC, id ASC").Limit(limit).Find(&candidates).Error; err != nil {
		return 0, err
	}

	expired := 0
	for i := range candidates {
		requestID := fmt.Sprintf("tenant-grant-expiry:%s", candidates[i].ID)
		didExpire := false
		err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			grant, err := loadTenantGrant(tx, candidates[i].TenantID, candidates[i].ID)
			if err != nil {
				if err == ErrTenantGrantNotFound {
					return nil
				}
				return err
			}
			if grant.RowVersion != candidates[i].RowVersion ||
				(grant.Status != model.TenantAccessGrantEnabled && grant.Status != model.TenantAccessGrantSuspended) ||
				grant.ExpiresAt == nil || grant.ExpiresAt.After(now) {
				return nil
			}
			result := tx.Model(&model.TenantAccessGrant{}).
				Where("tenant_id = ? AND id = ? AND row_version = ? AND status = ? AND expires_at IS NOT NULL AND julianday(expires_at) <= julianday(?)",
					grant.TenantID, grant.ID, grant.RowVersion, grant.Status, now).
				Updates(map[string]any{
					"status": model.TenantAccessGrantExpired, "revision": gorm.Expr("revision + 1"),
					"row_version": gorm.Expr("row_version + 1"),
				})
			if result.Error != nil {
				return mapTenantGrantConstraint(result.Error)
			}
			if result.RowsAffected != 1 {
				return nil
			}
			if err := tx.Where("tenant_id = ? AND id = ?", grant.TenantID, grant.ID).First(grant).Error; err != nil {
				return err
			}
			systemAuthorization := &ManagementAuthorizationContext{
				ActorUserID: grant.CreatedByUserID, EffectiveUserID: grant.CreatedByUserID,
				ScopeType: model.ManagementScopeTenant, ScopeID: grant.TenantID,
			}
			if err := createTenantGrantEvent(tx, systemAuthorization, grant, "expired", requestID, tenantGrantExpiryReason, now); err != nil {
				return err
			}
			if err := endSessionsForGrant(tx, grant, tenantGrantExpiryReasonCode, tenantGrantExpiryReason, requestID, now); err != nil {
				return err
			}
			if err := AppendTenantManagementOutbox(tx, TenantManagementOutboxInput{
				EventType: "tenant_access_grant.expired", AggregateType: "tenant_access_grant", AggregateID: grant.ID,
				AggregateRevision: grant.Revision, TenantID: grant.TenantID, ResourceID: grant.TenantResourceID,
				GrantID: grant.ID, Status: string(grant.Status), RowVersion: grant.RowVersion,
				ReasonCode: tenantGrantExpiryReasonCode, RequestID: requestID, AvailableAt: now,
			}); err != nil {
				return err
			}
			if err := appendTenantAuthorizationSystemAudit(tx, grant.TenantID, requestID, "expire_tenant_access_grant",
				"tenant_access_grant", grant.ID, grant.ID, map[string]any{
					"reason": tenantGrantExpiryReason, "resource_id": grant.TenantResourceID,
					"revision": grant.Revision, "row_version": grant.RowVersion,
				}); err != nil {
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

func (s *TenantAuthorizationMaintenanceService) EndSessionsWhenAuthorizationDisabled(ctx context.Context, limit int) (int, error) {
	if s == nil || s.db == nil || limit < 1 || limit > 1000 {
		return 0, ErrResourceSessionInvalidInput
	}
	now := s.now().UTC()
	var candidates []model.ResourceSession
	if err := s.db.WithContext(ctx).Select("id", "tenant_id", "row_version").
		Where("status IN ?", []model.ResourceSessionStatus{model.ResourceSessionAuthorizing, model.ResourceSessionActive}).
		Order("started_at ASC, id ASC").Limit(limit).Find(&candidates).Error; err != nil {
		return 0, err
	}

	ended := 0
	for i := range candidates {
		requestID := fmt.Sprintf("session-auth-off:%s", candidates[i].ID)
		didEnd := false
		err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			session, err := loadResourceSession(tx, candidates[i].TenantID, candidates[i].ID)
			if err != nil {
				if err == ErrResourceSessionNotFound {
					return nil
				}
				return err
			}
			if session.RowVersion != candidates[i].RowVersion ||
				(session.Status != model.ResourceSessionAuthorizing && session.Status != model.ResourceSessionActive) {
				return nil
			}
			result := tx.Model(&model.ResourceSession{}).
				Where("tenant_id = ? AND id = ? AND row_version = ? AND status IN ?", session.TenantID, session.ID, session.RowVersion,
					[]model.ResourceSessionStatus{model.ResourceSessionAuthorizing, model.ResourceSessionActive}).
				Updates(map[string]any{
					"status": model.ResourceSessionEnding, "close_reason": sessionAuthorizationDisabledReasonCode,
					"row_version": gorm.Expr("row_version + 1"),
				})
			if result.Error != nil {
				return mapResourceSessionConstraint(result.Error)
			}
			if result.RowsAffected != 1 {
				return nil
			}
			if err := tx.Where("tenant_id = ? AND id = ?", session.TenantID, session.ID).First(session).Error; err != nil {
				return err
			}
			if err := createSessionTermination(tx, session, sessionAuthorizationDisabledReasonCode, sessionAuthorizationDisabledReason, now); err != nil {
				return err
			}
			if err := AppendTenantManagementOutbox(tx, TenantManagementOutboxInput{
				EventType: "resource_session.ending", AggregateType: "resource_session", AggregateID: session.ID,
				AggregateRevision: session.RowVersion, TenantID: session.TenantID, ResourceID: session.TenantResourceID,
				GrantID: session.GrantID, SessionID: session.ID, Status: string(session.Status), RowVersion: session.RowVersion,
				ReasonCode: sessionAuthorizationDisabledReasonCode, RequestID: requestID, AvailableAt: now,
			}); err != nil {
				return err
			}
			if err := appendTenantAuthorizationSystemAudit(tx, session.TenantID, requestID, "end_resource_session_for_disabled_authorization",
				"resource_session", session.ID, session.ID, map[string]any{
					"reason": sessionAuthorizationDisabledReason, "resource_id": session.TenantResourceID,
					"grant_id": session.GrantID, "row_version": session.RowVersion,
				}); err != nil {
				return err
			}
			didEnd = true
			return nil
		})
		if err != nil {
			return ended, err
		}
		if didEnd {
			ended++
		}
	}
	return ended, nil
}

func (s *TenantAuthorizationMaintenanceService) DrainSessionsWhenAuthorizationDisabled(ctx context.Context) (int, error) {
	total := 0
	for {
		count, err := s.EndSessionsWhenAuthorizationDisabled(ctx, tenantAuthorizationMaintenanceBatchSize)
		total += count
		if err != nil || count == 0 {
			return total, err
		}
	}
}

func (s *TenantAuthorizationMaintenanceService) StartPeriodicGrantExpiration(ctx context.Context) {
	ticker := time.NewTicker(tenantAuthorizationMaintenanceInterval)
	defer ticker.Stop()
	logger.Info("启动 Tenant AccessGrant 到期任务，间隔 1 分钟")
	for {
		select {
		case <-ctx.Done():
			logger.Info("Tenant AccessGrant 到期任务已停止")
			return
		case <-ticker.C:
			count, err := s.ExpireDueGrants(ctx, tenantAuthorizationMaintenanceBatchSize)
			if err != nil {
				logger.Warnf("Tenant AccessGrant 到期处理失败: %v", err)
			} else if count > 0 {
				logger.Infof("Tenant AccessGrant 已到期: count=%d", count)
			}
		}
	}
}

func appendTenantAuthorizationSystemAudit(tx *gorm.DB, tenantID, requestID, action, targetType, targetID, targetName string, detail map[string]any) error {
	encoded, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	return tx.Create(&model.AuditLog{
		UserType: "system", ActorUsername: "system:tenant_authorization_maintenance",
		ScopeType: string(model.ManagementScopeTenant), ScopeID: tenantID, TenantID: tenantID,
		RequestID: requestID, ActionType: action, TargetType: targetType, TargetID: targetID,
		TargetName: targetName, Detail: string(encoded),
	}).Error
}
