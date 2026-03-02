# Desktop 用户禁用体验设计

相关文档：

- `design_ztna_server_user_disable.md` — Server 用户禁用设计（禁用业务逻辑）
- `design_ztna_desktop_host.md` — Desktop.Host 架构设计（客户端整体架构）

## 概述

本文档描述 Desktop 客户端如何感知用户被禁用，以及如何设计用户体验，确保用户能够清晰地理解禁用状态并获得适当的引导。

## 禁用感知机制

Desktop 客户端通过以下方式感知用户被禁用：

### 1. 认证阶段感知

#### 场景 1：Logto 新设备登录

```
用户操作
    │
    ├─ 点击"登录"按钮
    │
    ├─ 浏览器打开 Logto 登录页面
    │
    ├─ 完成 Logto 认证
    │
    ├─ Server 回调处理
    │      └─ 检查 user.Enabled = false
    │
    └─ Desktop 收到登录结果
           ├─ Success = false
           ├─ IsDisabled = true
           └─ Message = "用户已禁用，请联系管理员"
```

用户体验：

- 登录页面显示错误提示："您的账号已被禁用"
- 不保存任何凭证，不进入主界面
- 提供"退出"按钮，关闭应用

#### 场景 2：已有设备重连认证

```
Desktop 启动
    │
    ├─ 读取本地保存的凭证（desktopID + secret）
    │
    ├─ 调用 gRPC Authenticate
    │      └─ Server 检查 user.Enabled = false
    │
    └─ 收到认证响应
           ├─ Success = false
           └─ Message = "用户已禁用"
```

用户体验：

- 主界面显示"连接失败"状态
- 弹出提示："您的账号已被禁用"
- 提供"退出"按钮，关闭应用

### 2. 心跳阶段感知

#### 场景 3：已连接设备被禁用

```
Desktop 正常运行
    │
    ├─ 管理员禁用用户
    │      ├─ Server 断开 gRPC 连接
    │      └─ 心跳流返回错误
    │
    ├─ Desktop 检测到心跳错误
    │      └─ 触发重连机制
    │
    ├─ 调用 gRPC Authenticate 重连
    │      └─ Server 检查 user.Enabled = false
    │
    └─ 收到认证失败
           └─ 触发 onReconnectNeeded 回调
```

用户体验：

- 主界面状态从"已连接"变为"连接中断"
- 自动尝试重连（后台进行，不打扰用户）
- 重连失败后，弹出提示："您的账号已被禁用"
- 提供"退出"按钮，关闭应用

#### 场景 4：心跳检查发现禁用

```
Desktop 正常运行
    │
    ├─ 管理员禁用用户（但未立即断开连接）
    │
    ├─ Desktop 发送心跳（30秒一次）
    │      └─ Server 检查 user.Enabled = false
    │
    ├─ Server 返回错误并断开连接
    │      └─ 错误码：PermissionDenied
    │      └─ 错误消息："用户已禁用"
    │
    └─ Desktop 检测到心跳错误
           └─ 触发重连机制（同场景 3）
```

用户体验：（同场景 3）

## 错误消息设计

### 认证失败消息

| 场景         | 错误消息                         | 用户操作建议         |
| ------------ | -------------------------------- | -------------------- |
| 新设备登录   | "您的账号已被禁用"               | 退出应用             |
| 已有设备重连 | "您的账号已被禁用"               | 退出应用             |
| 心跳检查失败 | "您的账号已被禁用"               | 退出应用             |

### 错误码设计

建议在 gRPC 响应中使用标准错误码：

| 错误场景   | gRPC 状态码      | 错误消息       |
| ---------- | ---------------- | -------------- |
| 用户禁用   | PermissionDenied | "用户已禁用"   |
| 凭证无效   | Unauthenticated  | "认证失败"     |
| 设备不存在 | NotFound         | "设备不存在"   |
| 网络错误   | Unavailable      | "网络连接失败" |

## 重连策略

### 当前实现

Desktop 客户端的重连策略：

1. 心跳错误检测：
   - 心跳接收失败时，立即触发重连
   - 使用指数退避：5秒 → 10秒 → 20秒 → ... → 最大2分钟

2. 重连流程：
   - 调用 `reconnect()` 方法
   - 使用本地保存的凭证重新认证
   - 认证成功：重新建立心跳流
   - 认证失败：触发 `onReconnectNeeded` 回调

3. 回调处理：
   - App 层收到回调后，清除本地凭证
   - 显示错误提示
   - 引导用户重新登录

### 优化建议

#### 问题 1：无限重连

当前实现中，即使用户被禁用，Desktop 仍会不断尝试重连，浪费资源。

解决方案：

在 `reconnect()` 方法中，检查认证失败的原因：

```
reconnect() 方法
    │
    ├─ 调用 Authenticate
    │
    ├─ 检查错误类型
    │      ├─ PermissionDenied（用户禁用）
    │      │      └─ 停止重连，触发回调
    │      │
    │      ├─ Unauthenticated（凭证无效）
    │      │      └─ 停止重连，触发回调
    │      │
    │      └─ Unavailable（网络错误）
    │             └─ 继续重连（指数退避）
    │
    └─ 触发 onReconnectNeeded 回调
```

#### 问题 2：错误消息不明确

当前实现中，所有认证失败都显示相同的错误消息，用户无法区分是禁用还是凭证过期。

解决方案：

在 `onReconnectNeeded` 回调中传递错误类型：

```
type ReconnectReason int

const (
    ReconnectReasonUnknown ReconnectReason = iota
    ReconnectReasonDisabled    // 用户禁用
    ReconnectReasonInvalidCred // 凭证无效
    ReconnectReasonNetworkError // 网络错误
)

onReconnectNeeded func(reason ReconnectReason, message string) error
```

App 层根据错误类型显示不同的提示：

- `ReconnectReasonDisabled`：显示"账号已被禁用"
- `ReconnectReasonInvalidCred`：显示"凭证已过期，请重新登录"
- `ReconnectReasonNetworkError`：显示"网络连接失败，请检查网络"

## UI 状态设计

### 连接状态指示

Desktop 主界面应显示清晰的连接状态：

| 状态       | 图标 | 颜色 | 说明                 |
| ---------- | ---- | ---- | -------------------- |
| 已连接     | ✓    | 绿色 | gRPC 和隧道都已连接  |
| 连接中     | ⟳    | 黄色 | 正在建立连接         |
| 连接中断   | ⚠    | 橙色 | 连接断开，正在重连   |
| 账号已禁用 | ✗    | 红色 | 用户被禁用，无法连接 |
| 未登录     | ○    | 灰色 | 未认证               |

### 错误提示设计

#### 轻量级提示（Toast）

用于临时性错误（网络波动、短暂断连）：

- 自动消失，不打断用户操作
- 示例："连接中断，正在重连..."

#### 模态对话框（Modal）

用于需要用户操作的错误（账号禁用、凭证过期）：

- 必须用户确认才能关闭
- 提供明确的操作按钮
- 示例：

```
┌─────────────────────────────────┐
│  账号已被禁用                   │
│                                 │
│  您的账号已被禁用，             │
│  无法继续使用。                 │
│                                 │
│            [退出]               │
└─────────────────────────────────┘
```

#### 状态栏提示

用于持续性状态（账号禁用、未登录）：

- 始终显示在界面底部或顶部
- 提供快速操作入口
- 示例："账号已禁用"

## 日志记录

Desktop 客户端应记录详细的禁用相关日志，便于问题排查：

```
[DesktopClient] Heartbeat receive error: rpc error: code = PermissionDenied desc = 用户已禁用
[DesktopClient] Re-authenticating: desktopID=123
[DesktopClient] Re-authenticate failed: rpc error: code = PermissionDenied desc = 用户已禁用
[DesktopClient] User disabled, stopping reconnect attempts
[App] Login failed: 用户已禁用 (isDisabled=true)
[App] Showing disabled user dialog
```

## 测试场景

### 场景 1：新设备登录时用户已禁用

1. 管理员禁用用户
2. 用户在新设备上点击"登录"
3. 完成 Logto 认证
4. 验证：显示"您的账号已被禁用"
5. 验证：不保存凭证，不进入主界面
6. 验证：点击"退出"按钮，应用关闭

### 场景 2：已连接设备被禁用

1. Desktop 设备正常连接
2. 管理员禁用用户
3. 验证：Desktop 在 30 秒内检测到连接中断
4. 验证：自动尝试重连
5. 验证：重连失败后显示"账号已被禁用"
6. 验证：停止重连尝试
7. 验证：点击"退出"按钮，应用关闭

### 场景 3：已有设备重启后重连

1. 管理员禁用用户
2. Desktop 设备重启
3. Desktop 使用本地凭证尝试认证
4. 验证：认证失败，显示"账号已被禁用"
5. 验证：提供"退出"按钮
6. 验证：点击"退出"按钮，应用关闭

### 场景 4：禁用后重新启用

1. 管理员禁用用户
2. Desktop 显示"账号已被禁用"，用户退出应用
3. 管理员重新启用用户
4. 用户重新打开应用并登录
5. 验证：登录成功，正常使用

## 实施要点

### 代码修改位置

1. 重连逻辑优化（`desktop/internal/client/client.go`）
   - 在 `reconnect()` 方法中检查错误类型
   - 对 PermissionDenied 和 Unauthenticated 停止重连
   - 对 Unavailable 继续重连

2. 回调接口优化（`desktop/internal/client/client.go`）
   - 修改 `onReconnectNeeded` 回调签名，传递错误类型和消息
   - 定义 `ReconnectReason` 枚举类型

3. UI 错误提示（`desktop/app.go`）
   - 根据错误类型显示不同的提示消息
   - 实现模态对话框和状态栏提示

4. 日志记录（`desktop/internal/client/client.go`）
   - 记录禁用相关的详细日志
   - 包含错误码、错误消息、desktopID 等信息

### 优先级

1. 高优先级：停止无限重连（避免资源浪费）
2. 中优先级：优化错误消息（提升用户体验）
3. 低优先级：UI 美化（状态图标、颜色等）
