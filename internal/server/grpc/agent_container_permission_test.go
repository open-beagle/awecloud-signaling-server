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

func TestContainerSSHPermissionsAreAgentScopedAndRequireLiveShellGrant(t *testing.T) {
	oldDB := db.DB
	t.Cleanup(func() { db.DB = oldDB })
	testDB, err := gorm.Open(sqlite.Open("file:agent_container_permissions_test?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	db.DB = testDB
	require.NoError(t, testDB.AutoMigrate(&model.User{}, &model.Tenant{}, &model.TenantMembership{}, &model.Resource{}, &model.ResourceTarget{}, &model.AccessGrant{}))

	now := time.Now()
	user := model.User{Name: "desktop-user", Role: model.UserRoleClient, SecretHash: "test", Enabled: true}
	require.NoError(t, testDB.Create(&user).Error)
	tenant := model.Tenant{ID: uuid.NewString(), Key: "acme", Name: "Acme", Status: model.TenantStatusActive}
	require.NoError(t, testDB.Create(&tenant).Error)
	require.NoError(t, testDB.Create(&model.TenantMembership{TenantID: tenant.ID, UserID: user.ID, Enabled: true}).Error)

	allowed := newContainerPermissionResource(11, "pod-a", true)
	otherAgent := newContainerPermissionResource(12, "pod-b", true)
	notReady := newContainerPermissionResource(11, "pod-c", false)
	allowed.TenantID = tenant.ID
	otherAgent.TenantID = tenant.ID
	notReady.TenantID = tenant.ID
	require.NoError(t, testDB.Create(&allowed).Error)
	require.NoError(t, testDB.Create(&otherAgent).Error)
	require.NoError(t, testDB.Create(&notReady).Error)
	for _, resource := range []model.Resource{allowed, otherAgent, notReady} {
		target := model.ResourceTarget{ResourceID: resource.ID, Revision: resource.TargetRevision, AgentNodeID: resource.AgentNodeID, Namespace: "dev", PodName: resource.PodName, PodUID: resource.PodUID, ContainerName: resource.ContainerName, Ready: resource.ID != notReady.ID, ObservedAt: now}
		require.NoError(t, testDB.Create(&target).Error)
		grant := model.AccessGrant{ID: uuid.NewString(), TenantID: tenant.ID, ResourceID: resource.ID, SubjectType: "user", SubjectUserID: user.ID, Actions: `["shell"]`, ValidFrom: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), MaxSessionSeconds: 600, Revision: 7, Status: "enabled"}
		require.NoError(t, testDB.Create(&grant).Error)
	}
	var readyTargets int64
	require.NoError(t, testDB.Model(&model.ResourceTarget{}).Where("resource_id = ? AND revision = ? AND agent_node_id = ? AND ready = ?", allowed.ID, allowed.TargetRevision, allowed.AgentNodeID, true).Count(&readyTargets).Error)
	require.Equal(t, int64(1), readyTargets)

	result := (&AgentServiceServer{}).queryContainerSSHPermissions(context.Background(), 11)
	require.Len(t, result, 1)
	require.Equal(t, allowed.ID, result[0].ResourceId)
	require.Equal(t, "desktop-user", result[0].UserName)
	require.Equal(t, int64(7), result[0].GrantRevision)
	require.Equal(t, int32(600), result[0].MaxSessionSeconds)
}

func newContainerPermissionResource(agentNodeID uint64, suffix string, ready bool) model.Resource {
	state := model.ResourceStateAvailable
	if !ready {
		state = model.ResourceStateDegraded
	}
	return model.Resource{ID: uuid.NewString(), TenantID: "", Type: model.ResourceTypeContainerSSH, DisplayName: suffix, AgentNodeID: agentNodeID, Namespace: "dev", PodName: suffix, PodUID: suffix + "-uid", ContainerName: "workspace", TargetRevision: 2, State: state}
}
