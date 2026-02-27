# 域名服务生命周期设计 V2

## 术语说明

本文档中使用以下术语：

- **Device（设备）**：概念层面，指运行 Agent 的设备，在 Tailscale 网络中注册为一个节点
- **node / node_id**：技术实现层面，数据库字段名、API 路径、代码变量名保持使用 `node`
- **Endpoint（终端）**：通过 Agent 转发访问的内网设备，不直接加入 Tailscale 网络

示例：

- 概念描述："管理员开启 Device 的 SSH 能力"
- 技术实现："UPDATE node SET ssh_enabled=true WHERE id=44"

## 1. 概述

域名服务是 ZTNA 体系中的核心概念，代表一个可访问的网络服务入口。

### 核心原则

- **域名由管理员创建**：管理员在 Web 界面开启设备能力时，自动创建域名
- **域名随能力消亡**：管理员关闭设备能力时，自动删除域名
- **状态存储在内存**：域名状态不依赖数据库字段，而是存储在 Server 内存缓存中
- **状态由设备触发**：域名状态由设备连接/断连、心跳等业务触发变更
- **状态实时判断**：通过心跳超时和 Headscale IP 查询两种方式判断在线状态

### 域名类型

域名类型根据来源设备（Device 或 Endpoint）分为两类，端口配置有所不同。

#### Device 本机能力

| 类型   | 说明                | target_port（数据库存储） | Desktop 访问端口     | 说明                                 |
| ------ | ------------------- | ------------------------- | -------------------- | ------------------------------------ |
| ssh    | SSH 访问            | 22                        | 22                   | Agent 物理 SSH 端口                  |
| k8sapi | Kubernetes API 访问 | 50050                     | 6443                 | Agent tsnet 虚拟端口，转发到 K8S API |
| k8ssvc | Kubernetes Service  | 50051                     | 动态（Service 端口） | Agent gRPC 服务，转发到 K8S Service  |

#### Endpoint 能力

| 类型   | 说明                | target_port（数据库存储） | Desktop 访问端口     | 说明                                             |
| ------ | ------------------- | ------------------------- | -------------------- | ------------------------------------------------ |
| ssh    | SSH 访问            | 50053+（动态分配）        | 22                   | Agent tsnet 虚拟端口 → Endpoint SSH              |
| k8sapi | Kubernetes API 访问 | 50153+（动态分配）        | 6443                 | Agent tsnet 虚拟端口 → Endpoint K8S API          |
| k8ssvc | Kubernetes Service  | 50051                     | 动态（Service 端口） | Agent gRPC 服务 → Endpoint K8S Service（待实现） |

说明：

- **target_port**：存储在 domain_registry 表中，Desktop 通过 Tailscale 连接到 Agent IP:target_port
- **Desktop 访问端口**：Desktop 本地代理监听的端口，用户实际连接的端口
- **Device SSH**：唯一直接访问 Agent 物理端口的能力，其他都通过 tsnet 虚拟端口或 gRPC 转发
- **Endpoint 能力**：所有请求先到达 Agent，再通过 Agent 的 Endpoint gRPC Server（50052）转发到 Endpoint
- **Endpoint SSH/K8SAPI 端口动态分配**：每个 Endpoint 分配一个独立端口，由 Agent 在 Endpoint 连接时分配，通过心跳上报给 Server
- **Endpoint K8SSVC 端口固定**：所有 Endpoint 共享 50051 端口，通过 gRPC 参数区分不同 Endpoint 和 Service

### 域名生成规则

域名格式由设备类型、能力类型和区域决定，遵循以下规则：

#### 规则 1：Device SSH 域名

```
格式：{device_name}.{region}.beagle
示例：beagle-242.beijing.beagle

说明：
- device_name：Device 的主机名（hostname）
- region：Agent User 的名称（如 beijing、neimeng）
- endpoint_id：为空（表示这是 Device 本机能力）
```

#### 规则 2：Device K8S API 域名

```
格式：kubernetes.{region}.beagle
示例：kubernetes.beijing.beagle

说明：
- 固定前缀：kubernetes
- region：Agent User 的名称
- endpoint_id：为空（表示这是 Device 本机能力）
- 注意：同一 region 只能有一个 Device K8S API 域名
```

#### 规则 3：Device K8S Service 域名

```
格式：{service_name}.{namespace}.{region}.beagle
示例：postgres.yygl.beijing.beagle

说明：
- service_name：K8S Service 名称
- namespace：K8S 命名空间
- region：Agent User 的名称
- endpoint_id：为空（表示这是 Device 本机能力）
- 注意：由 Agent 自动发现并上报，Server 自动创建
```

#### 规则 4：Endpoint SSH 域名

```
格式：{endpoint_name}.{region}.beagle
示例：beagle-002.neimeng.beagle

说明：
- endpoint_name：Endpoint 的名称（管理员创建时指定）
- region：Agent User 的名称（Endpoint 所属的 Agent User）
- endpoint_id：Endpoint 名称（如 beagle-002）
- node_id：Endpoint 连接到的 Agent Node ID（连接后填充）
```

#### 规则 5：Endpoint K8S API 域名

```
格式：kubernetes.{region}.beagle
示例：kubernetes.neimeng.beagle

说明：
- 固定前缀：kubernetes
- region：Agent User 的名称（Endpoint 所属的 Agent User）
- endpoint_id：Endpoint 名称（如 beagle-002）
- node_id：Endpoint 连接到的 Agent Node ID（连接后填充）
- 注意：格式与 Node K8S API 相同，通过 endpoint_id 字段区分
```

#### 规则 6：Endpoint K8S Service 域名

```
格式：{service_name}.{namespace}.{region}.beagle
示例：postgres.yygl.neimeng.beagle

说明：
- service_name：K8S Service 名称
- namespace：K8S 命名空间
- region：Agent User 的名称（Endpoint 所属的 Agent User）
- endpoint_id：Endpoint 名称（如 beagle-002）
- node_id：Endpoint 连接到的 Agent Node ID（连接后填充）
- 注意：格式与 Node K8S Service 相同，但通过 Agent 的 Endpoint gRPC Server 转发
- 注意：由 Endpoint 自动发现并上报，Server 自动创建（功能待实现）
```

#### 域名唯一性约束

```
约束：(domain, user_id) 联合唯一

含义：
- 同一 Agent User 下，域名不能重复
- 不同 Agent User 可以有相同的域名

示例：
✓ 允许：beijing 用户有 kubernetes.beijing.beagle（Node K8S API）
✓ 允许：neimeng 用户有 kubernetes.neimeng.beagle（Node K8S API）
✗ 禁止：beijing 用户同时有两个 kubernetes.beijing.beagle
✓ 允许：beijing 用户有 kubernetes.beijing.beagle（Node K8S API）
         + beagle-002 Endpoint 有 kubernetes.beijing.beagle（Endpoint K8S API）
         （通过 endpoint_id 字段区分，实际是不同的域名记录）
```

#### 域名与设备的关系

```
Device 本机能力域名：
- node_id：填充（关联到 Node）
- endpoint_id：为空
- 示例：beagle-242.beijing.beagle（SSH）
        kubernetes.beijing.beagle（K8S API）
        postgres.yygl.beijing.beagle（K8S Service）

Endpoint 能力域名：
- node_id：Endpoint 连接后填充（关联到 Agent Node）
- endpoint_id：填充（关联到 Endpoint）
- 示例：beagle-002.neimeng.beagle（SSH）
        kubernetes.neimeng.beagle（K8S API）

判断方法：
- endpoint_id 为空 → Device 本机能力
- endpoint_id 不为空 → Endpoint 能力
```

### 端口生成规则

域名的目标端口（target_port）由域名类型和来源设备决定。Desktop 通过魔法 DNS 将域名解析为 127.1.x.x 的 VIP 地址，然后通过本地代理转发到 Tailscale 网络中的实际端口。

#### Device 本机能力端口

| 域名类型         | 目标端口 | Desktop 访问端口 | 说明                                    |
| ---------------- | -------- | ---------------- | --------------------------------------- |
| Node SSH         | 22       | 22               | Node 本机 SSH 端口（固定）              |
| Node K8S API     | 50050    | 6443             | tsnet 虚拟端口（可配置，默认 50050）    |
| Node K8S Service | 50051    | 动态             | tsnet 虚拟端口（固定 50051，gRPC 服务） |

说明：

- Node SSH：Desktop 访问 127.1.x.x:22 → Tailscale → Agent IP:22（直接访问 Node 本机 SSH）
- Node K8S API：Desktop 访问 127.1.x.x:6443 → Tailscale → Agent IP:50050（Agent 的 tsnet 虚拟端口，转发到 K8S API Server）
- Node K8S Service：Desktop 访问 127.1.x.x:动态端口 → Tailscale → Agent IP:50051（Agent 的 gRPC 服务，转发到 K8S Service）

#### Endpoint 能力端口

| 域名类型             | 目标端口           | Desktop 访问端口 | 说明                                                |
| -------------------- | ------------------ | ---------------- | --------------------------------------------------- |
| Endpoint SSH         | 50053+（动态分配） | 22               | tsnet 虚拟端口，每个 Endpoint 独立端口              |
| Endpoint K8S API     | 50153+（动态分配） | 6443             | tsnet 虚拟端口，每个 Endpoint 独立端口              |
| Endpoint K8S Service | 50051              | 动态             | gRPC 服务，所有 Endpoint 共享，通过参数区分（固定） |

说明：

- Endpoint SSH：Desktop 访问 127.1.x.x:22 → Tailscale → Agent IP:50053+N（动态分配）→ Agent gRPC:50052 → Endpoint → 实际 SSH
- Endpoint K8S API：Desktop 访问 127.1.x.x:6443 → Tailscale → Agent IP:50153+N（动态分配）→ Agent gRPC:50052 → Endpoint → 实际 K8S API
- Endpoint K8S Service：Desktop 访问 127.1.x.x:动态端口 → Tailscale → Agent IP:50051（固定）→ Agent gRPC:50052 → Endpoint → 实际 Service
- **端口动态分配**：Endpoint 连接到 Agent 时，Agent 为其分配独立端口（SSH: 50053+, K8SAPI: 50153+）
- **端口上报**：Agent 通过心跳将分配的端口上报给 Server，Server 更新 domain_registry 表的 target_port 字段

#### 端口分配架构

```
Desktop 魔法 DNS 解析：
  *.beagle → 127.1.x.x（VIP 地址）
  Desktop 本地代理监听 127.1.x.x:各种端口
  转发到 Tailscale 网络中的 Agent IP:目标端口

Device 本机能力访问流程：
  Desktop → 127.1.x.x:22 → Tailscale → Agent IP:22（SSH）
  Desktop → 127.1.x.x:6443 → Tailscale → Agent IP:50050（K8S API，tsnet 虚拟端口）
  Desktop → 127.1.x.x:动态端口 → Tailscale → Agent IP:50051（K8S Service，gRPC）

Endpoint 能力访问流程：
  Desktop → 127.1.x.x:22 → Tailscale → Agent IP:50053+N（tsnet，动态分配）→ Agent gRPC:50052 → Endpoint SSH
  Desktop → 127.1.x.x:6443 → Tailscale → Agent IP:50153+N（tsnet，动态分配）→ Agent gRPC:50052 → Endpoint K8S API
  Desktop → 127.1.x.x:动态端口 → Tailscale → Agent IP:50051（gRPC，固定）→ Agent gRPC:50052 → Endpoint Service
```

#### 关键端口说明

| 端口   | 用途                                                                |
| ------ | ------------------------------------------------------------------- |
| 22     | Node 本机 SSH 端口（物理端口）                                      |
| 50050  | Node K8S API tsnet 虚拟端口（可配置，默认 50050）                   |
| 50051  | Node/Endpoint K8S Service gRPC 端口（tsnet 虚拟端口，固定）         |
| 50052  | Agent 的 Endpoint gRPC Server 端口（内网物理端口）                  |
| 50053+ | Endpoint SSH tsnet 虚拟端口（动态分配，每个 Endpoint 独立端口）     |
| 50153+ | Endpoint K8S API tsnet 虚拟端口（动态分配，每个 Endpoint 独立端口） |

注意：

- tsnet 虚拟端口：通过 Tailscale 的 FallbackTCPHandler 监听，不是物理端口，只在 Tailscale 网络中可达
- 物理端口：实际监听在网卡上的端口，如 22（SSH）、50052（Agent Endpoint gRPC Server）
- Desktop 访问端口：Desktop 本地代理监听的端口，用户实际连接的端口（如 SSH 用 22，K8S API 用 6443）
- **Endpoint 端口动态分配**：Endpoint SSH 和 K8SAPI 使用动态端口，由 Agent 在 Endpoint 连接时分配
- **端口分配范围**：Endpoint SSH (50053-50152)，Endpoint K8SAPI (50153-50252)，每个范围最多支持 100 个 Endpoint

#### Endpoint 端口静态化设计

为了解决 Endpoint 重连后端口变化导致的连接失败问题，Endpoint 的端口采用静态化设计：

端口存储位置：

- 端口记录在 endpoint 表中（ssh_port、k8sapi_port 字段）
- 不记录在 domain_registry 表中（domain_registry 的 target_port 从 endpoint 表读取）

端口分配时机：

- 创建 Endpoint 时，Server 根据已有 Endpoint 数量计算端口并写入数据库
- SSH 端口：50053 + count（count 为该 Agent 下已有 Endpoint 数量）
- K8SAPI 端口：50153 + count
- 端口一旦分配，除非删除 Endpoint，否则永不改变

端口下发流程：

```
Server 创建 Endpoint
  ↓
计算端口（ssh_port = 50053 + count, k8sapi_port = 50153 + count）
  ↓
写入 endpoint 表
  ↓
创建域名记录时，从 endpoint 表读取端口写入 domain_registry.target_port
  ↓
Agent 心跳时，Server 从 endpoint 表读取端口，通过 EndpointCapabilityConfig 下发
  ↓
Agent 收到配置后，调用 AllocateSpecificPort 按指定端口监听
  ↓
Endpoint 连接后，Agent 使用预分配的端口
```

端口持久化保证：

- Endpoint 重启：Agent 从 Server 获取相同端口配置，使用相同端口监听
- Agent 重启：Agent 从 Server 获取所有 Endpoint 端口配置，恢复监听
- Server 重启：端口存储在数据库中，重启后自动恢复

数据流向：

- Server 是唯一的端口状态源（存储在 endpoint 表）
- Agent 不记录端口状态，完全依赖 Server 下发
- Endpoint 不参与端口分配，由 Agent 按 Server 指定的端口监听

优势：

- 端口固定，Endpoint 重连不影响 Desktop 连接
- 数据模型清晰，端口是 Endpoint 的属性
- 查询简单，不需要 JOIN domain_registry 表
- 易于维护，一个 Endpoint 的所有信息都在 endpoint 表

### 数据存储

- **domain_registry 表**：持久化域名记录
- **NodeStatusCache**：内存缓存，存储 Node 状态
- **EndpointStatusCache**：内存缓存，存储 Endpoint 状态

注意：domain_registry 表中不存储 status 字段，状态完全由内存缓存管理。

## 2. 核心业务

### 2.1 域名数据创建

#### 业务 2.1.1：管理员开启 Device SSH 能力

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
  │     │       created_at
  │     │     ) VALUES (
  │     │       'beagle-242.beijing.beagle', 'ssh', 7, 44,
  │     │       '100.64.0.23', 22,
  │     │       now()
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

#### 业务 2.1.2：管理员开启 Device K8SAPI 能力

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
  │     │       created_at
  │     │     ) VALUES (
  │     │       'kubernetes.beijing.beagle', 'k8sapi', 7, 44,
  │     │       '100.64.0.23', 6443,
  │     │       now()
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

#### 业务 2.1.3：管理员开启 Device K8SSVC 能力（自动发现）

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
  │               ports: [5432, 9187]  // 数组，包含所有端口
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
  │           │     // 将端口数组序列化为 JSON
  │           │     service_ports_json = json.Marshal([5432, 9187])
  │           │     // 结果："[5432,9187]"
  │           │
  │           │     INSERT INTO domain_registry (
  │           │       domain, type, user_id, node_id,
  │           │       target_ip, target_port,
  │           │       namespace, service_name, service_ports,
  │           │       created_at
  │           │     ) VALUES (
  │           │       'postgres.yygl.beijing.beagle', 'k8ssvc', 7, 44,
  │           │       '100.64.0.23', 50051,
  │           │       'yygl', 'postgres', '[5432,9187]',
  │           │       now()
  │           │     )
  │           │
  │           └─→ 已存在 → 更新记录
  │                 // 更新端口列表（可能有变化）
  │                 service_ports_json = json.Marshal([5432, 9187])
  │
  │                 UPDATE domain_registry SET
  │                   target_ip=?, target_port=?, service_ports=?
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
  │     ├─→ 创建域名记录（暂不填充 node_id 和 target_port）
  │     │     INSERT INTO domain_registry (
  │     │       domain, type, user_id, node_id, endpoint_id,
  │     │       target_ip, target_port,
  │     │       created_at
  │     │     ) VALUES (
  │     │       'beagle-241.beijing.beagle', 'ssh', 7, 0, 'beagle-241',
  │     │       '', 0,
  │     │       now()
  │     │     )
  │     │     注意：node_id=0, target_port=0，等待 Endpoint 连接后由 Agent 分配端口并填充
  │     │
  │     └─→ 生成 Endpoint Token（用于 Endpoint 连接 Agent）
  │
  ├─→ 管理员在 Endpoint 机器上安装并启动 Endpoint
  │     使用 Token 连接到 Agent
  │
  ├─→ Endpoint 连接到 Agent
  │     │
  │     ├─→ Agent 为 Endpoint 分配端口
  │     │     SSH 端口: 50053（第一个 Endpoint）
  │     │     记录: EndpointConnection.SSHPort = 50053
  │     │
  │     └─→ Agent 下次心跳上报
  │           connected_endpoints: [
  │             {
  │               name: "beagle-241",
  │               ssh_port: 50053
  │             }
  │           ]
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
  │     └─→ 更新域名记录，填充 node_id 和 target_port
  │           UPDATE domain_registry SET
  │             node_id=44, target_ip='100.64.0.23', target_port=50053
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
  │     ├─→ 创建域名记录（暂不填充 node_id 和 target_port）
  │     │     INSERT INTO domain_registry (
  │     │       domain, type, user_id, node_id, endpoint_id,
  │     │       target_ip, target_port,
  │     │       created_at
  │     │     ) VALUES (
  │     │       'kubernetes-beagle-002.neimeng.beagle', 'k8sapi', 8, 0, 'beagle-002',
  │     │       '', 0,
  │     │       now()
  │     │     )
  │     │     注意：node_id=0, target_port=0，等待 Endpoint 连接后由 Agent 分配端口并填充
  │     │
  │     └─→ 生成 Endpoint Token（用于 Endpoint 连接 Agent）
  │
  ├─→ 管理员在 Endpoint 机器上安装并启动 Endpoint
  │     使用 Token 连接到 Agent
  │
  ├─→ Endpoint 连接到 Agent
  │     │
  │     ├─→ Agent 为 Endpoint 分配端口
  │     │     K8SAPI 端口: 50153（第一个 Endpoint）
  │     │     记录: EndpointConnection.K8SAPIPort = 50153
  │     │
  │     └─→ Agent 下次心跳上报
  │           connected_endpoints: [
  │             {
  │               name: "beagle-002",
  │               k8sapi_port: 50153
  │             }
  │           ]
  │
  ├─→ Server 处理心跳 (handleConnectedEndpoints)
  │     │
  │     ├─→ 更新内存缓存
  │     │     EndpointStatusCache["beagle-002"] = {
  │     │       EndpointName: "beagle-002",
  │     │       UserID: 8,
  │     │       LastHeartbeat: now()
  │     │     }
  │     │
  │     └─→ 更新域名记录，填充 node_id 和 target_port
  │           UPDATE domain_registry SET
  │             node_id=45, target_ip='100.64.0.22', target_port=50153
  │           WHERE endpoint_id='beagle-002' AND user_id=8
  │
  └─→ Endpoint K8SAPI 域名创建完成
```

数据变化：

- endpoint_k8sapi 表：新增记录
- domain_registry 表：新增记录，type='k8sapi', node_id=45（连接后填充）, endpoint_id='beagle-002', target_port=50153（动态分配）
- EndpointStatusCache：新增缓存（Endpoint 连接后）

状态判断：

- 通过 EndpointStatusCache["beagle-002"] 判断 Endpoint 在线状态

#### 业务 2.1.6：管理员创建 Endpoint K8SSVC 能力（自动发现）

触发条件：管理员在 Web 界面创建 Endpoint 并开启 K8SSVC 能力

```
管理员在 Web 界面操作
  │
  ├─→ POST /api/endpoints
  │     {
  │       name: "beagle-002",
  │       user_id: 8,
  │       k8sservice_enabled: true
  │     }
  │
  ├─→ Server 处理 (CreateEndpoint)
  │     │
  │     ├─→ 创建 Endpoint 记录
  │     │     INSERT INTO endpoint_k8sservice (
  │     │       name, user_id, k8sservice_enabled
  │     │     ) VALUES (
  │     │       'beagle-002', 8, true
  │     │     )
  │     │
  │     └─→ 生成 Endpoint Token（用于 Endpoint 连接 Agent）
  │
  ├─→ 管理员在 Endpoint 机器上安装并启动 Endpoint
  │     使用 Token 连接到 Agent
  │
  ├─→ Endpoint 连接到 Agent
  │     │
  │     ├─→ Endpoint 启动 K8S Service 自动发现
  │     │     定期扫描 K8S 集群中的 Service
  │     │
  │     └─→ Endpoint 通过 Agent gRPC 上报发现的 Service
  │           discovered_services: [
  │             {
  │               namespace: "yygl",
  │               service_name: "postgres",
  │               cluster_ip: "10.96.0.10",
  │               ports: [5432, 9187]  // 数组，包含所有端口
  │             }
  │           ]
  │
  ├─→ Agent 转发到 Server（通过心跳或专用 RPC）
  │
  ├─→ Server 处理 Endpoint 发现的 Service (handleEndpointDiscoveredServices)
  │     │
  │     └─→ 对每个 discovered_service:
  │           │
  │           ├─→ 生成域名
  │           │     domain = "{service_name}.{namespace}.{region}.beagle"
  │           │     例如：postgres.yygl.neimeng.beagle
  │           │
  │           ├─→ 查询是否已存在
  │           │     SELECT * FROM domain_registry
  │           │     WHERE domain=? AND user_id=? AND endpoint_id=?
  │           │
  │           ├─→ 不存在 → 创建新记录
  │           │     // 将端口数组序列化为 JSON
  │           │     service_ports_json = json.Marshal([5432, 9187])
  │           │     // 结果："[5432,9187]"
  │           │
  │           │     INSERT INTO domain_registry (
  │           │       domain, type, user_id, node_id, endpoint_id,
  │           │       target_ip, target_port,
  │           │       namespace, service_name, service_ports,
  │           │       created_at
  │           │     ) VALUES (
  │           │       'postgres.yygl.neimeng.beagle', 'k8ssvc', 8, 45, 'beagle-002',
  │           │       '100.64.0.22', 50055,
  │           │       'yygl', 'postgres', '[5432,9187]',
  │           │       now()
  │           │     )
  │           │
  │           └─→ 已存在 → 更新记录
  │                 // 更新端口列表（可能有变化）
  │                 service_ports_json = json.Marshal([5432, 9187])
  │
  │                 UPDATE domain_registry SET
  │                   target_ip=?, target_port=?, service_ports=?
  │                 WHERE domain=? AND user_id=? AND endpoint_id=?
  │
  └─→ Endpoint K8SSVC 域名创建完成
```

数据变化：

- endpoint_k8sservice 表：新增记录
- domain_registry 表：新增记录，type='k8ssvc', node_id=45（连接后填充）, endpoint_id='beagle-002', target_port=50055
- EndpointStatusCache：新增缓存（Endpoint 连接后）

状态判断：

- 通过 EndpointStatusCache["beagle-002"] 判断 Endpoint 在线状态

说明：

- Endpoint K8SSVC 域名是自动发现的，不是手动创建
- Endpoint 定期扫描 K8S 集群，通过 Agent 转发上报新发现的 Service
- Server 根据上报的 Service 自动创建域名记录
- target_port 固定为 50055（Agent 的 Endpoint K8S Service gRPC 端口）
- 功能待实现

### 2.2 域名数据删除

#### 业务 2.2.1：管理员关闭 Device SSH 能力

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

#### 业务 2.2.2：管理员关闭 Device K8SAPI 能力

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

#### 业务 2.2.3：管理员关闭 Device K8SSVC 能力

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

#### 业务 2.2.3a：管理员关闭 Endpoint SSH 能力

触发条件：管理员在 Web 界面关闭 Endpoint 的 SSH 能力

```
管理员在 Web 界面操作
  │
  ├─→ PATCH /api/endpoints/beagle-241
  │     {ssh_enabled: false}
  │
  ├─→ Server 处理 (UpdateEndpoint)
  │     │
  │     ├─→ 更新 Endpoint 配置
  │     │     UPDATE endpoint SET ssh_enabled=false WHERE name='beagle-241'
  │     │
  │     ├─→ 删除 SSH 域名记录
  │     │     DELETE FROM domain_registry
  │     │     WHERE endpoint_id='beagle-241' AND type='ssh'
  │     │
  │     └─→ 通知 Agent 配置变更（通过心跳响应或推送）
  │
  ├─→ Agent 收到配置更新
  │     │
  │     └─→ 停止 Endpoint SSH 代理服务
  │
  └─→ Endpoint SSH 域名删除完成
```

数据变化：

- domain_registry 表：删除 endpoint_id='beagle-241' 且 type='ssh' 的记录

#### 业务 2.2.3b：管理员关闭 Endpoint K8SAPI 能力

触发条件：管理员在 Web 界面关闭 Endpoint 的 K8SAPI 能力

```
管理员在 Web 界面操作
  │
  ├─→ PATCH /api/endpoints/beagle-002
  │     {k8sapi_enabled: false}
  │
  ├─→ Server 处理 (UpdateEndpoint)
  │     │
  │     ├─→ 更新 Endpoint 配置
  │     │     UPDATE endpoint_k8sapi SET k8sapi_enabled=false WHERE name='beagle-002'
  │     │
  │     ├─→ 删除 K8SAPI 域名记录
  │     │     DELETE FROM domain_registry
  │     │     WHERE endpoint_id='beagle-002' AND type='k8sapi'
  │     │
  │     └─→ 通知 Agent 配置变更
  │
  ├─→ Agent 收到配置更新
  │     │
  │     └─→ 停止 Endpoint K8SAPI 代理服务
  │
  └─→ Endpoint K8SAPI 域名删除完成
```

数据变化：

- domain_registry 表：删除 endpoint_id='beagle-002' 且 type='k8sapi' 的记录

#### 业务 2.2.3c：管理员关闭 Endpoint K8SSVC 能力

触发条件：管理员在 Web 界面关闭 Endpoint 的 K8SSVC 能力

```
管理员在 Web 界面操作
  │
  ├─→ PATCH /api/endpoints/beagle-002
  │     {k8sservice_enabled: false}
  │
  ├─→ Server 处理 (UpdateEndpoint)
  │     │
  │     ├─→ 更新 Endpoint 配置
  │     │     UPDATE endpoint_k8sservice SET k8sservice_enabled=false WHERE name='beagle-002'
  │     │
  │     ├─→ 删除所有 K8SSVC 域名记录
  │     │     DELETE FROM domain_registry
  │     │     WHERE endpoint_id='beagle-002' AND type='k8ssvc'
  │     │
  │     └─→ 通知 Agent 配置变更
  │
  ├─→ Agent 收到配置更新
  │     │
  │     └─→ 通知 Endpoint 停止 K8S Service 自动发现
  │
  └─→ 所有 Endpoint K8SSVC 域名删除完成
```

数据变化：

- domain_registry 表：删除 endpoint_id='beagle-002' 且 type='k8ssvc' 的所有记录

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

#### 业务 2.2.5：管理员删除 Device

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
- domain_registry 表：删除所有 node_id=44 的记录（包括 Device 本机能力和 Endpoint 域名）
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

#### 业务 2.3.1：Device上线（Agent 启动并连接）

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

#### 业务 2.3.2：Device持续在线（心跳正常）

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

#### 业务 2.3.3：Device断连（Agent 退出或网络断开）

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
  │             node_id=44, target_ip='100.64.0.23'
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

#### 业务 2.3.9：Device重新上线

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

| 字段          | 类型   | 说明                                                               |
| ------------- | ------ | ------------------------------------------------------------------ |
| id            | int64  | 主键                                                               |
| domain        | string | 完整域名（如 beagle-242.beijing.beagle）                           |
| type          | string | 类型：ssh / k8sapi / k8ssvc                                        |
| user_id       | uint64 | 所属 Agent User ID                                                 |
| node_id       | uint64 | 关联的 Node ID（Agent 本机能力时填充）                             |
| endpoint_id   | string | 关联的 Endpoint Name（Endpoint 能力时填充）                        |
| target_ip     | string | 目标 IP（Agent 的 Tailscale IP）                                   |
| target_port   | int    | 目标端口（Desktop 通过 Tailscale 连接的端口）                      |
| namespace     | string | K8S 命名空间（k8ssvc 类型时）                                      |
| service_name  | string | K8S Service 名称（k8ssvc 类型时）                                  |
| service_ports | string | K8S Service 端口列表（k8ssvc 类型时，JSON 数组，如 "[5432,9187]"） |
| ssh_users     | string | SSH 用户列表（ssh 类型时，JSON 数组，如 "[\"root\",\"deploy\"]"）  |
| created_at    | time   | 创建时间                                                           |

注意：

- 表中不再有 `status` 字段，状态完全由内存缓存管理
- 表中不再有 `updated_at` 字段，这是在线数据，不应持久化
- `target_ip` 和 `service_ports` 会随心跳动态更新，但不记录更新时间

#### 端口字段说明

不同域名类型的端口记录方式：

| 域名类型        | target_ip   | target_port    | service_ports | 说明                                                            |
| --------------- | ----------- | -------------- | ------------- | --------------------------------------------------------------- |
| Device SSH      | 100.64.0.23 | 22             | -             | Desktop → TS → Agent IP:22                                      |
| Device K8SAPI   | 100.64.0.23 | 50050          | -             | Desktop → TS → Agent IP:50050（tsnet 虚拟端口，固定）           |
| Device K8SSVC   | 100.64.0.23 | 50051          | "[5432,9187]" | Desktop → TS → Agent IP:50051（gRPC，固定），Service 多端口支持 |
| Endpoint SSH    | 100.64.0.23 | 50053+（动态） | -             | Desktop → TS → Agent IP:50053+N（tsnet 虚拟端口，动态分配）     |
| Endpoint K8SAPI | 100.64.0.23 | 50153+（动态） | -             | Desktop → TS → Agent IP:50153+N（tsnet 虚拟端口，动态分配）     |
| Endpoint K8SSVC | 100.64.0.23 | 50051          | "[5432,9187]" | Desktop → TS → Agent IP:50051（gRPC，固定），Service 多端口支持 |

说明：

- `target_ip`: 始终是 Agent 的 Tailscale IP
- `target_port`: Desktop 通过 Tailscale 连接到 Agent 的端口（物理端口或 tsnet 虚拟端口）
- `service_ports`: 仅 k8ssvc 类型使用，JSON 数组字符串，记录 K8S Service 的所有端口
- **Endpoint SSH/K8SAPI 端口动态分配**：每个 Endpoint 分配独立端口，由 Agent 在 Endpoint 连接时分配，通过心跳上报给 Server
- **Endpoint K8SSVC 端口固定**：所有 Endpoint 共享 50051 端口，通过 gRPC 参数区分不同 Endpoint 和 Service

#### k8ssvc 类型的完整访问流程

以 `postgres.yygl.beijing.beagle:5432` 为例（Service 有多个端口：5432 和 9187）：

```
Desktop 访问流程：
  用户连接：postgres.yygl.beijing.beagle:5432
    │
    ├─→ 魔法 DNS 解析：postgres.yygl.beijing.beagle → 127.1.x.x（VIP）
    │
    ├─→ 查询域名注册表：
    │     domain = "postgres.yygl.beijing.beagle"
    │     target_ip = "100.64.0.23"
    │     target_port = 50051（Agent gRPC 端口）
    │     service_ports = "[5432,9187]"（Service 所有端口）
    │
    ├─→ Desktop 本地代理为每个端口创建监听：
    │     127.1.x.x:5432 → Agent gRPC (port=5432)
    │     127.1.x.x:9187 → Agent gRPC (port=9187)
    │
    ├─→ 用户连接 127.1.x.x:5432
    │
    ├─→ Desktop 通过 Tailscale 连接：100.64.0.23:50051
    │     携带参数：namespace=yygl, service_name=postgres, port=5432
    │
    ├─→ Agent gRPC 服务接收请求：
    │     解析参数：namespace=yygl, service_name=postgres, port=5432
    │
    ├─→ Agent 转发到 K8S ClusterIP：10.3.168.100:5432
    │
    └─→ 最终到达 K8S Service 的 5432 端口 → Pod
```

多端口访问示例：

```
用户可以访问同一 Service 的不同端口：
- postgres.yygl.beijing.beagle:5432  → PostgreSQL 数据库
- postgres.yygl.beijing.beagle:9187  → Prometheus Metrics

Desktop 为两个端口都创建了本地代理监听，用户根据需要选择连接哪个端口。
```

数据库记录示例：

```
Device K8SSVC（单端口）:
  domain: "redis.cache.beijing.beagle"
  type: "k8ssvc"
  target_ip: "100.64.0.23"（Agent Tailscale IP）
  target_port: 50051（Agent gRPC 端口）
  namespace: "cache"
  service_name: "redis"
  service_ports: "[6379]"（单端口也用数组）

Device K8SSVC（多端口）:
  domain: "postgres.yygl.beijing.beagle"
  type: "k8ssvc"
  target_ip: "100.64.0.23"（Agent Tailscale IP）
  target_port: 50051（Agent gRPC 端口）
  namespace: "yygl"
  service_name: "postgres"
  service_ports: "[5432,9187]"（多端口数组）

Endpoint K8SSVC（多端口）:
  domain: "postgres.yygl.neimeng.beagle"
  type: "k8ssvc"
  target_ip: "100.64.0.22"（Agent Tailscale IP）
  target_port: 50055（Agent gRPC 端口）
  namespace: "yygl"
  service_name: "postgres"
  service_ports: "[5432,9187]"（多端口数组）
```

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
- Node 状态可以通过 Headscale IP 查询验证，Endpoint 只能依赖心跳超时判断

## 4. Web 界面显示规范

### 4.1 域名列表"所属用户"列显示规则

域名列表（https://signal.wodcloud.com/domains）中的"所属用户"列，根据域名类型显示不同的内容：

#### 规则 1：Device 本机能力域名

显示格式：`{user_name}`

示例：

- beagle-242.beijing.beagle → 显示：beijing
- kubernetes.beijing.beagle → 显示：beijing
- postgres.yygl.beijing.beagle → 显示：beijing

判断条件：

- endpoint_id 为空
- node_id > 0

说明：

- 显示 Agent User 的名称（如 beijing、neimeng）
- 这是 Device本机提供的能力

#### 规则 2：Endpoint 能力域名

显示格式：`{user_name} / {device_name}`

示例：

- beagle-241.beijing.beagle → 显示：beijing / beagle-242
- kubernetes.neimeng.beagle → 显示：neimeng / beagle-003

判断条件：

- endpoint_id 不为空
- node_id > 0（Endpoint 连接后填充）

说明：

- 第一部分：Agent User 的名称（Endpoint 所属的 Agent User）
- 第二部分：Agent 设备名称（Endpoint 连接到的 Agent 设备的 Hostname）
- 用斜杠分隔，表示 Endpoint 通过 Agent 提供服务

#### 规则 3：Endpoint 未连接状态

显示格式：`{user_name} / -`

示例：

- beagle-241.beijing.beagle → 显示：beijing / -

判断条件：

- endpoint_id 不为空
- node_id = 0（Endpoint 尚未连接到 Agent）

说明：

- 第一部分：Agent User 的名称
- 第二部分：显示 `-`，表示 Endpoint 尚未连接到任何 Agent

### 4.2 实现逻辑

后端 API 返回数据结构：

```
DomainListItem {
    domain: string           // 域名
    type: string            // 类型：ssh/k8sapi/k8ssvc
    user_id: uint64         // Agent User ID
    user_name: string       // Agent User 名称（如 beijing）
    node_id: uint64         // Node ID（0 表示未关联）
    device_name: string     // Agent 设备名称（Node.Hostname，如 beagle-242）
    endpoint_id: string     // Endpoint ID（非空表示 Endpoint 域名）
    endpoint_name: string   // Endpoint 名称（如 beagle-241）
    status: string          // 状态：online/offline
}
```

前端显示逻辑：

```
显示"所属用户"列：
1. 如果 endpoint_id 为空：
   显示：user_name

2. 如果 endpoint_id 不为空 且 device_name 不为空：
   显示：user_name / device_name

3. 如果 endpoint_id 不为空 且 device_name 为空：
   显示：user_name / -
```

### 4.3 显示示例对比

| 域名                                | endpoint_id | node_id | user_name | device_name | 显示结果             |
| ----------------------------------- | ----------- | ------- | --------- | ----------- | -------------------- |
| beagle-242.beijing.beagle           | (空)        | 44      | beijing   | beagle-242  | beijing              |
| kubernetes.beijing.beagle           | (空)        | 44      | beijing   | beagle-242  | beijing              |
| postgres.yygl.beijing.beagle        | (空)        | 44      | beijing   | beagle-242  | beijing              |
| beagle-241.beijing.beagle           | beagle-241  | 44      | beijing   | beagle-242  | beijing / beagle-242 |
| kubernetes.neimeng.beagle           | beagle-002  | 45      | neimeng   | beagle-003  | neimeng / beagle-003 |
| beagle-241.beijing.beagle（未连接） | beagle-241  | 0       | beijing   | (空)        | beijing / -          |

### 4.4 设计意图

这种显示方式清晰地表达了域名的所属关系：

1. **设备能力**：只显示 User 名称，表示这是该 Agent User 的设备本机提供的服务
2. **终端能力**：显示 `User / Device`，表示这是 Endpoint（终端）通过指定的 Agent 设备提供的服务
3. **未连接终端**：显示 `User / -`，提示管理员该 Endpoint 尚未连接到任何 Agent

这样的设计让管理员一眼就能看出：

- 服务属于哪个 Agent User（区域）
- 服务是由哪个设备提供的（设备本机 或 通过哪个 Agent 转发的终端）
- Endpoint 的连接状态（是否已连接到 Agent） 只能依赖心跳超时
