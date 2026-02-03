# Desktop 登录页设计

## 一、登录界面

### 1.1 状态1：首次登录

```
┌─────────────────────────────────────────────────────┐
│                                                     │
│                    [AWECloud Logo]                  │
│                                                     │
│                 Signaling Desktop                   │
│                      v0.2.3                         │
│                连接到您的远程服务                   │
│                                                     │
│  服务器地址    ┌─────────────────────────────────┐  │
│               │ signal.example.com             │  │
│               └─────────────────────────────────┘  │
│                                                     │
│  用户名       ┌─────────────────────────────────┐  │
│               │ 请输入用户名                    │  │
│               └─────────────────────────────────┘  │
│                                                     │
│               [✓] 记住登录                          │
│                                                     │
│               ┌─────────────────────────────────┐  │
│               │            登录                 │  │
│               └─────────────────────────────────┘  │
│                                                     │
└─────────────────────────────────────────────────────┘
```

**字段说明**：

| 字段       | 说明                                              |
| ---------- | ------------------------------------------------- |
| 服务器地址 | Server 的 gRPC 地址，用于后续认证和通信           |
| 用户名     | 用于 Logto login_hint（预填充）和本地记忆用户身份 |
| 记住登录   | 勾选后保存凭据，下次自动登录                      |
| 登录按钮   | 打开浏览器进行 Logto 授权，不是密码登录           |

**点击登录后的流程**：

```
1. Desktop 构造 Logto 授权 URL
   - 包含 login_hint=用户名（预填充）
   - redirect_uri=signaling://callback

2. 打开系统默认浏览器

3. 用户在浏览器完成 Logto 登录
   - 可能是钉钉扫码、密码登录等（由 Logto 决定）

4. 浏览器回调到 signaling://callback?code=xxx

5. Desktop 用 code 换取 id_token

6. Desktop 调用 Server gRPC LoginWithLogto

7. Server 验证 id_token，返回 desktop_id + secret

8. Desktop 保存凭据（如果勾选了"记住登录"）

9. 进入服务列表页面
```

### 1.2 状态2：自动登录（有保存凭据）

```
┌─────────────────────────────────────────────────────┐
│                                                     │
│                 [AWECloud Logo]                     │
│                   (旋转动画)                        │
│                                                     │
│                 Signaling Desktop                   │
│                      v0.2.3                         │
│                  正在自动登录...                    │
│                                                     │
│  服务器地址    ┌─────────────────────────────────┐  │
│               │ signal.example.com      (禁用) │  │
│               └─────────────────────────────────┘  │
│                                                     │
│  用户名       ┌─────────────────────────────────┐  │
│               │ admin                    (禁用) │  │
│               └─────────────────────────────────┘  │
│                                                     │
│               [✓] 记住登录                          │
│                                                     │
│               ┌─────────────────────────────────┐  │
│               │            登录                 │  │
│               └─────────────────────────────────┘  │
│                                                     │
│               ┌─────────────────────────────────┐  │
│               │      使用其他账号登录           │  │
│               └─────────────────────────────────┘  │
│                                                     │
└─────────────────────────────────────────────────────┘
```

**自动登录流程**：

```
1. Desktop 启动时检查本地凭据
   - 读取 server_address, username, desktop_id, secret

2. 显示自动登录界面
   - 服务器地址和用户名字段禁用
   - 显示"正在自动登录..."

3. 调用 Server gRPC Authenticate
   - 使用 desktop_id + secret 认证

4. 认证成功
   - 直接进入服务列表页面

5. 认证失败
   - 清除本地凭据
   - 显示首次登录界面
   - 提示"登录已过期，请重新登录"
```

**"使用其他账号登录"按钮**：

- 点击后清除当前凭据
- 返回首次登录界面
- 允许用户输入新的服务器地址和用户名

## 二、登录流程设计

### 2.1 完整流程图

```
┌─────────────────────────────────────────────────────────────────────────────────────────┐
│                              Desktop Logto 登录完整流程                                  │
├─────────────────────────────────────────────────────────────────────────────────────────┤
│                                                                                         │
│  ┌─────────┐    ┌─────────┐    ┌─────────┐    ┌─────────┐    ┌─────────┐              │
│  │ Desktop │    │  浏览器 │    │  Server │    │  Logto  │    │  gRPC   │              │
│  │  前端   │    │         │    │  HTTP   │    │         │    │  Stream │              │
│  └────┬────┘    └────┬────┘    └────┬────┘    └────┬────┘    └────┬────┘              │
│       │              │              │              │              │                     │
│       │ 1.点击登录   │              │              │              │                     │
│       │─────────────►│              │              │              │                     │
│       │              │              │              │              │                     │
│       │ 2.调用 gRPC LoginWithLogto │              │              │                     │
│       │──────────────────────────────────────────────────────────►│                     │
│       │  (username, device_info)   │              │              │                     │
│       │              │              │              │              │                     │
│       │              │              │  3.创建登录会话             │                     │
│       │              │              │  session_id = uuid()        │                     │
│       │              │              │  保存 gRPC stream           │                     │
│       │              │              │              │              │                     │
│       │ 4.返回登录 URL              │              │              │                     │
│       │◄──────────────────────────────────────────────────────────│                     │
│       │  login_url = https://signal.example.com/auth/desktop/{session_id}              │
│       │              │              │              │              │                     │
│       │ 5.打开浏览器 │              │              │              │                     │
│       │─────────────►│              │              │              │                     │
│       │              │              │              │              │                     │
│       │              │ 6.访问 Server│              │              │                     │
│       │              │─────────────►│              │              │                     │
│       │              │  GET /auth/desktop/{session_id}?login_hint=username              │
│       │              │              │              │              │                     │
│       │              │              │ 7.构造 Logto URL            │                     │
│       │              │              │  - redirect_uri = /auth/desktop/callback            │
│       │              │              │  - login_hint = username    │                     │
│       │              │              │  - state, code_challenge    │                     │
│       │              │              │              │              │                     │
│       │              │ 8.重定向到 Logto            │              │                     │
│       │              │◄─────────────│              │              │                     │
│       │              │  302 → Logto /oidc/auth    │              │                     │
│       │              │              │              │              │                     │
│       │              │ 9.访问 Logto │              │              │                     │
│       │              │─────────────────────────────►│              │                     │
│       │              │              │              │              │                     │
│       │              │ 10.展示登录页面             │              │                     │
│       │              │◄─────────────────────────────│              │                     │
│       │              │  (可能包含钉钉扫码选项)     │              │                     │
│       │              │              │              │              │                     │
│       │              │ 11.用户完成登录             │              │                     │
│       │              │─────────────────────────────►│              │                     │
│       │              │  (钉钉扫码/密码/其他 SSO)   │              │                     │
│       │              │              │              │              │                     │
│       │              │ 12.Logto 回调到 Server      │              │                     │
│       │              │◄─────────────────────────────│              │                     │
│       │              │  302 → /auth/desktop/callback?state={session_id}&code=xxx        │
│       │              │              │              │              │                     │
│       │              │ 13.访问回调  │              │              │                     │
│       │              │─────────────►│              │              │                     │
│       │              │  GET /auth/desktop/callback?state={session_id}&code=xxx          │
│       │              │              │              │              │                     │
│       │              │              │ 14.用 code 换 token         │                     │
│       │              │              │─────────────►│              │                     │
│       │              │              │              │              │                     │
│       │              │              │ 15.返回 id_token            │                     │
│       │              │              │◄─────────────│              │                     │
│       │              │              │              │              │                     │
│       │              │              │ 16.验证 id_token            │                     │
│       │              │              │  提取用户信息 (phone/email) │                     │
│       │              │              │  查询/创建 User             │                     │
│       │              │              │  创建 Node (Desktop)        │                     │
│       │              │              │  生成 desktop_secret        │                     │
│       │              │              │  创建 Tailscale AuthKey     │                     │
│       │              │              │              │              │                     │
│       │              │              │ 17.通过 gRPC Stream 推送    │                     │
│       │              │              │──────────────────────────────►                     │
│       │              │              │  LoginResult {              │                     │
│       │              │              │    success: true,           │                     │
│       │              │              │    desktop_id, secret,      │                     │
│       │              │              │    auth_key, services       │                     │
│       │              │              │  }                          │                     │
│       │              │              │              │              │                     │
│       │ 18.收到登录成功             │              │              │                     │
│       │◄──────────────────────────────────────────────────────────│                     │
│       │              │              │              │              │                     │
│       │              │ 19.显示成功页面             │              │                     │
│       │              │◄─────────────│              │              │                     │
│       │              │  "登录成功，可关闭此页面"   │              │                     │
│       │              │              │              │              │                     │
│       │ 20.保存凭据  │              │              │              │                     │
│       │  21.初始化 Tailscale        │              │              │                     │
│       │  22.进入服务列表            │              │              │                     │
│       │              │              │              │              │                     │
└─────────────────────────────────────────────────────────────────────────────────────────┘
```

### 2.2 关键步骤说明

#### 步骤 1-2：Desktop 发起登录

Desktop 前端调用 Go 后端方法 `LoginWithLogto`，传入：

- `server_address`: Server 的 HTTP 地址
- `username`: 用户输入的用户名（用于 login_hint）
- `device_info`: 设备信息（名称、指纹、系统信息）

#### 步骤 3：Server 创建登录会话

Server 收到 gRPC 请求后：

- 生成唯一的 `session_id`（UUID）
- 创建登录会话记录，保存到数据库
- 保存 gRPC stream 引用，用于后续推送结果
- 设置会话超时时间（如 10 分钟）

#### 步骤 4-5：返回登录 URL 并打开浏览器

Server 返回登录 URL：

```
https://signal.example.com/auth/desktop/{session_id}?login_hint=admin
```

Desktop 调用系统 API 打开默认浏览器。

#### 步骤 6-8：Server 重定向到 Logto

Server HTTP 处理器 `/auth/desktop/{session_id}` 接收请求后：

- 验证 `session_id` 是否存在且未过期
- 生成 PKCE 参数（`code_verifier`, `code_challenge`）
- 保存 `code_verifier` 到会话中
- 构造 Logto 授权 URL：

  ```
  https://logto.example.com/oidc/auth?
    response_type=code
    &client_id=signaling-desktop
    &redirect_uri=https://signal.example.com/auth/desktop/callback/{session_id}
    &scope=openid profile email phone
    &state={session_id}
    &code_challenge={challenge}
    &code_challenge_method=S256
    &login_hint={username}
  ```

- 302 重定向到 Logto

#### 步骤 9-11：用户在 Logto 完成登录

用户在浏览器看到 Logto 登录页面，可能包含：

- 用户名密码登录（`login_hint` 预填充用户名）
- 钉钉扫码登录（如果 Logto 配置了钉钉 Connector）
- 其他 SSO 选项（GitHub、Google 等）

用户选择任意方式完成登录。

#### 步骤 12-13：Logto 回调到 Server

Logto 验证成功后，重定向到：
```
https://signal.wodcloud.com/auth/desktop/callback?state={session_id}&code=xxx
```

注意：`session_id` 通过 `state` 参数传递，不在 URL 路径中。

#### 步骤 14-16：Server 换取 Token 并验证

Server 回调处理器 `/auth/desktop/callback` 执行：

1. 从 `state` 参数中提取 `session_id`
2. 验证 `session_id` 对应的会话存在且未过期
3. 从会话中读取 `code_verifier`
4. 调用 Logto Token 端点换取 Token
5. 获取 `id_token` 和 `access_token`
6. 验证 `id_token` 签名和声明
7. 从 `id_token` 提取用户信息（`phone`, `email`, `name` 等）
8. 根据 `phone` 或 `email` 查询 User 表
   - 找到 → 更新用户信息
   - 未找到 → 返回错误"用户未授权"
9. 创建或更新 Node（Desktop 设备）
10. 生成 `desktop_secret`
11. 调用 Headscale API 创建 PreAuthKey

#### 步骤 17：通过 gRPC Stream 推送结果

Server 通过之前保存的 gRPC stream 推送登录结果。

#### 步骤 18-19：Desktop 收到结果，浏览器显示成功页面

Desktop 的 gRPC 客户端收到推送后：

- 关闭等待状态
- 保存凭据到本地（如果勾选了"记住登录"）

同时，浏览器显示成功页面（Server 返回 HTML）：

```
┌─────────────────────────────────────────────────────┐
│                                                     │
│                       ✓                             │
│                   (绿色大勾)                        │
│                                                     │
│                   登录成功                          │
│                                                     │
│        您可以关闭此页面，返回 Desktop 客户端        │
│                                                     │
└─────────────────────────────────────────────────────┘
```

#### 步骤 20-22：Desktop 完成初始化

Desktop 执行：

1. 保存凭据到系统密钥存储
2. 使用 `auth_key` 初始化 Tailscale 连接
3. 跳转到服务列表页面

### 2.3 关键 API 端点

| 端点                                     | 方法 | 说明                                    |
| ---------------------------------------- | ---- | --------------------------------------- |
| `/auth/desktop/{session_id}`             | GET  | 发起登录，重定向到 Logto                |
| `/auth/desktop/callback`                 | GET  | Logto 回调，从 state 参数获取 session_id |
| `DesktopService.LoginWithLogto` (gRPC)   | RPC  | 创建登录会话，返回 URL                  |
| `DesktopService.LoginWithLogto` (Stream) | RPC  | 推送登录结果                            |

### 2.4 登录会话数据模型

| 字段               | 类型     | 说明                          |
| ------------------ | -------- | ----------------------------- |
| session_id         | string   | 会话唯一标识（UUID）          |
| username           | string   | 用户名（login_hint）          |
| device_name        | string   | 设备名称                      |
| device_fingerprint | string   | 设备指纹                      |
| code_verifier      | string   | PKCE verifier                 |
| state              | string   | OIDC state（等于 session_id） |
| grpc_stream        | Stream   | gRPC stream 引用（内存）      |
| status             | string   | pending / success / failed    |
| created_at         | datetime | 创建时间                      |
| expires_at         | datetime | 过期时间（10 分钟）           |

### 2.5 错误处理

| 错误场景             | 处理方式                                       |
| -------------------- | ---------------------------------------------- |
| session_id 不存在    | 返回 404，浏览器显示"会话不存在"               |
| session_id 已过期    | 返回 410，浏览器显示"会话已过期，请重新登录"   |
| Logto Token 换取失败 | 返回 500，浏览器显示"认证失败"                 |
| id_token 验证失败    | 返回 401，浏览器显示"身份验证失败"             |
| 用户未授权           | 返回 403，浏览器显示"用户未授权，请联系管理员" |
| gRPC stream 已断开   | 记录日志，浏览器仍显示成功（用户可手动重试）   |
| 浏览器未打开         | Desktop 显示错误，提供手动复制链接选项         |

### 2.6 安全考虑

1. **PKCE 防护**：使用 PKCE 防止授权码拦截攻击
2. **State 验证**：验证 state 参数防止 CSRF
3. **会话超时**：登录会话 10 分钟后自动过期
4. **一次性使用**：每个 session_id 只能使用一次
5. **HTTPS 强制**：所有 HTTP 端点必须使用 HTTPS
6. **Token 验证**：严格验证 id_token 的签名和声明

### 2.7 配置要求

#### Server 配置

```toml
[logto]
endpoint = "https://logto.example.com"
client_id = "signaling-desktop"
client_secret = "your-client-secret"
redirect_uri_base = "https://signal.example.com/auth/desktop/callback"

[server]
http_addr = "https://signal.example.com"
session_timeout = "10m"
```

#### Logto 应用配置

- 应用类型：Traditional Web Application
- Redirect URIs：`https://signal.example.com/auth/desktop/callback/*`
- Post sign-out redirect URIs：（可选）
- Allowed CORS origins：（不需要，因为是服务端流程）
