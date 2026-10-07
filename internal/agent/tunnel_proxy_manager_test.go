package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

// getFreePort 获取一个可用的本地测试端口
func getFreePort(t *testing.T) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// TestTunnel_ZeroPortSilent 验证获授权资源 local_port <= 0 时本地保持静默，不开放任何端口
func TestTunnel_ZeroPortSilent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr := NewTunnelProxyManager("edge-gpu-5090", nil, ctx)
	defer mgr.Stop()

	// 下发两个资源，local_port 均为 0 或负数
	mgr.SyncTunnelProxies([]*pb.ContainerServiceResource{
		{
			ResourceId:  "res-1",
			ServiceName: "mcp-service",
			LocalPort:   0,
			AgentName:   "edge-gpu-5090",
		},
		{
			ResourceId:  "res-2",
			ServiceName: "k8s-api",
			LocalPort:   -1,
			AgentName:   "edge-gpu-5090",
		},
	})

	// 断言：本地活跃端口列表必须为空
	require.Empty(t, mgr.GetActivePorts())
}

// TestTunnel_ExplicitPortListen 验证显式设置 local_port > 0 时本地成功启动监听并可建立连接
func TestTunnel_ExplicitPortListen(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr := NewTunnelProxyManager("edge-gpu-5090", nil, ctx)
	defer mgr.Stop()

	port := getFreePort(t)

	mgr.SyncTunnelProxies([]*pb.ContainerServiceResource{
		{
			ResourceId:  "res-10080",
			ServiceName: "mcp-service",
			LocalPort:   int32(port),
			AgentName:   "edge-gpu-5090",
		},
	})

	active := mgr.GetActivePorts()
	require.Len(t, active, 1)
	require.Equal(t, port, active[0])

	// 验证本地端口可成功连接
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 2*time.Second)
	require.NoError(t, err)
	_ = conn.Close()
}

// TestTunnel_1to1AgentFilter 验证严格 1:1 对等隔离，非目标 Agent 的资源一律丢弃
func TestTunnel_1to1AgentFilter(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr := NewTunnelProxyManager("edge-gpu-5090", nil, ctx)
	defer mgr.Stop()

	port1 := getFreePort(t)
	port2 := getFreePort(t)

	mgr.SyncTunnelProxies([]*pb.ContainerServiceResource{
		{
			ResourceId:  "res-5090",
			ServiceName: "mcp-service",
			LocalPort:   int32(port1),
			AgentName:   "edge-gpu-5090", // 目标匹配
		},
		{
			ResourceId:  "res-4090",
			ServiceName: "other-service",
			LocalPort:   int32(port2),
			AgentName:   "edge-gpu-4090", // 目标不匹配
		},
	})

	// 只有 port1 被监听，port2 必须被过滤
	active := mgr.GetActivePorts()
	require.Len(t, active, 1)
	require.Equal(t, port1, active[0])
}

// TestTunnel_DynamicPortRevocation 验证动态授权变更时，端口立即 Close 释放
func TestTunnel_DynamicPortRevocation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr := NewTunnelProxyManager("edge-gpu-5090", nil, ctx)
	defer mgr.Stop()

	port := getFreePort(t)

	// 1. 开启端口
	mgr.SyncTunnelProxies([]*pb.ContainerServiceResource{
		{
			ResourceId:  "res-revocation",
			ServiceName: "mcp-service",
			LocalPort:   int32(port),
			AgentName:   "edge-gpu-5090",
		},
	})
	require.Len(t, mgr.GetActivePorts(), 1)

	// 2. 变更下发：置为 0（撤销端口）
	mgr.SyncTunnelProxies([]*pb.ContainerServiceResource{
		{
			ResourceId:  "res-revocation",
			ServiceName: "mcp-service",
			LocalPort:   0,
			AgentName:   "edge-gpu-5090",
		},
	})

	// 3. 验证活跃端口列表已清空
	require.Empty(t, mgr.GetActivePorts())

	// 4. 验证本地端口无法连接（已关闭释放）
	_, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 500*time.Millisecond)
	require.Error(t, err)
}

// TestTunnel_ContainerServiceRejectsDirectDial 验证普通容器服务在未配置 SvcProxyPort 时坚决不拨号宿主机端口
func TestTunnel_ContainerServiceRejectsDirectDial(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr := NewTunnelProxyManager("edge-gpu-5090", nil, ctx)
	defer mgr.Stop()

	localPort := getFreePort(t)

	// 下发普通业务应用，SvcProxyPort 为 0（未配置有效 SVCProxy）
	mgr.SyncTunnelProxies([]*pb.ContainerServiceResource{
		{
			ResourceId:   "res-mcp",
			ServiceName:  "mcp-service",
			LocalPort:    int32(localPort),
			PortNumber:   8000,
			SvcProxyPort: 0, // 无有效代理端口
			AgentIp:      "100.64.0.50",
			AgentName:    "edge-gpu-5090",
			Protocol:     "TCP",
		},
	})

	require.Len(t, mgr.GetActivePorts(), 1)

	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", localPort), 2*time.Second)
	require.NoError(t, err)
	defer conn.Close()

	// 服务端应立即断开连接（EOF），坚决不尝试拨号 AgentIp:PortNumber
	buf := make([]byte, 1024)
	_, err = conn.Read(buf)
	require.Equal(t, io.EOF, err)
}

// TestTunnel_K8sAPIProxy6443 验证精确匹配 k8s-api 时透明转发至宿主机 6443
func TestTunnel_K8sAPIProxy6443(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 启动一个模拟的边缘 K8s API HTTP Server
	mockK8sAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/livez", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer mockK8sAPI.Close()

	mgr := NewTunnelProxyManager("edge-gpu-5090", nil, ctx)
	defer mgr.Stop()

	// Mock Dialer 路由到模拟的 mockK8sAPI
	mgr.SetDialer(func(dialCtx context.Context, network, addr string) (net.Conn, error) {
		require.Equal(t, "100.64.0.10:6443", addr, "K8s API 目标端口必须固定为 6443")
		var d net.Dialer
		return d.DialContext(dialCtx, "tcp", mockK8sAPI.Listener.Addr().String())
	})

	localPort := getFreePort(t)

	// 下发 K8s API 代理资源（ResourceId 必须精确匹配 k8s-api）
	mgr.SyncTunnelProxies([]*pb.ContainerServiceResource{
		{
			ResourceId:  "k8s-api",
			ServiceName: "kubernetes",
			Namespace:   "default",
			LocalPort:   int32(localPort),
			PortNumber:  6443,
			AgentIp:     "100.64.0.10",
			AgentName:   "edge-gpu-5090",
		},
	})

	require.Len(t, mgr.GetActivePorts(), 1)
	require.Equal(t, localPort, mgr.GetActivePorts()[0])

	// 本地客户端向本地 Tunnel 端口发起 HTTP GET /livez 请求
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/livez", localPort))
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "ok", string(body))
}

type mockAgentSVCProxyServer struct {
	pb.UnimplementedAgentServiceServer
	rejectMsg string
	received  chan *pb.SVCProxyData
}

func (s *mockAgentSVCProxyServer) SVCProxy(stream grpc.BidiStreamingServer[pb.SVCProxyData, pb.SVCProxyData]) error {
	req, err := stream.Recv()
	if err != nil {
		return err
	}
	if s.received != nil {
		s.received <- req
	}
	if s.rejectMsg != "" {
		return stream.Send(&pb.SVCProxyData{
			Error:      s.rejectMsg,
			SessionId:  req.SessionId,
			ResourceId: req.ResourceId,
			IsClose:    true,
		})
	}
	// 首包确认建立连接
	if err := stream.Send(&pb.SVCProxyData{
		SessionId:  req.SessionId,
		ResourceId: req.ResourceId,
		Data:       []byte("connected"),
	}); err != nil {
		return err
	}

	for {
		data, err := stream.Recv()
		if err != nil {
			return err
		}
		if data.IsClose {
			return nil
		}
		if err := stream.Send(data); err != nil {
			return err
		}
	}
}

// TestTunnel_ContainerServiceWithPort6443UsesSVCProxy (T8) 验证普通容器服务即使端口为 6443 也必须走 SVCProxy 而非 host_direct
func TestTunnel_ContainerServiceWithPort6443UsesSVCProxy(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 启动模拟的 Agent gRPC 服务
	grpcListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer grpcListener.Close()

	grpcServer := grpc.NewServer()
	mockSvc := &mockAgentSVCProxyServer{
		received: make(chan *pb.SVCProxyData, 1),
	}
	pb.RegisterAgentServiceServer(grpcServer, mockSvc)
	go func() { _ = grpcServer.Serve(grpcListener) }()
	defer grpcServer.Stop()

	mgr := NewTunnelProxyManager("edge-gpu-5090", nil, ctx)
	defer mgr.Stop()

	var dialedAddrs []string
	var dialMu sync.Mutex

	mgr.SetDialer(func(dialCtx context.Context, network, addr string) (net.Conn, error) {
		dialMu.Lock()
		dialedAddrs = append(dialedAddrs, addr)
		dialMu.Unlock()

		// 将发往 100.64.0.50:50051 的 gRPC 拨号重定向至本地 grpcListener
		var d net.Dialer
		return d.DialContext(dialCtx, "tcp", grpcListener.Addr().String())
	})

	localPort := getFreePort(t)

	// 下发一个 PortNumber 刚好为 6443 的普通容器服务（非 k8s-api）
	mgr.SyncTunnelProxies([]*pb.ContainerServiceResource{
		{
			ResourceId:            "obs-custom-webhook-6443",
			ServiceName:           "custom-webhook",
			Namespace:             "dev-ops",
			LocalPort:             int32(localPort),
			PortNumber:            6443, // 端口虽然是 6443，但绝不是 k8s-api
			SvcProxyPort:          50051,
			AgentIp:               "100.64.0.50",
			AgentName:             "edge-gpu-5090",
			Protocol:              "TCP",
			SessionId:             "session-test-6443",
			ServiceUid:            "uid-webhook-1",
			AuthorizationRevision: 1,
		},
	})

	require.Len(t, mgr.GetActivePorts(), 1)

	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", localPort), 2*time.Second)
	require.NoError(t, err)
	defer conn.Close()

	select {
	case req := <-mockSvc.received:
		require.Equal(t, "obs-custom-webhook-6443", req.ResourceId)
		require.Equal(t, "custom-webhook", req.ServiceName)
		require.Equal(t, int32(6443), req.Port)
	case <-time.After(3 * time.Second):
		t.Fatal("超时未收到 Agent SVCProxy 首包")
	}

	dialMu.Lock()
	defer dialMu.Unlock()
	require.Contains(t, dialedAddrs, "100.64.0.50:50051", "必须拨号 Agent gRPC 代理端口 50051")
	for _, a := range dialedAddrs {
		require.NotEqual(t, "100.64.0.50:6443", a, "绝对禁止直连宿主机 6443 端口")
	}
}

// TestTunnel_NoFallbackAfterAgentReject (T11) 验证被 Agent 拒绝后坚决不降级直连
func TestTunnel_NoFallbackAfterAgentReject(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 启动模拟的 Agent gRPC 服务，配置为拒绝连接
	grpcListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer grpcListener.Close()

	grpcServer := grpc.NewServer()
	mockSvc := &mockAgentSVCProxyServer{
		rejectMsg: "tenant permission denied: service revoked",
	}
	pb.RegisterAgentServiceServer(grpcServer, mockSvc)
	go func() { _ = grpcServer.Serve(grpcListener) }()
	defer grpcServer.Stop()

	mgr := NewTunnelProxyManager("edge-gpu-5090", nil, ctx)
	defer mgr.Stop()

	var dialedAddrs []string
	var dialMu sync.Mutex

	mgr.SetDialer(func(dialCtx context.Context, network, addr string) (net.Conn, error) {
		dialMu.Lock()
		dialedAddrs = append(dialedAddrs, addr)
		dialMu.Unlock()

		var d net.Dialer
		return d.DialContext(dialCtx, "tcp", grpcListener.Addr().String())
	})

	localPort := getFreePort(t)

	mgr.SyncTunnelProxies([]*pb.ContainerServiceResource{
		{
			ResourceId:   "res-mcp-reject",
			ServiceName:  "mcp-service",
			LocalPort:    int32(localPort),
			PortNumber:   8000,
			SvcProxyPort: 50051,
			AgentIp:      "100.64.0.50",
			AgentName:    "edge-gpu-5090",
			Protocol:     "TCP",
		},
	})

	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", localPort), 2*time.Second)
	require.NoError(t, err)
	defer conn.Close()

	// 客户端读取响应，对端应直接断开 EOF
	buf := make([]byte, 1024)
	_, _ = conn.Read(buf)

	dialMu.Lock()
	defer dialMu.Unlock()

	// 断言：拨号记录必须仅包含 AgentIp:50051，从未拨过 AgentIp:8000
	require.Equal(t, []string{"100.64.0.50:50051"}, dialedAddrs)
}

// TestTunnel_StatuszEndpoint (T13b) 验证 /statusz 监控端点输出路径、指标与守卫计数
func TestTunnel_StatuszEndpoint(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr := NewTunnelProxyManager("edge-gpu-5090", nil, ctx)
	defer mgr.Stop()

	statusPort := getFreePort(t)
	err := mgr.StartStatusServer(fmt.Sprintf("127.0.0.1:%d", statusPort))
	require.NoError(t, err)

	mgr.SyncTunnelProxies([]*pb.ContainerServiceResource{
		{
			ResourceId:   "obs-web-app",
			ServiceName:  "web-app",
			Namespace:    "prod",
			LocalPort:    18080,
			PortNumber:   8080,
			SvcProxyPort: 50051,
			AgentIp:      "100.64.0.50",
			AgentName:    "edge-gpu-5090",
		},
		{
			ResourceId:  "k8s-api",
			ServiceName: "kubernetes-api",
			Namespace:   "default",
			LocalPort:   16443,
			PortNumber:  6443,
			AgentIp:     "100.64.0.50",
			AgentName:   "edge-gpu-5090",
		},
	})

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/statusz", statusPort))
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	var statuses []*TunnelResourceStatus
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&statuses))

	require.Len(t, statuses, 2)

	// k8s-api 判定为 host_direct
	var k8sStatus, webStatus *TunnelResourceStatus
	for _, s := range statuses {
		if s.ResourceID == "k8s-api" {
			k8sStatus = s
		} else if s.ResourceID == "obs-web-app" {
			webStatus = s
		}
	}

	require.NotNil(t, k8sStatus)
	require.Equal(t, "host_direct", k8sStatus.Path)
	require.Equal(t, int64(0), k8sStatus.DirectDialViolation)

	require.NotNil(t, webStatus)
	require.Equal(t, "svcproxy", webStatus.Path)
	require.Equal(t, int64(0), webStatus.DirectDialViolation)
}
