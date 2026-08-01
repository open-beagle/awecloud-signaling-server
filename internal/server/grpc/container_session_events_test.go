package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

func TestContainerSessionEventsValidateContextAndRemainIdempotent(t *testing.T) {
	oldDB := db.DB
	t.Cleanup(func() { db.DB = oldDB })
	database, err := gorm.Open(sqlite.Open("file:container_session_events_test?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	db.DB = database
	require.NoError(t, database.AutoMigrate(
		&model.User{}, &model.Node{}, &model.Tenant{}, &model.TenantMembership{},
		&model.Group{}, &model.GroupMember{}, &model.Resource{}, &model.ResourceTarget{}, &model.AccessGrant{}, &model.ContainerSession{},
	))

	now := time.Now()
	tenant := model.Tenant{ID: uuid.NewString(), Key: "session-events", Name: "Session Events", Status: model.TenantStatusActive}
	user := model.User{Name: "alice", Role: model.UserRoleClient, SecretHash: "test", Enabled: true}
	agentUser := model.User{Name: "agent-session", Role: model.UserRoleAgent, SecretHash: "test", Enabled: true}
	require.NoError(t, database.Create(&tenant).Error)
	require.NoError(t, database.Create(&user).Error)
	require.NoError(t, database.Create(&agentUser).Error)
	agentNode := model.Node{UserID: agentUser.ID, Name: "agent-session", Type: model.NodeTypeAgent, ContainerSSHProtocol: "v1"}
	device := model.Node{UserID: user.ID, Name: "alice-desktop", Type: model.NodeTypeDesktop, HeadscaleNodeID: 7001}
	require.NoError(t, database.Create(&agentNode).Error)
	require.NoError(t, database.Create(&device).Error)
	require.NoError(t, database.Create(&model.TenantMembership{TenantID: tenant.ID, UserID: user.ID, Enabled: true}).Error)
	resource := model.Resource{
		ID: uuid.NewString(), TenantID: tenant.ID, Type: model.ResourceTypeContainerSSH,
		DisplayName: "IDE", ExternalWorkspaceID: "workspace-a", AgentNodeID: agentNode.ID,
		PodUID: "pod-a", ContainerName: "workspace", TargetRevision: 3,
		ContainerSSHPort: 50200, State: model.ResourceStateAvailable,
	}
	require.NoError(t, database.Create(&resource).Error)
	require.NoError(t, database.Create(&model.ResourceTarget{
		ResourceID: resource.ID, Revision: 3, AgentNodeID: agentNode.ID,
		Namespace: "dev", PodName: "ide-0", PodUID: "pod-a", ContainerName: "workspace", Ready: true, ObservedAt: now,
	}).Error)
	require.NoError(t, database.Create(&model.AccessGrant{
		ID: uuid.NewString(), TenantID: tenant.ID, ResourceID: resource.ID,
		SubjectType: "user", SubjectUserID: user.ID, Actions: `["shell"]`,
		ValidFrom: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), Revision: 4, Status: "enabled",
	}).Error)

	sessionID := uuid.NewString()
	started := &pb.ContainerSSHSessionEvent{
		EventId: uuid.NewString(), SessionId: sessionID, Phase: "started",
		UserId: user.ID, UserName: user.Name, DeviceNodeId: device.HeadscaleNodeID,
		ResourceId: resource.ID, TargetRevision: 3, GrantRevision: 4,
		PodUid: "pod-a", ContainerName: "workspace", OccurredAt: now.Unix(),
	}
	server := &AgentServiceServer{}
	require.Equal(t, []string{started.EventId}, server.handleContainerSessionEvents(context.Background(), agentNode.ID, []*pb.ContainerSSHSessionEvent{started}))
	require.Equal(t, []string{started.EventId}, server.handleContainerSessionEvents(context.Background(), agentNode.ID, []*pb.ContainerSSHSessionEvent{started}))
	var count int64
	require.NoError(t, database.Model(&model.ContainerSession{}).Where("id = ?", sessionID).Count(&count).Error)
	require.Equal(t, int64(1), count)

	spoofed := proto.Clone(started).(*pb.ContainerSSHSessionEvent)
	spoofed.EventId = uuid.NewString()
	spoofed.SessionId = uuid.NewString()
	spoofed.PodUid = "other-pod"
	require.Empty(t, server.handleContainerSessionEvents(context.Background(), agentNode.ID, []*pb.ContainerSSHSessionEvent{spoofed}))

	ended := &pb.ContainerSSHSessionEvent{EventId: uuid.NewString(), SessionId: sessionID, Phase: "ended", OccurredAt: now.Add(time.Minute).Unix(), Result: "success", CloseReason: "shell_exited"}
	require.Equal(t, []string{ended.EventId}, server.handleContainerSessionEvents(context.Background(), agentNode.ID, []*pb.ContainerSSHSessionEvent{ended}))
	require.Equal(t, []string{ended.EventId}, server.handleContainerSessionEvents(context.Background(), agentNode.ID, []*pb.ContainerSSHSessionEvent{ended}))
	var session model.ContainerSession
	require.NoError(t, database.First(&session, "id = ?", sessionID).Error)
	require.Equal(t, model.ContainerSessionEnded, session.Status)
	require.Equal(t, "shell_exited", session.CloseReason)
	require.Equal(t, device.ID, session.DeviceID)
	require.Equal(t, user.ID, session.ActorUserID)
	require.Equal(t, user.ID, session.EffectiveUserID)
	require.Empty(t, session.SimulationSessionID)

	group := model.Group{TenantID: tenant.ID, Name: "session-developers"}
	require.NoError(t, database.Create(&group).Error)
	groupMember := model.GroupMember{GroupID: group.ID, UserID: user.ID}
	require.NoError(t, database.Create(&groupMember).Error)
	require.NoError(t, database.Create(&model.AccessGrant{
		ID: uuid.NewString(), TenantID: tenant.ID, ResourceID: resource.ID,
		SubjectType: "group", SubjectGroupID: &group.ID, Actions: `["shell"]`,
		ValidFrom: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), Revision: 5, Status: "enabled",
	}).Error)
	groupStarted := proto.Clone(started).(*pb.ContainerSSHSessionEvent)
	groupStarted.EventId = uuid.NewString()
	groupStarted.SessionId = uuid.NewString()
	groupStarted.GrantRevision = 5
	require.Equal(t, []string{groupStarted.EventId}, server.handleContainerSessionEvents(context.Background(), agentNode.ID, []*pb.ContainerSSHSessionEvent{groupStarted}))

	require.NoError(t, database.Delete(&groupMember).Error)
	revokedGroupStart := proto.Clone(groupStarted).(*pb.ContainerSSHSessionEvent)
	revokedGroupStart.EventId = uuid.NewString()
	revokedGroupStart.SessionId = uuid.NewString()
	require.Empty(t, server.handleContainerSessionEvents(context.Background(), agentNode.ID, []*pb.ContainerSSHSessionEvent{revokedGroupStart}))

	revoked := model.ContainerSession{
		ID: uuid.NewString(), TenantID: tenant.ID, UserID: user.ID, DeviceID: device.ID,
		ResourceID: resource.ID, AgentNodeID: agentNode.ID, Status: model.ContainerSessionRevoked, StartedAt: now,
	}
	require.NoError(t, database.Create(&revoked).Error)
	revokedEnd := &pb.ContainerSSHSessionEvent{EventId: uuid.NewString(), SessionId: revoked.ID, Phase: "ended", OccurredAt: now.Add(time.Minute).Unix(), Result: "revoked", CloseReason: "admin_disconnect"}
	require.Equal(t, []string{revokedEnd.EventId}, server.handleContainerSessionEvents(context.Background(), agentNode.ID, []*pb.ContainerSSHSessionEvent{revokedEnd}))
	require.NoError(t, database.First(&revoked, "id = ?", revoked.ID).Error)
	require.NotNil(t, revoked.DisconnectAcknowledgedAt)
}
