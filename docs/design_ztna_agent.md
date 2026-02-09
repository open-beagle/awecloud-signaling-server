# ZTNA Agent 能力对象设计

## 概述

Agent 部署在内网环境，以 agent 角色加入 Tailscale 网络。Agent 不区分类型（没有 Host Agent / K8s Agent 的分类），通过配置和挂载的能力对象决定它能做什么。

任何 Agent 都可以挂载任意能力，只要网络可达。部署在物理机上的 Agent 可以同时启用 SSH + K8S + SVC；部署在 K8S Pod 里的 Agent 也可以只启用 SVC。

## 四种本机能力

Agent 本机能力是配置级的，不是独立对象。通过 agent.toml 配置启用。

```
          能力              说明                              控制方式
  AgentSSH          Agent 本机 SSH（已有）                User.SSHEnabled + AclSSHPermission
  AgentK8SAPI       Agent 本机 K8S API 代理（新增）       agent.toml [k8s] + AclK8sPermission
  AgentK8SService   Agent 本机 K8S SVC 代理（新增）       agent.toml [svc] + AclK8SServicePermission
  AgentService      Agent 手动端口映射（现有 ProxyService 改名） agent.toml [service] + AclServicePermission
```

## AgentSSH — Agent 本机 SSH

这不是新对象，是现有 Tailscale SSH 能力的描述。

```
已有实现：
  User.SSHEnabled = true  → Agent 启用 SSH
  AclSSHUserPermission    → 谁能 SSH 到这个 Agent，用哪些 Linux 用户
  AclSSHGroupPermission   → 分组级 SSH 授权

  Desktop → tsnet SSH → Agent → Tailscale SSH 模块 → shell

不需要额外改动。
```

## AgentK8SAPI — Agent 本机 K8S API 代理

Agent 部署在 K8S 主节点时，直接访问本机 K8S API Server，做 Impersonation 代理。

### 配置

```
agent.toml:
  [k8s]
  enabled = true
  kubeconfig = "/etc/kubernetes/admin.conf"
  api_server = "https://localhost:6443"
```

### 工作流程

```
Agent 启动时：
  如果 k8s.enabled = true
    → 在 tsnet 上监听 K8S API 代理端口
    → 收到请求时从 tsnet 提取身份
    → 查询 AclK8sPermission（心跳同步的缓存）
    → Impersonation 转发到本机 K8S API Server
```

### Impersonation 机制

```
Desktop kubectl 请求到达 Agent：

  1. Agent 从 tsnet 连接提取身份 → zhangsan
  2. 查询 AclK8sPermission → namespace: yygl, role: developer
  3. 构造 Impersonation 请求头
       Impersonate-User: zhangsan
       Impersonate-Group: developer
  4. 使用主节点 kubeconfig 转发到 K8S API Server
       （localhost:6443，管理员权限执行 Impersonation）
  5. 返回结果给 Desktop
```

### 权限控制

```
AclK8sUserPermission:
  agent_user_id   → 哪个 Agent（哪个集群）
  user_id         → 被授权用户
  namespaces      → 允许的命名空间列表（"*" = 全部）
  k8s_role        → Impersonation 使用的 K8S 角色

AclK8sGroupPermission:
  agent_user_id   → 哪个 Agent
  group_id        → 被授权分组
  namespaces      → 允许的命名空间列表
  k8s_role        → K8S 角色
```

### 域名

```
api.<agent-name>.k8s:6443
api.beijing.k8s:6443
```

## AgentK8SService — Agent 本机 K8S SVC 代理

Agent 部署在 K8S 集群中时，通过 K8S API 监听 Service 资源变更，自动发现带有特定标签的 Service，并通过 tsnet gRPC 代理暴露。

AgentK8SService 和 AgentService（原 ProxyService）共存不冲突。AgentService 走 tsnet 独立端口（手动配置），AgentK8SService 走 tsnet gRPC 代理（自动发现）。

### 配置

```
agent.toml:
  [svc]
  enabled = true
  label_selector = "tailscale=true"
  namespaces = []                     # 空 = 所有命名空间
```

### Service 自动发现

```
Agent 启动时：
  如果 svc.enabled = true
    → 启动 K8S Service Informer（Watch 带标签的 Service）
    → 发现 Service 后注册到 gRPC 代理
    → 收到 gRPC SVCProxy 请求时从 tsnet 提取身份
    → 查询 AclK8SServicePermission（心跳同步的缓存）
    → 透明转发到 K8S ClusterIP
```

### 标签约定

K8S 管理员通过给 Service 添加标签来控制暴露：

| 标签            | 值     | 说明                   |
| --------------- | ------ | ---------------------- |
| tailscale       | "true" | 标记为可通过隧道访问   |
| tailscale/alias | 自定义 | 自定义域名前缀（可选） |

### 发现流程

```
K8S Service 变更事件
  ├── Add: postgresql.yygl (5432, tailscale=true)
  │     → tsnet 监听 :5432
  │     → 上报 Server（心跳）
  │     → 域名注册: pg.yygl.beijing.k8s:5432
  │
  ├── Update: 标签变更
  │     → 更新监听和上报
  │
  └── Delete: postgresql.yygl
        → 关闭监听
        → 注销域名
```

### 域名生成规则

```
默认规则：
  <service_name>.<namespace>.<agent-name>.k8s

使用 alias：
  <tailscale/alias>.<namespace>.<agent-name>.k8s

示例：
  postgresql.yygl.beijing.k8s:5432     （默认）
  pg.yygl.beijing.k8s:5432             （alias=pg）
```

### 端口冲突处理

同一 Agent 上可能有多个 Service 使用相同端口：

```
冲突场景：
  postgresql.yygl:5432
  postgresql.prod:5432

解决方案：
  AgentK8SService 走 gRPC 代理（SVCProxy RPC），不走独立端口
  通过 RPC 参数传递目标 Service 信息（namespace + service name）
  Desktop 侧用 VIP 隔离端口冲突：
    pg.yygl.beijing.k8s:5432   → VIP 127.1.0.1:5432 → gRPC SVCProxy(pg, yygl)
    pg.prod.beijing.k8s:5432   → VIP 127.1.0.2:5432 → gRPC SVCProxy(pg, prod)
```

### 部署要求（K8S Pod 场景）

```
Agent 部署在 K8S Pod 中时需要：

  ServiceAccount:
    绑定 ClusterRole（service list/watch 权限）

  不需要：
    NET_ADMIN 权限（tsnet 用户态）
    HostNetwork
    特权容器
```

## AgentService — Agent 手动端口映射

AgentService 是现有 ProxyService 的改名，保留用于非 K8S 场景。管理员手动配置端口映射，Agent 在 tsnet 上监听对应端口，透明转发到目标地址。

与 AgentK8SService 的区别：AgentService 是手动配置、走 tsnet 独立端口、Headscale ACL 端口级控制；AgentK8SService 是自动发现、走 gRPC 代理、应用层鉴权。

### 配置

```
agent.toml（现有配置不变）：
  [[service]]
  name = "postgresql"
  local_addr = "192.168.1.100:5432"
  remote_port = 5432
```

### 权限控制

```
AclServicePermission（现有，不变）：
  service_id → 哪个 AgentService
  user_id/group_id → 被授权用户/分组

翻译成 Headscale ACL：
  { "action": "accept", "src": ["tag:client-xxx"], "dst": ["tag:agent-xxx:5432"] }
```

### 域名

```
AgentService 不参与域名体系（手动配置，用户直接用 Tailscale IP + 端口访问）。
未来可选：支持手动绑定域名。
```

## Agent gRPC Server

Agent 需要两个 gRPC 接口，用于对接 Desktop 和 Endpoint：

```
Agent 进程
  │
  ├── tsnet gRPC Server（面向 Desktop）
  │     监听在 Tailscale 网络上
  │     身份来自 tsnet 连接
  │     │
  │     ├── SSHJump RPC     → 桥接到 EndpointSSH
  │     ├── K8sAPIProxy RPC → 桥接到 EndpointK8SAPI
  │     └── SVCProxy RPC    → 桥接到 EndpointK8SService
  │
  └── 内网 gRPC Server（面向 Endpoint）
        监听在内网地址上（如 0.0.0.0:50052）
        Endpoint 主动连接并注册
        │
        ├── RegisterEndpoint RPC  → Endpoint 注册自己
        ├── Heartbeat RPC         → Endpoint 心跳保活
        └── 反向指令流             → Agent 下发 OpenShell / K8sAPIProxy / SVCProxy 等
```

### Desktop 发起跳跃的完整流程

```
Desktop 用户执行: ssh deploy@web-server-1.beijing.k8s

  1. DNS 劫持 → VIP → tsnet 隧道 → Agent

  2. Agent tsnet gRPC Server 收到 SSHJump 请求
       目标: web-server-1
       用户: deploy

  3. Agent 从 tsnet 连接提取身份 → zhangsan

  4. Agent 查找 EndpointSSH "web-server-1"
       → 找到，状态 online（有活跃 gRPC 连接）

  5. Agent 查询 AclSSHJumpPermission
       → zhangsan 对 web-server-1 的权限
       → 允许，ssh_users: ["root", "deploy"]
       → deploy ∈ 允许列表 → 放行

  6. Agent 通过内网 gRPC 连接告诉 EndpointSSH
       → OpenShell(user: "deploy")
       → EndpointSSH 启动 shell

  7. 双向桥接
       Desktop ←tsnet gRPC stream→ Agent ←内网 gRPC stream→ EndpointSSH

  8. 审计记录
       zhangsan → agent-beijing → web-server-1 → deploy → 成功
```

## 身份提取

Agent 从 tsnet 连接中提取对端身份，不需要 Identity Token：

```
连接进来
  │
  ├── 从 tsnet 连接中提取对端信息
  │     对端 IP: 100.64.1.1
  │     对端节点名: desktop-zhangsan-macbook
  │     对端用户: client-zhangsan
  │
  ├── 从用户名中提取身份
  │     client-zhangsan → User: zhangsan, Role: client
  │
  └── 根据身份查询权限（心跳同步的本地缓存）
```

## 权限数据来源

Agent 通过 gRPC 心跳从 Server 获取权限数据：

```
心跳响应新增字段：
  k8s_permissions:          → 第 3 层，AgentK8SAPI 权限
  k8s_service_permissions:  → 第 3 层，AgentK8SService 权限
  ssh_endpoint_permissions: → Endpoint SSH 授权
  k8sapi_endpoint_permissions: → Endpoint K8SAPI 授权
  k8sservice_endpoint_permissions: → Endpoint K8SService 授权

Agent 本地缓存，随心跳刷新（30 秒一次）。
```

## 对象关系总览

```
User (agent-beijing)
  │
  ├── AgentSSH（已有能力）— 本机 SSH
  │     通过 User.SSHEnabled + AclSSHPermission 控制
  │
  ├── AgentK8SAPI（新增配置）— 本机 K8S API 代理
  │     通过 agent.toml [k8s] 配置 + AclK8sPermission 控制
  │
  ├── AgentK8SService（新增配置）— 本机 K8S SVC 代理
  │     通过 agent.toml [svc] 配置 + AclK8SServicePermission 控制
  │     自动发现 K8S Service，走 tsnet gRPC 代理
  │
  ├── EndpointSSH（新增对象）— 内网 SSH 跳跃端点
  │     ├── web-server-1  → 192.168.1.100 (online)
  │     ├── web-server-2  → 192.168.1.101 (online)
  │     └── db-server     → 192.168.1.200 (offline)
  │     通过 AclSSHJumpPermission 控制
  │
  ├── EndpointK8SAPI（新增对象）— 内网 K8S API 跳跃端点
  │     └── beijing-prod  → 192.168.1.10:6443 (online)
  │     通过 AclK8SAPIJumpPermission 控制
  │
  └── EndpointK8SService（新增对象）— 内网 K8S SVC 跳跃端点
        └── remote-cluster  → (online, 发现 12 个 Service)
        通过 AclK8SServiceJumpPermission 控制
```

## 配置扩展

Agent 在现有配置基础上新增能力相关配置：

```
# agent.toml 新增配置项

[k8s]
enabled = true
kubeconfig = "/etc/kubernetes/admin.conf"
api_server = "https://localhost:6443"

[svc]
enabled = true
label_selector = "tailscale=true"
namespaces = []

[endpoint_server]
enabled = true
listen = "0.0.0.0:50052"           # 内网 gRPC 端口，面向 Endpoint
```

## 实现优先级

| 阶段 | 内容                                          | 依赖             |
| ---- | --------------------------------------------- | ---------------- |
| P1   | AgentK8SAPI — K8S API 代理 + Impersonation    | AclK8sPermission |
| P1   | AgentK8SService — Service 自动发现 + SVC 代理 | K8S RBAC         |
| P2   | Agent tsnet gRPC Server（面向 Desktop）       | Endpoint 体系    |
| P2   | Agent 内网 gRPC Server（面向 Endpoint）       | Endpoint 体系    |
| P2   | 心跳同步扩展（权限数据下发）                  | ACL 模型         |
