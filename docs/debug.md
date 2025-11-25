# 调试规范

## 原则

1. **不允许随意调试系统**
2. **调试前必须与项目负责人讨论方案**
3. **所有调试活动必须记录在本文档**

## 构建规范

### 允许的构建操作

**开发/调试阶段**（只构建当前架构）
```bash
# 构建Server和Agent（当前架构，输出到bin/）
GOARCHS=$(go env GOARCH) ./scripts/build.sh

# 或者直接使用go build
go build -o bin/server cmd/server/main.go
go build -o bin/agent cmd/agent/main.go
```

**生产构建**（多架构）
```bash
# 构建所有架构（amd64 + arm64）
./scripts/build.sh

# 指定架构
GOARCHS=amd64,arm64 ./scripts/build.sh

# 指定版本
BUILD_VERSION=v1.0.0 ./scripts/build.sh
```

### 禁止的操作

❌ 不允许在其他位置生成可执行文件（必须输出到 bin/）
❌ 不允许使用 Makefile（已删除，统一使用 scripts/build.sh）
❌ 不允许未经讨论的系统级调试
❌ 不允许修改核心配置进行调试
❌ 开发阶段不需要构建多架构（浪费时间）

## 调试流程

### 1. 发现问题
- 记录问题现象
- 收集错误日志
- 确定问题范围

### 2. 讨论方案
- 与项目负责人讨论
- 确定调试方案
- 评估影响范围

### 3. 执行调试
- 按照批准的方案执行
- 记录调试过程
- 保存调试日志

### 4. 总结归档
- 记录问题原因
- 记录解决方案
- 更新相关文档

## 调试记录

### 格式

```markdown
## [日期] 问题标题

**问题描述**:
简要描述问题

**讨论方案**:
- 方案1: xxx
- 方案2: xxx
- 选择: xxx

**调试过程**:
1. 步骤1
2. 步骤2

**解决方案**:
最终的解决方案

**影响范围**:
- 文件1
- 文件2

**状态**: 已解决/进行中/待处理
```

---

## 调试历史

### [2025-11-25] 项目初始化

**问题描述**:
无，项目初始化阶段

**状态**: 已完成

---

### [2025-11-25] 构建系统优化

**问题描述**:
需要统一构建方式，支持多架构编译

**讨论方案**:
- 方案1: 使用 Makefile
- 方案2: 使用 shell 脚本
- 选择: 使用 `scripts/build.sh`，更灵活，支持版本注入

**调试过程**:
1. 创建 `scripts/build.sh` 脚本
2. 支持多架构编译（amd64, arm64）
3. 注入版本信息（版本号、Git提交、构建日期）
4. 删除 Makefile，统一使用脚本构建

**解决方案**:
- 开发阶段：只构建当前架构 `GOARCHS=$(go env GOARCH) ./scripts/build.sh`
- 生产构建：构建所有架构 `./scripts/build.sh`
- 所有二进制文件输出到 `bin/` 目录

**影响范围**:
- 删除 Makefile
- 创建 scripts/build.sh
- 更新 docs/debug.md

**状态**: 已完成

---

### [2025-11-25] 架构重大调整

**问题描述**:
原架构设计有误，Agent的角色和通信方式理解错误

**讨论方案**:
- 原方案: Agent作为单一FRP Client，直接连接Server-FRP
- 新方案: Agent分为两个组件
  - Agent-Web: 通过gRPC与Server-Web通信，接收管理指令
  - Agent-FRP: 通过WebSocket与Server-FRP通信，建立FRP隧道
- 选择: 采用新方案，重新设计整个架构

**正确的架构**:
1. Server分为两个独立组件:
   - Server-Web (端口8080): 管理界面和API
   - Server-FRP (端口7000): FRP信令服务

2. Agent分为两个组件:
   - Agent-Web: 通过gRPC与Server-Web通信
   - Agent-FRP: 通过WebSocket与Server-FRP通信

3. Client (Visitor):
   - 通过WebSocket与Server-FRP通信
   - 通过STCP隧道访问Agent-FRP
   - Agent-FRP才是真正访问SSH/MySQL/Redis的组件

**影响范围**:
- 整个项目架构
- Agent实现方式
- 通信协议设计
- 可能需要调整数据库设计

**状态**: 待重新设计

---

<!-- 后续调试记录追加在此 -->
