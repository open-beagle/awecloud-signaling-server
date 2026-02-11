# Server 用户管理设计

相关文档：

- `design_ztna_server_device.md` — 设备管理设计（删除用户子任务 1：批量删除设备）
- `design_ztna_server_heartbeat.md` — 心跳业务设计
- `design_ztna_acl.md` — 授权管理设计（删除用户子任务 2：授权清理）
- `design_ztna_data_database.md` — 数据库表设计（User 表、Node 表、DeployToken 表结构）
- `design_ztna_data_server.md` — 业务实体设计（User.List、User.Agent.Detail、User.Client.Detail 组装方式）
- `design_ztna_data_headscale.md` — Headscale 数据交互（User/Node/PreAuthKey/Tag 管理）

## 概述

User 是系统中的核心实体，代表一个可以连接到隧道网络的身份。系统中有两种角色的用户：

- Agent：部署在内网环境中的代理，提供对内网服务（SSH、MySQL、Redis 等）的访问。每个 Agent 用户通常对应一台内网服务器。
- Client：终端用户，通过 Desktop 客户端或 CloudIDE（Pod）访问内网服务。一个 Client 用户可以有多个设备（多台电脑、多个 CloudIDE 实例）。

User 不直接连接网络。User 拥有 Node（设备），Node 才是实际连接 Tailscale 隧道的实体。User 与 Node 是一对多关系。

User 在三个系统中存在对应实体：

| 系统             | 实体    | 命名规则          | 说明                                        |
| ---------------- | ------- | ----------------- | ------------------------------------------- |
| 数据库（SQLite） | User 表 | name 字段         | 业务主体，存储角色、密钥、启用状态等        |
| Headscale        | User    | {role}-{name}     | 隧道身份，如 agent-beijing、client-shucheng |
| ACL Policy       | Tag     | tag:{role}-{name} | 访问控制标识，如 tag:agent-beijing          |

三个系统的数据必须保持一致，否则会导致隧道连接失败或权限错误。

## 核心业务

### 创建用户

创建用户涉及两个系统的写入：

```
管理员请求创建
    │
    ├─ 1. 检查名称唯一性（数据库）
    │
    ├─ 2. 生成随机密钥，bcrypt 哈希
    │
    ├─ 3. 在 Headscale 创建 User
    │      命名：{role}-{name}
    │      如：agent-beijing 或 client-shucheng
    │      记录返回的 HeadscaleUID
    │
    ├─ 4. 写入数据库 User 表
    │      包含 HeadscaleUID
    │
    └─ 5. 返回明文密钥（仅此一次）
```

创建用户时不创建 Node，也不创建 Headscale Node。Node 在设备首次注册/心跳时自动创建。

来源说明：

- manual：管理员通过 Web 界面手动创建
- logto：用户通过 Logto SSO 登录 Desktop 时自动创建（默认 enabled=false，需管理员审批）

### 删除用户

删除用户是一个编排操作，包含三个子任务，每个子任务有独立的业务逻辑。用户管理只负责编排顺序，不负责子任务的具体实现。

执行顺序：子任务 1 → 子任务 2 → 子任务 3。先删设备（断连接、清隧道），再清授权（清规则），最后删用户本身。

子任务 1：删除该用户的所有设备

- 关联文档：`design_ztna_server_device.md`「批量删除（用户删除触发）」
- 包含：关闭 gRPC 连接、删除 Headscale Node、删除数据库 Node

子任务 2：清理该用户的所有授权关系

- 关联文档：`design_ztna_acl.md`「用户删除时的授权清理」
- 包含：清理所有 ACL 表中该用户作为授权方或被授权方的记录，Agent 用户还需清理关联服务和端口转发的授权

子任务 3：删除用户自身（本文档负责）

```
删除用户自身
    │
    ├─ 删除 Headscale User（使用 {role}-{name}）
    ├─ 删除数据库 User 记录
    └─ 记录审计日志
```

子任务 1 和 2 的具体实现在各自的关联文档中，这里不展开。

Headscale User 删除失败时忽略错误，数据库 User 仍然删除。Headscale 中残留的 User 不影响业务。

### 编辑用户

可编辑的字段：

| 字段    | 说明      | 影响范围                               |
| ------- | --------- | -------------------------------------- |
| Alias   | 显示名称  | 仅数据库，无外部影响                   |
| Enabled | 启用/禁用 | 禁用后设备无法注册，但已连接的不会断开 |

编辑用户只写数据库，不涉及 Headscale 操作。

### 重置密钥

```
管理员请求重置
    │
    ├─ 1. 生成新的随机密钥
    │
    ├─ 2. bcrypt 哈希后写入数据库
    │
    └─ 3. 返回明文密钥（仅此一次）
```

重置密钥只影响 User 自身的 SecretHash 字段，不影响该用户下的任何设备。设备有自己的认证机制（Desktop 用 Node.SecretHash，Agent 用 gRPC 流），与 User 密钥无关。User 密钥仅在 Agent 首次注册（gRPC Register）时用于身份验证。

### 启用/禁用

禁用用户后：

- 新的注册请求会被拒绝
- 新的 Deploy Token 注册会被拒绝
- 已连接的设备不会立即断开（需要手动操作）
- Desktop Logto 登录时会返回"用户已禁用"

### 查询用户

列表查询聚合多个表的统计信息：

| 统计项   | 来源                        | 说明                                |
| -------- | --------------------------- | ----------------------------------- |
| 设备数量 | Node 表 COUNT               | 该用户的 Node 总数                  |
| 在线数量 | Node 表 SUM                 | last_heartbeat 在 60 秒内的 Node 数 |
| 服务数量 | ProxyService 表 COUNT       | 该用户的服务总数                    |
| 分组数量 | GroupMember 表 COUNT        | 所属分组数                          |
| 最后在线 | Node 表 MAX(last_heartbeat) | 最近一次心跳时间                    |

详情查询额外返回：

- 设备列表（Node 表，含在线状态）
- 服务列表（ProxyService 表，含运行状态，仅 Agent）

### 设备认证

设备认证是设备接入系统的入口。认证完成后，Server 为设备创建 Headscale PreAuthKey，设备用 AuthKey 连接 Headscale 隧道。详细的设备创建逻辑见 `design_ztna_server_device.md`「设备创建」。

认证完成时 Server 已知的关键信息：

- User（哪个用户）
- Node.Name（设备名称，即未来 Headscale Node 的 GivenName）
- Headscale User（{role}-{name}）

这意味着数据库 Node 与 Headscale Node 的逻辑关联在认证时就已确定（Headscale User + GivenName = 数据库 User + Node.Name）。HeadscaleNodeID 的物理关联在首次心跳时写入，详见 `design_ztna_server_device.md`「HeadscaleNodeID 的写入时机」。

#### Deploy Token 认证

Deploy Token 认证用于 Agent 和 Pod（CloudIDE）的注册，通过 REST API 完成。

写入数据：

| 认证方式                 | 写入的表          | 写入的字段                                                            |
| ------------------------ | ----------------- | --------------------------------------------------------------------- |
| Agent Deploy Token 注册  | deploy_tokens     | status→bound, device_fingerprint, device_name, bound_at, last_used_at |
| Agent Deploy Token 注册  | Headscale（外部） | PreAuthKey（user=agent-{name}，24h 有效期）                           |
| Client Deploy Token 注册 | deploy_tokens     | status→bound, device_fingerprint, device_name, bound_at, last_used_at |
| Client Deploy Token 注册 | Headscale（外部） | PreAuthKey（user=client-{name}，带身份 Tag + 分组 Tag）               |

Agent Deploy Token 注册（REST /api/v1/register）：

```
Agent 安装脚本 → POST /api/v1/register(token, fingerprint, device_name)
    → 验证 Deploy Token（查 deploy_tokens 表）
    → 检查 Token 可用性（pending 或 bound 且指纹匹配）
    → 检查关联 User 是否启用
    → 首次使用：绑定设备（更新 deploy_tokens 表）
    → 获取 Headscale User（agent-{name}）
    → 创建 PreAuthKey（无 Tag，24h 有效期）
    → 返回 AuthKey + ServerURL + Agent 配置
```

Client Deploy Token 注册（CloudIDE/Pod 场景）：

```
Pod 启动 → POST /api/v1/register(token, fingerprint)
    → 验证 Deploy Token
    → 绑定设备（更新 deploy_tokens 表）
    → 获取或创建 Headscale User（client-{name}）
    → 创建 PreAuthKey（带身份 Tag + 分组 Tag）
    → 返回 AuthKey + ServerURL
```

#### Desktop Logto 认证

Desktop 登录是一个完整的核心业务流程，涉及 gRPC 服务、REST API、Logto SSO 三方协作。整体流程分为三个阶段：创建登录会话、Logto 认证回调、等待登录结果。另外还有一个独立的注销子流程。

整体时序：

```
Desktop 客户端                    Server gRPC                   Server REST                    Logto
    │                               │                              │                            │
    ├─ CreateLoginSession ─────────▶│                              │                            │
    │◀─ session_id + login_url ─────│                              │                            │
    │                               │                              │                            │
    ├─ WaitForLoginResult(stream) ─▶│ 注册结果通道，阻塞等待       │                            │
    │                               │                              │                            │
    ├─ WebView 打开 login_url ─────────────────────────────────────▶│                            │
    │                               │                              ├─ 重定向到 Logto ──────────▶│
    │                               │                              │                            │
    │                               │                              │◀─ 用户完成登录，回调 ──────│
    │                               │                              │                            │
    │                               │                              ├─ 查询或创建 User           │
    │                               │                              ├─ 创建或更新 Node           │
    │                               │                              ├─ 创建 Headscale PreAuthKey │
    │                               │                              ├─ 更新 session → completed  │
    │                               │                              ├─ NotifyLoginResult ───────▶│（通知 gRPC 流）
    │                               │                              │                            │
    │◀─ 登录结果（凭证）────────────│ 收到通知，推送结果            │                            │
    │                               │                              │                            │
    ├─ Authenticate ───────────────▶│ 验证 node secret             │                            │
    ├─ Heartbeat(stream) ──────────▶│ 建立心跳流                   │                            │
```

##### 阶段一：创建登录会话

gRPC CreateLoginSession，Desktop 启动时调用。

```
Desktop → gRPC CreateLoginSession(device_fingerprint, device_name, username_hint)
    │
    ├─ 检查 Logto 是否已配置
    │
    ├─ 创建 desktop_login_sessions 记录
    │      session_id: UUID
    │      status: pending
    │      device_fingerprint, device_name, username_hint
    │      expires_at: 当前时间 + 10 分钟
    │
    ├─ 创建内存 SessionStorage（Logto SDK 需要）
    │
    ├─ 注册登录结果通道（chan LoginResult，供阶段三使用）
    │
    └─ 返回 session_id + login_url（相对路径 /auth/desktop/{session_id}）
```

写入数据：

| 表                     | 字段                                                                                   |
| ---------------------- | -------------------------------------------------------------------------------------- |
| desktop_login_sessions | session_id, device_fingerprint, device_name, username_hint, status=pending, expires_at |
| 内存                   | SessionStorage（Logto SDK 状态）、LoginResult 通道                                     |

##### 阶段二：Logto 认证回调

REST API，分两步：登录页重定向 + 回调处理。

步骤 1：Desktop WebView 访问 login_url

```
GET /auth/desktop/{session_id}
    │
    ├─ 查找 desktop_login_sessions 记录
    ├─ 检查 session 状态（必须 pending）和过期时间
    ├─ 从内存获取 SessionStorage
    ├─ 调用 Logto SDK 生成登录 URL（带 login_hint）
    └─ 302 重定向到 Logto 登录页面
```

步骤 2：用户在 Logto 完成登录后回调

```
GET /auth/desktop/callback?code=xxx&state=xxx
    │
    ├─ 遍历所有 pending 状态的 session
    │      用每个 session 的 SessionStorage 尝试处理回调
    │      Logto SDK 通过 state 参数匹配正确的 session
    │
    ├─ 查询或创建 User
    │      优先用 Logto username，其次 email 前缀，最后 sub
    │      已存在：检查 enabled 状态
    │      不存在：创建新 User（source=logto, enabled=false，待管理员审批）
    │
    ├─ 用户被禁用或待审批时：
    │      更新 session status → failed
    │      通过 NotifyLoginResult 通知 gRPC 流（IsDisabled=true）
    │      显示错误页面
    │      流程终止
    │
    ├─ 用户正常时：
    │      更新 session status → completed, user_id
    │      绑定 userID → sessionID 映射（注销时用）
    │      通过 NotifyLoginResult 通知 gRPC 流（Success=true）
    │      显示成功页面
    │
    └─ 注意：此阶段不创建 Node 和 AuthKey，由阶段三完成
```

写入数据：

| 表                     | 字段                                                          |
| ---------------------- | ------------------------------------------------------------- |
| user                   | 新建时：name, alias, role=client, enabled=false, source=logto |
| desktop_login_sessions | status→completed 或 failed, user_id, completed_at             |
| 内存                   | userID → sessionID 映射                                       |

##### 阶段三：等待登录结果

gRPC WaitForLoginResult（双向流），Desktop 在阶段一之后立即调用，阻塞等待。

```
Desktop → gRPC WaitForLoginResult(session_id, device_fingerprint)
    │
    ├─ 检查 session 是否存在且未过期
    │
    ├─ 获取登录结果通道（阶段一注册的）
    │
    ├─ 阻塞等待（超时 5 分钟）
    │      ├─ 超时 → 返回 TIMEOUT
    │      ├─ 通道关闭 → 返回 FAILED
    │      ├─ 收到失败结果 → 返回 FAILED 或 DISABLED
    │      └─ 收到成功结果 → 继续处理
    │
    ├─ 创建或更新 Node（Desktop 设备）
    │      按 user_id + type=desktop + name 查找
    │      新设备：创建 node 记录
    │      已有设备：更新 secret_hash
    │      生成新的 node secret（每次登录都重新生成）
    │
    ├─ 获取 Headscale AuthKey
    │      获取或创建 Headscale User（client-{name}）
    │      创建 PreAuthKey（带身份 Tag + 分组 Tag，24h 有效期）
    │
    ├─ 清理登录会话（UnregisterLoginSession）
    │
    └─ 推送登录结果给 Desktop
           desktop_id, device_token(secret), auth_key, server_url, username
```

写入数据：

| 表                | 字段                                                                        |
| ----------------- | --------------------------------------------------------------------------- |
| node              | 新建或更新：user_id, name, type=desktop, secret_hash, hostname, system_info |
| Headscale（外部） | PreAuthKey（user=client-{name}，带身份 Tag + 分组 Tag，24h 有效期）         |

##### 子流程：Desktop 注销

gRPC Logout，Desktop 用户主动注销时调用。

```
Desktop → gRPC Logout(desktop_id)
    │
    ├─ 验证 Node 存在且 type=desktop
    │
    ├─ 关闭该设备的心跳连接（从 connections map 移除并 Cancel）
    │
    ├─ 关闭该设备的数据流连接（从 dataStreams map 移除并 Cancel）
    │
    ├─ 清除数据库心跳时间（node.last_heartbeat → nil）
    │
    ├─ 注销 Logto 上游会话（尽力而为）
    │      通过 userID 反查 sessionID
    │      获取 SessionStorage
    │      调用 Logto SDK SignOut（撤销 token + 生成注销 URL）
    │      清理内存中的 Storage 和映射
    │
    └─ 返回 logout_url（Desktop 需要在 WebView 中访问以清除 Logto cookie）
```

注销不删除 Node 记录，也不删除 Headscale Node。设备下次登录时复用已有 Node。

Logto 会话注销失败时忽略错误（Server 重启后内存中的 SessionStorage 丢失，此时无法注销上游会话，不影响业务）。

##### 子流程：Desktop 重连认证

Desktop 保存了上次登录的凭证（desktop_id + secret），重启后无需重新走 Logto 登录。

```
Desktop → gRPC Authenticate(desktop_id, secret, system_info)
    │
    ├─ 查找 Node（按 desktop_id）
    ├─ 验证 type=desktop
    ├─ bcrypt 比对 secret 与 node.secret_hash
    │
    ├─ 更新 node：last_heartbeat, system_info, hostname
    │
    ├─ 获取 Headscale AuthKey（同登录流程）
    │
    └─ 返回 auth_key + server_url
```

重连认证不涉及 Logto，不创建新 Node，只更新现有 Node 信息并获取新的 AuthKey。

## 核心数据

### 数据库 User 表

| 字段         | 类型   | 说明                 |
| ------------ | ------ | -------------------- |
| ID           | uint64 | 自增主键             |
| HeadscaleUID | uint64 | Headscale User ID    |
| Name         | string | 唯一名称（不可修改） |
| Alias        | string | 显示名称             |
| Role         | string | agent / client       |
| SecretHash   | string | bcrypt 哈希密钥      |
| Enabled      | bool   | 启用状态             |
| Source       | string | 来源：manual / logto |

### Headscale User

| 属性 | 说明                                         |
| ---- | -------------------------------------------- |
| Name | {role}-{name}，如 agent-beijing              |
| ID   | Headscale 内部 ID，存储在数据库 HeadscaleUID |

Headscale User 是 Headscale Node 的容器。一个 Headscale User 下可以有多个 Node（如 Client 用户的多台 Desktop 设备）。

### ACL Tag

| Tag 格式               | 说明            |
| ---------------------- | --------------- |
| tag:agent-{name}       | Agent 身份标识  |
| tag:client-{name}      | Client 身份标识 |
| tag:group-{group_name} | 分组标识        |

Tag 由 ACL 同步服务（SyncAllNodeTags）写入 Headscale Node 的 ForcedTags。Tag 决定了 ACL 规则中的访问权限。

## 数据一致性

### 正常情况

创建用户时同步创建 Headscale User。删除用户时按子任务顺序执行：先删设备（含 Headscale Node），再清授权（含 ACL 同步），最后删 Headscale User 和数据库 User。数据库 HeadscaleUID 字段记录映射关系。

### 异常情况

| 场景                            | 处理方式                                                           |
| ------------------------------- | ------------------------------------------------------------------ |
| 创建 Headscale User 失败        | 数据库 User 仍然创建，HeadscaleUID=0，设备注册时会重试创建         |
| 删除 Headscale User 失败        | 忽略错误，数据库 User 仍然删除，Headscale 中残留的 User 不影响业务 |
| 数据库有 User 但 Headscale 没有 | 设备注册时自动创建 Headscale User                                  |
| Headscale 有 User 但数据库没有  | 不影响业务，可通过隧道管理页面手动清理                             |

### ACL 同步

ACL 同步服务（SyncACL）每 5 分钟全量同步一次，根据数据库中的 User、Group、Permission 生成完整的 ACL Policy 写入 Headscale。即使中间状态不一致，5 分钟内会自动修复。
