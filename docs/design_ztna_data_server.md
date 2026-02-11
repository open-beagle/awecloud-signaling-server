# ZTNA Server 业务实体设计

## 概述

本文档以业务实体为单位，定义 Server 如何从多个数据源组装页面所需的数据。

每个业务实体对应 Web 页面上的一个列表或详情视图。实体不等于数据库表，它是 DB 表 + 内存缓存 + Headscale 数据 + Agent gRPC 上报的组合。

业务实体的命名严格对齐 Web 页面路由和视图文件。

相关业务设计文档：

- `design_ztna_server_user.md` — User 实体的业务逻辑（创建/删除/编辑/注册流程，数据一致性）
- `design_ztna_server_device.md` — Node 实体的业务逻辑（创建/删除/心跳/生命周期）
- `design_ztna_server_heartbeat.md` — Node 心跳优化（NodeCache 减少 DB 读写，影响 Node.List 数据时效性）

## 数据源分类

| 缩写 | 数据源     | 说明                            |
| ---- | ---------- | ------------------------------- |
| DB   | SQLite     | 持久化表，GORM 查询             |
| C    | Cache      | Server 内存缓存（map，进程内）  |
| HS   | Headscale  | Headscale gRPC 查询（外部系统） |
| gRPC | Agent 上报 | Agent 心跳流实时上报的数据      |

## User 实体

User 是统一身份模型，agent 和 client 共用 User 表。列表页统一展示，详情页根据 role 展示不同内容。

### User.List

页面：用户管理（/users），agent 和 client 统一列表，支持角色/启用/来源筛选。

| 字段       | 数据源 | 来源表/缓存     | 说明                 |
| ---------- | ------ | --------------- | -------------------- |
| id         | DB     | user.id         | 用户 ID              |
| name       | DB     | user.name       | 用户名               |
| alias      | DB     | user.alias      | 别名                 |
| role       | DB     | user.role       | 角色（agent/client） |
| enabled    | DB     | user.enabled    | 是否启用             |
| source     | DB     | user.source     | 来源（manual/logto） |
| status     | DB     | user.status     | 审批状态             |
| created_at | DB     | user.created_at | 创建时间             |

组装策略：纯 DB 查询。User.List 是管理维度的统一视图，不需要实时状态。

说明：现有的 Agent 列表页（/agents）和 Client 列表页（/clients）本质上是 User.List 按 role 筛选后加上各自的关联数据（服务数、设备数、在线状态等），不是独立实体。

### User.Agent.Detail

页面：Agent 详情（/agents/:id），包含基本信息、所属分组、设备列表、服务列表、转发列表。

Agent 是用户，IP/hostname/version/隧道状态等属于设备（Node），不在用户基本信息中。一个 Agent 用户可以有多个设备。

基本信息：

| 字段       | 数据源 | 来源表/缓存     | 说明                 |
| ---------- | ------ | --------------- | -------------------- |
| id         | DB     | user.id         | Agent ID             |
| name       | DB     | user.name       | 名称                 |
| alias      | DB     | user.alias      | 别名                 |
| enabled    | DB     | user.enabled    | 是否启用             |
| source     | DB     | user.source     | 来源（manual/logto） |
| created_at | DB     | user.created_at | 创建时间             |

所属分组：

| 字段   | 数据源 | 来源表/缓存          | 说明     |
| ------ | ------ | -------------------- | -------- |
| groups | DB     | group_member + group | 分组列表 |

设备列表：

| 字段                  | 数据源 | 来源表/缓存                 | 说明                            |
| --------------------- | ------ | --------------------------- | ------------------------------- |
| id                    | DB     | node.id                     | 设备 ID                         |
| hostname              | C      | AgentTsStatus.Hostname      | 主机名                          |
| ip                    | DB     | node.ip                     | 隧道 IP                         |
| version               | DB     | node.version                | Agent 版本                      |
| runtime               | C      | AgentTsStatus.Runtime       | 运行环境（docker/k8s/physical） |
| networks              | C      | AgentTsStatus.Networks      | 网络接口列表                    |
| tunnel_ip             | C      | AgentTsStatus.TunnelIP      | 隧道 IP                         |
| tunnel_connected      | C      | AgentTsStatus.TsConnectedAt | 是否已连接                      |
| tunnel_connected_time | C      | AgentTsStatus.TsConnectedAt | 连接时间                        |
| status                | C      | AgentTsStatus               | online/offline                  |
| last_heartbeat        | DB     | node.last_heartbeat         | 最后心跳时间                    |
| tags                  | HS     | Headscale Node.ForcedTags   | 节点 Tag 列表                   |

服务列表（端口映射）：

| 字段           | 数据源 | 来源表/缓存               | 说明                                               |
| -------------- | ------ | ------------------------- | -------------------------------------------------- |
| id             | DB     | proxy_service.id          | 服务 ID                                            |
| name           | DB     | proxy_service.name        | 服务名称                                           |
| alias          | DB     | proxy_service.alias       | 别名                                               |
| source_addr    | DB     | proxy_service.source_addr | 源地址                                             |
| target_addr    | DB     | proxy_service.target_addr | 目标地址                                           |
| enabled        | DB     | proxy_service.enabled     | 是否启用                                           |
| display_status | C      | ServiceRuntimeStatus      | 显示状态（running/stopped/error/disabled/offline） |
| error_msg      | C      | ServiceRuntimeStatus      | 错误信息                                           |

转发列表（远程服务）：

| 字段                | 数据源 | 来源表/缓存              | 说明                                               |
| ------------------- | ------ | ------------------------ | -------------------------------------------------- |
| id                  | DB     | port_forward.id          | 转发 ID                                            |
| name                | DB     | port_forward.name        | 名称                                               |
| alias               | DB     | port_forward.alias       | 别名                                               |
| target_agent_name   | DB     | user.name (JOIN)         | 目标 Agent 名称                                    |
| target_service_name | DB     | proxy_service.name       | 目标服务名称                                       |
| source_addr         | DB     | port_forward.source_addr | 源地址                                             |
| target_addr         | DB     | port_forward.target_addr | 目标地址                                           |
| enabled             | DB     | port_forward.enabled     | 是否启用                                           |
| display_status      | C      | ServiceRuntimeStatus     | 显示状态（running/stopped/error/disabled/offline） |
| error_msg           | C      | ServiceRuntimeStatus     | 错误信息                                           |

ZTNA 新增（Agent 详情扩展）：

| 字段         | 数据源     | 来源表/缓存                        | 说明                   |
| ------------ | ---------- | ---------------------------------- | ---------------------- |
| k8s_services | C(新增)    | K8SServiceDiscoveryCache           | 自动发现的 K8S Service |
| endpoints    | DB+C(新增) | endpoint\_\* + EndpointStatusCache | Endpoint 列表          |

组装策略：查询时组装。基本信息纯 DB，设备列表从 Node 表查询后用 Cache 补充实时状态（hostname、runtime、网络、隧道），Headscale Tags 按设备维度按需查询。服务和转发列表从 DB 查询后用 Cache 补充运行状态。

### User.Client.Detail

页面：Client 详情（/clients/:id），包含基本信息、所属分组、设备列表、已授权服务。

基本信息：

| 字段       | 数据源 | 来源表/缓存     | 说明      |
| ---------- | ------ | --------------- | --------- |
| id         | DB     | user.id         | Client ID |
| name       | DB     | user.name       | 用户名    |
| alias      | DB     | user.alias      | 别名      |
| created_at | DB     | user.created_at | 创建时间  |

所属分组：

| 字段   | 数据源 | 来源表/缓存          | 说明     |
| ------ | ------ | -------------------- | -------- |
| groups | DB     | group_member + group | 分组列表 |

设备列表：

| 字段        | 数据源 | 来源表/缓存         | 说明           |
| ----------- | ------ | ------------------- | -------------- |
| id          | DB     | node.id             | 设备 ID        |
| device_name | DB     | node.hostname       | 设备名称       |
| tunnel_ip   | C      | AgentTsStatus       | 隧道 IP        |
| status      | C      | AgentTsStatus       | online/offline |
| last_online | DB     | node.last_heartbeat | 最后在线时间   |

已授权服务：

| 字段        | 数据源 | 来源表/缓存                                 | 说明       |
| ----------- | ------ | ------------------------------------------- | ---------- |
| name        | DB     | proxy_service.name                          | 服务名称   |
| agent_name  | DB     | user.name (JOIN)                            | 所属 Agent |
| listen_addr | DB     | proxy_service.source_addr                   | 访问地址   |
| auth_type   | DB     | acl_service/user/group\_\*\_permission 合并 | 授权方式   |
| granted_at  | DB     | \*.granted_at                               | 授权时间   |

组装策略：查询时组装。基本信息纯 DB，设备列表从 Cache 补充在线状态，已授权服务需要查询多张 ACL 表合并。

## AgentService 实体

### AgentService.List

页面：服务列表（/services）

| 字段           | 数据源 | 来源表/缓存               | 说明                                               |
| -------------- | ------ | ------------------------- | -------------------------------------------------- |
| id             | DB     | proxy_service.id          | 服务 ID                                            |
| name           | DB     | proxy_service.name        | 服务名称                                           |
| alias          | DB     | proxy_service.alias       | 别名                                               |
| source_addr    | DB     | proxy_service.source_addr | 源地址（VPN IP:端口）                              |
| target_addr    | DB     | proxy_service.target_addr | 目标地址（内网地址）                               |
| enabled        | DB     | proxy_service.enabled     | 是否启用                                           |
| agent_name     | DB     | user.name (JOIN)          | 所属 Agent 名称                                    |
| agent_online   | C      | AgentTsStatus             | Agent 是否在线                                     |
| display_status | C      | ServiceRuntimeStatus      | 显示状态（running/stopped/error/disabled/offline） |

组装策略：查询时组装。DB 查询 ProxyService JOIN User，遍历结果从 Cache 补充 Agent 在线状态和服务运行状态，通过 GetDisplayStatus 计算最终显示状态。

### AgentService.Detail

页面：服务详情（/services/:id）

在 AgentService.List 基础上增加：

| 字段       | 数据源 | 来源表/缓存                          | 说明         |
| ---------- | ------ | ------------------------------------ | ------------ |
| acl_users  | DB     | acl_service_user_permission + user   | 授权用户列表 |
| acl_groups | DB     | acl_service_group_permission + group | 授权分组列表 |
| error_msg  | C      | ServiceRuntimeStatus.ErrorMsg        | 错误信息     |

组装策略：查询时组装。

## K8SService 实体（新增）

### K8SService.List

页面：K8S 服务列表（/k8s-services，新增页面）

| 字段          | 数据源 | 来源表/缓存              | 说明                  |
| ------------- | ------ | ------------------------ | --------------------- |
| service_name  | C      | K8SServiceDiscoveryCache | K8S Service 名称      |
| namespace     | C      | K8SServiceDiscoveryCache | 命名空间              |
| cluster_ip    | C      | K8SServiceDiscoveryCache | ClusterIP             |
| ports         | C      | K8SServiceDiscoveryCache | 端口列表              |
| domain        | DB     | domain_registry          | 分配的域名            |
| agent_name    | DB     | user.name                | 所属 Agent 名称       |
| agent_user_id | C      | K8SServiceDiscoveryCache | 所属 Agent 的 User ID |
| status        | C      | K8SServiceDiscoveryCache | online/offline        |

组装策略：缓存为主。K8S Service 是 Agent 通过心跳上报的运行时数据，不持久化到数据库。Server 维护 K8SServiceDiscoveryCache（map[agent_user_id][]K8SServiceInfo），心跳更新时刷新。域名从 domain_registry 补充。

说明：K8SServiceDiscoveryCache 是 ZTNA 新增的 Server 内存缓存，结构见下方"新增缓存"章节。

## Node 实体

### Node.List

页面：设备管理（/nodes），展示所有 Headscale 节点，按类型分为代理设备和桌面设备。

| 字段              | 数据源 | 来源表/缓存            | 说明                     |
| ----------------- | ------ | ---------------------- | ------------------------ |
| id                | DB     | node.id                | 设备 ID                  |
| name              | DB     | node.hostname          | 设备名称                 |
| user_name         | DB     | user.name (JOIN)       | 所属用户名               |
| user_role         | DB     | user.role (JOIN)       | 用户角色（agent/client） |
| ip                | DB     | node.ip                | IP 地址                  |
| hostname          | C      | AgentTsStatus          | 主机名（在线时）         |
| status            | C      | AgentTsStatus          | online/offline           |
| last_heartbeat    | DB     | node.last_heartbeat    | 最后心跳时间             |
| headscale_node_id | DB     | node.headscale_node_id | Headscale 节点 ID        |

组装策略：查询时组装。DB 查询 Node JOIN User，遍历结果从 Cache 补充在线状态。

## Endpoint 实体（新增）

### Endpoint.List

页面：Endpoint 列表（/endpoints/\*，按类型分三个子页面：ssh/k8s/svc）

| 字段       | 数据源 | 来源表/缓存                | 说明                      |
| ---------- | ------ | -------------------------- | ------------------------- |
| id         | DB     | endpoint\_\*.id            | Endpoint ID               |
| name       | DB     | endpoint\_\*.name          | 名称                      |
| alias      | DB     | endpoint\_\*.alias         | 别名                      |
| type       | —      | 由查询的表决定             | ssh / k8sapi / k8sservice |
| agent_name | DB     | user.name (JOIN)           | 所属 Agent 名称           |
| host       | DB     | endpoint_ssh.host          | 内网地址（SSH 类型）      |
| api_server | DB     | endpoint_k8sapi.api_server | API 地址（K8SAPI 类型）   |
| domain     | DB     | domain_registry            | 自动生成的域名            |
| status     | C      | EndpointStatusCache        | online/offline            |
| enabled    | DB     | endpoint\_\*.enabled       | 是否启用                  |

组装策略：查询时组装。DB 查询对应类型的 endpoint 表 JOIN User，Cache 补充在线状态。

说明：EndpointStatusCache 是 ZTNA 新增的 Server 内存缓存，Agent 通过心跳转发 Endpoint 的在线状态。

### Endpoint.Detail

页面：Endpoint 详情（各授权详情页中展示）

在 Endpoint.List 基础上增加：

| 字段         | 数据源 | 来源表/缓存                            | 说明                                   |
| ------------ | ------ | -------------------------------------- | -------------------------------------- |
| port         | DB     | endpoint_ssh.port                      | 端口（SSH 类型）                       |
| acl_users    | DB     | acl\_\*\_jump_user_permission + user   | 授权用户列表                           |
| acl_groups   | DB     | acl\_\*\_jump_group_permission + group | 授权分组列表                           |
| k8s_services | C      | EndpointK8SServiceCache                | 发现的 Service 列表（k8sservice 类型） |

组装策略：查询时组装。

## Group 实体

### Group.List

页面：分组列表（/groups）

| 字段         | 数据源 | 来源表/缓存         | 说明     |
| ------------ | ------ | ------------------- | -------- |
| id           | DB     | group.id            | 分组 ID  |
| name         | DB     | group.name          | 名称     |
| alias        | DB     | group.alias         | 别名     |
| description  | DB     | group.description   | 描述     |
| member_count | DB     | COUNT(group_member) | 成员数量 |

组装策略：纯 DB 查询。

### Group.Detail

页面：分组详情（/groups/:id）

在 Group.List 基础上增加：

| 字段    | 数据源 | 来源表/缓存                         | 说明                   |
| ------- | ------ | ----------------------------------- | ---------------------- |
| members | DB+C   | group_member + user + AgentTsStatus | 成员列表（含在线状态） |

组装策略：查询时组装。

## ACL 实体

所有 ACL 列表页结构类似，纯 DB 查询 + JOIN 关联表获取名称。

### ACL.ServiceList

| 字段            | 数据源 | 来源表                         |
| --------------- | ------ | ------------------------------ |
| id              | DB     | acl_service\_\*\_permission.id |
| service_name    | DB     | proxy_service.name (JOIN)      |
| agent_name      | DB     | user.name (JOIN)               |
| user/group_name | DB     | user.name / group.name         |
| granted_at      | DB     | \*.granted_at                  |

其他 ACL 列表（UserList、GroupList、SSHList、K8SList、K8SSvcList、JumpList）结构类似，区别在于 JOIN 的目标表不同。不逐一展开。

## Resource 实体（新增）

### Resource.List

页面：资源发现（/resources），展示所有 AgentK8SService 和 EndpointK8SService 自动发现的 K8S Service 汇总。

| 字段          | 数据源 | 来源表/缓存                                        | 说明                                     |
| ------------- | ------ | -------------------------------------------------- | ---------------------------------------- |
| domain        | DB     | domain_registry.domain                             | 完整域名                                 |
| source        | —      | 由数据来源决定                                     | agent_k8s_service / endpoint_k8s_service |
| service_name  | C      | K8SServiceDiscoveryCache / EndpointK8SServiceCache | K8S Service 名称                         |
| namespace     | C      | 同上                                               | 命名空间                                 |
| ports         | C      | 同上                                               | 端口列表                                 |
| agent_name    | DB     | user.name (JOIN)                                   | 所属 Agent 名称                          |
| endpoint_name | DB     | endpoint_k8sservice.name                           | 所属 Endpoint 名称（Endpoint 来源时）    |
| status        | C      | 同上                                               | online/offline                           |

组装策略：缓存为主。合并 K8SServiceDiscoveryCache 和 EndpointK8SServiceCache 的数据，从 domain_registry 补充域名。

## Domain 实体（新增）

### Domain.List

页面：域名列表（/domains，新增页面）

| 字段          | 数据源 | 来源表/缓存                 | 说明            |
| ------------- | ------ | --------------------------- | --------------- |
| id            | DB     | domain_registry.id          | ID              |
| domain        | DB     | domain_registry.domain      | 完整域名        |
| type          | DB     | domain_registry.type        | 类型            |
| agent_name    | DB     | user.name (JOIN)            | 所属 Agent 名称 |
| endpoint_name | DB     | endpoint\_\*.name (JOIN)    | Endpoint 名称   |
| target_port   | DB     | domain_registry.target_port | 目标端口        |
| status        | DB     | domain_registry.status      | online/offline  |
| created_at    | DB     | domain_registry.created_at  | 注册时间        |

组装策略：纯 DB 查询。域名状态由 Agent 心跳上报时同步更新到 DB。

## DeployToken 实体

### DeployToken.List

页面：部署 Token 列表（/deploy-tokens）

| 字段            | 数据源 | 来源表                     | 说明                  |
| --------------- | ------ | -------------------------- | --------------------- |
| id              | DB     | deploy_tokens.id           | ID                    |
| name            | DB     | deploy_tokens.name         | Token 名称            |
| user_name       | DB     | user.name (JOIN)           | 关联用户名            |
| user_role       | DB     | user.role (JOIN)           | 用户角色              |
| status          | DB     | deploy_tokens.status       | pending/bound/revoked |
| device_name     | DB     | deploy_tokens.device_name  | 绑定设备名            |
| created_by_name | DB     | admin.username (JOIN)      | 创建人                |
| created_at      | DB     | deploy_tokens.created_at   | 创建时间              |
| last_used_at    | DB     | deploy_tokens.last_used_at | 最后使用时间          |

组装策略：纯 DB 查询。

## AuditLog 实体

### AuditLog.List

页面：审计日志（/audit-logs）

| 字段     | 数据源 | 来源表    | 说明       |
| -------- | ------ | --------- | ---------- |
| 全部字段 | DB     | audit_log | 纯 DB 查询 |

ZTNA 增强后新增 operation_audit_log 表，增加操作类型、Agent、Endpoint、目标详情等字段。

组装策略：纯 DB 查询。

## 新增缓存

ZTNA 需要在 Server 新增以下内存缓存：

| 缓存名                   | 键                     | 值                             | 更新来源       |
| ------------------------ | ---------------------- | ------------------------------ | -------------- |
| K8SServiceDiscoveryCache | agent_user_id          | []K8SServiceInfo               | Agent 心跳上报 |
| EndpointStatusCache      | endpoint_id            | EndpointStatus(online/offline) | Agent 心跳转发 |
| EndpointK8SServiceCache  | endpoint_k8sservice_id | []K8SServiceInfo               | Agent 心跳转发 |

K8SServiceInfo 结构：

| 字段         | 说明             |
| ------------ | ---------------- |
| service_name | K8S Service 名称 |
| namespace    | 命名空间         |
| cluster_ip   | ClusterIP        |
| ports        | 端口列表         |
| labels       | 标签             |
| status       | online/offline   |

现有缓存（不变）：

| 缓存名               | 说明                          |
| -------------------- | ----------------------------- |
| AgentTsStatus        | Agent 隧道状态和网络信息      |
| ServiceRuntimeStatus | 服务运行状态（proxy/forward） |
