package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

const (
	resourceSessionEventStateVersion  = 1
	resourceSessionHeartbeatBatchSize = 100
	resourceSessionIdleGrace          = 30 * time.Second
)

type containerActiveSession struct {
	ctx                   context.Context
	cancel                context.CancelFunc
	expiryTimer           *time.Timer
	idleTimer             *time.Timer
	connections           int
	idleResult            string
	idleReason            string
	reason                string
	userName              string
	resourceID            string
	targetRevision        int64
	grantRevision         int64
	authorizationRevision int64
	validUntil            time.Time
	v2                    bool
}

type containerPendingEvent struct {
	event *pb.ContainerSSHSessionEvent
}

type resourceSessionEventState struct {
	Version         int                             `json:"version"`
	Events          []persistedResourceSessionEvent `json:"events"`
	NextSequence    map[string]int64                `json:"next_sequence"`
	TerminationAcks []persistedTerminationAck       `json:"termination_acks"`
}

type persistedResourceSessionEvent struct {
	EventID        string `json:"event_id"`
	SessionID      string `json:"session_id"`
	SourceSequence int64  `json:"source_sequence"`
	EventType      string `json:"event_type"`
	OccurredAt     string `json:"occurred_at"`
	ResultCode     string `json:"result_code,omitempty"`
}

type persistedTerminationAck struct {
	SessionID       string `json:"session_id"`
	CommandRevision int64  `json:"command_revision"`
}

type ContainerSessionManager struct {
	mu              sync.Mutex
	active          map[string]*containerActiveSession
	events          map[string]*containerPendingEvent
	resourceEvents  map[string]*pb.ResourceSessionEventV2
	nextSequence    map[string]int64
	terminationAcks map[string]*pb.ResourceSessionTerminationAckV2
	statePath       string
	idleGrace       time.Duration
}

func NewContainerSessionManager() *ContainerSessionManager {
	return newContainerSessionManager("")
}

func NewPersistentContainerSessionManager(stateDir string) (*ContainerSessionManager, error) {
	if stateDir == "" {
		return nil, fmt.Errorf("resource session event state directory is required")
	}
	expanded, err := expandResourceSessionStatePath(stateDir)
	if err != nil {
		return nil, err
	}
	manager := newContainerSessionManager(filepath.Join(expanded, "resource_session_events_v2.json"))
	if err := manager.load(); err != nil {
		return nil, err
	}
	return manager, nil
}

func newContainerSessionManager(statePath string) *ContainerSessionManager {
	return &ContainerSessionManager{
		active:          make(map[string]*containerActiveSession),
		events:          make(map[string]*containerPendingEvent),
		resourceEvents:  make(map[string]*pb.ResourceSessionEventV2),
		nextSequence:    make(map[string]int64),
		terminationAcks: make(map[string]*pb.ResourceSessionTerminationAckV2),
		statePath:       statePath,
		idleGrace:       resourceSessionIdleGrace,
	}
}

// Begin is the unchanged v1 ContainerSSH session path.
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

// BeginV2 registers the exact ResourceSession ID created by Server. The
// connected event is durable before the caller starts Kubernetes exec.
func (m *ContainerSessionManager) BeginV2(parent context.Context, permission *pb.ResourceSessionPermissionV2) (context.Context, error) {
	if m == nil || permission == nil || permission.SessionId == "" || permission.ValidUntil == nil || !permission.ValidUntil.IsValid() {
		return nil, fmt.Errorf("invalid resource session permission")
	}
	validUntil := permission.ValidUntil.AsTime().UTC()
	if !validUntil.After(time.Now().UTC()) {
		return nil, ErrSessionSnapshotExpired
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if active := m.active[permission.SessionId]; active != nil {
		if !active.v2 || active.userName != permission.UserName || active.resourceID != permission.ResourceId ||
			active.grantRevision != permission.GrantRevision || active.authorizationRevision != permission.AuthorizationRevision {
			return nil, fmt.Errorf("resource session identity changed")
		}
		if active.reason != "" || active.ctx.Err() != nil {
			return nil, fmt.Errorf("resource session is ending")
		}
		if active.idleTimer != nil {
			active.idleTimer.Stop()
			active.idleTimer = nil
			active.idleResult = ""
			active.idleReason = ""
		}
		active.connections++
		return active.ctx, nil
	}
	if m.nextSequence[permission.SessionId] > 0 {
		return nil, fmt.Errorf("resource session has already started")
	}
	ctx, cancel := context.WithCancel(parent)
	if err := m.queueResourceEventLocked(permission.SessionId, "connected", "", true); err != nil {
		cancel()
		return nil, err
	}
	active := &containerActiveSession{
		ctx: ctx, cancel: cancel, connections: 1, userName: permission.UserName, resourceID: permission.ResourceId,
		grantRevision: permission.GrantRevision, authorizationRevision: permission.AuthorizationRevision,
		validUntil: validUntil, v2: true,
	}
	m.active[permission.SessionId] = active
	m.resetV2ExpiryTimerLocked(permission.SessionId, active, validUntil)
	return ctx, nil
}

func (m *ContainerSessionManager) resetV2ExpiryTimerLocked(sessionID string, active *containerActiveSession, validUntil time.Time) {
	if active.expiryTimer != nil {
		active.expiryTimer.Stop()
	}
	active.validUntil = validUntil.UTC()
	deadline := active.validUntil
	delay := time.Until(active.validUntil)
	if delay < 0 {
		delay = 0
	}
	active.expiryTimer = time.AfterFunc(delay, func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		current := m.active[sessionID]
		if current != active || !current.v2 {
			return
		}
		if !current.validUntil.Equal(deadline) {
			return
		}
		if current.reason == "" {
			current.reason = "SESSION_AUTHORIZATION_EXPIRED"
		}
		current.cancel()
		if current.connections == 0 {
			_ = m.finalizeV2Locked(sessionID, current, "ended", "context_canceled")
		}
	})
}

func (m *ContainerSessionManager) FailV2(sessionID, resultCode string) error {
	if m == nil || sessionID == "" || resultCode == "" {
		return fmt.Errorf("invalid failed resource session event")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, active := m.active[sessionID]; active {
		return fmt.Errorf("resource session is already active")
	}
	if m.nextSequence[sessionID] > 0 {
		return nil
	}
	return m.queueResourceEventLocked(sessionID, "failed", resultCode, true)
}

func (m *ContainerSessionManager) ReconcilePermissions(permissions *PermissionCache) {
	if permissions == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, active := range m.active {
		if active.v2 {
			continue
		}
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

func (m *ContainerSessionManager) ReconcileV2(authorizations *SessionAuthorizationCache, now time.Time) {
	if m == nil || authorizations == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for sessionID, active := range m.active {
		if !active.v2 {
			continue
		}
		permission, allowed := authorizations.Permission(sessionID, now)
		if !allowed || permission.AuthorizationRevision != active.authorizationRevision || permission.GrantRevision != active.grantRevision ||
			permission.ResourceId != active.resourceID || permission.UserName != active.userName {
			if active.reason == "" {
				active.reason = "SESSION_AUTHORIZATION_REVOKED"
			}
			active.cancel()
			if active.connections == 0 {
				_ = m.finalizeV2Locked(sessionID, active, "ended", "context_canceled")
			}
			continue
		}
		validUntil := permission.ValidUntil.AsTime().UTC()
		if active.reason == "" && !validUntil.Equal(active.validUntil) {
			m.resetV2ExpiryTimerLocked(sessionID, active, validUntil)
		}
	}
	changed := false
	for sessionID := range m.nextSequence {
		if _, active := m.active[sessionID]; active || m.hasPendingResourceEventLocked(sessionID) {
			continue
		}
		if _, allowed := authorizations.Permission(sessionID, now); !allowed {
			delete(m.nextSequence, sessionID)
			changed = true
		}
	}
	if changed {
		_ = m.persistLocked()
	}
}

func (m *ContainerSessionManager) hasPendingResourceEventLocked(sessionID string) bool {
	for _, event := range m.resourceEvents {
		if event.SessionId == sessionID {
			return true
		}
	}
	return false
}

func (m *ContainerSessionManager) ApplyTerminationCommands(commands []*pb.ResourceSessionTerminationCommandV2) error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	nextAcks := make(map[string]*pb.ResourceSessionTerminationAckV2, len(commands))
	for _, command := range commands {
		if command == nil || command.SessionId == "" || command.CommandRevision <= 0 {
			return fmt.Errorf("invalid resource session termination command")
		}
		key := terminationAckKey(command.SessionId, command.CommandRevision)
		nextAcks[key] = &pb.ResourceSessionTerminationAckV2{SessionId: command.SessionId, CommandRevision: command.CommandRevision}
		if active := m.active[command.SessionId]; active != nil && active.v2 {
			active.reason = command.ReasonCode
			active.cancel()
			if active.connections == 0 {
				_ = m.finalizeV2Locked(command.SessionId, active, "ended", "context_canceled")
			}
		}
	}
	previous := m.terminationAcks
	m.terminationAcks = nextAcks
	if err := m.persistLocked(); err != nil {
		m.terminationAcks = previous
		return err
	}
	return nil
}

func (m *ContainerSessionManager) End(sessionID, result, reason string) {
	_ = m.end(sessionID, result, reason, false)
}

func (m *ContainerSessionManager) EndV2(sessionID, result, reason string) error {
	return m.end(sessionID, result, reason, false)
}

func (m *ContainerSessionManager) EndV2WithIdleGrace(sessionID, result, reason string) error {
	return m.end(sessionID, result, reason, true)
}

func (m *ContainerSessionManager) end(sessionID, result, reason string, allowIdleGrace bool) error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	active, ok := m.active[sessionID]
	if !ok {
		m.mu.Unlock()
		return nil
	}
	if active.v2 {
		if active.connections <= 0 {
			m.mu.Unlock()
			return nil
		}
		active.connections--
		if active.connections > 0 {
			m.mu.Unlock()
			return nil
		}
		if !allowIdleGrace || active.reason != "" || result == "failed" || result == "rejected" || m.idleGrace <= 0 {
			err := m.finalizeV2Locked(sessionID, active, result, reason)
			m.mu.Unlock()
			return err
		}
		active.idleResult = result
		active.idleReason = reason
		var timer *time.Timer
		timer = time.AfterFunc(m.idleGrace, func() {
			m.mu.Lock()
			defer m.mu.Unlock()
			current := m.active[sessionID]
			if current != active || current.idleTimer != timer || current.connections != 0 {
				return
			}
			current.idleTimer = nil
			_ = m.finalizeV2Locked(sessionID, current, current.idleResult, current.idleReason)
		})
		active.idleTimer = timer
		m.mu.Unlock()
		return nil
	}
	delete(m.active, sessionID)
	if active.expiryTimer != nil {
		active.expiryTimer.Stop()
	}
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
	return nil
}

func (m *ContainerSessionManager) finalizeV2Locked(sessionID string, active *containerActiveSession, result, reason string) error {
	if m.active[sessionID] != active {
		return nil
	}
	delete(m.active, sessionID)
	if active.expiryTimer != nil {
		active.expiryTimer.Stop()
	}
	if active.idleTimer != nil {
		active.idleTimer.Stop()
		active.idleTimer = nil
	}
	active.cancel()
	eventType := "ended"
	resultCode := reason
	if active.reason != "" {
		eventType, resultCode = "terminated", active.reason
	} else if result == "failed" || result == "rejected" {
		eventType = "failed"
	}
	return m.queueResourceEventLocked(sessionID, eventType, resultCode, false)
}

func (m *ContainerSessionManager) Disconnect(sessionIDs []string, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, sessionID := range sessionIDs {
		if active := m.active[sessionID]; active != nil && !active.v2 {
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

func (m *ContainerSessionManager) ResourceEventsForHeartbeat() []*pb.ResourceSessionEventV2 {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]*pb.ResourceSessionEventV2, 0, len(m.resourceEvents))
	for _, event := range m.resourceEvents {
		result = append(result, proto.Clone(event).(*pb.ResourceSessionEventV2))
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].SessionId == result[j].SessionId {
			return result[i].SourceSequence < result[j].SourceSequence
		}
		return result[i].SessionId < result[j].SessionId
	})
	if len(result) > resourceSessionHeartbeatBatchSize {
		result = result[:resourceSessionHeartbeatBatchSize]
	}
	return result
}

func (m *ContainerSessionManager) TerminationAcksForHeartbeat() []*pb.ResourceSessionTerminationAckV2 {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]*pb.ResourceSessionTerminationAckV2, 0, len(m.terminationAcks))
	for _, ack := range m.terminationAcks {
		result = append(result, proto.Clone(ack).(*pb.ResourceSessionTerminationAckV2))
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].SessionId == result[j].SessionId {
			return result[i].CommandRevision < result[j].CommandRevision
		}
		return result[i].SessionId < result[j].SessionId
	})
	if len(result) > resourceSessionHeartbeatBatchSize {
		result = result[:resourceSessionHeartbeatBatchSize]
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

func (m *ContainerSessionManager) AckResourceEvents(acks []*pb.ResourceSessionEventAckV2) error {
	if m == nil || len(acks) == 0 {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	removed := make(map[string]*pb.ResourceSessionEventV2)
	for _, ack := range acks {
		if ack == nil || ack.EventId == "" {
			continue
		}
		if event := m.resourceEvents[ack.EventId]; event != nil {
			removed[ack.EventId] = event
			delete(m.resourceEvents, ack.EventId)
		}
	}
	if err := m.persistLocked(); err != nil {
		for eventID, event := range removed {
			m.resourceEvents[eventID] = event
		}
		return err
	}
	return nil
}

func (m *ContainerSessionManager) queueResourceEventLocked(sessionID, eventType, resultCode string, durableRequired bool) error {
	next := m.nextSequence[sessionID] + 1
	event := &pb.ResourceSessionEventV2{
		EventId: uuid.NewString(), SessionId: sessionID, SourceSequence: next,
		EventType: eventType, OccurredAt: timestamppb.Now(), ResultCode: resultCode,
	}
	m.resourceEvents[event.EventId] = event
	m.nextSequence[sessionID] = next
	if err := m.persistLocked(); err != nil {
		if durableRequired {
			delete(m.resourceEvents, event.EventId)
			m.nextSequence[sessionID] = next - 1
			if next == 1 {
				delete(m.nextSequence, sessionID)
			}
		}
		return err
	}
	return nil
}

func (m *ContainerSessionManager) load() error {
	data, err := os.ReadFile(m.statePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read resource session event state: %w", err)
	}
	var state resourceSessionEventState
	if err := json.Unmarshal(data, &state); err != nil || state.Version != resourceSessionEventStateVersion {
		return fmt.Errorf("decode resource session event state")
	}
	for _, stored := range state.Events {
		occurredAt, err := time.Parse(time.RFC3339Nano, stored.OccurredAt)
		if err != nil || stored.EventID == "" || stored.SessionID == "" || stored.SourceSequence <= 0 {
			return fmt.Errorf("decode resource session event state")
		}
		m.resourceEvents[stored.EventID] = &pb.ResourceSessionEventV2{
			EventId: stored.EventID, SessionId: stored.SessionID, SourceSequence: stored.SourceSequence,
			EventType: stored.EventType, OccurredAt: timestamppb.New(occurredAt), ResultCode: stored.ResultCode,
		}
	}
	for sessionID, sequence := range state.NextSequence {
		if sessionID == "" || sequence <= 0 {
			return fmt.Errorf("decode resource session event state")
		}
		m.nextSequence[sessionID] = sequence
	}
	for _, stored := range state.TerminationAcks {
		if stored.SessionID == "" || stored.CommandRevision <= 0 {
			return fmt.Errorf("decode resource session event state")
		}
		ack := &pb.ResourceSessionTerminationAckV2{SessionId: stored.SessionID, CommandRevision: stored.CommandRevision}
		m.terminationAcks[terminationAckKey(ack.SessionId, ack.CommandRevision)] = ack
	}
	return nil
}

func (m *ContainerSessionManager) persistLocked() error {
	if m.statePath == "" {
		return nil
	}
	state := resourceSessionEventState{
		Version: resourceSessionEventStateVersion, NextSequence: make(map[string]int64, len(m.nextSequence)),
	}
	for sessionID, sequence := range m.nextSequence {
		state.NextSequence[sessionID] = sequence
	}
	for _, event := range m.resourceEvents {
		state.Events = append(state.Events, persistedResourceSessionEvent{
			EventID: event.EventId, SessionID: event.SessionId, SourceSequence: event.SourceSequence,
			EventType: event.EventType, OccurredAt: event.OccurredAt.AsTime().UTC().Format(time.RFC3339Nano), ResultCode: event.ResultCode,
		})
	}
	sort.Slice(state.Events, func(i, j int) bool {
		if state.Events[i].SessionID == state.Events[j].SessionID {
			return state.Events[i].SourceSequence < state.Events[j].SourceSequence
		}
		return state.Events[i].SessionID < state.Events[j].SessionID
	})
	for _, ack := range m.terminationAcks {
		state.TerminationAcks = append(state.TerminationAcks, persistedTerminationAck{SessionID: ack.SessionId, CommandRevision: ack.CommandRevision})
	}
	sort.Slice(state.TerminationAcks, func(i, j int) bool {
		if state.TerminationAcks[i].SessionID == state.TerminationAcks[j].SessionID {
			return state.TerminationAcks[i].CommandRevision < state.TerminationAcks[j].CommandRevision
		}
		return state.TerminationAcks[i].SessionID < state.TerminationAcks[j].SessionID
	})
	payload, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode resource session event state: %w", err)
	}
	dir := filepath.Dir(m.statePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create resource session event state directory: %w", err)
	}
	temp, err := os.CreateTemp(dir, ".resource-session-events-*")
	if err != nil {
		return fmt.Errorf("create resource session event state: %w", err)
	}
	tempPath := temp.Name()
	cleanup := func() {
		_ = temp.Close()
		_ = os.Remove(tempPath)
	}
	if err := temp.Chmod(0600); err != nil {
		cleanup()
		return err
	}
	if _, err := temp.Write(payload); err != nil {
		cleanup()
		return err
	}
	if err := temp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	if err := replaceResourceSessionStateFile(tempPath, m.statePath); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return syncResourceSessionStateDirectory(dir)
}

func terminationAckKey(sessionID string, revision int64) string {
	return fmt.Sprintf("%s\x00%d", sessionID, revision)
}

func expandResourceSessionStatePath(path string) (string, error) {
	if path != "~" && (len(path) < 2 || path[:2] != "~/") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve resource session event state home directory: %w", err)
	}
	if path == "~" {
		return home, nil
	}
	return filepath.Join(home, path[2:]), nil
}
