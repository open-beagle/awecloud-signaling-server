# Token 统一与 Agent 部署升级

## 背景

当前系统存在两套 Token 机制：

| 对比项         | Agent Deploy Token          | Client Token                  |
| -------------- | --------------------------- | ----------------------------- |
| 数据表         | agent_deploy_token          | client_tokens                 |
| 模型文件       | model/agent_deploy_token.go | model/client_token.go         |
| API 文件       | api/agent_deploy.go         | api/client_token.go           |
| 注册接口       | POST /api/v1/agent/register | POST /api/v1/client/register  |
| 关联对象       | Agent 用户（UserRoleAgent） | Client 用户（UserRoleClient） |
| 创建时需要     | user_id + device_name       | user_id + name                |
| 首次使用时限   | 24 小时                     | 无限制                        |
| 设备绑定       | hostname 指纹               | hostname 指纹                 |
| Headscale 用户 | agent-{name}                | client-{username}             |
| 前端 API       | web/src/api/agentDeploy.ts  | web/src/api/clientToken.ts    |

Agent 和 CloudIDE 使用同一个二进制，两套 Token 增加了不必要的复杂度。CloudIDE 本质是 Agent 的升级形态，应该统一。

## 统一方案

### 核心思路

合并为一个 deploy_token 表，Token 不需要前缀。User 的 Role（agent/client）决定注册时的 Headscale 行为，Agent 端代码不需要判断模式。

### 统一后的数据模型

```
表名: deploy_tokens

字段:
- id: 主键
- token: 部署 Token（随机生成，无前缀）
- user_id: 关联的用户 ID（User.Role 决定行为）
- name: Token 名称/备注
- status: pending | bound | revoked
- device_fingerprint: 设备指纹（SHA256(hostname)）
- device_name: 设备名称（hostname）
- ssh_enabled: 是否启用 SSH
- ssh_users: SSH 用户名列表（JSON 数组，Client 角色专用）
- expires_at: 首次使用截止时间（可选，Agent 默认 24h，Client 无限制）
- created_at: 创建时间
- bound_at: 绑定时间
- last_used_at: 最后使用时间
- node_id: 关联的 Headscale Node ID
- created_by: 创建人 ID（管理员）
```

### 统一后的注册接口

```
POST /api/v1/register
请求: { "token": "xxx...", "device_fingerprint": "sha256...", "device_name": "pod-abc" }
响应: { "auth_key": "tskey-auth-xxx...", "headscale_url": "https://...", "user_name": "...", "user_role": "agent|client", "config": {...} }
```

Server 注册逻辑根据 User.Role 分支：

```
收到注册请求
  │
  ├── 查找 Token → 获取 User
  │
  ├── User.Role == agent
  │     ├── Headscale 用户: agent-{name}
  │     ├── Tag: 无（Agent 用 ACL src 匹配）
  │     ├── 返回 config 包含 agent name/device
  │     └── 生成安装命令用 install_agent.sh
  │
  └── User.Role == client
        ├── Headscale 用户: client-{name}
        ├── Tag: tag:client-{name} + tag:group-{xxx}
        ├── 返回 config 不含 agent name（CloudIDE 不需要）
        └── 生成安装命令用 install_signal.sh
```

### 统一后的环境变量

Agent 和 CloudIDE 统一使用相同的环境变量：

| 环境变量      | 必填 | 说明                                                    |
| ------------- | ---- | ------------------------------------------------------- |
| SIGNAL_TOKEN  | 是   | 部署 Token（统一，无前缀区分）                          |
| SIGNAL_SERVER | 是   | Server 地址                                             |
| SIGNAL_NAME   | 否   | Agent 名称（Agent 模式由 Server 返回，CloudIDE 不需要） |

Agent 启动流程变化：

```
当前流程:
  1. 读取 SIGNAL_TOKEN
  2. 判断 ct_ 前缀 → CloudIDE 模式 / 无前缀 → Agent 模式
  3. 分别调用不同的注册接口

统一后:
  1. 读取 SIGNAL_TOKEN
  2. 调用统一注册接口 POST /api/v1/register
  3. Server 返回 user_role，Agent 根据 role 决定后续行为
  4. 不需要前缀判断
```

### 统一后的管理 API

```
创建 Token:
  POST /api/v1/admin/users/:id/deploy-token
  请求: { "name": "备注" }
  响应: { "token": "xxx...", "expires_at": "...", "env_config": "SIGNAL_TOKEN=...\nSIGNAL_SERVER=..." }

Token 列表:
  GET /api/v1/admin/users/:id/deploy-tokens?page=1&size=20

撤销 Token:
  DELETE /api/v1/admin/deploy-tokens/:token_id
```

Agent 用户和 Client 用户共用同一套 Token 管理接口，前端只需要一个 Token 管理组件。

## 需要修改的文件

### Server 端

| 文件                                        | 变更                                                               |
| ------------------------------------------- | ------------------------------------------------------------------ |
| internal/server/model/agent_deploy_token.go | 重命名为 deploy_token.go，合并 client_token.go 的字段              |
| internal/server/model/client_token.go       | 删除，合并到 deploy_token.go                                       |
| internal/server/api/agent_deploy.go         | 重命名为 deploy.go，合并注册逻辑                                   |
| internal/server/api/client_token.go         | 删除，合并到 deploy.go                                             |
| internal/server/server.go                   | 路由合并：去掉 /agent/register 和 /client/register，新增 /register |
| internal/server/db/db.go                    | AutoMigrate 更新表名                                               |

### Agent 端

| 文件                            | 变更                                                    |
| ------------------------------- | ------------------------------------------------------- |
| cmd/agent/main.go               | 去掉 ct\_ 前缀判断，统一调用 /api/v1/register           |
| internal/common/config/agent.go | 去掉 IsCloudIDEMode()，改为根据 Server 返回的 role 判断 |

### 前端

| 文件                           | 变更                                       |
| ------------------------------ | ------------------------------------------ |
| web/src/api/agentDeploy.ts     | 合并 clientToken.ts，统一为 deployToken.ts |
| web/src/api/clientToken.ts     | 删除                                       |
| web/src/views/Agent/Detail.vue | Token 管理组件统一                         |

### 部署配置

| 文件                                         | 变更                                         |
| -------------------------------------------- | -------------------------------------------- |
| deployments/kubernetes/agent-deployment.yaml | 环境变量不变（SIGNAL_TOKEN + SIGNAL_SERVER） |
| deployments/kubernetes/agent-secret.yaml     | 不变                                         |
| scripts/install_agent.sh                     | 注册接口改为 /api/v1/register                |
| scripts/install_signal.sh                    | 注册接口改为 /api/v1/register                |
| config/agent.toml.example                    | 去掉 ct\_ 前缀说明                           |

### 设计文档

| 文件                        | 变更                                |
| --------------------------- | ----------------------------------- |
| docs/design_cloudide.md     | 更新 Token 模型、注册流程、API 设计 |
| docs/design_cloudide_env.md | 去掉 ct\_ 前缀判断逻辑              |

## 数据库迁移

旧表 agent_deploy_token 和 client_tokens 需要迁移到新表 deploy_tokens。

迁移策略：GORM AutoMigrate 创建新表，旧表数据在升级时自动迁移。

```
迁移步骤:
1. 创建 deploy_tokens 表
2. 从 agent_deploy_token 迁移数据（status expired → revoked）
3. 从 client_tokens 迁移数据（expires_at 设为空，表示无限制）
4. 旧表保留不删除（安全起见）
```

## 兼容性

- 旧版 Agent 仍然调用 /api/v1/agent/register → Server 保留旧接口作为兼容，内部转发到新逻辑
- 新版 Agent 调用 /api/v1/register → 新接口
- 过渡期两个接口并存，旧接口在日志中输出 deprecation 警告

## 实现优先级

| 优先级 | 任务                   | 说明                                  |
| ------ | ---------------------- | ------------------------------------- |
| P0     | deploy_token 模型合并  | 新表结构                              |
| P0     | 注册接口统一           | POST /api/v1/register                 |
| P0     | Agent 端去掉 ct\_ 判断 | 统一注册流程                          |
| P1     | 管理 API 合并          | 前端 Token 管理统一                   |
| P1     | 前端适配               | 合并 agentDeploy.ts 和 clientToken.ts |
| P1     | 旧接口兼容             | /agent/register 转发                  |
| P2     | 数据库迁移             | 旧表数据迁移                          |
| P2     | install_agent.sh 更新  | 注册接口改为 /register                |
