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
		"updater_protocol": "v1", "system_info": `{"os":"linux","arch":"amd64"}`,
	}).Error)
	now := fixture.now
	release := model.Release{ID: uuid.NewString(), Component: model.ComponentAgent, Version: "2.0.0", Channel: "stable", Status: model.ReleaseStatusPublished, PublishedAt: &now}
	require.NoError(t, fixture.database.Create(&release).Error)
	require.NoError(t, fixture.database.Create(&model.Artifact{
		ID: uuid.NewString(), ReleaseID: release.ID, OS: "linux", Arch: "amd64", Filename: "agent", DownloadURL: "https://example.invalid/agent",
		SHA256: strings.Repeat("a", 64), Status: model.ArtifactStatusAvailable,
	}).Error)
	task, err := fixture.service.CreateTechnicalResourceUpdateTask(ctx, fixture.authorization, agent.ID, release.ID, false)
	require.NoError(t, err)
	require.Equal(t, model.UpdateTargetNode, task.TargetType)
	require.Equal(t, "1001", task.TargetID)
	tasks, err := fixture.service.ListTechnicalResourceUpdateTasks(ctx, fixture.authorization, agent.ID)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
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
}

func (f providerSupplyFixture) createBoundAgent(t *testing.T, stableKey string, nodeID uint64) *model.TechnicalResource {
	t.Helper()
	resource, err := f.service.CreateTechnicalResource(context.Background(), f.authorization, CreateTechnicalResourceInput{
		Type: model.TechnicalResourceAgent, StableKey: stableKey, CredentialRevision: 1,
		RuntimeName: strings.ReplaceAll(stableKey, ":", "-"), DomainLabel: strings.ReplaceAll(stableKey, ":", "-"),
	})
	require.NoError(t, err)
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

func TestProviderSupplyLifecycleHeartbeatAndLease(t *testing.T) {
	fixture := newProviderSupplyFixture(t)
	ctx := context.Background()
	agent := fixture.createBoundAgent(t, "agent-heartbeat", 1001)
	credential := TechnicalResourceCredential{SourceType: model.TechnicalResourceBindingLegacyNode, SourceID: "1001", CredentialRevision: 1}

	heartbeat, err := fixture.service.RecordTechnicalResourceHeartbeat(ctx, credential, 2*time.Minute)
	require.NoError(t, err)
	require.Equal(t, model.ResourceHealthOnline, heartbeat.HealthState)
	require.Equal(t, fixture.now.Add(2*time.Minute), *heartbeat.LeaseExpiresAt)
	require.Equal(t, agent.RowVersion, heartbeat.RowVersion)

	count, err := fixture.service.ExpireTechnicalResourceLeases(ctx, fixture.now.Add(time.Minute))
	require.NoError(t, err)
	require.Zero(t, count)
	count, err = fixture.service.ExpireTechnicalResourceLeases(ctx, fixture.now.Add(2*time.Minute))
	require.NoError(t, err)
	require.Equal(t, int64(1), count)

	disabled, err := fixture.service.SetTechnicalResourceLifecycle(ctx, fixture.authorization, SetTechnicalResourceLifecycleInput{
		TechnicalResourceID: agent.ID, TargetState: model.TechnicalResourceDisabled,
		ExpectedRowVersion: agent.RowVersion, Reason: "maintenance",
	})
	require.NoError(t, err)
	_, err = fixture.service.RecordTechnicalResourceHeartbeat(ctx, credential, time.Minute)
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
	_, err = fixture.service.RecordTechnicalResourceHeartbeat(ctx, credential, time.Minute)
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

	_, err = fixture.service.RecordTechnicalResourceHeartbeat(ctx, TechnicalResourceCredential{
		SourceType: model.TechnicalResourceBindingLegacyNode, SourceID: "1001", CredentialRevision: 2,
	}, time.Minute)
	require.ErrorIs(t, err, ErrCredentialRevisionStale)

	require.NoError(t, fixture.database.Model(&model.ResourceProvider{}).Where("id = ?", fixture.provider.ID).Update("status", model.ProviderStatusSuspended).Error)
	_, err = fixture.service.SetTechnicalResourceLifecycle(ctx, fixture.authorization, SetTechnicalResourceLifecycleInput{
		TechnicalResourceID: agent.ID, TargetState: model.TechnicalResourceDisabled,
		ExpectedRowVersion: agent.RowVersion, Reason: "suspended Provider cannot write",
	})
	require.ErrorIs(t, err, ErrManagementPermissionDenied)
}
