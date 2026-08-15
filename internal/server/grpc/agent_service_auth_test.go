package grpc

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/cache"
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
	provider := model.ResourceProvider{ID: "provider-szzy", Key: "szzy", DisplayName: "Shenzhen Zhiyi", DomainScope: model.ProviderDomainNamed, DomainLabel: "szzy", Status: model.ProviderStatusActive, Revision: 1, RowVersion: 1}
	require.NoError(t, testDB.Create(&provider).Error)
	resource := model.TechnicalResource{
		ID: "resource-szzy", ProviderID: provider.ID, Type: model.TechnicalResourceAgent, StableKey: "resource-szzy", DomainLabel: "resource-szzy",
		LifecycleState: model.TechnicalResourceRegistered, HealthState: model.ResourceHealthOnline,
		CredentialRevision: 1, RuntimeUserID: user.ID, ConfigRevision: 1, RowVersion: 1,
	}
	require.NoError(t, testDB.Create(&resource).Error)
	token := model.TechnicalResourceDeployToken{
		ID: "token-szzy", TechnicalResourceID: resource.ID, Token: "szzy-runtime-token", Name: "szzy",
		RuntimeUserID: user.ID, Status: model.TechnicalResourceDeployTokenConsumed, CreatedByUserID: user.ID,
	}
	require.NoError(t, testDB.Create(&token).Error)

	runtimeStore := cache.NewNodeRuntimeStore()
	server := &AgentServiceServer{runtimeStore: runtimeStore}
	response, err := server.Authenticate(context.Background(), &pb.AgentAuthenticateRequest{AgentId: user.ID, Secret: token.Token, Version: "v1.0.0"})
	require.NoError(t, err)
	require.True(t, response.Success)
	runtimeNode, ok := runtimeStore.GetNodeByUserAndName(user.ID, model.NodeTypeAgent, user.Name)
	require.True(t, ok)
	require.Equal(t, "v1.0.0", runtimeNode.Version)

	require.NoError(t, testDB.Model(&resource).Update("lifecycle_state", model.TechnicalResourceRetired).Error)
	response, err = server.Authenticate(context.Background(), &pb.AgentAuthenticateRequest{AgentId: user.ID, Secret: token.Token})
	require.NoError(t, err)
	require.False(t, response.Success)
}

func TestAgentRegisterResumesWithConsumedTechnicalResourceToken(t *testing.T) {
	oldDB := db.DB
	t.Cleanup(func() { db.DB = oldDB })
	testDB, err := gorm.Open(sqlite.Open("file:agent_resource_token_register_test?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	db.DB = testDB
	require.NoError(t, testDB.AutoMigrate(
		&model.User{}, &model.Node{}, &model.DeployToken{}, &model.ResourceProvider{},
		&model.TechnicalResource{}, &model.TechnicalResourceDeployToken{},
	))

	user := model.User{Name: "beijing", Role: model.UserRoleAgent, SecretHash: "fixture", Enabled: true}
	require.NoError(t, testDB.Create(&user).Error)
	provider := model.ResourceProvider{ID: "provider-beagle", Key: "beagle", DisplayName: "Beagle", DomainScope: model.ProviderDomainRoot, Status: model.ProviderStatusActive, Revision: 1, RowVersion: 1}
	require.NoError(t, testDB.Create(&provider).Error)
	resource := model.TechnicalResource{
		ID: "resource-beijing", ProviderID: provider.ID, Type: model.TechnicalResourceAgent, StableKey: "resource-beijing", DomainLabel: "beijing",
		LifecycleState: model.TechnicalResourceRegistered, HealthState: model.ResourceHealthOnline,
		CredentialRevision: 1, RuntimeUserID: user.ID, ConfigRevision: 1, RowVersion: 1,
	}
	require.NoError(t, testDB.Create(&resource).Error)
	node := model.Node{UserID: user.ID, Name: "beagle-242", Type: model.NodeTypeAgent}
	require.NoError(t, testDB.Create(&node).Error)
	token := model.TechnicalResourceDeployToken{
		ID: "token-beijing", TechnicalResourceID: resource.ID, Token: "persisted-runtime-token", Name: node.Name,
		RuntimeUserID: user.ID, Status: model.TechnicalResourceDeployTokenConsumed, CreatedByUserID: user.ID,
	}
	require.NoError(t, testDB.Create(&token).Error)

	runtimeStore := cache.NewNodeRuntimeStore()
	runtimeStore.UpsertNode(&node)
	server := &AgentServiceServer{runtimeStore: runtimeStore}
	commitDate := "2026-08-12T18:42:06+08:00"
	commitID := strings.Repeat("a", 40)
	binarySHA256 := strings.Repeat("b", 64)
	response, err := server.Register(context.Background(), &pb.AgentRegisterRequest{
		Secret: token.Token, Version: "v1.0.1", CommitId: commitID, CommitDate: commitDate, BinarySha256: binarySHA256,
	})
	require.NoError(t, err)
	require.True(t, response.Success)
	require.Equal(t, user.ID, response.AgentId)
	require.Equal(t, node.Name, response.DeviceName)

	var persistedToken model.TechnicalResourceDeployToken
	require.NoError(t, testDB.First(&persistedToken, "id = ?", token.ID).Error)
	require.Equal(t, model.TechnicalResourceDeployTokenConsumed, persistedToken.Status)
	var persistedNode model.Node
	require.NoError(t, testDB.First(&persistedNode, node.ID).Error)
	require.Equal(t, "v1.0.1", persistedNode.Version)
	require.NotNil(t, persistedNode.CommitDate)
	expectedCommitDate, err := time.Parse(time.RFC3339, commitDate)
	require.NoError(t, err)
	require.True(t, expectedCommitDate.Equal(*persistedNode.CommitDate))
	runtimeNode, ok := runtimeStore.GetNode(node.ID)
	require.True(t, ok)
	require.Equal(t, "v1.0.1", runtimeNode.Version)
	require.Equal(t, commitID, runtimeNode.CommitID)
	require.Equal(t, binarySHA256, runtimeNode.BinarySHA256)
	require.NotNil(t, runtimeNode.CommitDate)
	require.True(t, expectedCommitDate.Equal(*runtimeNode.CommitDate))

	require.NoError(t, testDB.Delete(&persistedNode).Error)
	response, err = server.Register(context.Background(), &pb.AgentRegisterRequest{Secret: token.Token, Version: "v1.0.1"})
	require.NoError(t, err)
	require.False(t, response.Success)
	require.Equal(t, "Agent Node 不存在", response.Message)
}

func TestAgentRegisterRejectsInvalidCommitDate(t *testing.T) {
	server := &AgentServiceServer{}
	_, err := server.Register(context.Background(), &pb.AgentRegisterRequest{CommitDate: "2026-08-12 18:42:06"})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}
