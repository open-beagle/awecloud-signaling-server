# ZTNA Endpoint 跳跃端点设计

## 概述

Endpoint 是部署在内网机器上的轻量 daemon。它不在 Tailscale 网络中，不连 Server 或 Headscale，只反向连接 Agent 的内网 gRPC 端口。

Endpoint 的作用是让 Desktop 通过 Agent 中转，访问内网中没有安装 Tailscale 的机器和服务。

三种 Endpoint 完全对称：

```
  EndpointSSH          → 内网机器上的轻量 daemon，提供 SSH 会话桥接
  EndpointK8SAPI       → 内网 K8S 主节点上的轻量 daemon，提供 K8S API 代理
  EndpointK8SService   → 内网 K8S 节点上的轻量 daemon，提供 K8S SVC 代理
```

## 架构

```
  Tailscale 网络                     内网（无 Tailscale）
  ─────────────                     ──────────────────

  Desktop ──tsnet──▶ Agent          EndpointSSH (192.168.1.100)
                       │                  │
                       │◀── gRPC 反向连接 ─┘
                       │
                       │            EndpointK8SAPI (192.168.1.10)
                       │                  │
                       │◀── gRPC 反向连接 ─┘
                       │
                       │            EndpointK8SService (192.168.1.20)
                       │                  │
                       │◀── gRPC 反向连接 ─┘
```

连接方向：Endpoint 启动时主动连 Agent（反向连接），不是 Agent 连 Endpoint。

## EndpointSSH

EndpointSSH 装在内网机器上，提供 SSH 会话桥接。

### 工作流程

```
EndpointSSH 启动：
  1. 读取配置：Agent 地址、自身名称、认证 token
  2. 连接 Agent 的内网 gRPC 端口（普通 TCP，不走 Tailscale）
  3. 注册自己：我是 web-server-1，提供 SSH 能力
  4. 保持长连接，等待 Agent 下发指令

Agent 收到 Desktop 的跳跃请求：
  1. 从 tsnet 提取身份 → zhangsan
  2. 查找目标 EndpointSSH → web-server-1
  3. 检查 EndpointSSH 是否在线（有没有活跃的 gRPC 连接）
  4. 查询 AclSSHJumpPermission → zhangsan 能不能跳到 web-server-1
  5. 通过 gRPC 流告诉 EndpointSSH：开一个 deploy 用户的 shell
  6. 双向桥接：Desktop ←tsnet→ Agent ←gRPC stream→ EndpointSSH ←shell
```

### 数据模型

```
EndpointSSH:
  字段              类型      说明
  ─────────────────────────────────────────────────
  id                string    UUID 主键
  user_id           uint64    所属 Agent 的 User ID
  name              string    名称（如 "web-server-1"）
  alias             string    别名（显示名称）
  host              string    内网地址（Endpoint 自己上报）
  port              int       SSH 端口（默认 22）
  status            string    online/offline（有活跃 gRPC 连接 = online）
  enabled           bool      是否启用
  created_at        time
  updated_at        time
```

### ACL

```
AclSSHJumpUserPermission:
  endpoint_ssh_id  → 哪个 EndpointSSH
  user_id          → 被授权用户
  ssh_users        → 允许的 Linux 用户列表

AclSSHJumpGroupPermission:
  endpoint_ssh_id  → 哪个 EndpointSSH
  group_id         → 被授权分组
  ssh_users        → 允许的 Linux 用户列表
```

### 域名

```
<endpoint-name>.<agent-name>.beagle:22
web-server-1.beijing.beagle:22
```

## EndpointK8SAPI

EndpointK8SAPI 装在内网 K8S 主节点上，提供 K8S API 代理。和 EndpointSSH 一样，反向连接 Agent。

### 工作流程

```
Desktop kubectl 请求：
  1. Desktop → tsnet → Agent gRPC Server → K8sAPIProxy RPC
  2. Agent 从 tsnet 提取身份 → zhangsan
  3. Agent 查找目标 EndpointK8SAPI → beijing-prod
  4. Agent 查询 AclK8SAPIJumpPermission → namespace: yygl, role: developer
  5. Agent 通过 gRPC 流转发请求到 EndpointK8SAPI
  6. EndpointK8SAPI 用本地 kubeconfig + Impersonation 转发到 K8S API Server
  7. 返回结果
```

### 数据模型

```
EndpointK8SAPI:
  字段              类型      说明
  ─────────────────────────────────────────────────
  id                string    UUID 主键
  user_id           uint64    所属 Agent 的 User ID
  name              string    集群名称（如 "beijing-prod"）
  alias             string    别名（显示名称）
  api_server        string    K8S API Server 地址（Endpoint 上报）
  kubeconfig_path   string    kubeconfig 文件路径
  status            string    online/offline
  enabled           bool      是否启用
  created_at        time
  updated_at        time
```

### ACL

```
AclK8SAPIJumpUserPermission:
  endpoint_k8sapi_id → 哪个 EndpointK8SAPI
  user_id            → 被授权用户
  namespaces         → 允许的命名空间列表
  k8s_role           → Impersonation 角色

AclK8SAPIJumpGroupPermission:
  endpoint_k8sapi_id → 哪个 EndpointK8SAPI
  group_id           → 被授权分组
  namespaces         → 允许的命名空间列表
  k8s_role           → Impersonation 角色
```

### 域名

```
api.<endpoint-name>.<agent-name>.beagle:6443
api.beijing-prod.beijing.beagle:6443
```

## EndpointK8SService

EndpointK8SService 装在内网 K8S 集群节点上，提供 K8S Service 代理。核心能力和 AgentK8SService 一样：K8S Service 自动发现 + SVC 代理。区别是 EndpointK8SService 不在 Tailscale 网络中，它通过 Agent 中转。

### 工作流程

```
Desktop 访问 pg.yygl.remote.beagle:5432：
  1. Desktop → tsnet → Agent gRPC Server → SVCProxy RPC
  2. Agent 从 tsnet 提取身份 → zhangsan
  3. Agent 查找目标 EndpointK8SService → remote-cluster
  4. Agent 查询 AclK8SServiceJumpPermission → zhangsan 能不能访问这个 SVC
  5. Agent 通过 gRPC 流转发到 EndpointK8SService
  6. EndpointK8SService 转发到 K8S ClusterIP (10.96.23.45:5432)
  7. 双向桥接
```

EndpointK8SService 启动后自动发现 K8S Service（和 AgentK8SService 逻辑相同），通过 gRPC 上报给 Agent，Agent 再通过心跳上报给 Server。

### 数据模型

```
EndpointK8SService:
  字段              类型      说明
  ─────────────────────────────────────────────────
  id                string    UUID 主键
  user_id           uint64    所属 Agent 的 User ID
  name              string    集群名称（如 "remote-cluster"）
  alias             string    别名（显示名称）
  status            string    online/offline
  enabled           bool      是否启用
  created_at        time
  updated_at        time

EndpointK8SService 发现的 Service 列表（Endpoint 上报，Agent 缓存）：
  endpoint_k8sservice_id → 所属 EndpointK8SService
  service_name      → K8S Service 名称
  namespace         → 命名空间
  cluster_ip        → ClusterIP
  ports             → 端口列表
  status            → online/offline
```

### ACL

```
AclK8SServiceJumpUserPermission:
  endpoint_k8sservice_id → 哪个 EndpointK8SService
  user_id                → 被授权用户
  service_pattern        → 允许访问的 Service 模式（如 "*.yygl" 或 "*"）

AclK8SServiceJumpGroupPermission:
  endpoint_k8sservice_id → 哪个 EndpointK8SService
  group_id               → 被授权分组
  service_pattern        → 允许访问的 Service 模式
```

### 域名

```
<service>.<namespace>.<endpoint-name>.<agent-name>.beagle
pg.yygl.remote-cluster.beijing.beagle:5432
```

## Endpoint 部署模型

### 统一二进制

EndpointSSH、EndpointK8SAPI、EndpointK8SService 是同一个二进制 `signal_endpoint`，通过配置决定启用哪些能力：

```
signal_endpoint 二进制

  endpoint.toml:
    [agent]
    address = "192.168.1.1:50052"   # Agent 内网 gRPC 地址
    token = "xxx"                    # 注册令牌（Agent 生成）

    [ssh]
    enabled = true
    allowed_users = ["root", "deploy"]

    [k8s]
    enabled = true
    kubeconfig = "/etc/kubernetes/admin.conf"
    api_server = "https://localhost:6443"

    [svc]
    enabled = true
    label_selector = "signal.beagle.io/expose=true"
    namespaces = []

  一台机器可以同时是 SSH 端点、K8SAPI 端点和 K8SService 端点
```

### EndpointSSH 部署

```
内网机器上安装 EndpointSSH：

  安装方式：单个二进制，无依赖
    [agent]
    address = "192.168.1.1:50052"
    token = "xxx"

    [ssh]
    enabled = true
    allowed_users = ["root", "deploy", "app"]

  启动后：
    1. 连接 Agent 内网 gRPC Server
    2. 用 token 认证
    3. 注册自己（名称、IP、能力）
    4. 保持长连接，等待指令
    5. 收到 OpenShell 指令 → 启动 shell → 桥接到 gRPC 流
```

### EndpointK8SAPI 部署

```
K8S 主节点上安装 EndpointK8SAPI：

  配置文件：endpoint.toml
    [agent]
    address = "192.168.1.1:50052"
    token = "xxx"

    [k8s]
    enabled = true
    kubeconfig = "/etc/kubernetes/admin.conf"
    api_server = "https://localhost:6443"

  启动后：
    1. 连接 Agent 内网 gRPC Server
    2. 注册自己（集群名、API 地址）
    3. 收到 K8sAPIProxy 指令 → Impersonation 转发 → 返回结果
```

### EndpointK8SService 部署

```
K8S 集群节点上安装 EndpointK8SService：

  配置文件：endpoint.toml
    [agent]
    address = "192.168.1.1:50052"
    token = "xxx"

    [svc]
    enabled = true
    label_selector = "signal.beagle.io/expose=true"
    namespaces = []

  启动后：
    1. 连接 Agent 内网 gRPC Server
    2. 启动 K8S Service Informer，自动发现 Service
    3. 通过 gRPC 上报发现的 Service 给 Agent
    4. 收到 SVCProxy 指令 → 转发到 ClusterIP
```

## Endpoint 注册令牌

Endpoint 连接 Agent 时需要认证。Agent 生成 token 给 Endpoint 使用。

```
Endpoint 注册流程：
  1. 管理员在 Web 界面为 Agent 生成 Endpoint 注册令牌
  2. 将令牌配置到 Endpoint 的 endpoint.toml
  3. Endpoint 启动时用令牌连接 Agent
  4. Agent 验证令牌，注册 Endpoint
  5. Endpoint 信息通过 Agent 心跳上报给 Server
```

## 实现优先级

| 阶段 | 内容                              | 依赖            |
| ---- | --------------------------------- | --------------- |
| P2   | signal_endpoint 二进制框架        | 无              |
| P2   | EndpointSSH — SSH 会话桥接        | Agent 内网 gRPC |
| P2   | EndpointK8SAPI — K8S API 代理     | Agent 内网 gRPC |
| P2   | EndpointK8SService — K8S SVC 代理 | Agent 内网 gRPC |
| P2   | Endpoint 注册令牌管理             | Web 管理界面    |
