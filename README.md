# AWECloud Signaling Server

基于FRP的内网穿透信令服务系统。

**Server端口**:
- 8080: Web管理界面（HTTP）- 管理员访问，管理Agent、Client和STCP实例
- 7000: FRP信令服务（WebSocket）- Agent和Client的FRP控制连接

**部署架构**:
- Server使用WebSocket协议（非加密）
- TLS加密由Traefik网关统一处理
- 客户端通过WSS连接到Traefik，Traefik转发到Server

**访问地址**:
- Web管理: `https://your-domain.com/`
- FRP信令: `wss://your-domain.com/ws`

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

### 1. 安装依赖

```bash
go mod tidy
```

### 2. 启动Server

```bash
go run cmd/server/main.go -c config/server.toml
```

默认管理员账号：
- 用户名: `admin`
- 密码: `admin123`

Web管理界面: http://localhost:8080

### 3. 启动Agent

首先在Web管理界面创建Agent，获取agent_name和token，然后：

```bash
# 修改config/agent.toml，填入agent_name和token
go run cmd/agent/main.go -c config/agent.toml
```

## API文档

### 管理员认证

**登录**
```bash
curl -X POST http://localhost:8080/api/admin/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"admin123"}'
```

### Agent管理

**获取Agent列表**
```bash
curl http://localhost:8080/api/agents \
  -H "Authorization: Bearer YOUR_TOKEN"
```

**创建Agent**
```bash
curl -X POST http://localhost:8080/api/agents \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"agent_name":"dev-agent-001"}'
```

### Client管理

**创建Client**
```bash
curl -X POST http://localhost:8080/api/clients \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"client_id":"user@example.com"}'
```

### STCP实例管理

**创建STCP实例**
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

**授权Client访问**
```bash
curl -X POST http://localhost:8080/api/stcp-instances/1/grant-access \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"client_id": 1}'
```

## 开发状态

**当前进度**: Week 1 已完成 ✅

- [x] 项目初始化
- [x] 数据库设计
- [x] 管理员认证
- [x] Agent管理API
- [x] Client管理API
- [x] STCP实例管理API
- [ ] FRP Server集成（Week 2）
- [ ] Agent端实现（Week 3）
- [ ] Web管理界面（Week 4-5）
- [ ] Desktop应用（Week 6-7）

详细进度查看: `docs/progress.md`

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
