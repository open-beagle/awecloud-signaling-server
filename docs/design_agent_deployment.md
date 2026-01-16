# Agent 部署模式设计

> 版本：v0.1.0

## 1. 概述

Agent 支持多种部署模式，不同模式适用于不同场景，功能支持也有差异。

## 2. 部署模式对比

| 部署模式   | 适用场景                | SSH 到 Agent | TCP 代理转发 | 升级方式   |
| ---------- | ----------------------- | ------------ | ------------ | ---------- |
| Systemd    | 需要 SSH 功能的服务器   | ✅ 支持      | ✅ 支持      | 自动/手动  |
| Docker     | 纯 TCP 代理场景         | ❌ 不支持    | ✅ 支持      | 镜像更新   |
| Kubernetes | 云原生环境，纯 TCP 代理 | ❌ 不支持    | ✅ 支持      | Deployment |

## 3. Systemd 部署模式

### 3.1 适用场景

- 需要 SSH 到 Agent 本机的场景
- 需要完整系统管理能力
- 长期运行的物理服务器或虚拟机

### 3.2 为什么 SSH 需要 Systemd 部署

Tailscale SSH 的工作原理：

```
┌─────────────────────────────────────────────────────────────────────────────┐
│  Tailscale SSH 需要访问宿主机资源                                            │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  ┌────────────────────────────────────────────────────────────────────┐    │
│  │                      Tailscale SSH 服务                             │    │
│  │                                                                     │    │
│  │  1. 读取 /etc/passwd 获取用户列表                                   │    │
│  │  2. 切换到目标用户（setuid）                                        │    │
│  │  3. 启动用户 Shell（/bin/bash）                                     │    │
│  │  4. 设置用户环境变量（HOME, PATH 等）                               │    │
│  │  5. 访问用户 home 目录                                              │    │
│  └────────────────────────────────────────────────────────────────────┘    │
│                                                                             │
│  Docker 容器的限制：                                                        │
│  ├─ 容器内用户与宿主机隔离                                                  │
│  ├─ 无法切换到宿主机用户                                                    │
│  ├─ 文件系统隔离，无法访问宿主机目录                                        │
│  └─ SSH 进去只能看到容器环境，无法管理宿主机                                │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 3.3 安装流程

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                      Systemd 安装流程                                        │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  1. 下载安装脚本                                                            │
│     curl -fsSL https://example.com/install.sh | bash                        │
│                                                                             │
│  2. 脚本执行步骤                                                            │
│     ├─ 检测系统架构（amd64/arm64）                                          │
│     ├─ 下载对应架构的 Agent 二进制                                          │
│     ├─ 安装到 /usr/local/bin/awecloud-agent                                │
│     ├─ 创建配置目录 /etc/awecloud/                                          │
│     ├─ 生成默认配置 /etc/awecloud/agent.toml                               │
│     ├─ 创建数据目录 /var/lib/awecloud/                                      │
│     ├─ 创建日志目录 /var/log/awecloud/                                      │
│     ├─ 安装 systemd 服务文件                                                │
│     └─ 启用并启动服务                                                       │
│                                                                             │
│  3. 配置 Agent                                                              │
│     编辑 /etc/awecloud/agent.toml，填入 Server 地址和认证信息               │
│                                                                             │
│  4. 重启服务                                                                │
│     systemctl restart awecloud-agent                                        │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 3.4 目录结构

| 路径                                       | 说明             |
| ------------------------------------------ | ---------------- |
| /usr/local/bin/awecloud-agent              | Agent 二进制文件 |
| /etc/awecloud/agent.toml                   | 配置文件         |
| /var/lib/awecloud/                         | 数据目录         |
| /var/log/awecloud/agent.log                | 日志文件         |
| /etc/systemd/system/awecloud-agent.service | Systemd 服务文件 |

### 3.5 Systemd 服务文件

```
[Unit]
Description=AWECloud Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/awecloud-agent -c /etc/awecloud/agent.toml
Restart=always
RestartSec=5
StandardOutput=append:/var/log/awecloud/agent.log
StandardError=append:/var/log/awecloud/agent.log

# 安全加固
NoNewPrivileges=false
ProtectSystem=full
ProtectHome=read-only

[Install]
WantedBy=multi-user.target
```

注意：`NoNewPrivileges=false` 是必须的，因为 Tailscale SSH 需要 setuid 切换用户。

### 3.6 升级机制

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                      Agent 升级流程                                          │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  方式一：自动升级（推荐）                                                    │
│  ─────────────────────                                                      │
│                                                                             │
│  ┌─────────┐    检查版本    ┌─────────┐    有新版本    ┌─────────┐         │
│  │  Agent  │ ─────────────▶ │ Server  │ ─────────────▶ │  Agent  │         │
│  │ 定时器  │                │  API    │                │ 下载更新 │         │
│  └─────────┘                └─────────┘                └────┬────┘         │
│                                                              │              │
│                                                              ▼              │
│                                                        ┌─────────┐         │
│                                                        │ 替换二进制│         │
│                                                        │ 重启服务 │         │
│                                                        └─────────┘         │
│                                                                             │
│  自动升级流程：                                                              │
│  1. Agent 定期（如每小时）向 Server 查询最新版本                             │
│  2. 对比当前版本，如有更新则下载新版本                                       │
│  3. 校验文件完整性（SHA256）                                                 │
│  4. 下载到临时目录                                                          │
│  5. 停止当前服务                                                            │
│  6. 备份旧版本                                                              │
│  7. 替换二进制文件                                                          │
│  8. 启动新版本                                                              │
│  9. 健康检查，失败则回滚                                                    │
│                                                                             │
│  方式二：手动升级                                                            │
│  ─────────────                                                              │
│                                                                             │
│  curl -fsSL https://example.com/install.sh | bash -s -- --upgrade           │
│                                                                             │
│  或者：                                                                      │
│  systemctl stop awecloud-agent                                              │
│  wget https://example.com/agent-linux-amd64 -O /usr/local/bin/awecloud-agent│
│  chmod +x /usr/local/bin/awecloud-agent                                     │
│  systemctl start awecloud-agent                                             │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 3.7 版本检查 API

| 接口                   | 方法 | 说明                |
| ---------------------- | ---- | ------------------- |
| /api/v1/agent/version  | GET  | 获取最新 Agent 版本 |
| /api/v1/agent/download | GET  | 下载 Agent 二进制   |

版本响应示例：

```
{
  "version": "1.2.0",
  "release_date": "2024-01-15",
  "download_url": "/api/v1/agent/download?arch=amd64",
  "checksum": "sha256:abc123...",
  "changelog": "修复了 xxx 问题"
}
```

## 4. Docker 部署模式

### 4.1 适用场景

- 只需要 TCP 代理转发功能
- 不需要 SSH 到 Agent 本机
- 容器化环境

### 4.2 功能限制

| 功能         | 支持情况  | 说明                          |
| ------------ | --------- | ----------------------------- |
| TCP 代理转发 | ✅ 支持   | 转发到内网其他机器            |
| SSH 到 Agent | ❌ 不支持 | 容器内用户环境受限            |
| SSH 代理转发 | ✅ 支持   | 作为跳板转发到内网 SSH 服务器 |

### 4.3 部署方式

```
docker run -d \
  --name awecloud-agent \
  --restart always \
  -v /path/to/agent.toml:/etc/awecloud/agent.toml \
  awecloud/agent:latest
```

### 4.4 升级方式

```
docker pull awecloud/agent:latest
docker stop awecloud-agent
docker rm awecloud-agent
docker run -d ... awecloud/agent:latest
```

## 5. Kubernetes 部署模式

### 5.1 适用场景

- 云原生环境
- 需要高可用
- 只需要 TCP 代理功能

### 5.2 功能限制

与 Docker 模式相同，不支持 SSH 到 Agent 本机。

### 5.3 升级方式

更新 Deployment 的镜像版本，Kubernetes 自动滚动更新。

## 6. 部署模式选择指南

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                      部署模式选择流程                                        │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│                        需要 SSH 到 Agent 本机？                              │
│                                 │                                           │
│                    ┌────────────┴────────────┐                              │
│                    │                         │                              │
│                   是                        否                              │
│                    │                         │                              │
│                    ▼                         ▼                              │
│             ┌───────────┐           运行环境是什么？                         │
│             │  Systemd  │                    │                              │
│             │   部署    │         ┌──────────┼──────────┐                   │
│             └───────────┘         │          │          │                   │
│                               物理机/VM   Docker    Kubernetes              │
│                                   │          │          │                   │
│                                   ▼          ▼          ▼                   │
│                             ┌─────────┐ ┌────────┐ ┌──────────┐            │
│                             │ Systemd │ │ Docker │ │ K8s 部署  │            │
│                             │  部署   │ │  部署  │ │          │            │
│                             └─────────┘ └────────┘ └──────────┘            │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

## 7. 配置差异

| 配置项       | Systemd                  | Docker      | Kubernetes |
| ------------ | ------------------------ | ----------- | ---------- |
| 配置文件路径 | /etc/awecloud/agent.toml | 挂载卷      | ConfigMap  |
| 数据目录     | /var/lib/awecloud/       | 挂载卷      | PVC        |
| 日志         | /var/log/awecloud/       | stdout      | stdout     |
| 网络模式     | 宿主机网络               | bridge/host | Pod 网络   |
| SSH 功能     | 启用                     | 禁用        | 禁用       |

## 8. 安全考虑

### 8.1 Systemd 模式

- Agent 以 root 运行（SSH 需要）
- 使用 systemd 安全加固选项
- 配置文件权限 600

### 8.2 Docker/K8s 模式

- 可以使用非 root 用户运行
- 网络隔离
- 资源限制

## 9. 监控和日志

| 部署模式   | 日志查看命令                     | 健康检查         |
| ---------- | -------------------------------- | ---------------- |
| Systemd    | journalctl -u awecloud-agent -f  | systemctl status |
| Docker     | docker logs -f awecloud-agent    | docker inspect   |
| Kubernetes | kubectl logs -f deployment/agent | livenessProbe    |
