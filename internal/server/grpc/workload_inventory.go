package grpc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/service"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

const workloadInventoryProtocolVersion = "v1"

type workloadInventoryPayload struct {
	ServicePorts []workloadServicePortPayload `json:"service_ports,omitempty"`
	Containers   []workloadContainerPayload   `json:"containers,omitempty"`
}

type workloadServicePortPayload struct {
	ServiceUID      string            `json:"service_uid"`
	ServiceName     string            `json:"service_name"`
	ClusterIP       string            `json:"cluster_ip"`
	PortName        string            `json:"port_name"`
	PortNumber      int               `json:"port_number"`
	Protocol        string            `json:"protocol"`
	Ready           bool              `json:"ready"`
	LabelsAllowlist map[string]string `json:"labels_allowlist"`
}

type workloadContainerPayload struct {
	WorkloadUID     string            `json:"workload_uid"`
	WorkloadKind    string            `json:"workload_kind"`
	WorkloadName    string            `json:"workload_name"`
	PodUID          string            `json:"pod_uid"`
	PodName         string            `json:"pod_name"`
	ContainerName   string            `json:"container_name"`
	Ready           bool              `json:"ready"`
	LabelsAllowlist map[string]string `json:"labels_allowlist"`
}

func (s *AgentServiceServer) ReportWorkloadInventory(stream pb.AgentService_ReportWorkloadInventoryServer) error {
	if s == nil || s.config == nil || s.workloadInventory == nil ||
		!s.config.FeatureFlags.Enabled(config.FeatureResourceModelWrite) ||
		!s.config.FeatureFlags.Enabled(config.FeatureResourceReconciliation) {
		return status.Error(codes.Unavailable, "WORKLOAD_FEATURE_DISABLED")
	}
	credential, err := authenticateSupplyInventoryAgent(stream.Context())
	if err != nil {
		return err
	}
	for {
		envelope, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		ack, receiveErr := s.receiveWorkloadInventoryEnvelope(stream.Context(), credential, envelope)
		if receiveErr != nil {
			if workloadInventoryStreamFatal(receiveErr) {
				return workloadInventoryFatalStatus(receiveErr)
			}
			ack = workloadInventoryErrorAck(envelope, receiveErr)
		}
		if err := stream.Send(ack); err != nil {
			return err
		}
	}
}

func (s *AgentServiceServer) receiveWorkloadInventoryEnvelope(ctx context.Context, authenticated service.TechnicalResourceCredential, envelope *pb.WorkloadInventoryEnvelope) (*pb.WorkloadInventoryAck, error) {
	if envelope == nil || envelope.Sequence > math.MaxInt64 || envelope.SchemaVersion > math.MaxInt32 ||
		envelope.BatchIndex > math.MaxInt32 || envelope.BatchCount > math.MaxInt32 || envelope.CredentialRevision <= 0 {
		return nil, service.ErrWorkloadInventoryInvalidInput
	}
	if envelope.ObservedAt == nil || envelope.SentAt == nil || envelope.ObservedAt.CheckValid() != nil || envelope.SentAt.CheckValid() != nil {
		return nil, service.ErrWorkloadInventoryInvalidInput
	}
	observedAt, sentAt := envelope.ObservedAt.AsTime().UTC(), envelope.SentAt.AsTime().UTC()
	if observedAt.IsZero() || sentAt.IsZero() || sentAt.Before(observedAt) || sentAt.After(time.Now().UTC().Add(10*time.Minute)) {
		return nil, service.ErrWorkloadInventoryInvalidInput
	}
	kind := model.WorkloadObservationKind(strings.TrimSpace(envelope.SnapshotKind))
	payload, err := marshalWorkloadInventoryPayload(kind, envelope.ServicePorts, envelope.Containers)
	if err != nil {
		return nil, err
	}
	authenticated.CredentialRevision = envelope.CredentialRevision
	ack, err := s.workloadInventory.ReceiveBatch(ctx, service.ReceiveWorkloadInventoryBatchInput{
		AuthenticatedSource: authenticated, SourceTechnicalResourceID: envelope.SourceTechnicalResourceId,
		SourceCredentialRevision: envelope.CredentialRevision, SchemaVersion: int(envelope.SchemaVersion),
		SourceEpoch: envelope.SourceEpoch, Sequence: int64(envelope.Sequence), SnapshotID: envelope.SnapshotId,
		BatchIndex: int(envelope.BatchIndex), BatchCount: int(envelope.BatchCount),
		ClusterIdentityDigest: envelope.ClusterIdentityDigest, NamespaceUID: envelope.NamespaceUid,
		NamespaceName: envelope.NamespaceName, Kind: kind, ObservedAt: observedAt,
		PayloadHash: envelope.PayloadHash, Payload: payload,
	})
	if err != nil {
		return nil, err
	}
	return &pb.WorkloadInventoryAck{
		AcceptedSequence: uint64(ack.AcceptedSequence), SnapshotId: ack.SnapshotID, BatchIndex: uint32(ack.BatchIndex),
		ResultCode: ack.ResultCode, Replayed: ack.Replayed, Committed: ack.Committed, Retryable: ack.Retryable,
		ServerReceivedAt: timestamppb.New(ack.ServerReceivedAt),
	}, nil
}

func marshalWorkloadInventoryPayload(kind model.WorkloadObservationKind, servicePorts []*pb.WorkloadServicePort, containers []*pb.WorkloadContainer) ([]byte, error) {
	payload := workloadInventoryPayload{}
	switch kind {
	case model.WorkloadObservationServicePort:
		if len(containers) != 0 {
			return nil, service.ErrWorkloadInventoryInvalidInput
		}
		payload.ServicePorts = make([]workloadServicePortPayload, 0, len(servicePorts))
		for _, item := range servicePorts {
			if item == nil || item.PortNumber > math.MaxInt32 {
				return nil, service.ErrWorkloadInventoryInvalidInput
			}
			payload.ServicePorts = append(payload.ServicePorts, workloadServicePortPayload{
				ServiceUID: item.ServiceUid, ServiceName: item.ServiceName, ClusterIP: item.ClusterIp,
				PortName: item.PortName, PortNumber: int(item.PortNumber), Protocol: item.Protocol,
				Ready: item.Ready, LabelsAllowlist: item.LabelsAllowlist,
			})
		}
	case model.WorkloadObservationContainer:
		if len(servicePorts) != 0 {
			return nil, service.ErrWorkloadInventoryInvalidInput
		}
		payload.Containers = make([]workloadContainerPayload, 0, len(containers))
		for _, item := range containers {
			if item == nil {
				return nil, service.ErrWorkloadInventoryInvalidInput
			}
			payload.Containers = append(payload.Containers, workloadContainerPayload{
				WorkloadUID: item.WorkloadUid, WorkloadKind: item.WorkloadKind, WorkloadName: item.WorkloadName,
				PodUID: item.PodUid, PodName: item.PodName, ContainerName: item.ContainerName,
				Ready: item.Ready, LabelsAllowlist: item.LabelsAllowlist,
			})
		}
	default:
		return nil, service.ErrWorkloadInventoryInvalidInput
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	var canonical any
	if err := json.Unmarshal(raw, &canonical); err != nil {
		return nil, err
	}
	return json.Marshal(canonical)
}

func workloadInventoryErrorAck(envelope *pb.WorkloadInventoryEnvelope, err error) *pb.WorkloadInventoryAck {
	ack := &pb.WorkloadInventoryAck{ResultCode: "WORKLOAD_INTERNAL", Retryable: true, RetryAfterMs: 1000, ServerReceivedAt: timestamppb.Now()}
	if envelope != nil {
		ack.SnapshotId, ack.BatchIndex = envelope.SnapshotId, envelope.BatchIndex
	}
	for target, code := range map[error]string{
		service.ErrWorkloadPayloadHashMismatch:      "WORKLOAD_PAYLOAD_HASH_MISMATCH",
		service.ErrWorkloadSequenceGap:              "WORKLOAD_SEQUENCE_GAP",
		service.ErrWorkloadSequenceConflict:         "WORKLOAD_SEQUENCE_CONFLICT",
		service.ErrWorkloadSourceEpochStale:         "WORKLOAD_SOURCE_EPOCH_STALE",
		service.ErrWorkloadSnapshotMetadataConflict: "WORKLOAD_SNAPSHOT_METADATA_CONFLICT",
		service.ErrWorkloadSnapshotIncomplete:       "WORKLOAD_SNAPSHOT_INCOMPLETE",
		service.ErrWorkloadProtocolUnsupported:      "WORKLOAD_PROTOCOL_UNSUPPORTED",
		service.ErrWorkloadIdentityInsufficient:     "WORKLOAD_IDENTITY_INSUFFICIENT",
		service.ErrWorkloadPayloadForbidden:         "WORKLOAD_PAYLOAD_FORBIDDEN",
		service.ErrWorkloadInventoryInvalidInput:    "WORKLOAD_INVALID_INVENTORY",
	} {
		if errors.Is(err, target) {
			ack.ResultCode = code
			ack.Retryable = target == service.ErrWorkloadSequenceGap
			if !ack.Retryable {
				ack.RetryAfterMs = 0
			}
			break
		}
	}
	return ack
}

func workloadInventoryStreamFatal(err error) bool {
	for _, target := range []error{
		service.ErrTechnicalResourceUnbound, service.ErrTechnicalResourceDisabled, service.ErrTechnicalResourceRetired,
		service.ErrCredentialRevisionStale, service.ErrWorkloadScopeNotTrusted, service.ErrWorkloadTechnicalCapabilityDenied,
	} {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}

func workloadInventoryFatalStatus(err error) error {
	code, message := codes.PermissionDenied, "WORKLOAD_SOURCE_NOT_ALLOWED"
	if errors.Is(err, service.ErrCredentialRevisionStale) || errors.Is(err, service.ErrTechnicalResourceUnbound) {
		code, message = codes.Unauthenticated, "WORKLOAD_SOURCE_NOT_AUTHENTICATED"
	} else if errors.Is(err, service.ErrWorkloadScopeNotTrusted) {
		message = "WORKLOAD_SCOPE_NOT_TRUSTED"
	} else if errors.Is(err, service.ErrWorkloadTechnicalCapabilityDenied) {
		message = "WORKLOAD_CAPABILITY_NOT_ALLOWED"
	}
	return status.Error(code, message)
}

func (s *AgentServiceServer) workloadInventoryConfigForBinding(ctx context.Context, sourceType model.TechnicalResourceBindingSourceType, sourceID string) *pb.WorkloadInventoryConfig {
	if s == nil || s.config == nil || s.workloadInventory == nil ||
		!s.config.FeatureFlags.Enabled(config.FeatureResourceModelWrite) ||
		!s.config.FeatureFlags.Enabled(config.FeatureResourceReconciliation) {
		return nil
	}
	resource, err := s.workloadInventory.ReportingSource(ctx, sourceType, sourceID)
	if err != nil {
		return nil
	}
	return &pb.WorkloadInventoryConfig{
		Enabled: true, ProtocolVersion: workloadInventoryProtocolVersion, Capability: "workload_inventory_v1",
		TechnicalResourceId: resource.ID, CredentialRevision: resource.CredentialRevision,
	}
}

func nodeSourceID(nodeID uint64) string { return strconv.FormatUint(nodeID, 10) }
