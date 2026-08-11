package grpc

import (
	"context"
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

func TestDesktopListDomainsIncludesUnifiedHostSSHGrant(t *testing.T) {
	oldDB := db.DB
	t.Cleanup(func() { db.DB = oldDB })
	testDB, err := gorm.Open(sqlite.Open("file:desktop_host_domains_test?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	db.DB = testDB
	require.NoError(t, testDB.AutoMigrate(
		&model.User{}, &model.Node{}, &model.Tenant{}, &model.TenantMembership{}, &model.Group{}, &model.GroupMember{},
		&model.Resource{}, &model.AccessGrant{}, &model.DomainRegistry{},
		&model.AclSSHUserPermission{}, &model.AclSSHGroupPermission{},
		&model.AclK8SUserPermission{}, &model.AclK8SGroupPermission{},
		&model.AclK8SServiceUserPermission{}, &model.AclK8SServiceGroupPermission{},
	))

	now := time.Now().UTC()
	client := model.User{Name: "desktop-host-user", Role: model.UserRoleClient, SecretHash: "test", Enabled: true}
	agentUser := model.User{Name: "host-agent", Role: model.UserRoleAgent, SecretHash: "test", Enabled: true}
	require.NoError(t, testDB.Create(&client).Error)
	require.NoError(t, testDB.Create(&agentUser).Error)
	tenant := model.Tenant{ID: uuid.NewString(), Key: "host-tenant", Name: "Host Tenant", Status: model.TenantStatusActive}
	require.NoError(t, testDB.Create(&tenant).Error)
	require.NoError(t, testDB.Create(&model.TenantMembership{TenantID: tenant.ID, UserID: client.ID, Role: "member", Enabled: true}).Error)
	agentNode := model.Node{ID: 6201, UserID: agentUser.ID, Name: "host-a100", Type: model.NodeTypeAgent, IP: "100.64.0.117", LastHeartbeat: &now}
	require.NoError(t, testDB.Create(&agentNode).Error)
	resource := model.Resource{
		ID: uuid.NewString(), TenantID: tenant.ID, Type: model.ResourceTypeHostSSH, DisplayName: "host-a100",
		AgentNodeID: agentNode.ID, State: model.ResourceStateAvailable,
	}
	require.NoError(t, testDB.Create(&resource).Error)
	require.NoError(t, testDB.Create(&model.AccessGrant{
		ID: uuid.NewString(), TenantID: tenant.ID, ResourceID: resource.ID, SubjectType: "user", SubjectUserID: client.ID,
		Actions: `["shell"]`, ValidFrom: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), Status: "enabled",
	}).Error)
	require.NoError(t, testDB.Create(&model.DomainRegistry{
		Domain: "host-a100.a100.xny.beagle", Type: model.DomainTypeSSH, UserID: agentUser.ID,
		ResourceKind: model.DomainResourceNode, ResourceID: fmt.Sprint(agentNode.ID), NodeID: agentNode.ID,
		TargetIP: "100.64.0.117", TargetPort: 22, SshUsers: `["root"]`, Status: model.DomainStatusOnline,
	}).Error)

	resp, err := (&DesktopServiceServer{}).ListDomains(context.WithValue(context.Background(), "client_id", client.ID), &pb.ListDomainsRequest{})
	require.NoError(t, err)
	require.Len(t, resp.Domains, 1)
	require.Equal(t, "host-a100.a100.xny.beagle", resp.Domains[0].Domain)
	require.Equal(t, "ssh", resp.Domains[0].Type)
	require.Equal(t, []string{"root"}, resp.Domains[0].SshUsers)
}

func TestDesktopListDomainsIncludesUnifiedHostSSHGrantCreatedInLocalTimezone(t *testing.T) {
	oldDB := db.DB
	t.Cleanup(func() { db.DB = oldDB })
	testDB, err := gorm.Open(sqlite.Open("file:desktop_host_domains_localtime_test?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	db.DB = testDB
	require.NoError(t, testDB.AutoMigrate(
		&model.User{}, &model.Node{}, &model.Tenant{}, &model.TenantMembership{}, &model.Group{}, &model.GroupMember{},
		&model.Resource{}, &model.AccessGrant{}, &model.DomainRegistry{},
		&model.AclSSHUserPermission{}, &model.AclSSHGroupPermission{},
		&model.AclK8SUserPermission{}, &model.AclK8SGroupPermission{},
		&model.AclK8SServiceUserPermission{}, &model.AclK8SServiceGroupPermission{},
	))

	now := time.Now().UTC()
	localTime := now.In(time.FixedZone("CST", 8*60*60))
	client := model.User{Name: "desktop-host-local-user", Role: model.UserRoleClient, SecretHash: "test", Enabled: true}
	agentUser := model.User{Name: "host-agent-local", Role: model.UserRoleAgent, SecretHash: "test", Enabled: true}
	require.NoError(t, testDB.Create(&client).Error)
	require.NoError(t, testDB.Create(&agentUser).Error)
	tenant := model.Tenant{ID: uuid.NewString(), Key: "host-local-tenant", Name: "Host Local Tenant", Status: model.TenantStatusActive}
	require.NoError(t, testDB.Create(&tenant).Error)
	require.NoError(t, testDB.Create(&model.TenantMembership{TenantID: tenant.ID, UserID: client.ID, Role: "member", Enabled: true}).Error)
	group := model.Group{TenantID: tenant.ID, Name: "devops"}
	require.NoError(t, testDB.Create(&group).Error)
	require.NoError(t, testDB.Create(&model.GroupMember{GroupID: group.ID, UserID: client.ID}).Error)
	desktopNode := model.Node{ID: 6300, UserID: client.ID, Name: "desktop-local", Type: model.NodeTypeDesktop, IP: "100.64.0.31", LastHeartbeat: &now}
	require.NoError(t, testDB.Create(&desktopNode).Error)
	agentNode := model.Node{ID: 6301, UserID: agentUser.ID, Name: "host-local", Type: model.NodeTypeAgent, IP: "100.64.0.123", LastHeartbeat: &now}
	require.NoError(t, testDB.Create(&agentNode).Error)
	resource := model.Resource{
		ID: uuid.NewString(), TenantID: tenant.ID, Type: model.ResourceTypeHostSSH, DisplayName: "host-local",
		AgentNodeID: agentNode.ID, State: model.ResourceStateAvailable,
	}
	require.NoError(t, testDB.Create(&resource).Error)
	require.NoError(t, testDB.Create(&model.AccessGrant{
		ID: uuid.NewString(), TenantID: tenant.ID, ResourceID: resource.ID, SubjectType: "group", SubjectGroupID: &group.ID,
		Actions: `["shell"]`, ValidFrom: localTime.Add(-time.Minute), ExpiresAt: localTime.Add(time.Hour), Status: "enabled",
	}).Error)
	require.NoError(t, testDB.Create(&model.DomainRegistry{
		Domain: "host-local.ali.szzy.beagle", Type: model.DomainTypeSSH, UserID: agentUser.ID,
		ResourceKind: model.DomainResourceNode, ResourceID: fmt.Sprint(agentNode.ID), NodeID: agentNode.ID,
		TargetIP: "100.64.0.123", TargetPort: 22, SshUsers: `["root"]`, Status: model.DomainStatusOnline,
	}).Error)

	resp, err := (&DesktopServiceServer{}).ListDomains(context.WithValue(context.Background(), "client_id", client.ID), &pb.ListDomainsRequest{})
	require.NoError(t, err)
	require.Len(t, resp.Domains, 1)
	require.Equal(t, "host-local.ali.szzy.beagle", resp.Domains[0].Domain)

	domainResp, err := (&DesktopServiceServer{}).GetDomainList(context.Background(), &pb.GetDomainListRequest{DesktopId: desktopNode.ID})
	require.NoError(t, err)
	require.Len(t, domainResp.Domains, 1)
	require.Equal(t, "host-local.ali.szzy.beagle", domainResp.Domains[0].Domain)
	require.Equal(t, []string{"root"}, domainResp.Domains[0].SshUsers)

	resourceResp, err := (&DesktopServiceServer{}).GetResources(context.Background(), &pb.GetResourcesRequest{
		DesktopId: desktopNode.ID, ResourceProtocol: sessionAuthorizationProtocolV2, TenantId: tenant.ID,
	})
	require.NoError(t, err)
	require.Len(t, resourceResp.Ssh, 1)
	require.Equal(t, "host-local.ali.szzy.beagle", resourceResp.Ssh[0].Domain)
	require.Equal(t, []string{"root"}, resourceResp.Ssh[0].SshUsers)
}

func TestDesktopTenantContainerResourceProjectionUsesLiveSessionAuthorization(t *testing.T) {
	originalDB := db.DB
	t.Cleanup(func() { db.DB = originalDB })
	require.NoError(t, db.InitDB(config.DatabaseSection{Type: "sqlite", Path: filepath.Join(t.TempDir(), "signal.db")}))
	database := db.DB
	t.Cleanup(func() {
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	now := time.Now().UTC()
	owner := model.User{ID: 8101, Name: "projection-owner", Role: model.UserRoleClient, SecretHash: "fixture", Enabled: true}
	member := model.User{ID: 8102, Name: "projection-member", Role: model.UserRoleClient, SecretHash: "fixture", Enabled: true}
	require.NoError(t, database.Create(&[]model.User{owner, member}).Error)
	tenant := model.Tenant{ID: uuid.NewString(), Key: "projection", Name: "Projection Tenant", Status: model.TenantStatusActive}
	require.NoError(t, database.Create(&tenant).Error)
	membership := model.TenantMembership{ID: 8201, TenantID: tenant.ID, UserID: member.ID, Role: "member", Enabled: true}
	require.NoError(t, database.Create(&membership).Error)
	otherTenant := model.Tenant{ID: uuid.NewString(), Key: "projection-other", Name: "Projection Other Tenant", Status: model.TenantStatusActive}
	require.NoError(t, database.Create(&otherTenant).Error)
	require.NoError(t, database.Create(&model.TenantMembership{
		ID: 8202, TenantID: otherTenant.ID, UserID: member.ID, Role: "member", Enabled: true,
	}).Error)
	desktop := model.Node{
		ID: 8301, UserID: member.ID, Name: "projection-desktop", Type: model.NodeTypeDesktop,
		IP: "100.64.0.31", HeadscaleNodeID: 9301, LastHeartbeat: &now,
	}
	agentNode := model.Node{
		ID: 8302, UserID: owner.ID, Name: "projection-agent", Type: model.NodeTypeAgent,
		IP: "100.64.0.32", LastHeartbeat: &now,
	}
	require.NoError(t, database.Create(&[]model.Node{desktop, agentNode}).Error)

	provider := model.ResourceProvider{
		ID: uuid.NewString(), Key: "projection-provider", DisplayName: "Projection Provider",
		DomainScope: model.ProviderDomainNamed, DomainLabel: "projection-provider", Status: model.ProviderStatusActive, Revision: 1, RowVersion: 1,
	}
	require.NoError(t, database.Create(&provider).Error)
	technical := model.TechnicalResource{
		ID: uuid.NewString(), ProviderID: provider.ID, Type: model.TechnicalResourceAgent, StableKey: "projection-agent", DomainLabel: "projection-agent",
		LifecycleState: model.TechnicalResourceRegistered, HealthState: model.ResourceHealthOnline,
		CredentialRevision: 1, ConfigRevision: 1, RowVersion: 1,
	}
	require.NoError(t, database.Create(&technical).Error)
	require.NoError(t, database.Create(&model.TechnicalResourceBinding{
		ID: uuid.NewString(), TechnicalResourceID: technical.ID, SourceType: model.TechnicalResourceBindingLegacyNode,
		SourceID: fmt.Sprint(agentNode.ID), CredentialRevision: 1, Enabled: true, BoundByUserID: owner.ID,
		Reason: "projection binding", RowVersion: 1,
	}).Error)

	candidate := model.SupplyCandidate{
		ID: uuid.NewString(), ProviderID: provider.ID, TechnicalResourceID: technical.ID,
		ResourceType: model.SupplyResourceKubernetes, StableKey: strings.Repeat("3", 64), IdentityQuality: model.SupplyIdentityStrong,
		PayloadHash: strings.Repeat("2", 64), ObservationSnapshot: `{"capabilities":["workload_inventory_v1"]}`,
		FirstObservedAt: now.Add(-time.Minute), LastObservedAt: now, LeaseExpiresAt: now.Add(time.Hour),
		ReviewState: model.SupplyCandidateLinked, RowVersion: 1,
	}
	require.NoError(t, database.Create(&candidate).Error)
	platform := model.PlatformResource{
		ID: uuid.NewString(), ProviderID: provider.ID, Type: model.SupplyResourceKubernetes, StableKey: strings.Repeat("3", 64),
		DisplayName: "Projection Cluster", LifecycleState: model.PlatformResourceActive, HealthState: model.ResourceHealthOnline,
		CapabilityRevision: 1, AllocatableScopeCount: 1, RowVersion: 1,
	}
	require.NoError(t, database.Create(&platform).Error)
	require.NoError(t, database.Create(&model.PlatformResourceSource{
		ID: uuid.NewString(), ProviderID: provider.ID, PlatformResourceID: platform.ID,
		SupplyCandidateID: candidate.ID, IsPrimary: true, LinkedAt: now, LastConfirmedAt: now,
	}).Error)
	namespace := model.NamespaceObservation{
		ID: uuid.NewString(), ProviderID: provider.ID, ClusterResourceID: platform.ID,
		NamespaceUID: "projection-namespace-uid", Name: "projection-workloads", Revision: 1,
		ObservedAt: now, LeaseExpiresAt: now.Add(time.Hour), State: model.NamespaceObservationObserved,
	}
	require.NoError(t, database.Create(&namespace).Error)
	clusterScope := model.ResourceScope{
		ID: uuid.NewString(), ProviderID: provider.ID, PlatformResourceID: platform.ID,
		Type: model.ResourceScopeCluster, StableKey: platform.StableKey, LifecycleState: model.ResourceScopeActive,
		IsolationMode: model.ResourceScopeIsolationNone, ConfigRevision: 1, EvidenceRevision: 1, RowVersion: 1,
	}
	require.NoError(t, database.Create(&clusterScope).Error)
	namespaceScope := model.ResourceScope{
		ID: uuid.NewString(), ProviderID: provider.ID, PlatformResourceID: platform.ID,
		Type:                   model.ResourceScopeNamespace,
		StableKey:              fmt.Sprintf("%x", sha256.Sum256([]byte("kubernetes-namespace-v1\x00"+platform.ID+"\x00"+namespace.NamespaceUID))),
		ParentID:               &clusterScope.ID,
		NamespaceObservationID: &namespace.ID, LifecycleState: model.ResourceScopeAllocatable,
		IsolationMode: model.ResourceScopeIsolationNamespaceIsolated, ConfigRevision: 1, EvidenceRevision: 1, RowVersion: 1,
	}
	require.NoError(t, database.Create(&namespaceScope).Error)
	expiresAt := now.Add(time.Hour)
	allocation := model.ResourceAllocation{
		ID: uuid.NewString(), TenantID: tenant.ID, Mode: model.ResourceAllocationLeased,
		ValidFrom: now.Add(-time.Minute), ExpiresAt: &expiresAt, State: model.ResourceAllocationDraft,
		RowVersion: 1, CreatedByUserID: owner.ID,
	}
	require.NoError(t, database.Create(&allocation).Error)
	item := model.ResourceAllocationItem{
		ID: uuid.NewString(), AllocationID: allocation.ID, ScopeID: namespaceScope.ID,
		ScopeRowVersionSnapshot: namespaceScope.RowVersion,
	}
	require.NoError(t, database.Create(&item).Error)
	require.NoError(t, database.Model(&model.ResourceAllocation{}).Where("id = ?", allocation.ID).Updates(map[string]any{
		"state": model.ResourceAllocationActive, "row_version": int64(2),
	}).Error)

	targetSnapshot := `{"namespace_uid":"projection-namespace-uid","namespace_name":"projection-workloads","service_uid":"projection-service-uid","service_name":"projection-api","cluster_ip":"10.0.0.42","port_name":"https","port_number":443,"protocol":"TCP","labels_allowlist":{}}`
	observation := model.WorkloadObservation{
		ID: uuid.NewString(), NamespaceScopeID: namespaceScope.ID, Kind: model.WorkloadObservationServicePort,
		StableKey: strings.Repeat("6", 64), IdentityQuality: model.WorkloadIdentityStrong,
		State: model.WorkloadObservationEligible, Ready: true, ObservedRevision: 1, LabelSnapshot: `{}`,
		FirstObservedAt: now, LastObservedAt: now, LeaseExpiresAt: now.Add(time.Hour), RowVersion: 1,
	}
	require.NoError(t, database.Create(&observation).Error)
	require.NoError(t, database.Create(&model.WorkloadObservationSource{
		ID: uuid.NewString(), WorkloadObservationID: observation.ID, SourceTechnicalResourceID: technical.ID,
		SourceEpoch: uuid.NewString(), Sequence: 1, PayloadHash: strings.Repeat("7", 64),
		State: model.WorkloadObservationSourceObserved, Ready: true, TargetSnapshot: targetSnapshot,
		ObservedAt: now, ReceivedAt: now, LeaseExpiresAt: now.Add(time.Hour), SourceRevision: 1, RowVersion: 1,
	}).Error)
	resource := model.TenantResource{
		ID: uuid.NewString(), TenantID: tenant.ID, Type: model.TenantResourceContainerService,
		StableKey: strings.Repeat("8", 64), EntitlementLineageID: allocation.ID, DisplayName: "Projection API",
		VisibilityState: model.TenantResourceVisible, AvailabilityState: model.TenantResourceAvailable,
		Revision: 1, RowVersion: 1,
	}
	require.NoError(t, database.Create(&resource).Error)
	source := model.TenantResourceSource{
		ID: uuid.NewString(), TenantResourceID: resource.ID, AllocationItemID: item.ID,
		WorkloadObservationID: observation.ID, Enabled: true, EnabledAt: now, SourceRevision: 1, RowVersion: 1,
	}
	require.NoError(t, database.Create(&source).Error)
	target := model.TenantResourceTargetRevision{
		ID: uuid.NewString(), TenantResourceSourceID: source.ID, Revision: 1,
		TargetType: model.WorkloadObservationServicePort, TargetSnapshot: targetSnapshot,
		SourceTechnicalResourceID: technical.ID, AccessTechnicalResourceID: technical.ID,
		Ready: true, ObservedAt: now, ObservationRevision: 1, SourceRevision: 1,
	}
	require.NoError(t, database.Create(&target).Error)
	grant := model.TenantAccessGrant{
		ID: uuid.NewString(), TenantID: tenant.ID, TenantResourceID: resource.ID,
		SubjectType: model.TenantAccessGrantSubjectUser, SubjectKey: fmt.Sprint(member.ID), SubjectUserID: &member.ID,
		Actions: `["connect"]`, ValidFrom: now.Add(-time.Minute), MaxSessionSeconds: 3600,
		Status: model.TenantAccessGrantEnabled, Revision: 1, RowVersion: 1, CreatedByUserID: owner.ID,
	}
	require.NoError(t, database.Create(&grant).Error)

	sshTargetSnapshot := `{"namespace_uid":"projection-namespace-uid","namespace_name":"projection-workloads","workload_uid":"projection-shell-workload","workload_kind":"Deployment","workload_name":"projection-shell","pod_name":"projection-shell-0","pod_uid":"projection-shell-pod-uid","container_name":"shell","labels_allowlist":{},"ssh_users":["code"]}`
	sshObservation := model.WorkloadObservation{
		ID: uuid.NewString(), NamespaceScopeID: namespaceScope.ID, Kind: model.WorkloadObservationContainer,
		StableKey: strings.Repeat("9", 64), IdentityQuality: model.WorkloadIdentityStrong,
		State: model.WorkloadObservationEligible, Ready: true, ObservedRevision: 1, LabelSnapshot: `{}`,
		FirstObservedAt: now, LastObservedAt: now, LeaseExpiresAt: now.Add(time.Hour), RowVersion: 1,
	}
	require.NoError(t, database.Create(&sshObservation).Error)
	require.NoError(t, database.Create(&model.WorkloadObservationSource{
		ID: uuid.NewString(), WorkloadObservationID: sshObservation.ID, SourceTechnicalResourceID: technical.ID,
		SourceEpoch: uuid.NewString(), Sequence: 2, PayloadHash: strings.Repeat("a", 64),
		State: model.WorkloadObservationSourceObserved, Ready: true, TargetSnapshot: sshTargetSnapshot,
		ObservedAt: now, ReceivedAt: now, LeaseExpiresAt: now.Add(time.Hour), SourceRevision: 1, RowVersion: 1,
	}).Error)
	sshResource := model.TenantResource{
		ID: uuid.NewString(), TenantID: tenant.ID, Type: model.TenantResourceContainerSSH,
		StableKey: strings.Repeat("b", 64), EntitlementLineageID: allocation.ID, DisplayName: "Projection Shell",
		VisibilityState: model.TenantResourceVisible, AvailabilityState: model.TenantResourceAvailable,
		Revision: 1, RowVersion: 1,
	}
	require.NoError(t, database.Create(&sshResource).Error)
	sshSource := model.TenantResourceSource{
		ID: uuid.NewString(), TenantResourceID: sshResource.ID, AllocationItemID: item.ID,
		WorkloadObservationID: sshObservation.ID, Enabled: true, EnabledAt: now, SourceRevision: 1, RowVersion: 1,
	}
	require.NoError(t, database.Create(&sshSource).Error)
	sshTarget := model.TenantResourceTargetRevision{
		ID: uuid.NewString(), TenantResourceSourceID: sshSource.ID, Revision: 1,
		TargetType: model.WorkloadObservationContainer, TargetSnapshot: sshTargetSnapshot,
		SourceTechnicalResourceID: technical.ID, AccessTechnicalResourceID: technical.ID,
		Ready: true, ObservedAt: now, ObservationRevision: 1, SourceRevision: 1,
	}
	require.NoError(t, database.Create(&sshTarget).Error)
	sshGrant := model.TenantAccessGrant{
		ID: uuid.NewString(), TenantID: tenant.ID, TenantResourceID: sshResource.ID,
		SubjectType: model.TenantAccessGrantSubjectUser, SubjectKey: fmt.Sprint(member.ID), SubjectUserID: &member.ID,
		Actions: `["shell"]`, ValidFrom: now.Add(-time.Minute), MaxSessionSeconds: 3600,
		Status: model.TenantAccessGrantEnabled, Revision: 1, RowVersion: 1, CreatedByUserID: owner.ID,
	}
	require.NoError(t, database.Create(&sshGrant).Error)

	server := &DesktopServiceServer{config: &config.ServerConfig{FeatureFlags: config.FeatureFlagsSection{
		ResourceModelWrite: true,
	}}}
	legacyResponse, err := server.GetResources(context.Background(), &pb.GetResourcesRequest{DesktopId: desktop.ID})
	require.NoError(t, err)
	require.Empty(t, legacyResponse.ContainerService)
	var sessionCount int64
	require.NoError(t, database.Model(&model.ResourceSession{}).Where("user_id = ? AND device_id = ?", member.ID, desktop.ID).Count(&sessionCount).Error)
	require.Zero(t, sessionCount, "old Desktop request must not create resource_session_v2 state")

	negotiatedResponse, err := server.GetResources(context.Background(), &pb.GetResourcesRequest{
		DesktopId: desktop.ID, ResourceProtocol: sessionAuthorizationProtocolV2,
	})
	require.NoError(t, err)
	require.Len(t, negotiatedResponse.ContainerSsh, 1)
	require.Len(t, negotiatedResponse.ContainerService, 1)

	readOnlyServer := &DesktopServiceServer{config: &config.ServerConfig{FeatureFlags: config.FeatureFlagsSection{}}}
	readOnlyResponse, err := readOnlyServer.GetResources(context.Background(), &pb.GetResourcesRequest{
		DesktopId: desktop.ID, ResourceProtocol: sessionAuthorizationProtocolV2,
	})
	require.NoError(t, err)
	require.Len(t, readOnlyResponse.ContainerSsh, 1, "resource_model_write must not hide existing Pod resources")
	require.Len(t, readOnlyResponse.ContainerService, 1, "resource_model_write must not hide existing Service resources")

	scopedResponse, err := server.GetResources(context.Background(), &pb.GetResourcesRequest{
		DesktopId: desktop.ID, ResourceProtocol: sessionAuthorizationProtocolV2, TenantId: tenant.ID,
	})
	require.NoError(t, err)
	require.Empty(t, scopedResponse.Ssh)
	require.Empty(t, scopedResponse.K8SApi)
	require.Empty(t, scopedResponse.K8SService)
	require.Len(t, scopedResponse.ContainerSsh, 1)
	require.Len(t, scopedResponse.ContainerService, 1)
	otherScopedResponse, err := server.GetResources(context.Background(), &pb.GetResourcesRequest{
		DesktopId: desktop.ID, ResourceProtocol: sessionAuthorizationProtocolV2, TenantId: otherTenant.ID,
	})
	require.NoError(t, err)
	require.Empty(t, otherScopedResponse.ContainerSsh)
	require.Empty(t, otherScopedResponse.ContainerService)
	_, err = server.GetResources(context.Background(), &pb.GetResourcesRequest{
		DesktopId: desktop.ID, ResourceProtocol: sessionAuthorizationProtocolV2, TenantId: uuid.NewString(),
	})
	require.Equal(t, codes.PermissionDenied, status.Code(err))

	sshResources, services := server.queryTenantContainerResourcesGRPC(context.Background(), &desktop, nil, "")
	require.Len(t, sshResources, 1)
	projectedSSH := sshResources[0]
	require.Equal(t, sshResource.ID, projectedSSH.ResourceId)
	require.Equal(t, "projection-shell.projection-workloads.projection-agent.projection-provider.beagle", projectedSSH.Domain)
	require.Equal(t, agentNode.IP, projectedSSH.AgentIp)
	require.Equal(t, []string{"code"}, projectedSSH.SshUsers)
	require.Greater(t, projectedSSH.ListenPort, uint32(0))
	require.NotEmpty(t, projectedSSH.SessionId)
	require.Equal(t, sshSource.ID, projectedSSH.SourceId)
	require.Equal(t, sshTarget.ID, projectedSSH.TargetRevisionId)
	require.Greater(t, projectedSSH.AuthorizationRevision, int64(0))
	require.Len(t, services, 1)
	projected := services[0]
	require.Equal(t, resource.ID, projected.ResourceId)
	require.Equal(t, tenant.ID, projected.TenantId)
	require.Equal(t, "Projection API", projected.DisplayName)
	require.Equal(t, agentNode.IP, projected.AgentIp)
	require.Equal(t, uint32(50051), projected.SvcProxyPort)
	require.Equal(t, resource.ID+".service.beagle", projected.Domain)
	require.Equal(t, "projection-workloads", projected.Namespace)
	require.Equal(t, "projection-service-uid", projected.ServiceUid)
	require.Equal(t, "projection-api", projected.ServiceName)
	require.Equal(t, "https", projected.PortName)
	require.Equal(t, int32(443), projected.PortNumber)
	require.Equal(t, "TCP", projected.Protocol)
	require.NotEmpty(t, projected.SessionId)
	require.Equal(t, source.ID, projected.SourceId)
	require.Equal(t, target.ID, projected.TargetRevisionId)
	require.Greater(t, projected.AuthorizationRevision, int64(0))

	sshResources, services = server.queryTenantContainerResourcesGRPC(context.Background(), &desktop, nil, "")
	require.Len(t, sshResources, 1)
	require.Equal(t, projectedSSH.SessionId, sshResources[0].SessionId)
	require.Len(t, services, 1)
	require.Equal(t, projected.SessionId, services[0].SessionId)
	require.NoError(t, database.Model(&model.ResourceSession{}).Where("user_id = ? AND device_id = ?", member.ID, desktop.ID).Count(&sessionCount).Error)
	require.Equal(t, int64(2), sessionCount)

	require.NoError(t, database.Model(&model.TenantAccessGrant{}).Where("id = ?", grant.ID).Updates(map[string]any{
		"status": model.TenantAccessGrantSuspended, "revision": int64(2), "row_version": int64(2),
	}).Error)
	sshResources, services = server.queryTenantContainerResourcesGRPC(context.Background(), &desktop, nil, "")
	require.Len(t, sshResources, 1)
	require.Empty(t, services)
}
