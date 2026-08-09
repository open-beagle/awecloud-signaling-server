package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetAgentVersionInfoUsesContentAddressedManifest(t *testing.T) {
	agentVersionCacheMutex.Lock()
	cachedAgentVersionInfo = nil
	agentVersionCacheTime = agentVersionCacheTime.Add(-versionCacheTTL)
	agentVersionCacheMutex.Unlock()

	manifest := `{"version":"v1.0.0","commit_id":"0123456789abcdef0123456789abcdef01234567","build_date":"2026-08-09T07:00:00Z","files":{"linux-amd64":"https://artifacts.example/agent/abc/signal_agent"},"sha256":{"linux-amd64":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/signal_agent-version.json", r.URL.Path)
		_, _ = w.Write([]byte(manifest))
	}))
	defer server.Close()

	info, err := getAgentVersionInfo(server.URL)
	require.NoError(t, err)
	require.Equal(t, "v1.0.0", info.Version)
	require.Equal(t, "0123456789abcdef0123456789abcdef01234567", info.CommitID)
	require.Equal(t, "https://artifacts.example/agent/abc/signal_agent", info.Files["linux-amd64"])
	require.Equal(t, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", info.SHA256["linux-amd64"])
}

func TestGetAgentVersionInfoRejectsVersionOnlyManifest(t *testing.T) {
	agentVersionCacheMutex.Lock()
	cachedAgentVersionInfo = nil
	agentVersionCacheMutex.Unlock()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"version":"v1.0.0","build_date":"2026-08-09T07:00:00Z"}`))
	}))
	defer server.Close()

	_, err := getAgentVersionInfo(server.URL)
	require.EqualError(t, err, "Agent 版本信息缺少构建身份")
}
