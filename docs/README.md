# 文档索引

> ⚠️ **重要规范**: 禁止随意创建文档，所有文档创建必须经过讨论和批准。

## 开发规范

### 构建规范

- ✅ **允许**: 构建项目到 `bin/` 目录
- ❌ **禁止**: 在其他位置生成可执行文件
- 📝 **要求**: 使用 `scripts/build.sh` 或标准 go build 命令
- 🔧 **开发**: 只构建当前架构 `GOARCHS=$(go env GOARCH) ./scripts/build.sh`
- 🚀 **生产**: 构建所有架构 `./scripts/build.sh`

### 调试规范

- ❌ **禁止**: 随意调试系统
- 💬 **要求**: 调试前必须讨论方案
- 📋 **记录**: 所有调试活动记录在 `docs/debug.md`

### 进度更新规范

- 📝 **开发计划** (`docs/plan.md`): 完成任务后更新状态
- 📊 **进度跟踪** (`docs/progress.md`): 每日更新，完成任务后清理详情
- ❌ **禁止**: 随意创建文档，所有新文档必须经过讨论

## 核心文档

### 📋 开发计划 (`plan.md`)

**重要文档** - 项目开发的核心指导文档

- 包含完整的 8 周开发计划
- 详细的任务清单和交付物
- 技术难点和解决方案
- **完成任务后必须更新进度**

### 📊 进度跟踪 (`progress.md`)

**每日更新** - 项目整体进度跟踪

- 当前阶段和周次
- 各阶段完成状态
- 里程碑达成情况
- 风险和问题记录
- **完成任务后清理详情，保留简要信息**

### 🐛 调试规范 (`debug.md`)

**调试记录** - 所有调试活动的记录

- 调试原则和流程
- 构建规范
- 调试历史记录
- **调试前必须讨论方案**

### 🎨 设计文档 (`design.md`)

**技术设计** - 系统架构和技术方案

- 项目概述和架构
- 核心架构设计
- 引用详细设计文档：
  - `design_server.md` - Server 进程内部设计（重要）
  - `design_http2.md` - HTTP/2 统一端口设计（重要）
  - `design_security_token_audit.md` - 安全令牌与审计日志设计（重要）
  - `design_version_control.md` - Desktop 版本管理设计（重要）
  - `design_api_download.md` - Desktop 客户端下载 API 设计（重要）
  - `design_server_tcp_service_management.md` - TCP 服务管理设计（新功能）
  - `design_public_url.md` - 公网地址配置设计（待实现）
  - `design_server_access_control.md` - 访问控制系统设计（未来功能）
  - `design_server_web.md` - Web 管理界面设计
  - `design_desktop.md` - Desktop 客户端设计
  - `design_database.md` - 数据库详细设计
  - `design_api.md` - API 详细设计
  - `design_deployment.md` - 部署方案
  - `design_frp.md` - FRP 隧道设计和实现

### 📝 变更记录 (`changes.md`)

**设计变更** - 重要设计决策的记录

- 2025-12-29: Wails v3 升级计划（详见 `changes_wails_v3.md`）
  - 为实现系统托盘功能，升级 Wails v2 → v3
  - 设计文档：`design_desktop_system_tray.md`
- 2025-11-27: HTTP/2 统一端口设计
  - 将 HTTP 和 gRPC 合并到端口 8080
  - 详细的变更原因和实施计划

### 🧪 测试规范 (`test.md`)

**测试文档** - API 测试规范和流程

- 测试原则和流程
- 测试目录结构
- 测试脚本规范
- 测试用例说明
- **测试脚本位于 `tests/` 目录**

## 快速参考

### 📖 项目说明 (`../README.md`)

- 项目简介
- 目录结构
- API 文档
- 开发状态

### 🖥️ Desktop 客户端

**独立仓库** - Desktop 客户端应用

- 仓库地址: https://github.com/open-beagle/awecloud-signaling-desktop
- 本地路径: `../desktop/`
- Desktop 相关代码在独立仓库中开发
- 与 Server 通过 gRPC 和 FRP 通信

**Desktop 文档**:

- `../desktop/docs/README.md` - Desktop 文档索引
- `../desktop/docs/development.md` - 开发指南
- `../desktop/docs/user-guide.md` - 用户手册
- `../desktop/docs/logo.md` - Logo 使用说明
- `../desktop/PROGRESS.md` - Desktop 开发进度

## 文档规范

### ⚠️ 重要规则

1. **禁止随意创建文档**
   - 所有新文档必须经过讨论和批准
   - 不允许创建未经授权的文档
   - 保持文档结构简洁清晰

### 更新规则

1. **开发计划** (`plan.md`)

   - 完成任务后更新状态（[ ] → [x]）
   - 添加完成时间
   - 更新里程碑状态

2. **进度跟踪** (`progress.md`)

   - 每日更新当天进展
   - 完成任务后清理详情
   - 保持简洁，避免冗余

3. **调试记录** (`debug.md`)
   - 调试前讨论方案
   - 记录调试过程
   - 总结解决方案

### 文档关系

```
plan.md (核心计划)
   ↓
progress.md (进度跟踪)
   ↓
debug.md (调试记录)
```

## 构建和调试规范

### 构建规范

```bash
# 开发阶段：只构建当前架构
GOARCHS=$(go env GOARCH) ./scripts/build.sh

# 生产构建：构建所有架构（amd64 + arm64）
./scripts/build.sh

# 所有二进制文件输出到 bin/ 目录
```

### 调试规范

- ❌ 禁止随意调试系统
- 💬 调试前必须讨论方案
- 📋 所有调试活动记录在 `debug.md`

### 文档规范

- ❌ 禁止随意创建文档
- 💬 新文档必须经过讨论和批准
- 📋 保持文档结构简洁清晰

## 测试规范

### 测试流程

```bash
# 1. 清理数据库
rm -f data/server.db

# 2. 启动Server（手动）
./bin/server -c config/server.toml

# 3. 运行测试（新终端）
./tests/run_all.sh

# 或运行单个测试
./tests/api/test_admin.sh
```

### 测试目录

```
tests/
├── api/                    # API测试脚本
│   ├── test_admin.sh      # 管理员认证测试
│   ├── test_agent.sh      # Agent管理测试
│   ├── test_client.sh     # Client管理测试
│   ├── test_stcp.sh       # STCP实例管理测试
│   └── test_client_auth.sh # Client认证测试
├── common.sh              # 公共函数
└── run_all.sh             # 运行所有测试
```

## 文档版本

- **创建日期**: 2025-11-25
- **最后更新**: 2025-11-27
- **维护者**: 项目团队

## 最近更新

### 2025-12-07

- 新增 `design_server_tcp_service_management.md` - TCP 服务管理设计
  - 允许 Server 端在指定 Agent 端创建 TCP 服务实例
  - 自动端口分配机制（从 9000 开始）
  - 服务默认禁用，需手动启用
  - 端口只在删除时释放，禁用不释放
- 新增 `implementation_tcp_service_management.md` - TCP 服务管理实施计划
  - 4 个阶段的详细实施计划
  - 完整的测试策略（单元、集成、性能）
  - 监控和告警方案
  - 风险管理和应急预案

### 2025-12-05

- 新增 `design_api_download.md` - Desktop 客户端下载 API 设计
  - 智能识别客户端操作系统
  - 自动获取最新版本信息
  - 支持直接下载和 JSON 信息获取
  - S3 扁平化目录结构设计

### 2025-11-27

- 新增 `design_security_token_audit.md` - 安全令牌与审计日志设计
  - Device Token 系统：替代明文 secret 存储
  - Desktop 登录双模式设计
  - 审计日志系统：记录用户连接行为
  - 设备管理功能：查看、下线、删除设备
- 新增 `design_version_control.md` - Desktop 版本管理设计
  - Server 端设置最低支持版本
  - Desktop 端版本检查和强制升级
  - Web 管理界面版本管理功能
  - 版本发布流程规范
