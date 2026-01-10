# AWECloud Signaling Server

安全的内网穿透访问系统，通过 Tailscale/Headscale 建立安全隧道，允许用户通过 Desktop 客户端访问内网服务。

**核心功能**：

- 通过 Tailscale/Headscale 建立安全隧道
- 设备令牌认证，绑定硬件指纹
- 服务权限管理（公开/私有/分组访问）
- 连接审计日志
- Desktop 客户端版本控制

## 核心模块

```
awecloud-signaling-server/
├── cmd/
│   ├── server/          # Server 入口
│   └── agent/           # Agent 入口
├── internal/
│   ├── server/          # Server 实现（API/gRPC/数据库）
│   ├── agent/           # Agent 实现（Tailscale/代理管理）
│   └── common/          # 公共代码（配置/日志）
├── pkg/proto/           # Protocol Buffers 定义
├── web/                 # Web 管理界面（Vue 3）
├── desktop/             # Desktop 客户端（独立仓库）
├── config/              # 配置文件
└── docs/                # 文档
```

**Server**：部署在公有云，作为信令服务器和流量中继，提供 REST API、gRPC 服务和 Web 管理界面。

**Agent**：部署在内网环境，通过 Tailscale 连接到 Server，提供对内网服务的访问。

**Desktop**：桌面客户端应用（独立 Git 仓库），供终端用户访问内网服务。

**Web**：Vue 3 管理界面，用于管理 Agent、Client、Service 等资源。

## 开发框架

**Go 1.25+**

- Gin - HTTP 路由和中间件
- gRPC - Agent/Desktop 通信
- GORM - ORM，使用 SQLite
- Tailscale - 网络隧道

**Vue 3 + TypeScript**

- Element Plus - UI 组件库
- Pinia - 状态管理
- Vite - 构建工具

**Desktop 客户端**

- Wails v3 - Go + Web 桌面框架

## 快速开始

### 环境要求

- Go 1.25+
- Node.js 20+（Web 和 Desktop）
- Wails CLI（Desktop）

### 构建命令

```bash
# Server & Agent（开发构建，仅当前架构）
BUILD_VERSION=v0.2.0 GOARCHS=$(go env GOARCH) bash scripts/build.sh

# Web 前端
BUILD_VERSION=v0.2.0 bash scripts/build_frontend.sh

# Desktop 客户端
BUILD_VERSION=v0.2.0 \
BUILD_ADDRESS=${SIGNALING_ADDRESS} \
PLATFORMS="windows/amd64" \
bash scripts/build_desktop.sh
```

### 运行命令

```bash
# 启动 Server
./scripts/run_server.sh
# 或：./bin/server -c config/server.toml

# 启动 Agent
./scripts/run_agent.sh
# 或：./bin/agent -c config/agent.toml
```

## License

MIT
