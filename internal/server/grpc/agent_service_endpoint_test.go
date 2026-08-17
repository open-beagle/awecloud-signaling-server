package grpc

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

func setupEndpointTestDB(t *testing.T) (*gorm.DB, uint64, uint64, string) {
	testDB, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:endpoint_test_%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{})
	require.NoError(t, err)

	require.NoError(t, testDB.AutoMigrate(
		&model.User{},
		&model.Node{},
		&model.Endpoint{},
		&model.TechnicalResource{},
		&model.TechnicalResourceBinding{},
		&model.SupplyCandidate{},
		&model.PlatformResource{},
		&model.PlatformResourceSource{},
	))

	agentUserID := uint64(100)
	agentNodeID := uint64(200)
	providerID := "provider-beagle-test"
	parentTechID := uuid.New().String()

	// 创建用户与节点
	require.NoError(t, testDB.Create(&model.User{
		ID:   agentUserID,
		Name: "agent-test-user",
	}).Error)

	require.NoError(t, testDB.Create(&model.Node{
		ID:     agentNodeID,
		UserID: agentUserID,
		Name:   "agent-node-242",
		Type:   model.NodeTypeAgent,
	}).Error)

	// 创建父 Agent 的 TechnicalResource 与 Binding
	require.NoError(t, testDB.Create(&model.TechnicalResource{
		ID:                 parentTechID,
		ProviderID:         providerID,
		Type:               model.TechnicalResourceAgent,
		StableKey:          "legacy-node:200",
		DomainLabel:        "agent-test-242",
		LifecycleState:     model.TechnicalResourceRegistered,
		HealthState:        model.ResourceHealthOnline,
		CredentialRevision: 1,
		ConfigRevision:     1,
		RowVersion:         1,
	}).Error)

	require.NoError(t, testDB.Create(&model.TechnicalResourceBinding{
		ID:                  uuid.New().String(),
		TechnicalResourceID: parentTechID,
		SourceType:          model.TechnicalResourceBindingLegacyNode,
		SourceID:            "200",
		CredentialRevision:  1,
		Enabled:             true,
		BoundByUserID:       1,
		RowVersion:          1,
	}).Error)

	return testDB, agentUserID, agentNodeID, parentTechID
}

func TestHandleConnectedEndpoints_AutoCreatesTechnicalResource(t *testing.T) {
	oldDB := db.DB
	t.Cleanup(func() { db.DB = oldDB })

	testDB, agentUserID, agentNodeID, parentTechID := setupEndpointTestDB(t)
	db.DB = testDB

	server := &AgentServiceServer{}

	endpoints := []*pb.ConnectedEndpoint{
		{
			Name:            "beagle-232",
			Version:         "v1.0.2",
			CommitId:        "abc1234",
			BinarySha256:    "sha256-test",
			UpdaterProtocol: "v1",
			Os:              "linux",
			Arch:            "arm64",
			SshUsers:        []string{"root", "admin"},
		},
	}

	// 1. 执行首次上报
	server.handleConnectedEndpoints(context.Background(), agentUserID, agentNodeID, endpoints)

	// 验证 Endpoint 表记录
	var ep model.Endpoint
	require.NoError(t, testDB.Where("user_id = ? AND name = ?", agentUserID, "beagle-232").First(&ep).Error)
	require.Equal(t, "online", ep.Status)
	require.Equal(t, "v1.0.2", ep.Version)

	// 验证 TechnicalResource 自动创建与关联
	var techRes model.TechnicalResource
	require.NoError(t, testDB.Where("parent_id = ? AND type = ?", parentTechID, model.TechnicalResourceEndpoint).First(&techRes).Error)
	require.Equal(t, model.ResourceHealthOnline, techRes.HealthState)
	require.Equal(t, model.TechnicalResourceRegistered, techRes.LifecycleState)
	require.Equal(t, "legacy-endpoint:"+ep.ID, techRes.StableKey)

	// 验证 TechnicalResourceBinding
	var binding model.TechnicalResourceBinding
	require.NoError(t, testDB.Where("technical_resource_id = ? AND source_type = ?", techRes.ID, model.TechnicalResourceBindingLegacyEndpoint).First(&binding).Error)
	require.Equal(t, ep.ID, binding.SourceID)
	require.True(t, binding.Enabled)

	// 验证 SupplyCandidate
	var candidate model.SupplyCandidate
	require.NoError(t, testDB.Where("technical_resource_id = ? AND resource_type = ?", techRes.ID, model.SupplyResourceHost).First(&candidate).Error)
	require.Equal(t, model.SupplyCandidateLinked, candidate.ReviewState)

	// 验证 PlatformResource
	var platformRes model.PlatformResource
	require.NoError(t, testDB.Where("stable_key = ? AND type = ?", "legacy-host-legacy_endpoint:"+ep.ID, model.SupplyResourceHost).First(&platformRes).Error)
	require.Equal(t, "beagle-232", platformRes.DisplayName)
	require.Equal(t, model.ResourceHealthOnline, platformRes.HealthState)

	// 2. 验证心跳幂等性（再次上报不重复创建）
	server.handleConnectedEndpoints(context.Background(), agentUserID, agentNodeID, endpoints)

	var count int64
	require.NoError(t, testDB.Model(&model.TechnicalResource{}).Where("parent_id = ?", parentTechID).Count(&count).Error)
	require.Equal(t, int64(1), count)

	var bindCount int64
	require.NoError(t, testDB.Model(&model.TechnicalResourceBinding{}).Where("source_type = ?", model.TechnicalResourceBindingLegacyEndpoint).Count(&bindCount).Error)
	require.Equal(t, int64(1), bindCount)
}

func TestHandleConnectedEndpoints_OfflineAndOnlineTransition(t *testing.T) {
	oldDB := db.DB
	t.Cleanup(func() { db.DB = oldDB })

	testDB, agentUserID, agentNodeID, parentTechID := setupEndpointTestDB(t)
	db.DB = testDB

	server := &AgentServiceServer{}

	endpoints := []*pb.ConnectedEndpoint{
		{
			Name: "beagle-232",
			Os:   "linux",
			Arch: "arm64",
		},
	}

	// 1. 上报上线
	server.handleConnectedEndpoints(context.Background(), agentUserID, agentNodeID, endpoints)

	var ep model.Endpoint
	require.NoError(t, testDB.Where("user_id = ? AND name = ?", agentUserID, "beagle-232").First(&ep).Error)
	require.Equal(t, "online", ep.Status)

	var techRes model.TechnicalResource
	require.NoError(t, testDB.Where("parent_id = ?", parentTechID).First(&techRes).Error)
	require.Equal(t, model.ResourceHealthOnline, techRes.HealthState)

	var platformRes model.PlatformResource
	require.NoError(t, testDB.Where("stable_key = ?", "legacy-host-legacy_endpoint:"+ep.ID).First(&platformRes).Error)
	require.Equal(t, model.ResourceHealthOnline, platformRes.HealthState)

	// 2. 空列表上报（模拟掉线 / 离线）
	server.handleConnectedEndpoints(context.Background(), agentUserID, agentNodeID, []*pb.ConnectedEndpoint{})

	require.NoError(t, testDB.Where("id = ?", ep.ID).First(&ep).Error)
	require.Equal(t, "offline", ep.Status)

	require.NoError(t, testDB.Where("id = ?", techRes.ID).First(&techRes).Error)
	require.Equal(t, model.ResourceHealthOffline, techRes.HealthState)

	require.NoError(t, testDB.Where("id = ?", platformRes.ID).First(&platformRes).Error)
	require.Equal(t, model.ResourceHealthOffline, platformRes.HealthState)

	// 3. 再次上报上线（模拟重新连接）
	server.handleConnectedEndpoints(context.Background(), agentUserID, agentNodeID, endpoints)

	require.NoError(t, testDB.Where("id = ?", ep.ID).First(&ep).Error)
	require.Equal(t, "online", ep.Status)

	require.NoError(t, testDB.Where("id = ?", techRes.ID).First(&techRes).Error)
	require.Equal(t, model.ResourceHealthOnline, techRes.HealthState)

	require.NoError(t, testDB.Where("id = ?", platformRes.ID).First(&platformRes).Error)
	require.Equal(t, model.ResourceHealthOnline, platformRes.HealthState)
}
