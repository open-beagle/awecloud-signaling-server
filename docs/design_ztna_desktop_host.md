# ZTNA Desktop.Host 桌面客户端重构设计

## 概述

Desktop.Host 是运行在用户物理机上的桌面应用（macOS/Windows/Linux），使用 Wails GUI。最大的变更：去掉 tailscaled（系统级 VPN），改用 tsnet（用户态）+ DNS 劫持 + VIP 分配。

共通设计见 design_ztna_desktop.md。本文档仅描述 Desktop.Host 特有的部分。

## 核心洞察

Desktop 不需要 VPN。

当前 Desktop 使用 tailscaled 创建 TUN 设备、注入系统路由表，本质上是一个完整的 VPN 客户端。但 Desktop 的实际需求只是"访问特定的内网服务"，不需要全局路由。

tsnet 用户态方案完全满足需求：

| 维度     | tailscaled（当前）           | tsnet（ZTNA）                      |
| -------- | ---------------------------- | ---------------------------------- |
| 权限要求 | 需要管理员权限（TUN 设备）   | 普通用户权限即可                   |
| 系统影响 | 修改路由表、DNS 配置         | 零系统侵入                         |
| 端口冲突 | 靠 Tailscale IP 段隔离       | VIP（127.1.x.x）隔离，同端口不冲突 |
| 安装体验 | 需要安装 Tailscale 客户端    | 内嵌在 Desktop 中，无需额外安装    |
| 多实例   | 系统只能运行一个 tailscaled  | 可以运行多个 tsnet 实例            |
| 跨平台   | 各平台 tailscaled 行为不一致 | tsnet 行为一致                     |

## 架构变更

### 当前架构

```
Desktop 进程
  │
  ├── Wails 前端（Vue 3）
  ├── gRPC 客户端 → Server
  └── tailscaled 管理
        ├── 启动/停止 tailscaled 进程
        ├── 等待 tailscaled 就绪
        └── 通过 tailscaled 路由访问 Agent
```

### ZTNA 架构

```
Desktop 进程
  │
  ├── Wails 前端（Vue 3）
  │     ├── 资源发现 UI（新增）
  │     └── 连接管理（增强）
  │
  ├── gRPC 客户端 → Server
  │
  ├── tsnet 引擎（替换 tailscaled）
  │
  ├── DNS 劫持模块（新增）
  │     └── 拦截 .beagle 域名解析
  │
  ├── VIP 分配器（新增）
  │     └── 分配 127.1.x.x 本地地址
  │
  └── 本地代理（新增）
        └── VIP:端口 → tsnet Dial → Agent
```

## tsnet 迁移

### 迁移影响

| 模块                 | 变更                                     |
| -------------------- | ---------------------------------------- |
| tailscale/manager.go | 重写：从进程管理改为 tsnet 管理          |
| platform_darwin.go   | 删除：不再需要平台特定的 tailscaled 路径 |
| platform_linux.go    | 删除：同上                               |
| platform_windows.go  | 删除：同上                               |
| embed_windows.go     | 删除：不再需要嵌入 wintun.dll            |
| app.go               | 修改：连接流程改为 tsnet                 |

## DNS 劫持（各平台实现）

### macOS

macOS 支持按域名后缀指定 DNS 服务器，最简洁：

```
创建 /etc/resolver/beagle：
  nameserver 127.0.0.1
  port 15353

效果：
  所有 .beagle 域名查询发送到 127.0.0.1:15353
  其他域名不受影响
```

### Linux

```
方案 1：systemd-resolved（推荐）
  配置 .beagle 域名路由到本地 DNS

方案 2：修改 /etc/resolv.conf
  添加本地 DNS 作为首选
```

### Windows

```
修改网络适配器 DNS 设置
  或使用 NRPT（Name Resolution Policy Table）
```

## 资源发现 UI

Desktop.Host 特有的 GUI 功能，展示用户可访问的所有资源：

```
┌─────────────────────────────────────────────────────┐
│  资源浏览                                            │
├─────────────────────────────────────────────────────┤
│  集群: [全部 ▼]  类型: [全部 ▼]  搜索: [        ]  │
│                                                     │
│  ┌─────────────────────────────────────────────┐    │
│  │ 📁 beijing                                   │    │
│  │   ├── 🖥 beijing.beagle                         │    │
│  │   │     SSH (22)  ● 在线                     │    │
│  │   ├── 🗄 pg.yygl.beijing.beagle                 │    │
│  │   │     PostgreSQL (5432)  ● 在线            │    │
│  │   ├── 🖥 web-server-1.beijing.beagle            │    │
│  │   │     EndpointSSH (22)  ● 在线             │    │
│  │   └── 🔧 kubernetes.beijing-prod.beijing.beagle        │    │
│  │         EndpointK8SAPI (50050)  ● 在线       │    │
│  └─────────────────────────────────────────────┘    │
│                                                     │
│  点击资源可复制连接命令                              │
└─────────────────────────────────────────────────────┘
```

### 连接操作

| 资源类型        | 操作                             |
| --------------- | -------------------------------- |
| AgentSSH        | 复制 ssh 命令 / 打开终端直接连接 |
| EndpointSSH     | 复制 ssh 命令                    |
| AgentK8SService | 复制连接字符串 / 显示 VIP 地址   |
| AgentK8SAPI     | 复制 kubeconfig 配置             |
| EndpointK8SAPI  | 复制 kubeconfig 配置             |

## K8S API 访问流程

### AgentK8SAPI（Agent 本机 K8S API）

Agent 部署在 K8S 主节点，直接访问本机 K8S API Server，做 Impersonation 代理。

```
业务流程：

  1. Desktop 从 Server 获取可访问的 K8S 集群列表
       → 发现 beijing 集群（AgentK8SAPI）
       → 域名: kubernetes.beijing.beagle
       → 端口: 6443

  2. Desktop 魔法 DNS 解析
       → kubernetes.beijing.beagle → 127.1.x.x（VIP 地址）
       → 本地代理监听 127.1.x.x:6443

  3. Desktop 生成 kubeconfig
       → server: https://kubernetes.beijing.beagle:6443
       → 证书验证跳过（TLS 由 tsnet 隧道保证）
       → token: 任意值（如 "whoisyourdaddy"）
       → 写入 ~/.kube/config

  4. 用户执行 kubectl 命令
       → kubectl get pods -n yygl
       → 请求发送到 kubernetes.beijing.beagle:6443

  5. Desktop 魔法 DNS 劫持
       → kubernetes.beijing.beagle → 127.1.x.x
       → 本地代理接收请求（127.1.x.x:6443）

  6. Desktop 本地代理转发
       → 查询域名注册表：target_ip=100.64.0.23, target_port=50050
       → 通过 tsnet Dial 连接 Agent（100.64.0.23:50050）

  7. Agent 接收请求
       → 从 tsnet 连接提取身份（zhangsan）
       → 查询权限（本地缓存）
       → 允许的 namespaces: ["yygl"]
       → k8s_role: developer

  8. Agent 构造 Impersonation 请求
       → Impersonate-User: zhangsan
       → Impersonate-Group: developer
       → 使用主节点 kubeconfig 转发到本机 K8S API Server

  9. K8S API Server 处理请求
       → 以 zhangsan 身份执行
       → 应用 developer 角色的 RBAC 规则
       → 返回结果

  10. 结果返回给 Desktop → kubectl 显示
```

```
技术实现：

  Desktop 侧：
    ├── 魔法 DNS 劫持
    │     *.beagle → 127.1.x.x（VIP 地址）
    │
    ├── 本地代理监听 VIP
    │     127.1.x.x:6443 → 监听 K8S API 请求
    │
    ├── 查询域名注册表
    │     kubernetes.beijing.beagle → target_ip=100.64.0.23, target_port=50050
    │
    ├── tsnet Dial 转发
    │     通过 Tailscale 隧道连接 Agent（100.64.0.23:50050）
    │     桥接请求和响应
    │
    └── kubeconfig 生成
          server: https://kubernetes.beijing.beagle:6443
          token: "whoisyourdaddy"  # 必须提供，值任意（Agent 不验证）
          insecure-skip-tls-verify: true

  Agent 侧：
    ├── tsnet 监听 K8S API 代理端口（50050）
    │     从 tsnet 连接提取身份
    │     查询 AclK8sPermission（本地缓存）
    │
    ├── Impersonation 转发
    │     添加 Impersonate-User 和 Impersonate-Group 请求头
    │     使用主节点 kubeconfig 转发到 localhost:6443
    │
    └── 响应返回
          透明转发 K8S API Server 的响应
```

### EndpointK8SAPI（Endpoint 跳跃 K8S API）

Endpoint 部署在内网 K8S 集群，通过 gRPC 连接到 Agent，Agent 作为身份中继。

```
业务流程：

  1. Desktop 从 Server 获取可访问的 K8S 集群列表
       → 发现 beijing-prod 集群（EndpointK8SAPI）
       → 域名: kubernetes-beijing-prod.beijing.beagle
       → 端口: 6443

  2. Desktop 魔法 DNS 解析
       → kubernetes-beijing-prod.beijing.beagle → 127.1.x.y（另一个 VIP）
       → 本地代理监听 127.1.x.y:6443

  3. Desktop 生成 kubeconfig
       → server: https://kubernetes-beijing-prod.beijing.beagle:6443
       → 证书验证跳过
       → token: 任意值（如 "whoisyourdaddy"）
       → 写入 ~/.kube/config

  4. 用户执行 kubectl 命令
       → kubectl get pods -n prod --context beijing-prod
       → 请求发送到 kubernetes-beijing-prod.beijing.beagle:6443

  5. Desktop 魔法 DNS 劫持
       → kubernetes-beijing-prod.beijing.beagle → 127.1.x.y
       → 本地代理接收请求（127.1.x.y:6443）

  6. Desktop 本地代理转发
       → 查询域名注册表：target_ip=100.64.0.23, target_port=50054
       → 通过 tsnet Dial 连接 Agent（100.64.0.23:50054）

  7. Agent 接收请求
       → 从 tsnet 连接提取身份（zhangsan）
       → 查询权限（本地缓存）
       → 允许的 namespaces: ["prod"]
       → k8s_role: viewer

  8. Agent 通过 gRPC 连接向 Endpoint 发送 K8sAPIProxy 请求
       → 参数: user=zhangsan, namespaces=["prod"], role=viewer
       → 请求体: kubectl 原始 HTTP 请求

  9. Endpoint 收到请求
       → 构造 Impersonation 请求
       → Impersonate-User: zhangsan
       → Impersonate-Group: viewer
       → 使用 Endpoint 的 kubeconfig 转发到内网 K8S API Server

  10. 内网 K8S API Server 处理请求
        → 以 zhangsan 身份执行
        → 应用 viewer 角色的 RBAC 规则
        → 返回结果

  11. 结果返回
        Endpoint → Agent → Desktop → kubectl 显示
```

```
技术实现：

  Desktop 侧：
    ├── 魔法 DNS 劫持
    │     *.beagle → 127.1.x.x（VIP 地址，每个域名分配不同的 VIP）
    │
    ├── 本地代理监听 VIP
    │     127.1.x.y:6443 → 监听 K8S API 请求
    │
    ├── 查询域名注册表
    │     kubernetes-beijing-prod.beijing.beagle → target_ip=100.64.0.23, target_port=50054
    │
    ├── tsnet Dial 转发
    │     通过 Tailscale 隧道连接 Agent（100.64.0.23:50054）
    │     桥接请求和响应
    │
    └── kubeconfig 生成
          server: https://kubernetes-beijing-prod.beijing.beagle:6443
          token: "whoisyourdaddy"  # 必须提供，值任意（Agent 不验证）
          insecure-skip-tls-verify: true

  Agent 侧：
    ├── tsnet 监听 Endpoint K8S API 代理端口（50054）
    │     从 tsnet 连接提取身份
    │     查询 AclK8SAPIJumpPermission（本地缓存）
    │
    ├── gRPC 调用 Endpoint
    │     K8sAPIProxy RPC
    │     参数: user, namespaces, role
    │     请求体: kubectl 原始 HTTP 请求
    │
    └── 响应返回
          透明转发 Endpoint 的响应

  Endpoint 侧：
    ├── gRPC Server（K8sAPIProxy RPC）
    │     接收 Agent 的代理请求
    │     提取 user, namespaces, role 参数
    │
    ├── Impersonation 转发
    │     添加 Impersonate-User 和 Impersonate-Group 请求头
    │     使用 Endpoint 的 kubeconfig 转发到内网 K8S API Server
    │
    └── 响应返回
          透明转发 K8S API Server 的响应
```

### 两种方式对比

```
                    AgentK8SAPI                          EndpointK8SAPI
  部署位置          K8S 主节点                           内网任意位置
  访问方式          直接访问本机 API Server              通过 gRPC 跳跃
  身份中继          Agent 自己完成                       Agent → Endpoint
  域名格式          kubernetes.{region}.beagle           kubernetes-{endpoint}.{region}.beagle
  VIP 分配          127.1.x.x:6443                       127.1.x.y:6443（不同 VIP）
  Agent 端口        50050（tsnet 虚拟端口）              50054（tsnet 虚拟端口）
  适用场景          Agent 在主节点                       Agent 不在主节点
```

## 实现优先级

| 阶段 | 内容                   | 依赖         |
| ---- | ---------------------- | ------------ |
| P0   | tsnet 替换 tailscaled  | 无           |
| P0   | DNS 劫持（macOS 优先） | tsnet 迁移   |
| P0   | VIP 分配器             | DNS 劫持     |
| P0   | 本地代理               | VIP 分配     |
| P1   | 资源发现 UI            | 资源发现 API |
| P2   | 各平台 DNS 劫持适配    | DNS 劫持模块 |
| P2   | 连接状态监控和展示     | 本地代理     |

## kubeconfig 配置说明

### 认证机制

Desktop.Host 访问 K8S API 的认证完全依赖 Tailscale 身份，不验证 kubeconfig 中的 token。

### 为什么必须提供 token

kubectl 是原生的 K8S 客户端工具，要求 kubeconfig 必须包含认证信息（token/cert/username+password）。如果不提供 token，kubectl 会报错拒绝执行。

但是，Agent 从 tsnet 连接中提取 Tailscale 用户身份进行认证，完全忽略 HTTP 请求中的 Authorization 头（即 kubeconfig 中的 token）。

因此，kubeconfig 中的 token 必须提供，但值可以是任意字符串。

### kubeconfig 示例

#### AgentK8SAPI 配置

```yaml
apiVersion: v1
kind: Config
clusters:
  - cluster:
      insecure-skip-tls-verify: true
      server: https://kubernetes.beijing.beagle:6443
    name: beijing
contexts:
  - context:
      cluster: beijing
      user: beijing-user
    name: beijing
current-context: beijing
users:
  - name: beijing-user
    user:
      token: "whoisyourdaddy" # 必须提供，值任意（Agent 不验证）
```

#### EndpointK8SAPI 配置

```yaml
apiVersion: v1
kind: Config
clusters:
  - cluster:
      insecure-skip-tls-verify: true
      server: https://kubernetes-beijing-prod.beijing.beagle:6443
    name: beijing-prod
contexts:
  - context:
      cluster: beijing-prod
      user: beijing-prod-user
    name: beijing-prod
current-context: beijing-prod
users:
  - name: beijing-prod-user
    user:
      token: "whoisyourdaddy" # 必须提供，值任意（Agent 不验证）
```

### 多集群配置

Desktop 可以同时访问多个 K8S 集群，每个集群使用不同的域名和 VIP：

```yaml
apiVersion: v1
kind: Config
clusters:
  - cluster:
      insecure-skip-tls-verify: true
      server: https://kubernetes.beijing.beagle:6443
    name: beijing
  - cluster:
      insecure-skip-tls-verify: true
      server: https://kubernetes-beijing-prod.beijing.beagle:6443
    name: beijing-prod
contexts:
  - context:
      cluster: beijing
      user: beijing-user
    name: beijing
  - context:
      cluster: beijing-prod
      user: beijing-prod-user
    name: beijing-prod
current-context: beijing
users:
  - name: beijing-user
    user:
      token: "whoisyourdaddy"
  - name: beijing-prod-user
    user:
      token: "whoisyourdaddy"
```

使用时通过 `--context` 切换集群：

```bash
kubectl get pods -n yygl --context beijing
kubectl get pods -n prod --context beijing-prod
```

### 安全性说明

1. **Tailscale 身份不可伪造**：tsnet 连接的身份由 Tailscale 网络保证，无法伪造
2. **权限由 Server 下发**：AclK8sPermission 通过 Desktop 从 Server 同步
3. **K8S RBAC 仍然生效**：通过 Impersonation 机制，K8S 的 RBAC 规则仍然应用
4. **kubeconfig token 无关紧要**：即使 token 泄露，攻击者也无法绕过 Tailscale 身份认证
