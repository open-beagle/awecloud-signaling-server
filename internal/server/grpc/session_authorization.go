package grpc

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"sort"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/service"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

const (
	sessionAuthorizationProtocolV2  = "resource_session_v2"
	sessionAuthorizationPushPeriod  = 2 * time.Second
	sessionAuthorizationSnapshotTTL = 30 * time.Second
)

type sessionAuthorizationHeartbeatAcks struct {
	direct   []*pb.ResourceSessionEventAckV2
	endpoint map[string][]*pb.ResourceSessionEventAckV2
}

type authorizationSnapshotState struct {
	revision    int64
	contentHash string
	issuedAt    time.Time
	validUntil  time.Time
	payloadHash string
	ackRevision int64
	ackHash     string
	ackedAt     time.Time
}

func (s *AgentServiceServer) processSessionAuthorizationHeartbeat(ctx context.Context, conn *AgentConnection, req *pb.AgentHeartbeatRequest) *sessionAuthorizationHeartbeatAcks {
	acks := &sessionAuthorizationHeartbeatAcks{endpoint: make(map[string][]*pb.ResourceSessionEventAckV2)}
	if conn == nil || req == nil {
		return acks
	}
	conn.SessionAuthorizationProtocol = req.SessionAuthorizationProtocol
	if conn.EndpointSessionAuthorizationProtocols == nil {
		conn.EndpointSessionAuthorizationProtocols = make(map[string]string)
	}
	if req.SessionAuthorizationProtocol == sessionAuthorizationProtocolV2 {
		technical, err := s.sessionAuthorization.ResolveTechnicalResource(ctx, model.TechnicalResourceBindingLegacyNode, fmt.Sprint(conn.NodeID))
		if err != nil {
			logger.Warnf("Agent Session 授权来源解析失败: node_id=%d err=%v", conn.NodeID, err)
		} else {
			acks.direct = s.processSessionAuthorizationReport(ctx, technical.ID, req.AuthorizationSnapshotAckRevision,
				req.AuthorizationSnapshotAckHash, req.SessionTerminationAcks, req.ResourceSessionEventsV2)
		}
	}
	for _, report := range req.EndpointSessionAuthorizationReportsV2 {
		if report == nil || report.EndpointName == "" {
			continue
		}
		conn.EndpointSessionAuthorizationProtocols[report.EndpointName] = report.ProtocolVersion
		if report.ProtocolVersion != sessionAuthorizationProtocolV2 {
			continue
		}
		technical, err := s.resolveEndpointTechnicalResource(ctx, conn.AgentID, report.EndpointName)
		if err != nil {
			logger.Warnf("Endpoint Session 授权来源解析失败: agent_id=%d endpoint=%s err=%v", conn.AgentID, report.EndpointName, err)
			continue
		}
		acks.endpoint[report.EndpointName] = s.processSessionAuthorizationReport(ctx, technical.ID,
			report.AuthorizationSnapshotAckRevision, report.AuthorizationSnapshotAckHash, report.TerminationAcks, report.Events)
	}
	return acks
}

func (s *AgentServiceServer) processSessionAuthorizationReport(ctx context.Context, technicalResourceID string, snapshotRevision int64, snapshotHash string,
	terminationAcks []*pb.ResourceSessionTerminationAckV2, events []*pb.ResourceSessionEventV2) []*pb.ResourceSessionEventAckV2 {
	if snapshotRevision != 0 || snapshotHash != "" {
		if !s.acknowledgeAuthorizationSnapshot(technicalResourceID, snapshotRevision, snapshotHash) {
			logger.Warnf("拒绝不匹配的 Session 授权快照 ACK: technical_resource_id=%s revision=%d", technicalResourceID, snapshotRevision)
		}
	}
	if len(terminationAcks) > 0 {
		inputs := make([]service.SessionTerminationAckInput, 0, len(terminationAcks))
		for _, ack := range terminationAcks {
			if ack != nil {
				inputs = append(inputs, service.SessionTerminationAckInput{SessionID: ack.SessionId, CommandRevision: ack.CommandRevision})
			}
		}
		if err := s.sessionAuthorization.AcknowledgeTerminations(ctx, technicalResourceID, inputs); err != nil {
			logger.Warnf("Session 终止命令 ACK 失败: technical_resource_id=%s err=%v", technicalResourceID, err)
		}
	}
	if len(events) == 0 {
		return nil
	}
	inputs := make([]service.SessionEventInput, 0, len(events))
	for _, event := range events {
		if event == nil || event.OccurredAt == nil || !event.OccurredAt.IsValid() {
			continue
		}
		inputs = append(inputs, service.SessionEventInput{
			EventID: event.EventId, SessionID: event.SessionId, SourceSequence: event.SourceSequence,
			EventType: model.ResourceSessionEventType(event.EventType), OccurredAt: event.OccurredAt.AsTime(), ResultCode: event.ResultCode,
		})
	}
	accepted, err := s.sessionAuthorization.AcceptEvents(ctx, technicalResourceID, inputs)
	if err != nil {
		logger.Warnf("Session Event 接收失败: technical_resource_id=%s err=%v", technicalResourceID, err)
		return nil
	}
	result := make([]*pb.ResourceSessionEventAckV2, 0, len(accepted))
	for _, ack := range accepted {
		result = append(result, &pb.ResourceSessionEventAckV2{EventId: ack.EventID, ResultCode: ack.ResultCode, Replay: ack.Replay})
	}
	return result
}

func (s *AgentServiceServer) acknowledgeAuthorizationSnapshot(technicalResourceID string, revision int64, payloadHash string) bool {
	if s == nil || technicalResourceID == "" || revision <= 0 || len(payloadHash) != sha256.Size*2 {
		return false
	}
	s.authorizationSnapshotMutex.Lock()
	defer s.authorizationSnapshotMutex.Unlock()
	state := s.authorizationSnapshotStates[technicalResourceID]
	if state == nil || state.revision != revision || state.payloadHash != payloadHash {
		return false
	}
	state.ackRevision = revision
	state.ackHash = payloadHash
	state.ackedAt = time.Now().UTC()
	return true
}

func (s *AgentServiceServer) appendSessionAuthorizationResponse(ctx context.Context, conn *AgentConnection, resp *pb.AgentHeartbeatResponse, acks *sessionAuthorizationHeartbeatAcks) {
	if conn == nil || resp == nil || s.sessionAuthorization == nil {
		return
	}
	includePermissions := s.sessionAuthorizationPermissionsEnabled()
	if conn.SessionAuthorizationProtocol == sessionAuthorizationProtocolV2 {
		technical, err := s.sessionAuthorization.ResolveTechnicalResource(ctx, model.TechnicalResourceBindingLegacyNode, fmt.Sprint(conn.NodeID))
		if err == nil {
			if snapshot, buildErr := s.sessionAuthorization.BuildSnapshot(ctx, technical.ID, includePermissions); buildErr != nil {
				logger.Warnf("构建 Agent Session 授权快照失败: node_id=%d err=%v", conn.NodeID, buildErr)
			} else {
				resp.AuthorizationSnapshotV2 = s.toProtoAuthorizationSnapshot(snapshot, includePermissions)
			}
		}
		if acks != nil {
			resp.ResourceSessionEventAcksV2 = acks.direct
		}
	}
	for endpointName, protocolVersion := range conn.EndpointSessionAuthorizationProtocols {
		if protocolVersion != sessionAuthorizationProtocolV2 {
			continue
		}
		wrapper := &pb.EndpointSessionAuthorizationSnapshotV2{EndpointName: endpointName}
		if acks != nil {
			wrapper.EventAcks = acks.endpoint[endpointName]
		}
		technical, err := s.resolveEndpointTechnicalResource(ctx, conn.AgentID, endpointName)
		if err == nil {
			if snapshot, buildErr := s.sessionAuthorization.BuildSnapshot(ctx, technical.ID, includePermissions); buildErr != nil {
				logger.Warnf("构建 Endpoint Session 授权快照失败: endpoint=%s err=%v", endpointName, buildErr)
			} else {
				wrapper.Snapshot = s.toProtoAuthorizationSnapshot(snapshot, includePermissions)
			}
		}
		resp.EndpointAuthorizationSnapshotsV2 = append(resp.EndpointAuthorizationSnapshotsV2, wrapper)
	}
}

func (s *AgentServiceServer) sessionAuthorizationPermissionsEnabled() bool {
	if s == nil || s.config == nil {
		return false
	}
	flags := s.config.FeatureFlags
	return flags.Enabled(config.FeatureManagementContextV2) && flags.Enabled(config.FeatureTenantResourceReadV2) &&
		flags.Enabled(config.FeatureResourceModelWrite) && flags.Enabled(config.FeatureSessionAuthorizationV2)
}

func (s *AgentServiceServer) resolveEndpointTechnicalResource(ctx context.Context, agentID uint64, endpointName string) (*model.TechnicalResource, error) {
	var endpoint model.Endpoint
	if err := db.DB.WithContext(ctx).Where("user_id = ? AND name = ? AND revoked = ?", agentID, endpointName, false).First(&endpoint).Error; err != nil {
		return nil, service.ErrSessionAuthorizationSourceDenied
	}
	return s.sessionAuthorization.ResolveTechnicalResource(ctx, model.TechnicalResourceBindingLegacyEndpoint, endpoint.ID)
}

func (s *AgentServiceServer) toProtoAuthorizationSnapshot(snapshot *service.SessionAuthorizationSnapshot, enforceV2 bool) *pb.ResourceSessionAuthorizationSnapshotV2 {
	if snapshot == nil {
		return nil
	}
	permissions := make([]*pb.ResourceSessionPermissionV2, 0, len(snapshot.Permissions))
	listenPorts := allocateSessionAuthorizationListenPorts(snapshot.Permissions)
	validUntil := time.Now().UTC().Add(sessionAuthorizationSnapshotTTL)
	for _, permission := range snapshot.Permissions {
		if permission.ValidUntil.Before(validUntil) {
			validUntil = permission.ValidUntil
		}
		permissions = append(permissions, &pb.ResourceSessionPermissionV2{
			SessionId: permission.SessionID, TenantId: permission.TenantID, ResourceId: permission.ResourceID,
			SourceId: permission.SourceID, TargetRevisionId: permission.TargetRevisionID,
			UserId: permission.UserID, UserName: permission.UserName, DeviceId: permission.DeviceID,
			DeviceHeadscaleNodeId: permission.DeviceHeadscaleNodeID, ResourceType: string(permission.ResourceType), Action: permission.Action,
			AllocationId: permission.AllocationID, GrantId: permission.GrantID, GrantRevision: permission.GrantRevision,
			AuthorizationRevision: permission.AuthorizationRevision, ValidUntil: timestamppb.New(permission.ValidUntil),
			ListenPort: uint32(listenPorts[permission.ResourceID]),
			Target: &pb.ResourceSessionTargetV2{
				NamespaceUid: permission.Target.NamespaceUID, NamespaceName: permission.Target.NamespaceName,
				ServiceUid: permission.Target.ServiceUID, ServiceName: permission.Target.ServiceName,
				PortName: permission.Target.PortName, PortNumber: permission.Target.PortNumber, Protocol: permission.Target.Protocol,
				WorkloadUid: permission.Target.WorkloadUID, PodName: permission.Target.PodName,
				PodUid: permission.Target.PodUID, ContainerName: permission.Target.ContainerName,
			},
		})
	}
	commands := make([]*pb.ResourceSessionTerminationCommandV2, 0, len(snapshot.Terminations))
	for _, command := range snapshot.Terminations {
		commands = append(commands, &pb.ResourceSessionTerminationCommandV2{
			SessionId: command.SessionID, CommandRevision: command.CommandRevision, ReasonCode: command.ReasonCode, Reason: command.Reason,
		})
	}
	content := &pb.ResourceSessionAuthorizationSnapshotV2{ReplaceAll: true, Permissions: permissions, TerminationCommands: commands, EnforceV2: enforceV2}
	contentBytes, _ := proto.MarshalOptions{Deterministic: true}.Marshal(content)
	contentDigest := sha256.Sum256(contentBytes)
	contentHash := hex.EncodeToString(contentDigest[:])
	now := time.Now().UTC()

	s.authorizationSnapshotMutex.Lock()
	state := s.authorizationSnapshotStates[snapshot.TechnicalResourceID]
	if state == nil || state.contentHash != contentHash || !state.validUntil.After(now.Add(5*time.Second)) {
		revision := now.UnixNano()
		if state != nil && revision <= state.revision {
			revision = state.revision + 1
		}
		state = &authorizationSnapshotState{revision: revision, contentHash: contentHash, issuedAt: now, validUntil: validUntil}
		final := &pb.ResourceSessionAuthorizationSnapshotV2{
			Revision: state.revision, IssuedAt: timestamppb.New(state.issuedAt), ValidUntil: timestamppb.New(state.validUntil),
			ReplaceAll: true, Permissions: permissions, TerminationCommands: commands, EnforceV2: enforceV2,
		}
		payload, _ := proto.MarshalOptions{Deterministic: true}.Marshal(final)
		digest := sha256.Sum256(payload)
		state.payloadHash = hex.EncodeToString(digest[:])
		s.authorizationSnapshotStates[snapshot.TechnicalResourceID] = state
	}
	s.authorizationSnapshotMutex.Unlock()

	return &pb.ResourceSessionAuthorizationSnapshotV2{
		Revision: state.revision, IssuedAt: timestamppb.New(state.issuedAt), ValidUntil: timestamppb.New(state.validUntil),
		ReplaceAll: true, PayloadHash: state.payloadHash, Permissions: permissions, TerminationCommands: commands, EnforceV2: enforceV2,
	}
}

// allocateSessionAuthorizationListenPorts gives each active ContainerSSH
// Resource a deterministic Agent-local port. Open addressing removes hash
// collisions while every session for the same Resource keeps one route.
func allocateSessionAuthorizationListenPorts(permissions []service.SessionAuthorizationPermission) map[string]uint16 {
	resourceSet := make(map[string]struct{})
	for _, permission := range permissions {
		if permission.ResourceType == model.TenantResourceContainerSSH && permission.ResourceID != "" {
			resourceSet[permission.ResourceID] = struct{}{}
		}
	}
	resourceIDs := make([]string, 0, len(resourceSet))
	for resourceID := range resourceSet {
		resourceIDs = append(resourceIDs, resourceID)
	}
	sort.Strings(resourceIDs)
	result := make(map[string]uint16, len(resourceIDs))
	occupied := make(map[uint16]struct{}, len(resourceIDs))
	rangeSize := int(service.ContainerSSHPortEnd-service.ContainerSSHPortBase) + 1
	for _, resourceID := range resourceIDs {
		digest := sha256.Sum256([]byte("resource-session-listen-port\x00" + resourceID))
		start := int(binary.BigEndian.Uint16(digest[:2])) % rangeSize
		for offset := 0; offset < rangeSize; offset++ {
			port := service.ContainerSSHPortBase + uint16((start+offset)%rangeSize)
			if _, exists := occupied[port]; exists {
				continue
			}
			occupied[port] = struct{}{}
			result[resourceID] = port
			break
		}
	}
	return result
}
