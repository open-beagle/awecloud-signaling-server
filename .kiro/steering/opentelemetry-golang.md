# Go 应用 OpenTelemetry 接入规范

本规范基于项目现有实现，指导 Go 应用如何接入 OpenTelemetry 追踪。

## 核心架构原则

### 调用链起点与下游

```
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
│         ┌─────────────────┐                                     │
│         │      GORM       │    ← 下游（只创建子 span）          │
│         │  (otelgorm 插件) │       必须从上游接收 context        │
│         └─────────────────┘                                     │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

**关键规则：**

| 组件 | 角色 | 行为                                                  |
| ---- | ---- | ----------------------------------------------------- |
| HTTP | 入口 | 从请求头提取上游 trace context，若无则创建根 span     |
| gRPC | 入口 | 从 metadata 提取上游 trace context，若无则创建根 span |
| GORM | 下游 | 只在收到 context 时创建子 span，绝不独立创建          |

### 跨服务 Trace 传播

HTTP 和 gRPC 入口会自动从上游服务接收 trace context：

- **HTTP**: 从 `traceparent`、`tracestate` 等标准 W3C Trace Context 请求头提取
- **gRPC**: 从 gRPC metadata 中提取 trace context

这意味着：

1. 如果上游服务传递了 trace context，本服务的 span 会作为子 span 关联到上游
2. 如果没有上游 trace context，本服务会创建新的根 span

**GORM 绝不作为起点**：如果 GORM 调用没有传递 context，则不会产生任何 trace 数据，这是设计预期。

### GORM 孤立 Span 防护

otelgorm 默认会为所有查询创建 span，即使没有父 span 也会创建孤立的根 span。这会导致 Jaeger 中出现大量无意义的孤立 trace。

**解决方案：** 在 `EnableTracing()` 中注册 GORM 回调，在执行查询前检查 context 是否包含有效的父 span，如果没有则跳过 trace 创建。

**实现原理：**

1. 在 otelgorm 的 before 回调之前注册自定义回调
2. 检查 `trace.SpanFromContext(ctx).SpanContext().IsValid()`
3. 如果无效，替换为干净的 `context.Background()`，otelgorm 就不会创建 span

## 配置方式

### TOML 配置文件

```toml
[telemetry]
# OTLP Endpoint，设置后自动启用追踪
# http:// 前缀使用非安全连接，https:// 前缀使用 TLS
# 不指定端口时：https 默认 443，http 默认 80
endpoint = "https://otel.example.com"

# 服务名称（可选，默认 signaling-server）
service_name = "my-service"

# 服务命名空间（可选，默认 default）
namespace = "my-namespace"
```

### 环境变量覆盖

环境变量优先级高于配置文件：

| 环境变量                      | 说明          | 示例                       |
| ----------------------------- | ------------- | -------------------------- |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | OTLP Endpoint | `https://otel.example.com` |
| `OTEL_SERVICE_NAME`           | 服务名称      | `signaling-server`         |
| `OTEL_SERVICE_NAMESPACE`      | 服务命名空间  | `default`                  |

### Endpoint 格式说明

```
https://otel.example.com      → TLS 连接，端口 443
https://otel.example.com:4317 → TLS 连接，端口 4317
http://otel-collector:4317    → 非安全连接，端口 4317
http://otel-collector         → 非安全连接，端口 80
```

## 追踪组件

| 组件 | 依赖包                                                                        | 角色 | 说明                    |
| ---- | ----------------------------------------------------------------------------- | ---- | ----------------------- |
| HTTP | `go.opentelemetry.io/otel`                                                    | 起点 | Gin 中间件，自定义实现  |
| gRPC | `go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc` | 起点 | gRPC StatsHandler       |
| GORM | `github.com/uptrace/opentelemetry-go-extra/otelgorm`                          | 下游 | GORM 插件，依赖 context |

## Context 传递规范

### HTTP Handler（Gin）

```go
func (a *XxxAPI) List(c *gin.Context) {
    // 第一行：从 Gin context 获取 trace context
    ctx := c.Request.Context()

    // 所有 GORM 调用必须使用 WithContext
    db.DB.WithContext(ctx).Model(&model.Xxx{}).Find(&items)
    db.DB.WithContext(ctx).Where("id = ?", id).First(&item)
    db.DB.WithContext(ctx).Create(&item)
    db.DB.WithContext(ctx).Save(&item)
    db.DB.WithContext(ctx).Delete(&item)
}
```

### gRPC Handler

```go
func (s *XxxServiceServer) Method(ctx context.Context, req *pb.Request) (*pb.Response, error) {
    // ctx 已由 gRPC StatsHandler 注入 trace context
    // 直接使用即可

    // 所有 GORM 调用必须使用 WithContext
    db.DB.WithContext(ctx).First(&item, id)
    db.DB.WithContext(ctx).Create(&item)
}
```

### 后台任务 / 定时任务

```go
func (s *BackgroundService) DoWork() {
    // 后台任务没有上游请求，使用 context.Background()
    // 此时 GORM 不会产生 trace（符合预期）
    ctx := context.Background()

    db.DB.WithContext(ctx).Find(&items)
}
```

## 调用链示意

### HTTP 请求调用链

```
┌──────────────────────────────────────────────────────────────┐
│  HTTP Request: GET /api/v1/admin/users                       │
│  ┌────────────────────────────────────────────────────────┐  │
│  │  Gin Middleware (telemetry.GinMiddleware)              │  │
│  │  创建根 span: "GET /api/v1/admin/users"                │  │
│  │  ┌──────────────────────────────────────────────────┐  │  │
│  │  │  API Handler: UserAPI.List()                     │  │  │
│  │  │  ctx := c.Request.Context()                      │  │  │
│  │  │  ┌────────────────────────────────────────────┐  │  │  │
│  │  │  │  db.DB.WithContext(ctx).Find(&users)      │  │  │  │
│  │  │  │  子 span: "SELECT users"                   │  │  │  │
│  │  │  └────────────────────────────────────────────┘  │  │  │
│  │  │  ┌────────────────────────────────────────────┐  │  │  │
│  │  │  │  db.DB.WithContext(ctx).Count(&total)     │  │  │  │
│  │  │  │  子 span: "SELECT COUNT users"            │  │  │  │
│  │  │  └────────────────────────────────────────────┘  │  │  │
│  │  └──────────────────────────────────────────────────┘  │  │
│  └────────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────────┘
```

### gRPC 请求调用链

```
┌──────────────────────────────────────────────────────────────┐
│  gRPC Request: /proto.AgentService/Connect                   │
│  ┌────────────────────────────────────────────────────────┐  │
│  │  gRPC StatsHandler (otelgrpc.NewServerHandler)         │  │
│  │  创建根 span: "/proto.AgentService/Connect"            │  │
│  │  ┌──────────────────────────────────────────────────┐  │  │
│  │  │  AgentServiceServer.Connect(ctx, req)            │  │  │
│  │  │  ctx 已包含 trace context                        │  │  │
│  │  │  ┌────────────────────────────────────────────┐  │  │  │
│  │  │  │  db.DB.WithContext(ctx).First(&user)      │  │  │  │
│  │  │  │  子 span: "SELECT user"                    │  │  │  │
│  │  │  └────────────────────────────────────────────┘  │  │  │
│  │  └──────────────────────────────────────────────────┘  │  │
│  └────────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────────┘
```

## 初始化流程

```
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
┌─────────────────┐
│ db.EnableTracing│  ← 注册 otelgorm 插件（下游）
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│  gRPC Server    │  ← 注册 StatsHandler（起点）
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│  Gin Router     │  ← 注册中间件（起点）
└─────────────────┘
```

## 文件位置

| 文件                                     | 说明                      |
| ---------------------------------------- | ------------------------- |
| `internal/common/telemetry/telemetry.go` | 核心初始化逻辑            |
| `internal/common/telemetry/gin.go`       | Gin HTTP 中间件（起点）   |
| `internal/server/server.go`              | gRPC StatsHandler（起点） |
| `internal/server/db/db.go`               | GORM 追踪插件（下游）     |
| `internal/common/config/server.go`       | TelemetrySection 配置     |
| `cmd/server/main.go`                     | 初始化入口                |

## 健康检查服务排除

健康检查相关的 HTTP 服务和接口不需要添加链路监控。

**排除原则：**

1. **纯健康检查 HTTP 服务**：如果一个 HTTP 服务只包含健康检查相关接口（如 `/health`、`/health/ready`），其目的仅是供 K8s 探测，则整个服务不添加 Gin telemetry 中间件

2. **混合服务中的健康检查接口**：如果 HTTP 服务包含业务接口和健康检查接口，Gin 中间件应自动跳过健康检查路径：`/health`、`/health/ready`

**原因：**

- 健康检查请求频繁（K8s liveness/readiness probe），会产生大量无意义的 trace
- 健康检查逻辑简单，不涉及数据库或外部服务调用
- 这些 trace 数据对排查业务问题没有帮助，只会增加存储和查询负担

## K8s 部署配置

```yaml
env:
  - name: OTEL_EXPORTER_OTLP_ENDPOINT
    value: "http://otel-collector.beagle-devops:4317"
  - name: OTEL_SERVICE_NAME
    value: "signaling-server"
  - name: OTEL_SERVICE_NAMESPACE
    value: "default"
```

## 验证追踪

启动服务后，检查日志确认初始化成功：

```
OpenTelemetry 初始化成功: endpoint=https://otel.example.com, service=signaling-server, namespace=default, tls=true
GORM OpenTelemetry 追踪已启用
gRPC OpenTelemetry 追踪已启用
```

在 Jaeger UI 中应能看到完整调用链：

- 根 span: HTTP 请求（如 `GET /api/v1/admin/users`）或 gRPC 调用
- 子 span: SQL 查询（如 `SELECT`、`INSERT`）

## Request/Response Body 记录规范

为便于调试，HTTP 和 gRPC 的 span 应记录请求和响应内容。

### 记录的 Tag

| 协议 | Tag                       | 说明                             |
| ---- | ------------------------- | -------------------------------- |
| HTTP | `http.request.url`        | 完整请求 URL（含路径和查询参数） |
| HTTP | `http.request.body`       | 请求体（脱敏后）                 |
| HTTP | `http.response.body`      | 响应体（脱敏后）                 |
| HTTP | `http.request.body_size`  | 原始请求体大小（字节）           |
| HTTP | `http.response.body_size` | 原始响应体大小（字节）           |
| gRPC | `rpc.request.body`        | 请求消息 JSON（脱敏后）          |
| gRPC | `rpc.response.body`       | 响应消息 JSON（脱敏后）          |

### Body 大小限制

| 规则           | 值   | 说明                                  |
| -------------- | ---- | ------------------------------------- |
| 最大记录长度   | 4KB  | 超过则截断，末尾标记 `...[truncated]` |
| 超大 Body 阈值 | 64KB | 超过此值只记录大小，不记录内容        |

### 敏感字段脱敏

**脱敏字段列表（不区分大小写）：**

- `password`、`passwd`、`pwd`
- `token`、`access_token`、`refresh_token`
- `secret`、`api_key`、`apikey`
- `authorization`
- `credential`、`credentials`

**脱敏规则：**

- JSON 字段值替换为 `[REDACTED]`
- 保留字段名，只隐藏值
- 递归处理嵌套对象和数组

### Content-Type 过滤

只记录以下类型的 Body 内容：

- `application/json`
- `application/x-www-form-urlencoded`
- `text/plain`

二进制类型（`multipart/form-data`、`application/octet-stream`、`image/*` 等）只记录大小，Body 内容标记为 `[binary]`。

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

## 常见问题

### Q: 为什么 GORM 查询没有出现在 trace 中？

A: 检查是否传递了 context：

- ❌ `db.DB.Find(&items)` - 没有 context，不产生 trace
- ✅ `db.DB.WithContext(ctx).Find(&items)` - 正确传递 context

### Q: 后台任务的 GORM 查询会产生 trace 吗？

A: 不会。后台任务使用 `context.Background()`，没有上游 span，GORM 不会创建独立的根 span。这是设计预期，避免产生孤立的 trace 数据。

### Q: 如何为后台任务创建 trace？

A: 如果需要追踪后台任务，手动创建根 span：

```go
func (s *BackgroundService) DoWork() {
    ctx, span := telemetry.StartSpan(context.Background(), "background.DoWork")
    defer span.End()

    db.DB.WithContext(ctx).Find(&items)  // 现在会产生子 span
}
```

## gRPC 客户端限流规范

### 设计原则

gRPC 客户端（如 Agent）的所有调用都应追踪，但通过**动态限流**控制数据量。核心思想是自动识别高频调用并限流，而非预先配置固定规则。

### 高频定义与检测

**高频调用**：同一方法在 1 分钟内调用次数超过阈值。

| 参数     | 值         | 说明                         |
| -------- | ---------- | ---------------------------- |
| 检测窗口 | 1 分钟     | 滑动窗口统计周期             |
| 高频阈值 | 10 次/分钟 | 超过此值触发限流             |
| 限流上限 | 10 条/分钟 | 高频方法最多记录 10 条 trace |

**检测方式**：滑动窗口计数器，按方法名独立统计。

### 动态限流流程

```
┌─────────────────────────────────────────────────────────────┐
│                      gRPC 调用                              │
└─────────────────────┬───────────────────────────────────────┘
                      │
                      ▼
┌─────────────────────────────────────────────────────────────┐
│  1. 计数器递增（按方法名，滑动窗口 1 分钟）                 │
└─────────────────────┬───────────────────────────────────────┘
                      │
                      ▼
┌─────────────────────────────────────────────────────────────┐
│  2. 检查当前分钟内该方法的 trace 记录数                     │
│     - 已记录数 < 10：记录 trace                             │
│     - 已记录数 ≥ 10：跳过 trace（调用正常执行）             │
└─────────────────────────────────────────────────────────────┘
```

### 限流效果示例

| 场景         | 实际调用频率 | trace 记录数 | 说明                 |
| ------------ | ------------ | ------------ | -------------------- |
| 正常心跳     | 2 次/分钟    | 2 条/分钟    | 未触发限流           |
| 异常重连     | 60 次/分钟   | 10 条/分钟   | 触发限流，丢弃 50 条 |
| 正常状态上报 | 5 次/分钟    | 5 条/分钟    | 未触发限流           |
| 状态抖动     | 100 次/分钟  | 10 条/分钟   | 触发限流，丢弃 90 条 |
| 启动注册     | 1 次/天      | 1 条/天      | 未触发限流           |

### 实现要点

**滑动窗口计数器**：

- 数据结构：每个方法维护一个计数器和时间戳
- 窗口重置：当前时间与时间戳差值超过 1 分钟时重置计数
- 并发安全：使用 sync.Mutex 保护

**限流决策**：

- 时机：在 otelgrpc Filter 回调中判断
- 返回 false 跳过 trace，但不影响 gRPC 调用本身

**内存占用**：

- 每个方法约 16 字节（int64 计数 + int64 时间戳）
- Agent 通常 4-5 个方法，可忽略

### 日志输出

限流触发时输出警告（每分钟每方法最多一次）：

```
gRPC trace 限流: method=/proto.AgentService/Heartbeat, calls=60, traced=10
```

### 文件位置

| 文件                                        | 说明                |
| ------------------------------------------- | ------------------- |
| `internal/common/telemetry/grpc_limiter.go` | gRPC 限流过滤器实现 |
| `internal/agent/agent.go`                   | Agent gRPC 连接配置 |
