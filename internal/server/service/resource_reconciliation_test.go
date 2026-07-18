package service

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func TestExpireCandidatesMarksOnlyLeaseExpiredObservations(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:resource_reconciliation_expire_test?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&model.DiscoveryCandidate{}, &model.Resource{}))

	now := time.Now().UTC()
	expired := now.Add(-time.Minute)
	future := now.Add(time.Minute)
	newCandidate := func(status model.DiscoveryCandidateStatus, lease *time.Time) model.DiscoveryCandidate {
		return model.DiscoveryCandidate{
			ID: uuid.NewString(), AgentNodeID: 1, Namespace: "acme", PodUID: uuid.NewString(),
			ContainerName: "workspace", Status: status, LeaseExpiresAt: lease,
		}
	}
	resource := model.Resource{ID: uuid.NewString(), TenantID: uuid.NewString(), Type: model.ResourceTypeContainerSSH, DisplayName: "IDE", State: model.ResourceStateAvailable, TargetRevision: 1}
	require.NoError(t, database.Create(&resource).Error)
	candidates := []model.DiscoveryCandidate{
		newCandidate(model.DiscoveryCandidateObserved, &expired),
		newCandidate(model.DiscoveryCandidatePendingClaim, &expired),
		{ID: uuid.NewString(), AgentNodeID: 1, Namespace: "acme", PodUID: uuid.NewString(), ContainerName: "workspace", Status: model.DiscoveryCandidatePublished, LeaseExpiresAt: &expired, ResourceID: resource.ID},
		newCandidate(model.DiscoveryCandidateRejected, &expired),
		newCandidate(model.DiscoveryCandidateObserved, &future),
	}
	for i := range candidates {
		require.NoError(t, database.Create(&candidates[i]).Error)
	}

	count, err := NewResourceReconciliationService(database).ExpireCandidates(context.Background(), now)
	require.NoError(t, err)
	require.Equal(t, int64(3), count)

	var refreshed []model.DiscoveryCandidate
	require.NoError(t, database.Find(&refreshed).Error)
	for _, candidate := range refreshed {
		switch candidate.ID {
		case candidates[0].ID, candidates[1].ID, candidates[2].ID:
			require.Equal(t, model.DiscoveryCandidateStale, candidate.Status)
			require.Equal(t, "Agent 观测租约已过期", candidate.ConflictReason)
		case candidates[3].ID:
			require.Equal(t, model.DiscoveryCandidateRejected, candidate.Status)
		case candidates[4].ID:
			require.Equal(t, model.DiscoveryCandidateObserved, candidate.Status)
		}
	}
	require.NoError(t, database.First(&resource, "id = ?", resource.ID).Error)
	require.Equal(t, model.ResourceStatePending, resource.State)
}
