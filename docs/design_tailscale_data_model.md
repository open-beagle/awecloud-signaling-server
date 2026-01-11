# Tailscale 数据模型设计

## 1. 概述

### 1.1 设计目标

本文档定义 Server 端与 Headscale 端的数据结构映射关系，解决以下问题：

- Server 数据库 ID（自增键）与 Headscale ID（uint64）的映射
- Agent 多分组支持（当前仅支持单分组）
- Headscale User、Tag、ACL 的管理策略
- 数据同步机制

### 1.2 现状分析

| 对象    | 现有分组方式                    | 状态                    |
| ------- | ------------------------------- | ----------------------- |
| Desktop | Group + GroupMember 表，多分组  | ✅ 已支持多分组         |
| Agent   | agent.group_name 字符串，单分组 | ❌ 仅支持单分组，需改造 |

### 1.3 核心问题

| 问题              | 现状                            | 目标                           |
| ----------------- | ------------------------------- | ------------------------------ |
| ID 类型不匹配     | Server 用自增 ID 传给 Headscale | 使用独立的 ts\_\* 字段存储     |
| Agent 单分组      | agent.group_name 单值           | 改为多分组（AgentGroupMember） |
| Headscale ID 存储 | 未存储 User ID / Node ID        | 新增 ts_user_id, ts_node_id    |

---

## 2. Headscale 数据结构

### 2.1 Headscale 核心对象

| 对象       | ID 类型 | 说明                     |
| ---------- | ------- | ------------------------ |
| User       | uint64  | 用户，节点的命名空间     |
| Node       | uint64  | 节点（机器），有唯一 IP  |
| PreAuthKey | string  | 预认证密钥，用于节点注册 |
| Tag        | string  | 标签，用于 ACL 分组      |
| ACL        | JSON    | 访问控制列表             |

### 2.2 Headscale User 设计

```txt
┌─────────────────────────────────────────────────────────────────────────────┐
│                          Headscale User 模型                                 │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│   User 命名规则:                                                             │
│                                                                             │
│   Agent:   agent-{agent_name}                                               │
│            例: agent-k8s-prod, agent-nas-office                             │
│                                                                             │
│   Desktop: desktop-{client_id}                                              │
│            例: desktop-zhangsan@company.com                                 │
│                                                                             │
│   ⚠️ 重要: Headscale User.ID 是 uint64，不是 User.Name                       │
│   Server 必须存储 User.ID 用于后续 API 调用                                  │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 2.3 Headscale Tag 设计

Tag 用于 ACL 分组，格式为 `tag:<name>`：

| Tag 类型     | 格式                    | 说明              |
| ------------ | ----------------------- | ----------------- |
| 节点类型     | `tag:agent`             | 标识 Agent 节点   |
| 节点类型     | `tag:desktop`           | 标识 Desktop 节点 |
| Agent 分组   | `tag:agent-group-{id}`  | Agent 所属分组    |
| Desktop 分组 | `tag:client-group-{id}` | Desktop 所属分组  |

### 2.4 Headscale ACL 结构

```txt
┌─────────────────────────────────────────────────────────────────────────────┐
│                          ACL Policy 结构                                     │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│   {                                                                         │
│     "groups": {                                                             │
│       "group:agents": ["tag:agent"],                                        │
│       "group:desktops": ["tag:desktop"],                                    │
│       "group:agent-internal": ["tag:agent-group-1"]                         │
│     },                                                                      │
│     "tagOwners": {                                                          │
│       "tag:agent": ["autogroup:admin"],                                     │
│       "tag:desktop": ["autogroup:admin"]                                    │
│     },                                                                      │
│     "acls": [                                                               │
│       {                                                                     │
│         "action": "accept",                                                 │
│         "src": ["100.64.6.4"],                                              │
│         "dst": ["100.64.0.1:3306"]                                          │
│       }                                                                     │
│     ]                                                                       │
│   }                                                                         │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 3. Server 现有数据模型

### 3.1 Agent 表（现有）

| 字段         | 类型   | 说明                       |
| ------------ | ------ | -------------------------- |
| id           | int64  | 主键，自增                 |
| agent_name   | string | Agent 名称，唯一           |
| agent_token  | string | 认证 Token                 |
| group_name   | string | 分组名称（单分组，需改造） |
| tailscale_ip | string | Tailscale IP               |
| ts_connected | bool   | 连接状态                   |
| ts_conn_type | string | 连接方式 p2p/derp          |
| ts_node_key  | string | 节点密钥                   |

**问题**: 缺少 ts_user_id, ts_node_id；group_name 仅支持单分组

### 3.2 Client 表（现有）

| 字段          | 类型   | 说明                 |
| ------------- | ------ | -------------------- |
| id            | int64  | 主键，自增           |
| client_id     | string | 客户端标识（如邮箱） |
| client_secret | string | 密钥                 |
| tailscale_ip  | string | Tailscale IP         |

**问题**: 缺少 ts_user_id, ts_node_id

### 3.3 Group 表（现有，用于 Desktop 分组）

| 字段        | 类型   | 说明           |
| ----------- | ------ | -------------- |
| id          | int64  | 主键，自增     |
| name        | string | 分组名称，唯一 |
| description | string | 描述           |

### 3.4 GroupMember 表（现有，Desktop 多分组）

| 字段      | 类型   | 说明              |
| --------- | ------ | ----------------- |
| id        | int64  | 主键，自增        |
| group_id  | int64  | Group ID          |
| client_id | int64  | Client ID         |
| role      | string | 角色 admin/member |

唯一约束: (group_id, client_id)

---

## 4. 改造方案

### 4.1 Agent 表扩展

新增字段：

| 字段         | 类型   | 说明                       |
| ------------ | ------ | -------------------------- |
| ts_user_id   | string | Headscale User ID (uint64) |
| ts_user_name | string | Headscale User Name        |
| ts_node_id   | string | Headscale Node ID (uint64) |
| ts_tags      | string | Tags JSON 数组             |

移除字段：

| 字段       | 说明                               |
| ---------- | ---------------------------------- |
| group_name | 改用 AgentGroup + AgentGroupMember |

### 4.2 新增 AgentGroup 表

| 字段        | 类型     | 说明               |
| ----------- | -------- | ------------------ |
| id          | int64    | 主键，自增         |
| name        | string   | 分组名称，唯一     |
| description | string   | 描述               |
| ts_tag      | string   | 对应 Headscale Tag |
| created_at  | datetime | 创建时间           |

### 4.3 新增 AgentGroupMember 表

| 字段       | 类型     | 说明          |
| ---------- | -------- | ------------- |
| id         | int64    | 主键，自增    |
| agent_id   | int64    | Agent ID      |
| group_id   | int64    | AgentGroup ID |
| created_at | datetime | 创建时间      |

唯一约束: (agent_id, group_id)

### 4.4 Client 表扩展

新增字段：

| 字段         | 类型   | 说明                       |
| ------------ | ------ | -------------------------- |
| ts_user_id   | string | Headscale User ID (uint64) |
| ts_user_name | string | Headscale User Name        |
| ts_node_id   | string | Headscale Node ID (uint64) |
| ts_tags      | string | Tags JSON 数组             |

### 4.5 Group 表扩展（Desktop 分组）

新增字段：

| 字段   | 类型   | 说明               |
| ------ | ------ | ------------------ |
| ts_tag | string | 对应 Headscale Tag |

### 4.6 最终字段汇总

#### Agent 表（改造后）

| 字段             | 类型     | 必填 | 说明                           |
| ---------------- | -------- | ---- | ------------------------------ |
| id               | int64    | 是   | 主键，自增（仅 Server 内部用） |
| agent_name       | string   | 是   | Agent 名称，唯一               |
| agent_token      | string   | 是   | 认证 Token                     |
| description      | string   | 否   | 描述                           |
| status           | string   | 是   | 状态 online/offline            |
| version          | string   | 否   | Agent 版本                     |
| last_heartbeat   | datetime | 否   | 最后心跳时间                   |
| ts_user_id       | string   | 否   | Headscale User ID (uint64)     |
| ts_user_name     | string   | 否   | Headscale User Name            |
| ts_node_id       | string   | 否   | Headscale Node ID (uint64)     |
| ts_ip            | string   | 否   | Tailscale IP                   |
| ts_tags          | string   | 否   | Tags JSON 数组                 |
| ts_connected     | bool     | 否   | Tailscale 连接状态             |
| ts_conn_type     | string   | 否   | 连接方式 p2p/derp              |
| ts_registered_at | datetime | 否   | Tailscale 注册时间             |
| lan_ip           | string   | 否   | 局域网 IP                      |
| lan_gateway      | string   | 否   | 网关地址                       |
| lan_interface    | string   | 否   | 网卡名称                       |
| runtime_env      | string   | 否   | 运行环境                       |
| hostname         | string   | 否   | 主机名                         |
| created_at       | datetime | 是   | 创建时间                       |
| updated_at       | datetime | 是   | 更新时间                       |

**移除字段**: group_name（改用 AgentGroupMember 多分组）

#### Client 表（改造后）

| 字段          | 类型     | 必填 | 说明                           |
| ------------- | -------- | ---- | ------------------------------ |
| id            | int64    | 是   | 主键，自增（仅 Server 内部用） |
| client_id     | string   | 是   | 客户端标识（如邮箱），唯一     |
| client_secret | string   | 是   | 密钥                           |
| tunnel_token  | string   | 否   | 隧道 Token                     |
| enabled       | bool     | 是   | 是否启用                       |
| ts_user_id    | string   | 否   | Headscale User ID (uint64)     |
| ts_user_name  | string   | 否   | Headscale User Name            |
| ts_node_id    | string   | 否   | Headscale Node ID (uint64)     |
| ts_ip         | string   | 否   | Tailscale IP                   |
| ts_tags       | string   | 否   | Tags JSON 数组                 |
| created_at    | datetime | 是   | 创建时间                       |
| updated_at    | datetime | 是   | 更新时间                       |

#### AgentGroup 表（新增）

| 字段        | 类型     | 必填 | 说明               |
| ----------- | -------- | ---- | ------------------ |
| id          | int64    | 是   | 主键，自增         |
| name        | string   | 是   | 分组名称，唯一     |
| description | string   | 否   | 描述               |
| ts_tag      | string   | 否   | 对应 Headscale Tag |
| created_at  | datetime | 是   | 创建时间           |
| updated_at  | datetime | 是   | 更新时间           |

#### AgentGroupMember 表（新增）

| 字段       | 类型     | 必填 | 说明          |
| ---------- | -------- | ---- | ------------- |
| id         | int64    | 是   | 主键，自增    |
| agent_id   | int64    | 是   | Agent ID      |
| group_id   | int64    | 是   | AgentGroup ID |
| created_at | datetime | 是   | 创建时间      |

唯一约束: (agent_id, group_id)

#### Group 表（改造后，Desktop 分组）

| 字段        | 类型     | 必填 | 说明               |
| ----------- | -------- | ---- | ------------------ |
| id          | int64    | 是   | 主键，自增         |
| name        | string   | 是   | 分组名称，唯一     |
| description | string   | 否   | 描述               |
| ts_tag      | string   | 否   | 对应 Headscale Tag |
| created_at  | datetime | 是   | 创建时间           |
| updated_at  | datetime | 是   | 更新时间           |

#### GroupMember 表（现有，无需改动）

| 字段       | 类型     | 必填 | 说明              |
| ---------- | -------- | ---- | ----------------- |
| id         | int64    | 是   | 主键，自增        |
| group_id   | int64    | 是   | Group ID          |
| client_id  | int64    | 是   | Client ID         |
| role       | string   | 否   | 角色 admin/member |
| created_at | datetime | 是   | 创建时间          |

唯一约束: (group_id, client_id)

---

## 5. 数据关系图

```txt
┌─────────────────────────────────────────────────────────────────────────────┐
│                          改造后数据关系                                      │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│   ┌─────────────────┐       ┌──────────────────┐       ┌─────────────┐     │
│   │     Agent       │◄─────►│AgentGroupMember  │◄─────►│ AgentGroup  │     │
│   │                 │  N:M  │                  │  N:M  │             │     │
│   │ id              │       │ agent_id         │       │ id          │     │
│   │ agent_name      │       │ group_id         │       │ name        │     │
│   │ ts_user_id  ◄───┼───────┼──────────────────┼───────┼─► ts_tag    │     │
│   │ ts_node_id      │       └──────────────────┘       └─────────────┘     │
│   │ ts_tags         │                                                      │
│   │ tailscale_ip    │                                                      │
│   └─────────────────┘                                                      │
│                                                                             │
│   ┌─────────────────┐       ┌──────────────────┐       ┌─────────────┐     │
│   │     Client      │◄─────►│  GroupMember     │◄─────►│   Group     │     │
│   │                 │  N:M  │                  │  N:M  │             │     │
│   │ id              │       │ client_id        │       │ id          │     │
│   │ client_id       │       │ group_id         │       │ name        │     │
│   │ ts_user_id  ◄───┼───────┼──────────────────┼───────┼─► ts_tag    │     │
│   │ ts_node_id      │       └──────────────────┘       └─────────────┘     │
│   │ ts_tags         │                                                      │
│   │ tailscale_ip    │                                                      │
│   └─────────────────┘                                                      │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 6. Server 与 Headscale 数据映射

### 6.1 ID 映射关系

```txt
┌─────────────────────────────────────────────────────────────────────────────┐
│                     Server ID 与 Headscale ID 映射                           │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│   Server 端                              Headscale 端                        │
│   ──────────                             ────────────                        │
│                                                                             │
│   Agent                                  User                               │
│   ├── id (自增，内部用)                  ├── id (uint64)  ◄── ts_user_id    │
│   ├── agent_name ─────────────────────► └── name         ◄── ts_user_name  │
│   │                                                                         │
│   │                                      Node                               │
│   ├── ts_node_id ──────────────────────► id (uint64)                        │
│   └── tailscale_ip ◄─────────────────── ipAddresses[0]                      │
│                                                                             │
│   Client                                 User                               │
│   ├── id (自增，内部用)                  ├── id (uint64)  ◄── ts_user_id    │
│   ├── client_id ──────────────────────► └── name         ◄── ts_user_name  │
│   │                                                                         │
│   │                                      Node                               │
│   ├── ts_node_id ──────────────────────► id (uint64)                        │
│   └── tailscale_ip ◄─────────────────── ipAddresses[0]                      │
│                                                                             │
│   ⚠️ 关键: Server.id 不传给 Headscale，使用 ts_user_id/ts_node_id           │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 6.2 分组与 Tag 映射

```txt
┌─────────────────────────────────────────────────────────────────────────────┐
│                     分组与 Headscale Tag 映射                                │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│   Server 端                              Headscale 端                        │
│   ──────────                             ────────────                        │
│                                                                             │
│   AgentGroup                             Tag                                │
│   ├── id: 1                              tag:agent-group-1                  │
│   ├── name: "内网服务器"                                                     │
│   └── ts_tag ──────────────────────────► tag:agent-group-1                  │
│                                                                             │
│   Group (Desktop)                        Tag                                │
│   ├── id: 1                              tag:client-group-1                 │
│   ├── name: "开发组"                                                         │
│   └── ts_tag ──────────────────────────► tag:client-group-1                 │
│                                                                             │
│   节点 Tags 更新:                                                            │
│   当 Agent 加入 AgentGroup 时:                                               │
│   1. 查询 Agent 所属的所有 AgentGroup                                        │
│   2. 收集所有 ts_tag                                                         │
│   3. 更新 Agent.ts_tags = ["tag:agent", "tag:agent-group-1", ...]           │
│   4. 调用 Headscale API 更新节点 Tags                                        │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 7. 数据同步流程

### 7.1 Agent 注册流程

```txt
┌─────────────────────────────────────────────────────────────────────────────┐
│                          Agent 注册数据同步                                  │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│   Agent 首次连接                                                             │
│        │                                                                    │
│        ▼                                                                    │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │ 1. Server 创建/获取 Headscale User                                   │   │
│   │    POST /api/v1/user  { name: "agent-{agent_name}" }                │   │
│   │    返回: { user: { id: "123", name: "agent-xxx" } }                  │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
│        │                                                                    │
│        ▼                                                                    │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │ 2. Server 存储 Headscale User ID                                     │   │
│   │    UPDATE agents SET ts_user_id="123", ts_user_name="agent-xxx"     │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
│        │                                                                    │
│        ▼                                                                    │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │ 3. Server 创建 PreAuthKey（使用 User ID，不是 Name）                 │   │
│   │    POST /api/v1/preauthkey  { user: "123", ... }                    │   │
│   │    ⚠️ user 字段必须是 uint64 ID，不是字符串 Name                     │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
│        │                                                                    │
│        ▼                                                                    │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │ 4. Agent 使用 PreAuthKey 连接 Headscale                              │   │
│   │    Headscale 分配 IP，创建 Node                                      │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
│        │                                                                    │
│        ▼                                                                    │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │ 5. Agent 心跳上报 Tailscale IP                                       │   │
│   │    Server 存储: tailscale_ip, ts_connected                          │   │
│   │    Server 查询 Headscale 获取 Node ID 并存储: ts_node_id             │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 7.2 分组变更同步

```txt
┌─────────────────────────────────────────────────────────────────────────────┐
│                          分组变更数据同步                                    │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│   管理员将 Agent 加入分组                                                    │
│        │                                                                    │
│        ▼                                                                    │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │ 1. 创建 AgentGroupMember 记录                                        │   │
│   │    INSERT INTO agent_group_members (agent_id, group_id)             │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
│        │                                                                    │
│        ▼                                                                    │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │ 2. 查询 Agent 所属的所有分组                                         │   │
│   │    SELECT g.ts_tag FROM agent_groups g                              │   │
│   │    JOIN agent_group_members m ON g.id = m.group_id                  │   │
│   │    WHERE m.agent_id = ?                                             │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
│        │                                                                    │
│        ▼                                                                    │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │ 3. 更新 Agent.ts_tags                                                │   │
│   │    ts_tags = ["tag:agent", "tag:agent-group-1", "tag:agent-group-2"]│   │
│   └─────────────────────────────────────────────────────────────────────┘   │
│        │                                                                    │
│        ▼                                                                    │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │ 4. 同步到 Headscale（可选，取决于 Headscale 版本是否支持）           │   │
│   │    PUT /api/v1/node/{node_id}/tags                                  │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
│        │                                                                    │
│        ▼                                                                    │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │ 5. 重新生成 ACL 规则                                                 │   │
│   │    基于分组关系生成 ACL，同步到 Headscale                            │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 8. ACL 生成策略

### 8.1 ACL 规则来源

| 规则类型           | 来源                       | 说明                  |
| ------------------ | -------------------------- | --------------------- |
| 服务基础权限       | ProxyService.access_type   | public/private/group  |
| 额外授权（客户端） | ServicePermission (client) | 单个 Desktop 访问服务 |
| 额外授权（分组）   | ServicePermission (group)  | 整个分组访问服务      |
| Agent 组内互访     | AgentGroupMember           | 同组 Agent 互访       |

### 8.2 ACL 生成流程

```txt
┌─────────────────────────────────────────────────────────────────────────────┐
│                          ACL 生成流程                                        │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│   触发条件:                                                                  │
│   - 服务权限变更                                                             │
│   - 分组成员变更                                                             │
│   - Agent/Desktop 上线                                                       │
│        │                                                                    │
│        ▼                                                                    │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │ 1. 收集所有 Agent IP                                                 │   │
│   │    SELECT id, tailscale_ip FROM agents WHERE tailscale_ip != ''     │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
│        │                                                                    │
│        ▼                                                                    │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │ 2. 收集所有 Desktop IP                                               │   │
│   │    SELECT id, tailscale_ip FROM clients WHERE tailscale_ip != ''    │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
│        │                                                                    │
│        ▼                                                                    │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │ 3. 生成服务访问规则                                                  │   │
│   │                                                                     │   │
│   │    public 服务:                                                      │   │
│   │    src: [所有 Desktop IP], dst: [Agent:Port]                        │   │
│   │                                                                     │   │
│   │    private 服务:                                                     │   │
│   │    src: [创建者 IP], dst: [Agent:Port]                              │   │
│   │                                                                     │   │
│   │    group 服务:                                                       │   │
│   │    src: [分组成员 IP], dst: [Agent:Port]                            │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
│        │                                                                    │
│        ▼                                                                    │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │ 4. 生成额外授权规则                                                  │   │
│   │                                                                     │   │
│   │    按客户端授权:                                                     │   │
│   │    src: [Client IP], dst: [Agent:Port]                              │   │
│   │                                                                     │   │
│   │    按分组授权:                                                       │   │
│   │    src: [分组所有成员 IP], dst: [Agent:Port]                        │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
│        │                                                                    │
│        ▼                                                                    │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │ 5. 生成 Agent 组内互访规则                                           │   │
│   │                                                                     │   │
│   │    同组 Agent 互访:                                                  │   │
│   │    src: [Agent-A IP], dst: [Agent-B:*]                              │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
│        │                                                                    │
│        ▼                                                                    │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │ 6. 合并规则，同步到 Headscale                                        │   │
│   │    PUT /api/v1/policy                                               │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 9. 实施计划

### 9.1 Phase 1: 数据模型改造

- 新增 AgentGroup 表
- 新增 AgentGroupMember 表
- Agent 表新增 ts_user_id, ts_node_id, ts_tags 字段
- Client 表新增 ts_user_id, ts_node_id, ts_tags 字段
- Group 表新增 ts_tag 字段
- 数据迁移：将 agent.group_name 迁移到 AgentGroupMember

### 9.2 Phase 2: API 改造

- Agent 注册时存储 Headscale User ID
- PreAuthKey 创建使用 User ID 而非 Name
- 新增 Agent 分组管理 API
- 分组变更时同步 Tags

### 9.3 Phase 3: ACL 同步改造

- 基于新数据模型重构 ACL 生成逻辑
- 支持 Agent 多分组的 ACL 规则

---

**文档版本**: 1.0  
**创建日期**: 2026-01-12  
**维护者**: 开发团队
