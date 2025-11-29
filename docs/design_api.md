# AWECloud-Signaling API 设计

## 1. API 概述

系统提供三种 API：

1. **RESTful API**：Web 管理界面和 Client 认证
2. **gRPC API**：Agent 管理和 Client 服务查询
3. **WebSocket API**：FRP 信令通道

## 2. RESTful API

### 2.1 基础信息

- **Base URL**: `https://your-domain.com/api/v1`
- **Content-Type**: `application/json`
- **认证方式**:
  - 管理员 API：Session Cookie 或 JWT Token
  - Client API：JWT Bearer Token

### 2.2 健康检查 API（非业务 API）

健康检查 API 使用根路径，不使用 `/api/v1` 前缀，符合 Kubernetes 生态标准。

#### 2.2.1 基础健康检查

```
GET /health
```

**用途**: Kubernetes Liveness Probe（存活性探测）

**响应**:

```json
{
  "status": "ok",
  "timestamp": "2025-11-29T10:30:00Z"
}
```

**HTTP 状态码**:

- `200 OK`: 服务正常运行
- `503 Service Unavailable`: 服务不可用

#### 2.2.2 就绪性检查

```
GET /health/ready
```

**用途**: Kubernetes Readiness Probe（就绪性探测）

**响应（就绪）**:

```json
{
  "status": "ready",
  "timestamp": "2025-11-29T10:30:00Z",
  "checks": {
    "database": "ok",
    "frp_server": "ok",
    "grpc_server": "ok"
  }
}
```

**响应（未就绪）**:

```json
{
  "status": "not_ready",
  "timestamp": "2025-11-29T10:30:00Z",
  "checks": {
    "database": "error",
    "frp_server": "ok",
    "grpc_server": "ok"
  },
  "errors": {
    "database": "connection timeout"
  }
}
```

**HTTP 状态码**:

- `200 OK`: 服务就绪，可以接收流量
- `503 Service Unavailable`: 服务未就绪

**检查项**:

1. **数据库连接**: 执行简单查询验证连接
2. **FRP Server 状态**: 检查 FRP 服务线程是否运行
3. **gRPC Server 状态**: 检查 gRPC 服务是否可用

**详细设计**: 参见 [健康检查接口设计](./design_health.md)

### 2.3 API 版本规范

所有业务 API 统一使用 `/api/v1` 前缀：

- **管理员 API**: `/api/v1/admin/...`
  - 认证：`/api/v1/admin/auth/...`
  - 资源管理：`/api/v1/admin/{resource}/...`
- **Client API**: `/api/v1/client/...`
  - 认证：`/api/v1/client/auth/...`
  - 服务：`/api/v1/client/services`
  - 偏好设置：`/api/v1/client/preferences/...`
  - 审计：`/api/v1/client/audit/...`

**向后兼容**：

- `/api/client/auth` - 已废弃，保留用于向后兼容，建议使用 `/api/v1/client/auth/login`

### 2.4 管理员认证

#### 2.4.1 登录

```
POST /api/v1/admin/auth/login
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

#### 2.4.2 登出

```
POST /api/v1/admin/auth/logout
```

**响应**:

```json
{
  "success": true,
  "message": "登出成功"
}
```

### 2.4 Agent 管理

#### 2.4.1 获取 Agent 列表

```
GET /api/v1/admin/agents
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

#### 2.4.2 创建 Agent

```
POST /api/v1/admin/agents
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

#### 2.4.3 删除 Agent

```
DELETE /api/v1/admin/agents/:id
```

**响应**:

```json
{
  "success": true,
  "message": "Agent删除成功"
}
```

#### 2.4.4 重新生成 Token

```
POST /api/v1/admin/agents/:id/regenerate-token
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

### 2.5 Client 管理

#### 2.5.1 获取 Client 列表

```
GET /api/v1/admin/clients
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

#### 2.5.2 创建 Client

```
POST /api/v1/admin/clients
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

#### 2.5.3 禁用 Client

```
PUT /api/v1/admin/clients/:id/disable
```

**响应**:

```json
{
  "success": true,
  "message": "Client已禁用"
}
```

#### 2.5.4 启用 Client

```
PUT /api/v1/admin/clients/:id/enable
```

**响应**:

```json
{
  "success": true,
  "message": "Client已启用"
}
```

#### 2.5.5 删除 Client

```
DELETE /api/v1/admin/clients/:id
```

**响应**:

```json
{
  "success": true,
  "message": "Client删除成功"
}
```

#### 2.5.6 重新生成 Secret

```
POST /api/v1/admin/clients/:id/regenerate-secret
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

### 2.6 STCP 实例管理

#### 2.6.1 获取 STCP 实例列表

```
GET /api/v1/admin/stcp-instances
```

**查询参数**:

- `agent_id` (可选): 按 Agent ID 过滤

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

#### 2.6.2 创建 STCP 实例

```
POST /api/v1/admin/stcp-instances
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

#### 2.6.3 删除 STCP 实例

```
DELETE /api/v1/admin/stcp-instances/:id
```

**响应**:

```json
{
  "success": true,
  "message": "STCP实例删除成功"
}
```

#### 2.7.4 授权访问

```
POST /api/v1/admin/stcp-instances/:id/grant
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

#### 2.7.5 撤销访问

```
POST /api/v1/admin/stcp-instances/:id/revoke
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

#### 2.7.6 设置访问权限类型

```
PUT /api/v1/admin/stcp-instances/:id/access-type
```

**请求体**:

```json
{
  "access_type": "public", // "public" | "private" | "group"
  "group_id": 1 // 当 access_type = "group" 时必需
}
```

**响应**:

```json
{
  "success": true,
  "message": "访问权限已更新"
}
```

### 2.8 用户组管理

#### 2.8.1 获取所有组

```
GET /api/v1/admin/groups
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

#### 2.8.2 创建组

```
POST /api/v1/admin/groups
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

#### 2.8.3 更新组

```
PUT /api/v1/admin/groups/:id
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

#### 2.8.4 删除组

```
DELETE /api/v1/admin/groups/:id
```

**响应**:

```json
{
  "success": true,
  "message": "组已删除"
}
```

#### 2.8.5 获取组成员

```
GET /api/v1/admin/groups/:id/members
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

#### 2.8.6 添加组成员

```
POST /api/v1/admin/groups/:id/members
```

**请求体**:

```json
{
  "client_id": 2,
  "role": "member" // "admin" | "member"
}
```

**响应**:

```json
{
  "success": true,
  "message": "成员已添加"
}
```

#### 2.8.7 移除组成员

```
DELETE /api/v1/admin/groups/:id/members/:client_id
```

**响应**:

```json
{
  "success": true,
  "message": "成员已移除"
}
```

### 2.8 Client 端 API

**认证流程说明**:

Desktop客户端的认证分为两个阶段：

**阶段1：登录（获取JWT Token）**

支持两种登录方式，最终都返回JWT Token：

1. **Secret登录** (`POST /api/v1/client/auth/login`)
   - 用途：首次登录或Device Token过期
   - 输入：`client_id` + `client_secret` + 设备信息
   - 输出：`device_token` + `jwt_token`
   - 安全：Secret不保存到本地，只在登录时使用

2. **Device Token登录** (`POST /api/v1/client/auth/login/token`)
   - 用途：自动登录（记住登录）
   - 输入：`client_id` + `device_token` + 设备指纹
   - 输出：`jwt_token`
   - 安全：Device Token保存到本地，有效期7天，可远程撤销

**阶段2：API认证（使用JWT Token）**

所有后续API调用统一使用JWT Token认证，不关心用户使用哪种方式登录：

```
Authorization: Bearer <jwt_token>
```

JWT Token特点：
- 有效期：24小时
- 用途：所有API调用（服务列表、隧道配置、设备管理等）
- 刷新：JWT过期后需要重新登录（使用Device Token自动登录）

**安全设计**:
- ✅ Secret不保存到本地
- ✅ Device Token可远程撤销
- ✅ JWT短期有效，降低泄露风险
- ✅ 设备指纹验证，防止Token被盗用

---

#### 2.8.1 Client 认证（旧版，已废弃）

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

#### 2.8.2 使用 Secret 登录并获取 Device Token

```
POST /api/v1/client/auth/login
```

**说明**: 用户首次登录或 Device Token 过期后使用此接口

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

#### 2.8.3 使用 Device Token 登录

```
POST /api/v1/client/auth/login/token
```

**说明**: Desktop 客户端自动登录时使用此接口

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

#### 2.8.4 撤销 Device Token

```
POST /api/v1/client/auth/login/devices/:device_token/offline
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

**注意**: 如果不提供`device_token`，则撤销当前使用的 token

**响应**:

```json
{
  "success": true,
  "message": "Device token revoked successfully"
}
```

#### 2.9.5 列出用户已登录的设备

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

#### 2.8.6 让设备下线（撤销 Device Token）

```
POST /api/v1/client/auth/login/devices/:device_token/offline
```

**说明**: 撤销指定设备的 Device Token，使其无法继续使用该 Token 登录

**请求头**:

```
Authorization: Bearer <jwt-token>
```

**路径参数**:

- `device_token`: 要下线的设备 Token

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

#### 2.9.7 删除设备记录

```
DELETE /api/v1/client/auth/login/devices/:device_token
```

**说明**: 从数据库中删除设备记录（仅限已撤销或过期的设备）

**请求头**:

```
Authorization: Bearer <jwt-token>
```

**路径参数**:

- `device_token`: 要删除的设备 Token

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

#### 2.9.8 获取可访问服务列表

```
GET /api/v1/client/services
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

#### 2.9.9 获取端口偏好

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

#### 2.9.10 保存端口偏好

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

#### 2.8.11 获取隧道配置

```
GET /api/v1/client/tunnel/config
```

**说明**: Desktop 客户端在连接服务前调用此接口获取隧道配置（FRP连接信息）

**设计原则**:
- 登录时不返回隧道配置
- 按需获取：只在连接服务时获取
- 不在本地保存：每次连接都从Server获取最新配置
- 为未来独立Token做准备：支持为每个Client分配独立的隧道Token

**请求头**:

```
Authorization: Bearer <jwt-token>
```

**响应**:

```json
{
  "success": true,
  "tunnel_server": "wss://your-domain.com/ws",
  "tunnel_port": 0,
  "tunnel_token": "awecloud-frp-secret-token-2024"
}
```

**字段说明**:

- `tunnel_server`: 隧道服务器地址
  - 如果配置了`public_url`，返回完整URL（如：`wss://your-domain.com/ws`）
  - 如果未配置，返回空字符串，客户端使用`tunnel_port`构建地址
- `tunnel_port`: 隧道服务器端口
  - 如果`tunnel_server`为完整URL，此字段为0
  - 否则返回实际端口号（如：7000）
- `tunnel_token`: 隧道认证Token
  - 当前：所有Client共享Server配置中的统一Token
  - 未来：每个Client拥有独立的Token（待实现）

**使用流程**:

```
1. Desktop用户点击"连接服务"
   ↓
2. Desktop调用此API获取隧道配置
   ↓
3. Desktop使用获取的配置初始化FRP客户端
   ↓
4. 建立STCP隧道，连接成功
```

**安全性**:

- ✅ 不在本地保存Token，避免泄露风险
- ✅ 使用JWT认证，确保只有合法用户能获取
- ✅ 按需获取，减少Token暴露时间
- ⏳ 未来支持每个Client独立Token（参见 `docs/design_tunnel_token.md`）

**错误响应**:

```json
{
  "success": false,
  "error": "Unauthorized"
}
```

**HTTP 状态码**:

- `200 OK`: 成功获取配置
- `401 Unauthorized`: JWT Token无效或过期
- `500 Internal Server Error`: 服务器内部错误

#### 2.9.11 记录连接审计日志

```
POST /api/v1/client/audit/connection
```

**说明**: Desktop 客户端在连接/断开服务时调用此接口记录审计日志

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

### 2.9 管理员审计日志 API

#### 2.9.1 查询连接审计日志

```
GET /api/v1/admin/audit/connection
```

**说明**: 管理员查询用户的连接审计日志

**请求头**:

```
Authorization: Bearer <admin-jwt-token>
```

**查询参数**:

- `client_id` (可选): 按客户端 ID 过滤
- `stcp_instance_id` (可选): 按 STCP 实例 ID 过滤
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

#### 2.10.2 导出审计日志

```
GET /api/v1/admin/audit/connection/export
```

**说明**: 导出审计日志为 CSV 或 Excel 文件

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

### 2.10 版本管理 API

#### 2.10.1 检查 Desktop 版本（Client 调用）

```
POST /api/v1/client/version/check
```

**说明**: Desktop 登录前调用此接口检查版本是否符合要求

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

#### 2.11.2 获取版本设置（管理员）

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

#### 2.11.3 更新最低版本要求（管理员）

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

#### 2.11.4 更新下载地址（管理员）

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

#### 2.11.5 启用/禁用版本检查（管理员）

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

### 2.12 错误码

| 错误码 | 说明                     |
| ------ | ------------------------ |
| 400    | 请求参数错误             |
| 401    | 未认证或认证失败         |
| 403    | 无权限访问               |
| 404    | 资源不存在               |
| 409    | 资源冲突（如名称重复）   |
| 410    | Device Token 已过期      |
| 423    | Device Token 已被撤销    |
| 426    | 客户端版本过低，需要升级 |
| 500    | 服务器内部错误           |

## 3. gRPC API

### 3.1 Protocol Buffers 定义

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

### 3.2 gRPC 服务端点

- **Agent 服务**: `https://your-domain.com/` (gRPC over HTTP/2)
- **Client 服务**: `https://your-domain.com/` (gRPC over HTTP/2)

### 3.3 gRPC 认证

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

所有消息使用 JSON 格式：

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

#### 4.4.2 STCP 控制消息

**Server → Agent-FRP** (创建 STCP 代理):

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
- `STCP_NOT_FOUND`: STCP 实例不存在
- `PERMISSION_DENIED`: 权限不足
- `INTERNAL_ERROR`: 内部错误

---

**文档版本**: 1.1  
**最后更新**: 2025-11-29

**更新日志**:
- 2025-11-29: 新增隧道配置接口 `GET /api/v1/client/tunnel/config`
