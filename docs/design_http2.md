# HTTP/2 统一端口设计

## 概述

Server-Web线程使用**单一端口8080**同时处理HTTP和gRPC请求，通过HTTP/2协议实现统一。

## 设计原理

### 协议基础

- **HTTP/1.1**: 传统的HTTP协议
- **HTTP/2**: 现代HTTP协议，支持多路复用
- **gRPC**: 基于HTTP/2的RPC框架

### 关键特性

gRPC使用HTTP/2作为传输层，通过特殊的Content-Type标识：
- gRPC请求: `Content-Type: application/grpc`
- HTTP请求: `Content-Type: application/json` 或其他

## 实现方案

### 方案选择

我们选择**方案3: 纯HTTP/2**，原因：
1. ✅ 最简单，无需额外依赖
2. ✅ 性能好，原生支持
3. ✅ 易于调试和维护
4. ✅ 符合现代Web标准

### 核心实现

```go
package main

import (
    "net/http"
    "strings"
    
    "github.com/gin-gonic/gin"
    "google.golang.org/grpc"
    "golang.org/x/net/http2"
    "golang.org/x/net/http2/h2c"
)

func main() {
    // 创建Gin路由（HTTP处理）
    ginRouter := gin.Default()
    ginRouter.GET("/api/...", httpHandler)
    
    // 创建gRPC服务器
    grpcServer := grpc.NewServer()
    pb.RegisterAgentServiceServer(grpcServer, &agentService{})
    pb.RegisterClientServiceServer(grpcServer, &clientService{})
    
    // 创建统一处理器
    handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // 根据Content-Type区分请求类型
        if r.ProtoMajor == 2 && strings.HasPrefix(
            r.Header.Get("Content-Type"), "application/grpc") {
            // gRPC请求
            grpcServer.ServeHTTP(w, r)
        } else {
            // HTTP请求
            ginRouter.ServeHTTP(w, r)
        }
    })
    
    // 启动HTTP/2服务器
    // 开发环境：使用h2c（HTTP/2 Cleartext，无TLS）
    h2s := &http2.Server{}
    server := &http.Server{
        Addr:    ":8080",
        Handler: h2c.NewHandler(handler, h2s),
    }
    
    // 生产环境：使用TLS
    // server.ListenAndServeTLS("cert.pem", "key.pem")
    
    // 开发环境：不使用TLS
    server.ListenAndServe()
}
```

### 关键代码说明

1. **请求区分**:
```go
if r.ProtoMajor == 2 && strings.HasPrefix(
    r.Header.Get("Content-Type"), "application/grpc") {
    // 这是gRPC请求
    grpcServer.ServeHTTP(w, r)
} else {
    // 这是HTTP请求
    ginRouter.ServeHTTP(w, r)
}
```

2. **HTTP/2支持**:
```go
// 开发环境：h2c（HTTP/2 without TLS）
h2c.NewHandler(handler, h2s)

// 生产环境：自动支持HTTP/2（需要TLS）
server.ListenAndServeTLS("cert.pem", "key.pem")
```

## 端口配置

### 开发环境

```toml
[web]
listen_addr = "0.0.0.0"
listen_port = 8080  # HTTP + gRPC统一端口
```

### 生产环境（Traefik）

```yaml
# Traefik配置
http:
  routers:
    server-web:
      rule: "Host(`your-domain.com`)"
      service: server-web
      tls:
        certResolver: letsencrypt
  
  services:
    server-web:
      loadBalancer:
        servers:
          - url: "http://server:8080"  # 统一端口
```

## 客户端连接

### Agent连接

```go
// gRPC连接（HTTP/2）
conn, err := grpc.Dial(
    "server:8080",
    grpc.WithTransportCredentials(insecure.NewCredentials()),
)
```

### Desktop连接

```go
// gRPC连接（HTTP/2）
conn, err := grpc.Dial(
    "server:8080",
    grpc.WithTransportCredentials(insecure.NewCredentials()),
)
```

### Web浏览器

```
# HTTP/1.1或HTTP/2（浏览器自动协商）
https://your-domain.com/
```

## 协议协商

### HTTP/2协商流程

1. **TLS环境**（生产）:
```
Client → Server: ClientHello (ALPN: h2, http/1.1)
Server → Client: ServerHello (ALPN: h2)
→ 使用HTTP/2
```

2. **非TLS环境**（开发）:
```
Client → Server: HTTP/1.1 Upgrade: h2c
Server → Client: 101 Switching Protocols
→ 使用HTTP/2
```

### Content-Type路由

```
请求 → Server:8080
  ↓
检查Content-Type
  ↓
├─ application/grpc → gRPC处理器
│   ├─ AgentService
│   └─ ClientService
│
└─ 其他 → HTTP处理器（Gin）
    ├─ Web界面
    └─ RESTful API
```

## 优势

### 1. 简化部署

- ✅ 只需要开放一个端口（8080）
- ✅ 防火墙配置更简单
- ✅ 负载均衡配置更简单

### 2. 性能优化

- ✅ HTTP/2多路复用，减少连接数
- ✅ 头部压缩，减少带宽
- ✅ 服务器推送（可选）

### 3. 开发体验

- ✅ 统一的端口管理
- ✅ 更容易调试
- ✅ 符合现代Web标准

### 4. 安全性

- ✅ TLS加密（生产环境）
- ✅ 统一的安全策略
- ✅ 更少的攻击面

## 兼容性

### 浏览器支持

- ✅ Chrome 41+
- ✅ Firefox 36+
- ✅ Safari 9+
- ✅ Edge 12+

### gRPC客户端

- ✅ Go gRPC库（原生支持）
- ✅ 其他语言的gRPC库（原生支持）

### 开发工具

- ✅ curl（需要--http2参数）
- ✅ Postman（自动支持）
- ✅ grpcurl（原生支持）

## 测试

### HTTP请求测试

```bash
# HTTP/1.1
curl http://localhost:8080/api/agents

# HTTP/2（需要TLS）
curl --http2 https://your-domain.com/api/agents
```

### gRPC请求测试

```bash
# 使用grpcurl
grpcurl -plaintext localhost:8080 list
grpcurl -plaintext localhost:8080 awecloud.signaling.AgentService/Register
```

## 注意事项

### 1. TLS要求

- **开发环境**: 可以不使用TLS（使用h2c）
- **生产环境**: 强烈建议使用TLS
  - 浏览器要求HTTPS才能使用HTTP/2
  - gRPC可以使用明文HTTP/2，但不安全

### 2. 代理配置

如果使用Nginx或Traefik等反向代理：
- 确保代理支持HTTP/2
- 确保代理正确转发gRPC请求
- Traefik默认支持，无需特殊配置

### 3. 防火墙

只需要开放：
- 端口8080（HTTP + gRPC）
- 端口7000（FRP WebSocket）

## 迁移指南

### 从双端口迁移

如果之前使用8080（HTTP）和8081（gRPC）：

1. **更新Server代码**:
   - 实现统一处理器
   - 移除8081端口监听

2. **更新客户端**:
   - Agent: 连接8080而不是8081
   - Desktop: 连接8080而不是8081

3. **更新配置**:
   - 移除gRPC端口配置
   - 只保留web端口配置

4. **更新文档**:
   - 所有文档中的8081改为8080

## 参考资料

- [HTTP/2 RFC 7540](https://tools.ietf.org/html/rfc7540)
- [gRPC over HTTP/2](https://github.com/grpc/grpc/blob/master/doc/PROTOCOL-HTTP2.md)
- [Go HTTP/2 Package](https://pkg.go.dev/golang.org/x/net/http2)
- [gRPC Go Documentation](https://grpc.io/docs/languages/go/)

---

**文档版本**: v1.0  
**创建日期**: 2025-11-27  
**状态**: 设计完成，待实现
