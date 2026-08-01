//go:build linux

package main

import (
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
	return cmd.Process.Signal(syscall.SIGHUP)
}
