package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
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

func TestLegacyResourceClaimIsExplicitAndDoesNotGrantAccess(t *testing.T) {
	oldDB := db.DB
	t.Cleanup(func() { db.DB = oldDB })
	database, err := gorm.Open(sqlite.Open("file:legacy_resource_claim_test?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	db.DB = database
	require.NoError(t, database.AutoMigrate(
		&model.Admin{}, &model.AdminTenantMembership{}, &model.Tenant{}, &model.User{}, &model.Node{}, &model.Endpoint{},
		&model.LegacyResourceClaim{}, &model.Resource{}, &model.AccessGrant{}, &model.AuditLog{},
	))
	admin := model.Admin{Username: "legacy-claim-admin", PasswordHash: "test", Role: "admin"}
	require.NoError(t, database.Create(&admin).Error)
	tenantA := model.Tenant{ID: uuid.NewString(), Key: "legacy-a", Name: "Legacy A", Status: model.TenantStatusActive}
	tenantB := model.Tenant{ID: uuid.NewString(), Key: "legacy-b", Name: "Legacy B", Status: model.TenantStatusActive}
	require.NoError(t, database.Create(&tenantA).Error)
	require.NoError(t, database.Create(&tenantB).Error)
	agentUser := model.User{Name: "legacy-agent", Role: model.UserRoleAgent, SecretHash: "test", Enabled: true}
	require.NoError(t, database.Create(&agentUser).Error)
	now := time.Now()
	node := model.Node{UserID: agentUser.ID, Name: "legacy-node", Type: model.NodeTypeAgent, LastHeartbeat: &now}
	require.NoError(t, database.Create(&node).Error)
	endpoint := model.Endpoint{ID: uuid.NewString(), UserID: agentUser.ID, Name: "legacy-endpoint", Status: "online"}
	require.NoError(t, database.Create(&endpoint).Error)

	api := NewLegacyResourceClaimAPI()
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("admin_id", admin.ID) })
	router.GET("/claims", api.List)
	router.POST("/claims", api.Claim)
	router.POST("/claims/:id/revoke", api.Revoke)
	post := func(path string, payload interface{}) *httptest.ResponseRecorder {
		body, marshalErr := json.Marshal(payload)
		require.NoError(t, marshalErr)
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)
		return resp
	}
	payload := map[string]interface{}{"source_type": model.LegacySourceAgentNode, "source_id": strconv.FormatUint(node.ID, 10), "tenant_id": tenantA.ID, "reason": "管理员确认资产归属"}
	created := post("/claims", payload)
	require.Equal(t, http.StatusCreated, created.Code)
	var createdBody struct {
		Data model.LegacyResourceClaim `json:"data"`
	}
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &createdBody))
	require.Equal(t, tenantA.ID, createdBody.Data.TenantID)

	require.Equal(t, http.StatusOK, post("/claims", payload).Code)
	payload["tenant_id"] = tenantB.ID
	require.Equal(t, http.StatusConflict, post("/claims", payload).Code)
	require.Equal(t, http.StatusOK, post("/claims/"+createdBody.Data.ID+"/revoke", map[string]interface{}{}).Code)
	require.Equal(t, http.StatusOK, post("/claims", payload).Code)

	endpointPayload := map[string]interface{}{"source_type": model.LegacySourceEndpoint, "source_id": endpoint.ID, "tenant_id": tenantA.ID, "reason": "管理员确认 Endpoint 归属"}
	require.Equal(t, http.StatusCreated, post("/claims", endpointPayload).Code)

	listReq := httptest.NewRequest(http.MethodGet, "/claims?status=active", nil)
	listResp := httptest.NewRecorder()
	router.ServeHTTP(listResp, listReq)
	require.Equal(t, http.StatusOK, listResp.Code)
	var listBody struct {
		Total int64 `json:"total"`
	}
	require.NoError(t, json.Unmarshal(listResp.Body.Bytes(), &listBody))
	require.Equal(t, int64(2), listBody.Total)

	var resources, grants int64
	require.NoError(t, database.Model(&model.Resource{}).Count(&resources).Error)
	require.NoError(t, database.Model(&model.AccessGrant{}).Count(&grants).Error)
	require.Zero(t, resources)
	require.Zero(t, grants)
	var adminMemberships int64
	require.NoError(t, database.Model(&model.AdminTenantMembership{}).Where("admin_id = ?", admin.ID).Count(&adminMemberships).Error)
	require.Zero(t, adminMemberships)
	var auditCount int64
	require.NoError(t, database.Model(&model.AuditLog{}).Where("target_type = ?", "legacy_resource_claim").Count(&auditCount).Error)
	require.Equal(t, int64(4), auditCount)
}
