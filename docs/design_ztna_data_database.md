# ZTNA 数据库表设计

## 概述

本文档定义 ZTNA 所有数据库表的结构。分为现有表（不变/微调）和新增表两部分。

数据库：SQLite，ORM：GORM。

相关业务设计文档：

- `design_ztna_server_user.md` — User 表的业务逻辑（创建/删除/编辑/查询流程）
- `design_ztna_server_device.md` — Node 表的业务逻辑（创建/删除/心跳/生命周期）
- `design_ztna_server_heartbeat.md` — Node 表心跳写入优化（NodeCache 内存缓存）

## 现有表（23 张，不变）

以下表结构在 ZTNA 中不做修改，仅列出供参考。

### 身份与设备（5 张）

| 表名         | 主键类型 | 说明                                           |
| ------------ | -------- | ---------------------------------------------- |
| user         | uint64   | 用户（agent/client），统一身份                 |
| admin        | int64    | 管理员                                         |
| group        | int64    | 分组                                           |
| group_member | int64    | 分组成员关系（User ↔ Group 多对多）            |
| node         | uint64   | 设备（agent/desktop），一个 User 可有多个 Node |

### 认证（3 张）

| 表名                   | 主键类型 | 说明                                  |
| ---------------------- | -------- | ------------------------------------- |
| deploy_tokens          | uint64   | 统一部署 Token（Agent + Client 共用） |
| device_token           | int64    | Desktop 设备令牌（Logto 登录后绑定）  |
| desktop_login_sessions | uint64   | Desktop 登录会话（Logto OAuth 流程）  |

### 服务（3 张）

| 表名          | 主键类型 | 说明                                               |
| ------------- | -------- | -------------------------------------------------- |
| proxy_service | string   | 端口映射服务（ZTNA 中改名 AgentService，表名不变） |
| port_forward  | string   | 端口转发配置                                       |
| visitor       | int64    | 访问者（Agent 间端口访问）                         |

### 现有 ACL（8 张）

| 表名                         | 层级    | 授权维度           |
| ---------------------------- | ------- | ------------------ |
| acl_service_user_permission  | 第 1 层 | 服务授权（用户级） |
| acl_service_group_permission | 第 1 层 | 服务授权（分组级） |
| acl_user_user_permission     | 第 1 层 | 用户授权（用户级） |
| acl_user_group_permission    | 第 1 层 | 用户授权（分组级） |
| acl_group_user_permission    | 第 1 层 | 分组授权（用户级） |
| acl_group_group_permission   | 第 1 层 | 分组授权（分组级） |
| acl_ssh_user_permission      | 第 2 层 | SSH 授权（用户级） |
| acl_ssh_group_permission     | 第 2 层 | SSH 授权（分组级） |

### 辅助（4 张）

| 表名              | 说明     |
| ----------------- | -------- |
| audit_log         | 审计日志 |
| system_config     | 系统配置 |
| service_favorites | 服务收藏 |
| port_preferences  | 端口偏好 |

## 新增表（15 张）

### Endpoint 表（3 张）

三种 Endpoint 结构对称，都挂在 Agent（User role=agent）下。

endpoint_ssh:

| 字段       | 类型   | 约束                           | 说明                      |
| ---------- | ------ | ------------------------------ | ------------------------- |
| id         | string | PK, UUID                       | 主键                      |
| user_id    | uint64 | NOT NULL, INDEX, FK            | 所属 Agent 的 User ID     |
| name       | string | NOT NULL, UNIQUE(user_id,name) | 名称（如 web-server-1）   |
| alias      | string |                                | 别名（显示名称）          |
| host       | string |                                | 内网地址（Endpoint 上报） |
| port       | int    | DEFAULT 22                     | SSH 端口                  |
| status     | string | DEFAULT 'offline'              | online/offline            |
| enabled    | bool   | DEFAULT true                   | 是否启用                  |
| created_at | time   |                                |                           |
| updated_at | time   |                                |                           |

endpoint_k8sapi:

| 字段            | 类型   | 约束                           | 说明                  |
| --------------- | ------ | ------------------------------ | --------------------- |
| id              | string | PK, UUID                       | 主键                  |
| user_id         | uint64 | NOT NULL, INDEX, FK            | 所属 Agent 的 User ID |
| name            | string | NOT NULL, UNIQUE(user_id,name) | 集群名称              |
| alias           | string |                                | 别名                  |
| api_server      | string |                                | K8S API Server 地址   |
| kubeconfig_path | string |                                | kubeconfig 文件路径   |
| status          | string | DEFAULT 'offline'              | online/offline        |
| enabled         | bool   | DEFAULT true                   | 是否启用              |
| created_at      | time   |                                |                       |
| updated_at      | time   |                                |                       |

endpoint_k8sservice:

| 字段       | 类型   | 约束                           | 说明                  |
| ---------- | ------ | ------------------------------ | --------------------- |
| id         | string | PK, UUID                       | 主键                  |
| user_id    | uint64 | NOT NULL, INDEX, FK            | 所属 Agent 的 User ID |
| name       | string | NOT NULL, UNIQUE(user_id,name) | 集群名称              |
| alias      | string |                                | 别名                  |
| status     | string | DEFAULT 'offline'              | online/offline        |
| enabled    | bool   | DEFAULT true                   | 是否启用              |
| created_at | time   |                                |                       |
| updated_at | time   |                                |                       |

### 新增 ACL 表（10 张）

第 3 层 — AgentK8SAPI 授权（2 张）：

acl_k8s_user_permission:

| 字段          | 类型   | 约束                         | 说明                                   |
| ------------- | ------ | ---------------------------- | -------------------------------------- |
| id            | int64  | PK                           | 主键                                   |
| agent_user_id | uint64 | NOT NULL, INDEX, UNIQUE(a,u) | 目标 Agent 的 User ID                  |
| user_id       | uint64 | NOT NULL, INDEX, UNIQUE(a,u) | 被授权用户 ID                          |
| namespaces    | string | NOT NULL                     | 允许的命名空间（JSON 数组，"\*"=全部） |
| k8s_role      | string | NOT NULL                     | Impersonation K8S 角色                 |
| granted_at    | time   |                              | 授权时间                               |

acl_k8s_group_permission:

| 字段          | 类型   | 约束                         | 说明                        |
| ------------- | ------ | ---------------------------- | --------------------------- |
| id            | int64  | PK                           | 主键                        |
| agent_user_id | uint64 | NOT NULL, INDEX, UNIQUE(a,g) | 目标 Agent 的 User ID       |
| group_id      | int64  | NOT NULL, INDEX, UNIQUE(a,g) | 被授权分组 ID               |
| namespaces    | string | NOT NULL                     | 允许的命名空间（JSON 数组） |
| k8s_role      | string | NOT NULL                     | K8S 角色                    |
| granted_at    | time   |                              | 授权时间                    |

第 3 层 — AgentK8S Service 授权（2 张）：

acl_k8s_service_user_permission:

| 字段            | 类型   | 约束                         | 说明                          |
| --------------- | ------ | ---------------------------- | ----------------------------- |
| id              | int64  | PK                           | 主键                          |
| agent_user_id   | uint64 | NOT NULL, INDEX, UNIQUE(a,u) | 目标 Agent 的 User ID         |
| user_id         | uint64 | NOT NULL, INDEX, UNIQUE(a,u) | 被授权用户 ID                 |
| namespaces      | string | NOT NULL                     | 允许的命名空间（JSON 数组）   |
| service_pattern | string | NOT NULL                     | Service 名称模式（"\*"=全部） |
| granted_at      | time   |                              | 授权时间                      |

acl_k8s_service_group_permission:

| 字段            | 类型   | 约束                         | 说明                        |
| --------------- | ------ | ---------------------------- | --------------------------- |
| id              | int64  | PK                           | 主键                        |
| agent_user_id   | uint64 | NOT NULL, INDEX, UNIQUE(a,g) | 目标 Agent 的 User ID       |
| group_id        | int64  | NOT NULL, INDEX, UNIQUE(a,g) | 被授权分组 ID               |
| namespaces      | string | NOT NULL                     | 允许的命名空间（JSON 数组） |
| service_pattern | string | NOT NULL                     | Service 名称模式            |
| granted_at      | time   |                              | 授权时间                    |

第 4 层 — Endpoint 授权（6 张）：

三种 Endpoint 类型 × 用户/分组 = 6 张表，结构对称。

acl_ssh_jump_user_permission / acl_ssh_jump_group_permission:

| 字段             | 类型         | 约束                           | 说明                           |
| ---------------- | ------------ | ------------------------------ | ------------------------------ |
| id               | int64        | PK                             | 主键                           |
| endpoint_ssh_id  | string       | NOT NULL, INDEX, UNIQUE(e,u/g) | 目标 EndpointSSH ID            |
| user_id/group_id | uint64/int64 | NOT NULL, INDEX, UNIQUE(e,u/g) | 被授权用户/分组                |
| ssh_users        | string       | NOT NULL                       | 允许的 Linux 用户（JSON 数组） |
| granted_at       | time         |                                | 授权时间                       |

acl_k8sapi_jump_user_permission / acl_k8sapi_jump_group_permission:

| 字段               | 类型         | 约束                           | 说明                        |
| ------------------ | ------------ | ------------------------------ | --------------------------- |
| id                 | int64        | PK                             | 主键                        |
| endpoint_k8sapi_id | string       | NOT NULL, INDEX, UNIQUE(e,u/g) | 目标 EndpointK8SAPI ID      |
| user_id/group_id   | uint64/int64 | NOT NULL, INDEX, UNIQUE(e,u/g) | 被授权用户/分组             |
| namespaces         | string       | NOT NULL                       | 允许的命名空间（JSON 数组） |
| k8s_role           | string       | NOT NULL                       | K8S 角色                    |
| granted_at         | time         |                                | 授权时间                    |

acl_k8sservice_jump_user_permission / acl_k8sservice_jump_group_permission:

| 字段                   | 类型         | 约束                           | 说明                       |
| ---------------------- | ------------ | ------------------------------ | -------------------------- |
| id                     | int64        | PK                             | 主键                       |
| endpoint_k8sservice_id | string       | NOT NULL, INDEX, UNIQUE(e,u/g) | 目标 EndpointK8SService ID |
| user_id/group_id       | uint64/int64 | NOT NULL, INDEX, UNIQUE(e,u/g) | 被授权用户/分组            |
| service_pattern        | string       | NOT NULL                       | Service 名称模式           |
| granted_at             | time         |                                | 授权时间                   |

### 域名注册表（1 张）

domain_registry:

| 字段         | 类型   | 约束             | 说明                                               |
| ------------ | ------ | ---------------- | -------------------------------------------------- |
| id           | int64  | PK               | 主键                                               |
| domain       | string | NOT NULL, INDEX  | 完整域名（如 beagle-242.beijing.beagle）           |
| type         | string | NOT NULL, INDEX  | 能力类型：ssh / k8sapi / k8ssvc                    |
| user_id      | uint64 | NOT NULL, INDEX  | 所属 User ID（Agent User 或 Client User）          |
| node_id      | uint64 |                  | 关联的 Node ID（Agent Node 或 Client Node 注册时） |
| endpoint_id  | string |                  | 关联的 Endpoint ID（Endpoint 注册时）              |
| target_ip    | string |                  | 目标 IP（Node 的 Tailscale IP 或 ClusterIP）       |
| target_port  | int    |                  | 目标端口                                           |
| namespace    | string |                  | K8S 命名空间（k8ssvc 类型时）                      |
| service_name | string |                  | K8S Service 名称（k8ssvc 类型时）                  |
| status       | string | DEFAULT 'online' | online/offline                                     |
| created_at   | time   |                  |                                                    |
| updated_at   | time   |                  |                                                    |

索引：(domain, node_id) 联合唯一、(domain, endpoint_id) 联合唯一、user_id、type。

说明：

- type 统一为三种能力：ssh / k8sapi / k8ssvc，不再区分 agent/endpoint 来源
- 通过 node_id 或 endpoint_id 区分域名来源（二者互斥，一条记录只填一个）
- 同一域名可以有多条记录（不同 node_id），表示负载均衡（如 kubernetes.beijing.beagle 对应多个 Node）
- domain 不再是唯一索引，因为负载均衡场景下同一域名对应多个 Node

### 操作级审计日志（1 张）

operation_audit_log:

| 字段          | 类型   | 约束            | 说明                                                                            |
| ------------- | ------ | --------------- | ------------------------------------------------------------------------------- |
| id            | int64  | PK              | 主键                                                                            |
| user_name     | string | NOT NULL, INDEX | 访问者用户名                                                                    |
| agent_user_id | uint64 | NOT NULL, INDEX | Agent 的 User ID                                                                |
| operation     | string | NOT NULL, INDEX | 操作类型：ssh_direct / k8s_direct / svc_direct / ssh_jump / k8s_jump / svc_jump |
| target_name   | string | NOT NULL        | 目标名称（Agent 名或 Endpoint 名）                                              |
| target_detail | string |                 | 详情（SSH 用户名、namespace、service 等）                                       |
| source_ip     | string |                 | 来源 IP                                                                         |
| result        | string | NOT NULL        | success / denied / error                                                        |
| error_message | string |                 | 错误信息                                                                        |
| created_at    | time   | INDEX           | 时间                                                                            |

索引：user_name、agent_user_id、operation、created_at。

## 表总览

| 分类       | 现有 | 新增 | 合计 |
| ---------- | ---- | ---- | ---- |
| 身份与设备 | 5    | 0    | 5    |
| 认证       | 3    | 0    | 3    |
| 服务       | 3    | 0    | 3    |
| ACL        | 8    | 10   | 18   |
| Endpoint   | 0    | 3    | 3    |
| 域名       | 0    | 1    | 1    |
| 审计       | 1    | 1    | 2    |
| 辅助       | 3    | 0    | 3    |
| 合计       | 23   | 15   | 38   |

## 迁移策略

GORM AutoMigrate 自动创建新表，现有表不做结构变更。

迁移顺序：

1. P0 — domain_registry（域名体系依赖）
2. P1 — acl*k8s*\*\_permission（4 张，AgentK8SAPI/K8S Service 授权）
3. P2 — endpoint*\*（3 张）+ acl*_*jump*_\_permission（6 张）+ operation_audit_log
