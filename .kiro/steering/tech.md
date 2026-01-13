# 技术栈

## 后端 (Go 1.25+)

- **Gin** - HTTP 路由和中间件
- **gRPC** - Agent/Desktop 通信（Protocol Buffers）
- **GORM** - ORM，使用 SQLite 数据库
- **Tailscale** - 网络隧道
- **JWT** - 认证令牌

## 前端 (Vue 3 + TypeScript)

- **Element Plus** - UI 组件库
- **Pinia** - 状态管理
- **Vue Router** - 路由
- **Vue I18n** - 国际化
- **Axios** - HTTP 客户端
- **Vite** - 构建工具

## Desktop 客户端

- **Wails v3** - Go + Web 桌面框架（独立仓库）

## 构建命令

```bash
# 构建 Server 和 Agent（默认当前架构）
bash scripts/build.sh

# 构建 Web 前端
bash scripts/build_frontend.sh

# 构建 Desktop 客户端
bash scripts/build_desktop.sh

# 生成 Protocol Buffers
bash scripts/generate_proto.sh
```

## 运行命令

```bash
# 启动 Server
./scripts/run_server.sh

# 启动 Agent
./scripts/run_agent.sh

# 前端开发
cd web && npm run dev
```

## 环境要求

- Go 1.25+
- Node.js 20+
- Wails CLI（用于 Desktop）

## 配置

配置文件使用 TOML 格式，示例在 `config/` 目录：

- `server.toml.example` - Server 配置
- `agent.toml.example` - Agent 配置
- `network.toml.example` - 网络配置
