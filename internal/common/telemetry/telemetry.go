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
}

// BuildInfo 构建信息，用于 Process 版本标识
type BuildInfo struct {
	Version   string // 应用版本，如 v0.2.2
	GitCommit string // Git 提交哈希
	BuildDate string // 构建日期
	GoVersion string // Go 编译器版本
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
func Init(cfg Config, buildInfo ...BuildInfo) error {
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

	ctx := context.Background()

	// 构建 Resource Attributes
	attrs := []attribute.KeyValue{
		semconv.ServiceName(cfg.ServiceName),
		semconv.ServiceNamespace(cfg.Namespace),
	}

	// 添加构建信息
	if len(buildInfo) > 0 {
		bi := buildInfo[0]
		if bi.Version != "" {
			attrs = append(attrs, semconv.ServiceVersion(bi.Version))
		}
		if bi.GitCommit != "" {
			attrs = append(attrs, attribute.String("service.git_commit", bi.GitCommit))
		}
		if bi.BuildDate != "" {
			attrs = append(attrs, attribute.String("service.build_date", bi.BuildDate))
		}
		if bi.GoVersion != "" {
			attrs = append(attrs, attribute.String("go.version", bi.GoVersion))
		}
	} else {
		attrs = append(attrs, semconv.ServiceVersion("dev"))
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

	logger.Infof("OpenTelemetry 初始化成功: endpoint=%s, service=%s, namespace=%s, tls=%v",
		cfg.Endpoint, cfg.ServiceName, cfg.Namespace, useTLS)
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
