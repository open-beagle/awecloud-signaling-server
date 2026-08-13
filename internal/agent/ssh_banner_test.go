package agent

import (
	"net"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildSSHBannerUsesDetailedSharedFormat(t *testing.T) {
	oldVersion, oldBuildDate := version, buildDate
	t.Cleanup(func() { SetVersionInfo(oldVersion, oldBuildDate) })
	SetVersionInfo("v1.2.3", "2026-08-14")
	banner := BuildSSHBanner(SSHBannerInfo{
		RemoteUser: "zhangsan", DisplayName: "Zhang San", RemoteIP: "100.64.0.112",
		RemoteDeviceName: "WIND-2026", RemoteDeviceOS: "windows",
	})
	for _, expected := range []string{
		"AWECloud Signaling - SSH Access",
		"Version:      v1.2.3",
		"Build Date:   2026-08-14",
		"Remote User:   zhangsan , Zhang San",
		"Remote Device: 100.64.0.112 , WIND-2026 , windows",
	} {
		if !strings.Contains(banner, expected) {
			t.Fatalf("detailed banner is missing %q:\n%s", expected, banner)
		}
	}
}

func TestRequestSSHBanner(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "banner.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	request := make(chan string, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		buffer := make([]byte, 128)
		n, _ := conn.Read(buffer)
		request <- string(buffer[:n])
		_, _ = conn.Write([]byte("detailed-banner"))
	}()

	banner, err := RequestSSHBanner(socketPath, "100.64.0.112", "54321")
	if err != nil {
		t.Fatal(err)
	}
	if banner != "detailed-banner" {
		t.Fatalf("banner = %q", banner)
	}
	if got := <-request; got != "100.64.0.112:54321\n" {
		t.Fatalf("request = %q", got)
	}
}
