package api

import (
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

func TestTenantMemberDevicesAndAuditStayInsideTenant(t *testing.T) {
	oldDB := db.DB
	t.Cleanup(func() { db.DB = oldDB })
	database, err := gorm.Open(sqlite.Open("file:tenant_business_test?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	db.DB = database
	require.NoError(t, database.AutoMigrate(
		&model.Admin{}, &model.AdminTenantMembership{}, &model.Tenant{}, &model.TenantMembership{},
		&model.User{}, &model.Node{}, &model.AuditLog{},
	))

	tenantA := model.Tenant{ID: uuid.NewString(), Key: "business-a", Name: "Business A", Status: model.TenantStatusActive}
	tenantB := model.Tenant{ID: uuid.NewString(), Key: "business-b", Name: "Business B", Status: model.TenantStatusActive}
	admin := model.Admin{Username: "tenant-business-admin", PasswordHash: "test", Role: "tenant_admin", Enabled: true}
	auditor := model.Admin{Username: "tenant-business-auditor", PasswordHash: "test", Role: "tenant_admin", Enabled: true}
	require.NoError(t, database.Create(&tenantA).Error)
	require.NoError(t, database.Create(&tenantB).Error)
	require.NoError(t, database.Create(&admin).Error)
	require.NoError(t, database.Create(&auditor).Error)
	require.NoError(t, database.Create(&model.AdminTenantMembership{AdminID: admin.ID, TenantID: tenantA.ID, Role: "tenant_viewer", Enabled: true}).Error)
	require.NoError(t, database.Create(&model.AdminTenantMembership{AdminID: auditor.ID, TenantID: tenantA.ID, Role: "security_auditor", Enabled: true}).Error)

	userA := model.User{Name: "member-a", Role: model.UserRoleClient, SecretHash: "test", Enabled: true}
	userB := model.User{Name: "member-b", Role: model.UserRoleClient, SecretHash: "test", Enabled: true}
	require.NoError(t, database.Create(&userA).Error)
	require.NoError(t, database.Create(&userB).Error)
	require.NoError(t, database.Create(&model.TenantMembership{TenantID: tenantA.ID, UserID: userA.ID, Role: "member", Enabled: true}).Error)
	require.NoError(t, database.Create(&model.TenantMembership{TenantID: tenantB.ID, UserID: userB.ID, Role: "member", Enabled: true}).Error)
	now := time.Now()
	onlineOlder := now.Add(-time.Minute)
	offlineRecent := now.Add(-3 * time.Minute)
	require.NoError(t, database.Create(&model.Node{UserID: userA.ID, Name: "desktop-online-newer", Type: model.NodeTypeDesktop, LastHeartbeat: &now}).Error)
	require.NoError(t, database.Create(&model.Node{UserID: userA.ID, Name: "desktop-online-older", Type: model.NodeTypeDesktop, LastHeartbeat: &onlineOlder}).Error)
	require.NoError(t, database.Create(&model.Node{UserID: userA.ID, Name: "desktop-offline", Type: model.NodeTypeDesktop, LastHeartbeat: &offlineRecent}).Error)
	require.NoError(t, database.Create(&model.Node{UserID: userA.ID, Name: "desktop-never-online", Type: model.NodeTypeDesktop}).Error)
	require.NoError(t, database.Create(&model.Node{UserID: userB.ID, Name: "desktop-b", Type: model.NodeTypeDesktop, LastHeartbeat: &now}).Error)
	require.NoError(t, database.Create(&model.Node{UserID: userA.ID, Name: "agent-a", Type: model.NodeTypeAgent, LastHeartbeat: &now}).Error)
	require.NoError(t, database.Create(&model.AuditLog{TenantID: tenantA.ID, ActorAdminID: admin.ID, ActorUsername: admin.Username, ActionType: "tenant_a_action", TargetType: "tenant", TargetID: tenantA.ID, TargetName: tenantA.Name}).Error)
	require.NoError(t, database.Create(&model.AuditLog{TenantID: tenantB.ID, ActorAdminID: admin.ID, ActorUsername: admin.Username, ActionType: "tenant_b_action", TargetType: "tenant", TargetID: tenantB.ID, TargetName: tenantB.Name}).Error)

	api := NewTenantBusinessAPI()
	router := gin.New()
	router.Use(func(c *gin.Context) {
		if c.GetHeader("X-Test-Auditor") == "true" {
			c.Set("admin_id", auditor.ID)
		} else {
			c.Set("admin_id", admin.ID)
		}
	})
	router.GET("/tenants/:id/member-devices", api.ListMemberDevices)
	router.GET("/tenants/:id/audit-logs", api.ListAuditLogs)

	request := func(path, tenantID string, useAuditor bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("X-Tenant-ID", tenantID)
		if useAuditor {
			req.Header.Set("X-Test-Auditor", "true")
		}
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)
		return resp
	}

	devices := request("/tenants/"+tenantA.ID+"/member-devices", tenantA.ID, false)
	require.Equal(t, http.StatusOK, devices.Code)
	var deviceBody struct {
		Data  []TenantMemberDeviceItem `json:"data"`
		Total int64                    `json:"total"`
	}
	require.NoError(t, json.Unmarshal(devices.Body.Bytes(), &deviceBody))
	require.Equal(t, int64(4), deviceBody.Total)
	require.Len(t, deviceBody.Data, 4)
	require.Equal(t, []string{"desktop-online-newer", "desktop-online-older", "desktop-offline", "desktop-never-online"}, []string{
		deviceBody.Data[0].DeviceName,
		deviceBody.Data[1].DeviceName,
		deviceBody.Data[2].DeviceName,
		deviceBody.Data[3].DeviceName,
	})
	require.True(t, deviceBody.Data[0].Online)
	require.True(t, deviceBody.Data[1].Online)
	require.False(t, deviceBody.Data[2].Online)
	require.False(t, deviceBody.Data[3].Online)

	audits := request("/tenants/"+tenantA.ID+"/audit-logs", tenantA.ID, false)
	require.Equal(t, http.StatusOK, audits.Code)
	var auditBody struct {
		Data  []TenantAuditItem `json:"data"`
		Total int64             `json:"total"`
	}
	require.NoError(t, json.Unmarshal(audits.Body.Bytes(), &auditBody))
	require.Equal(t, int64(1), auditBody.Total)
	require.Equal(t, "tenant_a_action", auditBody.Data[0].ActionType)

	deniedCrossTenant := request("/tenants/"+tenantB.ID+"/audit-logs", tenantB.ID, false)
	require.Equal(t, http.StatusForbidden, deniedCrossTenant.Code)
	auditorDeviceDenied := request("/tenants/"+tenantA.ID+"/member-devices", tenantA.ID, true)
	require.Equal(t, http.StatusForbidden, auditorDeviceDenied.Code)
	auditorAuditAllowed := request("/tenants/"+tenantA.ID+"/audit-logs", tenantA.ID, true)
	require.Equal(t, http.StatusOK, auditorAuditAllowed.Code)
}
