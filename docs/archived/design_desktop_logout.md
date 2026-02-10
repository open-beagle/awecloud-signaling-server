# Desktop 注销流程设计

## 问题现状

当前注销存在两个核心问题：

1. Desktop 注销后，Server 端只断开了 gRPC 连接和清除心跳，但没有注销 Logto 会话
2. 用户注销后重新登录时，Logto 浏览器 cookie 仍然有效，直接跳过登录页面弹出"登录成功"

根本原因：Server 端 Logout 方法只做了连接清理，没有撤销 Logto token，也没有清除 Logto 浏览器会话。

## 尝试过的方案

### 方案一：隐藏 WebView 打开 Logto 注销 URL（已废弃）

思路：注销时用隐藏 WebView 访问 Logto 的 end_session_endpoint 清除 cookie。

问题：新创建的 WebView 窗口虽然与登录窗口共享 WebView2 用户数据目录，但 Logto 的 end_session_endpoint 只接受 client_id 参数（无 id_token_hint），实际效果不稳定。

### 方案二：删除 WebView2 用户数据目录（已废弃）

思路：注销时删除 WebView2 的用户数据目录（cookie、缓存等）。

问题：应用运行时 WebView2 进程锁定了数据目录中的文件，无法删除。需要先关闭所有 WebView 窗口，用户体验差。

### 方案三：prompt=login 强制重新登录（当前方案）

思路：不清除 cookie，而是在生成 Logto 登录 URL 时加上 OIDC 标准参数 prompt=login，强制 Logto 每次都显示登录页面。

优点：

- 不需要清除 cookie，不需要额外的 WebView 操作
- 利用 OIDC 标准协议，Logto 原生支持
- 实现简单，只需修改 Server 端一行配置
- 用户体验好，注销后重新登录一定会看到登录页面

## 最终注销流程

```
Desktop                    Server                     Logto
  │                          │                          │
  │── gRPC Logout ──────────▶│                          │
  │                          │── 关闭心跳连接            │
  │                          │── 关闭数据流连接          │
  │                          │── 清除心跳时间            │
  │                          │── 撤销 refresh token ────▶│── token 失效
  │◀── 注销成功 ─────────────│                          │
  │                          │                          │
  │── 断开 Tailscale 隧道    │                          │
  │── 停止 gRPC 客户端       │                          │
  │── 清除本地配置           │                          │
  │   (保留 ServerAddress)   │                          │
  │                          │                          │
  │ 重新登录时：              │                          │
  │── CreateLoginSession ───▶│── 创建会话和 Storage      │
  │◀── 返回 sessionID ──────│   (不生成 Logto URL)      │
  │── 打开 WebView ─────────▶│                          │
  │   /auth/desktop/{id}     │── 生成 Logto URL         │
  │                          │   (prompt=login)          │
  │                          │── 重定向到 Logto ────────▶│
  │                          │                          │── prompt=login
  │                          │                          │── 忽略已有 cookie
  │                          │                          │── 强制显示登录页面
```

## 改动范围

### Server 端

1. LogtoClient.GetSignInURL 添加 Prompt: "login" 参数
2. DesktopLoginService 新增 BindUserSession / LogoutSession 方法
3. gRPC Logout 方法增强，撤销 Logto refresh token
4. 登录回调成功时绑定 userID → sessionID 映射
5. CreateLoginSession 不再生成 Logto URL，只创建会话和 Storage
6. DesktopLoginRedirect 中统一生成 Logto URL（避免重复调用覆盖 state）
7. DesktopLoginRedirect 传入正确的 loginHint（用户名提示而非 sessionID）

### Desktop 端

1. App.Logout 调用 gRPC 注销后做本地清理（保留 ServerAddress）
2. 移除 openLogoutWindow 方法（不再需要）
3. Layout.vue 中 Logout 调用改为 async/await，确保配置清除后再跳转

### Proto 变更

DesktopLogoutResponse 新增 logout_url 字段（field 3，保留但 Desktop 不再使用）

## 关键设计决策

### 为什么用 prompt=login 而不是清除 cookie

WebView2 的 cookie 在应用运行时被进程锁定，无法通过删除文件清除。
Wails v3 没有暴露 WebView2 的 CookieManager API，无法通过代码清除。
prompt=login 是 OIDC 标准参数，Logto 原生支持，效果等同于"忽略已有会话"。

### 为什么 Logto URL 只在 DesktopLoginRedirect 中生成

Logto SDK 的 SignIn 方法会在 Storage 中保存 state 和 codeVerifier。
如果 CreateLoginSession 和 DesktopLoginRedirect 各调用一次，第二次会覆盖第一次的 state，
导致回调时 state 不匹配或 codeVerifier 错误。
因此 CreateLoginSession 只创建 Storage，URL 在 WebView 实际访问时才生成。

### 容错

- Server 重启后内存中的 SessionStorage 丢失：跳过 Logto token 撤销，仅做连接清理
- Logto 不可达：撤销 token 失败记录日志，不影响注销流程
- gRPC 注销超时（3秒）：跳过，继续本地清理

### Logto 配置要求

需要在 Logto 管理后台的应用设置中，添加"退出登录后重定向 URI"（使用 callback_url 即可）。
