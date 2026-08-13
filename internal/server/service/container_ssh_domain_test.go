package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func TestContainerSSHBusinessDomainUsesWorkloadNamespaceAndRootProvider(t *testing.T) {
	database := containerSSHDomainTestDB(t)
	provider := model.ResourceProvider{
		ID: uuid.NewString(), Key: "beagle", DisplayName: "Beagle", DomainScope: model.ProviderDomainRoot,
		Status: model.ProviderStatusActive, Revision: 1, RowVersion: 1,
	}
	require.NoError(t, database.Create(&provider).Error)
	agent := model.TechnicalResource{
		ID: uuid.NewString(), ProviderID: provider.ID, Type: model.TechnicalResourceAgent,
		StableKey: "beijing-agent", DomainLabel: "beijing", LifecycleState: model.TechnicalResourceRegistered,
		HealthState: model.ResourceHealthOnline, CredentialRevision: 1, ConfigRevision: 1, RowVersion: 1,
	}
	require.NoError(t, database.Create(&agent).Error)

	domain, err := ContainerSSHBusinessDomain(context.Background(), database, agent.ID,
		`{"namespace_name":"beagle-ide","workload_name":"ide-public"}`)
	require.NoError(t, err)
	require.Equal(t, "ide-public.beagle-ide.beijing.beagle", domain)
}

func TestContainerServiceBusinessDomainUsesServiceNamespaceAndAgent(t *testing.T) {
	database := containerSSHDomainTestDB(t)
	provider := model.ResourceProvider{
		ID: uuid.NewString(), Key: "beagle", DisplayName: "Beagle", DomainScope: model.ProviderDomainRoot,
		Status: model.ProviderStatusActive, Revision: 1, RowVersion: 1,
	}
	require.NoError(t, database.Create(&provider).Error)
	agent := model.TechnicalResource{
		ID: uuid.NewString(), ProviderID: provider.ID, Type: model.TechnicalResourceAgent,
		StableKey: "beijing-agent", DomainLabel: "beijing", LifecycleState: model.TechnicalResourceRegistered,
		HealthState: model.ResourceHealthOnline, CredentialRevision: 1, ConfigRevision: 1, RowVersion: 1,
	}
	require.NoError(t, database.Create(&agent).Error)

	domain, err := ContainerServiceBusinessDomain(context.Background(), database, agent.ID,
		`{"namespace_name":"bookinfo","service_name":"reviews"}`)
	require.NoError(t, err)
	require.Equal(t, "reviews.bookinfo.beijing.beagle", domain)
}

func TestContainerSSHBusinessDomainDoesNotExposeNamedProvider(t *testing.T) {
	database := containerSSHDomainTestDB(t)
	provider := model.ResourceProvider{
		ID: uuid.NewString(), Key: "szzy", DisplayName: "SZYY", DomainScope: model.ProviderDomainNamed, DomainLabel: "szzy",
		Status: model.ProviderStatusActive, Revision: 1, RowVersion: 1,
	}
	require.NoError(t, database.Create(&provider).Error)
	agent := model.TechnicalResource{
		ID: uuid.NewString(), ProviderID: provider.ID, Type: model.TechnicalResourceAgent,
		StableKey: "guoziyun-agent", DomainLabel: "guoziyun", LifecycleState: model.TechnicalResourceRegistered,
		HealthState: model.ResourceHealthOnline, CredentialRevision: 1, ConfigRevision: 1, RowVersion: 1,
	}
	require.NoError(t, database.Create(&agent).Error)

	domain, err := ContainerSSHBusinessDomain(context.Background(), database, agent.ID,
		`{"namespace_name":"tenant-workloads","workload_name":"ide"}`)
	require.NoError(t, err)
	require.Equal(t, "ide.tenant-workloads.guoziyun.beagle", domain)
}

func TestContainerSSHBusinessDomainRejectsRuntimePodNameWithoutWorkload(t *testing.T) {
	database := containerSSHDomainTestDB(t)
	_, err := ContainerSSHBusinessDomain(context.Background(), database, uuid.NewString(),
		`{"namespace_name":"beagle-ide","pod_name":"ide-public-6ffbc8766b-6nrrz"}`)
	require.ErrorIs(t, err, ErrContainerSSHBusinessDomainInvalid)
}

func TestContainerSSHBusinessDomainConflictRejectsDifferentStableResource(t *testing.T) {
	database := containerSSHDomainTestDB(t)
	now := time.Now().UTC()
	provider := model.ResourceProvider{
		ID: uuid.NewString(), Key: "beagle", DisplayName: "Beagle", DomainScope: model.ProviderDomainRoot,
		Status: model.ProviderStatusActive, Revision: 1, RowVersion: 1,
	}
	require.NoError(t, database.Create(&provider).Error)
	agent := model.TechnicalResource{
		ID: uuid.NewString(), ProviderID: provider.ID, Type: model.TechnicalResourceAgent,
		StableKey: "beijing-agent", DomainLabel: "beijing", LifecycleState: model.TechnicalResourceRegistered,
		HealthState: model.ResourceHealthOnline, CredentialRevision: 1, ConfigRevision: 1, RowVersion: 1,
	}
	require.NoError(t, database.Create(&agent).Error)
	tenant := model.Tenant{ID: uuid.NewString(), Key: "beagle", Name: "Beagle", Status: model.TenantStatusActive, Revision: 1, RowVersion: 1}
	require.NoError(t, database.Create(&tenant).Error)
	allocation := model.ResourceAllocation{
		ID: uuid.NewString(), TenantID: tenant.ID, Mode: model.ResourceAllocationAssigned,
		ValidFrom: now.Add(-time.Minute), State: model.ResourceAllocationActive, RowVersion: 1, CreatedByUserID: 1,
	}
	require.NoError(t, database.Create(&allocation).Error)
	platform := model.PlatformResource{
		ID: uuid.NewString(), ProviderID: provider.ID, Type: model.SupplyResourceKubernetes,
		StableKey: strings.Repeat("a", 64), DisplayName: "Beijing Kubernetes",
		LifecycleState: model.PlatformResourceActive, HealthState: model.ResourceHealthOnline, CapabilityRevision: 1, RowVersion: 1,
	}
	require.NoError(t, database.Create(&platform).Error)
	scope := model.ResourceScope{
		ID: uuid.NewString(), ProviderID: provider.ID, PlatformResourceID: platform.ID,
		Type: model.ResourceScopeCluster, StableKey: strings.Repeat("b", 64),
		LifecycleState: model.ResourceScopeActive, IsolationMode: model.ResourceScopeIsolationNone,
		ConfigRevision: 1, EvidenceRevision: 1, RowVersion: 1,
	}
	require.NoError(t, database.Create(&scope).Error)
	item := model.ResourceAllocationItem{ID: uuid.NewString(), AllocationID: allocation.ID, ScopeID: scope.ID, ScopeRowVersionSnapshot: 1}
	require.NoError(t, database.Create(&item).Error)
	observation := model.WorkloadObservation{
		ID: uuid.NewString(), NamespaceScopeID: scope.ID, Kind: model.WorkloadObservationContainer,
		StableKey: strings.Repeat("c", 64), IdentityQuality: model.WorkloadIdentityStrong,
		State: model.WorkloadObservationEligible, Ready: true, ObservedRevision: 1, LabelSnapshot: `{}`,
		FirstObservedAt: now, LastObservedAt: now, LeaseExpiresAt: now.Add(time.Hour), RowVersion: 1,
	}
	require.NoError(t, database.Create(&observation).Error)
	targetSnapshot := `{"namespace_name":"beagle-ide","workload_name":"ide-public","pod_uid":"pod-a","pod_name":"ide-public-a","container_name":"ide"}`
	visible := model.TenantResource{
		ID: uuid.NewString(), TenantID: tenant.ID, Type: model.TenantResourceContainerSSH,
		StableKey: strings.Repeat("d", 64), EntitlementLineageID: allocation.ID, DisplayName: "ide-public/ide",
		VisibilityState: model.TenantResourceVisible, AvailabilityState: model.TenantResourceAvailable, Revision: 1, RowVersion: 1,
	}
	require.NoError(t, database.Create(&visible).Error)
	source := model.TenantResourceSource{
		ID: uuid.NewString(), TenantResourceID: visible.ID, AllocationItemID: item.ID, WorkloadObservationID: observation.ID,
		Enabled: true, EnabledAt: now, SourceRevision: 1, RowVersion: 1,
	}
	require.NoError(t, database.Create(&source).Error)
	target := model.TenantResourceTargetRevision{
		ID: uuid.NewString(), TenantResourceSourceID: source.ID, Revision: 1,
		TargetType: model.WorkloadObservationContainer, TargetSnapshot: targetSnapshot,
		SourceTechnicalResourceID: agent.ID, AccessTechnicalResourceID: agent.ID,
		Ready: true, ObservedAt: now, ObservationRevision: 1, SourceRevision: 1,
	}
	require.NoError(t, database.Create(&target).Error)

	candidate := &tenantResourceChain{
		Resource: model.TenantResource{ID: uuid.NewString(), Type: model.TenantResourceContainerSSH, StableKey: strings.Repeat("e", 64)},
		Target:   model.TenantResourceTargetRevision{AccessTechnicalResourceID: agent.ID, TargetSnapshot: targetSnapshot},
	}
	require.ErrorIs(t, ensureContainerSSHBusinessDomainUnique(context.Background(), database, candidate, now), ErrContainerSSHBusinessDomainConflict)
	candidate.Resource.StableKey = visible.StableKey
	require.NoError(t, ensureContainerSSHBusinessDomainUnique(context.Background(), database, candidate, now))
}

func containerSSHDomainTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(
		&model.ResourceProvider{}, &model.TechnicalResource{}, &model.SystemConfig{}, &model.Tenant{},
		&model.PlatformResource{}, &model.ResourceScope{}, &model.ResourceAllocation{}, &model.ResourceAllocationItem{},
		&model.WorkloadObservation{}, &model.TenantResource{}, &model.TenantResourceSource{}, &model.TenantResourceTargetRevision{},
	))
	return database
}
