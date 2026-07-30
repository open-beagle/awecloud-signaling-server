package grpc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/service"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

func TestReportWorkloadInventoryUsesDeployTokenAndStableAcks(t *testing.T) {
	database := newWorkloadInventoryGRPCDatabase(t)
	oldDB := db.DB
	db.DB = database
	t.Cleanup(func() { db.DB = oldDB })
	clusterDigest, namespaceUID := seedWorkloadInventoryAgent(t, database, "agent-token", 1001, "agent-a", "technical-agent-a")

	cfg := &config.ServerConfig{FeatureFlags: config.FeatureFlagsSection{ResourceModelWrite: true, ResourceReconciliation: true}}
	workload := service.NewWorkloadInventoryService(database)
	server := &AgentServiceServer{config: cfg, workloadInventory: workload}
	client, cleanup := startSupplyInventoryAgentServer(t, server)
	defer cleanup()

	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer agent-token"))
	stream, err := client.ReportWorkloadInventory(ctx)
	require.NoError(t, err)
	envelope := testWorkloadInventoryEnvelope(t, clusterDigest, namespaceUID, 1, "snapshot-a")
	require.NoError(t, stream.Send(envelope))
	ack, err := stream.Recv()
	require.NoError(t, err)
	require.Equal(t, "WORKLOAD_ACCEPTED", ack.ResultCode)
	require.True(t, ack.Committed)
	require.Equal(t, uint64(1), ack.AcceptedSequence)
	require.NotNil(t, ack.ServerReceivedAt)

	require.NoError(t, stream.Send(envelope))
	replay, err := stream.Recv()
	require.NoError(t, err)
	require.Equal(t, "WORKLOAD_REPLAYED", replay.ResultCode)
	require.True(t, replay.Replayed)

	gap := testWorkloadInventoryEnvelope(t, clusterDigest, namespaceUID, 3, "snapshot-gap")
	require.NoError(t, stream.Send(gap))
	retry, err := stream.Recv()
	require.NoError(t, err)
	require.Equal(t, "WORKLOAD_SEQUENCE_GAP", retry.ResultCode)
	require.True(t, retry.Retryable)
	require.NotZero(t, retry.RetryAfterMs)

	config := server.workloadInventoryConfigForBinding(context.Background(), model.TechnicalResourceBindingLegacyNode, "1001")
	require.NotNil(t, config)
	require.Equal(t, "workload_inventory_v1", config.Capability)
}

func TestReportWorkloadInventoryFailsClosedForFeatureAndUntrustedScope(t *testing.T) {
	database := newWorkloadInventoryGRPCDatabase(t)
	oldDB := db.DB
	db.DB = database
	t.Cleanup(func() { db.DB = oldDB })
	clusterDigest, namespaceUID := seedWorkloadInventoryAgent(t, database, "agent-token", 1001, "agent-a", "technical-agent-a")
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer agent-token"))

	disabled := &AgentServiceServer{
		config:            &config.ServerConfig{FeatureFlags: config.FeatureFlagsSection{ResourceModelWrite: true}},
		workloadInventory: service.NewWorkloadInventoryService(database),
	}
	disabledClient, disabledCleanup := startSupplyInventoryAgentServer(t, disabled)
	disabledStream, err := disabledClient.ReportWorkloadInventory(ctx)
	require.NoError(t, err)
	require.NoError(t, disabledStream.Send(testWorkloadInventoryEnvelope(t, clusterDigest, namespaceUID, 1, "snapshot-disabled")))
	_, err = disabledStream.Recv()
	require.Equal(t, codes.Unavailable, status.Code(err))
	disabledCleanup()

	enabled := &AgentServiceServer{
		config:            &config.ServerConfig{FeatureFlags: config.FeatureFlagsSection{ResourceModelWrite: true, ResourceReconciliation: true}},
		workloadInventory: service.NewWorkloadInventoryService(database),
	}
	client, cleanup := startSupplyInventoryAgentServer(t, enabled)
	defer cleanup()
	stream, err := client.ReportWorkloadInventory(ctx)
	require.NoError(t, err)
	forged := testWorkloadInventoryEnvelope(t, strings.Repeat("f", 64), namespaceUID, 1, "snapshot-forged")
	require.NoError(t, stream.Send(forged))
	_, err = stream.Recv()
	require.Equal(t, codes.PermissionDenied, status.Code(err))
}

func newWorkloadInventoryGRPCDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	database := newSupplyInventoryGRPCDatabase(t)
	require.NoError(t, database.AutoMigrate(
		&model.Tenant{}, &model.ResourceAllocation{}, &model.ResourceAllocationItem{},
		&model.WorkloadInventoryReceipt{}, &model.WorkloadInventoryBatch{}, &model.WorkloadObservation{}, &model.WorkloadObservationSource{},
		&model.TenantResource{}, &model.TenantResourceSource{}, &model.TenantResourceReviewDecision{}, &model.TenantResourceTargetRevision{},
		&model.OutboxEvent{}, &model.ConsumerRevision{}, &model.AuditLog{},
	))
	return database
}

func seedWorkloadInventoryAgent(t *testing.T, database *gorm.DB, token string, nodeID uint64, nodeName, technicalID string) (string, string) {
	t.Helper()
	seedSupplyInventoryAgent(t, database, token, nodeID, nodeName, technicalID)
	now := time.Now().UTC()
	var technical model.TechnicalResource
	require.NoError(t, database.First(&technical, "id = ?", technicalID).Error)
	clusterDigest := strings.Repeat("a", 64)
	resource := model.PlatformResource{
		ID: uuid.NewString(), ProviderID: technical.ProviderID, Type: model.SupplyResourceKubernetes, StableKey: clusterDigest,
		DisplayName: "Workload Cluster", LifecycleState: model.PlatformResourceActive, HealthState: model.ResourceHealthOnline,
		CapabilityRevision: 1, AllocatableScopeCount: 1, RowVersion: 1,
	}
	require.NoError(t, database.Create(&resource).Error)
	candidate := model.SupplyCandidate{
		ID: uuid.NewString(), ProviderID: technical.ProviderID, TechnicalResourceID: technical.ID,
		ResourceType: model.SupplyResourceKubernetes, StableKey: clusterDigest, IdentityQuality: model.SupplyIdentityStrong,
		PayloadHash:         strings.Repeat("b", 64),
		ObservationSnapshot: `{"cluster_uid":"cluster-a","capabilities":["workload_inventory_v1"],"namespaces":[{"uid":"namespace-a","name":"workloads","labels":{},"status":"Active"}]}`,
		FirstObservedAt:     now, LastObservedAt: now, LeaseExpiresAt: now.Add(time.Hour), ReviewState: model.SupplyCandidateLinked, RowVersion: 1,
	}
	require.NoError(t, database.Create(&candidate).Error)
	require.NoError(t, database.Create(&model.PlatformResourceSource{
		ID: uuid.NewString(), ProviderID: technical.ProviderID, PlatformResourceID: resource.ID,
		SupplyCandidateID: candidate.ID, IsPrimary: true, LinkedAt: now, LastConfirmedAt: now,
	}).Error)
	namespaceUID := "namespace-a"
	namespace := model.NamespaceObservation{
		ID: uuid.NewString(), ProviderID: technical.ProviderID, ClusterResourceID: resource.ID, NamespaceUID: namespaceUID,
		Name: "workloads", LabelSnapshot: "{}", Revision: 1, ObservedAt: now, LeaseExpiresAt: now.Add(time.Hour), State: model.NamespaceObservationObserved,
	}
	require.NoError(t, database.Create(&namespace).Error)
	clusterScope := model.ResourceScope{
		ID: uuid.NewString(), ProviderID: technical.ProviderID, PlatformResourceID: resource.ID, Type: model.ResourceScopeCluster,
		StableKey: clusterDigest, LifecycleState: model.ResourceScopeActive, IsolationMode: model.ResourceScopeIsolationNone,
		ConfigRevision: 1, EvidenceRevision: 1, RowVersion: 1,
	}
	require.NoError(t, database.Create(&clusterScope).Error)
	namespaceScope := model.ResourceScope{
		ID: uuid.NewString(), ProviderID: technical.ProviderID, PlatformResourceID: resource.ID, Type: model.ResourceScopeNamespace,
		StableKey: strings.Repeat("c", 64), ParentID: &clusterScope.ID, NamespaceObservationID: &namespace.ID,
		LifecycleState: model.ResourceScopeAllocatable, IsolationMode: model.ResourceScopeIsolationNamespaceIsolated,
		ConfigRevision: 1, EvidenceRevision: 1, RowVersion: 1,
	}
	require.NoError(t, database.Create(&namespaceScope).Error)
	return clusterDigest, namespaceUID
}

func testWorkloadInventoryEnvelope(t *testing.T, clusterDigest, namespaceUID string, sequence uint64, snapshotID string) *pb.WorkloadInventoryEnvelope {
	t.Helper()
	ports := []*pb.WorkloadServicePort{{
		ServiceUid: "service-a", ServiceName: "api", ClusterIp: "10.96.0.10", PortName: "https",
		PortNumber: 443, Protocol: "TCP", Ready: true,
		LabelsAllowlist: map[string]string{"signal.beagle.io/expose": "true"},
	}}
	payload, err := marshalWorkloadInventoryPayload(model.WorkloadObservationServicePort, ports, nil)
	require.NoError(t, err)
	digest := sha256.Sum256(payload)
	now := time.Now().UTC()
	return &pb.WorkloadInventoryEnvelope{
		SchemaVersion: 1, SourceEpoch: "epoch-a", Sequence: sequence, SnapshotId: snapshotID,
		BatchIndex: 0, BatchCount: 1, ObservedAt: timestamppb.New(now.Add(-time.Second)), SentAt: timestamppb.New(now),
		PayloadHash: hex.EncodeToString(digest[:]), CredentialRevision: 1, ClusterIdentityDigest: clusterDigest,
		NamespaceUid: namespaceUID, NamespaceName: "workloads", SnapshotKind: string(model.WorkloadObservationServicePort), ServicePorts: ports,
	}
}
