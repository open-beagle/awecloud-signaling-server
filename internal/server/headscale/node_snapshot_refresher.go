package headscale

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/singleflight"
)

const (
	defaultRefreshInterval = 10 * time.Second
	defaultRPCTimeout      = 15 * time.Second
)

// SnapshotRefresher 负责后台维护不可变 Headscale 节点快照
type SnapshotRefresher struct {
	client     *Client
	snapshot   atomic.Pointer[HeadscaleNodeSnapshot]
	sfg        singleflight.Group
	triggerCh  chan struct{}
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	versionSeq uint64
}

// NewSnapshotRefresher 创建 Headscale 快照刷新器
func NewSnapshotRefresher(client *Client) *SnapshotRefresher {
	r := &SnapshotRefresher{
		client:    client,
		triggerCh: make(chan struct{}, 1),
	}
	empty := NewHeadscaleNodeSnapshot(0, time.Time{}, nil)
	r.snapshot.Store(empty)
	return r
}

// Start 启动后台定时与事件驱动刷新循环
func (r *SnapshotRefresher) Start(parentCtx context.Context) {
	r.ctx, r.cancel = context.WithCancel(parentCtx)

	// 立即启动一次异步刷新
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		r.loop()
	}()
}

// Stop 停止刷新循环
func (r *SnapshotRefresher) Stop() {
	if r.cancel != nil {
		r.cancel()
	}
	r.wg.Wait()
}

// LoadSnapshot 安全获取当前最新只读快照（零锁原子读取）
func (r *SnapshotRefresher) LoadSnapshot() *HeadscaleNodeSnapshot {
	s := r.snapshot.Load()
	if s == nil {
		return NewHeadscaleNodeSnapshot(0, time.Time{}, nil)
	}
	return s
}

// NotifyRefresh 发送非阻塞刷新事件通知（设备注册/变更/删除后调用）
func (r *SnapshotRefresher) NotifyRefresh() {
	select {
	case r.triggerCh <- struct{}{}:
	default:
	}
}

// RefreshNow 立即执行或合并一次快照刷新（使用 singleflight 抑制并发重复 RPC）
func (r *SnapshotRefresher) RefreshNow(ctx context.Context) (*HeadscaleNodeSnapshot, error) {
	v, err, _ := r.sfg.Do("refresh", func() (interface{}, error) {
		return r.doRefresh(ctx)
	})
	if err != nil {
		return nil, err
	}
	return v.(*HeadscaleNodeSnapshot), nil
}

func (r *SnapshotRefresher) loop() {
	// 启动后立即执行一次刷新
	_, _ = r.RefreshNow(r.ctx)

	timer := time.NewTimer(defaultRefreshInterval)
	defer timer.Stop()

	for {
		select {
		case <-r.ctx.Done():
			return

		case <-r.triggerCh:
			// 事件驱动触发刷新
			_, _ = r.RefreshNow(r.ctx)
			// 重置定时器，避免事件刷新后紧接着定时刷新
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(defaultRefreshInterval)

		case <-timer.C:
			// 定时刷新
			_, _ = r.RefreshNow(r.ctx)
			timer.Reset(defaultRefreshInterval)
		}
	}
}

func (r *SnapshotRefresher) doRefresh(parentCtx context.Context) (*HeadscaleNodeSnapshot, error) {
	start := time.Now()
	tracer := otel.Tracer("headscale.snapshot")

	ctx, span := tracer.Start(parentCtx, "headscale.snapshot.refresh",
		trace.WithSpanKind(trace.SpanKindInternal),
	)
	defer span.End()

	reqCtx, cancel := context.WithTimeout(ctx, defaultRPCTimeout)
	defer cancel()

	nodes, err := r.client.ListNodes(reqCtx)
	duration := time.Since(start)

	if err != nil {
		current := r.LoadSnapshot()
		span.RecordError(err)
		span.SetAttributes(
			attribute.String("result", "failure"),
			attribute.Float64("duration_ms", float64(duration.Milliseconds())),
			attribute.Int64("snapshot_age_s", int64(current.Age(start).Seconds())),
		)
		logger.Warnf("Headscale 节点快照刷新失败: err=%v, duration=%v, age=%v",
			err, duration, current.Age(start))
		return current, fmt.Errorf("刷新快照失败: %w", err)
	}

	newVersion := atomic.AddUint64(&r.versionSeq, 1)
	newSnapshot := NewHeadscaleNodeSnapshot(newVersion, start, nodes)
	r.snapshot.Store(newSnapshot)

	span.SetAttributes(
		attribute.Int64("version", int64(newVersion)),
		attribute.Int("node_count", len(nodes)),
		attribute.Float64("duration_ms", float64(duration.Milliseconds())),
		attribute.String("result", "success"),
	)

	logger.Infof("Headscale 节点快照刷新完成: version=%d, node_count=%d, duration=%v",
		newVersion, len(nodes), duration)

	return newSnapshot, nil
}
