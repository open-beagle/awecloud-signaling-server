# Server 设备管理设计

相关文档：

- `design_ztna_server_user.md` — 用户管理设计
- `design_ztna_server_heartbeat.md` — 心跳业务设计
- `design_ztna_server_sync.md` — 定时同步设计（Tag 同步的调度和改造）
- `design_ztna_data_database.md` — 数据库表设计（Node 表结构）
- `design_ztna_data_server.md` — 业务实体设计（Node.List、User.Agent.Detail 设备列表组装方式）
- `design_ztna_data_headscale.md` — Headscale 数据交互（Node Tag 同步、PreAuthKey 生成、节点管理）

## 概述

Node（设备）是系统中实际连接 Tailscale 隧道的实体。每个 Node 属于一个 User，一个 User 可以有多个 Node。

设备类型（详见 `design_ztna.md`「Desktop 两种形态」）：

| 类型    | 说明                        | User.Role | 数量关系               | 示例                   |
| ------- | --------------------------- | --------- | ---------------------- | ---------------------- |
| agent   | 内网代理设备                | agent     | 每个 Agent 用户可多个  | beagle-xx1、beagle-xx2 |
| desktop | Desktop.Host 桌面客户端设备 | client    | 每个 Client 用户可多个 | ROG、macbook           |
| pod     | Desktop.Pod 容器设备        | client    | 每个 Client 用户可多个 | ide-sc、ide-prod       |

说明：

- agent：运行 signal_agent 二进制的 Agent 模式，通过 Deploy Token 注册
- desktop：运行 signal_desktop（Wails 桌面应用），通过 Logto 登录注册
- pod：运行 signal_agent 二进制的 Client 模式（CloudIDE 容器），通过 Deploy Token 注册

Node 在三个系统中存在对应实体：

| 系统             | 实体            | 唯一标识              | 说明                                  |
| ---------------- | --------------- | --------------------- | ------------------------------------- |
| 数据库（SQLite） | Node 表         | user_id + type + name | 业务主体，存储 IP、心跳时间、系统信息 |
| Headscale        | Node            | HeadscaleNodeID       | 隧道节点，分配 Tailscale IP           |
| Server 内存      | connections map | NodeID                | gRPC 连接状态，实时在线判断           |

三个系统的数据关系：数据库 Node 通过 HeadscaleNodeID 关联 Headscale Node，通过 NodeID 关联内存连接表。

## 核心业务

### 设备创建

设备不由管理员手动创建，而是在以下场景自动创建：

场景一：Agent 注册/心跳时

```
Agent gRPC Register 或 Heartbeat
    │
    ├─ 查询 Node（user_id + type=agent + name=hostname）
    │   ├─ 存在 → 更新信息
    │   └─ 不存在 → 创建 Node
    │       ├─ Name = hostname
    │       ├─ Type = agent
    │       └─ 写入数据库
    │
    └─ 同一 Agent User 可以有多个 Node（多实例部署）
```

场景二：Desktop Logto 登录成功时

```
Logto 登录回调
    │
    ├─ 查询 Node（user_id + type=desktop + name=设备名）
    │   ├─ 存在 → 更新 SecretHash
    │   └─ 不存在 → 创建 Node
    │       ├─ Name = Desktop 端传入的设备名（hostname）
    │       ├─ Type = desktop
    │       ├─ SecretHash = 随机生成
    │       └─ 写入数据库
    │
    └─ 返回 NodeID + Secret 给 Desktop
```

场景三：Pod（CloudIDE）Deploy Token 注册时

```
Pod 启动 → POST /api/v1/register(token, fingerprint, device_name)
    │
    ├─ 验证 Deploy Token
    ├─ 绑定设备信息（更新 deploy_tokens 表）
    ├─ 获取或创建 Headscale User（client-{name}）
    ├─ 创建 PreAuthKey（带身份 Tag + 分组 Tag）
    │
    └─ Pod 首次心跳时创建 Node
        ├─ Name = device_name
        ├─ Type = pod
        └─ 写入数据库
```

### 设备删除

删除设备涉及三个系统。根据触发场景不同，分为单个删除和批量删除。

场景一：管理员单个删除（Web API）

```
管理员请求删除（Web API）
    │
    ├─ 1. 查询数据库 Node
    │
    ├─ 2. 关闭该设备的 gRPC 连接（如果在线）
    │
    ├─ 3. 删除 Headscale Node
    │      使用 Node.HeadscaleNodeID 调用 Headscale API
    │      失败时仅记录警告，不阻塞
    │
    ├─ 4. 删除数据库 Node 记录
    │
    ├─ 5. NodeCache.Invalidate()（心跳缓存同步）
    │
    └─ 6. 记录审计日志
```

场景二：Desktop 端删除自己的其他设备

```
Desktop gRPC DeleteDevice
    │
    ├─ 验证目标设备属于同一用户
    ├─ 不能删除当前设备
    ├─ 关闭目标设备的 gRPC 连接
    └─ 删除数据库 Node 记录
```

场景三：批量删除（用户删除触发）

用户删除时，由用户管理编排调用设备管理的批量删除。详见 `design_ztna_server_user.md`「删除用户」的子任务 1。

```
用户管理调用批量删除（user_id）
    │
    ├─ 1. 查询该用户的所有 Node
    │
    ├─ 2. 通过 NodeConnectionManager.CloseConnectionsByUser(userID)
    │      统一关闭该用户所有设备的 gRPC 连接
    │
    ├─ 3. 逐个删除 Headscale Node
    │      使用 Node.HeadscaleNodeID 调用 Headscale API
    │      单个失败仅记录警告，继续处理下一个
    │
    ├─ 4. 批量删除数据库 Node 记录（WHERE user_id = ?）
    │
    └─ 5. NodeCache.Invalidate()
```

与单个删除的区别：批量删除不记录审计日志（由用户删除统一记录），且必须先关闭所有 gRPC 连接再删数据库，避免设备在删除过程中重连创建新 Node。

场景四：通过隧道管理页面删除 Headscale Node

```
管理员删除隧道 Node
    │
    ├─ 调用 Headscale DeleteNode
    │
    └─ 根据 Headscale User 名称前缀判断类型
        ├─ agent-* → 清空对应 Node 的 IP
        └─ client-* → 删除对应 Node 记录
```

### 设备注销（Expire）

注销不删除设备，而是使 Headscale Node 过期，设备需要重新认证才能连接：

```
管理员请求注销
    │
    ├─ 调用 Headscale ExpireNode
    │
    └─ 设备的 Tailscale 连接会断开
        下次启动需要新的 AuthKey
```

### 设备下线（Offline）

Desktop 端可以下线自己的其他设备（不删除，仅断开连接）：

```
Desktop gRPC OfflineDevice
    │
    ├─ 验证目标设备属于同一用户
    ├─ 关闭目标设备的 gRPC 连接
    └─ 清除数据库 last_heartbeat
```

### 心跳

心跳是设备与 Server 保持连接的核心机制。当前实现每次心跳都写数据库，存在性能问题。详见 `design_ztna_server_heartbeat.md` 的优化设计。

当前心跳流程（待优化）：

```
设备 gRPC 心跳流建立
    │
    ├─ 首次消息：验证设备存在，注册内存连接
    │
    ├─ 每次心跳消息：
    │   ├─ 更新 last_heartbeat（写数据库）
    │   ├─ 更新 IP（写数据库）
    │   ├─ 查询 HeadscaleNodeID（如果为 0，查 Headscale API）
    │   └─ Agent：返回服务配置和端口转发配置
    │
    └─ 流断开时：
        ├─ 从内存连接表移除
        ├─ 清空数据库 IP 和 last_heartbeat
        └─ Agent：设置关联域名为离线
```

心跳问题：

1. 每次心跳都写数据库，设备多时压力大
2. 设备启动时 ts.net 未就绪，TunnelIp 为空，导致 IP 被清空后又写回
3. 断连时清空 IP，重连后又写回，产生无效更新

优化方案见 `design_ztna_server_heartbeat.md`。

### 设备查询

列表查询：

| 筛选条件 | 说明                  |
| -------- | --------------------- |
| type     | agent / desktop / pod |
| user_id  | 指定用户的设备        |
| search   | 按名称/主机名模糊搜索 |

在线状态判断优先级：

1. 内存连接表（connections）— 最准确
2. 数据库 last_heartbeat 在 60 秒内 — 有延迟

详情查询额外返回 Headscale Node 信息（实时从 Headscale API 获取）：

- IP 地址列表
- 在线状态
- ForcedTags
- 最后上线时间
- 过期时间

## 核心数据

### 数据库 Node 表

| 字段            | 类型   | 说明                                        |
| --------------- | ------ | ------------------------------------------- |
| ID              | uint64 | 自增主键                                    |
| UserID          | uint64 | 所属用户 ID                                 |
| Name            | string | 设备名称（联合唯一：user_id + type + name） |
| Type            | string | agent / desktop / pod                       |
| HeadscaleNodeID | uint64 | Headscale Node ID                           |
| IP              | string | Tailscale 隧道 IP                           |
| Version         | string | 软件版本号                                  |
| Hostname        | string | 主机名                                      |
| SystemInfo      | string | 系统信息 JSON（OS、CPU、内存等）            |
| SecretHash      | string | 设备认证密钥哈希（Desktop 用）              |
| LastHeartbeat   | time   | 最后心跳时间                                |

唯一约束：(UserID, Type, Name)

### Headscale Node

| 属性        | 说明                                            |
| ----------- | ----------------------------------------------- |
| ID          | Headscale 内部 ID，存储在数据库 HeadscaleNodeID |
| GivenName   | 节点名称，与数据库 Node.Name 对应               |
| User        | 所属 Headscale User（{role}-{name}）            |
| IpAddresses | Tailscale 分配的 IP 列表                        |
| Online      | 实时在线状态                                    |
| ForcedTags  | ACL Tag 列表                                    |

### 内存连接表

所有设备类型（agent、desktop、pod）使用统一的连接结构和连接表，由 NodeConnectionManager 统一管理。

NodeConnection（统一连接结构）：

| 字段      | 说明                              |
| --------- | --------------------------------- |
| NodeID    | Node ID（统一用 NodeID 作为 key） |
| UserID    | 所属用户 ID                       |
| NodeType  | agent / desktop / pod             |
| TunnelIP  | 隧道 IP（设备上报）               |
| Connected | 隧道是否已连接                    |
| LastSeen  | 最后活跃时间                      |
| Cancel    | 取消函数，用于关闭 gRPC 连接      |

连接表：key = NodeID，所有设备类型共用一个 map。

NodeConnectionManager 提供的能力：

| 方法                        | 说明                                          |
| --------------------------- | --------------------------------------------- |
| Register(conn)              | 注册连接，同 NodeID 旧连接自动踢掉            |
| Unregister(nodeID)          | 注销连接，返回连接信息供调用方处理断连        |
| UpdateHeartbeat(...)        | 更新心跳信息（TunnelIP、Connected、LastSeen） |
| IsNodeOnline(nodeID)        | 判断单个设备是否在线                          |
| GetOnlineNodesByUser(uid)   | 获取某用户的所有在线设备                      |
| HasOnlineNode(userID, type) | 判断某用户是否有指定类型的在线设备            |
| CloseConnection(nodeID)     | 关闭单个设备的 gRPC 连接                      |
| CloseConnectionsByUser(uid) | 关闭某用户的所有 gRPC 连接                    |

gRPC Service 保持 AgentService 和 DesktopService 两个独立服务（proto 消息格式不同，心跳响应内容不同），但都使用同一个 NodeConnectionManager 管理连接状态。Pod 复用 AgentService 的 gRPC（通过 user.Role 判断行为差异）。

## 数据库与 Headscale 的关联

### HeadscaleNodeID 的写入时机

HeadscaleNodeID 是数据库 Node 与 Headscale Node 的关联 ID，一旦写入就不会变化（除非设备被删除）。

写入时机：心跳中，从无到有的那一刻，写入一次数据库，仅此一次。详见 `design_ztna_server_heartbeat.md`「HeadscaleNodeID 首次写入」。

逻辑关联（Headscale User + GivenName = 数据库 User + Node.Name）在设备认证时就已确定，详见 `design_ztna_server_user.md`「设备认证」。

SyncAllNodeTags 不负责 HeadscaleNodeID 的写入，只负责 Tag 同步。HeadscaleNodeID == 0 的 Node 在 Tag 同步时跳过。

### IP 的来源

设备 IP 来源于心跳上报。设备端从 ts.net 获取 IP 后通过心跳上报给 Server。

### Tag 同步

SyncAllNodeTags 负责将数据库中的权限信息同步为 Headscale Node 的 ForcedTags：

```
遍历数据库所有 Node
    │
    ├─ HeadscaleNodeID == 0 → 跳过（等首次心跳写入）
    │
    ├─ 通过 HeadscaleNodeID 找到 Headscale Node
    │
    ├─ 构建期望的 Tag 列表
    │   ├─ 身份 Tag：tag:{role}-{name}
    │   └─ 分组 Tag：tag:group-{group_name}（查询 GroupMember）
    │
    └─ 与当前 ForcedTags 比较，不同则调用 SetTags 更新
```

## 设备生命周期

### Agent 设备

```
┌─────────┐    ┌──────────┐    ┌──────────┐    ┌──────────┐
│ 不存在   │───▶│ 注册     │───▶│ 在线     │───▶│ 离线     │
└─────────┘    └──────────┘    └──────────┘    └──────────┘
                                    │  ▲            │
                                    │  │            │
                                    │  └────────────┘
                                    │     重连
                                    ▼
                               ┌──────────┐
                               │ 删除     │
                               └──────────┘
```

- 注册：Agent gRPC Register，创建 Node + Headscale PreAuthKey
- 在线：gRPC 心跳流保持，内存连接表有记录
- 离线：gRPC 流断开，内存连接表移除
- 删除：管理员通过 Web 删除，清理数据库 + Headscale

### Desktop 设备

```
┌─────────┐    ┌──────────┐    ┌──────────┐    ┌──────────┐
│ 不存在   │───▶│ 登录创建  │───▶│ 在线     │───▶│ 离线     │
└─────────┘    └──────────┘    └──────────┘    └──────────┘
                                    │  ▲            │
                                    │  │            │
                                    │  └────────────┘
                                    │     重连
                                    ▼
                          ┌──────────┐  ┌──────────┐
                          │ 注销     │  │ 删除     │
                          └──────────┘  └──────────┘
```

- 登录创建：Logto 登录成功后创建 Node
- 在线：gRPC 心跳流 + 数据流保持
- 离线：gRPC 流断开
- 注销（Logout）：用户主动注销，清除心跳时间，注销 Logto 会话
- 删除：管理员或用户自己删除其他设备

### CloudIDE（Pod）设备

```
┌─────────┐    ┌──────────┐    ┌──────────┐    ┌──────────┐
│ 不存在   │───▶│ Token注册 │───▶│ 在线     │───▶│ 离线     │
└─────────┘    └──────────┘    └──────────┘    └──────────┘
                                    │  ▲            │
                                    │  │            │
                                    │  └────────────┘
                                    │   Pod 重启
                                    ▼
                               ┌──────────┐
                               │ 删除     │
                               └──────────┘
```

- Token 注册：通过 Deploy Token + REST API 注册
- 在线：Agent gRPC 心跳流保持（Pod 内运行 Agent 二进制）
- 离线：Pod 销毁，gRPC 流断开
- 删除：管理员删除或 Pod 清理

## 边界情况

### 同名设备

同一 User 下不允许同 Type 同 Name 的 Node（联合唯一约束）。如果设备 hostname 变更，会创建新 Node 而不是更新旧 Node。旧 Node 会因为没有心跳而显示离线。

### Headscale Node 残留

设备从数据库删除后，如果 Headscale 删除失败，Headscale 中会残留 Node。这不影响业务（没有对应的数据库 Node，不会分配 Tag），但会占用 Tailscale IP。管理员可通过隧道管理页面手动清理。

### 多设备 IP 冲突

同一 Headscale User 下的多个 Node 各自有独立的 Tailscale IP。SyncAllNodeTags 通过 GivenName 精确匹配，确保每个数据库 Node 关联正确的 Headscale Node，避免 IP 写错。
