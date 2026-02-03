# Agent 授权设计

> 版本：v0.2.1

## 1. 概述

支持直接对 Agent 授权，授权后自动获得该 Agent 下所有服务的访问权限，简化权限管理。

## 2. 菜单结构

```
服务授权（针对单个服务）
├── 桌面（授权 Desktop/Client 访问服务）
└── 代理（授权 Agent 访问服务）

代理授权（针对整个 Agent，新增）
├── 桌面（授权 Desktop/Client 访问 Agent 所有服务）
└── 代理（授权 Agent 访问 Agent 所有服务）
```

## 3. 当前 vs 新增权限模型

```
当前: 服务级别授权
┌─────────────────────────────────────────────────────────────────────────────┐
│                                                                             │
│  Client/Desktop ──授权──▶ Service A (Agent 1)                               │
│                  ──授权──▶ Service B (Agent 1)                               │
│                  ──授权──▶ Service C (Agent 2)                               │
│                                                                             │
│  问题: Agent 有 100 个服务时，需要授权 100 次                                │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘

新增: Agent 级别授权
┌─────────────────────────────────────────────────────────────────────────────┐
│                                                                             │
│  Client/Desktop ──授权──▶ Agent 1 ──自动包含──▶ Service A                   │
│                                               ──▶ Service B                   │
│                                               ──▶ Service ...                 │
│                                                                             │
│  优点: 一次授权，自动获得 Agent 下所有服务权限                               │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

## 4. 新增数据模型

AgentClientPermission（Agent-Client 授权）：

| 字段      | 类型   | 说明          |
| --------- | ------ | ------------- |
| ID        | uint64 | 主键          |
| AgentID   | uint64 | Agent ID      |
| ClientID  | uint64 | Client ID     |
| GrantedAt | time   | 授权时间      |
| GrantedBy | uint64 | 授权管理员 ID |

AgentAgentPermission（Agent-Agent 授权）：

| 字段          | 类型   | 说明          |
| ------------- | ------ | ------------- |
| ID            | uint64 | 主键          |
| SourceAgentID | uint64 | 源 Agent ID   |
| TargetAgentID | uint64 | 目标 Agent ID |
| GrantedAt     | time   | 授权时间      |
| GrantedBy     | uint64 | 授权管理员 ID |

AgentClientGroupPermission（Agent-ClientGroup 授权）：

| 字段      | 类型   | 说明           |
| --------- | ------ | -------------- |
| ID        | uint64 | 主键           |
| AgentID   | uint64 | Agent ID       |
| GroupID   | uint64 | Client 分组 ID |
| GrantedAt | time   | 授权时间       |

AgentAgentGroupPermission（Agent-AgentGroup 授权）：

| 字段          | 类型   | 说明          |
| ------------- | ------ | ------------- |
| ID            | uint64 | 主键          |
| TargetAgentID | uint64 | 目标 Agent ID |
| GroupID       | uint64 | Agent 分组 ID |
| GrantedAt     | time   | 授权时间      |

## 5. API 设计

### 5.1 Agent-Client 授权（代理授权 - 桌面）

| 方法   | 路径                                             | 说明                 |
| ------ | ------------------------------------------------ | -------------------- |
| GET    | /api/v1/admin/agents/:id/client-permissions      | 获取 Client 授权列表 |
| POST   | /api/v1/admin/agents/:id/client-permissions      | 添加 Client 授权     |
| DELETE | /api/v1/admin/agents/:id/client-permissions/:pid | 删除授权             |

POST 请求体：client_id (uint64)

### 5.2 Agent-ClientGroup 授权（代理授权 - 桌面分组）

| 方法   | 路径                                                   | 说明                      |
| ------ | ------------------------------------------------------ | ------------------------- |
| GET    | /api/v1/admin/agents/:id/client-group-permissions      | 获取 ClientGroup 授权列表 |
| POST   | /api/v1/admin/agents/:id/client-group-permissions      | 添加 ClientGroup 授权     |
| DELETE | /api/v1/admin/agents/:id/client-group-permissions/:pid | 删除授权                  |

POST 请求体：group_id (uint64)

### 5.3 Agent-Agent 授权（代理授权 - 代理）

| 方法   | 路径                                            | 说明                |
| ------ | ----------------------------------------------- | ------------------- |
| GET    | /api/v1/admin/agents/:id/agent-permissions      | 获取 Agent 授权列表 |
| POST   | /api/v1/admin/agents/:id/agent-permissions      | 添加 Agent 授权     |
| DELETE | /api/v1/admin/agents/:id/agent-permissions/:pid | 删除授权            |

POST 请求体：source_agent_id (uint64)

### 5.4 Agent-AgentGroup 授权（代理授权 - 代理分组）

| 方法   | 路径                                                  | 说明                     |
| ------ | ----------------------------------------------------- | ------------------------ |
| GET    | /api/v1/admin/agents/:id/agent-group-permissions      | 获取 AgentGroup 授权列表 |
| POST   | /api/v1/admin/agents/:id/agent-group-permissions      | 添加 AgentGroup 授权     |
| DELETE | /api/v1/admin/agents/:id/agent-group-permissions/:pid | 删除授权                 |

POST 请求体：group_id (uint64)

### 5.5 Agent 授权统计

| 方法 | 路径                            | 说明                          |
| ---- | ------------------------------- | ----------------------------- |
| GET  | /api/v1/admin/agents/auth-stats | 获取 Agent 列表（带授权统计） |

查询参数：

- page: 页码
- size: 每页数量
- type: client（桌面授权统计）或 agent（代理授权统计）

返回字段（在 Agent 基础上增加）：

- client_count: Client 授权数量
- client_group_count: ClientGroup 授权数量
- agent_count: Agent 授权数量
- agent_group_count: AgentGroup 授权数量
- service_count: 服务数量

## 6. ACL 生成逻辑

Agent 级别授权生成的 ACL 规则使用 `:*` 后缀，表示允许访问所有端口：

| 授权类型          | ACL Src                        | ACL Dst                    |
| ----------------- | ------------------------------ | -------------------------- |
| Agent-Client      | tag:desktop-{client.name}      | tag:agent-{agent.name}:\*  |
| Agent-Agent       | tag:agent-{source.name}        | tag:agent-{target.name}:\* |
| Agent-ClientGroup | tag:desktop-group-{group.name} | tag:agent-{agent.name}:\*  |
| Agent-AgentGroup  | tag:agent-group-{group.name}   | tag:agent-{agent.name}:\*  |

## 7. 权限检查优先级

权限检查顺序（任一满足即可访问）：

1. Agent 级别授权（新增）

   - AgentClientPermission
   - AgentAgentPermission
   - AgentClientGroupPermission
   - AgentAgentGroupPermission

2. 服务级别授权（原有）
   - ServiceClientPermission
   - ServiceAgentPermission
   - ServiceClientGroupPermission
   - ServiceAgentGroupPermission

## 8. Web 界面设计

### 8.1 代理授权 - 桌面页面

```
┌─────────────────────────────────────────────────────────────────────────────┐
│  代理授权 - 桌面                                                            │
├─────────────────────────────────────────────────────────────────────────────┤
│  [搜索代理名称...]                                                          │
├─────────────────────────────────────────────────────────────────────────────┤
│  序号  代理名称      代理IP         服务数  分组  用户  操作                │
│  1     agent-bj     100.64.0.1     5       2     3     [+分组] [+用户]     │
│  2     agent-sh     100.64.0.2     3       0     1     [+分组] [+用户]     │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 8.2 代理授权 - 代理页面

```
┌─────────────────────────────────────────────────────────────────────────────┐
│  代理授权 - 代理                                                            │
├─────────────────────────────────────────────────────────────────────────────┤
│  [搜索代理名称...]                                                          │
├─────────────────────────────────────────────────────────────────────────────┤
│  序号  代理名称      代理IP         服务数  分组  代理  操作                │
│  1     agent-bj     100.64.0.1     5       1     2     [+分组] [+代理]     │
│  2     agent-sh     100.64.0.2     3       0     0     [+分组] [+代理]     │
└─────────────────────────────────────────────────────────────────────────────┘
```

## 9. 实现步骤

1. 新增数据模型（4 个授权表）✅
2. 数据库迁移 ✅
3. 新增 API（13 个接口）✅
4. 更新 ACL 同步逻辑（生成 Agent 级别规则）✅
5. Web 前端页面（代理授权 - 桌面/代理）✅
