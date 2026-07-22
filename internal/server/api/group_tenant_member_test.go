package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func TestAddTenantGroupMembersRejectsMixedCrossTenantBatch(t *testing.T) {
	oldDB := db.DB
	t.Cleanup(func() { db.DB = oldDB })
	testDB, err := gorm.Open(sqlite.Open("file:group_tenant_member_test?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	db.DB = testDB
	require.NoError(t, testDB.AutoMigrate(&model.User{}, &model.Tenant{}, &model.TenantMembership{}, &model.Group{}, &model.GroupMember{}))

	tenantA := model.Tenant{ID: uuid.NewString(), Key: "group-a", Name: "Group A", Status: model.TenantStatusActive}
	tenantB := model.Tenant{ID: uuid.NewString(), Key: "group-b", Name: "Group B", Status: model.TenantStatusActive}
	require.NoError(t, testDB.Create(&tenantA).Error)
	require.NoError(t, testDB.Create(&tenantB).Error)
	memberA := model.User{Name: "member-a", Role: model.UserRoleClient, Enabled: true}
	memberB := model.User{Name: "member-b", Role: model.UserRoleClient, Enabled: true}
	require.NoError(t, testDB.Create(&memberA).Error)
	require.NoError(t, testDB.Create(&memberB).Error)
	require.NoError(t, testDB.Create(&model.TenantMembership{TenantID: tenantA.ID, UserID: memberA.ID, Role: "member", Enabled: true}).Error)
	require.NoError(t, testDB.Create(&model.TenantMembership{TenantID: tenantB.ID, UserID: memberB.ID, Role: "member", Enabled: true}).Error)
	group := model.Group{TenantID: tenantA.ID, Name: "tenant-a-group"}
	require.NoError(t, testDB.Create(&group).Error)

	api := NewGroupAPINew(&config.ServerConfig{})
	router := gin.New()
	router.POST("/groups/:id/members", api.AddMembers)
	groupID := strconv.FormatInt(group.ID, 10)
	memberAID := strconv.FormatUint(memberA.ID, 10)
	memberBID := strconv.FormatUint(memberB.ID, 10)
	request := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/groups/"+groupID+"/members", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Tenant-ID", tenantA.ID)
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)
		return resp
	}

	mixed := request(`{"user_ids":[` + memberAID + `,` + memberBID + `]}`)
	require.Equal(t, http.StatusForbidden, mixed.Code)
	var count int64
	require.NoError(t, testDB.Model(&model.GroupMember{}).Where("group_id = ?", group.ID).Count(&count).Error)
	require.Zero(t, count)

	require.Equal(t, http.StatusOK, request(`{"user_ids":[`+memberAID+`]}`).Code)
	require.Equal(t, http.StatusOK, request(`{"user_ids":[`+memberAID+`]}`).Code)
	require.NoError(t, testDB.Model(&model.GroupMember{}).Where("group_id = ?", group.ID).Count(&count).Error)
	require.Equal(t, int64(1), count)
}
