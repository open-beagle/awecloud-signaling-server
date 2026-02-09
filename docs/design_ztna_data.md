# ZTNA 数据设计总览

## 概述

本文档是 ZTNA 数据设计的入口，定义设计原则和子文档职责。

## 子文档

| 文档                          | 职责                                                       |
| ----------------------------- | ---------------------------------------------------------- |
| design_ztna_data_database.md  | 数据库表设计（字段、索引、迁移），纯持久化层               |
| design_ztna_data_server.md    | Server 业务实体设计 — 页面实体如何由多数据源组装           |
| design_ztna_data_agent.md     | Agent 运行时数据 — 权限缓存、Endpoint 连接池、能力对象状态 |
| design_ztna_data_desktop.md   | Desktop 运行时数据 — VIP 映射、DNS 缓存、本地代理          |
| design_ztna_data_endpoint.md  | Endpoint 运行时数据 — 注册、会话、Service 发现             |
| design_ztna_data_headscale.md | Headscale 数据交互 — Tag/ACL 同步、节点管理                |

## 设计原则

### 原则一：业务实体驱动

数据设计不是从表出发，而是从业务实体出发。

业务实体 = 页面上用户看到的东西。比如 Web 管理界面的"Agent 列表"页面，每一行是一个 Agent 实体。这个实体不等于 User 表的一行，它是多个数据源组装出来的：

```
Agent 实体（页面上的一行）
  │
  ├── User 表 — name, alias, role, ssh_enabled, enabled
  ├── Node 表 — ip, version, hostname, last_heartbeat
  ├── Headscale — tunnel 连接状态（通过 Node.HeadscaleNodeID 关联）
  ├── AgentTsStatus 缓存 — 实时网络信息、连接时间
  └── ServiceRuntimeStatus 缓存 — 关联服务的运行状态汇总
```

每个子文档必须以业务实体为单位组织，说清楚：

- 这个实体在哪个页面出现（List / Detail）
- 由哪些数据源组成
- Server 是否需要在内存中维护组装后的实体，还是查询时实时组装

### 原则二：数据源分类

所有数据归为四类数据源：

| 数据源    | 说明                   | 示例                                |
| --------- | ---------------------- | ----------------------------------- |
| DB        | SQLite 持久化表        | User, Node, ProxyService, ACL 表    |
| Cache     | Server 内存缓存        | AgentTsStatus, ServiceRuntimeStatus |
| Headscale | Headscale gRPC 查询    | 节点在线状态、Tag、IP               |
| gRPC 上报 | Agent/Desktop 实时上报 | 心跳状态、K8S Service 发现          |

子文档中描述每个业务实体时，必须标注每个字段来自哪个数据源。

### 原则三：组装策略明确

对于每个业务实体，必须明确组装策略：

| 策略       | 说明                                | 适用场景                   |
| ---------- | ----------------------------------- | -------------------------- |
| 查询时组装 | API 请求时从多个数据源实时拼接      | 数据量小、实时性要求高     |
| 缓存组装   | 数据变更时更新内存中的组装实体      | 频繁查询、数据源变更有事件 |
| 混合       | DB 数据查询时取，实时状态从缓存补充 | 大多数列表页               |

### 原则四：子文档各司其职

- database.md — 只管表结构，不关心业务实体怎么组装
- server.md — 核心文档，定义所有业务实体的组装方式
- agent/desktop/endpoint.md — 各组件自己的运行时数据，不涉及 Server 组装
- headscale.md — Headscale 作为数据源的交互方式

## Server 业务实体清单

以下是 server.md 需要设计的所有业务实体，按 Web 页面对应：

| 业务实体                | 页面             | 数据源                                         |
| ----------------------- | ---------------- | ---------------------------------------------- |
| User.List               | 用户管理         | DB(User)                                       |
| User.Agent.Detail       | Agent 详情       | DB(User,Node,Service,Forward) + Cache + HS     |
| User.Client.Detail      | Client 详情      | DB(User,Node,Group,ACL) + Cache                |
| AgentService.List       | 服务列表         | DB(ProxyService) + Cache(ServiceRuntimeStatus) |
| AgentService.Detail     | 服务详情         | DB(ProxyService,ACL) + Cache                   |
| K8SService.List（新增） | K8S 服务列表     | Cache(Agent 上报) + DB(DomainRegistry)         |
| Node.List               | 设备管理         | DB(Node,User) + Cache(AgentTsStatus)           |
| Group.List              | 分组列表         | DB(Group,GroupMember)                          |
| Group.Detail            | 分组详情         | DB(Group,GroupMember,User) + Cache             |
| Endpoint.List（新增）   | Endpoint 列表    | DB(Endpoint\*,User) + Cache(EndpointStatus)    |
| Endpoint.Detail（新增） | Endpoint 详情    | DB(Endpoint\*,ACL) + Cache                     |
| ACL.ServiceList         | 服务授权列表     | DB(AclServicePermission,ProxyService,User)     |
| ACL.UserList            | 用户授权列表     | DB(AclUserPermission,User)                     |
| ACL.GroupList           | 分组授权列表     | DB(AclGroupPermission,Group)                   |
| ACL.SSHList             | SSH 授权列表     | DB(AclSSHPermission,User)                      |
| ACL.K8SList（新增）     | K8S 授权列表     | DB(AclK8sPermission,User)                      |
| ACL.K8SSvcList（新增）  | K8S 服务授权列表 | DB(AclK8SServicePermission,User)               |
| Resource.List（新增）   | 资源发现         | Cache(K8SService+EndpointK8SService) + DB      |
| Domain.List（新增）     | 域名列表         | DB(DomainRegistry)                             |
| DeployToken.List        | 部署 Token 列表  | DB(DeployToken,User)                           |
| AuditLog.List           | 审计日志列表     | DB(AuditLog,OperationAuditLog)                 |

server.md 的核心任务就是把上面每个实体的组装方式设计清楚。
