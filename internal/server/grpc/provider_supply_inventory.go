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
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/service"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

const supplyInventoryProtocolVersion = "v1"

type supplyInventoryPayload struct {
	KubernetesClusters []supplyInventoryCluster `json:"kubernetes_clusters"`
}

type supplyInventoryCluster struct {
	ClusterUID             string                     `json:"cluster_uid"`
	KubeSystemNamespaceUID string                     `json:"kube_system_namespace_uid"`
	CASHA256               string                     `json:"ca_sha256"`
	DisplayName            string                     `json:"display_name"`
	KubernetesVersion      string                     `json:"kubernetes_version"`
	Capabilities           []string                   `json:"capabilities"`
	Namespaces             []supplyInventoryNamespace `json:"namespaces"`
}

type supplyInventoryNamespace struct {
	UID    string            `json:"uid"`
	Name   string            `json:"name"`
	Labels map[string]string `json:"labels"`
	Status string            `json:"status"`
}

func (s *AgentServiceServer) ReportSupplyInventory(stream pb.AgentService_ReportSupplyInventoryServer) error {
	if s == nil || s.config == nil || s.providerSupply == nil ||
		!s.config.FeatureFlags.Enabled(config.FeatureResourceModelWrite) ||
		!s.config.FeatureFlags.Enabled(config.FeatureResourceReconciliation) {
		return status.Error(codes.Unavailable, "FEATURE_DISABLED")
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

		ack, receiveErr := s.receiveSupplyInventoryEnvelope(stream.Context(), credential, envelope)
		if receiveErr != nil {
			ack = supplyInventoryErrorAck(envelope, receiveErr)
		}
		if err := stream.Send(ack); err != nil {
			return err
		}
	}
}

func authenticateSupplyInventoryAgent(ctx context.Context) (service.TechnicalResourceCredential, error) {
	token, err := bearerTokenFromIncomingContext(ctx)
	if err != nil {
		return service.TechnicalResourceCredential{}, err
	}

	if credential, ok, err := authenticateTechnicalResourceInventoryToken(ctx, token); ok || err != nil {
		return credential, err
	}
	return authenticateLegacyInventoryToken(ctx, token)
}

func authenticateLegacyInventoryToken(ctx context.Context, token string) (service.TechnicalResourceCredential, error) {
	var deployToken model.DeployToken
	if err := db.DB.WithContext(ctx).Where("token = ? AND status = ?", token, model.DeployTokenStatusBound).First(&deployToken).Error; err != nil {
		return service.TechnicalResourceCredential{}, status.Error(codes.Unauthenticated, "INVALID_AGENT_TOKEN")
	}
	var user model.User
	if err := db.DB.WithContext(ctx).First(&user, deployToken.UserID).Error; err != nil || !user.Enabled || user.Role != model.UserRoleAgent {
		return service.TechnicalResourceCredential{}, status.Error(codes.PermissionDenied, "AGENT_NOT_ALLOWED")
	}
	var node model.Node
	if err := db.DB.WithContext(ctx).Where("user_id = ? AND type = ? AND name = ?", deployToken.UserID, model.NodeTypeAgent, deployToken.Name).First(&node).Error; err != nil {
		return service.TechnicalResourceCredential{}, status.Error(codes.PermissionDenied, "AGENT_NODE_UNBOUND")
	}

	return service.TechnicalResourceCredential{
		SourceType: model.TechnicalResourceBindingLegacyNode,
		SourceID:   strconv.FormatUint(node.ID, 10),
	}, nil
}

func authenticateTechnicalResourceInventoryToken(ctx context.Context, token string) (service.TechnicalResourceCredential, bool, error) {
	var deployToken model.TechnicalResourceDeployToken
	err := db.DB.WithContext(ctx).
		Where("token = ? AND status = ?", token, model.TechnicalResourceDeployTokenConsumed).
		First(&deployToken).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return service.TechnicalResourceCredential{}, false, nil
	}
	if err != nil {
		return service.TechnicalResourceCredential{}, true, status.Error(codes.Unauthenticated, "INVALID_AGENT_TOKEN")
	}
	var resource model.TechnicalResource
	if err := db.DB.WithContext(ctx).First(&resource, "id = ?", deployToken.TechnicalResourceID).Error; err != nil {
		return service.TechnicalResourceCredential{}, true, status.Error(codes.PermissionDenied, "AGENT_NOT_ALLOWED")
	}
	if resource.RuntimeUserID != deployToken.RuntimeUserID || resource.LifecycleState != model.TechnicalResourceRegistered {
		return service.TechnicalResourceCredential{}, true, status.Error(codes.PermissionDenied, "AGENT_NOT_ALLOWED")
	}
	var node model.Node
	if err := db.DB.WithContext(ctx).Where("user_id = ? AND type = ? AND name = ?", deployToken.RuntimeUserID, model.NodeTypeAgent, deployToken.Name).First(&node).Error; err != nil {
		return service.TechnicalResourceCredential{}, true, status.Error(codes.PermissionDenied, "AGENT_NODE_UNBOUND")
	}
	return service.TechnicalResourceCredential{
		SourceType: model.TechnicalResourceBindingLegacyNode,
		SourceID:   strconv.FormatUint(node.ID, 10),
	}, true, nil
}

func bearerTokenFromIncomingContext(ctx context.Context) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", status.Error(codes.Unauthenticated, "MISSING_AUTHORIZATION")
	}
	values := md.Get("authorization")
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") {
		return "", status.Error(codes.Unauthenticated, "MISSING_AUTHORIZATION")
	}
	token := strings.TrimSpace(strings.TrimPrefix(values[0], "Bearer "))
	if token == "" {
		return "", status.Error(codes.Unauthenticated, "MISSING_AUTHORIZATION")
	}
	return token, nil
}

func (s *AgentServiceServer) receiveSupplyInventoryEnvelope(ctx context.Context, authenticated service.TechnicalResourceCredential, envelope *pb.SupplyInventoryEnvelope) (*pb.SupplyInventoryAck, error) {
	if envelope == nil || envelope.Sequence > math.MaxInt64 || envelope.SchemaVersion > math.MaxInt32 ||
		envelope.BatchIndex > math.MaxInt32 || envelope.BatchCount > math.MaxInt32 || envelope.CredentialRevision <= 0 {
		return nil, service.ErrProviderSupplyInvalidInput
	}
	if envelope.ObservedAt == nil || envelope.SentAt == nil || envelope.ObservedAt.CheckValid() != nil || envelope.SentAt.CheckValid() != nil {
		return nil, service.ErrProviderSupplyInvalidInput
	}
	observedAt, sentAt := envelope.ObservedAt.AsTime(), envelope.SentAt.AsTime()
	if observedAt.IsZero() || sentAt.IsZero() || sentAt.Before(observedAt) || sentAt.After(time.Now().UTC().Add(10*time.Minute)) {
		return nil, service.ErrProviderSupplyInvalidInput
	}

	payload, err := marshalSupplyInventoryPayload(envelope.KubernetesClusters)
	if err != nil {
		return nil, service.ErrProviderSupplyInvalidInput
	}
	authenticated.CredentialRevision = envelope.CredentialRevision
	ack, err := s.providerSupply.ReceiveSupplyInventoryBatch(ctx, service.ReceiveSupplyInventoryBatchInput{
		AuthenticatedSource:       authenticated,
		SourceTechnicalResourceID: envelope.SourceTechnicalResourceId,
		SourceCredentialRevision:  envelope.CredentialRevision,
		SchemaVersion:             int(envelope.SchemaVersion),
		SourceEpoch:               envelope.SourceEpoch,
		Sequence:                  int64(envelope.Sequence),
		SnapshotID:                envelope.SnapshotId,
		BatchIndex:                int(envelope.BatchIndex),
		BatchCount:                int(envelope.BatchCount),
		PayloadHash:               envelope.PayloadHash,
		Payload:                   payload,
	})
	if err != nil {
		return nil, err
	}
	return &pb.SupplyInventoryAck{
		AcceptedSequence: uint64(ack.AcceptedSequence), SnapshotId: ack.SnapshotID,
		ResultCode: ack.ResultCode, Replay: ack.Replay, SnapshotCommitted: ack.SnapshotCommitted,
	}, nil
}

func marshalSupplyInventoryPayload(clusters []*pb.KubernetesClusterInventory) ([]byte, error) {
	payload := supplyInventoryPayload{KubernetesClusters: make([]supplyInventoryCluster, 0, len(clusters))}
	for _, cluster := range clusters {
		if cluster == nil {
			return nil, service.ErrProviderSupplyInvalidInput
		}
		item := supplyInventoryCluster{
			ClusterUID: cluster.ClusterUid, KubeSystemNamespaceUID: cluster.KubeSystemNamespaceUid,
			CASHA256: cluster.CaSha256, DisplayName: cluster.DisplayName,
			KubernetesVersion: cluster.KubernetesVersion,
			Capabilities:      append([]string(nil), cluster.Capabilities...),
			Namespaces:        make([]supplyInventoryNamespace, 0, len(cluster.Namespaces)),
		}
		for _, namespace := range cluster.Namespaces {
			if namespace == nil {
				return nil, service.ErrProviderSupplyInvalidInput
			}
			item.Namespaces = append(item.Namespaces, supplyInventoryNamespace{
				UID: namespace.Uid, Name: namespace.Name, Labels: namespace.Labels, Status: namespace.Status,
			})
		}
		payload.KubernetesClusters = append(payload.KubernetesClusters, item)
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

func supplyInventoryErrorAck(envelope *pb.SupplyInventoryEnvelope, err error) *pb.SupplyInventoryAck {
	ack := &pb.SupplyInventoryAck{ResultCode: "INTERNAL", Retryable: true, RetryAfterSeconds: 5}
	if envelope != nil {
		ack.SnapshotId = envelope.SnapshotId
	}
	for target, code := range map[error]string{
		service.ErrTechnicalResourceUnbound:   "TECHNICAL_RESOURCE_UNBOUND",
		service.ErrTechnicalResourceDisabled:  "TECHNICAL_RESOURCE_DISABLED",
		service.ErrTechnicalResourceRetired:   "TECHNICAL_RESOURCE_RETIRED",
		service.ErrCredentialRevisionStale:    "CREDENTIAL_REVISION_STALE",
		service.ErrSourceEpochStale:           "SOURCE_EPOCH_STALE",
		service.ErrSourceSequenceConflict:     "SOURCE_SEQUENCE_CONFLICT",
		service.ErrSourceSequenceOutOfOrder:   "SOURCE_SEQUENCE_OUT_OF_ORDER",
		service.ErrSnapshotMetadataConflict:   "SNAPSHOT_METADATA_CONFLICT",
		service.ErrSupplyPayloadHashMismatch:  "PAYLOAD_HASH_MISMATCH",
		service.ErrProviderSupplyInvalidInput: "INVALID_INVENTORY",
	} {
		if errors.Is(err, target) {
			ack.ResultCode = code
			ack.Retryable = target == service.ErrSourceSequenceOutOfOrder
			if !ack.Retryable {
				ack.RetryAfterSeconds = 0
			}
			break
		}
	}
	return ack
}

func (s *AgentServiceServer) supplyInventoryConfigForBinding(ctx context.Context, sourceType model.TechnicalResourceBindingSourceType, sourceID string) *pb.SupplyInventoryConfig {
	if s == nil || s.config == nil ||
		!s.config.FeatureFlags.Enabled(config.FeatureResourceModelWrite) ||
		!s.config.FeatureFlags.Enabled(config.FeatureResourceReconciliation) {
		return nil
	}
	var resource model.TechnicalResource
	err := db.DB.WithContext(ctx).Table("technical_resource AS tr").
		Select("tr.*").
		Joins("JOIN technical_resource_binding AS b ON b.technical_resource_id = tr.id").
		Where("b.source_type = ? AND b.source_id = ? AND b.enabled = ? AND tr.lifecycle_state = ? AND b.credential_revision = tr.credential_revision",
			sourceType, sourceID, true, model.TechnicalResourceRegistered).
		First(&resource).Error
	if err != nil {
		return nil
	}
	return &pb.SupplyInventoryConfig{
		Enabled: true, ProtocolVersion: supplyInventoryProtocolVersion,
		TechnicalResourceId: resource.ID, CredentialRevision: resource.CredentialRevision,
	}
}

func (s *AgentServiceServer) authorizeSupplyInventoryNegotiation(ctx context.Context, nodeID uint64) bool {
	if nodeID == 0 {
		return false
	}
	credential, err := authenticateSupplyInventoryAgent(ctx)
	return err == nil && credential.SourceID == strconv.FormatUint(nodeID, 10)
}
