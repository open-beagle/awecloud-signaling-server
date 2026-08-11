package agent

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

func TestAgentUpdateDirectivePreservesPlatformAndAction(t *testing.T) {
	directive := updateDirectiveFromProto(&pb.UpdateDirective{
		TaskId: "task-1", Component: "agent", Version: "v1.0.1", ArtifactId: "artifact-1",
		DownloadUrl: "https://cache.example/agent", Filename: "signal_agent", Size: 128,
		Sha256:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		CommitId: "1111111111111111111111111111111111111111",
		Os:       runtime.GOOS, Arch: runtime.GOARCH, Action: "install",
	})

	require.Equal(t, runtime.GOOS, directive.OS)
	require.Equal(t, runtime.GOARCH, directive.Arch)
	require.Equal(t, "install", directive.Action)
}
