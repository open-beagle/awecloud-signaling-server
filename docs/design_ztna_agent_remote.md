# Agent 远程能力控制设计

相关文档：

- `design_ztna_agent.md` — Agent 能力对象设计（四种本机能力定义）
- `design_ztna_server_heartbeat.md` — 心跳业务设计（配置下发通道）
- `design_ztna_server_device.md` — 设备管理设计（Node 数据模型）
- `design_ztna_web_detail.md` — Web 详情页设计（设备详情页 UI）

## 概述

当前 Agent 的三项能力（SSH、K8S API、K8S Service）由本地配置控制：agent.toml 配置文件 + 环境变量。管理员无法从 Server Web 远程开关或调整 Agent 能力，必须登录 Agent 所在机器修改配置并重启。

本设计引入 Server 远程配置层，使管理员可以通过 Web 界面动态控制 Agent 能力的开关和参数，无需重启 Agent。

## 配置优先级

三层配置，高优先级覆盖低优先级：

```
Server 远程配置（最高优先级）
    ↓
环境变量
    ↓
agent.toml 配置文件（最低优先级）
```

规则：

- Server 远程配置存在且明确设置时，使用 Server 的值
- Server 远程配置未设置（null/未下发）时，回退到环境变量
- 环境变量未设置时，回退到 agent.toml 配置文件
- Agent 首次连接 Server 前，仅使用本地配置（环境变量 + 配置文件）

## 可控制的能力和参数

### SSH 能力

| 参数        | 说明         | 本地配置                     | Server 远程配置         |
| ----------- | ------------ | ---------------------------- | ----------------------- |
| ssh_enabled | 是否启用 SSH | tunnel.enable_ssh / 环境变量 | User.SSHEnabled（已有） |

SSH 的远程控制已通过 User.SSHEnabled 实现，不需要新增字段。

### K8S API 能力

| 参数        | 说明                | 本地配置                                 | Server 远程配置      |
| ----------- | ------------------- | ---------------------------------------- | -------------------- |
| enabled     | 是否启用 K8S API    | k8s.enabled / SIGNAL_K8S_ENABLED         | AgentCapability 新增 |
| listen_port | tsnet 监听端口      | k8s.listen_port / SIGNAL_K8S_LISTEN_PORT | AgentCapability 新增 |
| api_server  | K8S API Server 地址 | k8s.api_server / SIGNAL_K8S_API_SERVER   | AgentCapability 新增 |

kubeconfig 路径不通过远程配置控制（涉及本地文件系统路径，远程设置无意义）。

### K8S Service 能力

| 参数             | 说明              | 本地配置                                           | Server 远程配置      |
| ---------------- | ----------------- | -------------------------------------------------- | -------------------- |
| enabled          | 是否启用 SVC 发现 | svc.enabled / SIGNAL_SVC_ENABLED                   | AgentCapability 新增 |
| label_selector   | 标签选择器        | svc.label_selector / SIGNAL_SVC_LABEL_SELECTOR     | AgentCapability 新增 |
| namespaces       | 监听的命名空间    | svc.namespaces / SIGNAL_SVC_NAMESPACES             | AgentCapability 新增 |
| listen_port_base | gRPC 监听端口     | svc.listen_port_base / SIGNAL_SVC_LISTEN_PORT_BASE | AgentCapability 新增 |

kubeconfig 路径同样不通过远程配置控制。

## 数据模型

### 数据库存储

能力配置分两个层级存储：

- SSH 开关：存储在 User 表（User.SSHEnabled），User 级别共享，同一 Agent User 下所有 Node 共享 SSH 配置
- K8S/SVC 配置：存储在 Node 表，Node 级别独立，每个 Node 有独立的 K8S/SVC 配置

#### User 表（SSH 配置）

| 字段       | 类型 | 默认值 | 说明                              |
| ---------- | ---- | ------ | --------------------------------- |
| SSHEnabled | bool | false  | SSH 开关（bool 非指针，始终有值） |

#### Node 表（K8S/SVC 配置）

| 字段              | 类型   | 默认值 | 说明                                       |
| ----------------- | ------ | ------ | ------------------------------------------ |
| K8SEnabled        | \*bool | nil    | K8S API 能力开关（nil=未设置，由本地决定） |
| K8SListenPort     | \*int  | nil    | K8S API tsnet 监听端口                     |
| K8SApiServer      | string | ""     | K8S API Server 地址（空=未设置）           |
| SVCEnabled        | \*bool | nil    | K8S Service 能力开关                       |
| SVCLabelSelector  | string | ""     | K8S Service 标签选择器（空=未设置）        |
| SVCNamespaces     | string | ""     | K8S Service 命名空间列表 JSON（空=未设置） |
| SVCListenPortBase | \*int  | nil    | K8S Service gRPC 监听端口                  |

使用指针类型（*bool、*int）区分"未设置"和"设置为 false/0"：

- nil = Server 未配置，Agent 使用本地配置
- 非 nil = Server 已配置，Agent 使用 Server 的值

string 类型用空字符串表示未设置。

### 为什么 K8S/SVC 放在 Node 表

- 同一 Agent User 下可能有多个 Node，每个 Node 的 K8S 环境不同，需要独立配置
- SSH 是 User 级别的（一个 Agent User 的 SSH 开关统一），保留在 User 表
- 心跳时通过 Node ID 精确查询对应 Node 的配置，避免多 Node 共享导致配置混乱

## 心跳下发机制

### Proto 扩展

AgentHeartbeatResponse 新增 capability_config 字段：

```
AgentCapabilityConfig 消息字段：

  ssh_enabled          bool    SSH 开关
  ssh_enabled_set      bool    SSH 开关是否由 Server 设置

  k8s_enabled          bool    K8S API 开关
  k8s_enabled_set      bool    K8S API 开关是否由 Server 设置
  k8s_listen_port      int32   K8S API 监听端口
  k8s_listen_port_set  bool    K8S API 监听端口是否由 Server 设置
  k8s_api_server       string  K8S API Server 地址
  k8s_api_server_set   bool    K8S API Server 地址是否由 Server 设置

  svc_enabled          bool    K8S Service 开关
  svc_enabled_set      bool    K8S Service 开关是否由 Server 设置
  svc_label_selector   string  标签选择器
  svc_label_selector_set bool  标签选择器是否由 Server 设置
  svc_namespaces       string  命名空间列表（逗号分隔）
  svc_namespaces_set   bool    命名空间列表是否由 Server 设置
  svc_listen_port_base int32   gRPC 监听端口
  svc_listen_port_base_set bool gRPC 监听端口是否由 Server 设置
```

使用 `xxx_set` 标志位区分"Server 设置为 false/0"和"Server 未设置"。Agent 收到响应后：

- `xxx_set == true` → 使用 Server 下发的值
- `xxx_set == false` → 使用本地配置（环境变量 > 配置文件）

### 下发流程

```
心跳响应构建时：
    │
    ├─ 查询 User 表的 SSHEnabled（SSH 配置）
    │
    ├─ 查询 Node 表的 K8S/SVC 配置（通过心跳流中记录的 Node ID）
    │
    ├─ 构建 AgentCapabilityConfig
    │   ├─ User.SSHEnabled → ssh_enabled=true, ssh_enabled_set=true
    │   │   （SSHEnabled 是 bool 非指针，始终有值，始终下发）
    │   │
    │   ├─ Node.K8SEnabled != nil → k8s_enabled=*val, k8s_enabled_set=true
    │   │   Node.K8SEnabled == nil → k8s_enabled_set=false
    │   │
    │   └─ 其他 K8S/SVC 字段同理（从 Node 表读取）
    │
    └─ 设置到 AgentHeartbeatResponse.capability_config
```

### Agent 端配置合并

```
Agent 收到心跳响应中的 capability_config：
    │
    ├─ 对每个参数：
    │   ├─ xxx_set == true → 使用 Server 下发的值
    │   └─ xxx_set == false → 使用本地配置
    │
    ├─ 与当前运行配置比较
    │   ├─ 无变化 → 不操作
    │   └─ 有变化 → 触发能力重载
    │       ├─ 能力从关闭变为开启 → 启动对应模块
    │       ├─ 能力从开启变为关闭 → 停止对应模块
    │       └─ 参数变化 → 重启对应模块
    │
    └─ 记录日志
```

### 变更生效时间

- Server Web 修改能力配置 → 写入数据库
- 下一次心跳响应（最多 30 秒）→ Agent 收到新配置
- Agent 比较并应用变更 → 能力生效

最坏情况 30 秒内生效，无需重启 Agent。

## REST API 设计

### 获取 Agent 能力配置

GET /api/v1/nodes/:id/capabilities

响应：

| 字段                 | 类型   | 说明                        |
| -------------------- | ------ | --------------------------- |
| ssh_enabled          | bool   | SSH 开关（User.SSHEnabled） |
| k8s_enabled          | \*bool | K8S API 开关（nil=未设置）  |
| k8s_listen_port      | \*int  | K8S API 监听端口            |
| k8s_api_server       | string | K8S API Server 地址         |
| svc_enabled          | \*bool | K8S Service 开关            |
| svc_label_selector   | string | 标签选择器                  |
| svc_namespaces       | string | 命名空间列表 JSON           |
| svc_listen_port_base | \*int  | gRPC 监听端口               |

说明：此 API 通过 Node ID 查询。SSH 从 Node.User 读取（User 级别共享），K8S/SVC 直接从 Node 表读取（Node 级别独立）。

### 更新 Agent 能力配置

PUT /api/v1/nodes/:id/capabilities

请求体（只传需要修改的字段）：

| 字段                 | 类型     | 说明                |
| -------------------- | -------- | ------------------- |
| ssh_enabled          | \*bool   | SSH 开关            |
| k8s_enabled          | \*bool   | K8S API 开关        |
| k8s_listen_port      | \*int    | K8S API 监听端口    |
| k8s_api_server       | \*string | K8S API Server 地址 |
| svc_enabled          | \*bool   | K8S Service 开关    |
| svc_label_selector   | \*string | 标签选择器          |
| svc_namespaces       | \*string | 命名空间列表 JSON   |
| svc_listen_port_base | \*int    | gRPC 监听端口       |

传 null 表示清除 Server 配置（回退到本地配置）。传具体值表示设置 Server 配置。

更新后触发 configVersion 变更，Agent 下次心跳时获取新配置。

### 重置 Agent 能力配置

DELETE /api/v1/nodes/:id/capabilities

清除所有 Server 远程配置，Agent 完全回退到本地配置。

## 完整流程

### 管理员开启 Agent K8S API 能力

```
管理员在 Web 设备详情页
    │
    ├─ 打开 K8S API 开关
    │
    ├─ PUT /api/v1/nodes/:id/capabilities
    │   body: { "k8s_enabled": true }
    │
    ├─ Server 更新 Node.K8SEnabled = true
    │
    ├─ 下一次心跳响应
    │   capability_config.k8s_enabled = true
    │   capability_config.k8s_enabled_set = true
    │
    ├─ Agent 收到配置
    │   Server 设置 k8s_enabled=true → 覆盖本地配置
    │   启动 K8S API 代理模块
    │
    └─ K8S API 能力生效（≤30 秒）
```

### 管理员清除远程配置

```
管理员在 Web 设备详情页
    │
    ├─ 点击"重置为本地配置"
    │
    ├─ DELETE /api/v1/nodes/:id/capabilities
    │
    ├─ Server 清除 Node 的所有 K8S/SVC 能力配置字段（设为 nil/空）
    │
    ├─ 下一次心跳响应
    │   capability_config 所有 xxx_set = false
    │
    ├─ Agent 收到配置
    │   所有参数回退到本地配置（环境变量 > 配置文件）
    │
    └─ Agent 按本地配置运行
```

## 边界情况

### Agent 离线时修改配置

Server 写入数据库成功，但 Agent 不在线无法收到心跳响应。Agent 重新上线后的首次心跳响应会携带最新配置，自动生效。

### 多 Node 独立配置

同一 Agent User 下的多个 Node 各自拥有独立的 K8S/SVC 能力配置（配置在 Node 级别）。修改某个 Node 的配置不影响其他 Node。SSH 配置仍在 User 级别共享。

### Server 配置与本地配置冲突

Server 配置优先级最高，不存在冲突。如果管理员希望 Agent 使用本地配置，清除 Server 配置即可（设为 nil）。

### 首次连接

Agent 首次连接 Server 时，Node 的 K8S/SVC 能力配置字段全部为 nil/空，心跳响应中对应的 xxx_set = false，Agent 使用本地配置。SSH 始终下发（User.SSHEnabled 是 bool 非指针）。管理员可以在 Web 上按需设置。

## 实现优先级

| 阶段 | 内容                                                 | 依赖               |
| ---- | ---------------------------------------------------- | ------------------ |
| P1   | Node 表新增 K8S/SVC 能力配置字段                     | 无                 |
| P1   | Proto 新增 AgentCapabilityConfig 消息                | 无                 |
| P1   | 心跳响应构建能力配置（SSH 从 User，K8S/SVC 从 Node） | Node 字段 + Proto  |
| P1   | REST API（获取/更新/重置能力配置）                   | Node 字段          |
| P1   | Web 设备详情页能力配置卡片                           | REST API           |
| P2   | Agent 端配置合并和动态重载                           | Proto + Agent 改造 |
