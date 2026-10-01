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
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
	"google.golang.org/protobuf/proto"
)

func setupTunnelTestDB(t *testing.T) *gorm.DB {
	database, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", uuid.NewString())), &gorm.Config{IgnoreRelationshipsWhenMigrating: true})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(
		&model.Admin{}, &model.User{}, &model.Node{}, &model.DeployToken{},
		&model.TechnicalResource{}, &model.TechnicalResourceDeployToken{},
		&model.Tenant{}, &model.TenantResource{}, &model.TenantAccessGrant{}, &model.TenantAccessGrantEvent{},
	))
	previous := serverdb.DB
	serverdb.DB = database
	t.Cleanup(func() { serverdb.DB = previous })
	return database
}

// TestTunnelDeployTokenCreation verifies creating a DeployToken in tunnel mode with TargetAgentName
func TestTunnelDeployTokenCreation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := setupTunnelTestDB(t)

	admin := model.Admin{Username: "superadmin", Role: "superadmin"}
	require.NoError(t, database.Create(&admin).Error)

	clientUser := model.User{Name: "svc-tunnel-5090", Role: model.UserRoleClient, SecretHash: "hash", Enabled: true}
	require.NoError(t, database.Create(&clientUser).Error)

	deployAPI := NewDeployAPI(&config.ServerConfig{})

	// 1. Create Deploy Token with mode: "tunnel" and target_agent_name: "edge-gpu-5090"
	createReq := CreateDeployTokenRequest{
		Name:            "signal-tunnel-5090",
		TargetAgentName: "edge-gpu-5090",
		Mode:            "tunnel",
	}
	body, _ := json.Marshal(createReq)
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/users/%d/deploy-token", clientUser.ID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", clientUser.ID)}}
	c.Set("admin_id", admin.ID)

	deployAPI.CreateDeployToken(c)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp struct {
		Code int                       `json:"code"`
		Data CreateDeployTokenResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "signal-tunnel-5090", resp.Data.Name)
	require.Equal(t, "edge-gpu-5090", resp.Data.TargetAgentName)
	require.Equal(t, "tunnel", resp.Data.Mode)
	require.Contains(t, resp.Data.K8sDeployYaml, "SIGNAL_TARGET_AGENT")
	require.Contains(t, resp.Data.K8sDeployYaml, "edge-gpu-5090")
	require.Contains(t, resp.Data.K8sDeployYaml, "run-tunnel")

	// 2. Query Deploy Token in DB
	var dbToken model.DeployToken
	require.NoError(t, database.Where("token = ?", resp.Data.Token).First(&dbToken).Error)
	require.Equal(t, "edge-gpu-5090", dbToken.TargetAgentName)
	require.Equal(t, "tunnel", dbToken.Mode)

	// 3. List Deploy Tokens
	listReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/users/%d/deploy-tokens", clientUser.ID), nil)
	listW := httptest.NewRecorder()
	listCtx, _ := gin.CreateTestContext(listW)
	listCtx.Request = listReq
	listCtx.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", clientUser.ID)}}
	listCtx.Set("admin_id", admin.ID)

	deployAPI.ListDeployTokens(listCtx)
	require.Equal(t, http.StatusOK, listW.Code)

	var listResp struct {
		Success bool                  `json:"success"`
		Data    []DeployTokenListItem `json:"data"`
		Total   int64                 `json:"total"`
	}
	require.NoError(t, json.Unmarshal(listW.Body.Bytes(), &listResp))
	require.Equal(t, int64(1), listResp.Total)
	require.Equal(t, "edge-gpu-5090", listResp.Data[0].TargetAgentName)
	require.Equal(t, "tunnel", listResp.Data[0].Mode)
}

// TestTunnelRegister_AntiMistake verifies the bidirectional anti-mistake check during Tunnel registration
func TestTunnelRegister_AntiMistake(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := setupTunnelTestDB(t)

	clientUser := model.User{Name: "svc-tunnel-5090", Role: model.UserRoleClient, SecretHash: "hash", Enabled: true}
	require.NoError(t, database.Create(&clientUser).Error)

	deployToken := model.DeployToken{
		Token:           "test-tunnel-token-5090",
		UserID:          clientUser.ID,
		Name:            "signal-tunnel-5090",
		Status:          model.DeployTokenStatusPending,
		CreatedBy:       1,
		TargetAgentName: "edge-gpu-5090",
		Mode:            "tunnel",
	}
	require.NoError(t, database.Create(&deployToken).Error)

	deployAPI := NewDeployAPI(&config.ServerConfig{})

	registerCall := func(rawToken, targetAgent string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(RegisterRequest{
			Token:             rawToken,
			DeviceFingerprint: "fingerprint-tunnel-pod-1",
			TargetAgent:       targetAgent,
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/register", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		deployAPI.Register(c)
		return w
	}

	// 1. Missing target_agent in request when token has TargetAgentName configured: returns 400
	missingResp := registerCall(deployToken.Token, "")
	require.Equal(t, http.StatusBadRequest, missingResp.Code)
	require.Contains(t, missingResp.Body.String(), "缺少声明的目标 Agent")

	// 2. Mismatched target_agent in request (declared edge-gpu-4090 instead of edge-gpu-5090): returns 403 Forbidden
	mismatchResp := registerCall(deployToken.Token, "edge-gpu-4090")
	require.Equal(t, http.StatusForbidden, mismatchResp.Code)
	require.Contains(t, mismatchResp.Body.String(), "目标 Agent 校验失败！客户端声明: edge-gpu-4090, 服务端权威绑定: edge-gpu-5090")

	// 3. Matched target_agent in request (declared edge-gpu-5090): returns 200 OK
	matchResp := registerCall(deployToken.Token, "edge-gpu-5090")
	require.Equal(t, http.StatusOK, matchResp.Code, matchResp.Body.String())

	// Token state updated to bound
	require.NoError(t, database.Where("id = ?", deployToken.ID).First(&deployToken).Error)
	require.Equal(t, model.DeployTokenStatusBound, deployToken.Status)
}

// TestTunnel_TenantAccessGrant_LocalPort verifies that local_port and allow_k8s_api fields
// persist correctly in the database and are properly decoded.
func TestTunnel_TenantAccessGrant_LocalPort(t *testing.T) {
	database := setupTunnelTestDB(t)

	now := time.Now().UTC()
	userID := uint64(1001)
	grant := model.TenantAccessGrant{
		ID:                uuid.NewString(),
		TenantID:          uuid.NewString(),
		TenantResourceID:  uuid.NewString(),
		SubjectType:       model.TenantAccessGrantSubjectUser,
		SubjectKey:        "1001",
		SubjectUserID:     &userID,
		Actions:           `["connect"]`,
		ValidFrom:         now,
		MaxSessionSeconds: 3600,
		Status:            model.TenantAccessGrantEnabled,
		Revision:          1,
		RowVersion:        1,
		CreatedByUserID:   1,
		LocalPort:         10080,
		AllowK8sAPI:       true,
	}

	require.NoError(t, database.Create(&grant).Error)

	var retrieved model.TenantAccessGrant
	require.NoError(t, database.Where("id = ?", grant.ID).First(&retrieved).Error)
	require.Equal(t, int32(10080), retrieved.LocalPort)
	require.True(t, retrieved.AllowK8sAPI)

	// Update local_port to 16443 and allow_k8s_api
	require.NoError(t, database.Model(&retrieved).Updates(map[string]any{
		"local_port":    16443,
		"allow_k8s_api": false,
	}).Error)

	var updated model.TenantAccessGrant
	require.NoError(t, database.Where("id = ?", grant.ID).First(&updated).Error)
	require.Equal(t, int32(16443), updated.LocalPort)
	require.False(t, updated.AllowK8sAPI)
}

// TestTunnel_ContainerServiceResourceProto verifies that ContainerServiceResource
// carries LocalPort (field 21) and AgentName (field 22) correctly over protobuf.
func TestTunnel_ContainerServiceResourceProto(t *testing.T) {
	original := &pb.ContainerServiceResource{
		ResourceId:  "res-1",
		TenantId:    "tenant-1",
		DisplayName: "mcp-service",
		ServiceName: "mcp-service",
		PortNumber:  8000,
		LocalPort:   10080,
		AgentName:   "edge-gpu-5090",
	}

	data, err := proto.Marshal(original)
	require.NoError(t, err)

	var decoded pb.ContainerServiceResource
	require.NoError(t, proto.Unmarshal(data, &decoded))

	require.Equal(t, int32(10080), decoded.LocalPort)
	require.Equal(t, "edge-gpu-5090", decoded.AgentName)
	require.Equal(t, "mcp-service", decoded.ServiceName)
	require.Equal(t, int32(8000), decoded.PortNumber)
}

// TestSignalTunnelAPI_CRUD tests the full lifecycle of Signal Tunnel REST endpoints
func TestSignalTunnelAPI_CRUD(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := setupTunnelTestDB(t)

	admin := model.Admin{Username: "superadmin", Role: "superadmin"}
	require.NoError(t, database.Create(&admin).Error)

	tunnelAPI := NewTunnelAPI(&config.ServerConfig{})

	router := gin.New()
	adminGroup := router.Group("/api/v1/admin")
	adminGroup.Use(func(c *gin.Context) {
		c.Set("admin_id", int64(admin.ID))
		c.Next()
	})
	{
		adminGroup.GET("/tunnels", tunnelAPI.ListSignalTunnels)
		adminGroup.POST("/tunnels", tunnelAPI.CreateSignalTunnel)
		adminGroup.GET("/tunnels/available-agents", tunnelAPI.GetAvailableAgents)
		adminGroup.GET("/tunnels/:id", tunnelAPI.GetSignalTunnel)
		adminGroup.PUT("/tunnels/:id/ports", tunnelAPI.UpdateSignalTunnelPorts)
		adminGroup.DELETE("/tunnels/:id", tunnelAPI.DeleteSignalTunnel)
	}

	// 1. Initial List (should be empty, HTTP 200)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/tunnels", nil)
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var listResp PagedResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &listResp))
	require.True(t, listResp.Success)
	require.Equal(t, int64(0), listResp.Total)

	// 2. Get Available Agents (HTTP 200, returns defaults or registered)
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/tunnels/available-agents", nil)
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// 3. Create Tunnel (POST /api/v1/admin/tunnels)
	createPayload := CreateSignalTunnelRequest{
		Name:        "signal-tunnel-test-5090",
		TargetAgent: "edge-gpu-5090",
	}
	body, _ := json.Marshal(createPayload)
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/tunnels", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var createResp Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &createResp))
	require.True(t, createResp.Success)

	dataMap := createResp.Data.(map[string]interface{})
	tunnelID := uint64(dataMap["id"].(float64))
	require.NotEmpty(t, dataMap["token"])
	require.Contains(t, dataMap["k8s_deploy_yaml"].(string), "run-tunnel")

	// 4. Query List again (should have 1 item)
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/tunnels", nil)
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var listResp2 PagedResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &listResp2))
	require.Equal(t, int64(1), listResp2.Total)

	// 5. Get Tunnel Detail (GET /api/v1/admin/tunnels/:id)
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/admin/tunnels/%d", tunnelID), nil)
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var detailResp Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &detailResp))
	require.True(t, detailResp.Success)
	detailMap := detailResp.Data.(map[string]interface{})
	require.Equal(t, "signal-tunnel-test-5090", detailMap["name"])
	require.Equal(t, "edge-gpu-5090", detailMap["target_agent"])

	// 6. Update Tunnel Ports (PUT /api/v1/admin/tunnels/:id/ports)
	updatePayload := UpdateSignalTunnelPortsRequest{
		K8sAPIEnabled: true,
		K8sAPIPort:    16443,
		Ports: []SignalTunnelPortMapping{
			{
				ResourceID:  "res-mcp-1",
				ServiceName: "mcp-service",
				TargetPort:  8000,
				Protocol:    "TCP",
				LocalPort:   10080,
			},
		},
	}
	body, _ = json.Marshal(updatePayload)
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/v1/admin/tunnels/%d/ports", tunnelID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// 7. Delete Tunnel (DELETE /api/v1/admin/tunnels/:id)
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/v1/admin/tunnels/%d", tunnelID), nil)
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// Verify status is revoked
	var tok model.DeployToken
	require.NoError(t, database.First(&tok, tunnelID).Error)
	require.Equal(t, model.DeployTokenStatusRevoked, tok.Status)
}



