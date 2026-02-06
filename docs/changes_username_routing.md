# 用户名路由改造

## 概述

将用户详情页面的路由从基于 ID 改为基于用户名，使 URL 更友好、更易读。

## 改动说明

### 路由变化

**之前**：`/users/123`（使用数字 ID）  
**之后**：`/users/sfaceing`（使用用户名）

### 兼容性

后端 API 同时支持 ID 和用户名访问，自动识别：

- 如果参数是纯数字，按 ID 查询
- 如果参数包含非数字字符，按用户名查询

这样既支持新的用户名路由，也保持了对旧 ID 路由的兼容。

## 修改的文件

### 后端

**internal/server/api/user.go**

- `Get()` - 获取用户详情
- `Update()` - 更新用户
- `UpdateSSH()` - 更新 SSH 配置
- `Delete()` - 删除用户
- `RegenerateSecret()` - 重新生成密钥

**internal/server/api/agent_deploy.go**

- `CreateDeployToken()` - 创建部署 Token
- `ListDeployTokens()` - 获取部署 Token 列表

所有方法都改为支持 ID 或用户名参数。

### 前端

**web/src/router/index.ts**

- 路由参数从 `:id` 改为 `:username`

**web/src/api/user.ts**

- API 方法参数类型从 `number` 改为 `number | string`

**web/src/api/agentDeploy.ts**

- `createDeployToken()` 和 `getDeployTokens()` 参数类型改为 `number | string`

**web/src/views/User/List.vue**

- 跳转链接从 `row.id` 改为 `row.name`

**web/src/views/User/Detail.vue**

- 获取路由参数从 `route.params.id` 改为 `route.params.username`
- API 调用传递用户名而不是 ID
- 添加部署历史相关功能（之前缺失）

**web/src/views/User/components/DeployDialog.vue**

- 调用 API 时使用 `user.name` 而不是 `user.id`

## 优点

1. **URL 更友好**：`/users/sfaceing` 比 `/users/123` 更易读
2. **SEO 友好**：搜索引擎更容易理解页面内容
3. **用户体验好**：用户可以从 URL 直接看出是哪个用户
4. **向后兼容**：旧的 ID 链接仍然可用

## 注意事项

1. **用户名唯一性**：数据库中用户名字段已有唯一索引约束
2. **用户名格式**：建议限制用户名只包含字母、数字、中划线、下划线
3. **改名影响**：如果允许用户改名，旧的 URL 会失效（建议不允许改名）

## 测试建议

1. 测试用户名访问：`/users/sfaceing`
2. 测试 ID 访问（兼容性）：`/users/1`
3. 测试不存在的用户名：`/users/nonexistent`
4. 测试编辑、删除等操作
5. 测试重新生成密钥功能
6. 测试部署 Token 生成和查询功能
