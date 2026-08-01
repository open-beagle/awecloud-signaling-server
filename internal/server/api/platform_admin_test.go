package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	serverdb "github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func TestPlatformAdminLifecycleAndSelfProtection(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:platform_admin_lifecycle_test?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(
		&model.Admin{}, &model.AdminTenantMembership{}, &model.User{}, &model.Tenant{},
		&model.UserIdentityProfile{}, &model.UserAuthenticationLink{}, &model.PlatformRoleMembership{},
		&model.UserTenantManagementMembership{}, &model.AuditLog{},
	))
	serverdb.DB = database
	operator := model.Admin{Username: "root-operator", PasswordHash: "test", Role: "admin", Enabled: true}
	require.NoError(t, database.Create(&operator).Error)
	api := NewPlatformAdminAPI()
	router := tenantManagementRouter(operator.ID, func(router *gin.Engine) {
		router.GET("/platform-admins", api.List)
		router.POST("/platform-admins", api.Create)
		router.PUT("/platform-admins/:id", api.Update)
	})

	createRequest := httptest.NewRequest(http.MethodPost, "/platform-admins", bytes.NewBufferString(`{"username":"scoped-manager","password":"initial-password","platform_role":"none","enabled":true}`))
	createRequest.Header.Set("Content-Type", "application/json")
	createResponse := httptest.NewRecorder()
	router.ServeHTTP(createResponse, createRequest)
	require.Equal(t, http.StatusCreated, createResponse.Code)
	require.NotContains(t, createResponse.Body.String(), "password")

	var created model.Admin
	require.NoError(t, database.Where("username = ?", "scoped-manager").First(&created).Error)
	require.Equal(t, string(model.PlatformRoleNone), created.Role)
	require.NotEqual(t, "initial-password", created.PasswordHash)
	var link model.UserAuthenticationLink
	require.NoError(t, database.Where("provider_type = ? AND provider_subject = ?",
		model.AuthenticationProviderLegacyAdmin, strconv.FormatInt(created.ID, 10)).First(&link).Error)
	var profile model.UserIdentityProfile
	require.NoError(t, database.First(&profile, "user_id = ?", link.UserID).Error)
	require.Equal(t, created.Username, profile.Username)
	var platformMembershipCount int64
	require.NoError(t, database.Model(&model.PlatformRoleMembership{}).Where("user_id = ?", link.UserID).Count(&platformMembershipCount).Error)
	require.Zero(t, platformMembershipCount)

	updateRequest := httptest.NewRequest(http.MethodPut, "/platform-admins/"+strconv.FormatInt(created.ID, 10), bytes.NewBufferString(`{"platform_role":"platform_viewer","enabled":false}`))
	updateRequest.Header.Set("Content-Type", "application/json")
	updateResponse := httptest.NewRecorder()
	router.ServeHTTP(updateResponse, updateRequest)
	require.Equal(t, http.StatusOK, updateResponse.Code)
	require.NoError(t, database.First(&created, created.ID).Error)
	require.Equal(t, string(model.PlatformRoleViewer), created.Role)
	require.False(t, created.Enabled)
	var platformMembership model.PlatformRoleMembership
	require.NoError(t, database.First(&platformMembership, "user_id = ?", link.UserID).Error)
	require.Equal(t, model.PlatformRoleViewer, platformMembership.Role)
	require.False(t, platformMembership.Enabled)
	require.NoError(t, database.First(&profile, "user_id = ?", link.UserID).Error)
	require.False(t, profile.Enabled)

	selfRequest := httptest.NewRequest(http.MethodPut, "/platform-admins/"+strconv.FormatInt(operator.ID, 10), bytes.NewBufferString(`{"platform_role":"platform_viewer","enabled":true}`))
	selfRequest.Header.Set("Content-Type", "application/json")
	selfResponse := httptest.NewRecorder()
	router.ServeHTTP(selfResponse, selfRequest)
	require.Equal(t, http.StatusConflict, selfResponse.Code)

	listResponse := httptest.NewRecorder()
	router.ServeHTTP(listResponse, httptest.NewRequest(http.MethodGet, "/platform-admins?search=scoped", nil))
	require.Equal(t, http.StatusOK, listResponse.Code)
	var listBody struct {
		Success bool                `json:"success"`
		Data    []platformAdminItem `json:"data"`
	}
	require.NoError(t, json.Unmarshal(listResponse.Body.Bytes(), &listBody))
	require.True(t, listBody.Success)
	require.Len(t, listBody.Data, 1)
	require.Equal(t, model.PlatformRoleViewer, listBody.Data[0].PlatformRole)

	var auditCount int64
	require.NoError(t, database.Model(&model.AuditLog{}).Where("action_type IN ?", []string{"create_platform_admin", "update_platform_admin"}).Count(&auditCount).Error)
	require.Equal(t, int64(2), auditCount)
}
