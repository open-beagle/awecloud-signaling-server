package service

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func TestEnsureContainerSSHPortScopesPortsByAgent(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:container_port_allocator_test?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&model.Resource{}))

	resources := []model.Resource{
		{ID: uuid.NewString(), TenantID: uuid.NewString(), Type: model.ResourceTypeContainerSSH, DisplayName: "agent-a-1"},
		{ID: uuid.NewString(), TenantID: uuid.NewString(), Type: model.ResourceTypeContainerSSH, DisplayName: "agent-a-2"},
		{ID: uuid.NewString(), TenantID: uuid.NewString(), Type: model.ResourceTypeContainerSSH, DisplayName: "agent-b-1"},
	}
	for i := range resources {
		require.NoError(t, database.Create(&resources[i]).Error)
	}

	require.NoError(t, EnsureContainerSSHPort(database, &resources[0], 10))
	require.NoError(t, EnsureContainerSSHPort(database, &resources[1], 10))
	require.NoError(t, EnsureContainerSSHPort(database, &resources[2], 20))
	require.Equal(t, ContainerSSHPortBase, resources[0].ContainerSSHPort)
	require.Equal(t, ContainerSSHPortBase+1, resources[1].ContainerSSHPort)
	require.Equal(t, ContainerSSHPortBase, resources[2].ContainerSSHPort)
}

func TestEnsureContainerSSHPortKeepsPortAndReallocatesOnAgentMove(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:container_port_agent_move_test?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&model.Resource{}))

	resource := model.Resource{ID: uuid.NewString(), TenantID: uuid.NewString(), Type: model.ResourceTypeContainerSSH, DisplayName: "moving"}
	occupied := model.Resource{ID: uuid.NewString(), TenantID: uuid.NewString(), Type: model.ResourceTypeContainerSSH, DisplayName: "occupied", AgentNodeID: 20, ContainerSSHPort: ContainerSSHPortBase}
	require.NoError(t, database.Create(&resource).Error)
	require.NoError(t, database.Create(&occupied).Error)

	require.NoError(t, EnsureContainerSSHPort(database, &resource, 10))
	firstPort := resource.ContainerSSHPort
	require.NoError(t, EnsureContainerSSHPort(database, &resource, 10))
	require.Equal(t, firstPort, resource.ContainerSSHPort)

	require.NoError(t, EnsureContainerSSHPort(database, &resource, 20))
	require.Equal(t, uint64(20), resource.AgentNodeID)
	require.Equal(t, ContainerSSHPortBase+1, resource.ContainerSSHPort)
}

func TestResourcePortIndexAllowsLegacyUnallocatedRows(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:container_port_legacy_rows_test?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&model.Resource{}))

	for i := 0; i < 2; i++ {
		resource := model.Resource{ID: uuid.NewString(), TenantID: uuid.NewString(), Type: model.ResourceTypeContainerSSH, DisplayName: "legacy"}
		require.NoError(t, database.Create(&resource).Error)
	}
}
