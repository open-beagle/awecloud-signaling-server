# 健康检查接口设计

## 1. 概述

为了支持Kubernetes部署和服务监控，系统需要提供标准的健康检查接口。本文档详细设计Server和Agent的健康检查接口。

## 2. 设计目标

- **Kubernetes就绪性探测（Readiness Probe）**：确定服务是否准备好接收流量
- **Kubernetes存活性探测（Liveness Probe）**：确定服务是否正常运行
- **监控集成**：提供详细的健康状态信息供监控系统使用
- **统一接口**：Server和Agent使用一致的接口设计

## 3. Server健康检查接口

### 3.1 基础健康检查

**接口地址**：`GET /health`

**用途**：Kubernetes Liveness Probe（存活性探测）

**设计说明**：使用根路径而非 `/api/v1/health`，原因：
- 符合Kubernetes生态标准（大多数应用使用 `/health`、`/healthz`）
- 健康检查是基础设施功能，不是业务API，应与业务API分离
- 更简洁，减少K8s频繁探测的网络开销
- 避免与业务API路由混淆

**响应格式**：
```json
{
  "status": "ok",
  "timestamp": "2025-11-28T10:30:00Z"
}
```

**HTTP状态码**：
- `200 OK`：服务正常运行
- `503 Service Unavailable`：服务不可用

**检查项**：
- HTTP服务器是否响应
- 基本进程存活

**特点**：
- 轻量级，响应快速（< 100ms）
- 不检查依赖服务
- 仅用于判断进程是否存活

### 3.2 就绪性检查

**接口地址**：`GET /health/ready`

**用途**：Kubernetes Readiness Probe（就绪性探测）

**响应格式**：
```json
{
  "status": "ready",
  "timestamp": "2025-11-28T10:30:00Z",
  "checks": {
    "database": "ok",
    "frp_server": "ok",
    "grpc_server": "ok"
  }
}
```

**HTTP状态码**：
- `200 OK`：服务就绪，可以接收流量
- `503 Service Unavailable`：服务未就绪

**检查项**：
1. **数据库连接**：执行简单查询（如 `SELECT 1`）
2. **FRP Server状态**：检查FRP服务线程是否运行
3. **gRPC Server状态**：检查gRPC服务是否可用

**响应示例（未就绪）**：
```json
{
  "status": "not_ready",
  "timestamp": "2025-11-28T10:30:00Z",
  "checks": {
    "database": "error",
    "frp_server": "ok",
    "grpc_server": "ok"
  },
  "errors": {
    "database": "connection timeout"
  }
}
```



## 4. Agent健康检查接口

### 4.1 基础健康检查

**接口地址**：`GET /health`

**用途**：Kubernetes Liveness Probe（存活性探测）

**响应格式**：
```json
{
  "status": "ok",
  "timestamp": "2025-11-28T10:30:00Z"
}
```

**HTTP状态码**：
- `200 OK`：服务正常运行
- `503 Service Unavailable`：服务不可用

**检查项**：
- HTTP服务器是否响应
- 基本进程存活

### 4.2 就绪性检查

**接口地址**：`GET /health/ready`

**用途**：Kubernetes Readiness Probe（就绪性探测）

**响应格式**：
```json
{
  "status": "ready",
  "timestamp": "2025-11-28T10:30:00Z",
  "checks": {
    "grpc_connection": "ok",
    "frp_connection": "ok"
  }
}
```

**HTTP状态码**：
- `200 OK`：服务就绪
- `503 Service Unavailable`：服务未就绪

**检查项**：
1. **gRPC连接**：与Server的gRPC连接是否正常
2. **FRP连接**：与Server的FRP WebSocket连接是否正常

**响应示例（未就绪）**：
```json
{
  "status": "not_ready",
  "timestamp": "2025-11-28T10:30:00Z",
  "checks": {
    "grpc_connection": "error",
    "frp_connection": "ok"
  },
  "errors": {
    "grpc_connection": "connection refused"
  }
}
```

## 5. 故障恢复策略

### 5.1 设计原则

Server 与 Agent 的稳定连接是系统的基石。当连接出现问题时，采用 **"快速失败 + Kubernetes 自愈"** 策略。

### 5.2 Agent 连接故障处理

**故障场景：**
1. **gRPC 连接断开** - 控制通道失效，无法接收指令
2. **gRPC 认证失败** - Token 过期/无效，无法建立信任
3. **FRP 连接断开** - 数据通道失效，无法转发流量

**处理策略（MVP）：**

采用 **方案 A：健康检查失败 → Kubernetes 自动重启容器**

**理由：**
- 实现简单可靠，利用 K8s 的自愈能力
- 重启后状态完全重置，避免脏数据和状态不一致
- 统一处理所有连接问题，易于监控和调试
- 符合云原生应用的标准做法

**实现逻辑：**

```go
// Agent 健康检查逻辑
func (h *HealthAPI) Ready(c *gin.Context) {
    checks := make(map[string]string)
    errors := make(map[string]string)
    allReady := true

    // gRPC 连接检查 - 失败则不健康
    if !h.agent.IsGRPCConnected() {
        checks["grpc_connection"] = "error"
        errors["grpc_connection"] = "not connected"
        allReady = false
        log.Printf("[HEALTH] gRPC connection check failed")
    } else {
        checks["grpc_connection"] = "ok"
    }

    // FRP 连接检查 - 失败则不健康
    if !h.agent.IsFRPConnected() {
        checks["frp_connection"] = "error"
        errors["frp_connection"] = "not connected"
        allReady = false
        log.Printf("[HEALTH] FRP connection check failed")
    } else {
        checks["frp_connection"] = "ok"
    }

    // 返回 503 会触发 K8s 重启
    if !allReady {
        c.JSON(503, gin.H{
            "status":    "not_ready",
            "timestamp": time.Now().Format(time.RFC3339),
            "checks":    checks,
            "errors":    errors,
        })
        return
    }

    c.JSON(200, gin.H{
        "status":    "ready",
        "timestamp": time.Now().Format(time.RFC3339),
        "checks":    checks,
    })
}
```

### 5.3 故障恢复流程

```
1. Agent 检测到连接断开（gRPC 或 FRP）
   ↓
2. 健康检查 /health/ready 返回 503
   ↓
3. Kubernetes 记录失败次数（第 1 次）
   ↓
4. 10 秒后再次检查，仍然失败（第 2 次）
   ↓
5. 10 秒后再次检查，仍然失败（第 3 次）
   ↓
6. Kubernetes 触发容器重启
   ↓
7. Agent 重新启动，重新连接 Server
   ↓
8. 连接成功，健康检查返回 200
   ↓
9. Kubernetes 将 Pod 标记为 Ready，恢复服务
```

**恢复时间估算：**
- 检测时间：最多 30 秒（3 次 × 10 秒）
- 重启时间：10-20 秒（进程启动 + 重新连接）
- **总计：40-50 秒**

### 5.4 监控和告警

**日志记录：**
```go
// 连接失败时记录详细日志
log.Printf("[HEALTH] Connection check failed - gRPC: %v, FRP: %v", 
    h.agent.IsGRPCConnected(), 
    h.agent.IsFRPConnected())

// 重启时 K8s 会记录事件
// kubectl describe pod <agent-pod> 可以看到重启原因
```

**监控指标：**
- Agent 重启次数（通过 K8s metrics）
- 健康检查失败次数
- 连接恢复时间

**告警规则：**
```yaml
# Prometheus 告警规则示例
- alert: AgentFrequentRestart
  expr: rate(kube_pod_container_status_restarts_total{container="agent"}[15m]) > 0.1
  annotations:
    summary: "Agent 频繁重启"
    description: "Agent {{ $labels.pod }} 在 15 分钟内重启超过 1.5 次"
```

### 5.5 未来优化方向

如果生产环境中发现重启过于频繁，可以考虑：

1. **混合策略** - 短期故障内部重连，持续故障才重启
2. **指数退避** - 重连间隔逐渐增加（1s → 2s → 4s → 8s）
3. **连接池** - 维护多个连接，提高容错能力
4. **优雅降级** - 连接断开时保持部分功能可用

但在 MVP 阶段，简单的重启策略已经足够可靠。

---

**文档版本**: 1.0  
**最后更新**: 2025-11-28  
**状态**: MVP设计
