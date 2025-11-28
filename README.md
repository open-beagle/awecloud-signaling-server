# AWECloud Signaling Server

基于 FRP 的内网穿透信令服务系统。

## 项目结构

```
awecloud-signaling-server/
├── cmd/
│   ├── server/          # Server端程序
│   └── agent/           # Agent端程序
├── internal/
│   ├── server/          # Server端实现
│   ├── agent/           # Agent端实现
│   └── common/          # 公共代码
├── config/              # 配置文件
├── docs/                # 文档
├── web/                 # Web管理界面
└── desktop/             # Desktop客户端（独立仓库）
```

**注意**: Desktop 客户端是独立的 Git 仓库，位于 `desktop/` 目录。

## 快速开始

### 1. 编译

**Server 和 Agent**：

```bash
# 开发构建（只构建当前架构）
BUILD_VERSION=v0.1.0 GOARCHS=$(go env GOARCH) bash scripts/build.sh
BUILD_VERSION=v0.1.0 bash scripts/build_frontend.sh

# 生产构建（构建所有架构）
bash scripts/build.sh
```

**Desktop 客户端**：

```bash
# 开发构建
BUILD_VERSION=v0.1.0 bash scripts/build_desktop.sh

# 构建指定平台
BUILD_VERSION=v0.1.0 PLATFORMS=linux/amd64 bash scripts/build_desktop.sh

# 构建多个平台
BUILD_VERSION=v0.1.0 PLATFORMS=linux/amd64,darwin/amd64,windows/amd64 bash scripts/build_desktop.sh

# 构建时注入默认 Server 地址（推荐用于企业内部分发）
BUILD_VERSION=v0.1.0 BUILD_ADDRESS=https://signaling.example.com bash scripts/build_desktop.sh
```

**前置要求**：
- Server/Agent: Go 1.24+
- Desktop: Go 1.24+, Node.js 20+, Wails CLI

### 2. 启动 Server

```bash
# 使用启动脚本（推荐）
./scripts/run_server.sh

# 或直接运行
./bin/server -c config/server.toml
```

**默认管理员账号**：

- 用户名: `admin`
- 密码: `admin123`

**Server 监听端口**：

- **8080**: HTTP/2 统一端口（Web 界面 + RESTful API + gRPC）
- **7000**: FRP 信令服务（WebSocket）

### 3. 启动 Agent

```bash
# 或使用启动脚本
./scripts/run_agent.sh

# 或直接运行
./bin/agent -c config/agent.toml
```

## 开发状态

**当前进度**: 约 35% (Week 3 完成)

### 核心里程碑

- [x] **里程碑 1: Server 开发完成** ✅

  - RESTful API 完整实现
  - gRPC 服务实现（HTTP/2 统一端口）
  - Server-FRP 线程实现
  - API 测试通过

- [x] **里程碑 2: Agent 开发完成** ✅

  - Agent-Web 线程（gRPC 客户端）
  - Agent-FRP 线程（FRP 客户端）
  - 动态代理管理
  - 构建测试通过

- [ ] **里程碑 3: Desktop 开发完成** 🔄

  - [x] Desktop-Web 线程（gRPC 客户端）✅
  - [x] Desktop-FRP 线程（FRP 客户端）✅
  - [x] Vue 3 前端界面 ✅
  - [x] Windows 可执行文件 ✅
  - [ ] 人工联调测试 ⏳

- [ ] **里程碑 4: Web 界面完成**（Week 4-5）

- [ ] **里程碑 5: MVP 发布**（Week 8）

### 周次进度

- [x] Week 1: 数据库模型、RESTful API、测试框架 ✅
- [x] Week 2: gRPC 服务、Server 内部设计、进程内通信 ✅
- [x] Week 3: Agent 完整实现、Server-FRP 线程 ✅
- [ ] Week 4-5: Web 管理界面
- [ ] Week 6-7: Desktop 客户端应用
- [ ] Week 8: 测试和优化

**详细进度**: [docs/progress.md](docs/progress.md)

## 文档

- [开发计划](docs/plan.md) - 完整的开发计划和任务清单
- [进度跟踪](docs/progress.md) - 每日更新的进度记录
- [设计文档](docs/design.md) - 系统架构和技术设计
- [测试规范](docs/test.md) - API 测试规范和流程

### 文档管理规范

**正式文档**：存储在 `docs/` 目录

- 设计文档、API 文档、用户指南等正式文档
- 需要版本控制和长期维护的文档

**临时文档**：存储在 `.tmp/` 目录

- AI 生成的过程文档和临时文件
- 调试记录、测试脚本等临时内容
- 不纳入版本控制（已在 .gitignore 中）

**规则**：

- AI 不得私自在 `docs/` 目录创建文档
- 所有 AI 生成的过程文档必须放在 `.tmp/` 目录
- 正式文档的创建需要明确授权

## License

MIT
