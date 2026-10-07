package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func setupACLWriteAuditDB(t *testing.T) *gorm.DB {
	t.Helper()
	database := setupTunnelLifecycleTestDB(t)
	require.NoError(t, database.AutoMigrate(&model.Group{}, &model.ProxyService{}, &model.Endpoint{},
		&model.AclServiceUserPermission{}, &model.AclServiceGroupPermission{},
		&model.AclUserUserPermission{}, &model.AclUserGroupPermission{},
		&model.AclGroupUserPermission{}, &model.AclGroupGroupPermission{},
		&model.AclSSHUserPermission{}, &model.AclSSHGroupPermission{},
		&model.AclK8SUserPermission{}, &model.AclK8SGroupPermission{}))
	require.NoError(t, database.Create(&model.Admin{ID: 9, Username: "audit-admin", Role: "admin"}).Error)
	require.NoError(t, database.Create(&model.User{ID: 1, Name: "audit-target", Role: model.UserRoleAgent, SecretHash: "must-not-log", SSHEnabled: true}).Error)
	require.NoError(t, database.Create(&model.Group{ID: 1, Name: "audit-group"}).Error)
	require.NoError(t, database.Create(&model.ProxyService{ID: "service", Name: "audit-service", UserID: 1}).Error)
	require.NoError(t, database.Create(&model.Endpoint{ID: "endpoint", Name: "audit-endpoint", UserID: 1}).Error)
	return database
}

func auditRequest(handler gin.HandlerFunc, method, target, member, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, "/audit-test", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("Authorization", "Bearer must-not-log-bearer")
	c.Request.Header.Set(HeaderRequestID, "audit-request")
	c.Params = gin.Params{{Key: "id", Value: target}, {Key: "uid", Value: member}, {Key: "gid", Value: member}}
	c.Set("admin_id", int64(9))
	c.Set(contextAuditActorUserID, uint64(53))
	c.Set(contextAuditEffectiveUserID, uint64(54))
	c.Set(contextAuditSimulationSessionID, "audit-simulation")
	c.Set(contextAuditScopeType, "platform")
	handler(c)
	return w
}

func lastSensitiveAudit(t *testing.T, database *gorm.DB) (model.AuditLog, sensitiveWriteDetail) {
	t.Helper()
	var entry model.AuditLog
	require.NoError(t, database.Last(&entry).Error)
	var detail sensitiveWriteDetail
	require.NoError(t, json.Unmarshal([]byte(entry.Detail), &detail))
	require.Equal(t, int64(9), entry.ActorAdminID)
	require.Equal(t, "audit-admin", entry.ActorUsername)
	require.Equal(t, uint64(53), entry.ActorUserID)
	require.Equal(t, uint64(54), entry.EffectiveUserID)
	require.Equal(t, "audit-simulation", entry.SimulationSessionID)
	require.Equal(t, "audit-request", entry.RequestID)
	require.NotContains(t, entry.Detail, "must-not-log")
	return entry, detail
}

func TestACLWriteAudit_AllRegisteredHandlers(t *testing.T) {
	database := setupACLWriteAuditDB(t)
	api := NewACLAPI(&config.ServerConfig{})
	api.aclSync = &testMockACLSyncer{}
	tests := []struct {
		kind, target                               string
		addUser, addGroup, removeUser, removeGroup gin.HandlerFunc
	}{
		{"service", "service", api.AddServiceACLUsers, api.AddServiceACLGroups, api.RemoveServiceACLUser, api.RemoveServiceACLGroup},
		{"user", "1", api.AddUserACLUsers, api.AddUserACLGroups, api.RemoveUserACLUser, api.RemoveUserACLGroup},
		{"group", "1", api.AddGroupACLUsers, api.AddGroupACLGroups, api.RemoveGroupACLUser, api.RemoveGroupACLGroup},
		{"ssh", "1", api.AddSSHACLUsers, api.AddSSHACLGroups, api.RemoveSSHACLUser, api.RemoveSSHACLGroup},
		{"k8s", "1", api.AddK8SACLUsers, api.AddK8SACLGroups, api.RemoveK8SACLUser, api.RemoveK8SACLGroup},
		{"endpoint_k8sapi", "endpoint", api.AddEndpointK8SAPIACLUsers, api.AddEndpointK8SAPIACLGroups, api.RemoveEndpointK8SAPIACLUser, api.RemoveEndpointK8SAPIACLGroup},
	}
	for _, test := range tests {
		t.Run(test.kind, func(t *testing.T) {
			for _, groups := range []bool{false, true} {
				add, remove, principal := test.addUser, test.removeUser, "user"
				body := `{"user_ids":[2,2],"ssh_users":["ubuntu"],"k8s_groups":["viewers"],"namespaces":["apps"],"token":"must-not-log"}`
				if groups {
					add, remove, principal = test.addGroup, test.removeGroup, "group"
					body = strings.Replace(body, "user_ids", "group_ids", 1)
				}
				w := auditRequest(add, http.MethodPost, test.target, "", body)
				require.Equal(t, http.StatusOK, w.Code, w.Body.String())
				entry, detail := lastSensitiveAudit(t, database)
				require.Equal(t, "grant_"+test.kind+"_acl_"+principal, entry.ActionType)
				require.Equal(t, test.target, entry.TargetID)
				require.Equal(t, "succeeded", detail.Result)
				require.True(t, detail.DatabaseCommitted)
				require.Len(t, detail.Before, 0)
				require.Len(t, detail.After, 1, "duplicate input must remain one grant")
				if strings.Contains(test.kind, "k8s") {
					require.Equal(t, "not_required", detail.SyncStatus)
				} else {
					require.Equal(t, "succeeded", detail.SyncStatus)
				}
				// Repeated grant/upsert must retain actual before and after evidence.
				w = auditRequest(add, http.MethodPost, test.target, "", strings.ReplaceAll(body, "ubuntu", "operator"))
				require.Equal(t, http.StatusOK, w.Code, w.Body.String())
				_, detail = lastSensitiveAudit(t, database)
				require.Len(t, detail.Before, 1)
				require.Len(t, detail.After, 1)
				if test.kind == "ssh" {
					data, err := json.Marshal(detail)
					require.NoError(t, err)
					require.Contains(t, string(data), "ubuntu")
					require.Contains(t, string(data), "operator")
				}
				w = auditRequest(remove, http.MethodDelete, test.target, "2", "")
				require.Equal(t, http.StatusOK, w.Code, w.Body.String())
				entry, detail = lastSensitiveAudit(t, database)
				require.Equal(t, "revoke_"+test.kind+"_acl_"+principal, entry.ActionType)
				require.Len(t, detail.Before, 1)
				require.Len(t, detail.After, 0)
				w = auditRequest(remove, http.MethodDelete, test.target, "2", "")
				require.Equal(t, http.StatusNotFound, w.Code)
				_, detail = lastSensitiveAudit(t, database)
				require.Equal(t, "failed", detail.Result)
				require.False(t, detail.DatabaseCommitted)
			}
		})
	}
}

func TestACLWriteAudit_FailuresAndAtomicity(t *testing.T) {
	database := setupACLWriteAuditDB(t)
	api := NewACLAPI(&config.ServerConfig{})
	for _, test := range []struct {
		target, body string
		status       int
	}{
		{"invalid", `{"user_ids":[2]}`, 400}, {"1", "{broken", 400},
		{"1", `{"user_ids":[]}`, 400}, {"999", `{"user_ids":[2]}`, 404},
	} {
		w := auditRequest(api.AddUserACLUsers, http.MethodPost, test.target, "", test.body)
		require.Equal(t, test.status, w.Code)
		_, detail := lastSensitiveAudit(t, database)
		require.Equal(t, "failed", detail.Result)
		require.False(t, detail.DatabaseCommitted)
	}
	// Force the second insertion to fail: the first grant must also roll back.
	require.NoError(t, database.Exec(`CREATE TRIGGER fail_second_grant BEFORE INSERT ON acl_user_user_permission WHEN NEW.granted_user_id=3 BEGIN SELECT RAISE(ABORT, 'injected write failure'); END`).Error)
	w := auditRequest(api.AddUserACLUsers, http.MethodPost, "1", "", `{"user_ids":[2,3]}`)
	require.Equal(t, http.StatusInternalServerError, w.Code)
	_, detail := lastSensitiveAudit(t, database)
	require.False(t, detail.DatabaseCommitted)
	var count int64
	require.NoError(t, database.Model(&model.AclUserUserPermission{}).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, database.Exec("DROP TRIGGER fail_second_grant").Error)
	// DB committed but network sync failed is a partial outcome, never success.
	api.aclSync = &testMockACLSyncer{syncErr: errors.New("injected sync failure")}
	w = auditRequest(api.AddUserACLUsers, http.MethodPost, "1", "", `{"user_ids":[2]}`)
	require.Equal(t, http.StatusInternalServerError, w.Code)
	_, detail = lastSensitiveAudit(t, database)
	require.Equal(t, "partial", detail.Result)
	require.True(t, detail.DatabaseCommitted)
	require.Equal(t, "failed", detail.SyncStatus)
	require.Len(t, detail.After, 1)
	// An audit failure inside the transaction must prevent the permission mutation.
	require.NoError(t, database.Exec(`CREATE TRIGGER fail_commit_audit BEFORE UPDATE ON audit_log WHEN json_extract(NEW.detail, '$.database_committed')=1 BEGIN SELECT RAISE(ABORT, 'injected audit failure'); END`).Error)
	w = auditRequest(api.RemoveUserACLUser, http.MethodDelete, "1", "2", "")
	require.Equal(t, http.StatusInternalServerError, w.Code)
	_, detail = lastSensitiveAudit(t, database)
	require.False(t, detail.DatabaseCommitted)
	require.NoError(t, database.Model(&model.AclUserUserPermission{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, database.Exec("DROP TRIGGER fail_commit_audit").Error)
	// Failure to create the attempt also stops mutation before touching permissions.
	require.NoError(t, database.Exec(`CREATE TRIGGER fail_start_audit BEFORE INSERT ON audit_log BEGIN SELECT RAISE(ABORT, 'injected audit failure'); END`).Error)
	w = auditRequest(api.RemoveUserACLUser, http.MethodDelete, "1", "2", "")
	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.NoError(t, database.Model(&model.AclUserUserPermission{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestSensitiveWriteAudit_CanceledRequestStillFinalizes(t *testing.T) {
	database := setupACLWriteAuditDB(t)
	w := auditRequest(func(c *gin.Context) {
		audit := beginSensitiveWrite(c, "test_cancel", "test", "1")
		require.NotNil(t, audit)
		defer audit.finish()
		ctx, cancel := context.WithCancel(c.Request.Context())
		c.Request = c.Request.WithContext(ctx)
		cancel()
		c.JSON(http.StatusBadRequest, NewErrorResponse("canceled"))
	}, http.MethodPost, "1", "", "")
	require.Equal(t, http.StatusBadRequest, w.Code)
	_, detail := lastSensitiveAudit(t, database)
	require.Equal(t, "failed", detail.Result)
	require.Equal(t, 400, detail.HTTPStatus)
}
