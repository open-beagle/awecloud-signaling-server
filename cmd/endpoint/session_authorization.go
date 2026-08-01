package main

import (
	"path/filepath"
	"time"

	signalagent "github.com/open-beagle/awecloud-signaling-server/internal/agent"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

type endpointSessionAuthorization struct {
	cache    *signalagent.SessionAuthorizationCache
	sessions *signalagent.ContainerSessionManager
}

func newEndpointSessionAuthorization(stateDir string) (*endpointSessionAuthorization, error) {
	sessions, err := signalagent.NewPersistentContainerSessionManager(filepath.Join(stateDir, "endpoint"))
	if err != nil {
		return nil, err
	}
	return &endpointSessionAuthorization{cache: signalagent.NewSessionAuthorizationCache(), sessions: sessions}, nil
}

func (a *endpointSessionAuthorization) appendReport(req *pb.EndpointHeartbeatRequest) {
	if a == nil || req == nil {
		return
	}
	req.SessionAuthorizationProtocol = "resource_session_v2"
	req.AuthorizationSnapshotAckRevision, req.AuthorizationSnapshotAckHash = a.cache.Ack()
	req.SessionTerminationAcks = a.sessions.TerminationAcksForHeartbeat()
	req.ResourceSessionEvents = a.sessions.ResourceEventsForHeartbeat()
}

func (a *endpointSessionAuthorization) applyResponse(resp *pb.EndpointHeartbeatResponse) {
	if a == nil || resp == nil {
		return
	}
	if err := a.sessions.AckResourceEvents(resp.ResourceSessionEventAcks); err != nil {
		logger.Errorf("Endpoint 持久化 ResourceSession Event ACK 失败: %v", err)
	}
	if resp.AuthorizationSnapshotV2 == nil {
		return
	}
	now := time.Now().UTC()
	if err := a.cache.Apply(resp.AuthorizationSnapshotV2, now); err != nil {
		logger.Warnf("Endpoint 拒绝 ResourceSession v2 快照: revision=%d err=%v", resp.AuthorizationSnapshotV2.Revision, err)
		return
	}
	if err := a.sessions.ApplyTerminationCommands(a.cache.Commands()); err != nil {
		logger.Errorf("Endpoint 应用 ResourceSession 终止命令失败，快照不 ACK: revision=%d err=%v", resp.AuthorizationSnapshotV2.Revision, err)
		return
	}
	a.sessions.ReconcileV2(a.cache, now)
	revision, payloadHash := a.cache.Current()
	if err := a.cache.CommitAck(revision, payloadHash); err != nil {
		logger.Errorf("Endpoint 提交 ResourceSession 快照 ACK 失败: revision=%d err=%v", revision, err)
	}
}

func (a *endpointSessionAuthorization) enabled(now time.Time) bool {
	return a != nil && a.cache.Enabled(now)
}

func (a *endpointSessionAuthorization) enforceV2() bool {
	return a != nil && a.cache.EnforceV2()
}
