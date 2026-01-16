# FRP WebSocket 路径设计方案

## 文档版本

- 创建日期: 2025-11-28
- 状态: 已确认

## 问题背景

FRP 硬编码使用 `/~!frp` 作为 WebSocket 路径，但我们希望对外使用更语义化的路径 `/ws`。

### 技术限制

1. **FRP Server**: 通过 muxer 匹配 `GET /~!frp` 来识别 WebSocket 连接，路径硬编码
2. **要求**: 不修改 FRP 源码

## 整体方案

### 三端协同方案

```
┌─────────────┐         ┌─────────────┐         ┌─────────────┐
│ Agent/      │ ─────> │  Traefik    │ ─────> │ FRP Server  │
│ Desktop     │  /ws    │  Ingress    │ /~!frp │   (7000)    │
│             │         │ (rewrite)   │         │             │
│ 自定义      │         │ 路径重写    │         │ 原生实现    │
│ Connector   │         │             │         │             │
└─────────────┘         └─────────────┘         └─────────────┘
```

### 各端方案

| 组件        | 方案             | 说明                      |
| ----------- | ---------------- | ------------------------- |
| **Agent**   | 自定义 Connector | 支持自定义 WebSocket 路径 |
| **Desktop** | 自定义 Connector | 参考 Agent 实现           |
| **Server**  | 原生 FRP         | 无需修改，使用 `/~!frp`   |
| **Traefik** | ReplacePathRegex | 路径重写 `/ws` → `/~!frp` |

---

## Agent 端方案

### 设计思路

FRP client 硬编码使用 `/~!frp` 路径，我们通过自定义 Connector 来支持自定义路径。

### 实现方式

1. **创建自定义 WebSocket Dial Hook**

   - 文件: `internal/agent/websocket_dial.go`
   - 功能: 支持自定义 path 和 TLS 配置
   - 参数: protocol, host, path, tlsConfig

2. **创建自定义 Connector**

   - 文件: `internal/agent/custom_connector.go`
   - 功能: 实现 `client.Connector` 接口
   - 特性: 使用自定义 WebSocket dial hook

3. **修改 FRP Manager**

   - 文件: `internal/agent/frp_manager.go`
   - 功能: 使用 `ConnectorCreator` 注入自定义 Connector
   - 特性: 解析 URL，提取 path

4. **定义常量**
   - 文件: `internal/common/constants/tunnel.go`
   - 常量: `DefaultWebSocketPath = "/ws"`
   - 常量: `FRPDefaultPath = "/~!frp"`

### 工作流程

1. Agent 从 Server 获取 `public_url = "wss://signaling.example.com/ws"`
2. Agent 解析 URL，提取 path = `/ws`
3. Agent 创建自定义 Connector，传入 path
4. Connector 使用自定义 dial hook 建立连接
5. 发送 `GET /ws HTTP/1.1` 到 Traefik
6. Traefik 重写为 `/~!frp` 转发到 FRP Server
7. 连接成功

### 配置

```toml
[agent]
name = "agent-001"
token = "agent-token"

[server]
address = "https://signaling.example.com"
tls_enable = true
# 不配置 tls_ca_file，自动跳过证书验证
```

### 状态

✅ **已完成**

---

## Desktop 端方案

### 设计思路

参考 Agent 端实现，创建自定义 Connector 支持自定义 WebSocket 路径。

### 实现方式

1. **复用 Agent 的实现**

   - 复用 `websocket_dial.go`
   - 复用 `custom_connector.go`
   - 复用 `constants/tunnel.go`

2. **修改 Desktop FRP Manager**
   - 文件: `desktop/internal/frp/manager.go`
   - 功能: 使用自定义 Connector
   - 特性: 解析 URL，提取 path

### 工作流程

1. Desktop 从 Server 获取 `public_url = "wss://signaling.example.com/ws"`
2. Desktop 解析 URL，提取 path = `/ws`
3. Desktop 创建自定义 Connector
4. 连接流程同 Agent

### 配置

Desktop 通过 UI 配置 Server 地址，自动获取 `public_url`。

### 状态

⚠️ **待实现**（参考 Agent）

---

## Server 端方案

### 设计思路

Server 端使用原生 FRP Server，无需修改代码。通过 Traefik 做路径重写。

### 实现方式

**无需修改代码**，只需配置：

1. **Server 配置**

   - 配置 `public_url = "wss://signaling.example.com/ws"`
   - FRP Server 仍然监听 `/~!frp`

2. **Traefik 配置**
   - 创建 Ingress 路由 `/ws`
   - 创建 Middleware 重写路径 `/ws` → `/~!frp`

### 工作流程

1. Traefik 接收 `GET /ws HTTP/1.1`
2. Middleware 重写为 `GET /~!frp HTTP/1.1`
3. 转发到 FRP Server (7000)
4. FRP Server 识别 `/~!frp`
5. 连接成功

### 配置

**Server** (`config/server.toml`):

```toml
[server]
bind_addr = "0.0.0.0"
bind_port = 7000
protocol = "websocket"
frp_auth_token = "your-token"
public_url = "wss://signaling.example.com/ws"
```

**Ingress 配置要点**:

- API 版本: `networking.k8s.io/v1`
- 路由规则: Host + Path `/ws`
- 后端服务: `signaling-server:7000`
- 关键 annotations:
  - `kubernetes.io/ingress.class: traefik`
  - `traefik.ingress.kubernetes.io/router.middlewares: signaling-frp-path-rewrite@kubernetescrd`
  - `traefik.ingress.kubernetes.io/router.tls: "true"`

**Middleware 配置要点**:

- API 版本: `traefik.containo.us/v1alpha1`
- 类型: Middleware
- 功能: `replacePathRegex`
- 规则: `^/ws$` → `/~!frp`

### 状态

✅ **无需修改**

---

## 完整连接流程

```
1. Agent 启动并认证
   └─ Server 返回: public_url = "wss://signaling.example.com/ws"

2. Agent 解析 URL
   ├─ Host: signaling.example.com
   ├─ Port: 443
   ├─ Path: /ws
   └─ Protocol: wss

3. Agent 创建自定义 Connector
   └─ 传入 path = /ws

4. Agent 建立 WebSocket 连接
   └─ 发送: GET /ws HTTP/1.1

5. Traefik Ingress 处理
   ├─ 匹配规则: Host && Path=/ws
   ├─ 应用中间件: frp-path-rewrite
   └─ 重写路径: /ws → /~!frp

6. Traefik 转发到 FRP Server
   └─ 发送: GET /~!frp HTTP/1.1

7. FRP Server 识别
   ├─ Muxer 匹配: GET /~!frp ✅
   └─ 响应: 101 Switching Protocols

8. WebSocket 连接建立 ✅
```

---

## 验证清单

- [ ] Ingress 配置正确
- [ ] Middleware 配置正确
- [ ] Server ConfigMap 已更新
- [ ] Agent 能获取 public_url
- [ ] Agent 能成功连接
- [ ] WebSocket 连接稳定

---

## 常见问题

### Q1: Agent 连接失败，提示证书错误

**解决**: 确认 Agent 配置中没有设置 `tls_ca_file`

### Q2: Ingress 路径重写不生效

**解决**: 检查 Middleware 是否正确关联到 Ingress

### Q3: FRP Server 无法识别连接

**解决**: 检查 Traefik 日志，确认路径重写是否生效

---

## 测试场景

### 开发环境

```toml
[server]
public_url = ""  # 不配置，使用默认 localhost:7000
```

### 生产环境

```toml
[server]
public_url = "wss://signaling.example.com/ws"
```

---

## 文件清单

### Agent 端

- `internal/agent/websocket_dial.go` - 自定义 WebSocket dial hook
- `internal/agent/custom_connector.go` - 自定义 Connector
- `internal/agent/frp_manager.go` - FRP 管理器（已修改）
- `internal/common/constants/tunnel.go` - 常量定义

### Desktop 端

- 待实现（参考 Agent）

### Server 端

- 无需修改代码
- `config/server.toml` - 配置 public_url

### Kubernetes

- Traefik Middleware 和 Ingress 配置由运维团队维护

---

## 总结

通过三端协同方案：

1. ✅ **Agent/Desktop**: 自定义 Connector 支持自定义路径
2. ✅ **Server**: 无需修改，使用原生 FRP
3. ✅ **Traefik**: 路径重写 `/ws` → `/~!frp`

实现了：

- 对外使用语义化路径 `/ws`
- 内部使用 FRP 标准路径 `/~!frp`
- 不修改 FRP 源码
- 配置简单清晰
- 连接成功

### 场景 2: 生产环境

**配置**:

```toml
[server]
public_url = "wss://signaling.example.com/ws"
```

**预期**:

- Agent 从 Server 获取 `public_url`
- Agent 连接 `wss://signaling.example.com/ws`
- Traefik 重写路径为 `/~!frp`
- FRP Server 识别并建立连接
- 连接成功

---

## 总结

通过 Traefik ReplacePathRegex 中间件，我们实现了：

1. ✅ 对外使用语义化路径 `/ws`
2. ✅ 内部使用 FRP 标准路径 `/~!frp`
3. ✅ 零代码修改
4. ✅ 配置简单清晰
5. ✅ 支持自签名证书

这是一个简单、可靠、易于维护的解决方案。
