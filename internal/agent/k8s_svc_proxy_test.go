package agent

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

func TestK8SSVCProxyV2RequiresExactSessionIdentityAndTarget(t *testing.T) {
	now := time.Now().UTC()
	cache := NewSessionAuthorizationCache()
	permission := serviceSessionPermission(now)
	require.NoError(t, cache.Apply(signedSessionSnapshot(t, 1, now, permission), now))
	proxy := &K8SSVCProxy{authorizations: cache}
	request := serviceSessionRequest(permission)

	authorized, err := proxy.authorizeV2Request(request, &PeerIdentity{UserName: "alice", NodeID: 7001, Role: "client"}, now)
	require.NoError(t, err)
	require.Equal(t, permission.SessionId, authorized.SessionId)

	_, err = proxy.authorizeV2Request(request, &PeerIdentity{UserName: "alice", NodeID: 7002, Role: "client"}, now)
	require.Error(t, err)
	changed := proto.Clone(request).(*pb.SVCProxyData)
	changed.Port = 8443
	_, err = proxy.authorizeV2Request(changed, &PeerIdentity{UserName: "alice", NodeID: 7001, Role: "client"}, now)
	require.Error(t, err)
	changed = proto.Clone(request).(*pb.SVCProxyData)
	changed.ServiceUid = "service-other"
	_, err = proxy.authorizeV2Request(changed, &PeerIdentity{UserName: "alice", NodeID: 7001, Role: "client"}, now)
	require.Error(t, err)
}

func TestServiceMatchesV2PermissionChecksUIDPortAndProtocol(t *testing.T) {
	target := serviceSessionPermission(time.Now().UTC()).Target
	svc := &DiscoveredService{
		UID: "service-uid", Namespace: "dev", Name: "api", ClusterIP: "10.96.0.20",
		Ports: []DiscoveredServicePort{{Name: "https", Port: 443, Protocol: "TCP"}},
	}
	require.True(t, serviceMatchesV2Permission(svc, target))

	changed := *svc
	changed.UID = "service-other"
	require.False(t, serviceMatchesV2Permission(&changed, target))
	changed = *svc
	changed.Ports = []DiscoveredServicePort{{Name: "https", Port: 443, Protocol: "UDP"}}
	require.False(t, serviceMatchesV2Permission(&changed, target))
	changed = *svc
	changed.ClusterIP = ""
	require.False(t, serviceMatchesV2Permission(&changed, target))
}

func TestK8SSVCProxyV2EnforcementIsExplicit(t *testing.T) {
	now := time.Now().UTC()
	cache := NewSessionAuthorizationCache()
	disabled := signedSessionSnapshot(t, 1, now)
	disabled.EnforceV2 = false
	disabled.PayloadHash = ""
	disabled = signExistingSessionSnapshot(t, disabled)
	require.NoError(t, cache.Apply(disabled, now))
	require.False(t, cache.Enabled(now))
	require.False(t, cache.EnforceV2())
}

func TestK8SSVCProxyV2FieldsCannotFallBackToLegacyAuthorization(t *testing.T) {
	require.False(t, hasV2SVCProxyFields(&pb.SVCProxyData{IsConnect: true, Namespace: "dev", ServiceName: "api", Port: 443}))
	for name, request := range map[string]*pb.SVCProxyData{
		"session":                {SessionId: "session-a"},
		"resource":               {ResourceId: "resource-a"},
		"source":                 {SourceId: "source-a"},
		"target revision":        {TargetRevisionId: "target-a"},
		"service uid":            {ServiceUid: "service-a"},
		"port name":              {PortName: "https"},
		"protocol":               {Protocol: "TCP"},
		"authorization revision": {AuthorizationRevision: 1},
	} {
		t.Run(name, func(t *testing.T) {
			require.True(t, hasV2SVCProxyFields(request))
		})
	}

	now := time.Now().UTC()
	cache := NewSessionAuthorizationCache()
	require.NoError(t, cache.Apply(signedSessionSnapshot(t, 1, now, serviceSessionPermission(now)), now))
	require.True(t, cache.EnforceV2())
	require.False(t, cache.Enabled(now.Add(time.Minute)))
	require.True(t, cache.EnforceV2(), "expired v2 policy must still block legacy fallback")
}

func serviceSessionPermission(now time.Time) *pb.ResourceSessionPermissionV2 {
	return &pb.ResourceSessionPermissionV2{
		SessionId: "session-service", TenantId: "tenant-a", ResourceId: "resource-service", SourceId: "source-a", TargetRevisionId: "target-a",
		UserId: 10, UserName: "alice", DeviceId: 20, DeviceHeadscaleNodeId: 7001,
		ResourceType: "container_service", Action: "connect", AllocationId: "allocation-a", GrantId: "grant-a",
		GrantRevision: 3, AuthorizationRevision: 4, ValidUntil: timestamppb.New(now.Add(time.Minute)),
		Target: &pb.ResourceSessionTargetV2{
			NamespaceUid: "namespace-uid", NamespaceName: "dev", ServiceUid: "service-uid", ServiceName: "api",
			PortName: "https", PortNumber: 443, Protocol: "TCP",
		},
	}
}

func serviceSessionRequest(permission *pb.ResourceSessionPermissionV2) *pb.SVCProxyData {
	return &pb.SVCProxyData{
		IsConnect: true, SessionId: permission.SessionId, ResourceId: permission.ResourceId, SourceId: permission.SourceId,
		TargetRevisionId: permission.TargetRevisionId, AuthorizationRevision: permission.AuthorizationRevision,
		Namespace: permission.Target.NamespaceName, ServiceName: permission.Target.ServiceName, ServiceUid: permission.Target.ServiceUid,
		PortName: permission.Target.PortName, Port: permission.Target.PortNumber, Protocol: permission.Target.Protocol,
	}
}
