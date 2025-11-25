# 项目进度跟踪

> 本文档记录项目的整体进度，已完成的任务只保留简要信息，详细内容已归档。

## 当前状态

- **当前阶段**: 架构重新设计完成 ✅
- **当前周次**: Week 1 ✅ 已完成，Week 2 需重新规划
- **总体进度**: 12.5% (1/8周)
- **重要**: 架构已重新设计，参见 `docs/design-v2.md`

---

## 第一阶段：Server端核心功能（Week 1-2）

### Week 1: 基础框架和数据层 ✅

**完成时间**: 2025-11-25

**完成任务**:
- ✅ 项目初始化（Go模块、依赖管理）
- ✅ 数据库设计与实现（5个数据模型）
- ✅ 管理员认证系统（JWT）
- ✅ 所有RESTful API（提前完成Week 2任务）
  - Agent管理API
  - Client管理API
  - STCP实例管理API
  - Client端API

**交付物**:
- ✅ 可运行的Server程序（bin/server）
- ✅ 完整的数据库schema
- ✅ 所有API端点
- ✅ API测试脚本
- ✅ Docker配置文件

**详细内容**: 已清理，保留简要信息

---

### Week 2: 业务API和FRP集成 🔄

**状态**: 待开始

**待完成任务**:
- [ ] FRP Server集成
  - [ ] 集成FRP Server核心代码
  - [ ] 配置WSS传输协议
  - [ ] 实现TLS证书加载
  - [ ] 实现Server与Agent的通信协议
  - [ ] 实现动态通知Agent创建/删除STCP实例

**预期交付物**:
- 集成FRP Server的可运行程序
- Server与Agent通信协议文档
- WSS连接测试通过

---

## 第二阶段：Agent端实现（Week 3）

**状态**: 未开始

**待完成任务**:
- [ ] Agent基础框架
- [ ] FRP Client集成
- [ ] 消息处理
- [ ] 动态代理管理
- [ ] Docker化

---

## 第三阶段：Web管理界面（Week 4-5）

**状态**: 未开始

**待完成任务**:
- [ ] 项目初始化（Vue 3 + TypeScript）
- [ ] 登录页面
- [ ] Agent管理页面
- [ ] Client管理页面
- [ ] STCP实例管理页面
- [ ] 整体布局

---

## 第四阶段：Desktop应用（Week 6-7）

**状态**: 未开始

**待完成任务**:
- [ ] Wails项目初始化
- [ ] 登录功能
- [ ] FRP Client集成
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
- [ ] Docker Compose配置

---

## 里程碑

- [x] **M1**: Server端完成（Week 2结束）- 提前完成API部分
- [ ] **M2**: Agent端完成（Week 3结束）
- [ ] **M3**: Web界面完成（Week 5结束）
- [ ] **M4**: Desktop应用完成（Week 7结束）
- [ ] **M5**: MVP发布（Week 8结束）

---

## 风险和问题

### 当前风险
- FRP集成复杂度待评估
- WSS穿透性需要实际测试

### 已解决问题
- 无

---

## 更新日志

### 2025-11-25
- ✅ 完成Week 1所有任务
- ✅ 提前完成Week 2的API开发任务
- ✅ 创建项目文档规范
- ✅ 完善设计文档：明确Server两个端口和路由设计
  - 端口8080: Web管理界面（HTTP）- 路由 `/`
  - 端口7000: FRP信令服务（WebSocket）- 路由 `/ws`
- ✅ 创建Kubernetes部署配置
- ✅ 删除docker-compose，改用K8s部署
- ✅ 更新FRP WebSocket路径：`/frp` → `/ws`
- ✅ 修改Client创建接口：支持指定client_id（用户名/邮箱）
  - 为未来用户登录功能预留扩展性
  - 添加重新生成secret接口
- ⚠️ **发现架构设计问题**：
  - Agent组件设计有误
  - 需要将Agent拆分为Agent-Web和Agent-FRP两个组件
  - Agent-Web通过gRPC与Server-Web通信
  - Agent-FRP通过WebSocket与Server-FRP通信
  - 需要重新设计整个架构
- ✅ **完成架构重新设计**：
  - 创建 `docs/design-v2.md`
  - Server分为Server-Web和Server-FRP两个服务
  - Agent分为Agent-Web和Agent-FRP两个模块
  - Client/Desktop也分为Web和FRP两个模块
  - 新增gRPC管理通道
  - 定义gRPC接口（Protocol Buffers）
- 📝 创建本进度跟踪文档

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

- `docs/design.md` - 设计文档
- `docs/development-plan.md` - 开发计划（重要文档，完成任务后需更新）
- `docs/debug.md` - 调试规范
- `docs/week1-progress.md` - Week 1详细进度（已归档）
- `QUICKSTART.md` - 快速启动指南
- `README.md` - 项目说明
