# Tailscale 升级 - Desktop 端变更设计

> 本文档描述 Tailscale 升级后 Desktop 客户端（Golang + Wails）的变更，包括模块结构、配置、核心流程等。
>
> **关联文档**: [Tailscale 多租户安全与权限设计](design_tailscale_security.md)

## 1. 变更概述

### 1.1 模块变更总览

| 模块                                | 变更类型 | 说明                          |
| ----------------------------------- | -------- | ----------------------------- |
| `desktop/internal/frp/`             | 删除     | FRP 模块整个删除              |
| `desktop/internal/tailscale/`       | 新增     | Tailscale 管理模块            |
| `desktop/internal/client/tunnel.go` | 修改     | 隧道配置获取逻辑变更          |
| `desktop/internal/client/auth.go`   | 修改     | 登录后获取 Tailscale 认证信息 |
| `desktop/internal/models/`          | 修改     | 服务模型变更                  |

### 1.2 依赖变更

| 依赖                      | 变更类型 | 说明             |
| ------------------------- | -------- | ---------------- |
| `github.com/fatedier/frp` | 删除     | 不再需要 FRP 库  |
| `tailscale.com/tsnet`     | 新增     | Tailscale 嵌入库 |

---

## 2. 核心模块设计

### 2.1 TailscaleManager

文件：`desktop/internal/tailscale/manager.go`（新增）

**职责**：管理 Desktop 端 Tailscale 客户端

```txt
TailscaleManager
├── tsServer       *tsnet.Server
├── connected      bool
├── tailscaleIP    string        // 固定 IP (100.65.x.x 网段)
├── deviceToken    string        // 设备令牌
├── deviceFingerprint string     // 设备指纹
├── mutex          sync.RWMutex
│
├── Connect(controlURL, authKey string) error
│   // 连接 Tailscale 网络
│   // 1. 初始化 tsnet.Server
│   // 2. 设置 Ephemeral = false（持久化节点，保持固定 IP）
│   // 3. 设置 ControlURL (Headscale)
│   // 4. 设置 AuthKey
│   // 5. 调用 Start()
│   // 6. 获取 Tailscale IP (100.65.x.x)
│
├── Disconnect() error
│   // 断开 Tailscale 连接
│   // 1. 调用 tsServer.Close()
│   // 2. 清理状态（不删除节点，保持固定 IP）
│
├── Dial(ctx, network, addr string) (net.Conn, error)
│   // 通过 Tailscale 网络拨号到 Agent
│   // 例如: Dial("tcp", "100.64.0.1:3306")
│
├── Listen(network, addr string) (net.Listener, error)
│   // 在 Tailscale IP 上监听端口（暴露服务）
│   // 例如: Listen("tcp", ":3389") 暴露 RDP
│
├── GetIP() string
│   // 获取 Desktop 的 Tailscale IP (100.65.x.x)
│
└── IsConnected() bool
    // 检查连接状态
```

**关键设计决策**（参考 [安全设计](design_tailscale_security.md#5-desktop-安全隔离)）：

| 决策         | 说明                                                 |
| ------------ | ---------------------------------------------------- |
| 固定 IP      | Desktop 使用 `100.65.0.0/16` 网段，每设备独立固定 IP |
| 非 Ephemeral | 不再使用临时节点，保持固定 IP 以支持服务暴露         |
| 多实例支持   | 同一员工可在多设备登录，每设备独立 IP                |
| 设备指纹     | 基于设备指纹识别实例，重连时保持 IP 不变             |

---

## 3. 登录流程变更

### 3.1 变更前流程（FRP）

```txt
用户登录
    │
    ├─► 1. 调用 /api/v1/client/auth/login
    │       └─► 获取 session_token
    │
    ├─► 2. 调用 /api/v1/client/tunnel/config
    │       └─► 获取 FRP 连接信息
    │           ├─► server_url
    │           └─► token
    │
    ├─► 3. 启动 FRP Manager
    │       └─► 连接 FRP Server
    │
    └─► 4. 获取服务列表
            └─► 为每个服务创建 STCP Visitor
```

### 3.2 变更后流程（Tailscale）

```txt
用户登录
    │
    ├─► 1. 调用 /api/v1/client/auth/login
    │       └─► 获取 session_token
    │
    ├─► 2. 调用 /api/v1/client/tailscale/auth
    │       └─► 获取 Tailscale 认证信息
    │           ├─► control_url (Headscale 地址)
    │           ├─► auth_key (预认证密钥)
    │           └─► derp_url (DERP 服务器)
    │
    ├─► 3. 启动 TailscaleManager
    │       ├─► 连接 Headscale
    │       └─► 获取 Tailscale IP
    │
    └─► 4. 获取服务列表
            └─► 服务列表包含 Agent Tailscale IP 和端口
```

---

## 4. 服务访问流程变更

### 4.1 变更前流程（FRP STCP Visitor）

```txt
用户点击"连接"服务
    │
    ├─► 1. 创建 STCP Visitor
    │       ├─► visitor_name
    │       ├─► server_name (STCP 实例名)
    │       ├─► secret_key
    │       ├─► bind_addr = "127.0.0.1"
    │       └─► bind_port = 用户指定端口
    │
    ├─► 2. FRP Manager 创建 Visitor
    │       └─► 通过 FRP Server 中继
    │
    └─► 3. 用户通过 127.0.0.1:bind_port 访问服务
```

### 4.2 变更后流程（Tailscale 直连）

```txt
用户查看服务列表
    │
    ├─► 1. 获取服务信息（Server 根据权限过滤）
    │       ├─► agent_tailscale_ip = "100.64.0.1"
    │       ├─► listen_port = 3306
    │       └─► access_type = "public" | "private" | "group"
    │
    └─► 2. 用户直接访问 100.64.0.1:3306
            ├─► Desktop 内置 tsnet 通过 Tailscale 网络拨号
            └─► Headscale ACL 在网络层验证访问权限
```

**权限控制**（参考 [安全设计](design_tailscale_security.md#3-权限模型设计)）：

| 权限类型  | 说明                | ACL 规则                 |
| --------- | ------------------- | ------------------------ |
| `public`  | 所有 Desktop 可访问 | 允许所有 100.65.x.x 访问 |
| `private` | 仅创建者可访问      | 仅允许特定 Desktop IP    |
| `group`   | 指定组可访问        | 允许组内成员 IP          |

---

## 5. 服务列表变更

### 5.1 变更前服务模型

```txt
Service
├── ID           int64
├── InstanceName string   // STCP 实例名
├── AgentID      int64
├── AgentName    string
├── SecretKey    string   // STCP 密钥
├── LocalIP      string   // Agent 内网 IP
├── LocalPort    int      // Agent 内网端口
├── Description  string
└── IsFavorite   bool
```

### 5.2 变更后服务模型

> 参考：[Tailscale 多租户安全设计](design_tailscale_security.md)

```txt
Service
├── ID              int64
├── Name            string   // 服务名称
├── AgentID         int64
├── AgentName       string
├── AgentTailscaleIP string  // [新增] Agent 的 Tailscale IP (100.64.x.x)
├── ListenPort      int      // [新增] Agent 监听端口
├── TargetAddr      string   // [新增] 内网目标地址（仅展示）
├── Description     string
├── Status          string   // [新增] running/stopped
├── AccessType      string   // [新增] public/private/group
└── IsFavorite      bool

访问地址 = AgentTailscaleIP:ListenPort
例如: 100.64.0.1:3306

网段说明:
- Agent IP: 100.64.0.0/16 (100.64.0.1 - 100.64.255.254)
- Desktop IP: 100.65.0.0/16 (100.65.0.1 - 100.65.255.254)

权限说明:
- Desktop 只能看到有权限访问的服务（Server 根据权限过滤）
- 即使知道 IP:Port，没有 ACL 授权也无法连接（Headscale ACL 控制）
- 权限撤销后，ACL 立即更新，连接被拒绝
```

---

## 6. 前端界面变更

### 6.1 服务列表界面

```txt
变更前：
┌─────────────────────────────────────────────────────────────────┐
│  服务名称        Agent      本地端口    状态      操作          │
├─────────────────────────────────────────────────────────────────┤
│  mysql-prod     beijing    3306       已连接    [断开]         │
│  redis-dev      beijing    6379       未连接    [连接]         │
└─────────────────────────────────────────────────────────────────┘

变更后：
┌─────────────────────────────────────────────────────────────────┐
│  服务名称        Agent      访问地址              状态    操作   │
├─────────────────────────────────────────────────────────────────┤
│  mysql-prod     beijing    100.64.0.10:3306     运行中  [复制]  │
│  redis-dev      beijing    100.64.0.10:6379     运行中  [复制]  │
│  nas-drive      beijing    100.64.0.11:6690     运行中  [复制]  │
└─────────────────────────────────────────────────────────────────┘

说明：
- 不再需要"连接/断开"操作
- 服务始终可用（只要 Agent 在线）
- 用户直接使用 Tailscale IP:Port 访问
- "复制"按钮复制访问地址到剪贴板
```

---

## 7. 删除文件清单

| 目录/文件                                  | 说明               |
| ------------------------------------------ | ------------------ |
| `desktop/internal/frp/`                    | FRP 模块整个目录   |
| `desktop/internal/frp/manager.go`          | FRP 管理器         |
| `desktop/internal/frp/custom_connector.go` | FRP 自定义连接器   |
| `desktop/internal/frp/websocket_dial.go`   | FRP WebSocket 拨号 |

---

## 8. API 调用变更

### 8.1 移除的 API 调用

| API                              | 说明              |
| -------------------------------- | ----------------- |
| GET /api/v1/client/tunnel/config | 获取 FRP 隧道配置 |

### 8.2 新增的 API 调用

| API                                        | 说明                    |
| ------------------------------------------ | ----------------------- |
| POST /api/v1/client/tailscale/auth         | 获取 Tailscale 认证信息 |
| DELETE /api/v1/client/tailscale/disconnect | 断开 Tailscale 连接     |

### 8.3 修改的 API 调用

| API                         | 变更说明                                 |
| --------------------------- | ---------------------------------------- |
| GET /api/v1/client/services | 响应新增 agent_tailscale_ip, listen_port |

---

## 9. 配置存储变更

### 9.1 Tailscale 状态存储

Desktop 需要存储 Tailscale 状态（密钥等）：

```txt
Windows: %APPDATA%\AWECloud\tailscale\
macOS:   ~/Library/Application Support/AWECloud/tailscale/
Linux:   ~/.config/awecloud/tailscale/
```

### 9.2 存储内容

```txt
tailscale/
├── tailscaled.state    // Tailscale 状态文件
└── tailscaled.log      // Tailscale 日志（可选）
```

---

## 10. 注销流程

### 10.1 用户注销

```txt
用户点击"注销"
    │
    ├─► 1. 断开 Tailscale 连接
    │       └─► tsManager.Disconnect()
    │
    ├─► 2. 调用 /api/v1/client/tailscale/disconnect
    │       └─► Server 从 Headscale 移除设备
    │
    ├─► 3. 清除本地 session
    │
    └─► 4. 返回登录界面
```

---

## 11. 错误处理

### 11.1 Tailscale 连接失败

```txt
连接失败场景：
├── Headscale 不可达
│   └─► 提示"无法连接控制服务器，请检查网络"
│
├── AuthKey 无效/过期
│   └─► 提示"认证失败，请重新登录"
│
└── 网络问题
    └─► 自动重试，显示"正在重新连接..."
```

### 11.2 服务访问失败

```txt
访问失败场景：
├── Agent 离线
│   └─► 服务状态显示"离线"，禁用访问
│
├── 端口映射未启动
│   └─► 服务状态显示"未启动"
│
└── 网络不通
    └─► 提示"无法连接到服务，请检查 Agent 状态"
```

---

**文档版本**: 1.1
**创建日期**: 2025-01-08
**更新日期**: 2025-01-08
**关联文档**:

- [Tailscale 升级方案设计](design_tailscale_upgrade.md)
- [Tailscale 多租户安全设计](design_tailscale_security.md)
- [Tailscale 升级 - Server 端变更设计](design_tailscale_server.md)
- [Tailscale 升级 - Agent 端变更设计](design_tailscale_agent.md)
