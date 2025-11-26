# AWECloud Signaling Server

基于 FRP 的内网穿透信令服务系统。

**Server 端口**:

- 8080: Web 管理界面（HTTP）- 管理员访问，管理 Agent、Client 和 STCP 实例
- 7000: FRP 信令服务（WebSocket）- Agent 和 Client 的 FRP 控制连接

**部署架构**:

- Server 使用 WebSocket 协议（非加密）
- TLS 加密由 Traefik 网关统一处理
- 客户端通过 WSS 连接到 Traefik，Traefik 转发到 Server

**访问地址**:

- Web 管理: `https://your-domain.com/`
- FRP 信令: `wss://your-domain.com/ws`

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
└── web/                 # Web管理界面（待开发）
```

## 快速开始

详细的快速开始指南请查看: [docs/quickstart.md](docs/quickstart.md)

### 1. 编译

```bash
# 编译Server和Agent
go build -o bin/server ./cmd/server
go build -o bin/agent ./cmd/agent
```

### 2. 启动 Server

```bash
./bin/server
```

默认管理员账号：

- 用户名: `admin`
- 密码: `admin123`

Server 监听端口：

- **8080**: Web 管理界面和 RESTful API
- **8081**: gRPC 服务（Agent 和 Desktop 连接）
- **7000**: FRP 信令服务（WebSocket）

### 3. 创建 Agent 并启动

```bash
# 使用演示脚本快速创建
./scripts/demo.sh

# 或手动创建Agent，然后启动
./bin/agent
```

## API 文档

### 管理员认证

**登录**

```bash
curl -X POST http://localhost:8080/api/admin/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"admin123"}'
```

### Agent 管理

**获取 Agent 列表**

```bash
curl http://localhost:8080/api/agents \
  -H "Authorization: Bearer YOUR_TOKEN"
```

**创建 Agent**

```bash
curl -X POST http://localhost:8080/api/agents \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"agent_name":"dev-agent-001"}'
```

### Client 管理

**创建 Client**

```bash
curl -X POST http://localhost:8080/api/clients \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"client_id":"user@example.com"}'
```

### STCP 实例管理

**创建 STCP 实例**

```bash
curl -X POST http://localhost:8080/api/stcp-instances \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "agent_id": 1,
    "instance_name": "mysql-dev",
    "service_type": "tcp",
    "local_ip": "192.168.1.100",
    "local_port": 3306
  }'
```

**授权 Client 访问**

```bash
curl -X POST http://localhost:8080/api/stcp-instances/1/grant-access \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"client_id": 1}'
```

## 开发状态

**当前进度**: 31% (2.9/8 周)

### 核心里程碑

- [x] **里程碑 1: Server 开发完成** ✅
  - RESTful API 完整实现
  - gRPC 服务实现
  - Server-FRP 线程实现
  - API 测试通过

- [ ] **里程碑 2: Agent 开发完成** 🔄
  - [x] Agent-Web 线程（gRPC 客户端）✅
  - [x] Agent-FRP 线程（FRP 客户端）✅
  - [ ] 人工联调测试（Server ↔ Agent）
  - [ ] 核心 MVP 功能验证：
    - [ ] Web 界面创建 Agent
    - [ ] Web 界面管理 STCP 实例
    - [ ] Agent 自动创建/删除 STCP 代理

- [ ] **里程碑 3: Desktop 开发完成**
  - [ ] Desktop-Web 线程（gRPC 客户端）
  - [ ] Desktop-FRP 线程（FRP 客户端）
  - [ ] 人工联调测试（Desktop ↔ Server ↔ Agent）
  - [ ] 完整功能验证：
    - [ ] Desktop 认证和获取服务列表
    - [ ] Desktop 建立 STCP 隧道
    - [ ] 本地端口访问远程服务（MySQL 等）

### 周次进度

- [x] Week 1: 数据库模型、RESTful API、测试框架 ✅
- [x] Week 2: gRPC 服务、Server 内部设计、进程内通信 ✅
- [x] Week 3: Agent 完整实现、Server-FRP 线程 ✅
- [ ] Week 4-5: Web 管理界面
- [ ] Week 6-7: Desktop 客户端应用
- [ ] Week 8: 测试和优化

详细进度查看: [docs/progress.md](docs/progress.md)

## 开发规范

### 构建规范

```bash
# 开发阶段：只构建当前架构
GOARCHS=$(go env GOARCH) ./scripts/build.sh

# 生产构建：构建所有架构
./scripts/build.sh

# 输出目录: bin/
```

### 调试规范

- 调试前必须讨论方案
- 所有调试活动记录在 `docs/debug.md`

### 文档规范

- `docs/plan.md` - 开发计划（完成任务后更新）
- `docs/progress.md` - 进度跟踪（每日更新）
- `docs/debug.md` - 调试记录
- ❌ 禁止随意创建文档

## License

MIT
