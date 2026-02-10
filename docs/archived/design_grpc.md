# gRPC 服务设计文档

## 1. 概述

本文档描述 AWECloud Signaling Server 的 gRPC 服务设计，包括 Agent 和 Desktop 与 Server 之间的通信接口。

### 1.1 gRPC 职责

- Agent 注册、认证和心跳
- Desktop 注册、认证和心跳
- 实时状态查询（运行环境、网络信息、隧道信息）
- 配置下发和同步

### 1.2 与其他模块的关系

- 与 REST API 协同工作，REST API 可通过 gRPC 向 Agent 请求实时数据
- 依赖 Headscale 进行隧道管理
- 依赖数据模型（`internal/server/model`）

## 2. 架构设计

### 2.1 通信架构

```txt
┌─────────────────────────────────────────────────────────────────────────────┐
│                           gRPC 通信架构                                      │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│   ┌─────────────┐                         ┌─────────────┐                  │
│   │   Agent     │                         │   Desktop   │                  │
│   │  (内网环境)  │                         │  (用户设备)  │                  │
│   └──────┬──────┘                         └──────┬──────┘                  │
│          │ gRPC (双向流)                         │ gRPC                     │
│          │                                       │                          │
│          └───────────────┬───────────────────────┘                          │
│                          ▼                                                  │
│                  ┌───────────────┐                                          │
│                  │    Server     │                                          │
│                  │  gRPC 服务    │                                          │
│                  └───────┬───────┘                                          │
│                          │                                                  │
│          ┌───────────────┼───────────────┐                                  │
│          ▼               ▼               ▼                                  │
│   ┌───────────┐   ┌───────────┐   ┌───────────┐                            │
│   │ 内存缓存   │   │  数据库   │   │ Headscale │                            │
│   │(连接状态)  │   │ (SQLite)  │   │   API     │                            │
│   └───────────┘   └───────────┘   └───────────┘                            │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 2.2 服务定义

| 服务           | 职责                         | 客户端  |
| -------------- | ---------------------------- | ------- |
| AgentService   | Agent 注册、认证、心跳、状态 | Agent   |
| DesktopService | Desktop 注册、认证、心跳     | Desktop |

### 2.3 数据来源分类

| 数据类型       | 来源      | 说明                                  |
| -------------- | --------- | ------------------------------------- |
| 静态信息       | DB 查询   | agent 表、client 表、desktop 表       |
| 连接状态       | 内存缓存  | Server.Agents、Server.Desktops        |
| 实时信息       | gRPC 请求 | 向 Agent 请求运行环境、网络、隧道信息 |
| Headscale 信息 | API 调用  | User、Node、PreAuthKey 等             |

---

## 3. Agent 服务 (AgentService)

### 3.1 服务概览

```protobuf
service AgentService {
  // 注册 - Agent 首次连接时调用
  rpc Register(RegisterRequest) returns (RegisterResponse);

  // 认证 - Agent 重连时调用
  rpc Authenticate(AuthenticateRequest) returns (AuthenticateResponse);

  // 心跳 - 双向流，保持连接并同步状态
  rpc Heartbeat(stream HeartbeatRequest) returns (stream HeartbeatResponse);

  // 获取实时状态 - Server 主动调用（用于 Web 详情页刷新）
  rpc GetRealtimeStatus(GetRealtimeStatusRequest) returns (GetRealtimeStatusResponse);
}
```

### 3.2 Register - Agent 注册

> 需求来源：Agent 首次部署时需要向 Server 注册

#### 业务流程

```txt
┌─────────────────────────────────────────────────────────────────────────────┐
│                         Agent 注册流程                                       │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│   Agent 启动，读取配置                                                      │
│       │                                                                     │
│       ▼                                                                     │
│   ┌─────────────┐                                                          │
│   │ 发送 Register│                                                          │
│   │ 请求到 Server│                                                          │
│   └─────────────┘                                                          │
│       │                                                                     │
│       ▼                                                                     │
│   ┌─────────────┐    失败    ┌─────────────┐                              │
│   │ 验证 name   │──────────►│ 返回错误     │                              │
│   │ 和 secret   │            │ INVALID_CRED│                              │
│   └─────────────┘            └─────────────┘                              │
│       │ 成功                                                               │
│       ▼                                                                     │
│   ┌─────────────┐                                                          │
│   │ 查询 Agent  │                                                          │
│   │ 是否已存在  │                                                          │
│   └─────────────┘                                                          │
│       │                                                                     │
│       ├─── 已存在 ───────────────────────────────────────┐                 │
│       │                                                  │                 │
│       ▼ 不存在                                           ▼                 │
│   ┌─────────────┐                              ┌─────────────┐             │
│   │ 调用Headscale│                              │ 检查 Node   │             │
│   │ 创建 User   │                              │ 是否已注册  │             │
│   └─────────────┘                              └─────────────┘             │
│       │                                              │                     │
│       ▼                                              ▼                     │
│   ┌─────────────┐                              ┌─────────────┐             │
│   │ 创建 Agent  │                              │ 更新 Agent  │             │
│   │ 记录到 DB   │                              │ 版本和系统  │             │
│   └─────────────┘                              └─────────────┘             │
│       │                                              │                     │
│       └──────────────────┬───────────────────────────┘                     │
│                          ▼                                                  │
│                  ┌─────────────┐                                           │
│                  │ 创建/获取   │                                           │
│                  │ PreAuthKey  │                                           │
│                  └─────────────┘                                           │
│                          │                                                  │
│                          ▼                                                  │
│                  ┌─────────────┐                                           │
│                  │ 返回注册成功│                                           │
│                  │ 和 AuthKey  │                                           │
│                  └─────────────┘                                           │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

#### 请求消息 RegisterRequest

| 字段        | 类型       | 必填 | 说明         |
| ----------- | ---------- | ---- | ------------ |
| name        | string     | 是   | Agent 名称   |
| secret      | string     | 是   | 认证密钥     |
| version     | string     | 是   | Agent 版本号 |
| system_info | SystemInfo | 是   | 系统信息     |

SystemInfo 消息：

| 字段       | 类型   | 说明                               |
| ---------- | ------ | ---------------------------------- |
| os         | string | 操作系统：linux / windows / darwin |
| os_version | string | 系统版本                           |
| arch       | string | CPU 架构：amd64 / arm64            |
| hostname   | string | 主机名                             |
| cpu        | string | CPU 型号                           |
| cpu_cores  | int32  | CPU 核心数                         |
| memory_gb  | int32  | 内存大小（GB）                     |

#### 响应消息 RegisterResponse

| 字段       | 类型   | 说明                               |
| ---------- | ------ | ---------------------------------- |
| success    | bool   | 是否成功                           |
| message    | string | 响应消息                           |
| agent_id   | uint64 | Agent ID（Headscale User ID）      |
| auth_key   | string | Tailscale PreAuthKey，用于隧道连接 |
| server_url | string | Headscale 服务器地址               |

### 3.3 Authenticate - Agent 认证

> 需求来源：Agent 重启后重新连接 Server

#### 业务流程

```txt
┌─────────────────────────────────────────────────────────────────────────────┐
│                         Agent 认证流程                                       │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│   Agent 重启，读取本地存储的 agent_id                                       │
│       │                                                                     │
│       ▼                                                                     │
│   ┌─────────────┐                                                          │
│   │ 发送 Auth   │                                                          │
│   │ 请求到Server│                                                          │
│   └─────────────┘                                                          │
│       │                                                                     │
│       ▼                                                                     │
│   ┌─────────────┐    失败    ┌─────────────┐                              │
│   │ 验证 agent_id│──────────►│ 返回错误     │                              │
│   │ 和 secret   │            │ INVALID_CRED│                              │
│   └─────────────┘            └─────────────┘                              │
│       │ 成功                                                               │
│       ▼                                                                     │
│   ┌─────────────┐                                                          │
│   │ 更新 Agent  │                                                          │
│   │ 版本和系统  │                                                          │
│   └─────────────┘                                                          │
│       │                                                                     │
│       ▼                                                                     │
│   ┌─────────────┐    Node已存在   ┌─────────────┐                         │
│   │ 检查Headscale│───────────────►│ 返回认证成功│                         │
│   │ Node 状态   │                 │ 无需AuthKey │                         │
│   └─────────────┘                 └─────────────┘                         │
│       │ Node不存在                                                         │
│       ▼                                                                     │
│   ┌─────────────┐                                                          │
│   │ 创建新的    │                                                          │
│   │ PreAuthKey  │                                                          │
│   └─────────────┘                                                          │
│       │                                                                     │
│       ▼                                                                     │
│   ┌─────────────┐                                                          │
│   │ 返回认证成功│                                                          │
│   │ 和 AuthKey  │                                                          │
│   └─────────────┘                                                          │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

#### 请求消息 AuthenticateRequest

| 字段        | 类型       | 必填 | 说明         |
| ----------- | ---------- | ---- | ------------ |
| agent_id    | uint64     | 是   | Agent ID     |
| secret      | string     | 是   | 认证密钥     |
| version     | string     | 是   | Agent 版本号 |
| system_info | SystemInfo | 是   | 系统信息     |

#### 响应消息 AuthenticateResponse

| 字段       | 类型   | 说明                                 |
| ---------- | ------ | ------------------------------------ |
| success    | bool   | 是否成功                             |
| message    | string | 响应消息                             |
| auth_key   | string | Tailscale PreAuthKey（如需重新连接） |
| server_url | string | Headscale 服务器地址                 |

### 3.4 Heartbeat - Agent 心跳（双向流）

> 需求来源：
>
> - 保持 Agent 与 Server 的连接状态
> - Server 实时获取 Agent 状态
> - Server 下发配置变更

#### 业务流程

```txt
┌─────────────────────────────────────────────────────────────────────────────┐
│                         Agent 心跳流程（双向流）                             │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│   Agent                                    Server                           │
│     │                                        │                              │
│     │──── 建立心跳流 ────────────────────────►│                              │
│     │                                        │                              │
│     │◄─── HeartbeatResponse ─────────────────│ (首次响应，下发配置)         │
│     │     - services: 端口映射配置            │                              │
│     │     - forwards: 端口访问配置            │                              │
│     │                                        │                              │
│     │                                        │                              │
│     │ ┌─────────────────────────────────────┐│                              │
│     │ │         心跳循环（每30秒）           ││                              │
│     │ │                                     ││                              │
│     │ │  HeartbeatRequest ──────────────────►│                              │
│     │ │  - tunnel_ip                        ││ 更新内存缓存                  │
│     │ │  - tunnel_connected                 ││ 更新 DB last_heartbeat       │
│     │ │  - service_status[]                 ││                              │
│     │ │  - forward_status[]                 ││                              │
│     │ │                                     ││                              │
│     │ │◄─────────────────HeartbeatResponse ─│                              │
│     │ │  - config_version                   ││ 如有配置变更则下发           │
│     │ │  - services (如有变更)              ││                              │
│     │ │  - forwards (如有变更)              ││                              │
│     │ └─────────────────────────────────────┘│                              │
│     │                                        │                              │
│     │                                        │                              │
│     │──── 连接断开 ──────────────────────────►│                              │
│     │                                        │ 更新状态为 offline           │
│     │                                        │                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

#### 请求消息 HeartbeatRequest

| 字段             | 类型            | 必填 | 说明                 |
| ---------------- | --------------- | ---- | -------------------- |
| agent_id         | uint64          | 是   | Agent ID             |
| tunnel_ip        | string          | 否   | 隧道 IP              |
| tunnel_connected | bool            | 是   | 隧道连接状态         |
| service_status   | ServiceStatus[] | 否   | 端口映射服务状态列表 |
| forward_status   | ForwardStatus[] | 否   | 端口访问服务状态列表 |

ServiceStatus 消息：

| 字段       | 类型   | 说明                                     |
| ---------- | ------ | ---------------------------------------- |
| service_id | string | 服务 ID                                  |
| status     | string | 运行状态：running/disabled/error/pending |
| error_code | string | 错误码（如有）                           |
| error_msg  | string | 错误信息（如有）                         |

ForwardStatus 消息：

| 字段            | 类型   | 说明                                     |
| --------------- | ------ | ---------------------------------------- |
| forward_id      | string | 端口访问 ID                              |
| status          | string | 运行状态：running/disabled/error/pending |
| configured_addr | string | 配置的源地址                             |
| actual_addr     | string | 实际使用的源地址（IP 变化时不同）        |
| error_code      | string | 错误码（如有）                           |
| error_msg       | string | 错误信息（如有）                         |

#### 响应消息 HeartbeatResponse

| 字段           | 类型            | 说明                     |
| -------------- | --------------- | ------------------------ |
| config_version | int64           | 配置版本号               |
| services       | ServiceConfig[] | 端口映射配置（如有变更） |
| forwards       | ForwardConfig[] | 端口访问配置（如有变更） |

ServiceConfig 消息：

| 字段        | 类型   | 说明                                    |
| ----------- | ------ | --------------------------------------- |
| id          | string | 服务 ID                                 |
| name        | string | 服务名称                                |
| source_addr | string | 源地址（VPN IP:端口，如 100.64.0.1:80） |
| target_addr | string | 目标地址（如 192.168.1.10:80）          |
| enabled     | bool   | 是否启用                                |

ForwardConfig 消息：

| 字段         | 类型   | 说明                                             |
| ------------ | ------ | ------------------------------------------------ |
| id           | string | 端口访问 ID                                      |
| service_id   | string | 关联的远程服务 ID                                |
| service_name | string | 关联的远程服务名称（agent/service 格式）         |
| source_addr  | string | 源地址（局域网 IP:端口，如 192.168.1.100:13306） |
| target_addr  | string | 目标地址（VPN 地址，如 100.64.0.1:3306）         |
| enabled      | bool   | 是否启用                                         |

### 3.5 GetRealtimeStatus - 获取实时状态

> 需求来源：`design_tailscale_server_web.md` 2.2 详情页 - 运行环境、网络信息、隧道信息
>
> 说明：Web 详情页点击 [刷新] 按钮时，REST API 通过此 gRPC 接口向 Agent 请求实时数据

#### 业务流程

```txt
┌─────────────────────────────────────────────────────────────────────────────┐
│                    获取 Agent 实时状态流程                                   │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│   Web 管理界面                Server                      Agent             │
│       │                         │                           │               │
│       │ GET /agents/:id/realtime│                           │               │
│       │────────────────────────►│                           │               │
│       │                         │                           │               │
│       │                         │ GetRealtimeStatus (gRPC)  │               │
│       │                         │──────────────────────────►│               │
│       │                         │                           │               │
│       │                         │                           │ 收集本地信息  │
│       │                         │                           │ - hostname    │
│       │                         │                           │ - runtime     │
│       │                         │                           │ - networks    │
│       │                         │                           │ - tunnel      │
│       │                         │                           │               │
│       │                         │◄──────────────────────────│               │
│       │                         │  GetRealtimeStatusResponse│               │
│       │                         │                           │               │
│       │◄────────────────────────│                           │               │
│       │  AgentRealtimeInfo      │                           │               │
│       │                         │                           │               │
└─────────────────────────────────────────────────────────────────────────────┘
```

#### 请求消息 GetRealtimeStatusRequest

| 字段     | 类型   | 必填 | 说明     |
| -------- | ------ | ---- | -------- |
| agent_id | uint64 | 是   | Agent ID |

#### 响应消息 GetRealtimeStatusResponse

| 字段                  | 类型               | 说明         | 数据来源       |
| --------------------- | ------------------ | ------------ | -------------- |
| hostname              | string             | 主机名       | Agent 本地获取 |
| runtime               | string             | 运行环境     | Agent 本地获取 |
| networks              | NetworkInterface[] | 网络接口列表 | Agent 本地获取 |
| tunnel_ip             | string             | 隧道 IP      | Agent 本地获取 |
| tunnel_connected      | bool               | 隧道连接状态 | Agent 本地获取 |
| tunnel_connected_time | int64              | 隧道连接时间 | Agent 本地获取 |

NetworkInterface 消息：

| 字段    | 类型   | 说明                        |
| ------- | ------ | --------------------------- |
| name    | string | 网卡名称（如 eth0, ens192） |
| ip      | string | IP 地址                     |
| mask    | string | 子网掩码                    |
| gateway | string | 网关地址                    |

runtime 枚举值：

| 值       | 说明           |
| -------- | -------------- |
| docker   | Docker 容器    |
| k8s      | Kubernetes Pod |
| vm       | 虚拟机         |
| physical | 物理机         |

---

## 4. Desktop 服务 (DesktopService)

### 4.1 服务概览

```protobuf
service DesktopService {
  // 首次登录 - Desktop 用 Client 凭证登录
  rpc Login(LoginRequest) returns (LoginResponse);

  // 认证 - Desktop 用设备凭证认证
  rpc Authenticate(DesktopAuthenticateRequest) returns (DesktopAuthenticateResponse);

  // 心跳 - 保持连接状态
  rpc Heartbeat(stream DesktopHeartbeatRequest) returns (stream DesktopHeartbeatResponse);
}
```

### 4.2 Login - Desktop 首次登录

> 需求来源：Desktop 用户首次使用时，用 Client 的用户名和密钥登录

#### 业务流程

```txt
┌─────────────────────────────────────────────────────────────────────────────┐
│                       Desktop 首次登录流程                                   │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│   Desktop 启动，用户输入 Client 凭证                                        │
│       │                                                                     │
│       ▼                                                                     │
│   ┌─────────────┐                                                          │
│   │ 发送 Login  │                                                          │
│   │ 请求到Server│                                                          │
│   └─────────────┘                                                          │
│       │                                                                     │
│       ▼                                                                     │
│   ┌─────────────┐    失败    ┌─────────────┐                              │
│   │ 验证 Client │──────────►│ 返回错误     │                              │
│   │ name+secret │            │ INVALID_CRED│                              │
│   └─────────────┘            └─────────────┘                              │
│       │ 成功                                                               │
│       ▼                                                                     │
│   ┌─────────────┐                                                          │
│   │ 检查设备是否│                                                          │
│   │ 已存在(指纹)│                                                          │
│   └─────────────┘                                                          │
│       │                                                                     │
│       ├─── 已存在 ───────────────────────────────────────┐                 │
│       │                                                  │                 │
│       ▼ 不存在                                           ▼                 │
│   ┌─────────────┐                              ┌─────────────┐             │
│   │ 调用Headscale│                              │ 更新 Desktop│             │
│   │ 创建 Node   │                              │ 系统信息    │             │
│   │ (PreAuthKey)│                              └─────────────┘             │
│   └─────────────┘                                    │                     │
│       │                                              │                     │
│       ▼                                              │                     │
│   ┌─────────────┐                                    │                     │
│   │ 创建 Desktop│                                    │                     │
│   │ 记录到 DB   │                                    │                     │
│   └─────────────┘                                    │                     │
│       │                                              │                     │
│       ▼                                              │                     │
│   ┌─────────────┐                                    │                     │
│   │ 生成 Desktop│                                    │                     │
│   │ 专属 secret │                                    │                     │
│   └─────────────┘                                    │                     │
│       │                                              │                     │
│       └──────────────────┬───────────────────────────┘                     │
│                          ▼                                                  │
│                  ┌─────────────┐                                           │
│                  │ 同步用户分组│                                           │
│                  │ Tag 到 Node │                                           │
│                  └─────────────┘                                           │
│                          │                                                  │
│                          ▼                                                  │
│                  ┌─────────────┐                                           │
│                  │ 返回登录成功│                                           │
│                  │ desktop_id  │                                           │
│                  │ secret      │                                           │
│                  │ auth_key    │                                           │
│                  └─────────────┘                                           │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

#### 请求消息 LoginRequest

| 字段               | 类型       | 必填 | 说明                     |
| ------------------ | ---------- | ---- | ------------------------ |
| client_name        | string     | 是   | Client 用户名            |
| client_secret      | string     | 是   | Client 认证密钥          |
| device_name        | string     | 是   | 设备名称（主机名）       |
| device_fingerprint | string     | 是   | 设备指纹（硬件唯一标识） |
| system_info        | SystemInfo | 是   | 系统信息                 |

#### 响应消息 LoginResponse

| 字段       | 类型   | 说明                               |
| ---------- | ------ | ---------------------------------- |
| success    | bool   | 是否成功                           |
| message    | string | 响应消息                           |
| desktop_id | uint64 | Desktop ID（Headscale Node ID）    |
| secret     | string | Desktop 专属密钥（仅首次返回）     |
| auth_key   | string | Tailscale PreAuthKey，用于隧道连接 |
| server_url | string | Headscale 服务器地址               |

### 4.3 Authenticate - Desktop 认证

> 需求来源：Desktop 重启后用设备专属凭证重新连接

#### 业务流程

```txt
┌─────────────────────────────────────────────────────────────────────────────┐
│                       Desktop 认证流程                                       │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│   Desktop 重启，读取本地存储的 desktop_id 和 secret                         │
│       │                                                                     │
│       ▼                                                                     │
│   ┌─────────────┐                                                          │
│   │ 发送 Auth   │                                                          │
│   │ 请求到Server│                                                          │
│   └─────────────┘                                                          │
│       │                                                                     │
│       ▼                                                                     │
│   ┌─────────────┐    失败    ┌─────────────┐                              │
│   │ 验证desktop │──────────►│ 返回错误     │                              │
│   │ _id + secret│            │ INVALID_CRED│                              │
│   └─────────────┘            │ 需重新登录  │                              │
│       │ 成功                 └─────────────┘                              │
│       ▼                                                                     │
│   ┌─────────────┐                                                          │
│   │ 验证设备指纹│                                                          │
│   │ 是否匹配    │                                                          │
│   └─────────────┘                                                          │
│       │                                                                     │
│       ├─── 不匹配 ──────────────────────────►┌─────────────┐               │
│       │                                      │ 返回错误     │               │
│       │                                      │ DEVICE_MISMATCH│             │
│       │                                      └─────────────┘               │
│       ▼ 匹配                                                                │
│   ┌─────────────┐                                                          │
│   │ 更新 Desktop│                                                          │
│   │ 系统信息    │                                                          │
│   └─────────────┘                                                          │
│       │                                                                     │
│       ▼                                                                     │
│   ┌─────────────┐    Node已存在   ┌─────────────┐                         │
│   │ 检查Headscale│───────────────►│ 返回认证成功│                         │
│   │ Node 状态   │                 │ 无需AuthKey │                         │
│   └─────────────┘                 └─────────────┘                         │
│       │ Node不存在/过期                                                    │
│       ▼                                                                     │
│   ┌─────────────┐                                                          │
│   │ 创建新的    │                                                          │
│   │ PreAuthKey  │                                                          │
│   └─────────────┘                                                          │
│       │                                                                     │
│       ▼                                                                     │
│   ┌─────────────┐                                                          │
│   │ 返回认证成功│                                                          │
│   │ 和 AuthKey  │                                                          │
│   └─────────────┘                                                          │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

#### 请求消息 DesktopAuthenticateRequest

| 字段               | 类型       | 必填 | 说明                 |
| ------------------ | ---------- | ---- | -------------------- |
| desktop_id         | uint64     | 是   | Desktop ID           |
| secret             | string     | 是   | Desktop 专属密钥     |
| device_fingerprint | string     | 是   | 设备指纹（用于验证） |
| system_info        | SystemInfo | 是   | 系统信息             |

#### 响应消息 DesktopAuthenticateResponse

| 字段       | 类型   | 说明                                 |
| ---------- | ------ | ------------------------------------ |
| success    | bool   | 是否成功                             |
| message    | string | 响应消息                             |
| auth_key   | string | Tailscale PreAuthKey（如需重新连接） |
| server_url | string | Headscale 服务器地址                 |

### 4.4 Heartbeat - Desktop 心跳（双向流）

> 需求来源：
>
> - 保持 Desktop 与 Server 的连接状态
> - 更新 Desktop 在线状态

#### 业务流程

```txt
┌─────────────────────────────────────────────────────────────────────────────┐
│                       Desktop 心跳流程（双向流）                             │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│   Desktop                                  Server                           │
│     │                                        │                              │
│     │──── 建立心跳流 ────────────────────────►│                              │
│     │                                        │                              │
│     │◄─── HeartbeatResponse ─────────────────│ (首次响应)                   │
│     │     - authorized_services: 已授权服务   │                              │
│     │                                        │                              │
│     │                                        │                              │
│     │ ┌─────────────────────────────────────┐│                              │
│     │ │         心跳循环（每30秒）           ││                              │
│     │ │                                     ││                              │
│     │ │  HeartbeatRequest ──────────────────►│                              │
│     │ │  - tunnel_ip                        ││ 更新内存缓存                  │
│     │ │  - tunnel_connected                 ││ 更新 DB last_online          │
│     │ │                                     ││                              │
│     │ │◄─────────────────HeartbeatResponse ─│                              │
│     │ │  - authorized_services (如有变更)   ││                              │
│     │ └─────────────────────────────────────┘│                              │
│     │                                        │                              │
│     │                                        │                              │
│     │──── 连接断开 ──────────────────────────►│                              │
│     │                                        │ 更新状态为 offline           │
│     │                                        │                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

#### 请求消息 DesktopHeartbeatRequest

| 字段             | 类型   | 必填 | 说明         |
| ---------------- | ------ | ---- | ------------ |
| desktop_id       | uint64 | 是   | Desktop ID   |
| tunnel_ip        | string | 否   | 隧道 IP      |
| tunnel_connected | bool   | 是   | 隧道连接状态 |

#### 响应消息 DesktopHeartbeatResponse

| 字段                | 类型                | 说明           |
| ------------------- | ------------------- | -------------- |
| authorized_services | AuthorizedService[] | 已授权服务列表 |

AuthorizedService 消息：

| 字段        | 类型   | 说明                           |
| ----------- | ------ | ------------------------------ |
| id          | string | 服务 ID                        |
| name        | string | 服务名称                       |
| agent_name  | string | 所属 Agent 名称                |
| listen_addr | string | 访问地址（如 100.64.0.1:3306） |

---

## 5. 服务状态上报接口

### 5.1 服务概览

Agent 需要主动上报本地服务和远程服务的状态变化，包括启动成功、启动失败、IP 变化等。

```protobuf
service AgentService {
  // ... 现有方法 ...

  // 上报本地服务状态
  rpc ReportProxyStatus(ReportProxyStatusRequest) returns (ReportProxyStatusResponse);

  // 上报远程服务状态
  rpc ReportVisitorStatus(ReportVisitorStatusRequest) returns (ReportVisitorStatusResponse);

  // 上报网络变化（局域网 IP 变化）
  rpc ReportNetworkChange(ReportNetworkChangeRequest) returns (ReportNetworkChangeResponse);
}
```

### 5.2 ReportProxyStatus - 上报本地服务状态

> 需求来源：`design_tailscale_agent_services.md` 本地服务生命周期

#### 请求消息 ReportProxyStatusRequest

| 字段        | 类型          | 必填 | 说明             |
| ----------- | ------------- | ---- | ---------------- |
| agent_id    | uint64        | 是   | Agent ID         |
| agent_token | string        | 是   | Agent 认证令牌   |
| statuses    | ProxyStatus[] | 是   | 本地服务状态列表 |

ProxyStatus 消息：

| 字段       | 类型   | 说明                                     |
| ---------- | ------ | ---------------------------------------- |
| service_id | string | 服务 ID                                  |
| status     | string | 运行状态：running/disabled/error/pending |
| error_code | string | 错误码（如有）                           |
| error_msg  | string | 错误信息（如有）                         |

#### 响应消息 ReportProxyStatusResponse

| 字段    | 类型   | 说明     |
| ------- | ------ | -------- |
| success | bool   | 是否成功 |
| message | string | 响应消息 |

### 5.3 ReportVisitorStatus - 上报远程服务状态

> 需求来源：`design_tailscale_agent_services.md` 远程服务生命周期、IP 变化处理

#### 请求消息 ReportVisitorStatusRequest

| 字段        | 类型            | 必填 | 说明             |
| ----------- | --------------- | ---- | ---------------- |
| agent_id    | uint64          | 是   | Agent ID         |
| agent_token | string          | 是   | Agent 认证令牌   |
| statuses    | VisitorStatus[] | 是   | 远程服务状态列表 |

VisitorStatus 消息：

| 字段            | 类型   | 说明                                     |
| --------------- | ------ | ---------------------------------------- |
| forward_id      | string | 端口转发 ID                              |
| status          | string | 运行状态：running/disabled/error/pending |
| configured_addr | string | 配置的源地址                             |
| actual_addr     | string | 实际使用的源地址（IP 变化时不同）        |
| ip_changed      | bool   | IP 是否发生变化                          |
| change_reason   | string | 变化原因：DHCP_IP_CHANGE 等              |
| error_code      | string | 错误码（如有）                           |
| error_msg       | string | 错误信息（如有）                         |

#### 响应消息 ReportVisitorStatusResponse

| 字段    | 类型   | 说明     |
| ------- | ------ | -------- |
| success | bool   | 是否成功 |
| message | string | 响应消息 |

### 5.4 ReportNetworkChange - 上报网络变化

> 需求来源：`design_tailscale_agent_services.md` 局域网 IP 变化处理

#### 请求消息 ReportNetworkChangeRequest

| 字段        | 类型   | 必填 | 说明           |
| ----------- | ------ | ---- | -------------- |
| agent_id    | uint64 | 是   | Agent ID       |
| agent_token | string | 是   | Agent 认证令牌 |
| old_lan_ip  | string | 是   | 旧的局域网 IP  |
| new_lan_ip  | string | 是   | 新的局域网 IP  |

#### 响应消息 ReportNetworkChangeResponse

| 字段    | 类型   | 说明     |
| ------- | ------ | -------- |
| success | bool   | 是否成功 |
| message | string | 响应消息 |

### 5.5 错误码定义

| 错误码                 | 说明                    |
| ---------------------- | ----------------------- |
| PORT_IN_USE            | 端口被占用              |
| NETWORK_INTERFACE_LOST | 配置的网段未找到可用 IP |
| TARGET_UNREACHABLE     | 目标服务不可达          |
| ACL_DENIED             | 权限不足                |
| VPN_NOT_READY          | VPN 未就绪              |
| TIMEOUT                | 连接超时                |

---

## 6. 相关文档

- [Headscale 集成设计](design_headscale_integration.md) - 对象映射、认证流程、ACL 同步
- [Headscale 客户端设计](design_headscale_client.md) - Server 连接 Headscale 的客户端实现

---

**文档版本**: 1.1  
**创建日期**: 2026-01-12  
**更新日期**: 2026-01-13  
**维护者**: 开发团队
