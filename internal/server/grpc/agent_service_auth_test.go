package grpc

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

func TestAgentAuthenticateAcceptsConsumedTechnicalResourceToken(t *testing.T) {
	oldDB := db.DB
	t.Cleanup(func() { db.DB = oldDB })
	testDB, err := gorm.Open(sqlite.Open("file:agent_resource_token_auth_test?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	db.DB = testDB
	require.NoError(t, testDB.AutoMigrate(
		&model.User{}, &model.Node{}, &model.DeployToken{}, &model.ResourceProvider{},
		&model.TechnicalResource{}, &model.TechnicalResourceDeployToken{},
	))

	user := model.User{Name: "szzy", Role: model.UserRoleAgent, SecretHash: "not-the-runtime-secret", Enabled: true}
	require.NoError(t, testDB.Create(&user).Error)
	provider := model.ResourceProvider{ID: "provider-szzy", Key: "szzy", DisplayName: "Shenzhen Zhiyi", DomainLabel: "szzy", Status: model.ProviderStatusActive, Revision: 1, RowVersion: 1}
	require.NoError(t, testDB.Create(&provider).Error)
	resource := model.TechnicalResource{
		ID: "resource-szzy", ProviderID: provider.ID, Type: model.TechnicalResourceAgent, StableKey: "resource-szzy",
		LifecycleState: model.TechnicalResourceRegistered, HealthState: model.ResourceHealthOnline,
		CredentialRevision: 1, RuntimeUserID: user.ID, ConfigRevision: 1, RowVersion: 1,
	}
	require.NoError(t, testDB.Create(&resource).Error)
	token := model.TechnicalResourceDeployToken{
		ID: "token-szzy", TechnicalResourceID: resource.ID, Token: "szzy-runtime-token", Name: "szzy",
		RuntimeUserID: user.ID, Status: model.TechnicalResourceDeployTokenConsumed, CreatedByUserID: user.ID,
	}
	require.NoError(t, testDB.Create(&token).Error)

	server := &AgentServiceServer{}
	response, err := server.Authenticate(context.Background(), &pb.AgentAuthenticateRequest{AgentId: user.ID, Secret: token.Token, Version: "v1.0.0"})
	require.NoError(t, err)
	require.True(t, response.Success)

	require.NoError(t, testDB.Model(&resource).Update("lifecycle_state", model.TechnicalResourceRetired).Error)
	response, err = server.Authenticate(context.Background(), &pb.AgentAuthenticateRequest{AgentId: user.ID, Secret: token.Token})
	require.NoError(t, err)
	require.False(t, response.Success)
}
