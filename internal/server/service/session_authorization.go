package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

const (
	sessionAuthorizationRetryInterval  = 5 * time.Second
	sessionAuthorizationExpiredCode    = "SESSION_AUTHORIZATION_EXPIRED"
	sessionAuthorizationInvalidCode    = "SESSION_AUTHORIZATION_INVALID"
	sessionAuthorizationReportMaxItems = 100
)

var (
	ErrSessionAuthorizationInvalidInput  = errors.New("invalid session authorization input")
	ErrSessionAuthorizationSourceDenied  = errors.New("session authorization source is denied")
	ErrSessionAuthorizationEventConflict = errors.New("session authorization event conflicts with accepted sequence")
)

type SessionAuthorizationTarget struct {
	NamespaceUID  string `json:"namespace_uid,omitempty"`
	NamespaceName string `json:"namespace_name,omitempty"`
	ServiceUID    string `json:"service_uid,omitempty"`
	ServiceName   string `json:"service_name,omitempty"`
	PortName      string `json:"port_name,omitempty"`
	PortNumber    int32  `json:"port_number,omitempty"`
	Protocol      string `json:"protocol,omitempty"`
	WorkloadUID   string `json:"workload_uid,omitempty"`
	PodName       string `json:"pod_name,omitempty"`
	PodUID        string `json:"pod_uid,omitempty"`
	ContainerName string `json:"container_name,omitempty"`
}

type SessionAuthorizationPermission struct {
	SessionID             string
	TenantID              string
	ResourceID            string
	SourceID              string
	TargetRevisionID      string
	UserID                uint64
	UserName              string
	DeviceID              uint64
	DeviceHeadscaleNodeID uint64
	ResourceType          model.TenantResourceType
	Action                string
	AllocationID          string
	GrantID               string
	GrantRevision         int64
	AuthorizationRevision int64
	ValidUntil            time.Time
	SSHUsers              []string
	Target                SessionAuthorizationTarget
}

type SessionTerminationCommand struct {
	SessionID       string
	CommandRevision int64
	ReasonCode      string
	Reason          string
}

type SessionAuthorizationSnapshot struct {
	TechnicalResourceID string
	Permissions         []SessionAuthorizationPermission
	Terminations        []SessionTerminationCommand
}

type SessionTerminationAckInput struct {
	SessionID       string
	CommandRevision int64
}

type SessionEventInput struct {
	EventID        string
	SessionID      string
	SourceSequence int64
	EventType      model.ResourceSessionEventType
	OccurredAt     time.Time
	ResultCode     string
}

type SessionEventAck struct {
	EventID    string
	ResultCode string
	Replay     bool
}

type SessionAuthorizationService struct {
	db  *gorm.DB
	now func() time.Time
}

func NewSessionAuthorizationService(database *gorm.DB) *SessionAuthorizationService {
	return &SessionAuthorizationService{db: database, now: time.Now}
}

func (s *SessionAuthorizationService) ResolveTechnicalResource(ctx context.Context, sourceType model.TechnicalResourceBindingSourceType, sourceID string) (*model.TechnicalResource, error) {
	if s == nil || s.db == nil || strings.TrimSpace(sourceID) == "" ||
		(sourceType != model.TechnicalResourceBindingLegacyNode && sourceType != model.TechnicalResourceBindingLegacyEndpoint) {
		return nil, ErrSessionAuthorizationInvalidInput
	}
	var binding model.TechnicalResourceBinding
	if err := s.db.WithContext(ctx).Where("source_type = ? AND source_id = ? AND enabled = ?", sourceType, strings.TrimSpace(sourceID), true).First(&binding).Error; err != nil {
		return nil, ErrSessionAuthorizationSourceDenied
	}
	var technical model.TechnicalResource
	if err := s.db.WithContext(ctx).Where("id = ? AND lifecycle_state = ?", binding.TechnicalResourceID, model.TechnicalResourceRegistered).First(&technical).Error; err != nil {
		return nil, ErrSessionAuthorizationSourceDenied
	}
	if technical.CredentialRevision != binding.CredentialRevision {
		return nil, ErrSessionAuthorizationSourceDenied
	}
	return &technical, nil
}

func (s *SessionAuthorizationService) BuildSnapshot(ctx context.Context, technicalResourceID string, includePermissions bool) (*SessionAuthorizationSnapshot, error) {
	if s == nil || s.db == nil || validateRequired("technical_resource_id", strings.TrimSpace(technicalResourceID), 36) != nil {
		return nil, ErrSessionAuthorizationInvalidInput
	}
	now := s.now().UTC()
	result := &SessionAuthorizationSnapshot{TechnicalResourceID: technicalResourceID, Permissions: []SessionAuthorizationPermission{}, Terminations: []SessionTerminationCommand{}}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var sessions []model.ResourceSession
		if err := tx.Where("access_technical_resource_id = ? AND status IN ?", technicalResourceID,
			[]model.ResourceSessionStatus{model.ResourceSessionAuthorizing, model.ResourceSessionActive}).Order("id ASC").Find(&sessions).Error; err != nil {
			return err
		}
		for i := range sessions {
			permission, reasonCode, err := s.revalidateSession(tx, &sessions[i], now, includePermissions)
			if err != nil {
				return err
			}
			if permission == nil {
				if reasonCode == "" {
					reasonCode = sessionAuthorizationInvalidCode
				}
				if err := endInvalidResourceSession(tx, &sessions[i], reasonCode, now); err != nil {
					return err
				}
				continue
			}
			result.Permissions = append(result.Permissions, *permission)
		}

		var terminations []model.ResourceSessionTermination
		if err := tx.Table("resource_session_termination AS termination").
			Select("termination.*").
			Joins("JOIN resource_session AS session ON session.id = termination.session_id").
			Where("session.access_technical_resource_id = ? AND termination.status IN ?",
				technicalResourceID, []model.ResourceSessionTerminationStatus{model.ResourceSessionTerminationPending, model.ResourceSessionTerminationDelivered}).
			Order("termination.session_id ASC, termination.command_revision ASC").Find(&terminations).Error; err != nil {
			return err
		}
		for i := range terminations {
			due := terminations[i].NextAttemptAt == nil || !terminations[i].NextAttemptAt.After(now)
			if due {
				updates := map[string]any{
					"status":          model.ResourceSessionTerminationDelivered,
					"next_attempt_at": now.Add(sessionAuthorizationRetryInterval),
				}
				if terminations[i].DeliveredAt == nil {
					updates["delivered_at"] = now
				} else {
					updates["retry_count"] = gorm.Expr("retry_count + 1")
				}
				if err := tx.Model(&model.ResourceSessionTermination{}).Where("id = ? AND status IN ?", terminations[i].ID,
					[]model.ResourceSessionTerminationStatus{model.ResourceSessionTerminationPending, model.ResourceSessionTerminationDelivered}).Updates(updates).Error; err != nil {
					return err
				}
			}
			result.Terminations = append(result.Terminations, SessionTerminationCommand{
				SessionID: terminations[i].SessionID, CommandRevision: terminations[i].CommandRevision,
				ReasonCode: terminations[i].ReasonCode, Reason: terminations[i].Reason,
			})
		}
		return nil
	})
	return result, err
}

func (s *SessionAuthorizationService) revalidateSession(tx *gorm.DB, session *model.ResourceSession, now time.Time, includePermissions bool) (*SessionAuthorizationPermission, string, error) {
	if !includePermissions {
		return nil, sessionAuthorizationDisabledReasonCode, nil
	}
	if !session.ValidUntil.After(now) {
		return nil, sessionAuthorizationExpiredCode, nil
	}
	chain, err := loadTenantResourceChain(tx, session.TenantID, session.TenantResourceID, now, true)
	if err != nil {
		return nil, sessionAuthorizationInvalidCode, nil
	}
	if chain.Resource.VisibilityState != model.TenantResourceVisible || chain.Source.ID != session.TenantResourceSourceID ||
		chain.Target.ID != session.TargetRevisionID || chain.Allocation.ID != session.AllocationID || chain.Item.ID != session.AllocationItemID ||
		chain.Target.AccessTechnicalResourceID != session.AccessTechnicalResourceID {
		return nil, sessionAuthorizationInvalidCode, nil
	}
	var grant model.TenantAccessGrant
	if err := tx.Where("id = ? AND tenant_id = ? AND tenant_resource_id = ? AND revision = ? AND status = ? AND valid_from <= ? AND (expires_at IS NULL OR expires_at > ?)",
		session.GrantID, session.TenantID, session.TenantResourceID, session.GrantRevision, model.TenantAccessGrantEnabled, now, now).First(&grant).Error; err != nil {
		return nil, sessionAuthorizationInvalidCode, nil
	}
	var actions []string
	if json.Unmarshal([]byte(grant.Actions), &actions) != nil || !containsString(actions, session.Action) {
		return nil, sessionAuthorizationInvalidCode, nil
	}
	if grant.SubjectType == model.TenantAccessGrantSubjectUser {
		if grant.SubjectUserID == nil || *grant.SubjectUserID != session.UserID {
			return nil, sessionAuthorizationInvalidCode, nil
		}
	} else {
		if grant.SubjectGroupID == nil {
			return nil, sessionAuthorizationInvalidCode, nil
		}
		var count int64
		if err := tx.Table("group_member AS member").Joins(`JOIN "group" AS subject_group ON subject_group.id = member.group_id`).
			Where("member.group_id = ? AND member.user_id = ? AND subject_group.tenant_id = ?", *grant.SubjectGroupID, session.UserID, session.TenantID).Count(&count).Error; err != nil {
			return nil, "", err
		}
		if count != 1 {
			return nil, sessionAuthorizationInvalidCode, nil
		}
	}
	var membership model.TenantMembership
	if err := tx.Where("id = ? AND tenant_id = ? AND user_id = ? AND enabled = ? AND (expires_at IS NULL OR expires_at > ?)",
		session.TenantMembershipID, session.TenantID, session.UserID, true, now).First(&membership).Error; err != nil {
		return nil, sessionAuthorizationInvalidCode, nil
	}
	var user model.User
	if err := tx.Where("id = ? AND enabled = ?", session.UserID, true).First(&user).Error; err != nil {
		return nil, sessionAuthorizationInvalidCode, nil
	}
	var device model.Node
	if err := tx.Where("id = ? AND user_id = ? AND type = ?", session.DeviceID, session.UserID, model.NodeTypeDesktop).First(&device).Error; err != nil ||
		device.LastHeartbeat == nil || device.LastHeartbeat.Before(now.Add(-resourceSessionDeviceHeartbeatTTL)) || device.HeadscaleNodeID == 0 {
		return nil, sessionAuthorizationInvalidCode, nil
	}
	deviceValidUntil := device.LastHeartbeat.UTC().Add(resourceSessionDeviceHeartbeatTTL)
	validUntil := calculateResourceSessionValidUntil(now, session.StartedAt, grant.MaxSessionSeconds,
		membership.ExpiresAt, chain.Allocation.ExpiresAt, &chain.Observation.LeaseExpiresAt,
		&chain.Evidence.LeaseExpiresAt, grant.ExpiresAt, &deviceValidUntil, namespaceLeaseDeadline(chain))
	if !validUntil.After(now) {
		return nil, sessionAuthorizationExpiredCode, nil
	}
	if !validUntil.Equal(session.ValidUntil.UTC()) {
		updated := tx.Model(&model.ResourceSession{}).
			Where("id = ? AND row_version = ? AND status IN ?", session.ID, session.RowVersion,
				[]model.ResourceSessionStatus{model.ResourceSessionAuthorizing, model.ResourceSessionActive}).
			Updates(map[string]any{"valid_until": validUntil, "row_version": gorm.Expr("row_version + 1")})
		if updated.Error != nil {
			return nil, "", mapResourceSessionConstraint(updated.Error)
		}
		if updated.RowsAffected != 1 {
			return nil, "", ErrResourceSessionVersionConflict
		}
		session.ValidUntil = validUntil
		session.RowVersion++
	}
	var target SessionAuthorizationTarget
	if err := json.Unmarshal([]byte(chain.Target.TargetSnapshot), &target); err != nil {
		return nil, sessionAuthorizationInvalidCode, nil
	}
	var sshUsers []string
	if chain.Resource.Type == model.TenantResourceContainerSSH {
		if json.Unmarshal([]byte(chain.Resource.SSHUsers), &sshUsers) != nil || len(sshUsers) == 0 {
			return nil, sessionAuthorizationInvalidCode, nil
		}
	}
	return &SessionAuthorizationPermission{
		SessionID: session.ID, TenantID: session.TenantID, ResourceID: session.TenantResourceID,
		SourceID: session.TenantResourceSourceID, TargetRevisionID: session.TargetRevisionID,
		UserID: session.UserID, UserName: user.Name, DeviceID: session.DeviceID, DeviceHeadscaleNodeID: device.HeadscaleNodeID,
		ResourceType: chain.Resource.Type, Action: session.Action, AllocationID: session.AllocationID,
		GrantID: session.GrantID, GrantRevision: session.GrantRevision,
		AuthorizationRevision: session.AuthorizationRevision, ValidUntil: validUntil, SSHUsers: sshUsers, Target: target,
	}, "", nil
}

func endInvalidResourceSession(tx *gorm.DB, session *model.ResourceSession, reasonCode string, now time.Time) error {
	reason := strings.ToLower(strings.ReplaceAll(reasonCode, "_", " "))
	requestID := "session-revalidate:" + session.ID
	updated := tx.Model(&model.ResourceSession{}).Where("id = ? AND row_version = ? AND status IN ?", session.ID, session.RowVersion,
		[]model.ResourceSessionStatus{model.ResourceSessionAuthorizing, model.ResourceSessionActive}).
		Updates(map[string]any{"status": model.ResourceSessionEnding, "close_reason": reasonCode, "row_version": gorm.Expr("row_version + 1")})
	if updated.Error != nil {
		return mapResourceSessionConstraint(updated.Error)
	}
	if updated.RowsAffected != 1 {
		return nil
	}
	session.Status = model.ResourceSessionEnding
	session.CloseReason = reasonCode
	session.RowVersion++
	if err := createSessionTermination(tx, session, reasonCode, reason, now); err != nil {
		return err
	}
	if err := AppendTenantManagementOutbox(tx, TenantManagementOutboxInput{
		EventType: "resource_session.ending", AggregateType: "resource_session", AggregateID: session.ID,
		AggregateRevision: session.RowVersion, TenantID: session.TenantID, ResourceID: session.TenantResourceID,
		GrantID: session.GrantID, SessionID: session.ID, Status: string(session.Status), RowVersion: session.RowVersion,
		ReasonCode: reasonCode, RequestID: requestID, AvailableAt: now,
	}); err != nil {
		return err
	}
	return appendTenantAuthorizationSystemAudit(tx, session.TenantID, requestID, "end_invalid_resource_session",
		"resource_session", session.ID, session.ID, map[string]any{
			"reason_code": reasonCode, "resource_id": session.TenantResourceID, "grant_id": session.GrantID,
			"row_version": session.RowVersion,
		})
}

func (s *SessionAuthorizationService) AcknowledgeTerminations(ctx context.Context, technicalResourceID string, acknowledgements []SessionTerminationAckInput) error {
	if s == nil || s.db == nil || validateRequired("technical_resource_id", strings.TrimSpace(technicalResourceID), 36) != nil ||
		len(acknowledgements) > sessionAuthorizationReportMaxItems {
		return ErrSessionAuthorizationInvalidInput
	}
	now := s.now().UTC()
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, ack := range acknowledgements {
			if validateRequired("session_id", strings.TrimSpace(ack.SessionID), 36) != nil || ack.CommandRevision <= 0 {
				return ErrSessionAuthorizationInvalidInput
			}
			var termination model.ResourceSessionTermination
			if err := tx.Table("resource_session_termination AS termination").Select("termination.*").
				Joins("JOIN resource_session AS session ON session.id = termination.session_id").
				Where("termination.session_id = ? AND termination.command_revision = ? AND session.access_technical_resource_id = ?",
					ack.SessionID, ack.CommandRevision, technicalResourceID).First(&termination).Error; err != nil {
				return ErrSessionAuthorizationSourceDenied
			}
			if termination.Status != model.ResourceSessionTerminationAcknowledged {
				if err := tx.Model(&model.ResourceSessionTermination{}).Where("id = ? AND status IN ?", termination.ID,
					[]model.ResourceSessionTerminationStatus{model.ResourceSessionTerminationPending, model.ResourceSessionTerminationDelivered}).
					Updates(map[string]any{"status": model.ResourceSessionTerminationAcknowledged, "acknowledged_at": now, "next_attempt_at": nil}).Error; err != nil {
					return err
				}
			}
			var session model.ResourceSession
			if err := tx.Where("id = ? AND access_technical_resource_id = ?", ack.SessionID, technicalResourceID).First(&session).Error; err != nil {
				return ErrSessionAuthorizationSourceDenied
			}
			if session.DisconnectAcknowledgedAt == nil && (session.Status == model.ResourceSessionEnding || session.Status == model.ResourceSessionActive || session.Status == model.ResourceSessionAuthorizing) {
				if err := tx.Model(&model.ResourceSession{}).Where("id = ? AND row_version = ?", session.ID, session.RowVersion).
					Updates(map[string]any{"disconnect_acknowledged_at": now, "row_version": gorm.Expr("row_version + 1")}).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (s *SessionAuthorizationService) AcceptEvents(ctx context.Context, technicalResourceID string, events []SessionEventInput) ([]SessionEventAck, error) {
	if s == nil || s.db == nil || validateRequired("technical_resource_id", strings.TrimSpace(technicalResourceID), 36) != nil ||
		len(events) > sessionAuthorizationReportMaxItems {
		return nil, ErrSessionAuthorizationInvalidInput
	}
	now := s.now().UTC()
	acks := make([]SessionEventAck, 0, len(events))
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, input := range events {
			if uuid.Validate(input.EventID) != nil || uuid.Validate(input.SessionID) != nil || input.SourceSequence <= 0 ||
				!validResourceSessionEventType(input.EventType) || input.OccurredAt.IsZero() || input.OccurredAt.After(now.Add(5*time.Minute)) || len(input.ResultCode) > 100 {
				return ErrSessionAuthorizationInvalidInput
			}
			var replay model.ResourceSessionEvent
			if err := tx.Where("source_technical_resource_id = ? AND event_id = ?", technicalResourceID, input.EventID).First(&replay).Error; err == nil {
				if replay.SessionID != input.SessionID || replay.SourceSequence != input.SourceSequence || replay.EventType != input.EventType {
					return ErrSessionAuthorizationEventConflict
				}
				acks = append(acks, SessionEventAck{EventID: input.EventID, ResultCode: "SESSION_EVENT_REPLAYED", Replay: true})
				continue
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			var sequenceOwner model.ResourceSessionEvent
			if err := tx.Where("session_id = ? AND source_sequence = ?", input.SessionID, input.SourceSequence).First(&sequenceOwner).Error; err == nil {
				return ErrSessionAuthorizationEventConflict
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			var session model.ResourceSession
			if err := tx.Where("id = ? AND access_technical_resource_id = ?", input.SessionID, technicalResourceID).First(&session).Error; err != nil {
				return ErrSessionAuthorizationSourceDenied
			}
			payload, _ := json.Marshal(map[string]string{"result_code": input.ResultCode})
			event := model.ResourceSessionEvent{
				ID: uuid.NewString(), EventID: input.EventID, SourceTechnicalResourceID: technicalResourceID,
				SessionID: input.SessionID, SourceSequence: input.SourceSequence, EventType: input.EventType,
				OccurredAt: input.OccurredAt.UTC(), ReceivedAt: now, ResultCode: input.ResultCode, Payload: string(payload),
			}
			if err := tx.Create(&event).Error; err != nil {
				return err
			}
			if err := projectResourceSessionEvent(tx, &session, input, now); err != nil {
				return err
			}
			acks = append(acks, SessionEventAck{EventID: input.EventID, ResultCode: "SESSION_EVENT_ACCEPTED"})
		}
		return nil
	})
	return acks, err
}

func projectResourceSessionEvent(tx *gorm.DB, session *model.ResourceSession, input SessionEventInput, now time.Time) error {
	if session.Status == model.ResourceSessionEnded || session.Status == model.ResourceSessionTerminated || session.Status == model.ResourceSessionRejected {
		return nil
	}
	updates := map[string]any{}
	switch input.EventType {
	case model.ResourceSessionEventAccepted:
		return nil
	case model.ResourceSessionEventConnected:
		if session.Status != model.ResourceSessionAuthorizing {
			return nil
		}
		updates["status"] = model.ResourceSessionActive
		updates["connected_at"] = input.OccurredAt.UTC()
	case model.ResourceSessionEventEnded:
		if session.Status == model.ResourceSessionAuthorizing {
			updates["status"] = model.ResourceSessionRejected
		} else {
			updates["status"] = model.ResourceSessionEnded
		}
		updates["ended_at"] = input.OccurredAt.UTC()
		updates["result"] = input.ResultCode
		updates["close_reason"] = input.ResultCode
	case model.ResourceSessionEventFailed:
		if session.Status == model.ResourceSessionAuthorizing {
			updates["status"] = model.ResourceSessionRejected
		} else {
			updates["status"] = model.ResourceSessionEnded
		}
		updates["ended_at"] = input.OccurredAt.UTC()
		updates["result"] = input.ResultCode
		updates["close_reason"] = input.ResultCode
	case model.ResourceSessionEventTerminated:
		if session.Status == model.ResourceSessionAuthorizing {
			if err := tx.Model(&model.ResourceSession{}).Where("id = ? AND row_version = ?", session.ID, session.RowVersion).
				Updates(map[string]any{"status": model.ResourceSessionEnding, "row_version": gorm.Expr("row_version + 1")}).Error; err != nil {
				return err
			}
			session.RowVersion++
			session.Status = model.ResourceSessionEnding
		}
		updates["status"] = model.ResourceSessionTerminated
		updates["ended_at"] = input.OccurredAt.UTC()
		updates["result"] = input.ResultCode
		updates["close_reason"] = input.ResultCode
	default:
		return ErrSessionAuthorizationInvalidInput
	}
	updates["row_version"] = gorm.Expr("row_version + 1")
	updated := tx.Model(&model.ResourceSession{}).Where("id = ? AND row_version = ?", session.ID, session.RowVersion).Updates(updates)
	if updated.Error != nil {
		return mapResourceSessionConstraint(updated.Error)
	}
	if updated.RowsAffected != 1 {
		return ErrResourceSessionVersionConflict
	}
	_ = now
	return nil
}

func validResourceSessionEventType(value model.ResourceSessionEventType) bool {
	switch value {
	case model.ResourceSessionEventAccepted, model.ResourceSessionEventConnected, model.ResourceSessionEventEnded,
		model.ResourceSessionEventTerminated, model.ResourceSessionEventFailed:
		return true
	default:
		return false
	}
}

func (p SessionAuthorizationPermission) String() string {
	return fmt.Sprintf("%s:%s:%d", p.SessionID, p.ResourceID, p.AuthorizationRevision)
}
