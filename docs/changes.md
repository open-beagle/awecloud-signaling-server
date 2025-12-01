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
