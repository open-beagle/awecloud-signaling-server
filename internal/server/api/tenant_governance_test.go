package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/service"
)

func TestTenantGovernanceAPIReadsOnlyCurrentTenant(t *testing.T) {
	fixture := newManagementContextAPIFixture(t)
	require.NoError(t, fixture.database.Model(&model.UserTenantManagementMembership{}).
		Where("user_id = ? AND tenant_id = ?", fixture.user.ID, fixture.tenant.ID).
		Updates(map[string]any{"role": model.TenantManagementRoleAdmin, "permission_revision": 3}).Error)

	otherTenant := model.Tenant{ID: uuid.NewString(), Key: "tenant-governance-b", Name: "Tenant Governance B", Status: model.TenantStatusActive}
	require.NoError(t, fixture.database.Create(&otherTenant).Error)
	otherUser := model.User{Name: "other-tenant-admin", Alias: "Other Tenant Admin", Role: model.UserRoleClient, SecretHash: "fixture", Enabled: true}
	require.NoError(t, fixture.database.Create(&otherUser).Error)
	require.NoError(t, fixture.database.Create(&model.UserIdentityProfile{
		UserID: otherUser.ID, Username: "other-tenant-admin", DisplayName: "Other Tenant Admin",
		Enabled: true, AuthRevision: 1, RowVersion: 1,
	}).Error)
	require.NoError(t, fixture.database.Create(&model.UserTenantManagementMembership{
		ID: uuid.NewString(), UserID: otherUser.ID, TenantID: otherTenant.ID,
		Role: model.TenantManagementRoleSecurityAuditor, Enabled: true, ValidFrom: time.Now().Add(-time.Minute),
		PermissionRevision: 1, CreatedByUserID: fixture.user.ID, Reason: "other tenant only", RowVersion: 1,
	}).Error)

	group := fixture.router.Group("/tenant-governance")
	group.Use(AuthMiddleware(fixture.config.Security.JWTSecret, false))
	group.Use(UnifiedManagementIdentityMiddleware())
	governanceAPI := NewTenantGovernanceAPI()
	group.GET("/tenants/:tenant_id/management-memberships", RequireManagementPermission(service.PermissionTenantAdminsRead), governanceAPI.ListManagementMemberships)

	login := fixture.login(t, fixture.admin.Username)
	request := httptest.NewRequest(http.MethodGet, "/tenant-governance/tenants/"+fixture.tenant.ID+"/management-memberships?size=100", nil)
	request.Header.Set("Authorization", "Bearer "+login.Token)
	request.Header.Set(HeaderManagementScopeType, string(model.ManagementScopeTenant))
	request.Header.Set(HeaderManagementScopeID, fixture.tenant.ID)
	response := httptest.NewRecorder()
	fixture.router.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), fixture.user.Name)
	require.NotContains(t, response.Body.String(), otherUser.Name)

	foreignRequest := httptest.NewRequest(http.MethodGet, "/tenant-governance/tenants/"+otherTenant.ID+"/management-memberships", nil)
	foreignRequest.Header.Set("Authorization", "Bearer "+login.Token)
	foreignRequest.Header.Set(HeaderManagementScopeType, string(model.ManagementScopeTenant))
	foreignRequest.Header.Set(HeaderManagementScopeID, fixture.tenant.ID)
	foreignResponse := httptest.NewRecorder()
	fixture.router.ServeHTTP(foreignResponse, foreignRequest)
	require.Equal(t, http.StatusNotFound, foreignResponse.Code, foreignResponse.Body.String())
	assertResponseErrorCode(t, foreignResponse, ErrorCodeManagementObjectMissing)
}
