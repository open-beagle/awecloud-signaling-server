package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

type platformAllocationFixture struct {
	providerSupplyFixture
	service       *PlatformAllocationService
	authorization *ManagementAuthorizationContext
	tenantA       model.Tenant
	tenantB       model.Tenant
	scopeA        model.ResourceScope
	scopeB        model.ResourceScope
	clusterScope  model.ResourceScope
}

func newPlatformAllocationFixture(t *testing.T) platformAllocationFixture {
	t.Helper()
	providerFixture := newProviderSupplyFixture(t)
	require.NoError(t, providerFixture.database.AutoMigrate(
		&model.Tenant{}, &model.PlatformRoleMembership{}, &model.ResourceAllocation{}, &model.ResourceAllocationItem{},
	))
	require.NoError(t, providerFixture.database.Create(&model.PlatformRoleMembership{
		ID: uuid.NewString(), UserID: providerFixture.actor.ID, Role: model.PlatformRoleAdmin, Enabled: true,
		ValidFrom: providerFixture.now.Add(-time.Hour), PermissionRevision: 3, CreatedByUserID: providerFixture.actor.ID,
		Reason: "Platform allocation fixture", RowVersion: 1,
	}).Error)
	tenantA := model.Tenant{ID: uuid.NewString(), Key: "allocation-tenant-a", Name: "Allocation Tenant A", Status: model.TenantStatusActive}
	tenantB := model.Tenant{ID: uuid.NewString(), Key: "allocation-tenant-b", Name: "Allocation Tenant B", Status: model.TenantStatusActive}
	require.NoError(t, providerFixture.database.Create(&[]model.Tenant{tenantA, tenantB}).Error)
	accepted := createLifecycleSupplyResource(t, providerFixture)
	_, cluster, namespaceA := activateLifecycleResourceAndNamespace(t, providerFixture, accepted)
	markedA, err := providerFixture.service.MarkResourceScopeAllocatable(context.Background(), providerFixture.authorization, MarkResourceScopeAllocatableInput{
		ScopeID: namespaceA.ID, ExpectedRowVersion: namespaceA.RowVersion, Reason: "publish first namespace",
	})
	require.NoError(t, err)
	namespaceB, err := providerFixture.service.SetResourceScopeLifecycle(context.Background(), providerFixture.authorization, SetResourceScopeLifecycleInput{
		ScopeID: accepted.NamespaceScopes[1].ID, TargetState: model.ResourceScopeActive,
		ExpectedRowVersion: accepted.NamespaceScopes[1].RowVersion, Reason: "activate second namespace",
	})
	require.NoError(t, err)
	markedB, err := providerFixture.service.MarkResourceScopeAllocatable(context.Background(), providerFixture.authorization, MarkResourceScopeAllocatableInput{
		ScopeID: namespaceB.ID, ExpectedRowVersion: namespaceB.RowVersion, Reason: "publish second namespace",
	})
	require.NoError(t, err)
	authorization, err := ResolveManagementContext(providerFixture.database, providerFixture.actor.ID, model.ManagementScopePlatform, "", providerFixture.now, false)
	require.NoError(t, err)
	allocationService := NewPlatformAllocationService(providerFixture.database)
	allocationService.now = func() time.Time { return providerFixture.now }
	return platformAllocationFixture{
		providerSupplyFixture: providerFixture, service: allocationService, authorization: authorization,
		tenantA: tenantA, tenantB: tenantB, scopeA: *markedA.Scope, scopeB: *markedB.Scope, clusterScope: *cluster,
	}
}

func TestPlatformAllocationLifecycleAndScopeReservation(t *testing.T) {
	fixture := newPlatformAllocationFixture(t)
	ctx := context.Background()
	allocation, err := fixture.service.CreateDraft(ctx, fixture.authorization, CreatePlatformAllocationInput{
		TenantID: fixture.tenantA.ID, Mode: model.ResourceAllocationAssigned, ScopeID: fixture.scopeA.ID,
		ValidFrom: fixture.now.Add(-time.Minute), ContractRef: "contract-a",
	})
	require.NoError(t, err)
	require.Equal(t, model.ResourceAllocationDraft, allocation.State)
	require.Len(t, allocation.Items, 1)

	active, err := fixture.service.Activate(ctx, fixture.authorization, PlatformAllocationActionInput{
		AllocationID: allocation.ID, ExpectedRowVersion: allocation.RowVersion, Reason: "start assigned capacity",
	})
	require.NoError(t, err)
	require.Equal(t, model.ResourceAllocationActive, active.State)
	require.Equal(t, int64(2), active.RowVersion)
	require.NotNil(t, active.ActivatedAt)

	suspended, err := fixture.service.Suspend(ctx, fixture.authorization, PlatformAllocationActionInput{
		AllocationID: active.ID, ExpectedRowVersion: active.RowVersion, Reason: "temporary contract hold",
	})
	require.NoError(t, err)
	require.Equal(t, model.ResourceAllocationSuspended, suspended.State)

	conflicting, err := fixture.service.CreateDraft(ctx, fixture.authorization, CreatePlatformAllocationInput{
		TenantID: fixture.tenantB.ID, Mode: model.ResourceAllocationLeased, ScopeID: fixture.scopeA.ID,
		ValidFrom: fixture.now.Add(time.Minute), ExpiresAt: timePointer(fixture.now.Add(2 * time.Minute)),
	})
	require.NoError(t, err)
	_, err = fixture.service.Schedule(ctx, fixture.authorization, PlatformAllocationActionInput{
		AllocationID: conflicting.ID, ExpectedRowVersion: conflicting.RowVersion, Reason: "reserve occupied scope",
	})
	require.ErrorIs(t, err, ErrPlatformAllocationScopeConflict)

	resumed, err := fixture.service.Resume(ctx, fixture.authorization, PlatformAllocationActionInput{
		AllocationID: suspended.ID, ExpectedRowVersion: suspended.RowVersion, Reason: "resume contract",
	})
	require.NoError(t, err)
	revoked, err := fixture.service.Revoke(ctx, fixture.authorization, PlatformAllocationActionInput{
		AllocationID: resumed.ID, ExpectedRowVersion: resumed.RowVersion, Reason: "release capacity",
	})
	require.NoError(t, err)
	require.Equal(t, model.ResourceAllocationRevoked, revoked.State)
	require.NotNil(t, revoked.TerminatedByUserID)

	scheduled, err := fixture.service.Schedule(ctx, fixture.authorization, PlatformAllocationActionInput{
		AllocationID: conflicting.ID, ExpectedRowVersion: conflicting.RowVersion, Reason: "reserve released scope",
	})
	require.NoError(t, err)
	require.Equal(t, model.ResourceAllocationScheduled, scheduled.State)
	_, err = fixture.service.Resume(ctx, fixture.authorization, PlatformAllocationActionInput{
		AllocationID: revoked.ID, ExpectedRowVersion: revoked.RowVersion, Reason: "terminal state cannot resume",
	})
	require.ErrorIs(t, err, ErrPlatformAllocationStateTransition)
}

func TestScopeGovernanceSuspensionSuspendsActiveAllocation(t *testing.T) {
	for _, test := range []struct {
		name    string
		suspend func(t *testing.T, fixture platformAllocationFixture)
	}{
		{name: "namespace", suspend: func(t *testing.T, fixture platformAllocationFixture) {
			_, err := fixture.providerSupplyFixture.service.SetResourceScopeLifecycle(context.Background(), fixture.providerSupplyFixture.authorization, SetResourceScopeLifecycleInput{
				ScopeID: fixture.scopeA.ID, TargetState: model.ResourceScopeSuspended,
				ExpectedRowVersion: fixture.scopeA.RowVersion, Reason: "stop assigned namespace",
			})
			require.NoError(t, err)
		}},
		{name: "cluster", suspend: func(t *testing.T, fixture platformAllocationFixture) {
			_, err := fixture.providerSupplyFixture.service.SetResourceScopeLifecycle(context.Background(), fixture.providerSupplyFixture.authorization, SetResourceScopeLifecycleInput{
				ScopeID: fixture.clusterScope.ID, TargetState: model.ResourceScopeSuspended,
				ExpectedRowVersion: fixture.clusterScope.RowVersion, Reason: "stop assigned cluster",
			})
			require.NoError(t, err)
		}},
		{name: "platform resource", suspend: func(t *testing.T, fixture platformAllocationFixture) {
			var resource model.PlatformResource
			require.NoError(t, fixture.database.First(&resource, "id = ?", fixture.scopeA.PlatformResourceID).Error)
			_, err := fixture.providerSupplyFixture.service.SetPlatformResourceLifecycle(context.Background(), fixture.providerSupplyFixture.authorization, SetPlatformResourceLifecycleInput{
				ResourceID: resource.ID, TargetState: model.PlatformResourceSuspended,
				ExpectedRowVersion: resource.RowVersion, Reason: "stop assigned resource",
			})
			require.NoError(t, err)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newPlatformAllocationFixture(t)
			ctx := context.Background()
			allocation, err := fixture.service.CreateDraft(ctx, fixture.authorization, CreatePlatformAllocationInput{
				TenantID: fixture.tenantA.ID, Mode: model.ResourceAllocationAssigned, ScopeID: fixture.scopeA.ID,
				ValidFrom: fixture.now.Add(-time.Minute), ContractRef: "governance-suspension",
			})
			require.NoError(t, err)
			active, err := fixture.service.Activate(ctx, fixture.authorization, PlatformAllocationActionInput{
				AllocationID: allocation.ID, ExpectedRowVersion: allocation.RowVersion, Reason: "activate governed allocation",
			})
			require.NoError(t, err)

			test.suspend(t, fixture)
			var persisted model.ResourceAllocation
			require.NoError(t, fixture.database.First(&persisted, "id = ?", active.ID).Error)
			require.Equal(t, model.ResourceAllocationSuspended, persisted.State)
			require.Equal(t, active.RowVersion+1, persisted.RowVersion)
		})
	}
}

func TestPlatformAllocationDraftPolicyRenewAndQuery(t *testing.T) {
	fixture := newPlatformAllocationFixture(t)
	ctx := context.Background()
	_, err := fixture.service.CreateDraft(ctx, fixture.authorization, CreatePlatformAllocationInput{
		TenantID: fixture.tenantA.ID, Mode: model.ResourceAllocationShared, ScopeID: fixture.scopeA.ID, ValidFrom: fixture.now,
	})
	require.ErrorIs(t, err, ErrPlatformAllocationModeUnsupported)
	_, err = fixture.service.CreateDraft(ctx, fixture.authorization, CreatePlatformAllocationInput{
		TenantID: fixture.tenantA.ID, Mode: model.ResourceAllocationAssigned, ScopeID: fixture.clusterScope.ID, ValidFrom: fixture.now,
	})
	require.ErrorIs(t, err, ErrPlatformAllocationScopeUnavailable)

	draft, err := fixture.service.CreateDraft(ctx, fixture.authorization, CreatePlatformAllocationInput{
		TenantID: fixture.tenantA.ID, Mode: model.ResourceAllocationLeased, ScopeID: fixture.scopeA.ID,
		ValidFrom: fixture.now.Add(-time.Minute), ExpiresAt: timePointer(fixture.now.Add(time.Hour)), ContractRef: "original",
	})
	require.NoError(t, err)
	updated, err := fixture.service.UpdateDraft(ctx, fixture.authorization, UpdatePlatformAllocationDraftInput{
		AllocationID: draft.ID, ExpectedRowVersion: draft.RowVersion, TenantID: fixture.tenantB.ID,
		Mode: model.ResourceAllocationLeased, ScopeID: fixture.scopeB.ID, ValidFrom: fixture.now.Add(-time.Minute),
		ExpiresAt: timePointer(fixture.now.Add(2 * time.Hour)), ContractRef: "updated",
	})
	require.NoError(t, err)
	require.Equal(t, fixture.tenantB.ID, updated.TenantID)
	require.Equal(t, fixture.scopeB.ID, updated.Items[0].ScopeID)

	active, err := fixture.service.Activate(ctx, fixture.authorization, PlatformAllocationActionInput{
		AllocationID: updated.ID, ExpectedRowVersion: updated.RowVersion, Reason: "activate updated draft",
	})
	require.NoError(t, err)
	contractRef := "renewed"
	renewed, err := fixture.service.Renew(ctx, fixture.authorization, RenewPlatformAllocationInput{
		AllocationID: active.ID, ExpectedRowVersion: active.RowVersion, ValidFrom: fixture.now.Add(2 * time.Hour),
		ExpiresAt: timePointer(fixture.now.Add(3 * time.Hour)), ContractRef: &contractRef, Reason: "renew contract",
	})
	require.NoError(t, err)
	require.NotEqual(t, active.ID, renewed.ID)
	require.Equal(t, model.ResourceAllocationDraft, renewed.State)
	require.NotNil(t, renewed.RenewedFromID)
	require.Equal(t, active.ID, *renewed.RenewedFromID)

	list, err := fixture.service.List(ctx, fixture.authorization, PlatformAllocationListInput{TenantID: fixture.tenantB.ID, Page: 1, PageSize: 20})
	require.NoError(t, err)
	require.Equal(t, int64(2), list.Total)
	detail, err := fixture.service.Get(ctx, fixture.authorization, renewed.ID)
	require.NoError(t, err)
	require.Equal(t, renewed.ID, detail.ID)
	require.Len(t, detail.Items, 1)
}

func TestPlatformAllocationRevalidatesTenantPermissionAndHierarchy(t *testing.T) {
	fixture := newPlatformAllocationFixture(t)
	ctx := context.Background()
	require.NoError(t, fixture.database.Model(&model.Tenant{}).Where("id = ?", fixture.tenantA.ID).Update("status", model.TenantStatusSuspended).Error)
	_, err := fixture.service.CreateDraft(ctx, fixture.authorization, CreatePlatformAllocationInput{
		TenantID: fixture.tenantA.ID, Mode: model.ResourceAllocationAssigned, ScopeID: fixture.scopeA.ID, ValidFrom: fixture.now,
	})
	require.ErrorIs(t, err, ErrPlatformAllocationTenantNotActive)
	require.NoError(t, fixture.database.Model(&model.Tenant{}).Where("id = ?", fixture.tenantA.ID).Update("status", model.TenantStatusActive).Error)

	clusterAllocation := model.ResourceAllocation{
		ID: uuid.NewString(), TenantID: fixture.tenantA.ID, Mode: model.ResourceAllocationLeased,
		ValidFrom: fixture.now.Add(time.Minute), ExpiresAt: timePointer(fixture.now.Add(3 * time.Minute)),
		State: model.ResourceAllocationScheduled, RowVersion: 1, CreatedByUserID: fixture.actor.ID,
	}
	require.NoError(t, fixture.database.Create(&clusterAllocation).Error)
	require.NoError(t, fixture.database.Create(&model.ResourceAllocationItem{
		ID: uuid.NewString(), AllocationID: clusterAllocation.ID, ScopeID: fixture.clusterScope.ID,
		ScopeRowVersionSnapshot: fixture.clusterScope.RowVersion,
	}).Error)
	draft, err := fixture.service.CreateDraft(ctx, fixture.authorization, CreatePlatformAllocationInput{
		TenantID: fixture.tenantB.ID, Mode: model.ResourceAllocationLeased, ScopeID: fixture.scopeA.ID,
		ValidFrom: fixture.now.Add(time.Minute), ExpiresAt: timePointer(fixture.now.Add(2 * time.Minute)),
	})
	require.NoError(t, err)
	_, err = fixture.service.Schedule(ctx, fixture.authorization, PlatformAllocationActionInput{
		AllocationID: draft.ID, ExpectedRowVersion: draft.RowVersion, Reason: "hierarchy conflict",
	})
	require.ErrorIs(t, err, ErrPlatformAllocationHierarchyConflict)

	staleAuthorization := *fixture.authorization
	staleAuthorization.PermissionRevision++
	_, err = fixture.service.Get(ctx, &staleAuthorization, draft.ID)
	require.ErrorIs(t, err, ErrManagementPermissionDenied)
}

func TestPlatformAllocationExpiryIsExplicitAndIdempotent(t *testing.T) {
	fixture := newPlatformAllocationFixture(t)
	ctx := context.Background()
	draft, err := fixture.service.CreateDraft(ctx, fixture.authorization, CreatePlatformAllocationInput{
		TenantID: fixture.tenantA.ID, Mode: model.ResourceAllocationLeased, ScopeID: fixture.scopeA.ID,
		ValidFrom: fixture.now.Add(time.Minute), ExpiresAt: timePointer(fixture.now.Add(2 * time.Minute)),
	})
	require.NoError(t, err)
	scheduled, err := fixture.service.Schedule(ctx, fixture.authorization, PlatformAllocationActionInput{
		AllocationID: draft.ID, ExpectedRowVersion: draft.RowVersion, Reason: "reserve expiring capacity",
	})
	require.NoError(t, err)

	expiry := NewPlatformAllocationExpiryService(fixture.database)
	expiry.now = func() time.Time { return fixture.now.Add(3 * time.Minute) }
	count, err := expiry.ExpireDue(ctx, 100)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	var expired model.ResourceAllocation
	require.NoError(t, fixture.database.First(&expired, "id = ?", scheduled.ID).Error)
	require.Equal(t, model.ResourceAllocationExpired, expired.State)
	require.Equal(t, int64(3), expired.RowVersion)
	require.NotNil(t, expired.TerminatedAt)
	require.Nil(t, expired.TerminatedByUserID)
	require.Equal(t, platformAllocationExpiryReason, expired.TerminationReason)

	var outboxCount, auditCount int64
	require.NoError(t, fixture.database.Model(&model.OutboxEvent{}).Where("aggregate_id = ?", expired.ID).Count(&outboxCount).Error)
	require.NoError(t, fixture.database.Model(&model.AuditLog{}).Where("target_id = ? AND action_type = ?", expired.ID, "expire_resource_allocation").Count(&auditCount).Error)
	require.Zero(t, outboxCount)
	require.Equal(t, int64(1), auditCount)

	count, err = expiry.ExpireDue(ctx, 100)
	require.NoError(t, err)
	require.Zero(t, count)
	require.NoError(t, fixture.database.Model(&model.OutboxEvent{}).Where("aggregate_id = ?", expired.ID).Count(&outboxCount).Error)
	require.Zero(t, outboxCount)
}
