package grpc

import (
	"context"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	googlegrpc "google.golang.org/grpc"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/cache"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

type fakeAgentHeartbeatStream struct {
	googlegrpc.ServerStream
	ctx       context.Context
	recvCh    chan *pb.AgentHeartbeatRequest
	exitGate  <-chan struct{}
	firstSent chan struct{}
	sendOnce  sync.Once
	sentMu    sync.Mutex
	sentResps []*pb.AgentHeartbeatResponse
}

func newFakeAgentHeartbeatStream(ctx context.Context, exitGate <-chan struct{}) *fakeAgentHeartbeatStream {
	return &fakeAgentHeartbeatStream{
		ctx:       ctx,
		recvCh:    make(chan *pb.AgentHeartbeatRequest, 4),
		exitGate:  exitGate,
		firstSent: make(chan struct{}),
	}
}

func (f *fakeAgentHeartbeatStream) Context() context.Context {
	return f.ctx
}

func (f *fakeAgentHeartbeatStream) Recv() (*pb.AgentHeartbeatRequest, error) {
	select {
	case req, ok := <-f.recvCh:
		if !ok {
			if f.exitGate != nil {
				<-f.exitGate
			}
			return nil, io.EOF
		}
		return req, nil
	case <-f.ctx.Done():
		if f.exitGate != nil {
			<-f.exitGate
		}
		return nil, f.ctx.Err()
	}
}

func (f *fakeAgentHeartbeatStream) Send(resp *pb.AgentHeartbeatResponse) error {
	f.sentMu.Lock()
	f.sentResps = append(f.sentResps, resp)
	f.sentMu.Unlock()
	f.sendOnce.Do(func() {
		close(f.firstSent)
	})
	return nil
}

func setupAgentHeartbeatTestEnv(t *testing.T) (*gorm.DB, *AgentServiceServer, *cache.NodeRuntimeStore, uint64, uint64) {
	t.Helper()

	oldDB := db.DB
	cache.ClearAllCache()
	t.Cleanup(func() {
		db.DB = oldDB
		cache.ClearAllCache()
	})

	dsn := fmt.Sprintf("file:agent_hb_reconnect_%d?mode=memory&cache=shared", time.Now().UnixNano())
	testDB, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	db.DB = testDB

	require.NoError(t, testDB.AutoMigrate(
		&model.User{},
		&model.Node{},
		&model.Endpoint{},
		&model.DomainRegistry{},
		&model.SystemConfig{},
		&model.ProxyService{},
		&model.PortForward{},
		&model.AclK8SUserPermission{},
		&model.AclK8SGroupPermission{},
		&model.AclSSHUserPermission{},
		&model.AclSSHGroupPermission{},
		&model.Group{},
		&model.GroupMember{},
		&model.TechnicalResource{},
		&model.TechnicalResourceBinding{},
		&model.SupplyCandidate{},
		&model.PlatformResource{},
		&model.PlatformResourceSource{},
	))

	agentUser := model.User{
		ID:      101,
		Name:    "agent-reconnect-user",
		Role:    model.UserRoleAgent,
		Enabled: true,
	}
	require.NoError(t, testDB.Create(&agentUser).Error)

	now := time.Now()
	agentNode := model.Node{
		ID:            202,
		UserID:        agentUser.ID,
		Name:          "device-node-202",
		Type:          model.NodeTypeAgent,
		Hostname:      "host-202",
		IP:            "100.64.0.1",
		LastHeartbeat: &now,
	}
	require.NoError(t, testDB.Create(&agentNode).Error)

	runtimeStore := cache.NewNodeRuntimeStore()
	require.NoError(t, runtimeStore.LoadFromDB(context.Background(), testDB))

	server := &AgentServiceServer{
		connections:                 make(map[uint64]*AgentConnection),
		configVersion:               1,
		containerSessionAcks:        make(map[uint64][]string),
		authorizationSnapshotStates: make(map[string]*authorizationSnapshotState),
		runtimeStore:                runtimeStore,
	}

	return testDB, server, runtimeStore, agentUser.ID, agentNode.ID
}

func TestAgentHeartbeatReconnect_StaleStreamExitPreservesNewConnectionState(t *testing.T) {
	testDB, server, runtimeStore, agentID, nodeID := setupAgentHeartbeatTestEnv(t)

	const endpointName = "ep-reconnect-1"

	// 1. 建立旧心跳流 A，使用 exitGate 控制旧流收到 Cancel 后的退出时机
	gateA := make(chan struct{})
	ctxA, cancelA := context.WithCancel(context.Background())
	defer cancelA()
	streamA := newFakeAgentHeartbeatStream(ctxA, gateA)
	streamA.recvCh <- &pb.AgentHeartbeatRequest{
		AgentId:         agentID,
		DeviceName:      "device-node-202",
		Hostname:        "host-202",
		TunnelIp:        "100.64.0.10",
		TunnelConnected: true,
		ConnectedEndpoints: []*pb.ConnectedEndpoint{
			{Name: endpointName, Os: "linux", Arch: "amd64"},
		},
	}

	doneA := make(chan error, 1)
	go func() {
		doneA <- server.Heartbeat(streamA)
	}()

	select {
	case <-streamA.firstSent:
	case <-time.After(3 * time.Second):
		t.Fatal("stream A did not complete initial heartbeat")
	}

	connA := server.GetAgentConnection(agentID)
	require.NotNil(t, connA)
	require.Equal(t, "100.64.0.10", connA.TunnelIP)

	// 2. 同一 nodeID 建立新心跳流 B（触发 oldConn.Cancel()，但旧流 A 因 gateA 尚未退出 defer）
	ctxB, cancelB := context.WithCancel(context.Background())
	defer cancelB()
	streamB := newFakeAgentHeartbeatStream(ctxB, nil)
	streamB.recvCh <- &pb.AgentHeartbeatRequest{
		AgentId:         agentID,
		DeviceName:      "device-node-202",
		Hostname:        "host-202",
		TunnelIp:        "100.64.0.20",
		TunnelConnected: true,
		ConnectedEndpoints: []*pb.ConnectedEndpoint{
			{Name: endpointName, Os: "linux", Arch: "amd64"},
		},
	}

	doneB := make(chan error, 1)
	go func() {
		doneB <- server.Heartbeat(streamB)
	}()

	select {
	case <-streamB.firstSent:
	case <-time.After(3 * time.Second):
		t.Fatal("stream B did not complete initial heartbeat")
	}

	// 为新连接 B 写入待回传的容器会话 ACK，并同步更新数据库 Node 的 IP 与心跳时间
	server.containerSessionAckMutex.Lock()
	server.containerSessionAcks[nodeID] = []string{"ack-event-b1", "ack-event-b2"}
	server.containerSessionAckMutex.Unlock()

	nowB := time.Now()
	require.NoError(t, testDB.Model(&model.Node{}).Where("id = ?", nodeID).Updates(map[string]any{
		"ip":             "100.64.0.20",
		"last_heartbeat": &nowB,
	}).Error)

	// 3. 放行旧流 A 退出，执行旧流 A 的 defer
	close(gateA)
	select {
	case <-doneA:
	case <-time.After(3 * time.Second):
		t.Fatal("stream A did not exit after gate closed")
	}

	// 4. 验证新连接 B 的所有状态均未被旧流 A 的 defer 误清理
	connAfterAExit := server.GetAgentConnection(agentID)
	require.NotNil(t, connAfterAExit, "新连接表项 s.connections[nodeID] 不得被旧流删除")
	require.NotSame(t, connA, connAfterAExit)
	require.Equal(t, "100.64.0.20", connAfterAExit.TunnelIP)

	server.containerSessionAckMutex.Lock()
	acks := append([]string(nil), server.containerSessionAcks[nodeID]...)
	server.containerSessionAckMutex.Unlock()
	require.Equal(t, []string{"ack-event-b1", "ack-event-b2"}, acks, "新连接 ACK 队列不得被旧流清空")

	nodeStatus, ok := cache.GetNodeStatus(nodeID)
	require.True(t, ok, "NodeStatus 缓存不得被旧流删除")
	require.Equal(t, "100.64.0.20", nodeStatus.TunnelIP)

	rtNode, ok := runtimeStore.GetNode(nodeID)
	require.True(t, ok)
	require.Equal(t, "100.64.0.20", rtNode.IP, "RuntimeStore 中的节点 IP 不得被旧流置空")
	require.False(t, rtNode.LastHeartbeat.IsZero(), "RuntimeStore 中的节点心跳时间不得被旧流置空")

	var dbNode model.Node
	require.NoError(t, testDB.First(&dbNode, nodeID).Error)
	require.Equal(t, "100.64.0.20", dbNode.IP, "数据库 Node.IP 不得被旧流清空")
	require.NotNil(t, dbNode.LastHeartbeat, "数据库 Node.LastHeartbeat 不得被旧流清空")

	_, epCached := cache.GetEndpointStatus(endpointName)
	require.True(t, epCached, "EndpointStatus 缓存不得被旧流删除")

	var dbEndpoint model.Endpoint
	require.NoError(t, testDB.Where("user_id = ? AND name = ?", agentID, endpointName).First(&dbEndpoint).Error)
	require.Equal(t, "online", dbEndpoint.Status, "数据库 Endpoint.Status 不得被旧流置为 offline")

	// 清理流 B
	cancelB()
	select {
	case <-doneB:
	case <-time.After(3 * time.Second):
		t.Fatal("stream B did not exit after cancel")
	}
}

func TestAgentHeartbeatDisconnect_SoleConnectionExitCleansUpAllState(t *testing.T) {
	testDB, server, runtimeStore, agentID, nodeID := setupAgentHeartbeatTestEnv(t)

	const endpointName = "ep-sole-1"

	ctxA, cancelA := context.WithCancel(context.Background())
	defer cancelA()
	streamA := newFakeAgentHeartbeatStream(ctxA, nil)
	streamA.recvCh <- &pb.AgentHeartbeatRequest{
		AgentId:         agentID,
		DeviceName:      "device-node-202",
		Hostname:        "host-202",
		TunnelIp:        "100.64.0.30",
		TunnelConnected: true,
		ConnectedEndpoints: []*pb.ConnectedEndpoint{
			{Name: endpointName, Os: "linux", Arch: "amd64"},
		},
	}

	doneA := make(chan error, 1)
	go func() {
		doneA <- server.Heartbeat(streamA)
	}()

	select {
	case <-streamA.firstSent:
	case <-time.After(3 * time.Second):
		t.Fatal("stream A did not complete initial heartbeat")
	}

	server.containerSessionAckMutex.Lock()
	server.containerSessionAcks[nodeID] = []string{"ack-sole-1"}
	server.containerSessionAckMutex.Unlock()

	now := time.Now()
	require.NoError(t, testDB.Model(&model.Node{}).Where("id = ?", nodeID).Updates(map[string]any{
		"ip":             "100.64.0.30",
		"last_heartbeat": &now,
	}).Error)

	// 正常关闭唯一连接流 A
	close(streamA.recvCh)
	select {
	case err := <-doneA:
		require.ErrorIs(t, err, io.EOF)
	case <-time.After(3 * time.Second):
		t.Fatal("stream A did not exit after EOF")
	}

	// 验证所有状态均已清理
	require.Nil(t, server.GetAgentConnection(agentID), "唯一连接退出后 s.connections 应清理")

	server.containerSessionAckMutex.Lock()
	_, ackExists := server.containerSessionAcks[nodeID]
	server.containerSessionAckMutex.Unlock()
	require.False(t, ackExists, "唯一连接退出后 containerSessionAcks 应清理")

	_, nodeCached := cache.GetNodeStatus(nodeID)
	require.False(t, nodeCached, "唯一连接退出后 NodeStatus 缓存应清理")

	rtNode, ok := runtimeStore.GetNode(nodeID)
	require.True(t, ok)
	require.Empty(t, rtNode.IP, "唯一连接退出后 RuntimeStore IP 应清空")
	require.True(t, rtNode.LastHeartbeat.IsZero(), "唯一连接退出后 RuntimeStore LastHeartbeat 应清零")

	var dbNode model.Node
	require.NoError(t, testDB.First(&dbNode, nodeID).Error)
	require.Empty(t, dbNode.IP, "唯一连接退出后数据库 Node.IP 应清空")
	require.Nil(t, dbNode.LastHeartbeat, "唯一连接退出后数据库 Node.LastHeartbeat 应置空")

	_, epCached := cache.GetEndpointStatus(endpointName)
	require.False(t, epCached, "唯一连接退出后 EndpointStatus 缓存应清理")

	var dbEndpoint model.Endpoint
	require.NoError(t, testDB.Where("user_id = ? AND name = ?", agentID, endpointName).First(&dbEndpoint).Error)
	require.Equal(t, "offline", dbEndpoint.Status, "唯一连接退出后数据库 Endpoint.Status 应为 offline")
}

func TestAgentHeartbeatDisconnectNode_ActiveDisconnectCleansUpAllState(t *testing.T) {
	testDB, server, runtimeStore, agentID, nodeID := setupAgentHeartbeatTestEnv(t)

	const endpointName = "ep-active-disconnect-1"

	ctxA, cancelA := context.WithCancel(context.Background())
	defer cancelA()
	streamA := newFakeAgentHeartbeatStream(ctxA, nil)
	streamA.recvCh <- &pb.AgentHeartbeatRequest{
		AgentId:         agentID,
		DeviceName:      "device-node-202",
		Hostname:        "host-202",
		TunnelIp:        "100.64.0.40",
		TunnelConnected: true,
		ConnectedEndpoints: []*pb.ConnectedEndpoint{
			{Name: endpointName, Os: "linux", Arch: "amd64"},
		},
	}

	doneA := make(chan error, 1)
	go func() {
		doneA <- server.Heartbeat(streamA)
	}()

	select {
	case <-streamA.firstSent:
	case <-time.After(3 * time.Second):
		t.Fatal("stream A did not complete initial heartbeat")
	}

	server.containerSessionAckMutex.Lock()
	server.containerSessionAcks[nodeID] = []string{"ack-active-1"}
	server.containerSessionAckMutex.Unlock()

	now := time.Now()
	require.NoError(t, testDB.Model(&model.Node{}).Where("id = ?", nodeID).Updates(map[string]any{
		"ip":             "100.64.0.40",
		"last_heartbeat": &now,
	}).Error)

	// 调用 DisconnectNode 主动断开连接
	server.DisconnectNode(nodeID)

	// 等待流退出
	select {
	case <-doneA:
	case <-time.After(3 * time.Second):
		t.Fatal("stream A did not exit after DisconnectNode")
	}

	// 验证主动断开后所有状态均按原语义清理
	require.Nil(t, server.GetAgentConnection(agentID), "DisconnectNode 后 s.connections 应清理")

	server.containerSessionAckMutex.Lock()
	_, ackExists := server.containerSessionAcks[nodeID]
	server.containerSessionAckMutex.Unlock()
	require.False(t, ackExists, "DisconnectNode 后 containerSessionAcks 应清理")

	_, nodeCached := cache.GetNodeStatus(nodeID)
	require.False(t, nodeCached, "DisconnectNode 后 NodeStatus 缓存应清理")

	rtNode, ok := runtimeStore.GetNode(nodeID)
	require.True(t, ok)
	require.Empty(t, rtNode.IP, "DisconnectNode 后 RuntimeStore IP 应清空")
	require.True(t, rtNode.LastHeartbeat.IsZero(), "DisconnectNode 后 RuntimeStore LastHeartbeat 应清零")

	var dbNode model.Node
	require.NoError(t, testDB.First(&dbNode, nodeID).Error)
	require.Empty(t, dbNode.IP, "DisconnectNode 后数据库 Node.IP 应清空")
	require.Nil(t, dbNode.LastHeartbeat, "DisconnectNode 后数据库 Node.LastHeartbeat 应置空")

	_, epCached := cache.GetEndpointStatus(endpointName)
	require.False(t, epCached, "DisconnectNode 后 EndpointStatus 缓存应清理")

	var dbEndpoint model.Endpoint
	require.NoError(t, testDB.Where("user_id = ? AND name = ?", agentID, endpointName).First(&dbEndpoint).Error)
	require.Equal(t, "offline", dbEndpoint.Status, "DisconnectNode 后数据库 Endpoint.Status 应为 offline")
}

func TestAgentHeartbeatDisconnectAgent_ActiveDisconnectCleansUpAllState(t *testing.T) {
	testDB, server, runtimeStore, agentID, nodeID := setupAgentHeartbeatTestEnv(t)

	const endpointName = "ep-active-disconnect-agent-1"

	ctxA, cancelA := context.WithCancel(context.Background())
	defer cancelA()
	streamA := newFakeAgentHeartbeatStream(ctxA, nil)
	streamA.recvCh <- &pb.AgentHeartbeatRequest{
		AgentId:         agentID,
		DeviceName:      "device-node-202",
		Hostname:        "host-202",
		TunnelIp:        "100.64.0.50",
		TunnelConnected: true,
		ConnectedEndpoints: []*pb.ConnectedEndpoint{
			{Name: endpointName, Os: "linux", Arch: "amd64"},
		},
	}

	doneA := make(chan error, 1)
	go func() {
		doneA <- server.Heartbeat(streamA)
	}()

	select {
	case <-streamA.firstSent:
	case <-time.After(3 * time.Second):
		t.Fatal("stream A did not complete initial heartbeat")
	}

	server.containerSessionAckMutex.Lock()
	server.containerSessionAcks[nodeID] = []string{"ack-agent-active-1"}
	server.containerSessionAckMutex.Unlock()

	now := time.Now()
	require.NoError(t, testDB.Model(&model.Node{}).Where("id = ?", nodeID).Updates(map[string]any{
		"ip":             "100.64.0.50",
		"last_heartbeat": &now,
	}).Error)

	// 调用 DisconnectAgent 主动断开该 Agent 下的所有连接
	server.DisconnectAgent(agentID)

	select {
	case <-doneA:
	case <-time.After(3 * time.Second):
		t.Fatal("stream A did not exit after DisconnectAgent")
	}

	require.Nil(t, server.GetAgentConnection(agentID), "DisconnectAgent 后 s.connections 应清理")

	server.containerSessionAckMutex.Lock()
	_, ackExists := server.containerSessionAcks[nodeID]
	server.containerSessionAckMutex.Unlock()
	require.False(t, ackExists, "DisconnectAgent 后 containerSessionAcks 应清理")

	_, nodeCached := cache.GetNodeStatus(nodeID)
	require.False(t, nodeCached, "DisconnectAgent 后 NodeStatus 缓存应清理")

	rtNode, ok := runtimeStore.GetNode(nodeID)
	require.True(t, ok)
	require.Empty(t, rtNode.IP, "DisconnectAgent 后 RuntimeStore IP 应清空")
	require.True(t, rtNode.LastHeartbeat.IsZero(), "DisconnectAgent 后 RuntimeStore LastHeartbeat 应清零")

	var dbNode model.Node
	require.NoError(t, testDB.First(&dbNode, nodeID).Error)
	require.Empty(t, dbNode.IP, "DisconnectAgent 后数据库 Node.IP 应清空")
	require.Nil(t, dbNode.LastHeartbeat, "DisconnectAgent 后数据库 Node.LastHeartbeat 应置空")

	_, epCached := cache.GetEndpointStatus(endpointName)
	require.False(t, epCached, "DisconnectAgent 后 EndpointStatus 缓存应清理")

	var dbEndpoint model.Endpoint
	require.NoError(t, testDB.Where("user_id = ? AND name = ?", agentID, endpointName).First(&dbEndpoint).Error)
	require.Equal(t, "offline", dbEndpoint.Status, "DisconnectAgent 后数据库 Endpoint.Status 应为 offline")
}
