# TCP 服务管理功能设计

## 1. 功能概述

### 1.1 需求描述

在现有的 STCP 实例管理基础上，新增 TCP 服务管理功能，允许 Server 端在指定的 Agent 端动态创建、管理和删除 TCP 服务实例。

### 1.2 与 STCP 的区别

**STCP（Secret TCP）**：

- 点对点加密连接
- 需要 Desktop 端作为 Visitor 主动连接
- 适合用户按需访问的场景
- 连接流程：Desktop → Server → Agent

**TCP 服务**：

- Server 端直接暴露端口
- 无需 Desktop 端，任何客户端都可以访问
- 适合持续运行的服务暴露场景
- 连接流程：外部客户端 → Server 端口 → Agent

### 1.3 应用场景

- 将 Agent 内网的 HTTP 服务暴露到公网
- 将 Agent 内网的数据库服务暴露到公网
- 将 Agent 内网的 API 服务暴露到公网
- 临时暴露内网服务供外部访问

## 2. 系统架构

### 2.1 组件交互

```
管理员                Server-Web(API)      Server-Web(gRPC)     Agent(gRPC)      Agent-FRP        Server-FRP
  |                        |                      |                  |                |                |
  |--POST /api/tcp-------->|                      |                  |                |                |
  |  services              |                      |                  |                |                |
  |                        |                      |                  |                |                |
  |                        |--保存到数据库-------->|                  |                |                |
  |                        |  (service_name,      |                  |                |                |
  |                        |   remote_port, etc)  |                  |                |                |
  |                        |                      |                  |                |                |
  |                        |--检查Agent在线------->|                  |                |                |
  |                        |                      |                  |                |                |
  |                        |                      |--发送CREATE_TCP->|                |                |
  |                        |                      |  Command         |                |                |
  |                        |                      |                  |                |                |
  |                        |                      |                  |--进程内通信--->|                |
  |                        |                      |                  |                |                |
  |                        |                      |                  |                |--创建TCP代理--->|
  |                        |                      |                  |                |  (FRP配置)     |
  |                        |                      |                  |                |                |
  |                        |                      |                  |                |<--代理创建成功--|
  |                        |                      |                  |                |                |
  |                        |                      |                  |<--返回成功-----|                |
  |                        |                      |                  |                |                |
  |                        |                      |<--CommandResponse-|                |                |
  |                        |                      |                  |                |                |
  |<--返回成功------------- |                       |                  |                |                |
  |  {success: true,       |                      |                  |                |                |
  |   service: {...}}      |                      |                  |                |                |
```

### 2.2 数据流向

**创建 TCP 服务**：

```
管理员 → RESTful API → 数据库 → gRPC Command → Agent → FRP TCP Proxy → Server-FRP
```

**访问 TCP 服务**：

```
外部客户端 → Server端口 → Server-FRP → Agent-FRP → 内网服务
```

## 3. 数据库设计

**详细设计**: 参见 [数据库设计文档](./design_database.md)

### 3.1 TCP 服务相关表

- **tcp_services**: TCP 服务实例表

  - 存储服务名称、Agent ID、本地/远程端口、启用状态等
  - `remote_port` 由系统自动分配（从 9000 开始）
  - `enabled` 默认为 false（禁用）

- **tcp_service_access_logs**: TCP 服务访问日志表

  - 记录客户端 IP、操作类型、流量统计等

- **system_settings**: 系统设置扩展
  - `tcp_service_port_start`: 端口起始值（默认 9000）
  - `tcp_service_max_per_agent`: 每 Agent 最大服务数（默认 50）

**端口分配规则**：

- 起始端口：9000（可配置）
- 分配策略：顺序分配，使用 `MAX(remote_port) + 1`
- 端口回收：只有删除 TCP 服务时才释放
- 端口复用：禁用的服务仍占用端口

## 4. API 设计

**详细设计**: 参见 [API 设计文档](./design_api.md) 第 2.7 节

### 4.1 RESTful API 概览

- `GET /api/v1/admin/tcp-services` - 获取 TCP 服务列表
- `POST /api/v1/admin/tcp-services` - 创建 TCP 服务（自动分配端口）
- `PUT /api/v1/admin/tcp-services/:id` - 更新 TCP 服务
- `DELETE /api/v1/admin/tcp-services/:id` - 删除 TCP 服务（释放端口）
- `PUT /api/v1/admin/tcp-services/:id/enable` - 启用 TCP 服务
- `PUT /api/v1/admin/tcp-services/:id/disable` - 禁用 TCP 服务
- `PUT /api/v1/admin/tcp-services/:id/whitelist` - 设置 IP 白名单
- `GET /api/v1/admin/tcp-services/:id/logs` - 获取访问日志
- `GET /api/v1/admin/settings/tcp-service` - 获取配置
- `PUT /api/v1/admin/settings/tcp-service` - 更新配置

**关键特性**：

- 创建时不指定端口，系统自动分配
- 创建后默认禁用，需手动启用
- 禁用不释放端口，只有删除才释放

### 4.2 gRPC API 扩展

#### 4.2.1 扩展 Command 消息

在 `pkg/proto/agent.proto` 中扩展：

```protobuf
// Server指令
message Command {
  enum Type {
    CREATE_STCP = 0;
    DELETE_STCP = 1;
    CREATE_TCP = 2;   // 新增
    DELETE_TCP = 3;   // 新增
    UPDATE_TCP = 4;   // 新增
  }

  string command_id = 1;
  Type type = 2;

  // STCP相关字段
  string instance_name = 3;
  string secret_key = 4;

  // TCP相关字段
  string service_name = 5;
  int32 remote_port = 6;

  // 公共字段
  string local_ip = 7;
  int32 local_port = 8;
  bool enabled = 9;
}
```

## 5. FRP 配置

### 5.1 Agent 端 TCP 代理配置

```go
func (f *FRPManager) addTCPProxyInternal(serviceName string, localIP string, localPort int32, remotePort int32) error {
    // 创建TCP代理配置
    proxyConfig := &v1.TCPProxyConfig{
        ProxyBaseConfig: v1.ProxyBaseConfig{
            Name: serviceName,  // 唯一标识，如 "dev-api-http"
            Type: "tcp",
            ProxyBackend: v1.ProxyBackend{
                LocalIP:   localIP,    // 本地服务IP，如 "127.0.0.1"
                LocalPort: int(localPort),  // 本地服务端口，如 8080
            },
        },
        RemotePort: int(remotePort),  // Server端暴露的端口，如 18080
    }

    f.tcpProxies[serviceName] = proxyConfig

    // 重启FRP Client以应用新配置
    if f.service != nil {
        f.service.Close()
    }

    return nil
}
```

### 5.2 Server 端配置

Server 端无需特殊配置，FRP Server 会自动监听 Agent 请求的 remote_port。

**端口管理**：

- Server 端需要维护已使用的端口列表
- 创建 TCP 服务前检查端口是否已被占用
- 删除 TCP 服务后释放端口

## 6. 实现流程

### 6.1 创建 TCP 服务流程

```
1. 管理员通过Web界面创建TCP服务（不指定remote_port）
   ↓
2. Server-Web API验证请求
   - 检查Agent是否存在
   - 检查Agent的TCP服务数量是否超限
   ↓
3. 自动分配端口
   - 查询数据库中最大的remote_port
   - 如果没有记录，使用配置的起始端口（默认9000）
   - 否则使用 max(remote_port) + 1
   ↓
4. 保存到数据库 (tcp_services表，enabled=false)
   ↓
5. 返回成功响应（包含分配的端口号）
   ↓
6. 管理员手动启用TCP服务
   ↓
7. Server-Web API通过gRPC发送CREATE_TCP命令给Agent
   ↓
8. Agent收到命令，通知Agent-FRP线程
   ↓
9. Agent-FRP创建TCP代理配置
   ↓
10. Agent-FRP重启FRP Client应用新配置
   ↓
11. FRP Client连接到Server-FRP，请求监听remote_port
   ↓
12. Server-FRP开始监听remote_port
   ↓
13. TCP实例启用成功
```

**端口分配算法**：

```go
func allocatePort() (int, error) {
    // 1. 查询数据库中最大的remote_port
    var maxPort int
    err := db.QueryRow("SELECT MAX(remote_port) FROM tcp_services").Scan(&maxPort)

    // 2. 如果没有记录，使用起始端口
    if err == sql.ErrNoRows || maxPort == 0 {
        return getPortStart(), nil  // 默认9000
    }

    // 3. 返回下一个端口
    nextPort := maxPort + 1

    // 4. 检查端口是否超过最大值
    if nextPort > 65535 {
        return 0, errors.New("无可用端口")
    }

    return nextPort, nil
}
```

### 6.2 启用 TCP 服务流程

```
1. 管理员通过Web界面启用TCP服务
   ↓
2. Server-Web API验证请求
   - 检查TCP服务是否存在
   - 检查Agent是否在线
   ↓
3. 通过gRPC发送CREATE_TCP命令给Agent
   ↓
4. Agent收到命令，通知Agent-FRP线程
   ↓
5. Agent-FRP创建TCP代理配置
   ↓
6. Agent-FRP重启FRP Client应用新配置
   ↓
7. FRP Client连接到Server-FRP，请求监听remote_port
   ↓
8. Server-FRP开始监听remote_port
   ↓
9. 更新数据库 (enabled=true)
   ↓
10. 返回成功响应
```

### 6.3 禁用 TCP 服务流程

```
1. 管理员通过Web界面禁用TCP服务
   ↓
2. Server-Web API验证请求
   ↓
3. 通过gRPC发送DELETE_TCP命令给Agent
   ↓
4. Agent收到命令，通知Agent-FRP线程
   ↓
5. Agent-FRP删除TCP代理配置
   ↓
6. Agent-FRP重启FRP Client应用新配置
   ↓
7. Server-FRP停止监听remote_port
   ↓
8. 更新数据库 (enabled=false)
   ↓
9. 返回成功响应
```

**注意**：禁用 TCP 服务不会释放端口，端口仍然被该服务占用。

### 6.4 删除 TCP 服务流程

```
1. 管理员通过Web界面删除TCP服务
   ↓
2. Server-Web API查询服务信息
   ↓
3. 如果服务已启用，先禁用
   - 通过gRPC发送DELETE_TCP命令给Agent
   - Agent-FRP删除TCP代理配置
   - Server-FRP停止监听remote_port
   ↓
4. 从数据库删除记录
   ↓
5. 端口被释放（可被后续创建的服务使用）
   ↓
6. 返回成功响应
```

**注意**：只有删除 TCP 服务时，端口才会被释放并可能被复用。

### 6.5 访问 TCP 服务流程

```
外部客户端
   ↓ (连接 server.example.com:9000)
Server-FRP (监听9000端口)
   ↓ (通过FRP隧道转发)
Agent-FRP
   ↓ (连接本地服务)
内网服务 (127.0.0.1:8080)
```

## 7. Web 管理界面

### 7.1 菜单结构调整

**新的菜单结构**：

```
管理后台
├── 仪表盘
├── Agent管理
├── Client管理
├── 服务管理 (新增上级菜单)
│   ├── STCP实例 (原STCP实例管理，路径改为 /admin/services/stcp)
│   └── TCP实例 (新增，路径为 /admin/services/tcp)
├── 系统设置
└── 退出登录
```

### 7.2 TCP 服务列表页面

**路径**: `/admin/services/tcp`

**功能**：

- 显示所有 TCP 服务列表
- 按 Agent 筛选
- 按启用状态筛选（启用/禁用）
- 显示分配的远程端口
- 显示服务状态（在线/离线）
- 显示访问地址（如：`server.example.com:9000`）

**列表字段**：

- 服务名称
- Agent 名称
- 本地地址（IP:端口）
- 远程端口（自动分配）
- 启用状态（启用/禁用，默认禁用）
- 服务状态（在线/离线）
- 操作按钮

**操作**：

- 创建新 TCP 服务（自动分配端口，默认禁用）
- 启用/禁用 TCP 服务（切换按钮）
- 编辑 TCP 服务（不能修改端口）
- 删除 TCP 服务（释放端口）
- 查看访问日志

**状态说明**：

- 禁用状态：服务已创建，端口已分配，但未在 Server 端暴露
- 启用状态：服务正在运行，Server 端正在监听端口
- 在线/离线：Agent 的连接状态

### 7.3 创建 TCP 服务对话框

**字段**：

- 服务名称（必填，唯一）
- 选择 Agent（下拉列表）
- 本地 IP（默认 127.0.0.1）
- 本地端口（必填）
- 描述（可选）
- 访问控制（public/whitelist）
- IP 白名单（当选择 whitelist 时显示）

**说明**：

- 不需要填写远程端口，系统会自动分配
- 创建后默认为禁用状态
- 需要在列表页面手动启用

**验证**：

- 服务名称不能重复
- Agent 必须存在
- Agent 的 TCP 服务数量不能超限

### 7.4 TCP 服务详情页面

**路径**: `/admin/services/tcp/:id`

**显示信息**：

- 基本信息（服务名称、Agent、端口等）
- 分配的远程端口（自动分配，不可修改）
- 访问地址（可复制，如：`server.example.com:9000`）
- 启用状态（启用/禁用）
- 状态信息（在线/离线、连接数等）
- 访问日志（最近 100 条）
- 流量统计（发送/接收字节数）

**操作**：

- 编辑服务（仅描述、访问控制等，不能修改端口）
- 启用/禁用服务
- 设置 IP 白名单
- 导出访问日志
- 删除服务（会释放端口）

**端口说明**：

- 端口由系统自动分配，不可修改
- 禁用服务不会释放端口
- 只有删除服务才会释放端口

### 7.5 STCP 实例列表页面调整

**路径**: `/admin/services/stcp` (原 `/admin/stcp-instances`)

**说明**：将现有 STCP 实例管理页面移动到服务管理菜单下

## 8. 风险和注意事项

### 8.1 端口管理

**端口分配策略**：

- 自动分配：从 9000 开始顺序递增
- 不会冲突：每次分配使用 `MAX(remote_port) + 1`
- 不会回收：禁用的服务仍占用端口
- 只在删除时释放：删除服务后端口可被复用

**端口耗尽问题**：

- 理论上限：从 9000 到 65535，共 56536 个端口
- 实际限制：通过 `tcp_service_max_per_agent` 限制每个 Agent 的服务数量
- 解决方案：定期清理不使用的 TCP 服务

### 8.2 Agent 离线

**问题**：Agent 离线时 TCP 服务不可用

**解决方案**：

- 显示服务状态（在线/离线）
- Agent 重连后自动恢复 TCP 服务
- 提供离线告警

### 8.3 性能影响

**问题**：大量 TCP 服务可能影响 Server 性能

**解决方案**：

- 限制每个 Agent 的 TCP 服务数量
- 限制总 TCP 服务数量
- 监控资源使用情况

---

**文档版本**: 1.0  
**创建日期**: 2025-12-07  
**状态**: 设计阶段
