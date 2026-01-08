# Tailscale 升级 - Server 端变更设计

> 本文档描述 Tailscale 升级后 Server 端（Golang）的变更，包括数据模型、gRPC 服务、API 接口等。

## 1. 变更概述

### 1.1 模块变更总览

| 模块                         | 变更类型 | 说明                                      |
| ---------------------------- | -------- | ----------------------------------------- |
| `internal/server/model/`     | 修改     | Agent 新增字段，新增 ProxyService 模型    |
| `internal/server/grpc/`      | 修改     | Agent 服务新增端口映射指令                |
| `internal/server/api/`       | 修改     | 新增服务管理 API，移除 STCP/TCP API       |
| `internal/server/frp/`       | 删除     | FRP Server 模块删除                       |
| `internal/server/headscale/` | 新增     | Headscale API 客户端                      |
| `pkg/proto/`                 | 修改     | 新增端口映射相关消息定义                  |
| `config/`                    | 修改     | 移除 FRP 配置，新增 Tailscale 配置        |
| `internal/common/config/`    | 修改     | 移除 ServerSection，新增 TailscaleSection |

### 1.2 依赖变更

| 依赖                      | 变更类型 | 说明                                 |
| ------------------------- | -------- | ------------------------------------ |
| `github.com/fatedier/frp` | 移除     | 不再需要 FRP 库                      |
| `tailscale.com/tsnet`     | 新增     | Tailscale 嵌入库（Agent/Desktop 用） |
| Headscale API Client      | 新增     | 调用 Headscale HTTP API              |

---

## 2. 数据模型变更

### 2.1 Agent 模型变更

文件：`internal/server/model/agent.go`

**新增字段**：

| 字段           | 类型        | 说明                           |
| -------------- | ----------- | ------------------------------ |
| TailscaleIP    | string      | Tailscale IP，如 100.64.0.10   |
| TsConnected    | bool        | Tailscale 连接状态             |
| TsConnType     | string      | 连接方式：p2p / derp           |
| TsRegisteredAt | \*time.Time | Tailscale 注册时间             |
| TsNodeKey      | string      | Tailscale 节点密钥（内部使用） |

**变更后模型**：

```txt
Agent
├── ID            int64      // 主键
├── AgentName     string     // Agent 名称
├── AgentToken    string     // Agent 认证令牌
├── Description   string     // 描述
├── Status        string     // 状态：online/offline
├── Version       string     // Agent 版本
├── LastHeartbeat *time.Time // 最后心跳时间
├── TailscaleIP   string     // [新增] Tailscale IP
├── TsConnected   bool       // [新增] Tailscale 连接状态
├── TsConnType    string     // [新增] 连接方式
├── TsRegisteredAt *time.Time // [新增] Tailscale 注册时间
├── TsNodeKey     string     // [新增] 节点密钥
├── CreatedAt     time.Time
└── UpdatedAt     time.Time
```

### 2.2 新增 ProxyService 模型

文件：`internal/server/model/proxy_service.go`（新建）

替代原有的 `stcp_instance.go` 和 `tcp_service.go`。

**模型定义**：

```txt
ProxyService
├── ID          int64      // 主键
├── Name        string     // 服务名称，唯一
├── AgentID     int64      // 所属 Agent ID
├── ListenPort  int        // 监听端口
├── TargetAddr  string     // 目标地址，如 192.168.1.100:3306
├── Status      string     // 状态：running/stopped/error
├── Connections int        // 当前连接数
├── BytesIn     int64      // 入站流量（字节）
├── BytesOut    int64      // 出站流量（字节）
├── Remark      string     // 备注
│
│   // [新增] 权限控制字段 - 参考 design_tailscale_security.md
├── AccessType  string     // 访问类型：public/private/group
├── OwnerID     int64      // 创建者 Client ID（private 时使用）
├── GroupID     *int64     // 所属组 ID（group 时使用）
│
├── CreatedAt   time.Time
├── UpdatedAt   time.Time
│
└── Agent       *Agent     // 关联 Agent
```

**索引**：

- `name` 唯一索引
- `agent_id` 普通索引
- `agent_id, listen_port` 联合唯一索引（同一 Agent 端口不能重复）
- `access_type` 普通索引
- `group_id` 普通索引

### 2.3 新增 ServicePermission 模型

文件：`internal/server/model/service_permission.go`（新建）

> 参考：[Tailscale 多租户安全设计](design_tailscale_security.md)

**模型定义**：

```txt
ServicePermission
├── ID          int64      // 主键
├── ServiceID   int64      // 服务 ID
├── ClientID    int64      // 被授权的 Client ID
├── GrantedBy   int64      // 授权人（Admin ID）
├── GrantedAt   time.Time  // 授权时间
├── ExpiresAt   *time.Time // 过期时间（可选）
├── CreatedAt   time.Time
│
├── Service     *ProxyService // 关联服务
└── Client      *Client       // 关联客户端
```

**说明**：

- 当 `AccessType = private` 时，只有 OwnerID 对应的 Client 可访问
- 当 `AccessType = group` 时，GroupID 对应组的所有成员可访问
- 当 `AccessType = public` 时，所有 Client 可访问
- ServicePermission 用于额外的细粒度授权（如临时授权某人访问 private 服务）

### 2.4 新增 AgentServicePermission 模型

文件：`internal/server/model/agent_service_permission.go`（新建）

> 参考：[Tailscale 多租户安全设计](design_tailscale_security.md)

**模型定义**：

```txt
AgentServicePermission
├── ID          int64      // 主键
├── AgentID     int64      // 被授权的 Agent ID（访问方）
├── ServiceID   int64      // 服务 ID（被访问的服务）
├── GrantedBy   int64      // 授权人（Admin ID）
├── GrantedAt   time.Time  // 授权时间
├── CreatedAt   time.Time
│
├── Agent       *Agent        // 关联 Agent（访问方）
└── Service     *ProxyService // 关联服务
```

**说明**：

- 用于 Agent 间访问授权（如外部 Agent 访问内网 Agent 的服务）
- 同组 Agent 默认可互访，不需要此表记录
- 无分组 Agent 需要通过此表显式授权

### 2.5 Agent 分组字段

> 参考：[Tailscale 多租户安全设计](design_tailscale_security.md)

Agent 分组采用文本字段方式，无需单独的分组表：

**Agent 模型新增字段**：

```txt
Agent
├── ... (现有字段)
├── GroupName   string     // [新增] 分组名称，空字符串表示无分组
```

**分组规则**：

- 相同 GroupName 的 Agent 自动归为一组
- 空字符串表示无分组，该 Agent 只能访问显式授权的服务
- 同组 Agent 可以互访所有端口
- 无需单独维护分组列表，在 Agent 列表界面直接编辑文本即可

### 2.6 移除的模型

| 模型         | 文件                     | 移除原因                        |
| ------------ | ------------------------ | ------------------------------- |
| STCPInstance | `model/stcp_instance.go` | 合并到 ProxyService             |
| STCPVisitor  | `model/stcp_visitor.go`  | Desktop 直接通过 Tailscale 访问 |
| TCPService   | `model/tcp_service.go`   | 合并到 ProxyService             |

### 2.7 保留的模型

| 模型               | 说明                     |
| ------------------ | ------------------------ |
| Admin              | 管理员，保持不变         |
| Client             | 客户端，新增 TailscaleIP |
| ClientSession      | 客户端会话，保持不变     |
| DeviceToken        | 设备令牌，保持不变       |
| Group              | 分组，保持不变           |
| GroupMember        | 分组成员，保持不变       |
| ConnectionAuditLog | 审计日志，保持不变       |
| ServiceFavorite    | 服务收藏，保持不变       |
| PortPreference     | 端口偏好，保持不变       |
| SystemConfig       | 系统配置，新增配置项     |

---

## 3. gRPC 服务变更

### 3.1 Proto 定义变更

文件：`pkg/proto/agent.proto`

**新增消息类型**：

```txt
// 端口映射指令
ProxyCommand
├── action       string   // "start" | "stop"
├── name         string   // 服务名称
├── listen_port  int32    // 监听端口
├── target_addr  string   // 目标地址

// 端口映射状态
ProxyStatus
├── name         string   // 服务名称
├── listen_port  int32    // 监听端口
├── target_addr  string   // 目标地址
├── status       string   // "running" | "stopped" | "error"
├── connections  int32    // 当前连接数
├── bytes_in     int64    // 入站流量
├── bytes_out    int64    // 出站流量
├── error_msg    string   // 错误信息（status=error 时）
```

**修改 Command 消息**：

```txt
Command
├── command_id    string
├── type          Type
│   ├── CREATE_STCP        // [废弃] 保留兼容
│   ├── DELETE_STCP        // [废弃] 保留兼容
│   ├── CREATE_TCP         // [废弃] 保留兼容
│   ├── DELETE_TCP         // [废弃] 保留兼容
│   ├── START_PROXY        // [新增] 启动端口映射
│   ├── STOP_PROXY         // [新增] 停止端口映射
│   └── SYNC_PROXIES       // [新增] 同步所有端口映射
│
├── proxy_command ProxyCommand  // [新增] 端口映射指令
└── ... (保留原有字段用于兼容)
```

**修改 RegisterResponse 消息**：

```txt
RegisterResponse
├── success       bool
├── message       string
├── agent_id      int64
├── token         string     // [废弃] FRP Token
├── server        string     // [废弃] FRP Server
├── port          int32      // [废弃] FRP Port
├── tailscale_ip  string     // [新增] 分配的 Tailscale IP
├── control_url   string     // [新增] Headscale 控制平面 URL
├── auth_key      string     // [新增] Tailscale 预认证密钥
└── derp_url      string     // [新增] DERP 服务器 URL
```

**新增 RPC 方法**：

```txt
service AgentService {
  // 现有方法保留...

  // [新增] 上报 Tailscale 状态
  rpc ReportTailscaleStatus(TailscaleStatusReport) returns (StatusResponse);

  // [新增] 上报端口映射状态
  rpc ReportProxyStatus(ProxyStatusReport) returns (StatusResponse);
}

// Tailscale 状态上报
TailscaleStatusReport
├── agent_id      int64
├── tailscale_ip  string
├── connected     bool
├── conn_type     string   // "p2p" | "derp"
├── latency_ms    int32    // 延迟（毫秒）

// 端口映射状态上报
ProxyStatusReport
├── agent_id      int64
├── proxies       []ProxyStatus
```

### 3.2 AgentServiceServer 变更

文件：`internal/server/grpc/agent_service.go`

**新增字段**：

```txt
AgentServiceServer
├── ... (现有字段)
├── headscaleClient *HeadscaleClient  // [新增] Headscale API 客户端
└── config          *config.ServerConfig
```

**新增方法**：

```txt
// 创建 Tailscale 预认证密钥
func (s *AgentServiceServer) CreateAuthKey(agentID int64) (string, error)

// 同步端口映射到 Agent
func (s *AgentServiceServer) SyncProxies(agentID int64)

// 发送端口映射指令
func (s *AgentServiceServer) SendProxyCommand(agentID int64, cmd *ProxyCommand) error

// 处理 Tailscale 状态上报
func (s *AgentServiceServer) ReportTailscaleStatus(ctx, req) (*StatusResponse, error)

// 处理端口映射状态上报
func (s *AgentServiceServer) ReportProxyStatus(ctx, req) (*StatusResponse, error)
```

**修改 Register 方法**：

```txt
Register 流程变更：

1. 验证 Agent 身份（保持不变）
2. 更新 Agent 状态为 online（保持不变）
3. [新增] 调用 Headscale API 创建预认证密钥
4. [新增] 返回 Tailscale 连接信息
   - tailscale_ip: 预分配的 IP（可选）
   - control_url: Headscale 地址
   - auth_key: 预认证密钥
   - derp_url: DERP 服务器地址
5. [废弃] 不再返回 FRP 连接信息
```

**修改 ReceiveCommands 方法**：

```txt
ReceiveCommands 流程变更：

1. 接收初始消息，获取 agent_id（保持不变）
2. 注册 stream（保持不变）
3. [修改] 同步端口映射（替代原来的 syncSTCPInstances）
   - 查询 ProxyService 表
   - 发送 START_PROXY 指令
4. 处理命令队列（保持不变）
```

### 3.3 移除的方法

| 方法                   | 移除原因                         |
| ---------------------- | -------------------------------- |
| syncSTCPInstances      | 替换为 SyncProxies               |
| GetEnabledTCPServices  | 合并到 SyncProxies               |
| GetEnabledSTCPVisitors | Desktop 直接访问，不需要 Visitor |

---

## 4. API 接口变更

### 4.1 新增 API

**服务管理 API**：

```txt
GET    /api/v1/admin/services              // 获取服务列表
POST   /api/v1/admin/services              // 创建服务
PUT    /api/v1/admin/services/:id          // 更新服务
DELETE /api/v1/admin/services/:id          // 删除服务
PUT    /api/v1/admin/services/:id/start    // 启动服务
PUT    /api/v1/admin/services/:id/stop     // 停止服务
GET    /api/v1/admin/services/:id/stats    // 获取服务统计
```

**服务权限管理 API**（参考 [Tailscale 多租户安全设计](design_tailscale_security.md)）：

```txt
GET    /api/v1/admin/services/:id/permissions      // 获取服务授权列表
POST   /api/v1/admin/services/:id/permissions      // 添加服务授权
DELETE /api/v1/admin/services/:id/permissions/:pid // 删除服务授权
PUT    /api/v1/admin/services/:id/access-type      // 修改服务访问类型
```

**Tailscale 管理 API**：

```txt
GET    /api/v1/admin/tailscale/status      // 获取 Tailscale 状态
POST   /api/v1/admin/tailscale/sync        // 强制同步 Headscale
```

**Client Tailscale API**：

```txt
POST   /api/v1/client/tailscale/auth       // 获取 Tailscale 认证信息
DELETE /api/v1/client/tailscale/disconnect // 断开 Tailscale 连接
```

### 4.2 废弃 API

| API                                     | 替代方案                          |
| --------------------------------------- | --------------------------------- |
| GET /api/v1/admin/stcp-instances        | GET /api/v1/admin/services        |
| POST /api/v1/admin/stcp-instances       | POST /api/v1/admin/services       |
| DELETE /api/v1/admin/stcp-instances/:id | DELETE /api/v1/admin/services/:id |
| GET /api/v1/admin/tcp-services          | GET /api/v1/admin/services        |
| POST /api/v1/admin/tcp-services         | POST /api/v1/admin/services       |
| DELETE /api/v1/admin/tcp-services/:id   | DELETE /api/v1/admin/services/:id |
| GET /api/v1/admin/stcp-visitors         | 移除，不再需要                    |
| POST /api/v1/admin/stcp-visitors        | 移除，不再需要                    |

### 4.3 修改 API

**Agent 列表 API**：

```txt
GET /api/v1/admin/agents

响应新增字段：
- tailscale_ip: Tailscale IP
- ts_connected: Tailscale 连接状态
- ts_conn_type: 连接方式
- services_count: 端口映射服务数量
```

**Client 列表 API**：

```txt
GET /api/v1/admin/clients

响应新增字段：
- tailscale_ip: Tailscale IP
```

### 4.4 API 文件变更

| 文件                   | 变更类型 | 说明                    |
| ---------------------- | -------- | ----------------------- |
| `api/stcp.go`          | 废弃     | 标记废弃，保留兼容      |
| `api/stcp_visitor.go`  | 移除     | 不再需要                |
| `api/tcp_service.go`   | 废弃     | 标记废弃，保留兼容      |
| `api/proxy_service.go` | 新增     | 统一的服务管理 API      |
| `api/tailscale.go`     | 新增     | Tailscale 管理 API      |
| `api/agent.go`         | 修改     | 新增 Tailscale 相关字段 |
| `api/client_auth.go`   | 修改     | 新增 Tailscale 认证接口 |

---

## 5. Headscale 集成

### 5.1 Headscale Client

文件：`internal/server/headscale/client.go`（新建）

**功能**：

```txt
HeadscaleClient
├── config          HeadscaleConfig
│
├── CreatePreAuthKey(user string, expiry time.Duration) (string, error)
│   // 创建预认证密钥
│
├── ListMachines() ([]Machine, error)
│   // 列出所有设备
│
├── GetMachine(machineID string) (*Machine, error)
│   // 获取设备信息
│
├── DeleteMachine(machineID string) error
│   // 删除设备
│
├── EnableRoute(machineID string, route string) error
│   // 启用子网路由
│
└── GetMachineByIP(ip string) (*Machine, error)
    // 根据 IP 获取设备
```

**配置**：

```txt
HeadscaleConfig
├── URL       string   // Headscale API 地址
├── APIKey    string   // API 密钥
├── Namespace string   // 命名空间
└── Timeout   time.Duration
```

### 5.2 调用时机

| 场景         | 调用方法         | 说明                    |
| ------------ | ---------------- | ----------------------- |
| Agent 注册   | CreatePreAuthKey | 为 Agent 创建认证密钥   |
| Desktop 登录 | CreatePreAuthKey | 为 Desktop 创建认证密钥 |
| Agent 删除   | DeleteMachine    | 从 Headscale 移除设备   |
| Desktop 注销 | DeleteMachine    | 从 Headscale 移除设备   |
| 定时同步     | ListMachines     | 同步设备状态到数据库    |
| 权限授权     | UpdateACL        | 添加 ACL 规则允许访问   |
| 权限撤销     | UpdateACL        | 删除 ACL 规则禁止访问   |

### 5.3 ACL 同步机制

> 参考：[Tailscale 多租户安全设计](design_tailscale_security.md)

**ACL 更新时机**：

```txt
1. 服务权限变更时
   - 授权 Client 访问服务 → 添加 ACL 规则
   - 撤销 Client 访问权限 → 删除 ACL 规则
   - 修改服务 AccessType → 重新生成 ACL 规则

2. Agent/Desktop 状态变更时
   - Agent 上线 → 确保 ACL 规则存在
   - Desktop 上线 → 确保 ACL 规则存在

3. 定时同步
   - 每 5 分钟全量同步一次 ACL
```

**ACL 规则生成逻辑**：

```txt
func generateACLRules() []ACLRule {
    var rules []ACLRule

    // 1. public 服务：所有 Desktop 可访问
    for _, svc := range publicServices {
        rules = append(rules, ACLRule{
            Src: ["100.65.0.0/16"],  // 所有 Desktop
            Dst: [fmt.Sprintf("%s:%d", svc.Agent.TailscaleIP, svc.ListenPort)],
        })
    }

    // 2. private 服务：仅 Owner 可访问
    for _, svc := range privateServices {
        ownerIP := getClientTailscaleIP(svc.OwnerID)
        rules = append(rules, ACLRule{
            Src: [ownerIP],
            Dst: [fmt.Sprintf("%s:%d", svc.Agent.TailscaleIP, svc.ListenPort)],
        })
    }

    // 3. group 服务：组成员可访问
    for _, svc := range groupServices {
        memberIPs := getGroupMemberTailscaleIPs(svc.GroupID)
        rules = append(rules, ACLRule{
            Src: memberIPs,
            Dst: [fmt.Sprintf("%s:%d", svc.Agent.TailscaleIP, svc.ListenPort)],
        })
    }

    // 4. 额外授权（ServicePermission 表）
    for _, perm := range servicePermissions {
        clientIP := getClientTailscaleIP(perm.ClientID)
        rules = append(rules, ACLRule{
            Src: [clientIP],
            Dst: [fmt.Sprintf("%s:%d", perm.Service.Agent.TailscaleIP, perm.Service.ListenPort)],
        })
    }

    return rules
}
```

---

## 6. 配置变更

### 6.1 配置文件变更

文件：`config/server.toml`

**变更后配置**：

```toml
# AWECloud Signaling Server 配置文件

[web]
listen_addr = "0.0.0.0"
listen_port = 9090
default_admin_username = "admin"
default_admin_password = "admin123"

[security]
jwt_secret = "dev-secret-key-please-change-in-production"

[database]
path = "data/server.db"

[log]
level = "info"
file = ""

# Tailscale 配置（替代原 [server] 段的 FRP 配置）
[tailscale]
# Headscale 控制平面地址
headscale_url = "https://signaling.your-domain.com/headscale"
# Headscale API 密钥
headscale_api_key = ""
# Headscale 命名空间
namespace = "default"
# DERP 服务器地址
derp_url = "https://signaling.your-domain.com/derp"
# STUN 端口（避开 Coturn 的 3478）
stun_port = 3479
# IP 地址段（避免与 Pod 网络冲突）
ip_prefix = "100.64.0.0/10"
# 预认证密钥有效期（小时）
auth_key_expiry_hours = 24
```

### 6.2 配置结构变更

文件：`internal/common/config/server.go`

**移除 ServerSection**（原 FRP 配置），**新增 TailscaleSection**：

```txt
ServerConfig
├── Web        WebSection
├── Security   SecuritySection
├── Database   DatabaseSection
├── Log        LogConfig
└── Tailscale  TailscaleSection    // [新增] 替代原 Server

TailscaleSection
├── HeadscaleURL         string   // Headscale API 地址（必填）
├── HeadscaleAPIKey      string   // Headscale API 密钥（必填，从环境变量获取）
└── Namespace            string   // Headscale 命名空间（从 POD_NAMESPACE 环境变量获取）

# 以下配置存储在数据库 system_config 表，可通过 Web 管理界面修改：
# - derp_url: DERP 服务器地址
# - stun_port: STUN 端口
# - ip_prefix: IP 地址段
# - auth_key_expiry_hours: 预认证密钥有效期
```

### 6.3 环境变量支持

| 环境变量          | 说明                                           |
| ----------------- | ---------------------------------------------- |
| HEADSCALE_URL     | Headscale API 地址，默认 http://headscale:8080 |
| HEADSCALE_API_KEY | Headscale API 密钥（必填）                     |
| POD_NAMESPACE     | Pod 命名空间（K8S Downward API 自动注入）      |
| JWT_SECRET        | JWT 密钥（保留）                               |

### 6.4 默认值

**配置文件默认值**：

```txt
TailscaleSection 默认值：
├── HeadscaleURL    = "http://headscale:8080"
├── HeadscaleAPIKey = ""（必填，从环境变量获取）
└── Namespace       = "beagle-access"（从 POD_NAMESPACE 获取，或使用默认值）
```

**数据库配置默认值**（system_config 表）：

| 配置项                | 默认值            | 说明                           |
| --------------------- | ----------------- | ------------------------------ |
| derp_url              | {public_url}/derp | 根据 public_url 自动生成       |
| stun_port             | 3479              | STUN 端口，避开 Coturn 的 3478 |
| ip_prefix             | 100.64.0.0/10     | Tailscale IP 段                |
| auth_key_expiry_hours | 24                | 预认证密钥有效期（小时）       |

### 6.5 Web 管理界面配置

在 Web 管理界面 > 系统配置 中可修改以下 Tailscale 相关配置：

- DERP 服务器地址
- STUN 端口
- IP 地址段
- 预认证密钥有效期

---

## 7. 删除模块

### 7.1 FRP Server 模块

目录：`internal/server/frp/`

| 文件                 | 说明            |
| -------------------- | --------------- |
| `server.go`          | FRP Server 实现 |
| `websocket_proxy.go` | WebSocket 代理  |

直接删除整个目录。

### 7.2 相关代码清理

| 位置                     | 清理内容                  |
| ------------------------ | ------------------------- |
| `server.go`              | 移除 frpServer 相关代码   |
| `grpc/agent_service.go`  | 移除 FRP 配置参数         |
| `api/tunnel_config.go`   | 修改为返回 Tailscale 配置 |
| `api/stcp.go`            | 删除                      |
| `api/stcp_visitor.go`    | 删除                      |
| `api/tcp_service.go`     | 删除                      |
| `model/stcp_instance.go` | 删除                      |
| `model/stcp_visitor.go`  | 删除                      |
| `model/tcp_service.go`   | 删除                      |

---

## 8. 数据库迁移

### 8.1 迁移内容

```txt
1. 修改 agents 表
   - 新增 tailscale_ip VARCHAR(50)
   - 新增 ts_connected BOOLEAN DEFAULT FALSE
   - 新增 ts_conn_type VARCHAR(20)
   - 新增 ts_registered_at TIMESTAMP
   - 新增 ts_node_key VARCHAR(255)

2. 创建 proxy_services 表
   - id BIGINT PRIMARY KEY
   - name VARCHAR(100) UNIQUE NOT NULL
   - agent_id BIGINT NOT NULL
   - listen_port INT NOT NULL
   - target_addr VARCHAR(255) NOT NULL
   - status VARCHAR(20) DEFAULT 'stopped'
   - connections INT DEFAULT 0
   - bytes_in BIGINT DEFAULT 0
   - bytes_out BIGINT DEFAULT 0
   - remark TEXT
   - created_at TIMESTAMP
   - updated_at TIMESTAMP
   - UNIQUE INDEX (agent_id, listen_port)
   - FOREIGN KEY (agent_id) REFERENCES agents(id)

3. 迁移数据
   - stcp_instances → proxy_services
   - tcp_services → proxy_services

4. 修改 clients 表
   - 新增 tailscale_ip VARCHAR(50)

5. 删除旧表
   - DROP TABLE stcp_instances
   - DROP TABLE stcp_visitors
   - DROP TABLE tcp_services
```

### 8.2 GORM AutoMigrate

由于项目使用 GORM AutoMigrate，迁移会自动执行：

- 新增字段会自动添加
- 新表会自动创建
- 旧表需要手动删除（或保留不影响）

---

**文档版本**: 1.1
**创建日期**: 2025-01-08
**更新日期**: 2025-01-08
**关联文档**:

- [Tailscale 升级方案设计](design_tailscale_upgrade.md)
- [Tailscale 多租户安全设计](design_tailscale_security.md)
- [Tailscale 升级 - Server Web 变更设计](design_tailscale_server_web.md)
