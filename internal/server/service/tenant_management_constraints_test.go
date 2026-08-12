package service

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	serverdb "github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

type tenantManagementConstraintFixture struct {
	now             time.Time
	actor           model.User
	member          model.User
	tenant          model.Tenant
	membership      model.TenantMembership
	desktop         model.Node
	actorMembership model.TenantMembership
	actorDesktop    model.Node
	technical       model.TechnicalResource
	allocation      model.ResourceAllocation
	item            model.ResourceAllocationItem
	resource        model.TenantResource
	source          model.TenantResourceSource
	target          model.TenantResourceTargetRevision
	grant           model.TenantAccessGrant
	authorization   *ManagementAuthorizationContext
}

func newTenantManagementConstraintFixture(t *testing.T) tenantManagementConstraintFixture {
	t.Helper()
	original := serverdb.DB
	t.Cleanup(func() { serverdb.DB = original })
	require.NoError(t, serverdb.InitDB(config.DatabaseSection{Type: "sqlite", Path: filepath.Join(t.TempDir(), "signal.db")}))
	database := serverdb.DB
	t.Cleanup(func() {
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	now := time.Now().UTC()
	actor := model.User{ID: 7001, Name: "tenant-trigger-admin", Role: model.UserRoleClient, SecretHash: "fixture", Enabled: true}
	member := model.User{ID: 7002, Name: "tenant-trigger-member", Role: model.UserRoleClient, SecretHash: "fixture", Enabled: true}
	require.NoError(t, database.Create(&[]model.User{actor, member}).Error)
	require.NoError(t, database.Create(&model.UserIdentityProfile{
		UserID: actor.ID, Username: "tenant-trigger-admin", DisplayName: "Tenant Trigger Admin",
		Enabled: true, AuthRevision: 1, RowVersion: 1,
	}).Error)
	tenant := model.Tenant{ID: uuid.NewString(), Key: "tenant-trigger", Name: "Tenant Trigger", Status: model.TenantStatusActive}
	require.NoError(t, database.Create(&tenant).Error)
	require.NoError(t, database.Create(&model.UserTenantManagementMembership{
		ID: uuid.NewString(), UserID: actor.ID, TenantID: tenant.ID, Role: model.TenantManagementRoleAdmin,
		Enabled: true, ValidFrom: now.Add(-time.Minute), PermissionRevision: 5,
		CreatedByUserID: actor.ID, Reason: "Tenant trigger fixture", RowVersion: 1,
	}).Error)
	membership := model.TenantMembership{ID: 7101, TenantID: tenant.ID, UserID: member.ID, Role: "member", Enabled: true}
	actorMembership := model.TenantMembership{ID: 7102, TenantID: tenant.ID, UserID: actor.ID, Role: "member", Enabled: true}
	require.NoError(t, database.Create(&[]model.TenantMembership{membership, actorMembership}).Error)
	desktop := model.Node{ID: 7201, UserID: member.ID, Name: "tenant-trigger-desktop", Type: model.NodeTypeDesktop, LastHeartbeat: &now}
	agentNode := model.Node{ID: 7202, UserID: actor.ID, Name: "tenant-trigger-agent", Type: model.NodeTypeAgent, LastHeartbeat: &now}
	actorDesktop := model.Node{ID: 7203, UserID: actor.ID, Name: "tenant-trigger-admin-desktop", Type: model.NodeTypeDesktop, LastHeartbeat: &now}
	require.NoError(t, database.Create(&[]model.Node{desktop, agentNode, actorDesktop}).Error)

	provider := model.ResourceProvider{ID: uuid.NewString(), Key: "tenant-trigger-provider", DisplayName: "Tenant Trigger Provider", DomainScope: model.ProviderDomainNamed, DomainLabel: "tenant-trigger-provider", Status: model.ProviderStatusActive, Revision: 1, RowVersion: 1}
	require.NoError(t, database.Create(&provider).Error)
	technical := model.TechnicalResource{
		ID: uuid.NewString(), ProviderID: provider.ID, Type: model.TechnicalResourceAgent, StableKey: "tenant-trigger-agent", DomainLabel: "tenant-trigger-agent",
		LifecycleState: model.TechnicalResourceRegistered, HealthState: model.ResourceHealthOnline,
		CredentialRevision: 1, ConfigRevision: 1, RowVersion: 1,
	}
	require.NoError(t, database.Create(&technical).Error)
	require.NoError(t, database.Create(&model.TechnicalResourceBinding{
		ID: uuid.NewString(), TechnicalResourceID: technical.ID, SourceType: model.TechnicalResourceBindingLegacyNode,
		SourceID: fmt.Sprint(agentNode.ID), CredentialRevision: 1, Enabled: true, BoundByUserID: actor.ID,
		Reason: "Tenant trigger binding", RowVersion: 1,
	}).Error)
	clusterStableKey := supplyStableDigest("kubernetes-cluster-v1", "tenant-trigger-cluster")
	candidate := model.SupplyCandidate{
		ID: uuid.NewString(), ProviderID: provider.ID, TechnicalResourceID: technical.ID,
		ResourceType: model.SupplyResourceKubernetes, StableKey: clusterStableKey, IdentityQuality: model.SupplyIdentityStrong,
		PayloadHash: strings.Repeat("a", 64), ObservationSnapshot: `{"capabilities":["workload_inventory_v1"]}`,
		FirstObservedAt: now.Add(-time.Minute), LastObservedAt: now, LeaseExpiresAt: now.Add(time.Hour),
		ReviewState: model.SupplyCandidateAccepted, RowVersion: 1,
	}
	require.NoError(t, database.Create(&candidate).Error)
	platform := model.PlatformResource{
		ID: uuid.NewString(), ProviderID: provider.ID, Type: model.SupplyResourceKubernetes, StableKey: clusterStableKey,
		DisplayName: "Tenant Trigger Cluster", LifecycleState: model.PlatformResourceActive, HealthState: model.ResourceHealthOnline,
		CapabilityRevision: 1, AllocatableScopeCount: 1, RowVersion: 1,
	}
	require.NoError(t, database.Create(&platform).Error)
	require.NoError(t, database.Create(&model.PlatformResourceSource{
		ID: uuid.NewString(), ProviderID: provider.ID, PlatformResourceID: platform.ID,
		SupplyCandidateID: candidate.ID, IsPrimary: true, LinkedAt: now, LastConfirmedAt: now,
	}).Error)
	require.NoError(t, database.Model(&model.SupplyCandidate{}).Where("id = ?", candidate.ID).Updates(map[string]any{
		"review_state": model.SupplyCandidateLinked, "row_version": int64(2),
	}).Error)
	namespaceObservation := model.NamespaceObservation{
		ID: uuid.NewString(), ProviderID: provider.ID, ClusterResourceID: platform.ID,
		NamespaceUID: "tenant-trigger-namespace-uid", Name: "tenant-trigger-workloads", Revision: 1,
		ObservedAt: now, LeaseExpiresAt: now.Add(time.Hour), State: model.NamespaceObservationObserved,
	}
	require.NoError(t, database.Create(&namespaceObservation).Error)
	clusterScope := model.ResourceScope{
		ID: uuid.NewString(), ProviderID: provider.ID, PlatformResourceID: platform.ID,
		Type: model.ResourceScopeCluster, StableKey: clusterStableKey, LifecycleState: model.ResourceScopeActive,
		IsolationMode: model.ResourceScopeIsolationNone, ConfigRevision: 1, EvidenceRevision: 1, RowVersion: 1,
	}
	require.NoError(t, database.Create(&clusterScope).Error)
	namespaceScope := model.ResourceScope{
		ID: uuid.NewString(), ProviderID: provider.ID, PlatformResourceID: platform.ID,
		Type: model.ResourceScopeNamespace, StableKey: supplyStableDigest("kubernetes-namespace-v1", platform.ID+"\x00"+namespaceObservation.NamespaceUID),
		ParentID: &clusterScope.ID, NamespaceObservationID: &namespaceObservation.ID,
		LifecycleState: model.ResourceScopeAllocatable, IsolationMode: model.ResourceScopeIsolationNamespaceIsolated,
		ConfigRevision: 1, EvidenceRevision: 1, RowVersion: 1,
	}
	require.NoError(t, database.Create(&namespaceScope).Error)
	expiresAt := now.Add(time.Hour)
	allocation := model.ResourceAllocation{
		ID: uuid.NewString(), TenantID: tenant.ID, Mode: model.ResourceAllocationLeased,
		ValidFrom: now.Add(-time.Minute), ExpiresAt: &expiresAt, State: model.ResourceAllocationDraft,
		RowVersion: 1, CreatedByUserID: actor.ID,
	}
	require.NoError(t, database.Create(&allocation).Error)
	item := model.ResourceAllocationItem{ID: uuid.NewString(), AllocationID: allocation.ID, ScopeID: namespaceScope.ID, ScopeRowVersionSnapshot: namespaceScope.RowVersion}
	require.NoError(t, database.Create(&item).Error)
	require.NoError(t, database.Model(&model.ResourceAllocation{}).Where("id = ?", allocation.ID).Updates(map[string]any{
		"state": model.ResourceAllocationActive, "row_version": int64(2),
	}).Error)
	allocation.State, allocation.RowVersion = model.ResourceAllocationActive, 2

	observation := model.WorkloadObservation{
		ID: uuid.NewString(), NamespaceScopeID: namespaceScope.ID, Kind: model.WorkloadObservationServicePort,
		StableKey: strings.Repeat("b", 64), IdentityQuality: model.WorkloadIdentityStrong,
		State: model.WorkloadObservationEligible, Ready: true, ObservedRevision: 1, LabelSnapshot: `{}`,
		FirstObservedAt: now, LastObservedAt: now, LeaseExpiresAt: now.Add(time.Hour), RowVersion: 1,
	}
	require.NoError(t, database.Create(&observation).Error)
	targetSnapshot := `{"namespace_uid":"tenant-trigger-namespace-uid","namespace_name":"tenant-trigger-workloads","service_uid":"trigger-service","service_name":"trigger","cluster_ip":"10.0.0.20","port_name":"https","port_number":443,"protocol":"TCP","labels_allowlist":{}}`
	evidence := model.WorkloadObservationSource{
		ID: uuid.NewString(), WorkloadObservationID: observation.ID, SourceTechnicalResourceID: technical.ID,
		SourceEpoch: uuid.NewString(), Sequence: 1, PayloadHash: strings.Repeat("c", 64),
		State: model.WorkloadObservationSourceObserved, Ready: true, TargetSnapshot: targetSnapshot,
		ObservedAt: now, ReceivedAt: now, LeaseExpiresAt: now.Add(time.Hour), SourceRevision: 1, RowVersion: 1,
	}
	require.NoError(t, database.Create(&evidence).Error)
	resource := model.TenantResource{
		ID: uuid.NewString(), TenantID: tenant.ID, Type: model.TenantResourceContainerService,
		StableKey: strings.Repeat("d", 64), EntitlementLineageID: allocation.ID, DisplayName: "trigger:443",
		VisibilityState: model.TenantResourceVisible, AvailabilityState: model.TenantResourceAvailable, Revision: 1, RowVersion: 1,
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
		Status: model.TenantAccessGrantEnabled, Revision: 1, RowVersion: 1, CreatedByUserID: actor.ID,
	}
	require.NoError(t, database.Create(&grant).Error)

	authorization := &ManagementAuthorizationContext{
		ActorUserID: actor.ID, EffectiveUserID: actor.ID, ScopeType: model.ManagementScopeTenant,
		ScopeID: tenant.ID, PermissionRevision: 5,
	}
	return tenantManagementConstraintFixture{
		now: now, actor: actor, member: member, tenant: tenant, membership: membership, desktop: desktop,
		actorMembership: actorMembership, actorDesktop: actorDesktop,
		technical: technical, allocation: allocation, item: item, resource: resource, source: source,
		target: target, grant: grant, authorization: authorization,
	}
}

func (f tenantManagementConstraintFixture) session(id string) model.ResourceSession {
	return model.ResourceSession{
		ID: id, TenantID: f.tenant.ID, TenantResourceID: f.resource.ID, TenantResourceSourceID: f.source.ID,
		TargetRevisionID: f.target.ID, AllocationID: f.allocation.ID, AllocationItemID: f.item.ID,
		GrantID: f.grant.ID, GrantRevision: f.grant.Revision, UserID: f.member.ID,
		TenantMembershipID: f.membership.ID, DeviceID: f.desktop.ID,
		ActorUserID: f.member.ID, EffectiveUserID: f.member.ID,
		SessionType: model.ResourceSessionContainerService, Action: "connect",
		AccessTechnicalResourceID: f.technical.ID, AuthorizationRevision: 1,
		ValidUntil: f.now.Add(30 * time.Second), Status: model.ResourceSessionAuthorizing,
		RequestID: "request-" + id, StartedAt: f.now, RowVersion: 1,
	}
}

func TestTenantManagementServicesRespectS4Triggers(t *testing.T) {
	fixture := newTenantManagementConstraintFixture(t)
	database := serverdb.DB
	candidateResource := fixture.resource
	candidateResource.ID = uuid.NewString()
	candidateResource.StableKey = strings.Repeat("e", 64)
	candidateResource.VisibilityState = model.TenantResourcePending
	require.NoError(t, database.Create(&candidateResource).Error)
	candidateSource := fixture.source
	candidateSource.ID = uuid.NewString()
	candidateSource.TenantResourceID = candidateResource.ID
	require.NoError(t, database.Create(&candidateSource).Error)
	candidateTarget := fixture.target
	candidateTarget.ID = uuid.NewString()
	candidateTarget.TenantResourceSourceID = candidateSource.ID
	require.NoError(t, database.Create(&candidateTarget).Error)
	published, err := NewTenantResourceService(database, NewWorkloadSnapshotStore()).Review(context.Background(), fixture.authorization, ReviewTenantResourceInput{
		TenantID: fixture.tenant.ID, ResourceID: candidateResource.ID, ExpectedRowVersion: 1,
		ObservationRevision: 1, Reason: "approved by trigger test", Publish: true,
	})
	require.NoError(t, err)
	require.Equal(t, model.TenantResourceVisible, published.VisibilityState)
	require.Equal(t, int64(2), published.Revision)

	displayName := "Tenant Trigger Service 443"
	resource, err := NewTenantResourceService(database, NewWorkloadSnapshotStore()).Update(context.Background(), fixture.authorization, UpdateTenantResourceInput{
		TenantID: fixture.tenant.ID, ResourceID: fixture.resource.ID, ExpectedRowVersion: 1, DisplayName: &displayName,
	})
	require.NoError(t, err)
	require.Equal(t, int64(2), resource.Revision)
	require.Equal(t, int64(2), resource.RowVersion)

	actorID := fixture.actor.ID
	actorGrant, err := NewTenantAccessGrantService(database).Create(context.Background(), fixture.authorization, CreateTenantGrantInput{
		TenantID: fixture.tenant.ID, ResourceID: fixture.resource.ID,
		SubjectType: model.TenantAccessGrantSubjectUser, SubjectUserID: &actorID,
		Actions: []string{"connect"}, MaxSessionSeconds: 3600, RequestID: "request-create-trigger-grant",
	})
	require.NoError(t, err)
	require.Equal(t, model.TenantAccessGrantEnabled, actorGrant.Status)
	authorizedSession, err := NewResourceSessionService(database).Create(context.Background(), fixture.authorization, CreateResourceSessionInput{
		TenantID: fixture.tenant.ID, ResourceID: fixture.resource.ID, Action: "connect",
		DeviceID: fixture.actorDesktop.ID, ClientCapability: "resource_session_v2", RequestID: "request-create-trigger-session",
	})
	require.NoError(t, err)
	require.Equal(t, actorGrant.ID, authorizedSession.GrantID)
	require.Equal(t, fixture.actorMembership.ID, authorizedSession.TenantMembershipID)
	require.Equal(t, model.ResourceSessionAuthorizing, authorizedSession.Status)

	session := fixture.session("session-trigger-grant-suspend")
	require.NoError(t, database.Create(&session).Error)
	grant, err := NewTenantAccessGrantService(database).Suspend(context.Background(), fixture.authorization, TenantGrantActionInput{
		TenantID: fixture.tenant.ID, GrantID: fixture.grant.ID, ExpectedRowVersion: 1,
		Reason: "security review", RequestID: "request-grant-suspend",
	})
	require.NoError(t, err)
	require.Equal(t, model.TenantAccessGrantSuspended, grant.Status)
	require.Equal(t, int64(2), grant.Revision)

	var persistedSession model.ResourceSession
	require.NoError(t, database.First(&persistedSession, "id = ?", session.ID).Error)
	require.Equal(t, model.ResourceSessionEnding, persistedSession.Status)
	require.Equal(t, int64(2), persistedSession.RowVersion)
	var terminationCount, eventCount, outboxCount int64
	require.NoError(t, database.Model(&model.ResourceSessionTermination{}).Where("session_id = ?", session.ID).Count(&terminationCount).Error)
	require.NoError(t, database.Model(&model.TenantAccessGrantEvent{}).Where("grant_id = ? AND grant_revision = ?", grant.ID, grant.Revision).Count(&eventCount).Error)
	require.NoError(t, database.Model(&model.OutboxEvent{}).Where("aggregate_id = ?", session.ID).Count(&outboxCount).Error)
	require.Equal(t, int64(1), terminationCount)
	require.Equal(t, int64(1), eventCount)
	require.Equal(t, int64(1), outboxCount)
}

func TestResourceSessionEnsureForDesktopUsesMemberGrantAndReusesLiveSession(t *testing.T) {
	fixture := newTenantManagementConstraintFixture(t)
	staleDatabaseHeartbeat := time.Now().UTC().Add(-10 * time.Minute)
	require.NoError(t, serverdb.DB.Model(&model.Node{}).Where("id = ?", fixture.desktop.ID).Updates(map[string]any{
		"headscale_node_id": uint64(8201), "last_heartbeat": staleDatabaseHeartbeat,
	}).Error)
	sessions := NewResourceSessionService(serverdb.DB)
	input := CreateResourceSessionInput{
		TenantID: fixture.tenant.ID, ResourceID: fixture.resource.ID, Action: "connect", DeviceID: fixture.desktop.ID,
		ClientCapability: "resource_session_v2", RequestID: "desktop-member-session-a",
	}
	first, err := sessions.EnsureForDesktop(context.Background(), fixture.member.ID, input)
	require.NoError(t, err)
	require.Equal(t, fixture.grant.ID, first.GrantID)
	require.Equal(t, fixture.membership.ID, first.TenantMembershipID)
	require.Equal(t, fixture.desktop.ID, first.DeviceID)

	input.RequestID = "desktop-member-session-b"
	second, err := sessions.EnsureForDesktop(context.Background(), fixture.member.ID, input)
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID)
	var count int64
	require.NoError(t, serverdb.DB.Model(&model.ResourceSession{}).Where("tenant_resource_id = ? AND user_id = ? AND device_id = ?", fixture.resource.ID, fixture.member.ID, fixture.desktop.ID).Count(&count).Error)
	require.Equal(t, int64(1), count)

	memberAuthorization := &ManagementAuthorizationContext{
		ActorUserID: fixture.member.ID, EffectiveUserID: fixture.member.ID,
		ScopeType: model.ManagementScopeTenant, ScopeID: fixture.tenant.ID,
		Role: "member", PermissionRevision: 1,
	}
	_, err = sessions.List(context.Background(), memberAuthorization, fixture.tenant.ID, ResourceSessionListInput{})
	require.ErrorIs(t, err, ErrManagementPermissionDenied)
}

func TestResourceSessionEnsureForDesktopAcceptsCurrentEvidenceWithIndependentRevision(t *testing.T) {
	fixture := newTenantManagementConstraintFixture(t)
	for revision := int64(2); revision <= 11; revision++ {
		require.NoError(t, serverdb.DB.Model(&model.WorkloadObservationSource{}).
			Where("workload_observation_id = ? AND source_technical_resource_id = ?", fixture.source.WorkloadObservationID, fixture.technical.ID).
			Updates(map[string]any{"source_revision": revision, "row_version": gorm.Expr("row_version + 1")}).Error)
	}

	session, err := NewResourceSessionService(serverdb.DB).EnsureForDesktop(context.Background(), fixture.member.ID, CreateResourceSessionInput{
		TenantID: fixture.tenant.ID, ResourceID: fixture.resource.ID, Action: "connect", DeviceID: fixture.desktop.ID,
		ClientCapability: "resource_session_v2", RequestID: "desktop-independent-evidence-revision",
	})
	require.NoError(t, err)
	require.Equal(t, fixture.target.ID, session.TargetRevisionID)
}

func TestResourceSessionEnsureForDesktopRejectsUnavailableEvidence(t *testing.T) {
	tests := []struct {
		name    string
		updates map[string]any
	}{
		{name: "not ready", updates: map[string]any{"ready": false}},
		{name: "stale", updates: map[string]any{"state": model.WorkloadObservationSourceStale}},
		{name: "expired", updates: map[string]any{
			"received_at": time.Now().UTC().Add(-2 * time.Minute), "lease_expires_at": time.Now().UTC().Add(-time.Minute),
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newTenantManagementConstraintFixture(t)
			tt.updates["row_version"] = gorm.Expr("row_version + 1")
			require.NoError(t, serverdb.DB.Model(&model.WorkloadObservationSource{}).
				Where("workload_observation_id = ? AND source_technical_resource_id = ?", fixture.source.WorkloadObservationID, fixture.technical.ID).
				Updates(tt.updates).Error)

			_, err := NewResourceSessionService(serverdb.DB).EnsureForDesktop(context.Background(), fixture.member.ID, CreateResourceSessionInput{
				TenantID: fixture.tenant.ID, ResourceID: fixture.resource.ID, Action: "connect", DeviceID: fixture.desktop.ID,
				ClientCapability: "resource_session_v2", RequestID: "desktop-unavailable-evidence-" + tt.name,
			})
			require.ErrorIs(t, err, ErrResourceSessionTargetUnavailable)
		})
	}
}

func TestResourceSessionEnsureManyForDesktopCommitsValidInputsAndIsolatesFailures(t *testing.T) {
	fixture := newTenantManagementConstraintFixture(t)
	service := NewResourceSessionService(serverdb.DB)
	base := CreateResourceSessionInput{
		TenantID: fixture.tenant.ID, ResourceID: fixture.resource.ID, Action: "connect", DeviceID: fixture.desktop.ID,
		ClientCapability: "resource_session_v2",
	}
	valid := base
	valid.TargetRevisionID = fixture.target.ID
	valid.RequestID = "desktop-batch-valid"
	invalid := base
	invalid.TargetRevisionID = uuid.NewString()
	invalid.RequestID = "desktop-batch-invalid"

	results, err := service.EnsureManyForDesktop(context.Background(), fixture.member.ID, []CreateResourceSessionInput{valid, invalid})
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.NotNil(t, results[0].Session)
	require.NoError(t, results[0].Err)
	require.Nil(t, results[1].Session)
	require.ErrorIs(t, results[1].Err, ErrResourceSessionTargetUnavailable)
	var count int64
	require.NoError(t, serverdb.DB.Model(&model.ResourceSession{}).Where("request_id = ?", valid.RequestID).Count(&count).Error)
	require.Equal(t, int64(1), count)
}

func TestResourceSessionEnsureManyForDesktopStopsOnCanceledContext(t *testing.T) {
	fixture := newTenantManagementConstraintFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	results, err := NewResourceSessionService(serverdb.DB).EnsureManyForDesktop(ctx, fixture.member.ID, []CreateResourceSessionInput{{
		TenantID: fixture.tenant.ID, ResourceID: fixture.resource.ID, TargetRevisionID: fixture.target.ID,
		Action: "connect", DeviceID: fixture.desktop.ID, ClientCapability: "resource_session_v2", RequestID: "desktop-batch-canceled",
	}})
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, results)
	var count int64
	require.NoError(t, serverdb.DB.Model(&model.ResourceSession{}).Where("request_id = ?", "desktop-batch-canceled").Count(&count).Error)
	require.Zero(t, count)
}

func TestTenantGrantMutationRollsBackWhenTerminationOutboxFails(t *testing.T) {
	fixture := newTenantManagementConstraintFixture(t)
	database := serverdb.DB
	session := fixture.session("session-trigger-outbox-rollback")
	require.NoError(t, database.Create(&session).Error)
	require.NoError(t, database.Migrator().DropTable(&model.OutboxEvent{}))

	_, err := NewTenantAccessGrantService(database).Suspend(context.Background(), fixture.authorization, TenantGrantActionInput{
		TenantID: fixture.tenant.ID, GrantID: fixture.grant.ID, ExpectedRowVersion: 1,
		Reason: "must roll back", RequestID: "request-outbox-rollback",
	})
	require.Error(t, err)

	var grant model.TenantAccessGrant
	require.NoError(t, database.First(&grant, "id = ?", fixture.grant.ID).Error)
	require.Equal(t, model.TenantAccessGrantEnabled, grant.Status)
	require.Equal(t, int64(1), grant.Revision)
	var persistedSession model.ResourceSession
	require.NoError(t, database.First(&persistedSession, "id = ?", session.ID).Error)
	require.Equal(t, model.ResourceSessionAuthorizing, persistedSession.Status)
	require.Equal(t, int64(1), persistedSession.RowVersion)
	var terminationCount, eventCount int64
	require.NoError(t, database.Model(&model.ResourceSessionTermination{}).Where("session_id = ?", session.ID).Count(&terminationCount).Error)
	require.NoError(t, database.Model(&model.TenantAccessGrantEvent{}).Where("grant_id = ? AND grant_revision = 2", grant.ID).Count(&eventCount).Error)
	require.Zero(t, terminationCount)
	require.Zero(t, eventCount)
}

func TestTenantGrantExpiryEndsSessionsAndIsIdempotent(t *testing.T) {
	fixture := newTenantManagementConstraintFixture(t)
	database := serverdb.DB
	expiresAt := fixture.now.Add(time.Minute)
	require.NoError(t, database.Model(&model.TenantAccessGrant{}).Where("id = ?", fixture.grant.ID).Updates(map[string]any{
		"expires_at": expiresAt, "revision": int64(2), "row_version": int64(2),
	}).Error)
	fixture.grant.ExpiresAt = &expiresAt
	fixture.grant.Revision = 2
	fixture.grant.RowVersion = 2
	session := fixture.session("session-trigger-grant-expiry")
	session.GrantRevision = fixture.grant.Revision
	require.NoError(t, database.Create(&session).Error)

	maintenance := NewTenantAuthorizationMaintenanceService(database)
	maintenance.now = func() time.Time { return fixture.now.Add(2 * time.Minute) }
	count, err := maintenance.ExpireDueGrants(context.Background(), 100)
	require.NoError(t, err)
	require.Equal(t, 1, count)

	var grant model.TenantAccessGrant
	require.NoError(t, database.First(&grant, "id = ?", fixture.grant.ID).Error)
	require.Equal(t, model.TenantAccessGrantExpired, grant.Status)
	require.Equal(t, int64(3), grant.Revision)
	require.Equal(t, int64(3), grant.RowVersion)
	var persistedSession model.ResourceSession
	require.NoError(t, database.First(&persistedSession, "id = ?", session.ID).Error)
	require.Equal(t, model.ResourceSessionEnding, persistedSession.Status)
	require.Equal(t, tenantGrantExpiryReasonCode, persistedSession.CloseReason)

	var grantEventCount, terminationCount, outboxCount, auditCount int64
	require.NoError(t, database.Model(&model.TenantAccessGrantEvent{}).Where("grant_id = ? AND grant_revision = ? AND event_type = ?", grant.ID, grant.Revision, "expired").Count(&grantEventCount).Error)
	require.NoError(t, database.Model(&model.ResourceSessionTermination{}).Where("session_id = ?", session.ID).Count(&terminationCount).Error)
	require.NoError(t, database.Model(&model.OutboxEvent{}).Where("aggregate_id IN ?", []string{grant.ID, session.ID}).Count(&outboxCount).Error)
	require.NoError(t, database.Model(&model.AuditLog{}).Where("target_id = ? AND action_type = ?", grant.ID, "expire_tenant_access_grant").Count(&auditCount).Error)
	require.Equal(t, int64(1), grantEventCount)
	require.Equal(t, int64(1), terminationCount)
	require.Equal(t, int64(2), outboxCount)
	require.Equal(t, int64(1), auditCount)

	count, err = maintenance.ExpireDueGrants(context.Background(), 100)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestTenantGrantExpiryRollsBackWhenOutboxFails(t *testing.T) {
	fixture := newTenantManagementConstraintFixture(t)
	database := serverdb.DB
	expiresAt := fixture.now.Add(time.Minute)
	require.NoError(t, database.Model(&model.TenantAccessGrant{}).Where("id = ?", fixture.grant.ID).Updates(map[string]any{
		"expires_at": expiresAt, "revision": int64(2), "row_version": int64(2),
	}).Error)
	fixture.grant.ExpiresAt = &expiresAt
	fixture.grant.Revision = 2
	fixture.grant.RowVersion = 2
	session := fixture.session("session-trigger-grant-expiry-rollback")
	session.GrantRevision = fixture.grant.Revision
	require.NoError(t, database.Create(&session).Error)
	require.NoError(t, database.Migrator().DropTable(&model.OutboxEvent{}))

	maintenance := NewTenantAuthorizationMaintenanceService(database)
	maintenance.now = func() time.Time { return fixture.now.Add(2 * time.Minute) }
	_, err := maintenance.ExpireDueGrants(context.Background(), 100)
	require.Error(t, err)

	var grant model.TenantAccessGrant
	require.NoError(t, database.First(&grant, "id = ?", fixture.grant.ID).Error)
	require.Equal(t, model.TenantAccessGrantEnabled, grant.Status)
	require.Equal(t, int64(2), grant.Revision)
	var persistedSession model.ResourceSession
	require.NoError(t, database.First(&persistedSession, "id = ?", session.ID).Error)
	require.Equal(t, model.ResourceSessionAuthorizing, persistedSession.Status)
	var eventCount, terminationCount, auditCount int64
	require.NoError(t, database.Model(&model.TenantAccessGrantEvent{}).Where("grant_id = ? AND grant_revision = ?", grant.ID, int64(3)).Count(&eventCount).Error)
	require.NoError(t, database.Model(&model.ResourceSessionTermination{}).Where("session_id = ?", session.ID).Count(&terminationCount).Error)
	require.NoError(t, database.Model(&model.AuditLog{}).Where("target_id = ?", grant.ID).Count(&auditCount).Error)
	require.Zero(t, eventCount)
	require.Zero(t, terminationCount)
	require.Zero(t, auditCount)
}

func TestDisabledSessionAuthorizationEndsSessionsAndRollsBackAtomically(t *testing.T) {
	t.Run("ending is idempotent", func(t *testing.T) {
		fixture := newTenantManagementConstraintFixture(t)
		database := serverdb.DB
		session := fixture.session("session-trigger-authorization-disabled")
		require.NoError(t, database.Create(&session).Error)

		maintenance := NewTenantAuthorizationMaintenanceService(database)
		count, err := maintenance.DrainSessionsWhenAuthorizationDisabled(context.Background())
		require.NoError(t, err)
		require.Equal(t, 1, count)
		count, err = maintenance.DrainSessionsWhenAuthorizationDisabled(context.Background())
		require.NoError(t, err)
		require.Zero(t, count)

		var persisted model.ResourceSession
		require.NoError(t, database.First(&persisted, "id = ?", session.ID).Error)
		require.Equal(t, model.ResourceSessionEnding, persisted.Status)
		require.Equal(t, sessionAuthorizationDisabledReasonCode, persisted.CloseReason)
		var terminationCount, outboxCount, auditCount int64
		require.NoError(t, database.Model(&model.ResourceSessionTermination{}).Where("session_id = ?", session.ID).Count(&terminationCount).Error)
		require.NoError(t, database.Model(&model.OutboxEvent{}).Where("aggregate_id = ?", session.ID).Count(&outboxCount).Error)
		require.NoError(t, database.Model(&model.AuditLog{}).Where("target_id = ? AND action_type = ?", session.ID, "end_resource_session_for_disabled_authorization").Count(&auditCount).Error)
		require.Equal(t, int64(1), terminationCount)
		require.Equal(t, int64(1), outboxCount)
		require.Equal(t, int64(1), auditCount)
	})

	t.Run("outbox failure rolls back session and termination", func(t *testing.T) {
		fixture := newTenantManagementConstraintFixture(t)
		database := serverdb.DB
		session := fixture.session("session-trigger-authorization-disabled-rollback")
		require.NoError(t, database.Create(&session).Error)
		require.NoError(t, database.Migrator().DropTable(&model.OutboxEvent{}))

		_, err := NewTenantAuthorizationMaintenanceService(database).DrainSessionsWhenAuthorizationDisabled(context.Background())
		require.Error(t, err)
		var persisted model.ResourceSession
		require.NoError(t, database.First(&persisted, "id = ?", session.ID).Error)
		require.Equal(t, model.ResourceSessionAuthorizing, persisted.Status)
		var terminationCount, auditCount int64
		require.NoError(t, database.Model(&model.ResourceSessionTermination{}).Where("session_id = ?", session.ID).Count(&terminationCount).Error)
		require.NoError(t, database.Model(&model.AuditLog{}).Where("target_id = ?", session.ID).Count(&auditCount).Error)
		require.Zero(t, terminationCount)
		require.Zero(t, auditCount)
	})
}
