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

## 核心原则

1. Endpoint 部署在内网节点，主动反向连接 Agent（Agent 部署在互联网跳板机上）
2. Endpoint 连上 Agent 后，Agent 通过心跳上报给 Server，Server 自动发现并记录（不需要在 Web 上手动创建）
3. Agent 负责 Endpoint 的状态管理，Server 存储 Endpoint 的远程可控设置
4. Endpoint 重启后，所有状态和设置尽量从 Server 恢复（通过 Agent 中转下发）
5. Server 可以注销 Endpoint（标记为注销 → Agent 心跳同步 → Agent 断开该 Endpoint 的 gRPC 连接）

## 架构

```
  互联网（Tailscale 网络）              内网（无 Tailscale）
  ─────────────────────              ──────────────────

  Server（公有云）
    │
    │ gRPC 心跳
    │
  Agent（互联网跳板机）◀── gRPC 反向连接 ── EndpointSSH (192.168.1.100)
    │                  ◀── gRPC 反向连接 ── EndpointK8SAPI (192.168.1.10)
    │                  ◀── gRPC 反向连接 ── EndpointK8SService (192.168.1.20)
    │
    │ tsnet
    │
  Desktop（用户电脑）
```

连接方向：Endpoint 启动时主动连 Agent（反向连接），不是 Agent 连 Endpoint。

## 状态管理链路

```
┌──────────┐     gRPC 反向连接      ┌──────────┐     gRPC 心跳      ┌──────────┐
│ Endpoint │ ──────────────────▶  │  Agent   │ ──────────────▶  │  Server  │
│          │ ◀── 配置下发（来自    │          │ ◀── 配置下发     │          │
│          │     Server，Agent     │          │    （心跳响应）   │          │
│          │     中转）             │          │                  │          │
└──────────┘                      └──────────┘                  └──────────┘
```

### 自动发现流程

```
1. Endpoint 启动，用 token 连接 Agent 内网 gRPC 端口
2. Agent 验证 token，注册 Endpoint（记录名称、能力、IP）
3. Agent 下一次心跳时，将 Endpoint 信息上报给 Server
4. Server 收到后自动创建/更新 Endpoint 记录（无需管理员手动操作）
5. Web 管理界面自动显示已发现的 Endpoint
```

### 配置下发流程（Endpoint 重启恢复）

```
1. 管理员在 Web 上修改 Endpoint 设置（如 enabled、alias、ssh_users 等）
2. Server 存储设置到数据库
3. Agent 心跳时，Server 在响应中携带 Endpoint 配置
4. Agent 缓存配置，下次 Endpoint 连接时下发
5. Endpoint 重启后重新连接 Agent，Agent 下发最新配置
6. Endpoint 应用配置（覆盖本地默认值）
```

### Server 注销流程

```
1. 管理员在 Web 上点击「注销」某个 Endpoint
2. Server 标记该 Endpoint 为已注销（deleted/revoked）
3. Agent 心跳时收到注销指令
4. Agent 断开该 Endpoint 的 gRPC 连接
5. Endpoint 被断开后尝试重连，Agent 拒绝（token 已注销）
6. Web 上该 Endpoint 记录消失或标记为已注销
```

## EndpointSSH

EndpointSSH 装在内网机器上，通过 gRPC 双向流提供 shell 会话，实现与 tailssh 等价的零信任 SSH 体验。

### 设计理念

tailssh 做了三件事：身份认证（WhoIs）、授权（ACL）、SSH Server（spawn shell）。
EndpointSSH 实现同样的事情，只是职责分离：

```
  tailssh (Agent SSH):
    Desktop → Tailscale → Agent tsnet SSH Server
                            ↓
                          WhoIs 确认身份 → ACL 授权 → spawn shell

  EndpointSSH (本方案):
    Desktop → Tailscale → Agent(WhoIs + 授权) → gRPC OpenShell → Endpoint spawn shell
                                                    ↑
                                              复用已有 gRPC 连接
                                              不需要额外端口
                                              不需要内置 SSH Server
                                              不需要签名密钥
```

Agent 负责身份认证和授权，Endpoint 只负责执行 shell。
信任基础是 Endpoint 与 Agent 之间已经过 endpoint_token 认证的 gRPC 长连接。

### 与 tailssh 的对比

```
  维度          tailssh (Agent SSH)              EndpointSSH (本方案)
  ──────────────────────────────────────────────────────────────────────
  身份认证      Tailscale WhoIs                  Agent WhoIs（Agent 侧完成）
  授权          Headscale ACL                    Server ACL（Agent 查询）
  会话通道      tsnet 内置 SSH Server            gRPC 双向流（复用已有连接）
  额外端口      不需要                           不需要
  额外依赖      tailssh 模块                     仅 os/exec + PTY
  网络要求      需要 Tailscale 网络              仅需内网连通 Agent
  用户体验      ssh user@host.beagle             ssh user@ep.agent.beagle（一致）
  密码/密钥     不需要                           不需要
```

### OpenShell gRPC 流

Agent 通过已有的 Endpoint gRPC 连接发送 OpenShell 指令，Endpoint 在 gRPC 双向流中桥接 shell I/O：

```
OpenShell RPC（gRPC 双向流）：

  Agent → Endpoint（首条消息，开启会话）：
    login:   "deploy"          → 要登录的系统用户名
    rows:    24                → 终端行数
    cols:    80                → 终端列数

  Endpoint 收到后：
    1. 检查 login 用户在系统中存在
    2. 检查 login 用户在允许列表中（ssh_users，Agent 下发的配置）
    3. 创建 PTY（伪终端）
    4. 以 login 用户身份 spawn shell（如 /bin/bash）
    5. 通过 gRPC 双向流传输 shell I/O

  后续消息（双向）：
    Agent → Endpoint:  stdin 数据、窗口大小变更
    Endpoint → Agent:  stdout/stderr 数据、退出码

  会话结束：
    shell 退出 → Endpoint 发送退出码 → 关闭 gRPC 流
```

### 工作流程

```
EndpointSSH 启动：
  1. 读取配置：Agent 地址、自身名称、认证 token
  2. 连接 Agent 的内网 gRPC 端口（普通 TCP，不走 Tailscale）
  3. 注册自己：我是 web-server-1，提供 SSH 能力
  4. Agent 下发来自 Server 的配置（ssh_users 等）
  5. 保持 gRPC 长连接（心跳保活）
  6. 等待 Agent 的 OpenShell 指令

Desktop 用户 SSH 到 Endpoint：
  1. ssh deploy@web-server-1.beijing.beagle
  2. ProxyCommand → DialSocket → tsnet → 到达 Agent
  3. Agent WhoIs 确认身份 → 查权限 → 放行
  4. Agent 通过 gRPC 连接发送 OpenShell(login=deploy)
  5. Endpoint spawn shell → gRPC 双向流传输 I/O
  6. 双向桥接: Desktop SSH ↔ Agent ↔ gRPC stream ↔ Endpoint PTY
```

### 数据模型

```
EndpointSSH（Server 数据库，自动发现 + 远程设置）:
  字段              类型      说明
  ─────────────────────────────────────────────────
  id                string    UUID 主键（Server 生成）
  user_id           uint64    所属 Agent 的 User ID
  name              string    名称（Endpoint 上报，如 "web-server-1"）
  alias             string    别名（Server 可修改的显示名称）
  host              string    内网地址（Endpoint 自己上报，仅用于展示）
  ssh_users         string    允许的 SSH 用户列表（Server 可修改，下发给 Endpoint）
  status            string    online/offline（Agent 上报）
  enabled           bool      是否启用（Server 可修改，下发给 Agent/Endpoint）
  revoked           bool      是否已注销（Server 注销后为 true）
  created_at        time      首次发现时间
  updated_at        time

注意：不需要 port 字段。SSH 会话通过 gRPC 双向流传输，
不需要 Endpoint 监听额外端口。
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

### 端口分配

EndpointSSH 使用动态端口 50053+（每个 Endpoint 独立端口）：

```
端口范围: 50053-50152（tsnet 虚拟端口，动态分配）
第一个 Endpoint: 50053
第二个 Endpoint: 50054
依此类推

Desktop 访问: web-server-1.beijing.beagle:22
  → 127.1.x.x:22（魔法 DNS）
  → Tailscale → Agent IP:50053（动态分配的端口）
  → Agent gRPC → Endpoint → SSH

端口分配时机:
  - Endpoint 连接到 Agent 时，Agent 调用 AllocatePort() 分配端口
  - Agent 通过心跳上报分配的端口给 Server
  - Server 更新 domain_registry 表的 target_port 字段
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
EndpointK8SAPI（Server 数据库，自动发现 + 远程设置）:
  字段              类型      说明
  ─────────────────────────────────────────────────
  id                string    UUID 主键（Server 生成）
  user_id           uint64    所属 Agent 的 User ID
  name              string    集群名称（Endpoint 上报，如 "beijing-prod"）
  alias             string    别名（Server 可修改）
  api_server        string    K8S API Server 地址（Endpoint 上报）
  status            string    online/offline（Agent 上报）
  enabled           bool      是否启用（Server 可修改）
  revoked           bool      是否已注销
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
kubernetes.<endpoint-name>.<agent-name>.beagle:6443
kubernetes.beijing-prod.beijing.beagle:6443
```

### 端口分配

EndpointK8SAPI 使用动态端口 50153+（每个 Endpoint 独立端口）：

```
端口范围: 50153-50252（tsnet 虚拟端口，动态分配）
第一个 Endpoint: 50153
第二个 Endpoint: 50154
依此类推

Desktop 访问: kubernetes.beijing-prod.beijing.beagle:6443
  → 127.1.x.x:6443（魔法 DNS）
  → Tailscale → Agent IP:50153（动态分配的端口）
  → Agent gRPC → Endpoint → K8S API Server

端口分配时机:
  - Endpoint 连接到 Agent 时，Agent 调用 AllocatePort() 分配端口
  - Agent 通过心跳上报分配的端口给 Server
  - Server 更新 domain_registry 表的 target_port 字段
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
EndpointK8SService（Server 数据库，自动发现 + 远程设置）:
  字段              类型      说明
  ─────────────────────────────────────────────────
  id                string    UUID 主键（Server 生成）
  user_id           uint64    所属 Agent 的 User ID
  name              string    集群名称（Endpoint 上报，如 "remote-cluster"）
  alias             string    别名（Server 可修改）
  status            string    online/offline（Agent 上报）
  enabled           bool      是否启用（Server 可修改）
  revoked           bool      是否已注销
  created_at        time
  updated_at        time

EndpointK8SService 发现的 Service 列表（Endpoint 上报 → Agent 缓存 → 心跳上报 Server）：
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

### 端口分配

EndpointK8SService 使用固定端口 50051（gRPC 服务，所有 Endpoint 共享）：

```
端口: 50051（tsnet 虚拟端口，固定）
所有 Endpoint 共享同一个端口，通过 gRPC 参数区分不同 Endpoint 和 Service

Desktop 访问: pg.yygl.remote-cluster.beijing.beagle:5432
  → 127.1.x.x:5432（魔法 DNS）
  → Tailscale → Agent IP:50051（固定端口）
  → Agent gRPC SVCProxy(endpoint=remote-cluster, namespace=yygl, service=pg)
  → Endpoint → K8S ClusterIP

说明:
  - EndpointK8SService 不需要动态端口分配
  - 通过 gRPC 参数传递目标 Endpoint 和 Service 信息
  - Desktop 通过 VIP 隔离不同 Service 的端口冲突
```

## Endpoint 部署模型

### 统一二进制

EndpointSSH、EndpointK8SAPI、EndpointK8SService 是同一个二进制 `signal_endpoint`，通过配置决定启用哪些能力：

```
signal_endpoint 二进制

  endpoint.toml（本地最小配置，其余从 Server 恢复）:
    [agent]
    address = "192.168.1.1:50052"   # Agent 内网 gRPC 地址
    token = "ep_xxxxxxxxxxxxxxxx"    # 注册令牌（Server 生成，Web 上复制）

    [ssh]
    enabled = true

    [k8s]
    enabled = true

    [svc]
    enabled = true
    label_selector = "signal.beagle.io/expose=true"

  一台机器可以同时是 SSH 端点、K8SAPI 端点和 K8SService 端点
  SSH 能力不需要额外端口，shell 会话通过 gRPC 双向流传输
```

本地配置只需要 Agent 地址和 token，其余设置（alias、ssh_users、enabled 等）重启后从 Server 通过 Agent 下发恢复。

### EndpointSSH 部署

```
内网机器上安装 EndpointSSH：

  安装方式：单个二进制，无依赖，不需要额外端口
    [agent]
    address = "192.168.1.1:50052"
    token = "ep_xxxxxxxxxxxxxxxx"

    [ssh]
    enabled = true

  启动后：
    1. 连接 Agent 内网 gRPC Server（50052）
    2. 用 token 认证（Agent 比对 Server 下发的 endpoint_token）
    3. 注册自己（名称、IP、SSH 能力）
    4. Agent 下发来自 Server 的配置（ssh_users、enabled 等）
    5. 保持 gRPC 长连接（心跳保活）
    6. 收到 OpenShell 指令 → 创建 PTY → spawn shell → gRPC 双向流传输 I/O
```

### EndpointK8SAPI 部署

```
K8S 主节点上安装 EndpointK8SAPI：

  配置文件：endpoint.toml
    [agent]
    address = "192.168.1.1:50052"
    token = "ep_xxxxxxxxxxxxxxxx"

    [k8s]
    enabled = true

  启动后：
    1. 连接 Agent 内网 gRPC Server（50052）
    2. 注册自己（集群名、API 地址）
    3. Agent 下发来自 Server 的配置
    4. 收到 K8sAPIProxy 指令 → Impersonation 转发 → 返回结果
```

### EndpointK8SService 部署

```
K8S 集群节点上安装 EndpointK8SService：

  配置文件：endpoint.toml
    [agent]
    address = "192.168.1.1:50052"
    token = "ep_xxxxxxxxxxxxxxxx"

    [svc]
    enabled = true
    label_selector = "signal.beagle.io/expose=true"

  启动后：
    1. 连接 Agent 内网 gRPC Server（50052）
    2. 启动 K8S Service Informer，自动发现 Service
    3. 通过 gRPC 上报发现的 Service 给 Agent
    4. Agent 下发来自 Server 的配置
    5. 收到 SVCProxy 指令 → 转发到 ClusterIP
```

## Agent Endpoint 功能开关

Agent 的 Endpoint 功能由 Server 远程控制（和 SSH/K8S/SVC 一样通过 CapabilityConfig 下发）。

```
Server 远程配置（心跳响应 CapabilityConfig）：
  endpoint_enabled       bool     是否启用 Endpoint 功能
  endpoint_listen_port   int      内网 gRPC 监听端口（默认 50052）
  endpoint_token         string   Endpoint 注册令牌（Server 生成）
```

### 开启流程

```
1. 管理员在 Web 上进入 Agent 详情页
2. 开启「Endpoint 功能」开关 → Server 自动生成 endpoint_token
3. 可选修改监听端口（默认 50052）
4. Server 心跳响应下发：endpoint_enabled=true, listen_port=50052, token=ep_xxx
5. Agent 收到后启动内网 gRPC Server（监听 0.0.0.0:50052）
6. 管理员从 Web 上复制 endpoint_token，配置到内网机器的 endpoint.toml
```

### Token 管理

```
生成：管理员在 Web 上开启 Endpoint 功能时自动生成
查看：Web Agent 详情页可查看/复制 token
重新生成：管理员可点击「重新生成」，旧 token 立即失效
          → 心跳下发新 token → Agent 更新 → 已连接的 Endpoint 下次重连时需要新 token
注销：关闭 Endpoint 功能 → token 清空 → Agent 关闭内网 gRPC Server → 所有 Endpoint 断开
```

## Endpoint 注册认证

Endpoint 连接 Agent 时使用 Server 生成的 endpoint_token 认证。

```
Endpoint 注册流程：
  1. 管理员在 Web 上为 Agent 开启 Endpoint 功能（自动生成 token）
  2. 管理员复制 token，配置到内网机器的 endpoint.toml
  3. Endpoint 启动，用 token 连接 Agent 内网 gRPC 端口（50052）
  4. Agent 验证 token（和心跳下发的 endpoint_token 比对）
  5. 验证通过 → 注册 Endpoint（记录名称、IP、能力）
  6. Endpoint 信息通过 Agent 心跳上报给 Server
  7. Server 自动创建 Endpoint 记录（无需手动操作）
```

## 与当前 Agent 直连模式的对比

```
当前 Agent 直连模式（P1 已实现）：
  Agent 部署在 K8S 集群内部，直接访问 K8S API 和 ClusterIP
  Desktop → tsnet → Agent → K8S API / ClusterIP

Endpoint 跳跃模式（P2 新增）：
  Agent 部署在互联网跳板机，Endpoint 部署在内网
  Desktop → tsnet → Agent → gRPC → Endpoint → K8S API / ClusterIP / SSH

两种模式共存，Agent 根据请求目标自动选择：
  - 目标是 Agent 自身的 K8S → 直连模式
  - 目标是某个 Endpoint → 跳跃模式（转发到 Endpoint 的 gRPC 连接）
```

## 实现优先级

| 阶段 | 内容                              | 依赖            |
| ---- | --------------------------------- | --------------- |
| P2   | signal_endpoint 二进制框架        | 无              |
| P2   | Agent 内网 gRPC Server            | Agent           |
| P2   | Endpoint 注册和状态上报           | Agent 心跳      |
| P2   | Server 自动发现和配置下发         | Server 心跳响应 |
| P2   | Server 注销 Endpoint              | Web API         |
| P2   | EndpointSSH — SSH 会话桥接        | Agent 内网 gRPC |
| P2   | EndpointK8SAPI — K8S API 代理     | Agent 内网 gRPC |
| P2   | EndpointK8SService — K8S SVC 代理 | Agent 内网 gRPC |
| P2   | Endpoint 注册令牌管理             | Web 管理界面    |
