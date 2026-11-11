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
	"github.com/open-beagle/awecloud-signaling-server/internal/server/cache"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func TestDesktopRDPTenantGrantsAndPlatformEmergencyKillSwitch(t *testing.T) {
	oldDB := db.DB
	t.Cleanup(func() { db.DB = oldDB })

	database, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:desktop_rdp_api_%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{})
	require.NoError(t, err)
	db.DB = database
	require.NoError(t, database.AutoMigrate(
		&model.Admin{},
		&model.AdminTenantMembership{},
		&model.Tenant{},
		&model.TenantMembership{},
		&model.User{},
		&model.Node{},
		&model.AclDesktopRDPPermission{},
		&model.DesktopRDPSessionAudit{},
		&model.AuditLog{},
	))

	tenantA := model.Tenant{ID: uuid.NewString(), Key: "rdp-tenant-a", Name: "RDP Tenant A", Status: model.TenantStatusActive}
	tenantB := model.Tenant{ID: uuid.NewString(), Key: "rdp-tenant-b", Name: "RDP Tenant B", Status: model.TenantStatusActive}
	require.NoError(t, database.Create(&tenantA).Error)
	require.NoError(t, database.Create(&tenantB).Error)

	platformAdmin := model.Admin{Username: "platform-admin", PasswordHash: "x", Role: "admin", Enabled: true}
	tenantAdmin := model.Admin{Username: "tenant-admin", PasswordHash: "x", Role: "tenant_admin", Enabled: true}
	require.NoError(t, database.Create(&platformAdmin).Error)
	require.NoError(t, database.Create(&tenantAdmin).Error)
	require.NoError(t, database.Create(&model.AdminTenantMembership{
		AdminID:  tenantAdmin.ID,
		TenantID: tenantA.ID,
		Role:     "tenant_admin",
		Enabled:  true,
	}).Error)

	owner := model.User{Name: "owner-user", Role: model.UserRoleClient, SecretHash: "x", Enabled: true}
	colleague := model.User{Name: "colleague-user", Role: model.UserRoleClient, SecretHash: "x", Enabled: true}
	outsider := model.User{Name: "outsider-user", Role: model.UserRoleClient, SecretHash: "x", Enabled: true}
	require.NoError(t, database.Create(&owner).Error)
	require.NoError(t, database.Create(&colleague).Error)
	require.NoError(t, database.Create(&outsider).Error)

	require.NoError(t, database.Create(&model.TenantMembership{TenantID: tenantA.ID, UserID: owner.ID, Role: "member", Enabled: true}).Error)
	require.NoError(t, database.Create(&model.TenantMembership{TenantID: tenantA.ID, UserID: colleague.ID, Role: "member", Enabled: true}).Error)
	require.NoError(t, database.Create(&model.TenantMembership{TenantID: tenantB.ID, UserID: outsider.ID, Role: "member", Enabled: true}).Error)

	now := time.Now()
	homeNode := model.Node{
		UserID:           owner.ID,
		Name:             "home-windows",
		Hostname:         "WIN-HOME",
		Type:             model.NodeTypeDesktop,
		IP:               "100.64.0.88",
		LastHeartbeat:    &now,
		RDPMode:          model.DesktopRDPModeDisabled,
		RDPAllowShare:    false,
		RDPAdminDisabled: false,
	}
	require.NoError(t, database.Create(&homeNode).Error)

	runtimeStore := cache.NewNodeRuntimeStore()
	runtimeStore.UpsertNode(&homeNode)
	_, err = runtimeStore.UpdateDesktopRDPStatus(homeNode.ID, true, 3389, true)
	require.NoError(t, err)

	require.NoError(t, database.Create(&model.DesktopRDPSessionAudit{
		SessionUUID:      "sess-1001",
		TargetNodeID:     homeNode.ID,
		OwnerUserID:      owner.ID,
		EventType:        model.RDPSessionEventStarted,
		SourceUserName:   "client-owner-user",
		SourceDeviceName: "travel-laptop",
		SourceIP:         "100.64.0.89",
		OccurredAt:       now,
	}).Error)

	tenantAPI := NewTenantBusinessAPI()
	tenantAPI.SetRuntimeStore(runtimeStore)
	nodeAPI := NewNodeAPI(&config.ServerConfig{})
	nodeAPI.SetRuntimeStore(runtimeStore)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		if c.GetHeader("X-Actor") == "platform" {
			c.Set("admin_id", platformAdmin.ID)
			c.Set("username", platformAdmin.Username)
		} else {
			c.Set("admin_id", tenantAdmin.ID)
			c.Set("username", tenantAdmin.Username)
		}
	})
	router.GET("/tenants/:id/member-devices", tenantAPI.ListMemberDevices)
	router.GET("/tenants/:id/member-devices/:node_id/rdp-grants", tenantAPI.ListDeviceRDPGrants)
	router.POST("/tenants/:id/member-devices/:node_id/rdp-grants", tenantAPI.CreateDeviceRDPGrant)
	router.DELETE("/tenants/:id/member-devices/:node_id/rdp-grants/:grant_id", tenantAPI.RevokeDeviceRDPGrant)
	router.GET("/nodes/:id/rdp", nodeAPI.GetDesktopRDPStatus)
	router.PUT("/nodes/:id/rdp/admin-disable", nodeAPI.UpdateDesktopRDPAdminDisable)

	doJSON := func(method, path, actor, tenantID string, body any) *httptest.ResponseRecorder {
		var raw []byte
		if body != nil {
			raw, _ = json.Marshal(body)
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Actor", actor)
		if tenantID != "" {
			req.Header.Set("X-Tenant-ID", tenantID)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	grantPath := fmt.Sprintf("/tenants/%s/member-devices/%d/rdp-grants", tenantA.ID, homeNode.ID)

	// 1. 机主未开启 rdp_allow_share 时，租户管理员无权代开跨人授权 -> 403
	resConsentDenied := doJSON(http.MethodPost, grantPath, "tenant", tenantA.ID, map[string]any{
		"grantee_user_id":  colleague.ID,
		"duration_minutes": 60,
		"reason":           "远程排障",
	})
	require.Equal(t, http.StatusForbidden, resConsentDenied.Code)

	// 2. 模拟机主开启 rdp_mode=auto 且开启 rdp_allow_share=true
	require.NoError(t, database.Model(&homeNode).Updates(map[string]any{
		"rdp_mode":        model.DesktopRDPModeAuto,
		"rdp_allow_share": true,
	}).Error)

	// 3. 未传到期时间必须拒绝 -> 400
	resNoExpiry := doJSON(http.MethodPost, grantPath, "tenant", tenantA.ID, map[string]any{
		"grantee_user_id": colleague.ID,
	})
	require.Equal(t, http.StatusBadRequest, resNoExpiry.Code)

	// 4. 跨租户成员必须拒绝 -> 403
	resOutsider := doJSON(http.MethodPost, grantPath, "tenant", tenantA.ID, map[string]any{
		"grantee_user_id":  outsider.ID,
		"duration_minutes": 60,
	})
	require.Equal(t, http.StatusForbidden, resOutsider.Code)

	// 5. 合法同租户成员创建临时授权成功 -> 201
	resCreated := doJSON(http.MethodPost, grantPath, "tenant", tenantA.ID, map[string]any{
		"grantee_user_id":  colleague.ID,
		"duration_minutes": 120,
		"reason":           "出差协助排查环境",
	})
	require.Equal(t, http.StatusCreated, resCreated.Code)
	var createdBody struct {
		Data TenantDeviceRDPGrantItem `json:"data"`
	}
	require.NoError(t, json.Unmarshal(resCreated.Body.Bytes(), &createdBody))
	require.Equal(t, colleague.ID, createdBody.Data.GranteeUserID)
	require.Equal(t, "active", createdBody.Data.Status)

	// 6. 成员设备列表返回 active_grant_count = 1
	resDevices := doJSON(http.MethodGet, "/tenants/"+tenantA.ID+"/member-devices", "tenant", tenantA.ID, nil)
	require.Equal(t, http.StatusOK, resDevices.Code)
	var listBody struct {
		Data []TenantMemberDeviceItem `json:"data"`
	}
	require.NoError(t, json.Unmarshal(resDevices.Body.Bytes(), &listBody))
	require.Len(t, listBody.Data, 1)
	require.Equal(t, int64(1), listBody.Data[0].ActiveGrantCount)
	require.True(t, listBody.Data[0].RDPAllowShare)

	// 7. 平台工作域只读诊断可见授权摘要与会话审计
	rdpNodePath := fmt.Sprintf("/nodes/%d/rdp", homeNode.ID)
	resDiag := doJSON(http.MethodGet, rdpNodePath, "platform", "", nil)
	require.Equal(t, http.StatusOK, resDiag.Code)
	var diagBody struct {
		Data DesktopNodeRDPDiagnostics `json:"data"`
	}
	require.NoError(t, json.Unmarshal(resDiag.Body.Bytes(), &diagBody))
	require.Len(t, diagBody.Data.ActiveGrants, 1)
	require.Len(t, diagBody.Data.RecentSessions, 1)
	require.Equal(t, "sess-1001", diagBody.Data.RecentSessions[0].SessionUUID)

	// 8. 撤销授权成功 -> 200
	revokePath := fmt.Sprintf("%s/%d", grantPath, createdBody.Data.ID)
	resRevoke := doJSON(http.MethodDelete, revokePath, "tenant", tenantA.ID, nil)
	require.Equal(t, http.StatusOK, resRevoke.Code)

	// 9. 平台工作域紧急熔断关闭：验证无法反向强制修改机主的 rdp_mode / rdp_allow_share！
	require.NoError(t, database.Model(&homeNode).Updates(map[string]any{
		"rdp_mode":        model.DesktopRDPModeDisabled,
		"rdp_allow_share": false,
	}).Error)

	disablePath := fmt.Sprintf("/nodes/%d/rdp/admin-disable", homeNode.ID)
	resKill := doJSON(http.MethodPut, disablePath, "platform", "", map[string]any{
		"admin_disabled":  true,
		"reason":          "安全应急熔断",
		"rdp_mode":        "enabled", // 恶意/误传多余字段必须被完全忽略
		"rdp_allow_share": true,      // 恶意/误传多余字段必须被完全忽略
	})
	require.Equal(t, http.StatusOK, resKill.Code)

	var verifyNode model.Node
	require.NoError(t, database.First(&verifyNode, homeNode.ID).Error)
	require.True(t, verifyNode.RDPAdminDisabled)
	require.Equal(t, model.DesktopRDPModeDisabled, verifyNode.RDPMode, "PlatformAdmin 绝不能强行修改机主 rdp_mode")
	require.False(t, verifyNode.RDPAllowShare, "PlatformAdmin 绝不能强行开启机主 rdp_allow_share")
}
