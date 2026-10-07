package agent

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/service"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

func tunnelSyntheticPermission(tokenID uint64, resourceID string) *pb.ResourceSessionPermissionV2 {
	sessionID, sourceID, targetRevisionID := service.TunnelSessionIDs(tokenID, resourceID)
	return &pb.ResourceSessionPermissionV2{
		SessionId: sessionID, ResourceId: resourceID, SourceId: sourceID, TargetRevisionId: targetRevisionID,
		UserName: "svc-tunnel-test", AllocationId: "tunnel-allocation-93", GrantId: "tunnel-grant-93",
		GrantRevision: 1, AuthorizationRevision: 1,
		ValidUntil: timestamppb.New(time.Now().Add(30 * time.Minute)),
	}
}

// TestIsTunnelSyntheticPermissionRequiresFullIdentity (B5) 仅凭 SessionId 前缀不得被视为 Tunnel 合成会话。
func TestIsTunnelSyntheticPermissionRequiresFullIdentity(t *testing.T) {
	require.True(t, isTunnelSyntheticPermission(tunnelSyntheticPermission(93, "res-studio")))

	cases := map[string]func(p *pb.ResourceSessionPermissionV2){
		"prefix only, uuid source":    func(p *pb.ResourceSessionPermissionV2) { p.SourceId = uuid.NewString() },
		"session/resource mismatch":   func(p *pb.ResourceSessionPermissionV2) { p.ResourceId = "res-other" },
		"allocation from other token": func(p *pb.ResourceSessionPermissionV2) { p.AllocationId = "tunnel-allocation-94" },
		"grant from other token":      func(p *pb.ResourceSessionPermissionV2) { p.GrantId = "tunnel-grant-94" },
		"target revision mismatch":    func(p *pb.ResourceSessionPermissionV2) { p.TargetRevisionId = "rev-1" },
		"non canonical token id":      func(p *pb.ResourceSessionPermissionV2) { p.SourceId = "tunnel-token-093" },
		"zero token id": func(p *pb.ResourceSessionPermissionV2) {
			p.SourceId, p.SessionId = "tunnel-token-0", "tunnel-0-res-studio"
		},
	}
	for name, mutate := range cases {
		p := tunnelSyntheticPermission(93, "res-studio")
		mutate(p)
		require.False(t, isTunnelSyntheticPermission(p), name)
	}
	require.False(t, isTunnelSyntheticPermission(nil))
}

// TestBeginV2TunnelReentryKeepsSequenceMonotonic (B5) Tunnel 合成会话可重入，且 source_sequence 不回退。
func TestBeginV2TunnelReentryKeepsSequenceMonotonic(t *testing.T) {
	m := newContainerSessionManager("")
	m.idleGrace = 0
	p := tunnelSyntheticPermission(93, "res-studio")

	var sequences []int64
	for i := 0; i < 3; i++ {
		_, err := m.BeginV2(context.Background(), p)
		require.NoError(t, err, "round %d", i)
		require.NoError(t, m.EndV2(p.SessionId, "ended", "client_closed"))
	}
	for _, event := range m.ResourceEventsForHeartbeat() {
		require.Equal(t, p.SessionId, event.SessionId)
		sequences = append(sequences, event.SourceSequence)
	}
	require.Len(t, sequences, 6)
	seen := map[int64]bool{}
	for _, s := range sequences {
		require.False(t, seen[s], "source_sequence %d 重复", s)
		seen[s] = true
	}
	require.Equal(t, int64(6), m.nextSequence[p.SessionId])
}

// TestBeginV2RejectsReentryForNonTunnelSessions (B5) 普通会话以及伪造 tunnel- 前缀的会话不得重入。
func TestBeginV2RejectsReentryForNonTunnelSessions(t *testing.T) {
	m := newContainerSessionManager("")
	m.idleGrace = 0

	normal := tunnelSyntheticPermission(93, "res-studio")
	normal.SessionId, normal.SourceId = uuid.NewString(), uuid.NewString()
	forged := tunnelSyntheticPermission(93, "res-studio")
	forged.SessionId = "tunnel-forged-session"

	for _, p := range []*pb.ResourceSessionPermissionV2{normal, forged} {
		_, err := m.BeginV2(context.Background(), p)
		require.NoError(t, err)
		require.NoError(t, m.EndV2(p.SessionId, "ended", "client_closed"))
		_, err = m.BeginV2(context.Background(), p)
		require.EqualError(t, err, "resource session has already started", p.SessionId)
	}
}
