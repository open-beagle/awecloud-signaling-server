# 项目进度跟踪

> 本文档记录项目的整体进度，已完成的任务只保留简要信息，详细内容已归档。

## 当前状态

- **当前阶段**: MVP 已完成 ✅
- **当前周次**: Week 1-7 已完成
- **总体进度**: 100% MVP 完成
- **当前任务**: 准备下一阶段功能开发
- **下一步工作**:
  1. **版本控制功能** - Desktop 版本管理和强制升级
  2. **隧道Token安全** - 每个Client独立的隧道Token（待设计）
- **重要里程碑**: 
  - ✅ Server、Agent、Desktop 核心功能完成
  - ✅ 完整的通信链路验证通过
  - ✅ Device Token 和审计日志系统实现
- **设计文档**: 已拆分为多个独立文档

---

## 第一阶段：Server 端核心功能（Week 1-2）

### Week 1: 基础框架和数据层 ✅

**完成时间**: 2025-11-25

**完成任务**:

- ✅ 项目初始化（Go 模块、依赖管理）
- ✅ 数据库设计与实现（5 个数据模型）
- ✅ 管理员认证系统（JWT）
- ✅ 所有 RESTful API（提前完成 Week 2 任务）
  - Agent 管理 API
  - Client 管理 API
  - STCP 实例管理 API
  - Client 端 API

**交付物**:

- ✅ 可运行的 Server 程序（bin/server）
- ✅ 完整的数据库 schema
- ✅ 所有 API 端点
- ✅ API 测试脚本
- ✅ Docker 配置文件

**详细内容**: 已清理，保留简要信息

---

### Week 2: gRPC 服务和 FRP Server 集成 🔄

**状态**: 进行中

**当前任务**: 实现 gRPC 服务

**待完成任务**:

- [x] gRPC 服务实现（Server-Web 线程）✅
  - [x] 定义 Protocol Buffers ✅
  - [x] 创建代码生成脚本 ✅
  - [x] 实现 AgentService（注册、心跳、指令流、状态上报）✅
  - [x] 实现 ClientService（认证、服务查询、连接服务）✅
  - [x] 实现进程内通信（RESTful API → gRPC → Agent）✅
  - [x] 测试 gRPC 服务 ✅
  - [ ] 实现 gRPC 认证中间件（可选优化）
- [x] FRP Server 集成（Server-FRP 线程）✅
  - [x] 集成 FRP Server 核心代码 ✅
  - [x] 实现连接管理（Agent 和 Desktop）✅
  - [x] 实现代理管理（STCP 代理）✅
  - [x] 实现连接监控 ✅
  - [x] 提供查询接口 ✅
  - [ ] 配置 WebSocket 传输协议（FRP 自动支持）
  - [ ] 实现 WebSocket 认证（Agent 和 Desktop）
  - [ ] 实现 STCP 隧道协调逻辑
  - [ ] 接收 Server-Web 线程的控制指令（进程内通信）

**预期交付物**:

- 完整的 gRPC 服务（Server-Web 线程）
- 集成 FRP Server 的可运行程序（Server-FRP 线程）
- Server-Web 线程和 Server-FRP 线程进程内通信机制
- gRPC 和 WebSocket 连接测试通过

---

## 第二阶段：Agent 进程实现（Week 3）

**状态**: 进行中 🔄

**已完成任务**:

- [x] Agent 基础框架（单一进程）✅
  - 创建 `internal/agent/agent.go`
  - 创建 `config/agent.toml`
- [x] Agent-Web 线程（gRPC 客户端）✅
  - [x] 连接 Server-Web 线程（端口 8081）
  - [x] 注册和认证
  - [x] 心跳机制（30 秒间隔）
  - [x] 接收 Server 指令（双向流）
  - [x] 命令处理框架（CREATE_STCP, DELETE_STCP）
- [x] 构建测试通过 ✅

**待完成任务**:

- [x] Agent-FRP 线程（FRP 客户端）✅
  - [x] 连接 Server-FRP 线程（端口 7000）✅
  - [x] 接收 Agent-Web 线程控制指令（进程内通信）✅
  - [x] 动态代理管理（创建/删除 STCP 代理）✅
  - [x] 自动重启机制（配置变更时）✅
  - [x] 错误恢复机制（连接失败自动重试）✅
- [ ] 测试完整流程（需要 Server-FRP 线程运行）
- [ ] Docker 化

---

## 第三阶段：Web 管理界面（Week 4-5）

**状态**: 未开始

**待完成任务**:

- [ ] 项目初始化（Vue 3 + TypeScript）
- [ ] 登录页面
- [ ] Agent 管理页面
- [ ] Client 管理页面
- [ ] STCP 实例管理页面
- [ ] 整体布局

---

## 第四阶段：Desktop 进程（Week 6-7）

**状态**: 未开始

**待完成任务**:

- [ ] Wails 项目初始化（单一进程，双线程架构）
- [ ] Desktop-Web 线程（gRPC 客户端）
  - [ ] 连接 Server-Web 线程
  - [ ] Client 认证
  - [ ] 获取服务列表
  - [ ] 连接服务（获取连接信息）
  - [ ] 与 Desktop-FRP 线程进程内通信（Go channel）
- [ ] Desktop-FRP 线程（FRP 客户端）
  - [ ] WebSocket 连接 Server-FRP 线程
  - [ ] WebSocket 认证
  - [ ] Visitor 模式
  - [ ] 接收 Desktop-Web 线程控制指令（进程内通信）
- [ ] 登录功能
- [ ] 服务列表功能
- [ ] 连接/断开功能
- [ ] 配置管理
- [ ] 打包和发布

---

## 第五阶段：测试和优化（Week 8）

**状态**: 未开始

**待完成任务**:

- [ ] 集成测试
- [ ] 性能优化
- [ ] 安全加固
- [ ] 文档完善
- [ ] Docker Compose 配置

---

## 里程碑

- [x] **里程碑 1: Server 开发完成** ✅

  - [x] RESTful API 完整实现 ✅
  - [x] gRPC 服务实现（Server-Web 线程）✅
  - [x] FRP Server 集成（Server-FRP 线程）✅
  - [x] 进程内通信机制 ✅
  - [x] API 测试通过 ✅

- [ ] **里程碑 2: Agent 开发完成** 🔄

  - [x] Agent-Web 线程（gRPC 客户端）✅
  - [x] Agent-FRP 线程（FRP 客户端）✅
  - [x] 进程内通信（Go channel）✅
  - [x] 动态代理管理 ✅
  - [ ] **人工联调测试**（待进行）
    - [ ] 启动 Server 和 Agent
    - [ ] Web 界面创建 Agent
    - [ ] Web 界面创建 STCP 实例
    - [ ] 验证 Agent 自动创建 STCP 代理
    - [ ] 验证 Agent 自动删除 STCP 代理
    - [ ] 验证 FRP 连接状态

- [x] **里程碑 3: Desktop 开发完成** ✅

  - [x] Desktop-Web 线程（gRPC 客户端）✅
  - [x] Desktop-FRP 线程（FRP 客户端）✅
  - [x] 进程内通信（Go channel）✅
  - [x] **人工联调测试** ✅
    - [x] Desktop 认证 ✅
    - [x] 获取服务列表 ✅
    - [x] 建立 STCP 隧道 ✅
    - [x] 本地端口访问远程服务 ✅

- [x] **里程碑 4: Web 界面完成** ✅

- [x] **里程碑 5: MVP 发布** ✅

## 下一阶段工作

### 1. 版本控制功能（优先级：高）

**目标**: 实现 Desktop 客户端版本管理和强制升级

**任务**:
- [ ] Server 端版本设置 API
- [ ] Desktop 端版本检查逻辑
- [ ] Web 界面版本管理页面
- [ ] 强制升级提示界面

**参考文档**: `docs/design_version_control.md`

### 2. 隧道Token安全（优先级：高）

**目标**: 每个Client拥有独立的隧道Token，提升安全性

**当前状态**: 
- ❌ 所有Client共享Server配置中的统一Token
- ❌ 登录时返回隧道配置（不安全）
- ❌ 无法按Client粒度控制隧道访问

**设计原则**:
1. **登录时不返回隧道配置** - 无论Secret还是Token登录
2. **每个Client独立Token** - 为每个Client生成唯一的隧道Token
3. **按需获取配置** - 只在连接服务时通过API获取隧道配置

**待设计内容**:
- [ ] 数据库模型变更（Client表添加tunnel_token字段）
- [ ] Token生成和管理机制
- [ ] 获取隧道配置API设计
- [ ] Desktop端按需获取逻辑
- [ ] Token撤销和更新机制

**设计文档**: 待创建 `docs/design_tunnel_token.md`（等待进一步设计）

**临时方案**: 
- 当前使用Server配置中的统一Token
- 所有Client共享同一个Token
- 这是MVP阶段的简化实现，存在安全风险

---

## 风险和问题

### 当前风险

- gRPC 双向流通信实现复杂度待评估
- FRP 集成复杂度待评估
- WebSocket 认证和路由机制需要仔细设计
- STCP 隧道协调逻辑较复杂
- 进程内通信机制设计（Go channel vs 接口调用）

### 已解决问题

- ✅ 架构设计问题：明确为单进程双线程架构
- ✅ 设计文档过大：已拆分为 4 个独立文档
- ✅ 术语混淆：统一使用"进程"和"线程"术语

---

## 更新日志

### 2025-11-26

**Agent-FRP 线程实现完成**:

- ✅ **完成 Agent-FRP 线程实现**
  - 实现 FRPManager（Agent-FRP 线程管理器）
  - 支持动态代理管理（添加/删除 STCP 代理）
  - 实现自动重启机制（配置变更时立即重启）
  - 实现错误恢复机制（连接失败 5 秒后自动重试）
  - 使用 Go channel 实现进程内通信（Agent-Web → Agent-FRP）
  - 命令响应机制（同步等待操作结果）
- ✅ **创建实现文档**
  - `docs/agent_frp_implementation.md` - 详细的实现说明
  - `docs/agent_frp_summary.md` - 快速参考总结
- ✅ **构建测试通过**
- 📊 **Week 3 进度更新**: 60% → 80%

**Server-FRP 线程实现完成**:

- ✅ **完成 Server-FRP 线程实现**
  - 增强 FRPServer（Server-FRP 线程管理器）
  - 实现连接管理（Agent 和 Desktop 连接跟踪）
  - 实现代理管理（STCP 代理状态跟踪）
  - 实现连接监控（30 秒统计一次）
  - 提供查询接口（连接状态、代理状态）
  - 并发安全（使用 RWMutex 保护共享数据）
- ✅ **创建实现文档**
  - `docs/server_frp_implementation.md` - 详细的实现说明
  - 包含架构设计、核心功能、工作流程
  - 包含配置说明、并发安全、性能考虑
  - 包含未来改进方向
- ✅ **构建测试通过**
- 📊 **Week 2 进度更新**: 完成 FRP Server 集成
- ⏭️ **下一步**: 测试完整的 STCP 隧道（Agent ↔ Server ↔ Desktop）

### 2025-11-25

**晚上**:

- 🔍 **代码审查完成**：
  - 逐文件审查已完成的 Go 代码
  - 对比设计文档，发现需要修复的问题
  - 制定修复优先级
- 📋 **修复优先级**：
  - **高优先级（立即修复）**：
    1. 数据库模型修复（表名、字段不一致）
    2. 添加缺失的 client_sessions 表
  - **中优先级（Week 2 任务）**： 3. 添加 gRPC 服务（Protocol Buffers + AgentService + ClientService） 4. FRP Server WebSocket 支持 5. 进程内通信机制
  - **低优先级（可延后）**： 6. 补充 updated_at 等字段
- 🔧 **开始修复工作**：
  - 修复数据库模型不一致问题
- ✅ **数据库模型修复完成**：
  - 重命名表：`client_permissions` → `stcp_access`
  - 新增表：`client_sessions`（会话管理）
  - 字段修复：
    - Client 表：`status`字段改为`enabled`（boolean）
    - 所有表添加`updated_at`字段
    - STCPInstance 表：移除`service_type`和`server_name`，添加`description`
    - STCPAccess 表：字段顺序调整为`stcp_instance_id, client_id`
  - 更新所有相关 API 代码
  - 构建测试通过 ✅
- ✅ **测试规范建立**：
  - 创建 `docs/test.md` 测试规范文档
  - 创建 `tests/` 测试目录结构
  - 实现测试脚本：
    - `tests/common.sh` - 公共函数库
    - `tests/run_all.sh` - 测试运行器
    - `tests/api/test_admin.sh` - 管理员认证测试（3 个用例）
    - `tests/api/test_agent.sh` - Agent 管理测试（4 个用例）
  - 更新 `docs/README.md` 添加测试规范说明
  - 优化测试输出，更简洁谦虚
  - 所有测试通过 ✅ (7/7)
- 📋 **Week 1 总结**：
  - ✅ 数据库模型完全符合设计
  - ✅ RESTful API 完整实现
  - ✅ 测试框架建立
  - ⏭️ 待完成：gRPC 服务、FRP Server WebSocket 支持
- 🚀 **Week 2 任务进展**：
  - ✅ 创建 Protocol Buffers 定义
    - `pkg/proto/agent.proto` - Agent 服务定义
    - `pkg/proto/client.proto` - Client 服务定义
  - ✅ 创建代码生成脚本 `scripts/generate_proto.sh`
  - ✅ 生成 gRPC Go 代码
  - ✅ 实现 gRPC 服务端
    - `internal/server/grpc/agent_service.go` - AgentService 实现
    - `internal/server/grpc/client_service.go` - ClientService 实现
  - ✅ 集成 gRPC 到 Server
    - 添加 grpc 依赖
    - 更新 Server 启动 gRPC 服务（端口 8081）
  - ✅ 构建测试通过
  - ✅ **完成 Server 内部设计文档**
    - 创建 `docs/design_server.md`
    - 详细设计进程内通信机制
    - 绘制核心业务流程图（创建/删除 STCP、Agent 注册、Desktop 连接）
    - 定义通信接口（CommandBus、EventBus）
    - 状态管理和错误处理策略
  - ✅ **实现进程内通信机制（MVP 核心）**
    - 修改 `internal/server/api/stcp.go`
      - 添加 AgentService 依赖注入
      - 创建 STCP 时通过 gRPC 发送 CREATE_STCP 命令
      - 删除 STCP 时通过 gRPC 发送 DELETE_STCP 命令
      - Agent 离线时保存到数据库，等待同步
    - 修改 `internal/server/server.go`
      - 在 setupRouter 中注入 AgentService 到 STCP API
    - ✅ 构建测试通过
  - ⏭️ 下一步：测试完整流程（需要实现 Agent 端）
- 🚀 **开始 Week 3 任务 - Agent 端实现**：
  - ✅ 创建 Agent 基础框架
    - `internal/agent/agent.go` - Agent 主逻辑
    - `config/agent.toml` - Agent 配置文件
  - ✅ 实现 Agent-Web 线程（gRPC 客户端）
    - 连接到 Server gRPC 服务（端口 8081）
    - Agent 注册和认证
    - 心跳机制（30 秒间隔）
    - 双向流接收 Server 指令
    - 命令处理框架（CREATE_STCP, DELETE_STCP）
  - ✅ 构建测试通过（Agent 从 2MB 增加到 11MB）
  - ✅ **测试 gRPC 通信链路**
    - Server → Agent 注册成功
    - Server → Agent 心跳正常
    - 创建 STCP → Server 发送 CREATE_STCP 命令 → Agent 接收处理 ✅
    - 删除 STCP → Server 发送 DELETE_STCP 命令 → Agent 接收处理 ✅
    - 完整的 gRPC 双向流通信验证成功 🎉
  - ⏭️ 下一步：实现 Agent-FRP 线程和动态代理管理

**上午**:

- ✅ 完成 Week 1 所有任务
- ✅ 提前完成 Week 2 的 API 开发任务
- ✅ 创建项目文档规范
- ✅ 完善设计文档：明确 Server 两个端口和路由设计
  - 端口 8080: Web 管理界面（HTTP）- 路由 `/`
  - 端口 7000: FRP 信令服务（WebSocket）- 路由 `/ws`
- ✅ 创建 Kubernetes 部署配置
- ✅ 删除 docker-compose，改用 K8s 部署
- ✅ 更新 FRP WebSocket 路径：`/frp` → `/ws`
- ✅ 修改 Client 创建接口：支持指定 client_id（用户名/邮箱）

**下午**:

- ⚠️ **发现架构设计问题**：
  - Agent 组件设计有误
  - 需要将 Agent 拆分为 Agent-Web 和 Agent-FRP 两个组件
  - 需要重新设计整个架构
- ✅ **完成架构重新设计**：
  - 更新 `docs/design.md`
  - 明确单进程双线程架构
  - Server 进程：Server-Web 线程 + Server-FRP 线程
  - Agent 进程：Agent-Web 线程 + Agent-FRP 线程
  - Desktop 进程：Desktop-Web 线程 + Desktop-FRP 线程
  - 新增 gRPC 管理通道
  - 定义 gRPC 接口（Protocol Buffers）
  - 进程内通信使用 Go channel 或接口调用
- ✅ **设计文档拆分**：
  - 拆分为 4 个独立文档：design.md, design_database.md, design_api.md, design_deployment.md
  - design.md 保持简洁，只包含核心架构
  - 数据库设计：6 个数据表完整定义
  - API 设计：RESTful + gRPC + WebSocket 完整定义
  - 部署方案：Docker + Kubernetes 完整配置
- ✅ **更新开发计划和进度文档**：
  - 根据新架构更新 plan.md
  - 更新 progress.md 反映当前状态
  - 统一使用"进程"和"线程"术语
  - 明确进程内通信机制
- ✅ **澄清架构误解**：
  - 修正 design.md，明确单进程双线程架构
  - 修正 plan.md 和 progress.md 中的术语
  - 避免"双服务"、"双模块"等容易误解的表述

---

## 文档说明

### 更新规则

1. **任务进行中**: 在对应章节详细记录进度
2. **任务完成**:
   - 更新状态为 ✅
   - 清理详细内容，只保留简要信息
   - 将详细内容归档到独立文档（如 `week1-progress.md`）
3. **每日更新**: 在"更新日志"中记录当天的主要进展
4. **里程碑达成**: 更新里程碑状态

### 相关文档

- `docs/design.md` - 核心架构设计
- `docs/design_database.md` - 数据库详细设计
- `docs/design_api.md` - API 详细设计（RESTful + gRPC + WebSocket）
- `docs/design_deployment.md` - 部署方案
- `docs/plan.md` - 开发计划（重要文档，完成任务后需更新）
- `docs/debug.md` - 调试规范
- `docs/README.md` - 文档索引
- `README.md` - 项目说明

---

## 今日总结 (2025-11-25)

### 🎉 重大里程碑

**完整的 gRPC 通信链路验证成功！**

今天完成了从 Week 1 到 Week 3 的核心功能实现和验证，实现了项目的第一个重要里程碑。

### ✅ 完成内容

#### Week 1 (100%)

- 数据库模型修复（6 个表完全符合设计）
- RESTful API 完整实现
- 测试框架建立（7 个测试用例全部通过）

#### Week 2 (100%)

- Protocol Buffers 定义（agent.proto + client.proto）
- gRPC 服务完整实现（AgentService + ClientService）
- Server 内部设计文档（详细流程图）
- 进程内通信实现（RESTful API → gRPC → Agent）

#### Week 3 (60%)

- Agent 基础框架（internal/agent/agent.go）
- Agent-Web 线程（gRPC 客户端）
  - 连接 Server（端口 8081）
  - 注册和认证
  - 心跳机制（30 秒间隔）
  - 双向流接收命令
  - 命令处理框架
- **完整通信链路验证**
  - ✅ Agent 注册成功
  - ✅ Agent 心跳正常
  - ✅ 创建 STCP 命令：Server → Agent ✅
  - ✅ 删除 STCP 命令：Server → Agent ✅

### 📊 进度统计

- **Week 1**: ✅ 100%
- **Week 2**: ✅ 100%
- **Week 3**: 🔄 60%
- **总体进度**: 27% (2.6/8 周)

### 🎯 核心成就

1. **完整的通信链路**：从 RESTful API 到 gRPC 到 Agent，整个链路完全打通
2. **架构验证**：证明了单进程双线程架构的可行性
3. **进程内通信**：STCP 创建/删除命令可以实时发送给 Agent
4. **稳定性验证**：gRPC 双向流通信稳定，心跳机制正常

### ⏭️ 下一步计划

#### Week 3 剩余任务 (40%)

1. 实现 Agent-FRP 线程（FRP 客户端）
2. 实现动态代理管理（实际创建/删除 FRP STCP 代理）
3. 测试完整的数据隧道

#### Week 4-5: Web 管理界面

1. Vue 3 + TypeScript 项目初始化
2. 实现所有管理页面

#### Week 6-7: Desktop 应用

1. Wails 项目初始化
2. 实现 Desktop-Web 和 Desktop-FRP 线程

### 💡 经验总结

1. **设计先行**：详细的设计文档（design_server.md）对实现帮助很大
2. **测试驱动**：测试框架的建立让开发更有信心
3. **逐步验证**：每完成一个模块就验证，避免积累问题
4. **文档同步**：及时更新进度文档，保持清晰的项目状态

### 📈 工作效率

今天完成了 2.6 周的工作量，超出预期！主要原因：

- 设计文档清晰，减少了思考时间
- 测试框架完善，快速发现问题
- 架构合理，模块之间耦合度低

### 📝 文档和工具

- ✅ 创建演示脚本 `scripts/demo.sh`
  - 自动创建 Agent、Client、STCP 实例
  - 自动授权访问
  - 显示配置信息
  - 方便快速演示和测试

---

### 🔧 Agent 代理管理框架

- ✅ 添加 STCP 代理管理
  - STCPProxy 结构体定义
  - 代理内存存储（map[string]\*STCPProxy）
  - 创建代理：保存到内存，记录日志
  - 删除代理：从内存删除，记录日志
  - GetSTCPProxies 方法（用于状态上报）
- ✅ 构建测试通过

**下次工作重点**：实现 Agent-FRP 线程（FRP 客户端集成）
