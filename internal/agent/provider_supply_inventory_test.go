package agent

import (
	"context"
	"io"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

type captureSupplyInventoryServer struct {
	pb.UnimplementedAgentServiceServer
	received chan *pb.SupplyInventoryEnvelope
}

func (s *captureSupplyInventoryServer) ReportSupplyInventory(stream pb.AgentService_ReportSupplyInventoryServer) error {
	for {
		envelope, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		s.received <- envelope
		if err := stream.Send(&pb.SupplyInventoryAck{AcceptedSequence: envelope.Sequence, SnapshotId: envelope.SnapshotId, ResultCode: "SNAPSHOT_COMMITTED"}); err != nil {
			return err
		}
	}
}

func TestEndpointSupplyInventoryForwardingUsesTrustedAgentMapping(t *testing.T) {
	upstream := &captureSupplyInventoryServer{received: make(chan *pb.SupplyInventoryEnvelope, 1)}
	upstreamClient, stopUpstream := startAgentSupplyInventoryTestServer(t, upstream)
	defer stopUpstream()

	endpointServer := NewEndpointServer(0, "endpoint-token", context.Background())
	endpointServer.SetSupplyInventoryClient(upstreamClient)
	endpointServer.connections["endpoint-a"] = &EndpointConnection{Name: "endpoint-a", Token: "endpoint-token"}
	endpointServer.UpdateServerConfig("endpoint-a", &EndpointServerConfig{SupplyInventory: &pb.SupplyInventoryConfig{
		Enabled: true, ProtocolVersion: "v1", TechnicalResourceId: "trusted-endpoint-resource", CredentialRevision: 7,
	}})
	client, stopEndpoint := startEndpointSupplyInventoryTestServer(t, endpointServer)
	defer stopEndpoint()

	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(
		"authorization", "Bearer endpoint-token", endpointNameMetadata, "endpoint-a",
	))
	stream, err := client.ReportSupplyInventory(ctx)
	require.NoError(t, err)
	require.NoError(t, stream.Send(&pb.SupplyInventoryEnvelope{Sequence: 9, SnapshotId: "snapshot-a", CredentialRevision: 999}))
	ack, err := stream.Recv()
	require.NoError(t, err)
	require.Equal(t, uint64(9), ack.AcceptedSequence)
	forwarded := <-upstream.received
	require.Equal(t, "trusted-endpoint-resource", forwarded.SourceTechnicalResourceId)
	require.Equal(t, int64(7), forwarded.CredentialRevision)
	require.Equal(t, uint64(9), forwarded.Sequence)
}

func TestEndpointSupplyInventoryRejectsClaimedMappingAndInvalidToken(t *testing.T) {
	upstream := &captureSupplyInventoryServer{received: make(chan *pb.SupplyInventoryEnvelope, 1)}
	upstreamClient, stopUpstream := startAgentSupplyInventoryTestServer(t, upstream)
	defer stopUpstream()
	endpointServer := NewEndpointServer(0, "endpoint-token", context.Background())
	endpointServer.SetSupplyInventoryClient(upstreamClient)
	endpointServer.connections["endpoint-a"] = &EndpointConnection{Name: "endpoint-a", Token: "endpoint-token"}
	endpointServer.UpdateServerConfig("endpoint-a", &EndpointServerConfig{SupplyInventory: &pb.SupplyInventoryConfig{
		Enabled: true, ProtocolVersion: "v1", TechnicalResourceId: "trusted-endpoint-resource", CredentialRevision: 7,
	}})
	client, stopEndpoint := startEndpointSupplyInventoryTestServer(t, endpointServer)
	defer stopEndpoint()

	badToken := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer wrong", endpointNameMetadata, "endpoint-a"))
	badStream, err := client.ReportSupplyInventory(badToken)
	require.NoError(t, err)
	require.NoError(t, badStream.Send(&pb.SupplyInventoryEnvelope{}))
	_, err = badStream.Recv()
	require.Equal(t, codes.Unauthenticated, status.Code(err))

	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer endpoint-token", endpointNameMetadata, "endpoint-a"))
	claimed, err := client.ReportSupplyInventory(ctx)
	require.NoError(t, err)
	require.NoError(t, claimed.Send(&pb.SupplyInventoryEnvelope{SourceTechnicalResourceId: "forged-resource"}))
	_, err = claimed.Recv()
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func startAgentSupplyInventoryTestServer(t *testing.T, server pb.AgentServiceServer) (pb.AgentServiceClient, func()) {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	pb.RegisterAgentServiceServer(grpcServer, server)
	go func() { _ = grpcServer.Serve(listener) }()
	conn := dialSupplyInventoryBufconn(t, listener)
	return pb.NewAgentServiceClient(conn), func() { _ = conn.Close(); grpcServer.Stop(); _ = listener.Close() }
}

func startEndpointSupplyInventoryTestServer(t *testing.T, server pb.EndpointServiceServer) (pb.EndpointServiceClient, func()) {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	pb.RegisterEndpointServiceServer(grpcServer, server)
	go func() { _ = grpcServer.Serve(listener) }()
	conn := dialSupplyInventoryBufconn(t, listener)
	return pb.NewEndpointServiceClient(conn), func() { _ = conn.Close(); grpcServer.Stop(); _ = listener.Close() }
}

func dialSupplyInventoryBufconn(t *testing.T, listener *bufconn.Listener) *grpc.ClientConn {
	t.Helper()
	conn, err := grpc.NewClient("passthrough:///bufconn", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
		return listener.Dial()
	}))
	require.NoError(t, err)
	return conn
}
