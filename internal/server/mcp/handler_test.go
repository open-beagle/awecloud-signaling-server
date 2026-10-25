package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/api"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/service"
)

type mcpTestFixture struct {
	database *gorm.DB
	config   *config.ServerConfig
	router   *gin.Engine
	admin    model.Admin
	agent    model.User
	client   model.User
	group    model.Group
	svc      model.ProxyService
	provider model.ResourceProvider
	tenant   model.Tenant
}

func newMCPTestFixture(t *testing.T) mcpTestFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	oldDB := db.DB
	t.Cleanup(func() { db.DB = oldDB })

	database, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared&_pragma=foreign_keys(1)", uuid.NewString())), &gorm.Config{})
	require.NoError(t, err)
	db.DB = database

	require.NoError(t, database.AutoMigrate(
		&model.Admin{},
		&model.AdminTenantMembership{},
		&model.User{},
		&model.UserIdentityProfile{},
		&model.UserAuthenticationLink{},
		&model.PlatformRoleMembership{},
		&model.ResourceProvider{},
		&model.AdminProviderMembership{},
		&model.Tenant{},
		&model.TenantMembership{},
		&model.UserTenantManagementMembership{},
		&model.Group{},
		&model.GroupMember{},
		&model.ProxyService{},
		&model.AclSSHUserPermission{},
		&model.AclSSHGroupPermission{},
		&model.AclK8SUserPermission{},
		&model.AclK8SGroupPermission{},
		&model.AclServiceUserPermission{},
		&model.AclServiceGroupPermission{},
		&model.AclGroupUserPermission{},
		&model.AclGroupGroupPermission{},
		&model.DeployToken{},
		&model.Node{},
		&model.Endpoint{},
		&model.TechnicalResource{},
		&model.TechnicalResourceBinding{},
		&model.PlatformResource{},
		&model.NamespaceObservation{},
		&model.ResourceScope{},
		&model.WorkloadObservation{},
		&model.WorkloadObservationSource{},
		&model.TenantAccessGrant{},
		&model.APIIdempotencyRecord{},
		&model.AuditLog{},
	))

	pwHash, err := bcrypt.GenerateFromPassword([]byte("admin-secret-pw"), bcrypt.MinCost)
	require.NoError(t, err)
	admin := model.Admin{Username: "admin", PasswordHash: string(pwHash), Role: "admin", Enabled: true}
	require.NoError(t, database.Create(&admin).Error)

	unifiedUser := model.User{Name: "admin-unified", Alias: "平台管理员", Role: model.UserRoleClient, SecretHash: "x", Enabled: true}
	require.NoError(t, database.Create(&unifiedUser).Error)
	require.NoError(t, database.Create(&model.UserIdentityProfile{
		UserID: unifiedUser.ID, Username: "admin", DisplayName: "Admin", Enabled: true, AuthRevision: 1, RowVersion: 1,
	}).Error)
	require.NoError(t, database.Create(&model.UserAuthenticationLink{
		ID: uuid.NewString(), UserID: unifiedUser.ID, ProviderType: model.AuthenticationProviderLegacyAdmin,
		ProviderSubject: strconv.FormatInt(admin.ID, 10), CredentialRevision: 1, Enabled: true, RowVersion: 1,
	}).Error)
	now := time.Now().Add(-time.Minute)
	require.NoError(t, database.Create(&model.PlatformRoleMembership{
		ID: uuid.NewString(), UserID: unifiedUser.ID, Role: model.PlatformRoleAdmin, Enabled: true, ValidFrom: now,
		PermissionRevision: 1, CreatedByUserID: unifiedUser.ID, Reason: "init", RowVersion: 1,
	}).Error)

	// 创建目标机器节点 bj-node-01（初始未开 SSH）与普通用户 zhangsan
	agent := model.User{Name: "bj-node-01", Alias: "北京GPU节点01", Role: model.UserRoleAgent, SecretHash: "x", Enabled: true, SSHEnabled: false}
	client := model.User{Name: "zhangsan", Alias: "张三", Role: model.UserRoleClient, SecretHash: "x", Enabled: true}
	require.NoError(t, database.Create(&agent).Error)
	require.NoError(t, database.Create(&client).Error)

	group := model.Group{Name: "ai-team", Alias: "AI研发组"}
	require.NoError(t, database.Create(&group).Error)
	require.NoError(t, database.Create(&model.GroupMember{GroupID: group.ID, UserID: client.ID}).Error)

	svc := model.ProxyService{
		ID:         uuid.NewString(),
		Name:       "gitlab-web",
		Alias:      "内部代码库",
		UserID:     agent.ID,
		SourceAddr: "127.0.0.1:8080",
		TargetAddr: "10.0.0.10:80",
		Enabled:    true,
	}
	require.NoError(t, database.Create(&svc).Error)

	provider := model.ResourceProvider{
		ID: uuid.NewString(), Key: "szzy", DisplayName: "深圳智翼", DomainScope: model.ProviderDomainNamed,
		DomainLabel: "szzy", Status: model.ProviderStatusActive, Revision: 1, RowVersion: 1,
	}
	require.NoError(t, database.Create(&provider).Error)
	require.NoError(t, database.Create(&model.AdminProviderMembership{
		ID: uuid.NewString(), UserID: unifiedUser.ID, ProviderID: provider.ID, Role: model.ProviderManagementRoleAdmin,
		Enabled: true, ValidFrom: now, PermissionRevision: 1, CreatedByUserID: unifiedUser.ID, Reason: "init", RowVersion: 1,
	}).Error)

	tenant := model.Tenant{ID: uuid.NewString(), Key: "beagle", Name: "北京比格", Status: model.TenantStatusActive}
	require.NoError(t, database.Create(&tenant).Error)

	cfg := &config.ServerConfig{
		Security: config.SecuritySection{
			JWTSecret:      "mcp-unit-test-jwt-secret",
			JWTExpireHours: 24,
		},
		FeatureFlags: config.FeatureFlagsSection{
			ManagementContextV2:  true,
			TenantResourceReadV2: true,
			ResourceModelWrite:   true,
			ResourceAllocation:   true,
		},
	}

	router := gin.New()
	router.Use(api.RequestMetadataMiddleware())

	v1 := router.Group("/api/v1")
	mcpHandler := NewHandler(cfg, router)
	v1.POST("/mcp", mcpHandler.ServeHTTP)

	adminGroup := v1.Group("/admin")
	adminAPI := api.NewAdminAPI(cfg)
	adminGroup.POST("/auth/login", adminAPI.Login)

	adminAuth := adminGroup.Group("")
	adminAuth.Use(api.AuthMiddleware(cfg.Security.JWTSecret, false))
	adminAuth.Use(api.ManagementAuthorizationMiddleware())
	adminAuth.POST("/auth/mcp-token", adminAPI.IssueMCPToken)

	userAPI := api.NewUserAPI(cfg)
	adminAuth.GET("/users", userAPI.List)
	adminAuth.GET("/users/:id", userAPI.Get)
	adminAuth.POST("/users", userAPI.Create)
	adminAuth.PUT("/users/:id/ssh", userAPI.UpdateSSH)

	aclAPI := api.NewACLAPI(cfg)
	adminAuth.GET("/acl/ssh", aclAPI.ListSSHACL)
	adminAuth.GET("/acl/ssh/:id", aclAPI.GetSSHACL)
	adminAuth.POST("/acl/ssh/:id/users", aclAPI.AddSSHACLUsers)
	adminAuth.POST("/acl/ssh/:id/groups", aclAPI.AddSSHACLGroups)
	adminAuth.DELETE("/acl/ssh/:id/users/:uid", aclAPI.RemoveSSHACLUser)
	adminAuth.GET("/acl/k8s", aclAPI.ListK8SACL)
	adminAuth.GET("/acl/k8s/:id", aclAPI.GetK8SACL)
	adminAuth.POST("/acl/k8s/:id/users", aclAPI.AddK8SACLUsers)
	adminAuth.GET("/acl/services", aclAPI.ListServiceACL)
	adminAuth.GET("/acl/services/:id", aclAPI.GetServiceACL)
	adminAuth.POST("/acl/services/:id/users", aclAPI.AddServiceACLUsers)

	tunnelAPI := api.NewTunnelAPI(cfg)
	adminAuth.GET("/tunnels", tunnelAPI.ListSignalTunnels)
	adminAuth.POST("/tunnels", tunnelAPI.CreateSignalTunnel)
	adminAuth.GET("/tunnels/:id", tunnelAPI.GetSignalTunnel)
	adminAuth.GET("/tunnels/:id/candidate-services", tunnelAPI.GetTunnelCandidateServices)
	adminAuth.PUT("/tunnels/:id/ports", tunnelAPI.UpdateSignalTunnelPorts)
	adminAuth.DELETE("/tunnels/:id", tunnelAPI.DeleteSignalTunnel)

	mgmtGroup := v1.Group("/management")
	mgmtGroup.Use(api.AuthMiddleware(cfg.Security.JWTSecret, false))
	mgmtGroup.Use(api.UnifiedManagementIdentityMiddleware())
	mgmtContextAPI := api.NewManagementContextAPI()
	mgmtGroup.GET("/contexts", mgmtContextAPI.List)
	mgmtGroup.GET("/contexts/current", mgmtContextAPI.Current)

	return mcpTestFixture{
		database: database,
		config:   cfg,
		router:   router,
		admin:    admin,
		agent:    agent,
		client:   client,
		group:    group,
		svc:      svc,
		provider: provider,
		tenant:   tenant,
	}
}

func (f mcpTestFixture) issueMCPToken(t *testing.T) string {
	t.Helper()
	// 1. 先调用 login 拿普通会话 JWT
	loginBody, _ := json.Marshal(map[string]string{
		"username": f.admin.Username,
		"password": "admin-secret-pw",
	})
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/auth/login", bytes.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	f.router.ServeHTTP(loginRec, loginReq)
	require.Equal(t, http.StatusOK, loginRec.Code)

	var loginResp struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(loginRec.Body.Bytes(), &loginResp))
	require.NotEmpty(t, loginResp.Data.Token)

	// 2. 调用 /api/v1/admin/auth/mcp-token 签发专用 SIGNAL_MCP_TOKEN
	issueBody, _ := json.Marshal(map[string]int{"expires_days": 30})
	issueReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/auth/mcp-token", bytes.NewReader(issueBody))
	issueReq.Header.Set("Authorization", "Bearer "+loginResp.Data.Token)
	issueReq.Header.Set("Content-Type", "application/json")
	issueRec := httptest.NewRecorder()
	f.router.ServeHTTP(issueRec, issueReq)
	require.Equal(t, http.StatusOK, issueRec.Code)

	var issueResp struct {
		Data struct {
			Token   string `json:"token"`
			Purpose string `json:"purpose"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(issueRec.Body.Bytes(), &issueResp))
	require.Equal(t, "mcp", issueResp.Data.Purpose)
	require.NotEmpty(t, issueResp.Data.Token)
	return issueResp.Data.Token
}

func (f mcpTestFixture) callMCPTool(t *testing.T, mcpToken, toolName string, args map[string]interface{}) (int, map[string]interface{}) {
	t.Helper()
	rpcPayload := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      "req-1",
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      toolName,
			"arguments": args,
		},
	}
	raw, err := json.Marshal(rpcPayload)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if mcpToken != "" {
		req.Header.Set("Authorization", "Bearer "+mcpToken)
	}
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)

	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	return rec.Code, resp
}

func extractToolOutput(t *testing.T, rpcResp map[string]interface{}) map[string]interface{} {
	t.Helper()
	result, ok := rpcResp["result"].(map[string]interface{})
	require.True(t, ok, "expected result in rpc response: %v", rpcResp)
	require.NotEqual(t, true, result["isError"], "unexpected tool error: %v", result)
	contents, ok := result["content"].([]interface{})
	require.True(t, ok && len(contents) > 0)
	first := contents[0].(map[string]interface{})
	text := first["text"].(string)
	var out map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(text), &out))
	return out
}

func TestMCPAuthenticationAndPasswordChangeRevocation(t *testing.T) {
	f := newMCPTestFixture(t)

	// 1. 未携带 SIGNAL_MCP_TOKEN 请求 /api/v1/mcp 必须返回 401
	code, _ := f.callMCPTool(t, "", "signal_ssh_acl", map[string]interface{}{"action": "list"})
	require.Equal(t, http.StatusUnauthorized, code)

	// 2. 签发 SIGNAL_MCP_TOKEN 后正常调用 initialize 与 tools/list
	mcpToken := f.issueMCPToken(t)
	code, rpcResp := f.callMCPTool(t, mcpToken, "signal_ssh_acl", map[string]interface{}{"action": "list"})
	require.Equal(t, http.StatusOK, code)
	_ = extractToolOutput(t, rpcResp)

	// 3. 当管理员修改密码导致 credential_revision 递增后，旧 SIGNAL_MCP_TOKEN 立即自动失效
	require.NoError(t, service.BumpLegacyAdminCredentialRevision(f.database, f.admin.ID))
	code, staleResp := f.callMCPTool(t, mcpToken, "signal_ssh_acl", map[string]interface{}{"action": "list"})
	require.Equal(t, http.StatusUnauthorized, code)
	require.Equal(t, "AUTH_REVISION_STALE", staleResp["code"])
}

func TestBusinessGrantUserSSHAndInspectPermissions(t *testing.T) {
	f := newMCPTestFixture(t)
	mcpToken := f.issueMCPToken(t)

	// 1. 初始盘点 zhangsan 对 bj-node-01 的连通性：应诊断为“目标机器尚未开启 SSH 能力”
	code, diagBeforeResp := f.callMCPTool(t, mcpToken, "signal_user_inspect", map[string]interface{}{
		"action":       "inspect_permissions",
		"user":         "zhangsan",
		"check_target": "bj-node-01",
	})
	require.Equal(t, http.StatusOK, code)
	diagBefore := extractToolOutput(t, diagBeforeResp)["diagnosis"].(map[string]interface{})
	require.Equal(t, false, diagBefore["reachable"])

	// 2. 执行核心业务 B1.1：直接用人名 "zhangsan" 和机器名 "bj-node-01" 授予 root,ubuntu SSH 权限
	//    目标机器 bj-node-01 初始 SSHEnabled=false，工具应自动开启其 SSH 能力并完成授权
	code, grantResp := f.callMCPTool(t, mcpToken, "signal_ssh_acl", map[string]interface{}{
		"action":    "grant_user",
		"target":    "bj-node-01",
		"user":      "zhangsan",
		"ssh_users": []string{"root", "ubuntu"},
	})
	require.Equal(t, http.StatusOK, code)
	grantOut := extractToolOutput(t, grantResp)
	require.Equal(t, "bj-node-01", grantOut["target_name"])
	require.Equal(t, "zhangsan", grantOut["granted_username"])
	require.Equal(t, true, grantOut["ssh_auto_enabled"])

	// 验证数据库中 bj-node-01 的 SSHEnabled 已变为 true，且授权记录已落库
	var updatedAgent model.User
	require.NoError(t, f.database.First(&updatedAgent, f.agent.ID).Error)
	require.True(t, updatedAgent.SSHEnabled)

	// 3. 同时给 zhangsan 开通 K8S 权限 (B2.1) 与 gitlab-web 内网服务权限 (B3.1)
	code, k8sResp := f.callMCPTool(t, mcpToken, "signal_k8s_acl", map[string]interface{}{
		"action":     "grant_user",
		"target":     "北京GPU节点01", // 支持直接用中文别名寻址
		"user":       "张三",        // 支持直接用中文别名寻址
		"k8s_groups": []string{"system:masters"},
	})
	require.Equal(t, http.StatusOK, code)
	_ = extractToolOutput(t, k8sResp)

	code, svcResp := f.callMCPTool(t, mcpToken, "signal_service_acl", map[string]interface{}{
		"action":  "grant_user",
		"service": "gitlab-web",
		"user":    "zhangsan",
	})
	require.Equal(t, http.StatusOK, code)
	_ = extractToolOutput(t, svcResp)

	// 4. 再次执行 B4.1 用户权限全景盘点：应查出 zhangsan 的分组、SSH、K8S、Service 权限，且诊断为 reachable=true
	code, inspectResp := f.callMCPTool(t, mcpToken, "signal_user_inspect", map[string]interface{}{
		"action":       "inspect_permissions",
		"user":         "zhangsan",
		"check_target": "bj-node-01",
	})
	require.Equal(t, http.StatusOK, code)
	inspectOut := extractToolOutput(t, inspectResp)
	require.Len(t, inspectOut["ssh_permissions"].([]interface{}), 1)
	require.Len(t, inspectOut["k8s_permissions"].([]interface{}), 1)
	require.Len(t, inspectOut["service_permissions"].([]interface{}), 1)
	diagAfter := inspectOut["diagnosis"].(map[string]interface{})
	require.Equal(t, true, diagAfter["reachable"])

	// 5. 执行 B1.3 撤销 zhangsan 在 bj-node-01 上的 SSH 权限
	code, revokeResp := f.callMCPTool(t, mcpToken, "signal_ssh_acl", map[string]interface{}{
		"action": "revoke_user",
		"target": "bj-node-01",
		"user":   "zhangsan",
	})
	require.Equal(t, http.StatusOK, code)
	_ = extractToolOutput(t, revokeResp)
}

func TestWorkspaceV2ScopeNameResolution(t *testing.T) {
	f := newMCPTestFixture(t)
	mcpToken := f.issueMCPToken(t)

	// 传入中文名 "深圳智翼" 自动解析出 provider.ID 并查询 contexts/current
	code, wsResp := f.callMCPTool(t, mcpToken, "signal_workspace_v2", map[string]interface{}{
		"action":     "call",
		"scope_type": "provider",
		"scope":      "深圳智翼",
		"method":     "GET",
		"path":       "/api/v1/management/contexts/current",
	})
	require.Equal(t, http.StatusOK, code)
	wsOut := extractToolOutput(t, wsResp)
	data := wsOut["data"].(map[string]interface{})
	require.Equal(t, "provider", data["scope_type"])
	require.Equal(t, f.provider.ID, data["scope_id"])
}

func TestSignalTunnelInspectAndIncrementalPortBind(t *testing.T) {
	f := newMCPTestFixture(t)
	mcpToken := f.issueMCPToken(t)

	now := time.Now()
	leaseExp := now.Add(time.Hour)
	hash64 := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	// 1. 构造 5090 边缘 Agent 节点（deploy-edge-gpu-5090）及其资源治理链路
	agent5090 := model.User{Name: "deploy-edge-gpu-5090", Alias: "5090边缘集群", Role: model.UserRoleAgent, SecretHash: "x", Enabled: true}
	require.NoError(t, f.database.Create(&agent5090).Error)
	agentNode := model.Node{Name: "deploy-edge-gpu-5090", Type: model.NodeTypeAgent, UserID: agent5090.ID}
	require.NoError(t, f.database.Create(&agentNode).Error)

	techRes := model.TechnicalResource{
		ID: uuid.NewString(), ProviderID: f.provider.ID, Type: model.TechnicalResourceAgent,
		StableKey: "agent-5090", DomainLabel: "gpu5090", LifecycleState: model.TechnicalResourceRegistered,
		HealthState: model.ResourceHealthOnline, CredentialRevision: 1, ConfigRevision: 1, RowVersion: 1,
	}
	require.NoError(t, f.database.Create(&techRes).Error)
	require.NoError(t, f.database.Create(&model.TechnicalResourceBinding{
		ID: uuid.NewString(), TechnicalResourceID: techRes.ID, SourceType: model.TechnicalResourceBindingLegacyNode,
		SourceID: strconv.FormatUint(agentNode.ID, 10), CredentialRevision: 1, Enabled: true, BoundByUserID: f.agent.ID,
		Reason: "bind", RowVersion: 1,
	}).Error)

	platRes := model.PlatformResource{
		ID: uuid.NewString(), ProviderID: f.provider.ID, Type: model.SupplyResourceKubernetes,
		StableKey: "k8s-5090", DisplayName: "5090 K8s", LifecycleState: model.PlatformResourceActive,
		HealthState: model.ResourceHealthOnline, CapabilityRevision: 1, RowVersion: 1,
	}
	require.NoError(t, f.database.Create(&platRes).Error)
	clusterScope := model.ResourceScope{
		ID: uuid.NewString(), ProviderID: f.provider.ID, PlatformResourceID: platRes.ID,
		Type: model.ResourceScopeCluster, StableKey: "cluster-5090", LifecycleState: model.ResourceScopeAllocatable,
		ConfigRevision: 1, EvidenceRevision: 1, RowVersion: 1,
	}
	require.NoError(t, f.database.Create(&clusterScope).Error)
	nsObs := model.NamespaceObservation{
		ID: uuid.NewString(), ProviderID: f.provider.ID, ClusterResourceID: platRes.ID,
		NamespaceUID: "ns-uid-vf", Name: "verdantflare-station", LabelSnapshot: "{}",
		Revision: 1, ObservedAt: now.Add(-time.Minute), LeaseExpiresAt: leaseExp, State: model.NamespaceObservationObserved,
	}
	require.NoError(t, f.database.Create(&nsObs).Error)
	nsScope := model.ResourceScope{
		ID: uuid.NewString(), ProviderID: f.provider.ID, PlatformResourceID: platRes.ID,
		Type: model.ResourceScopeNamespace, StableKey: "ns-vf", ParentID: &clusterScope.ID,
		NamespaceObservationID: &nsObs.ID, LifecycleState: model.ResourceScopeAllocatable,
		IsolationMode: model.ResourceScopeIsolationNamespaceIsolated, ConfigRevision: 1, EvidenceRevision: 1, RowVersion: 1,
	}
	require.NoError(t, f.database.Create(&nsScope).Error)

	// 构造两个候选服务：studio:8000 与 www:80
	studioObs := model.WorkloadObservation{
		ID: "obs-studio", NamespaceScopeID: nsScope.ID, Kind: model.WorkloadObservationServicePort,
		StableKey: "1111111111111111111111111111111111111111111111111111111111111111",
		IdentityQuality: model.WorkloadIdentityStrong, State: model.WorkloadObservationEligible,
		Ready: true, ObservedRevision: 1, LabelSnapshot: "{}", FirstObservedAt: now.Add(-time.Hour),
		LastObservedAt: now, LeaseExpiresAt: leaseExp, RowVersion: 1,
	}
	wwwObs := model.WorkloadObservation{
		ID: "obs-www", NamespaceScopeID: nsScope.ID, Kind: model.WorkloadObservationServicePort,
		StableKey: "2222222222222222222222222222222222222222222222222222222222222222",
		IdentityQuality: model.WorkloadIdentityStrong, State: model.WorkloadObservationEligible,
		Ready: true, ObservedRevision: 1, LabelSnapshot: "{}", FirstObservedAt: now.Add(-time.Hour),
		LastObservedAt: now, LeaseExpiresAt: leaseExp, RowVersion: 1,
	}
	require.NoError(t, f.database.Create(&studioObs).Error)
	require.NoError(t, f.database.Create(&wwwObs).Error)
	require.NoError(t, f.database.Create(&model.WorkloadObservationSource{
		ID: uuid.NewString(), WorkloadObservationID: studioObs.ID, SourceTechnicalResourceID: techRes.ID,
		SourceEpoch: "e1", Sequence: 1, PayloadHash: hash64, State: model.WorkloadObservationSourceObserved,
		Ready: true, TargetSnapshot: `{"service_uid":"svc-uid-studio","service_name":"studio","port_name":"http","protocol":"TCP","port_number":8000}`,
		ObservedAt: now, ReceivedAt: now, LeaseExpiresAt: leaseExp, SourceRevision: 1, RowVersion: 1,
	}).Error)
	require.NoError(t, f.database.Create(&model.WorkloadObservationSource{
		ID: uuid.NewString(), WorkloadObservationID: wwwObs.ID, SourceTechnicalResourceID: techRes.ID,
		SourceEpoch: "e1", Sequence: 2, PayloadHash: hash64, State: model.WorkloadObservationSourceObserved,
		Ready: true, TargetSnapshot: `{"service_uid":"svc-uid-www","service_name":"www","port_name":"http","protocol":"TCP","port_number":80}`,
		ObservedAt: now, ReceivedAt: now, LeaseExpiresAt: leaseExp, SourceRevision: 1, RowVersion: 1,
	}).Error)

	// 初始 tunnel-gpu-5090 已暴露 studio -> 18080 且开启了 K8sAPI(6443)
	tunnelTok := model.DeployToken{
		Name:            "tunnel-gpu-5090",
		Token:           uuid.NewString(),
		Mode:            "tunnel",
		TargetAgentName: "deploy-edge-gpu-5090",
		Status:          model.DeployTokenStatusBound,
		PortsConfig:     `{"k8s_api_enabled":true,"k8s_api_port":6443,"ports":[{"resource_id":"obs-studio","namespace":"verdantflare-station","namespace_uid":"ns-uid-vf","service_name":"studio","service_uid":"svc-uid-studio","port_name":"http","target_port":8000,"protocol":"TCP","local_port":18080}]}`,
		UserID:          f.client.ID,
		CreatedBy:       uint64(f.admin.ID),
	}
	require.NoError(t, f.database.Create(&tunnelTok).Error)

	// 2. 通过 MCP bind_tunnel_service 增量暴露 www -> 18030，验证不会覆盖已有的 studio(18080) 和 K8sAPI(6443)
	code, bindResp := f.callMCPTool(t, mcpToken, "signal_node_tunnel", map[string]interface{}{
		"action":     "bind_tunnel_service",
		"tunnel":     "tunnel-gpu-5090",
		"service":    "www",
		"namespace":  "verdantflare-station",
		"local_port": 18030,
	})
	require.Equal(t, http.StatusOK, code)
	bindOut := extractToolOutput(t, bindResp)
	require.Equal(t, float64(18030), bindOut["local_port"])
	require.Equal(t, true, bindOut["k8s_api_enabled"])

	// 3. 通过 inspect_tunnel 查看隧道全景，确认 studio(18080) 与 www(18030) 同时存在于 service_ports
	code, inspectResp := f.callMCPTool(t, mcpToken, "signal_node_tunnel", map[string]interface{}{
		"action": "inspect_tunnel",
		"tunnel": "tunnel-gpu-5090",
	})
	require.Equal(t, http.StatusOK, code)
	inspectOut := extractToolOutput(t, inspectResp)
	detailData := inspectOut["detail"].(map[string]interface{})["data"].(map[string]interface{})
	svcPorts := detailData["service_ports"].([]interface{})
	require.Len(t, svcPorts, 2)

	// 4. 传入 local_port=0 解绑 www，确认只保留 studio(18080)
	code, unbindResp := f.callMCPTool(t, mcpToken, "signal_node_tunnel", map[string]interface{}{
		"action":     "bind_tunnel_service",
		"tunnel":     "tunnel-gpu-5090",
		"service":    "www",
		"local_port": 0,
	})
	require.Equal(t, http.StatusOK, code)
	unbindOut := extractToolOutput(t, unbindResp)
	unbindDetail := unbindOut["tunnel_detail"].(map[string]interface{})["data"].(map[string]interface{})
	require.Len(t, unbindDetail["service_ports"].([]interface{}), 1)
}


