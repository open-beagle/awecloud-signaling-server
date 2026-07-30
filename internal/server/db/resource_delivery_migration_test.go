package db

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func TestInitDBRegistersResourceDeliveryTables(t *testing.T) {
	original := DB
	t.Cleanup(func() { DB = original })

	require.NoError(t, InitDB(config.DatabaseSection{Type: "sqlite", Path: filepath.Join(t.TempDir(), "signal.db")}))
	t.Cleanup(func() {
		sqlDB, err := DB.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	for _, table := range []any{
		&model.MigrationBatch{},
		&model.MigrationSourceMapping{},
		&model.APIIdempotencyRecord{},
		&model.OutboxEvent{},
		&model.ConsumerRevision{},
		&model.UserIdentityProfile{},
		&model.UserAuthenticationLink{},
		&model.PlatformRoleMembership{},
		&model.ResourceProvider{},
		&model.AdminProviderMembership{},
		&model.UserTenantManagementMembership{},
		&model.UserSimulationSession{},
		&model.TechnicalResource{},
		&model.TechnicalResourceBinding{},
		&model.SupplyInventoryReceipt{},
		&model.SupplyCandidate{},
		&model.PlatformResource{},
		&model.PlatformResourceSource{},
		&model.NamespaceObservation{},
		&model.ResourceScope{},
	} {
		require.True(t, DB.Migrator().HasTable(table))
	}
}

func TestInitDBAddsM1ATablesWithoutChangingLegacyRows(t *testing.T) {
	original := DB
	t.Cleanup(func() { DB = original })
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacy, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, legacy.AutoMigrate(&model.Admin{}, &model.User{}, &model.Tenant{}, &model.AdminTenantMembership{}))
	require.NoError(t, legacy.Create(&model.Admin{ID: 11, Username: "legacy-admin", PasswordHash: "legacy-password-hash", Role: "viewer", Enabled: true}).Error)
	require.NoError(t, legacy.Create(&model.User{ID: 22, Name: "legacy-user", Role: model.UserRoleClient, SecretHash: "legacy-secret-hash", Enabled: true}).Error)
	require.NoError(t, legacy.Create(&model.Tenant{ID: "legacy-tenant", Key: "legacy-tenant", Name: "Legacy Tenant", Status: model.TenantStatusActive}).Error)
	require.NoError(t, legacy.Create(&model.AdminTenantMembership{ID: 33, AdminID: 11, TenantID: "legacy-tenant", Role: "tenant_viewer", Enabled: true, PermissionRevision: 1}).Error)
	sqlDB, err := legacy.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	require.NoError(t, InitDB(config.DatabaseSection{Type: "sqlite", Path: path}))
	t.Cleanup(func() {
		if current, dbErr := DB.DB(); dbErr == nil {
			_ = current.Close()
		}
	})

	var admin model.Admin
	require.NoError(t, DB.First(&admin, 11).Error)
	require.Equal(t, "legacy-password-hash", admin.PasswordHash)
	var user model.User
	require.NoError(t, DB.First(&user, 22).Error)
	require.Equal(t, "legacy-secret-hash", user.SecretHash)
	var membership model.AdminTenantMembership
	require.NoError(t, DB.First(&membership, 33).Error)
	require.Equal(t, int64(11), membership.AdminID)
	for _, table := range []any{
		&model.UserIdentityProfile{}, &model.UserAuthenticationLink{}, &model.PlatformRoleMembership{},
		&model.ResourceProvider{}, &model.AdminProviderMembership{}, &model.UserTenantManagementMembership{}, &model.UserSimulationSession{},
		&model.TechnicalResource{}, &model.TechnicalResourceBinding{}, &model.SupplyInventoryReceipt{}, &model.SupplyCandidate{},
		&model.PlatformResource{}, &model.PlatformResourceSource{}, &model.NamespaceObservation{}, &model.ResourceScope{},
	} {
		require.True(t, DB.Migrator().HasTable(table))
	}
	var profileCount int64
	require.NoError(t, DB.Model(&model.UserIdentityProfile{}).Count(&profileCount).Error)
	require.Zero(t, profileCount)
	var technicalResourceCount int64
	require.NoError(t, DB.Model(&model.TechnicalResource{}).Count(&technicalResourceCount).Error)
	require.Zero(t, technicalResourceCount)
}

func TestInitDBEnforcesS2RelationshipsWithoutGlobalForeignKeys(t *testing.T) {
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
	require.NoError(t, ensureProviderSupplyConstraints(DB))
	var triggerCount int64
	require.NoError(t, DB.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger' AND name LIKE 'trg_s2_%'").Scan(&triggerCount).Error)
	require.Equal(t, int64(len(providerSupplyTriggers)), triggerCount)

	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	require.NoError(t, DB.Create(&model.User{ID: 1, Name: "provider-admin", Role: model.UserRoleClient, SecretHash: "fixture-hash", Enabled: true}).Error)
	require.NoError(t, DB.Create(&model.Node{ID: 1001, UserID: 1, Name: "fixture-agent", Type: model.NodeTypeAgent}).Error)
	require.NoError(t, DB.Create(&[]model.ResourceProvider{
		{ID: "provider-a", Key: "provider-a", DisplayName: "Provider A", Status: model.ProviderStatusActive, Revision: 1, RowVersion: 1},
		{ID: "provider-b", Key: "provider-b", DisplayName: "Provider B", Status: model.ProviderStatusActive, Revision: 1, RowVersion: 1},
	}).Error)
	require.NoError(t, DB.Create(&[]model.TechnicalResource{
		{ID: "agent-a", ProviderID: "provider-a", Type: model.TechnicalResourceAgent, StableKey: "agent-a", LifecycleState: model.TechnicalResourceRegistered, HealthState: model.ResourceHealthOnline, CredentialRevision: 1, ConfigRevision: 1, RowVersion: 1},
		{ID: "agent-b", ProviderID: "provider-b", Type: model.TechnicalResourceAgent, StableKey: "agent-b", LifecycleState: model.TechnicalResourceRegistered, HealthState: model.ResourceHealthOnline, CredentialRevision: 1, ConfigRevision: 1, RowVersion: 1},
	}).Error)

	crossProviderParent := model.TechnicalResource{ID: "endpoint-b", ProviderID: "provider-b", Type: model.TechnicalResourceEndpoint, StableKey: "endpoint-b", ParentID: stringPointer("agent-a"), LifecycleState: model.TechnicalResourceRegistered, HealthState: model.ResourceHealthOnline, CredentialRevision: 1, ConfigRevision: 1, RowVersion: 1}
	requireS2ConstraintError(t, DB.Create(&crossProviderParent).Error, "S2_TECHNICAL_RESOURCE_PARENT_MISMATCH")

	missingUserBinding := model.TechnicalResourceBinding{ID: "binding-a", TechnicalResourceID: "agent-a", SourceType: model.TechnicalResourceBindingLegacyNode, SourceID: "1001", CredentialRevision: 1, Enabled: true, BoundByUserID: 99, Reason: "anonymous fixture", RowVersion: 1}
	requireS2ConstraintError(t, DB.Create(&missingUserBinding).Error, "S2_BINDING_USER_NOT_FOUND")
	missingSourceBinding := missingUserBinding
	missingSourceBinding.ID, missingSourceBinding.SourceID, missingSourceBinding.BoundByUserID = "binding-missing-source", "9999", 1
	requireS2ConstraintError(t, DB.Create(&missingSourceBinding).Error, "S2_LEGACY_BINDING_SOURCE_NOT_FOUND")
	staleCredentialBinding := missingUserBinding
	staleCredentialBinding.ID, staleCredentialBinding.BoundByUserID, staleCredentialBinding.CredentialRevision = "binding-stale-credential", 1, 2
	requireS2ConstraintError(t, DB.Create(&staleCredentialBinding).Error, "S2_BINDING_CREDENTIAL_REVISION_MISMATCH")
	validBinding := missingUserBinding
	validBinding.ID, validBinding.BoundByUserID = "binding-valid", 1
	require.NoError(t, DB.Create(&validBinding).Error)
	wrongSourceTypeBinding := validBinding
	wrongSourceTypeBinding.ID, wrongSourceTypeBinding.TechnicalResourceID, wrongSourceTypeBinding.SourceType, wrongSourceTypeBinding.SourceID = "binding-wrong-source-type", "agent-b", model.TechnicalResourceBindingLegacyEndpoint, "legacy-endpoint-b"
	requireS2ConstraintError(t, DB.Create(&wrongSourceTypeBinding).Error, "S2_BINDING_SOURCE_TYPE_MISMATCH")
	require.NoError(t, DB.Create(&model.Endpoint{ID: "legacy-endpoint-b", UserID: 1, Name: "fixture-endpoint", SSHUsers: "[]"}).Error)
	endpointB := model.TechnicalResource{ID: "endpoint-b-valid-parent", ProviderID: "provider-b", Type: model.TechnicalResourceEndpoint, StableKey: "endpoint-b-valid-parent", ParentID: stringPointer("agent-b"), LifecycleState: model.TechnicalResourceRegistered, HealthState: model.ResourceHealthOnline, CredentialRevision: 1, ConfigRevision: 1, RowVersion: 1}
	require.NoError(t, DB.Create(&endpointB).Error)
	unboundParentBinding := model.TechnicalResourceBinding{ID: "binding-endpoint-b", TechnicalResourceID: endpointB.ID, SourceType: model.TechnicalResourceBindingLegacyEndpoint, SourceID: "legacy-endpoint-b", CredentialRevision: 1, Enabled: true, BoundByUserID: 1, Reason: "anonymous fixture", RowVersion: 1}
	requireS2ConstraintError(t, DB.Create(&unboundParentBinding).Error, "S2_ENDPOINT_PARENT_UNBOUND")

	missingSourceReceipt := model.SupplyInventoryReceipt{ID: "receipt-a", TechnicalResourceID: "missing-agent", SourceEpoch: "epoch-a", Sequence: 1, SchemaVersion: 1, SnapshotID: "snapshot-a", BatchIndex: 0, BatchCount: 1, PayloadHash: strings.Repeat("a", 64), ReceivedAt: now, Status: model.SupplyInventoryReceiptStaging, ResultCode: "BATCH_STAGED"}
	requireS2ConstraintError(t, DB.Create(&missingSourceReceipt).Error, "S2_TECHNICAL_RESOURCE_NOT_FOUND")

	candidateA := model.SupplyCandidate{ID: "candidate-a", ProviderID: "provider-a", TechnicalResourceID: "agent-a", ResourceType: model.SupplyResourceKubernetes, StableKey: "cluster-a", IdentityQuality: model.SupplyIdentityStrong, PayloadHash: strings.Repeat("b", 64), FirstObservedAt: now, LastObservedAt: now, LeaseExpiresAt: now.Add(time.Minute), ReviewState: model.SupplyCandidateAccepted, RowVersion: 1}
	require.NoError(t, DB.Create(&candidateA).Error)
	crossProviderCandidate := candidateA
	crossProviderCandidate.ID, crossProviderCandidate.ProviderID, crossProviderCandidate.StableKey = "candidate-cross", "provider-b", "cluster-cross"
	requireS2ConstraintError(t, DB.Create(&crossProviderCandidate).Error, "S2_CANDIDATE_SOURCE_PROVIDER_MISMATCH")
	requireS2ConstraintError(t, DB.Model(&candidateA).Update("provider_id", "provider-b").Error, "S2_CANDIDATE_SOURCE_PROVIDER_MISMATCH")

	candidateB := candidateA
	candidateB.ID, candidateB.ProviderID, candidateB.TechnicalResourceID = "candidate-b", "provider-b", "agent-b"
	require.NoError(t, DB.Create(&candidateB).Error)
	resources := []model.PlatformResource{
		{ID: "resource-a", ProviderID: "provider-a", Type: model.SupplyResourceKubernetes, StableKey: "cluster-a", DisplayName: "Cluster A", LifecycleState: model.PlatformResourceDraft, HealthState: model.ResourceHealthOnline, CapabilityRevision: 1, RowVersion: 1},
		{ID: "resource-a-2", ProviderID: "provider-a", Type: model.SupplyResourceKubernetes, StableKey: "cluster-a-2", DisplayName: "Cluster A2", LifecycleState: model.PlatformResourceDraft, HealthState: model.ResourceHealthOnline, CapabilityRevision: 1, RowVersion: 1},
	}
	require.NoError(t, DB.Create(&resources).Error)
	crossProviderSource := model.PlatformResourceSource{ID: "source-cross", ProviderID: "provider-a", PlatformResourceID: "resource-a", SupplyCandidateID: "candidate-b", LinkedAt: now, LastConfirmedAt: now}
	requireS2ConstraintError(t, DB.Create(&crossProviderSource).Error, "S2_RESOURCE_SOURCE_CANDIDATE_MISMATCH")

	crossResourceObservation := model.NamespaceObservation{ID: "observation-cross", ProviderID: "provider-b", ClusterResourceID: "resource-a", NamespaceUID: "namespace-cross", Name: "namespace-cross", Revision: 1, ObservedAt: now, LeaseExpiresAt: now.Add(time.Minute), State: model.NamespaceObservationObserved}
	requireS2ConstraintError(t, DB.Create(&crossResourceObservation).Error, "S2_NAMESPACE_CLUSTER_PROVIDER_MISMATCH")
	observationA := model.NamespaceObservation{ID: "observation-a", ProviderID: "provider-a", ClusterResourceID: "resource-a", NamespaceUID: "namespace-a", Name: "namespace-a", Revision: 1, ObservedAt: now, LeaseExpiresAt: now.Add(time.Minute), State: model.NamespaceObservationObserved}
	observationA2 := model.NamespaceObservation{ID: "observation-a-2", ProviderID: "provider-a", ClusterResourceID: "resource-a-2", NamespaceUID: "namespace-a-2", Name: "namespace-a-2", Revision: 1, ObservedAt: now, LeaseExpiresAt: now.Add(time.Minute), State: model.NamespaceObservationObserved}
	require.NoError(t, DB.Create(&[]model.NamespaceObservation{observationA, observationA2}).Error)
	clusterScopeA := model.ResourceScope{ID: "scope-cluster-a", ProviderID: "provider-a", PlatformResourceID: "resource-a", Type: model.ResourceScopeCluster, StableKey: "cluster-a", LifecycleState: model.ResourceScopeDraft, IsolationMode: model.ResourceScopeIsolationNone, ConfigRevision: 1, EvidenceRevision: 1, RowVersion: 1}
	clusterScopeA2 := model.ResourceScope{ID: "scope-cluster-a-2", ProviderID: "provider-a", PlatformResourceID: "resource-a-2", Type: model.ResourceScopeCluster, StableKey: "cluster-a-2", LifecycleState: model.ResourceScopeDraft, IsolationMode: model.ResourceScopeIsolationNone, ConfigRevision: 1, EvidenceRevision: 1, RowVersion: 1}
	require.NoError(t, DB.Create(&[]model.ResourceScope{clusterScopeA, clusterScopeA2}).Error)
	crossResourceParent := model.ResourceScope{ID: "scope-cross-parent", ProviderID: "provider-a", PlatformResourceID: "resource-a-2", Type: model.ResourceScopeNamespace, StableKey: "namespace-cross-parent", ParentID: &clusterScopeA.ID, NamespaceObservationID: &observationA2.ID, LifecycleState: model.ResourceScopeDraft, IsolationMode: model.ResourceScopeIsolationNamespaceIsolated, ConfigRevision: 1, EvidenceRevision: 1, RowVersion: 1}
	requireS2ConstraintError(t, DB.Create(&crossResourceParent).Error, "S2_SCOPE_PARENT_MISMATCH")
	crossResourceScopeObservation := crossResourceParent
	crossResourceScopeObservation.ID, crossResourceScopeObservation.StableKey, crossResourceScopeObservation.ParentID, crossResourceScopeObservation.NamespaceObservationID = "scope-cross-observation", "namespace-cross-observation", &clusterScopeA2.ID, &observationA.ID
	requireS2ConstraintError(t, DB.Create(&crossResourceScopeObservation).Error, "S2_SCOPE_OBSERVATION_MISMATCH")
}

func requireS2ConstraintError(t *testing.T, err error, code string) {
	t.Helper()
	require.Error(t, err)
	require.Contains(t, err.Error(), code)
}

func stringPointer(value string) *string {
	return &value
}
