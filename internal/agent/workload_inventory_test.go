package agent

import (
	"context"
	"io"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

type captureWorkloadInventoryServer struct {
	pb.UnimplementedAgentServiceServer
	received chan *pb.WorkloadInventoryEnvelope
}

func (s *captureWorkloadInventoryServer) ReportWorkloadInventory(stream pb.AgentService_ReportWorkloadInventoryServer) error {
	for {
		envelope, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		s.received <- envelope
		if err := stream.Send(&pb.WorkloadInventoryAck{
			AcceptedSequence: envelope.Sequence, SnapshotId: envelope.SnapshotId,
			BatchIndex: envelope.BatchIndex, ResultCode: "WORKLOAD_ACCEPTED", Committed: true,
		}); err != nil {
			return err
		}
	}
}

func TestEndpointWorkloadInventoryForwardingUsesTrustedMapping(t *testing.T) {
	upstream := &captureWorkloadInventoryServer{received: make(chan *pb.WorkloadInventoryEnvelope, 1)}
	upstreamClient, stopUpstream := startAgentSupplyInventoryTestServer(t, upstream)
	defer stopUpstream()

	endpointServer := NewEndpointServer(0, "endpoint-token", context.Background())
	endpointServer.SetSupplyInventoryClient(upstreamClient)
	endpointServer.connections["endpoint-a"] = &EndpointConnection{Name: "endpoint-a", Token: "endpoint-token"}
	endpointServer.UpdateServerConfig("endpoint-a", &EndpointServerConfig{WorkloadInventory: &pb.WorkloadInventoryConfig{
		Enabled: true, ProtocolVersion: "v1", Capability: "workload_inventory_v1",
		TechnicalResourceId: "trusted-workload-endpoint", CredentialRevision: 11,
	}})
	client, stopEndpoint := startEndpointSupplyInventoryTestServer(t, endpointServer)
	defer stopEndpoint()

	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(
		"authorization", "Bearer endpoint-token", endpointNameMetadata, "endpoint-a",
	))
	stream, err := client.ReportWorkloadInventory(ctx)
	require.NoError(t, err)
	require.NoError(t, stream.Send(&pb.WorkloadInventoryEnvelope{Sequence: 7, SnapshotId: "snapshot-a", CredentialRevision: 999}))
	ack, err := stream.Recv()
	require.NoError(t, err)
	require.True(t, ack.Committed)
	forwarded := <-upstream.received
	require.Equal(t, "trusted-workload-endpoint", forwarded.SourceTechnicalResourceId)
	require.Equal(t, int64(11), forwarded.CredentialRevision)
	require.Equal(t, uint64(7), forwarded.Sequence)

	require.NoError(t, stream.Send(&pb.WorkloadInventoryEnvelope{SourceTechnicalResourceId: "forged"}))
	_, err = stream.Recv()
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestEndpointWorkloadInventoryUpstreamClientAccessIsSynchronized(t *testing.T) {
	upstream := &captureWorkloadInventoryServer{received: make(chan *pb.WorkloadInventoryEnvelope, 1)}
	upstreamClient, stopUpstream := startAgentSupplyInventoryTestServer(t, upstream)
	defer stopUpstream()

	endpointServer := NewEndpointServer(0, "endpoint-token", context.Background())
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		for i := 0; i < 1000; i++ {
			endpointServer.SetSupplyInventoryClient(upstreamClient)
			endpointServer.SetSupplyInventoryClient(nil)
		}
	}()
	go func() {
		defer wait.Done()
		for i := 0; i < 2000; i++ {
			_ = endpointServer.getSupplyInventoryClient()
		}
	}()
	wait.Wait()
}
