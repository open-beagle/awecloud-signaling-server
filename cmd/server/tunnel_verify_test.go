package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/headscale"
)

func TestCanonicalPolicyHash(t *testing.T) {
	p1 := &headscale.ACLPolicy{
		ACLs: []headscale.ACLRule{
			{Action: "accept", Src: []string{"tag:b", "tag:a"}, Dst: []string{"tag:agent:50051", "tag:agent:6443"}},
			{Action: "accept", Src: []string{"tag:c"}, Dst: []string{"tag:x:80"}},
		},
	}
	p2 := &headscale.ACLPolicy{
		ACLs: []headscale.ACLRule{
			{Action: "accept", Src: []string{"tag:c"}, Dst: []string{"tag:x:80"}},
			{Action: "accept", Src: []string{"tag:a", "tag:b"}, Dst: []string{"tag:agent:6443", "tag:agent:50051"}},
		},
	}

	h1 := canonicalPolicyHash(p1)
	h2 := canonicalPolicyHash(p2)
	require.NotEmpty(t, h1)
	require.Equal(t, h1, h2, "不同顺序但等价的 ACLPolicy 规范化哈希必须一致")
}

func TestRunTunnelVerify_MissingTunnelFlag(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := runTunnelVerifyWithOutput([]string{"-c", "nonexistent.toml"}, &stdout, &stderr)
	require.Equal(t, 1, code)
	require.Contains(t, stderr.String(), "必须通过 --tunnel 指定 Tunnel 名称")
}

func TestRunTunnelVerify_NonexistentConfigFile(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := runTunnelVerifyWithOutput([]string{"-c", "nonexistent-server-config.toml", "--tunnel", "my-tunnel"}, &stdout, &stderr)
	require.Equal(t, 1, code)
	require.Contains(t, stderr.String(), "加载配置文件失败")
}
