package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/service"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

func TestHandleContainerCandidatesUsesAuthenticatedNodeAndRefreshesLease(t *testing.T) {
	oldDB := db.DB
	t.Cleanup(func() { db.DB = oldDB })
	testDB, err := gorm.Open(sqlite.Open("file:agent_candidate_heartbeat_test?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	db.DB = testDB
	require.NoError(t, testDB.AutoMigrate(&model.DiscoveryCandidate{}))
	server := &AgentServiceServer{}

	server.handleContainerCandidates(context.Background(), 42, []*pb.ContainerDiscoveryCandidate{{
		ProviderHint: "beagle-ide", WorkspaceHint: "ws-a", GenerationHint: 7,
		ClusterId: "cluster-a", Namespace: "tenant-a", PodName: "ide-a", PodUid: "pod-a",
		ContainerName: "workspace", Ready: true, LeaseSeconds: 999999,
	}})

	var candidate model.DiscoveryCandidate
	require.NoError(t, testDB.First(&candidate).Error)
	require.Equal(t, uint64(42), candidate.AgentNodeID)
	require.Equal(t, "beagle-ide", candidate.ProviderHint)
	require.Equal(t, int64(7), candidate.GenerationHint)
	require.Equal(t, model.DiscoveryCandidateObserved, candidate.Status)
	require.NotNil(t, candidate.LeaseExpiresAt)
	require.True(t, candidate.LeaseExpiresAt.After(time.Now().Add(90*time.Second)), "expires_at=%s", candidate.LeaseExpiresAt.Format(time.RFC3339))
	require.True(t, candidate.LeaseExpiresAt.Before(time.Now().Add(3*time.Minute)), "expires_at=%s", candidate.LeaseExpiresAt.Format(time.RFC3339))

	candidate.Status = model.DiscoveryCandidateStale
	require.NoError(t, testDB.Save(&candidate).Error)
	server.handleContainerCandidates(context.Background(), 42, []*pb.ContainerDiscoveryCandidate{{
		ProviderHint: "beagle-ide", WorkspaceHint: "ws-a", GenerationHint: 7,
		ClusterId: "cluster-a", Namespace: "tenant-a", PodName: "ide-a", PodUid: "pod-a",
		ContainerName: "workspace", Ready: false, LeaseSeconds: 60,
	}})
	require.NoError(t, testDB.First(&candidate, "id = ?", candidate.ID).Error)
	require.Equal(t, model.DiscoveryCandidateObserved, candidate.Status)
	require.False(t, candidate.Ready)
}

func TestLegacyAgentHeartbeatWithoutContainerCandidatesRemainsCompatible(t *testing.T) {
	oldDB := db.DB
	t.Cleanup(func() { db.DB = oldDB })
	testDB, err := gorm.Open(sqlite.Open("file:legacy_agent_heartbeat_test?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	db.DB = testDB
	require.NoError(t, testDB.AutoMigrate(&model.User{}, &model.Node{}, &model.DiscoveryCandidate{}))

	agent := model.User{Name: "legacy-agent", Role: model.UserRoleAgent, SecretHash: "test", Enabled: true}
	require.NoError(t, testDB.Create(&agent).Error)
	server := &AgentServiceServer{}
	nodeID := server.handleHeartbeat(context.Background(), agent.ID, &pb.AgentHeartbeatRequest{
		TunnelIp: "100.64.0.20", Hostname: "legacy-host", Version: "old-agent",
		// No DeviceName, updater_protocol or container_candidates: this is the
		// shape sent by an older Agent.
	})
	require.NotZero(t, nodeID)
	var node model.Node
	require.NoError(t, testDB.First(&node, nodeID).Error)
	require.Equal(t, "legacy-host", node.Name)
	require.Equal(t, "old-agent", node.Version)
	require.Equal(t, "", node.UpdaterProtocol)
	require.Equal(t, "", node.ContainerSSHProtocol)
	var candidates int64
	require.NoError(t, testDB.Model(&model.DiscoveryCandidate{}).Count(&candidates).Error)
	require.Zero(t, candidates)

	node.UpdaterProtocol = "v1"
	node.ContainerSSHProtocol = "v1"
	require.NoError(t, testDB.Save(&node).Error)
	server.handleHeartbeat(context.Background(), agent.ID, &pb.AgentHeartbeatRequest{
		TunnelIp: "100.64.0.20", Hostname: "legacy-host", Version: "old-agent",
	})
	require.NoError(t, testDB.First(&node, nodeID).Error)
	require.Equal(t, "v1", node.UpdaterProtocol)
	require.Equal(t, "v1", node.ContainerSSHProtocol)
}

func TestHandleContainerCandidatesAutomaticallyPublishesAndIsIdempotent(t *testing.T) {
	oldDB := db.DB
	t.Cleanup(func() { db.DB = oldDB })
	testDB, err := gorm.Open(sqlite.Open("file:agent_candidate_reconcile_test?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	db.DB = testDB
	require.NoError(t, testDB.AutoMigrate(
		&model.User{}, &model.Node{}, &model.Tenant{}, &model.ProviderTenantBinding{},
		&model.WorkspaceBinding{}, &model.Resource{}, &model.ResourceTarget{}, &model.DiscoveryCandidate{},
	))

	tenant := model.Tenant{ID: uuid.NewString(), Key: "acme", Name: "Acme", Status: model.TenantStatusActive}
	require.NoError(t, testDB.Create(&tenant).Error)
	agentUser := model.User{Name: "agent-reconcile", Alias: "Agent", Role: model.UserRoleAgent, SecretHash: "test", Enabled: true}
	require.NoError(t, testDB.Create(&agentUser).Error)
	agentNode := model.Node{UserID: agentUser.ID, Name: "agent-reconcile", Type: model.NodeTypeAgent}
	require.NoError(t, testDB.Create(&agentNode).Error)
	require.NoError(t, testDB.Create(&model.ProviderTenantBinding{
		ID: uuid.NewString(), ProviderID: "beagle-ide", ExternalTenantID: "customer-acme",
		TenantID: tenant.ID, Status: model.ProviderBindingActive,
	}).Error)
	resource := model.Resource{
		ID: uuid.NewString(), TenantID: tenant.ID, Type: model.ResourceTypeContainerSSH,
		DisplayName: "IDE / workspace-a", ProviderID: "beagle-ide", ExternalWorkspaceID: "workspace-a",
		State: model.ResourceStatePending,
	}
	require.NoError(t, testDB.Create(&resource).Error)
	require.NoError(t, testDB.Create(&model.WorkspaceBinding{
		ID: uuid.NewString(), ProviderID: "beagle-ide", ExternalTenantID: "customer-acme",
		ExternalWorkspaceID: "workspace-a", TenantID: tenant.ID, ResourceID: resource.ID,
		Generation: 1, Status: model.WorkspaceBindingActive,
	}).Error)

	server := &AgentServiceServer{resourceReconciler: service.NewResourceReconciliationService(testDB)}
	report := &pb.ContainerDiscoveryCandidate{
		ProviderHint: "beagle-ide", WorkspaceHint: "workspace-a", GenerationHint: 1,
		ClusterId: "cluster-a", Namespace: "acme", PodName: "ide-a", PodUid: "pod-a",
		ContainerName: "workspace", Ready: true, LeaseSeconds: 60,
	}
	server.handleContainerCandidates(context.Background(), agentNode.ID, []*pb.ContainerDiscoveryCandidate{report})

	var candidate model.DiscoveryCandidate
	require.NoError(t, testDB.First(&candidate).Error)
	require.Equal(t, model.DiscoveryCandidatePublished, candidate.Status)
	require.Equal(t, resource.ID, candidate.ResourceID)
	var published model.Resource
	require.NoError(t, testDB.First(&published, "id = ?", resource.ID).Error)
	require.Equal(t, int64(1), published.TargetRevision)
	require.Equal(t, model.ResourceStateAvailable, published.State)

	server.handleContainerCandidates(context.Background(), agentNode.ID, []*pb.ContainerDiscoveryCandidate{report})
	var targetCount int64
	require.NoError(t, testDB.Model(&model.ResourceTarget{}).Where("resource_id = ?", resource.ID).Count(&targetCount).Error)
	require.Equal(t, int64(1), targetCount)
	require.NoError(t, testDB.First(&published, "id = ?", resource.ID).Error)
	require.Equal(t, int64(1), published.TargetRevision)

	require.NoError(t, testDB.First(&candidate, "id = ?", candidate.ID).Error)
	candidate.LeaseExpiresAt = func() *time.Time {
		expired := time.Now().Add(-time.Minute)
		return &expired
	}()
	require.NoError(t, testDB.Save(&candidate).Error)
	_, err = service.NewResourceReconciliationService(testDB).ExpireCandidates(context.Background(), time.Now())
	require.NoError(t, err)
	require.NoError(t, testDB.First(&published, "id = ?", resource.ID).Error)
	require.Equal(t, model.ResourceStatePending, published.State)

	server.handleContainerCandidates(context.Background(), agentNode.ID, []*pb.ContainerDiscoveryCandidate{report})
	require.NoError(t, testDB.First(&published, "id = ?", resource.ID).Error)
	require.Equal(t, model.ResourceStateAvailable, published.State)
	require.Equal(t, int64(1), published.TargetRevision)
}
