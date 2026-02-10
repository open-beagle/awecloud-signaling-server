# Desktop 登录流程设计

## 概述

Desktop 客户端的登录流程分为两种：

1. **首次登录**：使用 Client 凭证（用户名/密码）
2. **Logto 登录**：通过 Server 的 Logto 集成进行 SSO 登录

## 核心原则

- **Desktop 不负责 Logto 交互**：所有 Logto 相关的逻辑都在 Server 端处理
- **Desktop 只负责 UI 展示**：打开 WebView 窗口显示 Server 的登录页面
- **Server 是中心**：Server 生成登录 URL、处理 Logto 回调、返回认证结果

## 登录流程

### 1. 首次登录流程（Client 凭证）

```
┌─────────────┐
│   Desktop   │
└──────┬──────┘
       │ 1. 输入 Server 地址、用户名、密码
       │
       ▼
┌─────────────────────────────────────┐
│  gRPC: Login(clientName, secret)    │
│  - 设备指纹验证                      │
│  - 返回 DesktopID + Secret           │
└──────┬──────────────────────────────┘
       │
       ▼
┌─────────────────────────────────────┐
│  初始化 Tailscale 隧道              │
│  - 使用返回的 AuthKey               │
│  - 连接到 Headscale                 │
└──────┬──────────────────────────────┘
       │
       ▼
┌─────────────────────────────────────┐
│  登录成功，进入主界面               │
└─────────────────────────────────────┘
```

### 2. Logto 登录流程（SSO）

```
┌─────────────┐
│   Desktop   │
└──────┬──────┘
       │ 1. 输入 Server 地址、用户名提示（可选）
       │
       ▼
┌──────────────────────────────────────────┐
│  REST API: GET /api/auth/desktop/login   │
│  - 返回登录页面 URL                      │
│  - Server 生成会话 ID                    │
└──────┬───────────────────────────────────┘
       │
       ▼
┌──────────────────────────────────────────┐
│  打开 WebView 窗口                       │
│  - 加载登录页面 URL                      │
│  - 用户在 Server 页面中完成 Logto 认证  │
└──────┬───────────────────────────────────┘
       │
       ▼
┌──────────────────────────────────────────┐
│  Server 处理 Logto 回调                  │
│  - 验证用户身份                          │
│  - 创建 Desktop 账户                     │
│  - 生成 DesktopID + Secret               │
└──────┬───────────────────────────────────┘
       │
       ▼
┌──────────────────────────────────────────┐
│  Server 重定向到成功页面                 │
│  - 显示"登录成功"提示                    │
│  - Desktop 检测到登录完成                │
└──────┬───────────────────────────────────┘
       │
       ▼
┌──────────────────────────────────────────┐
│  Desktop 获取登录结果                    │
│  - 调用 REST API 获取 DesktopID + Secret │
│  - 保存到本地配置                        │
└──────┬───────────────────────────────────┘
       │
       ▼
┌──────────────────────────────────────────┐
│  初始化 Tailscale 隧道                   │
│  - 使用返回的 AuthKey                    │
│  - 连接到 Headscale                      │
└──────┬───────────────────────────────────┘
       │
       ▼
┌──────────────────────────────────────────┐
│  登录成功，进入主界面                    │
└──────────────────────────────────────────┘
```

## API 设计

### 获取登录 URL

**请求**：

```
GET /api/auth/desktop/login?username_hint=xxx
```

**响应**：

```json
{
  "login_url": "https://server.com/auth/desktop/login-page?session_id=xxx",
  "message": "success"
}
```

**说明**：

- Server 生成会话 ID
- 返回登录页面 URL（Server 的登录页面，不是直接的 Logto URL）
- Desktop 在 WebView 中打开此 URL

### 获取登录结果

**请求**：

```
GET /api/auth/desktop/login-result?session_id=xxx
```

**响应**：

```json
{
  "success": true,
  "desktop_id": 12345,
  "secret": "xxx",
  "auth_key": "xxx",
  "server_url": "https://headscale.com",
  "username": "user@example.com",
  "message": "success"
}
```

**说明**：

- Desktop 在登录完成后调用此 API
- 获取 DesktopID、Secret 等认证信息
- 用于初始化 Tailscale 隧道

## 自动登录

当 Desktop 启动时，如果本地有保存的凭证（DesktopID + Secret），则：

1. 调用 gRPC `Authenticate()` 方法进行认证
2. 如果认证成功，自动初始化 Tailscale 隧道
3. 进入主界面

## 注销

注销时需要：

1. 停止 Tailscale 隧道连接
2. 清除本地保存的所有凭证（DesktopID、Secret、Token 等）
3. 返回登录界面

## 凭证存储

Desktop 本地存储的凭证包括：

- `ServerAddress`：Server 地址
- `ClientID`：用户名（用于显示和自动填充）
- `DeviceToken`：格式为 `{DesktopID}:{Secret}`
- `RememberMe`：是否记住登录状态

**安全考虑**：

- 凭证存储在本地配置文件中（通常在用户主目录）
- 建议使用操作系统的密钥管理服务（如 Windows Credential Manager、macOS Keychain）
- 不应在日志中输出完整的 Secret

## 错误处理

### 登录失败场景

1. **网络错误**：无法连接到 Server
   - 显示错误提示，允许重试

2. **认证失败**：用户名/密码错误
   - Server 返回 401 错误
   - 显示错误提示，允许重新输入

3. **Logto 错误**：Logto 认证失败
   - Server 处理 Logto 错误
   - 显示错误页面，允许重试

4. **会话过期**：登录页面打开后超过 5 分钟未完成
   - Server 清理过期会话
   - Desktop 显示超时提示

### 隧道连接失败

如果 Tailscale 隧道连接失败：

1. 显示警告提示
2. 允许用户手动重连
3. 不影响登录状态（用户仍然可以访问 Web 管理界面）

## 证书处理

对于自签名证书：

- **Linux**：通过环境变量 `WEBKIT_IGNORE_TLS_ERRORS=1` 跳过验证
- **Windows/macOS**：提示用户安装证书到系统信任列表

详见 `desktop/docs/certificate-guide.md`
