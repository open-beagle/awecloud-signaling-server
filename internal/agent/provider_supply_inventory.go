package agent

import (
	"context"
	"errors"
	"io"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

const endpointNameMetadata = "x-endpoint-name"

func (s *EndpointServer) ReportSupplyInventory(stream pb.EndpointService_ReportSupplyInventoryServer) error {
	name, err := s.authenticateSupplyInventoryEndpoint(stream.Context())
	if err != nil {
		return err
	}

	s.supplyInventoryMutex.Lock()
	if s.supplyInventoryStreams[name] {
		s.supplyInventoryMutex.Unlock()
		return status.Error(codes.ResourceExhausted, "SUPPLY_INVENTORY_STREAM_ALREADY_ACTIVE")
	}
	client := s.supplyInventoryClient
	s.supplyInventoryStreams[name] = true
	s.supplyInventoryMutex.Unlock()
	defer func() {
		s.supplyInventoryMutex.Lock()
		delete(s.supplyInventoryStreams, name)
		s.supplyInventoryMutex.Unlock()
	}()
	if client == nil {
		return status.Error(codes.Unavailable, "SUPPLY_INVENTORY_UPSTREAM_UNAVAILABLE")
	}

	cfg := s.getServerConfig(name)
	if cfg == nil || cfg.SupplyInventory == nil || !cfg.SupplyInventory.Enabled ||
		cfg.SupplyInventory.ProtocolVersion != "v1" || cfg.SupplyInventory.TechnicalResourceId == "" ||
		cfg.SupplyInventory.CredentialRevision <= 0 {
		return status.Error(codes.FailedPrecondition, "SUPPLY_INVENTORY_NOT_ENABLED")
	}

	upstream, err := client.ReportSupplyInventory(stream.Context())
	if err != nil {
		return status.Error(codes.Unavailable, "SUPPLY_INVENTORY_UPSTREAM_UNAVAILABLE")
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

		forwarded := proto.Clone(envelope).(*pb.SupplyInventoryEnvelope)
		forwarded.SourceTechnicalResourceId = cfg.SupplyInventory.TechnicalResourceId
		forwarded.CredentialRevision = cfg.SupplyInventory.CredentialRevision
		if err := upstream.Send(forwarded); err != nil {
			return status.Error(codes.Unavailable, "SUPPLY_INVENTORY_UPSTREAM_SEND_FAILED")
		}
		ack, err := upstream.Recv()
		if err != nil {
			return status.Error(codes.Unavailable, "SUPPLY_INVENTORY_UPSTREAM_ACK_FAILED")
		}
		if err := stream.Send(ack); err != nil {
			return err
		}
	}
}

func (s *EndpointServer) authenticateSupplyInventoryEndpoint(ctx context.Context) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", status.Error(codes.Unauthenticated, "MISSING_ENDPOINT_AUTHORIZATION")
	}
	auth := md.Get("authorization")
	names := md.Get(endpointNameMetadata)
	if len(auth) != 1 || len(names) != 1 || !strings.HasPrefix(auth[0], "Bearer ") ||
		strings.TrimSpace(strings.TrimPrefix(auth[0], "Bearer ")) != s.token {
		return "", status.Error(codes.Unauthenticated, "INVALID_ENDPOINT_TOKEN")
	}
	name := strings.TrimSpace(names[0])
	if name == "" {
		return "", status.Error(codes.Unauthenticated, "MISSING_ENDPOINT_NAME")
	}
	s.connMutex.RLock()
	connection := s.connections[name]
	s.connMutex.RUnlock()
	if connection == nil || connection.Token != s.token {
		return "", status.Error(codes.PermissionDenied, "ENDPOINT_NOT_REGISTERED")
	}
	return name, nil
}
