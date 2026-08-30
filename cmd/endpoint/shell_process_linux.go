//go:build linux

package main

import (
	"errors"
	"fmt"
	"os/exec"
	"os/user"
	"strconv"
	"syscall"
)

func configureShellProcess(cmd *exec.Cmd, account *user.User) error {
	uid, err := strconv.ParseUint(account.Uid, 10, 32)
	if err != nil {
		return fmt.Errorf("parse uid %q: %w", account.Uid, err)
	}
	gid, err := strconv.ParseUint(account.Gid, 10, 32)
	if err != nil {
		return fmt.Errorf("parse gid %q: %w", account.Gid, err)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)},
		Setsid:     true,
	}
	return nil
}

func signalShellProcess(cmd *exec.Cmd) error {
	return signalShellProcessGroup(cmd, syscall.SIGHUP)
}

func signalShellProcessByName(cmd *exec.Cmd, name string) error {
	var signal syscall.Signal
	switch name {
	case "INT":
		signal = syscall.SIGINT
	case "TERM":
		signal = syscall.SIGTERM
	default:
		return fmt.Errorf("unsupported shell signal %q", name)
	}
	return signalShellProcessGroup(cmd, signal)
}

func signalShellProcessGroup(cmd *exec.Cmd, signal syscall.Signal) error {
	if cmd == nil || cmd.Process == nil {
		return fmt.Errorf("shell process is not running")
	}
	return syscall.Kill(-cmd.Process.Pid, signal)
}

func killShellProcess(cmd *exec.Cmd) error {
	return signalShellProcessGroup(cmd, syscall.SIGKILL)
}

func shellProcessResult(err error) (int, string) {
	if err == nil {
		return 0, ""
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return 1, ""
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() {
		code := exitErr.ExitCode()
		if code < 0 {
			return 1, ""
		}
		return code, ""
	}
	signal := status.Signal()
	name := ""
	switch signal {
	case syscall.SIGHUP:
		name = "HUP"
	case syscall.SIGINT:
		name = "INT"
	case syscall.SIGKILL:
		name = "KILL"
	case syscall.SIGTERM:
		name = "TERM"
	}
	return 128 + int(signal), name
}
