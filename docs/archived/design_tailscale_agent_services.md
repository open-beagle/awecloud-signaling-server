# Agent 服务管理设计

> 本文档描述 Agent 端本地服务（Proxy）和远程服务（Visitor）的完整生命周期管理，包括创建、编辑、删除，以及 Agent 重连恢复机制。

## 1. 概述

### 1.1 服务类型

| 服务类型                | 说明                          | 数据流向                                   |
| ----------------------- | ----------------------------- | ------------------------------------------ |
| **本地服务（Proxy）**   | 将局域网服务暴露到 VPN 网络   | VPN 客户端 → Agent(VPN IP) → 局域网服务    |
| **远程服务（Visitor）** | 访问 VPN 网络中其他节点的服务 | 局域网客户端 → Agent(局域网 IP) → VPN 服务 |

### 1.2 核心挑战

Agent 运行在容器环境时面临的问题：

| 问题           | 场景                           | 影响                         |
| -------------- | ------------------------------ | ---------------------------- |
| VPN IP 固定    | Agent 重启后 VPN IP 不变       | 本地服务源地址固定，无需处理 |
| 局域网 IP 变化 | 容器重启后局域网 IP 可能变化   | 远程服务源地址需要动态更新   |
| 服务状态丢失   | 容器销毁后内存中的服务列表丢失 | 需要从 Server 重新同步       |
| 端口冲突       | 新 IP 上端口可能被占用         | 需要检测并处理冲突           |

---

## 2. 本地服务（Proxy）生命周期

### 2.1 数据模型

```txt
ProxyService（本地服务）
├── ID           uint      // 服务 ID
├── Name         string    // 服务名称（唯一）
├── Alias        string    // 服务别名
├── SourceAddr   string    // 源地址（VPN IP:端口，如 100.64.0.1:3306）
├── TargetAddr   string    // 目标地址（局域网地址，如 192.168.1.10:3306）
├── Enabled      bool      // 是否启用
├── Status       string    // 运行状态：running/stopped/error
├── Listener     net.Listener  // 监听器（运行时）
├── Connections  int       // 当前连接数
├── BytesIn      int64     // 入站流量
├── BytesOut     int64     // 出站流量
└── ErrorMsg     string    // 错误信息
```

### 2.2 创建流程

**前提条件**：本地服务依赖 VPN 网络，必须在 Tailscale 初始化完成并获取到 VPN IP 后才能启动。

```txt
Web 管理界面创建本地服务
    │
    ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│ 1. Server 端处理                                                             │
├─────────────────────────────────────────────────────────────────────────────┤
│   a. 验证参数                                                                │
│      - 名称唯一性检查                                                        │
│      - 源地址格式验证（必须是 VPN_IP:端口）                                   │
│      - 目标地址格式验证                                                      │
│      - 端口范围检查（1-65535）                                               │
│                                                                             │
│   b. 保存到数据库                                                            │
│      INSERT INTO proxy_services (agent_id, name, alias, source_addr,        │
│                                  target_addr, enabled)                      │
│                                                                             │
│   c. 发送命令给 Agent                                                        │
│      Command: START_PROXY                                                   │
│      Payload: { name, source_addr, target_addr }                            │
└─────────────────────────────────────────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│ 2. Agent 端处理                                                              │
├─────────────────────────────────────────────────────────────────────────────┤
│   a. 检查 VPN 状态（前提条件）                                               │
│      - 检查 TailscaleManager 是否已初始化                                    │
│      - 检查是否已获取 VPN IP                                                 │
│      - 如果 VPN 未就绪，返回错误：VPN_NOT_READY                              │
│        服务配置保存在内存，等待 VPN 就绪后自动启动                           │
│                                                                             │
│   b. 解析源地址                                                              │
│      - 提取端口号（如 3306）                                                 │
│      - 验证 IP 是否为本机 VPN IP                                             │
│                                                                             │
│   c. 在 VPN 网络上监听                                                       │
│      listener = tsManager.Listen("tcp", "{VPN_IP}:3306")                    │
│      // 必须指定 VPN IP，确保只在 VPN 网络上监听                             │
│                                                                             │
│   d. 启动代理协程                                                            │
│      go proxyLoop(listener, targetAddr)                                     │
│                                                                             │
│   e. 上报状态给 Server                                                       │
│      ReportProxyStatus: { name, status: "running" }                         │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 2.3 VPN 就绪后自动启动

```txt
VPN 初始化完成
    │
    ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│ Agent 检查待启动的本地服务                                                   │
├─────────────────────────────────────────────────────────────────────────────┤
│   a. 遍历等待队列中的服务                                                    │
│      for service := range pendingProxies {                                  │
│          proxyManager.Start(service)                                        │
│      }                                                                      │
│                                                                             │
│   b. 上报启动结果                                                            │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 2.4 编辑流程

```txt
Web 管理界面编辑本地服务
    │
    ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│ 1. Server 端处理                                                             │
├─────────────────────────────────────────────────────────────────────────────┤
│   a. 验证参数（同创建）                                                      │
│                                                                             │
│   b. 判断是否需要重启服务                                                    │
│      - 源地址变化 → 需要重启                                                 │
│      - 目标地址变化 → 需要重启                                               │
│      - 仅别名变化 → 不需要重启                                               │
│                                                                             │
│   c. 更新数据库                                                              │
│                                                                             │
│   d. 如需重启，发送命令给 Agent                                              │
│      Command: STOP_PROXY  → 等待确认                                        │
│      Command: START_PROXY → 使用新配置                                      │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 2.5 删除流程

```txt
Web 管理界面删除本地服务
    │
    ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│ 1. Server 端处理                                                             │
├─────────────────────────────────────────────────────────────────────────────┤
│   a. 发送停止命令给 Agent                                                    │
│      Command: STOP_PROXY { name }                                           │
│                                                                             │
│   b. 等待 Agent 确认停止                                                     │
│                                                                             │
│   c. 删除数据库记录                                                          │
│      DELETE FROM proxy_services WHERE id = ?                                │
│                                                                             │
│   d. 清理相关权限记录                                                        │
│      DELETE FROM service_permissions WHERE service_id = ?                   │
└─────────────────────────────────────────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│ 2. Agent 端处理                                                              │
├─────────────────────────────────────────────────────────────────────────────┤
│   a. 关闭监听器                                                              │
│      listener.Close()                                                       │
│                                                                             │
│   b. 关闭所有活跃连接                                                        │
│      for conn := range activeConns { conn.Close() }                         │
│                                                                             │
│   c. 从内存中移除服务记录                                                    │
│                                                                             │
│   d. 上报状态给 Server                                                       │
│      ReportProxyStatus: { name, status: "stopped" }                         │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 2.6 禁用/启用流程

```txt
禁用服务:
    Server: UPDATE proxy_services SET enabled = false
    Server: 发送 STOP_PROXY 命令
    Agent:  停止监听，保留配置

启用服务:
    Server: UPDATE proxy_services SET enabled = true
    Server: 发送 START_PROXY 命令
    Agent:  重新启动监听
```

---

## 3. 远程服务（Visitor）生命周期

### 3.1 数据模型

```txt
PortForward（远程服务）
├── ID              uint      // 记录 ID
├── AgentID         uint      // 所属 Agent
├── ServiceID       uint      // 关联的远程 ProxyService
├── SourceAddr      string    // 源地址（局域网 IP:端口，如 192.168.1.100:13306）
├── TargetAddr      string    // 目标地址（VPN 地址，如 100.64.0.1:3306）
├── Enabled         bool      // 是否启用
├── Status          string    // 运行状态：running/stopped/error
├── Listener        net.Listener  // 监听器（运行时）
├── Connections     int       // 当前连接数
├── BytesIn         int64     // 入站流量
├── BytesOut        int64     // 出站流量
└── ErrorMsg        string    // 错误信息

说明：
- 名称、别名从关联的 ProxyService 获取，不在本表维护
- SourceAddr 的 IP 部分可以是具体 IP 或 0.0.0.0（监听所有接口）
```

### 3.2 源地址设计

**源地址格式**：`IP:端口`

| IP 类型       | 示例                | 说明             | 推荐场景         |
| ------------- | ------------------- | ---------------- | ---------------- |
| 具体局域网 IP | 192.168.1.100:13306 | 只在该 IP 上监听 | 生产环境（推荐） |
| 0.0.0.0       | 0.0.0.0:13306       | 监听所有网络接口 | 测试环境         |

**默认行为**：

- 创建时默认使用 Agent 检测到的局域网 IP
- 不推荐使用 0.0.0.0，因为会暴露到所有网络接口

### 3.3 源地址 IP 变化处理

当配置了具体 IP（如 192.168.1.100:13306）时，需要处理以下场景：

#### 场景 1：DHCP 导致 IP 变化

```txt
┌─────────────────────────────────────────────────────────────────────────────┐
│ 场景：Agent 重启后，DHCP 分配了新 IP                                         │
│       配置: 192.168.1.100:13306 → 实际: 192.168.1.99                        │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│   Agent 启动时检测:                                                          │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │ 1. 获取当前局域网 IP 列表                                            │   │
│   │    current_ips = [192.168.1.99, 10.0.0.50]                          │   │
│   │                                                                     │   │
│   │ 2. 检查配置的 IP 是否存在                                            │   │
│   │    configured_ip = 192.168.1.100                                    │   │
│   │    exists = configured_ip in current_ips  // false                  │   │
│   │                                                                     │   │
│   │ 3. 查找同网段的 IP                                                   │   │
│   │    configured_subnet = 192.168.1.0/24                               │   │
│   │    same_subnet_ip = findIPInSubnet(current_ips, configured_subnet)  │   │
│   │    // 找到 192.168.1.99                                              │   │
│   │                                                                     │   │
│   │ 4. 自动适配新 IP                                                     │   │
│   │    new_source_addr = 192.168.1.99:13306                             │   │
│   │    启动服务，监听新地址                                              │   │
│   │                                                                     │   │
│   │ 5. 上报变更给 Server                                                 │   │
│   │    ReportVisitorStatus: {                                           │   │
│   │        service_id,                                                  │   │
│   │        status: "running",                                           │   │
│   │        configured_addr: "192.168.1.100:13306",                      │   │
│   │        actual_addr: "192.168.1.99:13306",                           │   │
│   │        ip_changed: true,                                            │   │
│   │        change_reason: "DHCP_IP_CHANGE"                              │   │
│   │    }                                                                │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
│                                                                             │
│   Server 处理:                                                               │
│   - 更新数据库中的 source_addr 为实际地址                                    │
│   - 记录审计日志                                                             │
│   - Web 界面显示实际地址（可标注"已自动适配"）                               │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

#### 场景 2：网卡丢失

```txt
┌─────────────────────────────────────────────────────────────────────────────┐
│ 场景：Agent 重启后，网卡丢失或被移除                                         │
│       配置: 192.168.1.100:13306 → 实际: 网卡不存在                          │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│   Agent 启动时检测:                                                          │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │ 1. 获取当前局域网 IP 列表                                            │   │
│   │    current_ips = [10.0.0.50]  // 只有另一个网段的 IP                 │   │
│   │                                                                     │   │
│   │ 2. 检查配置的 IP 是否存在                                            │   │
│   │    configured_ip = 192.168.1.100                                    │   │
│   │    exists = false                                                   │   │
│   │                                                                     │   │
│   │ 3. 查找同网段的 IP                                                   │   │
│   │    configured_subnet = 192.168.1.0/24                               │   │
│   │    same_subnet_ip = findIPInSubnet(current_ips, configured_subnet)  │   │
│   │    // 未找到同网段 IP                                                │   │
│   │                                                                     │   │
│   │ 4. 无法启动服务，上报错误                                            │   │
│   │    ReportVisitorStatus: {                                           │   │
│   │        service_id,                                                  │   │
│   │        status: "error",                                             │   │
│   │        configured_addr: "192.168.1.100:13306",                      │   │
│   │        actual_addr: "",                                             │   │
│   │        error_code: "NETWORK_INTERFACE_LOST",                        │   │
│   │        error_msg: "配置的网段 192.168.1.0/24 在本机未找到可用 IP，   │   │
│   │                    请检查网卡状态或更新配置"                         │   │
│   │    }                                                                │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
│                                                                             │
│   Server 处理:                                                               │
│   - 更新服务状态为 error                                                     │
│   - 记录错误信息                                                             │
│   - Web 界面显示错误状态和提示信息                                           │
│   - 可选：发送告警通知管理员                                                 │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 3.4 IP 适配算法

```txt
func resolveSourceAddr(configuredAddr string, currentIPs []string) (string, error) {
    configuredIP, port := parseAddr(configuredAddr)

    // 1. 如果是 0.0.0.0，直接使用
    if configuredIP == "0.0.0.0" {
        return configuredAddr, nil
    }

    // 2. 检查配置的 IP 是否存在
    if contains(currentIPs, configuredIP) {
        return configuredAddr, nil
    }

    // 3. 查找同网段的 IP
    configuredSubnet := getSubnet(configuredIP)  // 如 192.168.1.0/24
    for _, ip := range currentIPs {
        if isInSubnet(ip, configuredSubnet) {
            // 找到同网段 IP，自动适配
            return ip + ":" + port, nil
        }
    }

    // 4. 未找到可用 IP，返回错误
    return "", fmt.Errorf("NETWORK_INTERFACE_LOST: 网段 %s 未找到可用 IP", configuredSubnet)
}
```

### 3.5 创建流程

```txt
Web 管理界面创建远程服务
    │
    ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│ 1. Server 端处理                                                             │
├─────────────────────────────────────────────────────────────────────────────┤
│   a. 验证参数                                                                │
│      - 目标服务存在性检查（关联的 ProxyService）                             │
│      - 源地址格式验证                                                        │
│      - 端口范围检查                                                          │
│      - ACL 权限检查（Agent 是否有权访问目标服务）                            │
│                                                                             │
│   b. 保存到数据库                                                            │
│      INSERT INTO port_forwards (agent_id, service_id, source_addr,          │
│                                 target_addr, enabled)                       │
│                                                                             │
│   c. 发送命令给 Agent                                                        │
│      Command: START_VISITOR                                                 │
│      Payload: { service_id, source_addr, target_addr }                      │
└─────────────────────────────────────────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│ 2. Agent 端处理                                                              │
├─────────────────────────────────────────────────────────────────────────────┤
│   a. 解析源地址                                                              │
│      - 如果 IP 为空或 0.0.0.0，使用检测到的局域网 IP                         │
│      - 提取端口号                                                            │
│                                                                             │
│   b. 在局域网上监听                                                          │
│      listener = net.Listen("tcp", sourceAddr)                               │
│                                                                             │
│   c. 启动转发协程                                                            │
│      go visitorLoop(listener, targetAddr, tsManager)                        │
│      // 通过 tsManager.Dial() 连接 VPN 目标                                  │
│                                                                             │
│   d. 上报状态给 Server                                                       │
│      ReportVisitorStatus: { service_id, status: "running", actual_addr }    │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 3.6 编辑流程

```txt
Web 管理界面编辑远程服务
    │
    ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│ 1. Server 端处理                                                             │
├─────────────────────────────────────────────────────────────────────────────┤
│   a. 验证参数                                                                │
│                                                                             │
│   b. 判断是否需要重启                                                        │
│      - 源地址变化 → 需要重启                                                 │
│      - 目标服务变化 → 需要重启                                               │
│                                                                             │
│   c. 更新数据库                                                              │
│                                                                             │
│   d. 如需重启，发送命令给 Agent                                              │
│      Command: STOP_VISITOR  → 等待确认                                      │
│      Command: START_VISITOR → 使用新配置                                    │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 3.7 删除流程

```txt
Web 管理界面删除远程服务
    │
    ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│ 1. Server 端处理                                                             │
├─────────────────────────────────────────────────────────────────────────────┤
│   a. 发送停止命令给 Agent                                                    │
│      Command: STOP_VISITOR { service_id }                                   │
│                                                                             │
│   b. 等待 Agent 确认停止                                                     │
│                                                                             │
│   c. 删除数据库记录                                                          │
│      DELETE FROM port_forwards WHERE id = ?                                 │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 4. Agent 重连与恢复

### 4.1 重连场景

| 场景           | 触发条件           | VPN IP | 局域网 IP | 处理方式       |
| -------------- | ------------------ | ------ | --------- | -------------- |
| gRPC 断线重连  | 网络抖动           | 不变   | 不变      | 保持服务运行   |
| Tailscale 重连 | VPN 网络抖动       | 不变   | 不变      | 自动恢复       |
| Agent 进程重启 | 进程崩溃/手动重启  | 不变   | 可能变化  | 从 Server 同步 |
| 容器重启       | 容器销毁重建       | 不变   | 可能变化  | 从 Server 同步 |
| 容器迁移       | K8s 调度到其他节点 | 不变   | 变化      | 从 Server 同步 |

### 4.2 恢复流程

```txt
Agent 启动/重连
    │
    ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│ 1. 建立连接                                                                  │
├─────────────────────────────────────────────────────────────────────────────┤
│   a. 连接 Server (gRPC)                                                     │
│   b. 注册 Agent，获取 Tailscale AuthKey                                     │
│   c. 启动 TailscaleManager，恢复 VPN 连接                                   │
│   d. 获取 VPN IP（固定不变）                                                 │
└─────────────────────────────────────────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│ 2. 检测局域网 IP                                                             │
├─────────────────────────────────────────────────────────────────────────────┤
│   a. 自动检测当前局域网 IP                                                   │
│   b. 与上次记录的 IP 比较                                                    │
│   c. 如果变化，上报新 IP 给 Server                                           │
│      ReportNetworkChange: { old_ip, new_ip }                                │
└─────────────────────────────────────────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│ 3. 同步本地服务（Proxy）                                                     │
├─────────────────────────────────────────────────────────────────────────────┤
│   a. 从 Server 获取服务列表                                                  │
│      SyncProxies() → []ProxyService                                         │
│                                                                             │
│   b. 遍历启用的服务，逐个启动                                                │
│      for service := range enabledServices {                                 │
│          proxyManager.Start(service)                                        │
│      }                                                                      │
│                                                                             │
│   c. 上报启动结果                                                            │
│      - 成功：status = "running"                                             │
│      - 失败：status = "error", error_msg = "..."                            │
└─────────────────────────────────────────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│ 4. 同步远程服务（Visitor）                                                   │
├─────────────────────────────────────────────────────────────────────────────┤
│   a. 从 Server 获取服务列表                                                  │
│      SyncVisitors() → []PortForward                                         │
│                                                                             │
│   b. 处理局域网 IP 变化                                                      │
│      if currentIP != service.SourceIP {                                     │
│          // 使用新 IP 替换旧 IP                                              │
│          service.SourceAddr = currentIP + ":" + port                        │
│      }                                                                      │
│                                                                             │
│   c. 遍历启用的服务，逐个启动                                                │
│      for service := range enabledServices {                                 │
│          visitorManager.Start(service)                                      │
│      }                                                                      │
│                                                                             │
│   d. 上报启动结果（包含实际使用的源地址）                                    │
│      ReportVisitorStatus: { service_id, status, actual_source_addr }        │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 4.3 局域网 IP 变化处理

```txt
┌─────────────────────────────────────────────────────────────────────────────┐
│                        局域网 IP 变化处理策略                                │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  场景：Agent 容器重启后，局域网 IP 从 192.168.1.100 变为 192.168.1.101      │
│                                                                             │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │ 远程服务配置                                                         │   │
│  │                                                                     │   │
│  │ 数据库记录:                                                          │   │
│  │   source_addr: 192.168.1.100:13306  (旧 IP)                         │   │
│  │   target_addr: 100.64.0.1:3306                                      │   │
│  │                                                                     │   │
│  │ Agent 检测到 IP 变化后:                                              │   │
│  │   实际监听: 192.168.1.101:13306  (新 IP)                            │   │
│  │   上报给 Server: actual_source_addr = 192.168.1.101:13306           │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                                                             │
│  处理策略:                                                                  │
│                                                                             │
│  1. 自动适配（推荐）                                                        │
│     - Agent 自动使用新 IP 启动服务                                          │
│     - 上报实际地址给 Server                                                 │
│     - Server 更新数据库记录                                                 │
│     - Web 界面显示实际地址                                                  │
│                                                                             │
│  2. 使用 0.0.0.0（备选）                                                    │
│     - 配置 source_addr 为 0.0.0.0:13306                                    │
│     - 监听所有网络接口，IP 变化不影响                                       │
│     - 缺点：安全性较低                                                      │
│                                                                             │
│  3. 手动指定（特殊场景）                                                    │
│     - 配置文件指定固定 IP                                                   │
│     - 适用于 IP 不会变化的环境                                              │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 4.4 Server 端 IP 变化处理

```txt
Agent 上报 IP 变化
    │
    ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│ Server 处理 ReportNetworkChange                                              │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│   a. 更新 Agent 记录                                                         │
│      UPDATE agents SET lan_ip = ? WHERE id = ?                              │
│                                                                             │
│   b. 更新远程服务的源地址                                                    │
│      UPDATE port_forwards                                                   │
│      SET source_addr = REPLACE(source_addr, old_ip, new_ip)                 │
│      WHERE agent_id = ? AND source_addr LIKE old_ip + '%'                   │
│                                                                             │
│   c. 记录审计日志                                                            │
│      INSERT INTO audit_logs (action, detail)                                │
│      VALUES ('IP_CHANGE', 'Agent xxx IP changed: old → new')                │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 5. 错误处理

### 5.1 常见错误

| 错误类型     | 原因               | 处理方式                     |
| ------------ | ------------------ | ---------------------------- |
| 端口被占用   | 其他进程占用了端口 | 上报错误，等待管理员处理     |
| 目标不可达   | 局域网服务未启动   | 重试 3 次后上报错误          |
| VPN 连接断开 | Tailscale 网络问题 | 等待 VPN 恢复后自动重连      |
| 权限不足     | ACL 未授权         | 上报错误，提示管理员检查权限 |
| 连接超时     | 网络延迟过高       | 增加超时时间，记录日志       |

### 5.2 错误上报

```txt
ProxyError / VisitorError
├── ServiceID    uint      // 服务 ID
├── ServiceName  string    // 服务名称
├── ErrorCode    string    // 错误码
├── ErrorMsg     string    // 错误信息
├── Timestamp    time.Time // 发生时间
└── Retries      int       // 已重试次数

错误码:
- PORT_IN_USE      端口被占用
- TARGET_UNREACHABLE  目标不可达
- VPN_DISCONNECTED    VPN 断开
- ACL_DENIED          权限不足
- TIMEOUT             连接超时
- UNKNOWN             未知错误
```

---

## 6. gRPC 接口

### 6.1 服务同步接口

```txt
service AgentService {
    // 同步本地服务列表
    rpc SyncProxies(SyncProxiesRequest) returns (SyncProxiesResponse);

    // 同步远程服务列表
    rpc SyncVisitors(SyncVisitorsRequest) returns (SyncVisitorsResponse);

    // 上报本地服务状态
    rpc ReportProxyStatus(ProxyStatusReport) returns (StatusResponse);

    // 上报远程服务状态
    rpc ReportVisitorStatus(VisitorStatusReport) returns (StatusResponse);

    // 上报网络变化
    rpc ReportNetworkChange(NetworkChangeReport) returns (StatusResponse);
}
```

### 6.2 消息定义

```txt
SyncProxiesRequest:
├── agent_id    int64
└── agent_token string

SyncProxiesResponse:
└── proxies     []ProxyConfig
    ├── id          uint
    ├── name        string
    ├── alias       string
    ├── source_addr string
    ├── target_addr string
    └── enabled     bool

SyncVisitorsRequest:
├── agent_id    int64
└── agent_token string

SyncVisitorsResponse:
└── visitors    []VisitorConfig
    ├── id          uint
    ├── service_id  uint
    ├── service_name string   // 关联服务名称（agent/service）
    ├── service_alias string  // 关联服务别名
    ├── source_addr string
    ├── target_addr string
    └── enabled     bool

NetworkChangeReport:
├── agent_id    int64
├── agent_token string
├── old_lan_ip  string
└── new_lan_ip  string
```

---

## 7. 配置示例

### 7.1 Agent 配置

```toml
[visitor]
# 可选，手动指定局域网监听地址
# 留空则自动检测
listen_addr = ""

# 连接超时（秒）
connect_timeout = 10

# 重试次数
max_retries = 3

# 重试间隔（秒）
retry_interval = 5
```

---

**文档版本**: 1.0
**创建日期**: 2026-01-14
**关联文档**:

- [Agent 端变更设计](design_tailscale_agent.md)
- [Server Web 管理界面设计](design_tailscale_server_web.md)
