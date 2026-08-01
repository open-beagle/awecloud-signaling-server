package db

import (
	"strconv"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func legacyAdminIdentityTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared&_pragma=foreign_keys(1)"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(
		&model.Admin{}, &model.AdminTenantMembership{}, &model.User{}, &model.Tenant{},
		&model.UserIdentityProfile{}, &model.UserAuthenticationLink{}, &model.PlatformRoleMembership{},
		&model.UserTenantManagementMembership{},
	))
	return database
}

func TestSyncLegacyAdminIdentityCreatesExplicitIdempotentGraph(t *testing.T) {
	database := legacyAdminIdentityTestDB(t)
	unrelated := model.User{Name: "desktop-admin", Alias: "Unrelated", Role: model.UserRoleClient, SecretHash: "test", Enabled: true}
	require.NoError(t, database.Create(&unrelated).Error)
	require.NoError(t, database.Create(&model.UserIdentityProfile{
		UserID: unrelated.ID, Username: "admin", DisplayName: "Unrelated Admin", Enabled: true, AuthRevision: 1, RowVersion: 1,
	}).Error)

	admin := model.Admin{Username: "admin", PasswordHash: "test", Role: "admin", Enabled: true}
	tenant := model.Tenant{ID: uuid.NewString(), Key: "tenant-a", Name: "Tenant A", Status: model.TenantStatusActive}
	require.NoError(t, database.Create(&admin).Error)
	require.NoError(t, database.Create(&tenant).Error)
	require.NoError(t, database.Create(&model.AdminTenantMembership{
		AdminID: admin.ID, TenantID: tenant.ID, Role: string(model.TenantManagementRoleAdmin), Enabled: true, PermissionRevision: 3,
	}).Error)

	userID, err := SyncLegacyAdminIdentity(database, admin.ID, "test bootstrap")
	require.NoError(t, err)
	require.NotZero(t, userID)
	require.NotEqual(t, unrelated.ID, userID)

	var link model.UserAuthenticationLink
	require.NoError(t, database.Where("provider_type = ? AND provider_subject = ?",
		model.AuthenticationProviderLegacyAdmin, strconv.FormatInt(admin.ID, 10)).First(&link).Error)
	require.Equal(t, userID, link.UserID)
	require.True(t, link.Enabled)

	var profile model.UserIdentityProfile
	require.NoError(t, database.First(&profile, "user_id = ?", userID).Error)
	require.NotEqual(t, "admin", profile.Username)
	require.Equal(t, "admin", profile.DisplayName)
	var platform model.PlatformRoleMembership
	require.NoError(t, database.First(&platform, "user_id = ?", userID).Error)
	require.Equal(t, model.PlatformRoleAdmin, platform.Role)
	require.True(t, platform.Enabled)
	var tenantMembership model.UserTenantManagementMembership
	require.NoError(t, database.First(&tenantMembership, "user_id = ? AND tenant_id = ?", userID, tenant.ID).Error)
	require.Equal(t, model.TenantManagementRoleAdmin, tenantMembership.Role)
	require.Equal(t, int64(3), tenantMembership.PermissionRevision)

	authRevision := profile.AuthRevision
	credentialRevision := link.CredentialRevision
	platformRevision := platform.PermissionRevision
	tenantRevision := tenantMembership.PermissionRevision
	secondUserID, err := SyncLegacyAdminIdentity(database, admin.ID, "idempotent retry")
	require.NoError(t, err)
	require.Equal(t, userID, secondUserID)
	require.NoError(t, database.First(&profile, "user_id = ?", userID).Error)
	require.NoError(t, database.First(&link, "id = ?", link.ID).Error)
	require.NoError(t, database.First(&platform, "id = ?", platform.ID).Error)
	require.NoError(t, database.First(&tenantMembership, "id = ?", tenantMembership.ID).Error)
	require.Equal(t, authRevision, profile.AuthRevision)
	require.Equal(t, credentialRevision, link.CredentialRevision)
	require.Equal(t, platformRevision, platform.PermissionRevision)
	require.Equal(t, tenantRevision, tenantMembership.PermissionRevision)

	var legacyLinkCount int64
	require.NoError(t, database.Model(&model.UserAuthenticationLink{}).
		Where("provider_type = ? AND provider_subject = ?", model.AuthenticationProviderLegacyAdmin, strconv.FormatInt(admin.ID, 10)).
		Count(&legacyLinkCount).Error)
	require.Equal(t, int64(1), legacyLinkCount)
}

func TestSyncLegacyAdminIdentityInvalidatesStatusAndRoleChanges(t *testing.T) {
	database := legacyAdminIdentityTestDB(t)
	admin := model.Admin{Username: "lifecycle-admin", PasswordHash: "test", Role: "admin", Enabled: true}
	tenant := model.Tenant{ID: uuid.NewString(), Key: "tenant-lifecycle", Name: "Tenant Lifecycle", Status: model.TenantStatusActive}
	require.NoError(t, database.Create(&admin).Error)
	require.NoError(t, database.Create(&tenant).Error)
	legacyTenant := model.AdminTenantMembership{
		AdminID: admin.ID, TenantID: tenant.ID, Role: string(model.TenantManagementRoleAdmin), Enabled: true, PermissionRevision: 1,
	}
	require.NoError(t, database.Create(&legacyTenant).Error)
	userID, err := SyncLegacyAdminIdentity(database, admin.ID, "initial")
	require.NoError(t, err)

	var initialProfile model.UserIdentityProfile
	var initialLink model.UserAuthenticationLink
	var initialPlatform model.PlatformRoleMembership
	var initialTenant model.UserTenantManagementMembership
	require.NoError(t, database.First(&initialProfile, "user_id = ?", userID).Error)
	require.NoError(t, database.First(&initialLink, "user_id = ?", userID).Error)
	require.NoError(t, database.First(&initialPlatform, "user_id = ?", userID).Error)
	require.NoError(t, database.First(&initialTenant, "user_id = ? AND tenant_id = ?", userID, tenant.ID).Error)

	require.NoError(t, database.Model(&admin).Update("role", "viewer").Error)
	require.NoError(t, database.Model(&legacyTenant).Updates(map[string]any{
		"role": string(model.TenantManagementRoleViewer), "permission_revision": gorm.Expr("permission_revision + 1"),
	}).Error)
	require.NoError(t, database.First(&admin, admin.ID).Error)
	_, err = SyncLegacyAdminIdentity(database, admin.ID, "role update")
	require.NoError(t, err)
	var updatedPlatform model.PlatformRoleMembership
	var updatedTenant model.UserTenantManagementMembership
	require.NoError(t, database.First(&updatedPlatform, "user_id = ?", userID).Error)
	require.NoError(t, database.First(&updatedTenant, "user_id = ? AND tenant_id = ?", userID, tenant.ID).Error)
	require.Equal(t, model.PlatformRoleViewer, updatedPlatform.Role)
	require.Greater(t, updatedPlatform.PermissionRevision, initialPlatform.PermissionRevision)
	require.Equal(t, model.TenantManagementRoleViewer, updatedTenant.Role)
	require.Greater(t, updatedTenant.PermissionRevision, initialTenant.PermissionRevision)

	require.NoError(t, database.Model(&admin).Update("enabled", false).Error)
	require.NoError(t, database.First(&admin, admin.ID).Error)
	_, err = SyncLegacyAdminIdentity(database, admin.ID, "disable")
	require.NoError(t, err)
	var disabledUser model.User
	var disabledProfile model.UserIdentityProfile
	var disabledLink model.UserAuthenticationLink
	require.NoError(t, database.First(&disabledUser, userID).Error)
	require.NoError(t, database.First(&disabledProfile, "user_id = ?", userID).Error)
	require.NoError(t, database.First(&disabledLink, "user_id = ?", userID).Error)
	require.NoError(t, database.First(&updatedPlatform, "user_id = ?", userID).Error)
	require.NoError(t, database.First(&updatedTenant, "user_id = ? AND tenant_id = ?", userID, tenant.ID).Error)
	require.False(t, disabledUser.Enabled)
	require.False(t, disabledProfile.Enabled)
	require.False(t, disabledLink.Enabled)
	require.False(t, updatedPlatform.Enabled)
	require.False(t, updatedTenant.Enabled)
	require.Greater(t, disabledProfile.AuthRevision, initialProfile.AuthRevision)
	require.Greater(t, disabledLink.CredentialRevision, initialLink.CredentialRevision)

	disabledAuthRevision := disabledProfile.AuthRevision
	require.NoError(t, database.Model(&admin).Update("enabled", true).Error)
	require.NoError(t, database.First(&admin, admin.ID).Error)
	_, err = SyncLegacyAdminIdentity(database, admin.ID, "enable")
	require.NoError(t, err)
	require.NoError(t, database.First(&disabledProfile, "user_id = ?", userID).Error)
	require.True(t, disabledProfile.Enabled)
	require.Greater(t, disabledProfile.AuthRevision, disabledAuthRevision)
}
