# 配置文件说明

## 文件列表

- `server.toml` - Server配置文件（开发环境）
- `agent.toml` - Agent配置文件（开发环境）
- `server.toml.example` - Server配置模板
- `agent.toml.example` - Agent配置模板

## Server配置 (server.toml)

### [web] - Web服务配置

- `listen_addr` - 监听地址（默认：0.0.0.0）
- `listen_port` - HTTP端口（默认：8080）
- `default_admin_username` - 默认管理员用户名
- `default_admin_password` - 默认管理员密码

### [security] - 安全配置

- `jwt_secret` - JWT密钥（生产环境必须修改）

### [database] - 数据库配置

- `path` - SQLite数据库文件路径

### [log] - 日志配置

- `level` - 日志级别（debug/info/warn/error）
- `file` - 日志文件路径（空表示输出到控制台）

### [server] - FRP服务配置

- `bind_addr` - FRP监听地址
- `bind_port` - FRP端口（默认：7000）
- `transport_protocol` - 传输协议（websocket）
- `tls_cert_file` - TLS证书文件（可选）
- `tls_key_file` - TLS密钥文件（可选）

## Agent配置 (agent.toml)

### [agent] - Agent配置

- `agent_name` - Agent名称
- `agent_token` - Agent认证Token（从Server获取）

### [server] - Server连接配置

- `address` - Server地址
- `port` - Server FRP端口
- `tls_enable` - 是否启用TLS

## 使用说明

### 1. 开发环境

配置文件已经创建好，可以直接使用：

```bash
# 启动Server
go run cmd/server/main.go -c config/server.toml

# 或使用VSCode调试（F5）
```

### 2. 生产环境

复制示例文件并修改：

```bash
cp config/server.toml.example config/server.toml
cp config/agent.toml.example config/agent.toml

# 修改配置
vim config/server.toml
```

**重要**：生产环境必须修改：
- `jwt_secret` - 使用强密码
- `default_admin_password` - 修改默认密码
- `tls_cert_file` 和 `tls_key_file` - 启用TLS

### 3. Agent配置

Agent的token需要从Server获取：

1. 启动Server
2. 登录Web界面（http://localhost:8080）
3. 创建Agent，获取token
4. 将token填入 `config/agent.toml`
5. 启动Agent

## 端口说明

- **8080** - Web管理界面和API
- **8081** - gRPC服务（Agent连接）
- **7000** - FRP信令服务（WebSocket）

## 安全建议

### 开发环境

- 使用默认配置即可
- 不需要TLS

### 生产环境

1. **修改默认密码**
   ```toml
   default_admin_password = "strong-password-here"
   ```

2. **使用强JWT密钥**
   ```toml
   jwt_secret = "use-a-long-random-string-here"
   ```

3. **启用TLS**
   ```toml
   tls_cert_file = "/path/to/cert.pem"
   tls_key_file = "/path/to/key.pem"
   ```

4. **限制监听地址**
   ```toml
   listen_addr = "127.0.0.1"  # 只监听本地
   ```

5. **使用反向代理**
   - 使用Nginx或Traefik
   - 在代理层处理TLS
   - 限制访问IP

## 故障排查

### 问题1：配置文件不存在

```bash
mkdir -p config
cp config/server.toml.example config/server.toml
```

### 问题2：端口被占用

修改配置文件中的端口：
```toml
listen_port = 8081  # 改为其他端口
```

### 问题3：数据库权限错误

确保data目录可写：
```bash
mkdir -p data
chmod 755 data
```

### 问题4：Agent无法连接

检查：
1. Server是否启动
2. agent_token是否正确
3. Server地址和端口是否正确
4. 防火墙是否开放端口

## 配置文件位置

- 开发环境：`config/server.toml`
- 生产环境：可通过 `-c` 参数指定

```bash
./bin/server -c /etc/awecloud/server.toml
./bin/agent -c /etc/awecloud/agent.toml
```

## 环境变量

暂不支持环境变量配置，所有配置通过TOML文件。

## 相关文档

- [开发指南](../DEVELOPMENT.md)
- [部署文档](../deployments/kubernetes/README.md)
- [项目README](../README.md)
