# 数据模型设计

> 本文档描述 Server 的数据库表结构设计，基于前端页面需求和 Headscale 集成需求。

## 1. 设计原则

### 1.1 Headscale 映射字段

每个需要与 Headscale 交互的实体都需要存储映射字段：

| 字段       | 类型   | 说明                    |
| ---------- | ------ | ----------------------- |
| ts_user_id | uint64 | Headscale User ID       |
| ts_node_id | uint64 | Headscale Node ID       |
| ts_ip      | string | Headscale 分配的隧道 IP |

Headscale User Name 不需要存储，通过命名规则拼接：

| 实体   | User Name 规则              |
| ------ | --------------------------- |
| Agent  | `agent-{agent.name}`        |
| Client | `desktop-{client.username}` |

### 1.2 为什么需要 ts_user_id

Headscale API 要求使用 User ID（uint64）而非 User Name：

| 场景            | 需要的字段 | 说明                         |
| --------------- | ---------- | ---------------------------- |
| 创建 PreAuthKey | ts_user_id | API 参数必须是 ID，不是 Name |
| 删除 Node       | ts_node_id | 需要指定 Node ID             |
| 更新 Node Tags  | ts_node_id | 需要指定 Node ID             |
| 生成 ACL 规则   | ts_ip      | ACL 基于 IP 地址             |

---

## 2. Agent 表 (agent)

### 2.1 表结构

| 字段           | 类型     | 必填 | 说明                         |
| -------------- | -------- | ---- | ---------------------------- |
| id             | uint64   | 是   | 主键，复用 Headscale User ID |
| name           | string   | 是   | Agent 名称，唯一索引         |
| alias          | string   | 否   | 别名，显示用                 |
| secret_hash    | string   | 是   | 认证密钥哈希（bcrypt）       |
| version        | string   | 否   | Agent 版本号                 |
| system_info    | json     | 否   | 系统信息（硬件指纹）         |
| last_heartbeat | datetime | 否   | 最后心跳时间                 |
| node_id        | uint64   | 否   | Headscale Node ID            |
| ip             | string   | 否   | 隧道 IP                      |
| created_at     | datetime | 是   | 创建时间                     |
| updated_at     | datetime | 是   | 更新时间                     |

说明：

- `id` 直接使用 Headscale 返回的 User ID
- `status` 不存储在数据库，由 Server 内存维护（基于心跳判断）

### 2.2 system_info 字段结构

Agent 心跳时上报，存储系统硬件信息：

```json
{
  "os": "linux",
  "os_version": "Ubuntu 22.04",
  "arch": "amd64",
  "hostname": "server-01",
  "cpu": "Intel Xeon E5-2680 v4",
  "cpu_cores": 8,
  "memory_gb": 32
}
```

| 字段       | 说明                              |
| ---------- | --------------------------------- |
| os         | 操作系统：linux / windows / darwin|
| os_version | 系统版本                          |
| arch       | CPU 架构：amd64 / arm64           |
| hostname   | 主机名                            |
| cpu        | CPU 型号                          |
| cpu_cores  | CPU 核心数                        |
| memory_gb  | 内存大小（GB）                    |

### 2.2 Secret 设计

| 项目     | 说明                                         |
| -------- | -------------------------------------------- |
| 生成规则 | 创建 Agent 时自动生成，32 字符随机字符串     |
| 存储方式 | bcrypt 哈希后存储，禁止明文                  |
| 展示时机 | 仅创建时返回一次明文，之后无法查看           |
| 重置方式 | 管理员可重置，重置后生成新 secret 并返回一次 |

### 2.3 索引

| 索引名称      | 字段 | 类型 |
| ------------- | ---- | ---- |
| pk_agent      | id   | 主键 |
| uk_agent_name | name | 唯一 |

### 2.4 Headscale 映射说明

| 时机           | 操作                                       |
| -------------- | ------------------------------------------ |
| 创建 Agent     | 调用 Headscale 创建 User，User ID 作为主键 |
| Agent Register | 使用 id 创建 PreAuthKey                    |
| Agent 心跳     | 上报 ip，查询存储 node_id                  |
| 删除 Agent     | 调用 Headscale 删除 Node 和 User           |

---

## 3. Client 表 (client)

### 3.1 表结构

| 字段        | 类型     | 必填 | 说明                         |
| ----------- | -------- | ---- | ---------------------------- |
| id          | uint64   | 是   | 主键，复用 Headscale User ID |
| name        | string   | 是   | 用户名，唯一索引             |
| alias       | string   | 否   | 用户别名（如：张三）         |
| secret_hash | string   | 是   | 认证密钥哈希（bcrypt）       |
| created_at  | datetime | 是   | 创建时间                     |
| updated_at  | datetime | 是   | 更新时间                     |

说明：`id` 直接使用 Headscale 返回的 User ID

### 3.2 Secret 设计

| 项目     | 说明                                         |
| -------- | -------------------------------------------- |
| 生成规则 | 创建 Client 时自动生成，32 字符随机字符串    |
| 存储方式 | bcrypt 哈希后存储，禁止明文                  |
| 展示时机 | 仅创建时返回一次明文，之后无法查看           |
| 重置方式 | 管理员可重置，重置后生成新 secret 并返回一次 |

生成流程：

```txt
1. 创建 Client
   ↓
2. 生成随机 secret: crypto/rand 生成 32 字符
   ↓
3. 计算哈希: bcrypt.GenerateFromPassword(secret, cost=10)
   ↓
4. 存储 secret_hash 到数据库
   ↓
5. 返回明文 secret 给管理员（仅此一次）
```

### 3.3 索引

| 索引名称       | 字段 | 类型 |
| -------------- | ---- | ---- |
| pk_client      | id   | 主键 |
| uk_client_name | name | 唯一 |

### 3.4 说明

Client 表只存储用户信息，不存储设备信息。一个 Client 可以有多个 Desktop 设备。

---

## 4. Desktop 表 (desktop) 

### 4.1 表结构

| 字段        | 类型     | 必填 | 说明                           |
| ----------- | -------- | ---- | ------------------------------ |
| id          | uint64   | 是   | 主键，复用 Headscale Node ID   |
| client_id   | uint64   | 是   | 所属 Client，外键              |
| name        | string   | 是   | 设备名称，Desktop 收集的主机名 |
| alias       | string   | 否   | 设备别名                       |
| secret_hash | string   | 是   | 设备认证密钥哈希（bcrypt）     |
| system_info | json     | 否   | 系统信息（硬件指纹）           |
| ip          | string   | 否   | 隧道 IP                        |
| last_online | datetime | 否   | 最后在线时间                   |
| created_at  | datetime | 是   | 创建时间                       |
| updated_at  | datetime | 是   | 更新时间                       |

说明：`id` 直接使用 Headscale 返回的 Node ID，不自增

### 4.2 Secret 设计

Desktop 首次登录成功后生成设备专属 secret：

| 项目     | 说明                                                |
| -------- | --------------------------------------------------- |
| 生成时机 | Desktop 首次用 Client 凭证登录成功后自动生成        |
| 存储方式 | bcrypt 哈希后存储，禁止明文                         |
| 客户端   | 明文 secret 存储在 Desktop 本地，用于后续认证       |
| 有效期   | 永久有效，除非 Client 重置                          |
| 重置方式 | Client 可注销设备或重置 secret，重置后设备需重新登录|

认证流程：

```txt
首次登录:
1. Desktop 用 Client 的 username + secret 登录
2. Server 验证成功，创建 Desktop 记录
3. 生成 Desktop 专属 secret，返回给客户端
4. Desktop 存储 secret 到本地

后续登录:
1. Desktop 用 desktop_id + secret 认证
2. Server 验证 secret_hash
3. 认证成功，建立连接
```

### 4.4 system_info 字段结构

Desktop 连接时上报，存储设备硬件信息：

```json
{
  "os": "windows",
  "os_version": "Windows 11 Pro",
  "arch": "amd64",
  "hostname": "DESKTOP-ABC123",
  "cpu": "Intel Core i7-12700",
  "cpu_cores": 12,
  "memory_gb": 32
}
```

| 字段       | 说明                              |
| ---------- | --------------------------------- |
| os         | 操作系统：windows / darwin / linux|
| os_version | 系统版本                          |
| arch       | CPU 架构：amd64 / arm64           |
| hostname   | 主机名                            |
| cpu        | CPU 型号                          |
| cpu_cores  | CPU 核心数                        |
| memory_gb  | 内存大小（GB）                    |

### 4.5 索引

| 索引名称           | 字段      | 类型 |
| ------------------ | --------- | ---- |
| pk_desktop         | id        | 主键 |
| idx_desktop_client | client_id | 普通 |

### 4.6 与 Headscale 的映射关系

| Server  | Headscale | 关系 | 说明                      |
| ------- | --------- | ---- | ------------------------- |
| Client  | User      | 1:1  | 每个 Client 对应一个 User |
| Desktop | Node      | 1:1  | 每个设备对应一个 Node     |

---

## 5. 端口映射表 (proxy_service)

### 5.1 表结构

| 字段        | 类型     | 必填 | 说明                           |
| ----------- | -------- | ---- | ------------------------------ |
| id          | string   | 是   | 主键，UUID                     |
| name        | string   | 是   | 服务名称                       |
| alias       | string   | 否   | 别名                           |
| agent_id    | uint64   | 是   | 所属 Agent，外键               |
| target_addr | string   | 是   | 目标地址（如 192.168.1.10:80） |
| listen_addr | string   | 是   | 监听地址（如 100.64.0.1:80）   |
| enabled     | bool     | 是   | 是否启用                       |
| created_at  | datetime | 是   | 创建时间                       |
| updated_at  | datetime | 是   | 更新时间                       |

### 5.2 索引

| 索引名称            | 字段           | 类型 |
| ------------------- | -------------- | ---- |
| idx_proxy_agent     | agent_id       | 普通 |
| uk_proxy_name_agent | name, agent_id | 唯一 |

### 5.3 约束

- 名称 + Agent 不可重复

---

## 6. 端口访问表 (port_forward)

### 6.1 表结构

| 字段              | 类型     | 必填 | 说明                         |
| ----------------- | -------- | ---- | ---------------------------- |
| id                | string   | 是   | 主键，UUID                   |
| name              | string   | 是   | 服务名称                     |
| alias             | string   | 否   | 别名                         |
| agent_id          | uint64   | 是   | 所属 Agent（访问方），外键   |
| target_service_id | string   | 是   | 目标服务，外键 proxy_service |
| target_addr       | string   | 是   | 目标地址（冗余，便于显示）   |
| listen_addr       | string   | 是   | 本地监听地址                 |
| enabled           | bool     | 是   | 是否启用                     |
| created_at        | datetime | 是   | 创建时间                     |
| updated_at        | datetime | 是   | 更新时间                     |

### 6.2 索引

| 索引名称              | 字段              | 类型 |
| --------------------- | ----------------- | ---- |
| idx_forward_agent     | agent_id          | 普通 |
| idx_forward_target    | target_service_id | 普通 |
| uk_forward_name_agent | name, agent_id    | 唯一 |

### 6.3 约束

- 名称 + Agent 不可重复

---

## 7. 用户分组表 (client_group)

### 7.1 表结构

| 字段        | 类型     | 必填 | 说明               |
| ----------- | -------- | ---- | ------------------ |
| id          | int64    | 是   | 主键，自增         |
| name        | string   | 是   | 分组名称，唯一索引 |
| alias       | string   | 否   | 别名               |
| description | string   | 否   | 描述               |
| created_at  | datetime | 是   | 创建时间           |
| updated_at  | datetime | 是   | 更新时间           |

### 7.2 索引

| 索引名称             | 字段 | 类型 |
| -------------------- | ---- | ---- |
| uk_client_group_name | name | 唯一 |

---

## 8. 用户分组成员表 (client_group_member)

### 8.1 表结构

| 字段       | 类型     | 必填 | 说明            |
| ---------- | -------- | ---- | --------------- |
| id         | int64    | 是   | 主键，自增      |
| group_id   | int64    | 是   | 分组 ID，外键   |
| client_id  | uint64   | 是   | Client ID，外键 |
| created_at | datetime | 是   | 创建时间        |

### 8.2 索引

| 索引名称               | 字段                | 类型 |
| ---------------------- | ------------------- | ---- |
| uk_client_group_member | group_id, client_id | 唯一 |
| idx_cgm_client         | client_id           | 普通 |

---

## 9. 代理分组表 (agent_group)

### 9.1 表结构

| 字段        | 类型     | 必填 | 说明               |
| ----------- | -------- | ---- | ------------------ |
| id          | int64    | 是   | 主键，自增         |
| name        | string   | 是   | 分组名称，唯一索引 |
| alias       | string   | 否   | 别名               |
| description | string   | 否   | 描述               |
| created_at  | datetime | 是   | 创建时间           |
| updated_at  | datetime | 是   | 更新时间           |

### 9.2 索引

| 索引名称            | 字段 | 类型 |
| ------------------- | ---- | ---- |
| uk_agent_group_name | name | 唯一 |

---

## 10. 代理分组成员表 (agent_group_member)

### 10.1 表结构

| 字段       | 类型     | 必填 | 说明           |
| ---------- | -------- | ---- | -------------- |
| id         | int64    | 是   | 主键，自增     |
| group_id   | int64    | 是   | 分组 ID，外键  |
| agent_id   | uint64   | 是   | Agent ID，外键 |
| created_at | datetime | 是   | 创建时间       |

### 10.2 索引

| 索引名称              | 字段              | 类型 |
| --------------------- | ----------------- | ---- |
| uk_agent_group_member | group_id, agent_id| 唯一 |
| idx_agm_agent         | agent_id          | 普通 |

---

## 11. 服务授权表 - 桌面授权

### 11.1 服务-用户授权表 (service_client_permission)

| 字段       | 类型     | 必填 | 说明                        |
| ---------- | -------- | ---- | --------------------------- |
| id         | int64    | 是   | 主键，自增                  |
| service_id | string   | 是   | 服务 ID，外键 proxy_service |
| client_id  | uint64   | 是   | Client ID，外键             |
| granted_at | datetime | 是   | 授权时间                    |

索引：

| 索引名称           | 字段                 | 类型 |
| ------------------ | -------------------- | ---- |
| uk_svc_client_perm | service_id, client_id| 唯一 |
| idx_scp_client     | client_id            | 普通 |

### 11.2 服务-用户分组授权表 (service_client_group_permission)

| 字段       | 类型     | 必填 | 说明                        |
| ---------- | -------- | ---- | --------------------------- |
| id         | int64    | 是   | 主键，自增                  |
| service_id | string   | 是   | 服务 ID，外键 proxy_service |
| group_id   | int64    | 是   | 用户分组 ID，外键           |
| granted_at | datetime | 是   | 授权时间                    |

索引：

| 索引名称           | 字段                 | 类型 |
| ------------------ | -------------------- | ---- |
| uk_svc_cgroup_perm | service_id, group_id | 唯一 |
| idx_scgp_group     | group_id             | 普通 |

---

## 12. 服务授权表 - 代理授权

### 12.1 服务-代理授权表 (service_agent_permission)

| 字段       | 类型     | 必填 | 说明                        |
| ---------- | -------- | ---- | --------------------------- |
| id         | int64    | 是   | 主键，自增                  |
| service_id | string   | 是   | 服务 ID，外键 proxy_service |
| agent_id   | uint64   | 是   | Agent ID，外键              |
| granted_at | datetime | 是   | 授权时间                    |

索引：

| 索引名称          | 字段                | 类型 |
| ----------------- | ------------------- | ---- |
| uk_svc_agent_perm | service_id, agent_id| 唯一 |
| idx_sap_agent     | agent_id            | 普通 |

### 12.2 服务-代理分组授权表 (service_agent_group_permission)

| 字段       | 类型     | 必填 | 说明                        |
| ---------- | -------- | ---- | --------------------------- |
| id         | int64    | 是   | 主键，自增                  |
| service_id | string   | 是   | 服务 ID，外键 proxy_service |
| group_id   | int64    | 是   | 代理分组 ID，外键           |
| granted_at | datetime | 是   | 授权时间                    |

索引：

| 索引名称           | 字段                 | 类型 |
| ------------------ | -------------------- | ---- |
| uk_svc_agroup_perm | service_id, group_id | 唯一 |
| idx_sagp_group     | group_id             | 普通 |

---

## 13. 审计日志表 (audit_log)

### 13.1 表结构

| 字段        | 类型     | 必填 | 说明                         |
| ----------- | -------- | ---- | ---------------------------- |
| id          | int64    | 是   | 主键，自增                   |
| admin_id    | int64    | 否   | 操作人 ID，外键              |
| action_type | string   | 是   | 操作类型                     |
| target_type | string   | 是   | 目标类型（agent/client/...） |
| target_id   | string   | 是   | 目标 ID                      |
| target_name | string   | 是   | 目标名称                     |
| detail      | string   | 否   | 详情（JSON）                 |
| created_at  | datetime | 是   | 创建时间                     |

### 13.2 索引

| 索引名称         | 字段        | 类型 |
| ---------------- | ----------- | ---- |
| idx_audit_admin  | admin_id    | 普通 |
| idx_audit_action | action_type | 普通 |
| idx_audit_time   | created_at  | 普通 |

### 13.3 操作类型枚举

| 操作类型            | 说明         |
| ------------------- | ------------ |
| create_agent        | 创建 Agent   |
| delete_agent        | 删除 Agent   |
| create_service      | 创建服务     |
| delete_service      | 删除服务     |
| desktop_auth_grant  | 桌面授权     |
| desktop_auth_revoke | 撤销桌面授权 |
| agent_auth_grant    | 代理授权     |
| agent_auth_revoke   | 撤销代理授权 |
| create_port_forward | 创建端口访问 |
| delete_port_forward | 删除端口访问 |
| create_client_group | 创建用户分组 |
| delete_client_group | 删除用户分组 |
| create_agent_group  | 创建代理分组 |
| delete_agent_group  | 删除代理分组 |

---

## 14. 系统配置表 (system_config)

### 14.1 表结构

| 字段       | 类型     | 必填 | 说明             |
| ---------- | -------- | ---- | ---------------- |
| id         | int64    | 是   | 主键，自增       |
| key        | string   | 是   | 配置键，唯一索引 |
| value      | string   | 是   | 配置值           |
| updated_at | datetime | 是   | 更新时间         |

### 14.2 索引

| 索引名称         | 字段 | 类型 |
| ---------------- | ---- | ---- |
| uk_system_config | key  | 唯一 |

### 14.3 配置项

| key                   | 说明             | 示例值                            |
| --------------------- | ---------------- | --------------------------------- |
| client_download_url   | 客户端下载地址   | https://cdn.example.com/downloads |
| desktop_min_version   | 客户端最低版本   | 1.0.0                             |
| headscale_public_url  | 隧道公网地址     | https://signaling.example.com     |
| stun_port             | STUN 端口        | 3479                              |
| ip_prefix             | IP 地址段        | 100.64.0.0/10                     |
| auth_key_expiry_hours | 预认证密钥有效期 | 24                                |

---

## 15. 管理员表 (admin)

### 15.1 表结构

| 字段       | 类型     | 必填 | 说明                 |
| ---------- | -------- | ---- | -------------------- |
| id         | int64    | 是   | 主键，自增           |
| username   | string   | 是   | 用户名，唯一索引     |
| password   | string   | 是   | 密码（加密存储）     |
| role       | string   | 是   | 角色：admin / viewer |
| created_at | datetime | 是   | 创建时间             |
| updated_at | datetime | 是   | 更新时间             |

### 15.2 索引

| 索引名称      | 字段     | 类型 |
| ------------- | -------- | ---- |
| uk_admin_name | username | 唯一 |

---

## 16. 表关系图

```txt
┌─────────────────────────────────────────────────────────────────────────────┐
│                              数据模型关系图                                  │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│   ┌─────────┐                                                               │
│   │  admin  │                                                               │
│   └────┬────┘                                                               │
│        │ 1:N                                                                │
│        ▼                                                                    │
│   ┌───────────┐                                                             │
│   │ audit_log │                                                             │
│   └───────────┘                                                             │
│                                                                             │
│   ┌─────────┐ 1:N ┌───────────────┐ N:1 ┌─────────────┐                    │
│   │  agent  │────►│ proxy_service │◄────│ port_forward│                    │
│   └────┬────┘     └───────┬───────┘     └──────┬──────┘                    │
│        │                  │                    │                            │
│        │ N:M              │ 1:N                │ N:1                        │
│        ▼                  ▼                    ▼                            │
│   ┌─────────────┐   ┌─────────────────────────────────────┐                │
│   │ agent_group │   │        授权表 (4种)                  │                │
│   └─────────────┘   │  service_client_permission          │                │
│                     │  service_client_group_permission    │                │
│                     │  service_agent_permission           │                │
│                     │  service_agent_group_permission     │                │
│                     └─────────────────────────────────────┘                │
│                                    │                                        │
│                                    │ N:1                                    │
│                                    ▼                                        │
│   ┌─────────┐ 1:N ┌─────────┐    ┌──────────────┐                          │
│   │ client  │────►│ desktop │    │ client_group │                          │
│   └─────────┘     └─────────┘    └──────────────┘                          │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 17. Headscale 映射总结

### 17.1 映射字段分布

| 表      | ts_user_id | ts_node_id | ts_ip |
| ------- | ---------- | ---------- | ----- |
| agent   | ✓          | ✓          | ✓     |
| client  | ✓          | -          | -     |
| desktop | -          | ✓          | ✓     |

### 17.2 命名规则

| 实体   | Headscale User Name 格式  | 说明         |
| ------ | ------------------------- | ------------ |
| Agent  | agent-{agent.name}        | 拼接，不存储 |
| Client | desktop-{client.username} | 拼接，不存储 |

### 17.3 ID 生成时机

| 字段       | 生成时机                               |
| ---------- | -------------------------------------- |
| ts_user_id | 创建 Agent/Client 时调用 Headscale API |
| ts_node_id | 设备连接后查询 Headscale API 获取      |
| ts_ip      | 设备连接后上报                         |

---

**文档版本**: 1.0
**创建日期**: 2026-01-12
**维护者**: 开发团队
