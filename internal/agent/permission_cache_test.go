package agent

import (
	"testing"

	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
	"github.com/stretchr/testify/require"
)

func TestContainerSSHPermissionProtoSnapshotReplacesCache(t *testing.T) {
	cache := NewPermissionCache()
	cache.UpdateContainerSSHPermissionsFromProto([]*pb.ContainerSSHPermission{{
		UserName: "alice", ResourceId: "resource-a", Namespace: "dev", PodName: "ide-0", PodUid: "pod-a", ContainerName: "workspace", TargetRevision: 2, GrantRevision: 3, MaxSessionSeconds: 60,
	}})
	permission, allowed := cache.CheckContainerSSHAccess("alice", "resource-a")
	require.True(t, allowed)
	require.Equal(t, "pod-a", permission.PodUID)
	require.Equal(t, 60, permission.MaxSessionSeconds)

	cache.UpdateContainerSSHPermissionsFromProto(nil)
	_, allowed = cache.CheckContainerSSHAccess("alice", "resource-a")
	require.False(t, allowed)
}
