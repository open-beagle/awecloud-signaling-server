package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

func TestAgentDownloadAndVerifyStagesUnsignedArtifact(t *testing.T) {
	payload := []byte("signed signal artifact")
	digest := sha256.Sum256(payload)
	digestText := hex.EncodeToString(digest[:])
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	manager, err := NewManager(Config{
		Component:       "agent",
		CurrentVersion:  "v1.0.0",
		CurrentCommitID: strings.Repeat("a", 40),
		CurrentSHA256:   strings.Repeat("b", 64),
		StateDir:        t.TempDir(),
		CurrentLink:     "/tmp/signal_agent",
		ServiceName:     "signal-agent",
	})
	require.NoError(t, err)
	manager.client = server.Client()

	staged, err := manager.downloadAndVerify(Directive{
		TaskID:      "task-1",
		Component:   "agent",
		Version:     "v1.2.3",
		DownloadURL: server.URL,
		Filename:    "signal_agent-v1.2.3-linux-amd64",
		Size:        int64(len(payload)),
		SHA256:      digestText,
	})
	require.NoError(t, err)
	content, err := os.ReadFile(staged)
	require.NoError(t, err)
	require.Equal(t, payload, content)
}

func TestEndpointManagerRequiresCurrentBuildIdentity(t *testing.T) {
	_, err := NewManager(Config{
		Component: "endpoint", CurrentVersion: "v1.0.2", StateDir: t.TempDir(),
		CurrentLink: "/tmp/signal_endpoint", ServiceName: "signal-endpoint",
	})
	require.EqualError(t, err, "updater current build identity is invalid")
}

func TestHandleIgnoresRepeatedDirectiveForExistingTask(t *testing.T) {
	manager, err := NewManager(Config{
		Component:       "endpoint",
		CurrentVersion:  "v1.0.2",
		CurrentCommitID: strings.Repeat("a", 40),
		CurrentSHA256:   strings.Repeat("b", 64),
		StateDir:        t.TempDir(),
		CurrentLink:     "/tmp/signal_endpoint",
		ServiceName:     "signal-endpoint",
	})
	require.NoError(t, err)
	manager.states["task-1"] = taskState{Status: Status{TaskID: "task-1", Phase: "restarting"}}

	manager.Handle(Directive{TaskID: "task-1", Component: "endpoint", Version: "v1.0.2"})

	require.NotContains(t, manager.processing, "task-1")
	require.Equal(t, "restarting", manager.states["task-1"].Phase)
}

func TestEndpointArtifactUsesDirectiveFilename(t *testing.T) {
	payload := []byte("endpoint artifact")
	digest := sha256.Sum256(payload)
	digestText := hex.EncodeToString(digest[:])
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	manager, err := NewManager(Config{
		Component:       "endpoint",
		CurrentVersion:  "v1.0.1",
		CurrentCommitID: strings.Repeat("a", 40),
		CurrentSHA256:   strings.Repeat("b", 64),
		StateDir:        t.TempDir(),
		CurrentLink:     "/tmp/signal_endpoint",
		ServiceName:     "signal-endpoint",
	})
	require.NoError(t, err)
	manager.client = server.Client()

	staged, err := manager.downloadAndVerify(Directive{
		TaskID: "task-1", Component: "endpoint", Version: "v1.0.2", DownloadURL: server.URL,
		Filename: "signal_endpoint", Size: int64(len(payload)), SHA256: digestText,
	})
	require.NoError(t, err)
	require.Equal(t, "signal_endpoint", filepath.Base(staged))
}

func TestDownloadAndVerifyRejectsInvalidChecksum(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("x"))
	}))
	defer server.Close()
	manager, err := NewManager(Config{
		Component:       "agent",
		CurrentVersion:  "v1.0.0",
		CurrentCommitID: strings.Repeat("a", 40),
		CurrentSHA256:   strings.Repeat("b", 64),
		StateDir:        t.TempDir(),
		CurrentLink:     "/tmp/signal_agent",
		ServiceName:     "signal-agent",
	})
	require.NoError(t, err)
	manager.client = server.Client()
	_, err = manager.downloadAndVerify(Directive{
		TaskID:      "task-1",
		Component:   "agent",
		Version:     "v1.2.3",
		DownloadURL: server.URL,
		Filename:    "signal_agent",
		Size:        1,
		SHA256:      "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	})
	require.EqualError(t, err, "checksum_mismatch")
}

func TestSwitchSymlinkIsAtomicReplacement(t *testing.T) {
	directory := t.TempDir()
	linkPath := filepath.Join(directory, "signal_agent")
	require.NoError(t, os.Symlink("old", linkPath))
	require.NoError(t, switchSymlink(linkPath, "new"))
	target, err := os.Readlink(linkPath)
	require.NoError(t, err)
	require.Equal(t, "new", target)
}

func TestHealthConfirmationMustMatchActiveTaskIdentity(t *testing.T) {
	stateDir := t.TempDir()
	manager, err := NewManager(Config{
		Component:       "agent",
		CurrentVersion:  "v1.0.0",
		CurrentCommitID: strings.Repeat("a", 40),
		CurrentSHA256:   strings.Repeat("b", 64),
		StateDir:        stateDir,
		CurrentLink:     "/tmp/signal_agent",
		ServiceName:     "signal-agent",
	})
	require.NoError(t, err)
	manager.states["task-1"] = taskState{
		Status:         Status{TaskID: "task-1", Phase: "restarting"},
		TargetVersion:  "v1.0.0",
		TargetCommitID: strings.Repeat("c", 40),
		TargetSHA256:   strings.Repeat("d", 64),
	}

	manager.HandleHealthConfirmations([]*pb.UpdateHealthConfirmation{{
		TaskId: "task-1", Version: "v1.0.0", CommitId: strings.Repeat("e", 40),
		ArtifactSha256: strings.Repeat("d", 64), ConfirmedAtUnix: time.Now().Unix(),
	}})
	healthPath := filepath.Join(stateDir, "health", "task-1.json")
	_, err = os.Stat(healthPath)
	require.ErrorIs(t, err, os.ErrNotExist)

	manager.HandleHealthConfirmations([]*pb.UpdateHealthConfirmation{{
		TaskId: "task-1", Version: "v1.0.0", CommitId: strings.Repeat("c", 40),
		ArtifactSha256: strings.Repeat("d", 64), ConfirmedAtUnix: time.Now().Unix(),
	}})
	require.True(t, validHealthFile(healthPath, manager.states["task-1"]))
}

func TestValidHealthFileRejectsLoosePermissions(t *testing.T) {
	healthDir := t.TempDir()
	state := taskState{
		Status:         Status{TaskID: "task-1"},
		TargetVersion:  "v1.0.0",
		TargetCommitID: strings.Repeat("c", 40),
		TargetSHA256:   strings.Repeat("d", 64),
	}
	health := HealthFile{
		SchemaVersion: 2, TaskID: state.TaskID, Version: state.TargetVersion,
		CommitID: state.TargetCommitID, BinarySHA256: state.TargetSHA256,
		HeartbeatConfirmed: time.Now(),
	}
	require.NoError(t, writeHealthFile(healthDir, health))
	path := filepath.Join(healthDir, "task-1.json")
	require.NoError(t, os.Chmod(path, 0644))
	require.False(t, validHealthFile(path, state))
}
