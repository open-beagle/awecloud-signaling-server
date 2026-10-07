package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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
	"github.com/open-beagle/awecloud-signaling-server/internal/server/service"
)

type testMockACLSyncer struct {
	syncCount int
	syncErr   error
}

func (m *testMockACLSyncer) FullSync(ctx context.Context) error {
	m.syncCount++
	return m.syncErr
}

func setupTunnelLifecycleTestDB(t *testing.T) *gorm.DB {
	database, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", uuid.NewString())), &gorm.Config{IgnoreRelationshipsWhenMigrating: true})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(
		&model.Admin{}, &model.User{}, &model.Node{}, &model.DeployToken{},
		&model.TechnicalResource{}, &model.TechnicalResourceDeployToken{},
		&model.Tenant{}, &model.TenantResource{}, &model.TenantMembership{}, &model.TenantAccessGrant{}, &model.TenantAccessGrantEvent{},
		&model.TechnicalResourceBinding{}, &model.WorkloadObservation{},
		&model.WorkloadObservationSource{}, &model.ResourceScope{}, &model.NamespaceObservation{},
		&model.SystemConfig{}, &model.AuditLog{},
	))
	previous := serverdb.DB
	serverdb.DB = database
	t.Cleanup(func() { serverdb.DB = previous })
	return database
}

func setupTestNamespaceScope(t *testing.T, database *gorm.DB, nsName, nsUID string) (model.NamespaceObservation, model.ResourceScope) {
	now := time.Now()
	providerID := uuid.NewString()
	platformID := uuid.NewString()

	nsObs := model.NamespaceObservation{
		ID:                uuid.NewString(),
		ProviderID:        providerID,
		ClusterResourceID: platformID,
		Name:              nsName,
		NamespaceUID:      nsUID,
		Revision:          1,
		ObservedAt:        now,
		LeaseExpiresAt:    now.Add(10 * time.Hour),
		State:             model.NamespaceObservationObserved,
	}
	require.NoError(t, database.Create(&nsObs).Error)

	clusterScope := model.ResourceScope{
		ID:                 uuid.NewString(),
		ProviderID:         providerID,
		PlatformResourceID: platformID,
		Type:               model.ResourceScopeCluster,
		StableKey:          strings.Repeat("1", 64),
		LifecycleState:     model.ResourceScopeActive,
		IsolationMode:      model.ResourceScopeIsolationNone,
		ConfigRevision:     1,
		EvidenceRevision:   1,
		RowVersion:         1,
	}
	require.NoError(t, database.Create(&clusterScope).Error)

	namespaceScope := model.ResourceScope{
		ID:                     uuid.NewString(),
		ProviderID:             providerID,
		PlatformResourceID:     platformID,
		Type:                   model.ResourceScopeNamespace,
		StableKey:              strings.Repeat("2", 64),
		ParentID:               &clusterScope.ID,
		NamespaceObservationID: &nsObs.ID,
		LifecycleState:         model.ResourceScopeAllocatable,
		IsolationMode:          model.ResourceScopeIsolationNamespaceIsolated,
		ConfigRevision:         1,
		EvidenceRevision:       1,
		RowVersion:             1,
	}
	require.NoError(t, database.Create(&namespaceScope).Error)

	return nsObs, namespaceScope
}

func createTestWorkloadObservation(t *testing.T, database *gorm.DB, techResID string, scopeID string, obsID string, svcUID, svcName, portName, protocol string, portNum int, ready bool) {
	now := time.Now()
	stableKey := fmt.Sprintf("%x", sha256.Sum256([]byte(obsID+svcUID)))
	obs := model.WorkloadObservation{
		ID:               obsID,
		NamespaceScopeID: scopeID,
		Kind:             model.WorkloadObservationServicePort,
		StableKey:        stableKey,
		IdentityQuality:  model.WorkloadIdentityStrong,
		State:            model.WorkloadObservationEligible,
		Ready:            ready,
		ObservedRevision: 1,
		LabelSnapshot:    `{}`,
		FirstObservedAt:  now,
		LastObservedAt:   now,
		LeaseExpiresAt:   now.Add(10 * time.Hour),
		RowVersion:       1,
	}
	require.NoError(t, database.Create(&obs).Error)

	snapshotData, _ := json.Marshal(map[string]interface{}{
		"service_uid":  svcUID,
		"service_name": svcName,
		"port_name":    portName,
		"protocol":     protocol,
		"port_number":  portNum,
	})

	obsSource := model.WorkloadObservationSource{
		ID:                        uuid.NewString(),
		WorkloadObservationID:     obs.ID,
		SourceTechnicalResourceID: techResID,
		SourceEpoch:               uuid.NewString(),
		Sequence:                  1,
		PayloadHash:               strings.Repeat("4", 64),
		State:                     model.WorkloadObservationSourceObserved,
		Ready:                     ready,
		TargetSnapshot:            string(snapshotData),
		ObservedAt:                now,
		ReceivedAt:                now,
		LeaseExpiresAt:            now.Add(10 * time.Hour),
		SourceRevision:            1,
		RowVersion:                1,
	}
	require.NoError(t, database.Create(&obsSource).Error)
}

// TestTunnelLifecycle_T3_ACLSyncVerifications 验证生命周期（创建、更新端口、注销）即时触发 FullSync，且失败返回 500
func TestTunnelLifecycle_T3_ACLSyncVerifications(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := setupTunnelLifecycleTestDB(t)

	admin := model.Admin{Username: "superadmin", Role: "superadmin"}
	require.NoError(t, database.Create(&admin).Error)

	clientUser := model.User{Name: "svc-tunnel-lifecycle", Role: model.UserRoleClient, SecretHash: "hash", Enabled: true}
	require.NoError(t, database.Create(&clientUser).Error)

	agentNode := model.Node{
		ID:        1001,
		Name:      "edge-gpu-lifecycle",
		Type:      model.NodeTypeAgent,
		IP:        "10.0.0.100",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	require.NoError(t, database.Create(&agentNode).Error)

	techRes := model.TechnicalResource{
		ID:             uuid.NewString(),
		ProviderID:     uuid.NewString(),
		Type:           model.TechnicalResourceAgent,
		StableKey:      "tech-edge-stable",
		DomainLabel:    "edge-lifecycle-label",
		LifecycleState: model.TechnicalResourceRegistered,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	require.NoError(t, database.Create(&techRes).Error)

	resBinding := model.TechnicalResourceBinding{
		ID:                  uuid.NewString(),
		SourceType:          model.TechnicalResourceBindingLegacyNode,
		SourceID:            fmt.Sprint(agentNode.ID),
		TechnicalResourceID: techRes.ID,
		CredentialRevision:  1,
		Enabled:             true,
		CreatedAt:           time.Now(),
		UpdatedAt:           time.Now(),
	}
	require.NoError(t, database.Create(&resBinding).Error)

	_, scope := setupTestNamespaceScope(t, database, "prod-apps", "ns-uid-prod-123")
	createTestWorkloadObservation(t, database, techRes.ID, scope.ID, "obs-svc-http", "svc-uid-8080", "web-api", "http", "TCP", 8080, true)

	tunnelAPI := NewTunnelAPI(&config.ServerConfig{})
	mockSyncer := &testMockACLSyncer{}
	tunnelAPI.SetACLSyncer(mockSyncer)

	// 1. 测试 CreateSignalTunnel 成功触发 FullSync
	createReq := CreateSignalTunnelRequest{
		Name:        "tunnel-lifecycletest",
		TargetAgent: "edge-gpu-lifecycle",
	}
	body, _ := json.Marshal(createReq)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/tunnels", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set("admin_id", admin.ID)

	tunnelAPI.CreateSignalTunnel(c)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, 1, mockSyncer.syncCount, "CreateSignalTunnel 应调用 1 次 FullSync")

	var createResp struct {
		Code int                        `json:"code"`
		Data CreateSignalTunnelResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &createResp))
	tunnelID := createResp.Data.ID

	// 2. 测试 UpdateSignalTunnelPorts 成功触发 FullSync
	updateReq := UpdateSignalTunnelPortsRequest{
		Ports: []SignalTunnelPortMapping{
			{
				ResourceID: "obs-svc-http",
				LocalPort:  18080,
			},
		},
		K8sAPIEnabled: true,
		K8sAPIPort:    16443,
	}
	body, _ = json.Marshal(updateReq)
	req = httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/v1/admin/tunnels/%d/ports", tunnelID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = req
	c.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", tunnelID)}}
	c.Set("admin_id", admin.ID)

	tunnelAPI.UpdateSignalTunnelPorts(c)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, 2, mockSyncer.syncCount, "UpdateSignalTunnelPorts 应累计调用 2 次 FullSync")

	// 3. 测试 DeleteSignalTunnel 成功触发 FullSync
	req = httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/v1/admin/tunnels/%d", tunnelID), nil)
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = req
	c.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", tunnelID)}}
	c.Set("admin_id", admin.ID)

	tunnelAPI.DeleteSignalTunnel(c)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, 3, mockSyncer.syncCount, "DeleteSignalTunnel 应累计调用 3 次 FullSync")

	// 4. 测试同步失败时返回 HTTP 500
	mockSyncer.syncErr = errors.New("headscale network timeout")

	// 4a. CreateSignalTunnel 遇到同步失败
	createReq2 := CreateSignalTunnelRequest{
		Name:        "tunnel-fail-sync",
		TargetAgent: "edge-gpu-lifecycle",
	}
	body, _ = json.Marshal(createReq2)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/tunnels", bytes.NewReader(body))
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = req
	c.Set("admin_id", admin.ID)

	tunnelAPI.CreateSignalTunnel(c)
	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.Contains(t, w.Body.String(), "ACL 同步失败")

	// 4b. UpdateSignalTunnelPorts 遇到同步失败
	body, _ = json.Marshal(updateReq)
	req = httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/v1/admin/tunnels/%d/ports", tunnelID), bytes.NewReader(body))
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = req
	c.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", tunnelID)}}
	c.Set("admin_id", admin.ID)

	tunnelAPI.UpdateSignalTunnelPorts(c)
	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.Contains(t, w.Body.String(), "ACL 同步失败")

	// 4c. DeleteSignalTunnel 遇到同步失败
	req = httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/v1/admin/tunnels/%d", tunnelID), nil)
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = req
	c.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", tunnelID)}}
	c.Set("admin_id", admin.ID)

	tunnelAPI.DeleteSignalTunnel(c)
	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.Contains(t, w.Body.String(), "ACL 同步失败")

	var audits []model.AuditLog
	require.NoError(t, database.Order("id").Find(&audits).Error)
	require.Len(t, audits, 6)
	for i, entry := range audits {
		var detail sensitiveWriteDetail
		require.NoError(t, json.Unmarshal([]byte(entry.Detail), &detail))
		require.True(t, detail.DatabaseCommitted)
		require.NotEmpty(t, entry.TargetID)
		require.Equal(t, admin.ID, entry.ActorAdminID)
		require.NotContains(t, entry.Detail, createResp.Data.Token)
		require.NotContains(t, entry.Detail, "SIGNAL_DEPLOY_TOKEN")
		require.NotContains(t, entry.Detail, "secret_hash")
		require.NotNil(t, detail.After)
		if i < 3 {
			require.Equal(t, "succeeded", detail.Result)
			require.Equal(t, "succeeded", detail.SyncStatus)
		} else {
			require.Equal(t, "partial", detail.Result)
			require.Equal(t, "failed", detail.SyncStatus)
			require.Equal(t, 500, detail.HTTPStatus)
		}
	}
}

// TestTunnelCandidates_T4_FilterAndFields 验证候选服务 API 严格过滤非 TCP 服务，且元数据字段与 inventory 一致
func TestTunnelCandidates_T4_FilterAndFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := setupTunnelLifecycleTestDB(t)

	admin := model.Admin{Username: "superadmin", Role: "superadmin"}
	require.NoError(t, database.Create(&admin).Error)

	agentNode := model.Node{
		ID:        2001,
		Name:      "edge-k8s-cluster",
		Type:      model.NodeTypeAgent,
		IP:        "10.0.0.200",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	require.NoError(t, database.Create(&agentNode).Error)

	techRes := model.TechnicalResource{
		ID:             uuid.NewString(),
		ProviderID:     uuid.NewString(),
		Type:           model.TechnicalResourceAgent,
		StableKey:      "tech-k8s-stable",
		DomainLabel:    "edge-k8s-label",
		LifecycleState: model.TechnicalResourceRegistered,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	require.NoError(t, database.Create(&techRes).Error)

	resBinding := model.TechnicalResourceBinding{
		ID:                  uuid.NewString(),
		SourceType:          model.TechnicalResourceBindingLegacyNode,
		SourceID:            fmt.Sprint(agentNode.ID),
		TechnicalResourceID: techRes.ID,
		CredentialRevision:  1,
		Enabled:             true,
		CreatedAt:           time.Now(),
		UpdatedAt:           time.Now(),
	}
	require.NoError(t, database.Create(&resBinding).Error)

	_, scope := setupTestNamespaceScope(t, database, "beagle-ai", "ns-uid-beagle-ai-777")

	// 1. TCP 服务: mcp-service (端口 8000) -> 应该出现在候选集
	createTestWorkloadObservation(t, database, techRes.ID, scope.ID, "obs-mcp-8000", "svc-uid-mcp-1", "mcp-service", "http-api", "TCP", 8000, true)

	// 2. UDP 服务: kube-dns (端口 53) -> 必须被过滤掉！
	createTestWorkloadObservation(t, database, techRes.ID, scope.ID, "obs-dns-udp", "svc-uid-dns-udp", "kube-dns", "dns", "UDP", 53, true)

	// 3. 非法的端口 (port_number <= 0) -> 必须被过滤掉！
	createTestWorkloadObservation(t, database, techRes.ID, scope.ID, "obs-invalid-port", "svc-uid-invalid", "invalid-svc", "tcp", "TCP", 0, true)

	// 4. 创建对应的 Tunnel DeployToken
	tok := model.DeployToken{
		Name:            "tunnel-candidate-test",
		TargetAgentName: "edge-k8s-cluster",
		Mode:            "tunnel",
		Status:          model.DeployTokenStatusBound,
		Token:           "test-candidate-token-val",
	}
	require.NoError(t, database.Create(&tok).Error)

	tunnelAPI := NewTunnelAPI(&config.ServerConfig{})
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/admin/tunnels/%d/candidate-services", tok.ID), nil)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", tok.ID)}}
	c.Set("admin_id", admin.ID)

	tunnelAPI.GetTunnelCandidateServices(c)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp struct {
		Code int                              `json:"code"`
		Data []service.TunnelCandidateService `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	// 断言：只有 1 个候选（即 TCP 的 mcp-service），UDP 和 port=0 均被剔除
	require.Len(t, resp.Data, 1)
	cand := resp.Data[0]
	require.Equal(t, "obs-mcp-8000", cand.ResourceID)
	require.Equal(t, "beagle-ai", cand.Namespace)
	require.Equal(t, "ns-uid-beagle-ai-777", cand.NamespaceUID)
	require.Equal(t, "mcp-service", cand.ServiceName)
	require.Equal(t, "svc-uid-mcp-1", cand.ServiceUID)
	require.Equal(t, "http-api", cand.PortName)
	require.Equal(t, 8000, cand.PortNumber)
	require.Equal(t, "TCP", cand.Protocol)
	require.True(t, cand.Ready)

	// SQLite stores offsets in timestamp text. Candidate leases must be compared
	// as instants, regardless of the writer's or API caller's time zone.
	createTestWorkloadObservation(t, database, techRes.ID, scope.ID, "obs-expired", "svc-expired", "expired", "http", "TCP", 8080, true)
	now := time.Date(2026, 10, 7, 11, 0, 0, 0, time.UTC)
	utcPlus8 := time.FixedZone("UTC+8", 8*60*60)
	utcMinus7 := time.FixedZone("UTC-7", -7*60*60)
	for _, test := range []struct {
		name    string
		written *time.Location
		queried *time.Location
	}{
		{"UTC_written_UTC_plus_8_query", time.UTC, utcPlus8},
		{"UTC_plus_8_written_UTC_query", utcPlus8, time.UTC},
		{"UTC_written_UTC_minus_7_query", time.UTC, utcMinus7},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.NoError(t, database.Model(&model.WorkloadObservationSource{}).
				Where("workload_observation_id = ?", "obs-mcp-8000").
				Updates(map[string]any{"received_at": now.Add(-10 * time.Minute).In(test.written), "lease_expires_at": now.Add(5 * time.Minute).In(test.written)}).Error)
			require.NoError(t, database.Model(&model.WorkloadObservationSource{}).
				Where("workload_observation_id = ?", "obs-expired").
				Updates(map[string]any{"received_at": now.Add(-10 * time.Minute).In(test.written), "lease_expires_at": now.Add(-time.Minute).In(test.written)}).Error)
			candidates, err := service.QueryTunnelCandidateServices(context.Background(), database, agentNode.Name, now.In(test.queried))
			require.NoError(t, err)
			require.Len(t, candidates, 1)
			require.Equal(t, "obs-mcp-8000", candidates[0].ResourceID)
		})
	}
}

// TestTunnelPortsUpdate_T5_ValidationAndPersistence 验证 UpdateSignalTunnelPorts 的校验拦截与真实持久化
func TestTunnelPortsUpdate_T5_ValidationAndPersistence(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := setupTunnelLifecycleTestDB(t)

	admin := model.Admin{Username: "superadmin", Role: "superadmin"}
	require.NoError(t, database.Create(&admin).Error)

	agentNode := model.Node{
		ID:        3001,
		Name:      "edge-gpu-t5",
		Type:      model.NodeTypeAgent,
		IP:        "10.0.0.50",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	require.NoError(t, database.Create(&agentNode).Error)

	techRes := model.TechnicalResource{
		ID:             uuid.NewString(),
		ProviderID:     uuid.NewString(),
		Type:           model.TechnicalResourceAgent,
		StableKey:      "tech-t5-stable",
		DomainLabel:    "edge-t5-label",
		LifecycleState: model.TechnicalResourceRegistered,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	require.NoError(t, database.Create(&techRes).Error)

	resBinding := model.TechnicalResourceBinding{
		ID:                  uuid.NewString(),
		SourceType:          model.TechnicalResourceBindingLegacyNode,
		SourceID:            fmt.Sprint(agentNode.ID),
		TechnicalResourceID: techRes.ID,
		CredentialRevision:  1,
		Enabled:             true,
		CreatedAt:           time.Now(),
		UpdatedAt:           time.Now(),
	}
	require.NoError(t, database.Create(&resBinding).Error)

	_, scope := setupTestNamespaceScope(t, database, "data-mesh", "ns-uid-datamesh-999")
	createTestWorkloadObservation(t, database, techRes.ID, scope.ID, "obs-valid-service-1", "svc-uid-real-1", "real-app-svc", "grpc-port", "TCP", 9000, true)

	// 创建 Tunnel DeployToken
	tok := model.DeployToken{
		Name:            "tunnel-t5-test",
		TargetAgentName: "edge-gpu-t5",
		Mode:            "tunnel",
		Status:          model.DeployTokenStatusBound,
		Token:           "test-t5-token-val",
	}
	require.NoError(t, database.Create(&tok).Error)

	tunnelAPI := NewTunnelAPI(&config.ServerConfig{})
	tunnelAPI.SetACLSyncer(&testMockACLSyncer{})

	// 1. 校验拦截：伪造未知的 resource_id -> 400
	{
		reqBody, _ := json.Marshal(UpdateSignalTunnelPortsRequest{
			Ports: []SignalTunnelPortMapping{
				{
					ResourceID: "fake-unknown-resource-id",
					LocalPort:  19000,
				},
			},
		})
		req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/v1/admin/tunnels/%d/ports", tok.ID), bytes.NewReader(reqBody))
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", tok.ID)}}
		c.Set("admin_id", admin.ID)

		tunnelAPI.UpdateSignalTunnelPorts(c)
		require.Equal(t, http.StatusBadRequest, w.Code)
		require.Contains(t, w.Body.String(), "候选服务不存在或已失效")
	}

	// 2. 校验拦截：本地端口小于 1024 (如 80) -> 400
	{
		reqBody, _ := json.Marshal(UpdateSignalTunnelPortsRequest{
			Ports: []SignalTunnelPortMapping{
				{
					ResourceID: "obs-valid-service-1",
					LocalPort:  80,
				},
			},
		})
		req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/v1/admin/tunnels/%d/ports", tok.ID), bytes.NewReader(reqBody))
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", tok.ID)}}
		c.Set("admin_id", admin.ID)

		tunnelAPI.UpdateSignalTunnelPorts(c)
		require.Equal(t, http.StatusBadRequest, w.Code)
		require.Contains(t, w.Body.String(), "1024-65535")
	}

	// 3. 校验拦截：容器服务之间本地端口重复 -> 400
	{
		reqBody, _ := json.Marshal(UpdateSignalTunnelPortsRequest{
			Ports: []SignalTunnelPortMapping{
				{
					ResourceID: "obs-valid-service-1",
					LocalPort:  19000,
				},
				{
					ResourceID: "obs-valid-service-1",
					LocalPort:  19000,
				},
			},
		})
		req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/v1/admin/tunnels/%d/ports", tok.ID), bytes.NewReader(reqBody))
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", tok.ID)}}
		c.Set("admin_id", admin.ID)

		tunnelAPI.UpdateSignalTunnelPorts(c)
		require.Equal(t, http.StatusBadRequest, w.Code)
		require.Contains(t, w.Body.String(), "本地端口重复或与 K8s API 冲突")
	}

	// 4. 校验拦截：容器服务的本地端口与 k8s_api_port 冲突 -> 400
	{
		reqBody, _ := json.Marshal(UpdateSignalTunnelPortsRequest{
			Ports: []SignalTunnelPortMapping{
				{
					ResourceID: "obs-valid-service-1",
					LocalPort:  16443,
				},
			},
			K8sAPIEnabled: true,
			K8sAPIPort:    16443,
		})
		req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/v1/admin/tunnels/%d/ports", tok.ID), bytes.NewReader(reqBody))
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", tok.ID)}}
		c.Set("admin_id", admin.ID)

		tunnelAPI.UpdateSignalTunnelPorts(c)
		require.Equal(t, http.StatusBadRequest, w.Code)
		require.Contains(t, w.Body.String(), "本地端口重复或与 K8s API 冲突")
	}

	// 5. 正常保存：反查完整元数据并写入 DB ports_config
	{
		reqBody, _ := json.Marshal(UpdateSignalTunnelPortsRequest{
			Ports: []SignalTunnelPortMapping{
				{
					ResourceID: "obs-valid-service-1",
					LocalPort:  19000,
					// 故意不传其他元数据（如 Namespace, ServiceName），测试后端根据候选列表自动反查填充
				},
			},
			K8sAPIEnabled: true,
			K8sAPIPort:    16443,
		})
		req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/v1/admin/tunnels/%d/ports", tok.ID), bytes.NewReader(reqBody))
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", tok.ID)}}
		c.Set("admin_id", admin.ID)

		tunnelAPI.UpdateSignalTunnelPorts(c)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		// 读取数据库中的 ports_config
		var updatedTok model.DeployToken
		require.NoError(t, database.First(&updatedTok, tok.ID).Error)

		parsedConfig, err := service.ParseTunnelPortsConfig(updatedTok.PortsConfig)
		require.NoError(t, err)
		require.True(t, parsedConfig.K8sAPIEnabled)
		require.Equal(t, 16443, parsedConfig.K8sAPIPort)
		require.Len(t, parsedConfig.Ports, 1)

		b := parsedConfig.Ports[0]
		require.True(t, b.Complete(), "保存后的条目必须满足 Complete() 完整性校验")
		require.Equal(t, "obs-valid-service-1", b.ResourceID)
		require.Equal(t, int32(19000), b.LocalPort)
		require.Equal(t, "data-mesh", b.Namespace)
		require.Equal(t, "ns-uid-datamesh-999", b.NamespaceUID)
		require.Equal(t, "real-app-svc", b.ServiceName)
		require.Equal(t, "svc-uid-real-1", b.ServiceUID)
		require.Equal(t, "grpc-port", b.PortName)
		require.Equal(t, int32(9000), b.PortNumber)
		require.Equal(t, "TCP", b.Protocol)
	}
}
