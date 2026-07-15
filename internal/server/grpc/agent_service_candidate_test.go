package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

func TestHandleContainerCandidatesUsesAuthenticatedNodeAndRefreshesLease(t *testing.T) {
	oldDB := db.DB
	t.Cleanup(func() { db.DB = oldDB })
	testDB, err := gorm.Open(sqlite.Open("file:agent_candidate_heartbeat_test?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	db.DB = testDB
	require.NoError(t, testDB.AutoMigrate(&model.DiscoveryCandidate{}))
	server := &AgentServiceServer{}

	server.handleContainerCandidates(context.Background(), 42, []*pb.ContainerDiscoveryCandidate{{
		ProviderHint: "beagle-ide", WorkspaceHint: "ws-a", GenerationHint: 7,
		ClusterId: "cluster-a", Namespace: "tenant-a", PodName: "ide-a", PodUid: "pod-a",
		ContainerName: "workspace", Ready: true, LeaseSeconds: 999999,
	}})

	var candidate model.DiscoveryCandidate
	require.NoError(t, testDB.First(&candidate).Error)
	require.Equal(t, uint64(42), candidate.AgentNodeID)
	require.Equal(t, "beagle-ide", candidate.ProviderHint)
	require.Equal(t, int64(7), candidate.GenerationHint)
	require.Equal(t, model.DiscoveryCandidateObserved, candidate.Status)
	require.NotNil(t, candidate.LeaseExpiresAt)
	require.True(t, candidate.LeaseExpiresAt.After(time.Now().Add(90*time.Second)), "expires_at=%s", candidate.LeaseExpiresAt.Format(time.RFC3339))
	require.True(t, candidate.LeaseExpiresAt.Before(time.Now().Add(3*time.Minute)), "expires_at=%s", candidate.LeaseExpiresAt.Format(time.RFC3339))

	candidate.Status = model.DiscoveryCandidateStale
	require.NoError(t, testDB.Save(&candidate).Error)
	server.handleContainerCandidates(context.Background(), 42, []*pb.ContainerDiscoveryCandidate{{
		ProviderHint: "beagle-ide", WorkspaceHint: "ws-a", GenerationHint: 7,
		ClusterId: "cluster-a", Namespace: "tenant-a", PodName: "ide-a", PodUid: "pod-a",
		ContainerName: "workspace", Ready: false, LeaseSeconds: 60,
	}})
	require.NoError(t, testDB.First(&candidate, "id = ?", candidate.ID).Error)
	require.Equal(t, model.DiscoveryCandidateObserved, candidate.Status)
	require.False(t, candidate.Ready)
}
