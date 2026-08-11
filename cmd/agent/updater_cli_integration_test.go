package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAgentBinaryUpdaterApplyCLI(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Agent updater is Linux-only")
	}
	root := t.TempDir()
	agentBinary := filepath.Join(root, "agent-b")
	build := exec.Command("go", "build", "-o", agentBinary, ".")
	build.Env = os.Environ()
	output, err := build.CombinedOutput()
	require.NoError(t, err, string(output))

	binDir := filepath.Join(root, "bin")
	stateDir := filepath.Join(root, "state")
	tasksDir := filepath.Join(stateDir, "tasks")
	healthDir := filepath.Join(stateDir, "health")
	require.NoError(t, os.MkdirAll(binDir, 0700))
	require.NoError(t, os.MkdirAll(tasksDir, 0700))
	require.NoError(t, os.MkdirAll(healthDir, 0700))
	agentA := filepath.Join(binDir, "agent-a")
	currentLink := filepath.Join(binDir, "signal_agent")
	previousLink := currentLink + ".previous"
	require.NoError(t, os.WriteFile(agentA, []byte("agent-a"), 0755))
	require.NoError(t, os.Symlink(agentA, currentLink))

	targetBytes, err := os.ReadFile(agentBinary)
	require.NoError(t, err)
	targetDigest := sha256.Sum256(targetBytes)
	targetSHA := hex.EncodeToString(targetDigest[:])
	taskID := "cli-task"
	state := map[string]any{
		"TaskID": taskID, "Phase": "staged", "Sequence": 3,
		"component": "agent", "target_version": "v1.0.1",
		"target_commit_id": "0123456789abcdef0123456789abcdef01234567",
		"target_sha256":    targetSHA, "staged_binary": agentBinary,
		"updated_at": time.Now().UTC(),
	}
	stateJSON, err := json.Marshal(state)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(tasksDir, taskID+".json"), stateJSON, 0600))

	fakeBin := filepath.Join(root, "fake-bin")
	require.NoError(t, os.MkdirAll(fakeBin, 0700))
	fakeSystemctl := filepath.Join(fakeBin, "systemctl")
	require.NoError(t, os.WriteFile(fakeSystemctl, []byte("#!/bin/sh\nexit 0\n"), 0755))

	stopHealth := make(chan struct{})
	defer close(stopHealth)
	go func() {
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopHealth:
				return
			case <-ticker.C:
				target, readErr := os.Readlink(currentLink)
				if readErr != nil || target != agentBinary {
					continue
				}
				health, marshalErr := json.Marshal(map[string]any{
					"schema_version": 2, "task_id": taskID, "version": "v1.0.1",
					"commit_id":     "0123456789abcdef0123456789abcdef01234567",
					"binary_sha256": targetSHA, "registered_at": time.Now().UTC(),
					"heartbeat_confirmed_at": time.Now().UTC(),
				})
				if marshalErr == nil {
					_ = os.WriteFile(filepath.Join(healthDir, taskID+".json"), health, 0600)
				}
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	apply := exec.CommandContext(ctx, agentBinary,
		"updater-apply",
		"--task-id", taskID,
		"--state-dir", stateDir,
		"--binary", agentBinary,
		"--current-link", currentLink,
		"--previous-link", previousLink,
		"--service", "fake-agent",
	)
	apply.Env = append(os.Environ(), "PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	output, err = apply.CombinedOutput()
	require.NoError(t, err, string(output))
	require.NoError(t, ctx.Err())

	currentTarget, err := os.Readlink(currentLink)
	require.NoError(t, err)
	require.Equal(t, agentBinary, currentTarget)
	previousTarget, err := os.Readlink(previousLink)
	require.NoError(t, err)
	require.Equal(t, agentA, previousTarget)
	resultJSON, err := os.ReadFile(filepath.Join(tasksDir, taskID+".json"))
	require.NoError(t, err)
	var result map[string]any
	require.NoError(t, json.Unmarshal(resultJSON, &result))
	require.Equal(t, "succeeded", result["Phase"])
}
