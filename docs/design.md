# AWECloud-Signaling 设计方案

## 1. 项目概述

**AWECloud Signaling** 是一个基于内网穿透技术的安全访问系统，允许用户通过 Desktop 客户端安全地访问部署在内网的服务（如 SSH、MySQL、Redis 等）。

系统由三个核心组件组成：

- **Server**：部署在公有云，作为信令服务器和流量中继
- **Agent**：部署在内网环境，提供对内网服务的访问
- **Desktop**：运行在用户电脑，作为访问内网服务的客户端

### 1.1 系统组成

```
AWECloud Signaling/
├── Server/                        # 服务端（部署在公有云）
│   ├── cmd/server/                # Server进程入口
│   ├── internal/server/           # Server实现
│   │   ├── api/                   # Server-Web线程（HTTP/gRPC）
│   │   └── frp/                   # Server-FRP线程（WebSocket）
│   └── web/                       # 管理网页
│
├── Agent/                         # Agent端（部署在内网）
│   ├── cmd/agent/                 # Agent进程入口
│   └── internal/agent/            # Agent实现
│       ├── web/                   # Agent-Web线程（gRPC客户端）
│       └── frp/                   # Agent-FRP线程（WebSocket客户端）
│
└── Desktop/                       # Desktop端（用户桌面应用）
    ├── frontend/                  # 前端界面（Wails）
    └── backend/                   # 后端实现
        ├── web/                   # Desktop-Web线程（gRPC客户端）
        └── frp/                   # Desktop-FRP线程（WebSocket客户端）
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
                    │ │HTTP/2    │    │WebSocket │   │
                    │ │RESTful   │    │          │   │
                    │ │+ gRPC    │    │          │   │
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

   - 部署在公有云，有公网 IP 和域名
   - Traefik 作为网关，提供 TLS 终止
   - Server-Web 和 Server-FRP 在同一进程内通信

2. **Agent（内网）**：

   - 部署在研发环境的内网
   - 没有公网 IP，但可以访问互联网
   - **主动连接**到 Server（gRPC + WebSocket）
   - 提供对内网服务的访问（SSH、MySQL 等）

3. **Desktop（内网）**：

   - 运行在用户电脑上（通常在内网）
   - 没有公网 IP，但可以访问互联网
   - **主动连接**到 Server（gRPC + WebSocket）
   - 通过 STCP 隧道访问 Agent 提供的服务

4. **连接方向**：
   - Agent-Web → Server-Web（gRPC，主动连接）
   - Agent-FRP → Server-FRP（WebSocket，主动连接）
   - Desktop-Web → Server-Web（gRPC，主动连接）
   - Desktop-FRP → Server-FRP（WebSocket，主动连接）
   - Desktop-FRP ↔ Agent-FRP（STCP 隧道，通过 Server-FRP 协调）

### 1.3 组件详细说明

**Traefik 网关路由**：

- `https://your-domain.com/` → Server:8080 (HTTP/2 统一端口: Web UI + REST API + gRPC)
- `wss://your-domain.com/ws` → Server:7000 (WebSocket: FRP 信令)

**Server 进程**（单一进程，两个服务线程）：

Server 是一个单一的 Go 进程，内部运行两个服务线程：

1. **Server-Web 线程** (监听端口 8080)：

   - 接收来自 Traefik 的 HTTPS 流量（路径 `/`）
   - **统一使用 HTTP/2 协议，同时支持 HTTP 和 gRPC**
   - 提供 Web 管理界面（HTTP/1.1 或 HTTP/2）
   - 提供 RESTful API（HTTP/1.1 或 HTTP/2）
   - 提供 gRPC 服务（HTTP/2，通过 Content-Type 区分）
   - 接收 Agent 的 gRPC 连接（管理和控制 Agent）
   - 接收 Desktop 的 gRPC 连接（认证和服务列表）
   - 管理 Agent、Client、STCP 实例
   - 通过进程内通信控制 Server-FRP 线程

2. **Server-FRP 线程** (监听端口 7000)：
   - 接收来自 Traefik 的 WSS/WebSocket 流量（路径 `/ws`）
   - 提供 FRP 信令服务（WebSocket）
   - 接收 Agent 和 Desktop 的 WebSocket 连接
   - 协调 STCP 隧道建立（Agent ↔ Desktop）
   - 接收 Server-Web 线程的控制指令（进程内通信）

**Agent 进程**（单一进程，两个工作线程）：

Agent 是一个单一的 Go 进程，内部运行两个工作线程：

1. **Agent-Web 线程**：

   - 通过 gRPC 连接到 Server-Web（经过 Traefik）
   - 连接地址：`https://your-domain.com/`（gRPC over HTTP/2）
   - 接收管理指令（创建/删除 STCP 代理）
   - 发送心跳和状态更新
   - 通过进程内通信控制 Agent-FRP 线程

2. **Agent-FRP 线程**：
   - 通过 WebSocket 连接到 Server-FRP（经过 Traefik）
   - 连接地址：`wss://your-domain.com/ws`
   - 建立 STCP 代理
   - 访问本地服务（SSH、MySQL、Redis 等）
   - 接收 Agent-Web 线程的控制指令（进程内通信）

**Desktop 进程**（单一进程，两个工作线程）：

Desktop 是一个单一的 Go 进程（Wails 应用），内部运行两个工作线程：

1. **Desktop-Web 线程**：

   - 通过 gRPC 连接到 Server-Web（经过 Traefik）
   - 连接地址：`https://your-domain.com/`（gRPC over HTTP/2）
   - 认证和获取可访问服务列表
   - 管理连接状态
   - 通过进程内通信控制 Desktop-FRP 线程

2. **Desktop-FRP 线程**：
   - 通过 WebSocket 连接到 Server-FRP（经过 Traefik）
   - 连接地址：`wss://your-domain.com/ws`
   - 作为 Visitor 建立 STCP 隧道
   - 提供本地端口供用户访问
   - 接收 Desktop-Web 线程的控制指令（进程内通信）

**通信协议总结**：

- **管理通道（HTTP/2 统一端口）**：
  - Agent → Server-Web 线程（经过 Traefik，端口 8080，路径 `/`）
    - RESTful API: HTTP/1.1 或 HTTP/2
    - gRPC: HTTP/2（Content-Type: application/grpc）
  - Desktop → Server-Web 线程（经过 Traefik，端口 8080，路径 `/`）
    - gRPC: HTTP/2（Content-Type: application/grpc）
  - **实现方式**：同一端口同时处理 HTTP 和 gRPC，服务器根据 Content-Type 自动路由
- **信令通道（WebSocket）**：
  - Agent → Server-FRP 线程（经过 Traefik，端口 7000，路径 `/ws`）
  - Desktop → Server-FRP 线程（经过 Traefik，端口 7000，路径 `/ws`）
- **数据通道（STCP 隧道）**：
  - Agent ↔ Desktop（点对点加密隧道，通过 Server-FRP 协调）
- **进程内通信（Go channel 或接口调用）**：
  - Server-Web 线程 ↔ Server-FRP 线程（同一进程内）
  - Agent-Web 线程 ↔ Agent-FRP 线程（同一进程内）
  - Desktop-Web 线程 ↔ Desktop-FRP 线程（同一进程内）

**重要说明**：

- **AWECloud Signaling** 是整个系统的名称
- **Server**、**Agent**、**Desktop** 都是 AWECloud Signaling 的组成部分
- 每个组件都是**单一进程**
- 每个进程内部有**两个工作线程**（goroutine）
- "Server-Web"、"Server-FRP"等术语指的是**进程内的服务线程**，不是独立进程
- 进程内通信使用 Go 的 channel、接口调用或共享内存，无需网络通信

## 2. Server 组件设计

**Server** 是 AWECloud Signaling 的服务端组件，部署在公有云上。

Server 是一个单一的 Go 进程，启动时会创建两个服务线程（goroutine）。

### 2.1 Server-Web 线程

**监听端口**: 8080

**协议**: HTTP/2（同时支持 HTTP/1.1 和 gRPC）

**实现方式**:

- 使用 HTTP/2 服务器同时处理 HTTP 和 gRPC 请求
- 根据 Content-Type 区分请求类型：
  - `application/grpc` → gRPC 处理器
  - 其他 → HTTP 处理器（Gin 路由）

**功能**:

1. Web 管理界面（HTTP）
2. RESTful API（HTTP）
3. gRPC 服务（gRPC over HTTP/2）

**API 概览**:

详细的 API 设计请参考 [API 设计文档](./design_api.md)。

- RESTful API：管理员认证、Agent 管理、Client 管理、STCP 实例管理
- gRPC API：Agent 注册/心跳/指令流、Client 认证/服务查询

**访问控制**:

详细的访问控制设计请参考 [访问控制设计文档](./design_server_access_control.md)。

- 当前：调试模式，所有服务为 public
- 未来：支持 public、private、group 三种权限模型

**进程内通信**:

- 通过 Go channel 或接口调用与 Server-FRP 线程通信
- 发送控制指令（创建/删除 STCP 代理）
- 查询连接状态

### 2.2 Server-FRP 线程

**监听端口**: 7000

**功能**:

1. FRP 信令服务（WebSocket）
2. 接收 Agent 和 Desktop 的 WebSocket 连接
3. 协调 STCP 隧道建立

**进程内通信**:

- 接收 Server-Web 线程的控制指令
- 上报 Agent 和 Desktop 的连接状态
- 通知 STCP 隧道建立结果

## 3. Agent 组件设计

**Agent** 是 AWECloud Signaling 的内网代理组件，部署在内网环境中。

Agent 是一个单一的 Go 进程，启动时会创建两个工作线程（goroutine）。

### 3.1 Agent-Web 线程

**功能**:

1. 通过 gRPC 连接到 Server-Web 线程
2. 注册和心跳
3. 接收管理指令
4. 通过进程内通信控制 Agent-FRP 线程

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

### 3.2 Agent-FRP 线程

**功能**:

1. 通过 WebSocket 连接到 Server-FRP 线程
2. 建立 STCP 代理
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

## 4. Desktop 组件设计

**Desktop** 是 AWECloud Signaling 的桌面客户端组件，运行在用户电脑上。

Desktop 是一个单一的 Go 进程（Wails 应用），启动时会创建两个工作线程（goroutine）。

### 4.1 Desktop-Web 线程

**功能**:

1. 通过 gRPC 连接到 Server-Web 线程（经过 Traefik）
2. 连接地址：`https://your-domain.com/`（gRPC over HTTP/2）
3. 认证和获取可访问服务列表
4. 管理连接状态
5. 通过进程内通信控制 Desktop-FRP 线程

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

### 4.2 Desktop-FRP 线程

**功能**:

1. 通过 WebSocket 连接到 Server-FRP 线程（经过 Traefik）
2. 连接地址：`wss://your-domain.com/ws`
3. 作为 Visitor 建立 STCP 隧道
4. 提供本地端口供用户访问
5. 接收 Desktop-Web 线程的控制指令（进程内通信）

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

## 5. Server 组件内部设计

Server 组件内部运行两个服务线程，详细设计请参考 [Server 内部设计文档](./design_server.md)。

**核心内容**：

- 线程架构和通信机制
- 核心业务流程（创建/删除 STCP 实例）
- 进程内通信接口设计
- 状态管理和错误处理

## 5.1 Desktop 组件详细设计

Desktop 组件是跨平台桌面应用，供用户访问内网服务。详细设计请参考 [Desktop 设计文档](./design_desktop.md)。

**核心内容**：

- 技术栈和架构设计
- 用户界面设计
- 连接管理和状态同步
- 本地端口映射

## 6. 数据库设计

使用 **SQLite** 作为数据库。详细的表结构设计请参考 [数据库设计文档](./design_database.md)。

**数据表列表**：

- `admins` - 管理员表
- `agents` - Agent 表
- `clients` - Client 表
- `stcp_instances` - STCP 实例表
- `stcp_access` - STCP 访问控制表
- `client_sessions` - Client 会话表

## 7. API 设计

详细的 API 设计请参考 [API 设计文档](./design_api.md)。

**API 概览**：

- RESTful API：管理界面和 Client 认证
- gRPC API：Agent 管理和 Client 服务查询
- WebSocket：FRP 信令通道

## 8. FRP 隧道设计

FRP 隧道是系统的核心功能，实现内网穿透和安全连接。详细设计请参考 [FRP 设计文档](./design_frp.md)。

**核心内容**：

- WebSocket 传输协议配置
- STCP 代理类型和工作原理
- Server/Agent/Desktop 端实现
- 连接流程和数据流向

## 9. Web 管理界面设计

Web 管理界面供管理员管理系统资源。详细设计请参考 [Web 界面设计文档](./design_server_web.md)。

**核心内容**：

- 前端技术栈（Vue 3 + Element Plus）
- 功能模块（Agent 管理、STCP 实例管理）
- 界面布局和交互设计
- 国际化支持

## 10. 部署方案

详细的部署方案请参考 [部署设计文档](./design_deployment.md)。

**部署概览**：

- Docker 容器化部署
- Kubernetes 集群部署
- Traefik 网关配置

## 11. 安全令牌与审计日志设计

系统引入了 Device Token 机制和审计日志系统，提升安全性和可审计性。详细设计请参考 [安全令牌与审计日志设计文档](./design_security_token_audit.md)。

**核心内容**：

### 11.1 Device Token 系统

**问题**：Desktop 客户端明文存储`client_secret`存在安全风险

**解决方案**：

- 使用设备令牌（Device Token）替代明文 secret 存储
- Token 绑定设备硬件指纹（CPU、系统版本等）
- Token 有 7 天有效期，支持远程撤销
- 用户可以管理所有已登录设备

**Desktop 登录双模式**：

1. **模式 1（离线显示）**：Server 离线但有有效 Token 时
   - 显示服务器地址（明文）
   - 显示用户名（明文）
   - 点击"登录"按钮尝试使用本地 token 重连
2. **模式 2（完整登录）**：正常登录流程
   - 输入服务器地址、用户名、密码
   - 勾选"记住登录"
   - Token 失效后自动填充服务器地址和用户名

**配置文件管理**：

- 勾选"记住登录"：保存`server_address`、`client_id`、`device_token`
- Token 失效：清除 token 相关字段，保留基本信息
- 用户只需重新输入密码

### 11.2 审计日志系统

**问题**：端口偏好保存在客户端，服务端无法追踪用户行为

**解决方案**：

- 端口偏好迁移到服务端（云端同步）
- 记录所有连接/断开操作
- 记录设备信息、IP 地址、操作结果
- 支持管理员查询和导出审计日志

**新增 API**：

- `GET /api/v1/client/auth/login/devices` - 列出已登录设备
- `POST /api/v1/client/auth/login/devices/:device_token/offline` - 让设备下线
- `DELETE /api/v1/client/auth/login/devices/:device_token` - 删除设备记录
- `GET /api/v1/client/preferences/port` - 获取端口偏好
- `POST /api/v1/client/preferences/port` - 保存端口偏好
- `POST /api/v1/client/audit/connection` - 记录连接审计日志
- `GET /api/v1/admin/audit/connection` - 查询审计日志（管理员）

### 11.3 安全性提升

- ✅ 消除明文存储 secret 的风险
- ✅ Token 绑定设备，无法跨设备使用
- ✅ 支持远程撤销设备访问权限
- ✅ 完整的用户行为审计
- ✅ 异常行为检测基础

## 12. Desktop 版本管理

系统支持 Desktop 客户端版本管理，确保客户端版本符合安全要求。详细设计请参考 [Desktop 版本管理设计文档](./design_version_control.md)。

**核心功能**：

### 12.1 Server 端版本控制

- 管理员可在 Web 界面设置最低支持的 Desktop 版本
- 可配置 Desktop 下载地址
- 可启用/禁用版本检查功能

### 12.2 Desktop 端版本检查

- 登录前自动检查版本是否符合要求
- 版本过低时显示强制升级界面
- 提供下载链接，引导用户升级

### 12.3 版本号规范

使用语义化版本号（Semantic Versioning）：

- 格式：`MAJOR.MINOR.PATCH`（如：1.0.0）
- 编译时注入版本号
- 支持版本比较和验证

### 12.4 新增 API

- `POST /api/v1/client/version/check` - Desktop 检查版本
- `GET /api/v1/admin/settings/version` - 获取版本设置（管理员）
- `PUT /api/v1/admin/settings/version/min` - 更新最低版本（管理员）
- `PUT /api/v1/admin/settings/version/download-url` - 更新下载地址（管理员）
- `PUT /api/v1/admin/settings/version/check-enabled` - 启用/禁用版本检查（管理员）

### 12.5 数据库变更

新增`system_settings`表，存储系统级配置：

- `desktop_min_version`：最低支持版本
- `desktop_latest_version`：最新版本
- `desktop_download_url`：下载地址
- `version_check_enabled`：是否启用版本检查

## 13. 隧道Token安全设计（待实现）

### 13.1 当前状态

**问题**：
- 所有Client共享Server配置中的统一Token
- 登录时返回隧道配置（不安全）
- 无法按Client粒度控制隧道访问

**临时方案**：
- Server配置文件中设置统一的`token`
- 所有Agent和Desktop使用相同的Token连接FRP
- 这是MVP阶段的简化实现，存在安全风险

### 13.2 未来设计方向

**目标**：每个Client拥有独立的隧道Token

**设计原则**：
1. **登录时不返回隧道配置** - 无论Secret还是Token登录
2. **每个Client独立Token** - 为每个Client生成唯一的隧道Token
3. **按需获取配置** - 只在连接服务时通过API获取隧道配置

**待设计内容**：
- 数据库模型变更（Client表添加tunnel_token字段）
- Token生成和管理机制
- 获取隧道配置API设计
- Desktop端按需获取逻辑
- Token撤销和更新机制

**设计文档**：待创建 `docs/design_tunnel_token.md`（等待进一步设计）

**注意**：此功能尚未实现，当前系统使用统一Token。

## 14. 关键变更总结

### 13.1 架构设计要点

**系统命名**:

- 整体系统名称：**AWECloud Signaling**
- 三个核心组件：**Server**（服务端）、**Agent**（内网代理）、**Desktop**（桌面客户端）
- 所有组件都是 AWECloud Signaling 系统的组成部分

**架构特点**:

- 每个组件（Server、Agent、Desktop）都是**单一进程**
- 每个进程内部运行**两个工作线程**（goroutine）
- 使用 gRPC 管理通道（Agent/Desktop → Server-Web 线程）
- 使用 WebSocket 信令通道（Agent/Desktop → Server-FRP 线程）
- 通过 STCP 隧道实现 Agent 和 Desktop 之间的安全数据传输

**部署模型**:

- Server 部署在公有云（有公网 IP 和域名）
- Agent 部署在内网环境（无公网 IP，主动连接 Server）
- Desktop 运行在用户电脑（无公网 IP，主动连接 Server）
- 所有连接都是从内网主动发起，无需开放内网端口

### 13.2 技术栈更新

**新增**:

- gRPC：用于 Server-Web 和 Agent-Web 之间的管理通信
- Protocol Buffers：定义 gRPC 接口

**保持不变**:

- HTTP/RESTful API：Web 管理界面
- WebSocket：FRP 信令通道
- STCP：数据隧道

---

**文档版本**: 2.2  
**最后更新**: 2025-11-27  
**状态**: 当前设计
