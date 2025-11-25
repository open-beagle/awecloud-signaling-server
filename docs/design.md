# AWECloud-Signaling 设计方案

## 1. 项目概述

### 1.1 项目组成

```
awecloud-signaling/
├── awecloud-signaling-server/     # 信令服务器（包含agent和server两个bin）
│   ├── cmd/
│   │   ├── server/                # Server端程序
│   │   └── agent/                 # Agent端程序
│   └── web/                       # 管理网页
│
└── awecloud-signaling-desktop/    # 客户端应用（独立项目）
    └── 跨平台桌面应用
```

### 1.2 系统架构

```
┌─────────────────────────────────────────────────────────────┐
│                         互联网                               │
└─────────────────────────────────────────────────────────────┘
                              │
        ┌─────────────────────┼─────────────────────┐
        │                     │                     │
        ▼                     ▼                     ▼
  ┌──────────┐         ┌──────────┐         ┌──────────┐
  │ 管理员    │         │  Server  │         │  Client  │
  │ (浏览器)  │         │  (公网)   │         │  Desktop │
  └──────────┘         └──────────┘         └──────────┘
                              │
                    ┌─────────┴─────────┐
                    │                   │
              FRP控制连接          FRP控制连接
                    │                   │
                    ▼                   ▼
             ┌──────────┐         ┌──────────┐
             │  Agent   │◄────────┤  Client  │
             │(研发环境) │   STCP   │ (Visitor)│
             └────┬─────┘   隧道   └──────────┘
                  │
        ┌─────────┼─────────┐
        ▼         ▼         ▼
     [SSH]    [MySQL]   [Redis]
```

## 2. awecloud-signaling-server 设计

### 2.1 Server端（信令服务器）

#### 2.1.1 核心功能

**管理网页**
- 管理员登录（简单的用户名密码）
- Agent管理
- Client管理
- STCP连接实例管理

**基于FRP Server**
- 集成FRP Server核心代码
- 提供FRP控制端口（默认7000）
- 协调Agent和Client的STCP连接

#### 2.1.2 数据库设计

```sql
-- 管理员表
CREATE TABLE admins (
    id BIGSERIAL PRIMARY KEY,
    username VARCHAR(50) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Agent表
CREATE TABLE agents (
    id BIGSERIAL PRIMARY KEY,
    agent_name VARCHAR(100) UNIQUE NOT NULL,
    agent_token VARCHAR(255) NOT NULL,
    status VARCHAR(20) DEFAULT 'offline',
    last_heartbeat TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Client表
CREATE TABLE clients (
    id BIGSERIAL PRIMARY KEY,
    client_id VARCHAR(100) UNIQUE NOT NULL,
    client_secret VARCHAR(255) NOT NULL,
    status VARCHAR(20) DEFAULT 'active',  -- active, disabled
    is_online BOOLEAN DEFAULT false,
    last_seen TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- STCP连接实例表
CREATE TABLE stcp_instances (
    id BIGSERIAL PRIMARY KEY,
    agent_id BIGINT REFERENCES agents(id),
    instance_name VARCHAR(100) NOT NULL,
    service_type VARCHAR(20) NOT NULL,  -- tcp, udp
    local_ip VARCHAR(50) NOT NULL,
    local_port INT NOT NULL,
    secret_key VARCHAR(255) NOT NULL,
    server_name VARCHAR(200) UNIQUE NOT NULL,
    status VARCHAR(20) DEFAULT 'inactive',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(agent_id, instance_name)
);

-- Client访问权限表
CREATE TABLE client_permissions (
    id BIGSERIAL PRIMARY KEY,
    client_id BIGINT REFERENCES clients(id),
    stcp_instance_id BIGINT REFERENCES stcp_instances(id),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(client_id, stcp_instance_id)
);
```

#### 2.1.3 API设计

**管理员认证**
```
POST   /api/admin/login          # 管理员登录
POST   /api/admin/logout         # 管理员登出
```

**Agent管理**
```
GET    /api/agents               # 获取Agent列表
POST   /api/agents               # 新建Agent（生成agent_name和token）
DELETE /api/agents/:id           # 删除Agent
POST   /api/agents/:id/regenerate-token  # 重新生成token
```

**Client管理**
```
GET    /api/clients              # 获取Client列表
POST   /api/clients              # 新建Client（生成client_id和client_secret）
PUT    /api/clients/:id/disable  # 禁用Client
PUT    /api/clients/:id/enable   # 启用Client
DELETE /api/clients/:id          # 删除Client
```

**STCP实例管理**
```
GET    /api/stcp-instances                    # 获取实例列表
POST   /api/stcp-instances                    # 新建STCP实例
DELETE /api/stcp-instances/:id                # 删除STCP实例
POST   /api/stcp-instances/:id/grant-access   # 授权Client访问
DELETE /api/stcp-instances/:id/revoke-access  # 撤销Client访问
```

**Client端API（供Desktop应用调用）**
```
POST   /api/client/auth          # Client认证
GET    /api/client/services      # 获取可访问的服务列表
```

#### 2.1.4 配置文件

```toml
# server.toml

[server]
bind_addr = "0.0.0.0"
bind_port = 7000
# WSS支持配置
transport_protocol = "wss"  # 支持 tcp, websocket, wss
tls_cert_file = "./certs/server.crt"
tls_key_file = "./certs/server.key"

[database]
type = "sqlite"  # 简化部署，使用sqlite
path = "./data/awecloud.db"

[web]
listen_addr = "0.0.0.0"
listen_port = 8080
# 默认管理员账号
default_admin_username = "admin"
default_admin_password = "admin123"

[security]
jwt_secret = "change-this-in-production"
jwt_expire_hours = 24

[log]
level = "info"
file = "./logs/server.log"
```


### 2.2 Agent端

#### 2.2.1 核心功能

- 连接到Server（使用agent_name和agent_token）
- 等待Server发送创建STCP实例的请求
- 动态创建/删除STCP代理
- 定期发送心跳

#### 2.2.2 配置文件

```toml
# agent.toml

[agent]
agent_name = "dev-agent-001"
agent_token = "your-agent-token-from-server"

[server]
address = "server.example.com"
port = 7000
# WSS协议配置
protocol = "wss"  # 使用WSS协议连接Server
tls_enable = true

[log]
level = "info"
file = "./logs/agent.log"
```

#### 2.2.3 工作流程

```
1. Agent启动
   - 读取配置文件
   - 连接到Server（FRP控制连接）
   - 发送注册消息

2. 接收Server指令
   - 创建STCP实例
   - 删除STCP实例
   
3. 创建STCP实例
   Server -> Agent: {
       "action": "create_stcp",
       "instance_name": "mysql-dev",
       "secret_key": "xxx",
       "local_ip": "192.168.1.100",
       "local_port": 3306
   }
   
   Agent -> FRP: 创建STCP Proxy配置
   Agent -> Server: 返回成功/失败

4. 心跳
   每30秒发送一次心跳
```

#### 2.2.4 部署方式

**Docker部署（推荐）**
```bash
# 管理员在Server网页上创建Agent，获得agent_name和token

# 用户部署Agent容器
docker run -d \
  --name awecloud-signaling-agent \
  -e AGENT_NAME="dev-agent-001" \
  -e AGENT_TOKEN="your-token-here" \
  -e SERVER_ADDR="server.example.com:7000" \
  awecloud-signaling-agent:latest
```

## 3. awecloud-signaling-desktop 设计

### 3.1 技术栈

- **框架**: Wails (Go + Web前端)
- **前端**: Vue 3 + TypeScript
- **FRP集成**: 集成FRP Client作为Visitor

### 3.2 核心功能

#### 3.2.1 登录界面

```
┌─────────────────────────────────┐
│   Beagle Signaling Desktop      │
├─────────────────────────────────┤
│                                 │
│  Server地址: [____________]     │
│                                 │
│  Client ID: [____________]      │
│                                 │
│  Client Secret: [____________]  │
│                                 │
│         [  登录  ]              │
│                                 │
└─────────────────────────────────┘
```

#### 3.2.2 服务列表界面

```
┌─────────────────────────────────────────────────┐
│  Beagle Signaling Desktop         [用户: xxx]   │
├─────────────────────────────────────────────────┤
│                                                 │
│  可访问的服务:                                   │
│                                                 │
│  ┌───────────────────────────────────────────┐ │
│  │ ● mysql-dev                               │ │
│  │   类型: TCP                               │ │
│  │   本地端口: [3306]  [连接] [断开]         │ │
│  │   状态: 已连接                            │ │
│  └───────────────────────────────────────────┘ │
│                                                 │
│  ┌───────────────────────────────────────────┐ │
│  │ ○ redis-dev                               │ │
│  │   类型: TCP                               │ │
│  │   本地端口: [6379]  [连接] [断开]         │ │
│  │   状态: 未连接                            │ │
│  └───────────────────────────────────────────┘ │
│                                                 │
│  ┌───────────────────────────────────────────┐ │
│  │ ○ ssh-server                              │ │
│  │   类型: TCP                               │ │
│  │   本地端口: [2222]  [连接] [断开]         │ │
│  │   状态: 未连接                            │ │
│  └───────────────────────────────────────────┘ │
│                                                 │
└─────────────────────────────────────────────────┘
```

#### 3.2.3 功能实现

**登录流程**
```go
func (app *App) Login(serverAddr, clientID, clientSecret string) error {
    // 1. 连接到Server
    client := api.NewClient(serverAddr)
    
    // 2. 认证
    resp, err := client.Auth(clientID, clientSecret)
    if err != nil {
        return err
    }
    
    // 3. 保存Token
    app.token = resp.Token
    
    // 4. 获取可访问的服务列表
    services, err := client.GetServices(app.token)
    if err != nil {
        return err
    }
    
    app.services = services
    return nil
}
```

**连接服务**
```go
func (app *App) ConnectService(serviceID string, localPort int) error {
    // 1. 获取服务信息
    service := app.getService(serviceID)
    
    // 2. 创建STCP Visitor配置
    visitorConfig := &v1.STCPVisitorConfig{
        BaseVisitorConfig: v1.BaseVisitorConfig{
            Name:       service.Name + "-visitor",
            Type:       "stcp",
            ServerName: service.ServerName,
            SecretKey:  service.SecretKey,
            BindAddr:   "127.0.0.1",
            BindPort:   localPort,
        },
    }
    
    // 3. 启动Visitor
    err := app.frpClient.AddVisitor(visitorConfig)
    if err != nil {
        return err
    }
    
    // 4. 更新状态
    service.Status = "connected"
    service.LocalPort = localPort
    
    return nil
}
```

**断开服务**
```go
func (app *App) DisconnectService(serviceID string) error {
    service := app.getService(serviceID)
    
    // 停止Visitor
    err := app.frpClient.RemoveVisitor(service.Name + "-visitor")
    if err != nil {
        return err
    }
    
    service.Status = "disconnected"
    return nil
}
```

### 3.3 配置文件

```json
{
  "server_addr": "server.example.com:7000",
  "saved_credentials": {
    "client_id": "",
    "client_secret": ""
  },
  "services": [
    {
      "service_id": "1",
      "name": "mysql-dev",
      "local_port": 3306,
      "auto_connect": false
    }
  ]
}
```

## 4. 完整工作流程

### 4.1 管理员创建Agent

```
1. 管理员登录Server网页
2. 点击"新建Agent"
3. 输入agent_name: "dev-agent-001"
4. Server生成随机token
5. 显示给管理员:
   Agent Name: dev-agent-001
   Token: abc123xyz789
6. 管理员拿着这两个信息去部署Agent容器
```

### 4.2 管理员创建STCP实例

```
1. 管理员在Server网页选择Agent: "dev-agent-001"
2. 点击"新建STCP实例"
3. 填写表单:
   - 实例名称: mysql-dev
   - 服务类型: TCP
   - 本地IP: 192.168.1.100
   - 本地端口: 3306
4. Server生成secret_key
5. Server发送指令给Agent
6. Agent创建STCP Proxy
7. 实例状态变为"active"
```

### 4.3 管理员创建Client并授权

```
1. 管理员点击"新建Client"
2. Server生成:
   - Client ID: client-001
   - Client Secret: secret123
3. 管理员将凭证发给用户
4. 管理员在STCP实例页面点击"授权访问"
5. 选择Client: client-001
6. 选择实例: mysql-dev
7. 保存授权关系
```

### 4.4 用户使用Desktop应用

```
1. 用户启动Desktop应用
2. 输入:
   - Server地址: server.example.com:7000
   - Client ID: client-001
   - Client Secret: secret123
3. 点击登录
4. Desktop应用显示可访问的服务列表:
   - mysql-dev
5. 用户选择mysql-dev，输入本地端口3306
6. 点击"连接"
7. Desktop应用创建STCP Visitor
8. 用户使用本地应用连接localhost:3306
```

## 5. 项目目录结构

```
awecloud-signaling/
├── cmd/
│   ├── server/
│   │   └── main.go              # Server主程序
│   └── agent/
│       └── main.go              # Agent主程序
│
├── internal/
│   ├── server/
│   │   ├── api/                 # API处理器
│   │   │   ├── admin.go
│   │   │   ├── agent.go
│   │   │   ├── client.go
│   │   │   └── stcp.go
│   │   ├── model/               # 数据模型
│   │   │   ├── admin.go
│   │   │   ├── agent.go
│   │   │   ├── client.go
│   │   │   └── stcp_instance.go
│   │   ├── service/             # 业务逻辑
│   │   │   ├── agent_service.go
│   │   │   ├── client_service.go
│   │   │   └── stcp_service.go
│   │   ├── frp/                 # FRP Server集成
│   │   │   └── server.go
│   │   └── db/                  # 数据库
│   │       └── sqlite.go
│   │
│   ├── agent/
│   │   ├── client.go            # Agent客户端
│   │   ├── handler.go           # 消息处理
│   │   └── frp/                 # FRP Client集成
│   │       └── proxy.go
│   │
│   └── common/
│       ├── config/              # 配置
│       ├── protocol/            # 通信协议
│       └── util/                # 工具函数
│
├── web/                         # 管理网页
│   ├── src/
│   │   ├── views/
│   │   │   ├── Login.vue
│   │   │   ├── AgentList.vue
│   │   │   ├── ClientList.vue
│   │   │   └── StcpList.vue
│   │   ├── api/
│   │   └── router/
│   ├── package.json
│   └── vite.config.ts
│
├── scripts/
│   ├── build.sh
│   └── docker-build.sh
│
├── deployments/
│   ├── docker/
│   │   ├── Dockerfile.server
│   │   └── Dockerfile.agent
│   └── docker-compose.yml
│
├── go.mod
├── go.sum
└── README.md
```


## 6. 部署方案

### 6.1 Server部署

**Docker Compose部署**
```yaml
# docker-compose.yml
version: '3.8'

services:
  awecloud-signaling-server:
    image: awecloud-signaling-server:latest
    ports:
      - "7000:7000"      # FRP控制端口 (WSS)
      - "8080:8080"      # Web管理界面
    volumes:
      - ./data:/app/data
      - ./logs:/app/logs
      - ./config/server.toml:/app/config/server.toml
      - ./certs:/app/certs  # SSL证书目录
    restart: unless-stopped
```

**启动命令**
```bash
# 1. 创建配置文件和证书
mkdir -p config data logs certs

# 生成自签名证书（开发环境）
openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
  -keyout certs/server.key -out certs/server.crt \
  -subj "/C=CN/ST=Beijing/L=Beijing/O=AWECloud/CN=localhost"

cat > config/server.toml << EOF
[server]
bind_addr = "0.0.0.0"
bind_port = 7000
transport_protocol = "wss"
tls_cert_file = "./certs/server.crt"
tls_key_file = "./certs/server.key"

[database]
type = "sqlite"
path = "./data/awecloud.db"

[web]
listen_addr = "0.0.0.0"
listen_port = 8080
default_admin_username = "admin"
default_admin_password = "admin123"

# 2. 启动服务
docker-compose up -d

# 3. 访问管理界面
# http://your-server:8080
# 用户名: admin
# 密码: admin123
```

### 6.2 Agent部署

**Docker部署**
```bash
# 从Server网页获取agent_name和token后

docker run -d \
docker run -d \
  --name awecloud-signaling-agent \
  --restart unless-stopped \
  -e AGENT_NAME="dev-agent-001" \
  -e AGENT_TOKEN="your-token-here" \
  -e SERVER_ADDR="wss://server.example.com:7000" \
  -e PROTOCOL="wss" \
  awecloud-signaling-agent:latest

**二进制部署**
```bash
# 1. 下载Agent程序
wget https://github.com/your-org/awecloud-signaling/releases/download/v1.0.0/agent-linux-amd64

# 2. 创建配置文件
cat > agent.toml << EOF
[agent]
agent_name = "dev-agent-001"
agent_token = "your-token-here"

[server]
address = "server.example.com"
port = 7000
protocol = "wss"
tls_enable = true
EOF

# 3. 启动Agent
./agent-linux-amd64 -c agent.toml
```

### 6.3 Desktop应用部署

**Windows**
```
1. 下载 awecloud-signaling-desktop-windows.exe
2. 双击安装
3. 启动应用
4. 输入Server地址和Client凭证
5. 登录使用
```

## 7. 开发计划

### 7.1 第一阶段：核心功能（3-4周）

**Week 1-2: Server端**
- [ ] 搭建基础框架（Gin + SQLite）
- [ ] 实现管理员登录
- [ ] 实现Agent管理API
- [ ] 实现Client管理API
- [ ] 实现STCP实例管理API
- [ ] 集成FRP Server

**Week 2-3: Agent端**
- [ ] 搭建Agent框架
- [ ] 实现与Server的连接
- [ ] 实现消息处理
- [ ] 实现动态创建STCP Proxy
- [ ] 集成FRP Client

**Week 3-4: Web管理界面**
- [ ] 登录页面
- [ ] Agent管理页面
- [ ] Client管理页面
- [ ] STCP实例管理页面
- [ ] 权限授权页面

### 7.2 第二阶段：Desktop应用（2-3周）

**Week 5-6: Desktop应用**
- [ ] 搭建Wails项目
- [ ] 实现登录界面
- [ ] 实现服务列表界面
- [ ] 集成FRP Visitor
- [ ] 实现连接/断开功能

**Week 6-7: 测试和优化**
- [ ] 集成测试
- [ ] 性能优化
- [ ] 打包和发布

## 8. 最小可行产品（MVP）

### 8.1 MVP功能范围

**Server端**
- ✅ 管理员登录（单用户）
- ✅ Agent CRUD
- ✅ Client CRUD
- ✅ STCP实例 CRUD
- ✅ 简单的权限授权

**Agent端**
- ✅ 连接Server
- ✅ 创建/删除STCP Proxy
- ✅ 心跳

**Desktop应用**
- ✅ 登录
- ✅ 显示服务列表
- ✅ 连接/断开服务
- ✅ Windows支持

### 8.2 MVP之外的功能（后续版本）

- 多管理员支持
- 详细的监控统计
- 日志查询
- XTCP支持
- 移动端应用
- 更多平台支持（macOS、Linux）

## 9. 技术要点

### 9.1 WSS协议连接实现

#### 9.1.1 为什么使用WSS

**优势**
1. **穿透性强**: WSS基于HTTPS(443端口)，几乎所有防火墙和代理都允许通过
2. **安全性高**: 使用TLS加密，保证数据传输安全
3. **兼容性好**: 可以通过CDN、负载均衡器等网络设备
4. **企业友好**: 企业网络通常只开放80/443端口，WSS可以正常工作

#### 9.1.2 Server端WSS实现

**FRP Server配置修改**
```go
// internal/server/frp/server.go

import (
    "crypto/tls"
    "github.com/fatedier/frp/pkg/transport"
)

func NewFRPServer(cfg *config.ServerConfig) (*frpserver.Service, error) {
    // 配置WSS传输
    transportCfg := &transport.ServerConfig{
        Protocol: "wss",  // 使用WSS协议
        TLS: &transport.TLSConfig{
            CertFile: cfg.TLSCertFile,
            KeyFile:  cfg.TLSKeyFile,
        },
    }
    
    // 创建FRP Server
    svr, err := frpserver.NewService(&frpserver.ServiceOptions{
        BindAddr:      cfg.BindAddr,
        BindPort:      cfg.BindPort,
        Transport:     transportCfg,
        // ... 其他配置
    })
    
    return svr, err
}
```

**Nginx反向代理配置（可选）**
```nginx
# 如果使用Nginx作为前端代理
upstream frp_wss {
    server 127.0.0.1:7000;
}

server {
    listen 443 ssl;
    server_name frp.example.com;
    
    ssl_certificate /path/to/cert.pem;
    ssl_certificate_key /path/to/key.pem;
    
    location / {
        proxy_pass https://frp_wss;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        
        # WebSocket超时设置
        proxy_read_timeout 86400;
        proxy_send_timeout 86400;
    }
}
```

#### 9.1.3 Agent端WSS实现

**FRP Client配置修改**
```go
// internal/agent/frp/client.go

import (
    "crypto/tls"
    "github.com/fatedier/frp/pkg/transport"
)

func NewFRPClient(cfg *config.AgentConfig) (*frpclient.Service, error) {
    // 配置WSS传输
    transportCfg := &transport.ClientConfig{
        Protocol:  "wss",  // 使用WSS协议
        TLSEnable: true,
        // 可选：跳过证书验证（仅用于测试）
        // TLSInsecureSkipVerify: true,
    }
    
    // 创建FRP Client
    cli, err := frpclient.NewService(&frpclient.ServiceOptions{
        ServerAddr:    cfg.ServerAddr,
        ServerPort:    cfg.ServerPort,
        Transport:     transportCfg,
        User:          cfg.AgentName,
        Token:         cfg.AgentToken,
        // ... 其他配置
    })
    
    return cli, err
}
```

**连接建立流程**
```
1. Agent启动
   ↓
2. 解析Server地址: wss://server.example.com:7000
   ↓
3. 建立TLS连接
   ↓
4. 升级到WebSocket协议
   ↓
5. 发送认证信息（agent_name + token）
   ↓
6. Server验证通过，建立控制连接
   ↓
7. 保持长连接，等待Server指令
```

#### 9.1.4 Desktop端WSS实现

**Desktop应用也使用WSS连接**
```go
// awecloud-signaling-desktop/internal/frp/client.go

func (app *App) ConnectToServer(serverAddr string) error {
    // Desktop应用也通过WSS连接到Server
    transportCfg := &transport.ClientConfig{
        Protocol:  "wss",
        TLSEnable: true,
    }
    
    cli, err := frpclient.NewService(&frpclient.ServiceOptions{
        ServerAddr:    serverAddr,
        ServerPort:    7000,
        Transport:     transportCfg,
        User:          app.clientID,
        Token:         app.clientSecret,
    })
    
    if err != nil {
        return err
    }
    
    app.frpClient = cli
    return nil
}
```

#### 9.1.5 证书管理

**自签名证书生成（开发环境）**
```bash
# 生成私钥
openssl genrsa -out server.key 2048

# 生成证书签名请求
openssl req -new -key server.key -out server.csr \
  -subj "/C=CN/ST=Beijing/L=Beijing/O=AWECloud/CN=*.example.com"

# 生成自签名证书
openssl x509 -req -days 365 -in server.csr \
  -signkey server.key -out server.crt
```

**Let's Encrypt证书（生产环境）**
```bash
# 使用certbot自动获取证书
certbot certonly --standalone -d frp.example.com

# 证书路径
# /etc/letsencrypt/live/frp.example.com/fullchain.pem
# /etc/letsencrypt/live/frp.example.com/privkey.pem
```

**配置文件更新**
```toml
# server.toml
[server]
bind_addr = "0.0.0.0"
bind_port = 7000
transport_protocol = "wss"
tls_cert_file = "/etc/letsencrypt/live/frp.example.com/fullchain.pem"
tls_key_file = "/etc/letsencrypt/live/frp.example.com/privkey.pem"
```

#### 9.1.6 连接架构图

```
┌─────────────────────────────────────────────────────────┐
│                    Internet / 防火墙                     │
│                   (只开放443端口)                        │
└─────────────────────────────────────────────────────────┘
                            │
                            │ WSS (443)
                            ▼
                  ┌──────────────────┐
                  │  Nginx (可选)     │
                  │  SSL Termination │
                  └──────────────────┘
                            │
                            │ WSS (7000)
                            ▼
                  ┌──────────────────┐
                  │  FRP Server      │
                  │  (WSS Mode)      │
                  └──────────────────┘
                            │
              ┌─────────────┴─────────────┐
              │                           │
              │ WSS控制连接                │ WSS控制连接
              ▼                           ▼
      ┌──────────────┐            ┌──────────────┐
      │  Agent       │            │  Desktop     │
      │  (内网)       │◄───STCP───┤  (Visitor)   │
      └──────────────┘            └──────────────┘
              │
              │ 本地TCP连接
              ▼
      ┌──────────────┐
      │  MySQL/SSH   │
      │  等服务       │
      └──────────────┘
```

#### 9.1.7 WSS vs TCP 对比

| 特性 | TCP模式 | WSS模式 |
|-----|---------|---------|
| 端口要求 | 需要开放7000端口 | 只需443端口 |
| 防火墙穿透 | 可能被阻止 | 几乎总能通过 |
| 企业网络 | 通常被限制 | 完全兼容 |
| 加密 | 需要额外配置TLS | 内置TLS加密 |
| CDN支持 | 不支持 | 支持 |
| 负载均衡 | 需要特殊配置 | 标准HTTP负载均衡 |
| 性能开销 | 低 | 略高（WebSocket封装） |
| 延迟 | 低 | 略高 |

### 9.2 Server与Agent通信协议

```go
// 消息类型
type MessageType string

const (
    MsgTypeRegister      MessageType = "register"
    MsgTypeHeartbeat     MessageType = "heartbeat"
    MsgTypeCreateSTCP    MessageType = "create_stcp"
    MsgTypeDeleteSTCP    MessageType = "delete_stcp"
    MsgTypeResponse      MessageType = "response"
)

// 消息结构
type Message struct {
    Type    MessageType     `json:"type"`
    Payload json.RawMessage `json:"payload"`
}

// 创建STCP实例请求
type CreateSTCPRequest struct {
    InstanceName string `json:"instance_name"`
    SecretKey    string `json:"secret_key"`
    LocalIP      string `json:"local_ip"`
    LocalPort    int    `json:"local_port"`
}

// 响应
type Response struct {
    Success bool   `json:"success"`
    Message string `json:"message"`
}
```

### 9.3 Client认证流程

```go
// 1. Client发送认证请求
POST /api/client/auth
{
    "client_id": "client-001",
    "client_secret": "secret123"
}

// 2. Server验证并返回Token
{
    "success": true,
    "token": "jwt-token-here",
    "expires_in": 86400
}

// 3. Client使用Token获取服务列表
GET /api/client/services
Header: Authorization: Bearer jwt-token-here

// 4. Server返回服务列表（包含secret_key）
{
    "services": [
        {
            "id": "1",
            "name": "mysql-dev",
            "type": "tcp",
            "server_name": "dev-agent-001.mysql-dev",
            "secret_key": "encrypted-secret-key"
        }
    ]
}
```

### 9.4 关键代码示例

**Server端动态通知Agent**
```go
func (s *Server) CreateSTCPInstance(agentID int64, req *CreateSTCPRequest) error {
    // 1. 保存到数据库
    instance := &STCPInstance{
        AgentID:      agentID,
        InstanceName: req.InstanceName,
        SecretKey:    generateSecretKey(),
        LocalIP:      req.LocalIP,
        LocalPort:    req.LocalPort,
        ServerName:   fmt.Sprintf("%s.%s", agent.Name, req.InstanceName),
    }
    db.Save(instance)
    
    // 2. 通过FRP控制连接发送消息给Agent
    msg := &Message{
        Type: MsgTypeCreateSTCP,
        Payload: marshal(&CreateSTCPRequest{
            InstanceName: instance.InstanceName,
            SecretKey:    instance.SecretKey,
            LocalIP:      instance.LocalIP,
            LocalPort:    instance.LocalPort,
        }),
    }
    
    return s.sendToAgent(agentID, msg)
}
```

**Agent端处理创建请求**
```go
func (a *Agent) handleCreateSTCP(req *CreateSTCPRequest) error {
    // 创建STCP Proxy配置
    proxyConfig := &v1.STCPProxyConfig{
        BaseProxyConfig: v1.BaseProxyConfig{
            Name: req.InstanceName,
            Type: "stcp",
        },
        SecretKey: req.SecretKey,
        LocalIP:   req.LocalIP,
        LocalPort: req.LocalPort,
    }
    
    // 动态添加到FRP Client
    return a.frpClient.AddProxy(proxyConfig)
}
```

## 10. 总结

### 10.1 项目特点

1. **简单**: 只关注核心功能，去除复杂的特性
2. **实用**: 满足基本的内网穿透需求
3. **易部署**: Docker一键部署，配置简单
4. **安全**: 使用STCP模式，不暴露端口

### 10.2 与原方案的简化对比

| 功能 | 原方案 | 简化方案 |
|-----|--------|---------|
| 用户系统 | 完整的用户注册登录 | 仅管理员登录 |
| 数据库 | PostgreSQL + Redis | SQLite |
| 端口管理 | 复杂的端口池 | 无需端口（STCP） |
| 服务发现 | 自动扫描 | 手动配置 |
| 监控统计 | 详细的监控 | 基础状态显示 |
| 权限管理 | RBAC | 简单的授权表 |
| 代理模式 | STCP + XTCP | 仅STCP |

### 10.3 快速开始

```bash
# 1. 部署Server
docker-compose up -d

# 2. 访问管理界面
open http://localhost:8080

# 3. 创建Agent和Client
# 在网页上操作

# 4. 部署Agent
docker run -d -e AGENT_NAME=xxx -e AGENT_TOKEN=xxx awecloud-signaling-agent

# 5. 使用Desktop应用
# 下载并安装，输入凭证登录
```

---

**文档版本**: v1.0  
**最后更新**: 2025-11-25  
**状态**: 待开发
