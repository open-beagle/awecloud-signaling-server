package grpc

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func TestEndpointHealthConfirmationRequiresExactSameVersionBuildIdentity(t *testing.T) {
	now := time.Unix(1770000000, 0)
	task := model.UpdateTask{
		ID: "task-1", DesiredVersion: "v1.0.2",
		DesiredCommitID: "2222222222222222222222222222222222222222",
		DesiredSHA256:   "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	}
	endpoint := model.Endpoint{
		ID: "endpoint-id", Name: "endpoint-a", Version: "v1.0.2",
		CommitID:     "2222222222222222222222222222222222222222",
		BinarySHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	}

	confirmation := endpointUpdateHealthConfirmation(task, endpoint, now)
	require.NotNil(t, confirmation)
	require.Equal(t, "endpoint-a", confirmation.TargetName)
	require.Equal(t, now.Unix(), confirmation.ConfirmedAtUnix)

	endpoint.CommitID = "1111111111111111111111111111111111111111"
	require.Nil(t, endpointUpdateHealthConfirmation(task, endpoint, now))
	endpoint.CommitID = task.DesiredCommitID
	endpoint.BinarySHA256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	require.Nil(t, endpointUpdateHealthConfirmation(task, endpoint, now))
}
