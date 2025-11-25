# AWECloud-Signaling 开发计划

> **重要文档**: 本文档是项目开发的核心指导文档，完成任务后必须更新进度。

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

---

## 项目概述

基于 FRP 的内网穿透信令服务系统，包含 Server 端、Agent 端和 Desktop 客户端应用。

**技术栈**

- 后端：Go + Gin + SQLite + FRP
- 前端：Vue 3 + TypeScript
- Desktop：Wails (Go + Vue 3)
- 协议：WSS (WebSocket Secure)

---

## 开发阶段划分

### 第一阶段：Server 端核心功能（2 周）

#### Week 1: 基础框架和数据层 ✅

**状态**: 已完成（2025-11-25）

**任务清单**

1. **项目初始化** ✅

   - [x] 创建 Go 项目结构
   - [x] 初始化 go.mod，引入依赖（Gin、GORM、FRP 等）
   - [x] 配置文件解析（server.toml）
   - [x] 日志系统搭建

2. **数据库设计与实现** ✅

   - [x] 设计 SQLite 数据库 schema
   - [x] 实现数据模型（Admin、Agent、Client、STCPInstance、ClientPermission）
   - [x] 实现数据库初始化和迁移
   - [x] 编写基础 CRUD 操作

3. **管理员认证系统** ✅

   - [x] 实现 JWT 生成和验证
   - [x] 实现管理员登录 API
   - [x] 实现认证中间件
   - [x] 创建默认管理员账号

4. **提前完成 Week 2 的 API 任务** ✅
   - [x] Agent 管理 API（完整 CRUD）
   - [x] Client 管理 API（完整 CRUD）
   - [x] STCP 实例管理 API（完整 CRUD）
   - [x] Client 端 API（认证和服务查询）

**交付物** ✅

- 可运行的 Server 基础框架
- 完整的数据库 schema
- 管理员登录功能
- 所有 RESTful API
- API 测试脚本
- Docker 配置文件

**详细内容**: 已清理，保留简要信息

---

#### Week 2: FRP Server 集成

**状态**: 待开始

**任务清单**

1. **API 开发** ✅（已在 Week 1 完成）

   - [x] Agent 管理 API
   - [x] Client 管理 API
   - [x] STCP 实例管理 API
   - [x] Client 端 API

2. **FRP Server 集成** 🔄

   - [ ] 集成 FRP Server 核心代码
   - [ ] 配置 WSS 传输协议
   - [ ] 实现 TLS 证书加载
   - [ ] 实现 Server 与 Agent 的通信协议
   - [ ] 实现动态通知 Agent 创建/删除 STCP 实例

**交付物**

- 集成 FRP Server 的可运行程序
- Server 与 Agent 通信协议文档
- WSS 连接测试通过

---

### 第二阶段：Agent 端实现（1 周）

#### Week 3: Agent 端开发

**任务清单**

1. **Agent 基础框架**

   - [ ] 创建 Agent 项目结构
   - [ ] 配置文件解析（agent.toml）
   - [ ] 日志系统搭建

2. **FRP Client 集成**

   - [ ] 集成 FRP Client 核心代码
   - [ ] 配置 WSS 连接
   - [ ] 实现与 Server 的连接和认证

3. **消息处理**

   - [ ] 实现消息接收和解析
   - [ ] 处理创建 STCP 实例请求
   - [ ] 处理删除 STCP 实例请求
   - [ ] 实现心跳机制

4. **动态代理管理**

   - [ ] 实现动态创建 STCP Proxy
   - [ ] 实现动态删除 STCP Proxy
   - [ ] 代理状态管理

5. **Docker 化**

   - [ ] 编写 Dockerfile
   - [ ] 支持环境变量配置
   - [ ] 测试容器部署

**交付物**

- 可运行的 Agent 程序
- Docker 镜像
- 部署文档

---

### 第三阶段：Web 管理界面（1.5 周）

#### Week 4-5: 前端开发

**任务清单**

1. **项目初始化**

   - [ ] 创建 Vue 3 + TypeScript 项目
   - [ ] 配置路由（Vue Router）
   - [ ] 配置状态管理（Pinia）
   - [ ] 配置 HTTP 客户端（Axios）
   - [ ] UI 组件库选择和配置（Element Plus / Ant Design Vue）

2. **登录页面**

   - [ ] 登录表单 UI
   - [ ] 登录逻辑实现
   - [ ] Token 存储和管理
   - [ ] 路由守卫

3. **Agent 管理页面**

   - [ ] Agent 列表展示
   - [ ] 创建 Agent 对话框
   - [ ] 显示 Agent 状态（在线/离线）
   - [ ] 删除 Agent 确认
   - [ ] 重新生成 Token 功能

4. **Client 管理页面**

   - [ ] Client 列表展示
   - [ ] 创建 Client 对话框
   - [ ] 启用/禁用 Client
   - [ ] 删除 Client 确认

5. **STCP 实例管理页面**

   - [ ] 实例列表展示
   - [ ] 创建实例表单（选择 Agent、配置服务）
   - [ ] 实例状态显示
   - [ ] 删除实例确认
   - [ ] 授权管理（选择 Client 授权访问）

6. **整体布局**
   - [ ] 顶部导航栏
   - [ ] 侧边菜单
   - [ ] 响应式布局

**交付物**

- 完整的 Web 管理界面
- 打包后的静态文件
- 集成到 Server 程序

---

### 第四阶段：Desktop 应用（2 周）

#### Week 6: Desktop 基础功能

**任务清单**

1. **Wails 项目初始化**

   - [ ] 创建 Wails 项目
   - [ ] 配置前端（Vue 3 + TypeScript）
   - [ ] 配置 Go 后端
   - [ ] 配置构建脚本

2. **登录功能**

   - [ ] 登录界面 UI
   - [ ] Server 地址配置
   - [ ] Client 认证实现
   - [ ] 凭证本地存储

3. **FRP Client 集成**

   - [ ] 集成 FRP Client 库
   - [ ] 配置 WSS 连接
   - [ ] 实现 Visitor 模式

4. **服务列表功能**
   - [ ] 获取可访问服务列表
   - [ ] 服务列表 UI 展示
   - [ ] 服务状态管理

**交付物**

- 可运行的 Desktop 应用（开发版）
- 登录和服务列表功能

---

#### Week 7: Desktop 连接功能和打包

**任务清单**

1. **连接功能**

   - [ ] 实现连接服务逻辑
   - [ ] 动态创建 STCP Visitor
   - [ ] 本地端口配置
   - [ ] 连接状态显示

2. **断开功能**

   - [ ] 实现断开服务逻辑
   - [ ] 清理 Visitor 资源
   - [ ] 状态更新

3. **配置管理**

   - [ ] 保存服务配置
   - [ ] 自动连接功能
   - [ ] 配置导入导出

4. **打包和发布**
   - [ ] Windows 打包配置
   - [ ] 生成安装程序
   - [ ] 测试安装和运行

**交付物**

- 完整功能的 Desktop 应用
- Windows 安装包
- 用户使用文档

---

### 第五阶段：测试和优化（1 周）

#### Week 8: 集成测试和优化

**任务清单**

1. **集成测试**

   - [ ] Server 端 API 测试
   - [ ] Agent 连接测试
   - [ ] Desktop 应用连接测试
   - [ ] 端到端流程测试

2. **性能优化**

   - [ ] 数据库查询优化
   - [ ] 连接池配置
   - [ ] 内存使用优化

3. **安全加固**

   - [ ] 输入验证
   - [ ] SQL 注入防护
   - [ ] XSS 防护
   - [ ] Token 过期处理

4. **文档完善**

   - [ ] 部署文档
   - [ ] 用户手册
   - [ ] API 文档
   - [ ] 故障排查指南

5. **Docker Compose 配置**
   - [ ] 编写 docker-compose.yml
   - [ ] 测试一键部署
   - [ ] 编写部署脚本

**交付物**

- 测试报告
- 完整文档
- 生产就绪的部署包

---

## 最小可行产品（MVP）范围

### MVP 包含功能

**Server 端**

- ✅ 管理员登录（单用户）
- ✅ Agent CRUD
- ✅ Client CRUD
- ✅ STCP 实例 CRUD
- ✅ 权限授权
- ✅ WSS 协议支持

**Agent 端**

- ✅ WSS 连接 Server
- ✅ 动态创建/删除 STCP Proxy
- ✅ 心跳机制

**Web 管理界面**

- ✅ 所有管理功能的 UI

**Desktop 应用**

- ✅ 登录
- ✅ 服务列表
- ✅ 连接/断开服务
- ✅ Windows 支持

### MVP 不包含功能（后续版本）

- ❌ 多管理员支持
- ❌ 详细监控统计
- ❌ 日志查询界面
- ❌ XTCP 支持
- ❌ macOS/Linux Desktop 版本
- ❌ 移动端应用

---

## 技术难点和解决方案

### 1. WSS 协议集成

**难点**

- FRP 默认使用 TCP，需要改造为 WSS
- 证书管理和配置

**解决方案**

- 使用 FRP 的 transport 配置，设置 protocol 为"wss"
- 提供自签名证书生成脚本
- 支持 Let's Encrypt 证书

### 2. Server 与 Agent 动态通信

**难点**

- Server 需要主动通知 Agent 创建/删除代理
- 需要维护 Agent 连接状态

**解决方案**

- 利用 FRP 的控制连接发送自定义消息
- 设计简单的消息协议（JSON 格式）
- 实现心跳机制检测 Agent 在线状态

### 3. Desktop 应用的 FRP 集成

**难点**

- Wails 应用中集成 FRP Client
- 动态管理多个 Visitor

**解决方案**

- 将 FRP Client 作为 Go 库集成
- 实现 Visitor 的动态添加/删除接口
- 使用 goroutine 管理每个 Visitor 的生命周期

### 4. 跨平台打包

**难点**

- Desktop 应用需要支持多平台
- 打包配置复杂

**解决方案**

- MVP 阶段只支持 Windows
- 使用 Wails 的打包工具
- 后续版本逐步支持 macOS 和 Linux

---

## 开发资源分配

### 人员配置（建议）

- **后端开发**: 1 人（Server + Agent）
- **前端开发**: 1 人（Web + Desktop 前端）
- **全栈开发**: 1 人（Desktop 后端 + 集成测试）

### 时间估算

- **总工期**: 8 周
- **核心功能**: 5 周
- **测试优化**: 1 周
- **文档和发布**: 1 周
- **缓冲时间**: 1 周

---

## 里程碑

### M1: Server 端完成（Week 2 结束）

**状态**: 部分完成

- [x] 所有 API 可用（Week 1 提前完成）
- [ ] FRP Server 集成完成
- [x] 可通过 curl/Postman 测试

### M2: Agent 端完成（Week 3 结束）

**状态**: 未开始

- [ ] Agent 可连接 Server
- [ ] 可动态创建 STCP 代理
- [ ] Docker 镜像可用

### M3: Web 界面完成（Week 5 结束）

**状态**: 未开始

- [ ] 所有管理功能可用
- [ ] 集成到 Server 程序

### M4: Desktop 应用完成（Week 7 结束）

**状态**: 未开始

- [ ] 完整功能可用
- [ ] Windows 安装包可用

### M5: MVP 发布（Week 8 结束）

**状态**: 未开始

- [ ] 所有功能测试通过
- [ ] 文档完善
- 可正式部署使用

---

## 风险和应对

### 风险 1: FRP 集成复杂度超预期

**应对措施**

- 提前进行技术预研
- 准备降级方案（使用 FRP 原生配置文件）

### 风险 2: WSS 穿透性不如预期

**应对措施**

- 同时支持 TCP 和 WSS 两种模式
- 提供网络诊断工具

### 风险 3: Desktop 应用打包问题

**应对措施**

- 提前测试 Wails 打包流程
- 准备备选方案（Electron）

### 风险 4: 时间延期

**应对措施**

- 严格控制 MVP 范围
- 非核心功能延后到后续版本

---

## 后续版本规划

### v1.1（MVP 后 1-2 个月）

- macOS 和 Linux Desktop 版本
- 详细的监控统计
- 日志查询界面

### v1.2（MVP 后 3-4 个月）

- 多管理员支持
- XTCP 支持
- 移动端应用（iOS/Android）

### v2.0（MVP 后 6 个月）

- 服务自动发现
- 负载均衡
- 高可用部署方案

---

## 快速开始（开发环境）

### 1. 克隆代码

```bash
git clone https://github.com/your-org/awecloud-signaling.git
cd awecloud-signaling
```

### 2. 启动 Server

```bash
cd cmd/server
go run main.go -c ../../config/server.toml
```

### 3. 启动 Agent

```bash
cd cmd/agent
go run main.go -c ../../config/agent.toml
```

### 4. 启动 Web 开发服务器

```bash
cd web
npm install
npm run dev
```

### 5. 启动 Desktop 应用

```bash
cd ../awecloud-signaling-desktop
wails dev
```

---

## 相关文档

- `docs/design.md` - 设计文档
- `docs/progress.md` - 进度跟踪（每日更新）
- `docs/debug.md` - 调试规范
- `docs/README.md` - 文档索引
- `README.md` - 项目说明

---

**文档版本**: v1.1  
**创建日期**: 2025-11-25  
**最后更新**: 2025-11-25  
**状态**: 进行中（Week 1已完成）
