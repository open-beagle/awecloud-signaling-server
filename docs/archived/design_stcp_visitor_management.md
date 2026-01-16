# STCP访问列表设计文档

## 概述

STCP访问列表允许Agent作为STCP Visitor（访问者）来访问其他Agent提供的STCP服务。

## 业务场景

- Agent A 提供STCP服务（server端）
- Agent B 需要访问Agent A的STCP服务（visitor端）
- 管理员在Web界面为Agent B配置STCP访问
- Agent B启动后自动创建STCP visitor代理

## 数据模型

### STCPVisitor 表

```sql
CREATE TABLE stcp_visitors (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    visitor_name TEXT NOT NULL,              -- visitor名称（唯一标识）
    agent_id INTEGER NOT NULL,               -- 所属Agent ID
    server_name TEXT NOT NULL,               -- 要访问的STCP服务名称
    secret_key TEXT NOT NULL,                -- STCP密钥（自动从目标STCP实例获取）
    bind_addr TEXT DEFAULT '0.0.0.0',        -- 本地绑定地址（允许局域网访问）
    bind_port INTEGER NOT NULL,              -- 本地绑定端口
    description TEXT,                        -- 描述
    enabled BOOLEAN DEFAULT false,           -- 是否启用
    created_at DATETIME,
    updated_at DATETIME,
    deleted_at DATETIME,
    
    UNIQUE(visitor_name, agent_id)           -- 同一Agent下visitor名称唯一
);

CREATE INDEX idx_stcp_visitors_agent_id ON stcp_visitors(agent_id);
CREATE INDEX idx_stcp_visitors_enabled ON stcp_visitors(enabled);
CREATE INDEX idx_stcp_visitors_deleted_at ON stcp_visitors(deleted_at);
```

## API设计

### 管理员API

#### 1. 获取STCP访问列表
```
GET /api/v1/admin/stcp-visitors?agent_id=xxx&enabled=true
```

#### 2. 创建STCP访问
```
POST /api/v1/admin/stcp-visitors
{
    "visitor_name": "访问socks5",
    "agent_id": 2,
    "server_name": "socks5",
    "bind_addr": "0.0.0.0",
    "bind_port": 1080,
    "description": "访问Agent1的socks5服务"
}
```

#### 3. 更新STCP访问
```
PUT /api/v1/admin/stcp-visitors/:id
{
    "description": "更新描述",
    "bind_addr": "0.0.0.0",
    "bind_port": 1081
}
```

#### 4. 删除STCP访问
```
DELETE /api/v1/admin/stcp-visitors/:id
```

#### 5. 启用STCP访问
```
PUT /api/v1/admin/stcp-visitors/:id/enable
```

#### 6. 禁用STCP访问
```
PUT /api/v1/admin/stcp-visitors/:id/disable
```

### gRPC API（Agent调用）

```protobuf
message GetSTCPVisitorsRequest {
    int64 agent_id = 1;
}

message GetSTCPVisitorsResponse {
    bool success = 1;
    repeated STCPVisitor visitors = 2;
}

message STCPVisitor {
    string visitor_name = 1;
    string server_name = 2;
    string secret_key = 3;
    string bind_addr = 4;
    int32 bind_port = 5;
}
```

## Agent同步逻辑

### 启动顺序

1. 注册Agent
2. 启动FRP Manager
3. 同步STCP实例（server端）
4. **同步STCP访问（visitor端）**
5. 同步TCP实例

### 同步流程

```go
func (a *Agent) syncEnabledSTCPVisitors() error {
    // 1. 调用gRPC获取已启用的STCP访问列表
    resp := grpcClient.GetEnabledSTCPVisitors(agentID)
    
    // 2. 遍历创建STCP visitor代理
    for _, visitor := range resp.Visitors {
        frpManager.AddSTCPVisitor(
            visitor.VisitorName,
            visitor.ServerName,
            visitor.SecretKey,
            visitor.BindAddr,
            visitor.BindPort,
        )
    }
}
```

## FRP配置

### STCP Server（提供服务的Agent）
```ini
[socks5]
type = stcp
local_ip = 10.3.161.251
local_port = 1080
sk = b2e98f9e9a7bde22ed5c10762b36eb0904bdae00a78aa6d8eaaffbaa722d4fa4
```

### STCP Visitor（访问服务的Agent）
```ini
[访问socks5]
type = stcp
role = visitor
server_name = socks5
sk = b2e98f9e9a7bde22ed5c10762b36eb0904bdae00a78aa6d8eaaffbaa722d4fa4
bind_addr = 127.0.0.1
bind_port = 1080
```

## Web界面

### 菜单结构
```
服务管理
├── STCP实例（已有）
├── STCP访问（新增）
└── TCP实例（已有）
```

### STCP访问页面

**列表页**：
- 列：ID、访问名称、所属Agent、目标服务、绑定地址、状态、操作
- 筛选：Agent、启用状态
- 操作：新建、启用、禁用、编辑、删除

**创建对话框**：
- 访问名称（必填）
- 所属Agent（下拉选择）
- 目标STCP服务名称（必填，系统自动从该实例获取密钥）
- 本地绑定地址（默认0.0.0.0，允许局域网访问）
- 本地绑定端口（必填）
- 描述（可选）

## 实现步骤

1. ✅ 创建数据模型（model/stcp_visitor.go）
2. ✅ 实现后端API（api/stcp_visitor.go）
3. ✅ 添加gRPC接口（proto/agent.proto）
4. ✅ 实现Agent同步逻辑（agent/agent.go）
5. ✅ 实现FRP Manager支持（agent/frp_manager.go）
6. ✅ 创建前端页面（web/src/views/STCPVisitor/）
7. ✅ 添加路由和菜单

## 注意事项

1. **密钥一致性**：visitor的secret_key必须与server端的sk一致
2. **端口冲突**：同一Agent上的visitor bind_port不能冲突
3. **离线配置**：允许为离线Agent配置visitor，上线后自动同步
4. **优先级**：STCP访问在STCP实例之后、TCP实例之前同步
