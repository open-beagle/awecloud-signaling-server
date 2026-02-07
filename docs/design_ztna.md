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
│                 │          │  │ AgentSSH  AgentK8S AgentSVC│  │
│                 │          │  │                            │  │
│                 │          │  │ gRPC Server (tsnet 侧)     │  │
│                 │          │  │ gRPC Server (内网侧)       │  │
│                 │          │  └─────────┬──────────────────┘  │
│                 │          │            │ gRPC 反向连接        │
│                 │          │  ┌─────────┴──────────────────┐  │
│                 │          │  │ Endpoint (轻量 daemon)     │  │
│                 │          │  │                            │  │
│                 │          │  │ EndpointSSH (内网机器)     │  │
│                 │          │  │ EndpointK8S (K8S 主节点)   │  │
│                 │          │  │ EndpointSVC (K8S 节点)     │  │
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

能力分两层，三种能力完全对称（SSH、K8S、SVC）：

```
          Agent 本机（直连）          Endpoint 跳跃（中转）
  SSH     AgentSSH                   EndpointSSH
  K8S     AgentK8S                   EndpointK8S
  SVC     AgentSVC                   EndpointSVC
```

Agent 本机能力（配置级，不是独立对象）：

- AgentSSH — Agent 本机 SSH（Tailscale SSH，已有）
- AgentK8S — Agent 本机 K8S API 代理（Impersonation）
- AgentSVC — Agent 本机 K8S SVC 代理（DNS 发现，SVC 发现）

Endpoint 跳跃能力（独立对象 + 独立进程）：

- EndpointSSH — 内网机器上的轻量 daemon，提供 SSH 会话桥接
- EndpointK8S — 内网 K8S 主节点上的轻量 daemon，提供 K8S API 代理
- EndpointSVC — 内网 K8S 节点上的轻量 daemon，提供 K8S SVC 代理

Endpoint 不在 Tailscale 网络中，不连 Server 或 Headscale，只反向连接 Agent 的内网 gRPC 端口。

## Desktop 两种形态

| 维度      | Desktop.Host                    | Desktop.Pod                   |
| --------- | ------------------------------- | ----------------------------- |
| 运行环境  | 用户物理机                      | K8S Pod（CloudIDE 容器）      |
| 用户界面  | Wails GUI                       | 无 GUI，纯后台 daemon         |
| User.Role | client                          | client                        |
| 网络接入  | tsnet 用户态                    | tsnet 用户态                  |
| DNS 方案  | DNS 劫持 + VIP（127.1.x.x）     | DNS 劫持 + VIP（127.1.x.x）   |
| DNS 配置  | /etc/resolver/k8s (macOS) 等    | /etc/resolv.conf 指向本地 DNS |
| 注册方式  | Logto 登录 → Device Token       | Deploy Token → auth_key       |
| 权限      | Client 的一切权限               | Client 的一切权限             |
| 二进制    | desktop（Wails 应用，独立仓库） | agent 二进制的 RunClient 模式 |

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
│  第 3 层：Agent 本机 K8S API（Impersonation）                    │
│  待实现。Agent 部署在主节点时，直接代理 K8S API 请求。          │
│  规则来源：AclK8sUserPermission / AclK8sGroupPermission         │
│                                                                 │
│  第 4 层：Endpoint 跳跃（Agent 中转到内网）                      │
│  待实现。通过 Agent 跳跃到内网的 Endpoint。                     │
│  规则来源：                                                      │
│    AclSSHJumpUserPermission / AclSSHJumpGroupPermission         │
│    AclK8sJumpUserPermission / AclK8sJumpGroupPermission         │
│    AclSVCJumpUserPermission / AclSVCJumpGroupPermission         │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

## 域名体系

统一使用 `.k8s` 后缀（可配置），Desktop DNS 劫持拦截：

```
Agent 本机（AgentSSH 直连）：
  <agent-name>.k8s:22
  beijing.k8s:22

Agent 本机（AgentK8S 直连）：
  api.<agent-name>.k8s:6443
  api.beijing.k8s:6443

Agent 本机（AgentSVC 直连）：
  <service>.<namespace>.<agent-name>.k8s
  pg.yygl.beijing.k8s:5432

EndpointSSH（跳跃）：
  <endpoint-name>.<agent-name>.k8s:22
  web-server-1.beijing.k8s:22

EndpointK8S（跳跃）：
  api.<endpoint-name>.<agent-name>.k8s:6443
  api.beijing-prod.beijing.k8s:6443

EndpointSVC（跳跃）：
  <service>.<namespace>.<endpoint-name>.<agent-name>.k8s
  pg.yygl.remote-cluster.beijing.k8s:5432
```

## 实现优先级

| 阶段 | 内容                                    | 依赖              |
| ---- | --------------------------------------- | ----------------- |
| P0   | Desktop tsnet 化（去掉 tailscaled）     | 无                |
| P0   | DNS 劫持 + VIP 分配                     | Desktop tsnet     |
| P0   | 域名体系设计和实现                      | DNS 劫持          |
| P1   | AgentK8S — K8S API 代理 + Impersonation | 权限模型          |
| P1   | AgentSVC — K8S Service 自动发现和代理   | K8S RBAC          |
| P2   | Endpoint 体系（SSH/K8S/SVC 跳跃）       | Agent gRPC Server |
| P2   | AI 安全审计（指令拦截、会话录像）       | Agent 网关层      |
| P3   | Vaultwarden 集成（远期）                | 独立部署          |

## 相关设计文档

- design_ztna_server.md — Server 端变更
- design_ztna_web.md — Web 管理界面变更
- design_ztna_agent.md — Agent 能力对象设计（AgentSSH/AgentK8S/AgentSVC）
- design_ztna_endpoint.md — Endpoint 跳跃端点设计（EndpointSSH/EndpointK8S/EndpointSVC）
- design_ztna_desktop.md — Desktop 共通设计（.Host 和 .Pod）
- design_ztna_desktop_host.md — Desktop.Host 桌面客户端重构
- design_ztna_desktop_pod.md — Desktop.Pod 容器内 daemon
