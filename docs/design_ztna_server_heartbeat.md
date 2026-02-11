# Server 心跳业务设计

相关文档：

- `design_ztna_server_user.md` — 用户管理设计
- `design_ztna_server_device.md` — 设备管理设计
- `design_ztna_server_sync.md` — 定时同步设计（NodeCache 定时写入/读取的调度）

## 背景

当前心跳实现存在以下问题：

1. 每次心跳都写数据库（更新 last_heartbeat、ip），设备多时数据库压力大
2. 设备启动时 ts.net 未初始化完成，TunnelIp 为空，导致数据库 IP 被清空；下次心跳拿到 IP 后又写回，产生 IP → 空 → IP 的无效更新
3. 心跳间隔通常 10-30 秒，100 台设备意味着每秒 3-10 次数据库写入，全部是 last_heartbeat 更新

## 设计目标

- 心跳业务在内存中维护设备表，减少数据库读写
- 心跳导致的变更最多每 5 分钟写一次数据库
- 心跳业务最多每 10 分钟从数据库读一次（同步外部变更）
- 其他业务（删除设备、修改设备等）正常读写数据库，不受限制
- 其他业务的写操作会触发心跳业务立即从数据库重新加载

## 架构概览

```
┌──────────────┐     ┌──────────────────┐     ┌──────────┐
│ Agent/Desktop │────▶│  NodeCache       │────▶│  SQLite  │
│ /Pod 心跳gRPC │     │  (内存设备表)     │     │  (Node表) │
└──────────────┘     └──────────────────┘     └──────────┘
                            ▲
                            │ Invalidate()
                     ┌──────┴───────┐
                     │  其他业务     │
                     │  (API/gRPC)  │
                     └──────────────┘
```

所有设备类型（agent、desktop、pod）的心跳都经过 NodeCache，gRPC 连接状态由统一的 NodeConnectionManager 管理（详见 `design_ztna_server_device.md`「内存连接表」）。

## 核心组件：NodeCache

### 数据结构

内存设备表，以 NodeID 为 key，缓存 Node 的关键字段。

字段说明：

| 字段            | 来源     | 说明                                                    |
| --------------- | -------- | ------------------------------------------------------- |
| NodeID          | 数据库   | 主键                                                    |
| UserID          | 数据库   | 所属用户                                                |
| Name            | 数据库   | 设备名称                                                |
| Type            | 数据库   | agent / desktop / pod                                   |
| IP              | 心跳上报 | 隧道 IP                                                 |
| HeadscaleNodeID | 心跳查询 | Headscale 节点 ID（首次心跳时一次性写入，之后不再变化） |
| Hostname        | 心跳上报 | 主机名                                                  |
| LastHeartbeat   | 心跳上报 | 最后心跳时间                                            |
| Dirty           | 内部标记 | 是否有未持久化的变更                                    |

### 生命周期

```
Server 启动
    │
    ▼
从数据库加载所有 Node → 填充内存表
    │
    ▼
心跳到来 → 更新内存表（标记 Dirty）
    │
    ▼
定时器（5分钟）→ 将 Dirty 的记录批量写入数据库
    │
    ▼
定时器（10分钟）→ 从数据库重新加载（同步外部变更）
    │
    ▼
其他业务调用 Invalidate() → 立即从数据库重新加载
```

## 心跳处理流程

### 心跳到来时

```
收到心跳请求 (nodeID, tunnelIp, hostname, ...)
    │
    ├─ 内存表中有该 Node？
    │   ├─ 是 → 更新内存字段
    │   └─ 否 → 从数据库查询该 Node
    │           ├─ 存在 → 加入内存表，更新字段
    │           └─ 不存在 → 创建 Node（立即写数据库），加入内存表
    │
    ├─ tunnelIp 为空？
    │   ├─ 是 → 不更新 IP 字段（ts.net 未就绪，保持原值）
    │   └─ 否 → IP 与内存中不同？
    │           ├─ 是 → 更新内存 IP，标记 Dirty
    │           └─ 否 → 不操作
    │
    ├─ HeadscaleNodeID == 0 且 tunnelIp 不为空？
    │   └─ 是 → 查询 Headscale API（按 User + GivenName 精确匹配）
    │           ├─ 匹配成功 → 写入 HeadscaleNodeID（立即写数据库，一次性）
    │           └─ 匹配失败 → 不处理（下次心跳重试）
    │
    ├─ 更新 LastHeartbeat 为当前时间
    │
    └─ 标记 Dirty（仅 last_heartbeat 变更不立即写库）
```

### 设备断连时

```
gRPC 流断开
    │
    ├─ 从内存连接表（connections）中移除
    │
    ├─ 更新内存表：LastHeartbeat = nil, IP = ""
    │
    └─ 标记 Dirty，等待下次定时写入
        （不立即写数据库，减少断连风暴时的数据库压力）
```

### 定时写入（每 5 分钟）

```
遍历内存表
    │
    ├─ 筛选 Dirty == true 的记录
    │
    ├─ 批量 UPDATE（使用事务）
    │   更新字段：ip, last_heartbeat, headscale_node_id, hostname
    │
    └─ 清除 Dirty 标记
```

### 定时读取（每 10 分钟）

```
从数据库 SELECT 所有 Node
    │
    ├─ 与内存表合并
    │   ├─ 数据库中有、内存中没有 → 加入内存表
    │   ├─ 数据库中没有、内存中有 → 从内存表移除（设备已被删除）
    │   └─ 都有 → 保留内存中的 Dirty 字段值（未持久化的心跳数据优先）
    │
    └─ 更新 lastLoadTime
```

## HeadscaleNodeID 首次写入

HeadscaleNodeID 是数据库 Node 与 Headscale Node 的关联 ID，一旦写入就不会变化（除非设备被删除）。

### 写入条件

同时满足以下两个条件时触发：

1. node.HeadscaleNodeID == 0（还没关联）
2. TunnelIP 不为空（设备已连上 Headscale）

### 写入流程

```
心跳到来，HeadscaleNodeID == 0 且 TunnelIP 不为空
    │
    ├─ 通过 User 名称（{role}-{name}）找到 Headscale User
    │
    ├─ 在该 User 下按 GivenName == Node.Name 精确匹配
    │
    ├─ 匹配成功 → 立即写入数据库（不走 Dirty 机制，直接持久化）
    │
    └─ 匹配失败 → 不处理，下次心跳重试
```

### 为什么立即写数据库

HeadscaleNodeID 从无到有是一次性的关键状态变更，不同于 IP 和 LastHeartbeat 这类高频更新字段。立即写数据库确保：

- 不会因 Server 崩溃丢失这个关联关系
- 其他业务（删除设备、Tag 同步等）能立即使用

### 为什么不在认证时写入

设备认证时 Server 创建 PreAuthKey 返回给设备，此时 Headscale Node 还不存在（设备还没拿 AuthKey 去连 Headscale）。Headscale 也没有 webhook 通知机制。所以只能等设备连上 Headscale 后的首次心跳来确认。

认证时确定的是 Node.Name（逻辑关联），首次心跳时确定的是 HeadscaleNodeID（物理关联）。

## 外部业务交互

### 其他业务写数据库后

其他业务（API 删除设备、修改设备信息等）在写完数据库后，调用 NodeCache.Invalidate()。

Invalidate 的行为：

```
Invalidate()
    │
    ├─ 将 Dirty 记录先写入数据库（避免丢失心跳数据）
    │
    └─ 从数据库重新加载所有 Node（覆盖内存表）
```

### 其他业务读设备信息

其他业务需要读取 Node 信息时，有两种选择：

| 场景                 | 读取来源                  | 说明                         |
| -------------------- | ------------------------- | ---------------------------- |
| 需要实时在线状态     | 内存连接表（connections） | 已有机制，不变               |
| 需要设备 IP          | NodeCache 内存表          | 比数据库更新（心跳实时更新） |
| 需要设备完整信息     | 数据库                    | 不经过 NodeCache             |
| Web 页面展示设备列表 | 数据库                    | 列表查询走数据库，不影响     |

### 设备被删除的处理

心跳期间设备被删除的场景：

```
时间线：
T0: 设备 A 心跳正常，内存表中存在
T1: 管理员通过 Web 删除设备 A（数据库删除 + Invalidate）
T2: Invalidate 触发重新加载，设备 A 从内存表移除
T3: 设备 A 的下一次心跳到来
    → 内存表中找不到
    → 查数据库也找不到
    → 心跳被忽略（不创建新记录）
    → gRPC 流返回错误，设备端断开
```

关键点：删除设备时，除了数据库操作，还需要关闭该设备的 gRPC 连接（已有逻辑），这样设备端会感知到断连。

## 在线状态判断

当前在线状态判断方式不变：

| 判断方式                  | 说明                    |
| ------------------------- | ----------------------- |
| 内存连接表（connections） | gRPC 流是否存在，最准确 |
| NodeCache.LastHeartbeat   | 内存中的心跳时间，实时  |
| 数据库 last_heartbeat     | 可能延迟最多 5 分钟     |

建议优先级：connections > NodeCache > 数据库

## 涉及的现有代码

需要改造的位置：

| 文件                  | 函数                   | 当前行为                     | 改造后                                  |
| --------------------- | ---------------------- | ---------------------------- | --------------------------------------- |
| grpc/（新增）         | NodeConnectionManager  | 无（Agent/Desktop 各自管理） | 统一连接管理器，key=NodeID              |
| agent_service.go      | handleHeartbeat        | 每次心跳写数据库             | 更新 NodeCache 内存                     |
| agent_service.go      | Heartbeat 连接注册     | 用 UserID 作为 key           | 改用 NodeID 作为 key，通过 Manager 注册 |
| agent_service.go      | Heartbeat defer        | 断连时写数据库清空 IP        | 更新 NodeCache 内存                     |
| desktop_service.go    | handleDesktopHeartbeat | 每次心跳写数据库             | 更新 NodeCache 内存                     |
| desktop_service.go    | Heartbeat 连接注册     | 独立 connections map         | 改用统一 Manager 注册                   |
| desktop_service.go    | Heartbeat defer        | 断连时写数据库清空 IP        | 更新 NodeCache 内存                     |
| desktop_service.go    | Logout                 | 清除 last_heartbeat          | 直接写数据库 + Invalidate               |
| api/node.go           | 删除/修改 Node         | 直接写数据库                 | 写数据库 + Invalidate                   |
| api/tunnel.go         | 清空 Agent IP          | 直接写数据库                 | 写数据库 + Invalidate                   |
| headscale/acl_sync.go | SyncAllNodeTags        | 更新 IP 和 HeadscaleNodeID   | 写数据库 + Invalidate                   |

不需要改造的位置：

| 文件             | 函数         | 说明                       |
| ---------------- | ------------ | -------------------------- |
| api/user.go      | 用户列表统计 | 读数据库，展示用可接受延迟 |
| api/tailscale.go | 状态统计     | 读数据库，统计用可接受延迟 |

## 新增文件

```
internal/server/cache/node_cache.go          # NodeCache 实现
internal/server/cache/node_connection.go     # NodeConnectionManager 实现（统一连接管理）
```

## 时序图：心跳完整流程

```
Agent/Desktop/Pod      NodeCache(内存)         SQLite
    │                      │                     │
    │── 心跳请求 ──────────▶│                     │
    │                      │── 查内存表           │
    │                      │   (命中)             │
    │                      │── 更新 IP/心跳时间   │
    │                      │── 标记 Dirty         │
    │◀── 心跳响应 ─────────│                     │
    │                      │                     │
    │                      │    ... 5 分钟后 ...  │
    │                      │                     │
    │                      │── 批量写入 ─────────▶│
    │                      │   (Dirty 记录)       │
    │                      │◀── 写入完成 ─────────│
    │                      │── 清除 Dirty         │
```

## 时序图：设备删除流程

```
Web管理员              API               NodeCache          SQLite
    │                   │                   │                  │
    │── 删除设备 ──────▶│                   │                  │
    │                   │── DELETE ─────────────────────────▶│
    │                   │◀── 删除完成 ──────────────────────│
    │                   │── Invalidate() ──▶│                  │
    │                   │                   │── 写 Dirty ────▶│
    │                   │                   │── 重新加载 ◀────│
    │                   │                   │   (设备 A 消失)  │
    │                   │── 关闭 gRPC 连接   │                  │
    │◀── 删除成功 ──────│                   │                  │
```

## 边界情况

### Server 重启

Server 重启时从数据库加载所有 Node，内存表重建。此时所有设备的 gRPC 连接都会断开，设备端会重连并重新发送心跳。

### 并发安全

NodeCache 使用 sync.RWMutex 保护：

- 心跳更新：写锁（按 NodeID 粒度，可考虑分片锁优化）
- 定时写入：写锁
- 定时读取：写锁
- Invalidate：写锁
- 外部读取：读锁

### Dirty 数据丢失

如果 Server 在定时写入前崩溃，最多丢失 5 分钟的心跳数据（last_heartbeat 和 IP）。这是可接受的，因为设备重连后会重新上报。

### 心跳与 Invalidate 竞争

心跳更新内存的同时，Invalidate 触发重新加载：

- Invalidate 先将 Dirty 数据写入数据库
- 然后从数据库重新加载
- 心跳在 Invalidate 期间的更新可能被覆盖
- 但下次心跳会重新更新，影响极小（最多丢一次心跳时间）

## 立即上报机制

### 背景

Agent 的 K8S Service Informer 实时监听变更，但只在心跳时上报（默认 30 秒周期）。Web 管理员在资源发现页面需要立即看到最新数据时，30 秒等待体验不佳。

### 设计方案

通过心跳响应流中新增 `request_immediate_report` 标志位，Server 通知 Agent 立即发送一次心跳（携带最新的 K8S Service 数据）。

### 触发流程

```
Web 管理员点击"更新"按钮
    │
    ▼
POST /api/v1/resources/sync
    │
    ▼
Server 设置全局标志 requestImmediateReport = true
    │
    ▼
下一次心跳响应时，Server 在 AgentHeartbeatResponse 中
设置 request_immediate_report = true，然后清除标志
    │
    ▼
Agent 收到 request_immediate_report = true
    │
    ▼
Agent 立即发送一次心跳请求（携带最新 discovered_services）
    │
    ▼
Server 收到心跳，更新 K8S Service 缓存
    │
    ▼
Web 前端等待 3 秒后刷新列表，展示最新数据
```

### Proto 变更

AgentHeartbeatResponse 新增字段：

| 字段                     | 类型 | 说明                                   |
| ------------------------ | ---- | -------------------------------------- |
| request_immediate_report | bool | Server 请求 Agent 立即上报一次心跳数据 |

### REST API

POST /api/v1/resources/sync

- 无请求参数
- 响应：标准成功响应
- 作用：设置全局标志，通知所有在线 Agent 在下一次心跳响应时立即上报

### Agent 端处理

Agent 在收到心跳响应后检查 `request_immediate_report` 字段：

```
收到 AgentHeartbeatResponse
    │
    ├─ request_immediate_report == true？
    │   └─ 是 → 立即发送一次完整心跳（不等待下一个心跳周期）
    │
    └─ 继续正常处理其他响应字段（config_version、services 等）
```

### 时序图

```
Web管理员        Server API       心跳响应流        Agent
    │                │                │               │
    │── POST sync ──▶│                │               │
    │◀── 200 OK ─────│                │               │
    │                │── 设置标志 ────▶│               │
    │                │                │               │
    │                │                │  (下次心跳响应) │
    │                │                │── resp ───────▶│
    │                │                │  (immediate    │
    │                │                │   =true)       │
    │                │                │               │
    │                │                │◀── 立即心跳 ──│
    │                │                │  (携带最新     │
    │                │                │   services)    │
    │                │                │               │
    │  (3秒后刷新)    │                │               │
    │── GET resources▶│                │               │
    │◀── 最新数据 ───│                │               │
```

### 边界情况

- 没有在线 Agent 时：sync API 正常返回，但无 Agent 收到通知，前端刷新后数据不变
- 多次快速点击：标志位是布尔值，多次设置效果等同一次
- Agent 心跳间隔内：最坏情况等待一个心跳周期（30 秒）后 Agent 才收到通知，前端 3 秒后刷新可能还没拿到最新数据，用户可再次点击
