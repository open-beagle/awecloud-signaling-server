package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func TestWorkspaceBindingReconcilesCandidateOnlyAfterTrustedBinding(t *testing.T) {
	oldDB := db.DB
	t.Cleanup(func() { db.DB = oldDB })
	testDB, err := gorm.Open(sqlite.Open("file:workspace_binding_test?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	db.DB = testDB
	require.NoError(t, testDB.AutoMigrate(
		&model.User{}, &model.Node{}, &model.Tenant{}, &model.TenantMembership{},
		&model.ProviderTenantBinding{}, &model.WorkspaceBinding{}, &model.Resource{},
		&model.ResourceTarget{}, &model.DiscoveryCandidate{}, &model.AuditLog{},
	))
	tenant := model.Tenant{ID: uuid.NewString(), Key: "acme", Name: "Acme", Status: model.TenantStatusActive}
	require.NoError(t, testDB.Create(&tenant).Error)
	owner := model.User{Name: "alice", Alias: "Alice", Role: model.UserRoleClient, SecretHash: "test", Enabled: true}
	require.NoError(t, testDB.Create(&owner).Error)
	require.NoError(t, testDB.Create(&model.TenantMembership{TenantID: tenant.ID, UserID: owner.ID, Role: "tenant_admin", Enabled: true}).Error)
	agentUser := model.User{Name: "agent", Alias: "Agent", Role: model.UserRoleAgent, SecretHash: "test", Enabled: true}
	require.NoError(t, testDB.Create(&agentUser).Error)
	agentNode := model.Node{UserID: agentUser.ID, Name: "agent-a", Type: model.NodeTypeAgent}
	require.NoError(t, testDB.Create(&agentNode).Error)

	candidateAPI := NewResourceCandidateAPI()
	bindingAPI := NewWorkspaceBindingAPI()
	r := gin.New()
	r.POST("/resource-candidates", candidateAPI.Observe)
	r.POST("/resource-candidates/:id/reconcile", candidateAPI.Reconcile)
	r.POST("/provider-tenant-bindings", bindingAPI.CreateProviderTenantBinding)
	r.POST("/workspace-bindings", bindingAPI.CreateWorkspaceBinding)

	post := func(path string, payload interface{}) *httptest.ResponseRecorder {
		body, marshalErr := json.Marshal(payload)
		require.NoError(t, marshalErr)
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp := httptest.NewRecorder()
		r.ServeHTTP(resp, req)
		return resp
	}

	candidateResp := post("/resource-candidates", map[string]interface{}{
		"agent_node_id": agentNode.ID, "provider_hint": "beagle-ide", "cluster_id": "dev",
		"namespace": "acme", "pod_name": "ide-a", "pod_uid": "pod-a", "container_name": "workspace",
		"workspace_hint": "workspace-a", "generation_hint": 1, "ready": true,
	})
	require.Equal(t, http.StatusOK, candidateResp.Code)
	var candidateResult struct {
		Data model.DiscoveryCandidate `json:"data"`
	}
	require.NoError(t, json.Unmarshal(candidateResp.Body.Bytes(), &candidateResult))

	firstReconcile := post("/resource-candidates/"+candidateResult.Data.ID+"/reconcile", nil)
	require.Equal(t, http.StatusAccepted, firstReconcile.Code)
	var resourceCount int64
	require.NoError(t, testDB.Model(&model.Resource{}).Count(&resourceCount).Error)
	require.Zero(t, resourceCount)

	providerResp := post("/provider-tenant-bindings", map[string]interface{}{
		"provider_id": "beagle-ide", "external_tenant_id": "customer-acme", "tenant_id": tenant.ID,
	})
	require.Equal(t, http.StatusCreated, providerResp.Code)
	workspaceResp := post("/workspace-bindings", map[string]interface{}{
		"provider_id": "beagle-ide", "external_tenant_id": "customer-acme", "external_workspace_id": "workspace-a",
		"display_name": "IDE / workspace-a", "owner_user_id": owner.ID, "generation": 1,
	})
	require.Equal(t, http.StatusCreated, workspaceResp.Code)
	require.NoError(t, testDB.First(&candidateResult.Data, "id = ?", candidateResult.Data.ID).Error)
	require.Equal(t, model.DiscoveryCandidatePublished, candidateResult.Data.Status)
	require.NotEmpty(t, candidateResult.Data.ResourceID)

	secondReconcile := post("/resource-candidates/"+candidateResult.Data.ID+"/reconcile", nil)
	require.Equal(t, http.StatusOK, secondReconcile.Code)
	var published struct {
		Data model.DiscoveryCandidate `json:"data"`
	}
	require.NoError(t, json.Unmarshal(secondReconcile.Body.Bytes(), &published))
	require.Equal(t, model.DiscoveryCandidatePublished, published.Data.Status)
	require.NotEmpty(t, published.Data.ResourceID)
	require.NoError(t, testDB.Model(&model.Resource{}).Count(&resourceCount).Error)
	require.Equal(t, int64(1), resourceCount)
	var resource model.Resource
	require.NoError(t, testDB.First(&resource, "id = ?", published.Data.ResourceID).Error)
	require.Equal(t, model.ResourceStateAvailable, resource.State)
	require.Equal(t, int64(1), resource.TargetRevision)

	newCandidateResp := post("/resource-candidates", map[string]interface{}{
		"agent_node_id": agentNode.ID, "provider_hint": "beagle-ide", "cluster_id": "dev",
		"namespace": "acme", "pod_name": "ide-a-recreated", "pod_uid": "pod-a-recreated", "container_name": "workspace",
		"workspace_hint": "workspace-a", "generation_hint": 2, "ready": true,
	})
	require.Equal(t, http.StatusOK, newCandidateResp.Code)
	var newCandidateResult struct {
		Data model.DiscoveryCandidate `json:"data"`
	}
	require.NoError(t, json.Unmarshal(newCandidateResp.Body.Bytes(), &newCandidateResult))
	generationMismatch := post("/resource-candidates/"+newCandidateResult.Data.ID+"/reconcile", nil)
	require.Equal(t, http.StatusConflict, generationMismatch.Code)
	require.NoError(t, testDB.First(&newCandidateResult.Data, "id = ?", newCandidateResult.Data.ID).Error)
	require.Equal(t, model.DiscoveryCandidateConflict, newCandidateResult.Data.Status)
	require.NoError(t, testDB.First(&resource, "id = ?", published.Data.ResourceID).Error)
	require.Equal(t, int64(1), resource.TargetRevision)

	newGeneration := post("/workspace-bindings", map[string]interface{}{
		"provider_id": "beagle-ide", "external_tenant_id": "customer-acme", "external_workspace_id": "workspace-a",
		"display_name": "IDE / workspace-a", "owner_user_id": owner.ID, "generation": 2,
	})
	require.Equal(t, http.StatusOK, newGeneration.Code)
	olderGeneration := post("/workspace-bindings", map[string]interface{}{
		"provider_id": "beagle-ide", "external_tenant_id": "customer-acme", "external_workspace_id": "workspace-a",
		"display_name": "IDE / workspace-a", "owner_user_id": owner.ID, "generation": 1,
	})
	require.Equal(t, http.StatusConflict, olderGeneration.Code)
}
