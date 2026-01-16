# FRP 隧道设计文档

## 1. FRP 连接需求

### 1.1 WebSocket 传输协议

**为什么使用 WebSocket：**

- 穿透性好，可以通过 HTTP 代理和防火墙
- 支持双向通信
- 适合云环境和复杂网络环境

**配置要求：**

- **Server 端**：自动支持所有协议，无需配置
- **Client 端**：需要配置 `transport.protocol = "websocket"`
- 使用标准的 FRP 端口（通常是 7000）
- 可选：配合 TLS 使用（wss://）

**协议支持说明：**

FRP 0.65.0 的设计：

- Server 端自动监听并支持多种协议（tcp, kcp, quic, websocket, wss）
- Client 端选择使用哪种协议连接
- 这样的设计使得同一个 Server 可以同时服务不同协议的客户端

### 1.2 STCP 代理类型

**STCP（Secret TCP）特点：**

- 点对点加密连接
- 需要共享密钥（Secret Key）
- 适合安全的内网穿透场景

**角色说明：**

- **Server（frps）**：中继服务器，负责协调连接
- **Agent（frpc）**：提供服务的一方，运行 STCP 代理
- **Desktop（frpc visitor）**：访问服务的一方，运行 STCP 访问者

**连接流程：**

```
Desktop (Visitor) <--[WebSocket]--> Server <--[WebSocket]--> Agent (Proxy)
                                      ↓
                            验证 Secret Key 匹配
                                      ↓
                            建立加密的点对点隧道
```

## 2. 正确的实现方式

### 2.1 Server 端实现

**文件：** `internal/server/frp/server.go`

**重要说明：**

- FRP Server **自动支持所有传输协议**（TCP, KCP, QUIC, WebSocket, WSS）
- Server 端的 `ServerTransportConfig` **没有 Protocol 字段**
- 客户端可以选择任意协议连接，Server 会自动适配

**证据来源：**

查看 FRP 0.65.0 源码 `server/service.go` 的 `NewService` 函数：

```go
// 主 TCP listener（在 BindPort 上监听）
ln, err := net.Listen("tcp", address)
svr.muxer = mux.NewMux(ln)  // 使用 muxer 复用同一端口

// KCP listener（需要配置 KCPBindPort）
if cfg.KCPBindPort > 0 {
    svr.kcpListener, err = netpkg.ListenKcp(address)
}

// QUIC listener（需要配置 QUICBindPort）
if cfg.QUICBindPort > 0 {
    svr.quicListener, err = quic.ListenAddr(address, quicTLSCfg, ...)
}

// WebSocket listener（自动在主端口上通过 muxer 识别）
websocketPrefix := []byte("GET " + netpkg.FrpWebsocketPath)
websocketLn := svr.muxer.Listen(0, uint32(len(websocketPrefix)), func(data []byte) bool {
    return bytes.Equal(data, websocketPrefix)
})
svr.websocketListener = netpkg.NewWebsocketListener(websocketLn)
```

**关键点：**

1. TCP 和 WebSocket 共享同一个端口（BindPort），通过 muxer 根据请求头识别
2. KCP 和 QUIC 需要单独的端口配置（可选）
3. Server 在 `Run()` 方法中同时启动所有 listener：
   ```go
   go svr.HandleListener(svr.websocketListener, false)
   go svr.HandleListener(svr.kcpListener, false)
   go svr.HandleListener(svr.quicListener, false)
   ```

参考：[FRP 0.65.0 server/service.go](https://github.com/fatedier/frp/blob/v0.65.0/server/service.go)

**核心配置：**

```go
import (
    v1 "github.com/fatedier/frp/pkg/config/v1"
    "github.com/fatedier/frp/server"
)

func NewFRPServer(cfg *config.ServerConfig) (*FRPServer, error) {
    // 创建 FRP Server 配置
    svrCfg := &v1.ServerConfig{
        BindAddr: cfg.Server.BindAddr,  // "0.0.0.0"
        BindPort: cfg.Server.BindPort,  // 7000
    }

    // 注意：ServerTransportConfig 没有 Protocol 字段
    // Server 会自动支持所有协议（tcp, kcp, quic, websocket, wss）

    // 可选：配置 TLS（用于 wss:// 连接）
    if cfg.Server.TLSCertFile != "" && cfg.Server.TLSKeyFile != "" {
        svrCfg.Transport.TLS = v1.TLSServerConfig{
            Force: true,
            TLSConfig: v1.TLSConfig{
                CertFile: cfg.Server.TLSCertFile,
                KeyFile:  cfg.Server.TLSKeyFile,
            },
        }
    }

    // 可选：配置认证（生产环境推荐）
    // svrCfg.Auth.Method = v1.AuthMethod("token")
    // svrCfg.Auth.Token = "your-secure-token"

    // 创建 FRP Server 实例
    svr, err := server.NewService(svrCfg)
    if err != nil {
        return nil, fmt.Errorf("创建FRP Server失败: %w", err)
    }

    return &FRPServer{svr: svr, ...}, nil
}

func (f *FRPServer) Run() error {
    // 启动 FRP Server（阻塞运行）
    return f.svr.Run(f.ctx)
}
```

**配置文件：** `config/server.toml`

```toml
[server]
bind_addr = "0.0.0.0"
bind_port = 7000
# 注意：不需要 transport_protocol 配置
# Server 自动支持所有协议（tcp, kcp, quic, websocket, wss）
tls_cert_file = ""  # 可选，用于 wss:// 连接
tls_key_file = ""   # 可选
```

### 2.2 Agent 端实现

**文件：** `internal/agent/frp_manager.go`

**核心配置：**

```go
import (
    "github.com/fatedier/frp/client"
    v1 "github.com/fatedier/frp/pkg/config/v1"
)

func (f *FRPManager) runFRPClient() error {
    // 创建 FRP Client 配置
    clientCfg := v1.ClientCommonConfig{
        ServerAddr: f.config.Server.Address,  // "server.example.com"
        ServerPort: f.config.Server.Port,     // 7000
    }

    // 配置 WebSocket 传输协议
    clientCfg.Transport.Protocol = "websocket"

    // 可选：配置 TLS
    if f.config.Server.TLSEnable {
        enable := true
        clientCfg.Transport.TLS.Enable = &enable
    }

    // 可选：配置认证
    // clientCfg.Auth.Method = v1.AuthMethod("token")
    // clientCfg.Auth.Token = "your-secure-token"

    // 准备 STCP 代理配置列表
    f.mutex.RLock()
    proxyCfgs := make([]v1.ProxyConfigurer, 0, len(f.proxies))
    for _, cfg := range f.proxies {
        proxyCfgs = append(proxyCfgs, cfg)
    }
    f.mutex.RUnlock()

    // 创建 FRP Client 服务
    svr, err := client.NewService(client.ServiceOptions{
        Common:         &clientCfg,
        ProxyCfgs:      proxyCfgs,
        VisitorCfgs:    []v1.VisitorConfigurer{},
        ConfigFilePath: "",
    })
    if err != nil {
        return fmt.Errorf("创建FRP客户端失败: %w", err)
    }

    // 启动 FRP Client（阻塞运行）
    return svr.Run(f.ctx)
}
```

**STCP 代理配置：**

```go
func (f *FRPManager) addProxyInternal(instanceName, secretKey, localIP string, localPort int32) error {
    // 创建 STCP 代理配置
    proxyConfig := &v1.STCPProxyConfig{
        ProxyBaseConfig: v1.ProxyBaseConfig{
            Name: instanceName,  // 唯一标识，如 "ssh-server-1"
            Type: "stcp",
            ProxyBackend: v1.ProxyBackend{
                LocalIP:   localIP,    // 本地服务IP，如 "127.0.0.1"
                LocalPort: int(localPort),  // 本地服务端口，如 22
            },
        },
        Secretkey: secretKey,  // 密钥，如 "abc123"
    }

    f.proxies[instanceName] = proxyConfig

    // 重启 FRP Client 以应用新配置
    if f.service != nil {
        f.service.Close()
    }

    return nil
}
```

**配置文件：** `config/agent.toml`

```toml
[agent]
agent_name = "agent-01"
agent_token = "..."

[server]
address = "server.example.com"
port = 7000
grpc_port = 9091
tls_enable = false
```

### 2.3 Desktop 端实现

Desktop 端使用标准的 FRP 客户端（frpc），以 STCP Visitor 模式运行。

**配置文件方式：** `frpc.toml`

```toml
# 基础配置
serverAddr = "server.example.com"
serverPort = 7000

# WebSocket 传输协议
transport.protocol = "websocket"

# 可选：TLS
# transport.tls.enable = true

# 可选：认证
# auth.method = "token"
# auth.token = "your-secure-token"

# STCP Visitor 配置
[[visitors]]
name = "ssh-server-1-visitor"
type = "stcp"
role = "visitor"
serverName = "ssh-server-1"  # 对应 Agent 端的 instanceName
secretKey = "abc123"         # 必须与 Agent 端一致
bindAddr = "127.0.0.1"
bindPort = 2222              # 本地监听端口
```

**使用方式：**

```bash
# 启动 frpc
./frpc -c frpc.toml

# 访问服务
ssh user@127.0.0.1 -p 2222
```

**INI 格式（旧版兼容）：** `frpc.ini`

```ini
[common]
server_addr = server.example.com
server_port = 7000
protocol = websocket

[ssh-server-1-visitor]
type = stcp
role = visitor
server_name = ssh-server-1
sk = abc123
bind_addr = 127.0.0.1
bind_port = 2222
```

## 3. 完整的连接示例

### 场景：通过 STCP 访问 Agent 机器上的 SSH 服务

**步骤 1：Server 端启动**

```bash
# 启动 Server
./awecloud-signaling-server server

# 日志输出
[INFO] FRP Server启动在: 0.0.0.0:7000
[INFO] 传输协议: websocket
```

**步骤 2：Agent 端连接并创建 STCP 实例**

```bash
# 启动 Agent
./awecloud-signaling-server agent

# 通过 Web 界面或 API 创建 STCP 实例
# - Instance Name: ssh-server-1
# - Secret Key: abc123
# - Local IP: 127.0.0.1
# - Local Port: 22

# Agent 日志输出
[INFO] FRP客户端已创建，代理数量: 1
[INFO] FRP STCP代理已添加: ssh-server-1 -> 127.0.0.1:22
```

**步骤 3：Desktop 端连接**

```bash
# 创建 frpc.toml 配置文件（见 2.3 节）

# 启动 frpc
./frpc -c frpc.toml

# 日志输出
[INFO] start frpc service for config file [frpc.toml]
[INFO] [ssh-server-1-visitor] start visitor success

# 访问 SSH 服务
ssh user@127.0.0.1 -p 2222
```

### 数据流向

```
Desktop 本地应用
    ↓ (连接 127.0.0.1:2222)
frpc (STCP Visitor)
    ↓ (WebSocket 加密连接)
Server (frps)
    ↓ (WebSocket 加密连接)
Agent (frpc STCP Proxy)
    ↓ (本地连接 127.0.0.1:22)
SSH 服务
```

---

## 问题排查记录（临时）

### 问题已解决 ✅

**根本原因：**
缺少 `Complete()` 方法调用来填充 FRP 配置的默认值！

**解决方案：**

Server 端 (`internal/server/frp/server.go`)：
```go
svrCfg := &v1.ServerConfig{
    BindAddr: cfg.Server.BindAddr,
    BindPort: cfg.Server.BindPort,
}

// 关键步骤：完成配置（填充默认值）
if err := svrCfg.Complete(); err != nil {
    return nil, fmt.Errorf("完成FRP Server配置失败: %w", err)
}
```

Agent 端 (`internal/agent/frp_manager.go`)：
```go
clientCfg := v1.ClientCommonConfig{
    ServerAddr: f.config.Server.Address,
    ServerPort: f.config.Server.Port,
}
clientCfg.Transport.Protocol = "websocket"

// 关键步骤：完成配置（填充默认值）
if err := clientCfg.Complete(); err != nil {
    return fmt.Errorf("完成FRP客户端配置失败: %w", err)
}
```

**验证结果：**
- ✅ WebSocket 协议连接成功
- ✅ Token 认证工作正常
- ✅ STCP 代理创建成功
- ✅ 动态添加/删除代理正常
- ✅ Agent 重连机制正常

**参考：**
FRP 官方代码 `cmd/frps/root.go` 和 `cmd/frpc/sub/root.go` 中都调用了 `Complete()` 方法。

### 排查步骤

#### 第一步：修复 WebSocket 配置 ✅

**问题：** Agent 端缺少 WebSocket 协议配置

**修复：** 在 `internal/agent/frp_manager.go` 的 `runFRPClient()` 函数中添加：

```go
// 配置 WebSocket 传输协议
clientCfg.Transport.Protocol = "websocket"
log.Println("FRP客户端使用 WebSocket 协议")
```

**Server 端：** 无需修改，自动支持所有协议

#### 第二步：验证连接

```bash
# 启动 Server
./awecloud-signaling-server server

# 日志应显示：
# [INFO] FRP Server启动在: 0.0.0.0:7000
# [INFO] FRP Server 将自动支持所有传输协议（TCP, WebSocket 等）

# 启动 Agent
./awecloud-signaling-server agent

# 日志应显示：
# [INFO] FRP管理器启动，连接到: localhost:7000
# [INFO] FRP客户端使用 WebSocket 协议
# [INFO] FRP客户端已创建，代理数量: X
```

#### 第三步：测试 STCP 隧道

1. 在 Agent 机器上启动测试服务：
   ```bash
   python3 -m http.server 8080
   ```

2. 通过 Web 界面创建 STCP 实例：
   - Instance Name: `test-http`
   - Secret Key: `test123`
   - Local IP: `127.0.0.1`
   - Local Port: `8080`

3. 在 Desktop 端配置 frpc visitor 并测试访问

### 检查清单

Server 端：

- [ ] FRP Server 成功启动
- [ ] 监听 7000 端口
- [ ] WebSocket 协议已配置
- [ ] 看到 Agent 连接日志

Agent 端：

- [ ] FRP Client 成功启动
- [ ] 连接到 Server
- [ ] WebSocket 协议已配置
- [ ] STCP 代理已注册

Desktop 端：

- [ ] frpc 配置正确
- [ ] WebSocket 协议已配置
- [ ] Secret Key 与 Agent 一致
- [ ] 能够建立 Visitor 连接

---

**注意：问题解决后，删除"问题排查记录"部分，仅保留第 1、2 节内容。**
