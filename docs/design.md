# AWECloud-Signaling 设计方案

## 1. 项目概述

### 1.1 项目组成

```
awecloud-signaling/
├── awecloud-signaling-server/     # 信令服务器
│   ├── cmd/
│   │   ├── server/                # Server端程序（包含Web和FRP两个服务）
│   │   └── agent/                 # Agent端程序（包含Web和FRP两个模块）
│   └── web/                       # 管理网页
│
└── awecloud-signaling-desktop/    # 客户端应用（包含Web和FRP两个模块）
    └── 跨平台桌面应用
```

### 1.2 系统架构

```
                    ┌─────────────────────────────────┐
                    │      公有云（有公网IP）          │
                    │                                 │
                    │    ┌──────────────────┐         │
                    │    │  Traefik网关      │         │
                    │    │  (TLS终止)        │         │
                    │    └────────┬─────────┘         │
                    │             │                   │
                    │   ┌─────────┴─────────┐         │
                    │   │                   │         │
                    │   │ /                 │ /ws     │
                    │   ▼                   ▼         │
                    │ ┌──────────┐    ┌──────────┐   │
                    │ │Server-Web│    │Server-FRP│   │
                    │ │  :8080   │◄──►│  :7000   │   │
                    │ │HTTP+gRPC │    │WebSocket │   │
                    │ └────▲─────┘    └────▲─────┘   │
                    └──────┼───────────────┼─────────┘
                           │               │
                           │   互联网       │
        ┌──────────────────┼───────────────┼──────────────────┐
        │                  │               │                  │
        │ gRPC             │               │ WSS              │ WSS
        │ (主动连接)        │               │ (主动连接)        │ (主动连接)
        │                  │               │                  │
  ┌─────┴─────┐      ┌─────┴─────┐   ┌─────┴─────┐    ┌──────┴──────┐
  │   Agent   │      │ Desktop   │   │ Desktop   │    │  Desktop    │
  │ (内网环境) │      │ (用户A)    │   │ (用户B)    │    │  (用户C)    │
  │           │      │ (内网)     │   │ (内网)     │    │  (内网)     │
  │┌─────────┐│      │┌─────────┐│   │┌─────────┐│    │┌──────────┐ │
  ││Agent-Web││      ││Desktop  ││   ││Desktop  ││    ││Desktop   │ │
  ││  gRPC   ││      ││  -Web   ││   ││  -Web   ││    ││  -Web    │ │
  │└────┬────┘│      ││  gRPC   ││   ││  gRPC   ││    ││  gRPC    │ │
  │     │内部 │      │└────┬────┘│   │└────┬────┘│    │└────┬─────┘ │
  │┌────┴────┐│      │┌────┴────┐│   │┌────┴────┐│    │┌────┴─────┐│
  ││Agent-FRP││◄─────┼┤Desktop  ││   ││Desktop  ││    ││Desktop   ││
  ││WebSocket││ STCP ││  -FRP   ││   ││  -FRP   ││    ││  -FRP    ││
  │└─────────┘│ 隧道 ││WebSocket││   ││WebSocket││    ││WebSocket ││
  └─────┬─────┘      │└─────────┘│   │└─────────┘│    │└──────────┘│
        │            └───────────┘   └───────────┘    └────────────┘
        │ 本地TCP
        │
   ┌────┴────┬────────┐
   ▼         ▼        ▼
 [SSH]   [MySQL]  [Redis]
 (内网服务)
```

**架构说明**：

1. **Server（公有云）**：
   - 部署在公有云，有公网IP和域名
   - Traefik作为网关，提供TLS终止
   - Server-Web和Server-FRP在同一进程内通信

2. **Agent（内网）**：
   - 部署在研发环境的内网
   - 没有公网IP，但可以访问互联网
   - **主动连接**到Server（gRPC + WebSocket）
   - 提供对内网服务的访问（SSH、MySQL等）

3. **Desktop（内网）**：
   - 运行在用户电脑上（通常在内网）
   - 没有公网IP，但可以访问互联网
   - **主动连接**到Server（gRPC + WebSocket）
   - 通过STCP隧道访问Agent提供的服务

4. **连接方向**：
   - Agent-Web → Server-Web（gRPC，主动连接）
   - Agent-FRP → Server-FRP（WebSocket，主动连接）
   - Desktop-Web → Server-Web（gRPC，主动连接）
   - Desktop-FRP → Server-FRP（WebSocket，主动连接）
   - Desktop-FRP ↔ Agent-FRP（STCP隧道，通过Server-FRP协调）

### 1.3 架构说明

**Traefik网关路由**：
- `https://your-domain.com/` → Server-Web:8080 (HTTPS/HTTP + gRPC)
- `wss://your-domain.com/ws` → Server-FRP:7000 (WSS/WebSocket)

**Server组件**（单一进程，两个独立服务）：

1. **Server-Web** (端口8080)：
   - 接收来自Traefik的HTTPS/HTTP流量（路径 `/`）
   - 提供Web管理界面（HTTP）
   - 提供RESTful API（HTTP）
   - 提供gRPC服务（gRPC over HTTP/2）
   - 接收Agent-Web的gRPC连接（管理和控制Agent）
   - 接收Desktop-Web的gRPC连接（认证和服务列表）
   - 管理Agent、Client、STCP实例
   - 通过内部接口控制Server-FRP

2. **Server-FRP** (端口7000)：
   - 接收来自Traefik的WSS/WebSocket流量（路径 `/ws`）
   - 提供FRP信令服务（WebSocket）
   - 接收Agent-FRP和Desktop-FRP的WebSocket连接
   - 协调STCP隧道建立（Agent-FRP ↔ Desktop-FRP）
   - 接收Server-Web的控制指令（内部接口）

**Agent组件**（单一进程，两个模块）：

1. **Agent-Web**：
   - 通过gRPC连接到Server-Web（经过Traefik）
   - 连接地址：`https://your-domain.com/`（gRPC over HTTP/2）
   - 接收管理指令（创建/删除STCP代理）
   - 发送心跳和状态更新
   - 通过内部接口控制Agent-FRP

2. **Agent-FRP**：
   - 通过WebSocket连接到Server-FRP（经过Traefik）
   - 连接地址：`wss://your-domain.com/ws`
   - 建立STCP代理
   - 访问本地服务（SSH、MySQL、Redis等）
   - 接收Agent-Web的控制指令

**Desktop组件**（单一进程，两个模块）：

1. **Desktop-Web**：
   - 通过gRPC连接到Server-Web（经过Traefik）
   - 连接地址：`https://your-domain.com/`（gRPC over HTTP/2）
   - 认证和获取可访问服务列表
   - 管理连接状态
   - 通过内部接口控制Desktop-FRP

2. **Desktop-FRP**：
   - 通过WebSocket连接到Server-FRP（经过Traefik）
   - 连接地址：`wss://your-domain.com/ws`
   - 作为Visitor建立STCP隧道
   - 提供本地端口供用户访问
   - 接收Desktop-Web的控制指令

**通信协议总结**：
- **管理通道（gRPC over HTTP/2）**：
  - Agent-Web → Server-Web（经过Traefik，路径 `/`）
  - Desktop-Web → Server-Web（经过Traefik，路径 `/`）
- **信令通道（WebSocket）**：
  - Agent-FRP → Server-FRP（经过Traefik，路径 `/ws`）
  - Desktop-FRP → Server-FRP（经过Traefik，路径 `/ws`）
- **数据通道（STCP隧道）**：
  - Agent-FRP ↔ Desktop-FRP（点对点加密隧道）
- **内部通信（进程内接口）**：
  - Agent-Web ↔ Agent-FRP（同一进程内）
  - Desktop-Web ↔ Desktop-FRP（同一进程内）
  - Server-Web ↔ Server-FRP（同一进程内）

## 2. Server端设计

### 2.1 Server-Web服务

**端口**: 8080

**功能**:
1. Web管理界面（HTTP）
2. RESTful API
3. gRPC服务（Agent管理）

**API设计**:

```
# 管理员认证
POST   /api/admin/login
POST   /api/admin/logout

# Agent管理
GET    /api/agents
POST   /api/agents
DELETE /api/agents/:id
POST   /api/agents/:id/regenerate-token

# Client管理
GET    /api/clients
POST   /api/clients
PUT    /api/clients/:id/disable
PUT    /api/clients/:id/enable
DELETE /api/clients/:id
POST   /api/clients/:id/regenerate-secret

# STCP实例管理
GET    /api/stcp-instances
POST   /api/stcp-instances
DELETE /api/stcp-instances/:id
POST   /api/stcp-instances/:id/grant-access
DELETE /api/stcp-instances/:id/revoke-access

# Client端API
POST   /api/client/auth
GET    /api/client/services
```

**gRPC服务设计**:

```protobuf
service AgentService {
  // Agent注册和心跳
  rpc Register(RegisterRequest) returns (RegisterResponse);
  rpc Heartbeat(HeartbeatRequest) returns (HeartbeatResponse);
  
  // 接收Server指令（流式）
  rpc ReceiveCommands(stream CommandResponse) returns (stream Command);
  
  // 状态上报
  rpc ReportStatus(StatusReport) returns (StatusResponse);
}

message RegisterRequest {
  string agent_name = 1;
  string agent_token = 2;
}

message Command {
  enum Type {
    CREATE_STCP = 0;
    DELETE_STCP = 1;
  }
  Type type = 1;
  string instance_name = 2;
  string secret_key = 3;
  string local_ip = 4;
  int32 local_port = 5;
}
```

### 2.2 Server-FRP服务

**端口**: 7000

**功能**:
1. FRP信令服务（WebSocket）
2. 接收Agent-FRP和Client-FRP连接
3. 协调STCP隧道建立

**内部接口**:
- 接收Server-Web的控制指令
- 管理Agent-FRP和Client-FRP的连接状态

## 3. Agent端设计

### 3.1 Agent-Web模块

**功能**:
1. 通过gRPC连接到Server-Web
2. 注册和心跳
3. 接收管理指令
4. 控制Agent-FRP模块

**工作流程**:
```
1. Agent启动
   ↓
2. Agent-Web通过gRPC连接Server-Web
   ↓
3. 发送注册请求（agent_name + token）
   ↓
4. Server-Web验证通过
   ↓
5. 建立gRPC流，接收Server指令
   ↓
6. 定期发送心跳
   ↓
7. 接收到创建STCP指令
   ↓
8. 通知Agent-FRP创建代理
```

### 3.2 Agent-FRP模块

**功能**:
1. 通过WebSocket连接到Server-FRP
2. 建立STCP代理
3. 访问本地服务

**工作流程**:
```
1. Agent-FRP启动
   ↓
2. 通过WebSocket连接Server-FRP
   ↓
3. 等待Agent-Web的指令
   ↓
4. 接收创建STCP代理指令
   ↓
5. 创建STCP代理配置
   ↓
6. 等待Client-FRP连接
   ↓
7. 建立STCP隧道
   ↓
8. 转发流量到本地服务
```

## 4. Client/Desktop端设计

### 4.1 Client-Web/Desktop-Web模块

**功能**:
1. 通过gRPC连接到Server-Web（经过Traefik）
2. 连接地址：`https://your-domain.com/`（gRPC over HTTP/2）
3. 认证和获取可访问服务列表
4. 管理连接状态
5. 通过内部接口控制Desktop-FRP模块

**工作流程**:
```
1. Desktop启动
   ↓
2. Desktop-Web通过gRPC连接Server-Web
   ↓
3. 发送认证请求（client_id + secret）
   ↓
4. Server-Web验证通过，返回可访问服务列表
   ↓
5. 用户选择要连接的服务
   ↓
6. Desktop-Web通知Desktop-FRP建立连接
   ↓
7. 定期发送心跳和状态更新
```

### 4.2 Client-FRP/Desktop-FRP模块

**功能**:
1. 通过WebSocket连接到Server-FRP（经过Traefik）
2. 连接地址：`wss://your-domain.com/ws`
3. 作为Visitor建立STCP隧道
4. 提供本地端口供用户访问
5. 接收Desktop-Web的控制指令

**工作流程**:
```
1. Desktop-FRP启动
   ↓
2. 通过WebSocket连接Server-FRP
   ↓
3. 等待Desktop-Web的连接指令
   ↓
4. 接收连接指令（服务名、secret_key等）
   ↓
5. 创建STCP Visitor配置
   ↓
6. 通过Server-FRP连接到对应的Agent-FRP
   ↓
7. 建立STCP加密隧道
   ↓
8. 监听本地端口（如 127.0.0.1:3306）
   ↓
9. 转发用户流量到Agent-FRP
```

## 5. 数据库设计

### 5.1 数据库选型

使用 **SQLite** 作为数据库，原因：
- 轻量级，无需独立数据库服务
- 适合中小规模部署
- 支持完整的SQL功能
- 易于备份和迁移

### 5.2 数据表设计

#### 5.2.1 管理员表 (admins)

```sql
CREATE TABLE admins (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

**字段说明**：
- `id`: 主键
- `username`: 管理员用户名（唯一）
- `password_hash`: 密码哈希（bcrypt）
- `created_at`: 创建时间
- `updated_at`: 更新时间

#### 5.2.2 Agent表 (agents)

```sql
CREATE TABLE agents (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    agent_name TEXT NOT NULL UNIQUE,
    agent_token TEXT NOT NULL UNIQUE,
    status TEXT DEFAULT 'offline',
    last_heartbeat DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

**字段说明**：
- `id`: 主键
- `agent_name`: Agent名称（唯一）
- `agent_token`: 认证Token（唯一）
- `status`: 状态（online/offline）
- `last_heartbeat`: 最后心跳时间
- `created_at`: 创建时间
- `updated_at`: 更新时间

#### 5.2.3 Client表 (clients)

```sql
CREATE TABLE clients (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    client_id TEXT NOT NULL UNIQUE,
    client_secret TEXT NOT NULL,
    enabled BOOLEAN DEFAULT 1,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

**字段说明**：
- `id`: 主键
- `client_id`: Client标识（用户名/邮箱，唯一）
- `client_secret`: 认证密钥
- `enabled`: 是否启用
- `created_at`: 创建时间
- `updated_at`: 更新时间

#### 5.2.4 STCP实例表 (stcp_instances)

```sql
CREATE TABLE stcp_instances (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    instance_name TEXT NOT NULL UNIQUE,
    agent_id INTEGER NOT NULL,
    secret_key TEXT NOT NULL,
    local_ip TEXT NOT NULL,
    local_port INTEGER NOT NULL,
    description TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELE

## 6. 部署方案

部署方案保持不变，参考原design.md和Kubernetes配置。

**Traefik路由**:
- `https://your-domain.com/` → Server-Web:8080 (HTTP + gRPC)
- `wss://your-domain.com/ws` → Server-FRP:7000 (WebSocket)

## 7. 关键变更总结

### 7.1 与原设计的差异

**原设计问题**:
- Agent作为单一FRP Client，直接连接Server-FRP
- 缺少管理通道，无法动态控制Agent

**新设计改进**:
- Agent拆分为Agent-Web和Agent-FRP两个模块
- 增加gRPC管理通道（Agent-Web ↔ Server-Web）
- Agent-Web接收指令，控制Agent-FRP行为
- Client/Desktop也采用相同的双模块设计

### 7.2 技术栈更新

**新增**:
- gRPC：用于Server-Web和Agent-Web之间的管理通信
- Protocol Buffers：定义gRPC接口

**保持不变**:
- HTTP/RESTful API：Web管理界面
- WebSocket：FRP信令通道
- STCP：数据隧道

---

**文档版本**: 2.0  
**最后更新**: 2025-11-25  
**状态**: 当前设计
