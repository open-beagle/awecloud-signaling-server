package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	serverdb "github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func TestSessionAuthorizationBuildsTrustedSnapshotAndReliableTermination(t *testing.T) {
	fixture := newTenantManagementConstraintFixture(t)
	database := serverdb.DB
	require.NoError(t, database.Model(&model.Node{}).Where("id = ?", fixture.desktop.ID).Update("headscale_node_id", uint64(9901)).Error)
	session := fixture.session(uuid.NewString())
	require.NoError(t, database.Create(&session).Error)

	service := NewSessionAuthorizationService(database)
	service.now = func() time.Time { return fixture.now.Add(time.Second) }
	snapshot, err := service.BuildSnapshot(context.Background(), fixture.technical.ID, true)
	require.NoError(t, err)
	require.Len(t, snapshot.Permissions, 1)
	require.Empty(t, snapshot.Terminations)
	permission := snapshot.Permissions[0]
	require.Equal(t, session.ID, permission.SessionID)
	require.Equal(t, fixture.resource.ID, permission.ResourceID)
	require.Equal(t, fixture.source.ID, permission.SourceID)
	require.Equal(t, fixture.target.ID, permission.TargetRevisionID)
	require.Equal(t, fixture.member.Name, permission.UserName)
	require.Equal(t, uint64(9901), permission.DeviceHeadscaleNodeID)
	require.Equal(t, model.TenantResourceContainerService, permission.ResourceType)
	require.Equal(t, "trigger-service", permission.Target.ServiceUID)
	require.Equal(t, int32(443), permission.Target.PortNumber)
	require.Equal(t, "TCP", permission.Target.Protocol)
	require.Equal(t, fixture.now.Add(31*time.Second), permission.ValidUntil)
	var renewed model.ResourceSession
	require.NoError(t, database.First(&renewed, "id = ?", session.ID).Error)
	require.Equal(t, permission.ValidUntil, renewed.ValidUntil)
	require.Equal(t, int64(2), renewed.RowVersion)

	require.NoError(t, database.Model(&model.TenantAccessGrant{}).Where("id = ?", fixture.grant.ID).Updates(map[string]any{
		"status": model.TenantAccessGrantSuspended, "revision": int64(2), "row_version": int64(2),
	}).Error)
	snapshot, err = service.BuildSnapshot(context.Background(), fixture.technical.ID, true)
	require.NoError(t, err)
	require.Empty(t, snapshot.Permissions)
	require.Len(t, snapshot.Terminations, 1)
	require.Equal(t, session.ID, snapshot.Terminations[0].SessionID)
	require.Equal(t, sessionAuthorizationInvalidCode, snapshot.Terminations[0].ReasonCode)

	var stored model.ResourceSession
	require.NoError(t, database.First(&stored, "id = ?", session.ID).Error)
	require.Equal(t, model.ResourceSessionEnding, stored.Status)
	var termination model.ResourceSessionTermination
	require.NoError(t, database.First(&termination, "session_id = ?", session.ID).Error)
	require.Equal(t, model.ResourceSessionTerminationDelivered, termination.Status)
	require.NotNil(t, termination.DeliveredAt)
	var auditCount, outboxCount int64
	require.NoError(t, database.Model(&model.AuditLog{}).Where("target_id = ? AND action_type = ?", session.ID, "end_invalid_resource_session").Count(&auditCount).Error)
	require.NoError(t, database.Model(&model.OutboxEvent{}).Where("aggregate_id = ? AND event_type = ?", session.ID, "resource_session.ending").Count(&outboxCount).Error)
	require.Equal(t, int64(1), auditCount)
	require.Equal(t, int64(1), outboxCount)

	require.NoError(t, service.AcknowledgeTerminations(context.Background(), fixture.technical.ID, []SessionTerminationAckInput{{
		SessionID: session.ID, CommandRevision: termination.CommandRevision,
	}}))
	require.NoError(t, database.First(&termination, "id = ?", termination.ID).Error)
	require.Equal(t, model.ResourceSessionTerminationAcknowledged, termination.Status)
	require.NotNil(t, termination.AcknowledgedAt)
	require.NoError(t, database.First(&stored, "id = ?", session.ID).Error)
	require.NotNil(t, stored.DisconnectAcknowledgedAt)
}

func TestSessionAuthorizationRenewalKeepsOriginalMaximumSessionDeadline(t *testing.T) {
	fixture := newTenantManagementConstraintFixture(t)
	database := serverdb.DB
	require.NoError(t, database.Model(&model.Node{}).Where("id = ?", fixture.desktop.ID).Update("headscale_node_id", uint64(9903)).Error)
	require.NoError(t, database.Model(&model.TenantAccessGrant{}).Where("id = ?", fixture.grant.ID).Updates(map[string]any{
		"max_session_seconds": 10, "revision": int64(2), "row_version": int64(2),
	}).Error)
	session := fixture.session(uuid.NewString())
	session.GrantRevision = 2
	require.NoError(t, database.Create(&session).Error)

	service := NewSessionAuthorizationService(database)
	service.now = func() time.Time { return fixture.now.Add(5 * time.Second) }
	snapshot, err := service.BuildSnapshot(context.Background(), fixture.technical.ID, true)
	require.NoError(t, err)
	require.Len(t, snapshot.Permissions, 1)
	require.Equal(t, session.StartedAt.Add(10*time.Second), snapshot.Permissions[0].ValidUntil)
}

func TestSessionAuthorizationEventsAreIdempotentAndTerminalStateWins(t *testing.T) {
	fixture := newTenantManagementConstraintFixture(t)
	database := serverdb.DB
	require.NoError(t, database.Model(&model.Node{}).Where("id = ?", fixture.desktop.ID).Update("headscale_node_id", uint64(9902)).Error)
	session := fixture.session(uuid.NewString())
	require.NoError(t, database.Create(&session).Error)

	service := NewSessionAuthorizationService(database)
	service.now = func() time.Time { return fixture.now.Add(5 * time.Second) }
	acceptedID, connectedID := uuid.NewString(), uuid.NewString()
	acks, err := service.AcceptEvents(context.Background(), fixture.technical.ID, []SessionEventInput{
		{EventID: acceptedID, SessionID: session.ID, SourceSequence: 1, EventType: model.ResourceSessionEventAccepted, OccurredAt: fixture.now.Add(time.Second), ResultCode: "accepted"},
		{EventID: connectedID, SessionID: session.ID, SourceSequence: 2, EventType: model.ResourceSessionEventConnected, OccurredAt: fixture.now.Add(2 * time.Second), ResultCode: "connected"},
	})
	require.NoError(t, err)
	require.Len(t, acks, 2)
	require.False(t, acks[1].Replay)

	var stored model.ResourceSession
	require.NoError(t, database.First(&stored, "id = ?", session.ID).Error)
	require.Equal(t, model.ResourceSessionActive, stored.Status)
	require.NotNil(t, stored.ConnectedAt)

	replay, err := service.AcceptEvents(context.Background(), fixture.technical.ID, []SessionEventInput{{
		EventID: connectedID, SessionID: session.ID, SourceSequence: 2, EventType: model.ResourceSessionEventConnected,
		OccurredAt: fixture.now.Add(2 * time.Second), ResultCode: "connected",
	}})
	require.NoError(t, err)
	require.Len(t, replay, 1)
	require.True(t, replay[0].Replay)

	endedID := uuid.NewString()
	_, err = service.AcceptEvents(context.Background(), fixture.technical.ID, []SessionEventInput{{
		EventID: endedID, SessionID: session.ID, SourceSequence: 3, EventType: model.ResourceSessionEventEnded,
		OccurredAt: fixture.now.Add(3 * time.Second), ResultCode: "remote_closed",
	}})
	require.NoError(t, err)
	require.NoError(t, database.First(&stored, "id = ?", session.ID).Error)
	require.Equal(t, model.ResourceSessionEnded, stored.Status)

	_, err = service.AcceptEvents(context.Background(), fixture.technical.ID, []SessionEventInput{{
		EventID: uuid.NewString(), SessionID: session.ID, SourceSequence: 4, EventType: model.ResourceSessionEventConnected,
		OccurredAt: fixture.now.Add(4 * time.Second), ResultCode: "late_connected",
	}})
	require.NoError(t, err)
	require.NoError(t, database.First(&stored, "id = ?", session.ID).Error)
	require.Equal(t, model.ResourceSessionEnded, stored.Status)
	var eventCount int64
	require.NoError(t, database.Model(&model.ResourceSessionEvent{}).Where("session_id = ?", session.ID).Count(&eventCount).Error)
	require.Equal(t, int64(4), eventCount)
}

func TestSessionAuthorizationRejectsCrossSourceEventsAndSequenceConflicts(t *testing.T) {
	fixture := newTenantManagementConstraintFixture(t)
	database := serverdb.DB
	session := fixture.session(uuid.NewString())
	require.NoError(t, database.Create(&session).Error)
	service := NewSessionAuthorizationService(database)
	service.now = func() time.Time { return fixture.now.Add(5 * time.Second) }

	firstID := uuid.NewString()
	_, err := service.AcceptEvents(context.Background(), fixture.technical.ID, []SessionEventInput{{
		EventID: firstID, SessionID: session.ID, SourceSequence: 1, EventType: model.ResourceSessionEventAccepted,
		OccurredAt: fixture.now.Add(time.Second), ResultCode: "accepted",
	}})
	require.NoError(t, err)
	_, err = service.AcceptEvents(context.Background(), fixture.technical.ID, []SessionEventInput{{
		EventID: uuid.NewString(), SessionID: session.ID, SourceSequence: 1, EventType: model.ResourceSessionEventConnected,
		OccurredAt: fixture.now.Add(2 * time.Second), ResultCode: "connected",
	}})
	require.ErrorIs(t, err, ErrSessionAuthorizationEventConflict)
	_, err = service.AcceptEvents(context.Background(), uuid.NewString(), []SessionEventInput{{
		EventID: uuid.NewString(), SessionID: session.ID, SourceSequence: 2, EventType: model.ResourceSessionEventConnected,
		OccurredAt: fixture.now.Add(2 * time.Second), ResultCode: "connected",
	}})
	require.ErrorIs(t, err, ErrSessionAuthorizationSourceDenied)
}
