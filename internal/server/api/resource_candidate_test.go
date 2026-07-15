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

func TestResourceCandidateObservationDoesNotPublishResource(t *testing.T) {
	oldDB := db.DB
	t.Cleanup(func() { db.DB = oldDB })
	testDB, err := gorm.Open(sqlite.Open("file:resource_candidate_test?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	db.DB = testDB
	require.NoError(t, testDB.AutoMigrate(&model.User{}, &model.Node{}, &model.DiscoveryCandidate{}, &model.Resource{}))
	user := model.User{Name: "agent-user", Role: model.UserRoleAgent, SecretHash: "test", Enabled: true}
	require.NoError(t, testDB.Create(&user).Error)
	node := model.Node{UserID: user.ID, Name: "agent-a", Type: model.NodeTypeAgent}
	require.NoError(t, testDB.Create(&node).Error)

	api := NewResourceCandidateAPI()
	r := gin.New()
	r.POST("/resource-candidates", api.Observe)
	r.GET("/resource-candidates", api.List)

	payload := map[string]interface{}{
		"agent_node_id": node.ID, "cluster_id": "beagle-dev", "namespace": "acme-dev",
		"pod_name": "ide-alpha", "pod_uid": "uid-alpha", "container_name": "workspace",
		"workspace_hint": "workspace-alpha", "ready": true,
		"labels": map[string]string{"beagle.io/workspace": "workspace-alpha"},
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "/resource-candidates", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	r.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	var result struct {
		Success bool                     `json:"success"`
		Data    model.DiscoveryCandidate `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	require.True(t, result.Success)
	require.Equal(t, model.DiscoveryCandidateObserved, result.Data.Status)

	var resources int64
	require.NoError(t, testDB.Model(&model.Resource{}).Count(&resources).Error)
	require.Zero(t, resources)

	rejectRequest := httptest.NewRequest(http.MethodPost, "/resource-candidates/"+result.Data.ID+"/reject", bytes.NewBufferString(`{"reason":"未匹配可信 Workspace"}`))
	rejectRequest.Header.Set("Content-Type", "application/json")
	rejectResponse := httptest.NewRecorder()
	r.POST("/resource-candidates/:id/reject", api.Reject)
	r.ServeHTTP(rejectResponse, rejectRequest)
	require.Equal(t, http.StatusOK, rejectResponse.Code)
	var rejected model.DiscoveryCandidate
	require.NoError(t, testDB.First(&rejected, "id = ?", result.Data.ID).Error)
	require.Equal(t, model.DiscoveryCandidateRejected, rejected.Status)
	require.Equal(t, uuid.MustParse(result.Data.ID).String(), result.Data.ID)
}
