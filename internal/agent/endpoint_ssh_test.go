package agent

import (
	"encoding/binary"
	"testing"

	"golang.org/x/crypto/ssh"
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

func TestParseSSHSignal(t *testing.T) {
	for _, signal := range []string{"INT", "TERM"} {
		payload := ssh.Marshal(struct{ Signal string }{Signal: signal})
		if got, ok := parseSSHSignal(payload); !ok || got != signal {
			t.Fatalf("parseSSHSignal(%q) = %q, %t", signal, got, ok)
		}
	}
	for _, signal := range []string{"", "KILL", "USR1"} {
		payload := ssh.Marshal(struct{ Signal string }{Signal: signal})
		if got, ok := parseSSHSignal(payload); ok || got != "" {
			t.Fatalf("unsupported signal %q parsed as %q, %t", signal, got, ok)
		}
	}
	if got, ok := parseSSHSignal([]byte{0, 0, 0, 4, 'I'}); ok || got != "" {
		t.Fatalf("malformed signal parsed as %q, %t", got, ok)
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

func TestDimensionsForShellRequest(t *testing.T) {
	rows, cols := dimensionsForShellRequest(false, 24, 80)
	if rows != 0 || cols != 0 {
		t.Fatalf("non-PTY exec dimensions = %d,%d, want 0,0", rows, cols)
	}

	rows, cols = dimensionsForShellRequest(true, 0, 0)
	if rows != 1 || cols != 1 {
		t.Fatalf("PTY dimensions = %d,%d, want 1,1", rows, cols)
	}

	rows, cols = dimensionsForShellRequest(true, 32, 120)
	if rows != 32 || cols != 120 {
		t.Fatalf("PTY dimensions = %d,%d, want 32,120", rows, cols)
	}
}

func TestSupportsSSHGlobalRequest(t *testing.T) {
	if !supportsSSHGlobalRequest("keepalive@openssh.com") {
		t.Fatal("OpenSSH keepalive request must be accepted")
	}
	for _, requestType := range []string{"", "tcpip-forward", "cancel-tcpip-forward", "unknown@example.com"} {
		if supportsSSHGlobalRequest(requestType) {
			t.Fatalf("unsupported global request %q would be accepted", requestType)
		}
	}
}

func TestEndpointSSHRequestStateRejectsDuplicateStartsAndLatePTY(t *testing.T) {
	state := &endpointSSHRequestState{}
	if !state.allocatePTY() {
		t.Fatal("first PTY request must be accepted")
	}
	if state.allocatePTY() {
		t.Fatal("duplicate PTY request must be rejected")
	}
	if !state.start() {
		t.Fatal("first session start must be accepted")
	}
	if state.start() {
		t.Fatal("duplicate session start must be rejected")
	}
	if state.allocatePTY() {
		t.Fatal("PTY request after session start must be rejected")
	}
}

func TestEndpointShellStreamHandleCloseIsIdempotent(t *testing.T) {
	done := make(chan struct{})
	handle := &endpointShellStreamHandle{done: done}
	handle.Close()
	handle.Close()
	select {
	case <-done:
	default:
		t.Fatal("Close did not release the OpenShell handler")
	}
}
