package grpc

import (
	"testing"

	v1 "github.com/juanfont/headscale/gen/go/headscale/v1"
	"github.com/stretchr/testify/require"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func TestDeviceIPResolverPrefersBoundHeadscaleNodeIDOverDuplicateGivenName(t *testing.T) {
	nodes := []*v1.Node{
		{Id: 158, GivenName: "ide-duyingjun", Online: false, IpAddresses: []string{"100.64.0.117"}},
		{Id: 159, GivenName: "ide-duyingjun", Online: false, IpAddresses: []string{"100.64.0.118"}},
		{Id: 160, GivenName: "ide-duyingjun", Online: true, IpAddresses: []string{"100.64.0.119"}},
	}
	index := buildHeadscaleDeviceIPIndex(nodes)

	ip := resolveDeviceIP(model.Node{
		Name:            "ide-duyingjun",
		HeadscaleNodeID: 160,
		IP:              "100.64.0.119",
	}, index)

	require.Equal(t, "100.64.0.119", ip)
}

func TestDeviceIPResolverFallsBackToOnlineNewestDuplicateThenDatabaseIP(t *testing.T) {
	nodes := []*v1.Node{
		{Id: 158, GivenName: "ide-duyingjun", Online: false, IpAddresses: []string{"100.64.0.117"}},
		{Id: 160, GivenName: "ide-duyingjun", Online: true, IpAddresses: []string{"100.64.0.119"}},
	}
	index := buildHeadscaleDeviceIPIndex(nodes)

	require.Equal(t, "100.64.0.119", resolveDeviceIP(model.Node{Name: "ide-duyingjun"}, index))
	require.Equal(t, "100.64.0.42", resolveDeviceIP(model.Node{Name: "missing", IP: "100.64.0.42"}, index))
}
