# Beagle Signal Server

[![Build Images](https://github.com/open-beagle/awecloud-signaling-server/actions/workflows/build.yml/badge.svg?branch=main)](https://github.com/open-beagle/awecloud-signaling-server/actions/workflows/build.yml)

面向研发、测试和运维人员的零信任资源访问服务，通过 Headscale 建立安全通道，允许用户使用原生工具访问明确授权的远程资源。

项目网络控制面、节点注册、ACL 和身份查询统一基于 Headscale，不依赖第三方托管控制面。

**核心功能**：

- 通过 Headscale 建立安全隧道
- 设备令牌认证，绑定硬件指纹
- 服务权限管理（公开/私有/分组访问）
- 连接审计日志
- Desktop 客户端版本控制

## 目录结构

```txt
awecloud-signaling-server/
├── cmd/                 # 应用入口
│   ├── server/          # Server 入口
│   └── agent/           # Agent 入口
├── internal/            # 内部实现
│   ├── server/          # Server 实现（API/gRPC/数据库）
│   ├── agent/           # Agent 实现（Headscale/代理管理）
│   └── common/          # 公共代码（配置/日志）
├── pkg/                 # 公共包
│   └── proto/           # Protocol Buffers 定义
├── web/                 # Web 管理界面（Vue 3）
├── desktop/             # Desktop 客户端（独立仓库）
├── config/              # 配置文件
├── docs/                # 设计文档
├── images/              # 设计图（SVG）
├── scripts/             # 构建和运行脚本
├── deployments/         # 部署配置
│   ├── docker/          # Docker 部署
│   └── kubernetes/      # Kubernetes 部署
├── data/                # 数据文件（SQLite）
├── logs/                # 日志文件
└── bin/                 # 编译输出
```

## 核心模块

**Server**：部署在公有云，作为信令服务器和流量中继，提供 REST API、gRPC 服务和 Web 管理界面。

**Agent**：部署在内网环境，通过 Headscale 连接到 Server，提供对内网服务的访问。

**Desktop**：桌面客户端应用（独立 Git 仓库），供终端用户访问内网服务。

**Web**：Vue 3 管理界面，用于管理 Agent、Client、Service 等资源。

## 开发框架

**Go 1.26**

- Gin - HTTP 路由和中间件
- gRPC - Agent/Desktop 通信
- GORM - ORM，使用 SQLite
- Headscale - 网络隧道

**Vue 3 + TypeScript**

- Element Plus - UI 组件库
- Pinia - 状态管理
- Vite - 构建工具

**Desktop 客户端**

- Wails v3 - Go + Web 桌面框架

## 快速开始

### 环境要求

- Go 1.26
- Node.js 24（Web）

### 构建命令

```bash
# 生成 Protocol Buffers 代码
bash scripts/generate_proto.sh

# Web 前端
BUILD_VERSION=$(cat version) bash scripts/build_frontend.sh

# Server、Agent、Endpoint（日常开发，默认当前架构）
BUILD_VERSION=$(cat version) bash scripts/build.sh

# 仅构建 Server
BUILD_VERSION=$(cat version) bash scripts/build.sh server

```

Desktop 客户端位于独立仓库，使用其自身的 `version` 和构建说明。

### 流水线构建

GitHub Actions 工作流位于 `.github/workflows/build.yml`，支持以下触发方式：

- 经发布批准后，将已验证的 `dev` 提交推送到 `release-v1.0` 分支，自动构建并发布。
- `dev` 和 `main` 分支推送不会触发发布。

流水线使用 Go 1.26、Node.js 24，并执行：

1. 校验发布版本。
2. Server job 构建 Web 管理界面和 Linux amd64/arm64 Server 二进制，发布双架构镜像并滚动部署。
3. Agent job 交叉编译 Linux amd64/arm64 的 Agent 和 Endpoint。
4. 上传 Agent、Endpoint 双架构二进制和安装脚本到 S3；Agent 不发布容器镜像。

镜像标签读取根目录 `version` 文件：

```txt
registry.cn-qingdao.aliyuncs.com/wod/awecloud-signaling-server:<version>
```

流水线地址：<https://github.com/open-beagle/awecloud-signaling-server/actions/workflows/build.yml>

### 迭代命令

日常开发、测试和版本号修改都在 `dev` 完成。未经发布批准，不得向 `release-v1.0` 推送。批准发布后，将已验证的 `dev` 提交推送到固定流水线分支：

```bash
git switch dev && \
  git pull --ff-only origin dev && \
  git push origin dev && \
  git push origin dev:release-v1.0
```

版本号以 `version` 文件为准，流水线不会自动修改版本号。禁止创建 `release-v1.0.x` 补丁版本分支；`release-v1.0` 是 `v1.0.x` 系列唯一的发布流水线分支。

### 运行命令

```bash
# 启动 Server
./scripts/run_server.sh

# 启动 Agent
./scripts/run_agent.sh
```

## Agent 部署

Agent 支持一键部署到 Linux 服务器，默认启用 SSH 功能。

### 部署流程

1. 在 Server Web 界面创建 Agent 用户（角色选择"代理"），获取 Token
2. 在目标服务器执行安装命令

### 安装命令

```bash
curl -fsSL https://your-server.com/api/v1/download/install.sh | \
  sudo bash -s -- \
    -n <AGENT_NAME> \
    -t <TOKEN> \
    -s https://your-server.com
```

### 参数说明

| 参数              | 说明                         |
| ----------------- | ---------------------------- |
| `-n, --name`      | Agent 名称，如 `beijing-242` |
| `-t, --token`     | 认证 Token（从 Server 获取） |
| `-s, --server`    | Server 地址                  |
| `--no-ssh`        | 禁用 SSH（默认启用）         |
| `-u, --upgrade`   | 升级模式，保留现有配置       |
| `-U, --uninstall` | 卸载 Agent                   |

### 安装路径

| 路径                                                         | 说明                   |
| ------------------------------------------------------------ | ---------------------- |
| `/etc/kubernetes/downloads/signaling-<version>-linux-<arch>` | Agent 二进制（带版本） |
| `/opt/bin/signaling`                                         | 软链接                 |
| `/etc/kubernetes/config/k8s-signaling.toml`                  | 配置文件               |
| `/etc/kubernetes/data/signaling/`                            | 数据目录               |
| `k8s-signaling.service`                                      | systemd 服务名         |

### 常用命令

```bash
# 查看状态
systemctl status k8s-signaling

# 查看日志
journalctl -u k8s-signaling -f

# 重启服务
systemctl restart k8s-signaling
```

## 可观测性

Server 支持 OpenTelemetry 分布式追踪，通过环境变量配置：

| 环境变量                      | 说明                          | 示例                         |
| ----------------------------- | ----------------------------- | ---------------------------- |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | OTLP Endpoint，设置后自动启用 | `http://otel-collector:4317` |
| `OTEL_SERVICE_NAME`           | 服务名称（可选）              | `signaling-server`           |
| `OTEL_SERVICE_NAMESPACE`      | 服务命名空间（可选）          | `default`                    |

Endpoint 自动识别协议：`http://` 使用非安全连接，`https://` 使用 TLS。

K8s 部署示例：

```yaml
env:
  - name: OTEL_EXPORTER_OTLP_ENDPOINT
    value: "http://otel-collector:4317"
  - name: OTEL_SERVICE_NAME
    value: "signaling-server"
  - name: OTEL_SERVICE_NAMESPACE
    value: "default"
```

## License

MIT
