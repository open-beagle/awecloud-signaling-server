# AWECloud-Signaling 数据库设计

## 1. 数据库选型

使用 **SQLite** 作为数据库，原因：
- 轻量级，无需独立数据库服务
- 适合中小规模部署
- 支持完整的SQL功能
- 易于备份和迁移
- 支持并发读取，写入通过事务保证一致性

## 2. 数据表设计

### 2.1 管理员表 (admins)

管理员账户信息，用于登录Web管理界面。

```sql
CREATE TABLE admins (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_admins_username ON admins(username);
```

**字段说明**：
- `id`: 主键，自增
- `username`: 管理员用户名（唯一）
- `password_hash`: 密码哈希（使用bcrypt）
- `created_at`: 创建时间
- `updated_at`: 更新时间

**索引**：
- `idx_admins_username`: 用户名索引，加速登录查询

### 2.2 Agent表 (agents)

Agent信息，每个Agent代表一个内网环境。

```sql
CREATE TABLE agents (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    agent_name TEXT NOT NULL UNIQUE,
    agent_token TEXT NOT NULL UNIQUE,
    status TEXT DEFAULT 'offline',
    last_heartbeat DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_agents_name ON agents(agent_name);
CREATE INDEX idx_agents_token ON agents(agent_token);
CREATE INDEX idx_agents_status ON agents(status);
```

**字段说明**：
- `id`: 主键，自增
- `agent_name`: Agent名称（唯一），如 "dev-env-1"
- `agent_token`: 认证Token（唯一），用于Agent连接认证
- `status`: 状态，枚举值：'online', 'offline'
- `last_heartbeat`: 最后心跳时间
- `created_at`: 创建时间
- `updated_at`: 更新时间

**索引**：
- `idx_agents_name`: Agent名称索引
- `idx_agents_token`: Token索引，加速认证查询
- `idx_agents_status`: 状态索引，用于查询在线Agent

### 2.3 Client表 (clients)

Client（用户）信息，每个Client代表一个可以访问服务的用户。

```sql
CREATE TABLE clients (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    client_id TEXT NOT NULL UNIQUE,
    client_secret TEXT NOT NULL,
    enabled BOOLEAN DEFAULT 1,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_clients_client_id ON clients(client_id);
CREATE INDEX idx_clients_enabled ON clients(enabled);
```

**字段说明**：
- `id`: 主键，自增
- `client_id`: Client标识（唯一），如用户名或邮箱
- `client_secret`: 认证密钥
- `enabled`: 是否启用，0=禁用，1=启用
- `created_at`: 创建时间
- `updated_at`: 更新时间

**索引**：
- `idx_clients_client_id`: Client ID索引，加速认证查询
- `idx_clients_enabled`: 启用状态索引

### 2.4 STCP实例表 (stcp_instances)

STCP代理实例，每个实例代表Agent上的一个可访问服务。

```sql
CREATE TABLE stcp_instances (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    instance_name TEXT NOT NULL UNIQUE,
    agent_id INTEGER NOT NULL,
    secret_key TEXT NOT NULL,
    local_ip TEXT NOT NULL,
    local_port INTEGER NOT NULL,
    description TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE
);

CREATE INDEX idx_stcp_instances_name ON stcp_instances(instance_name);
CREATE INDEX idx_stcp_instances_agent_id ON stcp_instances(agent_id);
```

**字段说明**：
- `id`: 主键，自增
- `instance_name`: 实例名称（唯一），如 "dev-mysql"
- `agent_id`: 所属Agent ID（外键）
- `secret_key`: STCP密钥，用于建立加密隧道
- `local_ip`: 本地服务IP，如 "127.0.0.1"
- `local_port`: 本地服务端口，如 3306
- `description`: 描述信息，如 "开发环境MySQL数据库"
- `created_at`: 创建时间
- `updated_at`: 更新时间

**外键约束**：
- `agent_id` → `agents(id)`，级联删除（删除Agent时自动删除其所有STCP实例）

**索引**：
- `idx_stcp_instances_name`: 实例名称索引
- `idx_stcp_instances_agent_id`: Agent ID索引，用于查询某个Agent的所有实例

### 2.5 STCP访问控制表 (stcp_access)

控制哪些Client可以访问哪些STCP实例。

```sql
CREATE TABLE stcp_access (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    stcp_instance_id INTEGER NOT NULL,
    client_id INTEGER NOT NULL,
    granted_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (stcp_instance_id) REFERENCES stcp_instances(id) ON DELETE CASCADE,
    FOREIGN KEY (client_id) REFERENCES clients(id) ON DELETE CASCADE,
    UNIQUE(stcp_instance_id, client_id)
);

CREATE INDEX idx_stcp_access_instance ON stcp_access(stcp_instance_id);
CREATE INDEX idx_stcp_access_client ON stcp_access(client_id);
```

**字段说明**：
- `id`: 主键，自增
- `stcp_instance_id`: STCP实例ID（外键）
- `client_id`: Client ID（外键）
- `granted_at`: 授权时间

**外键约束**：
- `stcp_instance_id` → `stcp_instances(id)`，级联删除
- `client_id` → `clients(id)`，级联删除

**唯一约束**：
- `(stcp_instance_id, client_id)`: 防止重复授权

**索引**：
- `idx_stcp_access_instance`: STCP实例索引，用于查询某个实例的所有授权
- `idx_stcp_access_client`: Client索引，用于查询某个Client可访问的所有实例

### 2.6 Client会话表 (client_sessions)

Client认证后的会话信息，用于维持登录状态。

```sql
CREATE TABLE client_sessions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    client_id INTEGER NOT NULL,
    session_token TEXT NOT NULL UNIQUE,
    expires_at DATETIME NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (client_id) REFERENCES clients(id) ON DELETE CASCADE
);

CREATE INDEX idx_client_sessions_token ON client_sessions(session_token);
CREATE INDEX idx_client_sessions_client_id ON client_sessions(client_id);
CREATE INDEX idx_client_sessions_expires_at ON client_sessions(expires_at);
```

**字段说明**：
- `id`: 主键，自增
- `client_id`: Client ID（外键）
- `session_token`: 会话Token（唯一），用于后续请求认证
- `expires_at`: 过期时间
- `created_at`: 创建时间

**外键约束**：
- `client_id` → `clients(id)`，级联删除

**索引**：
- `idx_client_sessions_token`: 会话Token索引，加速认证查询
- `idx_client_sessions_client_id`: Client ID索引
- `idx_client_sessions_expires_at`: 过期时间索引，用于清理过期会话

## 3. 数据关系图

```
admins (管理员)
  ↓ (管理)
  
agents (Agent) ←──────┐
  ↓ (1:N)              │
  │                    │
stcp_instances (实例)  │
  ↓ (N:M)              │
  │                    │
stcp_access (访问控制) │
  ↓ (N:M)              │
  │                    │
clients (Client) ──────┘
  ↓ (1:N)
  │
client_sessions (会话)
```

## 4. 初始化数据

### 4.1 默认管理员

系统首次启动时，自动创建默认管理员账户：

```sql
INSERT INTO admins (username, password_hash) 
VALUES ('admin', '$2a$10$...');  -- 密码: admin123
```

**注意**：生产环境部署后应立即修改默认密码。

## 5. 数据维护

### 5.1 清理过期会话

定期清理过期的Client会话：

```sql
DELETE FROM client_sessions 
WHERE expires_at < datetime('now');
```

建议：每小时执行一次清理任务。

### 5.2 更新Agent状态

定期检查Agent心跳，更新离线状态：

```sql
UPDATE agents 
SET status = 'offline' 
WHERE last_heartbeat < datetime('now', '-60 seconds');
```

建议：每30秒执行一次检查。

## 6. 数据备份

### 6.1 备份策略

SQLite数据库文件备份：

```bash
# 热备份（使用SQLite备份API）
sqlite3 awecloud.db ".backup awecloud_backup.db"

# 或直接复制文件（需要先停止写入）
cp awecloud.db awecloud_backup_$(date +%Y%m%d_%H%M%S).db
```

建议：
- 每天自动备份一次
- 保留最近7天的备份
- 重要变更前手动备份

### 6.2 恢复策略

```bash
# 停止服务
systemctl stop awecloud-server

# 恢复数据库
cp awecloud_backup.db awecloud.db

# 启动服务
systemctl start awecloud-server
```

## 7. 性能优化

### 7.1 索引优化

所有外键字段都已创建索引，常用查询字段也已创建索引。

### 7.2 查询优化

- 使用预编译语句（Prepared Statements）
- 批量操作使用事务
- 避免SELECT *，只查询需要的字段

### 7.3 并发控制

SQLite默认配置：
- `journal_mode = WAL`：支持并发读写
- `synchronous = NORMAL`：平衡性能和安全性
- `cache_size = -64000`：64MB缓存

---

**文档版本**: 1.0  
**最后更新**: 2025-11-25
