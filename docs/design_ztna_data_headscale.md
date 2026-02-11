# ZTNA Headscale 数据交互设计

## 概述

Headscale 是 Server 的外部数据源之一。Server 通过 gRPC 与 Headscale 交互，管理节点、Tag、ACL Policy 和 PreAuthKey。

本文档定义 Server 与 Headscale 之间的数据交互方式，以及 Headscale 数据如何参与业务实体组装。

相关业务设计文档：

- `design_ztna_server_user.md` — 用户管理中的 Headscale User 创建/删除、HeadscaleUID 关联
- `design_ztna_server_device.md` — 设备管理中的 HeadscaleNodeID 写入、Tag 同步、IP 来源

## Headscale 提供的数据

| 数据       | 获取方式         | 用途                             |
| ---------- | ---------------- | -------------------------------- |
| Node 列表  | ListNodes RPC    | 节点在线状态、IP、Tag            |
| Node 详情  | GetNode RPC      | 单个节点信息                     |
| PreAuthKey | CreatePreAuthKey | Agent/Desktop 注册时生成认证密钥 |
| ACL Policy | SetPolicy RPC    | 推送网络层访问控制规则           |
| Node Tags  | SetTags RPC      | 设置节点身份和分组标签           |

## 数据写入 Headscale

Server 向 Headscale 写入三类数据：

### 1. 节点 Tag

每个 Headscale 节点携带 Tag，标识身份和分组归属。

Tag 格式：

- 身份 Tag：tag:{role}-{user.name}，如 tag:agent-beijing、tag:client-zhangsan
- 分组 Tag：tag:group-{group.name}，如 tag:group-北京机房

Tag 来源：

- 身份 Tag — 从 User 表的 role + name 生成
- 分组 Tag — 从 GroupMember + Group 表查询生成

同步时机：

- SyncAllNodeTags — 启动时 + 每 5 分钟全量同步
- 管理员修改分组成员时触发增量同步

同步流程：

1. 从 DB 查询所有 Node（Preload User）
2. 从 DB 查询每个 Node 的 User 所属 Group
3. 构建期望 Tag 列表
4. 从 Headscale ListNodes 获取当前 Tag
5. 对比差异，有变更则 SetTags

### 2. ACL Policy

ACL Policy 控制 Tailscale 网络层的访问规则（第 1 层 + 第 2 层）。

Policy 结构：

- tagOwners — 所有使用到的 Tag 的声明
- acls — 网络访问规则（src tag → dst tag:port）
- ssh — SSH 访问规则（src tag → dst tag, users）

ACL 规则来源（DB → Headscale ACL）：

| DB 表                     | 生成的 ACL 规则                                     |
| ------------------------- | --------------------------------------------------- |
| 分组内互访                | tag:group-X → tag:group-X:\*                        |
| AclServiceUserPermission  | tag:client-X → tag:agent-Y:端口                     |
| AclServiceGroupPermission | tag:group-X → tag:agent-Y:端口                      |
| AclUserUserPermission     | tag:client-X → tag:agent-Y:\*                       |
| AclUserGroupPermission    | tag:group-X → tag:agent-Y:\*                        |
| AclGroupUserPermission    | tag:client-X → tag:group-Y:\*                       |
| AclGroupGroupPermission   | tag:group-X → tag:group-Y:\*                        |
| 同用户互访                | tag:client-X → tag:client-X:\*（Client 多设备互访） |

SSH 规则来源（DB → Headscale SSH）：

| DB 表                 | 生成的 SSH 规则                                   |
| --------------------- | ------------------------------------------------- |
| AclSSHUserPermission  | tag:client-X → tag:agent-Y, users: [root, deploy] |
| AclSSHGroupPermission | tag:group-X → tag:agent-Y, users: [root, deploy]  |

不同步到 Headscale 的授权（第 3 层 + 第 4 层）：

| 授权类型        | 原因                                   | 替代方案         |
| --------------- | -------------------------------------- | ---------------- |
| K8SAPI 授权     | 需要 namespace + role 级别控制         | 心跳下发到 Agent |
| K8SService 授权 | 需要 namespace + service name 级别控制 | 心跳下发到 Agent |
| Endpoint 授权   | Endpoint 不在 Tailscale 网络中         | 心跳下发到 Agent |

同步时机：

- 启动时全量同步
- 每 5 分钟定时全量同步
- 管理员操作授权变更时立即同步

### 3. PreAuthKey

Agent/Desktop 注册到 Tailscale 网络时需要 PreAuthKey。

生成时机：

- Agent Register RPC — Agent 首次注册
- Agent Authenticate RPC — Agent 重连（如需重新连接）
- Desktop 登录完成 — Desktop.Host Logto 登录成功后
- Deploy Token 注册 — Desktop.Pod / Agent 使用 Deploy Token 注册

参数：

- user — Headscale User ID（从 User.HeadscaleUID 获取）
- expiry — 有效期（从 SystemConfig 读取，默认 24 小时）
- ephemeral — false（需要持久化节点）
- tags — 身份 Tag + 分组 Tag

## 数据读取 Headscale

Server 从 Headscale 读取数据的场景：

### Tag 同步时读取

SyncAllNodeTags 流程中，ListNodes 获取所有节点的当前 Tag 和 IP，用于：

- 对比期望 Tag 和实际 Tag，决定是否需要 SetTags
- 更新 DB 中 Node 的 HeadscaleNodeID 和 IP

### 业务实体组装时读取

| 业务实体     | 读取内容        | 场景               |
| ------------ | --------------- | ------------------ |
| Agent.Detail | Node.ForcedTags | 详情页展示节点 Tag |

说明：Agent.Detail 是唯一需要实时查询 Headscale 的业务实体。其他实体的 Headscale 相关数据（IP、在线状态）已通过 Tag 同步写入 DB（Node.IP）或通过 Agent 心跳写入 Cache（AgentTsStatus）。

## 节点管理

| 操作     | 触发场景                | Headscale RPC    |
| -------- | ----------------------- | ---------------- |
| 创建用户 | 管理员创建 Agent/Client | CreateUser       |
| 删除用户 | 管理员删除 Agent/Client | DeleteUser       |
| 创建密钥 | Agent/Desktop 注册      | CreatePreAuthKey |
| 设置 Tag | Tag 同步                | SetTags          |
| 设置策略 | ACL 同步                | SetPolicy        |
| 删除节点 | 管理员删除设备          | DeleteNode       |
| 下线节点 | 管理员下线设备          | ExpireNode       |

## ZTNA 变更

Headscale 交互在 ZTNA 中的变更很小：

| 模块       | 变更                                      |
| ---------- | ----------------------------------------- |
| Tag 同步   | 不变                                      |
| ACL 同步   | 不变（新增的第 3-4 层授权不走 Headscale） |
| PreAuthKey | 不变                                      |
| 节点管理   | 不变                                      |

ZTNA 新增的 K8SAPI/K8SService/Endpoint 授权完全不涉及 Headscale，走 Agent 心跳下发。Headscale 只负责第 1 层（网络可达性）和第 2 层（SSH）的控制。
