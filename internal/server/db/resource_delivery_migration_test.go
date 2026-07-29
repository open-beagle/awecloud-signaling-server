package db

import (
	"path/filepath"
	"testing"

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
	} {
		require.True(t, DB.Migrator().HasTable(table))
	}
	var profileCount int64
	require.NoError(t, DB.Model(&model.UserIdentityProfile{}).Count(&profileCount).Error)
	require.Zero(t, profileCount)
}
