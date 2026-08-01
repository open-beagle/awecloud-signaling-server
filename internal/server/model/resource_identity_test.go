package model

import (
	"fmt"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newResourceIdentityModelDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared&_pragma=foreign_keys(1)", uuid.NewString())
	database, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(
		&User{},
		&Tenant{},
		&UserIdentityProfile{},
		&UserAuthenticationLink{},
		&PlatformRoleMembership{},
		&ResourceProvider{},
		&AdminProviderMembership{},
		&UserTenantManagementMembership{},
		&UserSimulationSession{},
	))
	return database
}

func seedResourceIdentityModels(t *testing.T, database *gorm.DB) {
	t.Helper()
	users := []User{
		{ID: 1, Name: "legacy-user-1", Role: UserRoleClient, SecretHash: "fixture-hash", Enabled: true},
		{ID: 2, Name: "legacy-user-2", Role: UserRoleClient, SecretHash: "fixture-hash", Enabled: true},
		{ID: 3, Name: "legacy-user-3", Role: UserRoleClient, SecretHash: "fixture-hash", Enabled: true},
	}
	require.NoError(t, database.Create(&users).Error)
	profiles := []UserIdentityProfile{
		{UserID: 1, Username: "identity-1", DisplayName: "Identity 1", Enabled: true, AuthRevision: 1, RowVersion: 1},
		{UserID: 2, Username: "identity-2", DisplayName: "Identity 2", Enabled: true, AuthRevision: 1, RowVersion: 1},
	}
	require.NoError(t, database.Create(&profiles).Error)
	require.NoError(t, database.Create(&Tenant{ID: "tenant-1", Key: "tenant-1", Name: "Tenant 1", Status: TenantStatusActive}).Error)
	require.NoError(t, database.Create(&ResourceProvider{ID: "provider-1", Key: "provider-1", DisplayName: "Provider 1", Status: ProviderStatusActive, Revision: 1, RowVersion: 1}).Error)
}

func TestResourceIdentitySchemaConstraints(t *testing.T) {
	database := newResourceIdentityModelDB(t)
	seedResourceIdentityModels(t, database)
	now := time.Date(2026, 1, 15, 8, 0, 0, 0, time.UTC)

	duplicateUsername := UserIdentityProfile{UserID: 3, Username: "identity-1", DisplayName: "Duplicate", Enabled: true, AuthRevision: 1, RowVersion: 1}
	require.Error(t, database.Create(&duplicateUsername).Error)

	firstLink := UserAuthenticationLink{ID: "auth-1", UserID: 1, ProviderType: AuthenticationProviderOIDC, ProviderSubject: "subject-1", CredentialRevision: 1, Enabled: true, RowVersion: 1}
	require.NoError(t, database.Create(&firstLink).Error)
	duplicateSubject := firstLink
	duplicateSubject.ID, duplicateSubject.UserID = "auth-2", 2
	require.Error(t, database.Create(&duplicateSubject).Error)

	invalidProvider := ResourceProvider{ID: "provider-invalid", Key: "provider-invalid", DisplayName: "Invalid", Status: ProviderStatus("unknown"), Revision: 1, RowVersion: 1}
	require.Error(t, database.Create(&invalidProvider).Error)
	duplicateProviderKey := ResourceProvider{ID: "provider-2", Key: "provider-1", DisplayName: "Duplicate", Status: ProviderStatusActive, Revision: 1, RowVersion: 1}
	require.Error(t, database.Create(&duplicateProviderKey).Error)

	platformRole := PlatformRoleMembership{
		ID: "platform-role-1", UserID: 1, Role: PlatformRoleAdmin, Enabled: true, ValidFrom: now,
		PermissionRevision: 1, CreatedByUserID: 1, Reason: "fixture bootstrap", RowVersion: 1,
	}
	require.NoError(t, database.Create(&platformRole).Error)
	duplicatePlatformRole := platformRole
	duplicatePlatformRole.ID = "platform-role-2"
	require.Error(t, database.Create(&duplicatePlatformRole).Error)

	providerMembership := AdminProviderMembership{
		ID: "provider-membership-1", UserID: 2, ProviderID: "provider-1", Role: ProviderManagementRoleAdmin, Enabled: true,
		ValidFrom: now, PermissionRevision: 1, CreatedByUserID: 1, Reason: "fixture assignment", RowVersion: 1,
	}
	require.NoError(t, database.Create(&providerMembership).Error)
	duplicateProviderMembership := providerMembership
	duplicateProviderMembership.ID = "provider-membership-2"
	require.Error(t, database.Create(&duplicateProviderMembership).Error)

	tenantMembership := UserTenantManagementMembership{
		ID: "tenant-membership-1", UserID: 2, TenantID: "tenant-1", Role: TenantManagementRoleViewer, Enabled: true,
		ValidFrom: now, PermissionRevision: 1, CreatedByUserID: 1, Reason: "fixture assignment", RowVersion: 1,
	}
	require.NoError(t, database.Create(&tenantMembership).Error)
	duplicateTenantMembership := tenantMembership
	duplicateTenantMembership.ID = "tenant-membership-2"
	require.Error(t, database.Create(&duplicateTenantMembership).Error)

	err := database.Exec(`INSERT INTO admin_provider_membership (id, user_id, provider_id, role, enabled, valid_from, expires_at, permission_revision, created_by_user_id, reason, row_version, created_at, updated_at) VALUES (?, ?, ?, ?, 1, ?, ?, 1, ?, ?, 1, ?, ?)`,
		"membership-invalid", 1, "provider-1", ProviderManagementRoleAdmin, now, now, 2, "invalid interval", now, now).Error
	require.Error(t, err)

	err = database.Exec(`INSERT INTO user_simulation_session (id, actor_user_id, effective_user_id, scope_type, scope_id, reason, status, started_at, expires_at, created_request_id, permission_revision, row_version, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, 1, ?, ?)`,
		"simulation-invalid", 1, 2, UserSimulationScopeTenant, "tenant-1", "invalid interval", UserSimulationSessionActive, now, now, "request-1", now, now).Error
	require.Error(t, err)

	endedAt := now.Add(time.Minute)
	err = database.Exec(`INSERT INTO user_simulation_session (id, actor_user_id, effective_user_id, scope_type, scope_id, reason, status, started_at, expires_at, ended_at, created_request_id, permission_revision, row_version, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, 1, ?, ?)`,
		"simulation-active-ended", 1, 2, UserSimulationScopeTenant, "tenant-1", "invalid active end state", UserSimulationSessionActive, now, now.Add(time.Hour), endedAt, "request-2", now, now).Error
	require.Error(t, err)
}

func TestResourceIdentityForeignKeysAndOptimisticVersion(t *testing.T) {
	database := newResourceIdentityModelDB(t)
	seedResourceIdentityModels(t, database)

	missingProfile := UserAuthenticationLink{ID: "auth-missing", UserID: 99, ProviderType: AuthenticationProviderOIDC, ProviderSubject: "missing", CredentialRevision: 1, Enabled: true, RowVersion: 1}
	require.Error(t, database.Create(&missingProfile).Error)

	first := database.Model(&ResourceProvider{}).Where("id = ? AND row_version = ?", "provider-1", 1).Updates(map[string]any{"status": ProviderStatusSuspended, "row_version": gorm.Expr("row_version + 1")})
	require.NoError(t, first.Error)
	require.Equal(t, int64(1), first.RowsAffected)
	stale := database.Model(&ResourceProvider{}).Where("id = ? AND row_version = ?", "provider-1", 1).Updates(map[string]any{"status": ProviderStatusRetired, "row_version": gorm.Expr("row_version + 1")})
	require.NoError(t, stale.Error)
	require.Zero(t, stale.RowsAffected)
}
