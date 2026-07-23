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

func TestContainerSessionLifecycleAndAudit(t *testing.T) {
	oldDB := db.DB
	t.Cleanup(func() { db.DB = oldDB })
	testDB, err := gorm.Open(sqlite.Open("file:container_session_api_test?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	db.DB = testDB
	require.NoError(t, testDB.AutoMigrate(&model.User{}, &model.Node{}, &model.Tenant{}, &model.Resource{}, &model.ContainerSession{}, &model.AuditLog{}))

	user := model.User{Name: "session-user", Alias: "Session User", Role: model.UserRoleClient, SecretHash: "test", Enabled: true}
	require.NoError(t, testDB.Create(&user).Error)
	tenant := model.Tenant{ID: uuid.NewString(), Key: "session-acme", Name: "Session Acme", Status: model.TenantStatusActive}
	require.NoError(t, testDB.Create(&tenant).Error)
	resource := model.Resource{ID: uuid.NewString(), TenantID: tenant.ID, Type: model.ResourceTypeContainerSSH, DisplayName: "IDE Session", State: model.ResourceStateAvailable, TargetRevision: 2}
	require.NoError(t, testDB.Create(&resource).Error)
	require.NoError(t, testDB.Create(&model.Node{ID: 9, UserID: 999, Name: "container-agent", Type: model.NodeTypeAgent, ContainerSSHProtocol: "v1"}).Error)
	active := model.ContainerSession{ID: uuid.NewString(), TenantID: tenant.ID, UserID: user.ID, ResourceID: resource.ID, WorkspaceID: "workspace-a", GrantRevision: 3, TargetRevision: 2, PodUID: "pod-a", ContainerName: "workspace", AgentNodeID: 9, Status: model.ContainerSessionActive, StartedAt: time.Now().Add(-time.Minute)}
	require.NoError(t, testDB.Create(&active).Error)
	second := model.ContainerSession{ID: uuid.NewString(), TenantID: tenant.ID, UserID: user.ID, ResourceID: resource.ID, AgentNodeID: 9, Status: model.ContainerSessionActive, StartedAt: time.Now().Add(-2 * time.Minute)}
	require.NoError(t, testDB.Create(&second).Error)

	api := NewContainerSessionAPI()
	r := gin.New()
	r.GET("/sessions", api.List)
	r.GET("/sessions/:id", api.Get)
	r.POST("/sessions/:id/revoke", api.Revoke)
	r.POST("/sessions/:id/force-disconnect", api.ForceDisconnect)

	listResp := httptest.NewRecorder()
	r.ServeHTTP(listResp, httptest.NewRequest(http.MethodGet, "/sessions?status=active&tenant_id="+tenant.ID, nil))
	require.Equal(t, http.StatusOK, listResp.Code)
	var listResult struct {
		Total int64 `json:"total"`
	}
	require.NoError(t, json.Unmarshal(listResp.Body.Bytes(), &listResult))
	require.Equal(t, int64(2), listResult.Total)

	getResp := httptest.NewRecorder()
	r.ServeHTTP(getResp, httptest.NewRequest(http.MethodGet, "/sessions/"+active.ID, nil))
	require.Equal(t, http.StatusOK, getResp.Code)
	var detailResult struct {
		Data struct {
			Session model.ContainerSession `json:"session"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(getResp.Body.Bytes(), &detailResult))
	require.Equal(t, active.ID, detailResult.Data.Session.ID)

	revokeReq := httptest.NewRequest(http.MethodPost, "/sessions/"+active.ID+"/revoke", bytes.NewReader([]byte(`{"reason":"用户离职"}`)))
	revokeReq.Header.Set("Content-Type", "application/json")
	revokeResp := httptest.NewRecorder()
	r.ServeHTTP(revokeResp, revokeReq)
	require.Equal(t, http.StatusOK, revokeResp.Code)
	var revoked model.ContainerSession
	require.NoError(t, testDB.First(&revoked, "id = ?", active.ID).Error)
	require.Equal(t, model.ContainerSessionRevoked, revoked.Status)
	require.Equal(t, "用户离职", revoked.CloseReason)
	require.NotNil(t, revoked.EndedAt)

	repeatResp := httptest.NewRecorder()
	r.ServeHTTP(repeatResp, httptest.NewRequest(http.MethodPost, "/sessions/"+active.ID+"/revoke", nil))
	require.Equal(t, http.StatusConflict, repeatResp.Code)

	forceResp := httptest.NewRecorder()
	r.ServeHTTP(forceResp, httptest.NewRequest(http.MethodPost, "/sessions/"+second.ID+"/force-disconnect", nil))
	require.Equal(t, http.StatusOK, forceResp.Code)
	require.NoError(t, testDB.First(&second, "id = ?", second.ID).Error)
	require.Equal(t, model.ContainerSessionRevoked, second.Status)
	require.Equal(t, "管理员强制断开会话", second.CloseReason)

	legacyNode := model.Node{UserID: 998, Name: "legacy-agent", Type: model.NodeTypeAgent}
	require.NoError(t, testDB.Create(&legacyNode).Error)
	legacySession := model.ContainerSession{ID: uuid.NewString(), TenantID: tenant.ID, UserID: user.ID, ResourceID: resource.ID, AgentNodeID: legacyNode.ID, Status: model.ContainerSessionActive, StartedAt: time.Now()}
	require.NoError(t, testDB.Create(&legacySession).Error)
	legacyResp := httptest.NewRecorder()
	r.ServeHTTP(legacyResp, httptest.NewRequest(http.MethodPost, "/sessions/"+legacySession.ID+"/force-disconnect", nil))
	require.Equal(t, http.StatusConflict, legacyResp.Code)
	require.NoError(t, testDB.First(&legacySession, "id = ?", legacySession.ID).Error)
	require.Equal(t, model.ContainerSessionActive, legacySession.Status)

	var auditCount int64
	require.NoError(t, testDB.Model(&model.AuditLog{}).Where("target_type = ?", "container_session").Count(&auditCount).Error)
	require.Equal(t, int64(2), auditCount)
}
