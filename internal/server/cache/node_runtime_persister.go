package cache

import (
	"context"
	"fmt"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const (
	defaultPersistInterval = 5 * time.Minute
	highPriorityDelay      = 1 * time.Second
	technicalResourceLease = 2*defaultPersistInterval + time.Minute
)

// NodeRuntimePersister 负责将 NodeRuntimeStore 中的脏节点合并批量落库
type NodeRuntimePersister struct {
	store          *NodeRuntimeStore
	db             *gorm.DB
	highPriorityCh chan struct{}
	ctx            context.Context
	cancel         context.CancelFunc
	wg             sync.WaitGroup
}

// NewNodeRuntimePersister 创建持久化器实例
func NewNodeRuntimePersister(store *NodeRuntimeStore, db *gorm.DB) *NodeRuntimePersister {
	return &NodeRuntimePersister{
		store:          store,
		db:             db,
		highPriorityCh: make(chan struct{}, 1),
	}
}

// Start 启动后台定时与高优先级落库循环
func (p *NodeRuntimePersister) Start(parentCtx context.Context) {
	p.ctx, p.cancel = context.WithCancel(parentCtx)

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		p.loop()
	}()
}

// NotifyHighPriority 发送高优先级落库通知（如绑定 HeadscaleNodeID 后调用），在 1s 内落库
func (p *NodeRuntimePersister) NotifyHighPriority() {
	select {
	case p.highPriorityCh <- struct{}{}:
	default:
	}
}

// Stop 优雅关闭：停止轮询并强制执行最后一次 Flush
func (p *NodeRuntimePersister) Stop() {
	if p.cancel != nil {
		p.cancel()
	}
	p.wg.Wait()

	// 服务关闭时强制落库
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p.Flush(ctx)
}

func (p *NodeRuntimePersister) loop() {
	ticker := time.NewTicker(defaultPersistInterval)
	defer ticker.Stop()

	var hpTimer *time.Timer

	for {
		select {
		case <-p.ctx.Done():
			if hpTimer != nil {
				hpTimer.Stop()
			}
			return

		case <-p.highPriorityCh:
			// 1 秒延迟落库
			if hpTimer != nil {
				hpTimer.Stop()
			}
			hpTimer = time.AfterFunc(highPriorityDelay, func() {
				p.Flush(p.ctx)
			})

		case <-ticker.C:
			p.Flush(p.ctx)
		}
	}
}

// Flush 将当前 Dirty 状态的节点在单 SQLite 事务中合并持久化
func (p *NodeRuntimePersister) Flush(parentCtx context.Context) {
	start := time.Now()
	dirtyMap := p.store.SnapshotDirty()
	if len(dirtyMap) == 0 {
		return
	}

	tracer := otel.Tracer("node_runtime")
	ctx, span := tracer.Start(parentCtx, "node_runtime.flush",
		trace.WithSpanKind(trace.SpanKindInternal),
	)
	defer span.End()

	clearedRevisions := make(map[uint64]uint64, len(dirtyMap))

	err := p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for id, snapshot := range dirtyMap {
			updates := map[string]interface{}{
				"ip":                     snapshot.IP,
				"hostname":               snapshot.Hostname,
				"version":                snapshot.Version,
				"commit_id":              snapshot.CommitID,
				"commit_date":            snapshot.CommitDate,
				"binary_sha256":          snapshot.BinarySHA256,
				"system_info":            snapshot.SystemInfo,
				"updater_protocol":       snapshot.UpdaterProtocol,
				"container_ssh_protocol": snapshot.ContainerSSHProtocol,
				"headscale_node_id":      snapshot.HeadscaleNodeID,
				"updated_at":             start,
			}
			if !snapshot.LastHeartbeat.IsZero() {
				updates["last_heartbeat"] = snapshot.LastHeartbeat
			}

			if err := tx.Model(&model.Node{}).Where("id = ?", id).Updates(updates).Error; err != nil {
				return err
			}
			if !snapshot.LastHeartbeat.IsZero() {
				sourceID := fmt.Sprint(id)
				resourceIDs := tx.Model(&model.TechnicalResourceBinding{}).
					Select("technical_resource_id").
					Where("source_type = ? AND source_id = ? AND enabled = ?", model.TechnicalResourceBindingLegacyNode, sourceID, true)
				if err := tx.Model(&model.TechnicalResource{}).
					Where("id IN (?) AND lifecycle_state = ?", resourceIDs, model.TechnicalResourceRegistered).
					Updates(map[string]any{
						"health_state":     model.ResourceHealthOnline,
						"last_received_at": snapshot.LastHeartbeat,
						"lease_expires_at": start.Add(technicalResourceLease),
					}).Error; err != nil {
					return err
				}
				if err := tx.Model(&model.SupplyCandidate{}).
					Where("technical_resource_id IN (?) AND resource_type = ? AND stable_key = ?", resourceIDs, model.SupplyResourceHost, "legacy-host-legacy_node:"+sourceID).
					Updates(map[string]any{
						"last_observed_at": start,
						"lease_expires_at": start.Add(technicalResourceLease),
					}).Error; err != nil {
					return err
				}
			}
			clearedRevisions[id] = snapshot.Revision
		}
		return nil
	})

	duration := time.Since(start)

	if err != nil {
		span.RecordError(err)
		span.SetAttributes(
			attribute.Int("dirty_count", len(dirtyMap)),
			attribute.Float64("duration_ms", float64(duration.Milliseconds())),
			attribute.String("result", "failure"),
		)
		logger.Warnf("Node 运行态批量落库失败: dirty_count=%d, duration=%v, err=%v",
			len(dirtyMap), duration, err)
		return
	}

	p.store.ClearDirty(clearedRevisions)

	span.SetAttributes(
		attribute.Int("dirty_count", len(dirtyMap)),
		attribute.Int("updated_count", len(clearedRevisions)),
		attribute.Float64("duration_ms", float64(duration.Milliseconds())),
		attribute.String("result", "success"),
	)

	logger.Infof("Node 运行态批量落库完成: updated_count=%d, duration=%v",
		len(clearedRevisions), duration)
}
