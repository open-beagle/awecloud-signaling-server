package headscale

import (
	"testing"
	"time"

	v1 "github.com/juanfont/headscale/gen/go/headscale/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestHeadscaleNodeSnapshotIndexAndLookup(t *testing.T) {
	now := time.Now()
	rawNodes := []*v1.Node{
		{
			Id:          1001,
			GivenName:   "desktop-alice",
			User:        &v1.User{Name: "client-alice"},
			IpAddresses: []string{"100.64.0.1", "fd7a:115c:a1e0::1"},
			Online:      true,
			ForcedTags:  []string{"tag:desktop"},
			LastSeen:    timestamppb.New(now),
		},
		{
			Id:          1002,
			GivenName:   "agent-node-1",
			User:        &v1.User{Name: "agent-beagle"},
			IpAddresses: []string{"100.64.0.2"},
			Online:      false,
			ForcedTags:  []string{"tag:agent"},
			LastSeen:    timestamppb.New(now.Add(-10 * time.Minute)),
		},
		// 重名离线旧节点
		{
			Id:          1000,
			GivenName:   "desktop-alice",
			User:        &v1.User{Name: "client-alice"},
			IpAddresses: []string{"100.64.0.99"},
			Online:      false,
			LastSeen:    timestamppb.New(now.Add(-1 * time.Hour)),
		},
	}

	snapshot := NewHeadscaleNodeSnapshot(1, now, rawNodes)

	if snapshot.Version != 1 {
		t.Fatalf("expected version 1, got %d", snapshot.Version)
	}

	// 1. ByID
	v1001, ok := snapshot.GetByID(1001)
	if !ok || v1001.GivenName != "desktop-alice" || !v1001.Online {
		t.Fatalf("ByID 1001 lookup failed: %+v", v1001)
	}

	// 2. ByIP
	vIP, ok := snapshot.GetByIP("100.64.0.2")
	if !ok || vIP.ID != 1002 {
		t.Fatalf("ByIP 100.64.0.2 lookup failed: %+v", vIP)
	}

	// 3. ByUserNameAndNodeName 优先推荐在线且新 ID 的节点
	vPreferred, ok := snapshot.GetByUserNameAndNodeName("client-alice", "desktop-alice")
	if !ok || vPreferred.ID != 1001 {
		t.Fatalf("expected preferred node 1001, got %+v", vPreferred)
	}

	// 4. ByUser
	userNodes := snapshot.GetByUser("client-alice")
	if len(userNodes) != 2 {
		t.Fatalf("expected 2 nodes for client-alice, got %d", len(userNodes))
	}
}

func TestSnapshotAgeAndStaleness(t *testing.T) {
	now := time.Now()
	snapshot := NewHeadscaleNodeSnapshot(1, now.Add(-40*time.Second), nil)

	if snapshot.IsFresh(now) {
		t.Fatalf("expected not fresh at 40s age")
	}

	if !snapshot.IsDegraded(now) {
		t.Fatalf("expected degraded at 40s age")
	}

	if snapshot.IsExpired(now) {
		t.Fatalf("expected not expired at 40s age")
	}

	expiredSnapshot := NewHeadscaleNodeSnapshot(2, now.Add(-70*time.Second), nil)
	if !expiredSnapshot.IsExpired(now) {
		t.Fatalf("expected expired at 70s age")
	}
}
