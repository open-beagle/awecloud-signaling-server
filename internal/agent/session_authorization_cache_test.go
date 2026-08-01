package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

func TestSessionAuthorizationCacheAppliesCompleteSnapshotAndResolvesTrustedIdentity(t *testing.T) {
	now := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	cache := NewSessionAuthorizationCache()
	snapshot := signedSessionSnapshot(t, 10, now, sessionPermission(now, "session-a", "resource-a", "alice", 7001, 50200))
	require.NoError(t, cache.Apply(snapshot, now))
	revision, hash := cache.Current()
	require.NoError(t, cache.CommitAck(revision, hash))

	revision, hash = cache.Ack()
	require.Equal(t, int64(10), revision)
	require.Equal(t, snapshot.PayloadHash, hash)
	permission, allowed := cache.ResolveContainerSSH(50200, "alice", 7001, now)
	require.True(t, allowed)
	require.Equal(t, "session-a", permission.SessionId)
	_, allowed = cache.ResolveContainerSSH(50200, "alice", 7002, now)
	require.False(t, allowed)
	_, allowed = cache.ResolveContainerSSH(50200, "bob", 7001, now)
	require.False(t, allowed)

	require.NoError(t, cache.Apply(proto.Clone(snapshot).(*pb.ResourceSessionAuthorizationSnapshotV2), now.Add(time.Second)))
	revision, hash = cache.Ack()
	require.Equal(t, int64(10), revision)
	require.Equal(t, snapshot.PayloadHash, hash)
}

func TestSessionAuthorizationCacheRejectsOrderingAndHashFailuresWithoutChangingState(t *testing.T) {
	now := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	cache := NewSessionAuthorizationCache()
	current := signedSessionSnapshot(t, 10, now, sessionPermission(now, "session-a", "resource-a", "alice", 7001, 50200))
	require.NoError(t, cache.Apply(current, now))
	revision, hash := cache.Current()
	require.NoError(t, cache.CommitAck(revision, hash))

	rollback := signedSessionSnapshot(t, 9, now, sessionPermission(now, "session-b", "resource-b", "bob", 7002, 50201))
	require.ErrorIs(t, cache.Apply(rollback, now), ErrSessionSnapshotRevisionRollback)

	conflict := signedSessionSnapshot(t, 10, now, sessionPermission(now, "session-b", "resource-b", "bob", 7002, 50201))
	require.ErrorIs(t, cache.Apply(conflict, now), ErrSessionSnapshotRevisionConflict)

	badHash := signedSessionSnapshot(t, 11, now, sessionPermission(now, "session-c", "resource-c", "carol", 7003, 50202))
	badHash.PayloadHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	require.ErrorIs(t, cache.Apply(badHash, now), ErrSessionSnapshotHashMismatch)

	revision, hash = cache.Ack()
	require.Equal(t, int64(10), revision)
	require.Equal(t, current.PayloadHash, hash)
	_, allowed := cache.ResolveContainerSSH(50200, "alice", 7001, now)
	require.True(t, allowed)
}

func TestSessionAuthorizationCacheRejectsConflictsAndExpiresLocally(t *testing.T) {
	now := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	cache := NewSessionAuthorizationCache()
	conflicted := signedSessionSnapshot(t, 1, now,
		sessionPermission(now, "session-a", "resource-a", "alice", 7001, 50200),
		sessionPermission(now, "session-b", "resource-b", "bob", 7002, 50200),
	)
	require.ErrorIs(t, cache.Apply(conflicted, now), ErrSessionSnapshotRouteConflict)
	revision, hash := cache.Ack()
	require.Zero(t, revision)
	require.Empty(t, hash)

	snapshot := signedSessionSnapshot(t, 2, now, sessionPermission(now, "session-a", "resource-a", "alice", 7001, 50200))
	require.NoError(t, cache.Apply(snapshot, now))
	_, allowed := cache.ResolveContainerSSH(50200, "alice", 7001, now.Add(31*time.Second))
	require.False(t, allowed)
	require.Empty(t, cache.Permissions(now.Add(31*time.Second)))

	expired := signedSessionSnapshot(t, 3, now.Add(-time.Minute), sessionPermission(now.Add(-time.Minute), "session-c", "resource-c", "carol", 7003, 50202))
	require.ErrorIs(t, cache.Apply(expired, now), ErrSessionSnapshotExpired)
	revision, _ = cache.Current()
	require.Equal(t, int64(2), revision)
}

func TestSessionAuthorizationCacheReplaceAllCanClearPermissions(t *testing.T) {
	now := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	cache := NewSessionAuthorizationCache()
	require.NoError(t, cache.Apply(signedSessionSnapshot(t, 1, now, sessionPermission(now, "session-a", "resource-a", "alice", 7001, 50200)), now))
	require.NoError(t, cache.Apply(signedSessionSnapshot(t, 2, now), now))
	require.Empty(t, cache.Permissions(now))
	_, allowed := cache.ResolveContainerSSH(50200, "alice", 7001, now)
	require.False(t, allowed)
}

func signedSessionSnapshot(t *testing.T, revision int64, now time.Time, permissions ...*pb.ResourceSessionPermissionV2) *pb.ResourceSessionAuthorizationSnapshotV2 {
	t.Helper()
	snapshot := &pb.ResourceSessionAuthorizationSnapshotV2{
		Revision: revision, IssuedAt: timestamppb.New(now), ValidUntil: timestamppb.New(now.Add(30 * time.Second)),
		ReplaceAll: true, Permissions: permissions, EnforceV2: true,
	}
	return signExistingSessionSnapshot(t, snapshot)
}

func signExistingSessionSnapshot(t *testing.T, snapshot *pb.ResourceSessionAuthorizationSnapshotV2) *pb.ResourceSessionAuthorizationSnapshotV2 {
	t.Helper()
	payload, err := proto.MarshalOptions{Deterministic: true}.Marshal(snapshot)
	require.NoError(t, err)
	digest := sha256.Sum256(payload)
	snapshot.PayloadHash = hex.EncodeToString(digest[:])
	return snapshot
}

func sessionPermission(now time.Time, sessionID, resourceID, userName string, nodeID uint64, listenPort uint32) *pb.ResourceSessionPermissionV2 {
	return &pb.ResourceSessionPermissionV2{
		SessionId: sessionID, TenantId: "tenant-a", ResourceId: resourceID, SourceId: "source-a", TargetRevisionId: "target-a",
		UserId: 10, UserName: userName, DeviceId: 20, DeviceHeadscaleNodeId: nodeID, ResourceType: "container_ssh", Action: "shell",
		AllocationId: "allocation-a", GrantId: "grant-a", GrantRevision: 3, AuthorizationRevision: 4,
		ValidUntil: timestamppb.New(now.Add(time.Minute)), ListenPort: listenPort,
		Target: &pb.ResourceSessionTargetV2{
			NamespaceUid: "namespace-uid", NamespaceName: "dev", WorkloadUid: "workload-a", PodName: "ide-0", PodUid: "pod-a", ContainerName: "workspace",
		},
	}
}
