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

#### 2.5.6 设置访问权限类型

```
PUT /api/stcp-instances/:id/access-type
```

**请求体**:
```json
{
  "access_type": "public",  // "public" | "private" | "group"
  "group_id": 1             // 当 access_type = "group" 时必需
}
```

**响应**:
```json
{
  "success": true,
  "message": "访问权限已更新"
}
```

### 2.6 用户组管理

#### 2.6.1 获取所有组

```
GET /api/groups
```

**响应**:
```json
{
  "success": true,
  "data": [
    {
      "id": 1,
      "name": "dev-team",
      "description": "开发团队",
      "member_count": 5,
      "created_at": "2025-11-20T08:00:00Z"
    }
  ]
}
```

#### 2.6.2 创建组

```
POST /api/groups
```

**请求体**:
```json
{
  "name": "test-team",
  "description": "测试团队"
}
```

**响应**:
```json
{
  "success": true,
  "data": {
    "id": 2,
    "name": "test-team",
    "description": "测试团队",
    "created_at": "2025-11-25T10:50:00Z"
  }
}
```

#### 2.6.3 更新组

```
PUT /api/groups/:id
```

**请求体**:
```json
{
  "name": "test-team-updated",
  "description": "更新后的测试团队"
}
```

**响应**:
```json
{
  "success": true,
  "message": "组信息已更新"
}
```

#### 2.6.4 删除组

```
DELETE /api/groups/:id
```

**响应**:
```json
{
  "success": true,
  "message": "组已删除"
}
```

#### 2.6.5 获取组成员

```
GET /api/groups/:id/members
```

**响应**:
```json
{
  "success": true,
  "data": [
    {
      "id": 1,
      "client_id": "user@example.com",
      "role": "admin",
      "joined_at": "2025-11-20T08:00:00Z"
    }
  ]
}
```

#### 2.6.6 添加组成员

```
POST /api/groups/:id/members
```

**请求体**:
```json
{
  "client_id": 2,
  "role": "member"  // "admin" | "member"
}
```

**响应**:
```json
{
  "success": true,
  "message": "成员已添加"
}
```

#### 2.6.7 移除组成员

```
DELETE /api/groups/:id/members/:client_id
```

**响应**:
```json
{
  "success": true,
  "message": "成员已移除"
}
```

### 2.7 Client端API

#### 2.7.1 Client认证（旧版，已废弃）

```
POST /api/client/auth
```

**状态**: ⚠️ 已废弃，建议使用 `/api/v1/client/auth/login`

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

#### 2.7.2 使用Secret登录并获取Device Token

```
POST /api/v1/client/auth/login
```

**说明**: 用户首次登录或Device Token过期后使用此接口

**请求体**:
```json
{
  "client_id": "user@example.com",
  "client_secret": "cs_1234567890abcdef",
  "device_fingerprint": "a1b2c3d4e5f6...",
  "device_info": {
    "os": "windows",
    "os_version": "Windows 10",
    "arch": "amd64",
    "cpu_model": "Intel Core i7-9700K",
    "machine_id": "S-1-5-21-...",
    "hostname": "DESKTOP-ABC123"
  }
}
```

**响应**:
```json
{
  "success": true,
  "device_token": "dt_1234567890abcdef",
  "expires_at": "2025-12-04T10:00:00Z",
  "jwt_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "jwt_expires_in": 86400
}
```

#### 2.7.3 使用Device Token登录

```
POST /api/v1/client/auth/login/token
```

**说明**: Desktop客户端自动登录时使用此接口

**请求体**:
```json
{
  "client_id": "user@example.com",
  "device_token": "dt_1234567890abcdef",
  "device_fingerprint": "a1b2c3d4e5f6..."
}
```

**响应**:
```json
{
  "success": true,
  "jwt_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "jwt_expires_in": 86400
}
```

**错误响应**:
```json
{
  "success": false,
  "error": "Device token expired or invalid"
}
```

#### 2.7.4 撤销Device Token

```
POST /api/v1/client/auth/login/token/revoke
```

**说明**: 用户主动登出或撤销某个设备的访问权限

**请求头**:
```
Authorization: Bearer <jwt-token>
```

**请求体**:
```json
{
  "device_token": "dt_1234567890abcdef"
}
```

**注意**: 如果不提供`device_token`，则撤销当前使用的token

**响应**:
```json
{
  "success": true,
  "message": "Device token revoked successfully"
}
```

#### 2.7.5 列出用户已登录的设备

```
GET /api/v1/client/auth/login/devices
```

**说明**: 查看当前用户在哪些设备上登录，用户可以在这里操作让设备下线或删除设备

**请求头**:
```
Authorization: Bearer <jwt-token>
```

**响应**:
```json
{
  "success": true,
  "devices": [
    {
      "device_token": "dt_1234567890abcdef",
      "device_info": {
        "os": "windows",
        "os_version": "Windows 10",
        "hostname": "DESKTOP-ABC123",
        "cpu_model": "Intel Core i7-9700K"
      },
      "created_at": "2025-11-20T10:00:00Z",
      "last_used_at": "2025-11-27T09:30:00Z",
      "expires_at": "2025-12-04T10:00:00Z",
      "revoked": false,
      "is_current": true
    },
    {
      "device_token": "dt_0987654321fedcba",
      "device_info": {
        "os": "linux",
        "os_version": "Ubuntu 22.04",
        "hostname": "laptop-xyz",
        "cpu_model": "AMD Ryzen 5 5600X"
      },
      "created_at": "2025-11-15T14:00:00Z",
      "last_used_at": "2025-11-25T16:20:00Z",
      "expires_at": "2025-11-29T14:00:00Z",
      "revoked": false,
      "is_current": false
    }
  ]
}
```

#### 2.7.6 让设备下线（撤销Device Token）

```
POST /api/v1/client/auth/login/devices/:device_token/offline
```

**说明**: 撤销指定设备的Device Token，使其无法继续使用该Token登录

**请求头**:
```
Authorization: Bearer <jwt-token>
```

**路径参数**:
- `device_token`: 要下线的设备Token

**响应**:
```json
{
  "success": true,
  "message": "Device has been taken offline successfully"
}
```

**错误响应**:
```json
{
  "success": false,
  "error": "Device token not found or already revoked"
}
```

#### 2.7.7 删除设备记录

```
DELETE /api/v1/client/auth/login/devices/:device_token
```

**说明**: 从数据库中删除设备记录（仅限已撤销或过期的设备）

**请求头**:
```
Authorization: Bearer <jwt-token>
```

**路径参数**:
- `device_token`: 要删除的设备Token

**响应**:
```json
{
  "success": true,
  "message": "Device record deleted successfully"
}
```

**错误响应**:
```json
{
  "success": false,
  "error": "Cannot delete active device. Please take it offline first."
}
```

#### 2.7.8 获取可访问服务列表

```
GET /api/client/services
```

**请求头**:
```
Authorization: Bearer <jwt-token>
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

#### 2.7.9 获取端口偏好

```
GET /api/v1/client/preferences/port
```

**说明**: 获取用户保存的端口偏好设置

**请求头**:
```
Authorization: Bearer <jwt-token>
```

**响应**:
```json
{
  "success": true,
  "preferences": {
    "1": 13306,
    "2": 16379,
    "5": 15432
  }
}
```

**说明**: 键为`stcp_instance_id`，值为`preferred_port`

#### 2.7.10 保存端口偏好

```
POST /api/v1/client/preferences/port
```

**说明**: 保存用户的端口偏好设置

**请求头**:
```
Authorization: Bearer <jwt-token>
```

**请求体**:
```json
{
  "stcp_instance_id": 1,
  "preferred_port": 13306
}
```

**响应**:
```json
{
  "success": true,
  "message": "Port preference saved successfully"
}
```

#### 2.7.11 记录连接审计日志

```
POST /api/v1/client/audit/connection
```

**说明**: Desktop客户端在连接/断开服务时调用此接口记录审计日志

**请求头**:
```
Authorization: Bearer <jwt-token>
```

**请求体**:
```json
{
  "stcp_instance_id": 1,
  "action": "connect",
  "local_port": 13306,
  "device_fingerprint": "a1b2c3d4e5f6...",
  "device_info": {
    "os": "windows",
    "hostname": "DESKTOP-ABC123"
  },
  "success": true,
  "error_message": null
}
```

**字段说明**:
- `action`: 操作类型，`connect` 或 `disconnect`
- `success`: 操作是否成功
- `error_message`: 失败时的错误信息

**响应**:
```json
{
  "success": true,
  "audit_id": 12345
}
```

### 2.8 管理员审计日志API

#### 2.8.1 查询连接审计日志

```
GET /api/v1/admin/audit/connection
```

**说明**: 管理员查询用户的连接审计日志

**请求头**:
```
Authorization: Bearer <admin-jwt-token>
```

**查询参数**:
- `client_id` (可选): 按客户端ID过滤
- `stcp_instance_id` (可选): 按STCP实例ID过滤
- `action` (可选): 按操作类型过滤 (`connect` 或 `disconnect`)
- `start_date` (可选): 开始日期，格式 `2025-11-01`
- `end_date` (可选): 结束日期，格式 `2025-11-30`
- `page` (可选): 页码，默认 1
- `page_size` (可选): 每页数量，默认 50

**响应**:
```json
{
  "success": true,
  "logs": [
    {
      "id": 12345,
      "client_id": 5,
      "client_name": "user@example.com",
      "stcp_instance_id": 1,
      "stcp_instance_name": "dev-mysql",
      "action": "connect",
      "local_port": 13306,
      "device_info": {
        "os": "windows",
        "hostname": "DESKTOP-ABC123"
      },
      "device_fingerprint": "a1b2c3d4e5f6...",
      "ip_address": "192.168.1.100",
      "success": true,
      "error_message": null,
      "created_at": "2025-11-27T10:30:00Z"
    }
  ],
  "total": 1523,
  "page": 1,
  "page_size": 50,
  "total_pages": 31
}
```

#### 2.8.2 导出审计日志

```
GET /api/v1/admin/audit/connection/export
```

**说明**: 导出审计日志为CSV或Excel文件

**请求头**:
```
Authorization: Bearer <admin-jwt-token>
```

**查询参数**:
- 与查询接口相同的过滤参数
- `format` (可选): 导出格式，`csv` 或 `excel`，默认 `csv`

**响应**:
- Content-Type: `text/csv` 或 `application/vnd.openxmlformats-officedocument.spreadsheetml.sheet`
- 文件下载

### 2.9 版本管理API

#### 2.9.1 检查Desktop版本（Client调用）

```
POST /api/v1/client/version/check
```

**说明**: Desktop登录前调用此接口检查版本是否符合要求

**请求体**:
```json
{
  "client_version": "1.0.0",
  "os": "windows",
  "arch": "amd64"
}
```

**响应（版本符合要求）**:
```json
{
  "success": true,
  "version_valid": true,
  "min_version": "1.0.0",
  "latest_version": "1.2.0",
  "message": "Your version is supported"
}
```

**响应（版本过低，需要升级）**:
```json
{
  "success": true,
  "version_valid": false,
  "min_version": "1.1.0",
  "latest_version": "1.2.0",
  "current_version": "1.0.0",
  "download_url": "https://github.com/your-org/awecloud-desktop/releases",
  "message": "Your version is too old. Please upgrade to version 1.1.0 or later.",
  "force_upgrade": true
}
```

#### 2.9.2 获取版本设置（管理员）

```
GET /api/v1/admin/settings/version
```

**请求头**:
```
Cookie: session=<admin-session>
```

**响应**:
```json
{
  "success": true,
  "data": {
    "desktop_min_version": "1.0.0",
    "desktop_latest_version": "1.2.0",
    "desktop_download_url": "https://github.com/your-org/awecloud-desktop/releases",
    "version_check_enabled": true,
    "updated_at": "2025-11-27T10:00:00Z",
    "updated_by": "admin"
  }
}
```

#### 2.9.3 更新最低版本要求（管理员）

```
PUT /api/v1/admin/settings/version/min
```

**请求头**:
```
Cookie: session=<admin-session>
```

**请求体**:
```json
{
  "min_version": "1.1.0"
}
```

**响应**:
```json
{
  "success": true,
  "message": "Minimum version updated successfully",
  "data": {
    "desktop_min_version": "1.1.0",
    "updated_at": "2025-11-27T10:30:00Z"
  }
}
```

#### 2.9.4 更新下载地址（管理员）

```
PUT /api/v1/admin/settings/version/download-url
```

**请求头**:
```
Cookie: session=<admin-session>
```

**请求体**:
```json
{
  "download_url": "https://your-domain.com/downloads/desktop"
}
```

**响应**:
```json
{
  "success": true,
  "message": "Download URL updated successfully"
}
```

#### 2.9.5 启用/禁用版本检查（管理员）

```
PUT /api/v1/admin/settings/version/check-enabled
```

**请求头**:
```
Cookie: session=<admin-session>
```

**请求体**:
```json
{
  "enabled": false
}
```

**响应**:
```json
{
  "success": true,
  "message": "Version check disabled successfully"
}
```

### 2.10 错误码

| 错误码 | 说明 |
|--------|------|
| 400 | 请求参数错误 |
| 401 | 未认证或认证失败 |
| 403 | 无权限访问 |
| 404 | 资源不存在 |
| 409 | 资源冲突（如名称重复） |
| 410 | Device Token已过期 |
| 423 | Device Token已被撤销 |
| 426 | 客户端版本过低，需要升级 |
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
