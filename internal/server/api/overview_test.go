package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	serverdb "github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func TestTenantAndPlatformOverviewAggregation(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:overview_aggregation_test?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(
		&model.Admin{}, &model.AdminTenantMembership{}, &model.Tenant{}, &model.TenantMembership{},
		&model.Group{}, &model.Resource{}, &model.ContainerSession{}, &model.DiscoveryCandidate{},
		&model.Node{}, &model.Endpoint{},
	))
	serverdb.DB = database
	now := time.Now()
	platform := model.Admin{Username: "overview-platform", PasswordHash: "test", Role: "admin", Enabled: true}
	tenantAdmin := model.Admin{Username: "overview-tenant", PasswordHash: "test", Role: "tenant_admin", Enabled: true}
	tenantA := model.Tenant{ID: "overview-tenant-a", Key: "overview-a", Name: "Overview A", Status: model.TenantStatusActive}
	tenantB := model.Tenant{ID: "overview-tenant-b", Key: "overview-b", Name: "Overview B", Status: model.TenantStatusSuspended}
	require.NoError(t, database.Create(&platform).Error)
	require.NoError(t, database.Create(&tenantAdmin).Error)
	require.NoError(t, database.Create(&tenantA).Error)
	require.NoError(t, database.Create(&tenantB).Error)
	require.NoError(t, database.Create(&model.AdminTenantMembership{AdminID: tenantAdmin.ID, TenantID: tenantA.ID, Role: "tenant_admin", Enabled: true, PermissionRevision: 1}).Error)
	disabledMembership := model.AdminTenantMembership{AdminID: platform.ID, TenantID: tenantB.ID, Role: "tenant_viewer", Enabled: true, PermissionRevision: 1}
	require.NoError(t, database.Create(&disabledMembership).Error)
	require.NoError(t, database.Model(&disabledMembership).Update("enabled", false).Error)
	future := now.Add(time.Hour)
	past := now.Add(-time.Hour)
	require.NoError(t, database.Create(&model.TenantMembership{TenantID: tenantA.ID, UserID: 1, Role: "member", Enabled: true, ExpiresAt: &future}).Error)
	require.NoError(t, database.Create(&model.TenantMembership{TenantID: tenantA.ID, UserID: 2, Role: "member", Enabled: true, ExpiresAt: &past}).Error)
	require.NoError(t, database.Create(&model.Group{TenantID: tenantA.ID, Name: "overview-group"}).Error)
	resourceA := model.Resource{ID: "overview-resource-a", TenantID: tenantA.ID, Type: model.ResourceTypeContainerSSH, DisplayName: "待发布容器", State: model.ResourceStatePending}
	resourceB := model.Resource{ID: "overview-resource-b", TenantID: tenantB.ID, Type: model.ResourceTypeHostSSH, DisplayName: "异常主机", State: model.ResourceStateDegraded}
	require.NoError(t, database.Create(&resourceA).Error)
	require.NoError(t, database.Create(&resourceB).Error)
	require.NoError(t, database.Create(&model.ContainerSession{ID: "overview-session", TenantID: tenantA.ID, UserID: 1, ResourceID: resourceA.ID, Status: model.ContainerSessionActive, StartedAt: now}).Error)
	require.NoError(t, database.Create(&model.DiscoveryCandidate{ID: "overview-candidate", AgentNodeID: 10, Namespace: "default", PodUID: "pod-uid", ContainerName: "main", WorkspaceHint: "conflict-workspace", Status: model.DiscoveryCandidateConflict, ObservedAt: now, ConflictReason: "可信绑定冲突"}).Error)
	require.NoError(t, database.Create(&model.Node{UserID: 99, Name: "overview-agent", Type: model.NodeTypeAgent}).Error)
	require.NoError(t, database.Create(&model.Endpoint{ID: "overview-endpoint", UserID: 99, Name: "endpoint", Status: "online"}).Error)

	overview := NewOverviewAPI()
	tenantRouter := tenantManagementRouter(tenantAdmin.ID, func(router *gin.Engine) { router.GET("/tenants/:id/overview", overview.Tenant) })
	tenantRequest := httptest.NewRequest(http.MethodGet, "/tenants/"+tenantA.ID+"/overview", nil)
	tenantRequest.Header.Set("X-Tenant-ID", tenantA.ID)
	tenantResponse := httptest.NewRecorder()
	tenantRouter.ServeHTTP(tenantResponse, tenantRequest)
	require.Equal(t, http.StatusOK, tenantResponse.Code)
	var tenantBody struct {
		Success bool                   `json:"success"`
		Data    tenantOverviewResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(tenantResponse.Body.Bytes(), &tenantBody))
	require.True(t, tenantBody.Success)
	require.Equal(t, int64(1), tenantBody.Data.MemberCount)
	require.Equal(t, int64(1), tenantBody.Data.GroupCount)
	require.Equal(t, int64(1), tenantBody.Data.ResourceCount)
	require.Equal(t, int64(1), tenantBody.Data.ActiveSessions)
	require.Equal(t, int64(1), tenantBody.Data.RiskCount)
	require.Len(t, tenantBody.Data.Attention, 1)
	require.Equal(t, resourceA.ID, tenantBody.Data.Attention[0].TargetID)

	platformRouter := tenantManagementRouter(platform.ID, func(router *gin.Engine) { router.GET("/overview/platform", overview.Platform) })
	platformResponse := httptest.NewRecorder()
	platformRouter.ServeHTTP(platformResponse, httptest.NewRequest(http.MethodGet, "/overview/platform", nil))
	require.Equal(t, http.StatusOK, platformResponse.Code)
	var platformBody struct {
		Success bool                     `json:"success"`
		Data    platformOverviewResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(platformResponse.Body.Bytes(), &platformBody))
	require.True(t, platformBody.Success)
	require.Equal(t, int64(2), platformBody.Data.TenantCount)
	require.Equal(t, int64(1), platformBody.Data.AdminMembershipCount)
	require.Equal(t, int64(2), platformBody.Data.ResourceCount)
	require.Equal(t, int64(1), platformBody.Data.AgentCount)
	require.Equal(t, int64(1), platformBody.Data.EndpointCount)
	require.Equal(t, int64(3), platformBody.Data.HighRiskCount)
	require.Len(t, platformBody.Data.Attention, 3)
}

func TestTenantOverviewRejectsCrossTenantContext(t *testing.T) {
	_, tenantAdmin, tenant := setupTenantManagementTest(t)
	overview := NewOverviewAPI()
	router := tenantManagementRouter(tenantAdmin.ID, func(router *gin.Engine) { router.GET("/tenants/:id/overview", overview.Tenant) })
	request := httptest.NewRequest(http.MethodGet, "/tenants/"+tenant.ID+"/overview", nil)
	request.Header.Set("X-Tenant-ID", "different-tenant")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusForbidden, response.Code)
	require.Contains(t, response.Body.String(), ErrorCodeTenantContextConflict)
}
