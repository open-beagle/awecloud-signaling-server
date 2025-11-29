# AWECloud-Signaling 开发计划

> **重要文档**: 本文档是项目开发的核心指导文档，完成任务后必须更新进度。

## 项目概述

基于 FRP 的内网穿透信令服务系统，包含 Server 端、Agent 端和 Desktop 客户端应用。

**技术栈**

- 后端：Go + Gin + gRPC + SQLite + FRP
- 前端：Vue 3 + TypeScript
- Desktop：Wails (Go + Vue 3)
- 协议：gRPC (管理通道) + WebSocket (信令通道) + STCP (数据隧道)

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

#### Week 2: gRPC 服务和 FRP Server 集成

**状态**: 待开始

**任务清单**

1. **API 开发** ✅（已在 Week 1 完成）

   - [x] Agent 管理 API
   - [x] Client 管理 API
   - [x] STCP 实例管理 API
   - [x] Client 端 API

2. **gRPC 服务实现** 🔄

   - [ ] 定义 Protocol Buffers（参考 `docs/design_api.md`）
   - [ ] 实现 AgentService（注册、心跳、指令流、状态上报）
   - [ ] 实现 ClientService（认证、服务查询、连接服务）
   - [ ] 实现 gRPC 认证中间件
   - [ ] Server-Web 与 Server-FRP 内部通信接口

3. **FRP Server 集成** 🔄

   - [ ] 集成 FRP Server 核心代码
   - [ ] 配置 WebSocket 传输协议
   - [ ] 实现 WebSocket 认证（Agent 和 Client）
   - [ ] 实现 STCP 隧道协调逻辑
   - [ ] Server-FRP 接收 Server-Web 控制指令

**交付物**

- 完整的 gRPC 服务（Server-Web）
- 集成 FRP Server 的可运行程序（Server-FRP）
- Server-Web 和 Server-FRP 内部通信机制
- gRPC 和 WebSocket 连接测试通过

---

### 第二阶段：Agent 端实现（1 周）

#### Week 3: Agent 端开发

**任务清单**

1. **Agent 基础框架**

   - [ ] 创建 Agent 项目结构
   - [ ] 配置文件解析（agent.toml）
   - [ ] 日志系统搭建
   - [ ] 双线程架构设计（Agent-Web 线程 + Agent-FRP 线程）

2. **Agent-Web 线程（gRPC 客户端）**

   - [ ] 实现 gRPC 客户端连接到 Server-Web 线程
   - [ ] 实现 Agent 注册和认证
   - [ ] 实现心跳机制
   - [ ] 实现接收 Server 指令（双向流）
   - [ ] 实现状态上报
   - [ ] 实现与 Agent-FRP 线程的进程内通信（Go channel）

3. **Agent-FRP 线程（FRP 客户端）**

   - [ ] 集成 FRP Client 核心代码
   - [ ] 配置 WebSocket 连接到 Server-FRP 线程
   - [ ] 实现 WebSocket 认证
   - [ ] 接收 Agent-Web 线程的控制指令（进程内通信）

4. **动态代理管理**

   - [ ] 实现动态创建 STCP Proxy
   - [ ] 实现动态删除 STCP Proxy
   - [ ] 代理状态管理和上报

5. **Docker 化**

   - [ ] 编写 Dockerfile
   - [ ] 支持环境变量配置
   - [ ] 测试容器部署

**交付物**

- 可运行的 Agent 程序（单一进程，包含两个工作线程）
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
   - [ ] Desktop-Web 和 Desktop-FRP 模块架构

2. **Desktop-Web 模块（gRPC 客户端）**

   - [ ] 实现 gRPC 客户端连接到 Server-Web
   - [ ] 实现 Client 认证
   - [ ] 实现获取可访问服务列表
   - [ ] 实现连接服务（获取连接信息）
   - [ ] Desktop-Web 与 Desktop-FRP 内部通信接口

3. **登录功能**

   - [ ] 登录界面 UI
   - [ ] Server 地址配置
   - [ ] Client 认证实现
   - [ ] 凭证本地存储

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

1. **Desktop-FRP 模块（FRP 客户端）**

   - [ ] 集成 FRP Client 核心代码
   - [ ] 配置 WebSocket 连接到 Server-FRP
   - [ ] 实现 WebSocket 认证
   - [ ] 实现 Visitor 模式
   - [ ] 接收 Desktop-Web 的控制指令

2. **连接功能**

   - [ ] 实现连接服务逻辑
   - [ ] 动态创建 STCP Visitor
   - [ ] 本地端口配置
   - [ ] 连接状态显示

3. **断开功能**

   - [ ] 实现断开服务逻辑
   - [ ] 清理 Visitor 资源
   - [ ] 状态更新

4. **配置管理**

   - [ ] 保存服务配置
   - [ ] 自动连接功能
   - [ ] 配置导入导出

5. **打包和发布**

   - [ ] Windows 打包配置
   - [ ] 生成安装程序
   - [ ] 测试安装和运行

**交付物**

- 完整功能的 Desktop 应用（包含 Web 和 FRP 两个模块）
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

**Server 进程**（单一进程，两个服务线程）

- ✅ 管理员登录（单用户）
- ✅ Agent CRUD
- ✅ Client CRUD
- ✅ STCP 实例 CRUD
- ✅ 权限授权
- ✅ Server-Web 线程：gRPC 服务（Agent 和 Client）
- ✅ Server-FRP 线程：WebSocket 信令服务
- ✅ 两个线程之间进程内通信

**Agent 进程**（单一进程，两个工作线程）

- ✅ Agent-Web 线程：gRPC 连接 Server-Web 线程
- ✅ Agent-FRP 线程：WebSocket 连接 Server-FRP 线程
- ✅ 两个线程之间进程内通信
- ✅ 动态创建/删除 STCP Proxy
- ✅ 心跳机制
- ✅ 状态上报

**Web 管理界面**

- ✅ 所有管理功能的 UI

**Desktop 进程**（单一进程，两个工作线程）

- ✅ Desktop-Web 线程：gRPC 连接 Server-Web 线程
- ✅ Desktop-FRP 线程：WebSocket 连接 Server-FRP 线程
- ✅ 两个线程之间进程内通信
- ✅ 登录和认证
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

### 1. 双线程架构设计

**难点**

- Server、Agent、Desktop 都是单一进程，需要同时支持管理通道（gRPC）和信令通道（WebSocket）
- 两个工作线程之间需要进程内通信和协调

**解决方案**

- 每个进程启动两个工作线程（goroutine）
- Server 进程：Server-Web 线程（端口 8080）和 Server-FRP 线程（端口 7000）
- Agent 进程：Agent-Web 线程和 Agent-FRP 线程
- Desktop 进程：Desktop-Web 线程和 Desktop-FRP 线程
- 使用 Go channel 或接口调用实现进程内通信

### 2. gRPC 双向流通信

**难点**

- Server 需要主动推送指令给 Agent
- 需要维护 Agent 的 gRPC 流连接

**解决方案**

- 使用 gRPC 双向流（bidirectional streaming）
- Agent 建立长连接，持续接收 Server 指令
- 实现指令队列和响应机制
- 实现心跳机制检测连接状态

### 3. WebSocket 认证和路由

**难点**

- Agent-FRP 和 Desktop-FRP 都连接到同一个 WebSocket 端点
- 需要区分不同类型的客户端

**解决方案**

- 连接时通过查询参数传递类型和认证信息
- Agent-FRP: `wss://domain.com/ws?type=agent&agent_id=1&token=xxx`
- Desktop-FRP: `wss://domain.com/ws?type=client&session_token=xxx`
- Server-FRP 根据类型进行不同的处理

### 4. STCP 隧道协调

**难点**

- Desktop-FRP 需要通过 Server-FRP 找到对应的 Agent-FRP
- 需要协调建立点对点的 STCP 隧道

**解决方案**

- Server-FRP 维护 Agent-FRP 和 Desktop-FRP 的连接映射
- Desktop-FRP 请求连接时，Server-FRP 通知对应的 Agent-FRP
- 使用 FRP 的 STCP 协议建立加密隧道
- 隧道建立后，Server-FRP 只负责信令，数据直接在两端传输

### 5. Desktop 应用的 FRP 集成

**难点**

- Wails 应用中集成 FRP Client
- 动态管理多个 Visitor

**解决方案**

- 将 FRP Client 作为 Go 库集成
- 实现 Visitor 的动态添加/删除接口
- 使用 goroutine 管理每个 Visitor 的生命周期

### 6. 跨平台打包

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

- [x] 所有 RESTful API 可用（Week 1 提前完成）
- [ ] gRPC 服务实现完成（Server-Web 线程）
- [ ] FRP Server 集成完成（Server-FRP 线程）
- [ ] Server-Web 线程 和 Server-FRP 线程 进程内通信完成
- [x] 可通过 curl/Postman 测试 RESTful API
- [ ] 可通过 grpcurl 测试 gRPC API

### M2: Agent 端完成（Week 3 结束）

**状态**: 未开始

- [ ] Agent-Web 线程 可通过 gRPC 连接 Server-Web 线程
- [ ] Agent-FRP 线程 可通过 WebSocket 连接 Server-FRP 线程
- [ ] 两个线程之间进程内通信正常
- [ ] 可动态创建/删除 STCP 代理
- [ ] 心跳和状态上报正常
- [ ] Docker 镜像可用

### M3: Web 界面完成（Week 5 结束）

**状态**: 未开始

- [ ] 所有管理功能可用
- [ ] 集成到 Server 程序

### M4: Desktop 应用完成（Week 7 结束）

**状态**: 未开始

- [ ] Desktop-Web 线程 可通过 gRPC 连接 Server-Web 线程
- [ ] Desktop-FRP 线程 可通过 WebSocket 连接 Server-FRP 线程
- [ ] 两个线程之间进程内通信正常
- [ ] 完整功能可用（登录、服务列表、连接/断开）
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

- `docs/design.md` - 核心架构设计
- `docs/design_database.md` - 数据库详细设计
- `docs/design_api.md` - API 详细设计（RESTful + gRPC + WebSocket）
- `docs/design_deployment.md` - 部署方案
- `docs/progress.md` - 进度跟踪（每日更新）
- `docs/debug.md` - 调试规范
- `docs/README.md` - 文档索引
- `README.md` - 项目说明

---

**文档版本**: v1.1  
**创建日期**: 2025-11-25  
**最后更新**: 2025-11-25  
**状态**: 进行中（Week 1 已完成）
