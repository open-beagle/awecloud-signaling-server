# 域名服务生命周期设计 V2

## 1. 概述

域名服务是 ZTNA 体系中的核心概念，代表一个可访问的网络服务入口。

### 核心原则

- **域名由管理员创建**：管理员在 Web 界面开启设备能力时，自动创建域名
- **域名随能力消亡**：管理员关闭设备能力时，自动删除域名
- **状态存储在内存**：域名状态不依赖数据库字段，而是存储在 Server 内存缓存中
- **状态由设备触发**：域名状态由设备连接/断连、心跳等业务触发变更
- **状态实时判断**：通过心跳超时和 Headscale IP 查询两种方式判断在线状态

### 域名类型

| 类型   | 说明                | 来源设备      | 端口示例 |
| ------ | ------------------- | ------------- | -------- |
| ssh    | SSH 访问            | Node/Endpoint | 22/50053 |
| k8sapi | Kubernetes API 访问 | Node/Endpoint | 6443     |
| k8ssvc | Kubernetes Service  | Node          | 5432     |

### 数据存储

- **domain_registry 表**：持久化域名记录
- **NodeStatusCache**：内存缓存，存储 Node 状态
- **EndpointStatusCache**：内存缓存，存储 Endpoint 状态

注意：domain_registry 表中不存储 status 字段，状态完全由内存缓存管理。

## 2. 核心业务

### 2.1 域名数据创建

#### 业务 2.1.1：管理员开启 Node SSH 能力

触发条件：管理员在 Web 界面开启 Node 的 SSH 能力

```
管理员在 Web 界面操作
  │
  ├─→ PATCH /api/nodes/44
  │     {ssh_enabled: true}
  │
  ├─→ Server 处理 (UpdateNode)
  │     │
  │     ├─→ 更新 Node 配置
  │     │     UPDATE node SET ssh_enabled=true WHERE id=44
  │     │
  │     ├─→ 生成域名
  │     │     domain = "{node_name}.{region}.beagle"
  │     │     例如：beagle-242.beijing.beagle
  │     │
  │     ├─→ 创建域名记录
  │     │     INSERT INTO domain_registry (
  │     │       domain, type, user_id, node_id,
  │     │       target_ip, target_port,
  │     │       created_at, updated_at
  │     │     ) VALUES (
  │     │       'beagle-242.beijing.beagle', 'ssh', 7, 44,
  │     │       '100.64.0.23', 22,
  │     │       now(), now()
  │     │     )
  │     │
  │     └─→ 通知 Agent 配置变更（通过心跳响应或推送）
  │
  ├─→ Agent 收到配置更新
  │     │
  │     └─→ 启动 SSH 代理服务
  │           监听 Tailscale 网络的 22 端口
  │
  └─→ SSH 域名创建完成
```

数据变化：

- domain_registry 表：新增记录，type='ssh', node_id=44, target_port=22
- NodeStatusCache：如果 Agent 在线，缓存已存在

状态判断：

- 如果 Agent 在线（NodeStatusCache[44] 存在且心跳正常）→ status = online
- 如果 Agent 离线（NodeStatusCache[44] 不存在）→ status = offline

#### 业务 2.1.2：管理员开启 Node K8SAPI 能力

触发条件：管理员在 Web 界面开启 Node 的 K8SAPI 能力

```
管理员在 Web 界面操作
  │
  ├─→ PATCH /api/nodes/44
  │     {k8sapi_enabled: true}
  │
  ├─→ Server 处理 (UpdateNode)
  │     │
  │     ├─→ 更新 Node 配置
  │     │     UPDATE node SET k8sapi_enabled=true WHERE id=44
  │     │
  │     ├─→ 生成域名
  │     │     domain = "kubernetes.{region}.beagle"
  │     │     例如：kubernetes.beijing.beagle
  │     │
  │     ├─→ 创建域名记录
  │     │     INSERT INTO domain_registry (
  │     │       domain, type, user_id, node_id,
  │     │       target_ip, target_port,
  │     │       created_at, updated_at
  │     │     ) VALUES (
  │     │       'kubernetes.beijing.beagle', 'k8sapi', 7, 44,
  │     │       '100.64.0.23', 6443,
  │     │       now(), now()
  │     │     )
  │     │
  │     └─→ 通知 Agent 配置变更
  │
  ├─→ Agent 收到配置更新
  │     │
  │     └─→ 启动 K8S API 代理服务
  │           监听 Tailscale 网络的 6443 端口
  │
  └─→ K8SAPI 域名创建完成
```

数据变化：

- domain_registry 表：新增记录，type='k8sapi', node_id=44, target_port=6443

状态判断：

- 通过 NodeStatusCache[44] 判断 Agent 在线状态

#### 业务 2.1.3：管理员开启 Node K8SSVC 能力（自动发现）

触发条件：管理员在 Web 界面开启 Node 的 K8SSVC 能力

```
管理员在 Web 界面操作
  │
  ├─→ PATCH /api/nodes/44
  │     {k8sservice_enabled: true}
  │
  ├─→ Server 处理 (UpdateNode)
  │     │
  │     ├─→ 更新 Node 配置
  │     │     UPDATE node SET k8sservice_enabled=true WHERE id=44
  │     │
  │     └─→ 通知 Agent 配置变更
  │
  ├─→ Agent 收到配置更新
  │     │
  │     ├─→ 启动 K8S Service 自动发现
  │     │     定期扫描 K8S 集群中的 Service
  │     │
  │     └─→ 下次心跳上报发现的 Service
  │           discovered_services: [
  │             {
  │               namespace: "yygl",
  │               service_name: "postgres",
  │               cluster_ip: "10.96.0.10",
  │               ports: [5432]
  │             }
  │           ]
  │
  ├─→ Server 处理心跳 (handleDiscoveredServices)
  │     │
  │     └─→ 对每个 discovered_service:
  │           │
  │           ├─→ 生成域名
  │           │     domain = "{service_name}.{namespace}.{region}.beagle"
  │           │     例如：postgres.yygl.beijing.beagle
  │           │
  │           ├─→ 查询是否已存在
  │           │     SELECT * FROM domain_registry
  │           │     WHERE domain=? AND user_id=?
  │           │
  │           ├─→ 不存在 → 创建新记录
  │           │     INSERT INTO domain_registry (
  │           │       domain, type, user_id, node_id,
  │           │       target_ip, target_port,
  │           │       namespace, service_name,
  │           │       created_at, updated_at
  │           │     ) VALUES (
  │           │       'postgres.yygl.beijing.beagle', 'k8ssvc', 7, 44,
  │           │       '10.96.0.10', 5432,
  │           │       'yygl', 'postgres',
  │           │       now(), now()
  │           │     )
  │           │
  │           └─→ 已存在 → 更新记录
  │                 UPDATE domain_registry SET
  │                   target_ip=?, target_port=?, updated_at=now()
  │                 WHERE domain=? AND user_id=?
  │
  └─→ K8SSVC 域名创建完成
```

数据变化：

- domain_registry 表：新增记录，type='k8ssvc', node_id=44, namespace='yygl', service_name='postgres'

状态判断：

- 通过 NodeStatusCache[44] 判断 Agent 在线状态

说明：

- K8SSVC 域名是自动发现的，不是手动创建
- Agent 定期扫描 K8S 集群，通过心跳上报新发现的 Service
- Server 根据上报的 Service 自动创建域名记录

#### 业务 2.1.4：管理员创建 Endpoint SSH 能力

触发条件：管理员在 Web 界面创建 Endpoint

```
管理员在 Web 界面操作
  │
  ├─→ POST /api/endpoints
  │     {
  │       name: "beagle-241",
  │       user_id: 7,
  │       host: "192.168.1.100",
  │       port: 22,
  │       ssh_enabled: true
  │     }
  │
  ├─→ Server 处理 (CreateEndpoint)
  │     │
  │     ├─→ 创建 Endpoint 记录
  │     │     INSERT INTO endpoint (
  │     │       name, user_id, host, port, ssh_enabled
  │     │     ) VALUES (
  │     │       'beagle-241', 7, '192.168.1.100', 22, true
  │     │     )
  │     │
  │     ├─→ 生成域名
  │     │     domain = "{endpoint_name}.{region}.beagle"
  │     │     例如：beagle-241.beijing.beagle
  │     │
  │     ├─→ 创建域名记录（暂不填充 node_id）
  │     │     INSERT INTO domain_registry (
  │     │       domain, type, user_id, node_id, endpoint_id,
  │     │       target_ip, target_port,
  │     │       created_at, updated_at
  │     │     ) VALUES (
  │     │       'beagle-241.beijing.beagle', 'ssh', 7, 0, 'beagle-241',
  │     │       '', 50053,
  │     │       now(), now()
  │     │     )
  │     │     注意：node_id=0，等待 Endpoint 连接后填充
  │     │
  │     └─→ 生成 Endpoint Token（用于 Endpoint 连接 Agent）
  │
  ├─→ 管理员在 Endpoint 机器上安装并启动 Endpoint
  │     使用 Token 连接到 Agent
  │
  ├─→ Endpoint 连接到 Agent
  │     │
  │     └─→ Agent 下次心跳上报
  │           connected_endpoints: ["beagle-241"]
  │
  ├─→ Server 处理心跳 (handleConnectedEndpoints)
  │     │
  │     ├─→ 更新内存缓存
  │     │     EndpointStatusCache["beagle-241"] = {
  │     │       EndpointName: "beagle-241",
  │     │       UserID: 7,
  │     │       LastHeartbeat: now()
  │     │     }
  │     │
  │     └─→ 更新域名记录，填充 node_id
  │           UPDATE domain_registry SET
  │             node_id=44, target_ip='100.64.0.23', updated_at=now()
  │           WHERE endpoint_id='beagle-241' AND user_id=7
  │
  └─→ Endpoint SSH 域名创建完成
```

数据变化：

- endpoint 表：新增记录
- domain_registry 表：新增记录，type='ssh', node_id=44（连接后填充）, endpoint_id='beagle-241', target_port=50053
- EndpointStatusCache：新增缓存（Endpoint 连接后）

状态判断：

- 如果 Endpoint 未连接（EndpointStatusCache["beagle-241"] 不存在）→ status = offline
- 如果 Endpoint 已连接（EndpointStatusCache["beagle-241"] 存在且心跳正常）→ status = online

说明：

- Endpoint 域名的 target_port 固定为 50053（Agent 的 Endpoint gRPC Server 端口）
- node_id 在 Endpoint 连接到 Agent 后才能填充
- Endpoint 域名需要同时填充 node_id（Agent ID）和 endpoint_id（Endpoint 名称）

#### 业务 2.1.5：管理员创建 Endpoint K8SAPI 能力

触发条件：管理员在 Web 界面创建 Endpoint 并开启 K8SAPI 能力

```
管理员在 Web 界面操作
  │
  ├─→ POST /api/endpoints
  │     {
  │       name: "beagle-002",
  │       user_id: 8,
  │       api_server: "https://192.168.1.200:6443",
  │       k8sapi_enabled: true
  │     }
  │
  ├─→ Server 处理 (CreateEndpoint)
  │     │
  │     ├─→ 创建 Endpoint 记录
  │     │     INSERT INTO endpoint_k8sapi (
  │     │       name, user_id, api_server, k8sapi_enabled
  │     │     ) VALUES (
  │     │       'beagle-002', 8, 'https://192.168.1.200:6443', true
  │     │     )
  │     │
  │     ├─→ 生成域名
  │     │     domain = "kubernetes-{endpoint_name}.{region}.beagle"
  │     │     例如：kubernetes-beagle-002.neimeng.beagle
  │     │
  │     ├─→ 创建域名记录（暂不填充 node_id）
  │     │     INSERT INTO domain_registry (
  │     │       domain, type, user_id, node_id, endpoint_id,
  │     │       target_ip, target_port,
  │     │       created_at, updated_at
  │     │     ) VALUES (
  │     │       'kubernetes-beagle-002.neimeng.beagle', 'k8sapi', 8, 0, 'beagle-002',
  │     │       '', 50053,
  │     │       now(), now()
  │     │     )
  │     │
  │     └─→ 生成 Endpoint Token
  │
  ├─→ Endpoint 连接到 Agent 后，Server 更新 node_id
  │     UPDATE domain_registry SET
  │       node_id=45, target_ip='100.64.0.22', updated_at=now()
  │     WHERE endpoint_id='beagle-002' AND user_id=8
  │
  └─→ Endpoint K8SAPI 域名创建完成
```

数据变化：

- endpoint_k8sapi 表：新增记录
- domain_registry 表：新增记录，type='k8sapi', node_id=45（连接后填充）, endpoint_id='beagle-002'
- EndpointStatusCache：新增缓存（Endpoint 连接后）

状态判断：

- 通过 EndpointStatusCache["beagle-002"] 判断 Endpoint 在线状态

### 2.2 域名数据删除

#### 业务 2.2.1：管理员关闭 Node SSH 能力

触发条件：管理员在 Web 界面关闭 Node 的 SSH 能力

```
管理员在 Web 界面操作
  │
  ├─→ PATCH /api/nodes/44
  │     {ssh_enabled: false}
  │
  ├─→ Server 处理 (UpdateNode)
  │     │
  │     ├─→ 更新 Node 配置
  │     │     UPDATE node SET ssh_enabled=false WHERE id=44
  │     │
  │     ├─→ 删除 SSH 域名记录
  │     │     DELETE FROM domain_registry
  │     │     WHERE node_id=44 AND type='ssh' AND endpoint_id=''
  │     │
  │     └─→ 通知 Agent 配置变更
  │
  ├─→ Agent 收到配置更新
  │     │
  │     └─→ 停止 SSH 代理服务
  │
  └─→ SSH 域名删除完成
```

数据变化：

- domain_registry 表：删除 node_id=44 且 type='ssh' 且 endpoint_id='' 的记录

#### 业务 2.2.2：管理员关闭 Node K8SAPI 能力

触发条件：管理员在 Web 界面关闭 Node 的 K8SAPI 能力

```
管理员在 Web 界面操作
  │
  ├─→ PATCH /api/nodes/44
  │     {k8sapi_enabled: false}
  │
  ├─→ Server 处理 (UpdateNode)
  │     │
  │     ├─→ 更新 Node 配置
  │     │     UPDATE node SET k8sapi_enabled=false WHERE id=44
  │     │
  │     ├─→ 删除 K8SAPI 域名记录
  │     │     DELETE FROM domain_registry
  │     │     WHERE node_id=44 AND type='k8sapi' AND endpoint_id=''
  │     │
  │     └─→ 通知 Agent 配置变更
  │
  ├─→ Agent 收到配置更新
  │     │
  │     └─→ 停止 K8S API 代理服务
  │
  └─→ K8SAPI 域名删除完成
```

数据变化：

- domain_registry 表：删除 node_id=44 且 type='k8sapi' 且 endpoint_id='' 的记录

#### 业务 2.2.3：管理员关闭 Node K8SSVC 能力

触发条件：管理员在 Web 界面关闭 Node 的 K8SSVC 能力

```
管理员在 Web 界面操作
  │
  ├─→ PATCH /api/nodes/44
  │     {k8sservice_enabled: false}
  │
  ├─→ Server 处理 (UpdateNode)
  │     │
  │     ├─→ 更新 Node 配置
  │     │     UPDATE node SET k8sservice_enabled=false WHERE id=44
  │     │
  │     ├─→ 删除所有 K8SSVC 域名记录
  │     │     DELETE FROM domain_registry
  │     │     WHERE node_id=44 AND type='k8ssvc'
  │     │
  │     └─→ 通知 Agent 配置变更
  │
  ├─→ Agent 收到配置更新
  │     │
  │     └─→ 停止 K8S Service 自动发现
  │
  └─→ 所有 K8SSVC 域名删除完成
```

数据变化：

- domain_registry 表：删除 node_id=44 且 type='k8ssvc' 的所有记录

#### 业务 2.2.4：管理员删除 Endpoint

触发条件：管理员在 Web 界面删除 Endpoint

```
管理员在 Web 界面操作
  │
  ├─→ DELETE /api/endpoints/beagle-241
  │
  ├─→ Server 处理 (DeleteEndpoint)
  │     │
  │     ├─→ 标记 Endpoint 为已撤销
  │     │     UPDATE endpoint SET revoked=true WHERE name='beagle-241'
  │     │
  │     ├─→ 删除关联域名记录
  │     │     DELETE FROM domain_registry
  │     │     WHERE endpoint_id='beagle-241'
  │     │
  │     ├─→ 清理内存缓存
  │     │     DELETE EndpointStatusCache["beagle-241"]
  │     │
  │     └─→ 通知 Agent 断开 Endpoint 连接
  │
  ├─→ Agent 收到通知
  │     │
  │     └─→ 断开与 Endpoint 的连接
  │
  └─→ Endpoint 域名删除完成
```

数据变化：

- endpoint 表：revoked=true
- domain_registry 表：删除 endpoint_id='beagle-241' 的所有记录
- EndpointStatusCache：删除 endpoint_id='beagle-241' 的缓存

#### 业务 2.2.5：管理员删除 Node 设备

触发条件：管理员在 Web 界面删除 Node

```
管理员在 Web 界面操作
  │
  ├─→ DELETE /api/nodes/44
  │
  ├─→ Server 处理 (DeleteNode)
  │     │
  │     ├─→ 删除 Node 记录
  │     │     DELETE FROM node WHERE id=44
  │     │
  │     ├─→ 级联删除域名记录
  │     │     DELETE FROM domain_registry WHERE node_id=44
  │     │
  │     ├─→ 清理内存缓存
  │     │     DELETE NodeStatusCache[44]
  │     │
  │     └─→ 通知 Agent 断开连接（如果在线）
  │
  └─→ 所有关联域名删除完成
```

数据变化：

- node 表：删除记录
- domain_registry 表：删除所有 node_id=44 的记录（包括 Node 本机能力和 Endpoint 域名）
- NodeStatusCache：删除 node_id=44 的缓存

说明：

- 删除 Node 会同时删除该 Node 的所有域名（SSH、K8SAPI、K8SSVC）
- 删除 Node 也会删除连接到该 Node 的所有 Endpoint 域名

#### 业务 2.2.6：K8S Service 被删除（自动清理）

触发条件：K8S 集群中的 Service 被删除

```
K8S Service 被删除
  │
  ├─→ Agent 定期扫描 K8S 集群
  │     发现 Service 不存在
  │
  ├─→ Agent 下次心跳上报
  │     discovered_services: [
  │       // 不包含已删除的 Service
  │     ]
  │
  ├─→ Server 处理心跳 (handleDiscoveredServices)
  │     │
  │     ├─→ 查询数据库中该 Node 的所有 k8ssvc 域名
  │     │     SELECT * FROM domain_registry
  │     │     WHERE node_id=44 AND type='k8ssvc'
  │     │
  │     ├─→ 对比上报的 Service 列表
  │     │
  │     └─→ 删除不再存在的 Service 域名
  │           DELETE FROM domain_registry
  │           WHERE node_id=44 AND type='k8ssvc'
  │           AND service_name NOT IN (上报的 Service 列表)
  │
  └─→ K8SSVC 域名自动清理完成
```

数据变化：

- domain_registry 表：删除不再存在的 k8ssvc 记录

说明：

- K8SSVC 域名是自动发现的，也会自动清理
- Agent 定期扫描 K8S 集群，通过心跳上报当前存在的 Service
- Server 对比数据库记录，删除不再存在的 Service 域名

### 2.3 域名状态变更

#### 业务 2.3.1：Node 设备上线（Agent 启动并连接）

触发条件：Agent 启动后连接到 Server

```
Agent 启动
  │
  ├─→ 连接 Server gRPC (Heartbeat 流)
  │
  ├─→ 发送首次心跳消息
  │     ├─ hostname: "beagle-242"
  │     ├─ tunnel_ip: "100.64.0.23"
  │     └─ version: "v0.2.3"
  │
  ├─→ Server 处理心跳 (handleHeartbeat)
  │     │
  │     ├─→ 更新 Node 记录
  │     │     UPDATE node SET
  │     │       hostname=?, ip=?, version=?, last_heartbeat=now()
  │     │     WHERE id=44
  │     │
  │     └─→ 更新内存缓存
  │           NodeStatusCache[44] = {
  │             NodeID: 44,
  │             UserID: 7,
  │             TunnelIP: "100.64.0.23",
  │             LastHeartbeat: now()
  │           }
  │
  └─→ 域名状态变为 online
```

数据变化：

- NodeStatusCache：新增或更新缓存
- domain_registry 表：无变化（状态不存储在数据库）

状态判断：

- 查询域名时，通过 node_id=44 关联到 NodeStatusCache[44]
- NodeStatusCache[44] 存在且 LastHeartbeat < (now - 60s) → status = online

#### 业务 2.3.2：Node 设备持续在线（心跳正常）

触发条件：Agent 每 30 秒发送心跳

```
Agent 定时发送心跳（每 30 秒）
  │
  ├─→ 发送心跳消息
  │     ├─ hostname: "beagle-242"
  │     ├─ tunnel_ip: "100.64.0.23"
  │     └─ connected_endpoints: ["beagle-241"]
  │
  ├─→ Server 处理心跳 (handleHeartbeat)
  │     │
  │     ├─→ 更新 Node 记录
  │     │     UPDATE node SET last_heartbeat=now() WHERE id=44
  │     │
  │     └─→ 更新内存缓存
  │           NodeStatusCache[44].LastHeartbeat = now()
  │
  └─→ 域名状态保持 online
```

数据变化：

- NodeStatusCache：更新 LastHeartbeat
- domain_registry 表：无变化

状态判断：

- NodeStatusCache[44].LastHeartbeat < (now - 60s) → status = online

#### 业务 2.3.3：Node 设备断连（Agent 退出或网络断开）

触发条件：Agent 进程退出或网络断开

```
Agent 断开连接
  │
  ├─→ gRPC 流关闭
  │
  ├─→ Server 检测到连接断开
  │
  ├─→ 触发 defer 清理逻辑
  │     │
  │     ├─→ 清理内存缓存
  │     │     DELETE NodeStatusCache[44]
  │     │
  │     └─→ 清理 Node 数据（可选）
  │           UPDATE node SET ip='', last_heartbeat=NULL WHERE id=44
  │
  └─→ 域名状态变为 offline
```

数据变化：

- NodeStatusCache：删除 node_id=44 的缓存
- domain_registry 表：无变化

状态判断：

- 查询域名时，NodeStatusCache[44] 不存在 → status = offline

#### 业务 2.3.4：Endpoint 上线（连接到 Agent）

触发条件：Endpoint 启动后连接到 Agent

```
Endpoint 启动
  │
  ├─→ 连接 Agent gRPC (Endpoint 服务)
  │
  ├─→ Endpoint 注册自己的能力
  │     ├─ endpoint_name: "beagle-241"
  │     └─ capabilities: [ssh]
  │
  ├─→ Agent 下次心跳上报
  │     connected_endpoints: ["beagle-241"]
  │
  ├─→ Server 处理心跳 (handleConnectedEndpoints)
  │     │
  │     ├─→ 更新内存缓存
  │     │     EndpointStatusCache["beagle-241"] = {
  │     │       EndpointName: "beagle-241",
  │     │       UserID: 7,
  │     │       LastHeartbeat: now()
  │     │     }
  │     │
  │     └─→ 更新域名记录，填充 node_id（如果之前为 0）
  │           UPDATE domain_registry SET
  │             node_id=44, target_ip='100.64.0.23', updated_at=now()
  │           WHERE endpoint_id='beagle-241' AND user_id=7 AND node_id=0
  │
  └─→ Endpoint 域名状态变为 online
```

数据变化：

- EndpointStatusCache：新增缓存
- domain_registry 表：更新 node_id 和 target_ip（如果之前为 0）

状态判断：

- EndpointStatusCache["beagle-241"] 存在且 LastHeartbeat < (now - 60s) → status = online

#### 业务 2.3.5：Endpoint 持续在线（Agent 心跳转发）

触发条件：Agent 每 30 秒发送心跳，包含 connected_endpoints

```
Agent 定时发送心跳（每 30 秒）
  │
  ├─→ 发送心跳消息
  │     connected_endpoints: ["beagle-241"]
  │
  ├─→ Server 处理心跳 (handleConnectedEndpoints)
  │     │
  │     └─→ 更新内存缓存
  │           EndpointStatusCache["beagle-241"].LastHeartbeat = now()
  │
  └─→ Endpoint 域名状态保持 online
```

数据变化：

- EndpointStatusCache：更新 LastHeartbeat
- domain_registry 表：无变化

状态判断：

- EndpointStatusCache["beagle-241"].LastHeartbeat < (now - 60s) → status = online

#### 业务 2.3.6：Endpoint 断连（与 Agent 断开）

触发条件：Endpoint 进程退出或与 Agent 网络断开

```
Endpoint 断开与 Agent 的连接
  │
  ├─→ Agent 检测到 Endpoint 断连
  │
  ├─→ Agent 下次心跳上报
  │     connected_endpoints: [
  │       // 不包含 "beagle-241"
  │     ]
  │
  ├─→ Server 处理心跳 (handleConnectedEndpoints)
  │     │
  │     ├─→ 对比之前的 connected_endpoints
  │     │
  │     ├─→ 发现 "beagle-241" 不在列表中
  │     │
  │     └─→ 清理内存缓存
  │           DELETE EndpointStatusCache["beagle-241"]
  │
  └─→ Endpoint 域名状态变为 offline
```

数据变化：

- EndpointStatusCache：删除 endpoint_id="beagle-241" 的缓存
- domain_registry 表：无变化

状态判断：

- 查询域名时，EndpointStatusCache["beagle-241"] 不存在 → status = offline

#### 业务 2.3.7：心跳超时判断（被动检测）

触发条件：前端查询域名列表，Server 检测心跳超时

```
前端请求 GET /api/domains
  │
  ├─→ Server 查询 domain_registry 表
  │
  ├─→ 对每个域名，判断状态
  │     │
  │     ├─→ 域名 id=10 (node_id=44, endpoint_id=''):
  │     │     │
  │     │     ├─→ 查询 NodeStatusCache[44]
  │     │     │
  │     │     ├─→ 缓存不存在 → status = offline
  │     │     │
  │     │     └─→ 缓存存在:
  │     │           LastHeartbeat = 2026-02-18 22:10:00
  │     │           now() = 2026-02-18 22:11:30
  │     │           差值 = 90 秒 > 60 秒
  │     │           │
  │     │           └─→ 判断为疑似离线，查询 Headscale 验证
  │     │                 （见业务 2.3.8）
  │     │
  │     └─→ 域名 id=11 (node_id=44, endpoint_id='beagle-241'):
  │           │
  │           ├─→ 查询 EndpointStatusCache["beagle-241"]
  │           │
  │           ├─→ 缓存不存在 → status = offline
  │           │
  │           └─→ 缓存存在:
  │                 LastHeartbeat = 2026-02-18 22:10:00
  │                 now() = 2026-02-18 22:11:30
  │                 差值 = 90 秒 > 60 秒
  │                 │
  │                 └─→ 判断为 offline（Endpoint 无法查询 Headscale）
  │
  └─→ 返回域名列表
```

数据变化：

- 无变化（只读操作）

状态判断：

- Node：心跳超时 60 秒 → 查询 Headscale 验证（见业务 2.3.8）
- Endpoint：心跳超时 60 秒 → 直接判断为 offline

#### 业务 2.3.8：Headscale IP 查询验证（主动检测 - 仅 Node）

触发条件：Node 心跳超时，需要主动验证在线状态

```
查询域名状态时，发现 Node 心跳超时
  │
  ├─→ NodeStatusCache[44] 存在
  │     LastHeartbeat = 2026-02-18 22:10:00
  │     now() = 2026-02-18 22:11:30
  │     差值 = 90 秒 > 60 秒
  │
  ├─→ 查询 Headscale
  │     Headscale.GetNodeByIP("100.64.0.23")
  │
  ├─→ 场景 A：Headscale 返回节点在线
  │     {
  │       id: 123,
  │       name: "beagle-242",
  │       online: true,
  │       last_seen: "2026-02-18T22:11:25Z"
  │     }
  │     │
  │     ├─→ 判断为在线
  │     │
  │     ├─→ 更新内存缓存
  │     │     NodeStatusCache[44].LastHeartbeat = now()
  │     │
  │     └─→ 返回 status = online
  │
  └─→ 场景 B：Headscale 返回节点离线
        {
          id: 123,
          name: "beagle-242",
          online: false,
          last_seen: "2026-02-18T22:09:00Z"
        }
        │
        ├─→ 判断为离线
        │
        ├─→ 清理内存缓存
        │     DELETE NodeStatusCache[44]
        │
        └─→ 返回 status = offline
```

数据变化：

- NodeStatusCache：可能更新 LastHeartbeat 或删除缓存
- domain_registry 表：无变化

状态判断：

- Headscale 返回在线 → status = online，更新缓存
- Headscale 返回离线 → status = offline，清理缓存

说明：

- Endpoint 不在 Tailscale 网络中，无法通过 Headscale 查询
- Endpoint 只能依赖心跳超时判断（60 秒无心跳 = 离线）

#### 业务 2.3.9：Node 设备重新上线

触发条件：断连的 Agent 重新启动并连接

```
Agent 重新启动
  │
  ├─→ 连接 Server gRPC
  │
  ├─→ 发送首次心跳
  │     ├─ hostname: "beagle-242"
  │     ├─ tunnel_ip: "100.64.0.23"
  │     └─ connected_endpoints: ["beagle-241"]
  │
  ├─→ Server 处理心跳 (handleHeartbeat)
  │     │
  │     ├─→ 更新 Node 记录
  │     │     UPDATE node SET
  │     │       hostname=?, ip=?, last_heartbeat=now()
  │     │     WHERE id=44
  │     │
  │     ├─→ 更新内存缓存
  │     │     NodeStatusCache[44] = {
  │     │       NodeID: 44,
  │     │       UserID: 7,
  │     │       TunnelIP: "100.64.0.23",
  │     │       LastHeartbeat: now()
  │     │     }
  │     │
  │     └─→ 更新 Endpoint 缓存
  │           EndpointStatusCache["beagle-241"] = {
  │             EndpointName: "beagle-241",
  │             UserID: 7,
  │             LastHeartbeat: now()
  │           }
  │
  └─→ 域名状态变为 online
```

数据变化：

- NodeStatusCache：新增缓存
- EndpointStatusCache：新增缓存（如果有 connected_endpoints）
- domain_registry 表：无变化

状态判断：

- NodeStatusCache[44] 存在且心跳正常 → Node 域名 status = online
- EndpointStatusCache["beagle-241"] 存在且心跳正常 → Endpoint 域名 status = online

## 3. 核心数据

### 3.1 domain_registry 表（持久化）

| 字段         | 类型   | 说明                                         |
| ------------ | ------ | -------------------------------------------- |
| id           | int64  | 主键                                         |
| domain       | string | 完整域名（如 beagle-242.beijing.beagle）     |
| type         | string | 类型：ssh / k8sapi / k8ssvc                  |
| user_id      | uint64 | 所属 Agent User ID                           |
| node_id      | uint64 | 关联的 Node ID（Agent 本机能力时填充）       |
| endpoint_id  | string | 关联的 Endpoint Name（Endpoint 能力时填充）  |
| target_ip    | string | 目标 IP（Node 的 Tailscale IP 或 ClusterIP） |
| target_port  | int    | 目标端口                                     |
| namespace    | string | K8S 命名空间（k8ssvc 类型时）                |
| service_name | string | K8S Service 名称（k8ssvc 类型时）            |
| created_at   | time   | 创建时间                                     |
| updated_at   | time   | 更新时间                                     |

注意：表中不再有 status 字段，状态完全由内存缓存管理。

### 3.2 NodeStatusCache（内存缓存 - 设备维度）

```
结构：map[uint64]NodeStatus  // key = node_id

NodeStatus {
    NodeID         uint64    // Node ID
    UserID         uint64    // User ID
    TunnelIP       string    // Tailscale IP
    LastHeartbeat  time.Time // 最后心跳时间
}

判断逻辑：
1. 缓存不存在 → offline（已断连）
2. 缓存存在 + LastHeartbeat < (now - 60s) → 查询 Headscale
   - Headscale.GetNodeByIP(TunnelIP) 返回在线 → online
   - Headscale 返回离线 → offline
3. 缓存存在 + LastHeartbeat >= (now - 60s) → online
```

### 3.3 EndpointStatusCache（内存缓存 - Endpoint 维度）

```
结构：map[string]EndpointStatus  // key = endpoint_name

EndpointStatus {
    EndpointName   string    // Endpoint 名称
    UserID         uint64    // 所属 Agent User ID
    LastHeartbeat  time.Time // 最后心跳时间（Agent 转发）
}

判断逻辑：
1. 缓存不存在 → offline（已断连）
2. 缓存存在 + LastHeartbeat < (now - 60s) → offline（超时）
3. 缓存存在 + LastHeartbeat >= (now - 60s) → online
```

说明：

- Endpoint 不在 Tailscale 网络中，无法通过 Headscale 查询，只能依赖心跳超时判断
- Agent 通过心跳上报 connected_endpoints，Server 更新 EndpointStatusCache

### 3.4 数据关系

```
domain_registry（持久化）
  │
  ├─→ user_id → user.id
  │     └─→ 获取 Agent User 名称（如 "beijing"）
  │
  ├─→ node_id → NodeStatusCache[node_id]
  │     ├─→ 获取 Node 名称（从 node 表）
  │     └─→ 通过 LastHeartbeat 判断在线状态
  │           └─→ 必要时查询 Headscale.GetNodeByIP(TunnelIP)
  │
  └─→ endpoint_id → EndpointStatusCache[endpoint_name]
        ├─→ 获取 Endpoint 名称
        └─→ 通过 LastHeartbeat 判断在线状态
```

### 3.5 状态判断策略

| 场景                              | 判断方式                              | 结果    |
| --------------------------------- | ------------------------------------- | ------- |
| node_id > 0 且缓存不存在          | 直接判断                              | offline |
| node_id > 0 且心跳 < 60 秒        | 直接判断                              | online  |
| node_id > 0 且心跳 >= 60 秒       | 查询 Headscale.GetNodeByIP(tunnel_ip) | 动态    |
| endpoint_id != '' 且缓存不存在    | 直接判断                              | offline |
| endpoint_id != '' 且心跳 < 60 秒  | 直接判断                              | online  |
| endpoint_id != '' 且心跳 >= 60 秒 | 直接判断（无法查询 Headscale）        | offline |
| Headscale 查询失败                | 降级为 offline                        | offline |

### 3.6 关键约束

- (domain, user_id) 联合唯一索引（同一 Agent User 下域名不重复）
- node_id 和 endpoint_id 互斥（一条记录只能关联一个）
- 状态不存储在数据库中，完全由内存缓存管理
- 查询时从 node_id 或 endpoint_id 关联到内存缓存，读取设备状态
- Node 状态可以通过 Headscale IP 查询验证，Endpoint 只能依赖心跳超时
