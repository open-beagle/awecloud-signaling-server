package db

import (
	"fmt"
	"testing"

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
