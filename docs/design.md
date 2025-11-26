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
- `https://your-domain.com/` → Server:8080 (HTTPS/HTTP + gRPC)
- `wss://your-domain.com/ws` → Server:7000 (WSS/WebSocket)

**Server进程**（单一进程，两个服务线程）：

Server是一个单一的Go进程，内部运行两个服务线程：

1. **Server-Web线程** (监听端口8080)：
   - 接收来自Traefik的HTTPS/HTTP流量（路径 `/`）
   - 提供Web管理界面（HTTP）
   - 提供RESTful API（HTTP）
   - 提供gRPC服务（gRPC over HTTP/2）
   - 接收Agent的gRPC连接（管理和控制Agent）
   - 接收Desktop的gRPC连接（认证和服务列表）
   - 管理Agent、Client、STCP实例
   - 通过进程内通信控制Server-FRP线程

2. **Server-FRP线程** (监听端口7000)：
   - 接收来自Traefik的WSS/WebSocket流量（路径 `/ws`）
   - 提供FRP信令服务（WebSocket）
   - 接收Agent和Desktop的WebSocket连接
   - 协调STCP隧道建立（Agent ↔ Desktop）
   - 接收Server-Web线程的控制指令（进程内通信）

**Agent进程**（单一进程，两个工作线程）：

Agent是一个单一的Go进程，内部运行两个工作线程：

1. **Agent-Web线程**：
   - 通过gRPC连接到Server-Web（经过Traefik）
   - 连接地址：`https://your-domain.com/`（gRPC over HTTP/2）
   - 接收管理指令（创建/删除STCP代理）
   - 发送心跳和状态更新
   - 通过进程内通信控制Agent-FRP线程

2. **Agent-FRP线程**：
   - 通过WebSocket连接到Server-FRP（经过Traefik）
   - 连接地址：`wss://your-domain.com/ws`
   - 建立STCP代理
   - 访问本地服务（SSH、MySQL、Redis等）
   - 接收Agent-Web线程的控制指令（进程内通信）

**Desktop进程**（单一进程，两个工作线程）：

Desktop是一个单一的Go进程（Wails应用），内部运行两个工作线程：

1. **Desktop-Web线程**：
   - 通过gRPC连接到Server-Web（经过Traefik）
   - 连接地址：`https://your-domain.com/`（gRPC over HTTP/2）
   - 认证和获取可访问服务列表
   - 管理连接状态
   - 通过进程内通信控制Desktop-FRP线程

2. **Desktop-FRP线程**：
   - 通过WebSocket连接到Server-FRP（经过Traefik）
   - 连接地址：`wss://your-domain.com/ws`
   - 作为Visitor建立STCP隧道
   - 提供本地端口供用户访问
   - 接收Desktop-Web线程的控制指令（进程内通信）

**通信协议总结**：
- **管理通道（gRPC over HTTP/2）**：
  - Agent → Server-Web线程（经过Traefik，端口8080，路径 `/`）
  - Desktop → Server-Web线程（经过Traefik，端口8080，路径 `/`）
- **信令通道（WebSocket）**：
  - Agent → Server-FRP线程（经过Traefik，端口7000，路径 `/ws`）
  - Desktop → Server-FRP线程（经过Traefik，端口7000，路径 `/ws`）
- **数据通道（STCP隧道）**：
  - Agent ↔ Desktop（点对点加密隧道，通过Server-FRP协调）
- **进程内通信（Go channel或接口调用）**：
  - Server-Web线程 ↔ Server-FRP线程（同一进程内）
  - Agent-Web线程 ↔ Agent-FRP线程（同一进程内）
  - Desktop-Web线程 ↔ Desktop-FRP线程（同一进程内）

**重要说明**：
- Server、Agent、Desktop都是**单一进程**
- 每个进程内部有**两个工作线程**（或goroutine）
- "Server-Web"、"Server-FRP"等术语指的是**进程内的服务线程**，不是独立进程
- 进程内通信使用Go的channel、接口调用或共享内存，无需网络通信

## 2. Server进程设计

Server是一个单一的Go进程，启动时会创建两个服务线程（goroutine）。

### 2.1 Server-Web线程

**监听端口**: 8080

**功能**:
1. Web管理界面（HTTP）
2. RESTful API（HTTP）
3. gRPC服务（gRPC over HTTP/2）

**API概览**:

详细的API设计请参考 [API设计文档](./design_api.md)。

- RESTful API：管理员认证、Agent管理、Client管理、STCP实例管理
- gRPC API：Agent注册/心跳/指令流、Client认证/服务查询

**进程内通信**:
- 通过Go channel或接口调用与Server-FRP线程通信
- 发送控制指令（创建/删除STCP代理）
- 查询连接状态

### 2.2 Server-FRP线程

**监听端口**: 7000

**功能**:
1. FRP信令服务（WebSocket）
2. 接收Agent和Desktop的WebSocket连接
3. 协调STCP隧道建立

**进程内通信**:
- 接收Server-Web线程的控制指令
- 上报Agent和Desktop的连接状态
- 通知STCP隧道建立结果

## 3. Agent进程设计

Agent是一个单一的Go进程，启动时会创建两个工作线程（goroutine）。

### 3.1 Agent-Web线程

**功能**:
1. 通过gRPC连接到Server-Web线程
2. 注册和心跳
3. 接收管理指令
4. 通过进程内通信控制Agent-FRP线程

**工作流程**:
```
1. Agent进程启动
   ↓
2. Agent-Web线程通过gRPC连接Server-Web
   ↓
3. 发送注册请求（agent_name + token）
   ↓
4. Server-Web验证通过
   ↓
5. 建立gRPC双向流，接收Server指令
   ↓
6. 定期发送心跳
   ↓
7. 接收到创建STCP指令
   ↓
8. 通过进程内通信通知Agent-FRP线程创建代理
```

### 3.2 Agent-FRP线程

**功能**:
1. 通过WebSocket连接到Server-FRP线程
2. 建立STCP代理
3. 访问本地服务

**工作流程**:
```
1. Agent-FRP线程启动
   ↓
2. 通过WebSocket连接Server-FRP
   ↓
3. 等待Agent-Web线程的指令（进程内通信）
   ↓
4. 接收创建STCP代理指令
   ↓
5. 创建STCP代理配置
   ↓
6. 等待Desktop连接
   ↓
7. 建立STCP隧道
   ↓
8. 转发流量到本地服务
```

## 4. Desktop进程设计

Desktop是一个单一的Go进程（Wails应用），启动时会创建两个工作线程（goroutine）。

### 4.1 Desktop-Web线程

**功能**:
1. 通过gRPC连接到Server-Web线程（经过Traefik）
2. 连接地址：`https://your-domain.com/`（gRPC over HTTP/2）
3. 认证和获取可访问服务列表
4. 管理连接状态
5. 通过进程内通信控制Desktop-FRP线程

**工作流程**:
```
1. Desktop进程启动
   ↓
2. Desktop-Web线程通过gRPC连接Server-Web
   ↓
3. 发送认证请求（client_id + secret）
   ↓
4. Server-Web验证通过，返回可访问服务列表
   ↓
5. 用户选择要连接的服务
   ↓
6. Desktop-Web通过进程内通信通知Desktop-FRP线程建立连接
   ↓
7. 定期发送心跳和状态更新
```

### 4.2 Desktop-FRP线程

**功能**:
1. 通过WebSocket连接到Server-FRP线程（经过Traefik）
2. 连接地址：`wss://your-domain.com/ws`
3. 作为Visitor建立STCP隧道
4. 提供本地端口供用户访问
5. 接收Desktop-Web线程的控制指令（进程内通信）

**工作流程**:
```
1. Desktop-FRP线程启动
   ↓
2. 通过WebSocket连接Server-FRP
   ↓
3. 等待Desktop-Web线程的连接指令（进程内通信）
   ↓
4. 接收连接指令（服务名、secret_key等）
   ↓
5. 创建STCP Visitor配置
   ↓
6. 通过Server-FRP连接到对应的Agent
   ↓
7. 建立STCP加密隧道
   ↓
8. 监听本地端口（如 127.0.0.1:3306）
   ↓
9. 转发���户流量到Agent
```

## 5. Server进程内部设计

Server进程内部运行两个服务线程，详细设计请参考 [Server内部设计文档](./design_server.md)。

**核心内容**：
- 线程架构和通信机制
- 核心业务流程（创建/删除STCP实例）
- 进程内通信接口设计
- 状态管理和错误处理

## 6. 数据库设计

使用 **SQLite** 作为数据库。详细的表结构设计请参考 [数据库设计文档](./design_database.md)。

**数据表列表**：
- `admins` - 管理员表
- `agents` - Agent表
- `clients` - Client表
- `stcp_instances` - STCP实例表
- `stcp_access` - STCP访问控制表
- `client_sessions` - Client会话表

## 7. API设计

详细的API设计请参考 [API设计文档](./design_api.md)。

**API概览**：
- RESTful API：管理界面和Client认证
- gRPC API：Agent管理和Client服务查询
- WebSocket：FRP信令通道

## 8. 部署方案

详细的部署方案请参考 [部署设计文档](./design_deployment.md)。

**部署概览**：
- Docker容器化部署
- Kubernetes集群部署
- Traefik网关配置

## 9. 关键变更总结

### 8.1 与原设计的差异

**原设计问题**:
- Agent作为单一FRP Client，直接连接Server
- 缺少管理通道，无法动态控制Agent

**新设计改进**:
- 每个组件（Server、Agent、Desktop）都是**单一进程**
- 每个进程内部运行**两个工作线程**（goroutine）
- 增加gRPC管理通道（Agent → Server-Web线程）
- Agent-Web线程接收指令，通过进程内通信控制Agent-FRP线程
- Desktop也采用相同的双线程设计

### 8.2 技术栈更新

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
