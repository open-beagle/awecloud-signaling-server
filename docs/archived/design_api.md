# API 设计文档

## 1. 概述

本文档描述 AWECloud Signaling Server 的 REST API 设计，包括接口定义、返回模型和业务流程。

### 1.1 API 职责

- 提供 Web 管理界面的数据接口
- 支持 Agent 管理、Client 管理、服务权限管理
- 提供审计日志查询和系统配置管理

### 1.2 与其他模块的关系

- 依赖数据模型（`internal/server/model`）
- 为 Web 界面提供数据（`web/src/api`）
- 与 gRPC 服务协同工作

## 2. 架构设计

### 2.1 通信架构

```txt
┌─────────────────────────────────────────────────────────────┐
│                     Server 通信架构                          │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│   ┌─────────────┐                    ┌─────────────────┐   │
│   │ Web 管理界面 │                    │ Agent / Desktop │   │
│   └─────┬───────┘                    └────────┬────────┘   │
│         │ HTTP/REST                           │ gRPC       │
│         ▼                                     ▼            │
│   ┌─────────────────────────────────────────────────────┐   │
│   │                   Server 进程                        │   │
│   │  ┌─────────────────────┐  ┌─────────────────────┐   │   │
│   │  │   HTTP API (Gin)    │  │   gRPC 服务          │   │   │
│   │  │   端口 8080         │  │   端口 8080 (HTTP/2) │   │   │
│   │  └──────────┬──────────┘  └──────────┬──────────┘   │   │
│   │             └──────────┬─────────────┘              │   │
│   │                        ▼                            │   │
│   │              ┌─────────────────────┐                │   │
│   │              │     业务逻辑层       │                │   │
│   │              └──────────┬──────────┘                │   │
│   │                         ▼                           │   │
│   │              ┌─────────────────────┐                │   │
│   │              │   数据库 (SQLite)   │                │   │
│   │              └─────────────────────┘                │   │
│   └─────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
```

说明：

- HTTP REST API 仅供 Web 管理界面使用
- Agent 和 Desktop 通过 gRPC 与 Server 通信

### 2.2 核心组件

| 组件       | 职责                  | 实现位置                |
| ---------- | --------------------- | ----------------------- |
| 路由层     | HTTP 请求路由和中间件 | `internal/server/api`   |
| 业务逻辑层 | API 处理器和业务逻辑  | `internal/server/api`   |
| 数据访问层 | 数据库操作和模型映射  | `internal/server/model` |
| 认证中间件 | JWT 认证和权限验证    | `internal/server/api`   |

### 2.3 统一响应格式

| 字段    | 类型   | 说明         |
| ------- | ------ | ------------ |
| success | bool   | 操作是否成功 |
| message | string | 响应消息     |
| data    | object | 响应数据     |
| error   | string | 错误信息     |

## 3. 公开 API（无需认证）

### 3.1 健康检查

| 项目 | 内容        |
| ---- | ----------- |
| 路径 | GET /health |
| 认证 | 无需认证    |

响应字段：

| 字段      | 类型   | 说明                      |
| --------- | ------ | ------------------------- |
| status    | string | 状态：healthy / unhealthy |
| timestamp | string | 时间戳                    |

### 3.2 就绪检查

| 项目 | 内容              |
| ---- | ----------------- |
| 路径 | GET /health/ready |
| 认证 | 无需认证          |

响应字段：

| 字段      | 类型   | 说明               |
| --------- | ------ | ------------------ |
| ready     | bool   | 是否就绪           |
| database  | string | 数据库状态         |
| headscale | string | Headscale 连接状态 |

## 4. 认证 API

### 4.1 业务流程

```txt
┌─────────────────────────────────────────────────────────────┐
│                    管理员登录流程                            │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│   管理员输入用户名密码                                      │
│       │                                                     │
│       ▼                                                     │
│   ┌─────────────┐                                          │
│   │ 查询 admin  │                                          │
│   │ 表验证用户  │                                          │
│   └─────────────┘                                          │
│       │                                                     │
│       ▼                                                     │
│   ┌─────────────┐    失败    ┌─────────────┐              │
│   │ bcrypt 验证 │──────────►│ 返回 401    │              │
│   │ 密码哈希    │            │ 认证失败    │              │
│   └─────────────┘            └─────────────┘              │
│       │ 成功                                               │
│       ▼                                                     │
│   ┌─────────────┐                                          │
│   │ 生成 JWT    │                                          │
│   │ Token       │                                          │
│   └─────────────┘                                          │
│       │                                                     │
│       ▼                                                     │
│   ┌─────────────┐                                          │
│   │ 返回 Token  │                                          │
│   │ 和用户信息  │                                          │
│   └─────────────┘                                          │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

### 4.2 管理员登录

| 项目 | 内容                          |
| ---- | ----------------------------- |
| 路径 | POST /api/v1/admin/auth/login |
| 认证 | 无需认证                      |

请求字段：

| 字段     | 类型   | 必填 | 说明   |
| -------- | ------ | ---- | ------ |
| username | string | 是   | 用户名 |
| password | string | 是   | 密码   |

响应字段：

| 字段       | 类型   | 说明       |
| ---------- | ------ | ---------- |
| token      | string | JWT Token  |
| expires_at | string | 过期时间   |
| admin      | object | 管理员信息 |

### 4.3 管理员登出

| 项目 | 内容                           |
| ---- | ------------------------------ |
| 路径 | POST /api/v1/admin/auth/logout |
| 认证 | 需要管理员 JWT                 |

### 4.4 获取当前管理员信息

| 项目 | 内容                      |
| ---- | ------------------------- |
| 路径 | GET /api/v1/admin/auth/me |
| 认证 | 需要管理员 JWT            |

响应字段：

| 字段       | 类型   | 说明      |
| ---------- | ------ | --------- |
| id         | int64  | 管理员 ID |
| username   | string | 用户名    |
| role       | string | 角色      |
| created_at | string | 创建时间  |

### 4.5 修改密码

| 项目 | 内容                            |
| ---- | ------------------------------- |
| 路径 | PUT /api/v1/admin/auth/password |
| 认证 | 需要管理员 JWT                  |

请求字段：

| 字段         | 类型   | 必填 | 说明   |
| ------------ | ------ | ---- | ------ |
| old_password | string | 是   | 旧密码 |
| new_password | string | 是   | 新密码 |

## 5. Agent 管理 API

### 5.1 获取 Agent 列表

> 需求来源：`design_tailscale_server_web.md` 2.1 列表页

| 项目 | 内容                     |
| ---- | ------------------------ |
| 路径 | GET /api/v1/admin/agents |
| 认证 | 需要管理员 JWT           |

查询参数：

| 参数   | 类型   | 必填 | 说明              |
| ------ | ------ | ---- | ----------------- |
| page   | int    | 否   | 页码，默认 1      |
| size   | int    | 否   | 每页数量，默认 20 |
| search | string | 否   | 搜索关键词        |

响应模型 AgentListItem：

| 字段          | 类型   | 说明         | 数据来源                           |
| ------------- | ------ | ------------ | ---------------------------------- |
| id            | uint64 | Agent ID     | agent.id                           |
| name          | string | Agent 名称   | agent.name                         |
| alias         | string | Agent 别名   | agent.alias                        |
| ip            | string | 隧道 IP      | agent.ip                           |
| service_count | int    | 服务数量     | COUNT(proxy_service.agent_id)      |
| group_count   | int    | 分组数量     | COUNT(agent_group_member.agent_id) |
| status        | string | 状态         | 内存计算（Server.Agents 缓存）     |
| version       | string | 版本         | agent.version                      |
| last_online   | string | 最后在线时间 | agent.last_heartbeat               |

### 5.2 获取 Agent 详情

> 需求来源：`design_tailscale_server_web.md` 2.2 详情页

业务流程：

```txt
┌─────────────────────────────────────────────────────────────┐
│              Agent 详情页数据获取流程                        │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│   Web 请求 Agent 详情页                                     │
│       │                                                     │
│       ├────────────┬────────────┬────────────┐             │
│       ▼            ▼            ▼            ▼             │
│   ┌───────┐  ┌─────────┐  ┌─────────┐  ┌─────────┐        │
│   │ 5.2.1 │  │ 5.2.2   │  │ 5.2.3   │  │ 5.2.4   │        │
│   │ 静态  │  │ 动态    │  │ 端口映射│  │ 端口访问│        │
│   │ (DB)  │  │ (gRPC)  │  │ (DB)    │  │ (DB)    │        │
│   └───┬───┘  └────┬────┘  └────┬────┘  └────┬────┘        │
│       │           │            │            │              │
│       │           ▼            │            │              │
│       │     ┌───────────┐      │            │              │
│       │     │ Server    │      │            │              │
│       │     │   │gRPC   │      │            │              │
│       │     │   ▼       │      │            │              │
│       │     │ Agent     │      │            │              │
│       │     └─────┬─────┘      │            │              │
│       │           │            │            │              │
│       ▼           ▼            ▼            ▼              │
│   ┌─────────────────────────────────────────────┐         │
│   │              页面渲染完成                    │         │
│   └─────────────────────────────────────────────┘         │
│                        │                                   │
│                        ▼ 用户点击 [刷新] 按钮              │
│                  ┌───────────┐                             │
│                  │ 重新请求   │                             │
│                  │ 5.2.2     │                             │
│                  └───────────┘                             │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

#### 5.2.1 获取静态信息

> 需求来源：`design_tailscale_server_web.md` 2.2 详情页 - 基本信息模型

| 项目 | 内容                         |
| ---- | ---------------------------- |
| 路径 | GET /api/v1/admin/agents/:id |
| 认证 | 需要管理员 JWT               |

路径参数：

| 参数 | 类型   | 说明     |
| ---- | ------ | -------- |
| id   | uint64 | Agent ID |

响应模型 AgentDetail：

| 字段           | 类型   | 说明       | 数据来源                                       |
| -------------- | ------ | ---------- | ---------------------------------------------- |
| id             | uint64 | Agent ID   | agent.id                                       |
| name           | string | Agent 名称 | agent.name                                     |
| alias          | string | Agent 别名 | agent.alias                                    |
| version        | string | 版本       | agent.version                                  |
| created_at     | string | 创建时间   | agent.created_at                               |
| last_heartbeat | string | 最后心跳   | agent.last_heartbeat                           |
| status         | string | 状态       | 内存计算（Server.Agents 缓存，online/offline） |
| connected_at   | string | 连接时间   | 内存计算（Server.Agents 缓存，本次连接时间）   |

#### 5.2.2 获取动态信息

> 需求来源：`design_tailscale_server_web.md` 2.2 详情页 - 运行环境模型、网络信息模型、隧道信息模型

| 项目 | 内容                                   |
| ---- | -------------------------------------- |
| 路径 | GET /api/v1/admin/agents/:id/realtime  |
| 认证 | 需要管理员 JWT                         |
| 说明 | Server 通过 gRPC 向 Agent 请求实时状态 |

响应模型 AgentRealtimeInfo：

| 字段                  | 类型               | 说明         | 数据来源        |
| --------------------- | ------------------ | ------------ | --------------- |
| hostname              | string             | 主机名       | Agent gRPC 响应 |
| runtime               | string             | 运行环境     | Agent gRPC 响应 |
| networks              | NetworkInterface[] | 网络接口列表 | Agent gRPC 响应 |
| tunnel_ip             | string             | 隧道 IP      | Agent gRPC 响应 |
| tunnel_connected      | bool               | 隧道连接状态 | Agent gRPC 响应 |
| tunnel_connected_time | string             | 隧道连接时间 | Agent gRPC 响应 |

NetworkInterface 模型：

| 字段    | 类型   | 说明                        |
| ------- | ------ | --------------------------- |
| name    | string | 网卡名称（如 eth0, ens192） |
| ip      | string | IP 地址                     |
| mask    | string | 子网掩码                    |
| gateway | string | 网关地址                    |

#### 5.2.3 获取端口映射列表

> 需求来源：`design_tailscale_server_web.md` 2.2 详情页 - 端口映射表格模型

| 项目 | 内容                                  |
| ---- | ------------------------------------- |
| 路径 | GET /api/v1/admin/agents/:id/services |
| 认证 | 需要管理员 JWT                        |

响应模型 ProxyServiceItem[]：

| 字段        | 类型   | 说明     | 数据来源                  |
| ----------- | ------ | -------- | ------------------------- |
| id          | uint64 | 服务 ID  | proxy_service.id          |
| name        | string | 服务名称 | proxy_service.name        |
| alias       | string | 服务别名 | proxy_service.alias       |
| source_addr | string | 源地址   | proxy_service.source_addr |
| target_addr | string | 目标地址 | proxy_service.target_addr |
| enabled     | bool   | 是否启用 | proxy_service.enabled     |
| status      | string | 运行状态 | proxy_service.status      |
| error_msg   | string | 错误信息 | proxy_service.error_msg   |

#### 5.2.4 获取端口访问列表

> 需求来源：`design_tailscale_server_web.md` 2.2 详情页 - 端口访问表格模型

| 项目 | 内容                                  |
| ---- | ------------------------------------- |
| 路径 | GET /api/v1/admin/agents/:id/forwards |
| 认证 | 需要管理员 JWT                        |

响应模型 PortForwardItem[]：

| 字段              | 类型   | 说明       | 数据来源                       |
| ----------------- | ------ | ---------- | ------------------------------ |
| id                | uint64 | 转发 ID    | port_forward.id                |
| name              | string | 名称       | proxy_service.name (关联查询)  |
| alias             | string | 别名       | proxy_service.alias (关联查询) |
| target_agent_name | string | 目标 Agent | agent.name (关联查询)          |
| source_addr       | string | 源地址     | port_forward.source_addr       |
| target_addr       | string | 目标地址   | port_forward.target_addr       |
| enabled           | bool   | 是否启用   | port_forward.enabled           |
| status            | string | 运行状态   | port_forward.status            |
| error_msg         | string | 错误信息   | port_forward.error_msg         |

### 5.3 创建 Agent

> 需求来源：`design_tailscale_server_web.md` 2.1 列表页 - [+ 创建] 按钮

| 项目 | 内容                      |
| ---- | ------------------------- |
| 路径 | POST /api/v1/admin/agents |
| 认证 | 需要管理员 JWT            |

业务流程：

```txt
┌─────────────────────────────────────────────────────────────┐
│                    Agent 创建流程                            │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│   管理员创建 Agent                                          │
│       │                                                     │
│       ▼                                                     │
│   ┌─────────────┐                                          │
│   │ 生成 Agent  │                                          │
│   │ 名称和密钥  │                                          │
│   └─────────────┘                                          │
│       │                                                     │
│       ▼                                                     │
│   ┌─────────────┐    成功    ┌─────────────┐              │
│   │调用Headscale│──────────►│ 保存到数据库 │              │
│   │创建 User    │            └─────────────┘              │
│   └─────────────┘                    │                     │
│       │ 失败                         ▼                     │
│       ▼                        ┌─────────────┐             │
│   ┌─────────────┐              │ 返回 Agent  │             │
│   │ 返回错误     │              │ 信息和密钥  │             │
│   └─────────────┘              └─────────────┘             │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

请求字段：

| 字段  | 类型   | 必填 | 说明       |
| ----- | ------ | ---- | ---------- |
| name  | string | 是   | Agent 名称 |
| alias | string | 否   | Agent 别名 |

响应字段：

| 字段   | 类型   | 说明                     |
| ------ | ------ | ------------------------ |
| id     | uint64 | Agent ID                 |
| name   | string | Agent 名称               |
| secret | string | 密钥（仅创建时返回一次） |

### 5.4 更新 Agent

> 需求来源：`design_tailscale_server_web.md` 2.2 详情页 - 基本信息 [编辑] 按钮

| 项目 | 内容                         |
| ---- | ---------------------------- |
| 路径 | PUT /api/v1/admin/agents/:id |
| 认证 | 需要管理员 JWT               |

请求字段：

| 字段  | 类型   | 必填 | 说明       |
| ----- | ------ | ---- | ---------- |
| alias | string | 否   | Agent 别名 |

### 5.5 删除 Agent

> 需求来源：`design_tailscale_server_web.md` 2.1 列表页 - [详情] 操作（详情页内删除）

| 项目 | 内容                            |
| ---- | ------------------------------- |
| 路径 | DELETE /api/v1/admin/agents/:id |
| 认证 | 需要管理员 JWT                  |

业务流程：

```txt
┌─────────────────────────────────────────────────────────────┐
│                    Agent 删除流程                            │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│   管理员删除 Agent                                          │
│       │                                                     │
│       ▼                                                     │
│   ┌─────────────┐                                          │
│   │ 查询 Agent  │                                          │
│   │ 是否存在    │                                          │
│   └─────────────┘                                          │
│       │                                                     │
│       ▼                                                     │
│   ┌─────────────┐                                          │
│   │调用Headscale│                                          │
│   │删除 Node    │                                          │
│   └─────────────┘                                          │
│       │                                                     │
│       ▼                                                     │
│   ┌─────────────┐                                          │
│   │调用Headscale│                                          │
│   │删除 User    │                                          │
│   └─────────────┘                                          │
│       │                                                     │
│       ▼                                                     │
│   ┌─────────────┐    成功    ┌─────────────┐              │
│   │ 删除数据库  │──────────►│ 返回成功     │              │
│   │ Agent 记录  │            └─────────────┘              │
│   └─────────────┘                                          │
│       │ 失败                                               │
│       ▼                                                     │
│   ┌─────────────┐                                          │
│   │ 返回错误     │                                          │
│   └─────────────┘                                          │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

## 6. Client 管理 API

### 6.1 获取 Client 列表

> 需求来源：`design_tailscale_server_web.md` 4.1 列表页

| 项目 | 内容                      |
| ---- | ------------------------- |
| 路径 | GET /api/v1/admin/clients |
| 认证 | 需要管理员 JWT            |

查询参数：

| 参数   | 类型   | 必填 | 说明              |
| ------ | ------ | ---- | ----------------- |
| page   | int    | 否   | 页码，默认 1      |
| size   | int    | 否   | 每页数量，默认 20 |
| search | string | 否   | 搜索关键词        |

响应模型 ClientListItem：

| 字段          | 类型   | 说明       | 数据来源                              |
| ------------- | ------ | ---------- | ------------------------------------- |
| id            | uint64 | Client ID  | client.id                             |
| name          | string | 用户名     | client.name                           |
| alias         | string | 用户别名   | client.alias                          |
| desktop_count | int    | 客户端数量 | COUNT(desktop.client_id)              |
| status        | string | 状态       | 计算字段（任一 Desktop 在线则为在线） |
| last_online   | string | 最后在线   | MAX(desktop.last_online)              |

### 6.2 获取 Client 详情

> 需求来源：`design_tailscale_server_web.md` 4.2 详情页

| 项目 | 内容                          |
| ---- | ----------------------------- |
| 路径 | GET /api/v1/admin/clients/:id |
| 认证 | 需要管理员 JWT                |

响应模型 ClientDetail：

| 字段       | 类型   | 说明      | 数据来源          |
| ---------- | ------ | --------- | ----------------- |
| id         | uint64 | Client ID | client.id         |
| name       | string | 用户名    | client.name       |
| alias      | string | 用户别名  | client.alias      |
| created_at | string | 创建时间  | client.created_at |

#### 6.2.1 获取 Client 所属分组

| 项目 | 内容                                 |
| ---- | ------------------------------------ |
| 路径 | GET /api/v1/admin/clients/:id/groups |
| 认证 | 需要管理员 JWT                       |

响应模型 ClientGroupItem[]：

| 字段 | 类型   | 说明    | 数据来源          |
| ---- | ------ | ------- | ----------------- |
| id   | uint64 | 分组 ID | client_group.id   |
| name | string | 分组名  | client_group.name |

#### 6.2.2 获取 Client 的 Desktop 列表

| 项目 | 内容                                   |
| ---- | -------------------------------------- |
| 路径 | GET /api/v1/admin/clients/:id/desktops |
| 认证 | 需要管理员 JWT                         |

响应模型 DesktopItem[]：

| 字段        | 类型   | 说明       | 数据来源            |
| ----------- | ------ | ---------- | ------------------- |
| id          | uint64 | Desktop ID | desktop.id          |
| device_name | string | 设备名称   | desktop.device_name |
| tunnel_ip   | string | 隧道 IP    | desktop.tunnel_ip   |
| status      | string | 状态       | desktop.status      |
| last_online | string | 最后在线   | desktop.last_online |

#### 6.2.3 获取 Client 已授权服务列表

| 项目 | 内容                                   |
| ---- | -------------------------------------- |
| 路径 | GET /api/v1/admin/clients/:id/services |
| 认证 | 需要管理员 JWT                         |

响应模型 AuthorizedServiceItem[]：

| 字段        | 类型   | 说明       | 数据来源                      |
| ----------- | ------ | ---------- | ----------------------------- |
| id          | uint64 | 服务 ID    | proxy_service.id              |
| name        | string | 服务名称   | proxy_service.name            |
| agent_name  | string | 所属 Agent | agent.name                    |
| listen_addr | string | 访问地址   | proxy_service.listen_addr     |
| auth_type   | string | 授权方式   | 计算字段（分组授权/单独授权） |
| granted_at  | string | 授权时间   | permission.granted_at         |

### 6.3 创建 Client

> 需求来源：`design_tailscale_server_web.md` 4.1 列表页 - [+ 创建] 按钮

| 项目 | 内容                       |
| ---- | -------------------------- |
| 路径 | POST /api/v1/admin/clients |
| 认证 | 需要管理员 JWT             |

业务流程：

```txt
┌─────────────────────────────────────────────────────────────┐
│                    Client 创建流程                           │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│   管理员创建 Client                                         │
│       │                                                     │
│       ▼                                                     │
│   ┌─────────────┐                                          │
│   │ 生成 Client │                                          │
│   │ 名称和密钥  │                                          │
│   └─────────────┘                                          │
│       │                                                     │
│       ▼                                                     │
│   ┌─────────────┐    成功    ┌─────────────┐              │
│   │调用Headscale│──────────►│ 保存到数据库 │              │
│   │创建 User    │            └─────────────┘              │
│   └─────────────┘                    │                     │
│       │ 失败                         ▼                     │
│       ▼                        ┌─────────────┐             │
│   ┌─────────────┐              │ 返回 Client │             │
│   │ 返回错误     │              │ 信息和密钥  │             │
│   └─────────────┘              └─────────────┘             │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

请求字段：

| 字段  | 类型   | 必填 | 说明     |
| ----- | ------ | ---- | -------- |
| name  | string | 是   | 用户名   |
| alias | string | 否   | 用户别名 |

响应字段：

| 字段   | 类型   | 说明                     |
| ------ | ------ | ------------------------ |
| id     | uint64 | Client ID                |
| name   | string | 用户名                   |
| secret | string | 密钥（仅创建时返回一次） |

### 6.4 更新 Client

> 需求来源：`design_tailscale_server_web.md` 4.2 详情页 - 基本信息 [编辑] 按钮

| 项目 | 内容                          |
| ---- | ----------------------------- |
| 路径 | PUT /api/v1/admin/clients/:id |
| 认证 | 需要管理员 JWT                |

请求字段：

| 字段  | 类型   | 必填 | 说明     |
| ----- | ------ | ---- | -------- |
| alias | string | 否   | 用户别名 |

### 6.5 删除 Client

> 需求来源：`design_tailscale_server_web.md` 4.1 列表页 - [删除] 按钮

| 项目 | 内容                             |
| ---- | -------------------------------- |
| 路径 | DELETE /api/v1/admin/clients/:id |
| 认证 | 需要管理员 JWT                   |

业务流程：

```txt
┌─────────────────────────────────────────────────────────────┐
│                    Client 删除流程                           │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│   管理员删除 Client                                         │
│       │                                                     │
│       ▼                                                     │
│   ┌─────────────┐                                          │
│   │ 查询 Client │                                          │
│   │ 是否存在    │                                          │
│   └─────────────┘                                          │
│       │                                                     │
│       ▼                                                     │
│   ┌─────────────┐                                          │
│   │调用Headscale│                                          │
│   │删除所有Node │                                          │
│   └─────────────┘                                          │
│       │                                                     │
│       ▼                                                     │
│   ┌─────────────┐                                          │
│   │调用Headscale│                                          │
│   │删除 User    │                                          │
│   └─────────────┘                                          │
│       │                                                     │
│       ▼                                                     │
│   ┌─────────────┐    成功    ┌─────────────┐              │
│   │ 删除数据库  │──────────►│ 返回成功     │              │
│   │ Client 记录 │            └─────────────┘              │
│   └─────────────┘                                          │
│       │ 失败                                               │
│       ▼                                                     │
│   ┌─────────────┐                                          │
│   │ 返回错误     │                                          │
│   └─────────────┘                                          │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

### 6.6 注销 Desktop

> 需求来源：`design_tailscale_server_web.md` 4.2 详情页 - 客户端表格 [注销] 按钮

| 项目 | 内容                                                |
| ---- | --------------------------------------------------- |
| 路径 | POST /api/v1/admin/clients/:id/desktops/:did/logout |
| 认证 | 需要管理员 JWT                                      |

说明：强制注销指定 Desktop，使其需要重新认证

### 6.7 删除 Desktop

> 需求来源：`design_tailscale_server_web.md` 4.2 详情页 - 客户端表格 [删除] 按钮

| 项目 | 内容                                           |
| ---- | ---------------------------------------------- |
| 路径 | DELETE /api/v1/admin/clients/:id/desktops/:did |
| 认证 | 需要管理员 JWT                                 |

业务流程：

```txt
┌─────────────────────────────────────────────────────────────┐
│                    Desktop 删除流程                          │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│   管理员删除 Desktop                                        │
│       │                                                     │
│       ▼                                                     │
│   ┌─────────────┐                                          │
│   │调用Headscale│                                          │
│   │删除 Node    │                                          │
│   └─────────────┘                                          │
│       │                                                     │
│       ▼                                                     │
│   ┌─────────────┐    成功    ┌─────────────┐              │
│   │ 删除数据库  │──────────►│ 返回成功     │              │
│   │Desktop 记录 │            └─────────────┘              │
│   └─────────────┘                                          │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

## 7. 端口映射服务 API

### 7.1 获取服务列表

> 需求来源：`design_tailscale_server_web.md` 3.1 桌面授权、3.2 代理授权

| 项目 | 内容                       |
| ---- | -------------------------- |
| 路径 | GET /api/v1/admin/services |
| 认证 | 需要管理员 JWT             |

查询参数：

| 参数     | 类型   | 必填 | 说明              |
| -------- | ------ | ---- | ----------------- |
| agent_id | uint64 | 否   | 按 Agent 筛选     |
| page     | int    | 否   | 页码，默认 1      |
| size     | int    | 否   | 每页数量，默认 20 |

响应模型 ServiceListItem[]：

| 字段         | 类型   | 说明       | 数据来源                                                  |
| ------------ | ------ | ---------- | --------------------------------------------------------- |
| id           | uint64 | 服务 ID    | proxy_service.id                                          |
| name         | string | 服务名称   | proxy_service.name                                        |
| agent_id     | uint64 | Agent ID   | proxy_service.agent_id                                    |
| agent_name   | string | 所属 Agent | agent.name                                                |
| source_addr  | string | 源地址     | proxy_service.source_addr                                 |
| target_addr  | string | 目标地址   | proxy_service.target_addr                                 |
| enabled      | bool   | 是否启用   | proxy_service.enabled                                     |
| status       | string | 运行状态   | proxy_service.status                                      |
| client_count | int    | 授权用户数 | COUNT(service_client_permission)                          |
| group_count  | int    | 授权分组数 | COUNT(service_group_permission WHERE group_type='client') |

### 7.2 获取服务详情

> 需求来源：`design_tailscale_server_web.md` 3.1 桌面授权 - 点击服务查看详情

| 项目 | 内容                           |
| ---- | ------------------------------ |
| 路径 | GET /api/v1/admin/services/:id |
| 认证 | 需要管理员 JWT                 |

响应模型 ServiceDetail：

| 字段        | 类型   | 说明       | 数据来源                  |
| ----------- | ------ | ---------- | ------------------------- |
| id          | uint64 | 服务 ID    | proxy_service.id          |
| name        | string | 服务名称   | proxy_service.name        |
| alias       | string | 服务别名   | proxy_service.alias       |
| agent_id    | uint64 | Agent ID   | proxy_service.agent_id    |
| agent_name  | string | 所属 Agent | agent.name                |
| source_addr | string | 源地址     | proxy_service.source_addr |
| target_addr | string | 目标地址   | proxy_service.target_addr |
| enabled     | bool   | 是否启用   | proxy_service.enabled     |
| status      | string | 运行状态   | proxy_service.status      |
| error_msg   | string | 错误信息   | proxy_service.error_msg   |
| created_at  | string | 创建时间   | proxy_service.created_at  |

### 7.3 创建服务

> 需求来源：`design_tailscale_server_web.md` 2.2 详情页 - 端口映射 [+ 创建] 按钮

| 项目 | 内容                        |
| ---- | --------------------------- |
| 路径 | POST /api/v1/admin/services |
| 认证 | 需要管理员 JWT              |

请求字段：

| 字段        | 类型   | 必填 | 说明                                      |
| ----------- | ------ | ---- | ----------------------------------------- |
| agent_id    | uint64 | 是   | 所属 Agent ID                             |
| name        | string | 是   | 服务名称                                  |
| alias       | string | 否   | 服务别名                                  |
| source_addr | string | 是   | 源地址（VPN IP:端口，如 100.64.0.1:2222） |
| target_addr | string | 是   | 目标地址（如 127.0.0.1:22）               |

### 7.4 更新服务

> 需求来源：`design_tailscale_server_web.md` 2.2 详情页 - 本地服务 [编辑] 按钮

| 项目 | 内容                           |
| ---- | ------------------------------ |
| 路径 | PUT /api/v1/admin/services/:id |
| 认证 | 需要管理员 JWT                 |

请求字段：

| 字段        | 类型   | 必填 | 说明     |
| ----------- | ------ | ---- | -------- |
| alias       | string | 否   | 服务别名 |
| source_addr | string | 否   | 源地址   |
| target_addr | string | 否   | 目标地址 |
| enabled     | bool   | 否   | 是否启用 |

### 7.5 启用/禁用服务

> 需求来源：`design_tailscale_server_web.md` 2.2 详情页 - 本地服务 [启用]/[禁用] 按钮

| 项目 | 内容                                  |
| ---- | ------------------------------------- |
| 路径 | PUT /api/v1/admin/services/:id/toggle |
| 认证 | 需要管理员 JWT                        |

请求字段：

| 字段    | 类型 | 必填 | 说明     |
| ------- | ---- | ---- | -------- |
| enabled | bool | 是   | 是否启用 |

业务流程：

```txt
┌─────────────────────────────────────────────────────────────┐
│                  服务启用/禁用流程                           │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│   管理员点击启用/禁用                                       │
│       │                                                     │
│       ▼                                                     │
│   ┌─────────────┐                                          │
│   │ 更新数据库  │                                          │
│   │ enabled 字段│                                          │
│   └─────────────┘                                          │
│       │                                                     │
│       ▼                                                     │
│   ┌─────────────┐                                          │
│   │ 通过 gRPC   │                                          │
│   │ 下发命令    │                                          │
│   │ 给 Agent    │                                          │
│   └─────────────┘                                          │
│       │                                                     │
│       ├─── 启用 ──► START_PROXY 命令                       │
│       │                                                     │
│       └─── 禁用 ──► STOP_PROXY 命令                        │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

### 7.6 重试服务

> 需求来源：`design_tailscale_server_web.md` 2.2 详情页 - 本地服务 [重试] 按钮（错误状态时显示）

| 项目 | 内容                                  |
| ---- | ------------------------------------- |
| 路径 | POST /api/v1/admin/services/:id/retry |
| 认证 | 需要管理员 JWT                        |

说明：重新尝试启动处于错误状态的服务

### 7.7 删除服务

> 需求来源：`design_tailscale_server_web.md` 2.2 详情页 - 本地服务 [删除] 按钮

| 项目 | 内容                              |
| ---- | --------------------------------- |
| 路径 | DELETE /api/v1/admin/services/:id |
| 认证 | 需要管理员 JWT                    |

业务流程：

```txt
┌─────────────────────────────────────────────────────────────┐
│                    服务删除流程                              │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│   管理员删除服务                                            │
│       │                                                     │
│       ▼                                                     │
│   ┌─────────────┐                                          │
│   │ 删除相关的  │                                          │
│   │ 权限记录    │                                          │
│   └─────────────┘                                          │
│       │                                                     │
│       ▼                                                     │
│   ┌─────────────┐                                          │
│   │ 删除服务记录│                                          │
│   └─────────────┘                                          │
│       │                                                     │
│       ▼                                                     │
│   ┌─────────────┐    成功    ┌─────────────┐              │
│   │ 同步 ACL 到 │──────────►│ 返回成功     │              │
│   │ Headscale   │            └─────────────┘              │
│   └─────────────┘                                          │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

## 8. 服务权限 API

服务权限分为两类：

- 桌面授权：控制哪些 Client/ClientGroup 可以访问服务
- 代理授权：控制哪些 Agent/AgentGroup 可以代理访问服务

### 8.1 桌面授权 - 用户授权

> 需求来源：`design_tailscale_server_web.md` 3.1 桌面授权 - [+ 用户] 按钮

#### 获取服务的用户授权列表

| 项目 | 内容                                   |
| ---- | -------------------------------------- |
| 路径 | GET /api/v1/admin/services/:id/clients |
| 认证 | 需要管理员 JWT                         |

响应模型 AuthorizedClient[]：

| 字段       | 类型   | 说明      | 数据来源                             |
| ---------- | ------ | --------- | ------------------------------------ |
| id         | uint64 | Client ID | client.id                            |
| name       | string | 用户名    | client.name                          |
| alias      | string | 用户别名  | client.alias                         |
| granted_at | string | 授权时间  | service_client_permission.created_at |

#### 添加用户授权

| 项目 | 内容                                    |
| ---- | --------------------------------------- |
| 路径 | POST /api/v1/admin/services/:id/clients |
| 认证 | 需要管理员 JWT                          |

业务流程：

```txt
┌─────────────────────────────────────────────────────────────┐
│                  添加用户授权流程                            │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│   管理员添加用户授权                                        │
│       │                                                     │
│       ▼                                                     │
│   ┌─────────────┐                                          │
│   │ 保存权限记录 │                                          │
│   │ 到数据库     │                                          │
│   └─────────────┘                                          │
│       │                                                     │
│       ▼                                                     │
│   ┌─────────────┐    成功    ┌─────────────┐              │
│   │ 同步 ACL 到 │──────────►│ 权限立即生效 │              │
│   │ Headscale   │            └─────────────┘              │
│   └─────────────┘                                          │
│       │ 失败                                               │
│       ▼                                                     │
│   ┌─────────────┐                                          │
│   │ 回滚数据库   │                                          │
│   │ 返回错误     │                                          │
│   └─────────────┘                                          │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

请求字段：

| 字段      | 类型   | 必填 | 说明      |
| --------- | ------ | ---- | --------- |
| client_id | uint64 | 是   | Client ID |

#### 移除用户授权

| 项目 | 内容                                           |
| ---- | ---------------------------------------------- |
| 路径 | DELETE /api/v1/admin/services/:id/clients/:cid |
| 认证 | 需要管理员 JWT                                 |

说明：移除后同步 ACL 到 Headscale

### 8.2 桌面授权 - 用户分组授权

> 需求来源：`design_tailscale_server_web.md` 3.1 桌面授权 - [+ 分组] 按钮

#### 获取服务的用户分组授权列表

| 项目 | 内容                                         |
| ---- | -------------------------------------------- |
| 路径 | GET /api/v1/admin/services/:id/client-groups |
| 认证 | 需要管理员 JWT                               |

响应模型 AuthorizedClientGroup[]：

| 字段         | 类型   | 说明           | 数据来源                            |
| ------------ | ------ | -------------- | ----------------------------------- |
| id           | uint64 | ClientGroup ID | client_group.id                     |
| name         | string | 分组名称       | client_group.name                   |
| alias        | string | 分组别名       | client_group.alias                  |
| member_count | int    | 成员数量       | COUNT(client_group_member)          |
| granted_at   | string | 授权时间       | service_group_permission.created_at |

#### 添加用户分组授权

| 项目 | 内容                                          |
| ---- | --------------------------------------------- |
| 路径 | POST /api/v1/admin/services/:id/client-groups |
| 认证 | 需要管理员 JWT                                |

请求字段：

| 字段     | 类型   | 必填 | 说明           |
| -------- | ------ | ---- | -------------- |
| group_id | uint64 | 是   | ClientGroup ID |

说明：添加后同步 ACL 到 Headscale

#### 移除用户分组授权

| 项目 | 内容                                                 |
| ---- | ---------------------------------------------------- |
| 路径 | DELETE /api/v1/admin/services/:id/client-groups/:gid |
| 认证 | 需要管理员 JWT                                       |

说明：移除后同步 ACL 到 Headscale

### 8.3 代理授权 - Agent 授权

> 需求来源：`design_tailscale_server_web.md` 3.2 代理授权 - [+ 代理] 按钮

#### 获取服务的 Agent 授权列表

| 项目 | 内容                                  |
| ---- | ------------------------------------- |
| 路径 | GET /api/v1/admin/services/:id/agents |
| 认证 | 需要管理员 JWT                        |

响应模型 AuthorizedAgent[]：

| 字段       | 类型   | 说明       | 数据来源                            |
| ---------- | ------ | ---------- | ----------------------------------- |
| id         | uint64 | Agent ID   | agent.id                            |
| name       | string | Agent 名称 | agent.name                          |
| alias      | string | Agent 别名 | agent.alias                         |
| granted_at | string | 授权时间   | service_agent_permission.created_at |

#### 添加 Agent 授权

| 项目 | 内容                                   |
| ---- | -------------------------------------- |
| 路径 | POST /api/v1/admin/services/:id/agents |
| 认证 | 需要管理员 JWT                         |

请求字段：

| 字段     | 类型   | 必填 | 说明     |
| -------- | ------ | ---- | -------- |
| agent_id | uint64 | 是   | Agent ID |

说明：添加后同步 ACL 到 Headscale

#### 移除 Agent 授权

| 项目 | 内容                                          |
| ---- | --------------------------------------------- |
| 路径 | DELETE /api/v1/admin/services/:id/agents/:aid |
| 认证 | 需要管理员 JWT                                |

说明：移除后同步 ACL 到 Headscale

### 8.4 代理授权 - Agent 分组授权

> 需求来源：`design_tailscale_server_web.md` 3.2 代理授权 - [+ 分组] 按钮

#### 获取服务的 Agent 分组授权列表

| 项目 | 内容                                        |
| ---- | ------------------------------------------- |
| 路径 | GET /api/v1/admin/services/:id/agent-groups |
| 认证 | 需要管理员 JWT                              |

响应模型 AuthorizedAgentGroup[]：

| 字段         | 类型   | 说明          | 数据来源                            |
| ------------ | ------ | ------------- | ----------------------------------- |
| id           | uint64 | AgentGroup ID | agent_group.id                      |
| name         | string | 分组名称      | agent_group.name                    |
| alias        | string | 分组别名      | agent_group.alias                   |
| member_count | int    | 成员数量      | COUNT(agent_group_member)           |
| granted_at   | string | 授权时间      | service_group_permission.created_at |

#### 添加 Agent 分组授权

| 项目 | 内容                                         |
| ---- | -------------------------------------------- |
| 路径 | POST /api/v1/admin/services/:id/agent-groups |
| 认证 | 需要管理员 JWT                               |

请求字段：

| 字段     | 类型   | 必填 | 说明          |
| -------- | ------ | ---- | ------------- |
| group_id | uint64 | 是   | AgentGroup ID |

说明：添加后同步 ACL 到 Headscale

#### 移除 Agent 分组授权

| 项目 | 内容                                                |
| ---- | --------------------------------------------------- |
| 路径 | DELETE /api/v1/admin/services/:id/agent-groups/:gid |
| 认证 | 需要管理员 JWT                                      |

说明：移除后同步 ACL 到 Headscale

## 9. 分组管理 API

### 9.1 获取用户分组列表

> 需求来源：`design_tailscale_server_web.md` 5.1 用户分组

| 项目 | 内容                            |
| ---- | ------------------------------- |
| 路径 | GET /api/v1/admin/client-groups |
| 认证 | 需要管理员 JWT                  |

查询参数：

| 参数 | 类型 | 必填 | 说明              |
| ---- | ---- | ---- | ----------------- |
| page | int  | 否   | 页码，默认 1      |
| size | int  | 否   | 每页数量，默认 20 |

响应模型 ClientGroupListItem[]：

| 字段         | 类型   | 说明     | 数据来源                            |
| ------------ | ------ | -------- | ----------------------------------- |
| id           | uint64 | 分组 ID  | client_group.id                     |
| name         | string | 分组名称 | client_group.name                   |
| alias        | string | 分组别名 | client_group.alias                  |
| member_count | int    | 成员数量 | COUNT(client_group_member.group_id) |
| description  | string | 描述     | client_group.description            |
| created_at   | string | 创建时间 | client_group.created_at             |

### 9.2 创建用户分组

> 需求来源：`design_tailscale_server_web.md` 5.1 用户分组 - [+ 创建分组] 按钮

| 项目 | 内容                             |
| ---- | -------------------------------- |
| 路径 | POST /api/v1/admin/client-groups |
| 认证 | 需要管理员 JWT                   |

请求字段：

| 字段        | 类型   | 必填 | 说明     |
| ----------- | ------ | ---- | -------- |
| name        | string | 是   | 分组名称 |
| alias       | string | 否   | 分组别名 |
| description | string | 否   | 描述     |

### 9.3 更新用户分组

> 需求来源：`design_tailscale_server_web.md` 5.1 用户分组 - [编辑] 按钮

| 项目 | 内容                                |
| ---- | ----------------------------------- |
| 路径 | PUT /api/v1/admin/client-groups/:id |
| 认证 | 需要管理员 JWT                      |

请求字段：

| 字段        | 类型   | 必填 | 说明     |
| ----------- | ------ | ---- | -------- |
| alias       | string | 否   | 分组别名 |
| description | string | 否   | 描述     |

### 9.4 删除用户分组

> 需求来源：`design_tailscale_server_web.md` 5.1 用户分组 - [删除] 按钮

| 项目 | 内容                                   |
| ---- | -------------------------------------- |
| 路径 | DELETE /api/v1/admin/client-groups/:id |
| 认证 | 需要管理员 JWT                         |

业务流程：

```txt
┌─────────────────────────────────────────────────────────────┐
│                  用户分组删除流程                            │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│   管理员删除用户分组                                        │
│       │                                                     │
│       ▼                                                     │
│   ┌─────────────┐                                          │
│   │ 查询分组内  │                                          │
│   │ 所有成员    │                                          │
│   └─────────────┘                                          │
│       │                                                     │
│       ▼                                                     │
│   ┌─────────────┐                                          │
│   │ 遍历每个成员│                                          │
│   │ 的所有Node  │                                          │
│   │ 移除分组Tag │                                          │
│   └─────────────┘                                          │
│       │                                                     │
│       ▼                                                     │
│   ┌─────────────┐                                          │
│   │ 删除分组权限│                                          │
│   │ 关联记录    │                                          │
│   └─────────────┘                                          │
│       │                                                     │
│       ▼                                                     │
│   ┌─────────────┐                                          │
│   │ 移除该分组  │                                          │
│   │ 相关的 ACL  │                                          │
│   └─────────────┘                                          │
│       │                                                     │
│       ▼                                                     │
│   ┌─────────────┐                                          │
│   │ 删除分组成员│                                          │
│   │ 关联记录    │                                          │
│   └─────────────┘                                          │
│       │                                                     │
│       ▼                                                     │
│   ┌─────────────┐    成功    ┌─────────────┐              │
│   │ 删除分组记录│──────────►│ 返回成功     │              │
│   └─────────────┘            └─────────────┘              │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

### 9.5 获取用户分组成员

> 需求来源：`design_tailscale_server_web.md` 5.1 用户分组 - 成员数字可点击

| 项目 | 内容                                        |
| ---- | ------------------------------------------- |
| 路径 | GET /api/v1/admin/client-groups/:id/members |
| 认证 | 需要管理员 JWT                              |

响应模型 ClientGroupMember[]：

| 字段      | 类型   | 说明      | 数据来源                       |
| --------- | ------ | --------- | ------------------------------ |
| id        | uint64 | Client ID | client.id                      |
| name      | string | 用户名    | client.name                    |
| alias     | string | 用户别名  | client.alias                   |
| joined_at | string | 加入时间  | client_group_member.created_at |

### 9.6 添加用户分组成员

| 项目 | 内容                                         |
| ---- | -------------------------------------------- |
| 路径 | POST /api/v1/admin/client-groups/:id/members |
| 认证 | 需要管理员 JWT                               |

业务流程：

```txt
┌─────────────────────────────────────────────────────────────┐
│                  添加用户分组成员流程                        │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│   管理员添加成员到分组                                      │
│       │                                                     │
│       ▼                                                     │
│   ┌─────────────┐                                          │
│   │ 保存成员关联│                                          │
│   │ 到数据库    │                                          │
│   └─────────────┘                                          │
│       │                                                     │
│       ▼                                                     │
│   ┌─────────────┐                                          │
│   │ 查询该用户  │                                          │
│   │ 所有 Desktop│                                          │
│   └─────────────┘                                          │
│       │                                                     │
│       ▼                                                     │
│   ┌─────────────┐    成功    ┌─────────────┐              │
│   │ 给每个Node  │──────────►│ 返回成功     │              │
│   │ 添加分组Tag │            └─────────────┘              │
│   └─────────────┘                                          │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

请求字段：

| 字段      | 类型   | 必填 | 说明      |
| --------- | ------ | ---- | --------- |
| client_id | uint64 | 是   | Client ID |

### 9.7 移除用户分组成员

| 项目 | 内容                                                |
| ---- | --------------------------------------------------- |
| 路径 | DELETE /api/v1/admin/client-groups/:id/members/:cid |
| 认证 | 需要管理员 JWT                                      |

业务流程：

```txt
┌─────────────────────────────────────────────────────────────┐
│                  移除用户分组成员流程                        │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│   管理员从分组移除成员                                      │
│       │                                                     │
│       ▼                                                     │
│   ┌─────────────┐                                          │
│   │ 删除成员关联│                                          │
│   │ 记录        │                                          │
│   └─────────────┘                                          │
│       │                                                     │
│       ▼                                                     │
│   ┌─────────────┐                                          │
│   │ 查询该用户  │                                          │
│   │ 所有 Desktop│                                          │
│   └─────────────┘                                          │
│       │                                                     │
│       ▼                                                     │
│   ┌─────────────┐    成功    ┌─────────────┐              │
│   │ 给每个Node  │──────────►│ 返回成功     │              │
│   │ 移除分组Tag │            └─────────────┘              │
│   └─────────────┘                                          │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

### 9.8 获取代理分组列表

> 需求来源：`design_tailscale_server_web.md` 5.2 代理分组

| 项目 | 内容                           |
| ---- | ------------------------------ |
| 路径 | GET /api/v1/admin/agent-groups |
| 认证 | 需要管理员 JWT                 |

查询参数：

| 参数 | 类型 | 必填 | 说明              |
| ---- | ---- | ---- | ----------------- |
| page | int  | 否   | 页码，默认 1      |
| size | int  | 否   | 每页数量，默认 20 |

响应模型 AgentGroupListItem[]：

| 字段         | 类型   | 说明     | 数据来源                           |
| ------------ | ------ | -------- | ---------------------------------- |
| id           | uint64 | 分组 ID  | agent_group.id                     |
| name         | string | 分组名称 | agent_group.name                   |
| alias        | string | 分组别名 | agent_group.alias                  |
| member_count | int    | 成员数量 | COUNT(agent_group_member.group_id) |
| description  | string | 描述     | agent_group.description            |
| created_at   | string | 创建时间 | agent_group.created_at             |

### 9.9 创建代理分组

> 需求来源：`design_tailscale_server_web.md` 5.2 代理分组 - [+ 创建分组] 按钮

| 项目 | 内容                            |
| ---- | ------------------------------- |
| 路径 | POST /api/v1/admin/agent-groups |
| 认证 | 需要管理员 JWT                  |

请求字段：

| 字段        | 类型   | 必填 | 说明     |
| ----------- | ------ | ---- | -------- |
| name        | string | 是   | 分组名称 |
| alias       | string | 否   | 分组别名 |
| description | string | 否   | 描述     |

### 9.10 更新代理分组

> 需求来源：`design_tailscale_server_web.md` 5.2 代理分组 - [编辑] 按钮

| 项目 | 内容                               |
| ---- | ---------------------------------- |
| 路径 | PUT /api/v1/admin/agent-groups/:id |
| 认证 | 需要管理员 JWT                     |

请求字段：

| 字段        | 类型   | 必填 | 说明     |
| ----------- | ------ | ---- | -------- |
| alias       | string | 否   | 分组别名 |
| description | string | 否   | 描述     |

### 9.11 删除代理分组

> 需求来源：`design_tailscale_server_web.md` 5.2 代理分组 - [删除] 按钮

| 项目 | 内容                                  |
| ---- | ------------------------------------- |
| 路径 | DELETE /api/v1/admin/agent-groups/:id |
| 认证 | 需要管理员 JWT                        |

业务流程：

```txt
┌─────────────────────────────────────────────────────────────┐
│                  代理分组删除流程                            │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│   管理员删除代理分组                                        │
│       │                                                     │
│       ▼                                                     │
│   ┌─────────────┐                                          │
│   │ 查询分组内  │                                          │
│   │ 所有成员    │                                          │
│   └─────────────┘                                          │
│       │                                                     │
│       ▼                                                     │
│   ┌─────────────┐                                          │
│   │ 遍历每个    │                                          │
│   │ Agent Node  │                                          │
│   │ 移除分组Tag │                                          │
│   └─────────────┘                                          │
│       │                                                     │
│       ▼                                                     │
│   ┌─────────────┐                                          │
│   │ 删除分组权限│                                          │
│   │ 关联记录    │                                          │
│   └─────────────┘                                          │
│       │                                                     │
│       ▼                                                     │
│   ┌─────────────┐                                          │
│   │ 移除该分组  │                                          │
│   │ 相关的 ACL  │                                          │
│   └─────────────┘                                          │
│       │                                                     │
│       ▼                                                     │
│   ┌─────────────┐                                          │
│   │ 删除分组成员│                                          │
│   │ 关联记录    │                                          │
│   └─────────────┘                                          │
│       │                                                     │
│       ▼                                                     │
│   ┌─────────────┐    成功    ┌─────────────┐              │
│   │ 删除分组记录│──────────►│ 返回成功     │              │
│   └─────────────┘            └─────────────┘              │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

### 9.12 获取代理分组成员

> 需求来源：`design_tailscale_server_web.md` 5.2 代理分组 - 成员数字可点击

| 项目 | 内容                                       |
| ---- | ------------------------------------------ |
| 路径 | GET /api/v1/admin/agent-groups/:id/members |
| 认证 | 需要管理员 JWT                             |

响应模型 AgentGroupMember[]：

| 字段      | 类型   | 说明       | 数据来源                      |
| --------- | ------ | ---------- | ----------------------------- |
| id        | uint64 | Agent ID   | agent.id                      |
| name      | string | Agent 名称 | agent.name                    |
| alias     | string | Agent 别名 | agent.alias                   |
| joined_at | string | 加入时间   | agent_group_member.created_at |

### 9.13 添加代理分组成员

| 项目 | 内容                                        |
| ---- | ------------------------------------------- |
| 路径 | POST /api/v1/admin/agent-groups/:id/members |
| 认证 | 需要管理员 JWT                              |

业务流程：

```txt
┌─────────────────────────────────────────────────────────────┐
│                  添加代理分组成员流程                        │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│   管理员添加 Agent 到分组                                   │
│       │                                                     │
│       ▼                                                     │
│   ┌─────────────┐                                          │
│   │ 保存成员关联│                                          │
│   │ 到数据库    │                                          │
│   └─────────────┘                                          │
│       │                                                     │
│       ▼                                                     │
│   ┌─────────────┐    成功    ┌─────────────┐              │
│   │ 给Agent Node│──────────►│ 返回成功     │              │
│   │ 添加分组Tag │            └─────────────┘              │
│   └─────────────┘                                          │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

请求字段：

| 字段     | 类型   | 必填 | 说明     |
| -------- | ------ | ---- | -------- |
| agent_id | uint64 | 是   | Agent ID |

### 9.14 移除代理分组成员

| 项目 | 内容                                               |
| ---- | -------------------------------------------------- |
| 路径 | DELETE /api/v1/admin/agent-groups/:id/members/:aid |
| 认证 | 需要管理员 JWT                                     |

业务流程：

```txt
┌─────────────────────────────────────────────────────────────┐
│                  移除代理分组成员流程                        │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│   管理员从分组移除 Agent                                    │
│       │                                                     │
│       ▼                                                     │
│   ┌─────────────┐                                          │
│   │ 删除成员关联│                                          │
│   │ 记录        │                                          │
│   └─────────────┘                                          │
│       │                                                     │
│       ▼                                                     │
│   ┌─────────────┐    成功    ┌─────────────┐              │
│   │ 给Agent Node│──────────►│ 返回成功     │              │
│   │ 移除分组Tag │            └─────────────┘              │
│   └─────────────┘                                          │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

## 10. 审计日志 API

> 需求来源：`design_tailscale_server_web.md` 6. 审计日志

### 10.1 查询审计日志

| 项目 | 内容                         |
| ---- | ---------------------------- |
| 路径 | GET /api/v1/admin/audit/logs |
| 认证 | 需要管理员 JWT               |

查询参数：

| 参数        | 类型   | 必填 | 说明              |
| ----------- | ------ | ---- | ----------------- |
| action_type | string | 否   | 操作类型筛选      |
| user_id     | int64  | 否   | 操作者 ID 筛选    |
| start_date  | string | 否   | 开始日期          |
| end_date    | string | 否   | 结束日期          |
| page        | int    | 否   | 页码，默认 1      |
| size        | int    | 否   | 每页数量，默认 20 |

响应模型 AuditLogItem[]：

| 字段        | 类型   | 说明       | 数据来源                |
| ----------- | ------ | ---------- | ----------------------- |
| id          | uint64 | 日志 ID    | audit_log.id            |
| action_type | string | 操作类型   | audit_log.action_type   |
| actor_name  | string | 操作者名称 | 关联查询 admin.username |
| target_name | string | 目标名称   | audit_log.target_name   |
| detail      | string | 详情       | audit_log.detail        |
| created_at  | string | 创建时间   | audit_log.created_at    |

操作类型枚举（action_type）：

| 值                  | 说明            |
| ------------------- | --------------- |
| create_agent        | 创建 Agent      |
| delete_agent        | 删除 Agent      |
| create_service      | 创建服务        |
| delete_service      | 删除服务        |
| grant_desktop       | 桌面授权        |
| revoke_desktop      | 撤销桌面授权    |
| grant_agent         | 代理授权        |
| revoke_agent        | 撤销代理授权    |
| create_port_forward | 创建端口访问    |
| delete_port_forward | 删除端口访问    |
| create_client_group | 创建用户分组    |
| delete_client_group | 删除用户分组    |
| create_agent_group  | 创建代理分组    |
| delete_agent_group  | 删除代理分组    |
| update_tunnel_user  | 更新隧道 User   |
| delete_tunnel_user  | 删除隧道 User   |
| update_tunnel_node  | 更新隧道 Node   |
| update_tunnel_tags  | 更新 Node Tags  |
| delete_tunnel_node  | 删除隧道 Node   |
| update_tunnel_acl   | 更新 ACL Policy |
| sync_tunnel_acl     | 强制同步 ACL    |

### 10.2 获取操作用户列表

| 项目 | 内容                          |
| ---- | ----------------------------- |
| 路径 | GET /api/v1/admin/audit/users |
| 认证 | 需要管理员 JWT                |

响应模型 UserOption[]：

| 字段     | 类型   | 说明    | 数据来源       |
| -------- | ------ | ------- | -------------- |
| id       | int64  | 用户 ID | admin.id       |
| username | string | 用户名  | admin.username |

## 11. 隧道管理 API

> 需求来源：`design_tunnel_management.md`
>
> 隧道管理 API 用于直接查看和管理 Headscale 中的 User、Node 和 ACL 数据。

### 11.1 User 管理

#### 获取 User 列表

| 项目 | 内容                           |
| ---- | ------------------------------ |
| 路径 | GET /api/v1/admin/tunnel/users |
| 认证 | 需要管理员 JWT                 |

查询参数：

| 参数   | 类型   | 必填 | 说明                                    |
| ------ | ------ | ---- | --------------------------------------- |
| type   | string | 否   | 类型筛选：agent/client/orphan/all       |
| search | string | 否   | 搜索关键词（匹配 name 或 display_name） |
| page   | int    | 否   | 页码，默认 1                            |
| size   | int    | 否   | 每页数量，默认 20                       |

响应模型 TunnelUserListItem[]：

| 字段          | 类型   | 说明         | 数据来源                        |
| ------------- | ------ | ------------ | ------------------------------- |
| id            | uint64 | User ID      | Headscale user.id               |
| name          | string | User 名称    | Headscale user.name             |
| display_name  | string | 显示名称     | Headscale user.display_name     |
| type          | string | 类型         | 计算字段（agent/client/orphan） |
| linked_entity | string | 关联实体名称 | 本地 DB 查询                    |
| linked_id     | uint64 | 关联实体 ID  | 本地 DB 查询                    |
| node_count    | int    | Node 数量    | Headscale ListNodes 统计        |
| created_at    | string | 创建时间     | Headscale user.created_at       |

#### 获取 User 详情

| 项目 | 内容                               |
| ---- | ---------------------------------- |
| 路径 | GET /api/v1/admin/tunnel/users/:id |
| 认证 | 需要管理员 JWT                     |

#### 更新 User

| 项目 | 内容                               |
| ---- | ---------------------------------- |
| 路径 | PUT /api/v1/admin/tunnel/users/:id |
| 认证 | 需要管理员 JWT                     |

请求字段：

| 字段         | 类型   | 必填 | 说明     |
| ------------ | ------ | ---- | -------- |
| display_name | string | 否   | 显示名称 |

#### 删除 User

| 项目 | 内容                                  |
| ---- | ------------------------------------- |
| 路径 | DELETE /api/v1/admin/tunnel/users/:id |
| 认证 | 需要管理员 JWT                        |

说明：删除 User 会同时删除其下所有 Node，并同步删除本地关联的 Agent/Client 记录

#### 获取 User 的 Node 列表

| 项目 | 内容                                     |
| ---- | ---------------------------------------- |
| 路径 | GET /api/v1/admin/tunnel/users/:id/nodes |
| 认证 | 需要管理员 JWT                           |

### 11.2 Node 管理

#### 获取 Node 列表

| 项目 | 内容                           |
| ---- | ------------------------------ |
| 路径 | GET /api/v1/admin/tunnel/nodes |
| 认证 | 需要管理员 JWT                 |

查询参数：

| 参数    | 类型   | 必填 | 说明                         |
| ------- | ------ | ---- | ---------------------------- |
| user_id | uint64 | 否   | 按 User ID 筛选              |
| status  | string | 否   | 状态筛选：online/offline/all |
| search  | string | 否   | 搜索关键词（匹配 name）      |
| page    | int    | 否   | 页码，默认 1                 |
| size    | int    | 否   | 每页数量，默认 20            |

响应模型 TunnelNodeListItem[]：

| 字段       | 类型     | 说明      | 数据来源                       |
| ---------- | -------- | --------- | ------------------------------ |
| id         | uint64   | Node ID   | Headscale node.id              |
| name       | string   | Node 名称 | Headscale node.given_name      |
| user_id    | uint64   | User ID   | Headscale node.user.id         |
| user_name  | string   | User 名称 | Headscale node.user.name       |
| ip_address | string   | IP 地址   | Headscale node.ip_addresses[0] |
| online     | bool     | 是否在线  | Headscale node.online          |
| tags       | []string | Tags 列表 | Headscale node.forced_tags     |
| last_seen  | string   | 最后在线  | Headscale node.last_seen       |
| created_at | string   | 创建时间  | Headscale node.created_at      |

#### 获取 Node 详情

| 项目 | 内容                               |
| ---- | ---------------------------------- |
| 路径 | GET /api/v1/admin/tunnel/nodes/:id |
| 认证 | 需要管理员 JWT                     |

#### 更新 Node

| 项目 | 内容                               |
| ---- | ---------------------------------- |
| 路径 | PUT /api/v1/admin/tunnel/nodes/:id |
| 认证 | 需要管理员 JWT                     |

请求字段：

| 字段       | 类型   | 必填 | 说明     |
| ---------- | ------ | ---- | -------- |
| given_name | string | 否   | 显示名称 |

#### 更新 Node Tags

| 项目 | 内容                                    |
| ---- | --------------------------------------- |
| 路径 | PUT /api/v1/admin/tunnel/nodes/:id/tags |
| 认证 | 需要管理员 JWT                          |

请求字段：

| 字段 | 类型     | 必填 | 说明      |
| ---- | -------- | ---- | --------- |
| tags | []string | 是   | Tags 列表 |

#### 删除 Node

| 项目 | 内容                                  |
| ---- | ------------------------------------- |
| 路径 | DELETE /api/v1/admin/tunnel/nodes/:id |
| 认证 | 需要管理员 JWT                        |

说明：删除 Node 会同步删除本地关联的 Desktop 记录，或清空 Agent 的 node_id

#### 获取常用 Tags 列表

| 项目 | 内容                          |
| ---- | ----------------------------- |
| 路径 | GET /api/v1/admin/tunnel/tags |
| 认证 | 需要管理员 JWT                |

响应模型 TagOption[]：

| 字段  | 类型   | 说明     | 数据来源                      |
| ----- | ------ | -------- | ----------------------------- |
| tag   | string | Tag 名称 | 从分组表生成                  |
| type  | string | 类型     | client-group / agent-group    |
| count | int    | 使用次数 | 统计 Node 中使用该 Tag 的数量 |

### 11.3 ACL 管理

#### 获取 ACL Policy

| 项目 | 内容                         |
| ---- | ---------------------------- |
| 路径 | GET /api/v1/admin/tunnel/acl |
| 认证 | 需要管理员 JWT               |

响应模型 ACLPolicyResponse：

| 字段           | 类型   | 说明            | 数据来源            |
| -------------- | ------ | --------------- | ------------------- |
| policy         | string | ACL Policy JSON | Headscale GetPolicy |
| last_synced_at | string | 最后同步时间    | 本地记录            |

#### 更新 ACL Policy

| 项目 | 内容                         |
| ---- | ---------------------------- |
| 路径 | PUT /api/v1/admin/tunnel/acl |
| 认证 | 需要管理员 JWT               |

请求字段：

| 字段   | 类型   | 必填 | 说明            |
| ------ | ------ | ---- | --------------- |
| policy | string | 是   | ACL Policy JSON |

#### 获取 ACL 规则列表（可视化）

| 项目 | 内容                               |
| ---- | ---------------------------------- |
| 路径 | GET /api/v1/admin/tunnel/acl/rules |
| 认证 | 需要管理员 JWT                     |

响应模型 ACLRulesResponse：

| 字段       | 类型       | 说明         |
| ---------- | ---------- | ------------ |
| rules      | ACLRule[]  | ACL 规则列表 |
| tag_owners | TagOwner[] | Tag 所有者   |

#### 强制同步 ACL

| 项目 | 内容                               |
| ---- | ---------------------------------- |
| 路径 | POST /api/v1/admin/tunnel/acl/sync |
| 认证 | 需要管理员 JWT                     |

说明：根据本地授权数据重新生成 ACL Policy 并同步到 Headscale

---

## 12. 系统配置 API

> 需求来源：`design_tailscale_server_web.md` 7. 系统配置

### 11.1 获取系统配置

| 项目 | 内容                            |
| ---- | ------------------------------- |
| 路径 | GET /api/v1/admin/system/config |
| 认证 | 需要管理员 JWT                  |

响应模型 SystemConfig：

| 字段                  | 类型   | 说明             | 数据来源                                        |
| --------------------- | ------ | ---------------- | ----------------------------------------------- |
| client_download_url   | string | 客户端下载地址   | system_config WHERE key='client_download_url'   |
| desktop_min_version   | string | 客户端最低版本   | system_config WHERE key='desktop_min_version'   |
| headscale_public_url  | string | 公网地址         | system_config WHERE key='headscale_public_url'  |
| stun_port             | int    | STUN 端口        | system_config WHERE key='stun_port'             |
| ip_prefix             | string | IP 地址段        | system_config WHERE key='ip_prefix'             |
| auth_key_expiry_hours | int    | 预认证密钥有效期 | system_config WHERE key='auth_key_expiry_hours' |

### 11.2 更新系统配置

| 项目 | 内容                            |
| ---- | ------------------------------- |
| 路径 | PUT /api/v1/admin/system/config |
| 认证 | 需要管理员 JWT                  |

请求字段：

| 字段                  | 类型   | 必填 | 说明                     |
| --------------------- | ------ | ---- | ------------------------ |
| client_download_url   | string | 否   | 客户端下载地址           |
| desktop_min_version   | string | 否   | 客户端最低版本           |
| headscale_public_url  | string | 否   | 公网地址                 |
| stun_port             | int    | 否   | STUN 端口                |
| ip_prefix             | string | 否   | IP 地址段                |
| auth_key_expiry_hours | int    | 否   | 预认证密钥有效期（小时） |

业务流程：

```txt
┌─────────────────────────────────────────────────────────────┐
│                  系统配置更新流程                            │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│   管理员更新配置                                            │
│       │                                                     │
│       ▼                                                     │
│   ┌─────────────┐                                          │
│   │ 保存配置到  │                                          │
│   │ 数据库      │                                          │
│   └─────────────┘                                          │
│       │                                                     │
│       ▼                                                     │
│   ┌─────────────┐                                          │
│   │ 通知相关组件│                                          │
│   │ 重载配置    │                                          │
│   └─────────────┘                                          │
│       │                                                     │
│       ▼                                                     │
│   ┌─────────────┐                                          │
│   │ 返回成功     │                                          │
│   └─────────────┘                                          │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

## 13. 错误码定义

| HTTP 状态码 | 说明           |
| ----------- | -------------- |
| 200         | 成功           |
| 400         | 请求参数错误   |
| 401         | 未认证         |
| 403         | 权限不足       |
| 404         | 资源不存在     |
| 409         | 资源冲突       |
| 500         | 服务器内部错误 |

---

**文档版本**: 2.1  
**更新日期**: 2026-01-13  
**维护者**: 开发团队
