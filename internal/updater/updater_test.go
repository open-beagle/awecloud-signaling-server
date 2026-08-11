package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
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

func TestManagerStartupCleansOnlyUnreferencedArtifactsAndDownloads(t *testing.T) {
	stateDir := t.TempDir()
	artifactsDir := filepath.Join(stateDir, "artifacts")
	currentSHA := strings.Repeat("1", 64)
	previousSHA := strings.Repeat("2", 64)
	activeSHA := strings.Repeat("3", 64)
	orphanSHA := strings.Repeat("4", 64)
	paths := make(map[string]string)
	for name, sha := range map[string]string{
		"current": currentSHA, "previous": previousSHA, "active": activeSHA, "orphan": orphanSHA,
	} {
		path := filepath.Join(artifactsDir, sha, "signal_agent")
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
		require.NoError(t, os.WriteFile(path, []byte(name), 0755))
		paths[name] = path
	}
	currentLink := filepath.Join(t.TempDir(), "signal_agent")
	require.NoError(t, os.Symlink(paths["current"], currentLink))
	require.NoError(t, os.Symlink(paths["previous"], currentLink+".previous"))
	require.NoError(t, writeTaskState(stateDir, taskState{
		Status: Status{TaskID: "active-task", Phase: "staged"}, Component: "agent",
		TargetVersion: "v1.0.1", TargetCommitID: strings.Repeat("c", 40),
		TargetSHA256: activeSHA, StagedBinary: paths["active"],
	}))
	downloadPath := filepath.Join(stateDir, "downloads", "partial")
	require.NoError(t, os.MkdirAll(filepath.Dir(downloadPath), 0700))
	require.NoError(t, os.WriteFile(downloadPath, []byte("partial"), 0600))

	_, err := NewManager(Config{
		Component: "agent", CurrentVersion: "v1.0.0", CurrentCommitID: strings.Repeat("a", 40),
		CurrentSHA256: strings.Repeat("b", 64), StateDir: stateDir,
		CurrentLink: currentLink, ServiceName: "signal-agent",
	})
	require.NoError(t, err)

	for _, name := range []string{"current", "previous", "active"} {
		_, err := os.Stat(paths[name])
		require.NoError(t, err, name)
	}
	_, err = os.Stat(paths["orphan"])
	require.ErrorIs(t, err, os.ErrNotExist)
	entries, err := os.ReadDir(filepath.Join(stateDir, "downloads"))
	require.NoError(t, err)
	require.Empty(t, entries)
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

func TestHandleResumesRestartingTaskWithPersistedArtifact(t *testing.T) {
	stateDir := t.TempDir()
	directive := Directive{
		TaskID: "task-1", Component: "agent", Version: "v1.0.1", ArtifactID: "artifact-1",
		CommitID: strings.Repeat("c", 40), SHA256: strings.Repeat("d", 64),
		OS: runtime.GOOS, Arch: runtime.GOARCH, Action: "install",
	}
	require.NoError(t, writeTaskState(stateDir, taskState{
		Status:    Status{TaskID: directive.TaskID, Phase: "restarting", Progress: 100},
		Component: "agent", TargetVersion: directive.Version, TargetCommitID: directive.CommitID,
		TargetSHA256: directive.SHA256, StagedBinary: "/tmp/staged-agent",
	}))
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
	started := make(chan string, 1)
	manager.startHelper = func(_ Directive, path string) error {
		started <- path
		return nil
	}

	manager.Handle(directive)

	select {
	case path := <-started:
		require.Equal(t, "/tmp/staged-agent", path)
	case <-time.After(time.Second):
		t.Fatal("persisted updater task was not resumed")
	}
}

func TestHandleResumesInterruptedDownload(t *testing.T) {
	payload := []byte("resumed agent artifact")
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
	directive := Directive{
		TaskID: "task-1", Component: "agent", Version: "v1.0.1", ArtifactID: "artifact-1",
		DownloadURL: server.URL, Filename: "signal_agent", Size: int64(len(payload)),
		CommitID: strings.Repeat("c", 40), SHA256: digestText,
		OS: runtime.GOOS, Arch: runtime.GOARCH, Action: "install",
	}
	manager.states[directive.TaskID] = taskState{
		Status: Status{TaskID: directive.TaskID, Phase: "downloading"}, Component: "agent",
		TargetVersion: directive.Version, TargetCommitID: directive.CommitID, TargetSHA256: directive.SHA256,
	}
	started := make(chan string, 1)
	manager.startHelper = func(_ Directive, path string) error {
		started <- path
		return nil
	}

	manager.Handle(directive)

	select {
	case path := <-started:
		content, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		require.Equal(t, payload, content)
	case <-time.After(time.Second):
		t.Fatal("interrupted download was not resumed")
	}
}

func TestHelperUnitRunningIncludesSystemdTransitionStates(t *testing.T) {
	for _, state := range []string{"active", "activating", "reloading", "deactivating"} {
		require.True(t, helperUnitRunning(state), state)
	}
	for _, state := range []string{"inactive", "failed", "unknown", ""} {
		require.False(t, helperUnitRunning(state), state)
	}
}

func TestRestartAndCheckResetsSystemdFailureStateBeforeRestart(t *testing.T) {
	binDir := t.TempDir()
	logPath := filepath.Join(binDir, "calls")
	systemctl := filepath.Join(binDir, "systemctl")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$SYSTEMCTL_CALLS\"\n"
	require.NoError(t, os.WriteFile(systemctl, []byte(script), 0755))
	t.Setenv("SYSTEMCTL_CALLS", logPath)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	require.NoError(t, restartAndCheck(context.Background(), "k8s-signaling"))
	calls, err := os.ReadFile(logPath)
	require.NoError(t, err)
	require.Equal(t, "reset-failed k8s-signaling\nrestart k8s-signaling\nis-active --quiet k8s-signaling\n", string(calls))
}

func TestValidateDirectiveRejectsWrongPlatformAndAction(t *testing.T) {
	manager, err := NewManager(Config{
		Component: "agent", CurrentVersion: "v1.0.0", CurrentCommitID: strings.Repeat("a", 40),
		CurrentSHA256: strings.Repeat("b", 64), StateDir: t.TempDir(),
		CurrentLink: "/tmp/signal_agent", ServiceName: "signal-agent",
	})
	require.NoError(t, err)
	directive := Directive{
		TaskID: "task-1", Component: "agent", Version: "v1.0.1", ArtifactID: "artifact-1",
		CommitID: strings.Repeat("c", 40), SHA256: strings.Repeat("d", 64),
		OS: runtime.GOOS, Arch: runtime.GOARCH, Action: "install",
	}
	require.NoError(t, manager.validateDirective(directive))

	directive.Arch = "wrong-arch"
	require.ErrorContains(t, manager.validateDirective(directive), "platform mismatch")
	directive.Arch = runtime.GOARCH
	directive.Action = "delete"
	require.ErrorContains(t, manager.validateDirective(directive), "unsupported update action")
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
	require.NoError(t, os.Symlink("stale", filepath.Join(directory, ".signal_agent.new")))
	require.NoError(t, switchSymlink(linkPath, "new"))
	target, err := os.Readlink(linkPath)
	require.NoError(t, err)
	require.Equal(t, "new", target)
}

func TestApplyRejectsModifiedStagedArtifactBeforeSwitch(t *testing.T) {
	stateDir := t.TempDir()
	binDir := t.TempDir()
	currentBinary := filepath.Join(binDir, "current-agent")
	stagedBinary := filepath.Join(binDir, "staged-agent")
	require.NoError(t, os.WriteFile(currentBinary, []byte("current"), 0755))
	require.NoError(t, os.WriteFile(stagedBinary, []byte("modified"), 0755))
	currentLink := filepath.Join(binDir, "signal_agent")
	require.NoError(t, os.Symlink(currentBinary, currentLink))

	state := taskState{
		Status: Status{TaskID: "task-1", Phase: "staged", Sequence: 3}, Component: "agent",
		TargetVersion: "v1.0.1", TargetCommitID: strings.Repeat("c", 40),
		TargetSHA256: strings.Repeat("d", 64), StagedBinary: stagedBinary,
	}
	require.NoError(t, writeTaskState(stateDir, state))

	require.NoError(t, Apply(context.Background(), ApplyConfig{
		TaskID: "task-1", StateDir: stateDir, BinaryPath: stagedBinary,
		CurrentLink: currentLink, ServiceName: "unused",
	}))

	target, err := os.Readlink(currentLink)
	require.NoError(t, err)
	require.Equal(t, currentBinary, target)
	persisted, err := readTaskState(taskStatePath(stateDir, "task-1"))
	require.NoError(t, err)
	require.Equal(t, "failed", persisted.Phase)
	require.Equal(t, "artifact_mismatch", persisted.ErrorCode)
}

func TestApplySwitchesAndRequiresMatchingHealthConfirmation(t *testing.T) {
	stateDir := t.TempDir()
	binDir := t.TempDir()
	currentBinary := filepath.Join(binDir, "agent-a")
	stagedBinary := filepath.Join(binDir, "agent-b")
	targetPayload := []byte("agent build b")
	targetDigest := sha256.Sum256(targetPayload)
	targetSHA := hex.EncodeToString(targetDigest[:])
	require.NoError(t, os.WriteFile(currentBinary, []byte("agent build a"), 0755))
	require.NoError(t, os.WriteFile(stagedBinary, targetPayload, 0755))
	currentLink := filepath.Join(binDir, "signal_agent")
	previousLink := currentLink + ".previous"
	require.NoError(t, os.Symlink(currentBinary, currentLink))

	state := taskState{
		Status: Status{TaskID: "task-1", Phase: "staged", Sequence: 3}, Component: "agent",
		TargetVersion: "v1.0.1", TargetCommitID: strings.Repeat("c", 40),
		TargetSHA256: targetSHA, StagedBinary: stagedBinary,
	}
	require.NoError(t, writeTaskState(stateDir, state))
	restarts := 0
	restart := func(context.Context, string) error {
		restarts++
		return writeHealthFile(filepath.Join(stateDir, "health"), HealthFile{
			SchemaVersion: 2, TaskID: state.TaskID, Version: state.TargetVersion,
			CommitID: state.TargetCommitID, BinarySHA256: state.TargetSHA256,
			HeartbeatConfirmed: time.Now(),
		})
	}

	require.NoError(t, Apply(context.Background(), ApplyConfig{
		TaskID: "task-1", StateDir: stateDir, BinaryPath: stagedBinary,
		CurrentLink: currentLink, PreviousLink: previousLink, ServiceName: "signal-agent",
		restart: restart, healthTimeout: time.Second, healthPoll: time.Millisecond,
	}))

	require.Equal(t, 1, restarts)
	currentTarget, err := os.Readlink(currentLink)
	require.NoError(t, err)
	require.Equal(t, stagedBinary, currentTarget)
	previousTarget, err := os.Readlink(previousLink)
	require.NoError(t, err)
	require.Equal(t, currentBinary, previousTarget)
	persisted, err := readTaskState(taskStatePath(stateDir, "task-1"))
	require.NoError(t, err)
	require.Equal(t, "succeeded", persisted.Phase)
}

func TestApplyRollsBackWhenHealthConfirmationTimesOut(t *testing.T) {
	stateDir := t.TempDir()
	binDir := t.TempDir()
	currentBinary := filepath.Join(binDir, "agent-a")
	stagedBinary := filepath.Join(binDir, "agent-b")
	targetPayload := []byte("agent build b")
	targetDigest := sha256.Sum256(targetPayload)
	targetSHA := hex.EncodeToString(targetDigest[:])
	require.NoError(t, os.WriteFile(currentBinary, []byte("agent build a"), 0755))
	require.NoError(t, os.WriteFile(stagedBinary, targetPayload, 0755))
	currentLink := filepath.Join(binDir, "signal_agent")
	require.NoError(t, os.Symlink(currentBinary, currentLink))
	state := taskState{
		Status: Status{TaskID: "task-1", Phase: "staged", Sequence: 3}, Component: "agent",
		TargetVersion: "v1.0.1", TargetCommitID: strings.Repeat("c", 40),
		TargetSHA256: targetSHA, StagedBinary: stagedBinary,
	}
	require.NoError(t, writeTaskState(stateDir, state))
	restarts := 0

	require.NoError(t, Apply(context.Background(), ApplyConfig{
		TaskID: "task-1", StateDir: stateDir, BinaryPath: stagedBinary,
		CurrentLink: currentLink, ServiceName: "signal-agent",
		restart:       func(context.Context, string) error { restarts++; return nil },
		healthTimeout: 20 * time.Millisecond, healthPoll: time.Millisecond,
	}))

	require.Equal(t, 2, restarts)
	currentTarget, err := os.Readlink(currentLink)
	require.NoError(t, err)
	require.Equal(t, currentBinary, currentTarget)
	persisted, err := readTaskState(taskStatePath(stateDir, "task-1"))
	require.NoError(t, err)
	require.Equal(t, "rolled_back", persisted.Phase)
	require.Equal(t, "heartbeat_timeout", persisted.ErrorCode)
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
