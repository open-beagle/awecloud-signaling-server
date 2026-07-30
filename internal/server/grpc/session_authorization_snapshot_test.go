package grpc

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/open-beagle/awecloud-signaling-server/internal/agent"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/service"
)

func TestSessionAuthorizationSnapshotInteroperatesWithAgentAtomicCache(t *testing.T) {
	now := time.Now().UTC()
	server := &AgentServiceServer{authorizationSnapshotStates: make(map[string]*authorizationSnapshotState)}
	snapshot := server.toProtoAuthorizationSnapshot(&service.SessionAuthorizationSnapshot{
		TechnicalResourceID: "technical-a",
		Permissions: []service.SessionAuthorizationPermission{
			testSessionAuthorizationPermission(now, "session-a", "resource-a", "alice", 7001),
			testSessionAuthorizationPermission(now, "session-b", "resource-b", "bob", 7002),
			testSessionAuthorizationPermission(now, "session-c", "resource-a", "carol", 7003),
		},
	}, true)
	require.NotNil(t, snapshot)
	require.NotZero(t, snapshot.Permissions[0].ListenPort)
	require.NotEqual(t, snapshot.Permissions[0].ListenPort, snapshot.Permissions[1].ListenPort)
	require.Equal(t, snapshot.Permissions[0].ListenPort, snapshot.Permissions[2].ListenPort)

	cache := agent.NewSessionAuthorizationCache()
	require.NoError(t, cache.Apply(snapshot, time.Now().UTC()))
	revision, payloadHash := cache.Current()
	require.NoError(t, cache.CommitAck(revision, payloadHash))
	ackRevision, ackHash := cache.Ack()
	require.Equal(t, snapshot.Revision, ackRevision)
	require.Equal(t, snapshot.PayloadHash, ackHash)
	require.True(t, server.acknowledgeAuthorizationSnapshot("technical-a", ackRevision, ackHash))
	require.Equal(t, ackRevision, server.authorizationSnapshotStates["technical-a"].ackRevision)
	require.False(t, server.acknowledgeAuthorizationSnapshot("technical-a", ackRevision, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))
}

func testSessionAuthorizationPermission(now time.Time, sessionID, resourceID, userName string, nodeID uint64) service.SessionAuthorizationPermission {
	return service.SessionAuthorizationPermission{
		SessionID: sessionID, TenantID: "tenant-a", ResourceID: resourceID, SourceID: "source-a", TargetRevisionID: "target-a",
		UserID: 10, UserName: userName, DeviceID: 20, DeviceHeadscaleNodeID: nodeID,
		ResourceType: model.TenantResourceContainerSSH, Action: "shell", AllocationID: "allocation-a", GrantID: "grant-a",
		GrantRevision: 3, AuthorizationRevision: 4, ValidUntil: now.Add(time.Minute),
		Target: service.SessionAuthorizationTarget{
			NamespaceUID: "namespace-uid", NamespaceName: "dev", WorkloadUID: "workload-a",
			PodName: "ide-0", PodUID: "pod-a", ContainerName: "workspace",
		},
	}
}
