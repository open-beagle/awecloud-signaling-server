# 桌面客户端下载 API

## 概述

Server 提供智能的桌面客户端下载 API，能够：

- 自动识别客户端操作系统
- 提供最新版本的下载链接
- 支持直接下载或获取下载信息

## 存储目录结构

管理员需要在系统配置中设置"客户端下载地址"，支持三种格式：

1. **目录 URL（推荐）**：`https://your-cdn.example.com/path/to/files`
2. **带斜杠的目录 URL**：`https://your-cdn.example.com/path/to/files/`
3. **完整文件 URL**：`https://your-cdn.example.com/path/to/files/awecloud-signaling-v0.1.0-windows-amd64.exe`

**存储目录结构**：

```
your-storage-path/
├── version.json                                      # 最新版本信息
├── awecloud-signaling-v0.1.0-windows-amd64.exe     # 版本文件
├── awecloud-signaling-v0.1.0-linux-amd64
├── awecloud-signaling-v0.1.0-darwin-universal.zip
├── awecloud-signaling-v0.2.0-windows-amd64.exe     # 其他版本
├── awecloud-signaling-v0.2.0-linux-amd64
└── awecloud-signaling-v0.2.0-darwin-universal.zip
```

## API 端点

### 1. 获取下载信息（JSON）

**端点**: `GET /api/v1/public/download/desktop`

**描述**: 返回适合当前操作系统的下载信息（JSON 格式）

**参数**:

- `os` (可选): 指定操作系统 (`windows`, `linux`, `darwin`/`macos`)
  - 如果不提供，自动从 User-Agent 检测

**响应示例**:

```json
{
  "version": "v0.1.0",
  "download_url": "https://your-cdn.example.com/path/to/files/awecloud-signaling-v0.1.0-windows-amd64.exe",
  "filename": "awecloud-signaling-v0.1.0-windows-amd64.exe",
  "os": "windows",
  "arch": "amd64",
  "build_date": "2025-12-05T10:30:00Z"
}
```

**使用示例**:

```bash
# 自动检测操作系统
curl https://your-server.example.com/api/v1/public/download/desktop

# 指定操作系统
curl https://your-server.example.com/api/v1/public/download/desktop?os=macos
```

### 2. 直接下载（重定向）

**端点**: `GET /api/v1/public/download/desktop/direct`

**描述**: 直接重定向到下载链接，适合浏览器直接访问

**参数**:

- `os` (可选): 指定操作系统 (`windows`, `linux`, `darwin`/`macos`)

**使用示例**:

```bash
# 浏览器访问，自动下载
https://your-server.example.com/api/v1/public/download/desktop/direct

# 指定 macOS 版本
https://your-server.example.com/api/v1/public/download/desktop/direct?os=macos
```

### 3. 列出所有版本

**端点**: `GET /api/v1/public/download/desktop/versions`

**描述**: 列出所有平台的下载信息

**响应示例**:

```json
{
  "version": "v0.1.0",
  "build_date": "2025-12-05T10:30:00Z",
  "downloads": {
    "windows": {
      "version": "v0.1.0",
      "download_url": "https://your-cdn.example.com/path/to/files/awecloud-signaling-v0.1.0-windows-amd64.exe",
      "filename": "awecloud-signaling-v0.1.0-windows-amd64.exe",
      "os": "windows",
      "arch": "amd64",
      "build_date": "2025-12-05T10:30:00Z"
    },
    "linux": {
      "version": "v0.1.0",
      "download_url": "https://your-cdn.example.com/path/to/files/awecloud-signaling-v0.1.0-linux-amd64",
      "filename": "awecloud-signaling-v0.1.0-linux-amd64",
      "os": "linux",
      "arch": "amd64",
      "build_date": "2025-12-05T10:30:00Z"
    },
    "darwin": {
      "version": "v0.1.0",
      "download_url": "https://your-cdn.example.com/path/to/files/awecloud-signaling-v0.1.0-darwin-universal.zip",
      "filename": "awecloud-signaling-v0.1.0-darwin-universal.zip",
      "os": "darwin",
      "arch": "universal",
      "build_date": "2025-12-05T10:30:00Z"
    },
    "macos": {
      "version": "v0.1.0",
      "download_url": "https://your-cdn.example.com/path/to/files/awecloud-signaling-v0.1.0-darwin-universal.zip",
      "filename": "awecloud-signaling-v0.1.0-darwin-universal.zip",
      "os": "darwin",
      "arch": "universal",
      "build_date": "2025-12-05T10:30:00Z"
    }
  }
}
```

## 操作系统检测

API 通过以下方式检测操作系统：

1. **查询参数优先**: 如果提供 `?os=xxx` 参数，使用指定的操作系统
2. **User-Agent 检测**: 从 HTTP User-Agent 头自动识别
   - 包含 `Windows`/`Win64`/`Win32` → Windows
   - 包含 `Macintosh`/`Mac OS X`/`Darwin` → macOS
   - 包含 `Linux`/`X11` → Linux
3. **默认值**: 如果无法识别，使用服务器运行的操作系统

## 版本管理

### version.json 格式

```json
{
  "version": "v0.1.0",
  "build_date": "2025-12-05T10:30:00Z"
}
```

### 自动更新流程

1. GitHub Actions 构建完成后，上传到 S3
2. 上传带版本号的文件（如 `awecloud-signaling-v0.1.0-windows-amd64.exe`）作为归档
3. 同时上传 `latest` 文件（如 `awecloud-signaling-latest-windows-amd64.exe`）覆盖旧版本
4. 更新 `version.json` 文件
5. 客户端通过 API 获取最新版本信息

## 前端集成示例

### JavaScript/TypeScript

```typescript
// 获取下载信息
async function getDownloadInfo() {
  const response = await fetch("/api/v1/public/download/desktop");
  const data = await response.json();
  console.log(`最新版本: ${data.version}`);
  console.log(`下载链接: ${data.download_url}`);
  return data;
}

// 直接下载
function downloadDesktop() {
  window.location.href = "/api/v1/public/download/desktop/direct";
}

// 获取所有平台的下载链接
async function getAllDownloads() {
  const response = await fetch("/api/v1/public/download/desktop/versions");
  const data = await response.json();
  return data.downloads;
}
```

### HTML 示例

```html
<!-- 自动检测并下载 -->
<a href="/api/v1/public/download/desktop/direct" class="btn">
  下载桌面客户端
</a>

<!-- 指定平台 -->
<a href="/api/v1/public/download/desktop/direct?os=windows">
  下载 Windows 版本
</a>
<a href="/api/v1/public/download/desktop/direct?os=macos"> 下载 macOS 版本 </a>
<a href="/api/v1/public/download/desktop/direct?os=linux"> 下载 Linux 版本 </a>
```

## 注意事项

1. **存储配置**: 管理员需要在 Web 界面的"系统配置"中设置"客户端下载地址"
2. **公开访问**: 确保存储服务（S3/OSS/CDN）的文件设置为公开读取
3. **CORS 配置**: 如果前端跨域访问，需要配置存储服务的 CORS 策略
4. **缓存策略**:
   - `version.json`：设置较短的缓存时间（如 1 分钟）
   - `*-v0.x.x-*` 文件：可以长期缓存（如 1 年）
5. **文件大小**: macOS 的 .zip 文件通常比其他平台大，注意下载体验
6. **版本归档**: 每次发布都会保留带版本号的文件，方便回滚或下载历史版本
7. **隐私保护**: 存储地址不会硬编码在代码中，完全由管理员配置

## 安全性

- API 端点为公开访问，不需要认证
- 所有下载链接指向管理员配置的存储地址
- 存储地址不会硬编码在代码中，保护隐私
- 建议在生产环境中添加下载速率限制
- 建议使用 CDN 加速下载
