package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	serverdb "github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func TestResourceDeployTokenRegistersOnceAndBindsTechnicalResource(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", uuid.NewString())), &gorm.Config{IgnoreRelationshipsWhenMigrating: true})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(
		&model.User{}, &model.Node{}, &model.Endpoint{}, &model.ResourceProvider{}, &model.TechnicalResource{}, &model.TechnicalResourceBinding{},
		&model.SupplyCandidate{}, &model.PlatformResource{}, &model.PlatformResourceSource{}, &model.TechnicalResourceDeployToken{}, &model.DeployToken{},
	))
	previous := serverdb.DB
	serverdb.DB = database
	t.Cleanup(func() { serverdb.DB = previous })

	user := model.User{Name: "runtime-agent", Role: model.UserRoleAgent, SecretHash: "fixture", Enabled: true}
	require.NoError(t, database.Create(&user).Error)
	provider := model.ResourceProvider{ID: uuid.NewString(), Key: "provider-a", DisplayName: "Provider A", DomainScope: model.ProviderDomainNamed, DomainLabel: "provider-a", Status: model.ProviderStatusActive, Revision: 1, RowVersion: 1}
	require.NoError(t, database.Create(&provider).Error)
	resource := model.TechnicalResource{ID: uuid.NewString(), ProviderID: provider.ID, Type: model.TechnicalResourceAgent, StableKey: "resource:test", DomainLabel: "resource-test", LifecycleState: model.TechnicalResourcePending, HealthState: model.ResourceHealthUnknown, CredentialRevision: 1, RuntimeUserID: user.ID, ConfigRevision: 1, RowVersion: 1}
	require.NoError(t, database.Create(&resource).Error)
	expires := time.Now().Add(time.Hour)
	token := model.TechnicalResourceDeployToken{ID: uuid.NewString(), TechnicalResourceID: resource.ID, Token: "resource-token", Name: "production-agent", RuntimeUserID: user.ID, Status: model.TechnicalResourceDeployTokenPending, ExpiresAt: &expires, CreatedByUserID: user.ID}
	require.NoError(t, database.Create(&token).Error)

	api := NewDeployAPI(&config.ServerConfig{})
	register := func(rawToken string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(RegisterRequest{Token: rawToken, DeviceFingerprint: "hostname-hash"})
		request := httptest.NewRequest(http.MethodPost, "/api/v1/register", bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(response)
		context.Request = request
		api.Register(context)
		return response
	}

	first := register(token.Token)
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())
	require.NoError(t, database.First(&resource, "id = ?", resource.ID).Error)
	require.Equal(t, model.TechnicalResourceRegistered, resource.LifecycleState)
	require.NoError(t, database.First(&token, "id = ?", token.ID).Error)
	require.Equal(t, model.TechnicalResourceDeployTokenConsumed, token.Status)
	var binding model.TechnicalResourceBinding
	require.NoError(t, database.First(&binding, "technical_resource_id = ? AND enabled = ?", resource.ID, true).Error)
	hostStableKey := "legacy-host-legacy_node:" + binding.SourceID
	var candidate model.SupplyCandidate
	require.NoError(t, database.First(&candidate, "technical_resource_id = ? AND resource_type = ? AND stable_key = ?", resource.ID, model.SupplyResourceHost, hostStableKey).Error)
	require.Equal(t, model.SupplyCandidateLinked, candidate.ReviewState)
	var hostResource model.PlatformResource
	require.NoError(t, database.First(&hostResource, "provider_id = ? AND type = ? AND stable_key = ?", provider.ID, model.SupplyResourceHost, hostStableKey).Error)
	require.Equal(t, "production-agent", hostResource.DisplayName)
	require.Equal(t, model.PlatformResourceActive, hostResource.LifecycleState)
	var source model.PlatformResourceSource
	require.NoError(t, database.First(&source, "platform_resource_id = ? AND supply_candidate_id = ? AND is_primary = ?", hostResource.ID, candidate.ID, true).Error)

	second := register(token.Token)
	require.Equal(t, http.StatusForbidden, second.Code)

	legacyAgent := model.DeployToken{Token: "legacy-agent-token", UserID: user.ID, Name: "legacy-agent", Status: model.DeployTokenStatusPending, CreatedBy: 1}
	require.NoError(t, database.Create(&legacyAgent).Error)
	require.Equal(t, http.StatusForbidden, register(legacyAgent.Token).Code)

	client := model.User{Name: "client-user", Role: model.UserRoleClient, SecretHash: "fixture", Enabled: true}
	require.NoError(t, database.Create(&client).Error)
	legacy := model.DeployToken{Token: "legacy-user-token", UserID: client.ID, Name: "desktop", Status: model.DeployTokenStatusPending, CreatedBy: 1}
	require.NoError(t, database.Create(&legacy).Error)
	require.Equal(t, http.StatusOK, register(legacy.Token).Code)
	require.NoError(t, database.First(&legacy, legacy.ID).Error)
	require.Equal(t, model.DeployTokenStatusBound, legacy.Status)
}
