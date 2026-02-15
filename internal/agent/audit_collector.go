package agent

import (
	"sync"
	"time"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

// AuditCollector 操作审计记录收集器
// 收集 Agent 侧的操作审计记录，在心跳时批量上报给 Server
type AuditCollector struct {
	mu      sync.Mutex
	records []*pb.OperationAuditRecord
}

// NewAuditCollector 创建审计收集器
func NewAuditCollector() *AuditCollector {
	return &AuditCollector{}
}

// Record 记录一条审计日志
func (c *AuditCollector) Record(clientUserName, endpointName, operationType, target, detail string, startedAt, endedAt time.Time) {
	record := &pb.OperationAuditRecord{
		ClientUserName: clientUserName,
		EndpointName:   endpointName,
		OperationType:  operationType,
		Target:         target,
		Detail:         detail,
		StartedAt:      startedAt.Unix(),
		EndedAt:        endedAt.Unix(),
	}

	c.mu.Lock()
	c.records = append(c.records, record)
	c.mu.Unlock()

	logger.Debugf("[Audit] 记录: type=%s, client=%s, target=%s", operationType, clientUserName, target)
}

// Flush 取出所有待上报的记录并清空缓冲区
func (c *AuditCollector) Flush() []*pb.OperationAuditRecord {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.records) == 0 {
		return nil
	}

	records := c.records
	c.records = nil
	return records
}
