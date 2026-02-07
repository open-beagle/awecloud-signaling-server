# CloudIDE 主机集成设计

## 背景与前提

### 已完成的工作

1. CloudIDE 复用同一个 Agent 二进制，不是精简版
2. Agent 支持环境变量启动（SIGNAL_NAME、SIGNAL_TOKEN、SIGNAL_SERVER 等），专为 CloudIDE 容器场景设计
3. 设计文档中 CloudIDE 使用 client.token 注册到 client-{username} 下，和 Desktop 同属一个 Headscale 用户
4. Client 和 Agent 本质都是 User，只是 Role 不同

### tsnet 的关键限制

Agent 使用 tsnet.Server（用户态库），不创建 TUN 设备、不注入系统路由。

| 组件    | 网络实现                 | 路由                | 效果                              |
| ------- | ------------------------ | ------------------- | --------------------------------- |
| Desktop | tailscaled（系统级 VPN） | 有 TUN 设备和路由表 | 本地 ssh/kubectl 直接通过 IP 访问 |
| Agent   | tsnet.Server（用户态库） | 无路由，仅进程内    | 只能通过 Listen/Dial API 桥接     |

Agent 的桥接方式：

- ProxyManager：tsManager.Listen（Tailscale 入站）→ 转发到本地地址
- VisitorManager：net.Listen（局域网监听）→ tsManager.Dial（Tailscale 出站）
- Tailscale SSH：tsnet 内置 tailssh 模块，不经过操作系统网络栈

### 设计决策

主机部署保持 tsnet 模式不变（零侵入，不与主机网络冲突）。CloudIDE 容器部署也使用 tsnet，通过 Agent 自动配置实现静默劫持。注意：Tailscale SSH 功能需要 root 权限（be-child ssh 需要切换用户身份），因此 Agent 需要以 sudo 启动。

## 需求 1：Desktop 发现 CloudIDE 主机

### 现状

buildHostsData 只走一条链路：SSH ACL → Agent 用户（UserRoleAgent）→ Agent 节点。CloudIDE 注册在 client-{name} 下，不在此链路中。

### 方案

在 buildHostsData 中增加第二个数据源：

```
┌──────────────────────────────────────────────────────────────┐
│                    主机列表构建流程                           │
├──────────────────────────────────────────────────────────────┤
│                                                              │
│  数据源 1（现有，不变）:                                     │
│  SSH ACL 权限 → Agent 用户 → Agent 节点                     │
│                                                              │
│  数据源 2（新增）:                                           │
│  deploy_tokens (user_id=当前用户, ssh_enabled=true,          │
│  status=bound) → Headscale 查询 client-{name} 节点 IP       │
│                                                              │
│  合并 → 统一主机列表                                         │
│                                                              │
└──────────────────────────────────────────────────────────────┘
```

### 数据模型变更

deploy_tokens 表已包含以下字段：

| 字段        | 类型 | 说明                                              |
| ----------- | ---- | ------------------------------------------------- |
| ssh_enabled | bool | 是否启用 SSH，默认 false                          |
| ssh_users   | text | SSH 用户名列表（JSON 数组，如 ["root", "coder"]） |

### 主机 ID 格式

- Agent 主机："{agent_user_id}"（现有）
- CloudIDE 主机："cloudide:{token_id}"（新增）

## 需求 2：双向 SSH

### 方向 A：Desktop → SSH → CloudIDE（入站）

已可工作，不需要改 Agent 代码。

```
Desktop (tailscaled, 有路由)
  │
  │ ssh root@100.64.1.2（走 Tailscale 网络）
  │
  ▼
CloudIDE Agent (tsnet)
  │
  │ tsnet 内置 tailssh 模块接收 SSH 连接
  │
  ▼
启动 shell（be-child ssh --shell）
```

前提条件：

- CloudIDE Agent 配置 SIGNAL_SSH=true
- ACL 规则允许同用户节点互访（需补充）

### 方向 B：CloudIDE → SSH → 其他 Agent（出站）

核心难点：tsnet 没有路由，容器内 ssh 命令不知道 100.64.x.x 怎么走。

#### 方案：Agent 静默劫持（推荐）

CloudIDE 环境完全受控，Agent 启动时自动配置一切，用户无感知。

##### 整体架构

```
CloudIDE 容器
┌─────────────────────────────────────────────────────────┐
│                                                         │
│  用户终端                                               │
│  $ ssh root@100.64.0.1                                  │
│       │                                                 │
│       │ 读取 ~/.ssh/config                              │
│       │ Host 100.64.*                                   │
│       │   ProxyCommand signaling dial %h %p             │
│       │                                                 │
│       ▼                                                 │
│  signaling dial 100.64.0.1 22                           │
│       │                                                 │
│       │ 连接 Agent 的 Unix Socket                       │
│       │ /tmp/signaling-agent.sock                       │
│       │                                                 │
│       ▼                                                 │
│  Agent 进程                                             │
│       │                                                 │
│       │ tsManager.Dial("tcp", "100.64.0.1:22")          │
│       │                                                 │
│       ▼                                                 │
│  tsnet → Tailscale 网络                                 │
│                                                         │
└─────────────────────────────────────────────────────────┘
       │
       ▼
目标 Agent (100.64.0.1:22)
```

##### Agent 启动时自动配置

Agent 在 CloudIDE 模式下启动后，自动完成以下配置：

1. 启动 Unix Socket 监听（/tmp/signaling-agent.sock）
2. 启动 SOCKS5 代理（127.0.0.1:1080），供非 SSH 程序按需使用
3. 自动写入 ~/.ssh/config：

```
# === AWECloud Signaling Agent 自动生成 ===
# 请勿手动修改此段，Agent 会自动维护
Host 100.64.*
    ProxyCommand signaling dial %h %p
    StrictHostKeyChecking no
    UserKnownHostsFile /dev/null
# === AWECloud Signaling Agent 结束 ===
```

用户打开终端，直接 ssh root@100.64.0.1 就通了。完全静默，零配置。

##### signaling dial 子命令

Agent 二进制的子命令，工作流程：

```
signaling dial <host> <port>
       │
       ├── 1. 连接 Unix Socket (/tmp/signaling-agent.sock)
       ├── 2. 发送目标地址 (host:port)
       ├── 3. Agent 进程通过 tsManager.Dial 建立连接
       ├── 4. stdin/stdout 桥接到 Tailscale 连接
       └── 5. SSH 协议在这个管道上跑
```

##### SOCKS5 代理

监听 127.0.0.1:1080，无认证。供非 SSH 程序按需手动指定：

```
curl --proxy socks5://127.0.0.1:1080 http://100.64.0.1:8080
kubectl --proxy socks5://127.0.0.1:1080 ...
```

不自动设置 ALL_PROXY 环境变量，避免劫持所有流量（容器内访问外网、k8s 内部服务等不应走代理）。

##### 环境变量配置

所有环境变量统一使用 SIGNAL\_ 前缀，详见 design_cloudide_env.md。

| 环境变量           | 默认值              | 说明                       |
| ------------------ | ------------------- | -------------------------- |
| SIGNAL_SSH_CONFIG  | false               | 是否自动维护 ~/.ssh/config |
| SIGNAL_SOCKS       | false               | 是否启用 SOCKS5 代理       |
| SIGNAL_SOCKS_ADDR  | 127.0.0.1:1080      | SOCKS5 代理监听地址        |
| SIGNAL_DIAL_SOCKET | /tmp/signaling.sock | Unix Socket 路径           |

CloudIDE 部署时设置 SIGNAL_SOCKS=true 和 SIGNAL_SSH_CONFIG=true 即可。

## ACL 规则补充

当前 ACL 生成逻辑中缺少同用户节点互访规则：

```
对每个 client 用户:
  src: tag:client-{name}
  dst: tag:client-{name}:*
```

CloudIDE 访问其他 Agent 的 SSH 权限不需要额外规则——CloudIDE 和 Desktop 同属 client-{name}，共享 ACL tag，天然继承 Desktop 的 SSH 授权。

## 实现优先级

| 优先级 | 任务                                   | 说明                             | 状态   |
| ------ | -------------------------------------- | -------------------------------- | ------ |
| P0     | ClientRegister 实现 AuthKey 创建       | 当前是 TODO，CloudIDE 注册的前提 | 已完成 |
| P0     | ACL 补充同用户互访规则                 | Desktop ↔ CloudIDE 的前提        | 已完成 |
| P1     | signaling dial 子命令 + Unix Socket    | CloudIDE 出站 SSH 的核心         | 待实现 |
| P1     | Agent 自动维护 ~/.ssh/config           | 静默劫持 SSH                     | 待实现 |
| P1     | buildHostsData 增加 CloudIDE 数据源    | Desktop 发现 CloudIDE 主机       | 待实现 |
| P1     | deploy_tokens 的 ssh_enabled/ssh_users | 标识可 SSH 的 CloudIDE           | 已完成 |
| P2     | SOCKS5 代理模块                        | 非 SSH 程序的出站访问            | 待实现 |
