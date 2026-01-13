# Headscale gRPC 客户端设计

## 1. 概述

本文档描述 Server 通过 gRPC 连接 Headscale 的客户端实现。

### 1.1 连接方式

统一使用 gRPC 协议连接 Headscale，支持以下场景：

| 场景           | URL 格式                               | TLS | 说明                   |
| -------------- | -------------------------------------- | --- | ---------------------- |
| 本地/内网直连  | `http://localhost:8080`                | 否  | 开发环境，无 TLS       |
| 公网带路径前缀 | `https://signal.example.com/headscale` | 是  | 通过反向代理访问       |
| 自签名证书     | `https://signal.example.com/headscale` | 是  | 需要 insecure 跳过验证 |

### 1.2 配置参数

| 参数               | 类型   | 必填 | 说明                                |
| ------------------ | ------ | ---- | ----------------------------------- |
| headscale_url      | string | 是   | Headscale gRPC 地址                 |
| headscale_api_key  | string | 是   | API Key（Bearer Token）             |
| headscale_insecure | bool   | 否   | 跳过 TLS 证书验证（自签名证书场景） |

### 1.3 配置示例

```toml
[tailscale]
# Headscale gRPC 地址
# - 本地测试：http://localhost:8080
# - 公网代理：https://signal.example.com/headscale
headscale_url = "https://signal.example.com/headscale"

# API 密钥
headscale_api_key = "your-api-key"

# 跳过 TLS 证书验证（自签名证书场景）
headscale_insecure = false
```

### 1.4 客户端职责

- User 管理（创建、查询、删除）
- PreAuthKey 管理（创建、过期）
- Node 管理（查询、删除、设置 Tags）
- ACL Policy 管理（获取、设置）

### 1.5 实现位置

```
internal/server/headscale/
└── client.go      # Headscale gRPC 客户端
```

> ACL 同步逻辑参考 [Headscale 集成设计](design_headscale_integration.md) 第 10 节

---

## 2. 连接架构

```txt
┌─────────────────────────────────────────────────────────────────────────────┐
│                    Headscale gRPC 连接架构                                   │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  场景一：内网直连（开发/测试环境）                                          │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                                                                     │   │
│  │   Server ──── gRPC (insecure) ────► Headscale:8080                  │   │
│  │                                                                     │   │
│  │   headscale_url = "http://localhost:8080"                          │   │
│  │                                                                     │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                                                             │
│  场景二：公网代理（生产环境）                                               │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                                                                     │   │
│  │   Server ──── gRPC over TLS ────► Traefik ────► Headscale:8080      │   │
│  │                                                                     │   │
│  │   headscale_url = "https://signal.example.com/headscale"           │   │
│  │                                                                     │   │
│  │   反向代理配置要点：                                                 │   │
│  │   1. 路径前缀 /headscale，StripPrefix 后转发                        │   │
│  │   2. 后端使用 h2c 连接 Headscale gRPC 端口                          │   │
│  │   3. 客户端使用拦截器添加路径前缀                                   │   │
│  │                                                                     │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 3. gRPC 接口

> Proto 包：`github.com/juanfont/headscale/gen/go/headscale/v1`

### 3.1 HeadscaleServiceClient 接口

| 分类       | 方法             | 说明           |
| ---------- | ---------------- | -------------- |
| User 管理  | CreateUser       | 创建用户       |
|            | RenameUser       | 重命名用户     |
|            | DeleteUser       | 删除用户       |
|            | ListUsers        | 列出所有用户   |
| PreAuthKey | CreatePreAuthKey | 创建预认证密钥 |
|            | ExpirePreAuthKey | 过期预认证密钥 |
|            | ListPreAuthKeys  | 列出预认证密钥 |
| Node 管理  | GetNode          | 获取节点信息   |
|            | DeleteNode       | 删除节点       |
|            | ListNodes        | 列出所有节点   |
|            | SetTags          | 设置节点标签   |
|            | RenameNode       | 重命名节点     |
|            | MoveNode         | 移动节点       |
|            | ExpireNode       | 过期节点       |
|            | RegisterNode     | 注册节点       |
| ACL Policy | GetPolicy        | 获取 ACL 策略  |
|            | SetPolicy        | 设置 ACL 策略  |
| API Key    | CreateApiKey     | 创建 API Key   |
|            | ExpireApiKey     | 过期 API Key   |
|            | ListApiKeys      | 列出 API Keys  |
|            | DeleteApiKey     | 删除 API Key   |
| 健康检查   | Health           | 健康检查       |

### 3.2 认证方式

使用 Bearer Token 认证（gRPC Metadata）：

```
authorization: Bearer <api_key>
```

---

## 4. 关键数据结构

### 4.1 User

| 字段         | 类型      | 说明     |
| ------------ | --------- | -------- |
| id           | uint64    | 用户 ID  |
| name         | string    | 用户名   |
| display_name | string    | 显示名称 |
| email        | string    | 邮箱     |
| created_at   | Timestamp | 创建时间 |

### 4.2 Node

| 字段         | 类型       | 说明         |
| ------------ | ---------- | ------------ |
| id           | uint64     | 节点 ID      |
| name         | string     | 节点名称     |
| given_name   | string     | 显示名称     |
| user         | User       | 所属用户     |
| ip_addresses | []string   | IP 地址列表  |
| online       | bool       | 是否在线     |
| last_seen    | Timestamp  | 最后在线时间 |
| forced_tags  | []string   | 强制标签     |
| valid_tags   | []string   | 有效标签     |
| expiry       | Timestamp  | 过期时间     |
| pre_auth_key | PreAuthKey | 预认证密钥   |

### 4.3 PreAuthKey

| 字段       | 类型      | 说明         |
| ---------- | --------- | ------------ |
| id         | uint64    | 密钥 ID      |
| key        | string    | 密钥值       |
| user       | User      | 所属用户     |
| reusable   | bool      | 是否可重用   |
| ephemeral  | bool      | 是否临时节点 |
| used       | bool      | 是否已使用   |
| expiration | Timestamp | 过期时间     |
| acl_tags   | []string  | ACL 标签     |

---

## 5. 业务流程

### 5.1 User 创建流程

> Agent 或 Client 注册时，在 Headscale 创建对应 User

```txt
┌─────────────────────────────────────────────────────────────────────────────┐
│                       Headscale User 创建流程                                │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│   Server 业务逻辑                                                           │
│       │                                                                     │
│       ▼                                                                     │
│   ┌─────────────┐                                                          │
│   │ 构造 User   │  命名规则：                                              │
│   │ Name        │  - Agent: agent-{name}                                   │
│   │             │  - Client: client-{name}                                 │
│   └─────────────┘                                                          │
│       │                                                                     │
│       ▼                                                                     │
│   ┌─────────────┐                                                          │
│   │ 调用        │                                                          │
│   │ CreateUser  │                                                          │
│   └─────────────┘                                                          │
│       │                                                                     │
│       ├─── 成功 ──► 返回 User ID                                           │
│       │                                                                     │
│       ▼ 失败                                                                │
│   ┌─────────────┐                                                          │
│   │ 检查是否    │                                                          │
│   │ 已存在      │                                                          │
│   └─────────────┘                                                          │
│       │                                                                     │
│       ├─── 已存在 ──► 获取现有 User ID                                     │
│       │                                                                     │
│       ▼ 其他错误                                                            │
│   ┌─────────────┐                                                          │
│   │ 返回错误     │                                                          │
│   └─────────────┘                                                          │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 5.2 Node 删除流程

> 删除 Agent 或 Desktop 时，删除 Headscale Node

```txt
┌─────────────────────────────────────────────────────────────────────────────┐
│                       Headscale Node 删除流程                                │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│   Server 业务逻辑                                                           │
│       │                                                                     │
│       ▼                                                                     │
│   ┌─────────────┐                                                          │
│   │ 查询 Node ID│                                                          │
│   │ 从数据库    │                                                          │
│   └─────────────┘                                                          │
│       │                                                                     │
│       ├─── Node ID 为空 ──► 跳过删除（Node 未注册）                        │
│       │                                                                     │
│       ▼ Node ID 存在                                                        │
│   ┌─────────────┐                                                          │
│   │ 调用        │                                                          │
│   │ DeleteNode  │                                                          │
│   └─────────────┘                                                          │
│       │                                                                     │
│       ├─── 成功 ──► 继续后续删除流程                                       │
│       │                                                                     │
│       ├─── 404 ──► 忽略（Node 已不存在）                                   │
│       │                                                                     │
│       ▼ 其他错误                                                            │
│   ┌─────────────┐                                                          │
│   │ 记录错误日志│                                                          │
│   │ 继续删除流程│                                                          │
│   └─────────────┘                                                          │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 5.3 Node Tag 更新流程

> 用户分组/代理分组成员变更时，更新 Node 的 Tag

Tag 命名规则：

| 分组类型 | Tag 格式                | 示例                     |
| -------- | ----------------------- | ------------------------ |
| 用户分组 | tag:client-group-{name} | tag:client-group-dev     |
| 代理分组 | tag:agent-group-{name}  | tag:agent-group-internal |

### 5.4 ACL 同步流程

> 服务授权变更时，同步 ACL 到 Headscale

ACL Policy 结构示例：

```json
{
  "acls": [
    {
      "action": "accept",
      "src": ["tag:client-group-dev"],
      "dst": ["100.64.0.1:3306"]
    }
  ],
  "tagOwners": {
    "tag:client-group-dev": ["autogroup:admin"]
  }
}
```

---

## 6. 相关文档

- [Headscale 集成设计](design_headscale_integration.md) - 对象映射、认证流程、ACL 同步
- [gRPC 服务设计](design_grpc.md) - Server 提供的 AgentService、DesktopService

---

**文档版本**: 1.0  
**创建日期**: 2026-01-13  
**维护者**: 开发团队
