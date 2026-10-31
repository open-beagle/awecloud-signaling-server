package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	serverdb "github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func TestSessionAuthorizationDistinguishesRevokedSourceFromUnavailableDatabase(t *testing.T) {
	fixture := newTenantManagementConstraintFixture(t)
	database := serverdb.DB
	var binding model.TechnicalResourceBinding
	require.NoError(t, database.Where("technical_resource_id = ?", fixture.technical.ID).First(&binding).Error)
	authorizer := NewSessionAuthorizationService(database)
	resolved, err := authorizer.ResolveTechnicalResource(context.Background(), binding.SourceType, binding.SourceID)
	require.NoError(t, err)
	require.Equal(t, fixture.technical.ID, resolved.ID)
	require.NoError(t, database.Model(&binding).Updates(map[string]any{"enabled": false, "row_version": gorm.Expr("row_version + 1")}).Error)
	resolved, err = authorizer.ResolveTechnicalResource(context.Background(), binding.SourceType, binding.SourceID)
	require.ErrorIs(t, err, ErrSessionAuthorizationSourceDenied)
	require.Equal(t, fixture.technical.ID, resolved.ID, "revoked sources need an empty snapshot with the original identity")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = authorizer.ResolveTechnicalResource(ctx, binding.SourceType, binding.SourceID)
	require.ErrorIs(t, err, context.Canceled)
	require.NotErrorIs(t, err, ErrSessionAuthorizationSourceDenied)
	_, err = authorizer.ResolveTechnicalResource(context.Background(), binding.SourceType, fmt.Sprint(99999999))
	require.ErrorIs(t, err, ErrSessionAuthorizationSourceDenied)
}

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
	require.Equal(t, session.StartedAt.Add(time.Hour), permission.ValidUntil)
	var renewed model.ResourceSession
	require.NoError(t, database.First(&renewed, "id = ?", session.ID).Error)
	require.Equal(t, session.StartedAt.Add(time.Hour), renewed.ValidUntil)
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

func TestSessionAuthorizationBuildSnapshotStopsOnCanceledContext(t *testing.T) {
	fixture := newTenantManagementConstraintFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	snapshot, err := NewSessionAuthorizationService(serverdb.DB).BuildSnapshot(ctx, fixture.technical.ID, true)
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, snapshot.Permissions)
	require.Empty(t, snapshot.Terminations)
}

func TestSessionAuthorizationRevalidatesOnlyRequestedSessions(t *testing.T) {
	fixture := newTenantManagementConstraintFixture(t)
	database := serverdb.DB
	require.NoError(t, database.Model(&model.Node{}).Where("id = ?", fixture.desktop.ID).Update("headscale_node_id", uint64(9904)).Error)
	selected := fixture.session(uuid.NewString())
	unrelated := fixture.session(uuid.NewString())
	require.NoError(t, database.Create(&selected).Error)
	require.NoError(t, database.Create(&unrelated).Error)

	authorizer := NewSessionAuthorizationService(database)
	authorizer.now = func() time.Time { return fixture.now.Add(time.Second) }
	results, err := authorizer.RevalidateSessions(context.Background(), []model.ResourceSession{selected})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, selected.ID, results[0].SessionID)
	require.NotNil(t, results[0].Permission)
	require.Equal(t, selected.ID, results[0].Permission.SessionID)

	var storedUnrelated model.ResourceSession
	require.NoError(t, database.First(&storedUnrelated, "id = ?", unrelated.ID).Error)
	require.Equal(t, int64(1), storedUnrelated.RowVersion)
	require.Equal(t, unrelated.ValidUntil.UTC(), storedUnrelated.ValidUntil.UTC())
}

func TestSessionAuthorizationLimitsDueTerminationBatch(t *testing.T) {
	fixture := newTenantManagementConstraintFixture(t)
	database := serverdb.DB
	service := NewSessionAuthorizationService(database)
	service.now = func() time.Time { return fixture.now.Add(time.Second) }
	for i := 0; i < sessionAuthorizationReportMaxItems+5; i++ {
		session := fixture.session(uuid.NewString())
		session.Status = model.ResourceSessionEnding
		require.NoError(t, database.Create(&session).Error)
		require.NoError(t, database.Create(&model.ResourceSessionTermination{
			ID: uuid.NewString(), SessionID: session.ID, CommandRevision: 1,
			ReasonCode: sessionAuthorizationExpiredCode, Reason: "expired", Status: model.ResourceSessionTerminationPending,
		}).Error)
	}

	snapshot, err := service.BuildSnapshot(context.Background(), fixture.technical.ID, true)
	require.NoError(t, err)
	require.Len(t, snapshot.Terminations, sessionAuthorizationReportMaxItems)
}

func TestSessionAuthorizationSkipsTerminationUntilRetryIsDue(t *testing.T) {
	fixture := newTenantManagementConstraintFixture(t)
	database := serverdb.DB
	session := fixture.session(uuid.NewString())
	session.Status = model.ResourceSessionEnding
	require.NoError(t, database.Create(&session).Error)
	nextAttempt := fixture.now.Add(time.Minute)
	require.NoError(t, database.Create(&model.ResourceSessionTermination{
		ID: uuid.NewString(), SessionID: session.ID, CommandRevision: 1,
		ReasonCode: sessionAuthorizationExpiredCode, Reason: "expired", Status: model.ResourceSessionTerminationDelivered,
		DeliveredAt: &fixture.now, NextAttemptAt: &nextAttempt,
	}).Error)

	service := NewSessionAuthorizationService(database)
	service.now = func() time.Time { return fixture.now.Add(time.Second) }
	snapshot, err := service.BuildSnapshot(context.Background(), fixture.technical.ID, true)
	require.NoError(t, err)
	require.Empty(t, snapshot.Terminations)
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

func TestSessionAuthorizationPermissionHonorsRealChainDeadlines(t *testing.T) {
	for _, boundary := range []string{"grant", "membership", "allocation", "namespace"} {
		t.Run(boundary, func(t *testing.T) {
			fixture := newTenantManagementConstraintFixture(t)
			database := serverdb.DB
			require.NoError(t, database.Model(&model.Node{}).Where("id = ?", fixture.desktop.ID).Update("headscale_node_id", uint64(9905)).Error)
			deadline := fixture.now.Add(10 * time.Minute)
			switch boundary {
			case "grant":
				require.NoError(t, database.Model(&model.TenantAccessGrant{}).Where("id = ?", fixture.grant.ID).Updates(map[string]any{
					"expires_at": deadline, "revision": gorm.Expr("revision + 1"), "row_version": gorm.Expr("row_version + 1"),
				}).Error)
			case "membership":
				require.NoError(t, database.Model(&model.TenantMembership{}).Where("id = ?", fixture.membership.ID).Update("expires_at", deadline).Error)
			case "allocation":
				// Active allocations are immutable. Extend the other limits so the
				// fixture's original one-hour allocation is the earliest deadline.
				deadline = fixture.now.Add(time.Hour)
				require.NoError(t, database.Model(&model.TenantAccessGrant{}).Where("id = ?", fixture.grant.ID).Updates(map[string]any{
					"max_session_seconds": 7200, "revision": gorm.Expr("revision + 1"), "row_version": gorm.Expr("row_version + 1"),
				}).Error)
				require.NoError(t, database.Model(&model.NamespaceObservation{}).Where("namespace_uid = ?", "tenant-trigger-namespace-uid").Update("lease_expires_at", fixture.now.Add(2*time.Hour)).Error)
			case "namespace":
				require.NoError(t, database.Model(&model.NamespaceObservation{}).Where("namespace_uid = ?", "tenant-trigger-namespace-uid").Update("lease_expires_at", deadline).Error)
			}
			session := fixture.session(uuid.NewString())
			if boundary == "grant" || boundary == "allocation" {
				session.GrantRevision++
			}
			require.NoError(t, database.Create(&session).Error)
			authorizer := NewSessionAuthorizationService(database)
			for _, elapsed := range []time.Duration{time.Second, time.Minute} {
				authorizer.now = func() time.Time { return fixture.now.Add(elapsed) }
				snapshot, err := authorizer.BuildSnapshot(context.Background(), fixture.technical.ID, true)
				require.NoError(t, err)
				require.Len(t, snapshot.Permissions, 1)
				require.Equal(t, deadline, snapshot.Permissions[0].ValidUntil)
			}
		})
	}
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

// Every read in the authorization chain must preserve the last trusted view on
// operational failure, without committing a termination or partial renewal.
func TestSessionAuthorizationDatabaseFaultDoesNotRevoke(t *testing.T) {
	for _, table := range []string{"tenant_resource", "tenant_resource_source", "workload_observation", "resource_allocation_item", "resource_allocation", "resource_scope", "namespace_observation", "resource_target_revision_v2", "workload_observation_source", "technical_resource", "technical_resource_binding", "tenant_access_grant", "tenant_membership", "user", "node"} {
		t.Run(table, func(t *testing.T) {
			fixture := newTenantManagementConstraintFixture(t)
			database := serverdb.DB
			require.NoError(t, database.Model(&model.Node{}).Where("id = ?", fixture.desktop.ID).Update("headscale_node_id", uint64(9901)).Error)
			session := fixture.session(uuid.NewString())
			require.NoError(t, database.Create(&session).Error)
			authorizer := NewSessionAuthorizationService(database)
			authorizer.now = func() time.Time { return fixture.now.Add(time.Second) }
			injected := fmt.Errorf("injected database unavailable: %s", table)
			hits := 0
			require.NoError(t, database.Callback().Query().Before("gorm:query").Register("test:authorization_fault", func(tx *gorm.DB) {
				if tx.Statement.Table == table {
					hits++
					tx.AddError(injected)
				}
			}))
			_, err := authorizer.BuildSnapshot(context.Background(), fixture.technical.ID, true)
			require.NoError(t, database.Callback().Query().Remove("test:authorization_fault"))
			require.Positive(t, hits, "fault must reach the intended query")
			require.ErrorIs(t, err, injected)
			var stored model.ResourceSession
			require.NoError(t, database.First(&stored, "id = ?", session.ID).Error)
			require.Equal(t, session.Status, stored.Status)
			require.Equal(t, session.RowVersion, stored.RowVersion)
			var count int64
			require.NoError(t, database.Model(&model.ResourceSessionTermination{}).Where("session_id = ?", session.ID).Count(&count).Error)
			require.Zero(t, count)
			recovered, err := authorizer.BuildSnapshot(context.Background(), fixture.technical.ID, true)
			require.NoError(t, err)
			require.Len(t, recovered.Permissions, 1)
		})
	}
}
