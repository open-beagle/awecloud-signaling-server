package agent

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestContainerSessionManagerRetainsEventsUntilAckAndCancelsSession(t *testing.T) {
	manager := NewContainerSessionManager()
	permission := &ContainerSSHUserPermission{
		UserID: 10, ResourceID: "resource-a", PodUID: "pod-a", ContainerName: "workspace",
		TargetRevision: 3, GrantRevision: 4,
	}
	sessionID, sessionCtx := manager.Begin(context.Background(), &PeerIdentity{UserName: "alice", NodeID: 7001}, permission)
	events := manager.EventsForHeartbeat()
	require.Len(t, events, 1)
	require.Equal(t, "started", events[0].Phase)
	require.Equal(t, sessionID, events[0].SessionId)
	require.Len(t, manager.EventsForHeartbeat(), 1)

	manager.Disconnect([]string{sessionID}, "admin_disconnect")
	<-sessionCtx.Done()
	manager.End(sessionID, "ended", "context_canceled")
	events = manager.EventsForHeartbeat()
	require.Len(t, events, 2)
	var startID, endID string
	for _, event := range events {
		switch event.Phase {
		case "started":
			startID = event.EventId
		case "ended":
			endID = event.EventId
			require.Equal(t, "revoked", event.Result)
			require.Equal(t, "admin_disconnect", event.CloseReason)
		}
	}
	require.NotEmpty(t, startID)
	require.NotEmpty(t, endID)

	manager.AckEvents([]string{startID})
	require.Len(t, manager.EventsForHeartbeat(), 1)
	manager.AckEvents([]string{endID})
	require.Empty(t, manager.EventsForHeartbeat())
}

func TestContainerSessionManagerCancelsSessionWhenSnapshotChanges(t *testing.T) {
	manager := NewContainerSessionManager()
	cache := NewPermissionCache()
	permission := &ContainerSSHUserPermission{
		UserID: 10, ResourceID: "resource-a", Namespace: "dev", PodName: "ide-0",
		PodUID: "pod-a", ContainerName: "workspace", ListenPort: 50200,
		TargetRevision: 3, GrantRevision: 4,
	}
	cache.UpdateContainerSSHPermissions(map[string][]*ContainerSSHUserPermission{"alice": {permission}})
	sessionID, sessionCtx := manager.Begin(context.Background(), &PeerIdentity{UserName: "alice", NodeID: 7001}, permission)

	changed := *permission
	changed.TargetRevision++
	cache.UpdateContainerSSHPermissions(map[string][]*ContainerSSHUserPermission{"alice": {&changed}})
	manager.ReconcilePermissions(cache)
	<-sessionCtx.Done()
	manager.End(sessionID, "ended", "context_canceled")

	events := manager.EventsForHeartbeat()
	require.Len(t, events, 2)
	for _, event := range events {
		if event.Phase == "ended" {
			require.Equal(t, "revoked", event.Result)
			require.Equal(t, "target_gone", event.CloseReason)
		}
	}
}

func TestContainerSessionManagerCancelsSessionWhenGrantDisappears(t *testing.T) {
	manager := NewContainerSessionManager()
	cache := NewPermissionCache()
	permission := &ContainerSSHUserPermission{
		UserID: 10, ResourceID: "resource-a", Namespace: "dev", PodName: "ide-0",
		PodUID: "pod-a", ContainerName: "workspace", ListenPort: 50200,
		TargetRevision: 3, GrantRevision: 4,
	}
	cache.UpdateContainerSSHPermissions(map[string][]*ContainerSSHUserPermission{"alice": {permission}})
	sessionID, sessionCtx := manager.Begin(context.Background(), &PeerIdentity{UserName: "alice", NodeID: 7001}, permission)

	cache.UpdateContainerSSHPermissions(nil)
	manager.ReconcilePermissions(cache)
	<-sessionCtx.Done()
	manager.End(sessionID, "ended", "context_canceled")

	for _, event := range manager.EventsForHeartbeat() {
		if event.Phase == "ended" {
			require.Equal(t, "revoked", event.Result)
			require.Equal(t, "grant_revoked", event.CloseReason)
			return
		}
	}
	t.Fatal("missing ended event")
}

func TestContainerSessionManagerV2UsesServerSessionIDAndReplaysDurableEvents(t *testing.T) {
	now := time.Now().UTC()
	stateDir := t.TempDir()
	manager, err := NewPersistentContainerSessionManager(stateDir)
	require.NoError(t, err)
	permission := sessionPermission(now, "session-server", "resource-a", "alice", 7001, 50200)

	sessionCtx, err := manager.BeginV2(context.Background(), permission)
	require.NoError(t, err)
	select {
	case <-sessionCtx.Done():
		t.Fatal("session was canceled before valid_until")
	default:
	}
	events := manager.ResourceEventsForHeartbeat()
	require.Len(t, events, 1)
	require.Equal(t, "session-server", events[0].SessionId)
	require.Equal(t, int64(1), events[0].SourceSequence)
	require.Equal(t, "connected", events[0].EventType)

	restarted, err := NewPersistentContainerSessionManager(stateDir)
	require.NoError(t, err)
	replayed := restarted.ResourceEventsForHeartbeat()
	require.Len(t, replayed, 1)
	require.Equal(t, events[0].EventId, replayed[0].EventId)
	require.Equal(t, events[0].SourceSequence, replayed[0].SourceSequence)

	require.NoError(t, manager.EndV2("session-server", "success", "shell_exited"))
	restarted, err = NewPersistentContainerSessionManager(stateDir)
	require.NoError(t, err)
	replayed = restarted.ResourceEventsForHeartbeat()
	require.Len(t, replayed, 2)
	require.Equal(t, int64(1), replayed[0].SourceSequence)
	require.Equal(t, int64(2), replayed[1].SourceSequence)
	require.Equal(t, "ended", replayed[1].EventType)
}

func TestContainerSessionManagerV2TerminationCancelsAndPersistsAck(t *testing.T) {
	now := time.Now().UTC()
	stateDir := t.TempDir()
	manager, err := NewPersistentContainerSessionManager(stateDir)
	require.NoError(t, err)
	permission := sessionPermission(now, "session-server", "resource-a", "alice", 7001, 50200)
	sessionCtx, err := manager.BeginV2(context.Background(), permission)
	require.NoError(t, err)

	command := &pb.ResourceSessionTerminationCommandV2{
		SessionId: "session-server", CommandRevision: 3, ReasonCode: "GRANT_REVOKED", Reason: "grant revoked",
	}
	require.NoError(t, manager.ApplyTerminationCommands([]*pb.ResourceSessionTerminationCommandV2{command}))
	select {
	case <-sessionCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("termination command did not cancel the active session")
	}
	require.NoError(t, manager.EndV2("session-server", "ended", "context_canceled"))
	events := manager.ResourceEventsForHeartbeat()
	require.Len(t, events, 2)
	require.Equal(t, "terminated", events[1].EventType)
	require.Equal(t, "GRANT_REVOKED", events[1].ResultCode)
	acks := manager.TerminationAcksForHeartbeat()
	require.Len(t, acks, 1)
	require.Equal(t, "session-server", acks[0].SessionId)
	require.Equal(t, int64(3), acks[0].CommandRevision)

	restarted, err := NewPersistentContainerSessionManager(stateDir)
	require.NoError(t, err)
	restartedAcks := restarted.TerminationAcksForHeartbeat()
	require.Len(t, restartedAcks, 1)
	require.Equal(t, "session-server", restartedAcks[0].SessionId)
	require.Equal(t, int64(3), restartedAcks[0].CommandRevision)
	require.NoError(t, restarted.ApplyTerminationCommands(nil))
	require.Empty(t, restarted.TerminationAcksForHeartbeat())
}

func TestContainerSessionManagerV2SnapshotRemovalCancelsSession(t *testing.T) {
	now := time.Now().UTC()
	cache := NewSessionAuthorizationCache()
	permission := sessionPermission(now, "session-server", "resource-a", "alice", 7001, 50200)
	require.NoError(t, cache.Apply(signedSessionSnapshot(t, 1, now, permission), now))
	manager := NewContainerSessionManager()
	sessionCtx, err := manager.BeginV2(context.Background(), permission)
	require.NoError(t, err)

	require.NoError(t, cache.Apply(signedSessionSnapshot(t, 2, now), now))
	manager.ReconcileV2(cache, now)
	select {
	case <-sessionCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("replace-all permission removal did not cancel the active session")
	}
	require.NoError(t, manager.EndV2("session-server", "ended", "context_canceled"))
	events := manager.ResourceEventsForHeartbeat()
	require.Equal(t, "terminated", events[1].EventType)
	require.Equal(t, "SESSION_AUTHORIZATION_REVOKED", events[1].ResultCode)
}

func TestContainerSessionManagerV2RefreshExtendsLocalExpiry(t *testing.T) {
	now := time.Now().UTC()
	manager := NewContainerSessionManager()
	permission := sessionPermission(now, "session-refresh", "resource-a", "alice", 7001, 50200)
	permission.ValidUntil = timestamppb.New(now.Add(200 * time.Millisecond))
	sessionCtx, err := manager.BeginV2(context.Background(), permission)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)
	refreshedAt := time.Now().UTC()
	refreshed := sessionPermission(refreshedAt, "session-refresh", "resource-a", "alice", 7001, 50200)
	refreshed.ValidUntil = timestamppb.New(now.Add(600 * time.Millisecond))
	snapshot := &pb.ResourceSessionAuthorizationSnapshotV2{
		Revision: 1, IssuedAt: timestamppb.New(refreshedAt), ValidUntil: timestamppb.New(now.Add(150 * time.Millisecond)),
		ReplaceAll: true, Permissions: []*pb.ResourceSessionPermissionV2{refreshed}, EnforceV2: true,
	}
	cache := NewSessionAuthorizationCache()
	require.NoError(t, cache.Apply(signExistingSessionSnapshot(t, snapshot), refreshedAt))
	manager.ReconcileV2(cache, refreshedAt)

	select {
	case <-sessionCtx.Done():
		t.Fatal("session expired at the original valid_until after a trusted refresh")
	case <-time.After(250 * time.Millisecond):
	}
	select {
	case <-sessionCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("session did not expire at the refreshed valid_until")
	}
	require.NoError(t, manager.EndV2(permission.SessionId, "ended", "context_canceled"))
	events := manager.ResourceEventsForHeartbeat()
	require.Len(t, events, 2)
	require.Equal(t, "terminated", events[1].EventType)
	require.Equal(t, "SESSION_AUTHORIZATION_EXPIRED", events[1].ResultCode)
}

func TestContainerSessionManagerBatchesReliableReports(t *testing.T) {
	manager := NewContainerSessionManager()
	for i := 0; i < 105; i++ {
		eventID := fmt.Sprintf("event-%03d", i)
		manager.resourceEvents[eventID] = &pb.ResourceSessionEventV2{
			EventId: eventID, SessionId: fmt.Sprintf("session-%03d", i), SourceSequence: 1,
			EventType: "connected", OccurredAt: timestamppb.Now(),
		}
		ack := &pb.ResourceSessionTerminationAckV2{SessionId: fmt.Sprintf("session-%03d", i), CommandRevision: 1}
		manager.terminationAcks[terminationAckKey(ack.SessionId, ack.CommandRevision)] = ack
	}

	firstEvents := manager.ResourceEventsForHeartbeat()
	require.Len(t, firstEvents, resourceSessionHeartbeatBatchSize)
	require.Equal(t, "session-000", firstEvents[0].SessionId)
	require.Equal(t, "session-099", firstEvents[99].SessionId)
	eventAcks := make([]*pb.ResourceSessionEventAckV2, 0, len(firstEvents))
	for _, event := range firstEvents {
		eventAcks = append(eventAcks, &pb.ResourceSessionEventAckV2{EventId: event.EventId})
	}
	require.NoError(t, manager.AckResourceEvents(eventAcks))
	require.Len(t, manager.ResourceEventsForHeartbeat(), 5)

	terminationAcks := manager.TerminationAcksForHeartbeat()
	require.Len(t, terminationAcks, resourceSessionHeartbeatBatchSize)
	require.Equal(t, "session-000", terminationAcks[0].SessionId)
	require.Equal(t, "session-099", terminationAcks[99].SessionId)
}

func TestPersistentContainerSessionManagerExpandsHomeDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	manager, err := NewPersistentContainerSessionManager("~/signal-state")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(home, "signal-state", "resource_session_events_v2.json"), manager.statePath)
}
