# Agent 列表功能增强

## 变更概述

本次变更解决了 Agent 在线状态显示不准确的问题，并在 Agent 列表中增加了"版本"和"最后在线时间"两个字段的显示。

## 变更目的

1. **修复状态显示 bug**：Agent 掉线后，列表中仍显示"已连接"状态，导致用户无法准确判断 Agent 的真实在线情况
2. **增强信息展示**：在 Agent 列表中显示版本号和最后在线时间，便于运维人员快速了解各 Agent 的版本分布和活跃情况

## 变更详情

### 修改文件清单

| 文件路径                                | 修改类型 | 说明                                                |
| --------------------------------------- | -------- | --------------------------------------------------- |
| `pkg/proto/agent.proto`                 | 修改     | 在注册请求和心跳请求中添加 version 字段             |
| `pkg/proto/agent.pb.go`                 | 自动生成 | 由 protoc 重新生成                                  |
| `internal/server/model/agent.go`        | 修改     | Agent 模型添加 Version 字段                         |
| `internal/server/api/agent.go`          | 修改     | 列表接口实时计算在线状态                            |
| `internal/server/grpc/agent_service.go` | 修改     | 注册和心跳时保存版本信息                            |
| `internal/agent/agent.go`               | 修改     | Agent 结构体添加 version 字段，注册和心跳时发送版本 |
| `cmd/agent/main.go`                     | 修改     | 创建 Agent 时传入版本号                             |
| `web/src/types/models.ts`               | 修改     | Agent 类型添加 version 和 last_heartbeat 字段       |
| `web/src/views/Agent/List.vue`          | 修改     | 列表增加版本和最后在线时间列                        |
| `web/src/locales/zh-CN.ts`              | 修改     | 添加中文翻译                                        |
| `web/src/locales/en-US.ts`              | 修改     | 添加英文翻译                                        |

### 业务流程变更

#### 1. Agent 注册流程

**变更前**：Agent 注册时只发送 agent_name 和 agent_token

**变更后**：Agent 注册时额外发送 version 字段，Server 端在注册成功后将版本信息保存到数据库

#### 2. Agent 心跳流程

**变更前**：心跳只发送 agent_id 和 agent_token，Server 端更新心跳时间和状态

**变更后**：心跳额外发送 version 字段，Server 端在更新心跳时间的同时更新版本信息（支持 Agent 热升级后版本自动更新）

#### 3. Agent 列表查询流程

**变更前**：直接返回数据库中的 status 字段，该字段可能因 Agent 异常断开而未及时更新

**变更后**：查询时实时计算在线状态——检查 last_heartbeat 字段，若最后心跳时间在 60 秒内则判定为在线，否则为离线。同时返回 version 和 last_heartbeat 字段供前端展示

#### 4. 前端展示变更

Agent 列表字段排序对比：

| 变更前     | 变更后              |
| ---------- | ------------------- |
| ID         | ID                  |
| Agent 名称 | Agent 名称          |
| 描述       | 描述                |
| 状态       | 状态                |
| 创建时间   | **版本** (新增)     |
| 操作       | **最后在线** (新增) |
|            | 创建时间            |
|            | 操作                |

新增字段说明：

- **版本**：显示 Agent 当前运行的版本号，未上报时显示"-"
- **最后在线**：显示 Agent 最后一次心跳的相对时间（如"5 分钟前"）

## 变更进度

- [x] Proto 定义添加 version 字段
- [x] 重新生成 proto Go 代码
- [x] Agent 模型添加 Version 字段
- [x] Server 端注册接口保存版本
- [x] Server 端心跳接口保存版本
- [x] Server 端列表接口实时计算状态
- [x] Agent 端注册时发送版本
- [x] Agent 端心跳时发送版本
- [x] 前端类型定义更新
- [x] 前端列表组件更新
- [x] 国际化翻译更新
- [x] 代码编译验证通过

## 注意事项

1. 数据库会自动迁移添加 version 字段（GORM AutoMigrate）
2. 旧版本 Agent 不会发送 version 字段，列表中显示为"-"
3. Agent 需要重新连接后才会上报版本信息
