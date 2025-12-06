# 变更记录

## 2025-12-06: 下载页面功能

### 功能概述

在 Server 端新增免登录的下载页面，用户可以在不登录的情况下查看和下载客户端。登录页的"下载客户端"按钮改为跳转到下载页面，而不是直接下载文件。

### Server 端变更

#### 1. API 变更

新增公开 API 接口（无需认证）：

1. **GET /api/v1/public/download/desktop** - 获取桌面客户端下载信息

   - 无需认证
   - 返回：`{ success: bool, version: string, downloads: []DownloadItem, changelog_url: string }`
   - 自动扫描 `bin` 目录下的客户端文件

2. **GET /api/v1/public/download/desktop/direct** - 直接重定向到下载文件

   - 无需认证
   - 参数：`platform` (windows/linux/darwin), `arch` (amd64/arm64)
   - 重定向到对应的下载文件

3. **GET /api/v1/public/download/desktop/versions** - 列出所有可用版本
   - 无需认证
   - 目前返回当前版本，未来可扩展支持多版本

**新增文件**：

- `internal/server/api/download.go` - 下载 API 实现

**修改文件**：

- `internal/server/server.go` - 添加静态文件服务 `/downloads` 路由

#### 2. 静态文件服务

- 新增 `/downloads` 路由，映射到 `./bin` 目录
- 支持直接下载客户端文件

### Web 端变更

#### 1. 新增下载页面

**新增文件**：

- `web/src/views/Download.vue` - 下载页面组件
- `web/src/api/download.ts` - 下载 API 调用

**修改文件**：

- `web/src/router/index.ts` - 添加 `/download` 路由（公开访问）

**页面功能**：

- 显示所有可用平台的下载选项（Windows、Linux、macOS）
- 显示版本信息和文件大小
- 提供返回登录页的链接
- 提供查看更新日志的链接
- 响应式设计，支持移动端

#### 2. 登录页面变更

**修改文件**：

- `web/src/views/Login.vue` - 修改下载按钮行为

**变更内容**：

- "下载客户端"按钮改为跳转到 `/download` 页面
- 移除直接下载逻辑
- 移除系统配置中的 `client_download_url` 字段依赖
- 下载按钮始终显示（不再依赖配置）

#### 3. 登录后下载入口变更

**修改文件**：

- `web/src/components/Layout/Header.vue` - 修改头部"客户端"按钮行为
- `web/src/views/System/Config.vue` - 修改系统配置页面

**变更内容**：

- 头部"客户端"按钮改为跳转到 `/download` 页面（新窗口打开）
- 按钮始终显示（不再依赖系统配置）
- 移除 `client_download_url` 配置项的加载逻辑
- 系统配置页面改为显示"前往下载页面"按钮和下载页面 URL

### 实现细节

1. **下载地址配置**：

   - 管理员在系统配置中设置"客户端下载地址"
   - 支持目录 URL（如 `https://cdn.example.com/downloads/`）
   - API 自动拼接文件名生成完整下载链接

2. **操作系统检测**：

   - 后端：从 User-Agent 或 `os` 查询参数检测
   - 前端：使用 `navigator.userAgent` 检测用户系统
   - 自动推荐适合用户系统的客户端

3. **文件命名规范**：

   - Windows: `awecloud-signaling-v0.1.0-windows-amd64.exe`
   - Linux: `awecloud-signaling-v0.1.0-linux-amd64`
   - macOS: `awecloud-signaling-v0.1.0-darwin-universal.zip`

4. **用户体验**：
   - 自动检测用户系统并标记"推荐"
   - 推荐的系统排在第一位
   - 显示文件名和架构信息
   - 一键下载，无需登录

### 优势

1. **更好的用户体验**：

   - 用户可以在不登录的情况下下载客户端
   - 可以查看所有平台的下载选项
   - 显示版本信息和文件大小

2. **便于维护**：

   - 自动扫描文件，无需手动配置
   - 支持多平台、多架构
   - 便于后续添加版本管理功能

3. **安全性**：
   - 公开访问，但有路径保护
   - 不暴露服务器敏感信息

---

## 2025-12-04: 服务收藏功能

### 功能概述

在 Desktop 应用中新增服务收藏功能，用户可以收藏常用的服务，收藏状态保存在服务器端，支持多设备同步。同时新增一键连接所有收藏服务的功能，提升用户体验。

### Server 端变更

#### 1. 数据库变更

新增 `service_favorites` 表：

```sql
CREATE TABLE service_favorites (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    client_id INTEGER NOT NULL,
    stcp_instance_id INTEGER NOT NULL,
    created_at DATETIME,
    updated_at DATETIME,
    FOREIGN KEY (client_id) REFERENCES clients(id) ON DELETE CASCADE,
    FOREIGN KEY (stcp_instance_id) REFERENCES stcp_instances(id) ON DELETE CASCADE,
    UNIQUE(client_id, stcp_instance_id)
);
```

**新增文件**：

- `internal/server/model/service_favorite.go` - 收藏数据模型
- `internal/server/api/service_favorite.go` - 收藏 API 实现

**修改文件**：

- `internal/server/db/db.go` - 添加表迁移
- `internal/server/server.go` - 注册 API 路由

#### 2. API 变更

新增两个 API 接口：

1. **GET /api/v1/client/favorites** - 获取用户的收藏列表

   - 需要 JWT 认证
   - 返回：`{ success: bool, favorites: []int64 }`

2. **POST /api/v1/client/favorites/toggle** - 切换收藏状态
   - 需要 JWT 认证
   - 请求：`{ stcp_instance_id: int64 }`
   - 返回：`{ success: bool, is_favorite: bool, message: string }`

### Desktop 端变更

#### 1. 后端变更

**新增文件**：

- `desktop/internal/client/favorite.go` - 收藏 API 客户端

**修改文件**：

- `desktop/internal/client/client.go` - 添加收藏客户端字段和方法
- `desktop/internal/client/auth.go` - 初始化收藏客户端
- `desktop/internal/models/service.go` - 添加 `is_favorite` 字段
- `desktop/app.go` - 添加 `ToggleFavorite` 方法，在 `GetServices` 中获取收藏状态

#### 2. 前端变更

**修改文件**：

- `desktop/frontend/src/stores/services.ts` - 简化收藏逻辑，移除 localStorage
- `desktop/frontend/src/components/ServiceCard.vue` - 添加收藏图标和交互
- `desktop/frontend/src/views/Services.vue` - 更新筛选逻辑

**功能特性**：

- 服务卡片右上角显示收藏图标（⭐）
- 点击图标切换收藏状态
- 筛选标签从"共 X 个已连接"改为"共 X 个收藏"
- 支持按收藏状态筛选服务
- 收藏状态保存在服务器，支持多设备同步
- **智能默认筛选**：
  - 有收藏时：默认只显示收藏的服务
  - 无收藏但有在线服务：默认显示在线服务
  - 无收藏且无在线服务：默认显示离线服务
- **新增**：一键连接/断开收藏服务按钮（⚡ 图标）
  - **智能切换**：根据当前状态自动切换功能
    - 默认状态（有未连接的收藏）：黄色按钮，点击连接所有断开的收藏
    - 已连接状态（所有收藏都已连接）：红色按钮，点击断开所有已连接的收藏
  - 依次连接/断开，避免并发过多
  - 显示确认对话框，避免误操作
  - 显示操作进度和结果统计
  - 操作失败时显示详细信息

### 实现细节

1. **数据流程**：

   - Desktop 启动 → 登录 → 获取服务列表 → 服务列表包含收藏状态
   - 用户点击收藏 → 调用后端 API → 更新服务器数据 → 乐观更新 UI

2. **多设备同步**：

   - 收藏数据存储在服务器端
   - 每次获取服务列表时自动同步收藏状态
   - 不同设备登录同一账号，收藏状态保持一致

3. **错误处理**：
   - 使用乐观更新策略，先更新 UI
   - 如果 API 调用失败，回滚 UI 状态
   - 不影响服务列表的正常显示

---

# 配置文件精简设计变更分析

## 变更概述

将 Desktop 客户端配置文件从：

```json
{
  "server_address": "localhost:9090",
  "client_id": "user@example.com",
  "device_token": "dt_uuid-generated-by-server",
  "remember_me": true,
  "token_expires_at": 1704628800
}
```

精简为：

```json
{
  "server": "localhost:9090",
  "client": "user@example.com",
  "token": "dt_uuid-generated-by-server"
}
```

## 影响分析

### 1. Server 端影响

#### 1.1 数据库变更

**结论：无需变更**

- ✅ `device_tokens` 表结构保持不变
- ✅ 所有字段（`device_token`, `device_fingerprint`, `expires_at`, `revoked` 等）仍然存储在服务器端
- ✅ 只是客户端不再存储部分字段，服务器端逻辑不受影响

#### 1.2 API 变更

**结论：无需变更**

现有 API 接口：

- `POST /api/v1/client/auth/login` - 使用 Secret 登录

  - 请求：`client_id`, `client_secret`, `device_fingerprint`, `device_info`
  - 响应：`device_token`, `jwt_token`, `expires_at`
  - **影响**：响应中的 `expires_at` 客户端不再存储，但仍然返回（向后兼容）

- `POST /api/v1/client/auth/login/token` - 使用 Device Token 登录

  - 请求：`client_id`, `device_token`, `device_fingerprint`
  - 响应：`jwt_token`, `expires_in`
  - **影响**：无变更

- `GET /api/v1/client/auth/login/devices` - 列出设备

  - **影响**：无变更

- `POST /api/v1/client/auth/login/devices/:device_token/offline` - 设备下线

  - **影响**：无变更

- `DELETE /api/v1/client/auth/login/devices/:device_token` - 删除设备
  - **影响**：无变更

**API 兼容性**：

- ✅ 所有 API 保持向后兼容
- ✅ 服务器仍然返回 `expires_at`，只是客户端不再存储
- ✅ 旧版本客户端仍然可以正常工作

#### 1.3 业务逻辑变更

**结论：无需变更**

- ✅ Device Token 生成逻辑不变（`CreateDeviceToken`）
- ✅ Device Token 验证逻辑不变（`ValidateDeviceToken`）
- ✅ 设备指纹验证逻辑不变
- ✅ Token 过期检查逻辑不变（服务器端检查）

### 2. Desktop 客户端影响

#### 2.1 配置文件结构变更

**需要修改的文件**：`desktop/internal/config/config.go`

**变更内容**：

```go
// 旧结构
type Config struct {
    ServerAddress   string `json:"server_address"`
    ClientID        string `json:"client_id"`
    ClientSecret    string `json:"client_secret"`
    DeviceToken     string `json:"device_token"`
    RememberMe      bool   `json:"remember_me"`
    TokenExpiresAt  int64  `json:"token_expires_at"`
    TunnelToken     string `json:"tunnel_token"`
    PortPreferences map[int64]int `json:"port_preferences"`
}

// 新结构
type Config struct {
    Server          string `json:"server"`           // 改名：server_address -> server
    Client          string `json:"client"`           // 改名：client_id -> client
    Token           string `json:"token"`            // 改名：device_token -> token
    PortPreferences map[int64]int `json:"port_preferences"` // 保留
}
```

**删除的字段**：

- ❌ `ClientSecret` - 已经不再使用
- ❌ `RememberMe` - 通过 `Token` 是否存在判断
- ❌ `TokenExpiresAt` - 服务器管理，客户端无需知道
- ❌ `TunnelToken` - 应该从服务器动态获取

#### 2.2 配置文件方法变更

**需要修改的方法**：

1. **`Load()` 方法**

   - 需要支持旧配置格式迁移
   - 自动将 `server_address` -> `server`
   - 自动将 `client_id` -> `client`
   - 自动将 `device_token` -> `token`

2. **`Save()` 方法**

   - 只保存新格式的 3 个字段

3. **`ClearToken()` 方法**

   - 只清除 `token` 字段
   - 保留 `server` 和 `client`

4. **`ShouldAutoFill()` 方法**

   - 改为：`return c.Server != "" && c.Client != ""`
   - 不再检查 `RememberMe`

5. **`HasValidToken()` 方法**
   - 改为：`return c.Token != ""`
   - 不再检查 `TokenExpiresAt`（由服务器验证）

#### 2.3 登录流程变更

**需要修改的文件**：`desktop/app.go`

**变更内容**：

1. **`CheckSavedCredentials()` 方法**

   - 不再返回 `RememberMe` 字段
   - 通过 `Token` 是否存在判断是否记住登录

2. **`Login()` 方法**

   - 不再保存 `RememberMe` 到配置
   - 不再保存 `TokenExpiresAt` 到配置

3. **`Logout()` 方法**
   - 只清除 `Token`
   - 保留 `Server` 和 `Client`（方便下次登录）

#### 2.4 前端界面变更

**需要修改的文件**：`desktop/frontend/src/views/Login.vue`

**变更内容**：

1. **表单数据绑定**

   - `form.serverAddress` -> `form.server`
   - `form.clientId` -> `form.client`

2. **`CheckSavedCredentials` 响应处理**

   - `savedCreds.server_address` -> `savedCreds.server`
   - `savedCreds.client_id` -> `savedCreds.client`
   - 不再处理 `savedCreds.remember_me`

3. **"记住登录"复选框**
   - 保留 UI，但不再存储到配置文件
   - 只影响是否保存 `token`

#### 2.5 API 调用变更

**需要修改的文件**：`desktop/internal/client/auth.go`

**变更内容**：

1. **`AuthWithSecret()` 方法**

   - 响应中的 `expires_at` 不再保存到配置
   - 只保存 `token`

2. **`AuthWithToken()` 方法**
   - 无需变更（已经不使用 `expires_at`）

### 3. 配置文件迁移策略

#### 3.1 自动迁移逻辑

在 `Load()` 方法中添加迁移逻辑：

```go
func Load() (*Config, error) {
    // 读取配置文件
    data, err := os.ReadFile(configPath)
    if err != nil {
        return defaultConfig(), nil
    }

    // 尝试解析为旧格式
    var oldConfig OldConfig
    if err := json.Unmarshal(data, &oldConfig); err == nil {
        // 检测到旧格式，自动迁移
        if oldConfig.ServerAddress != "" || oldConfig.ClientID != "" {
            newConfig := &Config{
                Server:          oldConfig.ServerAddress,
                Client:          oldConfig.ClientID,
                Token:           oldConfig.DeviceToken,
                PortPreferences: oldConfig.PortPreferences,
            }
            // 保存新格式
            newConfig.Save()
            return newConfig, nil
        }
    }

    // 解析为新格式
    var config Config
    if err := json.Unmarshal(data, &config); err != nil {
        return nil, err
    }

    return &config, nil
}
```

#### 3.2 迁移提示

- 首次启动新版本时，自动迁移配置文件
- 不显示任何提示（静默迁移）
- 保留旧配置文件备份（可选）

### 4. 测试影响

#### 4.1 需要更新的测试

1. **配置文件测试**

   - 测试新格式的加载和保存
   - 测试旧格式的自动迁移
   - 测试字段重命名

2. **登录流程测试**

   - 测试首次登录（保存 token）
   - 测试自动登录（使用 token）
   - 测试 token 失效（清除 token）

3. **API 集成测试**
   - 确保 API 响应兼容新旧客户端

#### 4.2 兼容性测试

- 新客户端 + 旧服务器：✅ 兼容（API 不变）
- 旧客户端 + 新服务器：✅ 兼容（API 不变）
- 配置文件迁移：✅ 自动迁移

### 5. 实施计划

#### 5.1 第一阶段：Desktop 客户端改造

1. 修改 `desktop/internal/config/config.go`

   - 更新 `Config` 结构体
   - 实现配置文件迁移逻辑
   - 更新所有相关方法

2. 修改 `desktop/app.go`

   - 更新 `CheckSavedCredentials()` 方法
   - 更新 `Login()` 方法
   - 更新 `Logout()` 方法

3. 修改 `desktop/internal/client/auth.go`

   - 更新 API 响应处理逻辑

4. 修改 `desktop/frontend/src/views/Login.vue`

   - 更新表单数据绑定
   - 更新 API 响应处理

5. 修改 `desktop/frontend/src/stores/auth.ts`
   - 更新字段名

#### 5.2 第二阶段：测试

1. 单元测试

   - 配置文件加载/保存
   - 配置文件迁移

2. 集成测试

   - 登录流程
   - Token 验证

3. 兼容性测试
   - 新旧版本互操作

#### 5.3 第三阶段：部署

1. 发布新版本 Desktop 客户端
2. 用户升级时自动迁移配置文件
3. 监控迁移过程中的问题

### 6. 风险评估

#### 6.1 低风险

- ✅ Server 端无需变更
- ✅ API 保持向后兼容
- ✅ 数据库无需变更
- ✅ 配置文件自动迁移

#### 6.2 需要注意的点

1. **配置文件迁移**

   - 确保迁移逻辑正确
   - 测试各种旧配置格式

2. **用户体验**

   - 迁移过程对用户透明
   - 不影响现有登录状态

3. **回滚策略**
   - 保留旧配置文件备份
   - 如果出现问题，可以回滚到旧版本

### 7. 总结

**Server 端**：

- ✅ 无需变更数据库
- ✅ 无需变更 API
- ✅ 无需变更业务逻辑

**Desktop 端**：

- 🔧 需要修改配置文件结构（字段重命名）
- 🔧 需要实现配置文件迁移逻辑
- 🔧 需要更新相关方法和前端代码
- ✅ 变更范围可控，风险较低

**整体评估**：

- 这是一个**低风险**的重构
- 主要影响 Desktop 客户端的配置管理
- Server 端完全不受影响
- 可以平滑升级，无需停机

---

**文档版本**: 1.0  
**创建日期**: 2025-12-01
