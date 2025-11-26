# AWECloud-Signaling API设计

## 1. API概述

系统提供三种API：
1. **RESTful API**：Web管理界面和Client认证
2. **gRPC API**：Agent管理和Client服务查询
3. **WebSocket API**：FRP信令通道

## 2. RESTful API

### 2.1 基础信息

- **Base URL**: `https://your-domain.com/api`
- **Content-Type**: `application/json`
- **认证方式**: 
  - 管理员API：Session Cookie
  - Client API：Bearer Token

### 2.2 管理员认证

#### 2.2.1 登录

```
POST /api/admin/login
```

**请求体**:
```json
{
  "username": "admin",
  "password": "admin123"
}
```

**响应**:
```json
{
  "success": true,
  "message": "登录成功"
}
```

**错误响应**:
```json
{
  "success": false,
  "error": "用户名或密码错误"
}
```

#### 2.2.2 登出

```
POST /api/admin/logout
```

**响应**:
```json
{
  "success": true,
  "message": "登出成功"
}
```

### 2.3 Agent管理

#### 2.3.1 获取Agent列表

```
GET /api/agents
```

**响应**:
```json
{
  "success": true,
  "data": [
    {
      "id": 1,
      "agent_name": "dev-env-1",
      "status": "online",
      "last_heartbeat": "2025-11-25T10:30:00Z",
      "created_at": "2025-11-20T08:00:00Z"
    }
  ]
}
```

#### 2.3.2 创建Agent

```
POST /api/agents
```

**请求体**:
```json
{
  "agent_name": "dev-env-2"
}
```

**响应**:
```json
{
  "success": true,
  "data": {
    "id": 2,
    "agent_name": "dev-env-2",
    "agent_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
    "created_at": "2025-11-25T10:35:00Z"
  }
}
```

#### 2.3.3 删除Agent

```
DELETE /api/agents/:id
```

**响应**:
```json
{
  "success": true,
  "message": "Agent删除成功"
}
```

#### 2.3.4 重新生成Token

```
POST /api/agents/:id/regenerate-token
```

**响应**:
```json
{
  "success": true,
  "data": {
    "agent_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
  }
}
```

### 2.4 Client管理

#### 2.4.1 获取Client列表

```
GET /api/clients
```

**响应**:
```json
{
  "success": true,
  "data": [
    {
      "id": 1,
      "client_id": "user@example.com",
      "enabled": true,
      "created_at": "2025-11-20T08:00:00Z"
    }
  ]
}
```

#### 2.4.2 创建Client

```
POST /api/clients
```

**请求体**:
```json
{
  "client_id": "user@example.com"
}
```

**响应**:
```json
{
  "success": true,
  "data": {
    "id": 2,
    "client_id": "user@example.com",
    "client_secret": "cs_1234567890abcdef",
    "enabled": true,
    "created_at": "2025-11-25T10:40:00Z"
  }
}
```

#### 2.4.3 禁用Client

```
PUT /api/clients/:id/disable
```

**响应**:
```json
{
  "success": true,
  "message": "Client已禁用"
}
```

#### 2.4.4 启用Client

```
PUT /api/clients/:id/enable
```

**响应**:
```json
{
  "success": true,
  "message": "Client已启用"
}
```

#### 2.4.5 删除Client

```
DELETE /api/clients/:id
```

**响应**:
```json
{
  "success": true,
  "message": "Client删除成功"
}
```

#### 2.4.6 重新生成Secret

```
POST /api/clients/:id/regenerate-secret
```

**响应**:
```json
{
  "success": true,
  "data": {
    "client_secret": "cs_0987654321fedcba"
  }
}
```

### 2.5 STCP实例管理

#### 2.5.1 获取STCP实例列表

```
GET /api/stcp-instances
```

**查询参数**:
- `agent_id` (可选): 按Agent ID过滤

**响应**:
```json
{
  "success": true,
  "data": [
    {
      "id": 1,
      "instance_name": "dev-mysql",
      "agent_id": 1,
      "agent_name": "dev-env-1",
      "local_ip": "127.0.0.1",
      "local_port": 3306,
      "description": "开发环境MySQL数据库",
      "created_at": "2025-11-20T08:00:00Z"
    }
  ]
}
```

#### 2.5.2 创建STCP实例

```
POST /api/stcp-instances
```

**请求体**:
```json
{
  "instance_name": "dev-redis",
  "agent_id": 1,
  "local_ip": "127.0.0.1",
  "local_port": 6379,
  "description": "开发环境Redis"
}
```

**响应**:
```json
{
  "success": true,
  "data": {
    "id": 2,
    "instance_name": "dev-redis",
    "agent_id": 1,
    "secret_key": "sk_1234567890abcdef",
    "local_ip": "127.0.0.1",
    "local_port": 6379,
    "description": "开发环境Redis",
    "created_at": "2025-11-25T10:45:00Z"
  }
}
```

#### 2.5.3 删除STCP实例

```
DELETE /api/stcp-instances/:id
```

**响应**:
```json
{
  "success": true,
  "message": "STCP实例删除成功"
}
```

#### 2.5.4 授权访问

```
POST /api/stcp-instances/:id/grant-access
```

**请求体**:
```json
{
  "client_id": 1
}
```

**响应**:
```json
{
  "success": true,
  "message": "访问权限已授予"
}
```

#### 2.5.5 撤销访问

```
DELETE /api/stcp-instances/:id/revoke-access
```

**请求体**:
```json
{
  "client_id": 1
}
```

**响应**:
```json
{
  "success": true,
  "message": "访问权限已撤销"
}
```

### 2.6 Client端API

#### 2.6.1 Client认证

```
POST /api/client/auth
```

**请求体**:
```json
{
  "client_id": "user@example.com",
  "client_secret": "cs_1234567890abcdef"
}
```

**响应**:
```json
{
  "success": true,
  "data": {
    "session_token": "st_abcdef1234567890",
    "expires_at": "2025-11-25T18:00:00Z"
  }
}
```

#### 2.6.2 获取可访问服务列表

```
GET /api/client/services
```

**请求头**:
```
Authorization: Bearer st_abcdef1234567890
```

**响应**:
```json
{
  "success": true,
  "data": [
    {
      "instance_id": 1,
      "instance_name": "dev-mysql",
      "agent_name": "dev-env-1",
      "description": "开发环境MySQL数据库",
      "local_port": 3306
    }
  ]
}
```

### 2.7 错误码

| 错误码 | 说明 |
|--------|------|
| 400 | 请求参数错误 |
| 401 | 未认证或认证失败 |
| 403 | 无权限访问 |
| 404 | 资源不存在 |
| 409 | 资源冲突（如名称重复） |
| 500 | 服务器内部错误 |

## 3. gRPC API

### 3.1 Protocol Buffers定义

```protobuf
syntax = "proto3";

package awecloud.signaling;

option go_package = "github.com/your-org/awecloud-signaling/pkg/proto";

// ==================== Agent服务 ====================

service AgentService {
  // Agent注册
  rpc Register(RegisterRequest) returns (RegisterResponse);
  
  // Agent心跳
  rpc Heartbeat(HeartbeatRequest) returns (HeartbeatResponse);
  
  // 接收Server指令（双向流）
  rpc ReceiveCommands(stream CommandResponse) returns (stream Command);
  
  // 状态上报
  rpc ReportStatus(StatusReport) returns (StatusResponse);
}

// Agent注册请求
message RegisterRequest {
  string agent_name = 1;
  string agent_token = 2;
}

// Agent注册响应
message RegisterResponse {
  bool success = 1;
  string message = 2;
  int64 agent_id = 3;
}

// Agent心跳请求
message HeartbeatRequest {
  int64 agent_id = 1;
  string agent_token = 2;
}

// Agent心跳响应
message HeartbeatResponse {
  bool success = 1;
  int64 timestamp = 2;
}

// Server指令
message Command {
  enum Type {
    CREATE_STCP = 0;
    DELETE_STCP = 1;
  }
  
  string command_id = 1;
  Type type = 2;
  string instance_name = 3;
  string secret_key = 4;
  string local_ip = 5;
  int32 local_port = 6;
}

// 指令响应
message CommandResponse {
  string command_id = 1;
  bool success = 2;
  string message = 3;
}

// 状态上报
message StatusReport {
  int64 agent_id = 1;
  repeated STCPStatus stcp_statuses = 2;
}

message STCPStatus {
  string instance_name = 1;
  string status = 2;  // "running", "stopped", "error"
  int32 active_connections = 3;
}

// 状态响应
message StatusResponse {
  bool success = 1;
}

// ==================== Client服务 ====================

service ClientService {
  // Client认证
  rpc Authenticate(AuthRequest) returns (AuthResponse);
  
  // 获取可访问服务列表
  rpc GetServices(GetServicesRequest) returns (GetServicesResponse);
  
  // 连接服务（获取连接信息）
  rpc ConnectService(ConnectRequest) returns (ConnectResponse);
}

// Client认证请求
message AuthRequest {
  string client_id = 1;
  string client_secret = 2;
}

// Client认证响应
message AuthResponse {
  bool success = 1;
  string message = 2;
  string session_token = 3;
  int64 expires_at = 4;
}

// 获取服务列表请求
message GetServicesRequest {
  string session_token = 1;
}

// 获取服务列表响应
message GetServicesResponse {
  bool success = 1;
  repeated ServiceInfo services = 2;
}

message ServiceInfo {
  int64 instance_id = 1;
  string instance_name = 2;
  string agent_name = 3;
  string description = 4;
  int32 local_port = 5;
}

// 连接服务请求
message ConnectRequest {
  string session_token = 1;
  int64 instance_id = 2;
}

// 连接服务响应
message ConnectResponse {
  bool success = 1;
  string message = 2;
  string instance_name = 3;
  string secret_key = 4;
  int32 suggested_local_port = 5;  // 建议的本地端口
}
```

### 3.2 gRPC服务端点

- **Agent服务**: `https://your-domain.com/` (gRPC over HTTP/2)
- **Client服务**: `https://your-domain.com/` (gRPC over HTTP/2)

### 3.3 gRPC认证

- **Agent**: 使用 `agent_token` 进行认证
- **Client**: 使用 `session_token` 进行认证

## 4. WebSocket API

### 4.1 连接端点

```
wss://your-domain.com/ws
```

### 4.2 连接认证

连接时通过查询参数传递认证信息：

**Agent-FRP**:
```
wss://your-domain.com/ws?type=agent&agent_id=1&token=xxx
```

**Desktop-FRP**:
```
wss://your-domain.com/ws?type=client&session_token=xxx
```

### 4.3 消息格式

所有消息使用JSON格式：

```json
{
  "type": "message_type",
  "data": { ... }
}
```

### 4.4 消息类型

#### 4.4.1 心跳消息

**Client → Server**:
```json
{
  "type": "ping",
  "data": {
    "timestamp": 1732531200
  }
}
```

**Server → Client**:
```json
{
  "type": "pong",
  "data": {
    "timestamp": 1732531200
  }
}
```

#### 4.4.2 STCP控制消息

**Server → Agent-FRP** (创建STCP代理):
```json
{
  "type": "create_stcp",
  "data": {
    "instance_name": "dev-mysql",
    "secret_key": "sk_1234567890abcdef",
    "local_ip": "127.0.0.1",
    "local_port": 3306
  }
}
```

**Agent-FRP → Server** (响应):
```json
{
  "type": "stcp_created",
  "data": {
    "instance_name": "dev-mysql",
    "success": true,
    "message": "STCP代理创建成功"
  }
}
```

#### 4.4.3 连接请求消息

**Desktop-FRP → Server** (请求连接):
```json
{
  "type": "connect_stcp",
  "data": {
    "instance_name": "dev-mysql",
    "secret_key": "sk_1234567890abcdef"
  }
}
```

**Server → Desktop-FRP** (响应):
```json
{
  "type": "stcp_ready",
  "data": {
    "instance_name": "dev-mysql",
    "success": true,
    "local_port": 13306
  }
}
```

### 4.5 错误消息

```json
{
  "type": "error",
  "data": {
    "code": "AUTH_FAILED",
    "message": "认证失败"
  }
}
```

**错误码**:
- `AUTH_FAILED`: 认证失败
- `INVALID_MESSAGE`: 无效消息格式
- `STCP_NOT_FOUND`: STCP实例不存在
- `PERMISSION_DENIED`: 权限不足
- `INTERNAL_ERROR`: 内部错误

---

**文档版本**: 1.0  
**最后更新**: 2025-11-25
