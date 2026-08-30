//go:build linux

package main

import (
	"errors"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestSignalShellProcessByNameSignalsProcessGroup(t *testing.T) {
	cmd := exec.Command("sh", "-c", "exec sleep 30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_, _ = cmd.Process.Wait()
	})

	if err := signalShellProcessByName(cmd, "TERM"); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("wait error = %v, want signal exit", err)
		}
		status, ok := exitErr.Sys().(syscall.WaitStatus)
		if !ok || status.Signal() != syscall.SIGTERM {
			t.Fatalf("wait status = %v, want SIGTERM", status)
		}
		if code, signal := shellProcessResult(err); code != 143 || signal != "TERM" {
			t.Fatalf("shellProcessResult() = %d, %q, want 143, TERM", code, signal)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("process group did not receive SIGTERM")
	}
}

func TestSignalShellProcessByNameRejectsUnknownSignal(t *testing.T) {
	if err := signalShellProcessByName(&exec.Cmd{}, "KILL"); err == nil {
		t.Fatal("unsupported signal must be rejected")
	}
}
