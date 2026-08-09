package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/open-beagle/awecloud-signaling-server/internal/updater"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

func TestEndpointUpdateDirectivePreservesBuildIdentity(t *testing.T) {
	directive := updateDirectiveFromProto(&pb.UpdateDirective{
		TaskId: "task-1", Component: "endpoint", Version: "v1.0.2", ArtifactId: "artifact-1",
		DownloadUrl: "https://cache.example/endpoint", Filename: "signal_endpoint", Size: 128,
		Sha256:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		CommitId: "1111111111111111111111111111111111111111", Force: true,
	})

	require.Equal(t, "artifact-1", directive.ArtifactID)
	require.Equal(t, "1111111111111111111111111111111111111111", directive.CommitID)
	require.Equal(t, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", directive.SHA256)
	require.True(t, directive.Force)
}

func TestEndpointUpdateStatusReportsCurrentBuildIdentity(t *testing.T) {
	statuses := toProtoUpdateStatuses([]updater.Status{{
		TaskID: "task-1", Phase: "succeeded", CurrentVersion: "v1.0.2",
		CurrentCommitID: "2222222222222222222222222222222222222222",
		CurrentSHA256:   "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	}})

	require.Len(t, statuses, 1)
	require.Equal(t, "2222222222222222222222222222222222222222", statuses[0].CurrentCommitId)
	require.Equal(t, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", statuses[0].CurrentSha256)
}

func TestEndpointUpgradeScriptUsesDigestWithoutNewToken(t *testing.T) {
	script, err := os.ReadFile(filepath.Join("..", "..", "scripts", "install_endpoint.sh"))
	require.NoError(t, err)
	content := string(script)
	require.Contains(t, content, `/api/v1/download/endpoint/version`)
	require.Contains(t, content, `binary_filename="${BINARY_NAME}-${artifact_sha}"`)
	require.Contains(t, content, `downloaded_sha=$(sha256sum "$tmp_file"`)
	require.Contains(t, content, `if [[ "$UPGRADE_MODE" == "true" ]]; then`)
	require.Contains(t, content, `return`)
}
