# ZTNA 授权管理设计

## 概述

ZTNA 授权管理分为两个控制平面：Headscale ACL（网络层）和 Agent 应用层鉴权。本文档梳理每一种授权的业务逻辑、与 Headscale 的同步关系、控制粒度。

## 两个控制平面

### 控制平面 A：Headscale ACL（网络层）

执行位置在 Headscale 控制平面。Tailscale 节点建立连接时检查，不通过则 TCP 握手都不会到达目标节点。

Server 把数据库授权规则翻译成 Headscale ACL Policy，通过 gRPC 推送给 Headscale（SetPolicy）。授权变更时立即同步，另外每 5 分钟全量同步一次。

控制粒度：src（tag:client-xxx / tag:group-xxx）→ dst（tag:agent-xxx:端口 / tag:agent-xxx:\*）。

能控制的维度：谁（src tag）→ 谁的哪个端口（dst tag:port）。不能控制 SSH 用户名、K8S 命名空间、K8S 角色等应用层信息。

### 控制平面 B：应用层鉴权

执行位置在 Agent 进程内或 Headscale SSH Policy。连接建立后处理具体请求时检查。

两种实现方式：

- Headscale SSH Policy — SSH 授权通过 Headscale SSH 规则实现，Agent 不需要自己鉴权，Tailscale SSH 模块自动执行
- Agent 本地鉴权 — K8SAPI/K8SService/跳跃等新能力，Agent 从 tsnet 提取身份后查询本地缓存的权限数据（心跳同步）

## Tag 体系

每个 Headscale 节点上的 Tag 决定了它的身份：

```
Agent 节点：
  tag:agent-beijing                    ← 身份 Tag
  tag:group-北京机房                   ← 分组 Tag（可多个）

Desktop 节点：
  tag:client-zhangsan                  ← 身份 Tag
  tag:group-开发组                     ← 分组 Tag（可多个）
```

Tag 来源：身份 Tag 格式为 tag:{role}-{user.name}，分组 Tag 格式为 tag:group-{group.name}。通过 SyncAllNodeTags 同步到 Headscale 节点上。

## 与 Headscale 同步的完整流程

```
管理员在 Web 界面操作（如添加服务授权）
  │
  ├── 1. 写入数据库
  │
  ├── 2. 触发 ACL 同步
  │       ├── 从数据库读取所有授权规则
  │       ├── 生成 ACL Policy JSON（acls + ssh 规则）
  │       └── 通过 gRPC 推送给 Headscale（SetPolicy）
  │
  └── 3. Headscale 更新 ACL 策略，立即生效

另外每 5 分钟全量同步一次：
  SyncAllNodeTags → 同步节点 Tag
  SyncACL → 全量重新生成并推送 ACL Policy
```

## Agent 能力对象命名

```
Agent 四个能力对象：
  AgentSSH            Agent 本机 SSH（现有）
  AgentK8SAPI         Agent 本机 K8S API 代理（新增）
  AgentK8SService     Agent 本机 K8S Service 自动发现（新增）
  AgentService        Agent 手动端口映射（现有 ProxyService 改名）

Endpoint 三个跳跃对象：
  EndpointSSH         内网 SSH 跳跃
  EndpointK8SAPI      内网 K8S API 跳跃
  EndpointK8SService  内网 K8S Service 跳跃
```

## 现有授权（4 种，同步 Headscale）

### 1. 服务授权（/acl/services）

业务含义：控制谁能访问哪个 AgentService（端口级）。

对象：AclServiceUserPermission、AclServiceGroupPermission。

以 AgentService（原 ProxyService）为维度，管理谁能访问哪个手动配置的端口映射服务。

翻译成 Headscale ACL 示例：

```
数据库：service_id=pg-yygl (source_addr=100.64.0.1:5432), user_id=zhangsan

Headscale ACL:
  { "action": "accept", "src": ["tag:client-zhangsan"], "dst": ["tag:agent-beijing:5432"] }
```

适用场景：Agent 部署在普通服务器上，手动配置端口映射（非 K8S 场景）。

### 2. 用户授权（/acl/users）

业务含义：控制谁能访问哪个 Agent 的所有端口。

对象：AclUserUserPermission、AclUserGroupPermission。

以 Agent 用户为维度（仅 role=agent），管理谁能访问该 Agent 的任意端口。

翻译成 Headscale ACL 示例：

```
数据库：target_user_id=beijing (agent), granted_user_id=zhangsan (client)

Headscale ACL:
  { "action": "accept", "src": ["tag:client-zhangsan"], "dst": ["tag:agent-beijing:*"] }
```

这是比服务授权更粗粒度的控制。如果已有用户授权（:\*），再配服务授权（:5432）是多余的。

### 3. 分组授权（/acl/groups）

业务含义：控制谁能访问哪个分组标记的所有节点的所有端口。

对象：AclGroupUserPermission、AclGroupGroupPermission。

以分组为维度，管理谁能访问该分组下所有节点。

翻译成 Headscale ACL 示例：

```
数据库：target_group_id=北京机房, user_id=zhangsan

Headscale ACL:
  { "action": "accept", "src": ["tag:client-zhangsan"], "dst": ["tag:group-北京机房:*"] }
```

这是最粗粒度的网络层控制。

### 4. SSH 授权（/acl/ssh）

业务含义：控制谁能 SSH 到哪个 Agent，用哪些 Linux 用户。

对象：AclSSHUserPermission、AclSSHGroupPermission。

以 Agent 用户为维度（仅 ssh_enabled=true），管理 SSH 访问权限和允许的 Linux 用户名。

翻译成 Headscale SSH Policy 示例：

```
数据库：target_user_id=beijing, user_id=zhangsan, ssh_users=["root","deploy"]

Headscale SSH:
  { "action": "accept", "src": ["tag:client-zhangsan"], "dst": ["tag:agent-beijing"], "users": ["root","deploy"] }
```

SSH 授权和网络层授权是独立维度。有 SSH 授权但无用户授权时，SSH 仍可工作（Tailscale SSH 有独立通道）。

### 现有授权的层级关系

```
粗 ──────────────────────────────────────────────────── 细

分组授权          用户授权          服务授权
group:*:*         agent:*           agent:port

三者是包含关系：分组授权 ⊃ 用户授权 ⊃ 服务授权
```

## ZTNA 新增授权（3 种，不同步 Headscale）

### 5. K8SAPI 授权（/acl/k8s）— 新增

业务含义：控制谁能通过 Agent 访问 K8S API，以及命名空间和角色。

对象：AclK8sUserPermission、AclK8sGroupPermission。

以 Agent 用户为维度（k8s.enabled=true），管理 K8S API 访问权限。

```
AclK8sUserPermission:
  agent_user_id   → 哪个 Agent（哪个集群）
  user_id         → 被授权用户
  namespaces      → 允许的命名空间列表（"*" = 全部）
  k8s_role        → Impersonation 使用的 K8S 角色
```

与 Headscale 的关系：不翻译成 Headscale ACL。这是应用层控制，执行在 Agent 侧。前提条件是 Desktop 必须先能连到 Agent（第 1 层通过）。

权限数据通过心跳同步到 Agent 本地缓存。

### 6. K8SService 授权（/acl/k8s-service）— 新增

业务含义：控制谁能通过 Agent 访问自动发现的 K8S Service，按命名空间和 Service 名称控制。

对象：AclK8SServiceUserPermission、AclK8SServiceGroupPermission。

以 Agent 用户为维度（svc.enabled=true），管理 K8S Service 访问权限。

```
AclK8SServiceUserPermission:
  agent_user_id   → 哪个 Agent
  user_id         → 被授权用户
  namespaces      → 允许的命名空间列表（"*" = 全部）
  service_pattern → 允许的 Service 名称模式（"*" = 全部，"pg*" = pg 开头）
```

与 Headscale 的关系：不翻译成 Headscale ACL。

为什么不能复用服务授权（AclServicePermission）：AgentK8SService 是自动发现模式，同一个 Agent 上可能有多个 Service 使用相同端口（如多个命名空间各有一个 PostgreSQL:5432）。Headscale ACL 只认 tag + 端口，无法区分 namespace/service name。因此 K8SService 的权限控制必须在应用层，按 namespace + service name 级别控制。

### 7. 跳跃授权（/acl/jump）— 新增

业务含义：控制谁能通过 Agent 跳跃到内网 Endpoint。

三种跳跃类型，结构对称：

```
SSH 跳跃（AclSSHJumpPermission）：
  endpoint_ssh_id → 哪个 EndpointSSH
  user_id/group_id → 被授权用户/分组
  ssh_users → 允许的 Linux 用户列表

K8SAPI 跳跃（AclK8SAPIJumpPermission）：
  endpoint_k8sapi_id → 哪个 EndpointK8SAPI
  user_id/group_id → 被授权用户/分组
  namespaces → 允许的命名空间
  k8s_role → K8S 角色

K8SService 跳跃（AclK8SServiceJumpPermission）：
  endpoint_k8sservice_id → 哪个 EndpointK8SService
  user_id/group_id → 被授权用户/分组
  service_pattern → 允许的 Service 模式
```

与 Headscale 的关系：完全不涉及 Headscale。Endpoint 不在 Tailscale 网络中，没有 Tag。

## AgentService 与 AgentK8SService 的区别

```
AgentService（原 ProxyService）：
  手动配置，一个端口一个服务
  走 tsnet 独立端口
  Headscale ACL 端口级控制
  适用于非 K8S 场景（普通服务器上的端口映射）

AgentK8SService：
  K8S Informer 自动发现
  走 tsnet gRPC 代理（SVCProxy RPC）
  Agent 应用层鉴权（namespace + service name）
  适用于 K8S 场景

两者共存，不冲突：
  AgentService 走 tsnet 独立端口（现有逻辑不变）
  AgentK8SService 走 tsnet gRPC 代理（新逻辑）
```

为什么 AgentK8SService 不走独立端口：

```
同一个 Agent 上可能发现多个同端口 Service：
  pg.yygl:5432    → ClusterIP 10.96.23.45:5432
  pg.devops:5432  → ClusterIP 10.96.50.12:5432

tsnet 上不能用同一个端口区分不同 Service（TCP 没有域名信息）。
所以 AgentK8SService 走 gRPC 代理，通过 RPC 参数传递目标 Service 信息。

Desktop 侧用 VIP 隔离端口冲突：
  pg.yygl.beijing.k8s:5432   → VIP 127.1.0.1:5432 → gRPC SVCProxy(pg, yygl)
  pg.devops.beijing.k8s:5432 → VIP 127.1.0.2:5432 → gRPC SVCProxy(pg, devops)

用户始终用原始端口 5432，体验不变。
```

## 授权管理总览

```
┌──────────────────┬──────────────┬──────────────┬──────────────────────────┬──────────────────┐
│ 授权类型         │ 控制平面     │ 同步 Headscale│ 控制粒度                │ 适用场景         │
├──────────────────┼──────────────┼──────────────┼──────────────────────────┼──────────────────┤
│ 服务授权         │ Headscale ACL│ 是           │ Agent 指定端口           │ AgentService     │
│ 用户授权         │ Headscale ACL│ 是           │ Agent 所有端口           │ 通用             │
│ 分组授权         │ Headscale ACL│ 是           │ 分组所有节点所有端口     │ 通用             │
│ SSH 授权         │ Headscale SSH│ 是           │ Agent SSH + Linux 用户   │ AgentSSH         │
│ K8SAPI 授权      │ Agent 本地   │ 否           │ Agent K8S + NS + 角色    │ AgentK8SAPI      │
│ K8SService 授权  │ Agent 本地   │ 否           │ Agent SVC + NS + Service │ AgentK8SService  │
│ 跳跃授权         │ Agent 本地   │ 否           │ Endpoint + 应用层参数    │ Endpoint         │
└──────────────────┴──────────────┴──────────────┴──────────────────────────┴──────────────────┘
```

## 四层权限控制体系（更新）

```
第 1 层：网络可达性（Headscale ACL）
  服务授权 → AgentService 端口级
  用户授权 → Agent 全端口
  分组授权 → 分组全端口

第 2 层：Agent 本机 SSH（Headscale SSH Policy）
  SSH 授权 → AgentSSH

第 3 层：Agent 本机应用层（Agent 本地鉴权）
  K8SAPI 授权 → AgentK8SAPI
  K8SService 授权 → AgentK8SService

第 4 层：Endpoint 跳跃（Agent 本地鉴权）
  SSH 跳跃授权 → EndpointSSH
  K8SAPI 跳跃授权 → EndpointK8SAPI
  K8SService 跳跃授权 → EndpointK8SService
```

第 1 层是所有后续层的前提。Desktop 必须先能连到 Agent（第 1 层通过），后续层的鉴权才会被触发。

## 实现优先级

| 阶段 | 内容                                 | 依赖                 |
| ---- | ------------------------------------ | -------------------- |
| P0   | 现有 4 种授权不变                    | 已实现               |
| P1   | K8SAPI 授权模型和 API                | AgentK8SAPI 实现     |
| P1   | K8SService 授权模型和 API            | AgentK8SService 实现 |
| P2   | 跳跃授权模型和 API（SSH/K8S/SVC）    | Endpoint 体系        |
| P2   | Web 授权管理页面（K8S + K8SService） | 授权 API             |
| P2   | Web 跳跃授权页面                     | 跳跃授权 API         |
