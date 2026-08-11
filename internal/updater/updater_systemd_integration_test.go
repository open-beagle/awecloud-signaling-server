package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestApplyWithRealSystemd(t *testing.T) {
	if os.Getenv("SIGNAL_RUN_SYSTEMD_UPDATER_TEST") != "1" {
		t.Skip("set SIGNAL_RUN_SYSTEMD_UPDATER_TEST=1 on a disposable systemd host")
	}
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		t.Fatal("real systemd updater test requires Linux and root")
	}
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		t.Fatalf("systemd is not running: %v", err)
	}

	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	binDir := filepath.Join(root, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0700))
	buildA := filepath.Join(binDir, "agent-a.sh")
	buildB := filepath.Join(binDir, "agent-b.sh")
	buildABytes := []byte("#!/bin/sh\nexit 0\n")
	buildBBytes := []byte("#!/bin/sh\nwhile :; do sleep 1; done\n# build-b\n")
	require.NoError(t, os.WriteFile(buildA, buildABytes, 0755))
	require.NoError(t, os.WriteFile(buildB, buildBBytes, 0755))
	currentLink := filepath.Join(binDir, "signal_agent")
	require.NoError(t, os.Symlink(buildA, currentLink))

	digest := sha256.Sum256(buildBBytes)
	targetSHA := hex.EncodeToString(digest[:])
	state := taskState{
		Status: Status{TaskID: "systemd-task", Phase: "staged", Sequence: 3}, Component: "agent",
		TargetVersion: "v1.0.1", TargetCommitID: strings.Repeat("c", 40),
		TargetSHA256: targetSHA, StagedBinary: buildB,
	}
	require.NoError(t, writeTaskState(stateDir, state))

	unitName := fmt.Sprintf("signal-updater-integration-%d.service", time.Now().UnixNano())
	unitPath := filepath.Join("/run/systemd/system", unitName)
	unit := fmt.Sprintf("[Unit]\nDescription=Signal updater integration test\nStartLimitIntervalSec=300\nStartLimitBurst=2\n[Service]\nExecStart=/bin/sh %s\nRestart=always\nRestartSec=10ms\n", currentLink)
	require.NoError(t, os.WriteFile(unitPath, []byte(unit), 0600))
	t.Cleanup(func() {
		_ = exec.Command("systemctl", "stop", unitName).Run()
		_ = os.Remove(unitPath)
		_ = exec.Command("systemctl", "daemon-reload").Run()
	})
	require.NoError(t, exec.Command("systemctl", "daemon-reload").Run())
	require.NoError(t, exec.Command("systemctl", "start", unitName).Run())
	require.Eventually(t, func() bool {
		return exec.Command("systemctl", "is-failed", "--quiet", unitName).Run() == nil
	}, 5*time.Second, 50*time.Millisecond, "test unit did not reach start-limit-hit")

	stopHealth := make(chan struct{})
	defer close(stopHealth)
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopHealth:
				return
			case <-ticker.C:
				target, err := os.Readlink(currentLink)
				if err != nil || target != buildB {
					continue
				}
				_ = writeHealthFile(filepath.Join(stateDir, "health"), HealthFile{
					SchemaVersion: 2, TaskID: state.TaskID, Version: state.TargetVersion,
					CommitID: state.TargetCommitID, BinarySHA256: state.TargetSHA256,
					HeartbeatConfirmed: time.Now(),
				})
			}
		}
	}()

	require.NoError(t, Apply(context.Background(), ApplyConfig{
		TaskID: state.TaskID, StateDir: stateDir, BinaryPath: buildB,
		CurrentLink: currentLink, ServiceName: unitName,
		healthTimeout: 15 * time.Second, healthPoll: 100 * time.Millisecond,
	}))
	target, err := os.Readlink(currentLink)
	require.NoError(t, err)
	require.Equal(t, buildB, target)
	require.NoError(t, exec.Command("systemctl", "is-active", "--quiet", unitName).Run())
	persisted, err := readTaskState(taskStatePath(stateDir, state.TaskID))
	require.NoError(t, err)
	require.Equal(t, "succeeded", persisted.Phase)
}
