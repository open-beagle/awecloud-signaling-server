package db

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func TestEnsureBeagleWorkspaceMigratesOnlyUnscopedDesktopUsers(t *testing.T) {
	oldDB := DB
	t.Cleanup(func() { DB = oldDB })

	database, err := gorm.Open(sqlite.Open("file:beagle_workspace_test?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	DB = database
	require.NoError(t, database.AutoMigrate(
		&model.Admin{}, &model.AdminTenantMembership{}, &model.User{},
		&model.Tenant{}, &model.TenantMembership{}, &model.UserIdentityProfile{},
		&model.UserAuthenticationLink{}, &model.PlatformRoleMembership{}, &model.UserTenantManagementMembership{},
	))

	admin := model.Admin{Username: "admin", PasswordHash: "test", Role: "admin", Enabled: true}
	require.NoError(t, database.Create(&admin).Error)
	unscoped := model.User{Name: "legacy-client", Role: model.UserRoleClient, SecretHash: "test", Enabled: true}
	disabled := model.User{Name: "legacy-disabled", Role: model.UserRoleClient, SecretHash: "test", Enabled: true}
	assigned := model.User{Name: "assigned-client", Role: model.UserRoleClient, SecretHash: "test", Enabled: true}
	agent := model.User{Name: "legacy-agent", Role: model.UserRoleAgent, SecretHash: "test", Enabled: true}
	require.NoError(t, database.Create(&unscoped).Error)
	require.NoError(t, database.Create(&disabled).Error)
	require.NoError(t, database.Model(&disabled).Update("enabled", false).Error)
	require.NoError(t, database.Create(&assigned).Error)
	require.NoError(t, database.Create(&agent).Error)

	existingTenant := model.Tenant{ID: "existing-tenant", Key: "existing", Name: "Existing", Status: model.TenantStatusActive}
	require.NoError(t, database.Create(&existingTenant).Error)
	require.NoError(t, database.Create(&model.TenantMembership{
		TenantID: existingTenant.ID, UserID: assigned.ID, Role: "viewer", Enabled: true,
	}).Error)
	managementUserID, err := SyncLegacyAdminIdentity(database, admin.ID, "test bootstrap")
	require.NoError(t, err)

	require.NoError(t, EnsureBeagleWorkspace(admin.Username))

	var workspace model.Tenant
	require.NoError(t, database.Where("key = ?", beagleWorkspaceKey).First(&workspace).Error)
	require.Equal(t, beagleWorkspaceName, workspace.Name)
	require.Equal(t, model.TenantStatusActive, workspace.Status)

	var adminMembership model.AdminTenantMembership
	require.NoError(t, database.Where("admin_id = ? AND tenant_id = ?", admin.ID, workspace.ID).First(&adminMembership).Error)
	require.Equal(t, string(model.TenantManagementRoleAdmin), adminMembership.Role)
	require.True(t, adminMembership.Enabled)

	assertWorkspaceMembership := func(userID uint64, expected int64) {
		t.Helper()
		var count int64
		require.NoError(t, database.Model(&model.TenantMembership{}).
			Where("tenant_id = ? AND user_id = ?", workspace.ID, userID).Count(&count).Error)
		require.Equal(t, expected, count)
	}
	assertWorkspaceMembership(unscoped.ID, 1)
	assertWorkspaceMembership(disabled.ID, 1)
	assertWorkspaceMembership(assigned.ID, 0)
	assertWorkspaceMembership(agent.ID, 0)
	assertWorkspaceMembership(managementUserID, 0)

	var assignedMembership model.TenantMembership
	require.NoError(t, database.Where("tenant_id = ? AND user_id = ?", existingTenant.ID, assigned.ID).First(&assignedMembership).Error)
	require.Equal(t, "viewer", assignedMembership.Role)

	require.NoError(t, database.Model(&adminMembership).Update("enabled", false).Error)
	require.NoError(t, database.Model(&model.TenantMembership{}).
		Where("tenant_id = ? AND user_id = ?", workspace.ID, unscoped.ID).Update("enabled", false).Error)
	require.NoError(t, EnsureBeagleWorkspace(admin.Username))

	require.NoError(t, database.First(&adminMembership, adminMembership.ID).Error)
	require.False(t, adminMembership.Enabled)
	var unscopedMembership model.TenantMembership
	require.NoError(t, database.Where("tenant_id = ? AND user_id = ?", workspace.ID, unscoped.ID).First(&unscopedMembership).Error)
	require.False(t, unscopedMembership.Enabled)

	var workspaceCount int64
	require.NoError(t, database.Model(&model.Tenant{}).Where("key = ?", beagleWorkspaceKey).Count(&workspaceCount).Error)
	require.Equal(t, int64(1), workspaceCount)

	require.NoError(t, database.Model(&workspace).Update("name", legacyBeagleWorkspaceName).Error)
	require.NoError(t, EnsureBeagleWorkspace(admin.Username))
	require.NoError(t, database.First(&workspace, "id = ?", workspace.ID).Error)
	require.Equal(t, beagleWorkspaceName, workspace.Name)

	require.NoError(t, database.Model(&workspace).Update("name", "自定义租户名称").Error)
	require.NoError(t, EnsureBeagleWorkspace(admin.Username))
	require.NoError(t, database.First(&workspace, "id = ?", workspace.ID).Error)
	require.Equal(t, "自定义租户名称", workspace.Name)
}
