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
	require.NoError(t, testDB.AutoMigrate(&model.User{}, &model.Node{}, &model.Tenant{}, &model.TenantMembership{}, &model.Group{}, &model.GroupMember{}, &model.Resource{}, &model.ResourceTarget{}, &model.AccessGrant{}, &model.ContainerSession{}, &model.WorkspaceBinding{}, &model.AuditLog{}))

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
	r.GET("/resources/summary", api.Summary)
	r.GET("/resources/:id/events", api.ListEvents)
	r.GET("/grants", api.ListAllGrants)
	r.POST("/resources/:id/grants", api.CreateGrant)
	r.POST("/grants/:id/revoke", api.RevokeGrant)
	r.POST("/resources/:id/targets", api.ObserveTarget)

	require.NoError(t, testDB.Create(&model.AuditLog{ActionType: "resource_a_event", TargetType: "resource", TargetID: resourceA.ID, TargetName: resourceA.DisplayName}).Error)
	require.NoError(t, testDB.Create(&model.AuditLog{ActionType: "resource_b_event", TargetType: "resource", TargetID: resourceB.ID, TargetName: resourceB.DisplayName}).Error)
	eventResp := httptest.NewRecorder()
	r.ServeHTTP(eventResp, httptest.NewRequest(http.MethodGet, "/resources/"+resourceA.ID+"/events", nil))
	require.Equal(t, http.StatusOK, eventResp.Code)
	var eventBody struct {
		Total int64           `json:"total"`
		Data  []resourceEvent `json:"data"`
	}
	require.NoError(t, json.Unmarshal(eventResp.Body.Bytes(), &eventBody))
	require.Equal(t, int64(1), eventBody.Total)
	require.Len(t, eventBody.Data, 1)
	require.Equal(t, "resource_a_event", eventBody.Data[0].ActionType)

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

	require.NoError(t, testDB.Create(&model.ContainerSession{
		ID: uuid.NewString(), TenantID: tenantA.ID, UserID: user.ID, ResourceID: resourceA.ID,
		Status: model.ContainerSessionActive, StartedAt: time.Now(),
	}).Error)
	summaryReq := httptest.NewRequest(http.MethodGet, "/resources/summary?tenant_id="+tenantA.ID, nil)
	summaryResp := httptest.NewRecorder()
	r.ServeHTTP(summaryResp, summaryReq)
	require.Equal(t, http.StatusOK, summaryResp.Code)
	var summaryBody struct {
		Success bool            `json:"success"`
		Data    resourceSummary `json:"data"`
	}
	require.NoError(t, json.Unmarshal(summaryResp.Body.Bytes(), &summaryBody))
	require.True(t, summaryBody.Success)
	require.Equal(t, int64(1), summaryBody.Data.Total)
	require.Equal(t, int64(1), summaryBody.Data.Available)
	require.Equal(t, int64(1), summaryBody.Data.ActiveSessions)
	require.Equal(t, int64(1), summaryBody.Data.ByType[string(model.ResourceTypeContainerSSH)])

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
	require.Equal(t, uint16(50200), updatedResource.ContainerSSHPort)
	require.Equal(t, agentNode.ID, updatedResource.AgentNodeID)

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
	var createdGrant struct {
		Data model.AccessGrant `json:"data"`
	}
	require.NoError(t, json.Unmarshal(grantResp.Body.Bytes(), &createdGrant))
	require.NotEmpty(t, createdGrant.Data.ID)

	grantListReq := httptest.NewRequest(http.MethodGet, "/grants?tenant_id="+tenantA.ID+"&status=enabled", nil)
	grantListResp := httptest.NewRecorder()
	r.ServeHTTP(grantListResp, grantListReq)
	require.Equal(t, http.StatusOK, grantListResp.Code)
	var grantListBody struct {
		Total int64                 `json:"total"`
		Data  []accessGrantListItem `json:"data"`
	}
	require.NoError(t, json.Unmarshal(grantListResp.Body.Bytes(), &grantListBody))
	require.Equal(t, int64(1), grantListBody.Total)
	require.Len(t, grantListBody.Data, 1)
	require.Equal(t, tenantA.ID, grantListBody.Data[0].TenantID)
	require.Equal(t, resourceA.DisplayName, grantListBody.Data[0].ResourceName)
	require.Equal(t, user.Alias, grantListBody.Data[0].SubjectName)

	otherGrantReq := httptest.NewRequest(http.MethodPost, "/resources/"+resourceB.ID+"/grants", bytes.NewReader(grantJSON))
	otherGrantReq.Header.Set("Content-Type", "application/json")
	otherGrantResp := httptest.NewRecorder()
	r.ServeHTTP(otherGrantResp, otherGrantReq)
	require.Equal(t, http.StatusForbidden, otherGrantResp.Code)

	tenantGroup := model.Group{TenantID: tenantA.ID, Name: "tenant-a-developers"}
	crossTenantGroup := model.Group{TenantID: tenantB.ID, Name: "tenant-b-developers"}
	legacyGroup := model.Group{Name: "legacy-global-group"}
	require.NoError(t, testDB.Create(&tenantGroup).Error)
	require.NoError(t, testDB.Create(&crossTenantGroup).Error)
	require.NoError(t, testDB.Create(&legacyGroup).Error)
	var tenantGroupGrantID string
	for _, tc := range []struct {
		groupID int64
		want    int
	}{
		{groupID: tenantGroup.ID, want: http.StatusCreated},
		{groupID: crossTenantGroup.ID, want: http.StatusForbidden},
		{groupID: legacyGroup.ID, want: http.StatusForbidden},
	} {
		payload, err := json.Marshal(map[string]interface{}{"subject_group_id": tc.groupID, "actions": []string{"shell"}})
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPost, "/resources/"+resourceA.ID+"/grants", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		resp := httptest.NewRecorder()
		r.ServeHTTP(resp, req)
		require.Equal(t, tc.want, resp.Code)
		if tc.groupID == tenantGroup.ID {
			var created struct {
				Data model.AccessGrant `json:"data"`
			}
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &created))
			tenantGroupGrantID = created.Data.ID
		}
	}
	require.NotEmpty(t, tenantGroupGrantID)
	groupGrantListResp := httptest.NewRecorder()
	r.ServeHTTP(groupGrantListResp, httptest.NewRequest(http.MethodGet, "/grants?tenant_id="+tenantA.ID+"&subject_type=group", nil))
	require.Equal(t, http.StatusOK, groupGrantListResp.Code)
	var groupGrantListBody struct {
		Data []accessGrantListItem `json:"data"`
	}
	require.NoError(t, json.Unmarshal(groupGrantListResp.Body.Bytes(), &groupGrantListBody))
	require.Len(t, groupGrantListBody.Data, 1)
	require.Equal(t, tenantGroupGrantID, groupGrantListBody.Data[0].ID)
	require.Equal(t, resourceA.DisplayName, groupGrantListBody.Data[0].ResourceName)
	require.Equal(t, tenantGroup.Name, groupGrantListBody.Data[0].SubjectName)

	hostResource := model.Resource{
		ID: uuid.NewString(), TenantID: tenantA.ID, Type: model.ResourceTypeHostSSH,
		DisplayName: "Host A", State: model.ResourceStateAvailable, AgentNodeID: agentNode.ID,
	}
	require.NoError(t, testDB.Create(&hostResource).Error)
	hostGrantReq := httptest.NewRequest(http.MethodPost, "/resources/"+hostResource.ID+"/grants", bytes.NewReader(grantJSON))
	hostGrantReq.Header.Set("Content-Type", "application/json")
	hostGrantResp := httptest.NewRecorder()
	r.ServeHTTP(hostGrantResp, hostGrantReq)
	require.Equal(t, http.StatusCreated, hostGrantResp.Code, hostGrantResp.Body.String())
	var hostGrant model.AccessGrant
	require.NoError(t, testDB.First(&hostGrant, "resource_id = ? AND subject_user_id = ?", hostResource.ID, user.ID).Error)
	require.Equal(t, model.ResourceTypeHostSSH, hostResource.Type)

	revokeResp := httptest.NewRecorder()
	r.ServeHTTP(revokeResp, httptest.NewRequest(http.MethodPost, "/grants/"+createdGrant.Data.ID+"/revoke", nil))
	require.Equal(t, http.StatusOK, revokeResp.Code)
	var revokedGrant model.AccessGrant
	require.NoError(t, testDB.First(&revokedGrant, "id = ?", createdGrant.Data.ID).Error)
	require.Equal(t, "revoked", revokedGrant.Status)
	require.Equal(t, int64(2), revokedGrant.Revision)
	repeatRevoke := httptest.NewRecorder()
	r.ServeHTTP(repeatRevoke, httptest.NewRequest(http.MethodPost, "/grants/"+createdGrant.Data.ID+"/revoke", nil))
	require.Equal(t, http.StatusConflict, repeatRevoke.Code)

	ownerReq := httptest.NewRequest(http.MethodPost, "/resources", bytes.NewReader([]byte(`{"tenant_id":"`+tenantA.ID+`","type":"container_ssh","display_name":"cross-tenant owner","owner_user_id":9999}`)))
	ownerReq.Header.Set("Content-Type", "application/json")
	ownerResp := httptest.NewRecorder()
	r.POST("/resources", api.Create)
	r.ServeHTTP(ownerResp, ownerReq)
	require.Equal(t, http.StatusBadRequest, ownerResp.Code)

	unsupportedCreate := httptest.NewRequest(http.MethodPost, "/resources", bytes.NewReader([]byte(`{"tenant_id":"`+tenantA.ID+`","type":"host_ssh","display_name":"legacy host"}`)))
	unsupportedCreate.Header.Set("Content-Type", "application/json")
	unsupportedCreateResp := httptest.NewRecorder()
	r.ServeHTTP(unsupportedCreateResp, unsupportedCreate)
	require.Equal(t, http.StatusConflict, unsupportedCreateResp.Code)

	unsupportedActionJSON, err := json.Marshal(map[string]interface{}{"subject_user_id": user.ID, "actions": []string{"port_forward"}})
	require.NoError(t, err)
	unsupportedActionReq := httptest.NewRequest(http.MethodPost, "/resources/"+resourceA.ID+"/grants", bytes.NewReader(unsupportedActionJSON))
	unsupportedActionReq.Header.Set("Content-Type", "application/json")
	unsupportedActionResp := httptest.NewRecorder()
	r.ServeHTTP(unsupportedActionResp, unsupportedActionReq)
	require.Equal(t, http.StatusBadRequest, unsupportedActionResp.Code)

	legacyResource := model.Resource{ID: uuid.NewString(), TenantID: tenantA.ID, Type: model.ResourceTypeHostSSH, DisplayName: "Legacy Host", State: model.ResourceStatePending}
	require.NoError(t, testDB.Create(&legacyResource).Error)
	legacyTargetReq := httptest.NewRequest(http.MethodPost, "/resources/"+legacyResource.ID+"/targets", bytes.NewReader(targetJSON))
	legacyTargetReq.Header.Set("Content-Type", "application/json")
	legacyTargetResp := httptest.NewRecorder()
	r.ServeHTTP(legacyTargetResp, legacyTargetReq)
	require.Equal(t, http.StatusConflict, legacyTargetResp.Code)
	legacyGrantReq := httptest.NewRequest(http.MethodPost, "/resources/"+legacyResource.ID+"/grants", bytes.NewReader(grantJSON))
	legacyGrantReq.Header.Set("Content-Type", "application/json")
	legacyGrantResp := httptest.NewRecorder()
	r.ServeHTTP(legacyGrantResp, legacyGrantReq)
	require.Equal(t, http.StatusConflict, legacyGrantResp.Code)
}
