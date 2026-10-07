package grpc

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/service"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

func TestProcessSessionAuthorizationReport_MixedTunnelAndNormalEvents(t *testing.T) {
	dbName := fmt.Sprintf("file:mixed_events_%d?mode=memory&cache=shared", time.Now().UnixNano())
	database, err := gorm.Open(sqlite.Open(dbName), &gorm.Config{})
	require.NoError(t, err)

	require.NoError(t, database.AutoMigrate(
		&model.ResourceSession{},
		&model.ResourceSessionEvent{},
	))

	techID := uuid.NewString()
	now := time.Now().UTC()

	// 准备正常 Desktop 会话记录
	normalSessionID := uuid.NewString()
	normalSession := model.ResourceSession{
		ID:                        normalSessionID,
		TenantID:                  "tenant-test",
		TenantResourceID:          "res-normal",
		TenantResourceSourceID:    "src-normal",
		TargetRevisionID:          "target-normal",
		AllocationID:              "alloc-normal",
		AllocationItemID:          "item-normal",
		GrantID:                   "grant-normal",
		GrantRevision:             1,
		UserID:                    100,
		TenantMembershipID:        1,
		DeviceID:                  200,
		ActorUserID:               100,
		EffectiveUserID:           100,
		SessionType:               model.ResourceSessionContainerService,
		Action:                    "connect",
		AccessTechnicalResourceID: techID,
		AuthorizationRevision:     1,
		ValidUntil:                now.Add(time.Hour),
		Status:                    model.ResourceSessionAuthorizing,
		RequestID:                 "req-1",
		StartedAt:                 now.Add(-time.Minute),
		RowVersion:                1,
	}
	require.NoError(t, database.Create(&normalSession).Error)

	authService := service.NewSessionAuthorizationService(database)
	server := &AgentServiceServer{
		sessionAuthorization: authService,
	}

	tunnelEventID := uuid.NewString()
	tunnelSessionID := "tunnel-93-res-studio"
	normalEventID := uuid.NewString()

	tunnelEvent := &pb.ResourceSessionEventV2{
		EventId:        tunnelEventID,
		SessionId:      tunnelSessionID,
		EventType:      string(model.ResourceSessionEventAccepted),
		SourceSequence: 1,
		OccurredAt:     timestamppb.New(now.Add(-10 * time.Second)),
		ResultCode:     "CONNECT_OK",
	}

	normalEvent := &pb.ResourceSessionEventV2{
		EventId:        normalEventID,
		SessionId:      normalSessionID,
		EventType:      string(model.ResourceSessionEventAccepted),
		SourceSequence: 1,
		OccurredAt:     timestamppb.New(now.Add(-10 * time.Second)),
		ResultCode:     "accepted",
	}

	// 混合批次：1 条 tunnel + 1 条正常事件
	acks := server.processSessionAuthorizationReport(context.Background(), techID, 0, "", nil, []*pb.ResourceSessionEventV2{
		tunnelEvent,
		normalEvent,
	})

	// 验收断言：两条都有 ACK
	require.Len(t, acks, 2, "混合批次应为两条事件均生成 ACK")

	ackMap := make(map[string]*pb.ResourceSessionEventAckV2)
	for _, ack := range acks {
		ackMap[ack.EventId] = ack
	}

	require.Contains(t, ackMap, tunnelEventID)
	require.Equal(t, "SESSION_EVENT_ACCEPTED", ackMap[tunnelEventID].ResultCode)

	require.Contains(t, ackMap, normalEventID)
	require.Equal(t, "SESSION_EVENT_ACCEPTED", ackMap[normalEventID].ResultCode)

	// 验收断言：正常事件已入库
	var normalSaved model.ResourceSessionEvent
	require.NoError(t, database.Where("event_id = ?", normalEventID).First(&normalSaved).Error)
	require.Equal(t, normalSessionID, normalSaved.SessionID)

	// Tunnel 合成事件不写 resource_session_event
	var tunnelCount int64
	require.NoError(t, database.Model(&model.ResourceSessionEvent{}).Where("event_id = ?", tunnelEventID).Count(&tunnelCount).Error)
	require.Equal(t, int64(0), tunnelCount, "Tunnel 合成会话事件不应落盘到 resource_session_event")
}
