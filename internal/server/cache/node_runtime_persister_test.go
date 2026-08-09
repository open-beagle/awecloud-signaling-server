package cache

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func TestNodeRuntimePersisterPersistsReportedProtocols(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&model.Node{}))

	node := model.Node{UserID: 1, Name: "agent", Type: model.NodeTypeAgent}
	require.NoError(t, database.Create(&node).Error)
	store := NewNodeRuntimeStore()
	store.UpsertNode(&node)
	_, err = store.UpdateHeartbeat(node.ID, "100.64.0.1", "agent", "v1.0.2", "", "", `{}`, "v2", "v1", time.Now())
	require.NoError(t, err)

	NewNodeRuntimePersister(store, database).Flush(context.Background())

	var persisted model.Node
	require.NoError(t, database.First(&persisted, node.ID).Error)
	require.Equal(t, "v2", persisted.UpdaterProtocol)
	require.Equal(t, "v1", persisted.ContainerSSHProtocol)
}
