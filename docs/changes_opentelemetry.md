# OpenTelemetry 全链路追踪实现

## 变更概述

为 AWECloud Signaling Server 系统的三个组件（Server、Agent、Desktop）实现了统一的 OpenTelemetry 全链路追踪，并添加了 Root Span 过滤机制以避免非法 Trace 污染。

## 主要变更

### 1. 新增 RootSpanFilter

文件：

- `internal/common/telemetry/span_filter.go`
- `desktop/internal/telemetry/span_filter.go`

功能：

- 在 Span 发送前过滤非法的 Root Span
- 只保留 SpanKindServer 和 SpanKindConsumer
- 丢弃 SpanKindClient、SpanKindProducer、SpanKindInternal、SpanKindUnspecified
- 记录过滤统计信息

### 2. 集成 RootSpanFilter

文件：

- `internal/common/telemetry/telemetry.go`

变更：

- 在创建 TracerProvider 时使用 RootSpanFilter 包装 OTLP Exporter
- 确保所有导出的 Trace 都经过过滤

### 3. Desktop 组件集成

新增文件：

- `desktop/internal/telemetry/telemetry.go`
- `desktop/internal/telemetry/span_filter.go`

变更文件：

- `desktop/internal/config/config.go`：添加 TelemetryConfig 配置结构
- `desktop/main.go`：初始化 OpenTelemetry，设置日志记录器
- `desktop/go.mod`：添加 OpenTelemetry 依赖

### 4. 配置支持

所有组件都支持通过配置文件启用 OpenTelemetry：

Server (server.toml)：

```toml
[telemetry]
endpoint = "http://otel-collector:4317"
name = "signaling-server"
namespace = "production"
cluster = "aws-us-east-1"
```

Agent (agent.toml)：

```toml
[telemetry]
endpoint = "http://otel-collector:4317"
name = "signaling-agent"
namespace = "production"
cluster = "aws-us-east-1"
```

Desktop (desktop.json)：

```json
{
  "telemetry": {
    "endpoint": "http://otel-collector:4317",
    "name": "signaling-desktop",
    "namespace": "production",
    "cluster": "default"
  }
}
```

## 实例区分

通过 Resource Attributes 区分不同实例：

- Server：无额外属性（单实例）
- Agent：service.node（如 agent-prod-01）
- Desktop：service.user（如 user@example.com）

## 过滤效果

过滤前的非法 Trace 示例：

- Root Span: get (kind=Client, db.system=redis)
- Root Span: SELECT \* FROM users (kind=Client, db.system=postgresql)
- Root Span: grpc.health.v1.Health/Check (kind=Client)

过滤后只保留合法的业务入口：

- Root Span: GET /api/users (kind=Server)
- Root Span: UserService.GetUser (kind=Server)
- Root Span: order-created (kind=Consumer)

## 监控统计

关闭时输出过滤统计：

```
OpenTelemetry: 共丢弃 1234 个非法 Root Span
  - Client: 1000
  - Internal: 200
  - Producer: 34
```

## 性能影响

- 使用 BatchSpanProcessor 批量导出，减少网络开销
- 过滤在发送前进行，不影响 Span 创建性能
- gRPC 追踪限流：每分钟每方法最多 10 条

## 兼容性

- 如果未配置 endpoint，自动跳过初始化
- 初始化失败不影响业务功能
- 向后兼容现有配置文件

## 测试建议

1. 配置 OTLP Collector 端点
2. 启动 Server、Agent、Desktop
3. 执行业务操作（登录、访问服务等）
4. 在追踪后端查看完整的调用链路
5. 验证没有非法的 Root Span

## 相关文档

- 详细设计：`docs/design_opentelemetry_trace.md`
- OpenTelemetry 规范：`.kiro/steering/opentelemetry-trace.md`
