package agent

import (
	"testing"

	"github.com/stretchr/testify/require"

	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

func TestEndpointServerRoutesHealthConfirmationByTargetName(t *testing.T) {
	server := NewEndpointServer(50052, "endpoint-token", t.Context())
	server.SetUpdateHealthConfirmations([]*pb.UpdateHealthConfirmation{
		{TaskId: "endpoint-a-task", TargetName: "endpoint-a"},
		{TaskId: "endpoint-b-task", TargetName: "endpoint-b"},
		{TaskId: "agent-task"},
	})

	confirmations := server.updateHealthConfirmationsFor("endpoint-a")
	require.Len(t, confirmations, 1)
	require.Equal(t, "endpoint-a-task", confirmations[0].TaskId)
	require.Empty(t, server.updateHealthConfirmationsFor("endpoint-c"))
}
