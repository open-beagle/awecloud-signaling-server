# ZTNA 零信任访问体系设计

## 概述

ZTNA（Zero Trust Network Access）是 AWECloud Signaling 的下一代架构演进方向。核心理念：身份即边界，域名即路由，凭据不落地，AI 可审计。

## 与现有架构的关系

ZTNA 不是推翻重来，而是在现有 Tailscale/Headscale 隧道基础上的能力升级。

| 维度     | 当前架构                          | ZTNA 架构                                 |
| -------- | --------------------------------- | ----------------------------------------- |
| Desktop  | tailscaled 系统级 VPN，注入路由表 | tsnet 用户态，DNS 劫持 + 本地 VIP         |
| 访问方式 | ssh root@100.64.x.x               | ssh user@node-1.beijing.k8s               |
| 端口冲突 | 靠 Tailscale IP 段隔离            | 靠 VIP（127.1.x.x）隔离，同端口不冲突     |
| 凭据管理 | 用户自己管 SSH 密钥               | Endpoint 侧管理，凭据不落地               |
| 身份传递 | 设备 Token + Logto 登录           | tsnet 连接携带身份，Agent 从连接提取      |
| Agent    | 单一角色，内网隧道端点            | 能力对象模型，挂载 SSH/K8S/SVC 能力       |
| Endpoint | 无                                | 内网轻量 daemon，反向连接 Agent           |
| Desktop  | 仅桌面应用                        | Desktop.Host（桌面）+ Desktop.Pod（容器） |

## 身份传递机制

不需要 Identity Token。Tailscale 连接本身携带身份。

tsnet 的每个连接都能拿到对端的节点名、用户名、IP。Agent 收到连接后直接提取：

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
  └── 根据身份查询权限（向 Server 查询或本地缓存）
```

Headscale ACL 保证了只有授权节点才能建立连接，Agent 只需从连接中提取身份即可。

## "凭据不落地"范围

| 凭据类型   | 是否纳入 ZTNA 管理 | 说明                                    |
| ---------- | ------------------ | --------------------------------------- |
| SSH 密钥   | 是                 | Endpoint 侧管理，用户不需要持有私钥     |
| kubeconfig | 是                 | 主节点上已有，Endpoint 做 Impersonation |
| 数据库密码 | 否                 | 用户自己记，ZTNA 不管                   |
| API Key 等 | 否                 | 用户自己管理                            |

Vaultwarden 集成降为远期目标，当前阶段不需要。

## 核心组件

```
┌─────────────────────────────────────────────────────────────────┐
│                        Server（公有云）                          │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────────┐   │
│  │ REST API │  │  gRPC    │  │ Web 管理 │  │  Headscale   │   │
│  │ (Gin)    │  │  服务    │  │ (Vue 3)  │  │  控制平面    │   │
│  └──────────┘  └──────────┘  └──────────┘  └──────────────┘   │
└─────────────────────────────────────────────────────────────────┘
         │ gRPC                          │ gRPC
         ▼                               ▼
┌─────────────────┐          ┌──────────────────────────────────┐
│  Desktop        │          │         内网环境                  │
│  (client 角色)  │          │                                  │
│                 │          │  ┌────────────────────────────┐  │
│  .Host (桌面)   │──tsnet──▶│  │ Agent (agent 角色)         │  │
│  .Pod  (容器)   │          │  │                            │  │
│                 │          │  │ AgentSSH / AgentK8SAPI    │  │
│  │ AgentK8SService           │  │
│  │ AgentService              │  │
│                 │          │  │                            │  │
│                 │          │  │ gRPC Server (tsnet 侧)     │  │
│                 │          │  │ gRPC Server (内网侧)       │  │
│                 │          │  └─────────┬──────────────────┘  │
│                 │          │            │ gRPC 反向连接        │
│                 │          │  ┌─────────┴──────────────────┐  │
│                 │          │  │ Endpoint (轻量 daemon)     │  │
│                 │          │  │                            │  │
│                 │          │  │ EndpointSSH (内网机器)     │  │
│                 │          │  │ EndpointK8SAPI (主节点)    │  │
│                 │          │  │ EndpointK8SService (节点)  │  │
│                 │          │  └────────────────────────────┘  │
│                 │          │                                  │
└─────────────────┘          └──────────────────────────────────┘
```

四种角色：

| 角色     | User.Role | 连接对象             | 说明                                   |
| -------- | --------- | -------------------- | -------------------------------------- |
| Server   | —         | Headscale, Logto, DB | 控制面，不参与数据面                   |
| Agent    | agent     | Server (gRPC), tsnet | 内网服务提供者，挂载能力对象           |
| Desktop  | client    | Server (gRPC), tsnet | 用户访问入口，.Host 或 .Pod            |
| Endpoint | —         | Agent (gRPC，内网)   | 内网轻量 daemon，不连 Server/Headscale |

## Agent 能力对象模型

Agent 就是 Agent，不区分类型。通过配置和挂载的能力对象决定它能做什么。任何 Agent 都可以挂载任意能力，只要网络可达。

能力分两层，四种 Agent 能力 + 三种 Endpoint 跳跃能力：

```
          Agent 本机（直连）              Endpoint 跳跃（中转）
  SSH     AgentSSH                       EndpointSSH
  K8SAPI  AgentK8SAPI                    EndpointK8SAPI
  K8SSvc  AgentK8SService                EndpointK8SService
  手动    AgentService（原 ProxyService）  —
```

Agent 本机能力（配置级，不是独立对象）：

- AgentSSH — Agent 本机 SSH（Tailscale SSH，已有）
- AgentK8SAPI — Agent 本机 K8S API 代理（Impersonation，新增）
- AgentK8SService — Agent 本机 K8S Service 自动发现和代理（新增）
- AgentService — Agent 手动端口映射（现有 ProxyService 改名）

Endpoint 跳跃能力（独立对象 + 独立进程）：

- EndpointSSH — 内网机器上的轻量 daemon，提供 SSH 会话桥接
- EndpointK8SAPI — 内网 K8S 主节点上的轻量 daemon，提供 K8S API 代理
- EndpointK8SService — 内网 K8S 节点上的轻量 daemon，提供 K8S Service 代理

Endpoint 不在 Tailscale 网络中，不连 Server 或 Headscale，只反向连接 Agent 的内网 gRPC 端口。

## Desktop 两种形态

| 维度      | Desktop.Host                           | Desktop.Pod                          |
| --------- | -------------------------------------- | ------------------------------------ |
| 运行环境  | 用户物理机                             | K8S Pod（CloudIDE 容器）             |
| 用户界面  | Wails GUI                              | 无 GUI，纯后台 daemon                |
| User.Role | client                                 | client                               |
| 网络接入  | tsnet 用户态                           | tsnet 用户态                         |
| DNS 方案  | DNS 劫持 + VIP（127.1.x.x）            | DNS 劫持 + VIP（127.1.x.x）          |
| DNS 配置  | /etc/resolver/k8s (macOS) 等           | /etc/resolv.conf 指向本地 DNS        |
| 注册方式  | Logto 登录 → Device Token              | Deploy Token → auth_key              |
| 权限      | Client 的一切权限                      | Client 的一切权限                    |
| 二进制    | signal_desktop（Wails 应用，独立仓库） | signal_agent 二进制的 RunClient 模式 |

两者都是 Client 角色，权限完全相同。

## 四层权限控制体系

```
┌─────────────────────────────────────────────────────────────────┐
│                    ZTNA 权限控制四层模型                          │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  第 1 层：网络可达性（Headscale ACL）                            │
│  已实现。决定 Desktop 能不能连到 Agent。                        │
│  规则来源：AclServicePermission / AclUserPermission /           │
│           AclGroupPermission / 同用户互访                       │
│                                                                 │
│  第 2 层：Agent 本机 SSH（Tailscale SSH ACL）                    │
│  已实现。决定谁能 SSH 到 Agent 节点，用哪个 Linux 用户。        │
│  规则来源：AclSSHUserPermission / AclSSHGroupPermission         │
│                                                                 │
│  第 3 层：Agent 本机应用层（Agent 本地鉴权）                     │
│  待实现。AgentK8SAPI 代理 K8S API 请求，AgentK8SService 代理    │
│  自动发现的 K8S Service。Agent 从 tsnet 提取身份后本地鉴权。    │
│  规则来源：AclK8sPermission / AclK8SServicePermission           │
│                                                                 │
│  第 4 层：Endpoint 跳跃（Agent 中转到内网）                      │
│  待实现。通过 Agent 跳跃到内网的 Endpoint。                     │
│  规则来源：                                                      │
│    AclSSHJumpPermission / AclK8SAPIJumpPermission /             │
│    AclK8SServiceJumpPermission                                  │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

## 域名体系

统一使用 `.k8s` 后缀（可配置），Desktop DNS 劫持拦截：

```
Agent 本机（AgentSSH 直连）：
  <agent-name>.k8s:22
  beijing.k8s:22

Agent 本机（AgentK8SAPI 直连）：
  api.<agent-name>.k8s:6443
  api.beijing.k8s:6443

Agent 本机（AgentK8SService 直连）：
  <service>.<namespace>.<agent-name>.k8s
  pg.yygl.beijing.k8s:5432

EndpointSSH（跳跃）：
  <endpoint-name>.<agent-name>.k8s:22
  web-server-1.beijing.k8s:22

EndpointK8SAPI（跳跃）：
  api.<endpoint-name>.<agent-name>.k8s:6443
  api.beijing-prod.beijing.k8s:6443

EndpointK8SService（跳跃）：
  <service>.<namespace>.<endpoint-name>.<agent-name>.k8s
  pg.yygl.remote-cluster.beijing.k8s:5432
```

## 二进制和构建产物

ZTNA 体系共 4 个二进制，统一 signal\_ 前缀：

| 二进制          | 源码位置             | 构建产物命名                         | 角色                  |
| --------------- | -------------------- | ------------------------------------ | --------------------- |
| signal_server   | cmd/server           | bin/signal_server-{os}-{arch}        | Server 控制面         |
| signal_agent    | cmd/agent            | bin/signal_agent-{os}-{arch}         | Agent + Desktop.Pod   |
| signal_desktop  | desktop/（独立仓库） | bin/signal_desktop-{ver}-{os}-{arch} | Desktop.Host 桌面应用 |
| signal_endpoint | cmd/endpoint         | bin/signal_endpoint-{os}-{arch}      | Endpoint 轻量 daemon  |

说明：

- signal_agent 同时承担 Agent 和 Desktop.Pod 两个角色。通过 Deploy Token 注册时，Server 返回 user_role，signal_agent 根据 role 决定运行模式：role=agent 走 Agent 模式（gRPC 心跳 + ProxyManager），role=client 走 Client 模式（tsnet + DNS 劫持，即 Desktop.Pod）。不需要单独的 desktop_pod 二进制。
- signal_endpoint 是 ZTNA 新增的二进制（P2 阶段）。一个 signal_endpoint 二进制通过配置决定启用哪些能力（SSH/K8SAPI/K8SService），不按能力拆分二进制。
- signal_desktop 是 Wails 桌面应用，独立仓库独立构建，产物是平台原生格式（Windows .exe、macOS .app、Linux 可执行文件）。

## 实现优先级

| 阶段 | 内容                                         | 依赖              |
| ---- | -------------------------------------------- | ----------------- |
| P0   | Desktop tsnet 化（去掉 tailscaled）          | 无                |
| P0   | DNS 劫持 + VIP 分配                          | Desktop tsnet     |
| P0   | 域名体系设计和实现                           | DNS 劫持          |
| P1   | AgentK8SAPI — K8S API 代理 + Impersonation   | 权限模型          |
| P1   | AgentK8SService — K8S Service 自动发现和代理 | K8S RBAC          |
| P2   | Endpoint 体系（SSH/K8SAPI/K8SService 跳跃）  | Agent gRPC Server |
| P2   | AI 安全审计（指令拦截、会话录像）            | Agent 网关层      |
| P3   | Vaultwarden 集成（远期）                     | 独立部署          |

## 相关设计文档

- design_ztna_acl.md — 授权管理设计（7 种授权的完整梳理）
- design_ztna_server.md — Server 端变更
- design_ztna_web.md — Web 管理界面变更
- design_ztna_agent.md — Agent 能力对象设计（AgentSSH/AgentK8SAPI/AgentK8SService/AgentService）
- design_ztna_endpoint.md — Endpoint 跳跃端点设计（EndpointSSH/EndpointK8SAPI/EndpointK8SService）
- design_ztna_desktop.md — Desktop 共通设计（.Host 和 .Pod）
- design_ztna_desktop_host.md — Desktop.Host 桌面客户端重构
- design_ztna_desktop_pod.md — Desktop.Pod 容器内 daemon
