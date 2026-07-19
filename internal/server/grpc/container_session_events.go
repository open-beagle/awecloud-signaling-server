package grpc

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

func (s *AgentServiceServer) handleContainerSessionEvents(ctx context.Context, nodeID uint64, events []*pb.ContainerSSHSessionEvent) []string {
	acks := make([]string, 0, len(events))
	for _, event := range events {
		if event == nil || event.EventId == "" || event.SessionId == "" {
			continue
		}
		var err error
		switch event.Phase {
		case "started":
			err = saveContainerSessionStarted(ctx, nodeID, event)
		case "ended":
			err = saveContainerSessionEnded(ctx, nodeID, event)
		default:
			err = errors.New("unknown ContainerSSH session phase")
		}
		if err != nil {
			logger.Warnf("拒绝 ContainerSSH Session 事件: node_id=%d session_id=%s phase=%s err=%v", nodeID, event.SessionId, event.Phase, err)
			continue
		}
		acks = append(acks, event.EventId)
	}
	return acks
}

func saveContainerSessionStarted(ctx context.Context, nodeID uint64, event *pb.ContainerSSHSessionEvent) error {
	if _, err := uuid.Parse(event.SessionId); err != nil || event.UserId == 0 || event.DeviceNodeId == 0 {
		return errors.New("invalid ContainerSSH session identity")
	}
	var existing model.ContainerSession
	if err := db.DB.WithContext(ctx).First(&existing, "id = ?", event.SessionId).Error; err == nil {
		if existing.AgentNodeID == nodeID && existing.ResourceID == event.ResourceId && existing.UserID == event.UserId {
			return nil
		}
		return errors.New("session ID belongs to different context")
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	now := time.Now()
	var resource model.Resource
	if err := db.DB.WithContext(ctx).Where("id = ? AND type = ? AND agent_node_id = ? AND target_revision = ? AND state IN ?", event.ResourceId, model.ResourceTypeContainerSSH, nodeID, event.TargetRevision, []model.ResourceState{model.ResourceStateAvailable, model.ResourceStateDegraded}).First(&resource).Error; err != nil {
		return errors.New("resource target is no longer current")
	}
	var target model.ResourceTarget
	if err := db.DB.WithContext(ctx).Where("resource_id = ? AND revision = ? AND agent_node_id = ? AND pod_uid = ? AND container_name = ? AND ready = ?", resource.ID, event.TargetRevision, nodeID, event.PodUid, event.ContainerName, true).First(&target).Error; err != nil {
		return errors.New("runtime target does not match")
	}
	var user model.User
	if err := db.DB.WithContext(ctx).Where("id = ? AND name = ? AND enabled = ?", event.UserId, event.UserName, true).First(&user).Error; err != nil {
		return errors.New("user identity does not match")
	}
	var membership model.TenantMembership
	if err := db.DB.WithContext(ctx).Where("tenant_id = ? AND user_id = ? AND enabled = ? AND (expires_at IS NULL OR expires_at > ?)", resource.TenantID, user.ID, true, now).First(&membership).Error; err != nil {
		return errors.New("tenant membership is not active")
	}
	if !hasActiveContainerShellGrant(ctx, resource, user.ID, event.GrantRevision, now) {
		return errors.New("shell grant is not active")
	}
	var device model.Node
	if err := db.DB.WithContext(ctx).Where("headscale_node_id = ? AND user_id = ? AND type = ?", event.DeviceNodeId, user.ID, model.NodeTypeDesktop).First(&device).Error; err != nil {
		return errors.New("Desktop device identity does not match")
	}
	startedAt := now
	if event.OccurredAt > 0 {
		startedAt = time.Unix(event.OccurredAt, 0)
	}
	session := model.ContainerSession{
		ID: event.SessionId, TenantID: resource.TenantID, UserID: user.ID, DeviceID: device.ID,
		ResourceID: resource.ID, WorkspaceID: resource.ExternalWorkspaceID,
		GrantRevision: event.GrantRevision, TargetRevision: event.TargetRevision,
		PodUID: target.PodUID, ContainerName: target.ContainerName, AgentNodeID: nodeID,
		Status: model.ContainerSessionActive, StartedAt: startedAt,
	}
	return db.DB.WithContext(ctx).Create(&session).Error
}

func hasActiveContainerShellGrant(ctx context.Context, resource model.Resource, userID uint64, revision int64, now time.Time) bool {
	var grants []model.AccessGrant
	if err := db.DB.WithContext(ctx).Where("resource_id = ? AND tenant_id = ? AND subject_type IN ? AND revision = ? AND status = ? AND valid_from <= ? AND expires_at > ?", resource.ID, resource.TenantID, []string{"user", "group"}, revision, "enabled", now, now).Find(&grants).Error; err != nil {
		return false
	}
	for _, grant := range grants {
		if !containsAction(parseJSONStringArray(grant.Actions), "shell") {
			continue
		}
		if grant.SubjectType == "user" && grant.SubjectUserID == userID {
			return true
		}
		if grant.SubjectType != "group" || grant.SubjectGroupID == nil {
			continue
		}
		var count int64
		if err := db.DB.WithContext(ctx).Table("group_member").
			Joins("JOIN `group` ON `group`.id = group_member.group_id").
			Where("group_member.group_id = ? AND group_member.user_id = ? AND `group`.tenant_id = ?", *grant.SubjectGroupID, userID, resource.TenantID).
			Count(&count).Error; err == nil && count > 0 {
			return true
		}
	}
	return false
}

func saveContainerSessionEnded(ctx context.Context, nodeID uint64, event *pb.ContainerSSHSessionEvent) error {
	var session model.ContainerSession
	if err := db.DB.WithContext(ctx).Where("id = ? AND agent_node_id = ?", event.SessionId, nodeID).First(&session).Error; err != nil {
		return errors.New("active session does not exist")
	}
	if session.Status != model.ContainerSessionActive {
		if session.Status == model.ContainerSessionRevoked && session.DisconnectAcknowledgedAt == nil {
			acknowledgedAt := time.Now()
			return db.DB.WithContext(ctx).Model(&session).Update("disconnect_acknowledged_at", acknowledgedAt).Error
		}
		return nil
	}
	endedAt := time.Now()
	if event.OccurredAt > 0 {
		endedAt = time.Unix(event.OccurredAt, 0)
	}
	return db.DB.WithContext(ctx).Model(&session).Updates(map[string]interface{}{
		"status": model.ContainerSessionEnded, "ended_at": endedAt,
		"result": event.Result, "close_reason": event.CloseReason,
	}).Error
}
