package agent

import (
	"encoding/binary"
	"testing"
)

func TestParseSSHString(t *testing.T) {
	payload := make([]byte, 4+len("sftp"))
	binary.BigEndian.PutUint32(payload, uint32(len("sftp")))
	copy(payload[4:], "sftp")

	if got := parseSSHString(payload); got != "sftp" {
		t.Fatalf("parseSSHString() = %q, want sftp", got)
	}
	for _, malformed := range [][]byte{
		nil,
		{0, 0, 0},
		{0, 0, 0, 5, 's', 'f', 't', 'p'},
	} {
		if got := parseSSHString(malformed); got != "" {
			t.Fatalf("malformed payload parsed as %q", got)
		}
	}
}

func TestSSHBannerOnlyForInteractiveShell(t *testing.T) {
	if !shouldShowSSHBanner(true, false) {
		t.Fatal("interactive PTY shell must retain the Beagle user information banner")
	}
	for _, session := range []struct {
		pty  bool
		sftp bool
	}{{false, false}, {false, true}, {true, true}} {
		if shouldShowSSHBanner(session.pty, session.sftp) {
			t.Fatalf("banner would contaminate non-interactive protocol: %#v", session)
		}
	}
}
