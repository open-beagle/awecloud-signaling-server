package grpc

import (
	"context"
	"fmt"
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
	require.Equal(t, "", node.ContainerSSHProtocol)
}

func TestAgentHeartbeatRefreshesBoundTechnicalResourceHealth(t *testing.T) {
	oldDB := db.DB
	t.Cleanup(func() { db.DB = oldDB })
	testDB, err := gorm.Open(sqlite.Open("file:agent_technical_resource_heartbeat_test?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	db.DB = testDB
	require.NoError(t, testDB.AutoMigrate(
		&model.User{}, &model.Node{}, &model.ResourceProvider{}, &model.TechnicalResource{}, &model.TechnicalResourceBinding{},
		&model.SupplyCandidate{}, &model.PlatformResource{}, &model.PlatformResourceSource{},
	))

	agent := model.User{Name: "managed-agent", Role: model.UserRoleAgent, SecretHash: "fixture", Enabled: true}
	require.NoError(t, testDB.Create(&agent).Error)
	node := model.Node{UserID: agent.ID, Name: "managed-agent", Type: model.NodeTypeAgent}
	require.NoError(t, testDB.Create(&node).Error)
	provider := model.ResourceProvider{ID: "provider-a", Key: "provider-a", DisplayName: "Provider A", DomainScope: model.ProviderDomainRoot, Status: model.ProviderStatusActive, Revision: 1, RowVersion: 1}
	require.NoError(t, testDB.Create(&provider).Error)
	resource := model.TechnicalResource{
		ID: "managed-agent-resource", ProviderID: provider.ID, Type: model.TechnicalResourceAgent, StableKey: "managed-agent",
		DomainLabel: "managed-agent", LifecycleState: model.TechnicalResourceRegistered, HealthState: model.ResourceHealthUnknown,
		CredentialRevision: 1, RuntimeUserID: agent.ID, ConfigRevision: 1, RowVersion: 1,
	}
	require.NoError(t, testDB.Create(&resource).Error)
	require.NoError(t, testDB.Create(&model.TechnicalResourceBinding{
		ID: "managed-agent-binding", TechnicalResourceID: resource.ID, SourceType: model.TechnicalResourceBindingLegacyNode,
		SourceID: fmt.Sprint(node.ID), CredentialRevision: 1, Enabled: true, BoundByUserID: agent.ID, Reason: "test", RowVersion: 1,
	}).Error)

	before := time.Now().UTC()
	server := &AgentServiceServer{providerSupply: service.NewProviderSupplyService(testDB)}
	nodeID := server.handleHeartbeat(context.Background(), agent.ID, &pb.AgentHeartbeatRequest{
		AgentId: agent.ID, DeviceName: node.Name, Hostname: "managed-host", Version: "v1.0.1",
	})
	require.Equal(t, node.ID, nodeID)
	require.NoError(t, testDB.First(&resource, "id = ?", resource.ID).Error)
	require.Equal(t, model.ResourceHealthOnline, resource.HealthState)
	require.NotNil(t, resource.LastReceivedAt)
	require.NotNil(t, resource.LeaseExpiresAt)
	require.False(t, resource.LastReceivedAt.Before(before))
	require.WithinDuration(t, resource.LastReceivedAt.Add(technicalResourceHeartbeatLeaseDuration), *resource.LeaseExpiresAt, time.Second)

	hostStableKey := "legacy-host-legacy_node:" + fmt.Sprint(node.ID)
	var candidate model.SupplyCandidate
	require.NoError(t, testDB.First(&candidate, "technical_resource_id = ? AND resource_type = ? AND stable_key = ?", resource.ID, model.SupplyResourceHost, hostStableKey).Error)
	require.Equal(t, model.SupplyCandidateLinked, candidate.ReviewState)
	var hostResource model.PlatformResource
	require.NoError(t, testDB.First(&hostResource, "provider_id = ? AND type = ? AND stable_key = ?", provider.ID, model.SupplyResourceHost, hostStableKey).Error)
	require.Equal(t, node.Name, hostResource.DisplayName)
	require.Equal(t, model.PlatformResourceActive, hostResource.LifecycleState)
	var source model.PlatformResourceSource
	require.NoError(t, testDB.First(&source, "platform_resource_id = ? AND supply_candidate_id = ? AND is_primary = ?", hostResource.ID, candidate.ID, true).Error)
}

func TestAgentHeartbeatRefreshesNodeSSHDomainTargetIP(t *testing.T) {
	oldDB := db.DB
	t.Cleanup(func() { db.DB = oldDB })
	testDB, err := gorm.Open(sqlite.Open("file:agent_ssh_domain_ip_refresh_test?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	db.DB = testDB
	require.NoError(t, testDB.AutoMigrate(&model.User{}, &model.Node{}, &model.DomainRegistry{}))

	client := model.User{Name: "duyingjun", Role: model.UserRoleClient, SecretHash: "test", Enabled: true}
	require.NoError(t, testDB.Create(&client).Error)
	node := model.Node{UserID: client.ID, Name: "ide-duyingjun", Type: model.NodeTypeDesktop, IP: "100.64.0.117"}
	require.NoError(t, testDB.Create(&node).Error)
	require.NoError(t, testDB.Create(&model.DomainRegistry{
		Domain: "duyingjun.oxaf1y22qr36.beagle", Type: model.DomainTypeSSH,
		UserID: client.ID, ResourceKind: model.DomainResourceNode, ResourceID: fmt.Sprint(node.ID),
		NodeID: node.ID, TargetIP: "100.64.0.4", TargetPort: 22,
	}).Error)

	server := &AgentServiceServer{}
	server.handleHeartbeat(context.Background(), client.ID, &pb.AgentHeartbeatRequest{
		DeviceName: "ide-duyingjun", TunnelIp: "100.64.0.119", TunnelConnected: true,
		SystemInfo: &pb.SystemInfo{Os: "linux", Arch: "amd64", Hostname: "ide-duyingjun"},
	})

	require.NoError(t, testDB.First(&node, node.ID).Error)
	require.Equal(t, "100.64.0.119", node.IP)
	var domain model.DomainRegistry
	require.NoError(t, testDB.First(&domain, "node_id = ? AND type = ?", node.ID, model.DomainTypeSSH).Error)
	require.Equal(t, "100.64.0.119", domain.TargetIP)
	require.Equal(t, 22, domain.TargetPort)
}

func TestAgentHeartbeatCreatesNodeSSHDomainFromRegistration(t *testing.T) {
	oldDB := db.DB
	t.Cleanup(func() { db.DB = oldDB })
	testDB, err := gorm.Open(sqlite.Open("file:agent_ssh_domain_registration_test?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	db.DB = testDB
	require.NoError(t, testDB.AutoMigrate(
		&model.User{}, &model.Node{}, &model.ResourceProvider{}, &model.TechnicalResource{},
		&model.TechnicalResourceBinding{}, &model.DomainRegistry{}, &model.SystemConfig{},
	))

	agent := model.User{Name: "provider-a-cpu", Role: model.UserRoleAgent, SecretHash: "test", Enabled: true, SSHEnabled: true}
	require.NoError(t, testDB.Create(&agent).Error)
	node := model.Node{UserID: agent.ID, Name: "cpu-119", Type: model.NodeTypeAgent, Hostname: "172.24.69.119", HostDomainLabel: "aliyun-119"}
	require.NoError(t, testDB.Create(&node).Error)
	provider := model.ResourceProvider{ID: "provider-a", Key: "provider-a", DisplayName: "Provider A", DomainScope: model.ProviderDomainNamed, DomainLabel: "xny", Status: model.ProviderStatusActive, Revision: 1, RowVersion: 1}
	require.NoError(t, testDB.Create(&provider).Error)
	resource := model.TechnicalResource{
		ID: "provider-a-agent", ProviderID: provider.ID, Type: model.TechnicalResourceAgent, StableKey: "cpu-agent",
		DomainLabel: "a100", LifecycleState: model.TechnicalResourceRegistered, RuntimeUserID: agent.ID,
		CredentialRevision: 1, ConfigRevision: 1, RowVersion: 1,
	}
	require.NoError(t, testDB.Create(&resource).Error)
	require.NoError(t, testDB.Create(&model.TechnicalResourceBinding{
		ID: "provider-a-agent-node", TechnicalResourceID: resource.ID, SourceType: model.TechnicalResourceBindingLegacyNode,
		SourceID: fmt.Sprint(node.ID), CredentialRevision: 1, Enabled: true, BoundByUserID: agent.ID, Reason: "test", RowVersion: 1,
	}).Error)
	require.NoError(t, testDB.Create(&model.DomainRegistry{
		Domain: "cpu-119.provider-a-cpu.beagle", Type: model.DomainTypeSSH, UserID: agent.ID,
		ProviderID: provider.ID, AgentResourceID: resource.ID, ResourceKind: model.DomainResourceNode,
		ResourceID: fmt.Sprint(node.ID), NodeID: node.ID, TargetIP: "100.64.0.4", TargetPort: 22,
		Status: model.DomainStatusOffline, SshUsers: `["old"]`,
	}).Error)
	require.NoError(t, testDB.Create(&model.DomainRegistry{
		Domain: "duplicate.provider-a-cpu.beagle", Type: model.DomainTypeSSH, UserID: agent.ID,
		ProviderID: provider.ID, AgentResourceID: resource.ID, ResourceKind: model.DomainResourceNode,
		ResourceID: fmt.Sprint(node.ID), NodeID: node.ID, TargetIP: "100.64.0.5", TargetPort: 22,
		Status: model.DomainStatusOffline, SshUsers: `["duplicate"]`,
	}).Error)

	server := &AgentServiceServer{}
	nodeID := server.handleHeartbeat(context.Background(), agent.ID, &pb.AgentHeartbeatRequest{
		DeviceName: "cpu-119", Hostname: "172.24.69.119", TunnelIp: "100.64.0.123", TunnelConnected: true,
		DomainRegistrations: []*pb.DomainRegistration{{
			Domain: "cpu-119.provider-a-cpu.beagle", Type: "ssh", TargetIp: "100.64.0.123", TargetPort: 22,
			SshUsers: []string{"root", "ubuntu"},
		}},
	})
	require.Equal(t, node.ID, nodeID)

	var domain model.DomainRegistry
	require.NoError(t, testDB.First(&domain, "node_id = ? AND type = ?", node.ID, model.DomainTypeSSH).Error)
	require.Equal(t, "aliyun-119.a100.xny.beagle", domain.Domain)
	require.Equal(t, agent.ID, domain.UserID)
	require.Equal(t, provider.ID, domain.ProviderID)
	require.Equal(t, resource.ID, domain.AgentResourceID)
	require.Equal(t, model.DomainResourceNode, domain.ResourceKind)
	require.Equal(t, fmt.Sprint(node.ID), domain.ResourceID)
	require.Equal(t, "100.64.0.123", domain.TargetIP)
	require.Equal(t, 22, domain.TargetPort)
	require.Equal(t, model.DomainStatusOnline, domain.Status)
	require.JSONEq(t, `["root","ubuntu"]`, domain.SshUsers)
	var domainCount int64
	require.NoError(t, testDB.Model(&model.DomainRegistry{}).Where("node_id = ? AND type = ?", node.ID, model.DomainTypeSSH).Count(&domainCount).Error)
	require.EqualValues(t, 1, domainCount)

	server.handleHeartbeat(context.Background(), agent.ID, &pb.AgentHeartbeatRequest{
		DeviceName: "cpu-119", Hostname: "172.24.69.119", TunnelIp: "100.64.0.123", TunnelConnected: true,
		DomainRegistrations: []*pb.DomainRegistration{{
			Domain: "cpu-119.provider-a-cpu.beagle", Type: "ssh", TargetIp: "100.64.0.123", TargetPort: 22,
			SshUsers: []string{},
		}},
	})
	require.NoError(t, testDB.First(&domain, "node_id = ? AND type = ?", node.ID, model.DomainTypeSSH).Error)
	require.JSONEq(t, `[]`, domain.SshUsers)
}

func TestAgentHeartbeatSkipsNodeSSHDomainWhenSSHDisabled(t *testing.T) {
	oldDB := db.DB
	t.Cleanup(func() { db.DB = oldDB })
	testDB, err := gorm.Open(sqlite.Open("file:agent_ssh_disabled_domain_test?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	db.DB = testDB
	require.NoError(t, testDB.AutoMigrate(
		&model.User{}, &model.Node{}, &model.ResourceProvider{}, &model.TechnicalResource{}, &model.TechnicalResourceBinding{},
		&model.DomainRegistry{},
	))

	agent := model.User{Name: "provider-a-cpu-disabled", Role: model.UserRoleAgent, SecretHash: "test", Enabled: true, SSHEnabled: false}
	require.NoError(t, testDB.Create(&agent).Error)
	node := model.Node{UserID: agent.ID, Name: "cpu-119", Type: model.NodeTypeAgent, HostDomainLabel: "aliyun-119", IP: "100.64.0.122"}
	require.NoError(t, testDB.Create(&node).Error)
	provider := model.ResourceProvider{
		ID: uuid.NewString(), Key: "provider-a", DisplayName: "Provider A",
		DomainScope: model.ProviderDomainNamed, DomainLabel: "xny", Status: model.ProviderStatusActive, Revision: 1, RowVersion: 1,
	}
	require.NoError(t, testDB.Create(&provider).Error)
	resource := model.TechnicalResource{
		ID: uuid.NewString(), ProviderID: provider.ID, Type: model.TechnicalResourceAgent,
		StableKey: "agent-disabled", DomainLabel: "a100", RuntimeUserID: agent.ID,
		LifecycleState: model.TechnicalResourceRegistered, HealthState: model.ResourceHealthOnline,
		CredentialRevision: 1, ConfigRevision: 1, RowVersion: 1,
	}
	require.NoError(t, testDB.Create(&resource).Error)
	require.NoError(t, testDB.Create(&model.TechnicalResourceBinding{
		ID: "provider-a-disabled-node", TechnicalResourceID: resource.ID, SourceType: model.TechnicalResourceBindingLegacyNode,
		SourceID: fmt.Sprint(node.ID), CredentialRevision: 1, Enabled: true, BoundByUserID: agent.ID, Reason: "test", RowVersion: 1,
	}).Error)
	require.NoError(t, testDB.Create(&model.DomainRegistry{
		Domain: "aliyun-119.a100.xny.beagle", Type: model.DomainTypeSSH, UserID: agent.ID,
		ProviderID: provider.ID, AgentResourceID: resource.ID, ResourceKind: model.DomainResourceNode,
		ResourceID: fmt.Sprint(node.ID), NodeID: node.ID, TargetIP: "100.64.0.122", TargetPort: 22,
		Status: model.DomainStatusOnline, SshUsers: `["root"]`,
	}).Error)

	server := &AgentServiceServer{}
	server.handleHeartbeat(context.Background(), agent.ID, &pb.AgentHeartbeatRequest{
		DeviceName: "cpu-119", Hostname: "172.24.69.119", TunnelIp: "100.64.0.123", TunnelConnected: true,
		DomainRegistrations: []*pb.DomainRegistration{{
			Domain: "aliyun-119.a100.xny.beagle", Type: "ssh", TargetIp: "100.64.0.123", TargetPort: 22,
			SshUsers: []string{"root", "ubuntu"},
		}},
	})

	var domainCount int64
	require.NoError(t, testDB.Model(&model.DomainRegistry{}).Where("node_id = ? AND type = ?", node.ID, model.DomainTypeSSH).Count(&domainCount).Error)
	require.Zero(t, domainCount)
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
