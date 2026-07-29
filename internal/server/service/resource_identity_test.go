package service

import (
	"errors"
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

func TestUserSimulationSessionRecomputesEffectiveContextAndTerminatesOnInvalidation(t *testing.T) {
	database := newResourceIdentityServiceDB(t)
	now := time.Date(2026, 1, 15, 8, 0, 0, 0, time.UTC)
	session := seedTenantSimulationSession(t, database, now, "simulation-resolve")

	resolved, context, err := ResolveUserSimulationSession(database, session.ID, session.ActorUserID, now.Add(time.Minute))
	require.NoError(t, err)
	require.Equal(t, session.ID, resolved.ID)
	require.Equal(t, session.ActorUserID, context.ActorUserID)
	require.Equal(t, session.EffectiveUserID, context.EffectiveUserID)
	require.Equal(t, session.ID, context.SimulationSessionID)
	require.Equal(t, string(model.TenantManagementRoleViewer), context.Role)
	require.Contains(t, context.Permissions, PermissionTenantResourcesRead)
	require.NotContains(t, context.Permissions, PermissionTenantResourcesWrite)

	require.NoError(t, database.Model(&model.UserTenantManagementMembership{}).
		Where("user_id = ? AND tenant_id = ?", session.EffectiveUserID, session.ScopeID).
		Update("enabled", false).Error)
	_, _, err = ResolveUserSimulationSession(database, session.ID, session.ActorUserID, now.Add(2*time.Minute))
	require.ErrorIs(t, err, ErrUserSimulationNotAllowed)
	var ended model.UserSimulationSession
	require.NoError(t, database.First(&ended, "id = ?", session.ID).Error)
	require.Equal(t, model.UserSimulationSessionRevoked, ended.Status)
	require.Equal(t, "effective_context_invalid", ended.EndReason)
	require.NotNil(t, ended.EndedAt)
}

func TestUserSimulationSessionExpiryAndActorInvalidationPersistEndState(t *testing.T) {
	database := newResourceIdentityServiceDB(t)
	now := time.Date(2026, 1, 15, 8, 0, 0, 0, time.UTC)
	expiring := seedTenantSimulationSession(t, database, now, "simulation-expiring")
	_, _, err := ResolveUserSimulationSession(database, expiring.ID, expiring.ActorUserID, expiring.ExpiresAt.Add(time.Second))
	require.ErrorIs(t, err, ErrUserSimulationInactive)
	require.NoError(t, database.First(&expiring, "id = ?", expiring.ID).Error)
	require.Equal(t, model.UserSimulationSessionExpired, expiring.Status)
	require.Equal(t, "expired", expiring.EndReason)

	active := seedTenantSimulationSession(t, database, now.Add(2*time.Hour), "simulation-actor-invalid")
	require.NoError(t, database.Model(&model.PlatformRoleMembership{}).Where("user_id = ?", active.ActorUserID).Update("enabled", false).Error)
	_, _, err = ResolveUserSimulationSession(database, active.ID, active.ActorUserID, active.StartedAt.Add(time.Minute))
	require.ErrorIs(t, err, ErrUserSimulationNotAllowed)
	require.NoError(t, database.First(&active, "id = ?", active.ID).Error)
	require.Equal(t, model.UserSimulationSessionRevoked, active.Status)
	require.Equal(t, "actor_permission_invalid", active.EndReason)
}

func TestUserSimulationSessionTerminatesWhenScopeIsSuspended(t *testing.T) {
	database := newResourceIdentityServiceDB(t)
	now := time.Date(2026, 1, 15, 8, 0, 0, 0, time.UTC)
	session := seedTenantSimulationSession(t, database, now, "simulation-scope-suspended")
	require.NoError(t, database.Model(&model.Tenant{}).Where("id = ?", session.ScopeID).Update("status", model.TenantStatusSuspended).Error)

	_, _, err := ResolveUserSimulationSession(database, session.ID, session.ActorUserID, now.Add(time.Minute))
	require.ErrorIs(t, err, ErrUserSimulationNotAllowed)
	require.NoError(t, database.First(&session, "id = ?", session.ID).Error)
	require.Equal(t, model.UserSimulationSessionRevoked, session.Status)
	require.Equal(t, "effective_context_invalid", session.EndReason)
}

func TestUserSimulationSessionRevocationUsesActorAndRowVersion(t *testing.T) {
	database := newResourceIdentityServiceDB(t)
	now := time.Date(2026, 1, 15, 8, 0, 0, 0, time.UTC)
	session := seedTenantSimulationSession(t, database, now, "simulation-revoke")

	_, err := RevokeUserSimulationSession(database, session.ID, session.ActorUserID, session.RowVersion+1, "operator exit", now.Add(time.Minute))
	require.ErrorIs(t, err, ErrUserSimulationVersion)
	_, err = RevokeUserSimulationSession(database, session.ID, session.EffectiveUserID, session.RowVersion, "not actor", now.Add(time.Minute))
	require.ErrorIs(t, err, ErrUserSimulationNotAllowed)
	revoked, err := RevokeUserSimulationSession(database, session.ID, session.ActorUserID, session.RowVersion, "operator exit", now.Add(time.Minute))
	require.NoError(t, err)
	require.Equal(t, model.UserSimulationSessionRevoked, revoked.Status)
	require.Equal(t, "operator exit", revoked.EndReason)
	require.Equal(t, int64(2), revoked.RowVersion)
}

func seedTenantSimulationSession(t *testing.T, database *gorm.DB, now time.Time, id string) model.UserSimulationSession {
	t.Helper()
	var platformRoleCount int64
	require.NoError(t, database.Model(&model.PlatformRoleMembership{}).Where("user_id = ?", 1).Count(&platformRoleCount).Error)
	if platformRoleCount == 0 {
		require.NoError(t, CreatePlatformRoleMembership(database, &model.PlatformRoleMembership{
			ID: id + "-platform-role", UserID: 1, Role: model.PlatformRoleAdmin, Enabled: true, ValidFrom: now.Add(-time.Hour),
			PermissionRevision: 1, CreatedByUserID: 1, Reason: "fixture bootstrap", RowVersion: 1,
		}))
	} else {
		require.NoError(t, database.Model(&model.PlatformRoleMembership{}).Where("user_id = ?", 1).
			Updates(map[string]any{"enabled": true, "valid_from": now.Add(-time.Hour), "expires_at": nil}).Error)
	}
	var membership model.UserTenantManagementMembership
	err := database.Where("user_id = ? AND tenant_id = ?", 2, "tenant-1").First(&membership).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		require.NoError(t, CreateUserTenantManagementMembership(database, &model.UserTenantManagementMembership{
			ID: id + "-tenant-membership", UserID: 2, TenantID: "tenant-1", Role: model.TenantManagementRoleViewer,
			Enabled: true, ValidFrom: now.Add(-time.Hour), PermissionRevision: 2, CreatedByUserID: 1, Reason: "fixture assignment", RowVersion: 1,
		}))
	} else {
		require.NoError(t, err)
		require.NoError(t, database.Model(&membership).Updates(map[string]any{
			"enabled": true, "valid_from": now.Add(-time.Hour), "expires_at": nil, "role": model.TenantManagementRoleViewer,
		}).Error)
	}
	session := model.UserSimulationSession{
		ID: id, ActorUserID: 1, EffectiveUserID: 2, ScopeType: model.UserSimulationScopeTenant,
		ScopeID: "tenant-1", Reason: "reproduce tenant issue", Status: model.UserSimulationSessionActive,
		StartedAt: now, ExpiresAt: now.Add(time.Hour), CreatedRequestID: id + "-request", PermissionRevision: 2, RowVersion: 1,
	}
	require.NoError(t, CreateUserSimulationSession(database, &session))
	return session
}
