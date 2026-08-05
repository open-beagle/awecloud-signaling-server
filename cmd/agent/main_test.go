package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/stretchr/testify/require"
)

func TestResolveAgentStartupConfigUsesStoredCredentialWithoutRegistering(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "agent.toml")
	require.NoError(t, os.WriteFile(configPath, []byte(`[agent]
token = "runtime-secret"
server = "https://signal.example"
`), 0o600))
	registerCalled := false
	cfg, result, err := resolveAgentStartupConfig(configPath, "", "", func(string, string) (*config.AgentConfig, *config.RegisterResult, error) {
		registerCalled = true
		return nil, nil, nil
	})

	require.NoError(t, err)
	require.False(t, registerCalled)
	require.Nil(t, result)
	require.Equal(t, "runtime-secret", cfg.Agent.AgentToken)
	require.Equal(t, "https://signal.example", cfg.Agent.Server)
}

func TestResolveAgentStartupConfigRegistersOnlyExplicitDeployToken(t *testing.T) {
	registered := &config.AgentConfig{Agent: config.AgentSection{AgentToken: "runtime-secret", Server: "https://signal.example"}}
	wantResult := &config.RegisterResult{UserID: 42, UserRole: "agent"}
	cfg, result, err := resolveAgentStartupConfig("unused", "deploy-token", "https://signal.example", func(serverAddr, token string) (*config.AgentConfig, *config.RegisterResult, error) {
		require.Equal(t, "https://signal.example", serverAddr)
		require.Equal(t, "deploy-token", token)
		return registered, wantResult, nil
	})

	require.NoError(t, err)
	require.Same(t, registered, cfg)
	require.Same(t, wantResult, result)
}

func TestMergeLocalAgentConfigPreservesContainerSSHConfig(t *testing.T) {
	registered := &config.AgentConfig{
		Agent: config.AgentSection{AgentToken: "token", Server: "https://signal.example"},
	}
	local := &config.AgentConfig{
		Container: config.ContainerSection{
			Enabled:            true,
			Kubeconfig:         "/root/.kube/config",
			LabelSelector:      "signal.beagle.io/container-ssh=true",
			Namespaces:         []string{"acceptance"},
			ProviderLabel:      "beagle.io/provider",
			WorkspaceLabel:     "beagle.io/workspace",
			GenerationLabel:    "beagle.io/workspace-generation",
			ContainerNameLabel: "beagle.io/container",
			LeaseSeconds:       120,
		},
	}

	mergeLocalAgentConfig(registered, local)

	require.Equal(t, local.Container, registered.Container)
	require.Equal(t, "token", registered.Agent.AgentToken)
	require.Equal(t, "https://signal.example", registered.Agent.Server)
}
