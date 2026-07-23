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

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func TestTenantGroupOnlyAcceptsCurrentEffectiveMembers(t *testing.T) {
	oldDB := db.DB
	t.Cleanup(func() { db.DB = oldDB })
	database, err := gorm.Open(sqlite.Open("file:tenant_group_membership_test?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	db.DB = database
	require.NoError(t, database.AutoMigrate(
		&model.Admin{}, &model.AdminTenantMembership{}, &model.Tenant{}, &model.TenantMembership{},
		&model.User{}, &model.Group{}, &model.GroupMember{}, &model.AuditLog{},
	))
	tenant := model.Tenant{ID: uuid.NewString(), Key: "group-scope", Name: "Group Scope", Status: model.TenantStatusActive}
	admin := model.Admin{Username: "group-scope-admin", PasswordHash: "test", Role: "tenant_admin", Enabled: true}
	require.NoError(t, database.Create(&tenant).Error)
	require.NoError(t, database.Create(&admin).Error)
	require.NoError(t, database.Create(&model.AdminTenantMembership{AdminID: admin.ID, TenantID: tenant.ID, Role: "tenant_admin", Enabled: true}).Error)
	valid := model.User{Name: "group-valid", Role: model.UserRoleClient, SecretHash: "test", Enabled: true}
	expired := model.User{Name: "group-expired", Role: model.UserRoleClient, SecretHash: "test", Enabled: true}
	outsider := model.User{Name: "group-outsider", Role: model.UserRoleClient, SecretHash: "test", Enabled: true}
	require.NoError(t, database.Create(&valid).Error)
	require.NoError(t, database.Create(&expired).Error)
	require.NoError(t, database.Create(&outsider).Error)
	expiredAt := time.Now().Add(-time.Minute)
	require.NoError(t, database.Create(&model.TenantMembership{TenantID: tenant.ID, UserID: valid.ID, Role: "member", Enabled: true}).Error)
	require.NoError(t, database.Create(&model.TenantMembership{TenantID: tenant.ID, UserID: expired.ID, Role: "member", Enabled: true, ExpiresAt: &expiredAt}).Error)
	group := model.Group{TenantID: tenant.ID, Name: "developers"}
	require.NoError(t, database.Create(&group).Error)
	require.NoError(t, database.Create(&model.GroupMember{GroupID: group.ID, UserID: expired.ID}).Error)

	groupAPI := NewGroupAPINew(&config.ServerConfig{})
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("admin_id", admin.ID) })
	router.GET("/groups/:id/members", groupAPI.GetMembers)
	router.POST("/groups/:id/members", groupAPI.AddMembers)

	body, err := json.Marshal(map[string]interface{}{"user_ids": []uint64{valid.ID, expired.ID, outsider.ID}})
	require.NoError(t, err)
	addRequest := httptest.NewRequest(http.MethodPost, "/groups/"+strconv.FormatInt(group.ID, 10)+"/members", bytes.NewReader(body))
	addRequest.Header.Set("Content-Type", "application/json")
	addRequest.Header.Set("X-Tenant-ID", tenant.ID)
	addResponse := httptest.NewRecorder()
	router.ServeHTTP(addResponse, addRequest)
	require.Equal(t, http.StatusForbidden, addResponse.Code)

	validBody, err := json.Marshal(map[string]interface{}{"user_ids": []uint64{valid.ID}})
	require.NoError(t, err)
	validRequest := httptest.NewRequest(http.MethodPost, "/groups/"+strconv.FormatInt(group.ID, 10)+"/members", bytes.NewReader(validBody))
	validRequest.Header.Set("Content-Type", "application/json")
	validRequest.Header.Set("X-Tenant-ID", tenant.ID)
	validResponse := httptest.NewRecorder()
	router.ServeHTTP(validResponse, validRequest)
	require.Equal(t, http.StatusOK, validResponse.Code)

	listRequest := httptest.NewRequest(http.MethodGet, "/groups/"+strconv.FormatInt(group.ID, 10)+"/members", nil)
	listRequest.Header.Set("X-Tenant-ID", tenant.ID)
	listResponse := httptest.NewRecorder()
	router.ServeHTTP(listResponse, listRequest)
	require.Equal(t, http.StatusOK, listResponse.Code)
	var response struct {
		Data []model.GroupMember `json:"data"`
	}
	require.NoError(t, json.Unmarshal(listResponse.Body.Bytes(), &response))
	require.Len(t, response.Data, 1)
	require.Equal(t, valid.ID, response.Data[0].UserID)

	var audit model.AuditLog
	require.NoError(t, database.Where("action_type = ?", "add_tenant_group_members").First(&audit).Error)
	require.Equal(t, tenant.ID, audit.TenantID)
	require.Equal(t, PermissionTenantGroupsWrite, audit.RequiredPermission)
}
