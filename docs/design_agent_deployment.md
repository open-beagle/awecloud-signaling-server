# Agent 部署模式设计

> 版本：v0.2.0

## 1. 概述

Agent 支持多种部署模式，不同模式适用于不同场景，功能支持也有差异。

## 2. 多区域 Agent 架构

### 2.1 命名规范

Agent 按区域命名，格式：`<location>.<project>`

示例：

- `beijing.beagle` - 北京区域
- `chengdu.beagle` - 成都区域
- `aliyun.beagle` - 阿里云区域

### 2.2 Agent 与设备的关系

一个 Agent（区域）可以部署到该区域的多台服务器上：

```txt
┌─────────────────────────────────────────────────────────────────────────────┐
│                      Agent 与设备对应关系                                    │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                     beijing.beagle（北京区域）                       │   │
│  │                                                                      │   │
│  │   ┌──────────────┐  ┌──────────────┐  ┌──────────────┐              │   │
│  │   │ beagle-241   │  │ beagle-242   │  │ beagle-243   │   ...        │   │
│  │   │ 192.168.1.10 │  │ 192.168.1.11 │  │ 192.168.1.12 │              │   │
│  │   │ SSH: ✅      │  │ SSH: ✅      │  │ SSH: ✅      │              │   │
│  │   └──────────────┘  └──────────────┘  └──────────────┘              │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                                                             │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                     chengdu.beagle（成都区域）                       │   │
│  │                                                                      │   │
│  │   ┌──────────────┐  ┌──────────────┐                                │   │
│  │   │ beagle-301   │  │ beagle-302   │   ...                          │   │
│  │   │ 10.0.1.10    │  │ 10.0.1.11    │                                │   │
│  │   │ SSH: ✅      │  │ SSH: ✅      │                                │   │
│  │   └──────────────┘  └──────────────┘                                │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                                                             │
│  说明：                                                                      │
│  - 每个区域对应一个 Agent 用户账号（如 beijing.beagle）                      │
│  - 该账号可部署到区域内的多台服务器                                          │
│  - 每台服务器用设备名标识（如 beagle-241）                                   │
│  - 所有设备共享同一个 Token，但有不同的设备名                                │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 2.3 部署流程

```txt
┌─────────────────────────────────────────────────────────────────────────────┐
│                      部署流程                                                │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  1. 在 Server Web 界面创建 Agent 用户                                        │
│     - 名称：beijing.beagle                                                  │
│     - 角色：代理                                                            │
│     - 获取 Token                                                            │
│                                                                             │
│  2. 在北京区域的每台服务器上部署                                             │
│                                                                             │
│     服务器 beagle-241:                                                      │
│     curl -fsSL https://server/install.sh | sudo bash -s -- \                │
│       -n beijing.beagle \                                                   │
│       -d beagle-241 \                                                       │
│       -t <TOKEN> \                                                          │
│       -s https://server                                                     │
│                                                                             │
│     服务器 beagle-242:                                                      │
│     curl -fsSL https://server/install.sh | sudo bash -s -- \                │
│       -n beijing.beagle \                                                   │
│       -d beagle-242 \                                                       │
│       -t <TOKEN> \                                                          │
│       -s https://server                                                     │
│                                                                             │
│  3. 重复步骤 1-2 部署其他区域（chengdu.beagle 等）                           │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

## 3. 部署模式对比

| 部署模式   | 适用场景                | SSH 到设备 | TCP 代理转发 | 升级方式   |
| ---------- | ----------------------- | ---------- | ------------ | ---------- |
| Systemd    | 需要 SSH 功能的服务器   | ✅ 支持    | ✅ 支持      | 脚本升级   |
| Docker     | 纯 TCP 代理场景         | ❌ 不支持  | ✅ 支持      | 镜像更新   |
| Kubernetes | 云原生环境，纯 TCP 代理 | ❌ 不支持  | ✅ 支持      | Deployment |

## 4. Systemd 一键部署

### 4.1 部署命令

```bash
curl -fsSL https://your-server.com/api/v1/download/install.sh | \
  sudo bash -s -- \
    -n beijing.beagle \
    -d beagle-241 \
    -t <TOKEN> \
    -s https://your-server.com
```

### 4.2 参数说明

| 参数        | 简写 | 必填 | 说明                            |
| ----------- | ---- | ---- | ------------------------------- |
| --name      | -n   | 是   | Agent 名称（区域），如 beijing.beagle |
| --device    | -d   | 是   | 设备名，如 beagle-241           |
| --token     | -t   | 是   | 从 Server 获取的认证 Token      |
| --server    | -s   | 是   | Server 地址                     |
| --no-ssh    |      | 否   | 禁用 SSH（默认启用）            |
| --upgrade   | -u   | 否   | 升级模式，保留配置              |
| --uninstall | -U   | 否   | 卸载 Agent                      |

### 4.3 配置文件生成

脚本会自动生成 /etc/awecloud/agent.toml：

```toml
[agent]
name = "beijing.beagle"      # Agent 名称（区域）
token = "xxx..."             # 认证 Token
device = "beagle-241"        # 设备名

[server]
address = "https://your-server.com"

[tunnel]
state_dir = "/var/lib/awecloud/"
enable_ssh = true            # 默认启用 SSH
```

### 4.4 多设备部署示例

```txt
┌─────────────────────────────────────────────────────────────────────────────┐
│                      北京区域部署示例                                        │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  准备工作：                                                                  │
│  1. Server Web 界面 → 用户管理 → 创建用户                                    │
│     - 名称：beijing.beagle                                                  │
│     - 角色：代理                                                            │
│  2. 复制 Token                                                              │
│                                                                             │
│  部署到 beagle-241:                                                         │
│  ─────────────────                                                          │
│  curl -fsSL https://signaling.example.com/api/v1/download/install.sh | \    │
│    sudo bash -s -- \                                                        │
│      -n beijing.beagle \                                                    │
│      -d beagle-241 \                                                        │
│      -t eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9... \                           │
│      -s https://signaling.example.com                                       │
│                                                                             │
│  部署到 beagle-242:                                                         │
│  ─────────────────                                                          │
│  curl -fsSL https://signaling.example.com/api/v1/download/install.sh | \    │
│    sudo bash -s -- \                                                        │
│      -n beijing.beagle \                                                    │
│      -d beagle-242 \                                                        │
│      -t eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9... \                           │
│      -s https://signaling.example.com                                       │
│                                                                             │
│  部署到 beagle-243:                                                         │
│  ─────────────────                                                          │
│  curl -fsSL https://signaling.example.com/api/v1/download/install.sh | \    │
│    sudo bash -s -- \                                                        │
│      -n beijing.beagle \                                                    │
│      -d beagle-243 \                                                        │
│      -t eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9... \                           │
│      -s https://signaling.example.com                                       │
│                                                                             │
│  注意：三台服务器使用相同的 Agent 名称和 Token，但设备名不同                    │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

## 5. Systemd 部署详情

### 5.1 目录结构

| 路径                                       | 说明             |
| ------------------------------------------ | ---------------- |
| /usr/local/bin/awecloud-agent              | Agent 二进制文件 |
| /etc/awecloud/agent.toml                   | 配置文件         |
| /var/lib/awecloud/                         | 数据目录         |
| /var/log/awecloud/agent.log                | 日志文件         |
| /etc/systemd/system/awecloud-agent.service | Systemd 服务文件 |

### 5.2 Systemd 服务配置

关键配置项：

| 配置项          | 值        | 说明                        |
| --------------- | --------- | --------------------------- |
| Type            | simple    | 简单服务类型                |
| Restart         | always    | 总是重启                    |
| RestartSec      | 5         | 重启间隔 5 秒               |
| NoNewPrivileges | false     | 允许提权（SSH 需要 setuid） |
| ProtectSystem   | full      | 保护系统目录                |
| ProtectHome     | read-only | 只读访问 home 目录          |

### 5.3 常用命令

```bash
# 查看服务状态
systemctl status awecloud-agent

# 查看日志
journalctl -u awecloud-agent -f

# 重启服务
systemctl restart awecloud-agent

# 停止服务
systemctl stop awecloud-agent
```

### 5.4 升级

```bash
curl -fsSL https://server/api/v1/download/install.sh | \
  sudo bash -s -- --upgrade -s https://server
```

### 5.5 卸载

```bash
curl -fsSL https://server/api/v1/download/install.sh | \
  sudo bash -s -- --uninstall
```

## 6. Docker 部署模式

### 6.1 适用场景

- 只需要 TCP 代理转发功能
- 不需要 SSH 到设备
- 容器化环境

### 6.2 功能限制

| 功能         | 支持情况  | 说明                          |
| ------------ | --------- | ----------------------------- |
| TCP 代理转发 | ✅ 支持   | 转发到内网其他机器            |
| SSH 到设备   | ❌ 不支持 | 容器内用户环境受限            |
| SSH 代理转发 | ✅ 支持   | 作为跳板转发到内网 SSH 服务器 |

## 7. 部署模式选择指南

```txt
┌─────────────────────────────────────────────────────────────────────────────┐
│                      部署模式选择                                            │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│                        需要 SSH 到设备？                                     │
│                              │                                              │
│                 ┌────────────┴────────────┐                                 │
│                 │                         │                                 │
│                是                        否                                 │
│                 │                         │                                 │
│                 ▼                         ▼                                 │
│          ┌───────────┐           ┌─────────────────┐                       │
│          │  Systemd  │           │ Docker / K8s    │                       │
│          │  （推荐）  │           │                 │                       │
│          └───────────┘           └─────────────────┘                       │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

## 8. 安装脚本

安装脚本位于 `scripts/install_agent.sh`，由 Server 通过 `/api/v1/download/install.sh` 提供下载。

脚本功能：

- 自动检测系统架构（amd64/arm64）
- 下载对应架构的 Agent 二进制
- 生成配置文件（包含 Agent 名称、设备名、Token）
- 安装 systemd 服务
- 默认启用 SSH
- 支持升级和卸载模式
