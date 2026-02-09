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
  │     └── 拦截 .k8s 域名解析
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
创建 /etc/resolver/k8s：
  nameserver 127.0.0.1
  port 15353

效果：
  所有 .k8s 域名查询发送到 127.0.0.1:15353
  其他域名不受影响
```

### Linux

```
方案 1：systemd-resolved（推荐）
  配置 .k8s 域名路由到本地 DNS

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
│  │   ├── 🖥 beijing.k8s                         │    │
│  │   │     SSH (22)  ● 在线                     │    │
│  │   ├── 🗄 pg.yygl.beijing.k8s                 │    │
│  │   │     PostgreSQL (5432)  ● 在线            │    │
│  │   ├── 🖥 web-server-1.beijing.k8s            │    │
│  │   │     EndpointSSH (22)  ● 在线             │    │
│  │   └── 🔧 api.beijing-prod.beijing.k8s        │    │
│  │         EndpointK8SAPI (6443)  ● 在线        │    │
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
