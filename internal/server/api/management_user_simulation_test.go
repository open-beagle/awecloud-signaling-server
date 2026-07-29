package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func TestUserSimulationAPIUsesEffectivePermissionsAndIdempotency(t *testing.T) {
	fixture := newManagementContextAPIFixture(t)
	target := seedTenantSimulationTarget(t, fixture, "simulated-viewer", model.TenantManagementRoleViewer)
	login := fixture.login(t, fixture.admin.Username)
	body := createSimulationBody(t, target.ID, model.UserSimulationScopeTenant, fixture.tenant.ID, "reproduce viewer access", time.Now().Add(2*time.Hour))
	headers := map[string]string{
		HeaderManagementScopeType: string(model.ManagementScopePlatform),
		HeaderIdempotencyKey:      "simulation-viewer-1",
	}

	created := fixture.managementJSONRequest(http.MethodPost, "/management/user-simulations", login.Token, headers, body)
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	session := decodeSimulationResponse(t, created)
	require.Equal(t, fixture.user.ID, session.ActorUserID)
	require.Equal(t, target.ID, session.EffectiveUserID)
	require.Equal(t, model.UserSimulationSessionActive, session.Status)
	require.False(t, session.CreatedAt.IsZero())
	require.Equal(t, `"1"`, created.Header().Get("ETag"))

	replayed := fixture.managementJSONRequest(http.MethodPost, "/management/user-simulations", login.Token, headers, body)
	require.Equal(t, http.StatusCreated, replayed.Code, replayed.Body.String())
	require.Equal(t, session.ID, decodeSimulationResponse(t, replayed).ID)
	var sessionCount int64
	require.NoError(t, fixture.database.Model(&model.UserSimulationSession{}).Count(&sessionCount).Error)
	require.Equal(t, int64(1), sessionCount)

	changedBody := createSimulationBody(t, target.ID, model.UserSimulationScopeTenant, fixture.tenant.ID, "different request", time.Now().Add(2*time.Hour))
	conflict := fixture.managementJSONRequest(http.MethodPost, "/management/user-simulations", login.Token, headers, changedBody)
	require.Equal(t, http.StatusConflict, conflict.Code, conflict.Body.String())
	requireResponseCode(t, conflict, ErrorCodeIdempotencyKeyReused)

	simulationHeaders := map[string]string{
		HeaderManagementScopeType: string(model.ManagementScopeTenant),
		HeaderManagementScopeID:   fixture.tenant.ID,
		HeaderUserSimulationID:    session.ID,
	}
	current := fixture.managementRequest(http.MethodGet, "/management/contexts/current", login.Token, simulationHeaders)
	require.Equal(t, http.StatusOK, current.Code, current.Body.String())
	var currentBody struct {
		Data managementContextResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(current.Body.Bytes(), &currentBody))
	require.Equal(t, string(model.TenantManagementRoleViewer), currentBody.Data.Role)
	require.NotContains(t, currentBody.Data.Permissions, servicePermissionTenantResourcesWrite)

	read := fixture.managementRequest(http.MethodGet, "/management/test/tenant-resources", login.Token, simulationHeaders)
	require.Equal(t, http.StatusOK, read.Code, read.Body.String())
	write := fixture.managementRequest(http.MethodPost, "/management/test/tenant-resources", login.Token, simulationHeaders)
	require.Equal(t, http.StatusForbidden, write.Code, write.Body.String())
	requireResponseCode(t, write, ErrorCodeManagementPermission)

	crossScope := fixture.managementRequest(http.MethodGet, "/management/test/tenant-resources", login.Token, map[string]string{
		HeaderManagementScopeType: string(model.ManagementScopeProvider),
		HeaderManagementScopeID:   fixture.provider.ID,
		HeaderUserSimulationID:    session.ID,
	})
	require.Equal(t, http.StatusNotFound, crossScope.Code, crossScope.Body.String())

	nested := fixture.managementJSONRequest(http.MethodPost, "/management/user-simulations", login.Token, map[string]string{
		HeaderManagementScopeType: string(model.ManagementScopePlatform),
		HeaderUserSimulationID:    session.ID,
		HeaderIdempotencyKey:      "nested-simulation",
	}, body)
	require.Equal(t, http.StatusForbidden, nested.Code, nested.Body.String())
	requireResponseCode(t, nested, ErrorCodeSimulationForbidden)

	legacyPassword := fixture.managementJSONRequest(http.MethodPut, "/password", login.Token, map[string]string{
		HeaderUserSimulationID: session.ID,
	}, []byte(`{"old_password":"fixture-password","new_password":"another-fixture-password"}`))
	require.Equal(t, http.StatusForbidden, legacyPassword.Code, legacyPassword.Body.String())
	requireResponseCode(t, legacyPassword, ErrorCodeSimulationForbidden)

	var audits []model.AuditLog
	require.NoError(t, fixture.database.Where("simulation_session_id = ? AND action_type IN ?", session.ID,
		[]string{"resolve_user_simulation_context", "authorize_user_simulation_request"}).Find(&audits).Error)
	require.GreaterOrEqual(t, len(audits), 3)
	for _, audit := range audits {
		require.Equal(t, fixture.user.ID, audit.ActorUserID)
		require.Equal(t, target.ID, audit.EffectiveUserID)
		require.Equal(t, fixture.tenant.ID, audit.ScopeID)
	}
}

func TestUserSimulationAPIRevocationAndInvalidation(t *testing.T) {
	fixture := newManagementContextAPIFixture(t)
	target := seedTenantSimulationTarget(t, fixture, "simulated-admin", model.TenantManagementRoleAdmin)
	login := fixture.login(t, fixture.admin.Username)
	session := createSimulationThroughAPI(t, fixture, login.Token, target.ID, "simulation-revoke-1")

	wrongVersion := fixture.managementJSONRequest(http.MethodPost, "/management/user-simulations/"+session.ID+"/revoke", login.Token, map[string]string{
		HeaderManagementScopeType: string(model.ManagementScopePlatform),
		HeaderIfMatch:             strconv.FormatInt(session.RowVersion+1, 10),
	}, []byte(`{"reason":"operator exit"}`))
	require.Equal(t, http.StatusConflict, wrongVersion.Code, wrongVersion.Body.String())
	requireResponseCode(t, wrongVersion, ErrorCodeSimulationVersionConflict)

	revoked := fixture.managementJSONRequest(http.MethodPost, "/management/user-simulations/"+session.ID+"/revoke", login.Token, map[string]string{
		HeaderManagementScopeType: string(model.ManagementScopePlatform),
		HeaderIfMatch:             strconv.FormatInt(session.RowVersion, 10),
	}, []byte(`{"reason":"operator exit"}`))
	require.Equal(t, http.StatusOK, revoked.Code, revoked.Body.String())
	revokedSession := decodeSimulationResponse(t, revoked)
	require.Equal(t, model.UserSimulationSessionRevoked, revokedSession.Status)
	require.Equal(t, "operator exit", revokedSession.EndReason)
	require.Equal(t, int64(2), revokedSession.RowVersion)

	inactive := fixture.managementRequest(http.MethodGet, "/management/contexts/current", login.Token, map[string]string{
		HeaderManagementScopeType: string(model.ManagementScopeTenant),
		HeaderManagementScopeID:   fixture.tenant.ID,
		HeaderUserSimulationID:    session.ID,
	})
	require.Equal(t, http.StatusConflict, inactive.Code, inactive.Body.String())
	requireResponseCode(t, inactive, ErrorCodeSimulationInactive)

	var revokeAudit model.AuditLog
	require.NoError(t, fixture.database.Where("action_type = ? AND simulation_session_id = ?", "revoke_user_simulation", session.ID).First(&revokeAudit).Error)
	require.Equal(t, fixture.user.ID, revokeAudit.ActorUserID)
	require.Equal(t, target.ID, revokeAudit.EffectiveUserID)
}

func TestUserSimulationAPISupportsProviderUsers(t *testing.T) {
	fixture := newManagementContextAPIFixture(t)
	target := model.User{Name: "simulated-provider-operator", Role: model.UserRoleClient, SecretHash: "fixture", Enabled: true}
	require.NoError(t, fixture.database.Create(&target).Error)
	require.NoError(t, fixture.database.Create(&model.UserIdentityProfile{
		UserID: target.ID, Username: target.Name, DisplayName: target.Name, Enabled: true, AuthRevision: 1, RowVersion: 1,
	}).Error)
	require.NoError(t, fixture.database.Create(&model.AdminProviderMembership{
		ID: uuid.NewString(), UserID: target.ID, ProviderID: fixture.provider.ID, Role: model.ProviderManagementRoleOperator,
		Enabled: true, ValidFrom: time.Now().Add(-time.Minute), PermissionRevision: 3,
		CreatedByUserID: fixture.user.ID, Reason: "simulation fixture", RowVersion: 1,
	}).Error)
	login := fixture.login(t, fixture.admin.Username)
	body := createSimulationBody(t, target.ID, model.UserSimulationScopeProvider, fixture.provider.ID, "reproduce provider issue", time.Now().Add(time.Hour))
	created := fixture.managementJSONRequest(http.MethodPost, "/management/user-simulations", login.Token, map[string]string{
		HeaderManagementScopeType: string(model.ManagementScopePlatform),
		HeaderIdempotencyKey:      "simulation-provider-1",
	}, body)
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	session := decodeSimulationResponse(t, created)

	current := fixture.managementRequest(http.MethodGet, "/management/contexts/current", login.Token, map[string]string{
		HeaderManagementScopeType: string(model.ManagementScopeProvider),
		HeaderManagementScopeID:   fixture.provider.ID,
		HeaderUserSimulationID:    session.ID,
	})
	require.Equal(t, http.StatusOK, current.Code, current.Body.String())
	var currentBody struct {
		Data managementContextResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(current.Body.Bytes(), &currentBody))
	require.Equal(t, string(model.ProviderManagementRoleOperator), currentBody.Data.Role)
	require.Contains(t, currentBody.Data.Permissions, "provider.resources.write")
	require.NotContains(t, currentBody.Data.Permissions, "platform.user_simulations.write")
}

func TestUserSimulationAPITerminatesWhenTargetOrActorPermissionChanges(t *testing.T) {
	fixture := newManagementContextAPIFixture(t)
	targetA := seedTenantSimulationTarget(t, fixture, "target-disabled", model.TenantManagementRoleViewer)
	targetB := seedTenantSimulationTarget(t, fixture, "actor-disabled", model.TenantManagementRoleViewer)
	login := fixture.login(t, fixture.admin.Username)
	sessionA := createSimulationThroughAPI(t, fixture, login.Token, targetA.ID, "simulation-target-invalid")
	sessionB := createSimulationThroughAPI(t, fixture, login.Token, targetB.ID, "simulation-actor-invalid")

	require.NoError(t, fixture.database.Model(&model.UserTenantManagementMembership{}).
		Where("user_id = ? AND tenant_id = ?", targetA.ID, fixture.tenant.ID).
		Updates(map[string]any{"enabled": false, "permission_revision": gorm.Expr("permission_revision + 1")}).Error)
	targetInvalid := fixture.managementRequest(http.MethodGet, "/management/contexts/current", login.Token, simulationHeadersFor(sessionA.ID, fixture.tenant.ID))
	require.Equal(t, http.StatusNotFound, targetInvalid.Code, targetInvalid.Body.String())
	var persistedSessionA model.UserSimulationSession
	require.NoError(t, fixture.database.First(&persistedSessionA, "id = ?", sessionA.ID).Error)
	require.Equal(t, model.UserSimulationSessionRevoked, persistedSessionA.Status)
	require.Equal(t, "effective_context_invalid", persistedSessionA.EndReason)

	require.NoError(t, fixture.database.Model(&model.PlatformRoleMembership{}).
		Where("user_id = ?", fixture.user.ID).
		Updates(map[string]any{"enabled": false, "permission_revision": gorm.Expr("permission_revision + 1")}).Error)
	actorInvalid := fixture.managementRequest(http.MethodGet, "/management/contexts/current", login.Token, simulationHeadersFor(sessionB.ID, fixture.tenant.ID))
	require.Equal(t, http.StatusNotFound, actorInvalid.Code, actorInvalid.Body.String())
	var persistedSessionB model.UserSimulationSession
	require.NoError(t, fixture.database.First(&persistedSessionB, "id = ?", sessionB.ID).Error)
	require.Equal(t, model.UserSimulationSessionRevoked, persistedSessionB.Status)
	require.Equal(t, "actor_permission_invalid", persistedSessionB.EndReason)
}

func seedTenantSimulationTarget(t *testing.T, fixture managementContextAPIFixture, name string, role model.TenantManagementRole) model.User {
	t.Helper()
	target := model.User{Name: name, Role: model.UserRoleClient, SecretHash: "fixture", Enabled: true}
	require.NoError(t, fixture.database.Create(&target).Error)
	require.NoError(t, fixture.database.Create(&model.UserIdentityProfile{
		UserID: target.ID, Username: name, DisplayName: name, Enabled: true, AuthRevision: 1, RowVersion: 1,
	}).Error)
	require.NoError(t, fixture.database.Create(&model.UserTenantManagementMembership{
		ID: uuid.NewString(), UserID: target.ID, TenantID: fixture.tenant.ID, Role: role, Enabled: true,
		ValidFrom: time.Now().Add(-time.Minute), PermissionRevision: 1, CreatedByUserID: fixture.user.ID,
		Reason: "simulation fixture", RowVersion: 1,
	}).Error)
	return target
}

func createSimulationThroughAPI(t *testing.T, fixture managementContextAPIFixture, token string, targetUserID uint64, idempotencyKey string) userSimulationResponse {
	t.Helper()
	body := createSimulationBody(t, targetUserID, model.UserSimulationScopeTenant, fixture.tenant.ID, "reproduce tenant issue", time.Now().Add(time.Hour))
	response := fixture.managementJSONRequest(http.MethodPost, "/management/user-simulations", token, map[string]string{
		HeaderManagementScopeType: string(model.ManagementScopePlatform),
		HeaderIdempotencyKey:      idempotencyKey,
	}, body)
	require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
	return decodeSimulationResponse(t, response)
}

func createSimulationBody(t *testing.T, targetUserID uint64, scopeType model.UserSimulationScopeType, scopeID, reason string, expiresAt time.Time) []byte {
	t.Helper()
	body, err := json.Marshal(createUserSimulationRequest{
		EffectiveUserID: targetUserID, ScopeType: scopeType, ScopeID: scopeID, Reason: reason, ExpiresAt: expiresAt,
	})
	require.NoError(t, err)
	return body
}

func decodeSimulationResponse(t *testing.T, response *httptest.ResponseRecorder) userSimulationResponse {
	t.Helper()
	var body struct {
		Data userSimulationResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	return body.Data
}

func (fixture managementContextAPIFixture) managementJSONRequest(method, path, token string, headers map[string]string, body []byte) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response := httptest.NewRecorder()
	fixture.router.ServeHTTP(response, request)
	return response
}

func simulationHeadersFor(sessionID, tenantID string) map[string]string {
	return map[string]string{
		HeaderManagementScopeType: string(model.ManagementScopeTenant),
		HeaderManagementScopeID:   tenantID,
		HeaderUserSimulationID:    sessionID,
	}
}

func requireResponseCode(t *testing.T, response *httptest.ResponseRecorder, expected string) {
	t.Helper()
	var body Response
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	require.Equal(t, expected, body.Code)
}

const servicePermissionTenantResourcesWrite = "tenant.resources.write"
