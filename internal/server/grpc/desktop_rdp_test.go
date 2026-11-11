package grpc

import (
	"context"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	v1 "github.com/juanfont/headscale/gen/go/headscale/v1"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/cache"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/headscale"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

type fakeHeartbeatServerStream struct {
	ctx      context.Context
	recvReqs []*pb.DesktopHeartbeatRequest
	recvIdx  int
	mu       sync.Mutex
	sent     []*pb.DesktopHeartbeatResponse
}

func (f *fakeHeartbeatServerStream) Send(resp *pb.DesktopHeartbeatResponse) error {
	f.mu.Lock()
	f.sent = append(f.sent, resp)
	f.mu.Unlock()
	return nil
}

func (f *fakeHeartbeatServerStream) Recv() (*pb.DesktopHeartbeatRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.recvIdx >= len(f.recvReqs) {
		return nil, io.EOF
	}
	req := f.recvReqs[f.recvIdx]
	f.recvIdx++
	return req, nil
}

func (f *fakeHeartbeatServerStream) SetHeader(metadata.MD) error  { return nil }
func (f *fakeHeartbeatServerStream) SendHeader(metadata.MD) error { return nil }
func (f *fakeHeartbeatServerStream) SetTrailer(metadata.MD)       {}
func (f *fakeHeartbeatServerStream) Context() context.Context     { return f.ctx }
func (f *fakeHeartbeatServerStream) SendMsg(any) error            { return nil }
func (f *fakeHeartbeatServerStream) RecvMsg(any) error            { return nil }

type fakeDataStreamServer struct {
	ctx      context.Context
	recvReqs []*pb.DesktopDataRequest
	recvIdx  int
	mu       sync.Mutex
	sent     []*pb.DesktopDataResponse
}

func (f *fakeDataStreamServer) Send(resp *pb.DesktopDataResponse) error {
	f.mu.Lock()
	f.sent = append(f.sent, resp)
	f.mu.Unlock()
	return nil
}

func (f *fakeDataStreamServer) Recv() (*pb.DesktopDataRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.recvIdx >= len(f.recvReqs) {
		return nil, io.EOF
	}
	req := f.recvReqs[f.recvIdx]
	f.recvIdx++
	return req, nil
}

func (f *fakeDataStreamServer) SetHeader(metadata.MD) error  { return nil }
func (f *fakeDataStreamServer) SendHeader(metadata.MD) error { return nil }
func (f *fakeDataStreamServer) SetTrailer(metadata.MD)       {}
func (f *fakeDataStreamServer) Context() context.Context     { return f.ctx }
func (f *fakeDataStreamServer) SendMsg(any) error            { return nil }
func (f *fakeDataStreamServer) RecvMsg(any) error            { return nil }

func setupDesktopRDPTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	original := db.DB
	t.Cleanup(func() { db.DB = original })

	dsn := fmt.Sprintf("file:desktop-rdp-%d?mode=memory&cache=shared", time.Now().UnixNano())
	database, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(
		&model.User{},
		&model.Node{},
		&model.Tenant{},
		&model.TenantMembership{},
		&model.Group{},
		&model.GroupMember{},
		&model.ProxyService{},
		&model.ServiceFavorite{},
		&model.AclSSHUserPermission{},
		&model.AclSSHGroupPermission{},
		&model.AclDesktopRDPPermission{},
		&model.DesktopRDPSessionAudit{},
		&model.TechnicalResource{},
		&model.TechnicalResourceBinding{},
		&model.SupplyCandidate{},
	))
	db.DB = database
	return database
}

func TestDesktopStreamsAndGetMyDevicesRequireValidSecretAndPreventUnauthEviction(t *testing.T) {
	database := setupDesktopRDPTestDB(t)

	user := model.User{Name: "alice", Role: model.UserRoleClient, SecretHash: "x", Enabled: true}
	require.NoError(t, database.Create(&user).Error)
	hash, err := bcrypt.GenerateFromPassword([]byte("valid-secret"), bcrypt.MinCost)
	require.NoError(t, err)
	node := model.Node{
		UserID:     user.ID,
		Name:       "home-win",
		Type:       model.NodeTypeDesktop,
		SecretHash: string(hash),
		RDPMode:    model.DesktopRDPModeAuto,
	}
	require.NoError(t, database.Create(&node).Error)

	cfg := &config.ServerConfig{}
	server := NewDesktopServiceServer(cfg)

	// 1. 默认严格鉴权：空 secret 与错误 secret 均返回 Unauthenticated
	for _, badSecret := range []string{"", "wrong-secret"} {
		_, err := server.GetMyDevices(context.Background(), &pb.GetMyDevicesRequest{
			DesktopId: node.ID,
			Secret:    badSecret,
		})
		require.Equal(t, codes.Unauthenticated, status.Code(err))

		hbErr := server.Heartbeat(&fakeHeartbeatServerStream{
			ctx:      context.Background(),
			recvReqs: []*pb.DesktopHeartbeatRequest{{DesktopId: node.ID, Secret: badSecret}},
		})
		require.Equal(t, codes.Unauthenticated, status.Code(hbErr))

		dsErr := server.DataStream(&fakeDataStreamServer{
			ctx:      context.Background(),
			recvReqs: []*pb.DesktopDataRequest{{DesktopId: node.ID, Secret: badSecret}},
		})
		require.Equal(t, codes.Unauthenticated, status.Code(dsErr))
	}

	// 2. 合法 secret 正常通过
	devResp, err := server.GetMyDevices(context.Background(), &pb.GetMyDevicesRequest{
		DesktopId: node.ID,
		Secret:    "valid-secret",
	})
	require.NoError(t, err)
	require.Len(t, devResp.Devices, 1)
	require.Equal(t, model.DesktopRDPModeAuto, devResp.Devices[0].RdpMode)

	// 3. 开启旧客户端灰度兼容开关时：非空错误凭证仍拒绝，且未认证连接不得顶替已认证活跃连接
	cfg.Security.AllowLegacyUnauthenticatedDesktopStream = true
	_, err = server.GetMyDevices(context.Background(), &pb.GetMyDevicesRequest{
		DesktopId: node.ID,
		Secret:    "wrong-secret",
	})
	require.Equal(t, codes.Unauthenticated, status.Code(err))

	legacyResp, err := server.GetMyDevices(context.Background(), &pb.GetMyDevicesRequest{
		DesktopId: node.ID,
		Secret:    "",
	})
	require.NoError(t, err)
	require.Len(t, legacyResp.Devices, 1)
	require.Empty(t, legacyResp.Devices[0].RdpMode) // 未认证兼容路径隐藏 RDP 元数据

	// 模拟当前已有活跃的已认证连接与数据流，验证空凭证流无法顶替
	_, cancelAuth := context.WithCancel(context.Background())
	defer cancelAuth()
	server.connections[node.ID] = &DesktopConnection{
		NodeID:        node.ID,
		UserID:        user.ID,
		Authenticated: true,
		LastSeen:      time.Now(),
		Cancel:        cancelAuth,
	}
	server.dataStreams[node.ID] = &DesktopDataStream{
		NodeID:        node.ID,
		UserID:        user.ID,
		Authenticated: true,
		Cancel:        cancelAuth,
	}

	evictHbErr := server.Heartbeat(&fakeHeartbeatServerStream{
		ctx:      context.Background(),
		recvReqs: []*pb.DesktopHeartbeatRequest{{DesktopId: node.ID, Secret: ""}},
	})
	require.Equal(t, codes.Unauthenticated, status.Code(evictHbErr))

	evictDsErr := server.DataStream(&fakeDataStreamServer{
		ctx:      context.Background(),
		recvReqs: []*pb.DesktopDataRequest{{DesktopId: node.ID, Secret: ""}},
	})
	require.Equal(t, codes.Unauthenticated, status.Code(evictDsErr))
}

func TestDesktopHeartbeatCrossChecksHeadscaleSnapshotAndDisconnectPreventsStaleFlush(t *testing.T) {
	database := setupDesktopRDPTestDB(t)

	user := model.User{Name: "mengsk", Role: model.UserRoleClient, SecretHash: "x", Enabled: true}
	require.NoError(t, database.Create(&user).Error)
	hash, err := bcrypt.GenerateFromPassword([]byte("home-secret"), bcrypt.MinCost)
	require.NoError(t, err)
	node := model.Node{
		UserID:     user.ID,
		Name:       "home-pc",
		Type:       model.NodeTypeDesktop,
		SecretHash: string(hash),
	}
	require.NoError(t, database.Create(&node).Error)

	store := cache.NewNodeRuntimeStore()
	store.UpsertNode(&node)
	persister := cache.NewNodeRuntimePersister(store, database)

	refresher := headscale.NewSnapshotRefresher(nil)
	now := time.Now()
	refresher.StoreSnapshot(headscale.NewHeadscaleNodeSnapshot(1, now, []*v1.Node{
		{
			Id:          101,
			GivenName:   "home-pc",
			IpAddresses: []string{"100.64.0.10"},
			Online:      true,
			User:        &v1.User{Name: "client-mengsk"},
		},
		{
			Id:          202,
			GivenName:   "victim-pc",
			IpAddresses: []string{"100.64.0.99"},
			Online:      true,
			User:        &v1.User{Name: "client-victim"},
		},
	}))

	server := NewDesktopServiceServer(&config.ServerConfig{})
	server.SetRuntimeStore(store)
	server.SetRuntimePersister(persister)
	server.SetSnapshotRefresher(refresher)

	// 伪造上报他人 IP 100.64.0.99，应被 Headscale 快照拦截并回退为真实同名节点 IP 100.64.0.10，同时绑定 HeadscaleNodeID=101
	server.handleDesktopHeartbeat(context.Background(), node.ID, user, true, &pb.DesktopHeartbeatRequest{
		DesktopId:        node.ID,
		Secret:           "home-secret",
		TunnelIp:         "100.64.0.99",
		TunnelConnected:  true,
		RdpLocalReady:    true,
		RdpLocalPort:     3389,
		RdpTunnelExposed: true,
	})

	rn, ok := store.GetNode(node.ID)
	require.True(t, ok)
	require.Equal(t, "100.64.0.10", rn.IP)
	require.Equal(t, uint64(101), rn.HeadscaleNodeID)
	require.True(t, rn.RDPLocalReady)
	require.True(t, rn.RDPTunnelExposed)

	// 先刷盘一次，确认在线时 HeadscaleNodeID 和真实 IP 落库
	persister.Flush(context.Background())
	var dbOnline model.Node
	require.NoError(t, database.First(&dbOnline, node.ID).Error)
	require.Equal(t, "100.64.0.10", dbOnline.IP)
	require.Equal(t, uint64(101), dbOnline.HeadscaleNodeID)

	// 再模拟一次心跳使 RuntimeStore Dirty=true，然后完整走一次 Heartbeat 断连清理，确认 Persister.Flush 不会把旧心跳/IP 写回 DB
	hbStream := &fakeHeartbeatServerStream{
		ctx: context.Background(),
		recvReqs: []*pb.DesktopHeartbeatRequest{
			{
				DesktopId:        node.ID,
				Secret:           "home-secret",
				TunnelIp:         "100.64.0.10",
				TunnelConnected:  true,
				RdpLocalReady:    true,
				RdpLocalPort:     3389,
				RdpTunnelExposed: true,
			},
		},
	}
	require.NoError(t, server.Heartbeat(hbStream))

	persister.Flush(context.Background())

	var dbOffline model.Node
	require.NoError(t, database.First(&dbOffline, node.ID).Error)
	require.Empty(t, dbOffline.IP)
	require.Nil(t, dbOffline.LastHeartbeat)
}

func TestBuildInboundRDPPolicyExcludesCloudIDEAndHonorsShareConsent(t *testing.T) {
	database := setupDesktopRDPTestDB(t)

	owner := model.User{Name: "mengsk", Role: model.UserRoleClient, SecretHash: "x", Enabled: true}
	colleague := model.User{Name: "bob", Role: model.UserRoleClient, SecretHash: "y", Enabled: true}
	require.NoError(t, database.Create(&owner).Error)
	require.NoError(t, database.Create(&colleague).Error)

	tenant := model.Tenant{ID: "tenant-1", Key: "rd", Name: "R&D", Status: model.TenantStatusActive}
	require.NoError(t, database.Create(&tenant).Error)
	require.NoError(t, database.Create(&model.TenantMembership{TenantID: tenant.ID, UserID: owner.ID, Role: "admin", Enabled: true}).Error)
	require.NoError(t, database.Create(&model.TenantMembership{TenantID: tenant.ID, UserID: colleague.ID, Role: "member", Enabled: true}).Error)

	hash, err := bcrypt.GenerateFromPassword([]byte("home-secret"), bcrypt.MinCost)
	require.NoError(t, err)

	homeWin := model.Node{
		UserID:     owner.ID,
		Name:       "home-win",
		Type:       model.NodeTypeDesktop,
		SecretHash: string(hash),
		RDPMode:    model.DesktopRDPModeAuto,
	}
	laptopMac := model.Node{
		UserID:          owner.ID,
		Name:            "travel-mac",
		Type:            model.NodeTypeDesktop,
		HeadscaleNodeID: 502,
		SecretHash:      "mac",
	}
	cloudIDE := model.Node{
		UserID:          owner.ID,
		Name:            "cloudide-pod",
		Type:            model.NodeTypeAgent, // 同用户名下的非 desktop 节点必须被排除
		HeadscaleNodeID: 503,
		SecretHash:      "ide",
	}
	bobDesktop := model.Node{
		UserID:          colleague.ID,
		Name:            "bob-laptop",
		Type:            model.NodeTypeDesktop,
		HeadscaleNodeID: 601,
		SecretHash:      "bob",
	}
	require.NoError(t, database.Create(&homeWin).Error)
	require.NoError(t, database.Create(&laptopMac).Error)
	require.NoError(t, database.Create(&cloudIDE).Error)
	require.NoError(t, database.Create(&bobDesktop).Error)

	store := cache.NewNodeRuntimeStore()
	store.UpsertNode(&homeWin)
	store.UpsertNode(&laptopMac)
	store.UpsertNode(&cloudIDE)
	store.UpsertNode(&bobDesktop)

	refresher := headscale.NewSnapshotRefresher(nil)
	now := time.Now()
	refresher.StoreSnapshot(headscale.NewHeadscaleNodeSnapshot(1, now, []*v1.Node{
		{Id: 501, GivenName: "home-win", IpAddresses: []string{"100.64.0.1"}, Online: true, User: &v1.User{Name: "client-mengsk"}},
		{Id: 502, GivenName: "travel-mac", IpAddresses: []string{"100.64.0.2"}, Online: true, User: &v1.User{Name: "client-mengsk"}},
		{Id: 503, GivenName: "cloudide-pod", IpAddresses: []string{"100.64.0.3"}, Online: true, User: &v1.User{Name: "client-mengsk"}},
		{Id: 504, GivenName: "ephemeral-ide", IpAddresses: []string{"100.64.0.4"}, Online: true, User: &v1.User{Name: "client-mengsk"}},
		{Id: 601, GivenName: "bob-laptop", IpAddresses: []string{"100.64.0.20"}, Online: true, User: &v1.User{Name: "client-bob"}},
	}))

	server := NewDesktopServiceServer(&config.ServerConfig{})
	server.SetRuntimeStore(store)
	server.SetSnapshotRefresher(refresher)

	// auto 模式下本地 3389 未就绪时：EffectiveEnabled 应为 false
	p0 := server.buildInboundRDPPolicy(context.Background(), owner.ID, homeWin.ID)
	require.False(t, p0.EffectiveEnabled)

	// 标记 home-win 在线且本地 3389 已监听
	_, _ = store.UpdateHeartbeat(homeWin.ID, "100.64.0.1", "home-win", "v1.0.3", "", nil, "", "{}", "v1", "v1", now)
	_, _ = store.UpdateDesktopRDPStatus(homeWin.ID, true, 3389, false)

	p1 := server.buildInboundRDPPolicy(context.Background(), owner.ID, homeWin.ID)
	require.True(t, p1.EffectiveEnabled)
	require.Len(t, p1.AllowedPeers, 1)
	require.Equal(t, uint64(502), p1.AllowedPeers[0].HeadscaleNodeId)
	require.Equal(t, "client-mengsk", p1.AllowedPeers[0].HeadscaleUserName)

	// 创建一条给 bob 的授权，但归属人尚未开启 RDPAllowShare -> 仍不得放行 bob
	grant := model.AclDesktopRDPPermission{
		TenantID:        tenant.ID,
		TargetNodeID:    homeWin.ID,
		OwnerUserID:     owner.ID,
		GranteeUserID:   colleague.ID,
		CreatedByUserID: owner.ID,
		Reason:          "remote support",
		ExpiresAt:       now.Add(time.Hour),
		Enabled:         true,
	}
	require.NoError(t, database.Create(&grant).Error)

	p2 := server.buildInboundRDPPolicy(context.Background(), owner.ID, homeWin.ID)
	require.Len(t, p2.AllowedPeers, 1)

	// 归属人通过 UpdateDeviceRDPConfig 开启共享许可 -> bob 的 Desktop 节点进入白名单
	updResp, err := server.UpdateDeviceRDPConfig(context.Background(), &pb.UpdateDeviceRDPConfigRequest{
		DesktopId:        homeWin.ID,
		Secret:           "home-secret",
		TargetNodeId:     homeWin.ID,
		UpdateAllowShare: true,
		RdpAllowShare:    true,
	})
	require.NoError(t, err)
	require.True(t, updResp.Success)

	p3 := server.buildInboundRDPPolicy(context.Background(), owner.ID, homeWin.ID)
	require.Len(t, p3.AllowedPeers, 2)

	// 归属人再次关闭共享许可 -> 自动撤销跨账号授权记录并立即从白名单剔除
	updOff, err := server.UpdateDeviceRDPConfig(context.Background(), &pb.UpdateDeviceRDPConfigRequest{
		DesktopId:        homeWin.ID,
		Secret:           "home-secret",
		TargetNodeId:     homeWin.ID,
		UpdateAllowShare: true,
		RdpAllowShare:    false,
	})
	require.NoError(t, err)
	require.True(t, updOff.Success)

	p4 := server.buildInboundRDPPolicy(context.Background(), owner.ID, homeWin.ID)
	require.Len(t, p4.AllowedPeers, 1)

	var revokedGrant model.AclDesktopRDPPermission
	require.NoError(t, database.First(&revokedGrant, grant.ID).Error)
	require.False(t, revokedGrant.Enabled)
	require.NotNil(t, revokedGrant.RevokedAt)

	// 验证会话审计写入
	auditResp, err := server.ReportRDPSessionAudit(context.Background(), &pb.ReportRDPSessionAuditRequest{
		DesktopId:             homeWin.ID,
		Secret:                "home-secret",
		SessionUuid:           "sess-001",
		EventType:             model.RDPSessionEventStarted,
		SourceHeadscaleNodeId: 502,
		SourceNodeId:          laptopMac.ID,
		SourceUserName:        "mengsk",
		SourceDeviceName:      "travel-mac",
		SourceIp:              "100.64.0.2",
		TimestampUnix:         now.Unix(),
	})
	require.NoError(t, err)
	require.True(t, auditResp.Success)

	var auditCount int64
	require.NoError(t, database.Model(&model.DesktopRDPSessionAudit{}).Where("session_uuid = ?", "sess-001").Count(&auditCount).Error)
	require.Equal(t, int64(1), auditCount)
}
