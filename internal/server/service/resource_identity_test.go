package service

import (
	"fmt"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func newResourceIdentityServiceDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", uuid.NewString())), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(
		&model.User{}, &model.Tenant{}, &model.TenantMembership{}, &model.UserIdentityProfile{}, &model.UserAuthenticationLink{},
		&model.PlatformRoleMembership{}, &model.ResourceProvider{}, &model.AdminProviderMembership{},
		&model.UserTenantManagementMembership{}, &model.UserSimulationSession{},
	))
	users := []model.User{
		{ID: 1, Name: "legacy-user-1", Role: model.UserRoleClient, SecretHash: "fixture-hash", Enabled: true},
		{ID: 2, Name: "legacy-user-2", Role: model.UserRoleClient, SecretHash: "fixture-hash", Enabled: true},
	}
	require.NoError(t, database.Create(&users).Error)
	for _, profile := range []model.UserIdentityProfile{
		{UserID: 1, Username: "identity-1", DisplayName: "Identity 1", Enabled: true, AuthRevision: 1, RowVersion: 1},
		{UserID: 2, Username: "identity-2", DisplayName: "Identity 2", Enabled: true, AuthRevision: 1, RowVersion: 1},
	} {
		profile := profile
		require.NoError(t, CreateUserIdentityProfile(database, &profile))
	}
	require.NoError(t, database.Create(&model.Tenant{ID: "tenant-1", Key: "tenant-1", Name: "Tenant 1", Status: model.TenantStatusActive}).Error)
	return database
}

func TestResourceIdentityCreationValidatesReferences(t *testing.T) {
	database := newResourceIdentityServiceDB(t)
	now := time.Date(2026, 1, 15, 8, 0, 0, 0, time.UTC)
	provider := model.ResourceProvider{ID: "provider-1", Key: "provider-1", DisplayName: "Provider 1", Status: model.ProviderStatusActive, Revision: 1, RowVersion: 1}
	require.NoError(t, CreateResourceProvider(database, &provider))

	providerMembership := model.AdminProviderMembership{
		ID: "provider-membership-1", UserID: 2, ProviderID: provider.ID, Role: model.ProviderManagementRoleAdmin,
		Enabled: true, ValidFrom: now, PermissionRevision: 1, CreatedByUserID: 1, Reason: "fixture assignment", RowVersion: 1,
	}
	require.NoError(t, CreateAdminProviderMembership(database, &providerMembership))
	providerMembership.ID, providerMembership.ProviderID = "provider-membership-missing", "missing"
	require.ErrorIs(t, CreateAdminProviderMembership(database, &providerMembership), ErrResourceIdentityReference)

	tenantMembership := model.UserTenantManagementMembership{
		ID: "tenant-membership-1", UserID: 2, TenantID: "tenant-1", Role: model.TenantManagementRoleViewer,
		Enabled: true, ValidFrom: now, PermissionRevision: 1, CreatedByUserID: 1, Reason: "fixture assignment", RowVersion: 1,
	}
	require.NoError(t, CreateUserTenantManagementMembership(database, &tenantMembership))
	expired := now.Add(-time.Minute)
	tenantMembership.ID, tenantMembership.ExpiresAt = "tenant-membership-invalid", &expired
	require.ErrorIs(t, CreateUserTenantManagementMembership(database, &tenantMembership), ErrResourceIdentityInvalid)
}

func TestUserSimulationSessionRequiresRealActorAndEffectiveMembership(t *testing.T) {
	database := newResourceIdentityServiceDB(t)
	now := time.Date(2026, 1, 15, 8, 0, 0, 0, time.UTC)
	actorRole := model.PlatformRoleMembership{
		ID: "platform-role-1", UserID: 1, Role: model.PlatformRoleAdmin, Enabled: true, ValidFrom: now.Add(-time.Hour),
		PermissionRevision: 1, CreatedByUserID: 1, Reason: "fixture bootstrap", RowVersion: 1,
	}
	require.NoError(t, CreatePlatformRoleMembership(database, &actorRole))
	tenantMembership := model.UserTenantManagementMembership{
		ID: "tenant-membership-1", UserID: 2, TenantID: "tenant-1", Role: model.TenantManagementRoleAdmin,
		Enabled: true, ValidFrom: now.Add(-time.Hour), PermissionRevision: 3, CreatedByUserID: 1, Reason: "fixture assignment", RowVersion: 1,
	}
	require.NoError(t, CreateUserTenantManagementMembership(database, &tenantMembership))
	session := model.UserSimulationSession{
		ID: "simulation-1", ActorUserID: 1, EffectiveUserID: 2, ScopeType: model.UserSimulationScopeTenant,
		ScopeID: "tenant-1", Reason: "reproduce tenant issue", Status: model.UserSimulationSessionActive,
		StartedAt: now, ExpiresAt: now.Add(time.Hour), CreatedRequestID: "request-1", PermissionRevision: 3, RowVersion: 1,
	}
	require.NoError(t, CreateUserSimulationSession(database, &session))

	withoutTargetMembership := session
	withoutTargetMembership.ID, withoutTargetMembership.EffectiveUserID = "simulation-no-target-membership", 1
	require.ErrorIs(t, CreateUserSimulationSession(database, &withoutTargetMembership), ErrUserSimulationNotAllowed)

	require.NoError(t, database.Model(&model.PlatformRoleMembership{}).Where("id = ?", actorRole.ID).Update("enabled", false).Error)
	withoutPlatformAdmin := session
	withoutPlatformAdmin.ID = "simulation-no-platform-admin"
	require.ErrorIs(t, CreateUserSimulationSession(database, &withoutPlatformAdmin), ErrUserSimulationNotAllowed)
}

func TestUserSimulationSessionSupportsProviderAndTenantMemberScopes(t *testing.T) {
	database := newResourceIdentityServiceDB(t)
	now := time.Date(2026, 1, 15, 8, 0, 0, 0, time.UTC)
	actorRole := model.PlatformRoleMembership{
		ID: "platform-role-1", UserID: 1, Role: model.PlatformRoleAdmin, Enabled: true, ValidFrom: now.Add(-time.Hour),
		PermissionRevision: 1, CreatedByUserID: 1, Reason: "fixture bootstrap", RowVersion: 1,
	}
	require.NoError(t, CreatePlatformRoleMembership(database, &actorRole))
	provider := model.ResourceProvider{ID: "provider-1", Key: "provider-1", DisplayName: "Provider 1", Status: model.ProviderStatusActive, Revision: 1, RowVersion: 1}
	require.NoError(t, CreateResourceProvider(database, &provider))
	providerMembership := model.AdminProviderMembership{
		ID: "provider-membership-1", UserID: 2, ProviderID: provider.ID, Role: model.ProviderManagementRoleOperator,
		Enabled: true, ValidFrom: now.Add(-time.Hour), PermissionRevision: 2, CreatedByUserID: 1, Reason: "fixture assignment", RowVersion: 1,
	}
	require.NoError(t, CreateAdminProviderMembership(database, &providerMembership))
	require.NoError(t, CreateUserSimulationSession(database, &model.UserSimulationSession{
		ID: "simulation-provider", ActorUserID: 1, EffectiveUserID: 2, ScopeType: model.UserSimulationScopeProvider,
		ScopeID: provider.ID, Reason: "reproduce provider issue", Status: model.UserSimulationSessionActive,
		StartedAt: now, ExpiresAt: now.Add(time.Hour), CreatedRequestID: "request-provider", PermissionRevision: 2, RowVersion: 1,
	}))

	require.NoError(t, database.Create(&model.TenantMembership{TenantID: "tenant-1", UserID: 2, Role: "member", Enabled: true}).Error)
	require.NoError(t, CreateUserSimulationSession(database, &model.UserSimulationSession{
		ID: "simulation-tenant-member", ActorUserID: 1, EffectiveUserID: 2, ScopeType: model.UserSimulationScopeTenant,
		ScopeID: "tenant-1", Reason: "reproduce member issue", Status: model.UserSimulationSessionActive,
		StartedAt: now, ExpiresAt: now.Add(time.Hour), CreatedRequestID: "request-tenant", PermissionRevision: 1, RowVersion: 1,
	}))
}
