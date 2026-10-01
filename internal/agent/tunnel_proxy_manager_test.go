package agent

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

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

// TestTunnel_TCPProxyForwarding 验证出站代理端到端双向数据转发
func TestTunnel_TCPProxyForwarding(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 启动一个模拟的边缘后端 Echo TCP Server
	echoBackend, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer echoBackend.Close()

	backendPort := echoBackend.Addr().(*net.TCPAddr).Port

	go func() {
		for {
			conn, err := echoBackend.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _ = io.Copy(c, c) // Echo back
			}(conn)
		}
	}()

	mgr := NewTunnelProxyManager("edge-gpu-5090", nil, ctx)
	defer mgr.Stop()

	// 使用 Mock Dialer 桥接目标地址
	mgr.SetDialer(func(dialCtx context.Context, network, addr string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(dialCtx, "tcp", fmt.Sprintf("127.0.0.1:%d", backendPort))
	})

	localPort := getFreePort(t)

	mgr.SyncTunnelProxies([]*pb.ContainerServiceResource{
		{
			ResourceId:  "res-echo",
			ServiceName: "mcp-service",
			LocalPort:   int32(localPort),
			PortNumber:  8000,
			AgentIp:     "100.64.0.50",
			AgentName:   "edge-gpu-5090",
		},
	})

	require.Len(t, mgr.GetActivePorts(), 1)

	// 客户端连接本地 Tunnel 端口
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", localPort), 2*time.Second)
	require.NoError(t, err)
	defer conn.Close()

	testMsg := "hello ztna signal tunnel"
	_, err = conn.Write([]byte(testMsg))
	require.NoError(t, err)

	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	require.NoError(t, err)
	require.Equal(t, testMsg, string(buf[:n]))
}

// TestTunnel_K8sAPIProxy6443 验证启用 6443 控制面代理后，本地发起的请求能透明路由至边缘 K8s API
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
		var d net.Dialer
		return d.DialContext(dialCtx, "tcp", mockK8sAPI.Listener.Addr().String())
	})

	localPort := getFreePort(t)

	// 下发 K8s API 代理资源（默认本地端口 6443，测试中使用 localPort）
	mgr.SyncTunnelProxies([]*pb.ContainerServiceResource{
		{
			ResourceId:  "res-k8s-api",
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

