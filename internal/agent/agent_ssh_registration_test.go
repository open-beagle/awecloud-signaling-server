package agent

import (
	"testing"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
)

func TestShouldRegisterSSHDomain(t *testing.T) {
	tests := []struct {
		name         string
		isClientMode bool
		enableSSH    bool
		want         bool
	}{
		{name: "agent disabled", enableSSH: false, want: false},
		{name: "agent explicitly enabled", enableSSH: true, want: true},
		{name: "client disabled", isClientMode: true, enableSSH: false, want: false},
		{name: "client compatibility enabled", isClientMode: true, enableSSH: true, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := &Agent{
				config: &config.AgentConfig{
					Tunnel: config.TunnelSection{EnableSSH: tt.enableSSH},
				},
				isClientMode: tt.isClientMode,
			}

			if got := agent.shouldRegisterSSHDomain(); got != tt.want {
				t.Fatalf("shouldRegisterSSHDomain() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNodeSSHPortDefaultsAndUsesTunnelConfig(t *testing.T) {
	tests := []struct {
		name string
		port int
		want int32
	}{
		{name: "default", want: 22},
		{name: "configured", port: 2222, want: 2222},
		{name: "invalid falls back", port: -1, want: 22},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := &Agent{config: &config.AgentConfig{
				Tunnel: config.TunnelSection{SSHPort: tt.port},
			}}
			if got := agent.nodeSSHPort(); got != tt.want {
				t.Fatalf("nodeSSHPort() = %d, want %d", got, tt.want)
			}
		})
	}
}
