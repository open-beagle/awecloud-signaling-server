# Agent 详情页增强 - TODO List

## 进度总览

- [x] **Phase 1: 前端 UI** ✅ 已完成
- [x] **Phase 2: 后端 API** ✅ 已完成
- [x] **Phase 3: Agent 上报** ✅ 已完成
- [ ] **Phase 4: 联调测试**

---

## Phase 1: 前端 UI ✅ 已完成

### 1.1 数据模型 ✅

- [x] 更新 `web/src/types/models.ts`

### 1.2 网络信息卡片 ✅

- [x] 在 `web/src/views/Agent/Detail.vue` 添加网络信息卡片

### 1.3 端口访问服务列表 ✅

- [x] 在 `web/src/views/Agent/Detail.vue` 添加 Visitor 列表

### 1.4 国际化 ✅

- [x] 更新 `web/src/locales/zh-CN.ts`
- [x] 更新 `web/src/locales/en-US.ts`

### 1.5 API 层 ✅

- [x] 创建 `web/src/api/visitor.ts`

---

## Phase 2: 后端 API ✅ 已完成

### 2.1 数据库 ✅

- [x] 扩展 agents 表（添加网络信息字段）
- [x] 创建 visitors 表

### 2.2 Model 层 ✅

- [x] 更新 Agent model（网络信息字段）
- [x] 创建 Visitor model

### 2.3 Handler 层 ✅

- [x] 更新 AgentHandler（详情接口返回 network_info 和 visitors）
- [x] 创建 VisitorHandler

### 2.4 路由注册 ✅

- [x] 在 server.go 注册 Visitor API 路由

---

## Phase 3: Agent 上报 ✅ 已完成

### 3.1 网络检测模块 ✅

- [x] 扩展 `internal/agent/lan_detector.go`
  - [x] 检测局域网 IP
  - [x] 检测网关地址
  - [x] 检测网卡名称
  - [x] 检测运行环境 (native/docker/kubernetes)
  - [x] 获取主机名

### 3.2 gRPC 扩展 ✅

- [x] 更新 proto 文件（心跳增加网络信息）
- [x] 重新生成 proto 代码
- [x] 更新 gRPC handler（保存网络信息）

### 3.3 心跳上报 ✅

- [x] 更新 Agent 结构体，添加 networkInfo 字段
- [x] 在 NewAgent 中初始化网络检测
- [x] 更新心跳请求，携带网络信息

### 3.4 Visitor Manager ✅

- [x] `internal/agent/visitor_manager.go` 已存在且完整

---

## Phase 4: 联调测试 ⏳ 进行中

### 4.1 前后端联调

- [ ] 启动 Server，验证数据库迁移
- [ ] 启动 Agent，验证网络信息上报
- [ ] 访问 Web 界面，验证 Agent 详情页显示

### 4.2 功能验证

- [ ] 网络信息卡片数据展示
- [ ] Visitor 列表数据展示
- [ ] Visitor 创建/启动/停止/删除操作

---

## 当前进度

**已完成**: Phase 1 + Phase 2 + Phase 3

**正在进行**: Phase 4 - 联调测试

**下一步**: 启动 Server 和 Agent 进行联调测试
