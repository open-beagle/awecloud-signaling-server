# ZTNA Server 端设计

## 概述

Server 在 ZTNA 架构中承担控制面角色：身份管理、权限配置、资源发现、域名注册。不参与数据面转发。

本文档描述 Server 从当前架构演进到 ZTNA 所需的变更。

## 与现有架构的关系

| 模块     | 当前状态                  | ZTNA 新增/变更                                  |
| -------- | ------------------------- | ----------------------------------------------- |
| 身份认证 | Logto 登录 + 设备 Token   | 不变，tsnet 连接携带身份，无需 Identity Token   |
| 用户管理 | User 模型（agent/client） | 不变                                            |
| 服务管理 | ProxyService（手动配置）  | 改名 AgentService，AgentK8SService 自动发现新增 |
| 权限管理 | SSH ACL + 服务公开/私有   | 新增 K8SAPI/K8SService ACL + Endpoint 跳跃 ACL  |
| Endpoint | 无                        | 新增 Endpoint 数据模型和管理 API                |
| 域名管理 | 无                        | 域名注册表，.k8s 后缀映射                       |
| 审计日志 | 连接审计                  | 操作级审计（SSH/K8SAPI/K8SService 直连和跳跃）  |

## 身份传递

不需要签发 Identity Token。Tailscale 连接本身携带身份。

Agent 从 tsnet 连接中提取对端节点名和用户名，即可确定访问者身份。Server 只需维护 User 和权限数据，通过心跳同步给 Agent。

## 资源发现 API

### Agent 上报

Agent 通过心跳流上报发现的资源：

```
心跳请求扩展：

现有字段：
  agent_id, tunnel_ip, tunnel_connected, service_status, ...

新增字段（AgentK8SService 上报）：
  discovered_services:
    - name: "postgresql"
      namespace: "yygl"
      ports: [5432]
      cluster_ip: "10.96.23.45"
      labels: {"tailscale": "true"}
      status: "online"

新增字段（Endpoint 上报，Agent 转发）：
  endpoints:
    - name: "web-server-1"
      type: "ssh"
      status: "online"
      host: "192.168.1.100"
    - name: "beijing-prod"
      type: "k8sapi"
      status: "online"
      api_server: "192.168.1.10:6443"
    - name: "remote-cluster"
      type: "k8sservice"
      status: "online"
      services: [...]
```

### 资源查询 API

Desktop 查询当前用户可访问的资源列表：

```
GET /api/v1/client/resources
参数: agent_id=1&type=k8s_service
响应: {
  "resources": [
    {
      "id": "...",
      "domain": "pg.yygl.beijing.k8s",
      "type": "agent_k8s_service",
      "ports": [5432],
      "agent_id": 1,
      "status": "online"
    }
  ]
}
```

## 域名注册表

### 域名格式

```
Agent 本机：
  <agent-name>.k8s:22                          → AgentSSH
  api.<agent-name>.k8s:6443                    → AgentK8SAPI
  <service>.<namespace>.<agent-name>.k8s       → AgentK8SService

Endpoint 跳跃：
  <endpoint>.<agent-name>.k8s:22               → EndpointSSH
  api.<endpoint>.<agent-name>.k8s:6443         → EndpointK8SAPI
  <service>.<namespace>.<endpoint>.<agent-name>.k8s → EndpointK8SService

### 域名注册流程

```

Agent 发现资源
│
├── AgentK8SService：监听 K8S Service 变更，自动注册域名
│ pg.yygl.beijing.k8s → agent_id:1, target:10.96.23.45:5432
│
├── AgentSSH：注册 Agent 主机域名
│ beijing.k8s → agent_id:1, target:100.64.x.x:22
│
├── AgentK8SAPI：注册 K8S API 域名
│ api.beijing.k8s → agent_id:1, target:localhost:6443
│
└── Endpoint：Agent 转发 Endpoint 注册信息
web-server-1.beijing.k8s → agent_id:1, endpoint:web-server-1

```

### 域名查询 API

Desktop DNS 劫持时查询域名对应的路由信息：

```

GET /api/v1/client/dns/resolve
参数: domain=pg.yygl.beijing.k8s
响应: {
"domain": "pg.yygl.beijing.k8s",
"agent_ip": "100.64.0.1",
"target_port": 5432,
"agent_id": 1,
"type": "agent_k8s_service"
}

```

## 权限模型变更

### 现有 ACL（不变）

| 模型                  | 层级    | 说明                 |
| --------------------- | ------- | -------------------- |
| AclServicePermission  | 第 1 层 | 端口级网络可达       |
| AclUserPermission     | 第 1 层 | Agent 级网络可达     |
| AclGroupPermission    | 第 1 层 | 分组级网络可达       |
| AclSSHUserPermission  | 第 2 层 | SSH 直连授权（用户） |
| AclSSHGroupPermission | 第 2 层 | SSH 直连授权（分组） |

### 新增 ACL

| 模型                             | 层级    | 说明                                |
| -------------------------------- | ------- | ----------------------------------- |
| AclK8sUserPermission             | 第 3 层 | AgentK8SAPI 权限（用户级）          |
| AclK8sGroupPermission            | 第 3 层 | AgentK8SAPI 权限（分组级）          |
| AclK8SServiceUserPermission      | 第 3 层 | AgentK8SService 权限（用户级）      |
| AclK8SServiceGroupPermission     | 第 3 层 | AgentK8SService 权限（分组级）      |
| AclSSHJumpUserPermission         | 第 4 层 | EndpointSSH 跳跃授权（用户）        |
| AclSSHJumpGroupPermission        | 第 4 层 | EndpointSSH 跳跃授权（分组）        |
| AclK8SAPIJumpUserPermission      | 第 4 层 | EndpointK8SAPI 跳跃授权（用户）     |
| AclK8SAPIJumpGroupPermission     | 第 4 层 | EndpointK8SAPI 跳跃授权（分组）     |
| AclK8SServiceJumpUserPermission  | 第 4 层 | EndpointK8SService 跳跃授权（用户） |
| AclK8SServiceJumpGroupPermission | 第 4 层 | EndpointK8SService 跳跃授权（分组） |

### ACL 字段说明

第 3 层 — AgentK8SAPI 权限：

```

AclK8sUserPermission:
agent_user_id → 哪个 Agent（哪个集群）
user_id → 被授权用户
namespaces → 允许的命名空间列表（"\*" = 全部）
k8s_role → Impersonation 使用的 K8S 角色

AclK8sGroupPermission:
agent_user_id → 哪个 Agent
group_id → 被授权分组
namespaces → 允许的命名空间列表
k8s_role → K8S 角色

```

第 3 层 — AgentK8SService 权限：

```

AclK8SServiceUserPermission:
agent*user_id → 哪个 Agent
user_id → 被授权用户
namespaces → 允许的命名空间列表（"*" = 全部）
service*pattern → 允许的 Service 名称模式（"*" = 全部，"pg\*" = pg 开头）

AclK8SServiceGroupPermission:
agent_user_id → 哪个 Agent
group_id → 被授权分组
namespaces → 允许的命名空间列表
service_pattern → 允许的 Service 名称模式

```

第 4 层 — Endpoint 跳跃权限（以 SSH 为例，K8S 和 SVC 结构类似）：

```

AclSSHJumpUserPermission:
endpoint_ssh_id → 哪个 EndpointSSH
user_id → 被授权用户
ssh_users → 允许的 Linux 用户列表

AclSSHJumpGroupPermission:
endpoint_ssh_id → 哪个 EndpointSSH
group_id → 被授权分组
ssh_users → 允许的 Linux 用户列表

AclK8SAPIJumpUserPermission:
endpoint_k8sapi_id → 哪个 EndpointK8SAPI
user_id → 被授权用户
namespaces → 允许的命名空间列表
k8s_role → Impersonation 角色

AclK8SServiceJumpUserPermission:
endpoint*k8sservice_id → 哪个 EndpointK8SService
user_id → 被授权用户
service_pattern → 允许访问的 Service 模式（如 "*.yygl" 或 "\_"）

```

## 心跳同步扩展

Agent 通过 gRPC 心跳从 Server 获取权限数据：

```

心跳响应新增字段：
k8s_permissions: → 第 3 层，AgentK8SAPI 权限
k8s_service_permissions: → 第 3 层，AgentK8SService 权限
ssh_jump_permissions: → 第 4 层，EndpointSSH 跳跃授权
k8sapi_jump_permissions: → 第 4 层，EndpointK8SAPI 跳跃授权
k8sservice_jump_permissions: → 第 4 层，EndpointK8SService 跳跃授权

Agent 本地缓存，随心跳刷新（30 秒一次）。

```

## 数据模型变更

### 新增 Endpoint 表

```

EndpointSSH:
id string UUID 主键
user_id uint64 所属 Agent 的 User ID
name string 名称（如 "web-server-1"）
alias string 别名（显示名称）
host string 内网地址（Endpoint 自己上报）
port int SSH 端口（默认 22）
status string online/offline
enabled bool 是否启用
created_at time
updated_at time

EndpointK8SAPI:
id string UUID 主键
user_id uint64 所属 Agent 的 User ID
name string 集群名称（如 "beijing-prod"）
alias string 别名（显示名称）
api_server string K8S API Server 地址
kubeconfig_path string kubeconfig 文件路径
status string online/offline
enabled bool 是否启用
created_at time
updated_at time

EndpointK8SService:
id string UUID 主键
user_id uint64 所属 Agent 的 User ID
name string 集群名称（如 "remote-cluster"）
alias string 别名（显示名称）
status string online/offline
enabled bool 是否启用
created_at time
updated_at time

```

### 新增 ACL 表

| 表名                                 | 说明                                |
| ------------------------------------ | ----------------------------------- |
| acl_k8s_user_permission              | AgentK8SAPI 权限（用户级）          |
| acl_k8s_group_permission             | AgentK8SAPI 权限（分组级）          |
| acl_k8s_service_user_permission      | AgentK8SService 权限（用户级）      |
| acl_k8s_service_group_permission     | AgentK8SService 权限（分组级）      |
| acl_ssh_jump_user_permission         | EndpointSSH 跳跃授权（用户）        |
| acl_ssh_jump_group_permission        | EndpointSSH 跳跃授权（分组）        |
| acl_k8sapi_jump_user_permission      | EndpointK8SAPI 跳跃授权（用户）     |
| acl_k8sapi_jump_group_permission     | EndpointK8SAPI 跳跃授权（分组）     |
| acl_k8sservice_jump_user_permission  | EndpointK8SService 跳跃授权（用户） |
| acl_k8sservice_jump_group_permission | EndpointK8SService 跳跃授权（分组） |

### 其他新增表

| 表名                | 说明                              |
| ------------------- | --------------------------------- |
| domain_registry     | 域名注册表（域名 → Agent + 目标） |
| operation_audit_log | 操作级审计日志                    |

### ProxyService 迁移

ProxyService（手动配置端口映射）改名为 AgentService，保留用于非 K8S 场景。AgentK8SService（自动发现）是新增能力，两者共存不冲突。

## 审计日志增强

所有四层操作都需要审计：

```

审计记录结构：
时间、用户、来源 IP、Agent、操作类型、目标、结果、详情

操作类型：
ssh_direct → SSH 直连 Agent 节点（第 2 层）
k8s_direct → K8SAPI 直连 Agent（第 3 层）
svc_direct → K8SService 直连 Agent（第 3 层）
ssh_jump → 通过 Agent 跳跃到 EndpointSSH（第 4 层）
k8s_jump → 通过 Agent 跳跃到 EndpointK8SAPI（第 4 层）
svc_jump → 通过 Agent 跳跃到 EndpointK8SService（第 4 层）

```

## 实现优先级

| 阶段 | 内容                               | 依赖           |
| ---- | ---------------------------------- | -------------- |
| P0   | 域名注册表 API                     | 无             |
| P0   | 资源发现 API（上报 + 查询）        | Agent 上报实现 |
| P1   | AclK8sPermission 模型和 API        | 无             |
| P1   | AclK8SServicePermission 模型和 API | 无             |
| P1   | Endpoint 数据模型和管理 API        | 无             |
| P1   | Endpoint 跳跃 ACL 模型             | Endpoint 模型  |
| P2   | 操作级审计日志                     | Agent 审计上报 |
| P3   | Vaultwarden 集成（远期）           | 独立部署       |
```
