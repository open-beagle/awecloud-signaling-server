---
inclusion: manual
---

# OpenTelemetry Trace 规范

## 问题背景

在可观测性系统中发现大量非法的 Trace 记录，这些 Trace 的 Root Span 不是合法的业务入口。

**非法 Trace 示例**：

- ❌ Root Span: `get` (kind=Client, db.system=redis) - Redis 操作不应该是入口
- ❌ Root Span: `SELECT * FROM users` (kind=Client, db.system=postgresql) - 数据库操作不应该是入口
- ❌ Root Span: `grpc.health.v1.Health/Check` (kind=Client) - 健康检查不应该是入口

**合法 Trace 示例**：

- ✅ Root Span: `GET /api/users` (kind=Server) - HTTP 请求
- ✅ Root Span: `UserService.GetUser` (kind=Server) - gRPC 业务调用
- ✅ Root Span: `order-created` (kind=Consumer) - 消息队列消费

## 核心原则

### Root Span 必须是合法的业务入口

**过滤策略：只保留 Server 和 Consumer，丢弃其他所有类型**

**合法的 Root Span**：

- ✅ `SpanKindServer` (kind=2) - HTTP/gRPC 服务端
- ✅ `SpanKindConsumer` (kind=5) - 消息队列消费者

**非法的 Root Span（必须丢弃）**：

- ❌ `SpanKindClient` (kind=3) - 客户端调用（数据库、Redis、HTTP 客户端等）
- ❌ `SpanKindProducer` (kind=4) - 消息生产者
- ❌ `SpanKindInternal` (kind=1) - 内部调用
- ❌ `SpanKindUnspecified` (kind=0) - 未指定类型

### 在发送前过滤，而不是在创建时避免

**错误方案**：在每个地方都检查是否应该创建 Span

- ❌ 代码分散，难以维护
- ❌ 容易遗漏，导致非法 Trace 泄漏
- ❌ 性能开销大，每次都要检查

**正确方案**：在 Trace 发送前统一过滤

- ✅ 集中管理，易于维护
- ✅ 不会遗漏，所有 Trace 都经过过滤
- ✅ 性能开销小，只在发送时检查一次

### 过滤规则

#### SpanKind 检查（唯一规则）

Root Span 只能是以下类型：

- ✅ `SpanKindServer` (kind=2) - HTTP/gRPC 服务端
- ✅ `SpanKindConsumer` (kind=5) - 消息队列消费者

其他所有类型都会被丢弃：

- ❌ `SpanKindClient` (kind=3) - 客户端调用
- ❌ `SpanKindProducer` (kind=4) - 消息生产者
- ❌ `SpanKindInternal` (kind=1) - 内部调用
- ❌ `SpanKindUnspecified` (kind=0) - 未指定类型

**为什么这个规则足够？**

1. **数据库操作**：XORM、GORM 等 ORM 库创建的 Span 都是 `SpanKindClient`
2. **Redis 操作**：`redisotel` 库创建的 Span 都是 `SpanKindClient`
3. **HTTP 客户端**：`http.Client` 创建的 Span 都是 `SpanKindClient`
4. **gRPC 客户端**：gRPC 客户端调用创建的 Span 都是 `SpanKindClient`
5. **消息生产者**：Kafka、RabbitMQ 生产者创建的 Span 都是 `SpanKindProducer`

只有真正的业务入口才会创建 `SpanKindServer` 或 `SpanKindConsumer` 的 Span。

## 实施要求

所有 Go 项目**必须**在 Trace 发送前过滤非法的 Root Span。

### 强制要求

1. **必须过滤**：在 Span 发送到后端之前，过滤掉非法的 Root Span
2. **过滤规则**：只保留 `SpanKindServer` 和 `SpanKindConsumer`，丢弃其他所有类型
3. **丢弃策略**：非法的 Root Span 必须直接丢弃，不发送到后端

### 建议要求

1. **监控指标**：记录被丢弃的 Span 数量和原因
2. **告警规则**：监控 Trace 数量异常增长

## 参考资料

- [OpenTelemetry Go SDK](https://pkg.go.dev/go.opentelemetry.io/otel/sdk/trace)
- [OpenTelemetry Semantic Conventions](https://opentelemetry.io/docs/specs/semconv/)
