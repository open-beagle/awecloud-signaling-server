package grpc

import (
	"testing"

	v1 "github.com/juanfont/headscale/gen/go/headscale/v1"
	"github.com/stretchr/testify/require"
)

func TestValidDesktopHeadscaleNodeRequiresCurrentIPAndUser(t *testing.T) {
	node := &v1.Node{
		Id:          61,
		IpAddresses: []string{"100.64.0.60", "fd7a:115c:a1e0::3c"},
		User:        &v1.User{Name: "client-shucheng"},
	}

	require.True(t, validDesktopHeadscaleNode(node, "client-shucheng", "100.64.0.60"))
	require.False(t, validDesktopHeadscaleNode(node, "client-other", "100.64.0.60"))
	require.False(t, validDesktopHeadscaleNode(node, "client-shucheng", "100.64.0.40"))
	require.False(t, validDesktopHeadscaleNode(nil, "client-shucheng", "100.64.0.60"))
}
