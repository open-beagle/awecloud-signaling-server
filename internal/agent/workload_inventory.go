package agent

import (
	"errors"
	"io"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

func (s *EndpointServer) ReportWorkloadInventory(stream pb.EndpointService_ReportWorkloadInventoryServer) error {
	name, err := s.authenticateSupplyInventoryEndpoint(stream.Context())
	if err != nil {
		return err
	}

	s.workloadInventoryMutex.Lock()
	if s.workloadInventoryStreams[name] {
		s.workloadInventoryMutex.Unlock()
		return status.Error(codes.ResourceExhausted, "WORKLOAD_INVENTORY_STREAM_ALREADY_ACTIVE")
	}
	s.workloadInventoryStreams[name] = true
	s.workloadInventoryMutex.Unlock()
	defer func() {
		s.workloadInventoryMutex.Lock()
		delete(s.workloadInventoryStreams, name)
		s.workloadInventoryMutex.Unlock()
	}()
	client := s.getSupplyInventoryClient()
	if client == nil {
		return status.Error(codes.Unavailable, "WORKLOAD_INVENTORY_UPSTREAM_UNAVAILABLE")
	}

	cfg := s.getServerConfig(name)
	if cfg == nil || cfg.WorkloadInventory == nil || !cfg.WorkloadInventory.Enabled ||
		cfg.WorkloadInventory.ProtocolVersion != "v1" || cfg.WorkloadInventory.Capability != "workload_inventory_v1" ||
		cfg.WorkloadInventory.TechnicalResourceId == "" || cfg.WorkloadInventory.CredentialRevision <= 0 {
		return status.Error(codes.FailedPrecondition, "WORKLOAD_INVENTORY_NOT_ENABLED")
	}

	upstream, err := client.ReportWorkloadInventory(stream.Context())
	if err != nil {
		return status.Error(codes.Unavailable, "WORKLOAD_INVENTORY_UPSTREAM_UNAVAILABLE")
	}
	defer upstream.CloseSend()

	for {
		envelope, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if envelope.SourceTechnicalResourceId != "" {
			return status.Error(codes.InvalidArgument, "SOURCE_TECHNICAL_RESOURCE_ID_NOT_ACCEPTED")
		}
		forwarded := proto.Clone(envelope).(*pb.WorkloadInventoryEnvelope)
		forwarded.SourceTechnicalResourceId = cfg.WorkloadInventory.TechnicalResourceId
		forwarded.CredentialRevision = cfg.WorkloadInventory.CredentialRevision
		if err := upstream.Send(forwarded); err != nil {
			return status.Error(codes.Unavailable, "WORKLOAD_INVENTORY_UPSTREAM_SEND_FAILED")
		}
		ack, err := upstream.Recv()
		if err != nil {
			return status.Error(codes.Unavailable, "WORKLOAD_INVENTORY_UPSTREAM_ACK_FAILED")
		}
		if err := stream.Send(ack); err != nil {
			return err
		}
	}
}
