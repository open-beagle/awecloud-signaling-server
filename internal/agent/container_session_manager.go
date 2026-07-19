package agent

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

type containerActiveSession struct {
	cancel         context.CancelFunc
	reason         string
	userName       string
	resourceID     string
	targetRevision int64
	grantRevision  int64
}

type containerPendingEvent struct {
	event *pb.ContainerSSHSessionEvent
}

type ContainerSessionManager struct {
	mu     sync.Mutex
	active map[string]*containerActiveSession
	events map[string]*containerPendingEvent
}

func NewContainerSessionManager() *ContainerSessionManager {
	return &ContainerSessionManager{active: make(map[string]*containerActiveSession), events: make(map[string]*containerPendingEvent)}
}

func (m *ContainerSessionManager) Begin(parent context.Context, identity *PeerIdentity, permission *ContainerSSHUserPermission) (string, context.Context) {
	sessionID := uuid.NewString()
	ctx, cancel := context.WithCancel(parent)
	now := time.Now()
	event := &pb.ContainerSSHSessionEvent{
		EventId: uuid.NewString(), SessionId: sessionID, Phase: "started",
		UserId: permission.UserID, UserName: identity.UserName, DeviceNodeId: identity.NodeID,
		ResourceId: permission.ResourceID, TargetRevision: permission.TargetRevision,
		GrantRevision: permission.GrantRevision, PodUid: permission.PodUID,
		ContainerName: permission.ContainerName, OccurredAt: now.Unix(),
	}
	m.mu.Lock()
	m.active[sessionID] = &containerActiveSession{
		cancel: cancel, userName: identity.UserName, resourceID: permission.ResourceID,
		targetRevision: permission.TargetRevision, grantRevision: permission.GrantRevision,
	}
	m.events[event.EventId] = &containerPendingEvent{event: event}
	m.mu.Unlock()
	return sessionID, ctx
}

func (m *ContainerSessionManager) ReconcilePermissions(permissions *PermissionCache) {
	if permissions == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, active := range m.active {
		permission, allowed := permissions.CheckContainerSSHAccess(active.userName, active.resourceID)
		if !allowed || permission.GrantRevision != active.grantRevision {
			active.reason = "grant_revoked"
			active.cancel()
		} else if permission.TargetRevision != active.targetRevision {
			active.reason = "target_gone"
			active.cancel()
		}
	}
}

func (m *ContainerSessionManager) End(sessionID, result, reason string) {
	m.mu.Lock()
	active, ok := m.active[sessionID]
	if !ok {
		m.mu.Unlock()
		return
	}
	delete(m.active, sessionID)
	active.cancel()
	if active.reason != "" {
		result, reason = "revoked", active.reason
	}
	event := &pb.ContainerSSHSessionEvent{
		EventId: uuid.NewString(), SessionId: sessionID, Phase: "ended",
		OccurredAt: time.Now().Unix(), Result: result, CloseReason: reason,
	}
	m.events[event.EventId] = &containerPendingEvent{event: event}
	m.mu.Unlock()
}

func (m *ContainerSessionManager) Disconnect(sessionIDs []string, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, sessionID := range sessionIDs {
		if active := m.active[sessionID]; active != nil {
			active.reason = reason
			active.cancel()
		}
	}
}

func (m *ContainerSessionManager) EventsForHeartbeat() []*pb.ContainerSSHSessionEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]*pb.ContainerSSHSessionEvent, 0, len(m.events))
	for _, pending := range m.events {
		result = append(result, pending.event)
	}
	return result
}

func (m *ContainerSessionManager) AckEvents(eventIDs []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, eventID := range eventIDs {
		delete(m.events, eventID)
	}
}
