package telemetry

import (
	"sync"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc/stats"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
)

// GRPCTraceLimiter gRPC trace 限流器
// 按方法名统计调用次数，超过阈值后跳过 trace 记录
type GRPCTraceLimiter struct {
	// 每分钟最大 trace 记录数
	maxPerMinute int
	// 方法计数器：method -> counter
	counters map[string]*methodCounter
	mu       sync.Mutex
}

// methodCounter 方法计数器
type methodCounter struct {
	count     int   // 当前窗口内的 trace 记录数
	total     int   // 当前窗口内的总调用数
	windowEnd int64 // 窗口结束时间（Unix 秒）
	logged    bool  // 本窗口是否已输出限流日志
}

// NewGRPCTraceLimiter 创建 gRPC trace 限流器
// maxPerMinute: 每分钟每方法最大 trace 记录数，默认 10
func NewGRPCTraceLimiter(maxPerMinute int) *GRPCTraceLimiter {
	if maxPerMinute <= 0 {
		maxPerMinute = 10
	}
	return &GRPCTraceLimiter{
		maxPerMinute: maxPerMinute,
		counters:     make(map[string]*methodCounter),
	}
}

// Filter 返回 otelgrpc.WithFilter 使用的过滤函数
// 返回 true 表示记录 trace，返回 false 表示跳过
func (l *GRPCTraceLimiter) Filter() otelgrpc.Filter {
	return func(info *stats.RPCTagInfo) bool {
		return l.Allow(info.FullMethodName)
	}
}

// Allow 判断是否允许记录 trace
// method: gRPC 方法名，如 /proto.AgentService/Heartbeat
func (l *GRPCTraceLimiter) Allow(method string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now().Unix()
	windowEnd := now - (now % 60) + 60 // 当前分钟的结束时间

	counter, exists := l.counters[method]
	if !exists {
		// 新方法，创建计数器
		l.counters[method] = &methodCounter{
			count:     1,
			total:     1,
			windowEnd: windowEnd,
			logged:    false,
		}
		return true
	}

	// 检查是否需要重置窗口
	if now >= counter.windowEnd {
		// 窗口已过期，重置
		counter.count = 1
		counter.total = 1
		counter.windowEnd = windowEnd
		counter.logged = false
		return true
	}

	// 窗口内，递增总调用数
	counter.total++

	// 检查是否超过限流阈值
	if counter.count >= l.maxPerMinute {
		// 超过阈值，输出日志（每窗口一次）
		if !counter.logged {
			counter.logged = true
			// 异步输出日志，避免阻塞
			go logger.Warnf("gRPC trace 限流: method=%s, limit=%d/min", method, l.maxPerMinute)
		}
		return false
	}

	// 未超过阈值，记录 trace
	counter.count++
	return true
}

// Stats 获取统计信息（用于调试）
func (l *GRPCTraceLimiter) Stats() map[string]map[string]int {
	l.mu.Lock()
	defer l.mu.Unlock()

	stats := make(map[string]map[string]int)
	for method, counter := range l.counters {
		stats[method] = map[string]int{
			"traced": counter.count,
			"total":  counter.total,
		}
	}
	return stats
}

// 全局限流器实例
var grpcLimiter *GRPCTraceLimiter

// InitGRPCLimiter 初始化全局 gRPC 限流器
func InitGRPCLimiter(maxPerMinute int) {
	grpcLimiter = NewGRPCTraceLimiter(maxPerMinute)
	logger.Infof("gRPC trace 限流器已初始化: limit=%d/min", maxPerMinute)
}

// GetGRPCLimiterFilter 获取全局限流器的 Filter
// 如果未初始化，返回 nil（不限流）
func GetGRPCLimiterFilter() otelgrpc.Filter {
	if grpcLimiter == nil {
		return nil
	}
	return grpcLimiter.Filter()
}
