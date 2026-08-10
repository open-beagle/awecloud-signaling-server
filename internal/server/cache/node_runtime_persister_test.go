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
	require.NoError(t, database.AutoMigrate(&model.Node{}, &model.TechnicalResource{}, &model.TechnicalResourceBinding{}, &model.SupplyCandidate{}))

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

func TestNodeRuntimePersisterRefreshesTechnicalResourceLeaseWithoutChangingConfigRevision(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:technical_resource_runtime_persister?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(
		&model.Node{}, &model.TechnicalResource{}, &model.TechnicalResourceBinding{}, &model.SupplyCandidate{},
	))

	node := model.Node{UserID: 1, Name: "agent", Type: model.NodeTypeAgent}
	require.NoError(t, database.Create(&node).Error)
	resource := model.TechnicalResource{
		ID: "resource-a", ProviderID: "provider-a", Type: model.TechnicalResourceAgent, StableKey: "agent-a", DomainLabel: "agent-a",
		LifecycleState: model.TechnicalResourceRegistered, HealthState: model.ResourceHealthOffline,
		CredentialRevision: 1, ConfigRevision: 4, ObservedRevision: 3, RowVersion: 1,
	}
	require.NoError(t, database.Create(&resource).Error)
	require.NoError(t, database.Create(&model.TechnicalResourceBinding{
		ID: "binding-a", TechnicalResourceID: resource.ID, SourceType: model.TechnicalResourceBindingLegacyNode,
		SourceID: "1", CredentialRevision: 1, Enabled: true, BoundByUserID: 1, Reason: "test", RowVersion: 1,
	}).Error)
	candidate := model.SupplyCandidate{
		ID: "candidate-a", ProviderID: resource.ProviderID, TechnicalResourceID: resource.ID,
		ResourceType: model.SupplyResourceHost, StableKey: "legacy-host-legacy_node:1",
		IdentityQuality: model.SupplyIdentityStrong, PayloadHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		FirstObservedAt: time.Now().Add(-time.Hour), LastObservedAt: time.Now().Add(-time.Hour),
		LeaseExpiresAt: time.Now().Add(time.Minute), ReviewState: model.SupplyCandidateLinked, RowVersion: 7,
	}
	require.NoError(t, database.Create(&candidate).Error)

	reportedAt := time.Now().UTC()
	store := NewNodeRuntimeStore()
	store.UpsertNode(&node)
	_, err = store.UpdateHeartbeat(node.ID, "100.64.0.1", "agent", "v1.0.2", "", "", `{}`, "v2", "v1", reportedAt)
	require.NoError(t, err)
	NewNodeRuntimePersister(store, database).Flush(context.Background())

	require.NoError(t, database.First(&resource, "id = ?", resource.ID).Error)
	require.Equal(t, model.ResourceHealthOnline, resource.HealthState)
	require.Equal(t, int64(3), resource.ObservedRevision)
	require.NotNil(t, resource.LeaseExpiresAt)
	require.True(t, resource.LeaseExpiresAt.After(reportedAt))
	require.NoError(t, database.First(&candidate, "id = ?", candidate.ID).Error)
	require.Equal(t, int64(7), candidate.RowVersion)
	require.WithinDuration(t, reportedAt, candidate.LastObservedAt, time.Second)
}
