package service

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

var (
	ErrResourceSessionInvalidInput        = errors.New("invalid resource session input")
	ErrResourceSessionNotFound            = errors.New("resource session not found")
	ErrResourceSessionDeviceUnauthorized  = errors.New("resource session device is not authorized")
	ErrResourceSessionAuthorizationDenied = errors.New("resource session authorization is denied")
	ErrResourceSessionTargetUnavailable   = errors.New("resource session target is unavailable")
	ErrResourceSessionVersionConflict     = errors.New("resource session version conflict")
	ErrResourceSessionStateTransition     = errors.New("resource session state transition is invalid")
)

const resourceSessionSnapshotTTL = 30 * time.Second
const resourceSessionDeviceHeartbeatTTL = 2 * time.Minute

type ResourceSessionView struct {
	ID                    string                      `json:"id"`
	ResourceID            string                      `json:"resource_id"`
	GrantID               string                      `json:"grant_id"`
	GrantRevision         int64                       `json:"grant_revision"`
	UserID                uint64                      `json:"user_id"`
	DeviceID              uint64                      `json:"device_id"`
	SessionType           model.ResourceSessionType   `json:"session_type"`
	Action                string                      `json:"action"`
	AuthorizationRevision int64                       `json:"authorization_revision"`
	ValidUntil            time.Time                   `json:"valid_until"`
	Status                model.ResourceSessionStatus `json:"status"`
	RequestID             string                      `json:"request_id"`
	StartedAt             time.Time                   `json:"started_at"`
	ConnectedAt           *time.Time                  `json:"connected_at,omitempty"`
	EndedAt               *time.Time                  `json:"ended_at,omitempty"`
	Result                string                      `json:"result,omitempty"`
	CloseReason           string                      `json:"close_reason,omitempty"`
	RowVersion            int64                       `json:"row_version"`
	CreatedAt             time.Time                   `json:"created_at"`
	UpdatedAt             time.Time                   `json:"updated_at"`
}

type ResourceSessionListInput struct {
	ResourceID string
	UserID     uint64
	Status     string
	Page       int
	PageSize   int
}

type ResourceSessionListResult struct {
	Items []ResourceSessionView
	Total int64
}

type ResourceSessionService struct {
	db  *gorm.DB
	now func() time.Time
}

func NewResourceSessionService(database *gorm.DB) *ResourceSessionService {
	return &ResourceSessionService{db: database, now: time.Now}
}

func (s *ResourceSessionService) View(session *model.ResourceSession) (*ResourceSessionView, error) {
	if session == nil {
		return nil, ErrResourceSessionInvalidInput
	}
	view := resourceSessionView(session)
	return &view, nil
}

func (s *ResourceSessionService) List(ctx context.Context, authorization *ManagementAuthorizationContext, tenantID string, input ResourceSessionListInput) (*ResourceSessionListResult, error) {
	if s == nil || s.db == nil {
		return nil, ErrResourceSessionInvalidInput
	}
	tenantID, input.ResourceID, input.Status = strings.TrimSpace(tenantID), strings.TrimSpace(input.ResourceID), strings.TrimSpace(input.Status)
	if input.Page == 0 {
		input.Page = 1
	}
	if input.PageSize == 0 {
		input.PageSize = 20
	}
	if validateRequired("tenant_id", tenantID, 36) != nil || len(input.ResourceID) > 36 || input.Page < 1 || input.PageSize < 1 || input.PageSize > 100 {
		return nil, ErrResourceSessionInvalidInput
	}
	if input.Status != "" && !validResourceSessionStatus(model.ResourceSessionStatus(input.Status)) {
		return nil, ErrResourceSessionInvalidInput
	}
	if err := reauthorizeTenantPermission(s.db.WithContext(ctx), authorization, tenantID, PermissionTenantSessionsRead, s.now().UTC()); err != nil {
		return nil, err
	}
	query := s.db.WithContext(ctx).Model(&model.ResourceSession{}).Where("tenant_id = ?", tenantID)
	if input.ResourceID != "" {
		query = query.Where("tenant_resource_id = ?", input.ResourceID)
	}
	if input.UserID != 0 {
		query = query.Where("user_id = ?", input.UserID)
	}
	if input.Status != "" {
		query = query.Where("status = ?", input.Status)
	}
	result := &ResourceSessionListResult{Items: []ResourceSessionView{}}
	if err := query.Count(&result.Total).Error; err != nil {
		return nil, err
	}
	var sessions []model.ResourceSession
	if err := query.Order("started_at DESC, id DESC").Offset((input.Page - 1) * input.PageSize).Limit(input.PageSize).Find(&sessions).Error; err != nil {
		return nil, err
	}
	result.Items = make([]ResourceSessionView, 0, len(sessions))
	for i := range sessions {
		result.Items = append(result.Items, resourceSessionView(&sessions[i]))
	}
	return result, nil
}

func (s *ResourceSessionService) Get(ctx context.Context, authorization *ManagementAuthorizationContext, tenantID, sessionID string) (*ResourceSessionView, error) {
	if s == nil || s.db == nil {
		return nil, ErrResourceSessionInvalidInput
	}
	tenantID, sessionID = strings.TrimSpace(tenantID), strings.TrimSpace(sessionID)
	if validateRequired("tenant_id", tenantID, 36) != nil || validateRequired("session_id", sessionID, 36) != nil {
		return nil, ErrResourceSessionInvalidInput
	}
	if err := reauthorizeTenantPermission(s.db.WithContext(ctx), authorization, tenantID, PermissionTenantSessionsRead, s.now().UTC()); err != nil {
		return nil, err
	}
	session, err := loadResourceSession(s.db.WithContext(ctx), tenantID, sessionID)
	if err != nil {
		return nil, err
	}
	view := resourceSessionView(session)
	return &view, nil
}

type CreateResourceSessionInput struct {
	TenantID         string
	ResourceID       string
	Action           string
	DeviceID         uint64
	ClientCapability string
	RequestID        string
	TraceID          string
}

func (s *ResourceSessionService) Create(ctx context.Context, authorization *ManagementAuthorizationContext, input CreateResourceSessionInput) (*model.ResourceSession, error) {
	if s == nil || s.db == nil {
		return nil, ErrResourceSessionInvalidInput
	}
	input.TenantID, input.ResourceID, input.Action = strings.TrimSpace(input.TenantID), strings.TrimSpace(input.ResourceID), strings.TrimSpace(input.Action)
	input.ClientCapability, input.RequestID, input.TraceID = strings.TrimSpace(input.ClientCapability), strings.TrimSpace(input.RequestID), strings.TrimSpace(input.TraceID)
	if validateRequired("tenant_id", input.TenantID, 36) != nil || validateRequired("resource_id", input.ResourceID, 36) != nil ||
		validateRequired("action", input.Action, 30) != nil || input.DeviceID == 0 || input.ClientCapability != "resource_session_v2" ||
		validateRequired("request_id", input.RequestID, 100) != nil || len(input.TraceID) > 100 {
		return nil, ErrResourceSessionInvalidInput
	}
	now := s.now().UTC()
	var session model.ResourceSession
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := reauthorizeTenantPermission(tx, authorization, input.TenantID, PermissionTenantSessionsRead, now); err != nil {
			return err
		}
		var activeTenant int64
		if err := tx.Model(&model.Tenant{}).Where("id = ? AND status = ?", input.TenantID, model.TenantStatusActive).Count(&activeTenant).Error; err != nil {
			return err
		}
		if activeTenant != 1 {
			return ErrResourceSessionAuthorizationDenied
		}
		var membership model.TenantMembership
		if err := tx.Where("tenant_id = ? AND user_id = ? AND enabled = ? AND (expires_at IS NULL OR expires_at > ?)",
			input.TenantID, authorization.EffectiveUserID, true, now).First(&membership).Error; err != nil {
			return ErrResourceSessionAuthorizationDenied
		}
		var effectiveUser model.User
		if err := tx.Where("id = ? AND enabled = ?", authorization.EffectiveUserID, true).First(&effectiveUser).Error; err != nil {
			return ErrResourceSessionAuthorizationDenied
		}
		var device model.Node
		if err := tx.Where("id = ? AND user_id = ? AND type = ?", input.DeviceID, authorization.EffectiveUserID, model.NodeTypeDesktop).First(&device).Error; err != nil {
			return ErrResourceSessionDeviceUnauthorized
		}
		if device.LastHeartbeat == nil || device.LastHeartbeat.Before(now.Add(-resourceSessionDeviceHeartbeatTTL)) {
			return ErrResourceSessionDeviceUnauthorized
		}
		chain, err := loadTenantResourceChain(tx, input.TenantID, input.ResourceID, now, true)
		if err != nil {
			if errors.Is(err, ErrTenantResourceNotFound) {
				return ErrResourceSessionNotFound
			}
			return ErrResourceSessionTargetUnavailable
		}
		if chain.Resource.VisibilityState != model.TenantResourceVisible {
			return ErrResourceSessionAuthorizationDenied
		}
		if err := validateActionsForResource(chain.Resource.Type, []string{input.Action}); err != nil {
			return ErrResourceSessionAuthorizationDenied
		}
		grant, err := selectResourceSessionGrant(tx, input.TenantID, input.ResourceID, authorization.EffectiveUserID, input.Action, now)
		if err != nil {
			return err
		}
		validUntil := now.Add(resourceSessionSnapshotTTL)
		for _, candidate := range []*time.Time{membership.ExpiresAt, chain.Allocation.ExpiresAt, &chain.Observation.LeaseExpiresAt, &chain.Evidence.LeaseExpiresAt, grant.ExpiresAt} {
			if candidate != nil && candidate.Before(validUntil) {
				validUntil = candidate.UTC()
			}
		}
		maxSessionUntil := now.Add(time.Duration(grant.MaxSessionSeconds) * time.Second)
		if maxSessionUntil.Before(validUntil) {
			validUntil = maxSessionUntil
		}
		if !validUntil.After(now) {
			return ErrResourceSessionAuthorizationDenied
		}
		sessionType := model.ResourceSessionContainerService
		if chain.Resource.Type == model.TenantResourceContainerSSH {
			sessionType = model.ResourceSessionContainerSSH
		}
		var simulationID *string
		if authorization.SimulationSessionID != "" {
			value := authorization.SimulationSessionID
			simulationID = &value
		}
		session = model.ResourceSession{
			ID: uuid.NewString(), TenantID: input.TenantID, TenantResourceID: chain.Resource.ID,
			TenantResourceSourceID: chain.Source.ID, TargetRevisionID: chain.Target.ID,
			AllocationID: chain.Allocation.ID, AllocationItemID: chain.Item.ID,
			GrantID: grant.ID, GrantRevision: grant.Revision, UserID: authorization.EffectiveUserID,
			TenantMembershipID: membership.ID, DeviceID: input.DeviceID,
			ActorUserID: authorization.ActorUserID, EffectiveUserID: authorization.EffectiveUserID, SimulationSessionID: simulationID,
			SessionType: sessionType, Action: input.Action, AccessTechnicalResourceID: chain.Target.AccessTechnicalResourceID,
			AuthorizationRevision: resourceAuthorizationRevision(chain, grant, authorization.PermissionRevision), ValidUntil: validUntil,
			Status: model.ResourceSessionAuthorizing, RequestID: input.RequestID, TraceID: input.TraceID,
			StartedAt: now, RowVersion: 1,
		}
		if err := tx.Create(&session).Error; err != nil {
			return mapResourceSessionConstraint(err)
		}
		return nil
	})
	return &session, err
}

type TerminateResourceSessionInput struct {
	TenantID           string
	SessionID          string
	ExpectedRowVersion int64
	Reason             string
	RequestID          string
}

func (s *ResourceSessionService) Terminate(ctx context.Context, authorization *ManagementAuthorizationContext, input TerminateResourceSessionInput) (*model.ResourceSession, error) {
	if s == nil || s.db == nil {
		return nil, ErrResourceSessionInvalidInput
	}
	input.TenantID, input.SessionID, input.Reason, input.RequestID = strings.TrimSpace(input.TenantID), strings.TrimSpace(input.SessionID), strings.TrimSpace(input.Reason), strings.TrimSpace(input.RequestID)
	if validateRequired("tenant_id", input.TenantID, 36) != nil || validateRequired("session_id", input.SessionID, 36) != nil ||
		input.ExpectedRowVersion <= 0 || validateRequired("reason", input.Reason, 500) != nil || validateRequired("request_id", input.RequestID, 100) != nil {
		return nil, ErrResourceSessionInvalidInput
	}
	now := s.now().UTC()
	var session model.ResourceSession
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := reauthorizeTenantPermission(tx, authorization, input.TenantID, PermissionTenantSessionsTerminate, now); err != nil {
			return err
		}
		current, err := loadResourceSession(tx, input.TenantID, input.SessionID)
		if err != nil {
			return err
		}
		if current.RowVersion != input.ExpectedRowVersion {
			return ErrResourceSessionVersionConflict
		}
		if current.Status != model.ResourceSessionAuthorizing && current.Status != model.ResourceSessionActive {
			return ErrResourceSessionStateTransition
		}
		result := tx.Model(&model.ResourceSession{}).Where("tenant_id = ? AND id = ? AND row_version = ? AND status IN ?", input.TenantID, input.SessionID, input.ExpectedRowVersion,
			[]model.ResourceSessionStatus{model.ResourceSessionAuthorizing, model.ResourceSessionActive}).
			Updates(map[string]any{"status": model.ResourceSessionEnding, "close_reason": "MANUAL_TERMINATION", "row_version": gorm.Expr("row_version + 1")})
		if result.Error != nil {
			return mapResourceSessionConstraint(result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrResourceSessionVersionConflict
		}
		if err := tx.Where("tenant_id = ? AND id = ?", input.TenantID, input.SessionID).First(&session).Error; err != nil {
			return err
		}
		if err := createSessionTermination(tx, &session, "MANUAL_TERMINATION", input.Reason, now); err != nil {
			return err
		}
		return AppendTenantManagementOutbox(tx, TenantManagementOutboxInput{
			EventType: "resource_session.ending", AggregateType: "resource_session", AggregateID: session.ID,
			AggregateRevision: session.RowVersion, TenantID: session.TenantID, ResourceID: session.TenantResourceID,
			GrantID: session.GrantID, SessionID: session.ID, Status: string(session.Status), RowVersion: session.RowVersion,
			ReasonCode: "MANUAL_TERMINATION", RequestID: input.RequestID, AvailableAt: now,
		})
	})
	return &session, err
}

func selectResourceSessionGrant(tx *gorm.DB, tenantID, resourceID string, userID uint64, action string, now time.Time) (*model.TenantAccessGrant, error) {
	var grants []model.TenantAccessGrant
	if err := tx.Where("tenant_id = ? AND tenant_resource_id = ? AND status = ? AND valid_from <= ? AND (expires_at IS NULL OR expires_at > ?)",
		tenantID, resourceID, model.TenantAccessGrantEnabled, now, now).Find(&grants).Error; err != nil {
		return nil, err
	}
	type candidate struct {
		grant  model.TenantAccessGrant
		direct bool
	}
	candidates := make([]candidate, 0, len(grants))
	for i := range grants {
		var actions []string
		if json.Unmarshal([]byte(grants[i].Actions), &actions) != nil || !containsString(actions, action) {
			continue
		}
		direct := grants[i].SubjectType == model.TenantAccessGrantSubjectUser && grants[i].SubjectUserID != nil && *grants[i].SubjectUserID == userID
		if !direct {
			if grants[i].SubjectType != model.TenantAccessGrantSubjectGroup || grants[i].SubjectGroupID == nil {
				continue
			}
			var membership int64
			if err := tx.Table("group_member AS member").Joins(`JOIN "group" AS subject_group ON subject_group.id = member.group_id`).
				Where("member.group_id = ? AND member.user_id = ? AND subject_group.tenant_id = ?", *grants[i].SubjectGroupID, userID, tenantID).Count(&membership).Error; err != nil {
				return nil, err
			}
			if membership != 1 {
				continue
			}
		}
		candidates = append(candidates, candidate{grant: grants[i], direct: direct})
	}
	if len(candidates) == 0 {
		return nil, ErrResourceSessionAuthorizationDenied
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].direct != candidates[j].direct {
			return candidates[i].direct
		}
		left, right := candidates[i].grant.ExpiresAt, candidates[j].grant.ExpiresAt
		if left != nil || right != nil {
			if left == nil {
				return false
			}
			if right == nil {
				return true
			}
			if !left.Equal(*right) {
				return left.Before(*right)
			}
		}
		if candidates[i].grant.MaxSessionSeconds != candidates[j].grant.MaxSessionSeconds {
			return candidates[i].grant.MaxSessionSeconds < candidates[j].grant.MaxSessionSeconds
		}
		return candidates[i].grant.ID < candidates[j].grant.ID
	})
	return &candidates[0].grant, nil
}

func resourceAuthorizationRevision(chain *tenantResourceChain, grant *model.TenantAccessGrant, permissionRevision int64) int64 {
	values := []int64{
		chain.Resource.Revision, chain.Resource.RowVersion, chain.Source.SourceRevision, chain.Source.RowVersion,
		chain.Observation.ObservedRevision, chain.Observation.RowVersion, chain.Target.Revision,
		chain.Allocation.RowVersion, chain.Scope.RowVersion, grant.Revision, grant.RowVersion, permissionRevision,
	}
	result := int64(1)
	for _, value := range values {
		if value > result {
			result = value
		}
	}
	return result
}

func loadResourceSession(tx *gorm.DB, tenantID, sessionID string) (*model.ResourceSession, error) {
	var session model.ResourceSession
	if err := tx.Where("tenant_id = ? AND id = ?", tenantID, sessionID).First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrResourceSessionNotFound
		}
		return nil, err
	}
	return &session, nil
}

func resourceSessionView(session *model.ResourceSession) ResourceSessionView {
	return ResourceSessionView{
		ID: session.ID, ResourceID: session.TenantResourceID, GrantID: session.GrantID, GrantRevision: session.GrantRevision,
		UserID: session.UserID, DeviceID: session.DeviceID, SessionType: session.SessionType, Action: session.Action,
		AuthorizationRevision: session.AuthorizationRevision, ValidUntil: session.ValidUntil, Status: session.Status,
		RequestID: session.RequestID, StartedAt: session.StartedAt, ConnectedAt: session.ConnectedAt, EndedAt: session.EndedAt,
		Result: session.Result, CloseReason: session.CloseReason, RowVersion: session.RowVersion,
		CreatedAt: session.CreatedAt, UpdatedAt: session.UpdatedAt,
	}
}

func validResourceSessionStatus(value model.ResourceSessionStatus) bool {
	switch value {
	case model.ResourceSessionAuthorizing, model.ResourceSessionActive, model.ResourceSessionEnding,
		model.ResourceSessionEnded, model.ResourceSessionTerminated, model.ResourceSessionRejected:
		return true
	default:
		return false
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func mapResourceSessionConstraint(err error) error {
	if err == nil {
		return nil
	}
	message := err.Error()
	switch {
	case strings.Contains(message, "S4_RESOURCE_SESSION_CHAIN_MISMATCH"), strings.Contains(message, "S4_RESOURCE_SESSION_SIMULATION_MISMATCH"):
		return ErrResourceSessionAuthorizationDenied
	case strings.Contains(message, "S4_RESOURCE_SESSION_VERSION_INVALID"):
		return ErrResourceSessionVersionConflict
	case strings.Contains(message, "S4_RESOURCE_SESSION_STATUS_TRANSITION_INVALID"):
		return ErrResourceSessionStateTransition
	case strings.Contains(message, "S4_SESSION_TERMINATION"):
		return ErrResourceSessionStateTransition
	case isDatabaseConstraintError(err):
		return ErrResourceSessionInvalidInput
	default:
		return err
	}
}
