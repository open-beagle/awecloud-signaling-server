package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
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
