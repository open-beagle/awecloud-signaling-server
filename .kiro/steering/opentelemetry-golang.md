# Go 应用 OpenTelemetry 接入规范

本规范指导 Go 应用如何接入 OpenTelemetry 追踪。

详细的组件规范请参考：

- HTTP (Gin): #[[file:opentelemetry-golang-http.md]]
- gRPC: #[[file:opentelemetry-golang-grpc.md]]
- GORM: #[[file:opentelemetry-golang-gorm.md]]
- XORM: #[[file:opentelemetry-golang-xorm.md]]

## 核心架构

```txt
┌─────────────────────────────────────────────────────────────────┐
│                        调用链架构                                │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │  上游服务（可选）                                        │   │
│  │  通过 HTTP Header / gRPC Metadata 传递 trace context    │   │
│  └─────────────────────────┬───────────────────────────────┘   │
│                            │                                    │
│                            ▼                                    │
│  ┌─────────────┐     ┌─────────────┐                           │
│  │    HTTP     │     │    gRPC     │    ← 入口（接收或创建 span）│
│  │  (Gin 中间件) │     │ (StatsHandler)│                         │
│  └──────┬──────┘     └──────┬──────┘                           │
│         │                   │                                   │
│         └─────────┬─────────┘                                   │
│                   │                                             │
│                   ▼                                             │
│         ┌─────────────────────────────────┐                     │
│         │      下游依赖（客户端拦截器）    │                     │
│         ├─────────────────────────────────┤                     │
│         │  GORM (数据库)                  │                     │
│         │  gRPC Client (ETCD/微服务)      │                     │
│         │  Redis Client                   │                     │
│         │  HTTP Client (外部 API)         │                     │
│         └─────────────────────────────────┘                     │
│                   ↓                                             │
│         在 Jaeger 中显示为独立节点                              │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

**关键规则：**

| 组件         | 角色 | 行为                                                  |
| ------------ | ---- | ----------------------------------------------------- |
| HTTP Server  | 入口 | 从请求头提取上游 trace context，若无则创建根 span     |
| gRPC Server  | 入口 | 从 metadata 提取上游 trace context，若无则创建根 span |
| GORM/XORM    | 下游 | 只在收到 context 时创建子 span，绝不独立创建          |
| gRPC Client  | 下游 | 通过拦截器创建子 span，在 Jaeger 中显示为独立节点     |
| Redis Client | 下游 | 通过拦截器创建子 span，在 Jaeger 中显示为独立节点     |
| HTTP Client  | 下游 | 通过拦截器创建子 span，在 Jaeger 中显示为独立节点     |

## 配置方式

### TOML 配置文件

```toml
[telemetry]
# OTLP Endpoint，设置后自动启用追踪
# http:// 前缀使用非安全连接，https:// 前缀使用 TLS
# 不指定端口时：https 默认 443，http 默认 80
endpoint = "https://otel.example.com"

# 服务名称（可选，默认 your-service）
service_name = "my-service"

# 服务命名空间（可选，默认 default）
namespace = "my-namespace"
```

### 环境变量覆盖

环境变量优先级高于配置文件：

| 环境变量                      | 说明                          | 示例                       |
| ----------------------------- | ----------------------------- | -------------------------- |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | OTLP Endpoint                 | `https://otel.example.com` |
| `OTEL_SERVICE_NAME`           | 服务名称                      | `your-service`             |
| `OTEL_SERVICE_NAMESPACE`      | 服务命名空间                  | `default`                  |
| `OTEL_SERVICE_CLUSTER`        | 集群标识（业务集群/数据来源） | `default`                  |

**注意**：`OTEL_SERVICE_CLUSTER` 表示服务所在的业务集群（数据来源），在 Jaeger 中显示为 `service.cluster` tag。这与 `k8s.cluster.name` 不同，后者表示 OpenTelemetry Collector 所在的 K8s 集群。

### TLS 配置说明

HTTPS 连接默认跳过证书验证（`InsecureSkipVerify: true`），以支持自签名证书场景。这适用于内网环境中的 OTLP Collector。

### Endpoint 格式说明

```txt
https://otel.example.com      → TLS 连接，端口 443
https://otel.example.com:4317 → TLS 连接，端口 4317
http://otel-collector:4317    → 非安全连接，端口 4317
http://otel-collector         → 非安全连接，端口 80
```

## 追踪组件

### 服务端（入口）

| 组件        | 依赖包                                                                        | 角色 | 说明                   |
| ----------- | ----------------------------------------------------------------------------- | ---- | ---------------------- |
| HTTP Server | `go.opentelemetry.io/otel`                                                    | 入口 | Gin 中间件，自定义实现 |
| gRPC Server | `go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc` | 入口 | gRPC StatsHandler      |

### 客户端（下游依赖）

| 组件        | 依赖包                                                                        | 角色 | 说明                                      |
| ----------- | ----------------------------------------------------------------------------- | ---- | ----------------------------------------- |
| GORM        | `github.com/uptrace/opentelemetry-go-extra/otelgorm`                          | 下游 | GORM 插件，依赖 context                   |
| XORM        | 自定义 Hook 实现                                                              | 下游 | XORM Hook，依赖 context                   |
| gRPC Client | `go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc` | 下游 | gRPC 客户端拦截器，用于 ETCD 等 gRPC 服务 |
| Redis       | `github.com/redis/go-redis/extra/redisotel/v9`                                | 下游 | Redis 客户端拦截器                        |
| HTTP Client | `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp`               | 下游 | HTTP 客户端拦截器，用于外部 API 调用      |

## 依赖版本规范

所有项目必须使用统一的 OpenTelemetry 版本，避免 Schema URL 冲突。

### 核心依赖

```go
require (
    go.opentelemetry.io/otel v1.39.0
    go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.39.0
    go.opentelemetry.io/otel/sdk v1.39.0
    go.opentelemetry.io/otel/trace v1.39.0
)
```

### 客户端拦截器依赖

```go
require (
    // GORM
    github.com/uptrace/opentelemetry-go-extra/otelgorm v0.3.2

    // gRPC Client (ETCD 等)
    go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.58.0

    // Redis
    github.com/redis/go-redis/extra/redisotel/v9 v9.7.0

    // HTTP Client
    go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.58.0
)
```

### semconv 版本

使用与 OTEL SDK 匹配的 semconv 版本：

```go
import semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
```

**重要**：

- 不要使用 `resource.Default()`，它会引入不同版本的 Schema URL
- 直接使用 `resource.NewWithAttributes()` 创建 Resource
- 必须设置 `otel.SetErrorHandler()` 将 OpenTelemetry 内部错误输出到 zap，避免使用标准库 `log` 包

### 升级命令

```bash
# 核心依赖
go get go.opentelemetry.io/otel@v1.39.0
go get go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc@v1.39.0
go get go.opentelemetry.io/otel/sdk@v1.39.0
go get go.opentelemetry.io/otel/trace@v1.39.0

# 客户端拦截器
go get github.com/uptrace/opentelemetry-go-extra/otelgorm@v0.3.2
go get go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc@v0.58.0
go get github.com/redis/go-redis/extra/redisotel/v9@v9.7.0
go get go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp@v0.58.0

go mod tidy
```

## 初始化流程

```txt
┌─────────────────┐
│  加载配置       │
│  (TOML + ENV)   │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│  telemetry.Init │  ← 初始化 TracerProvider
└────────┬────────┘
         │
         ▼
┌─────────────────────────────────────┐
│  注册客户端拦截器（下游依赖）        │
├─────────────────────────────────────┤
│  • db.EnableTracing (GORM)          │
│  • etcd.NewClient (gRPC Interceptor)│
│  • redis.NewClient (redisotel)      │
│  • http.NewClient (otelhttp)        │
└────────┬────────────────────────────┘
         │
         ▼
┌─────────────────┐
│  gRPC Server    │  ← 注册 StatsHandler（入口）
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│  Gin Router     │  ← 注册中间件（入口）
└─────────────────┘
```

## 文件位置

| 文件                                        | 说明              |
| ------------------------------------------- | ----------------- |
| `internal/common/telemetry/telemetry.go`    | 核心初始化逻辑    |
| `internal/common/telemetry/gin.go`          | Gin HTTP 中间件   |
| `internal/common/telemetry/grpc_limiter.go` | gRPC 限流过滤器   |
| `internal/server/server.go`                 | gRPC StatsHandler |
| `internal/server/db/db.go`                  | GORM 追踪插件     |
| `internal/common/config/server.go`          | TelemetrySection  |
| `cmd/server/main.go`                        | 初始化入口        |

## K8s 部署配置

```yaml
env:
  - name: OTEL_EXPORTER_OTLP_ENDPOINT
    value: "http://otel-collector.beagle-devops:4317"
  - name: OTEL_SERVICE_NAME
    value: "your-service"
  - name: OTEL_SERVICE_NAMESPACE
    value: "default"
  - name: OTEL_SERVICE_CLUSTER
    value: "default"
```

## 验证追踪

启动服务后，检查日志确认初始化成功：

```txt
OpenTelemetry 初始化成功: endpoint=https://otel.example.com, service=your-service, namespace=default, tls=true
GORM OpenTelemetry 追踪已启用
gRPC OpenTelemetry 追踪已启用
```

在 Jaeger UI 中应能看到完整调用链：

- 根 span: HTTP 请求（如 `GET /api/v1/admin/users`）或 gRPC 调用
- 子 span: SQL 查询（如 `SELECT`、`INSERT`）

## Process 版本信息规范

初始化时应将应用构建信息写入 Resource Attributes，便于在 Jaeger 中识别服务版本。

### 必须记录的 Attributes

| Attribute            | 说明          | 示例                  |
| -------------------- | ------------- | --------------------- |
| `service.version`    | 应用版本号    | `v0.2.2`              |
| `service.git_commit` | Git 提交哈希  | `1b70d34`             |
| `service.build_date` | 构建日期      | `2026-01-18_01:47:51` |
| `go.version`         | Go 编译器版本 | `go1.25.5`            |

### 初始化方式

`telemetry.Init()` 需要接收构建信息参数，在创建 Resource 时添加这些属性。构建信息通常通过 `-ldflags` 在编译时注入。

## 客户端拦截器集成

客户端拦截器用于追踪下游依赖（数据库、缓存、外部服务等），使其在 Jaeger 中显示为独立节点。

### 核心原则

1. **Context 传递**：所有客户端操作必须传递 `context.Context`
2. **自动注入**：使用官方插件自动注入 Semantic Conventions 属性
3. **节点识别**：Jaeger 根据 `db.system`、`rpc.system` 等属性识别节点类型

### 集成指南

详细的集成方式请参考各组件的专门文档：

- **GORM 数据库追踪**：#[[file:opentelemetry-golang-gorm.md]]
  - 使用 `otelgorm.NewPlugin()` 注册插件
  - 查询时使用 `db.WithContext(ctx)`
  - Span 名称：`SELECT`、`INSERT`、`UPDATE`、`DELETE`

- **gRPC Client 追踪**（ETCD 等）：#[[file:opentelemetry-golang-grpc.md]]
  - 使用 `grpc.WithStatsHandler(otelgrpc.NewClientHandler())`
  - 所有调用传递 context
  - Span 名称：`etcdserverpb.KV/Get`、`etcdserverpb.KV/Put`

- **Redis 追踪**：#[[file:opentelemetry-golang-redis.md]]
  - 使用 `redisotel.InstrumentTracing(rdb)`
  - 所有操作传递 context
  - Span 名称：`redis.get`、`redis.set`、`redis.del`

- **HTTP Client 追踪**：#[[file:opentelemetry-golang-http.md]]
  - 使用 `otelhttp.NewTransport(http.DefaultTransport)`
  - 使用 `http.NewRequestWithContext(ctx, ...)`
  - Span 名称：`HTTP GET`、`HTTP POST`

- **XORM 追踪**：#[[file:opentelemetry-golang-xorm.md]]
  - 自定义 Hook 实现
  - 查询时使用 `engine.Context(ctx)`
  - Span 名称：SQL 语句（截断至 100 字符）

### 快速参考

| 组件        | 初始化方法                         | Context 传递方式             |
| ----------- | ---------------------------------- | ---------------------------- |
| GORM        | `db.Use(otelgorm.NewPlugin())`     | `db.WithContext(ctx)`        |
| gRPC Client | `grpc.WithStatsHandler(...)`       | 方法参数自动传递             |
| Redis       | `redisotel.InstrumentTracing(rdb)` | `redis.Get(ctx, key)`        |
| HTTP Client | `otelhttp.NewTransport(...)`       | `http.NewRequestWithContext` |
| XORM        | `engine.AddHook(NewXORMHook())`    | `engine.Context(ctx)`        |

## Context 传递最佳实践

### 规则

1. **入口层**：从 `gin.Context` 或 gRPC `context.Context` 获取
2. **传递链**：通过函数参数一直传递到最底层
3. **客户端调用**：所有客户端操作必须使用带 context 的方法

### 调用链示例

```txt
HTTP/gRPC Handler (入口)
    ↓ ctx := c.Request.Context()
Service 层
    ↓ 传递 ctx 参数
Repository 层
    ↓ db.WithContext(ctx) / engine.Context(ctx)
数据库 / 缓存 / 外部服务
```

### 检查 Context 有效性

可以在关键位置检查 context 是否包含有效的 Span：

```go
import "go.opentelemetry.io/otel/trace"

span := trace.SpanFromContext(ctx)
if !span.IsRecording() {
    logger.Warn("Context 中没有活跃的 Span，追踪链路可能断开")
}
```

## 验证拓扑图

### 启动日志检查

服务启动后应看到所有拦截器的初始化日志：

```txt
OpenTelemetry 初始化成功: endpoint=https://otel.example.com, service=your-service
GORM OpenTelemetry 追踪已启用
ETCD OpenTelemetry 追踪已启用
Redis OpenTelemetry 追踪已启用
gRPC OpenTelemetry 追踪已启用
```

### Jaeger Trace 检查

执行一个完整的业务流程后，在 Jaeger UI 中应看到多个 Span：

```txt
▼ GET /api/v1/users/:id                    [200ms]
  │
  ├─▶ SELECT * FROM users WHERE id=?       [50ms]
  │   (db.system: sqlite)
  │
  ├─▶ etcdserverpb.KV/Get                  [30ms]
  │   (rpc.system: grpc)
  │
  └─▶ redis.set user:cache:123             [10ms]
      (db.system: redis)
```

### Service Map 检查

在 Jaeger 的 System Architecture 或 Service Map 视图中应看到完整的依赖关系图：

```txt
[your-service] ──▶ [sqlite/mysql/postgres]
      │
      ├──▶ [etcdserverpb.KV]
      │
      ├──▶ [redis]
      │
      └──▶ [external-api]
```

### 常见问题

| 问题                     | 原因                      | 解决方法                               |
| ------------------------ | ------------------------- | -------------------------------------- |
| 只看到 1 个 Span         | Context 没有传递          | 检查所有函数调用是否传递 `ctx`         |
| 看到 Span 但没有节点图标 | 缺少 Semantic Conventions | 使用官方插件（otelgorm、redisotel 等） |
| Schema URL 冲突          | OpenTelemetry 版本不一致  | 统一所有依赖版本到 v1.39.0             |
| Span 数量过多            | 没有采样或限流            | 配置采样率或使用 `grpc_limiter.go`     |
| 数据库 Span 没有 SQL     | 插件配置错误              | 参考对应组件的集成文档                 |
| 调用链断开               | 某个环节没有传递 context  | 使用 `trace.SpanFromContext` 检查      |
