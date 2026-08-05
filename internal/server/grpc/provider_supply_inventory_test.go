package grpc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/service"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

func TestReportSupplyInventoryAuthenticatesAgentAndReturnsStableAcks(t *testing.T) {
	database := newSupplyInventoryGRPCDatabase(t)
	oldDB := db.DB
	db.DB = database
	t.Cleanup(func() { db.DB = oldDB })

	seedSupplyInventoryAgent(t, database, "agent-token", 1001, "agent-a", "technical-agent-a")
	cfg := &config.ServerConfig{FeatureFlags: config.FeatureFlagsSection{
		ResourceModelWrite: true, ResourceReconciliation: true,
	}}
	server := &AgentServiceServer{config: cfg, providerSupply: service.NewProviderSupplyService(database)}
	client, cleanup := startSupplyInventoryAgentServer(t, server)
	defer cleanup()

	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer agent-token"))
	stream, err := client.ReportSupplyInventory(ctx)
	require.NoError(t, err)
	envelope := testSupplyInventoryEnvelope(t, 1)
	require.NoError(t, stream.Send(envelope))
	ack, err := stream.Recv()
	require.NoError(t, err)
	require.Equal(t, "SNAPSHOT_COMMITTED", ack.ResultCode)
	require.Equal(t, uint64(1), ack.AcceptedSequence)
	require.True(t, ack.SnapshotCommitted)

	require.NoError(t, stream.Send(envelope))
	replay, err := stream.Recv()
	require.NoError(t, err)
	require.True(t, replay.Replay)
	require.True(t, replay.SnapshotCommitted)

	outOfOrder := testSupplyInventoryEnvelope(t, 3)
	require.NoError(t, stream.Send(outOfOrder))
	retry, err := stream.Recv()
	require.NoError(t, err)
	require.Equal(t, "SOURCE_SEQUENCE_OUT_OF_ORDER", retry.ResultCode)
	require.True(t, retry.Retryable)
	require.NotZero(t, retry.RetryAfterSeconds)
}

func TestReportSupplyInventoryFailsClosedWithoutTokenOrFlags(t *testing.T) {
	database := newSupplyInventoryGRPCDatabase(t)
	oldDB := db.DB
	db.DB = database
	t.Cleanup(func() { db.DB = oldDB })
	seedSupplyInventoryAgent(t, database, "agent-token", 1001, "agent-a", "technical-agent-a")

	server := &AgentServiceServer{config: &config.ServerConfig{FeatureFlags: config.FeatureFlagsSection{
		ResourceModelWrite: true, ResourceReconciliation: true,
	}}, providerSupply: service.NewProviderSupplyService(database)}
	client, cleanup := startSupplyInventoryAgentServer(t, server)
	stream, err := client.ReportSupplyInventory(context.Background())
	require.NoError(t, err)
	_ = stream.Send(testSupplyInventoryEnvelope(t, 1))
	_, err = stream.Recv()
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	cleanup()

	disabledServer := &AgentServiceServer{config: &config.ServerConfig{FeatureFlags: config.FeatureFlagsSection{
		ResourceModelWrite: true, ResourceReconciliation: false,
	}}, providerSupply: service.NewProviderSupplyService(database)}
	disabledClient, disabledCleanup := startSupplyInventoryAgentServer(t, disabledServer)
	defer disabledCleanup()
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer agent-token"))
	disabled, err := disabledClient.ReportSupplyInventory(ctx)
	require.NoError(t, err)
	_ = disabled.Send(testSupplyInventoryEnvelope(t, 1))
	_, err = disabled.Recv()
	require.Equal(t, codes.Unavailable, status.Code(err))
}

func newSupplyInventoryGRPCDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(
		&model.User{}, &model.ResourceProvider{}, &model.Node{}, &model.DeployToken{},
		&model.TechnicalResource{}, &model.TechnicalResourceBinding{}, &model.SupplyInventoryReceipt{}, &model.SupplyCandidate{},
		&model.PlatformResource{}, &model.PlatformResourceSource{}, &model.NamespaceObservation{}, &model.ResourceScope{},
	))
	return database
}

func seedSupplyInventoryAgent(t *testing.T, database *gorm.DB, token string, nodeID uint64, nodeName, resourceID string) {
	t.Helper()
	user := model.User{Name: "inventory-agent", Role: model.UserRoleAgent, SecretHash: "fixture", Enabled: true}
	require.NoError(t, database.Create(&user).Error)
	require.NoError(t, database.Create(&model.Node{ID: nodeID, UserID: user.ID, Name: nodeName, Type: model.NodeTypeAgent}).Error)
	require.NoError(t, database.Create(&model.DeployToken{Token: token, UserID: user.ID, Name: nodeName, Status: model.DeployTokenStatusBound, CreatedBy: user.ID}).Error)
	provider := model.ResourceProvider{ID: uuid.NewString(), Key: "inventory-provider", DisplayName: "Inventory Provider", DomainScope: model.ProviderDomainNamed, DomainLabel: "inventory-provider", Status: model.ProviderStatusActive, Revision: 1, RowVersion: 1}
	require.NoError(t, database.Create(&provider).Error)
	resource := model.TechnicalResource{
		ID: resourceID, ProviderID: provider.ID, Type: model.TechnicalResourceAgent, StableKey: "agent-stable", DomainLabel: "agent-stable",
		LifecycleState: model.TechnicalResourceRegistered, HealthState: model.ResourceHealthUnknown,
		CredentialRevision: 1, ConfigRevision: 1, RowVersion: 1,
	}
	require.NoError(t, database.Create(&resource).Error)
	require.NoError(t, database.Create(&model.TechnicalResourceBinding{
		ID: uuid.NewString(), TechnicalResourceID: resource.ID, SourceType: model.TechnicalResourceBindingLegacyNode,
		SourceID: "1001", CredentialRevision: 1, Enabled: true, BoundByUserID: user.ID, Reason: "fixture", RowVersion: 1,
	}).Error)
}

func startSupplyInventoryAgentServer(t *testing.T, server *AgentServiceServer) (pb.AgentServiceClient, func()) {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	grpcServer := grpcgo.NewServer()
	pb.RegisterAgentServiceServer(grpcServer, server)
	go func() { _ = grpcServer.Serve(listener) }()
	conn, err := grpcgo.NewClient("passthrough:///bufconn", grpcgo.WithTransportCredentials(insecure.NewCredentials()), grpcgo.WithContextDialer(func(context.Context, string) (net.Conn, error) {
		return listener.Dial()
	}))
	require.NoError(t, err)
	return pb.NewAgentServiceClient(conn), func() {
		_ = conn.Close()
		grpcServer.Stop()
		_ = listener.Close()
	}
}

func testSupplyInventoryEnvelope(t *testing.T, sequence uint64) *pb.SupplyInventoryEnvelope {
	t.Helper()
	clusters := []*pb.KubernetesClusterInventory{{
		ClusterUid: "cluster-uid-a", DisplayName: "cluster-a", KubernetesVersion: "v1.32.0",
		Namespaces: []*pb.KubernetesNamespaceInventory{{Uid: "namespace-uid-a", Name: "team-a", Labels: map[string]string{"environment": "test"}, Status: "active"}},
	}}
	payload, err := marshalSupplyInventoryPayload(clusters)
	require.NoError(t, err)
	sum := sha256.Sum256(payload)
	now := time.Now().UTC()
	return &pb.SupplyInventoryEnvelope{
		SchemaVersion: 1, SourceEpoch: "epoch-a", Sequence: sequence, SnapshotId: "snapshot-a",
		BatchIndex: 0, BatchCount: 1, ObservedAt: timestamppb.New(now.Add(-time.Second)), SentAt: timestamppb.New(now),
		PayloadHash: hex.EncodeToString(sum[:]), CredentialRevision: 1, KubernetesClusters: clusters,
	}
}
