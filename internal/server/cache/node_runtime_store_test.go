package cache

import (
	"strings"
	"testing"
	"time"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func TestNodeRuntimeStoreBasicAndHeartbeat(t *testing.T) {
	store := NewNodeRuntimeStore()

	node := &model.Node{
		ID:       10,
		UserID:   1,
		Name:     "desktop-test",
		Type:     model.NodeTypeDesktop,
		IP:       "100.64.0.10",
		Hostname: "my-desktop",
	}

	store.UpsertNode(node)

	rn, ok := store.GetNode(10)
	if !ok || rn.Name != "desktop-test" {
		t.Fatalf("expected node 10, got ok=%v, rn=%+v", ok, rn)
	}

	rnUser, ok := store.GetNodeByUserAndName(1, model.NodeTypeDesktop, "desktop-test")
	if !ok || rnUser.ID != 10 {
		t.Fatalf("expected secondary lookup ID 10, got %+v", rnUser)
	}

	// 更新心跳
	now := time.Now()
	rev, err := store.UpdateHeartbeat(10, "100.64.0.10", "my-desktop-renamed", "v1.0.2", strings.Repeat("a", 40), &now, strings.Repeat("b", 64), "{}", "v1", "v1", now)
	if err != nil {
		t.Fatalf("update heartbeat failed: %v", err)
	}
	if rev <= 1 {
		t.Fatalf("expected revision > 1, got %d", rev)
	}

	dirtySnap := store.SnapshotDirty()
	if len(dirtySnap) != 1 || dirtySnap[10].Hostname != "my-desktop-renamed" {
		t.Fatalf("expected dirty snapshot with renamed hostname, got %+v", dirtySnap)
	}

	// 模拟 Flush 时又来了一次心跳
	_, _ = store.UpdateHeartbeat(10, "100.64.0.10", "my-desktop-renamed", "v1.0.2", strings.Repeat("a", 40), &now, strings.Repeat("b", 64), "{}", "v1", "v1", now.Add(time.Second))

	// 清除旧 Revision 的 Dirty 标记（因为来过新心跳，Dirty 不应该被误清除）
	store.ClearDirty(map[uint64]uint64{10: rev})

	rnAfter, _ := store.GetNode(10)
	if !rnAfter.Dirty {
		t.Fatalf("expected node 10 to stay dirty because revision changed")
	}

	// 用新 Revision 清除 Dirty 标记
	store.ClearDirty(map[uint64]uint64{10: rnAfter.Revision})
	rnCleared, _ := store.GetNode(10)
	if rnCleared.Dirty {
		t.Fatalf("expected node 10 dirty to be cleared")
	}
}

func TestNodeRuntimeStoreDelete(t *testing.T) {
	store := NewNodeRuntimeStore()

	node := &model.Node{
		ID:     20,
		UserID: 2,
		Name:   "agent-node-20",
		Type:   model.NodeTypeAgent,
	}
	store.UpsertNode(node)

	store.DeleteNode(20)

	_, ok := store.GetNode(20)
	if ok {
		t.Fatalf("expected node 20 to be deleted")
	}
	_, ok = store.GetNodeByUserAndName(2, model.NodeTypeAgent, "agent-node-20")
	if ok {
		t.Fatalf("expected secondary index for node 20 to be deleted")
	}
}
