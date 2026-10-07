package agent

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/service"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

// TestTunnelAuthorizationContract (T12) 验证 Server 授权模型与 Agent 校验契约的端到端联合一致性（正反例）
func TestTunnelAuthorizationContract(t *testing.T) {
	now := time.Now().UTC()

	// 1. 模拟从真实 Workload Inventory 生成的 ports_config
	bindings := []service.TunnelServiceBinding{
		{
			ResourceID:   "obs-mesh-inference-9000",
			Namespace:    "model-serving",
			NamespaceUID: "ns-uid-mesh-7788",
			ServiceName:  "vllm-inference",
			ServiceUID:   "svc-uid-vllm-4455",
			PortName:     "http-inference",
			PortNumber:   9000,
			Protocol:     "TCP",
			LocalPort:    19000,
		},
	}

	portsCfgJSON, err := json.Marshal(service.TunnelPortsConfig{
		K8sAPIEnabled: true,
		K8sAPIPort:    16443,
		Ports:         bindings,
	})
	require.NoError(t, err)

	parsedCfg, err := service.ParseTunnelPortsConfig(string(portsCfgJSON))
	require.NoError(t, err)
	require.Len(t, parsedCfg.Ports, 1)
	binding := parsedCfg.Ports[0]
	require.True(t, binding.Complete(), "元数据必须满足 Complete() 校验，严禁缺失 UID 或 PortName")

	sessionID := "tunnel-session-contract-1"
	peerUser := "svc-tunnel-runner"
	peerNodeID := uint64(8801)

	// 2. 模拟 Server 端基于 ports_config 构建下发的 ContainerServiceResource 与授权快照 (SessionAuthorizationSnapshot)
	resource := &pb.ContainerServiceResource{
		ResourceId:            binding.ResourceID,
		ServiceName:           binding.ServiceName,
		Namespace:             binding.Namespace,
		PortNumber:            binding.PortNumber,
		Protocol:              binding.Protocol,
		AgentIp:               "100.64.0.88",
		AgentName:             "edge-gpu-node",
		LocalPort:             binding.LocalPort,
		SessionId:             sessionID,
		SourceId:              "source-tunnel-1",
		ServiceUid:            binding.ServiceUID,
		PortName:              binding.PortName,
		TargetRevisionId:      "rev-contract-target-1",
		AuthorizationRevision: 1,
		SvcProxyPort:          50051,
	}

	permission := &pb.ResourceSessionPermissionV2{
		SessionId:             sessionID,
		TenantId:              "tenant-default",
		ResourceId:            binding.ResourceID,
		SourceId:              "source-tunnel-1",
		TargetRevisionId:      "rev-contract-target-1",
		UserId:                1001,
		UserName:              peerUser,
		DeviceId:              2001,
		DeviceHeadscaleNodeId: peerNodeID,
		ResourceType:          "container_service",
		Action:                "connect",
		AllocationId:          "alloc-contract-1",
		GrantId:               "grant-contract-1",
		GrantRevision:         1,
		AuthorizationRevision: 1,
		ValidUntil:            timestamppb.New(now.Add(2 * time.Hour)),
		Target: &pb.ResourceSessionTargetV2{
			NamespaceUid:  binding.NamespaceUID,
			NamespaceName: binding.Namespace,
			ServiceUid:    binding.ServiceUID,
			ServiceName:   binding.ServiceName,
			PortName:      binding.PortName,
			PortNumber:    binding.PortNumber,
			Protocol:      binding.Protocol,
		},
	}

	// 3. 载入 Agent 授权存储
	authCache := NewSessionAuthorizationCache()
	snapshot := signedSessionSnapshot(t, 1, now, permission)
	require.NoError(t, authCache.Apply(snapshot, now))

	proxy := &K8SSVCProxy{authorizations: authCache}
	peer := &PeerIdentity{UserName: peerUser, NodeID: peerNodeID, Role: "client"}

	// 4. 正例验证：以 ContainerServiceResource 构造的 SVCProxyData 首包调用 authorizeV2Request 成功
	validMsg := &pb.SVCProxyData{
		IsConnect:             true,
		SessionId:             resource.SessionId,
		ResourceId:            resource.ResourceId,
		SourceId:              resource.SourceId,
		TargetRevisionId:      resource.TargetRevisionId,
		AuthorizationRevision: resource.AuthorizationRevision,
		Namespace:             resource.Namespace,
		ServiceName:           resource.ServiceName,
		ServiceUid:            resource.ServiceUid,
		PortName:              resource.PortName,
		Port:                  resource.PortNumber,
		Protocol:              resource.Protocol,
	}

	authorized, err := proxy.authorizeV2Request(validMsg, peer, now)
	require.NoError(t, err, "合法元数据的首包请求必须通过 Agent 校验")
	require.Equal(t, sessionID, authorized.SessionId)

	// 5. 正例验证：用对应真实 DiscoveredService 调 serviceMatchesV2Permission 成功
	discoveredSvc := &DiscoveredService{
		UID:       binding.ServiceUID,
		Namespace: binding.Namespace,
		Name:      binding.ServiceName,
		ClusterIP: "10.96.88.99",
		Ports: []DiscoveredServicePort{
			{
				Name:     binding.PortName,
				Port:     binding.PortNumber,
				Protocol: binding.Protocol,
			},
		},
	}
	require.True(t, serviceMatchesV2Permission(discoveredSvc, authorized.Target), "匹配的 DiscoveredService 必须通过目标校验")

	// 6. 反例验证（B6）：Service UID 发生变化 → Agent 授权校验与服务匹配必须坚决拒绝！
	// 6a. 首包中的 Service UID 被篡改或与授权快照不一致
	tamperedMsg := proto.Clone(validMsg).(*pb.SVCProxyData)
	tamperedMsg.ServiceUid = "svc-uid-forged-9999"
	_, err = proxy.authorizeV2Request(tamperedMsg, peer, now)
	require.Error(t, err, "Service UID 变动时 authorizeV2Request 必须报错拒绝")

	// 6b. Kubernetes 实际发现的服务 UID 发生重建或漂移
	recreatedSvc := *discoveredSvc
	recreatedSvc.UID = "svc-uid-recreated-new"
	require.False(t, serviceMatchesV2Permission(&recreatedSvc, authorized.Target), "实际 Service UID 不一致时 serviceMatchesV2Permission 必须返回 false")

	// 6c. 端口不匹配
	wrongPortSvc := *discoveredSvc
	wrongPortSvc.Ports = []DiscoveredServicePort{{Name: binding.PortName, Port: 9001, Protocol: binding.Protocol}}
	require.False(t, serviceMatchesV2Permission(&wrongPortSvc, authorized.Target), "目标端口不一致时必须拒绝")

	// 6d. 协议不匹配
	wrongProtoSvc := *discoveredSvc
	wrongProtoSvc.Ports = []DiscoveredServicePort{{Name: binding.PortName, Port: binding.PortNumber, Protocol: "UDP"}}
	require.False(t, serviceMatchesV2Permission(&wrongProtoSvc, authorized.Target), "非 TCP 协议必须拒绝")
}
