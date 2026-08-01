package main

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

func TestEndpointSessionAuthorizationAppliesSnapshotAndBuildsReliableReport(t *testing.T) {
	now := time.Now().UTC()
	stateDir := t.TempDir()
	state, err := newEndpointSessionAuthorization(stateDir)
	require.NoError(t, err)
	permission := endpointServicePermission(now)
	snapshot := endpointSignedSnapshot(t, now, permission)
	state.applyResponse(&pb.EndpointHeartbeatResponse{AuthorizationSnapshotV2: snapshot})

	req := &pb.EndpointHeartbeatRequest{}
	state.appendReport(req)
	require.Equal(t, "resource_session_v2", req.SessionAuthorizationProtocol)
	require.Equal(t, snapshot.Revision, req.AuthorizationSnapshotAckRevision)
	require.Equal(t, snapshot.PayloadHash, req.AuthorizationSnapshotAckHash)

	require.NoError(t, state.sessions.FailV2(permission.SessionId, "SERVICE_CONNECT_FAILED"))
	req = &pb.EndpointHeartbeatRequest{}
	state.appendReport(req)
	require.Len(t, req.ResourceSessionEvents, 1)
	eventID := req.ResourceSessionEvents[0].EventId
	require.Equal(t, int64(1), req.ResourceSessionEvents[0].SourceSequence)

	restarted, err := newEndpointSessionAuthorization(stateDir)
	require.NoError(t, err)
	replayed := &pb.EndpointHeartbeatRequest{}
	restarted.appendReport(replayed)
	require.Len(t, replayed.ResourceSessionEvents, 1)
	require.Equal(t, eventID, replayed.ResourceSessionEvents[0].EventId)
	restarted.applyResponse(&pb.EndpointHeartbeatResponse{ResourceSessionEventAcks: []*pb.ResourceSessionEventAckV2{{EventId: eventID}}})
	req = &pb.EndpointHeartbeatRequest{}
	restarted.appendReport(req)
	require.Empty(t, req.ResourceSessionEvents)
}

func TestEndpointSVCRequestAndLocalDiscoveryMustMatchPermission(t *testing.T) {
	permission := endpointServicePermission(time.Now().UTC())
	req := &pb.SVCProxyRequest{
		SessionId: permission.SessionId, ResourceId: permission.ResourceId, SourceId: permission.SourceId,
		TargetRevisionId: permission.TargetRevisionId, AuthorizationRevision: permission.AuthorizationRevision,
		Namespace: permission.Target.NamespaceName, ServiceName: permission.Target.ServiceName, ServiceUid: permission.Target.ServiceUid,
		PortName: permission.Target.PortName, Port: permission.Target.PortNumber, Protocol: permission.Target.Protocol,
	}
	require.True(t, endpointSVCRequestMatchesPermission(req, permission))
	changed := proto.Clone(req).(*pb.SVCProxyRequest)
	changed.ClusterIp = "10.96.0.99"
	require.False(t, endpointSVCRequestMatchesPermission(changed, permission))

	discovery := &K8SServiceDiscovery{discoveredSvcs: []*pb.DiscoveredK8SService{{
		Namespace: "dev", ServiceName: "api", ServiceUid: "service-uid", ClusterIp: "10.96.0.20",
		Ports: []*pb.ServicePort{{Name: "https", Port: 443, Protocol: "TCP"}},
	}}}
	clusterIP, ok := discovery.ResolveService("dev", "api", "service-uid", "https", 443, "TCP")
	require.True(t, ok)
	require.Equal(t, "10.96.0.20", clusterIP)
	_, ok = discovery.ResolveService("dev", "api", "service-other", "https", 443, "TCP")
	require.False(t, ok)
}

func TestEndpointV2FieldsCannotFallBackToLegacyAuthorization(t *testing.T) {
	require.False(t, endpointRequestHasV2Fields(&pb.SVCProxyRequest{
		SessionId: "legacy-session", Namespace: "dev", ServiceName: "api", Port: 443, ClusterIp: "10.96.0.20",
	}))
	require.True(t, endpointRequestHasV2Fields(&pb.SVCProxyRequest{ResourceId: "resource-a"}))
	require.True(t, endpointRequestHasV2Fields(&pb.SVCProxyRequest{AuthorizationRevision: 1}))

	now := time.Now().UTC()
	state, err := newEndpointSessionAuthorization(t.TempDir())
	require.NoError(t, err)
	state.applyResponse(&pb.EndpointHeartbeatResponse{AuthorizationSnapshotV2: endpointSignedSnapshot(t, now, endpointServicePermission(now))})
	require.True(t, state.enforceV2())
	require.False(t, state.enabled(now.Add(time.Minute)))
	require.True(t, state.enforceV2(), "expired v2 policy must still block legacy fallback")
}

func endpointServicePermission(now time.Time) *pb.ResourceSessionPermissionV2 {
	return &pb.ResourceSessionPermissionV2{
		SessionId: "session-endpoint", TenantId: "tenant-a", ResourceId: "resource-a", SourceId: "source-a", TargetRevisionId: "target-a",
		UserId: 10, UserName: "alice", DeviceId: 20, DeviceHeadscaleNodeId: 7001,
		ResourceType: "container_service", Action: "connect", AllocationId: "allocation-a", GrantId: "grant-a",
		GrantRevision: 3, AuthorizationRevision: 4, ValidUntil: timestamppb.New(now.Add(time.Minute)),
		Target: &pb.ResourceSessionTargetV2{
			NamespaceUid: "namespace-uid", NamespaceName: "dev", ServiceUid: "service-uid", ServiceName: "api",
			PortName: "https", PortNumber: 443, Protocol: "TCP",
		},
	}
}

func endpointSignedSnapshot(t *testing.T, now time.Time, permissions ...*pb.ResourceSessionPermissionV2) *pb.ResourceSessionAuthorizationSnapshotV2 {
	t.Helper()
	snapshot := &pb.ResourceSessionAuthorizationSnapshotV2{
		Revision: 1, IssuedAt: timestamppb.New(now), ValidUntil: timestamppb.New(now.Add(30 * time.Second)),
		ReplaceAll: true, Permissions: permissions, EnforceV2: true,
	}
	payload, err := proto.MarshalOptions{Deterministic: true}.Marshal(snapshot)
	require.NoError(t, err)
	digest := sha256.Sum256(payload)
	snapshot.PayloadHash = hex.EncodeToString(digest[:])
	return snapshot
}
