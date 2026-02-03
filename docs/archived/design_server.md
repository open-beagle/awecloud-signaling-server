# Server进程内部设计

## 1. 概述

Server是一个单一Go进程，内部运行三个服务线程：
- **Server-Web线程**：HTTP/RESTful API + gRPC服务（端口8080/8081）
- **Server-FRP线程**：FRP信令服务（端口7000）
- **进程内通信**：两个线程之间通过Go channel通信

## 2. 线程架构

```
Server进程
├── Server-Web线程
│   ├── HTTP服务（端口8080）
│   │   ├── RESTful API（管理员、Agent、Client、STCP）
│   │   └── Web管理界面
│   └── gRPC服务（端口8081）
│       ├── AgentService（Agent管理）
│       └── ClientService（Client认证和服务查询）
│
├── Server-FRP线程
│   └── FRP信令服务（端口7000）
│       ├── WebSocket连接管理
│       ├── Agent连接
│       ├── Desktop连接
│       └── STCP隧道协调
│
└── 进程内通信
    ├── Command Channel（Web → FRP）
    └── Event Channel（FRP → Web）
```

## 3. 核心业务流程

### 3.1 创建STCP实例

**参与者**：
- 管理员（通过Web界面）
- Server-Web线程（RESTful API）
- Server-Web线程（gRPC AgentService）
- Agent（gRPC客户端）
- Agent-FRP（FRP客户端）
- Server-FRP线程（FRP服务端）

**流程**：

```
管理员                Server-Web(API)      Server-Web(gRPC)     Agent(gRPC)      Agent-FRP        Server-FRP
  |                        |                      |                  |                |                |
  |--POST /api/stcp------->|                      |                  |                |                |
  |  instances             |                      |                  |                |                |
  |                        |                      |                  |                |                |
  |                        |--保存到数据库-------->|                  |                |                |
  |                        |  (instance_name,     |                  |                |                |
  |                        |   secret_key, etc)   |                  |                |                |
  |                        |                      |                  |                |                |
  |                        |--查询Agent是否在线--->|                  |                |                |
  |                        |                      |                  |                |                |
  |                        |                      |--检查gRPC连接--->|                |                |
  |                        |                      |                  |                |                |
  |                        |                      |--发送CREATE_STCP->|                |                |
  |                        |                      |  Command         |                |                |
  |                        |                      |  (通过双向流)     |                |                |
  |                        |                      |                  |                |                |
  |                        |                      |                  |--进程内通信--->|                |
  |                        |                      |                  |  (Go channel)  |                |
  |                        |                      |                  |                |                |
  |                        |                      |                  |                |--创建STCP代理-->|
  |                        |                      |                  |                |  (FRP配置)     |
  |                        |                      |                  |                |                |
  |                        |                      |                  |                |<--代理创建成功--|
  |                        |                      |                  |                |                |
  |                        |                      |                  |<--返回成功-----|                |
  |                        |                      |                  |                |                |
  |                        |                      |<--CommandResponse-|                |                |
  |                        |                      |  (success=true)  |                |                |
  |                        |                      |                  |                |                |
  |<--返回成功-------------|                      |                  |                |                |
  |  {success: true,       |                      |                  |                |                |
  |   instance: {...}}     |                      |                  |                |                |
```

**关键点**：
1. RESTful API先保存到数据库
2. 检查Agent是否在线（gRPC连接是否存在）
3. 如果在线，通过gRPC双向流发送CREATE_STCP指令
4. Agent收到指令后，通过进程内通信通知Agent-FRP
5. Agent-FRP创建STCP代理并连接到Server-FRP
6. 返回成功响应

### 3.2 删除STCP实例

**流程**：

```
管理员                Server-Web(API)      Server-Web(gRPC)     Agent(gRPC)      Agent-FRP        Server-FRP
  |                        |                      |                  |                |                |
  |--DELETE /api/stcp----->|                      |                  |                |                |
  |  instances/:id         |                      |                  |                |                |
  |                        |                      |                  |                |                |
  |                        |--查询实例信息-------->|                  |                |                |
  |                        |  (agent_id,          |                  |                |                |
  |                        |   instance_name)     |                  |                |                |
  |                        |                      |                  |                |                |
  |                        |--检查Agent是否在线--->|                  |                |                |
  |                        |                      |                  |                |                |
  |                        |                      |--发送DELETE_STCP->|                |                |
  |                        |                      |  Command         |                |                |
  |                        |                      |                  |                |                |
  |                        |                      |                  |--进程内通信--->|                |
  |                        |                      |                  |                |                |
  |                        |                      |                  |                |--删除STCP代理-->|
  |                        |                      |                  |                |                |
  |                        |                      |                  |                |<--代理删除成功--|
  |                        |                      |                  |                |                |
  |                        |                      |                  |<--返回成功-----|                |
  |                        |                      |                  |                |                |
  |                        |                      |<--CommandResponse-|                |                |
  |                        |                      |                  |                |                |
  |                        |--从数据库删除-------->|                  |                |                |
  |                        |                      |                  |                |                |
  |<--返回成功-------------|                      |                  |                |                |
```

**关键点**：
1. 先发送删除指令给Agent
2. 等待Agent确认删除成功
3. 再从数据库中删除记录
4. 如果Agent离线，直接删除数据库记录

### 3.3 Agent注册和连接

**流程**：

```
Agent(gRPC)          Server-Web(gRPC)     Server-Web(API)      Agent-FRP        Server-FRP
  |                        |                      |                  |                |
  |--Register------------->|                      |                  |                |
  |  (agent_name, token)   |                      |                  |                |
  |                        |                      |                  |                |
  |                        |--验证Token---------->|                  |                |
  |                        |  查询数据库           |                  |                |
  |                        |                      |                  |                |
  |                        |<--验证成功-----------|                  |                |
  |                        |                      |                  |                |
  |                        |--更新状态为online---->|                  |                |
  |                        |                      |                  |                |
  |<--RegisterResponse-----|                      |                  |                |
  |  (success, agent_id)   |                      |                  |                |
  |                        |                      |                  |                |
  |--ReceiveCommands------>|                      |                  |                |
  |  (建立双向流)           |                      |                  |                |
  |                        |                      |                  |                |
  |                        |--注册stream--------->|                  |                |
  |                        |  保存到内存           |                  |                |
  |                        |                      |                  |                |
  |                        |                      |                  |--WebSocket---->|
  |                        |                      |                  |  连接          |
  |                        |                      |                  |                |
  |                        |                      |                  |<--连接成功-----|
  |                        |                      |                  |                |
  |<--等待指令-------------|                      |                  |                |
  |  (stream保持打开)       |                      |                  |                |
```

**关键点**：
1. Agent先通过gRPC注册
2. 建立双向流连接，保持长连接
3. Agent-FRP通过WebSocket连接到Server-FRP
4. 两个连接都保持活跃，等待指令

### 3.4 Desktop连接服务

**流程**：

```
Desktop(gRPC)        Server-Web(gRPC)     Server-Web(API)      Desktop-FRP      Server-FRP       Agent-FRP
  |                        |                      |                  |                |                |
  |--Authenticate--------->|                      |                  |                |                |
  |  (client_id, secret)   |                      |                  |                |                |
  |                        |                      |                  |                |                |
  |                        |--验证---------------->|                  |                |                |
  |                        |  查询数据库           |                  |                |                |
  |                        |                      |                  |                |                |
  |<--AuthResponse---------|                      |                  |                |                |
  |  (session_token)       |                      |                  |                |                |
  |                        |                      |                  |                |                |
  |--GetServices---------->|                      |                  |                |                |
  |  (session_token)       |                      |                  |                |                |
  |                        |                      |                  |                |                |
  |                        |--查询权限------------>|                  |                |                |
  |                        |  (stcp_access表)     |                  |                |                |
  |                        |                      |                  |                |                |
  |<--ServicesResponse-----|                      |                  |                |                |
  |  (服务列表)             |                      |                  |                |                |
  |                        |                      |                  |                |                |
  |--ConnectService------->|                      |                  |                |                |
  |  (instance_id)         |                      |                  |                |                |
  |                        |                      |                  |                |                |
  |                        |--查询实例信息-------->|                  |                |                |
  |                        |  (secret_key等)      |                  |                |                |
  |                        |                      |                  |                |                |
  |<--ConnectResponse------|                      |                  |                |                |
  |  (secret_key,          |                      |                  |                |                |
  |   instance_name)       |                      |                  |                |                |
  |                        |                      |                  |                |                |
  |--进程内通信------------>|                      |                  |                |                |
  |  通知Desktop-FRP        |                      |                  |                |                |
  |                        |                      |                  |                |                |
  |                        |                      |                  |--WebSocket---->|                |
  |                        |                      |                  |  连接          |                |
  |                        |                      |                  |                |                |
  |                        |                      |                  |--请求STCP----->|                |
  |                        |                      |                  |  (secret_key)  |                |
  |                        |                      |                  |                |                |
  |                        |                      |                  |                |--协调连接----->|
  |                        |                      |                  |                |                |
  |                        |                      |                  |                |<--建立隧道-----|
  |                        |                      |                  |                |                |
  |                        |                      |                  |<--隧道就绪-----|                |
  |                        |                      |                  |                |                |
  |--监听本地端口---------->|                      |                  |                |                |
  |  (如127.0.0.1:3306)    |                      |                  |                |                |
```

**关键点**：
1. Desktop先通过gRPC认证获取session_token
2. 查询可访问的服务列表
3. 选择服务后获取连接信息（secret_key）
4. Desktop-FRP使用secret_key通过Server-FRP连接到Agent-FRP
5. 建立STCP加密隧道
6. Desktop监听本地端口，用户可以访问

## 4. 进程内通信机制

### 4.1 通信接口设计

```go
// CommandBus 命令总线（Server-Web → Agent）
type CommandBus struct {
    // Agent命令队列（通过gRPC发送）
    agentCommands map[int64]chan *pb.Command
    mutex         sync.RWMutex
}

// EventBus 事件总线（Agent → Server-Web，FRP → Server-Web）
type EventBus struct {
    // 事件订阅者
    subscribers map[string][]chan Event
    mutex       sync.RWMutex
}

// Event 事件
type Event struct {
    Type      string      // 事件类型
    Source    string      // 事件源
    Data      interface{} // 事件数据
    Timestamp time.Time   // 时间戳
}
```

### 4.2 命令类型

```go
// 命令类型
const (
    CommandCreateSTCP  = "CREATE_STCP"
    CommandDeleteSTCP  = "DELETE_STCP"
    CommandUpdateSTCP  = "UPDATE_STCP"
)

// 事件类型
const (
    EventAgentOnline    = "AGENT_ONLINE"
    EventAgentOffline   = "AGENT_OFFLINE"
    EventSTCPCreated    = "STCP_CREATED"
    EventSTCPDeleted    = "STCP_DELETED"
    EventSTCPConnected  = "STCP_CONNECTED"
    EventSTCPDisconnected = "STCP_DISCONNECTED"
)
```

### 4.3 通信流程

**发送命令**：
```
RESTful API
    ↓
查询Agent是否在线（AgentService.IsAgentOnline）
    ↓
发送命令（AgentService.SendCommand）
    ↓
命令进入队列（commandQueues[agentID]）
    ↓
gRPC双向流发送（stream.Send）
    ↓
Agent接收命令
```

**接收响应**：
```
Agent发送响应
    ↓
gRPC双向流接收（stream.Recv）
    ↓
处理响应（记录日志、更新状态）
    ↓
可选：发布事件到EventBus
    ↓
订阅者接收事件
```

## 5. 数据流向

### 5.1 管理数据流

```
管理员 → RESTful API → 数据库
                    ↓
                gRPC Service → Agent
```

### 5.2 认证数据流

```
Client → gRPC Service → 数据库
                      ↓
                  JWT Token
```

### 5.3 服务数据流

```
Desktop → gRPC Service → 数据库（查询权限）
                       ↓
                   连接信息（secret_key）
                       ↓
        Desktop-FRP → Server-FRP → Agent-FRP
                       ↓
                   STCP隧道
```

## 6. 状态管理

### 6.1 Agent状态

- **online**: Agent已连接（gRPC stream存在）
- **offline**: Agent未连接

**状态转换**：
```
offline → Register → online
online → 断开连接 → offline
online → 心跳超时 → offline
```

### 6.2 STCP实例状态

当前设计中STCP实例没有状态字段，状态由Agent上报。

**可能的状态**（未来扩展）：
- **pending**: 已创建，等待Agent创建代理
- **active**: 代理已创建，可以连接
- **error**: 创建失败或运行错误
- **deleted**: 已删除

## 7. 错误处理

### 7.1 Agent离线

**场景**：管理员创建STCP实例时，Agent离线

**处理**：
1. 保存到数据库
2. 返回成功，但标注"Agent离线，将在Agent上线后自动创建"
3. Agent上线时，查询数据库中的实例，批量创建

### 7.2 命令发送失败

**场景**：发送命令时，gRPC连接断开

**处理**：
1. 捕获错误
2. 更新Agent状态为offline
3. 返回错误给管理员
4. 保留数据库记录，等待Agent重连

### 7.3 命令执行失败

**场景**：Agent收到命令，但执行失败

**处理**：
1. Agent返回失败响应
2. Server记录错误日志
3. 可选：更新数据库状态
4. 返回错误给管理员

## 8. 性能考虑

### 8.1 连接管理

- gRPC连接：长连接，复用
- WebSocket连接：长连接，复用
- 连接池：不需要，每个Agent/Desktop一个连接

### 8.2 并发控制

- 命令队列：带缓冲channel（100）
- 事件总线：带缓冲channel（1000）
- 锁粒度：细粒度锁，减少竞争

### 8.3 内存管理

- 定期清理过期会话（client_sessions）
- 定期清理离线Agent的命令队列
- 限制事件历史记录数量

## 9. 安全考虑

### 9.1 认证

- Agent：使用agent_token认证
- Client：使用client_secret认证，生成JWT
- 管理员：使用密码认证，生成JWT

### 9.2 授权

- Agent：只能访问自己的STCP实例
- Client：只能访问授权的STCP实例
- 管理员：可以访问所有资源

### 9.3 通信安全

- gRPC：支持TLS（可选）
- WebSocket：支持WSS（可选）
- STCP隧道：使用secret_key加密

## 10. 监控和日志

### 10.1 关键指标

- Agent在线数量
- STCP实例数量
- 活跃连接数量
- 命令成功率
- 命令响应时间

### 10.2 日志记录

- Agent注册/离线
- STCP实例创建/删除
- 命令发送/响应
- 错误和异常

---

**文档版本**: 1.0  
**最后更新**: 2025-11-25  
**状态**: 设计中
