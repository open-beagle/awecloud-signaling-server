//go:build !linux

package main

import (
	"errors"
	"fmt"
	"os/exec"
	"os/user"
)

func configureShellProcess(*exec.Cmd, *user.User) error {
	return fmt.Errorf("endpoint shell is only supported on linux")
}

func signalShellProcess(cmd *exec.Cmd) error {
	return cmd.Process.Kill()
}

func killShellProcess(cmd *exec.Cmd) error {
	return cmd.Process.Kill()
}

func signalShellProcessByName(*exec.Cmd, string) error {
	return fmt.Errorf("endpoint shell signals are only supported on linux")
}

func shellProcessResult(err error) (int, string) {
	if err == nil {
		return 0, ""
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() >= 0 {
		return exitErr.ExitCode(), ""
	}
	return 1, ""
}
