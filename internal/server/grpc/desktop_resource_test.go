package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func TestDesktopContainerResourcesRequireTenantMembershipAndLiveGrant(t *testing.T) {
	oldDB := db.DB
	t.Cleanup(func() { db.DB = oldDB })
	testDB, err := gorm.Open(sqlite.Open("file:desktop_container_resources_test?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	db.DB = testDB
	require.NoError(t, testDB.AutoMigrate(&model.User{}, &model.Tenant{}, &model.TenantMembership{}, &model.Resource{}, &model.AccessGrant{}))
	client := model.User{Name: "desktop-user", Role: model.UserRoleClient, SecretHash: "test", Enabled: true}
	require.NoError(t, testDB.Create(&client).Error)
	tenant := model.Tenant{ID: uuid.NewString(), Key: "acme", Name: "Acme", Status: model.TenantStatusActive}
	require.NoError(t, testDB.Create(&tenant).Error)
	require.NoError(t, testDB.Create(&model.TenantMembership{TenantID: tenant.ID, UserID: client.ID, Role: "member", Enabled: true}).Error)
	available := model.Resource{ID: uuid.NewString(), TenantID: tenant.ID, Type: model.ResourceTypeContainerSSH, DisplayName: "IDE A", ProviderID: "beagle-ide", ExternalWorkspaceID: "ws-a", State: model.ResourceStateAvailable, TargetRevision: 4, AgentNodeID: 22, ClusterID: "dev"}
	pending := model.Resource{ID: uuid.NewString(), TenantID: tenant.ID, Type: model.ResourceTypeContainerSSH, DisplayName: "IDE Pending", State: model.ResourceStatePending, TargetRevision: 0}
	revoked := model.Resource{ID: uuid.NewString(), TenantID: tenant.ID, Type: model.ResourceTypeContainerSSH, DisplayName: "IDE Revoked", State: model.ResourceStateRevoked, TargetRevision: 2}
	require.NoError(t, testDB.Create(&available).Error)
	require.NoError(t, testDB.Create(&pending).Error)
	require.NoError(t, testDB.Create(&revoked).Error)
	now := time.Now()
	grants := []model.AccessGrant{
		{ID: uuid.NewString(), TenantID: tenant.ID, ResourceID: available.ID, SubjectType: "user", SubjectUserID: client.ID, Actions: `["shell"]`, ValidFrom: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), Status: "enabled"},
		{ID: uuid.NewString(), TenantID: tenant.ID, ResourceID: pending.ID, SubjectType: "user", SubjectUserID: client.ID, Actions: `["shell"]`, ValidFrom: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), Status: "enabled"},
		{ID: uuid.NewString(), TenantID: tenant.ID, ResourceID: revoked.ID, SubjectType: "user", SubjectUserID: client.ID, Actions: `["shell"]`, ValidFrom: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), Status: "enabled"},
	}
	require.NoError(t, testDB.Create(&grants).Error)

	result := (&DesktopServiceServer{}).queryContainerSSHResourcesGRPC(context.Background(), client.ID)
	require.Len(t, result, 1)
	require.Equal(t, available.ID, result[0].ResourceId)
	require.Equal(t, int64(4), result[0].TargetRevision)
	require.Equal(t, "container_ssh", result[0].Capability)
}
