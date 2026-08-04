package db

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

func TestMigrateDeployTokensMovesAgentTokensAndPreservesDesktopTokens(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", uuid.NewString())), &gorm.Config{IgnoreRelationshipsWhenMigrating: true})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&model.User{}, &model.Node{}, &model.ResourceProvider{}, &model.TechnicalResource{}, &model.TechnicalResourceBinding{}, &model.DeployToken{}, &model.TechnicalResourceDeployToken{}))
	agent := model.User{Name: "agent-user", Role: model.UserRoleAgent, SecretHash: "fixture", Enabled: true}
	desktop := model.User{Name: "desktop-user", Role: model.UserRoleClient, SecretHash: "fixture", Enabled: true}
	require.NoError(t, database.Create(&agent).Error)
	require.NoError(t, database.Create(&desktop).Error)
	provider := model.ResourceProvider{ID: uuid.NewString(), Key: "beagle", DisplayName: "Beagle", Status: model.ProviderStatusActive, Revision: 1, RowVersion: 1}
	require.NoError(t, database.Create(&provider).Error)
	require.NoError(t, database.Create(&model.DeployToken{Token: "agent-pending", UserID: agent.ID, Name: "pending", Status: model.DeployTokenStatusPending, CreatedBy: 1}).Error)
	require.NoError(t, database.Create(&model.DeployToken{Token: "agent-bound", UserID: agent.ID, Name: "bound", Status: model.DeployTokenStatusBound, CreatedBy: 1}).Error)
	require.NoError(t, database.Create(&model.DeployToken{Token: "desktop-pending", UserID: desktop.ID, Name: "desktop", Status: model.DeployTokenStatusPending, CreatedBy: 1}).Error)

	require.NoError(t, migrateAgentDeployTokensToTechnicalResources(database))
	var resourceTokens []model.TechnicalResourceDeployToken
	require.NoError(t, database.Order("name").Find(&resourceTokens).Error)
	require.Len(t, resourceTokens, 2)
	statuses := map[string]model.TechnicalResourceDeployTokenStatus{}
	for _, token := range resourceTokens {
		statuses[token.Name] = token.Status
	}
	require.Equal(t, model.TechnicalResourceDeployTokenPending, statuses["pending"])
	require.Equal(t, model.TechnicalResourceDeployTokenConsumed, statuses["bound"])
	var resources int64
	require.NoError(t, database.Model(&model.TechnicalResource{}).Where("provider_id = ? AND runtime_user_id = ?", provider.ID, agent.ID).Count(&resources).Error)
	require.Equal(t, int64(2), resources)
	require.True(t, database.Migrator().HasTable(&model.DeployToken{}))
	var legacyTokens []model.DeployToken
	require.NoError(t, database.Find(&legacyTokens).Error)
	require.Len(t, legacyTokens, 1)
	require.Equal(t, "desktop-pending", legacyTokens[0].Token)
	require.Equal(t, desktop.ID, legacyTokens[0].UserID)

	require.NoError(t, migrateAgentDeployTokensToTechnicalResources(database))
	require.NoError(t, database.Find(&resourceTokens).Error)
	require.Len(t, resourceTokens, 2)
}

func TestMigrateDeployTokensReconcilesAlreadyMigratedToken(t *testing.T) {
	database := newDeployTokenMigrationTestDB(t)
	agent := model.User{Name: "agent-user", Role: model.UserRoleAgent, SecretHash: "fixture", Enabled: true}
	require.NoError(t, database.Create(&agent).Error)
	provider := model.ResourceProvider{ID: uuid.NewString(), Key: "beagle", DisplayName: "Beagle", Status: model.ProviderStatusActive, Revision: 1, RowVersion: 1}
	require.NoError(t, database.Create(&provider).Error)
	resource := model.TechnicalResource{
		ID: uuid.NewString(), ProviderID: provider.ID, Type: model.TechnicalResourceAgent, StableKey: "legacy-node:48",
		LifecycleState: model.TechnicalResourceRegistered, HealthState: model.ResourceHealthOnline,
		CredentialRevision: 1, RuntimeUserID: agent.ID, ConfigRevision: 1, RowVersion: 1,
	}
	require.NoError(t, database.Create(&resource).Error)
	now := time.Now().UTC().Truncate(time.Second)
	legacy := model.DeployToken{
		Token: "already-migrated", UserID: agent.ID, Name: "beagle-242", Status: model.DeployTokenStatusBound,
		DeviceFingerprint: "device-fingerprint", CreatedBy: 1, BoundAt: &now,
	}
	require.NoError(t, database.Create(&legacy).Error)
	existing := model.TechnicalResourceDeployToken{
		ID: uuid.NewString(), TechnicalResourceID: resource.ID, Token: legacy.Token, Name: legacy.Name,
		RuntimeUserID: agent.ID, Status: model.TechnicalResourceDeployTokenConsumed,
		DeviceFingerprint: legacy.DeviceFingerprint, ConsumedAt: &now, CreatedByUserID: agent.ID,
	}
	require.NoError(t, database.Create(&existing).Error)

	require.NoError(t, migrateAgentDeployTokensToTechnicalResources(database))
	require.NoError(t, migrateAgentDeployTokensToTechnicalResources(database))
	var legacyCount, migratedCount, resourceCount int64
	require.NoError(t, database.Model(&model.DeployToken{}).Where("token = ?", legacy.Token).Count(&legacyCount).Error)
	require.NoError(t, database.Model(&model.TechnicalResourceDeployToken{}).Where("token = ?", legacy.Token).Count(&migratedCount).Error)
	require.NoError(t, database.Model(&model.TechnicalResource{}).Count(&resourceCount).Error)
	require.Zero(t, legacyCount)
	require.Equal(t, int64(1), migratedCount)
	require.Equal(t, int64(1), resourceCount)
}

func TestMigrateDeployTokensRejectsConflictingMigratedToken(t *testing.T) {
	database := newDeployTokenMigrationTestDB(t)
	agent := model.User{Name: "agent-user", Role: model.UserRoleAgent, SecretHash: "fixture", Enabled: true}
	require.NoError(t, database.Create(&agent).Error)
	provider := model.ResourceProvider{ID: uuid.NewString(), Key: "beagle", DisplayName: "Beagle", Status: model.ProviderStatusActive, Revision: 1, RowVersion: 1}
	require.NoError(t, database.Create(&provider).Error)
	resource := model.TechnicalResource{
		ID: uuid.NewString(), ProviderID: provider.ID, Type: model.TechnicalResourceAgent, StableKey: "conflict",
		LifecycleState: model.TechnicalResourcePending, HealthState: model.ResourceHealthUnknown,
		CredentialRevision: 1, RuntimeUserID: agent.ID, ConfigRevision: 1, RowVersion: 1,
	}
	require.NoError(t, database.Create(&resource).Error)
	legacy := model.DeployToken{Token: "conflicting-token", UserID: agent.ID, Name: "legacy-name", Status: model.DeployTokenStatusPending, CreatedBy: 1}
	require.NoError(t, database.Create(&legacy).Error)
	existing := model.TechnicalResourceDeployToken{
		ID: uuid.NewString(), TechnicalResourceID: resource.ID, Token: legacy.Token, Name: "different-name",
		RuntimeUserID: agent.ID, Status: model.TechnicalResourceDeployTokenPending, CreatedByUserID: agent.ID,
	}
	require.NoError(t, database.Create(&existing).Error)

	err := migrateAgentDeployTokensToTechnicalResources(database)
	require.ErrorContains(t, err, "credential attributes differ")
	var legacyCount int64
	require.NoError(t, database.Model(&model.DeployToken{}).Where("token = ?", legacy.Token).Count(&legacyCount).Error)
	require.Equal(t, int64(1), legacyCount, "the transaction must retain the legacy row on conflict")
}

func newDeployTokenMigrationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", uuid.NewString())), &gorm.Config{IgnoreRelationshipsWhenMigrating: true})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&model.User{}, &model.Node{}, &model.ResourceProvider{}, &model.TechnicalResource{}, &model.TechnicalResourceBinding{}, &model.DeployToken{}, &model.TechnicalResourceDeployToken{}))
	return database
}
