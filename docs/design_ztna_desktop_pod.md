# ZTNA Desktop.Pod 容器内 daemon 设计

## 概述

Desktop.Pod 运行在 K8S Pod 中（CloudIDE 容器），以 Client 角色加入 Tailscale 网络。它不是 Agent，而是 Desktop 的另一种形态——没有 GUI 的纯后台 daemon。

Desktop.Pod 使用 signal_agent 二进制的 RunClient 模式运行，权限和 Desktop.Host 完全相同。

共通设计见 design_ztna_desktop.md。本文档仅描述 Desktop.Pod 特有的部分。

## 用户与角色关系

Desktop.Pod（CloudIDE）属于某个 client 用户的一个部署节点，和 Desktop.Host 地位相同。管理员为 client 用户创建 deploy token，CloudIDE 用该 token 注册后以 Client 模式运行，自动继承该用户的所有权限（ACL、分组等）。

```
用户: zhangsan (role=client)
  ├── Desktop.Host: macbook（Logto 登录）
  ├── Desktop.Pod:  ide-zhangsan（Deploy Token 注册）  ← CloudIDE
  └── 其他设备...

用户: beijing (role=agent)
  └── Agent: beagle-xxx（Deploy Token 注册）
```

注意区分：

- client 用户（如 zhangsan）→ 创建 deploy token → CloudIDE 以 Client 模式运行（RunClient）
- agent 用户（如 beijing）→ 创建 deploy token → Agent 以 Agent 模式运行（Run）

两者使用同一套 deploy_tokens 机制和统一注册接口 POST /api/v1/register。Server 根据 User.Role 返回不同的 user_role，signal_agent 二进制据此决定运行模式。

CloudIDE 的两个核心能力：

1. 被 SSH 访问 — 用户通过 Desktop.Host SSH 进入 CloudIDE 使用 VSCode 等 IDE
2. SSH 出站 — 在 CloudIDE 的终端中，通过 dial 子命令 SSH 访问其他 Agent 或 K8S 集群

这两个能力都由 tsnet 连接提供，不需要 gRPC 心跳或 Agent 注册。

## 当前实现状态

| 模块                      | 状态   | 说明                                                |
| ------------------------- | ------ | --------------------------------------------------- |
| Agent 二进制复用          | 已完成 | Desktop.Pod 使用 signal_agent 二进制 RunClient 模式 |
| Deploy Token 注册         | 已完成 | 统一 deploy_tokens 机制                             |
| tsnet 用户态连接          | 已完成 | 无需 TUN 设备，无需 NET_ADMIN                       |
| Tailscale SSH 入站        | 已完成 | Desktop.Host → SSH → Desktop.Pod 可工作             |
| SIGNAL\_ 环境变量         | 已完成 | 统一前缀，仅需 SIGNAL_TOKEN + SIGNAL_SERVER         |
| 设备指纹（hostname 哈希） | 已完成 | Pod 重建不影响 Token 绑定                           |
| ACL 同用户互访            | 已完成 | Desktop.Host ↔ Desktop.Pod 天然互通                 |
| agent dial 子命令         | 已完成 | SSH 出站的核心，通过 Unix Socket 桥接 tsnet         |
| ~/.ssh/config 自动维护    | 已完成 | 静默劫持 \*.beagle 的 SSH 连接                      |
| DNS 劫持                  | 待实现 | /etc/resolv.conf 指向本地 DNS                       |
| VIP 分配                  | 待实现 | 127.1.x.x 本地地址                                  |

## 与 Desktop.Host 的差异

### DNS 配置方式

Desktop.Pod 运行在容器内，有 root 权限，通过修改 /etc/resolv.conf 指向本地 DNS 服务器：

```
Desktop.Pod 启动后：
  1. 启动本地 DNS 服务器（127.0.0.1:53）
  2. 修改 /etc/resolv.conf
       nameserver 127.0.0.1
       # 保留原有的上游 DNS 作为转发目标
  3. 本地 DNS 处理逻辑：
     ├── .beagle 后缀 → 查询 VIP 映射表，返回 127.1.x.x
     └── 其他域名 → 转发到原上游 DNS
```

### SSH 出站

Desktop.Pod 没有系统 SSH 客户端的控制权，通过 signal_agent dial 子命令和 ~/.ssh/config 实现 SSH 劫持：

```
signal_agent dial 子命令：
  signal_agent dial <target-ip> <port>
    → 连接 Agent Unix Socket
    → 通过 tsnet 隧道连接目标
    → 桥接 stdin/stdout

~/.ssh/config 自动维护：
  Host *.beagle
    ProxyCommand signal_agent dial %h %p
    StrictHostKeyChecking no

效果：
  ssh deploy@web-server-1.beijing.beagle
    → SSH 客户端读取 ~/.ssh/config
    → 调用 signal_agent dial web-server-1.beijing.beagle 22
    → DNS 劫持 → VIP → tsnet → Agent
```

### kubectl 访问

Desktop.Pod 自动配置 kubeconfig，与 Desktop.Host 的实现方式相同，都走魔法 DNS。

```
业务流程：

  1. Desktop.Pod 启动时从 Server 获取可访问的 K8S 集群列表
       → 发现 beijing 集群（AgentK8SAPI）
       → 域名: kubernetes.beijing.beagle
       → 端口: 6443

  2. Desktop.Pod 魔法 DNS 解析
       → kubernetes.beijing.beagle → 127.1.x.x（VIP 地址）
       → 本地代理监听 127.1.x.x:6443

  3. Desktop.Pod 生成 kubeconfig
       → server: https://kubernetes.beijing.beagle:6443
       → 证书验证跳过（TLS 由 tsnet 隧道保证）
       → token: 任意值（如 "whoisyourdaddy"）
       → 写入 ~/.kube/config

  4. 用户在 CloudIDE 终端执行 kubectl 命令
       → kubectl get pods -n yygl
       → 请求发送到 kubernetes.beijing.beagle:6443

  5. Desktop.Pod 魔法 DNS 劫持
       → kubernetes.beijing.beagle → 127.1.x.x
       → 本地代理接收请求（127.1.x.x:6443）

  6. Desktop.Pod 本地代理转发
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

  10. 结果返回给 Desktop.Pod → kubectl 显示
```

```
技术实现：

  Desktop.Pod 侧：
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
          写入 ~/.kube/config

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

与 Desktop.Host 的区别：

- Desktop.Host 有 GUI，用户通过资源浏览器选择集群
- Desktop.Pod 无 GUI，启动时自动配置所有可访问集群
- 两者都走魔法 DNS + VIP 分配，技术实现完全相同

### 注册方式

Desktop.Pod 使用 Deploy Token 注册，不走 Logto 登录：

```
环境变量：
  SIGNAL_TOKEN=xxx          # Deploy Token
  SIGNAL_SERVER=https://signal.example.com

启动流程：
  1. Deploy Token 注册 → 获取 auth_key
  2. tsnet 连接 Headscale
  3. 启动 DNS 劫持 + VIP 分配 + 本地代理
  4. 启动 SSH 入站（如果 SIGNAL_SSH=true）
  5. 维护 ~/.ssh/config（如果 SIGNAL_SSH_CONFIG=true）
```

## 环境变量

| 环境变量          | 默认值 | 说明                            |
| ----------------- | ------ | ------------------------------- |
| SIGNAL_TOKEN      | 必填   | Deploy Token                    |
| SIGNAL_SERVER     | 必填   | Server 地址                     |
| SIGNAL_SSH        | false  | 是否启用 SSH 入站               |
| SIGNAL_SSH_CONFIG | false  | 是否自动维护 ~/.ssh/config      |
| SIGNAL_DNS        | false  | 是否启用 DNS 劫持（新增）       |
| SIGNAL_KUBECONFIG | false  | 是否自动配置 kubeconfig（新增） |

CloudIDE 部署时推荐配置：

```
SIGNAL_TOKEN=xxx
SIGNAL_SERVER=https://signal.example.com
SIGNAL_SSH=true
SIGNAL_SSH_CONFIG=true
SIGNAL_DNS=true
SIGNAL_KUBECONFIG=true
```

## 实现优先级

| 阶段 | 内容                         | 依赖              |
| ---- | ---------------------------- | ----------------- |
| P0   | signal_agent dial 子命令     | 无（当前待实现）  |
| P0   | ~/.ssh/config 自动维护       | signal_agent dial |
| P1   | DNS 劫持（/etc/resolv.conf） | 本地 DNS 服务器   |
| P1   | VIP 分配 + 本地代理          | DNS 劫持          |
| P2   | kubeconfig 自动配置          | 域名体系          |

## kubeconfig 配置说明

### 认证机制

Desktop.Pod 访问 K8S API 的认证完全依赖 Tailscale 身份，不验证 kubeconfig 中的 token。

### 为什么必须提供 token

kubectl 是原生的 K8S 客户端工具，要求 kubeconfig 必须包含认证信息（token/cert/username+password）。如果不提供 token，kubectl 会报错拒绝执行。

但是，Agent 从 tsnet 连接中提取 Tailscale 用户身份进行认证，完全忽略 HTTP 请求中的 Authorization 头（即 kubeconfig 中的 token）。

因此，kubeconfig 中的 token 必须提供，但值可以是任意字符串。

### kubeconfig 自动生成

Desktop.Pod 启动时，如果 `SIGNAL_KUBECONFIG=true`，会自动生成 kubeconfig 文件：

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

### 多集群配置

如果用户有多个 K8S 集群的访问权限，Desktop.Pod 会自动生成包含所有集群的 kubeconfig：

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

### 使用示例

在 CloudIDE 终端中直接使用 kubectl：

```bash
# 使用默认集群
kubectl get pods -n yygl

# 切换到其他集群
kubectl get pods -n prod --context beijing-prod

# 查看所有可用集群
kubectl config get-contexts
```

### 安全性说明

1. **Tailscale 身份不可伪造**：tsnet 连接的身份由 Tailscale 网络保证，无法伪造
2. **权限由 Server 下发**：AclK8sPermission 通过 Desktop.Pod 从 Server 同步
3. **K8S RBAC 仍然生效**：通过 Impersonation 机制，K8S 的 RBAC 规则仍然应用
4. **kubeconfig token 无关紧要**：即使 token 泄露，攻击者也无法绕过 Tailscale 身份认证
5. **容器隔离**：每个 CloudIDE 容器有独立的 kubeconfig，不会互相影响
