# ZTNA Desktop 设计（共通部分）

## 概述

Desktop 是 ZTNA 架构中用户侧的核心组件，以 client 角色加入 Tailscale 网络。Desktop 有两种形态：

- Desktop.Host — 桌面应用（macOS/Windows/Linux），Wails GUI
- Desktop.Pod — Pod 内 daemon（CloudIDE 容器），无 GUI

两者权限完全相同，都是 Client 角色。本文档描述两者的共同设计，各自的差异见：

- design_ztna_desktop_host.md — Desktop.Host 桌面客户端重构
- design_ztna_desktop_pod.md — Desktop.Pod 容器内 daemon

## 两种形态对比

| 维度      | Desktop.Host                           | Desktop.Pod                          |
| --------- | -------------------------------------- | ------------------------------------ |
| 运行环境  | 用户物理机                             | K8S Pod（CloudIDE 容器）             |
| 用户界面  | Wails GUI                              | 无 GUI，纯后台 daemon                |
| User.Role | client                                 | client                               |
| 网络接入  | tsnet 用户态                           | tsnet 用户态                         |
| DNS 方案  | DNS 劫持 + VIP（127.1.x.x）            | DNS 劫持 + VIP（127.1.x.x）          |
| DNS 配置  | /etc/resolver/k8s (macOS) 等           | /etc/resolv.conf 指向本地 DNS        |
| SSH 出站  | 系统 SSH 客户端                        | signal_agent dial + ~/.ssh/config    |
| 注册方式  | Logto 登录 → Device Token              | Deploy Token → auth_key              |
| 权限      | Client 的一切权限                      | Client 的一切权限                    |
| 二进制    | signal_desktop（Wails 应用，独立仓库） | signal_agent 二进制的 RunClient 模式 |

## 共同能力

两者都能做以下事情：

```
Desktop（.Host 和 .Pod 共同）：
  ├── User.Role = "client"
  ├── tsnet 加入 Tailscale 网络
  ├── 从 tsnet 连接携带身份（tag:client-{name}）
  ├── 受 Headscale ACL 控制（第 1 层）
  ├── 可 SSH 直连 Agent（第 2 层）
  ├── 可访问 AgentK8SAPI（第 3 层）
  ├── 可访问 AgentK8SService（第 3 层）
  ├── 可访问 AgentService（第 1 层）
  ├── 可通过 Agent gRPC 跳跃到 Endpoint（第 4 层）
  └── 同用户多设备互访（Desktop.Host ↔ Desktop.Pod）
```

## 共同的网络架构

### tsnet 用户态

两者都使用 tsnet 用户态连接，不需要 TUN 设备、不需要管理员权限：

```
tsnet.Server 初始化参数：

  Hostname:   设备名
  Dir:        状态目录
  ControlURL: Headscale 地址
  AuthKey:    认证密钥
  Ephemeral:  false（需要持久化节点）
```

### DNS 劫持 + VIP

两者都使用 DNS 劫持拦截 `.k8s` 后缀域名，返回本地 VIP 地址（127.1.x.x）：

```
DNS 劫持流程：

  1. 启动本地 DNS 服务器
  2. 配置系统 DNS，将 .k8s 域名指向本地 DNS
  3. 本地 DNS 处理逻辑：
     ├── .k8s 后缀 → 查询 VIP 映射表，返回 127.1.x.x
     └── 其他域名 → 转发到上游 DNS
```

区别仅在于 DNS 配置方式：

| 形态         | DNS 配置方式                                                              |
| ------------ | ------------------------------------------------------------------------- |
| Desktop.Host | /etc/resolver/k8s (macOS)、systemd-resolved (Linux)、网络适配器 (Windows) |
| Desktop.Pod  | /etc/resolv.conf 指向本地 DNS（容器内有 root 权限）                       |

### VIP 分配

```
127.1.0.0/16 地址段：

  127.1.0.1   — 第一个服务
  127.1.0.2   — 第二个服务
  ...
  127.1.255.254 — 最多 65534 个服务

每个 VIP 对应一个远程服务的完整地址（Agent IP + 端口）
```

端口冲突解决：

```
VIP 方式（无冲突）：
  127.1.0.1:5432 → pg.yygl.beijing.k8s:5432    ✓
  127.1.0.2:5432 → pg.prod.shanghai.k8s:5432   ✓ 不冲突
```

### 本地代理

Desktop 在每个 VIP 地址上启动本地 TCP 代理，将流量通过 tsnet 转发到目标 Agent：

```
用户连接 127.1.0.1:5432
  │
  ▼
本地代理（监听 127.1.0.1:5432）
  │
  ├── 查询映射表
  │     127.1.0.1:5432 → Agent 100.64.0.1:5432
  │
  ├── 通过 tsnet Dial 连接 Agent
  │
  └── 双向转发数据
        用户 ←→ 本地代理 ←→ tsnet ←→ Agent ←→ K8S Service
```

按需启动：DNS 查询触发代理启动，空闲 30 分钟自动关闭。

## 共同的访问方式

```
SSH 直连 Agent：
  ssh user@beijing.k8s

SSH 跳跃到 Endpoint：
  ssh deploy@web-server-1.beijing.k8s

SVC 直连 Agent：
  psql -h pg.yygl.beijing.k8s -p 5432

SVC 跳跃到 Endpoint：
  psql -h pg.yygl.remote-cluster.beijing.k8s -p 5432

K8S API 直连 Agent：
  kubectl --server=https://api.beijing.k8s:6443 get pods

K8S API 跳跃到 Endpoint：
  kubectl --server=https://api.beijing-prod.beijing.k8s:6443 get pods
```

## 身份传递

不需要 Identity Token。tsnet 连接本身携带身份。

```
Desktop 登录 Logto（.Host）或 Deploy Token 注册（.Pod）
  → Server 创建/查找 User (client-zhangsan)
  → Headscale 注册节点，分配 Tag (tag:client-zhangsan)
  → Desktop 加入 Tailscale 网络

Desktop 访问 Agent
  → Tailscale ACL 检查（第 1 层）
  → 连接建立，Agent 从 tsnet 获取对端身份
  → Agent 知道是 client-zhangsan 在访问
```

## 实现优先级

| 阶段 | 内容                         | 依赖              |
| ---- | ---------------------------- | ----------------- |
| P0   | tsnet 替换 tailscaled        | 无                |
| P0   | DNS 劫持模块                 | tsnet 迁移        |
| P0   | VIP 分配器                   | DNS 劫持          |
| P0   | 本地代理（VIP → tsnet Dial） | VIP 分配          |
| P0   | 域名体系集成                 | Server 域名注册表 |
| P1   | 资源发现 UI（仅 .Host）      | 资源发现 API      |
