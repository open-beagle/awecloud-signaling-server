package agent

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

func TestEndpointServerPushesRevocationWithoutAnotherHeartbeat(t *testing.T) {
	endpointServer := NewEndpointServer(0, "test-token", t.Context())
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := grpc.NewServer()
	pb.RegisterEndpointServiceServer(server, endpointServer)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	stream, err := pb.NewEndpointServiceClient(conn).Heartbeat(ctx)
	require.NoError(t, err)
	require.NoError(t, stream.Send(&pb.EndpointHeartbeatRequest{
		Name: "endpoint-a", Token: "test-token", SessionAuthorizationProtocol: sessionAuthorizationProtocolV2,
	}))
	_, err = stream.Recv()
	require.NoError(t, err)
	now := time.Now().UTC()
	permission := serviceSessionPermission(now)
	endpointServer.SetSessionAuthorizationSnapshots([]*pb.EndpointSessionAuthorizationSnapshotV2{{
		EndpointName: "endpoint-a", Snapshot: signedSessionSnapshot(t, 1, now, permission),
	}})
	response, err := stream.Recv()
	require.NoError(t, err, "snapshot must arrive without another heartbeat request")
	cache := NewSessionAuthorizationCache()
	require.NoError(t, cache.Apply(response.AuthorizationSnapshotV2, time.Now().UTC()))
	manager := NewContainerSessionManager()
	sessionCtx, err := manager.BeginV2(ctx, permission)
	require.NoError(t, err)
	defer manager.EndV2(permission.SessionId, "ended", "test_cleanup")
	// Full replacement removing the permission is also an explicit revocation.
	endpointServer.SetSessionAuthorizationSnapshots([]*pb.EndpointSessionAuthorizationSnapshotV2{{
		EndpointName: "endpoint-a", Snapshot: signedSessionSnapshot(t, 2, time.Now().UTC()),
	}})
	response, err = stream.Recv()
	require.NoError(t, err, "revocation must arrive without another heartbeat request")
	require.NoError(t, cache.Apply(response.AuthorizationSnapshotV2, time.Now().UTC()))
	manager.ReconcileV2(cache, time.Now().UTC())
	require.ErrorIs(t, sessionCtx.Err(), context.Canceled)
}

func TestEndpointServerForwardsAndScopesSessionAuthorization(t *testing.T) {
	now := time.Now().UTC()
	server := NewEndpointServer(50052, "endpoint-token", t.Context())
	permission := serviceSessionPermission(now)
	wrapper := &pb.EndpointSessionAuthorizationSnapshotV2{
		EndpointName: "endpoint-a", Snapshot: signedSessionSnapshot(t, 1, now, permission),
		EventAcks: []*pb.ResourceSessionEventAckV2{{EventId: "event-a", ResultCode: "SESSION_EVENT_ACCEPTED"}},
	}
	server.SetSessionAuthorizationSnapshots([]*pb.EndpointSessionAuthorizationSnapshotV2{wrapper})
	endpointName, resolved, ok := server.ResolveSessionAuthorization(permission.SessionId, now)
	require.True(t, ok)
	require.Equal(t, "endpoint-a", endpointName)
	require.Equal(t, permission.ResourceId, resolved.ResourceId)

	resp := &pb.EndpointHeartbeatResponse{}
	server.appendSessionAuthorizationResponse("endpoint-a", resp)
	require.NotNil(t, resp.AuthorizationSnapshotV2)
	require.Len(t, resp.ResourceSessionEventAcks, 1)

	server.recordSessionAuthorizationReport("endpoint-a", &pb.EndpointHeartbeatRequest{
		SessionAuthorizationProtocol: "resource_session_v2", AuthorizationSnapshotAckRevision: 1,
		AuthorizationSnapshotAckHash: wrapper.Snapshot.PayloadHash,
		SessionTerminationAcks:       []*pb.ResourceSessionTerminationAckV2{{SessionId: permission.SessionId, CommandRevision: 2}},
	})
	reports := server.SessionAuthorizationReports()
	require.Len(t, reports, 1)
	require.Equal(t, "endpoint-a", reports[0].EndpointName)
	require.Equal(t, wrapper.Snapshot.PayloadHash, reports[0].AuthorizationSnapshotAckHash)

	server.SetSessionAuthorizationSnapshots(nil)
	_, _, ok = server.ResolveSessionAuthorization(permission.SessionId, now)
	require.False(t, ok)
}

func TestEndpointServerPreservesEarlierFailClosedSnapshotWhenRefreshIsInvalid(t *testing.T) {
	now := time.Now().UTC()
	server := NewEndpointServer(50052, "endpoint-token", t.Context())
	permission := serviceSessionPermission(now)
	valid := signedSessionSnapshot(t, 1, now, permission)
	server.SetSessionAuthorizationSnapshots([]*pb.EndpointSessionAuthorizationSnapshotV2{{EndpointName: "endpoint-a", Snapshot: valid}})

	invalid := signedSessionSnapshot(t, 2, now, permission)
	invalid.PayloadHash = "invalid"
	server.SetSessionAuthorizationSnapshots([]*pb.EndpointSessionAuthorizationSnapshotV2{{EndpointName: "endpoint-a", Snapshot: invalid}})
	require.True(t, server.SessionAuthorizationEnforced())
	endpointName, resolved, ok := server.ResolveSessionAuthorization(permission.SessionId, now)
	require.True(t, ok)
	require.Equal(t, "endpoint-a", endpointName)
	require.Equal(t, permission.ResourceId, resolved.ResourceId)

	response := &pb.EndpointHeartbeatResponse{}
	server.appendSessionAuthorizationResponse("endpoint-a", response)
	require.NotNil(t, response.AuthorizationSnapshotV2)
	require.Equal(t, valid.Revision, response.AuthorizationSnapshotV2.Revision)
}
