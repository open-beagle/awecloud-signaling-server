# Tailscale 升级 - Agent 端变更设计

> 本文档描述 Tailscale 升级后 Agent 端（Golang）的变更，包括模块结构、配置、核心流程等。

## 核心业务

Agent 承担两个核心业务：

| 业务         | 说明                          | 数据流向                                   |
| ------------ | ----------------------------- | ------------------------------------------ |
| **服务暴露** | 将局域网服务暴露到 VPN 网络   | VPN 客户端 → VPN 网络 → Agent → 局域网服务 |
| **服务访问** | 访问 VPN 网络中其他节点的服务 | 局域网客户端 → Agent → VPN 网络 → VPN 服务 |

**服务暴露（Proxy）**：Agent 在 Tailscale IP 上监听端口，接收 VPN 网络流量，转发到局域网内部服务（SSH、MySQL 等）。

**服务访问（Visitor）**：Agent 在局域网 IP 上监听端口，将流量通过 VPN 网络转发到其他节点暴露的服务。此功能与 Server-Web 的 Agent 授权业务整合，管理员在授权服务时可选配置 Visitor，无需单独入口。不启用 Visitor 时，仅授权 ACL，Agent 可通过代码直接访问目标服务。

### 局域网 IP 检测

服务访问需要在局域网 IP 上监听端口，供局域网内其他客户端访问。自动检测局域网 IP 避免管理员手动配置。

**部署场景与网络模式**：

| 部署方式           | 网络模式    | 检测结果    | 适用场景             |
| ------------------ | ----------- | ----------- | -------------------- |
| Windows/Linux 主机 | -           | 物理网卡 IP | 局域网客户端访问     |
| Docker 容器        | host 网络   | 宿主机 IP   | 局域网客户端访问     |
| Docker 容器        | bridge 网络 | 容器 IP     | 同 Docker 网络内访问 |
| K8s Pod            | hostNetwork | 节点 IP     | 集群外客户端访问     |
| K8s Pod            | Pod 网络    | Pod IP      | 集群内其他 Pod 访问  |

**检测流程**：

```txt
获取所有网络接口
    │
    ├─► 1. 过滤黑名单接口
    │       ├─► docker*, br-*, veth*      (Docker)
    │       ├─► cni*, flannel*, calico*   (K8s CNI)
    │       ├─► virbr*                    (libvirt)
    │       ├─► vmnet*, VMware*           (VMware)
    │       ├─► vEthernet*                (Hyper-V)
    │       ├─► lo, lo0                   (回环)
    │       └─► tailscale*, ts*           (Tailscale)
    │
    ├─► 2. 过滤非私有 IP
    │       └─► 只保留 10.x / 172.16-31.x / 192.168.x
    │
    ├─► 3. 按优先级排序
    │       ├─► 优先: eth*, en*, ens*, eno* (物理以太网)
    │       ├─► 次选: wlan*, wl*            (无线网卡)
    │       └─► 兜底: 其他私有 IP 网卡
    │
    └─► 4. 返回第一个匹配的 IP
            └─► 检测失败则回退到 127.0.0.1
```

**配置覆盖**：支持配置文件手动指定，优先级高于自动检测。

```toml
[visitor]
# 可选，手动指定监听地址，留空则自动检测
listen_addr = ""
```

---

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
├── grpcClient  pb.AgentServiceClient  // 用于状态同步
├── agentID     int64
├── agentToken  string
├── ctx         context.Context
│
├── Start(controlURL, authKey string) error
│   // 启动 Tailscale 客户端
│   // 1. 从 Server 加载历史状态
│   // 2. 创建临时状态目录
│   // 3. 恢复状态到临时目录（如果存在）
│   // 4. 初始化 tsnet.Server（使用临时目录）
│   // 5. 设置 ControlURL (Headscale)
│   // 6. 设置 AuthKey
│   // 7. 调用 Start()
│   // 8. 等待获取 Tailscale IP
│   // 9. 启动定期状态同步协程
│
├── Stop() error
│   // 停止 Tailscale 客户端
│   // 1. 最后一次保存状态到 Server
│   // 2. 停止 tsnet.Server
│   // 3. 清理临时目录
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
├── IsConnected() bool
│   // 检查连接状态
│
├── loadStateFromServer() ([]byte, error)
│   // 从 Server 加载 Tailscale 状态
│
├── saveStateToServer() error
│   // 保存 Tailscale 状态到 Server
│
└── periodicStateSave()
    // 定期保存状态到 Server（每5分钟）
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
    │       ├─► 从 Server 加载历史状态（如果存在）
    │       ├─► 创建临时状态目录
    │       ├─► 恢复状态到临时目录
    │       ├─► 初始化 tsnet.Server（使用临时目录）
    │       ├─► 连接 Headscale
    │       ├─► 获取 Tailscale IP (100.64.x.x)
    │       │   └─► 使用 Server 预分配的固定 IP
    │       └─► 启动定期状态同步（每5分钟保存到 Server）
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

Agent 停止:
    │
    ├─► 1. 停止命令接收循环
    │
    ├─► 2. 停止心跳循环
    │
    ├─► 3. 停止所有代理服务
    │
    ├─► 4. 停止 TailscaleManager
    │       ├─► 最后一次保存状态到 Server
    │       ├─► 停止 tsnet.Server
    │       └─► 清理临时目录
    │
    └─► 5. 关闭 gRPC 连接

Agent IP 管理:
- 固定 IP: Agent 创建时分配，断线不回收，删除才释放
- 网段: 100.64.0.0/16 (65534 个可用 IP)
- 分组: 同组 Agent 可互访，无分组只能访问授权端口
- 状态存储: 集中存储在 Server，Agent 无状态化
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
├── START_PROXY        // 启动端口映射（服务暴露）
├── STOP_PROXY         // 停止端口映射
├── SYNC_PROXIES       // 同步所有端口映射
├── START_VISITOR      // 启动 Visitor（服务访问）
├── STOP_VISITOR       // 停止 Visitor
└── SYNC_VISITORS      // 同步所有 Visitor
```

### 5.3 命令处理流程

```txt
收到 START_PROXY 命令（服务暴露）
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

收到 START_VISITOR 命令（服务访问）
    │
    ├─► 1. 解析命令参数
    │       ├─► name: Visitor 名称
    │       ├─► listen_port: 本地监听端口
    │       └─► target_addr: VPN 网络目标地址（如 100.64.0.1:3306）
    │
    ├─► 2. 调用 VisitorManager.Start()
    │       ├─► 在局域网 IP 上监听端口
    │       └─► 启动转发协程（通过 Tailscale 拨号到目标）
    │
    ├─► 3. 上报状态给 Server
    │
    └─► 4. 返回命令响应

收到 STOP_VISITOR 命令
    │
    ├─► 1. 解析命令参数
    │       └─► name: Visitor 名称
    │
    ├─► 2. 调用 VisitorManager.Stop()
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

## 10. Tailscale 状态管理

### 10.1 状态集中存储设计

为实现 Agent 无状态化部署，Tailscale 节点状态集中存储在 Server 端。

**设计目标**：

- Agent 可随时销毁重建，状态在 Server 端持久化
- 支持容器化部署（Docker/K8s），无需挂载持久卷
- 集中管理便于备份、迁移、监控
- Agent 重启后自动恢复身份，保持固定 IP

### 10.2 状态存储流程

```txt
┌─────────────────────────────────────────────────────────────────────────────┐
│                        Tailscale 状态管理流程                                │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│   Agent 启动                                                                │
│        │                                                                    │
│        ▼                                                                    │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │ 1. 向 Server 请求历史状态                                            │   │
│   │                                                                     │   │
│   │    gRPC: GetTailscaleState(agent_id, agent_token)                  │   │
│   │    返回: state_data (二进制数据), exists (是否存在)                 │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
│        │                                                                    │
│        ▼                                                                    │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │ 2. 创建临时状态目录                                                  │   │
│   │                                                                     │   │
│   │    tmpDir = os.MkdirTemp("", "tailscale-*")                        │   │
│   │    例如: /tmp/tailscale-123456                                      │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
│        │                                                                    │
│        ▼                                                                    │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │ 3. 恢复状态到临时目录（如果存在历史状态）                             │   │
│   │                                                                     │   │
│   │    if exists && len(state_data) > 0 {                              │   │
│   │        解压 state_data 到 tmpDir                                    │   │
│   │        恢复节点密钥、身份信息等                                      │   │
│   │    }                                                                │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
│        │                                                                    │
│        ▼                                                                    │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │ 4. 启动 tsnet.Server                                                 │   │
│   │                                                                     │   │
│   │    tsServer = &tsnet.Server{                                       │   │
│   │        Hostname:   agent_name,                                     │   │
│   │        Dir:        tmpDir,  // 使用临时目录                         │   │
│   │        ControlURL: headscale_url,                                  │   │
│   │        AuthKey:    auth_key,                                       │   │
│   │    }                                                               │   │
│   │    tsServer.Start()                                                │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
│        │                                                                    │
│        ▼                                                                    │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │ 5. 启动定期状态同步                                                  │   │
│   │                                                                     │   │
│   │    每 5 分钟执行一次:                                                │   │
│   │    - 从 tmpDir 读取状态文件                                          │   │
│   │    - 压缩为二进制数据                                                │   │
│   │    - 调用 SaveTailscaleState(agent_id, state_data)                 │   │
│   │    - Server 保存到数据库                                             │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
│        │                                                                    │
│        ▼                                                                    │
│   Agent 运行中...                                                           │
│        │                                                                    │
│        ▼                                                                    │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │ 6. Agent 停止                                                        │   │
│   │                                                                     │   │
│   │    - 最后一次保存状态到 Server                                       │   │
│   │    - 停止 tsnet.Server                                              │   │
│   │    - 清理临时目录 (os.RemoveAll(tmpDir))                            │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 10.3 gRPC 接口定义

新增两个 RPC 方法用于状态管理：

```txt
service AgentService {
    // 现有方法...
    rpc Register(RegisterRequest) returns (RegisterResponse);
    rpc Heartbeat(HeartbeatRequest) returns (HeartbeatResponse);

    // 新增：状态管理
    rpc GetTailscaleState(GetStateRequest) returns (GetStateResponse);
    rpc SaveTailscaleState(SaveStateRequest) returns (SaveStateResponse);
}

GetStateRequest:
├── agent_id    int64   // Agent ID
└── agent_token string  // Agent 认证令牌

GetStateResponse:
├── state_data  bytes   // Tailscale 状态序列化数据（压缩后）
└── exists      bool    // 是否存在历史状态

SaveStateRequest:
├── agent_id    int64   // Agent ID
├── agent_token string  // Agent 认证令牌
└── state_data  bytes   // Tailscale 状态序列化数据

SaveStateResponse:
└── success     bool    // 是否保存成功
```

### 10.4 Server 端数据模型

新增数据表存储 Agent 的 Tailscale 状态：

```txt
AgentTailscaleState 表:
├── id          BIGINT PRIMARY KEY AUTO_INCREMENT
├── agent_id    BIGINT UNIQUE NOT NULL  // 外键关联 agents.id
├── state_data  BLOB                    // Tailscale 状态数据（压缩）
├── updated_at  TIMESTAMP               // 最后更新时间
└── created_at  TIMESTAMP               // 创建时间

索引:
- agent_id (唯一索引)

外键:
- agent_id REFERENCES agents(id) ON DELETE CASCADE
```

### 10.5 状态数据内容

Tailscale 状态目录包含以下关键文件：

```txt
临时状态目录结构:
/tmp/tailscale-123456/
├── tailscaled.state        // 节点状态（最重要）
│   ├── 节点私钥
│   ├── 节点 ID
│   ├── 机器密钥
│   └── 网络配置
├── tailscaled.log.txt      // 日志（可选，不保存）
└── *.sock                  // Unix socket（运行时，不保存）

保存到 Server 的数据:
- 只保存 tailscaled.state 文件
- 压缩后存储（减少数据库空间）
- 加密存储（可选，增强安全性）
```

### 10.6 安全考虑

**状态数据包含敏感信息**（节点私钥），需要保护：

| 安全措施     | 说明                                 | 优先级 |
| ------------ | ------------------------------------ | ------ |
| 传输加密     | gRPC 使用 TLS 加密传输               | 必须   |
| 访问控制     | 验证 agent_token，只能访问自己的状态 | 必须   |
| 数据库加密   | 对 state_data 字段加密存储           | 推荐   |
| 定期清理     | 删除 Agent 时同步删除状态数据        | 必须   |
| 审计日志     | 记录状态访问和修改操作               | 推荐   |
| 临时目录权限 | 设置 tmpDir 权限为 700               | 必须   |

### 10.7 优势与权衡

**优势**：

- Agent 完全无状态，可随时销毁重建
- 容器友好，无需挂载持久卷
- 集中管理，便于备份和迁移
- 支持 Agent 在不同机器间迁移

**权衡**：

- 首次启动需要从 Server 加载状态（增加启动时间约 1-2 秒）
- 定期同步增加网络开销（每 5 分钟约 10-50KB）
- Server 数据库存储增加（每 Agent 约 50-100KB）
- 状态数据包含私钥，需要额外安全措施

**适用场景**：

- ✅ 容器化部署（Docker/K8s）
- ✅ 云环境（实例可能随时销毁）
- ✅ 需要集中管理和备份
- ⚠️ 对启动速度要求极高的场景（可优化为异步加载）

### 10.8 配置变更

移除 Agent 配置文件中的 state_dir 配置：

```txt
变更前 (config/agent.toml):
[tailscale]
state_dir = "/var/lib/awecloud-agent/tailscale"  # 本地持久化

变更后 (config/agent.toml):
[tailscale]
# 状态现在保存在 Server 端，无需本地持久化
# 移除 state_dir 配置项
```

---

**文档版本**: 1.2
**创建日期**: 2025-01-08
**更新日期**: 2025-01-08
**关联文档**:

- [Tailscale 升级方案设计](design_tailscale_upgrade.md)
- [Tailscale 多租户安全设计](design_tailscale_security.md)
- [Tailscale 升级 - Server 端变更设计](design_tailscale_server.md)
- [Tailscale 升级 - Desktop 端变更设计](design_tailscale_desktop.md)
