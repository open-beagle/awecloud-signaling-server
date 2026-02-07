# CloudIDE 集成设计

## 概述

CloudIDE 通过复用 Agent 二进制，以 Client 身份加入 Tailscale 网络，实现与 Desktop 相同的内网访问能力。

相关文档：

- design_cloudide_env.md — 环境变量设计
- design_cloudide_host.md — 主机集成（Desktop 发现 CloudIDE、双向 SSH）
- design_cloudide_install.md — 安装部署

## 核心思路

CloudIDE 和 Desktop 本质相同——都是用户设备，只是部署形态不同：

| 特性     | Desktop              | CloudIDE                                 |
| -------- | -------------------- | ---------------------------------------- |
| 部署形态 | 桌面应用             | 容器内二进制                             |
| 认证方式 | Logto 网页登录       | 部署 Token（统一，无前缀区分）           |
| 网络实现 | tailscaled（系统级） | tsnet（用户态），SSH 需 sudo 启动        |
| 设备名   | 用户手动输入         | 自动使用容器 hostname                    |
| 配置方式 | GUI 界面             | 环境变量（SIGNAL_TOKEN + SIGNAL_SERVER） |

## 部署 Token 模型

Token 已统一管理，Agent 和 Client 共用同一套 deploy_tokens 机制。管理员在 Server Web 界面为用户生成部署 Token，配置到 CloudIDE 环境变量。User.Role 决定注册时的 Headscale 行为。

详细数据模型和统一方案见 design_cloudide_upgrade.md。

### Token 生命周期

```
┌─────────┐   首次注册（任意设备）   ┌─────────┐
│ pending │ ────────────────────▶  │  bound  │
│ 待使用  │                         │ 已绑定  │
└─────────┘                         └────┬────┘
                                         │
                                         │ 绑定设备指纹（hostname 哈希）
                                         │ 永久有效
                                         │ 仅限该设备使用
                                         ▼
                                    同设备可重复使用
                                    (Pod 重建/升级/漂移)

管理员可随时撤销:
  pending ──▶ revoked
  bound   ──▶ revoked
```

Agent Token 有 24 小时首次使用时限，Client Token 无时限。首次使用后均绑定设备指纹。管理员可随时撤销。

### 设备指纹设计

所有场景统一使用 hostname 的 SHA256 哈希作为设备指纹：

```
指纹 = SHA256(hostname)
```

hostname 在各场景下都稳定：

| 场景              | hostname 来源                    | 稳定性                           |
| ----------------- | -------------------------------- | -------------------------------- |
| Desktop           | 用户机器名                       | 用户不改就不变                   |
| Agent（物理机）   | 主机名                           | 运维不改就不变                   |
| Agent（k8s 容器） | Pod 名（Deployment/StatefulSet） | 由编排系统控制，稳定             |
| CloudIDE          | 平台分配的 Pod 名                | 与用户工作空间绑定，Pod 重建不变 |

不使用 machine-id 的原因：容器重建后 /etc/machine-id 会重新生成，导致指纹变化，Token 失效。统一用 hostname 避免了这个问题，同时保持了设备绑定的安全性——Token 首次使用后绑定到特定 hostname，其他设备无法复用。

## 注册流程

Agent 和 Client 使用统一注册接口 POST /api/v1/register，Server 根据 User.Role 分支 Headscale 逻辑。

```
┌──────────────┐     ┌──────────────┐     ┌──────────────┐
│   CloudIDE   │     │    Server    │     │  Headscale   │
│   Agent      │     │              │     │              │
└──────┬───────┘     └──────┬───────┘     └──────┬───────┘
       │                    │                    │
       │ 1. POST /api/v1/register                │
       │    token + fingerprint + hostname        │
       │───────────────────▶│                    │
       │                    │                    │
       │                    │ 2. 验证 Token      │
       │                    │    查找关联用户    │
       │                    │    绑定设备指纹    │
       │                    │                    │
       │                    │ 3. 根据 User.Role  │
       │                    │    分支处理        │
       │                    │                    │
       │                    │ Agent:             │
       │                    │  GetUser agent-{n} │
       │                    │  CreatePreAuthKey  │
       │                    │                    │
       │                    │ Client:            │
       │                    │  GetOrCreateUser   │
       │                    │  client-{name}     │
       │                    │  CreatePreAuthKey  │
       │                    │  带 Tag 列表       │
       │                    │───────────────────▶│
       │                    │                    │
       │ 4. 返回            │                    │
       │    auth_key +      │                    │
       │    headscale_url + │                    │
       │    user_name +     │                    │
       │    user_role       │                    │
       │◀───────────────────│                    │
       │                    │                    │
       │ 5. 启动 tsnet      │                    │
       │    加入网络        │                    │
       │────────────────────┼───────────────────▶│
```

旧版 Agent 仍可调用 POST /api/v1/agent/register（兼容接口，内部转发到统一逻辑）。

## Headscale 用户结构

```
用户: client-userA
  ├── 节点: desktop-userA-macbook (100.64.1.1)    ← Desktop 登录
  ├── 节点: cloudide-pod-abc (100.64.1.2)         ← CloudIDE (deploy token)
  └── 节点: kubectl-laptop (100.64.1.3)           ← kubectl ts (deploy token)

用户: client-userB
  ├── 节点: desktop-userB-windows (100.64.2.1)
  └── 节点: cloudide-pod-xyz (100.64.2.2)

用户: agent-beijing（Agent 专用账户）
  └── 节点: agent-beijing (100.64.0.1)
```

同一用户的 Desktop 和 CloudIDE 共享 ACL Tag（tag:client-{name}），天然继承相同的 Agent 访问权限。不同用户之间天然隔离。

## API 设计

### 统一注册

```
POST /api/v1/register
请求: { "token": "xxx...", "device_fingerprint": "sha256...", "device_name": "pod-abc" }
响应: { "user_role": "agent|client", "auth_key": "tskey-auth-xxx...", "headscale_url": "https://...", "user_name": "...", "config": {...} }
```

### 创建部署 Token（管理员）

```
POST /api/v1/admin/users/:id/deploy-token
请求: { "name": "备注" }
响应: { "token": "xxx...", "expires_at": "...", "install_command": "...", "env_config": "..." }
```

### Token 管理（管理员）

```
GET    /api/v1/admin/users/:id/deploy-tokens?page=1&size=20
GET    /api/v1/admin/deploy-tokens/:token_id/command
DELETE /api/v1/admin/deploy-tokens/:token_id
```

## 实现状态

| 模块                           | 状态   |
| ------------------------------ | ------ |
| 统一 deploy_tokens 数据模型    | 已完成 |
| 统一注册接口 POST /register    | 已完成 |
| 统一 Token 管理 API            | 已完成 |
| 旧版 Agent 注册兼容接口        | 已完成 |
| ACL 同用户互访规则             | 已完成 |
| SIGNAL\_ 环境变量统一          | 已完成 |
| 设备指纹（hostname 哈希）      | 已完成 |
| 前端 Token 管理统一            | 已完成 |
| signaling dial 子命令          | 待实现 |
| Agent 自动维护 ssh config      | 待实现 |
| buildHostsData CloudIDE 数据源 | 待实现 |
| SOCKS5 代理模块                | 待实现 |
