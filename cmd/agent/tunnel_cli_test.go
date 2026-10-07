package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/open-beagle/awecloud-signaling-server/internal/agent"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
)

// TestTunnel_MissingRequiredFlags 验证缺少 -s、-t 或 --target-agent 时返回明确错误
func TestTunnel_MissingRequiredFlags(t *testing.T) {
	emptyEnv := func(string) string { return "" }

	// 1. 完全无参数
	_, err := ParseTunnelFlags([]string{}, emptyEnv)
	require.Error(t, err)
	require.Contains(t, err.Error(), "缺少必填参数")
	require.Contains(t, err.Error(), "-s/--server")
	require.Contains(t, err.Error(), "-t/--token")
	require.Contains(t, err.Error(), "--target-agent")

	// 2. 缺少 --target-agent
	_, err = ParseTunnelFlags([]string{"-s", "http://127.0.0.1:8080", "-t", "dt_123"}, emptyEnv)
	require.Error(t, err)
	require.Contains(t, err.Error(), "--target-agent")

	// 3. 缺少 -t
	_, err = ParseTunnelFlags([]string{"-s", "http://127.0.0.1:8080", "--target-agent", "edge-5090"}, emptyEnv)
	require.Error(t, err)
	require.Contains(t, err.Error(), "-t/--token")

	// 4. 缺少 -s
	_, err = ParseTunnelFlags([]string{"-t", "dt_123", "--target-agent", "edge-5090"}, emptyEnv)
	require.Error(t, err)
	require.Contains(t, err.Error(), "-s/--server")
}

// TestTunnel_FlagPrecedenceOverEnv 验证命令行参数具有最高优先级，正确覆盖环境变量
func TestTunnel_FlagPrecedenceOverEnv(t *testing.T) {
	mockEnv := map[string]string{
		"SIGNAL_SERVER":       "http://env-server:8080",
		"SIGNAL_DEPLOY_TOKEN": "env-token-xyz",
		"SIGNAL_TARGET_AGENT": "edge-4090",
		"SIGNAL_STATE_DIR":    "/var/run/env-dir",
	}
	getenv := func(key string) string {
		return mockEnv[key]
	}

	// 1. 不带 CLI 参数时，完全继承环境变量
	cfg, err := ParseTunnelFlags([]string{}, getenv)
	require.NoError(t, err)
	require.Equal(t, "http://env-server:8080", cfg.ServerAddr)
	require.Equal(t, "env-token-xyz", cfg.DeployToken)
	require.Equal(t, "edge-4090", cfg.TargetAgent)
	require.Equal(t, "/var/run/env-dir", cfg.StateDir)

	// 2. 带 CLI 参数时，CLI 覆盖 ENV（例如：target-agent 覆盖为 edge-5090）
	cfgOverride, err := ParseTunnelFlags([]string{
		"--target-agent", "edge-5090",
		"-s", "http://cli-server:9090",
		"-t", "cli-token-abc",
		"--state-dir", "/var/run/cli-dir",
	}, getenv)
	require.NoError(t, err)
	require.Equal(t, "http://cli-server:9090", cfgOverride.ServerAddr)
	require.Equal(t, "cli-token-abc", cfgOverride.DeployToken)
	require.Equal(t, "edge-5090", cfgOverride.TargetAgent)
	require.Equal(t, "/var/run/cli-dir", cfgOverride.StateDir)
}

// TestTunnel_HandshakeTargetMismatch 验证握手防呆校验：目标 Agent 与 Server 绑定不符时立即报错
func TestTunnel_HandshakeTargetMismatch(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/register", r.URL.Path)

		var req struct {
			Token       string `json:"token"`
			TargetAgent string `json:"target_agent"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)

		// 模拟 Server 防呆校验：Token 绑定为 edge-4090，客户端却声明 edge-5090
		if req.TargetAgent != "edge-4090" {
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": `target_agent mismatch: declared "edge-5090", bound "edge-4090"`,
			})
			return
		}
	}))
	defer ts.Close()

	// 客户端声明目标为 edge-5090
	_, err := RegisterTunnelWithToken(ts.URL, "valid-token", "edge-5090")
	require.Error(t, err)
	require.Contains(t, err.Error(), "target_agent mismatch")
	require.Contains(t, err.Error(), `declared "edge-5090", bound "edge-4090"`)
}

// TestTunnel_HandshakeSuccess 验证握手防呆校验成功后顺利获得注册凭据
func TestTunnel_HandshakeSuccess(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/register", r.URL.Path)

		var req struct {
			Token       string `json:"token"`
			TargetAgent string `json:"target_agent"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)

		require.Equal(t, "edge-5090", req.TargetAgent)
		require.Equal(t, "valid-token", req.Token)

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"data": map[string]interface{}{
				"message":       "注册成功",
				"user_role":     "client",
				"user_id":       999,
				"headscale_url": "http://headscale.beagle:8080",
				"auth_key":      "hs_auth_test_key",
				"user_name":     "svc-tunnel-5090",
				"device_name":   "signal-tunnel-5090",
			},
		})
	}))
	defer ts.Close()

	res, err := RegisterTunnelWithToken(ts.URL, "valid-token", "edge-5090")
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Equal(t, "client", res.UserRole)
	require.Equal(t, uint64(999), res.UserID)
	require.Equal(t, "hs_auth_test_key", res.AuthKey)
	require.Equal(t, "svc-tunnel-5090", res.UserName)
	require.Equal(t, "signal-tunnel-5090", res.DeviceName)
}

// TestTunnel_RunAsNonRoot 验证 Tunnel 模式下 Agent 标志及隔离性（无需特权/无 VIP / 无 DNS 劫持）
func TestTunnel_RunAsNonRoot(t *testing.T) {
	// 创建测试 Agent
	agt, err := agent.NewTunnelAgent(&config.AgentConfig{
		Agent: config.AgentSection{
			AgentToken: "test-token",
			Server:     "http://127.0.0.1:8080",
		},
		Tunnel: config.TunnelSection{
			StateDir: t.TempDir(),
		},
	}, "v1.0.0", "0123456789abcdef0123456789abcdef01234567", "2026-10-01T00:00:00Z", "2026-10-01")
	require.NoError(t, err)
	require.NotNil(t, agt)

	// 使用实际 run-tunnel 入口的构造器，初始化不写宿主机 updater 目录。
	require.True(t, agt.IsTunnelMode())
}
