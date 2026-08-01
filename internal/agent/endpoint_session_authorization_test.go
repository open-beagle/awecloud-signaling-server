package agent

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

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
