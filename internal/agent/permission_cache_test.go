package agent

import (
	"testing"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
	"github.com/stretchr/testify/require"
)

func TestContainerSSHPermissionProtoSnapshotReplacesCache(t *testing.T) {
	cache := NewPermissionCache()
	cache.UpdateContainerSSHPermissionsFromProto([]*pb.ContainerSSHPermission{{
		UserName: "alice", ResourceId: "resource-a", Namespace: "dev", PodName: "ide-0", PodUid: "pod-a", ContainerName: "workspace", TargetRevision: 2, GrantRevision: 3, MaxSessionSeconds: 60, ListenPort: 50200,
	}})
	permission, allowed := cache.CheckContainerSSHAccess("alice", "resource-a")
	require.True(t, allowed)
	require.Equal(t, "pod-a", permission.PodUID)
	require.Equal(t, 60, permission.MaxSessionSeconds)
	resourceID, routed := cache.ResolveContainerSSHRoute(50200)
	require.True(t, routed)
	require.Equal(t, "resource-a", resourceID)

	cache.UpdateContainerSSHPermissionsFromProto(nil)
	_, allowed = cache.CheckContainerSSHAccess("alice", "resource-a")
	require.False(t, allowed)
	_, routed = cache.ResolveContainerSSHRoute(50200)
	require.False(t, routed)
}

func TestTechnicalResourceConfigAppliedWithoutRevision(t *testing.T) {
	agent := &Agent{config: &config.AgentConfig{}}
	agent.handleHeartbeatResponse(&pb.AgentHeartbeatResponse{
		CapabilityConfig: &pb.AgentCapabilityConfig{SshEnabledSet: true, SshEnabled: true},
	})
	require.True(t, agent.config.Tunnel.EnableSSH)
}

func TestContainerSSHPermissionSnapshotRejectsConflictingPortRoutes(t *testing.T) {
	cache := NewPermissionCache()
	cache.UpdateContainerSSHPermissionsFromProto([]*pb.ContainerSSHPermission{
		{UserName: "alice", ResourceId: "resource-a", Namespace: "dev", PodName: "ide-a", PodUid: "pod-a", ContainerName: "workspace", ListenPort: 50200},
		{UserName: "bob", ResourceId: "resource-b", Namespace: "dev", PodName: "ide-b", PodUid: "pod-b", ContainerName: "workspace", ListenPort: 50200},
	})
	_, routed := cache.ResolveContainerSSHRoute(50200)
	require.False(t, routed)
}

func TestOldServerResponseClearsContainerSSHRoute(t *testing.T) {
	cache := NewPermissionCache()
	agent := &Agent{permCache: cache}
	agent.handleHeartbeatResponse(&pb.AgentHeartbeatResponse{
		ContainerSshProtocol: "v1",
		ContainerSshPermissions: []*pb.ContainerSSHPermission{{
			UserName: "alice", ResourceId: "resource-a", Namespace: "dev", PodName: "ide-a", PodUid: "pod-a", ContainerName: "workspace", ListenPort: 50200,
		}},
	})
	_, routed := cache.ResolveContainerSSHRoute(50200)
	require.True(t, routed)

	agent.handleHeartbeatResponse(&pb.AgentHeartbeatResponse{})
	_, routed = cache.ResolveContainerSSHRoute(50200)
	require.False(t, routed)
}
