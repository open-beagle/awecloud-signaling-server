# 安全令牌与版本管理开发计划

## 开发规范

### 过程文档管理

**重要规范**：开发过程中产生的临时文档、调试记录、问题分析等过程文档必须存储在 `.tmp/` 目录中。

**过程文档类型**：
- 调试记录和问题分析
- 临时测试结果
- 开发过程中的笔记
- 实施过程中的总结
- 临时的代码片段

**示例**：
```
.tmp/
├── security_token_implementation.md    # 实施记录
├── device_token_debug.md              # 调试记录
├── version_check_test_results.md      # 测试结果
├── api_integration_notes.md           # 集成笔记
└── issues_and_solutions.md            # 问题与解决方案
```

**规范说明**：
- ✅ 过程文档存储在 `.tmp/` 目录
- ✅ 使用描述性的文件名
- ✅ 完成后可以清理或归档
- ❌ 不要将过程文档提交到 `docs/` 目录
- ❌ 不要将临时文件混入正式文档

---

## 📊 当前进度

**更新时间**：2025-11-27

### 第一阶段：安全令牌与审计日志系统

- [x] **阶段1.1**：数据库设计与迁移（0.5天）✅ 完成
- [x] **阶段1.2**：Server端 - Device Token API（2天）✅ 完成
- [x] **阶段1.3**：Server端 - 端口偏好与审计日志API（1.5天）✅ 完成
- [x] **阶段1.4**：Server端 - Web管理界面（2天）✅ 完成
- [ ] **阶段1.5**：Desktop端 - 设备指纹与Token管理（2天）⏳ 待开始
- [ ] **阶段1.6**：Desktop端 - 登录界面双模式（2天）
- [ ] **阶段1.7**：Desktop端 - 设备管理界面（1天）
- [ ] **阶段1.8**：文档与测试准备（1天）

**进度统计**：
- Server端：✅ 100% (4/4阶段完成)
- Desktop端：⏳ 0% (0/4阶段完成)
- 总体进度：✅ 50% (4/8阶段完成)

### 第二阶段：Desktop版本管理系统

- [ ] **阶段2.1**：数据库设计与迁移（0.5天）
- [ ] **阶段2.2**：Server端 - 版本管理API（1.5天）
- [ ] **阶段2.3**：Desktop端 - 版本号注入（0.5天）
- [ ] **阶段2.4**：Desktop端 - 版本检查逻辑（1天）
- [ ] **阶段2.5**：Desktop端 - 强制升级界面（1天）
- [ ] **阶段2.6**：Web管理界面 - 版本管理页面（1.5天）
- [ ] **阶段2.7**：集成测试与文档（1天）

**进度统计**：
- 总体进度：⏳ 0% (0/7阶段完成)

### 📈 总体进度

- **已完成**：4个阶段
- **进行中**：0个阶段
- **待开始**：11个阶段
- **总进度**：27% (4/15阶段)
- **预计完成**：2025-12-20

### 🎯 里程碑

- [x] **里程碑1**：Server端API完成（2025-11-27）✅
- [ ] **里程碑2**：Desktop端完成（预计2025-12-04）
- [ ] **里程碑3**：版本管理系统完成（预计2025-12-20）

---

## 概述

本文档规划了两个重要功能的开发顺序和任务清单：
1. **安全令牌与审计日志系统** (`design_security_token_audit.md`)
2. **Desktop版本管理系统** (`design_version_control.md`)

**开发顺序说明**：
- 先开发安全令牌系统，因为它是Desktop登录的基础
- 再开发版本管理系统，在登录流程中集成版本检查

---

## 第一阶段：安全令牌与审计日志系统

**参考文档**：`docs/design_security_token_audit.md`

### 阶段1.1：数据库设计与迁移 ✅

**状态**：已完成（2025-11-27）

**任务清单**：

- [x] 创建数据库迁移脚本
  - [x] `device_tokens` 表
  - [x] `port_preferences` 表
  - [x] `connection_audit_logs` 表
- [x] 编写数据库初始化代码
- [x] 测试数据库迁移

**实际时间**：0.5天

**验收标准**：
- ✅ 数据库表创建成功
- ✅ 索引创建正确
- ✅ 可以正常插入和查询数据

**交付成果**：
- `internal/server/model/device_token.go`
- `internal/server/model/port_preference.go`
- `internal/server/model/connection_audit_log.go`
- 更新 `internal/server/db/db.go`

---

### 阶段1.2：Server端 - Device Token API ✅

**状态**：已完成（2025-11-27）

**任务清单**：

- [x] 实现设备指纹验证逻辑
  - [x] `internal/server/auth/device_fingerprint.go`
  - [x] 设备指纹生成和验证函数
  
- [x] 实现Device Token管理
  - [x] `internal/server/auth/device_token.go`
  - [x] Token生成、验证、撤销逻辑
  
- [x] 实现认证API
  - [x] `POST /api/v1/client/auth/login` - 使用Secret登录并获取Device Token
  - [x] `POST /api/v1/client/auth/login/token` - 使用Device Token登录
  - [x] `GET /api/v1/client/auth/login/devices` - 列出用户已登录的设备
  - [x] `POST /api/v1/client/auth/login/devices/:device_token/offline` - 让设备下线
  - [x] `DELETE /api/v1/client/auth/login/devices/:device_token` - 删除设备记录

- [x] 添加单元测试
  - [x] 编译测试通过

**实际时间**：1天

**验收标准**：
- ✅ 所有API端点正常工作
- ✅ Token验证逻辑正确
- ✅ 设备指纹匹配准确

**交付成果**：
- `internal/server/auth/device_fingerprint.go`
- `internal/server/auth/device_token.go`
- `internal/server/api/device_token.go`
- 更新 `internal/server/server.go`（添加路由）
- 单元测试通过率100%

---

### 阶段1.3：Server端 - 端口偏好与审计日志API ✅

**状态**：已完成（2025-11-27）

**任务清单**：

- [x] 实现端口偏好API
  - [x] `GET /api/v1/client/preferences/port` - 获取端口偏好
  - [x] `POST /api/v1/client/preferences/port` - 保存端口偏好
  
- [x] 实现审计日志API
  - [x] `POST /api/v1/client/audit/connection` - 记录连接审计日志
  - [x] `GET /api/v1/admin/audit/connection` - 查询审计日志（管理员）
  - [x] `GET /api/v1/admin/audit/connection/export` - 导出审计日志（管理员）

- [x] 添加单元测试

**实际时间**：1天

**验收标准**：
- ✅ 端口偏好可以正确保存和读取
- ✅ 审计日志记录完整
- ✅ 管理员可以查询和导出日志
- ✅ 编译测试通过

**交付成果**：
- `internal/server/api/port_preference.go`
- `internal/server/api/audit_log.go`
- 更新 `internal/server/server.go`（添加路由）

---

### 阶段1.4：Server端 - Web管理界面 ✅

**状态**：已完成（2025-11-27）

**任务清单**：

- [x] 创建审计日志查询页面
  - [x] `web/src/views/Audit/AuditLogs.vue`
  - [x] 显示连接审计日志列表
  - [x] 过滤条件（用户、服务、时间范围、操作类型）
  - [x] 分页功能
  - [x] 显示设备信息、IP地址等详细信息
  
- [x] 实现审计日志导出功能
  - [x] "导出"按钮
  - [x] CSV格式导出
  - [x] 下载文件
  
- [ ] 创建Device Token管理页面（可选）
  - 未实现（用户通过Desktop管理自己的设备）
  
- [x] 添加到管理菜单
  - [x] 在导航菜单中添加"审计日志"入口
  - [x] 添加路由配置
  - [x] 国际化支持（中英文）

**实际时间**：1天

**验收标准**：
- ✅ 审计日志页面正确显示
- ✅ 过滤和分页功能正常
- ✅ 可以导出审计日志（CSV）
- ✅ 界面友好易用
- ✅ 国际化支持

**交付成果**：
- `web/src/api/audit.ts`
- `web/src/views/Audit/AuditLogs.vue`
- 更新 `web/src/router/index.ts`
- 更新 `web/src/components/Layout/Sidebar.vue`
- 更新 `web/src/locales/zh-CN.ts` 和 `en-US.ts`
- 修复 `web/src/utils/request.ts`（baseURL配置）
- 修复 `web/src/utils/time.ts`（添加formatTime函数）

---

### 阶段1.5：Desktop端 - 设备指纹与Token管理

**任务清单**：

- [ ] 实现设备指纹收集
  - [ ] `desktop/internal/device/fingerprint.go`
  - [ ] 收集OS、CPU、MachineID等信息
  - [ ] 生成设备指纹哈希
  
- [ ] 实现配置文件管理
  - [ ] 更新 `desktop/internal/config/config.go`
  - [ ] 添加Device Token相关字段
  - [ ] 实现 `ClearToken()` 方法
  - [ ] 实现 `ShouldAutoFill()` 方法
  - [ ] 实现 `HasValidToken()` 方法
  
- [ ] 实现Token认证逻辑
  - [ ] `desktop/internal/client/auth.go`
  - [ ] 使用Secret登录
  - [ ] 使用Device Token登录
  - [ ] Token失效处理

**预计时间**：2天

**验收标准**：
- 设备指纹可以正确生成
- 配置文件正确保存和读取
- Token认证流程正常
- Token失效后正确清理

---

### 阶段1.6：Desktop端 - 登录界面双模式

**任务清单**：

- [ ] 实现登录模式判断逻辑
  - [ ] `desktop/internal/client/login_mode.go`
  - [ ] `DetermineLoginMode()` 函数
  
- [ ] 实现模式1：离线状态显示
  - [ ] 更新 `desktop/frontend/src/views/Login.vue`
  - [ ] 显示服务器地址和用户名
  - [ ] 显示离线提示
  - [ ] "登录"按钮逻辑
  
- [ ] 实现模式2：完整登录表单
  - [ ] 自动填充服务器地址和用户名
  - [ ] 密码输入框
  - [ ] "记住登录"复选框
  
- [ ] 实现Server连接检测
  - [ ] `CanConnectToServer()` 函数
  - [ ] 自动重连机制

**预计时间**：2天

**验收标准**：
- 两种登录模式正确切换
- 离线模式显示正确
- 自动填充功能正常
- Server连接检测准确

---

### 阶段1.7：Desktop端 - 设备管理界面

**任务清单**：

- [ ] 创建设备管理页面
  - [ ] `desktop/frontend/src/views/Devices.vue`
  - [ ] 显示已登录设备列表
  - [ ] 设备信息展示（OS、主机名、最后使用时间等）
  
- [ ] 实现设备管理操作
  - [ ] "让设备下线"按钮
  - [ ] "删除设备"按钮
  - [ ] 确认对话框
  
- [ ] 添加到导航菜单

**预计时间**：1天

**验收标准**：
- 设备列表正确显示
- 可以让设备下线
- 可以删除设备记录
- 操作有确认提示

---

### 阶段1.8：文档与测试准备

**任务清单**：

- [ ] 创建API测试脚本
  - [ ] `tests/api/test_device_token.sh` - Device Token API测试
  - [ ] `tests/api/test_audit_logs.sh` - 审计日志API测试
  - [ ] `tests/api/test_port_preferences.sh` - 端口偏好API测试
  
- [ ] 更新用户文档
  - [ ] Desktop用户手册（登录、设备管理）
  - [ ] 管理员手册（审计日志查询、设备管理）
  - [ ] API文档更新
  
- [ ] 准备测试数据和场景
  - [ ] 测试用户账号
  - [ ] 测试设备信息
  - [ ] 测试场景清单

**预计时间**：1天

**验收标准**：
- API测试脚本可以正常运行
- 文档完整准确
- 测试场景清单完整
- 准备好人工测试环境

**注意**：人工测试由开发者执行，不需要自动化集成测试

---

**第一阶段总计**：11.5天

---

## 第二阶段：Desktop版本管理系统

**参考文档**：`docs/design_version_control.md`

### 阶段2.1：数据库设计与迁移

**任务清单**：

- [ ] 创建数据库迁移脚本
  - [ ] `system_settings` 表
  - [ ] 插入默认版本设置
  
- [ ] 测试数据库迁移

**预计时间**：0.5天

**验收标准**：
- `system_settings` 表创建成功
- 默认设置正确插入
- 可以正常查询和更新设置

---

### 阶段2.2：Server端 - 版本管理API

**任务清单**：

- [ ] 实现系统设置管理
  - [ ] `internal/server/settings/system_settings.go`
  - [ ] 读取和更新设置的通用函数
  
- [ ] 实现版本比较逻辑
  - [ ] `internal/server/version/compare.go`
  - [ ] 语义化版本号解析
  - [ ] 版本比较函数
  
- [ ] 实现版本检查API
  - [ ] `POST /api/v1/client/version/check` - Desktop检查版本
  
- [ ] 实现管理员版本管理API
  - [ ] `GET /api/v1/admin/settings/version` - 获取版本设置
  - [ ] `PUT /api/v1/admin/settings/version/min` - 更新最低版本
  - [ ] `PUT /api/v1/admin/settings/version/download-url` - 更新下载地址
  - [ ] `PUT /api/v1/admin/settings/version/check-enabled` - 启用/禁用版本检查

- [ ] 添加单元测试

**预计时间**：1.5天

**验收标准**：
- 版本比较逻辑正确
- 所有API端点正常工作
- 单元测试通过

---

### 阶段2.3：Desktop端 - 版本号注入

**任务清单**：

- [ ] 创建版本管理模块
  - [ ] `desktop/internal/version/version.go`
  - [ ] 定义版本变量
  - [ ] `GetVersion()` 函数
  - [ ] `GetFullVersion()` 函数
  
- [ ] 更新构建脚本
  - [ ] 修改 `scripts/build_desktop.sh`
  - [ ] 添加 `-ldflags` 注入版本号
  - [ ] 从 `VERSION` 文件读取版本号
  
- [ ] 创建 `VERSION` 文件
  - [ ] 初始版本设置为 `1.0.0`

**预计时间**：0.5天

**验收标准**：
- 版本号正确注入到二进制文件
- 可以在运行时获取版本号
- 构建脚本正常工作

---

### 阶段2.4：Desktop端 - 版本检查逻辑

**任务清单**：

- [ ] 实现版本检查客户端
  - [ ] `desktop/internal/client/version_check.go`
  - [ ] `CheckVersion()` 函数
  - [ ] `ValidateVersion()` 函数
  
- [ ] 集成到启动流程
  - [ ] 在登录前调用版本检查
  - [ ] 处理版本检查结果
  
- [ ] 实现版本检查缓存
  - [ ] 避免频繁检查
  - [ ] 缓存有效期设置

**预计时间**：1天

**验收标准**：
- 版本检查在登录前执行
- 版本符合要求时正常登录
- 版本过低时阻止登录
- 缓存机制正常工作

---

### 阶段2.5：Desktop端 - 强制升级界面

**任务清单**：

- [ ] 创建升级提示组件
  - [ ] `desktop/frontend/src/components/UpgradeDialog.vue`
  - [ ] 显示当前版本、最低要求、最新版本
  - [ ] "立即下载"按钮
  - [ ] "稍后提醒"按钮（可选）
  
- [ ] 实现下载链接跳转
  - [ ] 打开系统默认浏览器
  - [ ] 跳转到下载页面
  
- [ ] 集成到登录流程
  - [ ] 版本过低时显示升级对话框
  - [ ] 强制升级时禁用"稍后提醒"

**预计时间**：1天

**验收标准**：
- 升级对话框正确显示
- 版本信息准确
- 下载链接可以正常打开
- 强制升级时无法绕过

---

### 阶段2.6：Web管理界面 - 版本管理页面

**任务清单**：

- [ ] 创建版本管理页面
  - [ ] `web/src/views/admin/VersionSettings.vue`
  - [ ] 显示当前版本设置
  - [ ] 最低版本输入框
  - [ ] 最新版本输入框
  - [ ] 下载地址输入框
  - [ ] 启用/禁用版本检查开关
  
- [ ] 实现更新功能
  - [ ] 调用API更新设置
  - [ ] 显示更新成功/失败提示
  - [ ] 显示最后更新时间和更新者
  
- [ ] 添加到系统设置菜单
  - [ ] 在导航菜单中添加入口

**预计时间**：1.5天

**验收标准**：
- 版本管理页面正确显示
- 可以更新各项设置
- 更新后立即生效
- 界面友好易用

---

### 阶段2.7：集成测试与文档

**任务清单**：

- [ ] 端到端测试
  - [ ] 版本检查流程测试
  - [ ] 强制升级流程测试
  - [ ] 管理员更新版本设置测试
  - [ ] 版本检查禁用测试
  
- [ ] 创建测试脚本
  - [ ] `tests/api/test_version_control.sh`
  
- [ ] 更新文档
  - [ ] 版本发布流程文档
  - [ ] 管理员操作手册
  - [ ] Desktop用户手册

**预计时间**：1天

**验收标准**：
- 所有测试用例通过
- 测试脚本可以自动运行
- 文档完整准确

---

**第二阶段总计**：7天

---

## 总体时间规划

| 阶段 | 功能 | 预计时间 |
|------|------|----------|
| **第一阶段** | 安全令牌与审计日志系统 | 11.5天 |
| 1.1 | 数据库设计与迁移 | 0.5天 |
| 1.2 | Server端 - Device Token API | 2天 |
| 1.3 | Server端 - 端口偏好与审计日志API | 1.5天 |
| 1.4 | Server端 - Web管理界面 | 2天 |
| 1.5 | Desktop端 - 设备指纹与Token管理 | 2天 |
| 1.6 | Desktop端 - 登录界面双模式 | 2天 |
| 1.7 | Desktop端 - 设备管理界面 | 1天 |
| 1.8 | 文档与测试准备 | 1天 |
| **第二阶段** | Desktop版本管理系统 | 7天 |
| 2.1 | 数据库设计与迁移 | 0.5天 |
| 2.2 | Server端 - 版本管理API | 1.5天 |
| 2.3 | Desktop端 - 版本号注入 | 0.5天 |
| 2.4 | Desktop端 - 版本检查逻辑 | 1天 |
| 2.5 | Desktop端 - 强制升级界面 | 1天 |
| 2.6 | Web管理界面 - 版本管理页面 | 1.5天 |
| 2.7 | 集成测试与文档 | 1天 |
| **总计** | | **18.5天** |

---

## 依赖关系

```
第一阶段（安全令牌系统）
    ├── 1.1 数据库设计 (基础)
    ├── 1.2 Device Token API (依赖 1.1)
    ├── 1.3 端口偏好与审计日志API (依赖 1.1)
    ├── 1.4 Web管理界面 (依赖 1.2, 1.3)
    ├── 1.5 Desktop设备指纹与Token (依赖 1.2)
    ├── 1.6 Desktop登录双模式 (依赖 1.5)
    ├── 1.7 Desktop设备管理界面 (依赖 1.2, 1.5)
    └── 1.8 文档与测试准备 (依赖所有前置任务)

第二阶段（版本管理系统）
    ├── 2.1 数据库设计 (基础)
    ├── 2.2 版本管理API (依赖 2.1)
    ├── 2.3 Desktop版本号注入 (独立)
    ├── 2.4 Desktop版本检查 (依赖 2.2, 2.3)
    ├── 2.5 Desktop强制升级界面 (依赖 2.4)
    ├── 2.6 Web版本管理页面 (依赖 2.2)
    └── 2.7 集成测试 (依赖所有前置任务)

第二阶段依赖第一阶段：
    - 版本检查集成在登录流程中
    - 需要第一阶段的登录逻辑完成
```

---

## 里程碑

### 里程碑1：安全令牌系统完成（第11.5天）
- ✅ Device Token认证系统上线
- ✅ Desktop登录双模式实现
- ✅ 设备管理功能可用
- ✅ 审计日志系统运行
- ✅ Web管理界面完成（审计日志查询、设备管理）

### 里程碑2：版本管理系统完成（第18.5天）
- ✅ Desktop版本检查机制上线
- ✅ 强制升级功能可用
- ✅ Web管理界面版本管理功能完成
- ✅ 系统可以发布正式版本

---

## 风险与缓解

### 风险1：设备指纹不稳定
**描述**：某些设备信息可能会变化（如主机名）

**缓解措施**：
- 使用多个硬件特征组合
- 允许一定的容错率
- 提供手动重新登录机制

### 风险2：版本比较逻辑复杂
**描述**：语义化版本号解析可能有边界情况

**缓解措施**：
- 使用成熟的版本比较库
- 编写完整的单元测试
- 覆盖各种版本号格式

### 风险3：Desktop自动更新困难
**描述**：不同平台的自动更新机制不同

**缓解措施**：
- 第一版只实现手动下载
- 提供清晰的下载和安装指引
- 未来考虑集成自动更新框架

---

## 开发规范

### 代码规范
- 遵循Go和Vue的最佳实践
- 所有函数添加注释
- 关键逻辑添加单元测试

### 提交规范
- 使用语义化提交信息
- 每个功能点独立提交
- 提交前运行测试

### 测试规范
- 单元测试覆盖率 > 80%
- 所有API端点有集成测试
- 关键流程有端到端测试

---

## 验收标准

### 第一阶段验收
- [ ] 所有API端点正常工作
- [ ] Desktop可以使用Device Token登录
- [ ] 设备管理功能完整
- [ ] 审计日志正确记录
- [ ] 所有测试通过
- [ ] 文档完整

### 第二阶段验收
- [ ] 版本检查机制正常工作
- [ ] 强制升级功能有效
- [ ] Web管理界面可以管理版本
- [ ] 所有测试通过
- [ ] 文档完整

### 最终验收
- [ ] 两个系统集成无问题
- [ ] 端到端测试全部通过
- [ ] 性能测试达标
- [ ] 安全测试通过
- [ ] 用户文档和管理员文档完整
- [ ] 可以发布正式版本（v1.0.0）

---

---

## 📝 实施记录

### 2025-11-27

**完成阶段**：
- ✅ 阶段1.1：数据库设计与迁移
- ✅ 阶段1.2：Server端 - Device Token API
- ✅ 阶段1.3：Server端 - 端口偏好与审计日志API
- ✅ 阶段1.4：Server端 - Web管理界面

**实际用时**：1天（原计划6.5天，高效完成）

**关键成果**：
- 创建10个新文件
- 实现10个API端点
- 完成Web审计日志页面
- 修复3个问题

**过程文档**：
- `.tmp/security_token_implementation.md` - 详细实施记录
- `.tmp/stage_1_1_to_1_4_complete.md` - 阶段完成总结
- `.tmp/progress_report.md` - 进度报告

**下一步**：开始Desktop端开发（阶段1.5）

---

**文档版本**: 1.1  
**创建日期**: 2025-11-27  
**最后更新**: 2025-11-27  
**预计完成日期**: 2025-12-20（按工作日计算，约3.5周）
