# gRPC 双向流职责分离设计

## 问题

当前 Desktop 心跳流（Heartbeat）承担了两个职责：

1. 保活心跳：Desktop 定期发送心跳，Server 确认连接存活
2. 数据推送：Server 在心跳响应中携带已授权服务列表（authorized_services）

这导致：

- 心跳职责不纯粹，每次心跳都要查询数据库构建服务列表
- 服务列表更新频率被心跳频率绑定（30 秒一次）
- gRPC 断开时，服务列表丢失，前端页面空白
- 主机列表、设备列表等数据没有推送机制，只能前端主动请求

Agent 心跳流也有类似问题：心跳响应中携带服务配置（ServiceConfig、ForwardConfig），每次心跳都查询数据库。

## 设计目标

1. 心跳回归心跳：只负责保活和状态上报
2. 业务数据通过独立的双向流推送：Server 数据变更时主动推送
3. Desktop 本地缓存所有推送数据，gRPC 断开时仍可展示

## 当前 gRPC 通信全景

### Server ↔ Desktop

| 方法                | 类型     | 当前职责            | 问题                     |
| ------------------- | -------- | ------------------- | ------------------------ |
| Heartbeat           | 双向流   | 心跳 + 推送服务列表 | 职责混合                 |
| GetAuthorizedHosts  | 一元 RPC | 获取主机列表        | 断开时不可用             |
| GetHostServices     | 一元 RPC | 获取主机服务        | 断开时不可用             |
| GetMyDevices        | 一元 RPC | 获取设备列表        | 断开时不可用             |
| GetFavoriteServices | 一元 RPC | 获取收藏列表        | 断开时不可用             |
| ToggleFavorite      | 一元 RPC | 切换收藏            | 写操作，断开时不可用合理 |
| OfflineDevice       | 一元 RPC | 设备下线            | 写操作，断开时不可用合理 |
| DeleteDevice        | 一元 RPC | 删除设备            | 写操作，断开时不可用合理 |
| Authenticate        | 一元 RPC | 认证                | 正常                     |
| CreateLoginSession  | 一元 RPC | 创建登录会话        | 正常                     |
| WaitForLoginResult  | 双向流   | 等待登录结果        | 正常                     |
| Logout              | 一元 RPC | 注销                | 正常                     |

### Server ↔ Agent

| 方法                | 类型     | 当前职责            | 问题     |
| ------------------- | -------- | ------------------- | -------- |
| Heartbeat           | 双向流   | 心跳 + 推送服务配置 | 职责混合 |
| Register            | 一元 RPC | 注册                | 正常     |
| Authenticate        | 一元 RPC | 认证                | 正常     |
| GetRealtimeStatus   | 一元 RPC | 获取实时状态        | 正常     |
| ReportProxyStatus   | 一元 RPC | 上报服务状态        | 正常     |
| ReportVisitorStatus | 一元 RPC | 上报访问者状态      | 正常     |
| ReportNetworkChange | 一元 RPC | 上报网络变化        | 正常     |

## 重构方案

### 原则

- 心跳流：只传递心跳信号和连接状态（隧道 IP、连接状态）
- 数据流：新增独立的双向流，Server 主动推送业务数据变更
- 一元 RPC：写操作保持不变（ToggleFavorite、OfflineDevice 等）
- 读操作的一元 RPC（GetAuthorizedHosts 等）保留，作为主动刷新手段

### 1. Desktop 数据流（新增）

新增 `DataStream` 双向流，替代心跳中的数据推送职责：

```
rpc DataStream(stream DesktopDataRequest) returns (stream DesktopDataResponse);
```

Desktop → Server（请求）：

- 首次连接时发送 desktop_id 建立流
- 可发送主动刷新请求（指定刷新哪类数据）

Server → Desktop（推送）：

- 服务列表变更时推送（Agent 上下线、服务配置变更）
- 主机列表变更时推送（SSH 权限变更、Agent 状态变化）
- 设备列表变更时推送（其他设备上下线）
- 收藏列表变更时推送（收藏操作后）

推送消息结构：

```
DesktopDataResponse {
  DataType type          // 数据类型枚举
  ServiceListData services       // 服务列表（当 type = SERVICES）
  HostListData hosts             // 主机列表（当 type = HOSTS）
  DeviceListData devices         // 设备列表（当 type = DEVICES）
  FavoriteListData favorites     // 收藏列表（当 type = FAVORITES）
}
```

数据类型枚举：

- SERVICES：已授权服务列表
- HOSTS：已授权主机列表
- DEVICES：我的设备列表
- FAVORITES：收藏的服务 ID 列表

### 2. Desktop 心跳流（精简）

心跳请求不变：

```
DesktopHeartbeatRequest {
  desktop_id
  tunnel_ip
  tunnel_connected
}
```

心跳响应精简为：

```
DesktopHeartbeatResponse {
  // 空响应，仅作为心跳确认
}
```

移除 `authorized_services` 字段。

### 3. Agent 数据流（新增）

新增 `DataStream` 双向流：

```
rpc DataStream(stream AgentDataRequest) returns (stream AgentDataResponse);
```

Agent → Server（请求）：

- 首次连接时发送 agent_id 建立流
- 可发送主动刷新请求

Server → Agent（推送）：

- 服务配置变更时推送（Web 管理界面修改服务配置）
- 端口转发配置变更时推送

推送消息结构：

```
AgentDataResponse {
  int64 config_version
  repeated ServiceConfig services
  repeated ForwardConfig forwards
}
```

### 4. Agent 心跳流（精简）

心跳请求不变（保留状态上报）。

心跳响应精简为：

```
AgentHeartbeatResponse {
  // 空响应，仅作为心跳确认
}
```

移除 `config_version`、`services`、`forwards` 字段。

## Server 端推送触发时机

### Desktop 数据推送触发

| 事件                     | 推送数据类型    | 说明                   |
| ------------------------ | --------------- | ---------------------- |
| Agent 上线/下线          | SERVICES, HOSTS | 服务和主机状态变化     |
| 服务配置变更（Web 管理） | SERVICES        | 新增/删除/修改服务     |
| SSH 权限变更（Web 管理） | HOSTS           | SSH 授权变化           |
| 其他设备上下线           | DEVICES         | 同用户的设备状态变化   |
| 收藏操作                 | FAVORITES       | ToggleFavorite 后推送  |
| DataStream 首次建立      | ALL             | 推送所有数据的初始快照 |

### Agent 数据推送触发

| 事件                     | 说明                   |
| ------------------------ | ---------------------- |
| 服务配置变更（Web 管理） | 新增/删除/修改代理服务 |
| 端口转发配置变更         | 新增/删除/修改端口转发 |
| DataStream 首次建立      | 推送当前完整配置       |

## Desktop 端缓存策略

Desktop Go 后端维护内存缓存：

- 服务列表缓存
- 主机列表缓存
- 设备列表缓存
- 收藏列表缓存

缓存更新规则：

1. DataStream 推送到达时，更新对应缓存
2. 一元 RPC 成功时，也更新对应缓存（主动刷新场景）
3. gRPC 断开时，前端读取缓存数据
4. 注销时清空所有缓存

## 连接生命周期

```
Desktop 启动
    │
    ▼
Authenticate（一元 RPC）
    │
    ▼
并行建立两个流：
├── Heartbeat 流（保活）
└── DataStream 流（数据推送）
    │
    ├── Server 推送初始数据快照
    │
    ▼
正常运行
    │
    ├── 心跳流：30s 一次心跳
    ├── 数据流：Server 有变更时推送
    │
    ▼
gRPC 断开
    │
    ├── 前端使用缓存数据
    ├── 心跳流自动重连
    └── DataStream 自动重连
        │
        ▼
    重连成功后 Server 推送完整快照
```

## 实现步骤

1. Proto 定义：新增 DataStream RPC 和相关消息类型
2. Server 实现：DataStream 服务端，事件触发推送机制
3. Desktop 客户端：DataStream 客户端，缓存管理
4. 精简心跳：移除心跳中的业务数据
5. Agent 同步改造：Agent 的 DataStream

建议分两阶段：

- 第一阶段：Desktop DataStream + 心跳精简
- 第二阶段：Agent DataStream + 心跳精简

## 兼容性

Proto 变更涉及心跳响应字段移除，需要 Server 和 Desktop/Agent 同时更新。建议：

- 过渡期：心跳响应保留旧字段但不再填充数据
- Desktop/Agent 优先从 DataStream 获取数据
- 确认所有客户端更新后，移除旧字段
