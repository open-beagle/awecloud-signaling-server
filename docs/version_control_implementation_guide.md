# 版本控制功能实现指南

## 概述

本文档说明版本控制功能的实现细节和使用方法。该功能允许管理员设置 Desktop 客户端的最低支持版本，确保客户端版本符合服务端要求。

## 实现的功能

### 1. 管理员配置界面

在"系统管理 > 系统配置"页面，管理员可以配置：

- **客户端下载地址**：客户端文件存储的基础 URL
- **客户端最低版本**：Desktop 客户端的最低支持版本（格式：x.y.z）

### 2. 版本检查 API

Desktop 客户端可以调用版本检查 API 来验证自己的版本是否符合要求。

**API 端点**：`POST /api/v1/client/version/check`

**请求示例**：

```json
{
  "client_version": "1.0.0",
  "os": "windows",
  "arch": "amd64"
}
```

**响应示例**：

```json
{
  "success": true,
  "version_valid": true,
  "min_version": "1.0.0",
  "download_url": "https://example.com/download",
  "message": "版本检查通过"
}
```

### 3. 版本比较逻辑

使用语义化版本号（Semantic Versioning）进行比较：

- 格式：`MAJOR.MINOR.PATCH`（如：1.0.0, 1.2.3, 2.0.0）
- 比较规则：逐段比较数字大小
- 示例：1.0.0 < 1.0.1 < 1.1.0 < 2.0.0

## 技术实现

### 后端实现

#### 1. 数据库模型（internal/server/model/system_config.go）

```go
type SystemConfig struct {
    ID                uint      `gorm:"primaryKey" json:"id"`
    ClientDownloadURL string    `gorm:"type:text" json:"client_download_url"`
    DesktopMinVersion string    `gorm:"type:varchar(20);default:'1.0.0'" json:"desktop_min_version"`
    CreatedAt         time.Time `json:"created_at"`
    UpdatedAt         time.Time `json:"updated_at"`
}
```

#### 2. 版本检查 API（internal/server/api/version.go）

- `CheckVersion(c *gin.Context)`：处理版本检查请求
- `compareVersion(v1, v2 string) int`：比较两个版本号

#### 3. 系统配置 API（internal/server/api/system_config.go）

- `GetSystemConfig(c *gin.Context)`：获取系统配置（包含最低版本）
- `UpdateSystemConfig(c *gin.Context)`：更新系统配置（包含最低版本）

### 前端实现

#### 1. API 接口（web/src/api/system.ts）

```typescript
export interface SystemConfig {
  id: number;
  client_download_url: string;
  desktop_min_version: string;
  created_at: string;
  updated_at: string;
}

export function updateSystemConfig(data: {
  client_download_url: string;
  desktop_min_version: string;
});
```

#### 2. 系统配置页面（web/src/views/System/Config.vue）

- 添加"客户端最低版本"输入框
- 版本号格式验证（必须为 x.y.z 格式）
- 与客户端下载地址配置集成在同一页面

## 使用方法

### 管理员配置

1. 登录管理后台
2. 进入"系统管理 > 系统配置"
3. 在"客户端最低版本"字段输入版本号（如：1.2.0）
4. 点击"保存"按钮

### Desktop 客户端集成

Desktop 客户端需要实现以下功能：

#### 1. 版本号注入

在编译时通过 `-ldflags` 注入版本号：

```bash
VERSION="1.0.0"
GIT_COMMIT=$(git rev-parse --short HEAD)
BUILD_TIME=$(date -u '+%Y-%m-%d_%H:%M:%S')

go build -ldflags "\
  -X 'package/version.Version=${VERSION}' \
  -X 'package/version.GitCommit=${GIT_COMMIT}' \
  -X 'package/version.BuildTime=${BUILD_TIME}'" \
  -o bin/desktop cmd/desktop/main.go
```

#### 2. 启动时检查版本

```go
func checkVersion(serverURL, clientVersion string) error {
    req := map[string]string{
        "client_version": clientVersion,
        "os":            runtime.GOOS,
        "arch":          runtime.GOARCH,
    }

    resp := &VersionCheckResponse{}
    err := httpPost(serverURL+"/api/v1/client/version/check", req, resp)
    if err != nil {
        return err
    }

    if !resp.VersionValid {
        // 显示强制升级界面
        showUpgradeDialog(resp.MinVersion, resp.DownloadURL)
        return fmt.Errorf("version too old")
    }

    return nil
}
```

#### 3. 强制升级界面

当版本过低时，显示升级提示：

- 显示当前版本和最新版本
- 提供"立即下载"按钮
- 点击按钮打开浏览器访问下载页面
- 阻止用户继续使用应用

## 测试

### 自动化测试脚本

运行测试脚本验证功能：

```bash
# 确保服务器正在运行
./bin/server

# 在另一个终端运行测试
./.tmp/test_version_api.sh
```

测试脚本会验证：

1. 版本检查 API 是否正常工作
2. 版本比较逻辑是否正确
3. 系统配置更新是否生效
4. 版本过低时是否正确拒绝

### 手动测试

#### 测试 1：配置最低版本

1. 登录管理后台
2. 设置"客户端最低版本"为 "1.2.0"
3. 保存配置
4. 验证配置是否保存成功

#### 测试 2：版本检查通过

```bash
curl -X POST http://localhost:8080/api/v1/client/version/check \
  -H "Content-Type: application/json" \
  -d '{"client_version":"1.2.0","os":"windows","arch":"amd64"}'
```

预期结果：`version_valid: true`

#### 测试 3：版本检查失败

```bash
curl -X POST http://localhost:8080/api/v1/client/version/check \
  -H "Content-Type: application/json" \
  -d '{"client_version":"1.0.0","os":"windows","arch":"amd64"}'
```

预期结果：`version_valid: false`

## 数据库迁移

GORM 会自动迁移数据库，添加新字段：

- 字段名：`desktop_min_version`
- 类型：VARCHAR(20)
- 默认值：'1.0.0'

如果需要手动迁移，可以执行：

```sql
ALTER TABLE system_config ADD COLUMN desktop_min_version VARCHAR(20) DEFAULT '1.0.0';
```

## 安全考虑

1. **版本检查在服务端进行**：Desktop 客户端无法绕过版本检查
2. **版本号验证**：前端和后端都会验证版本号格式
3. **下载链接安全**：建议使用 HTTPS 下载链接
4. **管理员权限**：只有管理员可以修改最低版本要求

## 故障排查

### 问题 1：版本检查 API 返回 404

**原因**：路由未正确注册

**解决**：检查 `internal/server/server.go` 中是否包含：

```go
v1Group.POST("/client/version/check", api.CheckVersion)
```

### 问题 2：前端保存配置失败

**原因**：版本号格式不正确

**解决**：确保版本号格式为 `x.y.z`（如：1.0.0）

### 问题 3：数据库字段不存在

**原因**：数据库未自动迁移

**解决**：

1. 删除旧的数据库文件
2. 重启服务器，让 GORM 重新创建表
3. 或手动执行 SQL 添加字段

## 相关文档

- [版本管理设计文档](./design_version_control.md)
- [API 设计文档](./design_api.md) - 2.9 版本管理 API
- [数据库设计文档](./design_database.md) - 2.10 系统设置表

## 更新日志

- **2024-12-06**：初始实现，支持基本的版本检查和管理功能
