//go:build !linux

package main

import (
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
