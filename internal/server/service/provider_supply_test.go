package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

type providerSupplyFixture struct {
	database      *gorm.DB
	service       *ProviderSupplyService
	now           time.Time
	actor         model.User
	otherUser     model.User
	provider      model.ResourceProvider
	otherProvider model.ResourceProvider
	authorization *ManagementAuthorizationContext
}

func newProviderSupplyFixture(t *testing.T) providerSupplyFixture {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared&_pragma=foreign_keys(1)", uuid.NewString())
	database, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(
		&model.User{}, &model.UserIdentityProfile{}, &model.ResourceProvider{}, &model.AdminProviderMembership{},
		&model.Node{}, &model.Endpoint{}, &model.TechnicalResource{}, &model.TechnicalResourceBinding{},
		&model.TechnicalResourceDeployToken{},
		&model.SupplyInventoryReceipt{}, &model.SupplyCandidate{}, &model.PlatformResource{},
		&model.PlatformResourceSource{}, &model.NamespaceObservation{}, &model.ResourceScope{},
		&model.ResourceSession{},
		&model.OutboxEvent{}, &model.AuditLog{}, &model.DomainRegistry{}, &model.Release{}, &model.Artifact{}, &model.UpdateTask{}, &model.UpdateEvent{},
	))
	now := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	actor := model.User{Name: "provider-actor", Role: model.UserRoleClient, SecretHash: "fixture", Enabled: true}
	otherUser := model.User{Name: "other-user", Role: model.UserRoleClient, SecretHash: "fixture", Enabled: true}
	require.NoError(t, database.Create(&actor).Error)
	require.NoError(t, database.Create(&otherUser).Error)
	require.NoError(t, database.Create(&model.UserIdentityProfile{UserID: actor.ID, Username: "provider-actor", DisplayName: "Provider Actor", Enabled: true, AuthRevision: 1, RowVersion: 1}).Error)
	provider := model.ResourceProvider{ID: uuid.NewString(), Key: "provider-a", DisplayName: "Provider A", DomainScope: model.ProviderDomainNamed, DomainLabel: "provider-a", Status: model.ProviderStatusActive, Revision: 1, RowVersion: 1}
	otherProvider := model.ResourceProvider{ID: uuid.NewString(), Key: "provider-b", DisplayName: "Provider B", DomainScope: model.ProviderDomainNamed, DomainLabel: "provider-b", Status: model.ProviderStatusActive, Revision: 1, RowVersion: 1}
	require.NoError(t, database.Create(&provider).Error)
	require.NoError(t, database.Create(&otherProvider).Error)
	require.NoError(t, database.Create(&model.AdminProviderMembership{
		ID: uuid.NewString(), UserID: actor.ID, ProviderID: provider.ID, Role: model.ProviderManagementRoleOperator,
		Enabled: true, ValidFrom: now.Add(-time.Hour), PermissionRevision: 1, CreatedByUserID: actor.ID,
		Reason: "anonymous fixture", RowVersion: 1,
	}).Error)
	for _, node := range []model.Node{
		{ID: 1001, UserID: actor.ID, Name: "agent-a", Type: model.NodeTypeAgent},
		{ID: 1002, UserID: actor.ID, Name: "agent-b", Type: model.NodeTypeAgent},
	} {
		require.NoError(t, database.Create(&node).Error)
	}
	require.NoError(t, database.Create(&model.Endpoint{ID: "legacy-endpoint-a", UserID: actor.ID, Name: "endpoint-a", SSHUsers: "[]"}).Error)
	require.NoError(t, database.Create(&model.Endpoint{ID: "legacy-endpoint-other", UserID: otherUser.ID, Name: "endpoint-other", SSHUsers: "[]"}).Error)
	authorization, err := ResolveManagementContext(database, actor.ID, model.ManagementScopeProvider, provider.ID, now, false)
	require.NoError(t, err)
	service := NewProviderSupplyService(database)
	service.now = func() time.Time { return now }
	return providerSupplyFixture{
		database: database, service: service, now: now, actor: actor, otherUser: otherUser,
		provider: provider, otherProvider: otherProvider, authorization: authorization,
	}
}

func TestProviderTechnicalResourceCapabilitiesAndUpdaterUseScopedBinding(t *testing.T) {
	fixture := newProviderSupplyFixture(t)
	ctx := context.Background()
	agent := fixture.createBoundAgent(t, "agent-operations", 1001)

	capabilities, err := fixture.service.GetTechnicalResourceCapabilities(ctx, fixture.authorization, agent.ID)
	require.NoError(t, err)
	require.False(t, capabilities.K8SEnabled)

	capabilities.SVCEnabled = true
	capabilities.SVCLabelSelector = "signal.beagle.io/expose=true"
	capabilities.SVCNamespaces = []string{"team-a"}
	updated, err := fixture.service.UpdateTechnicalResourceCapabilities(ctx, fixture.authorization, UpdateTechnicalResourceCapabilitiesInput{
		TechnicalResourceID: agent.ID, ExpectedRowVersion: agent.RowVersion, Capabilities: *capabilities,
	})
	require.NoError(t, err)
	require.Equal(t, agent.ConfigRevision+1, updated.ConfigRevision)
	require.Equal(t, agent.RowVersion+1, updated.RowVersion)
	var node model.Node
	require.NoError(t, fixture.database.First(&node, 1001).Error)
	require.NotNil(t, node.SVCEnabled)
	require.True(t, *node.SVCEnabled)
	require.Equal(t, `["team-a"]`, node.SVCNamespaces)

	foreign := model.TechnicalResource{
		ID: uuid.NewString(), ProviderID: fixture.otherProvider.ID, Type: model.TechnicalResourceAgent, StableKey: "foreign-operations", DomainLabel: "foreign-operations",
		LifecycleState: model.TechnicalResourceRegistered, HealthState: model.ResourceHealthUnknown, CredentialRevision: 1, ConfigRevision: 1, RowVersion: 1,
	}
	require.NoError(t, fixture.database.Create(&foreign).Error)
	_, err = fixture.service.GetTechnicalResourceCapabilities(ctx, fixture.authorization, foreign.ID)
	require.ErrorIs(t, err, ErrProviderSupplyObjectNotFound)

	require.NoError(t, fixture.database.Model(&node).Updates(map[string]any{
		"updater_protocol": "v2", "system_info": `{"os":"linux","arch":"amd64"}`,
	}).Error)
	now := fixture.now
	release := model.Release{ID: uuid.NewString(), Component: model.ComponentAgent, Version: "2.0.0", CommitID: strings.Repeat("1", 40), Channel: "stable", Status: model.ReleaseStatusPublished, PublishedAt: &now}
	require.NoError(t, fixture.database.Create(&release).Error)
	require.NoError(t, fixture.database.Create(&model.Artifact{
		ID: uuid.NewString(), ReleaseID: release.ID, OS: "linux", Arch: "amd64", Filename: "agent", DownloadURL: "https://example.invalid/agent",
		Size: 1, SHA256: strings.Repeat("a", 64), Status: model.ArtifactStatusAvailable,
	}).Error)
	task, err := fixture.service.CreateTechnicalResourceUpdateTask(ctx, fixture.authorization, agent.ID, release.ID, false)
	require.NoError(t, err)
	require.Equal(t, model.UpdateTargetNode, task.TargetType)
	require.Equal(t, "1001", task.TargetID)
	tasks, err := fixture.service.ListTechnicalResourceUpdateTasks(ctx, fixture.authorization, agent.ID)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
}

func TestProviderTechnicalResourceEndpointAccess(t *testing.T) {
	fixture := newProviderSupplyFixture(t)
	ctx := context.Background()
	agent := fixture.createBoundAgent(t, "agent-endpoint-access", 1001)

	capabilities, err := fixture.service.GetTechnicalResourceCapabilities(ctx, fixture.authorization, agent.ID)
	require.NoError(t, err)
	capabilities.EndpointAccessEnabled = true
	capabilities.EndpointAddress = "agent.internal"
	port := 51052
	capabilities.EndpointListenPort = &port

	updated, err := fixture.service.UpdateTechnicalResourceCapabilities(ctx, fixture.authorization, UpdateTechnicalResourceCapabilitiesInput{
		TechnicalResourceID: agent.ID, ExpectedRowVersion: agent.RowVersion, Capabilities: *capabilities,
	})
	require.NoError(t, err)

	access, err := fixture.service.GetTechnicalResourceEndpointAccess(ctx, fixture.authorization, agent.ID)
	require.NoError(t, err)
	require.True(t, access.Enabled)
	require.True(t, access.TokenExists)
	require.Equal(t, "agent.internal", access.Address)
	require.Equal(t, 51052, access.Port)
	require.NotEmpty(t, access.Token)

	rotated, resource, err := fixture.service.RotateTechnicalResourceEndpointToken(ctx, fixture.authorization, agent.ID, updated.RowVersion)
	require.NoError(t, err)
	require.NotEqual(t, access.Token, rotated.Token)
	require.Equal(t, updated.RowVersion+1, resource.RowVersion)
	require.Equal(t, updated.CredentialRevision+1, resource.CredentialRevision)

	_, _, err = fixture.service.RotateTechnicalResourceEndpointToken(ctx, fixture.authorization, agent.ID, updated.RowVersion)
	require.ErrorIs(t, err, ErrProviderSupplyVersionConflict)
}

func TestProviderChangesBoundAgentHostDomainLabel(t *testing.T) {
	fixture := newProviderSupplyFixture(t)
	ctx := context.Background()
	agent := fixture.createBoundAgent(t, "agent-hostname", 1001)
	require.NoError(t, fixture.database.Model(&model.Node{}).Where("id = ?", 1001).Updates(map[string]any{
		"hostname": "reported-hostname", "host_domain_label": "reported-hostname",
	}).Error)
	var node model.Node
	require.NoError(t, fixture.database.First(&node, 1001).Error)

	updated, err := fixture.service.ChangeAgentHostDomainLabel(ctx, fixture.authorization, agent.ID, "friendly-host", agent.RowVersion)
	require.NoError(t, err)
	require.Equal(t, agent.RowVersion+1, updated.RowVersion)
	require.Equal(t, agent.ConfigRevision+1, updated.ConfigRevision)
	require.NoError(t, fixture.database.First(&node, 1001).Error)
	require.Equal(t, "friendly-host", node.HostDomainLabel)

	detail, err := fixture.service.GetTechnicalResource(ctx, fixture.authorization, agent.ID)
	require.NoError(t, err)
	require.Equal(t, "friendly-host", detail.Resource.HostDomainLabel)
	require.Equal(t, "reported-hostname", detail.Resource.Hostname)

	_, err = fixture.service.ChangeAgentHostDomainLabel(ctx, fixture.authorization, agent.ID, "invalid.name", updated.RowVersion)
	require.ErrorIs(t, err, ErrHostDomainLabelInvalid)
	_, err = fixture.service.ChangeAgentHostDomainLabel(ctx, fixture.authorization, agent.ID, "another-host", agent.RowVersion)
	require.ErrorIs(t, err, ErrProviderSupplyVersionConflict)
}

func TestProviderChangesPlatformHostDomainLabel(t *testing.T) {
	fixture := newProviderSupplyFixture(t)
	ctx := context.Background()
	agent := fixture.createBoundAgent(t, "agent-host-resource", 1001)
	require.NoError(t, fixture.database.Model(&model.Node{}).Where("id = ?", 1001).Updates(map[string]any{
		"name": "cpu-119", "hostname": "172.24.69.119", "host_domain_label": "aliyun-119", "ip": "100.64.0.123",
	}).Error)
	var runtimeUser model.User
	require.NoError(t, fixture.database.First(&runtimeUser, agent.RuntimeUserID).Error)
	runtimeUser.SSHEnabled = true
	require.NoError(t, fixture.database.Save(&runtimeUser).Error)
	require.NoError(t, fixture.database.Model(&model.Node{}).Where("id = ?", 1001).Update("user_id", runtimeUser.ID).Error)
	var node model.Node
	require.NoError(t, fixture.database.First(&node, 1001).Error)
	require.NoError(t, fixture.database.Exec(
		`INSERT INTO domain_registry (domain, type, user_id, provider_id, agent_resource_id, resource_kind, resource_id, node_id, endpoint_id, target_ip, target_port, ssh_users, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULL, ?, ?, ?, ?, ?, ?)`,
		"aliyun-119."+agent.DomainLabel+"."+fixture.provider.DomainLabel+".beagle", model.DomainTypeSSH, runtimeUser.ID, agent.ProviderID, agent.ID,
		model.DomainResourceNode, fmt.Sprint(node.ID), node.ID, node.IP, 22, `["root","ubuntu"]`, model.DomainStatusOnline, fixture.now, fixture.now,
	).Error)
	require.NoError(t, EnsureLegacyHostPlatformResource(fixture.database, agent, &node, fixture.actor.ID, fixture.now))
	var hostResource model.PlatformResource
	require.NoError(t, fixture.database.First(&hostResource, "provider_id = ? AND type = ?", fixture.provider.ID, model.SupplyResourceHost).Error)

	updated, err := fixture.service.ChangePlatformHostDomainLabel(ctx, fixture.authorization, hostResource.ID, "xny-a100", hostResource.RowVersion)
	require.NoError(t, err)
	require.Equal(t, hostResource.RowVersion+1, updated.RowVersion)

	require.NoError(t, fixture.database.First(&node, 1001).Error)
	require.Equal(t, "xny-a100", node.HostDomainLabel)
	var domain model.DomainRegistry
	require.NoError(t, fixture.database.First(&domain, "node_id = ? AND type = ?", node.ID, model.DomainTypeSSH).Error)
	require.Equal(t, "xny-a100."+agent.DomainLabel+"."+fixture.provider.DomainLabel+".beagle", domain.Domain)
	require.JSONEq(t, `["root","ubuntu"]`, domain.SshUsers)

	resources, err := fixture.service.ListPlatformResources(ctx, fixture.authorization, ProviderSupplyListInput{Type: string(model.SupplyResourceHost), Page: 1, PageSize: 20})
	require.NoError(t, err)
	require.Len(t, resources.Items, 1)
	require.Equal(t, domain.Domain, resources.Items[0].AccessDomain)
	require.Equal(t, "xny-a100", resources.Items[0].HostDomainLabel)
	require.Equal(t, node.ID, resources.Items[0].SourceNodeID)
}

func TestProviderChangesPlatformHostDomainLabelKeepsCurrentSSHUsersFromDuplicateDomain(t *testing.T) {
	fixture := newProviderSupplyFixture(t)
	ctx := context.Background()
	agent := fixture.createBoundAgent(t, "agent-host-duplicate", 1001)
	require.NoError(t, fixture.database.Model(&model.Node{}).Where("id = ?", 1001).Updates(map[string]any{
		"name": "cpu-119", "hostname": "172.24.69.119", "host_domain_label": "aliyun-119", "ip": "100.64.0.123",
	}).Error)
	var runtimeUser model.User
	require.NoError(t, fixture.database.First(&runtimeUser, agent.RuntimeUserID).Error)
	runtimeUser.SSHEnabled = true
	require.NoError(t, fixture.database.Save(&runtimeUser).Error)
	var node model.Node
	require.NoError(t, fixture.database.First(&node, 1001).Error)
	require.NoError(t, fixture.database.Exec(
		`INSERT INTO domain_registry (domain, type, user_id, provider_id, agent_resource_id, resource_kind, resource_id, node_id, endpoint_id, target_ip, target_port, ssh_users, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULL, ?, ?, ?, ?, ?, ?)`,
		"stale."+agent.DomainLabel+"."+fixture.provider.DomainLabel+".beagle", model.DomainTypeSSH, runtimeUser.ID, agent.ProviderID, agent.ID,
		model.DomainResourceNode, fmt.Sprint(node.ID), node.ID, "100.64.0.1", 22, `[]`, model.DomainStatusOffline, fixture.now.Add(-time.Minute), fixture.now.Add(-time.Minute),
	).Error)
	require.NoError(t, fixture.database.Exec(
		`INSERT INTO domain_registry (domain, type, user_id, provider_id, agent_resource_id, resource_kind, resource_id, node_id, endpoint_id, target_ip, target_port, ssh_users, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULL, ?, ?, ?, ?, ?, ?)`,
		"aliyun-119."+agent.DomainLabel+"."+fixture.provider.DomainLabel+".beagle", model.DomainTypeSSH, runtimeUser.ID, agent.ProviderID, agent.ID,
		model.DomainResourceNode, fmt.Sprint(node.ID), node.ID, node.IP, 22, `["root","ubuntu"]`, model.DomainStatusOnline, fixture.now, fixture.now,
	).Error)
	require.NoError(t, EnsureLegacyHostPlatformResource(fixture.database, agent, &node, fixture.actor.ID, fixture.now))
	var hostResource model.PlatformResource
	require.NoError(t, fixture.database.First(&hostResource, "provider_id = ? AND type = ?", fixture.provider.ID, model.SupplyResourceHost).Error)

	_, err := fixture.service.ChangePlatformHostDomainLabel(ctx, fixture.authorization, hostResource.ID, "xny-a100", hostResource.RowVersion)
	require.NoError(t, err)
	var domains []model.DomainRegistry
	require.NoError(t, fixture.database.Where("node_id = ? AND type = ?", node.ID, model.DomainTypeSSH).Find(&domains).Error)
	require.Len(t, domains, 1)
	require.Equal(t, "xny-a100."+agent.DomainLabel+"."+fixture.provider.DomainLabel+".beagle", domains[0].Domain)
	require.JSONEq(t, `["root","ubuntu"]`, domains[0].SshUsers)
	require.Equal(t, model.DomainStatusOnline, domains[0].Status)
}

func TestProviderChangesAgentDisplayNameWithoutChangingDomains(t *testing.T) {
	fixture := newProviderSupplyFixture(t)
	ctx := context.Background()
	agent := fixture.createBoundAgent(t, "agent-display", 1001)
	require.NoError(t, fixture.database.Model(&model.Node{}).Where("id = ?", 1001).Updates(map[string]any{
		"hostname": "reported-hostname", "host_domain_label": "ssh-host",
	}).Error)
	require.NoError(t, fixture.database.Exec(
		`INSERT INTO domain_registry (domain, type, user_id, provider_id, agent_resource_id, resource_kind, resource_id, node_id, endpoint_id, target_ip, target_port, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULL, ?, ?, ?, ?, ?)`,
		agent.DomainLabel+"."+fixture.provider.DomainLabel+".beagle", model.DomainTypeK8SAPI, agent.RuntimeUserID, agent.ProviderID, agent.ID,
		model.DomainResourceKubernetes, "1001", 1001, "100.64.0.10", 6443, model.DomainStatusOnline, fixture.now, fixture.now,
	).Error)

	updated, err := fixture.service.UpdateAgentDisplayName(ctx, fixture.authorization, agent.ID, "A100 平台", agent.RowVersion)
	require.NoError(t, err)
	require.Equal(t, agent.RowVersion+1, updated.RowVersion)
	require.Equal(t, agent.DomainLabel, updated.DomainLabel)

	var runtimeUser model.User
	require.NoError(t, fixture.database.First(&runtimeUser, agent.RuntimeUserID).Error)
	require.Equal(t, "A100 平台", runtimeUser.Alias)
	var node model.Node
	require.NoError(t, fixture.database.First(&node, 1001).Error)
	require.Equal(t, "ssh-host", node.HostDomainLabel)
	var domain model.DomainRegistry
	require.NoError(t, fixture.database.First(&domain, "agent_resource_id = ?", agent.ID).Error)
	require.Equal(t, agent.DomainLabel+"."+fixture.provider.DomainLabel+".beagle", domain.Domain)

	detail, err := fixture.service.GetTechnicalResource(ctx, fixture.authorization, agent.ID)
	require.NoError(t, err)
	require.Equal(t, "A100 平台", detail.Resource.DisplayName)
}

func TestProviderCreatesResourceOwnedOneTimeDeploymentCredential(t *testing.T) {
	fixture := newProviderSupplyFixture(t)
	ctx := context.Background()
	resource, err := fixture.service.CreateTechnicalResource(ctx, fixture.authorization, CreateTechnicalResourceInput{
		Type: model.TechnicalResourceAgent, CredentialRevision: 1, RuntimeName: "new-agent", DomainLabel: "new-agent",
	})
	require.NoError(t, err)
	require.Equal(t, "resource:"+resource.ID, resource.StableKey)
	var runtimeUser model.User
	require.NoError(t, fixture.database.First(&runtimeUser, resource.RuntimeUserID).Error)
	require.Equal(t, "provider-a-new-agent", runtimeUser.Name)
	require.Equal(t, "new-agent", runtimeUser.Alias)
	require.Equal(t, model.UserRoleAgent, runtimeUser.Role)
	credential, err := fixture.service.CreateTechnicalResourceDeploymentCredential(ctx, fixture.authorization, resource.ID, "new-agent", 30*time.Minute)
	require.NoError(t, err)
	require.NotEmpty(t, credential.Token)
	var token model.TechnicalResourceDeployToken
	require.NoError(t, fixture.database.First(&token, "id = ?", credential.ID).Error)
	require.Equal(t, resource.ID, token.TechnicalResourceID)
	require.Equal(t, runtimeUser.ID, token.RuntimeUserID)
	require.Equal(t, model.TechnicalResourceDeployTokenPending, token.Status)
	var updatedResource model.TechnicalResource
	require.NoError(t, fixture.database.First(&updatedResource, "id = ?", resource.ID).Error)
	require.Equal(t, resource.RowVersion+1, updatedResource.RowVersion)

	require.NoError(t, fixture.database.Model(&updatedResource).Update("lifecycle_state", model.TechnicalResourceRegistered).Error)
	require.NoError(t, fixture.database.Model(&token).Update("status", model.TechnicalResourceDeployTokenConsumed).Error)
	rotated, err := fixture.service.CreateTechnicalResourceDeploymentCredential(ctx, fixture.authorization, resource.ID, "new-agent-reinstall", 30*time.Minute)
	require.NoError(t, err)
	require.NotEqual(t, credential.Token, rotated.Token)
	require.NoError(t, fixture.database.First(&token, "id = ?", token.ID).Error)
	require.Equal(t, model.TechnicalResourceDeployTokenConsumed, token.Status)
	var rotatedToken model.TechnicalResourceDeployToken
	require.NoError(t, fixture.database.First(&rotatedToken, "id = ?", rotated.ID).Error)
	require.Equal(t, model.TechnicalResourceDeployTokenPending, rotatedToken.Status)
	require.NoError(t, fixture.database.First(&updatedResource, "id = ?", resource.ID).Error)
	require.Equal(t, resource.RowVersion+2, updatedResource.RowVersion)
}

func TestProviderCreateAgentUsesUniqueRuntimeUserNameWhenLegacyUserExists(t *testing.T) {
	fixture := newProviderSupplyFixture(t)
	ctx := context.Background()
	require.NoError(t, fixture.database.Create(&model.User{
		Name: "provider-a-new-agent", Alias: "legacy agent", Role: model.UserRoleAgent, SecretHash: "fixture", Enabled: true,
	}).Error)

	resource, err := fixture.service.CreateTechnicalResource(ctx, fixture.authorization, CreateTechnicalResourceInput{
		Type: model.TechnicalResourceAgent, CredentialRevision: 1, RuntimeName: "new-agent", DomainLabel: "new-agent",
	})
	require.NoError(t, err)
	var runtimeUser model.User
	require.NoError(t, fixture.database.First(&runtimeUser, resource.RuntimeUserID).Error)
	require.NotEqual(t, "provider-a-new-agent", runtimeUser.Name)
	require.Contains(t, runtimeUser.Name, "provider-a-new-agent-")
	require.Equal(t, "new-agent", runtimeUser.Alias)
	require.Equal(t, model.UserRoleAgent, runtimeUser.Role)
}

func TestProviderDeletesRetiredTechnicalResourceAsTombstone(t *testing.T) {
	fixture := newProviderSupplyFixture(t)
	ctx := context.Background()
	agent := fixture.createBoundAgent(t, "agent-delete", 1001)
	token := model.TechnicalResourceDeployToken{
		ID: uuid.NewString(), TechnicalResourceID: agent.ID, Token: "delete-token", Name: "agent-delete",
		RuntimeUserID: fixture.actor.ID, Status: model.TechnicalResourceDeployTokenPending, CreatedByUserID: fixture.actor.ID,
	}
	require.NoError(t, fixture.database.Create(&token).Error)
	retired, err := fixture.service.SetTechnicalResourceLifecycle(ctx, fixture.authorization, SetTechnicalResourceLifecycleInput{
		TechnicalResourceID: agent.ID, TargetState: model.TechnicalResourceRetired,
		ExpectedRowVersion: agent.RowVersion, Reason: "retire before deletion",
	})
	require.NoError(t, err)
	require.Equal(t, model.ResourceHealthUnknown, retired.HealthState)

	check, err := fixture.service.CheckTechnicalResourceDelete(ctx, fixture.authorization, agent.ID)
	require.NoError(t, err)
	require.True(t, check.Allowed)
	_, err = fixture.service.DeleteTechnicalResource(ctx, fixture.authorization, agent.ID, retired.RowVersion-1, "stale deletion")
	require.ErrorIs(t, err, ErrProviderSupplyVersionConflict)

	deleted, err := fixture.service.DeleteTechnicalResource(ctx, fixture.authorization, agent.ID, retired.RowVersion, "dependency cleanup complete")
	require.NoError(t, err)
	require.Equal(t, model.TechnicalResourceDeleted, deleted.LifecycleState)
	require.NotNil(t, deleted.DeletedAt)
	require.NoError(t, fixture.database.First(&token, "id = ?", token.ID).Error)
	require.Equal(t, model.TechnicalResourceDeployTokenRevoked, token.Status)
	check, err = fixture.service.CheckTechnicalResourceDelete(ctx, fixture.authorization, agent.ID)
	require.NoError(t, err)
	require.False(t, check.Allowed)
}

func TestProviderDeletesOfflineTechnicalResourceWithoutRetiring(t *testing.T) {
	fixture := newProviderSupplyFixture(t)
	ctx := context.Background()
	agent := fixture.createBoundAgent(t, "agent-offline-delete", 1002)
	require.NoError(t, fixture.database.Exec(
		`INSERT INTO domain_registry (domain, type, user_id, provider_id, agent_resource_id, resource_kind, resource_id, node_id, endpoint_id, target_ip, target_port, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULL, ?, ?, ?, ?, ?)`,
		"agent-offline-delete.provider-a.beagle", model.DomainTypeSSH, agent.RuntimeUserID, agent.ProviderID, agent.ID,
		model.DomainResourceNode, "1002", 1002, "100.64.0.12", 22, model.DomainStatusOnline, fixture.now, fixture.now,
	).Error)
	require.NoError(t, fixture.database.Model(&model.TechnicalResource{}).Where("id = ?", agent.ID).
		Update("health_state", model.ResourceHealthOffline).Error)
	require.NoError(t, fixture.database.First(agent, "id = ?", agent.ID).Error)
	require.Equal(t, model.TechnicalResourceRegistered, agent.LifecycleState)

	check, err := fixture.service.CheckTechnicalResourceDelete(ctx, fixture.authorization, agent.ID)
	require.NoError(t, err)
	require.True(t, check.Allowed)

	deleted, err := fixture.service.DeleteTechnicalResource(ctx, fixture.authorization, agent.ID, agent.RowVersion, "remove stale offline resource")
	require.NoError(t, err)
	require.Equal(t, model.TechnicalResourceDeleted, deleted.LifecycleState)
	require.NotNil(t, deleted.DeletedAt)
	var domainCount int64
	require.NoError(t, fixture.database.Model(&model.DomainRegistry{}).Where("agent_resource_id = ?", agent.ID).Count(&domainCount).Error)
	require.Zero(t, domainCount)
}

func TestProviderDeletesPendingUnboundTechnicalResource(t *testing.T) {
	fixture := newProviderSupplyFixture(t)
	ctx := context.Background()
	resource, err := fixture.service.CreateTechnicalResource(ctx, fixture.authorization, CreateTechnicalResourceInput{
		Type: model.TechnicalResourceAgent, CredentialRevision: 1, RuntimeName: "pending-agent", DomainLabel: "pending-agent",
	})
	require.NoError(t, err)
	require.Equal(t, model.TechnicalResourcePending, resource.LifecycleState)
	require.Equal(t, model.ResourceHealthUnknown, resource.HealthState)

	check, err := fixture.service.CheckTechnicalResourceDelete(ctx, fixture.authorization, resource.ID)
	require.NoError(t, err)
	require.True(t, check.Allowed)

	deleted, err := fixture.service.DeleteTechnicalResource(ctx, fixture.authorization, resource.ID, resource.RowVersion, "remove undeployed resource")
	require.NoError(t, err)
	require.Equal(t, model.TechnicalResourceDeleted, deleted.LifecycleState)
	require.NotNil(t, deleted.DeletedAt)
}

func TestProviderDeletesPendingEndpointWithoutDisablingParentAgent(t *testing.T) {
	fixture := newProviderSupplyFixture(t)
	ctx := context.Background()
	agent := fixture.createBoundAgent(t, "agent-with-pending-endpoint", 1001)
	endpoint, err := fixture.service.CreateTechnicalResource(ctx, fixture.authorization, CreateTechnicalResourceInput{
		Type: model.TechnicalResourceEndpoint, StableKey: "pending-endpoint", ParentID: agent.ID, CredentialRevision: 1,
	})
	require.NoError(t, err)
	require.Equal(t, agent.RuntimeUserID, endpoint.RuntimeUserID)

	deleted, err := fixture.service.DeleteTechnicalResource(ctx, fixture.authorization, endpoint.ID, endpoint.RowVersion, "remove undeployed endpoint")
	require.NoError(t, err)
	require.Equal(t, model.TechnicalResourceDeleted, deleted.LifecycleState)

	var runtimeUser model.User
	require.NoError(t, fixture.database.First(&runtimeUser, agent.RuntimeUserID).Error)
	require.True(t, runtimeUser.Enabled)
}

func TestProviderDeleteCheckIgnoresDeletedChildEndpoints(t *testing.T) {
	fixture := newProviderSupplyFixture(t)
	ctx := context.Background()
	agent := fixture.createBoundAgent(t, "agent-with-deleted-children", 1001)
	require.NoError(t, fixture.database.Model(&model.TechnicalResource{}).Where("id = ?", agent.ID).
		Update("health_state", model.ResourceHealthOffline).Error)

	for i := 0; i < 4; i++ {
		endpoint, err := fixture.service.CreateTechnicalResource(ctx, fixture.authorization, CreateTechnicalResourceInput{
			Type: model.TechnicalResourceEndpoint, StableKey: fmt.Sprintf("deleted-child-%d", i), ParentID: agent.ID, CredentialRevision: 1,
		})
		require.NoError(t, err)
		require.NoError(t, fixture.database.Model(&model.TechnicalResource{}).Where("id = ?", endpoint.ID).
			Update("deleted_at", fixture.now).Error)
	}

	check, err := fixture.service.CheckTechnicalResourceDelete(ctx, fixture.authorization, agent.ID)
	require.NoError(t, err)
	require.True(t, check.Allowed)
	require.NotContains(t, check.Blockers, TechnicalResourceDeleteBlocker{
		Code: "ACTIVE_CHILD_ENDPOINTS", Message: "仍有未退役的子 Endpoint", Count: 4,
	})

	_, err = fixture.service.CreateTechnicalResource(ctx, fixture.authorization, CreateTechnicalResourceInput{
		Type: model.TechnicalResourceEndpoint, StableKey: "active-child", ParentID: agent.ID, CredentialRevision: 1,
	})
	require.NoError(t, err)
	check, err = fixture.service.CheckTechnicalResourceDelete(ctx, fixture.authorization, agent.ID)
	require.NoError(t, err)
	require.False(t, check.Allowed)
	require.Contains(t, check.Blockers, TechnicalResourceDeleteBlocker{
		Code: "ACTIVE_CHILD_ENDPOINTS", Message: "仍有未退役的子 Endpoint", Count: 1,
	})
}

func (f providerSupplyFixture) createBoundAgent(t *testing.T, stableKey string, nodeID uint64) *model.TechnicalResource {
	t.Helper()
	resource, err := f.service.CreateTechnicalResource(context.Background(), f.authorization, CreateTechnicalResourceInput{
		Type: model.TechnicalResourceAgent, StableKey: stableKey, CredentialRevision: 1,
		RuntimeName: strings.ReplaceAll(stableKey, ":", "-"), DomainLabel: strings.ReplaceAll(stableKey, ":", "-"),
	})
	require.NoError(t, err)
	require.NoError(t, f.database.Model(&model.Node{}).Where("id = ?", nodeID).Update("user_id", resource.RuntimeUserID).Error)
	if nodeID == 1001 {
		require.NoError(t, f.database.Model(&model.Endpoint{}).Where("id = ?", "legacy-endpoint-a").Update("user_id", resource.RuntimeUserID).Error)
	}
	bound, err := f.service.BindTechnicalResource(context.Background(), f.authorization, BindTechnicalResourceInput{
		TechnicalResourceID: resource.ID, SourceType: model.TechnicalResourceBindingLegacyNode,
		SourceID: strconv.FormatUint(nodeID, 10), ExpectedResourceVersion: resource.RowVersion, Reason: "anonymous fixture binding",
	})
	require.NoError(t, err)
	return bound.TechnicalResource
}

func TestProviderSupplyTechnicalResourceBindingAndScopeIsolation(t *testing.T) {
	fixture := newProviderSupplyFixture(t)
	ctx := context.Background()

	agent := fixture.createBoundAgent(t, "agent-stable-a", 1001)
	require.Equal(t, fixture.provider.ID, agent.ProviderID)
	require.Equal(t, model.TechnicalResourceRegistered, agent.LifecycleState)
	require.Equal(t, int64(2), agent.RowVersion)

	_, err := fixture.service.CreateTechnicalResource(ctx, fixture.authorization, CreateTechnicalResourceInput{
		Type: model.TechnicalResourceAgent, StableKey: "agent-stable-a", CredentialRevision: 1, RuntimeName: "agent-stable-duplicate", DomainLabel: "agent-stable-duplicate",
	})
	require.ErrorIs(t, err, ErrProviderSupplyConflict)

	forgedAuthorization := *fixture.authorization
	forgedAuthorization.ScopeID = fixture.otherProvider.ID
	_, err = fixture.service.CreateTechnicalResource(ctx, &forgedAuthorization, CreateTechnicalResourceInput{
		Type: model.TechnicalResourceAgent, StableKey: "forged-provider", CredentialRevision: 1, RuntimeName: "forged-provider", DomainLabel: "forged-provider",
	})
	require.ErrorIs(t, err, ErrManagementPermissionDenied)

	otherProviderResource := model.TechnicalResource{
		ID: uuid.NewString(), ProviderID: fixture.otherProvider.ID, Type: model.TechnicalResourceAgent, StableKey: "other-provider-agent", DomainLabel: "other-provider-agent",
		LifecycleState: model.TechnicalResourceRegistered, HealthState: model.ResourceHealthUnknown,
		CredentialRevision: 1, ConfigRevision: 1, RowVersion: 1,
	}
	require.NoError(t, fixture.database.Create(&otherProviderResource).Error)
	_, err = fixture.service.SetTechnicalResourceLifecycle(ctx, fixture.authorization, SetTechnicalResourceLifecycleInput{
		TechnicalResourceID: otherProviderResource.ID, TargetState: model.TechnicalResourceDisabled,
		ExpectedRowVersion: 1, Reason: "cross Provider attempt",
	})
	require.ErrorIs(t, err, ErrProviderSupplyObjectNotFound)

	endpoint, err := fixture.service.CreateTechnicalResource(ctx, fixture.authorization, CreateTechnicalResourceInput{
		Type: model.TechnicalResourceEndpoint, StableKey: "endpoint-stable-a", ParentID: agent.ID, CredentialRevision: 1,
	})
	require.NoError(t, err)
	boundEndpoint, err := fixture.service.BindTechnicalResource(ctx, fixture.authorization, BindTechnicalResourceInput{
		TechnicalResourceID: endpoint.ID, SourceType: model.TechnicalResourceBindingLegacyEndpoint,
		SourceID: "legacy-endpoint-a", ExpectedResourceVersion: endpoint.RowVersion, Reason: "explicit Endpoint binding",
	})
	require.NoError(t, err)
	require.Equal(t, model.TechnicalResourceRegistered, boundEndpoint.TechnicalResource.LifecycleState)

	foreignEndpoint, err := fixture.service.CreateTechnicalResource(ctx, fixture.authorization, CreateTechnicalResourceInput{
		Type: model.TechnicalResourceEndpoint, StableKey: "endpoint-stable-other", ParentID: agent.ID, CredentialRevision: 1,
	})
	require.NoError(t, err)
	_, err = fixture.service.BindTechnicalResource(ctx, fixture.authorization, BindTechnicalResourceInput{
		TechnicalResourceID: foreignEndpoint.ID, SourceType: model.TechnicalResourceBindingLegacyEndpoint,
		SourceID: "legacy-endpoint-other", ExpectedResourceVersion: foreignEndpoint.RowVersion, Reason: "invalid ownership",
	})
	require.ErrorIs(t, err, ErrProviderSupplyConflict)
	var foreignBindingCount int64
	require.NoError(t, fixture.database.Model(&model.TechnicalResourceBinding{}).Where("technical_resource_id = ?", foreignEndpoint.ID).Count(&foreignBindingCount).Error)
	require.Zero(t, foreignBindingCount)
}

func TestProviderSupplyConfigConfirmationAndLease(t *testing.T) {
	fixture := newProviderSupplyFixture(t)
	ctx := context.Background()
	agent := fixture.createBoundAgent(t, "agent-heartbeat", 1001)

	confirmed, err := fixture.service.ConfirmTechnicalResourceConfig(ctx, model.TechnicalResourceBindingLegacyNode, "1001", agent.ConfigRevision)
	require.NoError(t, err)
	require.Equal(t, agent.ConfigRevision, confirmed.ObservedRevision)
	require.Equal(t, agent.RowVersion, confirmed.RowVersion)

	confirmed, err = fixture.service.ConfirmTechnicalResourceConfig(ctx, model.TechnicalResourceBindingLegacyNode, "1001", agent.ConfigRevision)
	require.NoError(t, err)
	require.Equal(t, agent.ConfigRevision, confirmed.ObservedRevision)
	_, err = fixture.service.ConfirmTechnicalResourceConfig(ctx, model.TechnicalResourceBindingLegacyNode, "1001", agent.ConfigRevision+1)
	require.ErrorIs(t, err, ErrTechnicalResourceConfigAhead)

	require.NoError(t, fixture.database.Model(&model.TechnicalResource{}).Where("id = ?", agent.ID).Update("observed_revision", int64(17360)).Error)
	confirmed, err = fixture.service.ConfirmTechnicalResourceConfig(ctx, model.TechnicalResourceBindingLegacyNode, "1001", agent.ConfigRevision)
	require.NoError(t, err)
	require.Equal(t, agent.ConfigRevision, confirmed.ObservedRevision)

	leaseExpiresAt := fixture.now.Add(2 * time.Minute)
	require.NoError(t, fixture.database.Model(&model.TechnicalResource{}).Where("id = ?", agent.ID).Updates(map[string]any{
		"health_state": model.ResourceHealthOnline, "lease_expires_at": leaseExpiresAt,
	}).Error)

	count, err := fixture.service.ExpireTechnicalResourceLeases(ctx, fixture.now.Add(time.Minute))
	require.NoError(t, err)
	require.Zero(t, count)
	count, err = fixture.service.ExpireTechnicalResourceLeases(ctx, fixture.now.Add(2*time.Minute))
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
	var expired model.TechnicalResource
	require.NoError(t, fixture.database.First(&expired, "id = ?", agent.ID).Error)
	require.Equal(t, agent.ConfigRevision, expired.ObservedRevision)

	disabled, err := fixture.service.SetTechnicalResourceLifecycle(ctx, fixture.authorization, SetTechnicalResourceLifecycleInput{
		TechnicalResourceID: agent.ID, TargetState: model.TechnicalResourceDisabled,
		ExpectedRowVersion: agent.RowVersion, Reason: "maintenance",
	})
	require.NoError(t, err)
	_, err = fixture.service.ConfirmTechnicalResourceConfig(ctx, model.TechnicalResourceBindingLegacyNode, "1001", agent.ConfigRevision)
	require.ErrorIs(t, err, ErrTechnicalResourceDisabled)

	_, err = fixture.service.SetTechnicalResourceLifecycle(ctx, fixture.authorization, SetTechnicalResourceLifecycleInput{
		TechnicalResourceID: agent.ID, TargetState: model.TechnicalResourceRegistered,
		ExpectedRowVersion: agent.RowVersion, Reason: "stale version",
	})
	require.ErrorIs(t, err, ErrProviderSupplyVersionConflict)
	resumed, err := fixture.service.SetTechnicalResourceLifecycle(ctx, fixture.authorization, SetTechnicalResourceLifecycleInput{
		TechnicalResourceID: agent.ID, TargetState: model.TechnicalResourceRegistered,
		ExpectedRowVersion: disabled.RowVersion, Reason: "maintenance complete",
	})
	require.NoError(t, err)
	retired, err := fixture.service.SetTechnicalResourceLifecycle(ctx, fixture.authorization, SetTechnicalResourceLifecycleInput{
		TechnicalResourceID: agent.ID, TargetState: model.TechnicalResourceRetired,
		ExpectedRowVersion: resumed.RowVersion, Reason: "decommissioned",
	})
	require.NoError(t, err)
	require.Equal(t, model.TechnicalResourceRetired, retired.LifecycleState)
	_, err = fixture.service.ConfirmTechnicalResourceConfig(ctx, model.TechnicalResourceBindingLegacyNode, "1001", agent.ConfigRevision)
	require.ErrorIs(t, err, ErrTechnicalResourceUnbound)
	_, err = fixture.service.SetTechnicalResourceLifecycle(ctx, fixture.authorization, SetTechnicalResourceLifecycleInput{
		TechnicalResourceID: agent.ID, TargetState: model.TechnicalResourceRegistered,
		ExpectedRowVersion: retired.RowVersion, Reason: "must not restore terminal state",
	})
	require.ErrorIs(t, err, ErrTechnicalResourceStateTransition)
}

func TestProviderSupplyRejectsStaleAuthorizationAndCredential(t *testing.T) {
	fixture := newProviderSupplyFixture(t)
	agent := fixture.createBoundAgent(t, "agent-stale-auth", 1001)
	ctx := context.Background()

	staleAuthorization := *fixture.authorization
	staleAuthorization.PermissionRevision++
	_, err := fixture.service.SetTechnicalResourceLifecycle(ctx, &staleAuthorization, SetTechnicalResourceLifecycleInput{
		TechnicalResourceID: agent.ID, TargetState: model.TechnicalResourceDisabled,
		ExpectedRowVersion: agent.RowVersion, Reason: "stale permission revision",
	})
	require.ErrorIs(t, err, ErrManagementPermissionDenied)

	require.NoError(t, fixture.database.Model(&model.ResourceProvider{}).Where("id = ?", fixture.provider.ID).Update("status", model.ProviderStatusSuspended).Error)
	_, err = fixture.service.SetTechnicalResourceLifecycle(ctx, fixture.authorization, SetTechnicalResourceLifecycleInput{
		TechnicalResourceID: agent.ID, TargetState: model.TechnicalResourceDisabled,
		ExpectedRowVersion: agent.RowVersion, Reason: "suspended Provider cannot write",
	})
	require.ErrorIs(t, err, ErrManagementPermissionDenied)
}
