# ZTNA Web 管理界面设计

## 概述

Web 管理界面在 ZTNA 架构中承担管理控制台角色。在现有管理功能基础上，新增终端管理、K8S 权限、域名管理、操作审计等功能模块。

## 现有导航结构

```
├── 用户管理          /users
│     用户列表（agent/client 统一），支持角色/启用/来源筛选
│     用户详情（Deploy Token、设备列表、服务列表）
│
├── 设备管理          /nodes
│     设备列表（代理设备/桌面设备），显示 IP、主机名、状态、心跳
│     设备详情
│
├── 分组管理          /groups
│     分组列表，分组成员管理
│
├── 授权管理（展开）
│     ├── 服务授权    /acl/services
│     │     ProxyService 列表，管理用户/分组级服务访问权限
│     ├── 用户授权    /acl/users
│     │     用户级 Agent 访问权限（AclUserPermission）
│     ├── 分组授权    /acl/groups
│     │     分组级 Agent 访问权限（AclGroupPermission）
│     └── SSH 授权    /acl/ssh
│           SSH 访问权限，管理用户/分组级 SSH 授权和 Linux 用户
│
├── 隧道管理（展开）
│     ├── User 管理   /tunnel/users    — Headscale 用户
│     ├── Node 管理   /tunnel/nodes    — Headscale 节点
│     ├── ACL 管理    /tunnel/acl      — Headscale ACL 策略
│     └── SSH 策略    /tunnel/ssh      — Headscale SSH 策略
│
├── 审计日志          /audit-logs
│     连接审计日志列表
│
└── 系统配置          /system/config
      系统参数配置
```

## 现有页面功能说明

### 用户管理

用户列表页面统一管理 agent 和 client 两种角色的用户。支持按角色、启用状态、来源（手动/Logto）筛选，支持搜索。可创建用户、启用/禁用用户。

用户详情页面展示用户基本信息，包含 Deploy Token 管理（生成/查看/删除）、关联的设备列表、关联的服务列表。

### 设备管理

设备列表页面展示所有 Headscale 节点。按类型分为代理设备（agent）和桌面设备（desktop）。显示 ID、名称、所属用户、类型、IP 地址、主机名、node.status、最后心跳时间。

### 分组管理

分组列表页面管理用户分组。支持创建分组、编辑分组、管理分组成员（添加/移除用户）。

### 授权管理

四个子页面：

- 服务授权：以 ProxyService 为维度，管理谁能访问哪个服务（用户级 + 分组级）
- 用户授权：以 Agent 用户为维度，管理谁能访问哪个 Agent 的所有端口
- 分组授权：以分组为维度，管理分组间的访问权限
- SSH 授权：以 Agent 用户为维度，管理 SSH 访问权限（允许的 Linux 用户列表）

### 隧道管理

直接对接 Headscale API 的管理界面，展示 Headscale 内部的用户、节点、ACL 策略、SSH 策略。主要用于调试和运维。

### 审计日志

连接级审计日志，记录用户的连接事件。

### 系统配置

系统参数配置页面。

## ZTNA 变更总览

| 模块               | 当前状态                               | ZTNA 新增/变更                                            |
| ------------------ | -------------------------------------- | --------------------------------------------------------- |
| 用户管理           | 用户列表（agent/client），Deploy Token | 不变                                                      |
| 设备管理           | 设备列表（代理/桌面），IP/状态/心跳    | 增强：Agent 详情页新增能力配置 + Endpoint 功能管理        |
| 终端管理(Endpoint) | 无                                     | 新增终端管理（SSH/K8SAPI/K8SService，自动发现，支持注销） |
| 资源发现           | 无                                     | 新增 AgentK8SService/EndpointK8SService 自动发现视图      |
| 域名管理           | 无                                     | 新增域名注册表查看                                        |
| 授权管理           | 服务授权、用户授权、分组授权、SSH 授权 | 新增 K8S API 授权 + K8S Service 授权                      |
| 隧道管理           | Headscale User/Node/ACL/SSH            | 不变                                                      |
| 审计日志           | 连接审计                               | 增强为操作级审计（直连 + 跳跃）                           |
| 系统配置           | 系统参数                               | 不变                                                      |

## ZTNA 导航结构

```
├── 用户管理          /users                    （不变）
├── 设备管理          /nodes                    （不变）
├── 终端管理（新增）    /endpoints                （新增，List+Detail 模式）
├── 分组管理          /groups                   （不变）
├── 资源发现（新增）    /resources               （新增）
├── 域名管理（新增）    /domains                 （新增）
├── 授权管理（增强）
│     ├── 服务授权    /acl/services             （不变，AgentService）
│     ├── 用户授权    /acl/users                （不变）
│     ├── 分组授权    /acl/groups               （不变）
│     ├── SSH 授权    /acl/ssh                  （增强，包含 Agent SSH + Endpoint SSH）
│     ├── K8S API 授权    /acl/k8s                  （新增，包含 Agent K8SAPI + Endpoint K8SAPI）
│     └── K8S Service 授权 /acl/k8s-service      （新增，包含 Agent K8SService + Endpoint K8SService）
├── 隧道管理                                    （不变）
│     ├── User 管理   /tunnel/users
│     ├── Node 管理   /tunnel/nodes
│     ├── ACL 管理    /tunnel/acl
│     └── SSH 策略    /tunnel/ssh
├── 审计日志（增强）    /audit-logs              （增强）
└── 系统配置          /system/config             （不变）
```

## 新增页面设计

### K8SAPI 授权页面（/acl/k8s）

管理 AgentK8SAPI 的访问权限（第 3 层）。以 Agent 为维度，配置哪些用户/分组可以访问哪个 Agent 的 K8S API，以及对应的命名空间和 K8S 角色。

列表视图：

| 列         | 说明                               |
| ---------- | ---------------------------------- |
| Agent      | Agent 名称（启用了 K8SAPI 能力的） |
| 别名       | Agent 别名                         |
| 用户授权数 | 用户级授权条数                     |
| 分组授权数 | 分组级授权条数                     |
| 操作       | 管理授权                           |

详情页面（点击"管理授权"进入）：

```
Agent: beijing（kubernetes.beijing.beagle:50050）

用户级授权：
  ┌──────────┬──────────┬──────────┐
  │ 用户     │ 命名空间 │ K8S 角色 │
  │ zhangsan │ yygl     │ developer│
  │ lisi     │ *        │ admin    │
  └──────────┴──────────┴──────────┘
  [+ 添加用户授权]

分组级授权：
  ┌──────────┬──────────┬──────────┐
  │ 分组     │ 命名空间 │ K8S 角色 │
  │ 开发组   │ yygl     │ developer│
  │ 运维组   │ *        │ admin    │
  └──────────┴──────────┴──────────┘
  [+ 添加分组授权]
```

页面结构与现有 SSH 授权页面（/acl/ssh）类似：列表页 + 详情页模式。

### K8S Service 授权页面（/acl/k8s-service）

管理 AgentK8SService 的访问权限（第 3 层）。以 Agent 为维度，配置哪些用户/分组可以访问哪个 Agent 自动发现的 K8S Service，按命名空间和 Service 名称控制。

列表视图：

| 列         | 说明                                   |
| ---------- | -------------------------------------- |
| Agent      | Agent 名称（启用了 K8SService 能力的） |
| 别名       | Agent 别名                             |
| 用户授权数 | 用户级授权条数                         |
| 分组授权数 | 分组级授权条数                         |
| 操作       | 管理授权                               |

详情页面（点击"管理授权"进入）：

```
Agent: beijing

用户级授权：
  ┌──────────┬──────────┬──────────────┐
  │ 用户     │ 命名空间 │ Service 模式 │
  │ zhangsan │ yygl     │ *            │
  │ lisi     │ *        │ pg*          │
  └──────────┴──────────┴──────────────┘
  [+ 添加用户授权]

分组级授权：
  ┌──────────┬──────────┬──────────────┐
  │ 分组     │ 命名空间 │ Service 模式 │
  │ 开发组   │ yygl     │ *            │
  │ 运维组   │ *        │ *            │
  └──────────┴──────────┴──────────────┘
  [+ 添加分组授权]
```

页面结构与 K8SAPI 授权页面类似。

### 终端管理页面（/endpoints）

管理连接到 Agent 的 Endpoint（轻量 daemon）。采用 List+Detail 模式，与设备管理（/nodes）一致。

Endpoint 不在 Web 上手动创建，而是通过安装 signal_endpoint 二进制自动注册（Endpoint 反向连接 Agent → Agent 心跳上报 Server → Server 自动发现）。管理员只能查看、编辑别名、启用/禁用、注销。

Endpoint 列表（/endpoints）：

| 列        | 说明                            |
| --------- | ------------------------------- |
| 名称      | Endpoint 名称（可点击进入详情） |
| 别名      | 显示名称                        |
| 类型      | SSH / K8S API / K8S Service     |
| 所属Agent | 连接的 Agent 名称               |
| 状态      | online/offline                  |
| 启用      | 是否启用                        |
| 创建时间  | 首次发现时间                    |
| 操作      | 编辑、启用/禁用、注销           |

筛选条件：Agent 筛选 + 类型筛选 + 状态筛选 + 搜索。

Endpoint 详情（/endpoints/:type/:id）：

详情页按能力分区展示，包含：

- 基本信息卡片：名称、别名、类型、所属 Agent、状态、启用状态、创建时间
- 能力卡片（根据类型显示）：
  - SSH 类型：主机地址、端口、域名、SSH 用户列表
  - K8SAPI 类型：API Server 地址、域名
  - K8SService 类型：域名
- 操作卡片：编辑别名、启用/禁用、注销

### 设备详情页 Endpoint 功能管理

在现有设备详情页（/nodes/:id）的能力配置卡片中，为 Agent 类型设备新增 Endpoint 功能区域。

Endpoint 功能区域：

| 字段           | 说明                                                       |
| -------------- | ---------------------------------------------------------- |
| 启用 Endpoint  | 开关，开启后 Agent 启动内网 gRPC Server 接受 Endpoint 连接 |
| 监听端口       | 内网 gRPC 监听端口（默认 50052）                           |
| Endpoint Token | Server 自动生成的注册令牌，管理员复制到 Endpoint 配置文件  |
| Token 操作     | 查看/复制/重新生成                                         |

Token 管理规则：

- 开启 Endpoint 功能时自动生成 token
- 管理员可查看、复制 token（用于配置 Endpoint）
- 可重新生成 token（旧 token 立即失效，已连接的 Endpoint 下次重连需新 token）
- 关闭 Endpoint 功能时 token 清空，Agent 关闭内网 gRPC Server，所有 Endpoint 断开

### 资源发现页面（/resources）

展示所有 AgentK8SService 和 EndpointK8SService 自动发现的 K8S Service。

| 列       | 说明                                  |
| -------- | ------------------------------------- |
| 域名     | 完整域名（如 pg.yygl.beijing.beagle） |
| 来源     | AgentK8SService / EndpointK8SService  |
| Service  | K8S Service 名称                      |
| 命名空间 | K8S 命名空间                          |
| 端口     | 服务端口列表                          |
| Agent    | 所属 Agent 名称                       |
| 状态     | 在线/离线                             |

过滤条件：按 Agent、来源、命名空间、状态筛选，支持关键词搜索。

"更新"按钮：通知所有在线 Agent 立即上报 K8S Service 发现数据（POST /api/v1/resources/sync），无需等待心跳周期。具体机制见 `design_ztna_server_heartbeat.md`「立即上报机制」。

### 域名管理页面（/domains）

查看域名注册表，展示域名到 Agent/Endpoint 的映射关系。

| 列       | 说明                                                                                                        |
| -------- | ----------------------------------------------------------------------------------------------------------- |
| 域名     | 完整域名                                                                                                    |
| 类型     | AgentSSH / AgentK8SAPI / AgentK8SService / AgentService / EndpointSSH / EndpointK8SAPI / EndpointK8SService |
| 目标     | Agent 名称 + Endpoint 名称（如有）                                                                          |
| 端口     | 服务端口                                                                                                    |
| 状态     | 活跃/离线                                                                                                   |
| 注册时间 | 域名注册时间                                                                                                |

### 审计日志增强

在现有连接审计基础上，新增操作类型筛选：

| 操作类型   | 说明                                            |
| ---------- | ----------------------------------------------- |
| ssh_direct | SSH 直连 Agent 节点（第 2 层）                  |
| k8s_direct | K8SAPI 直连 Agent（第 3 层）                    |
| svc_direct | K8SService 直连 Agent（第 3 层）                |
| ssh_jump   | 通过 Agent 跳跃到 EndpointSSH（第 4 层）        |
| k8s_jump   | 通过 Agent 跳跃到 EndpointK8SAPI（第 4 层）     |
| svc_jump   | 通过 Agent 跳跃到 EndpointK8SService（第 4 层） |

新增列：Agent（经过的 Agent）、Endpoint（目标 Endpoint，跳跃时）、详情（命令摘要、API 路径等）。

## 国际化

所有新增页面和组件需要同步更新中英文翻译文件：

- `web/src/locales/zh-CN.ts` — 中文翻译
- `web/src/locales/en-US.ts` — 英文翻译

翻译 key 命名规范：

```
menu.endpoints          — 终端管理
menu.resources          — 资源发现
menu.domains            — 域名管理
acl.k8s                 — K8S API 授权
acl.k8sService          — K8S Service 授权
endpoint.type           — Endpoint 类型
endpoint.basicInfo      — 基本信息
endpoint.sshCapability  — SSH 能力
endpoint.k8sapiCapability    — K8S API 能力
endpoint.k8sserviceCapability — K8S Service 能力
```

## 实现优先级

| 阶段 | 内容                            | 依赖                        |
| ---- | ------------------------------- | --------------------------- |
| P1   | K8S API 授权页面                | AclK8sPermission API        |
| P1   | K8S Service 授权页面            | AclK8SServicePermission API |
| P1   | 终端管理页面（自动发现 + 注销） | Endpoint 模型 API           |
| P1   | 设备详情页 Endpoint 功能管理    | 能力配置 API + Endpoint API |
| P1   | 资源发现页面                    | 资源发现 API                |
| P1   | 域名管理页面                    | 域名注册表 API              |
| P2   | 审计日志增强                    | 操作审计 API                |
