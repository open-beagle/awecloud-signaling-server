# Tailscale 升级 - Agent 端变更设计

> 本文档描述 Tailscale 升级后 Agent 端（Golang）的变更，包括模块结构、配置、核心流程等。

## 1. 变更概述

### 1.1 模块变更总览

| 模块                                  | 变更类型 | 说明                               |
| ------------------------------------- | -------- | ---------------------------------- |
| `internal/agent/agent.go`             | 修改     | 主流程变更，集成 Tailscale         |
| `internal/agent/frp_manager.go`       | 删除     | FRP 管理器删除                     |
| `internal/agent/tailscale_manager.go` | 新增     | Tailscale 管理器                   |
| `internal/agent/proxy_manager.go`     | 新增     | TCP 代理管理器                     |
| `internal/agent/custom_connector.go`  | 删除     | FRP 自定义连接器删除               |
| `internal/agent/websocket_dial.go`    | 删除     | FRP WebSocket 拨号删除             |
| `config/agent.toml`                   | 修改     | 移除 FRP 配置，新增 Tailscale 配置 |

### 1.2 依赖变更

| 依赖                      | 变更类型 | 说明             |
| ------------------------- | -------- | ---------------- |
| `github.com/fatedier/frp` | 删除     | 不再需要 FRP 库  |
| `tailscale.com/tsnet`     | 新增     | Tailscale 嵌入库 |

---

## 2. 配置变更

### 2.1 配置文件变更

文件：`config/agent.toml`

**变更后配置**：

```toml
# AWECloud Signaling Agent 配置

[agent]
name = "beijing"
token = "your-agent-token"

[server]
# Server gRPC 地址
address = "https://signaling.your-domain.com"

[tailscale]
# 状态存储目录
state_dir = "/var/lib/awecloud-agent/tailscale"

[health]
port = 8090

[log]
level = "info"
file = ""
```

### 2.2 配置结构变更

文件：`internal/common/config/agent.go`

**变更后结构**：

```txt
AgentConfig
├── Agent      AgentSection
│   ├── Name   string   // Agent 名称
│   └── Token  string   // Agent 认证令牌
├── Server     ServerSection
│   └── Address string  // Server gRPC 地址
├── Tailscale  TailscaleSection  // [新增]
│   └── StateDir string // Tailscale 状态存储目录
├── Health     HealthSection
│   └── Port   int      // 健康检查端口
└── Log        LogConfig
```

---

## 3. 核心模块设计

### 3.1 TailscaleManager

文件：`internal/agent/tailscale_manager.go`（新增）

**职责**：管理 Tailscale 客户端连接

```txt
TailscaleManager
├── tsServer    *tsnet.Server     // Tailscale 服务实例
├── config      *config.AgentConfig
├── ctx         context.Context
│
├── Start(controlURL, authKey string) error
│   // 启动 Tailscale 客户端
│   // 1. 初始化 tsnet.Server
│   // 2. 设置 ControlURL (Headscale)
│   // 3. 设置 AuthKey
│   // 4. 调用 Start()
│   // 5. 等待获取 Tailscale IP
│
├── Stop() error
│   // 停止 Tailscale 客户端
│
├── GetIP() string
│   // 获取 Tailscale IP (100.64.x.x)
│
├── Listen(network, addr string) (net.Listener, error)
│   // 在 Tailscale 网络上监听端口
│
├── Dial(ctx, network, addr string) (net.Conn, error)
│   // 通过 Tailscale 网络拨号
│
└── IsConnected() bool
    // 检查连接状态
```

### 3.2 ProxyManager

文件：`internal/agent/proxy_manager.go`（新增）

**职责**：管理 TCP 端口代理

```txt
ProxyManager
├── proxies     map[string]*TCPProxy  // name -> proxy
├── tsManager   *TailscaleManager
├── mutex       sync.RWMutex
├── ctx         context.Context
│
├── Start(name string, listenPort int, targetAddr string) error
│   // 启动端口代理
│   // 1. 在 Tailscale IP 上监听 listenPort
│   // 2. 接受连接后拨号到 targetAddr
│   // 3. 双向转发数据 (io.Copy)
│
├── Stop(name string) error
│   // 停止端口代理
│   // 1. 关闭监听器
│   // 2. 关闭所有活跃连接
│
├── List() []ProxyStatus
│   // 列出所有代理状态
│
└── GetStats(name string) *ProxyStats
    // 获取代理统计信息

TCPProxy
├── Name        string
├── ListenPort  int
├── TargetAddr  string
├── Listener    net.Listener
├── Status      string      // running/stopped/error
├── Connections int         // 当前连接数
├── BytesIn     int64       // 入站流量
├── BytesOut    int64       // 出站流量
├── StartedAt   time.Time
└── conns       []net.Conn  // 活跃连接列表
```

### 3.3 Agent 主流程变更

文件：`internal/agent/agent.go`

**变更后结构**：

```txt
Agent
├── config       *config.AgentConfig
├── version      string
│
├── grpcConn     *grpc.ClientConn
├── grpcClient   pb.AgentServiceClient
│
├── agentID      int64
├── tailscaleIP  string           // [新增] Tailscale IP
│
├── tsManager    *TailscaleManager // [新增] 替代 frpManager
├── proxyManager *ProxyManager     // [新增] TCP 代理管理
│
├── commandChan  chan *pb.Command
├── ctx          context.Context
└── wg           sync.WaitGroup
```

---

## 4. 启动流程变更

### 4.1 变更前流程（FRP）

```txt
Agent 启动
    │
    ├─► 1. 启动健康检查 HTTP 服务器
    │
    ├─► 2. 连接 Server (gRPC)
    │
    ├─► 3. 注册 Agent
    │       └─► 获取 FRP Token 和 Server 地址
    │
    ├─► 4. 启动 FRP Manager
    │       └─► 连接 FRP Server
    │
    ├─► 5. 同步 STCP/TCP 实例
    │
    ├─► 6. 启动心跳循环
    │
    └─► 7. 启动命令接收循环
```

### 4.2 变更后流程（Tailscale）

> 参考：[Tailscale 多租户安全设计](design_tailscale_security.md)

```txt
Agent 启动
    │
    ├─► 1. 启动健康检查 HTTP 服务器
    │
    ├─► 2. 连接 Server (gRPC)
    │
    ├─► 3. 注册 Agent
    │       └─► 获取 Tailscale AuthKey 和 ControlURL
    │           └─► Server 返回预分配的固定 IP (100.64.x.x)
    │
    ├─► 4. 启动 TailscaleManager
    │       ├─► 初始化 tsnet.Server
    │       ├─► 连接 Headscale
    │       └─► 获取 Tailscale IP (100.64.x.x)
    │           └─► 使用 Server 预分配的固定 IP
    │
    ├─► 5. 上报 Tailscale IP 给 Server
    │       └─► Server 更新数据库，同步 ACL
    │
    ├─► 6. 初始化 ProxyManager
    │
    ├─► 7. 同步端口映射服务
    │       └─► 为每个服务启动 TCP 代理
    │
    ├─► 8. 启动心跳循环
    │       └─► 心跳中携带 Tailscale 状态
    │
    └─► 9. 启动命令接收循环
            └─► 处理 START_PROXY / STOP_PROXY 指令

Agent IP 管理:
- 固定 IP: Agent 创建时分配，断线不回收，删除才释放
- 网段: 100.64.0.0/16 (65534 个可用 IP)
- 分组: 同组 Agent 可互访，无分组只能访问授权端口
```

---

## 5. 命令处理变更

### 5.1 变更前命令类型

```txt
Command.Type
├── CREATE_STCP        // 创建 STCP 代理
├── DELETE_STCP        // 删除 STCP 代理
├── CREATE_TCP         // 创建 TCP 代理
├── DELETE_TCP         // 删除 TCP 代理
├── CREATE_STCP_VISITOR // 创建 STCP Visitor
└── DELETE_STCP_VISITOR // 删除 STCP Visitor
```

### 5.2 变更后命令类型

```txt
Command.Type
├── START_PROXY        // 启动端口映射
├── STOP_PROXY         // 停止端口映射
└── SYNC_PROXIES       // 同步所有端口映射
```

### 5.3 命令处理流程

```txt
收到 START_PROXY 命令
    │
    ├─► 1. 解析命令参数
    │       ├─► name: 服务名称
    │       ├─► listen_port: 监听端口
    │       └─► target_addr: 目标地址
    │
    ├─► 2. 调用 ProxyManager.Start()
    │       ├─► tsManager.Listen(":listen_port")
    │       └─► 启动代理协程
    │
    ├─► 3. 上报状态给 Server
    │
    └─► 4. 返回命令响应

收到 STOP_PROXY 命令
    │
    ├─► 1. 解析命令参数
    │       └─► name: 服务名称
    │
    ├─► 2. 调用 ProxyManager.Stop()
    │       ├─► 关闭监听器
    │       └─► 关闭所有连接
    │
    ├─► 3. 上报状态给 Server
    │
    └─► 4. 返回命令响应
```

---

## 6. TCP 代理实现

### 6.1 代理核心逻辑

```txt
TCP 代理工作流程：

1. 在 Tailscale IP 上监听端口
   listener = tsManager.Listen("tcp", ":443")

2. 接受连接
   conn = listener.Accept()

3. 拨号到目标
   remote = net.Dial("tcp", "192.168.1.1:443")

4. 双向转发
   go io.Copy(remote, conn)  // 上行：客户端 → 目标
   io.Copy(conn, remote)     // 下行：目标 → 客户端

5. 统计流量
   BytesIn += 上行字节数
   BytesOut += 下行字节数
```

### 6.2 连接管理

```txt
连接生命周期：

Accept
    │
    ├─► 1. 创建连接记录
    │       └─► 加入 conns 列表
    │
    ├─► 2. 启动转发协程
    │
    ├─► 3. 等待转发完成或错误
    │
    └─► 4. 清理连接
            └─► 从 conns 列表移除

Stop 时：
    │
    ├─► 1. 关闭监听器（停止接受新连接）
    │
    └─► 2. 关闭所有活跃连接
            └─► 遍历 conns 列表，逐个关闭
```

---

## 7. 状态上报

### 7.1 心跳上报

心跳请求中新增 Tailscale 状态：

```txt
HeartbeatRequest
├── agent_id      int64
├── agent_token   string
├── version       string
├── tailscale_ip  string   // [新增]
├── ts_connected  bool     // [新增]
└── ts_conn_type  string   // [新增] "p2p" | "derp"
```

### 7.2 代理状态上报

新增 RPC 方法上报代理状态：

```txt
ReportProxyStatus(ProxyStatusReport)

ProxyStatusReport
├── agent_id      int64
└── proxies       []ProxyStatus
    ├── name         string
    ├── listen_port  int32
    ├── target_addr  string
    ├── status       string
    ├── connections  int32
    ├── bytes_in     int64
    └── bytes_out    int64
```

---

## 8. 删除文件清单

| 文件                                 | 说明               |
| ------------------------------------ | ------------------ |
| `internal/agent/frp_manager.go`      | FRP 管理器         |
| `internal/agent/custom_connector.go` | FRP 自定义连接器   |
| `internal/agent/websocket_dial.go`   | FRP WebSocket 拨号 |

---

## 9. 健康检查变更

### 9.1 健康检查接口

```txt
GET /health

响应变更：
{
  "status": "healthy",
  "grpc_connected": true,
  "tailscale_connected": true,    // [新增] 替代 frp_connected
  "tailscale_ip": "100.64.0.10",  // [新增]
  "proxy_count": 5                // [新增] 运行中的代理数量
}
```

---

**文档版本**: 1.1
**创建日期**: 2025-01-08
**更新日期**: 2025-01-08
**关联文档**:

- [Tailscale 升级方案设计](design_tailscale_upgrade.md)
- [Tailscale 多租户安全设计](design_tailscale_security.md)
- [Tailscale 升级 - Server 端变更设计](design_tailscale_server.md)
- [Tailscale 升级 - Desktop 端变更设计](design_tailscale_desktop.md)
