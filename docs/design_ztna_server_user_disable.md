# Server 用户禁用设计

相关文档：

- `design_ztna_server_user.md` — 用户管理设计（用户创建、删除、编辑、认证）
- `design_ztna_server_device.md` — 设备管理设计（设备断连、删除）
- `design_ztna_server_heartbeat.md` — 心跳业务设计（在线状态管理）
- `design_ztna_data_headscale.md` — Headscale 数据交互（Node 过期和删除）

## 概述

用户禁用是一个安全控制功能，用于立即阻止某个用户的所有访问能力。当管理员禁用一个用户（Client 或 Agent）时，系统必须确保该用户的所有设备立即离线，无法继续访问内网资源。

用户禁用与用户删除的区别：

- 禁用：保留用户数据，可恢复，适用于临时封禁、安全事件响应
- 删除：永久移除用户及其所有关联数据，不可恢复

用户禁用涉及三个层面的控制：

1. 业务层：阻止新设备注册和认证
2. 连接层：断开已连接设备的 gRPC 心跳流
3. 隧道层：使 Headscale 节点失效，断开 Tailscale 隧道

## 业务需求

### 禁用效果

当管理员禁用用户 zhangsan（Client 角色）时，系统必须达到以下效果：

#### 效果 1：阻止新设备接入

禁用后，该用户的任何新设备都无法注册或认证：

- Desktop 新设备通过 Logto 登录时，显示"用户已禁用，请联系管理员"
- Desktop 已有设备使用保存的凭证重连时，认证失败
- CloudIDE Pod 使用 Deploy Token 注册时，返回"用户已禁用"
- Agent 使用 Deploy Token 注册时，返回"用户已禁用"

#### 效果 2：断开已连接设备的 gRPC 连接（Server 层面）

禁用后，该用户的所有已连接设备立即离线：

- Desktop 设备的心跳流（Heartbeat）被服务端主动断开
- Desktop 设备的数据流（DataStream）被服务端主动断开
- Agent 设备的心跳流（Heartbeat）被服务端主动断开
- 数据库中该用户所有 Node 的 last_heartbeat 字段被清空

断开连接后，设备端会感知到连接断开，停止尝试重连（因为重连时认证会失败）。

#### 效果 3：魔法 DNS 失效

由于系统架构依赖魔法 DNS 进行域名解析，即使客户端保持与 Headscale 的连接，没有 Server 提供的 DNS 解析服务也无法访问任何资源。因此：

- 禁用用户时，只需在 Server 层面断开连接即可
- 不需要清理 Headscale 节点（避免对测试用户造成过大影响）
- Headscale 节点清理推迟到删除用户时执行

这种设计的优势：

- 对测试用户影响最小，重新启用后可快速恢复
- 简化禁用流程，提高响应速度
- 避免 Headscale API 调用失败影响禁用操作

### 启用效果

当管理员重新启用用户时，系统恢复该用户的访问能力：

- 新设备可以正常注册和认证
- 已有设备可以使用保存的凭证重新连接
- 设备重新连接后，自动获取新的 Headscale AuthKey，重新加入隧道

启用操作不会主动通知已离线的设备，设备需要自行重试连接。

## 核心业务

### 禁用用户

禁用用户是一个编排操作，包含两个子任务，按顺序执行：

```
管理员请求禁用用户
    │
    ├─ 子任务 1：更新数据库 User.Enabled = false
    │
    ├─ 子任务 2：断开所有已连接设备的 gRPC 连接
    │      ├─ 遍历该用户的所有 Node
    │      ├─ 对每个 Node，检查是否有活跃的 gRPC 连接
    │      ├─ 断开心跳流（Heartbeat）
    │      ├─ 断开数据流（DataStream，仅 Desktop）
    │      └─ 清空数据库 Node.last_heartbeat 和 Node.ip
    │
    └─ 记录审计日志
```

执行顺序说明：

1. 先更新数据库，确保后续的认证请求被拒绝
2. 再断开 gRPC 连接，使设备立即感知离线

子任务 2 的失败不影响禁用操作的成功，只记录日志。因为数据库已更新，即使连接未断开，设备重连时也会被拒绝。

注意：禁用用户时不清理 Headscale 节点，因为系统依赖魔法 DNS，即使客户端保持隧道连接也无法访问资源。Headscale 节点清理推迟到删除用户时执行。

### 删除用户

删除用户是一个编排操作，包含四个子任务，按顺序执行：

```
管理员请求删除用户
    │
    ├─ 子任务 1：断开所有已连接设备的 gRPC 连接
    │      ├─ 遍历该用户的所有 Node
    │      ├─ 对每个 Node，检查是否有活跃的 gRPC 连接
    │      ├─ 断开心跳流（Heartbeat）
    │      └─ 断开数据流（DataStream，仅 Desktop）
    │
    ├─ 子任务 2：清理 Headscale 资源
    │      ├─ 查询该用户在 Headscale 中的所有节点
    │      ├─ 对每个节点调用 DeleteNode API
    │      ├─ 调用 DeleteUser API 删除 Headscale 用户
    │      └─ 失败时记录日志，不阻塞流程
    │
    ├─ 子任务 3：删除数据库相关数据
    │      ├─ 删除所有 Node
    │      ├─ 删除所有 ProxyService
    │      ├─ 删除所有 PortForward
    │      └─ 删除所有 GroupMember
    │
    ├─ 子任务 4：删除用户记录
    │
    └─ 记录审计日志
```

删除用户时会彻底清理 Headscale 资源，确保不留下任何残留数据。

### 认证时检查用户状态

所有设备认证入口都必须检查用户状态：

#### Desktop Logto 登录

在 Logto 回调处理中检查：

```
Logto 回调处理
    │
    ├─ 查询或创建 User
    │
    ├─ 检查 User.Enabled
    │      如果 enabled = false：
    │      ├─ 更新 session status → failed
    │      ├─ 通知 gRPC 流（IsDisabled=true）
    │      ├─ 显示错误页面："用户已禁用，请联系管理员"
    │      └─ 流程终止
    │
    └─ 继续正常登录流程
```

#### Desktop 重连认证

在 gRPC Authenticate 中检查：

```
gRPC Authenticate
    │
    ├─ 验证 Node 存在且 secret 正确
    │
    ├─ 查询 User（通过 Node.UserID）
    │
    ├─ 检查 User.Enabled
    │      如果 enabled = false：
    │      └─ 返回 {Success: false, Message: "用户已禁用"}
    │
    └─ 继续正常认证流程
```

#### Deploy Token 注册

在 REST /api/v1/register 中检查：

```
Deploy Token 注册
    │
    ├─ 验证 Deploy Token
    │
    ├─ 查询关联的 User
    │
    ├─ 检查 User.Enabled
    │      如果 enabled = false：
    │      └─ 返回 403 Forbidden "用户已禁用"
    │
    └─ 继续正常注册流程
```

### 心跳时检查用户状态

设备心跳流建立后，定期检查用户状态，发现禁用时主动断开：

#### Desktop 心跳

在 gRPC Heartbeat 中检查：

```
gRPC Heartbeat（双向流）
    │
    ├─ 首次消息：验证 Node 存在
    │
    ├─ 建立心跳流，进入循环
    │
    └─ 每次收到心跳消息时：
           ├─ 查询 User（通过 Node.UserID）
           ├─ 检查 User.Enabled
           │      如果 enabled = false：
           │      ├─ 记录日志："用户已禁用，断开心跳流"
           │      ├─ 清空 Node.last_heartbeat 和 Node.ip
           │      └─ 返回错误，断开流
           │
           └─ 继续正常心跳处理
```

#### Agent 心跳

在 gRPC Heartbeat 中检查（与 Desktop 相同）：

```
gRPC Heartbeat（双向流）
    │
    ├─ 首次消息：验证 Node 存在
    │
    ├─ 建立心跳流，进入循环
    │
    └─ 每次收到心跳消息时：
           ├─ 查询 User（通过 Node.UserID）
           ├─ 检查 User.Enabled
           │      如果 enabled = false：
           │      ├─ 记录日志："用户已禁用，断开心跳流"
           │      ├─ 清空 Node.last_heartbeat 和 Node.ip
           │      └─ 返回错误，断开流
           │
           └─ 继续正常心跳处理
```

心跳检查的频率取决于客户端发送心跳的频率（通常 30 秒一次）。这意味着禁用用户后，最多 30 秒内设备会被断开。

## 数据变更

### 禁用用户时

| 表/系统           | 操作                                                  | 说明                       |
| ----------------- | ----------------------------------------------------- | -------------------------- |
| user              | UPDATE enabled = false WHERE id = ?                   | 标记用户为禁用状态         |
| node              | UPDATE last_heartbeat = NULL, ip = '' WHERE user_id = ? | 清空该用户所有设备的心跳信息 |
| Headscale（外部） | ExpireNode(node_id) 对该用户的所有节点                | 使隧道节点失效             |
| 内存              | 断开 gRPC 连接（connections、dataStreams map）        | 立即断开活跃连接           |

### 启用用户时

| 表/系统 | 操作                            | 说明               |
| ------- | ------------------------------- | ------------------ |
| user    | UPDATE enabled = true WHERE id = ? | 标记用户为启用状态 |

## 异常处理

### 断开 gRPC 连接失败

场景：内存中找不到该设备的连接（设备已离线或 Server 重启）

处理：忽略，继续执行后续子任务。设备重连时会被认证拒绝。

### Headscale 节点过期失败

场景：Headscale API 调用失败（网络问题、Headscale 服务异常）

处理：记录错误日志，不阻塞禁用流程。数据库已更新，设备重连时会被拒绝。管理员可稍后手动清理 Headscale 节点。

### 查询 Headscale 节点失败

场景：无法查询该用户的 Headscale 节点列表

处理：记录错误日志，跳过子任务 3。数据库已更新，设备重连时会被拒绝。

## 时序说明

### 禁用用户时序

```
管理员                    Server API                Server gRPC              Headscale
    │                         │                          │                       │
    ├─ PUT /api/v1/users/:id/disable ──▶│                          │                       │
    │                         │                          │                       │
    │                         ├─ UPDATE user.enabled=false                       │
    │                         │                          │                       │
    │                         ├─ 查询该用户的所有 Node    │                       │
    │                         │                          │                       │
    │                         ├─ 遍历 Node，断开连接 ────▶│                       │
    │                         │                          ├─ Cancel 心跳流        │
    │                         │                          ├─ Cancel 数据流        │
    │                         │                          ├─ 清空 last_heartbeat  │
    │                         │                          │                       │
    │                         ├─ 查询 Headscale 节点列表 ────────────────────────▶│
    │                         │                          │                       │
    │                         ├─ 遍历节点，调用 ExpireNode ─────────────────────▶│
    │                         │                          │                       │
    │◀─ 200 OK ───────────────│                          │                       │
    │                         │                          │                       │
    │                         │                          │                       │
Desktop                      │                          │                       │
    │                         │                          │                       │
    ├─ Heartbeat ────────────────────────────────────────▶│                       │
    │                         │                          ├─ 查询 User            │
    │                         │                          ├─ 检查 enabled=false   │
    │◀─ 错误：用户已禁用 ──────────────────────────────────│                       │
    │                         │                          │                       │
    ├─ Authenticate ─────────────────────────────────────▶│                       │
    │                         │                          ├─ 查询 User            │
    │                         │                          ├─ 检查 enabled=false   │
    │◀─ 认证失败：用户已禁用 ──────────────────────────────│                       │
```

### 启用用户时序

```
管理员                    Server API                Desktop
    │                         │                          │
    ├─ PUT /api/v1/users/:id/enable ───▶│                          │
    │                         │                          │
    │                         ├─ UPDATE user.enabled=true│
    │                         │                          │
    │◀─ 200 OK ───────────────│                          │
    │                         │                          │
    │                         │                          │
    │                         │                          ├─ 设备自行重试连接
    │                         │                          │
    │                         │◀─ Authenticate ──────────│
    │                         │                          │
    │                         ├─ 检查 enabled=true       │
    │                         ├─ 认证成功                │
    │                         ├─ 创建 Headscale AuthKey  │
    │                         │                          │
    │                         ├─ 返回 AuthKey ──────────▶│
    │                         │                          │
    │                         │                          ├─ 连接 Headscale 隧道
```

## 安全考虑

### 禁用响应时间

从管理员点击禁用到设备完全离线的时间：

- 认证阻止：立即生效（数据库更新后）
- gRPC 断开：立即生效（API 调用完成后）
- 隧道断开：取决于 Headscale API 响应时间（通常 < 1 秒）
- 心跳检测：最多 30 秒（下次心跳时检测到）

总体响应时间：< 1 秒（新连接）到 30 秒（已连接设备）。

### 禁用期间的访问尝试

禁用期间，设备的所有访问尝试都会被拒绝：

- 新设备注册：认证失败，无法获取 AuthKey
- 已有设备重连：认证失败，无法获取 AuthKey
- 已连接设备：心跳流被断开，无法继续通信
- 隧道访问：Headscale 节点失效，无法建立隧道连接

### 禁用与删除的选择

| 场景                 | 推荐操作 | 原因                                   |
| -------------------- | -------- | -------------------------------------- |
| 临时封禁             | 禁用     | 可恢复，保留数据                       |
| 安全事件响应         | 禁用     | 快速阻止访问，保留证据                 |
| 员工离职（短期）     | 禁用     | 可能需要恢复访问                       |
| 员工离职（长期）     | 删除     | 永久移除，释放资源                     |
| 测试账号清理         | 删除     | 不需要保留数据                         |
| Logto 注册待审批     | 禁用     | 新用户默认禁用，审批后启用             |

## 实施要点

### Desktop 用户体验

Desktop 客户端如何感知用户被禁用以及用户体验设计，详见：

- `design_ztna_desktop_host_user_disable.md` — Desktop.Host 用户禁用体验设计
- `design_ztna_desktop_pod_user_disable.md` — Desktop.Pod (CloudIDE) 用户禁用体验设计

关键点：

- Desktop.Host 通过认证失败和心跳错误感知禁用状态，显示错误对话框并提供"退出"按钮
- Desktop.Pod 通过注册失败感知禁用状态，记录日志并持续重试注册
- 运行中被禁用时，Desktop.Pod 继续运行但无法访问资源（依赖魔法 DNS）
- 需要优化重连策略，避免无限重连浪费资源
- 建议使用 gRPC 标准错误码（PermissionDenied）

### 代码修改位置

1. 用户禁用 API（`internal/server/api/user.go`）
   - 在 `setUserEnabled` 方法中添加子任务 2 和 3

2. Desktop 认证（`internal/server/grpc/desktop_service.go`）
   - 在 `Authenticate` 方法中添加 `user.Enabled` 检查
   - 在 `Heartbeat` 方法中添加 `user.Enabled` 检查

3. Desktop 登录回调（`internal/server/api/auth_desktop.go`）
   - 在 `findOrCreateUser` 方法中已有检查，无需修改

4. Agent 认证（`internal/server/grpc/agent_service.go`）
   - 在 `Authenticate` 方法中添加 `user.Enabled` 检查
   - 在 `Heartbeat` 方法中添加 `user.Enabled` 检查

5. Deploy Token 注册（`internal/server/api/deploy.go`）
   - 在注册方法中已有检查，无需修改

### 测试要点

1. 禁用前已连接的 Desktop 设备是否立即离线
2. 禁用前已连接的 Agent 设备是否立即离线
3. 禁用后新设备是否无法注册
4. 禁用后已有设备是否无法重连
5. 启用后设备是否可以正常重连
6. Headscale 节点是否被正确过期
7. 禁用操作的审计日志是否正确记录
