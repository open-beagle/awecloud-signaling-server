package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/stretchr/testify/require"
	"tailscale.com/cmd/tailscaled/childproc"
)

func TestAgentSFTPChildProtocol(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Tailscale ssh/sftp childproc 仅在 Unix 构建中注册")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestAgentSFTPChildHelper$")
	cmd.Env = append(os.Environ(), "BEAGLE_TEST_SFTP_CHILD=1")
	stdin, err := cmd.StdinPipe()
	require.NoError(t, err)
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	cmd.Stderr = os.Stderr
	require.NoError(t, cmd.Start())

	initPacket := make([]byte, 9)
	binary.BigEndian.PutUint32(initPacket[0:4], 5)
	initPacket[4] = 1 // SSH_FXP_INIT
	binary.BigEndian.PutUint32(initPacket[5:9], 3)
	_, err = stdin.Write(initPacket)
	require.NoError(t, err)

	result := make(chan error, 1)
	go func() {
		header := make([]byte, 4)
		if _, err := io.ReadFull(stdout, header); err != nil {
			result <- err
			return
		}
		length := binary.BigEndian.Uint32(header)
		if length < 5 || length > 1024*1024 {
			result <- fmt.Errorf("invalid SFTP response length %d (header=%q)", length, header)
			return
		}
		payload := make([]byte, length)
		if _, err := io.ReadFull(stdout, payload); err != nil {
			result <- err
			return
		}
		if payload[0] != 2 || binary.BigEndian.Uint32(payload[1:5]) != 3 {
			result <- fmt.Errorf("unexpected SFTP version response: %x", payload[:5])
			return
		}
		result <- nil
	}()

	select {
	case err := <-result:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("timed out waiting for SFTP version response")
	}
	_ = stdin.Close()
	require.NoError(t, cmd.Wait())
}

func TestAgentSFTPChildHelper(t *testing.T) {
	if os.Getenv("BEAGLE_TEST_SFTP_CHILD") != "1" {
		return
	}
	if err := runTailscaleChild([]string{"sftp"}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	os.Exit(0)
}

func TestResolveAgentStartupConfigUsesStoredCredentialWithoutRegistering(t *testing.T) {
	t.Setenv("SIGNAL_TOKEN", "")
	t.Setenv("AGENT_TOKEN", "")
	t.Setenv("SIGNAL_SERVER", "")
	t.Setenv("AGENT_ADDRESS", "")
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

func TestAgentMainHandlesTailscaleChildrenBeforeLoadingConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Tailscale ssh/sftp childproc 仅在 Unix 构建中注册")
	}
	source, err := os.ReadFile("main.go")
	require.NoError(t, err)
	content := string(source)

	childRunAt := strings.Index(content, `runTailscaleChildIfRequested()`)
	configFlagAt := strings.Index(content, `configPath := flag.String`)
	require.NotEqual(t, -1, childRunAt)
	require.NotEqual(t, -1, configFlagAt)
	require.Less(t, childRunAt, configFlagAt)
	require.Contains(t, content, `args[0] == "banner"`)
	require.Contains(t, content, `agent.RequestSSHBanner(agent.SSHBannerSocketPath`)
	require.Contains(t, content, `childproc.Code[args[0]]`)
	require.NotNil(t, childproc.Code["ssh"])
	require.NotNil(t, childproc.Code["sftp"])
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

func TestInstallSignalUpgradeDownloadFailureDoesNotStopClient(t *testing.T) {
	bashPath, err := exec.LookPath("bash")
	require.NoError(t, err)

	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	dataDir := filepath.Join(homeDir, ".local", "share", "signal")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "agent.toml"), []byte(`[agent]
token = "runtime-token"
server = "https://signal.example"
`), 0o600))

	eventPath := filepath.Join(tempDir, "events.log")
	fakeBin := filepath.Join(tempDir, "bin")
	require.NoError(t, os.MkdirAll(fakeBin, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(fakeBin, "curl"), []byte(`#!/bin/bash
echo "curl:$*" >> "$TEST_EVENT_FILE"
if [[ "$*" == *"signal_agent-version.json"* ]]; then
  cat <<'JSON'
{"version":"v1.0.2","files":{"linux-amd64":"https://artifacts.example/signal_agent"},"sha256":{"linux-amd64":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}
JSON
  exit 0
fi
exit 22
`), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(fakeBin, "pgrep"), []byte(`#!/bin/bash
echo "pgrep:$*" >> "$TEST_EVENT_FILE"
echo 4242
`), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(fakeBin, "sudo"), []byte(`#!/bin/bash
echo "sudo:$*" >> "$TEST_EVENT_FILE"
exit 0
`), 0o700))

	command := exec.Command(bashPath, filepath.ToSlash(filepath.Join("..", "..", "scripts", "install_signal.sh")), "--upgrade")
	command.Env = append(os.Environ(),
		"HOME="+filepath.ToSlash(homeDir),
		"PATH="+filepath.ToSlash(fakeBin)+string(os.PathListSeparator)+os.Getenv("PATH"),
		"TEST_EVENT_FILE="+filepath.ToSlash(eventPath),
	)
	output, runErr := command.CombinedOutput()
	require.Error(t, runErr, string(output))
	events, err := os.ReadFile(eventPath)
	require.NoError(t, err)
	require.Contains(t, string(events), "https://artifacts.example/signal_agent", string(output))
	require.NotContains(t, string(events), "https://cache.ali.wodcloud.com/vscode/awecloud-signaling/signal_agent-v1.0.2-linux-amd64", string(output))
	require.NotContains(t, string(events), "sudo:kill", string(output))
}

func TestInstallSignalUpgradeRollsBackFailedStart(t *testing.T) {
	bashPath, err := exec.LookPath("bash")
	require.NoError(t, err)
	scriptPath := filepath.Join("..", "..", "scripts", "install_signal.sh")
	content, err := os.ReadFile(scriptPath)
	require.NoError(t, err)

	tempDir := t.TempDir()
	eventPath := filepath.Join(tempDir, "events.log")
	harness := `
EVENT_FILE="$TEST_EVENT_FILE"
BIN_DIR="/opt/bin"
STAGED_BINARY_PATH=""
CURRENT_BINARY="/downloads/signal_agent-old"

stage_client() { STAGED_BINARY_PATH="/downloads/signal_agent-new"; echo "stage" >> "$EVENT_FILE"; }
readlink() { echo "/downloads/signal_agent-old"; }
stop_client() { echo "stop" >> "$EVENT_FILE"; }
switch_client_link() { CURRENT_BINARY="$1"; echo "link:$1" >> "$EVENT_FILE"; }
activate_staged_client() { switch_client_link "$STAGED_BINARY_PATH"; }
start_client() {
    echo "start:$CURRENT_BINARY" >> "$EVENT_FILE"
    [[ "$CURRENT_BINARY" == "/downloads/signal_agent-old" ]]
}

upgrade_client
`
	normalized := strings.ReplaceAll(string(content), "\r\n", "\n")
	source := strings.Replace(normalized, "\nmain \"$@\"", harness, 1)
	require.NotEqual(t, normalized, source)
	harnessPath := filepath.Join(tempDir, "upgrade-harness.sh")
	require.NoError(t, os.WriteFile(harnessPath, []byte(source), 0o700))

	command := exec.Command(bashPath, filepath.ToSlash(harnessPath))
	command.Env = append(os.Environ(), "TEST_EVENT_FILE="+filepath.ToSlash(eventPath))
	output, runErr := command.CombinedOutput()
	require.Error(t, runErr, string(output))
	events, err := os.ReadFile(eventPath)
	require.NoError(t, err)
	require.Equal(t, []string{
		"stage",
		"stop",
		"link:/downloads/signal_agent-new",
		"start:/downloads/signal_agent-new",
		"stop",
		"link:/downloads/signal_agent-old",
		"start:/downloads/signal_agent-old",
	}, strings.Split(strings.TrimSpace(string(events)), "\n"))
}

func TestInstallSignalUpgradeAcceptsRegistrationConfigRewrite(t *testing.T) {
	bashPath, err := exec.LookPath("bash")
	require.NoError(t, err)
	scriptPath := filepath.Join("..", "..", "scripts", "install_signal.sh")
	content, err := os.ReadFile(scriptPath)
	require.NoError(t, err)

	tempDir := t.TempDir()
	eventPath := filepath.Join(tempDir, "events.log")
	configPath := filepath.Join(tempDir, "agent.toml")
	require.NoError(t, os.WriteFile(configPath, []byte("original\n"), 0o600))
	harness := `
EVENT_FILE="$TEST_EVENT_FILE"
CONFIG_FILE="$TEST_CONFIG_FILE"
BIN_DIR="/opt/bin"
STAGED_BINARY_PATH=""

stage_client() { STAGED_BINARY_PATH="/downloads/signal_agent-new"; echo "stage" >> "$EVENT_FILE"; }
readlink() { echo "/downloads/signal_agent-old"; }
stop_client() { echo "stop" >> "$EVENT_FILE"; }
switch_client_link() { echo "link:$1" >> "$EVENT_FILE"; }
activate_staged_client() { switch_client_link "$STAGED_BINARY_PATH"; }
start_client() {
    echo "start" >> "$EVENT_FILE"
    echo "refreshed-registration" >> "$CONFIG_FILE"
}

upgrade_client
`
	normalized := strings.ReplaceAll(string(content), "\r\n", "\n")
	source := strings.Replace(normalized, "\nmain \"$@\"", harness, 1)
	require.NotEqual(t, normalized, source)
	harnessPath := filepath.Join(tempDir, "config-rewrite-harness.sh")
	require.NoError(t, os.WriteFile(harnessPath, []byte(source), 0o700))

	command := exec.Command(bashPath, filepath.ToSlash(harnessPath))
	command.Env = append(os.Environ(),
		"TEST_EVENT_FILE="+filepath.ToSlash(eventPath),
		"TEST_CONFIG_FILE="+filepath.ToSlash(configPath),
	)
	output, runErr := command.CombinedOutput()
	require.NoError(t, runErr, string(output))
	events, err := os.ReadFile(eventPath)
	require.NoError(t, err)
	require.Equal(t, []string{
		"stage",
		"stop",
		"link:/downloads/signal_agent-new",
		"start",
	}, strings.Split(strings.TrimSpace(string(events)), "\n"))
}

func TestInstallSignalStartRequiresClientReadiness(t *testing.T) {
	bashPath, err := exec.LookPath("bash")
	require.NoError(t, err)
	scriptPath := filepath.Join("..", "..", "scripts", "install_signal.sh")
	content, err := os.ReadFile(scriptPath)
	require.NoError(t, err)

	tempDir := t.TempDir()
	eventPath := filepath.Join(tempDir, "events.log")
	dataDir := filepath.Join(tempDir, "data")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))
	harness := `
EVENT_FILE="$TEST_EVENT_FILE"
BIN_DIR="/opt/bin"
CONFIG_FILE="/config/agent.toml"
DATA_DIR="$TEST_DATA_DIR"
SIGNAL_TOKEN="runtime-token"
SIGNAL_SERVER="https://signal.example"
SUDO_CMD=""
STAGED_BINARY_PATH="/downloads/signal_agent-new"

nohup() { return 0; }
sleep() { return 0; }
ps() { echo "wrapper-ps" >> "$EVENT_FILE"; return 0; }
pgrep() { echo 4321; }
readlink() { echo "/downloads/signal_agent-new"; }
find_client_pid() { echo 4321; }
wait_client_ready() { echo "readiness" >> "$EVENT_FILE"; return 1; }

start_client
`
	normalized := strings.ReplaceAll(string(content), "\r\n", "\n")
	source := strings.Replace(normalized, "\nmain \"$@\"", harness, 1)
	require.NotEqual(t, normalized, source)
	harnessPath := filepath.Join(tempDir, "start-harness.sh")
	require.NoError(t, os.WriteFile(harnessPath, []byte(source), 0o700))

	command := exec.Command(bashPath, filepath.ToSlash(harnessPath))
	command.Env = append(os.Environ(),
		"TEST_EVENT_FILE="+filepath.ToSlash(eventPath),
		"TEST_DATA_DIR="+filepath.ToSlash(dataDir),
	)
	output, runErr := command.CombinedOutput()
	require.Error(t, runErr, string(output))
	events, err := os.ReadFile(eventPath)
	require.NoError(t, err)
	require.Contains(t, string(events), "readiness")
	require.NotContains(t, string(events), "wrapper-ps")
}

func TestInstallSignalFindClientPIDUsesReadableCommandLine(t *testing.T) {
	bashPath, err := exec.LookPath("bash")
	require.NoError(t, err)
	scriptPath := filepath.Join("..", "..", "scripts", "install_signal.sh")
	content, err := os.ReadFile(scriptPath)
	require.NoError(t, err)

	tempDir := t.TempDir()
	harness := `
BIN_DIR="/opt/bin"
CONFIG_FILE="/config/agent.toml"

pgrep() { echo 4321; }
read_process_args() {
    printf '%s\n' "/opt/bin/signal_agent" "-c" "/config/agent.toml"
}
readlink() {
    if [[ "$*" == "-f /opt/bin/signal_agent" ]]; then
        echo "/downloads/signal_agent-new"
        return 0
    fi
    return 1
}

[[ "$(find_client_pid)" == "4321" ]]
`
	normalized := strings.ReplaceAll(string(content), "\r\n", "\n")
	source := strings.Replace(normalized, "\nmain \"$@\"", harness, 1)
	require.NotEqual(t, normalized, source)
	harnessPath := filepath.Join(tempDir, "pid-harness.sh")
	require.NoError(t, os.WriteFile(harnessPath, []byte(source), 0o700))

	command := exec.Command(bashPath, filepath.ToSlash(harnessPath))
	output, runErr := command.CombinedOutput()
	require.NoError(t, runErr, string(output))
}

func TestInstallSignalStartUsesDataDirForRelativeTunnelState(t *testing.T) {
	bashPath, err := exec.LookPath("bash")
	require.NoError(t, err)
	scriptPath := filepath.Join("..", "..", "scripts", "install_signal.sh")
	content, err := os.ReadFile(scriptPath)
	require.NoError(t, err)

	tempDir := t.TempDir()
	dataDir := filepath.Join(tempDir, "data")
	binDir := filepath.Join(tempDir, "bin")
	configPath := filepath.Join(dataDir, "agent.toml")
	eventPath := filepath.Join(tempDir, "events.log")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	require.NoError(t, os.WriteFile(configPath, nil, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "state-marker"), nil, 0o600))

	harness := fmt.Sprintf(`
EVENT_FILE=%q
BIN_DIR=%q
CONFIG_FILE=%q
DATA_DIR=%q
SIGNAL_TOKEN="runtime-token"
SIGNAL_SERVER="https://signal.example"
SUDO_CMD=""

nohup() {
    if [[ -f "$PWD/state-marker" ]]; then
        echo "data-dir" >> "$EVENT_FILE"
    else
        printf 'pwd:%%s\n' "$PWD" >> "$EVENT_FILE"
    fi
}
sleep() { return 0; }
find_client_pid() { echo 4321; }
wait_client_ready() { return 0; }

start_client
`, filepath.ToSlash(eventPath), filepath.ToSlash(binDir), filepath.ToSlash(configPath), filepath.ToSlash(dataDir))
	normalized := strings.ReplaceAll(string(content), "\r\n", "\n")
	source := strings.Replace(normalized, "\nmain \"$@\"", harness, 1)
	require.NotEqual(t, normalized, source)
	harnessPath := filepath.Join(tempDir, "working-dir-harness.sh")
	require.NoError(t, os.WriteFile(harnessPath, []byte(source), 0o700))

	command := exec.Command(bashPath, filepath.ToSlash(harnessPath))
	output, runErr := command.CombinedOutput()
	require.NoError(t, runErr, string(output))
	events, err := os.ReadFile(eventPath)
	require.NoError(t, err)
	require.Equal(t, "data-dir", strings.TrimSpace(string(events)))
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

func TestInstallAgentScriptInstallsPTYOnlyDetailedSSHBanner(t *testing.T) {
	script, err := os.ReadFile(filepath.Join("..", "..", "scripts", "install_agent.sh"))
	require.NoError(t, err)
	content := string(script)

	require.Contains(t, content, `SSH_BANNER_PROFILE="/etc/profile.d/awecloud-signaling-ssh-banner.sh"`)
	require.Contains(t, content, `if [ -n "\${SSH_TTY:-}" ] && [ -n "\${SSH_CONNECTION:-}" ]`)
	require.Contains(t, content, `be-child banner "\${AWE_SSH_REMOTE_IP}" "\${AWE_SSH_REMOTE_PORT}"`)
	require.Contains(t, content, `chmod 0644 "$SSH_BANNER_PROFILE"`)
	require.Contains(t, content, `rm -f "$SSH_BANNER_PROFILE"`)
}

func TestInstallAgentUpgradePreservesExistingCredentialConfig(t *testing.T) {
	script, err := os.ReadFile(filepath.Join("..", "..", "scripts", "install_agent.sh"))
	require.NoError(t, err)
	content := string(script)

	require.Contains(t, content, `CONFIG_FILE="${CONFIG_DIR}/k8s-signaling.toml"`)
	require.Contains(t, content, `stat -c %a "$CONFIG_FILE"`)
	require.Contains(t, content, `config_sha_before=$(sha256sum "$CONFIG_FILE"`)
	require.Contains(t, content, `config_sha_after=$(sha256sum "$CONFIG_FILE"`)
	require.Contains(t, content, `binary_filename="${BINARY_NAME}-${artifact_sha}"`)
	require.Contains(t, content, `if [[ "$downloaded_sha" != "$artifact_sha" ]]`)

	upgrade := content[strings.Index(content, "upgrade_agent() {"):strings.Index(content, "# 卸载 Agent")]
	require.NotContains(t, upgrade, "deploy_with_token")
	require.NotContains(t, upgrade, "generate_config")
	require.NotContains(t, upgrade, "AGENT_TOKEN")
}

func TestInstallAgentUpgradeStagesBeforeStopAndRollsBackFailedRestart(t *testing.T) {
	bashPath, err := exec.LookPath("bash")
	require.NoError(t, err)
	scriptPath := filepath.Join("..", "..", "scripts", "install_agent.sh")
	content, err := os.ReadFile(scriptPath)
	require.NoError(t, err)

	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "agent.toml")
	require.NoError(t, os.WriteFile(configPath, []byte("credential = true\n"), 0600))
	require.NoError(t, os.Chmod(configPath, 0600))
	eventPath := filepath.Join(tempDir, "events.log")
	restartCountPath := filepath.Join(tempDir, "restart-count")
	harness := `
CONFIG_FILE="$TEST_CONFIG_FILE"
EVENT_FILE="$TEST_EVENT_FILE"
RESTART_COUNT_FILE="$TEST_RESTART_COUNT_FILE"
SERVICE_NAME="k8s-signaling"
INSTALL_DIR="/opt/bin"
BINARY_NAME="signal_agent"

download_agent() { echo "download" >> "$EVENT_FILE"; }
stage_agent() { STAGED_BINARY_PATH="/downloads/signal_agent-new"; echo "stage" >> "$EVENT_FILE"; }
activate_staged_agent() { echo "activate:$STAGED_BINARY_PATH" >> "$EVENT_FILE"; switch_agent_link "$STAGED_BINARY_PATH"; }
switch_agent_link() { echo "link:$1" >> "$EVENT_FILE"; }
readlink() { echo "/downloads/signal_agent-old"; }
stat() { echo "600"; }
install_service() {
    echo "install" >> "$EVENT_FILE"
    systemctl restart "$SERVICE_NAME" || error "启动服务失败"
}
systemctl() {
    echo "systemctl:$*" >> "$EVENT_FILE"
    if [[ "$1" == "restart" ]]; then
        local count=0
        [[ -f "$RESTART_COUNT_FILE" ]] && count=$(cat "$RESTART_COUNT_FILE")
        count=$((count + 1))
        echo "$count" > "$RESTART_COUNT_FILE"
        [[ "$count" -eq 1 ]] && return 1
    fi
    return 0
}

upgrade_agent
`
	normalized := strings.ReplaceAll(string(content), "\r\n", "\n")
	source := strings.Replace(normalized, "\nmain \"$@\"", harness, 1)
	require.NotEqual(t, normalized, source)
	harnessPath := filepath.Join(tempDir, "upgrade-harness.sh")
	require.NoError(t, os.WriteFile(harnessPath, []byte(source), 0700))

	command := exec.Command(bashPath, filepath.ToSlash(harnessPath))
	command.Env = append(os.Environ(),
		"TEST_CONFIG_FILE="+filepath.ToSlash(configPath),
		"TEST_EVENT_FILE="+filepath.ToSlash(eventPath),
		"TEST_RESTART_COUNT_FILE="+filepath.ToSlash(restartCountPath),
	)
	output, runErr := command.CombinedOutput()
	require.Error(t, runErr, string(output))
	require.FileExists(t, eventPath, string(output))
	events, err := os.ReadFile(eventPath)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(events)), "\n")

	require.Equal(t, "stage", lines[0], lines)
	require.Contains(t, lines, "systemctl:stop k8s-signaling")
	require.Contains(t, lines, "activate:/downloads/signal_agent-new")
	require.Contains(t, lines, "link:/downloads/signal_agent-old")
	require.Equal(t, "systemctl:restart k8s-signaling", lines[len(lines)-1], lines)
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
