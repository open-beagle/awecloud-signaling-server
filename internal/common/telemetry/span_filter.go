package telemetry

import (
	"context"
	"sync/atomic"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
)

// RootSpanFilter 过滤非法的 Root Span
// 只保留 SpanKindServer 和 SpanKindConsumer，丢弃其他所有类型
type RootSpanFilter struct {
	next            sdktrace.SpanExporter
	droppedCount    atomic.Uint64
	droppedByKind   map[trace.SpanKind]*atomic.Uint64
	loggedFirstDrop atomic.Bool
}

// NewRootSpanFilter 创建 Root Span 过滤器
func NewRootSpanFilter(exporter sdktrace.SpanExporter) *RootSpanFilter {
	return &RootSpanFilter{
		next: exporter,
		droppedByKind: map[trace.SpanKind]*atomic.Uint64{
			trace.SpanKindUnspecified: {},
			trace.SpanKindInternal:    {},
			trace.SpanKindClient:      {},
			trace.SpanKindProducer:    {},
		},
	}
}

// ExportSpans 导出 Span，过滤非法的 Root Span
func (f *RootSpanFilter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	filtered := make([]sdktrace.ReadOnlySpan, 0, len(spans))

	for _, span := range spans {
		// 检查是否为 Root Span（没有父 Span）
		if !span.Parent().IsValid() {
			kind := span.SpanKind()

			// 只保留 Server 和 Consumer
			if kind == trace.SpanKindServer || kind == trace.SpanKindConsumer {
				filtered = append(filtered, span)
			} else {
				// 丢弃非法的 Root Span
				f.droppedCount.Add(1)
				if counter, ok := f.droppedByKind[kind]; ok {
					counter.Add(1)
				}

				// 只记录第一次丢弃的日志
				if f.loggedFirstDrop.CompareAndSwap(false, true) {
					logger.Warnf("OpenTelemetry: 丢弃非法 Root Span: name=%s, kind=%s (后续丢弃将不再记录)",
						span.Name(), spanKindString(kind))
				}
			}
		} else {
			// 非 Root Span，保留
			filtered = append(filtered, span)
		}
	}

	// 如果所有 Span 都被过滤，直接返回
	if len(filtered) == 0 {
		return nil
	}

	// 导出过滤后的 Span
	return f.next.ExportSpans(ctx, filtered)
}

// Shutdown 关闭导出器
func (f *RootSpanFilter) Shutdown(ctx context.Context) error {
	// 输出统计信息
	total := f.droppedCount.Load()
	if total > 0 {
		logger.Infof("OpenTelemetry: 共丢弃 %d 个非法 Root Span", total)
		for kind, counter := range f.droppedByKind {
			count := counter.Load()
			if count > 0 {
				logger.Infof("  - %s: %d", spanKindString(kind), count)
			}
		}
	}

	return f.next.Shutdown(ctx)
}

// spanKindString 返回 SpanKind 的字符串表示
func spanKindString(kind trace.SpanKind) string {
	switch kind {
	case trace.SpanKindUnspecified:
		return "Unspecified"
	case trace.SpanKindInternal:
		return "Internal"
	case trace.SpanKindServer:
		return "Server"
	case trace.SpanKindClient:
		return "Client"
	case trace.SpanKindProducer:
		return "Producer"
	case trace.SpanKindConsumer:
		return "Consumer"
	default:
		return "Unknown"
	}
}
