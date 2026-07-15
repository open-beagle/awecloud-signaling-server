package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func TestUnifiedResourceTenantAndGrantBoundaries(t *testing.T) {
	oldDB := db.DB
	t.Cleanup(func() { db.DB = oldDB })

	testDB, err := gorm.Open(sqlite.Open("file:unified_resource_test?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	db.DB = testDB
	require.NoError(t, testDB.AutoMigrate(&model.User{}, &model.Node{}, &model.Tenant{}, &model.TenantMembership{}, &model.Resource{}, &model.ResourceTarget{}, &model.AccessGrant{}))

	tenantA := model.Tenant{ID: uuid.NewString(), Key: "tenant-a", Name: "Tenant A", Status: model.TenantStatusActive}
	tenantB := model.Tenant{ID: uuid.NewString(), Key: "tenant-b", Name: "Tenant B", Status: model.TenantStatusActive}
	require.NoError(t, testDB.Create(&tenantA).Error)
	require.NoError(t, testDB.Create(&tenantB).Error)
	user := model.User{Name: "alice", Alias: "Alice", Role: model.UserRoleClient, SecretHash: "test", Enabled: true}
	require.NoError(t, testDB.Create(&user).Error)
	require.NoError(t, testDB.Create(&model.TenantMembership{TenantID: tenantA.ID, UserID: user.ID, Role: "member", Enabled: true}).Error)
	agentUser := model.User{Name: "agent-a", Alias: "Agent A", Role: model.UserRoleAgent, SecretHash: "test", Enabled: true}
	require.NoError(t, testDB.Create(&agentUser).Error)
	agentNode := model.Node{UserID: agentUser.ID, Name: "agent-node-a", Type: model.NodeTypeAgent}
	require.NoError(t, testDB.Create(&agentNode).Error)

	resourceA := model.Resource{ID: uuid.NewString(), TenantID: tenantA.ID, Type: model.ResourceTypeContainerSSH, DisplayName: "IDE A", ProviderID: "beagle-ide", ExternalWorkspaceID: "workspace-a", State: model.ResourceStateAvailable, TargetRevision: 1}
	resourceB := model.Resource{ID: uuid.NewString(), TenantID: tenantB.ID, Type: model.ResourceTypeContainerSSH, DisplayName: "IDE B", ProviderID: "beagle-ide", ExternalWorkspaceID: "workspace-b", State: model.ResourceStateAvailable, TargetRevision: 1}
	require.NoError(t, testDB.Create(&resourceA).Error)
	require.NoError(t, testDB.Create(&resourceB).Error)

	api := NewUnifiedResourceAPI()
	r := gin.New()
	r.GET("/resources", api.List)
	r.POST("/resources/:id/grants", api.CreateGrant)
	r.POST("/resources/:id/targets", api.ObserveTarget)

	listReq := httptest.NewRequest(http.MethodGet, "/resources?tenant_id="+tenantA.ID, nil)
	listResp := httptest.NewRecorder()
	r.ServeHTTP(listResp, listReq)
	require.Equal(t, http.StatusOK, listResp.Code)
	var listBody struct {
		Success bool             `json:"success"`
		Data    []model.Resource `json:"data"`
		Total   int64            `json:"total"`
	}
	require.NoError(t, json.Unmarshal(listResp.Body.Bytes(), &listBody))
	require.True(t, listBody.Success)
	require.Equal(t, int64(1), listBody.Total)
	require.Len(t, listBody.Data, 1)
	require.Equal(t, resourceA.ID, listBody.Data[0].ID)

	targetPayload := map[string]interface{}{
		"agent_node_id": agentNode.ID, "cluster_id": "beagle-dev", "namespace": "tenant-a",
		"pod_name": "ide-a", "pod_uid": "pod-a", "container_name": "workspace", "ready": true,
	}
	targetJSON, err := json.Marshal(targetPayload)
	require.NoError(t, err)
	targetReq := httptest.NewRequest(http.MethodPost, "/resources/"+resourceA.ID+"/targets", bytes.NewReader(targetJSON))
	targetReq.Header.Set("Content-Type", "application/json")
	targetResp := httptest.NewRecorder()
	r.ServeHTTP(targetResp, targetReq)
	require.Equal(t, http.StatusCreated, targetResp.Code)
	var targetBody struct {
		Data model.ResourceTarget `json:"data"`
	}
	require.NoError(t, json.Unmarshal(targetResp.Body.Bytes(), &targetBody))
	require.Equal(t, int64(2), targetBody.Data.Revision)
	var updatedResource model.Resource
	require.NoError(t, testDB.First(&updatedResource, "id = ?", resourceA.ID).Error)
	require.Equal(t, model.ResourceStateAvailable, updatedResource.State)

	conflictReq := httptest.NewRequest(http.MethodPost, "/resources/"+resourceB.ID+"/targets", bytes.NewReader(targetJSON))
	conflictReq.Header.Set("Content-Type", "application/json")
	conflictResp := httptest.NewRecorder()
	r.ServeHTTP(conflictResp, conflictReq)
	require.Equal(t, http.StatusConflict, conflictResp.Code)

	grantPayload := map[string]interface{}{
		"subject_user_id":     user.ID,
		"actions":             []string{"shell"},
		"valid_from":          time.Now().UTC().Format(time.RFC3339),
		"expires_at":          time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
		"max_session_seconds": 3600,
	}
	grantJSON, err := json.Marshal(grantPayload)
	require.NoError(t, err)
	grantReq := httptest.NewRequest(http.MethodPost, "/resources/"+resourceA.ID+"/grants", bytes.NewReader(grantJSON))
	grantReq.Header.Set("Content-Type", "application/json")
	grantResp := httptest.NewRecorder()
	r.ServeHTTP(grantResp, grantReq)
	require.Equal(t, http.StatusCreated, grantResp.Code)

	otherGrantReq := httptest.NewRequest(http.MethodPost, "/resources/"+resourceB.ID+"/grants", bytes.NewReader(grantJSON))
	otherGrantReq.Header.Set("Content-Type", "application/json")
	otherGrantResp := httptest.NewRecorder()
	r.ServeHTTP(otherGrantResp, otherGrantReq)
	require.Equal(t, http.StatusForbidden, otherGrantResp.Code)

	ownerReq := httptest.NewRequest(http.MethodPost, "/resources", bytes.NewReader([]byte(`{"tenant_id":"`+tenantA.ID+`","type":"container_ssh","display_name":"cross-tenant owner","owner_user_id":9999}`)))
	ownerReq.Header.Set("Content-Type", "application/json")
	ownerResp := httptest.NewRecorder()
	r.POST("/resources", api.Create)
	r.ServeHTTP(ownerResp, ownerReq)
	require.Equal(t, http.StatusBadRequest, ownerResp.Code)
}
