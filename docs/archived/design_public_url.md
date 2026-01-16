# FRP 公网地址配置设计

## 1. 需求背景

### 1.1 当前问题

在生产环境中，Server 部署在公有云，通过 **Traefik 网关**对外提供服务：

- Server 的 7000 和 8080 端口不直接对外暴露
- Desktop 和 Agent 通过 Traefik 网关访问 Server
- Traefik 提供 TLS 终止和路径路由

### 1.2 使用场景

**场景 1：开发环境（直连）**

- Desktop 直接连接 `localhost:8080`（Web/gRPC）
- Desktop 自动推导 FRP 地址为 `localhost:7000`
- Agent 直接连接 Server 的 7000 端口
- 不需要配置公网地址

**场景 2：生产环境（通过 Traefik 网关）**

- Desktop 连接 `https://signaling.example.com`（Web/gRPC，路由到 8080）
- Desktop 需要连接 `wss://signaling.example.com/ws`（FRP，路由到 7000）
- Agent 需要连接 `wss://signaling.example.com/ws`（FRP，路由到 7000）
- 需要配置 FRP 公网地址

### 1.3 目标

1. Server 支持配置 FRP 公网访问地址
2. Desktop 认证后获取 FRP 公网地址
3. Agent 认证后获取 FRP 公网地址
4. 兼容开发环境（不配置公网地址时使用默认行为）

## 2. 架构设计

### 2.1 系统架构

```
┌─────────┐         ┌─────────┐         ┌────────┐
│ Desktop │ ─────> │ Traefik │ ─────> │ Server │
│  Agent  │  HTTPS  │ Gateway │  HTTP   │        │
└─────────┘         └─────────┘         └────────┘
                        │
                        ├─ /          → HTTP/2 统一端口 (8080)
                        │               Web UI + REST API + gRPC
                        │
                        └─ /ws        → FRP WebSocket (7000)
```

### 2.2 配置流程

```
1. Server 启动
   ├─ 读取配置文件
   ├─ 获取 public_url（可选）
   └─ 初始化 gRPC 服务

2. Desktop 认证
   ├─ 调用 Authenticate API
   ├─ Server 返回 FRP 连接信息
   │   ├─ token: FRP 认证 token
   │   ├─ server: FRP 地址（完整 URL 或空）
   │   └─ port: FRP 端口（使用 URL 时为 0）
   └─ Desktop 连接 FRP 服务

3. Agent 认证
   ├─ 调用 Authenticate API
   ├─ Server 返回 FRP 连接信息
   │   ├─ token: FRP 认证 token
   │   ├─ server: FRP 地址（完整 URL 或空）
   │   └─ port: FRP 端口（使用 URL 时为 0）
   └─ Agent 连接 FRP 服务
```

## 3. 详细设计

### 3.1 配置文件设计

#### Server 配置 (`config/server.toml`)

```toml
[web]
listen_addr = "0.0.0.0"
listen_port = 8080
default_admin_username = "admin"
default_admin_password = "admin123"

[security]
jwt_secret = "your-secret-key-change-in-production"

[database]
path = "data/server.db"

[log]
level = "info"
file = ""

[server]
bind_addr = "0.0.0.0"
bind_port = 7000
protocol = "websocket"
tls_cert_file = ""
tls_key_file = ""
token = "change-this-to-a-secure-random-token"

# 公网访问地址（可选）
# 如果配置了此地址，Desktop 和 Agent 将使用此地址连接 FRP 服务
# 适用于生产环境中通过反向代理（如 Traefik）暴露 FRP 服务的场景
# 示例：
#   - WebSocket: "ws://signaling.example.com/ws"
#   - WebSocket Secure: "wss://signaling.example.com/ws"
# 如果不配置（留空），Desktop 将使用连接 Web/gRPC 时的地址 + bind_port
public_url = ""
```

#### Agent 配置 (`config/agent.toml`)

```toml
# Agent 配置
# 注意：name 和 token 可以通过环境变量覆盖
# 环境变量：AGENT_NAME, AGENT_TOKEN
[agent]
name = "agent-dev-001"
token = "your-agent-token-from-server"

# Server 连接配置
# Server 地址配置优先级：
#   1. 环境变量 AGENT_ADDRESS（最高优先级）
#   2. 配置文件 server.address
#   3. 编译时注入的 BUILD_URL（通过 -ldflags）
#   4. 硬编码默认值 "http://localhost:8080"（最低优先级）
#
# 注意：
#   - address 应包含完整的 URL，如 "http://localhost:8080" 或 "https://signaling.example.com"
#   - Agent 会自动从 Server 获取 FRP 连接信息（地址、端口、token）
#   - 如果不配置，将使用编译时注入的默认值或硬编码默认值
[server]
address = "http://localhost:8080"

# 日志配置
[log]
level = "info"
file = ""
```

#### 配置说明

**Server 配置**

| 字段                  | 类型   | 必填 | 默认值      | 说明                         |
| --------------------- | ------ | ---- | ----------- | ---------------------------- |
| `[server].bind_addr`  | string | 是   | "0.0.0.0"   | FRP 服务绑定地址             |
| `[server].bind_port`  | int    | 是   | 7000        | FRP 服务端口                 |
| `[server].protocol`   | string | 是   | "websocket" | 传输协议                     |
| `[server].token`      | string | 是   | -           | FRP 认证 token               |
| `[server].public_url` | string | 否   | ""          | FRP 公网访问地址（完整 URL） |

**Agent 配置**

| 字段               | 类型   | 必填 | 默认值                  | 环境变量        | 说明                    |
| ------------------ | ------ | ---- | ----------------------- | --------------- | ----------------------- |
| `[agent].name`     | string | 是   | -                       | `AGENT_NAME`    | Agent 名称              |
| `[agent].token`    | string | 是   | -                       | `AGENT_TOKEN`   | Agent 认证 token        |
| `[server].address` | string | 否   | "http://localhost:8080" | `AGENT_ADDRESS` | Server 地址（完整 URL） |

**配置规则**：

1. **Server `public_url`**：

   - 留空：开发环境，Desktop 自动推导地址
   - 配置完整 URL：生产环境，使用指定的公网地址
   - URL 格式：`ws://` 或 `wss://` + 域名 + 路径

2. **Agent 配置优先级**（所有参数统一）：

   - **环境变量**（最高优先级）
   - **配置文件**
   - **编译时注入** (`BUILD_URL`，仅适用于 `address`）
   - **硬编码默认值**（最低优先级）

   具体参数：

   - `name`：`AGENT_NAME` > 配置文件 > 无默认值
   - `token`：`AGENT_TOKEN` > 配置文件 > 无默认值
   - `address`：`AGENT_ADDRESS` > 配置文件 > `BUILD_URL` > `"http://localhost:8080"`

   注意：`address` 必须是完整的 URL（包含协议和端口）

### 3.2 数据结构设计

#### 配置结构体

```go
type ServerSection struct {
    BindAddr          string `toml:"bind_addr"`
    BindPort          int    `toml:"bind_port"`
    TransportProtocol string `toml:"protocol"`
    TLSCertFile       string `toml:"tls_cert_file"`
    TLSKeyFile        string `toml:"tls_key_file"`
    AuthToken      string `toml:"token"`
    PublicURL      string `toml:"public_url"` // 新增字段
}
```

#### Proto 定义

**Client 认证响应** (`pkg/proto/client.proto`)

```protobuf
message AuthResponse {
  bool success = 1;
  string message = 2;
  string session_token = 3;
  int64 expires_at = 4;
  string token = 5;  // 隧道认证 token
  string server = 6; // 隧道服务器地址（完整 URL 或空）
  int32 port = 7;    // 隧道服务器端口（使用 URL 时为 0）
}
```

**Agent 认证响应** (`pkg/proto/agent.proto`)

```protobuf
message RegisterResponse {
  bool success = 1;
  string message = 2;
  int64 agent_id = 3;
  string token = 4;  // 隧道认证 Token
  string server = 5; // 隧道服务器地址（完整 URL 或空）
  int32 port = 6;    // 隧道服务器端口（使用 URL 时为 0）
}
```

### 3.3 逻辑设计

#### Server 端逻辑

**ClientService.Authenticate**

```go
func (s *ClientServiceServer) Authenticate(ctx context.Context, req *pb.AuthRequest) (*pb.AuthResponse, error) {
    // ... 认证逻辑 ...

    // 构建 FRP 连接信息
    server := ""
    port := int32(s.config.Server.BindPort)

    // 如果配置了公网 URL，使用公网 URL
    if s.config.Server.PublicURL != "" {
        server = s.config.Server.PublicURL
        port = 0 // 使用完整 URL 时，端口信息已包含在 URL 中
    }

    return &pb.AuthResponse{
        Success:      true,
        SessionToken: tokenString,
        ExpiresAt:    expiresAt.Unix(),
        Token:     s.config.Server.AuthToken,
        Server:    server,
        Port:      port,
    }, nil
}
```

**AgentService.Register**

```go
func (s *AgentServiceServer) Register(ctx context.Context, req *pb.RegisterRequest) (*pb.RegisterResponse, error) {
    // ... 注册逻辑 ...

    // 构建 FRP 连接信息
    server := ""
    port := int32(s.port)

    // 如果配置了公网 URL，使用公网 URL
    if s.publicURL != "" {
        server = s.publicURL
        port = 0
    }

    return &pb.RegisterResponse{
        Success:   true,
        AgentId:   agent.ID,
        Token:  s.token,
        Server: server,
        Port:   port,
    }, nil
}
```

#### Desktop 端逻辑

```go
// 认证成功后
resp := Authenticate(clientId, clientSecret)

var addr string
if resp.Server != "" {
    // 使用公网 URL
    addr = resp.Server
} else {
    // 使用连接 Server 时的地址 + 端口
    serverHost := extractHost(serverAddr) // 从 Web/gRPC 地址提取主机
    addr = fmt.Sprintf("ws://%s:%d", serverHost, resp.Port)
}

// 连接 FRP
ConnectFRP(addr, resp.Token)
```

#### Agent 端逻辑

```go
// 注册成功后
resp := Register(agentName, agentToken)

var addr string
if resp.Server != "" {
    // 使用公网 URL
    addr = resp.Server
} else {
    // 使用配置文件中的地址 + 端口
    addr = fmt.Sprintf("ws://%s:%d", config.Server.Address, resp.Port)
}

// 连接 FRP
ConnectFRP(addr, resp.Token)
```

## 4. 编译时配置

### 4.1 构建参数

Agent 和 Desktop 支持在编译时注入默认的 Server 地址：

```bash
# 编译 Agent
go build -ldflags="-X 'main.BUILD_URL=https://signaling.example.com'" -o agent ./cmd/agent

# 编译 Desktop
go build -ldflags="-X 'main.BUILD_URL=https://signaling.example.com'" -o desktop ./cmd/desktop
```

### 4.2 代码实现

**Agent/Desktop main.go**

```go
package main

var (
    BUILD_URL = "" // 编译时注入的默认 Server 地址
)

func main() {
    // 优先级：命令行参数 > 配置文件 > BUILD_URL
    serverAddr := getServerAddress()

    // 使用 serverAddr 连接 Server
}

func getServerAddress() string {
    // 1. 检查命令行参数
    if flagServerAddr != "" {
        return flagServerAddr
    }

    // 2. 检查配置文件
    if config.Server.Address != "" {
        return config.Server.Address
    }

    // 3. 使用编译时注入的默认值
    if BUILD_URL != "" {
        return BUILD_URL
    }

    // 4. 使用硬编码默认值
    return "http://localhost:8080"
}
```

### 4.3 配置优先级

1. **命令行参数**（最高优先级）

   - Agent: `--server-addr=https://signaling.example.com`
   - Desktop: 用户在界面中输入的地址

2. **配置文件**

   - Agent: `config/agent.toml` 中的 `server.address`
   - Desktop: 用户配置文件中保存的地址

3. **编译时注入**（BUILD_URL）

   - 通过 `-ldflags` 注入的默认地址
   - 适用于企业内部分发的定制版本

4. **硬编码默认值**（最低优先级）
   - `http://localhost:8080`（开发环境）

## 5. 实施计划

### 5.1 开发任务

#### 阶段 1：Server 端实现 ✅

1. **配置文件** ✅

   - [x] 在 `config/server.toml.example` 添加 `public_url` 配置项
   - [x] 添加配置说明和示例

2. **配置结构体** ✅

   - [x] 在 `ServerSection` 添加 `PublicURL` 字段
   - [x] 更新配置加载逻辑

3. **Proto 定义** ✅

   - [x] 更新 `agent.proto` 的 `RegisterResponse`
   - [x] 更新 `client.proto` 的 `AuthResponse`
   - [x] 重新生成 proto 代码
   - [x] 使用通用命名（token/server/port）避免 frp 前缀

4. **gRPC 服务** ✅

   - [x] 更新 `ClientService.Authenticate` 实现
   - [x] 更新 `AgentService.Register` 实现
   - [x] 更新 `AgentServiceServer` 构造函数

5. **Server 初始化** ✅
   - [x] 更新 `NewAgentServiceServer` 调用

#### 阶段 2：Desktop 端实现 ✅

1. **编译时配置** ✅

   - [x] 在 `main.go` 添加 `BUILD_URL` 变量
   - [x] 实现配置优先级逻辑（BUILD_URL > 配置文件 > 默认值）
   - [x] 更新构建脚本支持 `-ldflags`

2. **连接逻辑** ✅

   - [x] 更新认证逻辑，从 Server 获取隧道连接信息
   - [x] 在 Login 方法中处理 Server 返回的连接信息
   - [x] 支持完整 URL 和地址+端口两种模式
   - [x] 添加日志记录
   - [x] 更新 proto 文件字段名（Token/Server/Port）

3. **测试**
   - [ ] 测试开发环境（不配置公网 URL）
   - [ ] 测试生产环境（配置公网 URL）
   - [ ] 测试编译时注入的 BUILD_URL

#### 阶段 3：Agent 端实现 ✅

1. **编译时配置** ✅

   - [x] 在 `main.go` 添加 `BUILD_URL` 变量
   - [x] 实现配置优先级逻辑（环境变量 > 配置文件 > BUILD_URL > 默认值）
   - [x] 更新环境变量名为 `AGENT_ADDRESS`

2. **连接逻辑** ✅

   - [x] 更新注册逻辑，从 Server 获取隧道连接信息
   - [x] 在 FRPManager 中添加 `SetServerURL` 和 `SetServerPort` 方法
   - [x] 支持完整 URL 和地址+端口两种模式
   - [x] 添加日志记录
   - [x] 更新字段名（Token/Server/Port）

3. **测试**
   - [ ] 测试开发环境
   - [ ] 测试生产环境
   - [ ] 测试编译时注入的 BUILD_URL

#### 阶段 4：构建脚本 ✅

1. **Agent 构建脚本** ✅

   - [x] 更新 `scripts/build.sh` 支持 BUILD_URL
   - [x] 添加环境变量 `BUILD_URL`
   - [x] 示例：`BUILD_URL=https://signaling.example.com ./scripts/build.sh`
   - [x] 测试构建成功

2. **Desktop 构建脚本**
   - [ ] 更新 Desktop 构建脚本支持 BUILD_URL
   - [ ] 添加编译参数注入

#### 阶段 5：文档和部署 ✅

1. **文档** ✅

   - [x] 更新配置文件示例（server.toml.example, agent.toml.example）
   - [x] 更新设计文档，使用通用命名避免 frp 前缀
   - [x] 添加 BUILD_URL 使用说明
   - [ ] 编写 Traefik 配置指南（待补充）
   - [ ] 更新部署文档（待补充）

2. **Kubernetes 配置** ✅
   - [x] 更新 ConfigMap 示例，添加 public_url 配置
   - [ ] 添加 Ingress 配置示例（待补充）

### 5.2 构建示例

#### Agent 构建

```bash
# 开发环境（不注入 BUILD_URL）
go build -o agent ./cmd/agent

# 生产环境（注入 BUILD_URL）
BUILD_URL="https://signaling.example.com"
go build -ldflags="-X 'main.BUILD_URL=${BUILD_URL}'" -o agent ./cmd/agent
```

#### Desktop 构建

```bash
# 开发环境
go build -o desktop ./cmd/desktop

# 生产环境
BUILD_URL="https://signaling.example.com"
go build -ldflags="-X 'main.BUILD_URL=${BUILD_URL}'" -o desktop ./cmd/desktop
```

### 5.3 测试计划

#### 测试场景 1：开发环境（不配置公网 URL）

```toml
[server]
public_url = ""
```

**预期行为**：

- Desktop 连接 `localhost:8080`，FRP 自动使用 `localhost:7000`
- Agent 使用配置文件中的地址和端口

#### 测试场景 2：生产环境（HTTP）

```toml
[server]
public_url = "ws://signaling.example.com/ws"
```

**预期行为**：

- Desktop 和 Agent 都使用 `ws://signaling.example.com/ws`

#### 测试场景 3：生产环境（HTTPS）

```toml
[server]
public_url = "wss://signaling.example.com/ws"
```

**预期行为**：

- Desktop 和 Agent 都使用 `wss://signaling.example.com/ws`

### 5.4 验证标准

- [ ] Server 正确读取配置
- [ ] Desktop 认证后获取正确的 FRP 地址
- [ ] Agent 认证后获取正确的 FRP 地址
- [ ] Desktop 能成功连接 FRP 服务
- [ ] Agent 能成功连接 FRP 服务
- [ ] 开发环境和生产环境都能正常工作

## 6. 风险和注意事项

### 6.1 兼容性

- **向后兼容**：不配置 `public_url` 时保持原有行为
- **Proto 兼容**：新增字段不影响旧版本客户端

### 6.2 安全性

- **HTTPS**：生产环境必须使用 `wss://`（WebSocket Secure）
- **证书验证**：Desktop 和 Agent 需要验证 SSL 证书

### 6.3 配置错误

- **URL 格式错误**：需要验证 URL 格式
- **地址不可达**：需要提供清晰的错误信息

### 6.4 性能

- **连接延迟**：通过反向代理可能增加延迟
- **连接稳定性**：需要配置合适的超时时间

## 6. 未来扩展

### 6.1 多地域支持

支持配置多个 FRP 地址，根据客户端地理位置选择最优地址：

```toml
[server]
public_urls = [
    "wss://cn.signaling.example.com/ws",
    "wss://us.signaling.example.com/ws",
]
```

### 6.2 动态地址

支持从环境变量或外部服务获取 FRP 地址：

```toml
[server]
public_url_source = "env:FRP_PUBLIC_URL"
```

## 7. 参考资料

### 7.1 相关文档

- `design_frp.md` - FRP 隧道设计和实现
- `design_http2.md` - HTTP/2 统一端口设计
- `design_deployment.md` - 部署方案（包含 Traefik 配置）

### 7.2 外部资源

- [FRP 官方文档](https://github.com/fatedier/frp)
- [WebSocket over HTTPS](https://developer.mozilla.org/en-US/docs/Web/API/WebSockets_API)
- [Traefik 官方文档](https://doc.traefik.io/traefik/)

## 8. 实施总结

### 8.1 已完成功能

**Server 端** ✅
- 配置文件支持 `public_url` 字段
- 配置结构体添加 `PublicURL` 字段
- Proto 定义使用通用命名（token/server/port）避免 frp 前缀
- gRPC 服务根据配置返回正确的连接信息
- 向后兼容：不配置时保持原有行为

**Desktop 端** ✅
- 支持从 Server 获取隧道连接信息
- 实现配置优先级：BUILD_URL > 配置文件 > 默认值
- 支持编译时注入 BUILD_URL
- Login 方法正确处理 Server 返回的连接信息
- 更新 proto 文件字段名使用通用命名
- 更新构建脚本支持 BUILD_URL

**Agent 端** ✅
- 支持从 Server 获取隧道连接信息
- 实现配置优先级：环境变量 > 配置文件 > BUILD_URL > 默认值
- 支持编译时注入 BUILD_URL
- FRPManager 支持动态更新连接配置
- 更新字段名使用通用命名

**构建脚本** ✅
- Server/Agent 构建脚本支持 BUILD_URL 环境变量
- Desktop 构建脚本支持 BUILD_URL 环境变量
- 自动注入到二进制文件
- 示例：`BUILD_URL=https://signaling.example.com ./scripts/build.sh`

**部署配置** ✅
- 更新 Kubernetes ConfigMap 示例
- 添加 public_url 配置说明

### 8.2 使用示例

**开发环境（不配置公网 URL）**
```toml
[server]
public_url = ""
```
Desktop 和 Agent 自动使用默认端口连接。

**生产环境（配置公网 URL）**
```toml
[server]
public_url = "wss://signaling.example.com/ws"
```
Desktop 和 Agent 使用配置的公网地址连接。

**编译时注入**
```bash
BUILD_URL="https://signaling.example.com" ./scripts/build.sh
```
Agent 二进制内置默认 Server 地址。

### 8.3 待完成工作

- [ ] 编写 Traefik 配置指南
- [ ] 添加 Ingress 配置示例
- [ ] 完整的端到端测试
  - [ ] 开发环境测试（不配置 public_url）
  - [ ] 生产环境测试（配置 public_url）
  - [ ] BUILD_URL 注入测试

## 9. 变更历史

| 日期       | 版本 | 变更内容                                | 作者 |
| ---------- | ---- | --------------------------------------- | ---- |
| 2025-11-28 | v1.0 | 初始设计                                | -    |
| 2025-11-28 | v1.1 | 完成 Server 端和 Agent 端实现，更新命名 | -    |
