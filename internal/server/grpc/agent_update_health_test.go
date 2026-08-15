package grpc

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/cache"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func TestAgentHealthConfirmationUsesAuthenticatedRuntimeIdentity(t *testing.T) {
	oldCommit := strings.Repeat("a", 40)
	oldSHA256 := strings.Repeat("b", 64)
	newCommit := strings.Repeat("c", 40)
	newSHA256 := strings.Repeat("d", 64)

	store := cache.NewNodeRuntimeStore()
	store.UpsertNode(&model.Node{
		ID: 138, UserID: 1, Name: "aliyun", Type: model.NodeTypeAgent,
		Version: "v1.0.2", CommitID: oldCommit, BinarySHA256: oldSHA256,
	})
	_, err := store.UpdateHeartbeat(138, "", "", "v1.0.2", newCommit, nil, newSHA256, "", "v2", "", time.Now())
	require.NoError(t, err)

	server := &AgentServiceServer{runtimeStore: store}
	identity, ok := server.currentAgentBuildIdentity(context.Background(), 138)
	require.True(t, ok)
	require.Equal(t, newCommit, identity.CommitID)
	require.Equal(t, newSHA256, identity.BinarySHA256)

	task := model.UpdateTask{
		ID: "task-1", DesiredVersion: "v1.0.2",
		DesiredCommitID: newCommit, DesiredSHA256: newSHA256,
	}
	confirmation := agentUpdateHealthConfirmation(task, identity, time.Unix(1770000000, 0))
	require.NotNil(t, confirmation)
	require.Equal(t, task.ID, confirmation.TaskId)
}

func TestAgentHealthConfirmationRequiresExactBuildIdentity(t *testing.T) {
	task := model.UpdateTask{
		ID: "task-1", DesiredVersion: "v1.0.2",
		DesiredCommitID: strings.Repeat("c", 40), DesiredSHA256: strings.Repeat("d", 64),
	}
	current := agentBuildIdentity{
		Version: task.DesiredVersion, CommitID: task.DesiredCommitID, BinarySHA256: task.DesiredSHA256,
	}
	require.NotNil(t, agentUpdateHealthConfirmation(task, current, time.Now()))

	current.CommitID = strings.Repeat("e", 40)
	require.Nil(t, agentUpdateHealthConfirmation(task, current, time.Now()))
	current.CommitID = task.DesiredCommitID
	current.BinarySHA256 = strings.Repeat("f", 64)
	require.Nil(t, agentUpdateHealthConfirmation(task, current, time.Now()))
}
