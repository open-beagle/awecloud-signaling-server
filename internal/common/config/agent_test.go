package config

import (
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
