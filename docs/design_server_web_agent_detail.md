# Agent 详情页增强设计

## 需求概述

在 Agent 详情页增加两个功能模块：

1. **网络信息卡片** - 展示 Agent 所处的网络环境，为 Visitor 功能做准备
2. **端口访问服务列表** - 展示该 Agent 作为访问方，访问其他 Agent 服务的 Visitor 列表

---

## 一、网络信息卡片

### 1.1 设计目标

- 展示 Agent 的局域网位置（Visitor 监听地址）
- 帮助管理员了解 Agent 的部署环境
- 为 Visitor 功能提供必要信息

### 1.2 界面设计

```txt
┌─────────────────────────────────────────────────────────────────────────────────────────┐
│  网络信息                                                                               │
├─────────────────────────────────────────────────────────────────────────────────────────┤
│                                                                                         │
│  局域网 IP          网关              网卡              运行环境        主机名          │
│  192.168.1.100     192.168.1.1      eth0             🐳 Docker       agent-beijing    │
│                                                                                         │
│  💡 Visitor 将在 192.168.1.100 上监听端口，供局域网内其他设备访问                        │
│                                                                                         │
└─────────────────────────────────────────────────────────────────────────────────────────┘
```

### 1.3 数据字段

| 字段          | 类型   | 说明                               | 来源           |
| ------------- | ------ | ---------------------------------- | -------------- |
| lan_ip        | string | 局域网 IP                          | Agent 自动检测 |
| lan_gateway   | string | 网关地址                           | Agent 自动检测 |
| lan_interface | string | 网卡名称                           | Agent 自动检测 |
| runtime_env   | string | 运行环境: native/docker/kubernetes | Agent 检测     |
| hostname      | string | 主机名                             | Agent 上报     |

### 1.4 运行环境图标

| 环境       | 图标 | 说明           |
| ---------- | ---- | -------------- |
| native     | 🖥️   | 物理机/虚拟机  |
| docker     | 🐳   | Docker 容器    |
| kubernetes | ☸️   | Kubernetes Pod |

---

## 二、端口访问服务列表 (Visitor)

### 2.1 设计目标

- 展示该 Agent 作为"访问方"配置的 Visitor 列表
- 与现有的"端口映射服务"（服务暴露）形成对应关系
- 支持 Visitor 的启动/停止/删除操作

### 2.2 概念对比

| 功能                   | 方向     | 说明                                              |
| ---------------------- | -------- | ------------------------------------------------- |
| 端口映射服务 (Proxy)   | 服务暴露 | Agent 在 Tailscale IP 上监听，转发到内网服务      |
| 端口访问服务 (Visitor) | 服务访问 | Agent 在局域网 IP 上监听，转发到其他 Agent 的服务 |

### 2.3 界面设计

```txt
┌─────────────────────────────────────────────────────────────────────────────────────────┐
│  端口访问服务 (0)                                                    [+ 添加访问]       │
├─────────────────────────────────────────────────────────────────────────────────────────┤
│                                                                                         │
│  名称          本地监听地址              目标服务              状态    连接数    操作    │
│  mysql-prod   192.168.1.100:13306      beijing/mysql         🟢运行  3        [...]    │
│  redis-cache  192.168.1.100:16379      beijing/redis         🟢运行  1        [...]    │
│  gateway      192.168.1.100:8443       shanghai/gateway-443  🔴停止  0        [...]    │
│                                                                                         │
│  💡 局域网客户端通过上述地址访问远程 Agent 暴露的服务                                    │
│                                                                                         │
└─────────────────────────────────────────────────────────────────────────────────────────┘

操作菜单: [启动/停止] [删除]
```

### 2.4 数据字段

| 字段                | 类型   | 说明                         |
| ------------------- | ------ | ---------------------------- |
| id                  | int    | 主键                         |
| name                | string | Visitor 名称                 |
| agent_id            | int    | 所属 Agent ID（访问方）      |
| listen_port         | int    | 本地监听端口                 |
| target_service_id   | int    | 目标服务 ID                  |
| target_agent_name   | string | 目标 Agent 名称（展示用）    |
| target_service_name | string | 目标服务名称（展示用）       |
| target_addr         | string | 目标地址，如 100.64.0.1:3306 |
| status              | string | 状态: running/stopped/error  |
| connections         | int    | 当前连接数                   |
| created_at          | time   | 创建时间                     |

---

## 三、完整页面布局

```txt
┌─────────────────────────────────────────────────────────────────────────────────────────┐
│  首页 / 代理管理 / Agent详情                                                            │
├─────────────────────────────────────────────────────────────────────────────────────────┤
│                                                                                         │
│  ┌─ 基本信息 ───────────────────────────────────────────────────────────────────────┐   │
│  │  名称: beijing    分组: [公司内网]    版本: v1.2.0    创建时间: 2天前    ...      │   │
│  └───────────────────────────────────────────────────────────────────────────────────┘   │
│                                                                                         │
│  ┌─ 隧道信息 ───────────────────────────────────────────────────────────────────────┐   │
│  │  Tailscale IP: 100.64.0.1    状态: 🟢已连接    连接时间: 5分钟前                  │   │
│  └───────────────────────────────────────────────────────────────────────────────────┘   │
│                                                                                         │
│  ┌─ 网络信息 ───────────────────────────────────────────────────────────────────────┐   │
│  │  局域网 IP: 192.168.1.100    网关: 192.168.1.1    网卡: eth0    环境: 🐳 Docker  │   │
│  │  💡 Visitor 将在 192.168.1.100 上监听端口                                         │   │
│  └───────────────────────────────────────────────────────────────────────────────────┘   │
│                                                                                         │
│  ┌─ 端口映射服务 (5) ─────────────────────────────────────────────── [+ 创建服务] ──┐   │
│  │  名称          监听地址              目标地址              状态    连接数    操作 │   │
│  │  gateway-443   100.64.0.1:443       192.168.1.1:443       🟢运行  12       [...] │   │
│  │  mysql         100.64.0.1:3306      192.168.1.100:3306    🟢运行  3        [...] │   │
│  │  ...                                                                              │   │
│  └───────────────────────────────────────────────────────────────────────────────────┘   │
│                                                                                         │
│  ┌─ 端口访问服务 (2) ─────────────────────────────────────────────── [+ 添加访问] ──┐   │
│  │  名称          本地监听地址              目标服务              状态    连接数    操作│   │
│  │  hk-mysql     192.168.1.100:13306      cloud-hk/mysql        🟢运行  1        [...]│   │
│  │  hk-redis     192.168.1.100:16379      cloud-hk/redis        🔴停止  0        [...]│   │
│  │                                                                                   │   │
│  │  💡 局域网客户端通过上述地址访问远程 Agent 暴露的服务                              │   │
│  └───────────────────────────────────────────────────────────────────────────────────┘   │
│                                                                                         │
└─────────────────────────────────────────────────────────────────────────────────────────┘
```

---

## 四、数据模型变更

### 4.1 Agent 表扩展

新增网络信息字段：

```sql
ALTER TABLE agents ADD COLUMN lan_ip VARCHAR(45);
ALTER TABLE agents ADD COLUMN lan_gateway VARCHAR(45);
ALTER TABLE agents ADD COLUMN lan_interface VARCHAR(32);
ALTER TABLE agents ADD COLUMN runtime_env VARCHAR(16);  -- native/docker/kubernetes
ALTER TABLE agents ADD COLUMN hostname VARCHAR(128);
```

### 4.2 新建 Visitor 表

```sql
CREATE TABLE visitors (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name VARCHAR(64) NOT NULL,
    agent_id INTEGER NOT NULL,           -- 访问方 Agent
    listen_port INTEGER NOT NULL,        -- 本地监听端口
    target_service_id INTEGER NOT NULL,  -- 目标服务 ID
    target_addr VARCHAR(128) NOT NULL,   -- 目标地址 (冗余，便于查询)
    status VARCHAR(16) DEFAULT 'stopped', -- running/stopped/error
    connections INTEGER DEFAULT 0,
    bytes_in INTEGER DEFAULT 0,
    bytes_out INTEGER DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,

    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE,
    FOREIGN KEY (target_service_id) REFERENCES proxy_services(id) ON DELETE CASCADE,
    UNIQUE(agent_id, listen_port)
);
```

### 4.3 TypeScript 类型定义

```typescript
// 网络信息
export interface NetworkInfo {
  lan_ip: string;
  lan_gateway: string;
  lan_interface: string;
  runtime_env: "native" | "docker" | "kubernetes";
  hostname: string;
}

// Visitor 模型
export interface Visitor {
  id: number;
  name: string;
  agent_id: number;
  listen_port: number;
  target_service_id: number;
  target_addr: string;
  status: "running" | "stopped" | "error";
  connections: number;
  bytes_in: number;
  bytes_out: number;
  created_at: string;
  updated_at: string;
  // 关联数据（展示用）
  target_agent_name?: string;
  target_service_name?: string;
}

// 扩展 AgentDetail
export interface AgentDetail extends Agent {
  services?: ProxyService[];
  visitors?: Visitor[]; // 新增
  network_info?: NetworkInfo; // 新增
  ts_connected_at?: string;
}
```

---

## 五、API 设计

### 5.1 Agent 详情 API 扩展

`GET /api/v1/agents/:id` 响应增加 `network_info` 和 `visitors` 字段。

### 5.2 Visitor API

| 方法   | 路径                       | 说明              |
| ------ | -------------------------- | ----------------- |
| GET    | /api/v1/visitors           | 获取 Visitor 列表 |
| GET    | /api/v1/visitors/:id       | 获取 Visitor 详情 |
| POST   | /api/v1/visitors           | 创建 Visitor      |
| DELETE | /api/v1/visitors/:id       | 删除 Visitor      |
| POST   | /api/v1/visitors/:id/start | 启动 Visitor      |
| POST   | /api/v1/visitors/:id/stop  | 停止 Visitor      |

### 5.3 Agent 心跳扩展

心跳请求增加网络信息：

```protobuf
message HeartbeatRequest {
  // 现有字段...

  // 新增网络信息
  string lan_ip = 10;
  string lan_gateway = 11;
  string lan_interface = 12;
  string runtime_env = 13;  // native/docker/kubernetes
  string hostname = 14;
}
```

---

## 六、实现计划

见 `.tmp/todo.md`
