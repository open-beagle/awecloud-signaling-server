package main

import (
	"os"
	"path/filepath"
	"strings"
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

func TestResolveAgentStartupConfigPreservesLocalConfigAfterDeployRegistration(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "agent.toml")
	require.NoError(t, os.WriteFile(configPath, []byte(`[agent]
token = "old-token"
server = "https://old.example"

[tunnel]
state_dir = "/home/code/.local/share/signal/tunnel"
state_sync_interval = 7
enable_ssh = true

[cloudide]
socks = true
socks_addr = "127.0.0.1:1080"
dial_socket = "/tmp/signaling.sock"

[log]
level = "debug"
file = "/home/code/.local/share/signal/logs/agent.log"
`), 0o600))
	registered := &config.AgentConfig{Agent: config.AgentSection{AgentToken: "runtime-secret", Server: "https://signal.example"}}
	wantResult := &config.RegisterResult{UserID: 42, UserRole: "client"}

	cfg, result, err := resolveAgentStartupConfig(configPath, "deploy-token", "https://signal.example", func(serverAddr, token string) (*config.AgentConfig, *config.RegisterResult, error) {
		require.Equal(t, "https://signal.example", serverAddr)
		require.Equal(t, "deploy-token", token)
		return registered, wantResult, nil
	})

	require.NoError(t, err)
	require.Same(t, wantResult, result)
	require.Equal(t, "runtime-secret", cfg.Agent.AgentToken)
	require.Equal(t, "https://signal.example", cfg.Agent.Server)
	require.Equal(t, "/home/code/.local/share/signal/tunnel", cfg.Tunnel.StateDir)
	require.Equal(t, 7, cfg.Tunnel.StateSyncInterval)
	require.True(t, cfg.Tunnel.EnableSSH)
	require.True(t, cfg.CloudIDE.Socks)
	require.Equal(t, "127.0.0.1:1080", cfg.CloudIDE.SocksAddr)
	require.Equal(t, "/tmp/signaling.sock", cfg.CloudIDE.DialSocket)
	require.Equal(t, "debug", cfg.Log.Level)
	require.Equal(t, "/home/code/.local/share/signal/logs/agent.log", cfg.Log.File)
}

func TestInstallSignalScriptUsesChinesePromptAndAvoidsDuplicateLogRedirect(t *testing.T) {
	script, err := os.ReadFile(filepath.Join("..", "..", "scripts", "install_signal.sh"))
	require.NoError(t, err)
	content := string(script)

	require.Contains(t, content, "Container Client / CloudIDE 安装入口")
	require.Contains(t, content, "此下载入口 install_signal.sh 面向 Container Client；")
	require.Contains(t, content, "Agent 请走 install_agent.sh")
	require.NotContains(t, content, "installer entry")
	require.NotContains(t, content, ">> \"$DATA_DIR/logs/agent.log\"")
	require.Contains(t, content, `nohup $SUDO_CMD "$BIN_DIR/signal_agent" -c "$CONFIG_FILE" -t "$SIGNAL_TOKEN" -s "$SIGNAL_SERVER" >/dev/null 2>&1 &`)

	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "echo ") {
			require.NotContains(t, line, "Agent 运行正常")
		}
	}
}

func TestInstallAgentScriptDeployRestartsServiceAndResetsTunnelState(t *testing.T) {
	scriptPaths := []string{filepath.Join("..", "..", "scripts", "install_agent.sh")}
	workspaceScript := filepath.Join("..", "..", "..", "scripts", "install_agent.sh")
	if _, err := os.Stat(workspaceScript); err == nil {
		scriptPaths = append(scriptPaths, workspaceScript)
	} else if !os.IsNotExist(err) {
		require.NoError(t, err)
	}

	for _, scriptPath := range scriptPaths {
		t.Run(scriptPath, func(t *testing.T) {
			script, err := os.ReadFile(scriptPath)
			require.NoError(t, err)
			content := string(script)

			require.Contains(t, content, "prepare_redeploy_agent()")
			require.Contains(t, content, `systemctl cat "$SERVICE_NAME" >/dev/null 2>&1`)
			require.Contains(t, content, `systemctl stop "$SERVICE_NAME" || error "停止旧服务失败"`)
			require.Contains(t, content, `mv "$TUNNEL_STATE_DIR" "$backup_dir" || error "备份旧隧道状态失败"`)
			require.Contains(t, content, "检测到旧隧道状态，已备份")
			require.Contains(t, content, "prepare_redeploy_agent")
			require.Contains(t, content, `systemctl restart "$SERVICE_NAME" || error "启动服务失败"`)

			mainScript := content[strings.LastIndex(content, "main() {"):]
			registerAt := strings.Index(mainScript, "deploy_with_token")
			downloadAt := strings.Index(mainScript, "download_agent")
			prepareAt := strings.Index(mainScript, "prepare_redeploy_agent")
			configAt := strings.Index(mainScript, "generate_config")
			require.NotEqual(t, -1, registerAt)
			require.NotEqual(t, -1, downloadAt)
			require.NotEqual(t, -1, prepareAt)
			require.NotEqual(t, -1, configAt)
			require.Less(t, registerAt, downloadAt, "注册应在下载前完成")
			require.Less(t, downloadAt, prepareAt, "下载失败时不应停止旧服务")
			require.Less(t, prepareAt, configAt, "旧状态应在写入新配置前备份")
		})
	}
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
