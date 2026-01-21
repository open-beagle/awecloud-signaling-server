# gRPC OpenTelemetry 集成规范

## 概述

gRPC 支持服务端和客户端的 OpenTelemetry 追踪。服务端作为入口接收或创建 trace，客户端用于追踪对下游 gRPC 服务（如 ETCD）的调用。

## 依赖包

```go
require (
    google.golang.org/grpc v1.60.0
    go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.58.0
)
```

## gRPC Server（入口）

### 集成位置

gRPC Server 初始化处，通常在 `internal/server/server.go` 或 `cmd/server/main.go`。

### 集成方式

```go
import (
    "google.golang.org/grpc"
    "go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
)

func NewGRPCServer() *grpc.Server {
    // 使用 StatsHandler 方式（推荐）
    server := grpc.NewServer(
        grpc.StatsHandler(otelgrpc.NewServerHandler()),
    )

    logger.Info("gRPC Server OpenTelemetry 追踪已启用")
    return server
}
```

### 自定义配置（可选）

```go
import (
    "go.opentelemetry.io/otel/metric"
    "go.opentelemetry.io/otel/trace"
)

server := grpc.NewServer(
    grpc.StatsHandler(otelgrpc.NewServerHandler(
        otelgrpc.WithTracerProvider(trace.TracerProvider),  // 自定义 TracerProvider
        otelgrpc.WithMeterProvider(metric.MeterProvider),   // 启用 Metrics
    )),
)
```

### Handler 实现

```go
type AgentServiceServer struct {
    pb.UnimplementedAgentServiceServer
}

// gRPC Handler 自动接收 context
func (s *AgentServiceServer) RegisterAgent(
    ctx context.Context,
    req *pb.RegisterAgentRequest,
) (*pb.RegisterAgentResponse, error) {
    // ctx 已经包含 trace context，直接传递给下游
    agent, err := s.agentService.Register(ctx, req)
    if err != nil {
        return nil, err
    }

    return &pb.RegisterAgentResponse{
        AgentId: agent.ID,
    }, nil
}
```

### Jaeger 显示效果

**Span 名称**：`/package.Service/Method`

示例：

- `/pb.AgentService/RegisterAgent`
- `/pb.DesktopService/GetServices`

**Span 属性**：

| 属性                   | 说明        | 示例                    |
| ---------------------- | ----------- | ----------------------- |
| `rpc.system`           | RPC 系统    | `grpc`                  |
| `rpc.service`          | 服务名称    | `pb.AgentService`       |
| `rpc.method`           | 方法名称    | `RegisterAgent`         |
| `rpc.grpc.status_code` | gRPC 状态码 | `0` (OK), `2` (UNKNOWN) |
| `net.peer.name`        | 客户端地址  | `192.168.1.100`         |
| `net.peer.port`        | 客户端端口  | `54321`                 |

## gRPC Client（下游）

### 集成位置

gRPC 客户端初始化处，用于连接 ETCD、其他微服务等。

### ETCD 客户端集成

```go
import (
    clientv3 "go.etcd.io/etcd/client/v3"
    "go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
    "google.golang.org/grpc"
)

func NewETCDClient(endpoints []string) (*clientv3.Client, error) {
    cli, err := clientv3.New(clientv3.Config{
        Endpoints:   endpoints,
        DialTimeout: 5 * time.Second,
        // 注入 gRPC 客户端拦截器
        DialOptions: []grpc.DialOption{
            grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
        },
    })
    if err != nil {
        return nil, fmt.Errorf("创建 ETCD 客户端失败: %w", err)
    }

    logger.Info("ETCD OpenTelemetry 追踪已启用")
    return cli, nil
}
```

### 通用 gRPC 客户端集成

```go
func NewGRPCClient(target string) (*grpc.ClientConn, error) {
    conn, err := grpc.Dial(
        target,
        grpc.WithTransportCredentials(insecure.NewCredentials()),
        // 注入客户端拦截器
        grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
    )
    if err != nil {
        return nil, fmt.Errorf("连接 gRPC 服务失败: %w", err)
    }

    logger.Info("gRPC Client OpenTelemetry 追踪已启用: %s", target)
    return conn, nil
}
```

### 使用方式

```go
// ETCD 操作
func (s *ConfigService) Get(ctx context.Context, key string) (string, error) {
    // 传递 context，自动创建子 span
    resp, err := s.etcdClient.Get(ctx, key)
    if err != nil {
        return "", err
    }

    if len(resp.Kvs) == 0 {
        return "", ErrNotFound
    }

    return string(resp.Kvs[0].Value), nil
}

func (s *ConfigService) Put(ctx context.Context, key, value string) error {
    _, err := s.etcdClient.Put(ctx, key, value)
    return err
}
```

### Jaeger 显示效果

**Span 名称**：`etcdserverpb.KV/Get`、`etcdserverpb.KV/Put`

**Span 属性**：

| 属性                   | 说明        | 示例                   |
| ---------------------- | ----------- | ---------------------- |
| `rpc.system`           | RPC 系统    | `grpc`                 |
| `rpc.service`          | 服务名称    | `etcdserverpb.KV`      |
| `rpc.method`           | 方法名称    | `Get`, `Put`, `Delete` |
| `rpc.grpc.status_code` | gRPC 状态码 | `0` (OK)               |
| `net.peer.name`        | 服务端地址  | `etcd-server`          |
| `net.peer.port`        | 服务端端口  | `2379`                 |

### Trace 示例

```txt
▼ POST /api/v1/config                      [150ms]
  │
  ├─▶ etcdserverpb.KV/Get                  [30ms]
  │   rpc.system: grpc
  │   rpc.service: etcdserverpb.KV
  │   rpc.method: Get
  │   rpc.grpc.status_code: 0
  │
  └─▶ etcdserverpb.KV/Put                  [40ms]
      rpc.system: grpc
      rpc.service: etcdserverpb.KV
      rpc.method: Put
      rpc.grpc.status_code: 0
```

## 常见问题

### 问题 1: gRPC Server 没有创建 Span

**原因**：使用了旧的 Interceptor 方式而不是 StatsHandler

**错误示例**：

```go
// 旧方式（不推荐）
server := grpc.NewServer(
    grpc.UnaryInterceptor(otelgrpc.UnaryServerInterceptor()),
    grpc.StreamInterceptor(otelgrpc.StreamServerInterceptor()),
)
```

**正确示例**：

```go
// 新方式（推荐）
server := grpc.NewServer(
    grpc.StatsHandler(otelgrpc.NewServerHandler()),
)
```

### 问题 2: ETCD 操作没有显示为独立节点

**原因**：创建 ETCD 客户端时没有注入拦截器

**解决方法**：

```go
cli, err := clientv3.New(clientv3.Config{
    Endpoints: endpoints,
    DialOptions: []grpc.DialOption{
        grpc.WithStatsHandler(otelgrpc.NewClientHandler()), // 必须添加
    },
})
```

### 问题 3: gRPC 调用链断开

**原因**：Handler 中没有传递 context

**错误示例**：

```go
func (s *Service) Handle(ctx context.Context, req *pb.Request) (*pb.Response, error) {
    // 错误：使用 context.Background() 创建新的 context
    result, err := s.repo.Query(context.Background(), req.Id)
    return &pb.Response{Data: result}, err
}
```

**正确示例**：

```go
func (s *Service) Handle(ctx context.Context, req *pb.Request) (*pb.Response, error) {
    // 正确：传递接收到的 ctx
    result, err := s.repo.Query(ctx, req.Id)
    return &pb.Response{Data: result}, err
}
```

### 问题 4: Span 数量过多导致性能问题

**解决方法**：使用限流过滤器

```go
// internal/common/telemetry/grpc_limiter.go
type limitedStatsHandler struct {
    handler stats.Handler
    limiter *rate.Limiter
}

func NewLimitedStatsHandler(handler stats.Handler, rps int) stats.Handler {
    return &limitedStatsHandler{
        handler: handler,
        limiter: rate.NewLimiter(rate.Limit(rps), rps),
    }
}

func (h *limitedStatsHandler) HandleRPC(ctx context.Context, s stats.RPCStats) {
    if h.limiter.Allow() {
        h.handler.HandleRPC(ctx, s)
    }
}

// 使用限流
server := grpc.NewServer(
    grpc.StatsHandler(NewLimitedStatsHandler(
        otelgrpc.NewServerHandler(),
        100, // 限制为 100 RPS
    )),
)
```

## 验证清单

### gRPC Server

- [ ] 使用 `grpc.StatsHandler(otelgrpc.NewServerHandler())`
- [ ] 启动日志显示 "gRPC Server OpenTelemetry 追踪已启用"
- [ ] Handler 中传递 context 给下游
- [ ] Jaeger 中能看到 gRPC 请求 Span
- [ ] Span 包含 `rpc.system`、`rpc.service`、`rpc.method` 属性

### gRPC Client

- [ ] 客户端初始化时注入 `otelgrpc.NewClientHandler()`
- [ ] 启动日志显示 "ETCD/gRPC Client OpenTelemetry 追踪已启用"
- [ ] 所有调用都传递 context
- [ ] Jaeger 中能看到 ETCD/gRPC 调用 Span
- [ ] Span 显示为独立节点

## 参考资料

- otelgrpc 文档: https://pkg.go.dev/go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc
- gRPC Go 文档: https://grpc.io/docs/languages/go/
- OpenTelemetry RPC Semantic Conventions: https://opentelemetry.io/docs/specs/semconv/rpc/
- ETCD Client 文档: https://etcd.io/docs/v3.5/dev-guide/interacting_v3/
