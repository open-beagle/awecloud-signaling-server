# OpenTelemetry 全链路追踪设计

## 概述

本文档描述 AWECloud Signaling Server 系统中 OpenTelemetry 全链路追踪的实现方案，包括 Server、Agent 和 Desktop 三个组件的追踪集成。

## 设计目标

1. 统一的追踪标准：使用 OpenTelemetry 标准实现全链路追踪
2. 过滤非法 Trace：只保留合法的业务入口 Span，避免污染追踪数据
3. 灵活配置：支持通过配置文件启用或禁用追踪
4. 实例区分：通过 Resource Attributes 区分不同的服务实例

## 架构设计

### 组件关系

```
┌─────────────┐
│   Desktop   │ (service.user=user@example.com)
└──────┬──────┘
       │ gRPC (Trace Context 传播)
       ▼
┌─────────────┐
│   Server    │ (service.name=signaling-server)
└──────┬──────┘
       │ gRPC (Trace Context 传播)
       ▼
┌─────────────┐
│    Agent    │ (service.node=agent-prod-01)
└─────────────┘
```

### Trace 传播流程

```
Desktop 发起请求
    │
    ├─ 创建 Root Span (SpanKindServer)
    │  └─ 生成 Trace ID 和 Span ID
    │
    ▼
通过 gRPC 传播 Trace Context
    │
    ├─ 使用 W3C Trace Context 标准
    │  └─ traceparent: 00-{trace-id}-{span-id}-01
    │
    ▼
Server 接收请求
    │
    ├─ 提取 Trace Context
    │  └─ 创建 Child Span (SpanKindServer)
    │
    ▼
Server 调用 Agent
    │
    ├─ 注入 Trace Context
    │  └─ 创建 Child Span (SpanKindClient)
    │
    ▼
Agent 接收请求
    │
    └─ 提取 Trace Context
       └─ 创建 Child Span (SpanKindServer)
```

## Root Span 过滤机制

### 问题背景

在可观测性系统中发现大量非法的 Trace 记录，这些 Trace 的 Root Span 不是合法的业务入口，例如：

- 数据库操作（GORM、XORM）
- Redis 操作
- HTTP 客户端调用
- 健康检查请求

### 过滤策略

只保留以下类型的 Root Span：

- SpanKindServer：HTTP/gRPC 服务端请求
- SpanKindConsumer：消息队列消费者

丢弃以下类型的 Root Span：

- SpanKindClient：客户端调用
- SpanKindProducer：消息生产者
- SpanKindInternal：内部调用
- SpanKindUnspecified：未指定类型

### 实现位置

在 Span 发送到后端之前统一过滤，而不是在创建时避免：

- 集中管理，易于维护
- 不会遗漏，所有 Trace 都经过过滤
- 性能开销小，只在发送时检查一次

### 过滤流程

```
Span 创建
    │
    ▼
Span 完成
    │
    ▼
BatchSpanProcessor 批处理
    │
    ▼
RootSpanFilter 过滤
    │
    ├─ 检查是否为 Root Span
    │  └─ Parent Span ID 是否有效
    │
    ├─ 检查 SpanKind
    │  ├─ Server/Consumer → 保留
    │  └─ 其他类型 → 丢弃
    │
    ▼
OTLP Exporter 导出
    │
    ▼
后端存储
```

## 配置设计

### Server 配置

配置文件：server.toml

```toml
[telemetry]
endpoint = "http://otel-collector:4317"  # OTLP Endpoint
name = "signaling-server"                # 服务名称
namespace = "production"                 # 命名空间
cluster = "aws-us-east-1"               # 集群标识
```

### Agent 配置

配置文件：agent.toml

```toml
[telemetry]
endpoint = "http://otel-collector:4317"
name = "signaling-agent"
namespace = "production"
cluster = "aws-us-east-1"
```

### Desktop 配置

配置文件：desktop.json

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

## Resource Attributes 设计

### 通用属性

所有组件都包含以下属性：

- service.name：服务名称
- service.namespace：服务命名空间
- service.cluster：集群标识
- service.version：应用版本
- service.git_commit：Git 提交哈希
- service.build_date：构建日期
- go.version：Go 编译器版本

### 实例区分属性

不同组件使用不同的属性区分实例：

Server：

- 无额外属性（单实例部署）

Agent：

- service.node：Agent 节点名称（如 agent-prod-01）

Desktop：

- service.user：Desktop 用户标识（如 user@example.com）

### 属性示例

Server 实例：

```
service.name = "signaling-server"
service.namespace = "production"
service.cluster = "aws-us-east-1"
service.version = "v0.2.2"
```

Agent 实例：

```
service.name = "signaling-agent"
service.namespace = "production"
service.cluster = "aws-us-east-1"
service.version = "v0.2.2"
service.node = "agent-prod-01"
```

Desktop 实例：

```
service.name = "signaling-desktop"
service.namespace = "production"
service.cluster = "default"
service.version = "v0.2.2"
service.user = "user@example.com"
```

## 初始化流程

### Server 初始化

```
启动 Server
    │
    ├─ 加载配置文件
    │  └─ 读取 telemetry 配置
    │
    ├─ 初始化 OpenTelemetry
    │  ├─ 创建 OTLP Exporter
    │  ├─ 包装 RootSpanFilter
    │  ├─ 创建 TracerProvider
    │  └─ 设置全局 Propagator
    │
    ├─ 初始化 gRPC Trace 限流器
    │  └─ 每分钟每方法最多 10 条
    │
    └─ 注册 Shutdown Hook
       └─ 优雅关闭 TracerProvider
```

### Agent 初始化

```
启动 Agent
    │
    ├─ 加载配置文件
    │  └─ 读取 telemetry 配置
    │
    ├─ 初始化 OpenTelemetry
    │  ├─ 设置 service.node 属性
    │  └─ 其他步骤同 Server
    │
    └─ 注册 Shutdown Hook
```

### Desktop 初始化

```
启动 Desktop
    │
    ├─ 加载配置文件
    │  └─ 读取 telemetry 配置
    │
    ├─ 设置日志记录器
    │  └─ 实现 telemetry.Logger 接口
    │
    ├─ 初始化 OpenTelemetry
    │  ├─ 设置 service.user 属性
    │  └─ 其他步骤同 Server
    │
    └─ 注册 Shutdown Hook
```

## 监控指标

### 过滤统计

RootSpanFilter 记录以下统计信息：

- 总丢弃数量
- 按 SpanKind 分类的丢弃数量
  - Unspecified
  - Internal
  - Client
  - Producer

### 日志输出

首次丢弃时输出警告日志：

```
OpenTelemetry: 丢弃非法 Root Span: name=get, kind=Client (后续丢弃将不再记录)
```

关闭时输出统计信息：

```
OpenTelemetry: 共丢弃 1234 个非法 Root Span
  - Client: 1000
  - Internal: 200
  - Producer: 34
```

## 性能考虑

### 批处理

使用 BatchSpanProcessor 批量导出 Span：

- 批处理超时：5 秒
- 减少网络请求次数
- 降低对业务的影响

### 采样策略

当前使用 AlwaysSample 策略：

- 所有 Span 都会被记录
- 后续可根据需要调整为概率采样

### gRPC 限流

对 gRPC 追踪进行限流：

- 每分钟每方法最多 10 条
- 避免高频调用产生过多追踪数据
- 保留足够的样本用于问题诊断

## 安全考虑

### TLS 支持

自动判断是否使用 TLS：

- http:// 开头：使用非安全连接
- https:// 开头或无协议前缀：使用 TLS
- 默认端口：TLS 使用 443，非 TLS 使用 80

### 敏感信息

避免在 Span 中记录敏感信息：

- 密码
- Token
- 个人身份信息

## 故障处理

### 初始化失败

如果 OpenTelemetry 初始化失败：

- 记录警告日志
- 继续启动应用
- 不影响业务功能

### 导出失败

如果 Span 导出失败：

- BatchSpanProcessor 会自动重试
- 超时后丢弃 Span
- 不阻塞业务请求

## 未来优化

### 动态采样

根据请求特征动态调整采样率：

- 错误请求：100% 采样
- 慢请求：100% 采样
- 正常请求：低采样率

### 自定义属性

为不同类型的请求添加自定义属性：

- 用户 ID
- 请求来源
- 业务标识

### 指标集成

集成 OpenTelemetry Metrics：

- 请求计数
- 请求延迟
- 错误率

## 参考资料

- OpenTelemetry Go SDK：https://pkg.go.dev/go.opentelemetry.io/otel/sdk/trace
- OpenTelemetry Semantic Conventions：https://opentelemetry.io/docs/specs/semconv/
- W3C Trace Context：https://www.w3.org/TR/trace-context/
