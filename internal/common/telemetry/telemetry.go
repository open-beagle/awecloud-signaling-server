package telemetry

import (
	"context"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
)

// Config OpenTelemetry 配置
type Config struct {
	Endpoint    string // OTLP Endpoint，设置后自动启用
	ServiceName string // 服务名称
	Namespace   string // 服务命名空间
	Cluster     string // 集群标识
}

// BuildInfo 构建信息，用于 Process 版本标识
type BuildInfo struct {
	Version   string // 应用版本，如 v0.2.2
	GitCommit string // Git 提交哈希
	BuildDate string // 构建日期
	GoVersion string // Go 编译器版本
}

// ProcessAttributes Process 级别的属性，用于区分不同实例
type ProcessAttributes struct {
	Node string // Agent 节点名称（如 agent-prod-01）
	User string // Desktop 用户名称（如 user@example.com）
}

var (
	tracerProvider *sdktrace.TracerProvider
	tracer         trace.Tracer
)

// IsEnabled 判断是否启用 OpenTelemetry
func (c Config) IsEnabled() bool {
	return c.Endpoint != ""
}

// Init 初始化 OpenTelemetry
// buildInfo 可选，传入构建信息用于 Process 版本标识
// processAttrs 可选，传入 Process 属性用于区分实例
func Init(cfg Config, buildInfo *BuildInfo, processAttrs *ProcessAttributes) error {
	if !cfg.IsEnabled() {
		logger.Info("OpenTelemetry 未配置 endpoint，跳过初始化")
		return nil
	}

	if cfg.ServiceName == "" {
		cfg.ServiceName = "signaling-server"
	}
	if cfg.Namespace == "" {
		cfg.Namespace = "default"
	}
	if cfg.Cluster == "" {
		cfg.Cluster = "default"
	}

	ctx := context.Background()

	// 构建 Resource Attributes
	attrs := []attribute.KeyValue{
		semconv.ServiceName(cfg.ServiceName),
		semconv.ServiceNamespace(cfg.Namespace),
		// service.cluster 表示服务所在的业务集群（数据来源）
		attribute.String("service.cluster", cfg.Cluster),
	}

	// 添加构建信息
	if buildInfo != nil {
		if buildInfo.Version != "" {
			attrs = append(attrs, semconv.ServiceVersion(buildInfo.Version))
		}
		if buildInfo.GitCommit != "" {
			attrs = append(attrs, attribute.String("service.git_commit", buildInfo.GitCommit))
		}
		if buildInfo.BuildDate != "" {
			attrs = append(attrs, attribute.String("service.build_date", buildInfo.BuildDate))
		}
		if buildInfo.GoVersion != "" {
			attrs = append(attrs, attribute.String("go.version", buildInfo.GoVersion))
		}
	} else {
		attrs = append(attrs, semconv.ServiceVersion("dev"))
	}

	// 添加 Process 属性（用于区分不同实例）
	if processAttrs != nil {
		if processAttrs.Node != "" {
			attrs = append(attrs, attribute.String("service.node", processAttrs.Node))
		}
		if processAttrs.User != "" {
			attrs = append(attrs, attribute.String("service.user", processAttrs.User))
		}
	}

	// 创建资源
	res, err := resource.New(ctx, resource.WithAttributes(attrs...))
	if err != nil {
		return err
	}

	// 根据 endpoint 自动判断是否使用 TLS
	// http:// 开头使用非安全连接，https:// 或无协议前缀使用 TLS
	endpoint := cfg.Endpoint
	useTLS := true
	if strings.HasPrefix(endpoint, "http://") {
		useTLS = false
		endpoint = strings.TrimPrefix(endpoint, "http://")
	} else if strings.HasPrefix(endpoint, "https://") {
		endpoint = strings.TrimPrefix(endpoint, "https://")
	}

	// 如果没有指定端口，根据协议添加默认端口
	if !strings.Contains(endpoint, ":") {
		if useTLS {
			endpoint = endpoint + ":443"
		} else {
			endpoint = endpoint + ":80"
		}
	}

	// 配置 gRPC 连接
	var opts []grpc.DialOption
	if useTLS {
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewClientTLSFromCert(nil, "")))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	// 创建 OTLP exporter
	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithDialOption(opts...),
	)
	if err != nil {
		return err
	}

	// 创建 TracerProvider
	tracerProvider = sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter,
			sdktrace.WithBatchTimeout(5*time.Second),
		),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)

	// 设置全局 TracerProvider
	otel.SetTracerProvider(tracerProvider)

	// 设置全局 Propagator
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// 创建 Tracer
	tracer = tracerProvider.Tracer(cfg.ServiceName)

	logger.Infof("OpenTelemetry 初始化成功: endpoint=%s, service=%s, namespace=%s, cluster=%s, tls=%v",
		cfg.Endpoint, cfg.ServiceName, cfg.Namespace, cfg.Cluster, useTLS)
	return nil
}

// Shutdown 关闭 OpenTelemetry
func Shutdown(ctx context.Context) error {
	if tracerProvider == nil {
		return nil
	}
	logger.Info("正在关闭 OpenTelemetry...")
	return tracerProvider.Shutdown(ctx)
}

// Tracer 获取全局 Tracer
func Tracer() trace.Tracer {
	if tracer == nil {
		return otel.Tracer("awecloud-signaling-server")
	}
	return tracer
}

// StartSpan 开始一个新的 Span
func StartSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return Tracer().Start(ctx, name, opts...)
}
