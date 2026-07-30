package db

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

type allocationConstraintFixture struct {
	user       model.User
	tenantA    model.Tenant
	tenantB    model.Tenant
	cluster    model.ResourceScope
	namespaceA model.ResourceScope
	namespaceB model.ResourceScope
	now        time.Time
}

func newAllocationConstraintFixture(t *testing.T) allocationConstraintFixture {
	t.Helper()
	original := DB
	t.Cleanup(func() { DB = original })
	require.NoError(t, InitDB(config.DatabaseSection{Type: "sqlite", Path: filepath.Join(t.TempDir(), "signal.db")}))
	t.Cleanup(func() {
		if current, err := DB.DB(); err == nil {
			_ = current.Close()
		}
	})

	var foreignKeysEnabled int
	require.NoError(t, DB.Raw("PRAGMA foreign_keys").Scan(&foreignKeysEnabled).Error)
	require.Zero(t, foreignKeysEnabled)
	require.NoError(t, ensurePlatformAllocationConstraints(DB))
	var triggerCount int64
	require.NoError(t, DB.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger' AND name LIKE 'trg_s3_%'").Scan(&triggerCount).Error)
	require.Equal(t, int64(len(platformAllocationTriggers)), triggerCount)

	now := time.Date(2026, 7, 30, 9, 0, 0, 0, time.UTC)
	user := model.User{ID: 1, Name: "platform-admin", Role: model.UserRoleClient, SecretHash: "fixture", Enabled: true}
	tenantA := model.Tenant{ID: "tenant-a", Key: "tenant-a", Name: "Tenant A", Status: model.TenantStatusActive}
	tenantB := model.Tenant{ID: "tenant-b", Key: "tenant-b", Name: "Tenant B", Status: model.TenantStatusActive}
	provider := model.ResourceProvider{ID: "provider-a", Key: "provider-a", DisplayName: "Provider A", Status: model.ProviderStatusActive, Revision: 1, RowVersion: 1}
	resource := model.PlatformResource{ID: "resource-a", ProviderID: provider.ID, Type: model.SupplyResourceKubernetes, StableKey: "resource-a", DisplayName: "Cluster A", LifecycleState: model.PlatformResourceActive, HealthState: model.ResourceHealthOnline, CapabilityRevision: 1, AllocatableScopeCount: 2, RowVersion: 1}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, DB.Create(&[]model.Tenant{tenantA, tenantB}).Error)
	require.NoError(t, DB.Create(&provider).Error)
	require.NoError(t, DB.Create(&resource).Error)

	observationA := model.NamespaceObservation{ID: "observation-a", ProviderID: provider.ID, ClusterResourceID: resource.ID, NamespaceUID: "namespace-a", Name: "namespace-a", Revision: 1, ObservedAt: now, LeaseExpiresAt: now.Add(24 * time.Hour), State: model.NamespaceObservationObserved}
	observationB := model.NamespaceObservation{ID: "observation-b", ProviderID: provider.ID, ClusterResourceID: resource.ID, NamespaceUID: "namespace-b", Name: "namespace-b", Revision: 1, ObservedAt: now, LeaseExpiresAt: now.Add(24 * time.Hour), State: model.NamespaceObservationObserved}
	require.NoError(t, DB.Create(&[]model.NamespaceObservation{observationA, observationB}).Error)
	cluster := model.ResourceScope{ID: "scope-cluster", ProviderID: provider.ID, PlatformResourceID: resource.ID, Type: model.ResourceScopeCluster, StableKey: resource.StableKey, LifecycleState: model.ResourceScopeAllocatable, IsolationMode: model.ResourceScopeIsolationNone, ConfigRevision: 1, EvidenceRevision: 1, RowVersion: 1}
	namespaceA := model.ResourceScope{ID: "scope-namespace-a", ProviderID: provider.ID, PlatformResourceID: resource.ID, Type: model.ResourceScopeNamespace, StableKey: "namespace-a", ParentID: &cluster.ID, NamespaceObservationID: &observationA.ID, LifecycleState: model.ResourceScopeAllocatable, IsolationMode: model.ResourceScopeIsolationNamespaceIsolated, ConfigRevision: 1, EvidenceRevision: 1, RowVersion: 1}
	namespaceB := model.ResourceScope{ID: "scope-namespace-b", ProviderID: provider.ID, PlatformResourceID: resource.ID, Type: model.ResourceScopeNamespace, StableKey: "namespace-b", ParentID: &cluster.ID, NamespaceObservationID: &observationB.ID, LifecycleState: model.ResourceScopeAllocatable, IsolationMode: model.ResourceScopeIsolationNamespaceIsolated, ConfigRevision: 1, EvidenceRevision: 1, RowVersion: 1}
	require.NoError(t, DB.Create(&cluster).Error)
	require.NoError(t, DB.Create(&[]model.ResourceScope{namespaceA, namespaceB}).Error)

	return allocationConstraintFixture{user: user, tenantA: tenantA, tenantB: tenantB, cluster: cluster, namespaceA: namespaceA, namespaceB: namespaceB, now: now}
}

func (f allocationConstraintFixture) createDraft(t *testing.T, id string, tenant model.Tenant, scope model.ResourceScope, validFrom, expiresAt time.Time) model.ResourceAllocation {
	t.Helper()
	allocation := model.ResourceAllocation{ID: id, TenantID: tenant.ID, Mode: model.ResourceAllocationLeased, ValidFrom: validFrom, ExpiresAt: &expiresAt, State: model.ResourceAllocationDraft, RowVersion: 1, CreatedByUserID: f.user.ID}
	require.NoError(t, DB.Create(&allocation).Error)
	require.NoError(t, DB.Create(&model.ResourceAllocationItem{ID: id + "-item", AllocationID: id, ScopeID: scope.ID, ScopeRowVersionSnapshot: scope.RowVersion}).Error)
	return allocation
}

func scheduleConstraintAllocation(t *testing.T, allocation model.ResourceAllocation) {
	t.Helper()
	require.NoError(t, DB.Model(&model.ResourceAllocation{}).Where("id = ?", allocation.ID).Updates(map[string]any{
		"state": model.ResourceAllocationScheduled, "row_version": allocation.RowVersion + 1,
	}).Error)
}

func requireS3ConstraintError(t *testing.T, err error, code string) {
	t.Helper()
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), code), "expected %s in %v", code, err)
}

func TestPlatformAllocationConstraintsRejectConflictsAndPreserveBoundaries(t *testing.T) {
	fixture := newAllocationConstraintFixture(t)
	start := fixture.now.Add(time.Hour)
	end := start.Add(time.Hour)

	first := fixture.createDraft(t, "allocation-first", fixture.tenantA, fixture.namespaceA, start, end)
	scheduleConstraintAllocation(t, first)

	sameScope := fixture.createDraft(t, "allocation-same", fixture.tenantB, fixture.namespaceA, start.Add(10*time.Minute), end.Add(time.Hour))
	requireS3ConstraintError(t, DB.Model(&model.ResourceAllocation{}).Where("id = ?", sameScope.ID).Updates(map[string]any{
		"state": model.ResourceAllocationScheduled, "row_version": int64(2),
	}).Error, "S3_ALLOCATION_SCOPE_CONFLICT")

	sibling := fixture.createDraft(t, "allocation-sibling", fixture.tenantB, fixture.namespaceB, start, end)
	scheduleConstraintAllocation(t, sibling)

	adjacent := fixture.createDraft(t, "allocation-adjacent", fixture.tenantB, fixture.namespaceA, end, end.Add(time.Hour))
	scheduleConstraintAllocation(t, adjacent)

	cluster := fixture.createDraft(t, "allocation-cluster", fixture.tenantB, fixture.cluster, start.Add(5*time.Minute), end.Add(5*time.Minute))
	requireS3ConstraintError(t, DB.Model(&model.ResourceAllocation{}).Where("id = ?", cluster.ID).Updates(map[string]any{
		"state": model.ResourceAllocationScheduled, "row_version": int64(2),
	}).Error, "S3_ALLOCATION_HIERARCHY_CONFLICT")
}

func TestPlatformAllocationConstraintsRevokeReleasesScopeAndHistoryCannotBeDeleted(t *testing.T) {
	fixture := newAllocationConstraintFixture(t)
	start := fixture.now.Add(time.Hour)
	end := start.Add(time.Hour)
	first := fixture.createDraft(t, "allocation-first", fixture.tenantA, fixture.namespaceA, start, end)
	scheduleConstraintAllocation(t, first)
	revokedAt := fixture.now.Add(30 * time.Minute)
	require.NoError(t, DB.Model(&model.ResourceAllocation{}).Where("id = ?", first.ID).Updates(map[string]any{
		"state": model.ResourceAllocationRevoked, "row_version": int64(3), "terminated_by_user_id": fixture.user.ID,
		"terminated_at": revokedAt, "termination_reason": "contract cancelled",
	}).Error)

	replacement := fixture.createDraft(t, "allocation-replacement", fixture.tenantB, fixture.namespaceA, start, end)
	scheduleConstraintAllocation(t, replacement)
	requireS3ConstraintError(t, DB.Delete(&replacement).Error, "S3_ALLOCATION_DELETE_FORBIDDEN")
}

func TestPlatformAllocationConstraintsRejectHierarchyInsideOneAggregate(t *testing.T) {
	fixture := newAllocationConstraintFixture(t)
	start := fixture.now.Add(time.Hour)
	end := start.Add(time.Hour)
	allocation := fixture.createDraft(t, "allocation-multi", fixture.tenantA, fixture.cluster, start, end)
	require.NoError(t, DB.Create(&model.ResourceAllocationItem{
		ID: "allocation-multi-namespace", AllocationID: allocation.ID, ScopeID: fixture.namespaceA.ID,
		ScopeRowVersionSnapshot: fixture.namespaceA.RowVersion,
	}).Error)
	requireS3ConstraintError(t, DB.Model(&model.ResourceAllocation{}).Where("id = ?", allocation.ID).Updates(map[string]any{
		"state": model.ResourceAllocationScheduled, "row_version": int64(2),
	}).Error, "S3_ALLOCATION_HIERARCHY_CONFLICT")
}

func TestPlatformAllocationConstraintsSerializeConcurrentReservations(t *testing.T) {
	fixture := newAllocationConstraintFixture(t)
	start := fixture.now.Add(time.Hour)
	end := start.Add(time.Hour)
	left := fixture.createDraft(t, "allocation-left", fixture.tenantA, fixture.namespaceA, start, end)
	right := fixture.createDraft(t, "allocation-right", fixture.tenantB, fixture.namespaceA, start, end)

	startWrites := make(chan struct{})
	results := make(chan error, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for _, allocation := range []model.ResourceAllocation{left, right} {
		allocation := allocation
		go func() {
			ready.Done()
			<-startWrites
			results <- DB.Model(&model.ResourceAllocation{}).Where("id = ?", allocation.ID).Updates(map[string]any{
				"state": model.ResourceAllocationScheduled, "row_version": int64(2),
			}).Error
		}()
	}
	ready.Wait()
	close(startWrites)
	firstErr, secondErr := <-results, <-results
	successes := 0
	for _, err := range []error{firstErr, secondErr} {
		if err == nil {
			successes++
		}
	}
	require.Equal(t, 1, successes, "errors: %v / %v", firstErr, secondErr)
	var occupied int64
	require.NoError(t, DB.Model(&model.ResourceAllocation{}).Where("state = ?", model.ResourceAllocationScheduled).Count(&occupied).Error)
	require.Equal(t, int64(1), occupied)
}
