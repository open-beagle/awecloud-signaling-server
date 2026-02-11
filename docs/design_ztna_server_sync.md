# Server 定时同步设计

相关文档：

- `design_ztna_server_heartbeat.md` — 心跳业务设计（NodeCache 定时写入/读取）
- `design_ztna_server_device.md` — 设备管理设计（Tag 同步、HeadscaleNodeID）
- `design_ztna_acl.md` — 授权管理设计（ACL Policy 同步）

## 概述

Server 运行期间有多个定时同步任务，负责将内存状态持久化、从外部系统同步数据、保持数据一致性。这些任务需要统一管理，避免各自为政。

## 同步任务清单

| 任务            | 间隔    | 数据方向           | 说明                                       |
| --------------- | ------- | ------------------ | ------------------------------------------ |
| NodeCache 写入  | 5 分钟  | 内存 → 数据库      | 将 Dirty 的心跳数据批量写入数据库          |
| NodeCache 读取  | 10 分钟 | 数据库 → 内存      | 从数据库重新加载 Node，同步外部变更        |
| SyncAllNodeTags | 5 分钟  | 数据库 → Headscale | 同步 Node 的 ForcedTags 到 Headscale       |
| SyncACL         | 5 分钟  | 数据库 → Headscale | 根据授权关系生成 ACL Policy 写入 Headscale |

## 当前问题

当前 SyncAllNodeTags 和 SyncACL 在同一个 goroutine 中串行执行，共用一个 5 分钟 ticker。问题：

1. 没有统一的任务管理，各任务分散在不同地方启动
2. SyncAllNodeTags 做了不该做的事（写 HeadscaleNodeID 和 IP），职责不清
3. NodeCache 的定时写入/读取是新增任务，需要和现有任务协调
4. 任务之间有依赖关系（NodeCache 写入应在 SyncAllNodeTags 之前），但当前没有编排

## 设计方案

### SyncManager

统一的同步任务管理器，负责注册、调度、启停所有定时同步任务。

SyncManager 的职责：

| 职责     | 说明                                           |
| -------- | ---------------------------------------------- |
| 注册任务 | 每个任务注册名称、间隔、执行函数               |
| 调度执行 | 按各自间隔独立触发，不互相阻塞                 |
| 依赖编排 | 支持任务组（同一组内串行执行，保证顺序）       |
| 启停控制 | 统一启动、优雅停止，context 取消时所有任务停止 |
| 错误处理 | 单个任务失败不影响其他任务，记录日志           |
| 手动触发 | 支持外部调用立即执行某个任务（如 ACL 变更后）  |

### 任务分组

任务分为两组，组内串行，组间并行：

```
┌─────────────────────────────────────────────┐
│              SyncManager                     │
│                                              │
│  ┌─────────────────────┐  ┌───────────────┐ │
│  │ 心跳组（5 分钟）     │  │ Headscale 组  │ │
│  │                     │  │ （5 分钟）     │ │
│  │ 1. NodeCache 写入   │  │               │ │
│  │                     │  │ 1. Tag 同步   │ │
│  │                     │  │ 2. ACL 同步   │ │
│  └─────────────────────┘  └───────────────┘ │
│                                              │
│  ┌─────────────────────┐                     │
│  │ 缓存组（10 分钟）    │                     │
│  │                     │                     │
│  │ 1. NodeCache 读取   │                     │
│  └─────────────────────┘                     │
└─────────────────────────────────────────────┘
```

分组说明：

| 组           | 间隔    | 包含任务            | 串行原因                                       |
| ------------ | ------- | ------------------- | ---------------------------------------------- |
| 心跳组       | 5 分钟  | NodeCache 写入      | 将内存中的心跳数据持久化                       |
| Headscale 组 | 5 分钟  | Tag 同步 → ACL 同步 | Tag 同步先执行，确保 Node Tag 正确后再同步 ACL |
| 缓存组       | 10 分钟 | NodeCache 读取      | 从数据库重新加载，同步外部变更                 |

### 执行时序

```
T=0（启动）
    │
    ├─ 心跳组：NodeCache 写入（首次无 Dirty 数据，快速跳过）
    ├─ Headscale 组：Tag 同步 → ACL 同步（初始全量同步）
    │
T=5min
    │
    ├─ 心跳组：NodeCache 写入
    ├─ Headscale 组：Tag 同步 → ACL 同步
    │
T=10min
    │
    ├─ 心跳组：NodeCache 写入
    ├─ Headscale 组：Tag 同步 → ACL 同步
    ├─ 缓存组：NodeCache 读取
    │
T=15min
    │
    ├─ 心跳组：NodeCache 写入
    ├─ Headscale 组：Tag 同步 → ACL 同步
    │
    ... 以此类推
```

### 手动触发

某些业务操作后需要立即同步，不等定时器：

| 触发场景                   | 触发的任务          | 说明                                |
| -------------------------- | ------------------- | ----------------------------------- |
| ACL 授权变更（增删改权限） | ACL 同步            | 权限变更后立即生效                  |
| 用户创建/删除              | Tag 同步 + ACL 同步 | 新用户的 Tag 需要同步，ACL 需要更新 |
| 分组成员变更               | Tag 同步 + ACL 同步 | 分组 Tag 变化，ACL 规则变化         |
| 设备删除/修改              | NodeCache 读取      | 通过 Invalidate 触发，已有机制      |

手动触发不影响定时器的节奏。如果手动触发时定时任务正在执行，等当前执行完成后再执行手动触发的。

## SyncAllNodeTags 改造

### 当前职责（需要拆分）

SyncAllNodeTags 当前做了三件事：

1. 匹配并写入 HeadscaleNodeID 和 IP → 移除，改由心跳首次写入
2. 匹配失败时清空 HeadscaleNodeID 和 IP → 移除
3. 同步 ForcedTags → 保留，这是唯一职责

### 改造后职责

SyncAllNodeTags 只做 Tag 同步：

```
获取 Headscale 所有 Node 列表
    │
    ├─ 建立 HeadscaleNodeID → Headscale Node 的映射
    │
    ├─ 遍历数据库所有 Node
    │   │
    │   ├─ HeadscaleNodeID == 0 → 跳过
    │   │
    │   ├─ 在映射中找到 Headscale Node
    │   │   ├─ 找到 → 比较 Tag，不同则更新
    │   │   └─ 未找到 → 记录警告，跳过
    │   │
    │   └─ 构建期望的 Tag 列表
    │       ├─ 身份 Tag：tag:{role}-{name}
    │       └─ 分组 Tag：tag:group-{group_name}
    │
    └─ 完成
```

关键变化：

- 不再通过 User + GivenName 匹配写入 HeadscaleNodeID
- 不再更新 IP
- 不再清空 HeadscaleNodeID 和 IP
- 直接用 HeadscaleNodeID 查找 Headscale Node（O(1) 查找，不需要遍历匹配）

## SyncACL 保持不变

SyncACL 的逻辑不需要改造。它从数据库读取 User、Group、Permission，生成完整的 ACL Policy 写入 Headscale。与 Node 无关，不受 HeadscaleNodeID 改造影响。

## 启动和停止

### 启动

```
Server 启动
    │
    ├─ 初始化 NodeCache（从数据库加载）
    │
    ├─ 初始化 SyncManager
    │   ├─ 注册心跳组任务
    │   ├─ 注册 Headscale 组任务（如果 Headscale 已配置）
    │   └─ 注册缓存组任务
    │
    └─ SyncManager.Start(ctx)
        ├─ 各组首次执行
        └─ 各组定时器启动
```

### 停止

```
Server 收到 SIGTERM
    │
    ├─ 取消 context
    │
    ├─ SyncManager.Stop()
    │   ├─ 等待正在执行的任务完成
    │   └─ NodeCache 最后一次写入（确保 Dirty 数据不丢失）
    │
    └─ gRPC GracefulStop
```

## 涉及的文件

| 文件                                  | 变更说明                                                                     |
| ------------------------------------- | ---------------------------------------------------------------------------- |
| internal/server/cache/sync_manager.go | 新增，SyncManager 实现                                                       |
| internal/server/headscale/acl_sync.go | 改造 SyncAllNodeTags（移除 HeadscaleNodeID/IP 写入），移除 StartPeriodicSync |
| internal/server/server.go             | 改用 SyncManager 启动同步任务                                                |
| internal/server/cache/node_cache.go   | 新增，NodeCache 实现（心跳设计文件已定义）                                   |
