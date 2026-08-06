package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAgentContainerKubeconfigPreservesInClusterFallback(t *testing.T) {
	t.Setenv("SIGNAL_TOKEN", "fixture-token")
	t.Setenv("SIGNAL_SERVER", "https://signal.example.com")
	cfg, err := LoadAgentConfig(filepath.Join(t.TempDir(), "missing-agent.toml"))
	require.NoError(t, err)
	require.Empty(t, cfg.Container.Kubeconfig)
	require.Equal(t, "signal.beagle.io/container-ssh=true", cfg.Container.LabelSelector)
}

func TestAgentTunnelSSHPortFromConfigAndEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.toml")
	require.NoError(t, os.WriteFile(path, []byte(`
[agent]
token = "fixture-token"
server = "https://signal.example.com"

[tunnel]
enable_ssh = true
ssh_port = 2222
`), 0o600))

	cfg, err := LoadAgentConfig(path)
	require.NoError(t, err)
	require.Equal(t, 2222, cfg.Tunnel.SSHPort)

	t.Setenv("SIGNAL_SSH_PORT", "22022")
	cfg, err = LoadAgentConfig(path)
	require.NoError(t, err)
	require.Equal(t, 22022, cfg.Tunnel.SSHPort)
}
