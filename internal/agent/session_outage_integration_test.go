package agent

import (
	"context"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

// Use the production authorization check and TCP bridge with a fixed test peer
// identity. Only Headscale identity discovery and Kubernetes discovery are
// replaced; both gRPC data transport and the destination TCP socket are real.
type outageProxyServer struct {
	pb.UnimplementedAgentServiceServer
	proxy *K8SSVCProxy
}

func (s *outageProxyServer) SVCProxy(stream pb.AgentService_SVCProxyServer) error {
	request, err := stream.Recv()
	if err != nil {
		return err
	}
	identity := &PeerIdentity{UserName: "alice", NodeID: 7001, Role: "client"}
	permission, err := s.proxy.authorizeV2Request(request, identity, time.Now().UTC())
	if err != nil {
		return err
	}
	return s.proxy.handleDirectConnectionV2(stream, identity, permission)
}

func TestServiceStreamSurvivesControlPlaneOutageAndRevokesImmediately(t *testing.T) {
	target, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = target.Close() })
	targetClosed := make(chan struct{}, 4)
	go func() {
		for {
			conn, acceptErr := target.Accept()
			if acceptErr != nil {
				return
			}
			go func() {
				defer conn.Close()
				_, _ = io.Copy(conn, conn)
				targetClosed <- struct{}{}
			}()
		}
	}()
	now := time.Now().UTC()
	permission := serviceSessionPermission(now)
	permission.ValidUntil = timestamppb.New(now.Add(2 * time.Minute))
	permission.Target.PortNumber = int32(target.Addr().(*net.TCPAddr).Port)
	cache := NewSessionAuthorizationCache()
	snapshot := signedSessionSnapshot(t, 1, now, permission)
	require.NoError(t, cache.Apply(snapshot, now))
	manager := NewContainerSessionManager()
	proxy := &K8SSVCProxy{authorizations: cache, sessions: manager, informer: &K8SServiceInformer{
		services: map[string]*DiscoveredService{"dev/api": {
			UID: permission.Target.ServiceUid, Namespace: "dev", Name: "api", ClusterIP: "127.0.0.1",
			Ports: []DiscoveredServicePort{{Name: "https", Port: permission.Target.PortNumber, Protocol: "TCP"}},
		}},
	}}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := grpc.NewServer()
	pb.RegisterAgentServiceServer(server, &outageProxyServer{proxy: proxy})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	clientConn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = clientConn.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	client := pb.NewAgentServiceClient(clientConn)
	open := func() pb.AgentService_SVCProxyClient {
		stream, openErr := client.SVCProxy(ctx)
		require.NoError(t, openErr)
		require.NoError(t, stream.Send(serviceSessionRequest(permission)))
		_, openErr = stream.Recv()
		require.NoError(t, openErr)
		return stream
	}
	stream := open()
	exchange := func(sequence int) {
		message := fmt.Sprintf("data: outage-event-%d\n\n", sequence)
		require.NoError(t, stream.Send(&pb.SVCProxyData{Data: []byte(message)}))
		var received []byte
		for len(received) < len(message) {
			response, receiveErr := stream.Recv()
			require.NoError(t, receiveErr)
			require.False(t, response.IsClose)
			received = append(received, response.Data...)
		}
		require.Equal(t, message, string(received))
	}
	// Stop delivering control-plane snapshots, but keep transmitting SSE-style
	// data on the same actual TCP connection past the real 30-second TTL.
	for i := 0; ; i++ {
		exchange(i)
		if time.Now().After(snapshot.ValidUntil.AsTime().Add(200 * time.Millisecond)) {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Logf("existing stream survived snapshot expiry after %s", time.Since(now))
	_, allowed := cache.Permission(permission.SessionId, time.Now().UTC())
	require.False(t, allowed)
	denied, err := client.SVCProxy(ctx)
	require.NoError(t, err)
	require.NoError(t, denied.Send(serviceSessionRequest(permission)))
	_, err = denied.Recv()
	require.Error(t, err, "another connection must fail even for an active Session")

	// Restore the control plane without replacing the data stream or Session.
	restoredAt := time.Now().UTC()
	require.NoError(t, cache.Apply(signedSessionSnapshot(t, 2, restoredAt, permission), restoredAt))
	manager.ReconcileV2(cache, restoredAt)
	exchange(999)
	t.Logf("same stream recovered authorization and exchanged data in %s", time.Since(restoredAt))

	// Deliver an explicit revocation, well before the real permission deadline.
	revokedAt := time.Now().UTC()
	require.NoError(t, manager.ApplyTerminationCommands([]*pb.ResourceSessionTerminationCommandV2{{
		SessionId: permission.SessionId, CommandRevision: 1, ReasonCode: "GRANT_REVOKED",
	}}))
	select {
	case <-targetClosed:
	case <-time.After(time.Second):
		t.Fatal("revocation did not promptly close the destination TCP connection")
	}
	t.Logf("explicit revocation closed TCP in %s", time.Since(revokedAt))
	require.Eventually(t, func() bool {
		events := manager.ResourceEventsForHeartbeat()
		return len(events) == 2 && events[1].ResultCode == "GRANT_REVOKED"
	}, time.Second, time.Millisecond)

	// A new Session still closes autonomously at its real authorization deadline
	// even if no more control-plane responses arrive.
	expiryStart := time.Now().UTC()
	permission.SessionId = "session-real-expiry"
	permission.ValidUntil = timestamppb.New(expiryStart.Add(700 * time.Millisecond))
	expiring := &pb.ResourceSessionAuthorizationSnapshotV2{
		Revision: 3, IssuedAt: timestamppb.New(expiryStart), ValidUntil: timestamppb.New(expiryStart.Add(200 * time.Millisecond)),
		ReplaceAll: true, EnforceV2: true, Permissions: []*pb.ResourceSessionPermissionV2{permission},
	}
	require.NoError(t, cache.Apply(signExistingSessionSnapshot(t, expiring), expiryStart))
	stream = open()
	exchange(1000)
	select {
	case <-targetClosed:
		require.False(t, time.Now().Before(permission.ValidUntil.AsTime()), "real deadline must not be shortened to the envelope TTL")
	case <-time.After(2 * time.Second):
		t.Fatal("real authorization expiry did not close the destination TCP connection")
	}
	require.Eventually(t, func() bool {
		for _, event := range manager.ResourceEventsForHeartbeat() {
			if event.SessionId == permission.SessionId && event.ResultCode == "SESSION_AUTHORIZATION_EXPIRED" {
				return true
			}
		}
		return false
	}, time.Second, time.Millisecond)
}
