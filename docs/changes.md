# 设计变更记录

## 2025-11-27: HTTP/2 统一端口设计

### 变更概述

将Server的HTTP和gRPC服务从两个端口（8080和8081）合并到单一端口（8080），使用HTTP/2协议统一处理。

### 变更原因

1. **简化部署**: 减少需要开放的端口数量
2. **符合标准**: gRPC基于HTTP/2，可以与HTTP共用端口
3. **易于管理**: 统一的端口配置和防火墙规则
4. **现代化**: 符合现代微服务架构的最佳实践

### 技术方案

选择**方案3: 纯HTTP/2（最简单）**

#### 实现方式

```go
// 统一处理器
handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    if r.ProtoMajor == 2 && strings.HasPrefix(
        r.Header.Get("Content-Type"), "application/grpc") {
        // gRPC请求
        grpcServer.ServeHTTP(w, r)
    } else {
        // HTTP请求
        ginRouter.ServeHTTP(w, r)
    }
})

// HTTP/2服务器
h2s := &http2.Server{}
server := &http.Server{
    Addr:    ":8080",
    Handler: h2c.NewHandler(handler, h2s),
}
```

### 变更内容

#### 端口配置

**之前**:
- 端口8080: HTTP（Web界面 + RESTful API）
- 端口8081: gRPC（Agent和Desktop连接）
- 端口7000: WebSocket（FRP信令）

**之后**:
- 端口8080: HTTP/2（Web界面 + RESTful API + gRPC）
- 端口7000: WebSocket（FRP信令）

#### 文档更新

已更新以下文档：

1. **docs/design.md**
   - 更新架构图
   - 更新Server-Web线程说明
   - 更新通信协议总结

2. **docs/design_http2.md** (新建)
   - HTTP/2统一端口的详细设计
   - 实现方案和代码示例
   - 测试和迁移指南

3. **docs/design_desktop.md**
   - 更新Desktop连接端口（8081 → 8080）

4. **README.md**
   - 更新端口说明

5. **desktop/RELEASE.md**
   - 更新示例地址（8081 → 8080）

6. **desktop/docs/user-guide.md**
   - 更新服务器地址示例（8081 → 8080）

7. **docs/README.md**
   - 添加design_http2.md链接

#### 配置文件

无需更改，config/server.toml已经使用8080端口。

### 影响范围

#### 需要更新的代码

1. **internal/server/server.go**
   - 实现HTTP/2统一处理器
   - 移除8081端口监听
   - 添加Content-Type路由逻辑

2. **Agent客户端**
   - 连接地址从8081改为8080
   - 无需其他更改（gRPC客户端自动支持）

3. **Desktop客户端**
   - 连接地址从8080改为8080（已经正确）
   - 无需其他更改

#### 不受影响的部分

- ✅ 数据库设计
- ✅ API定义
- ✅ FRP配置（端口7000不变）
- ✅ Web前端代码
- ✅ Protocol Buffers定义

### 实施计划

#### 阶段1: 代码实现 ✅ 已完成

1. ✅ 修改 `internal/server/server.go`
2. ✅ 实现HTTP/2统一处理器
3. ✅ 添加Content-Type路由逻辑
4. ✅ 移除8081端口相关代码
5. ✅ 更新Agent配置文件

**实现代码**:
```go
// 创建统一处理器
handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    if r.ProtoMajor == 2 && strings.HasPrefix(
        r.Header.Get("Content-Type"), "application/grpc") {
        s.grpcServer.ServeHTTP(w, r)  // gRPC
    } else {
        ginRouter.ServeHTTP(w, r)     // HTTP
    }
})

// 使用h2c支持HTTP/2
h2s := &http2.Server{}
s.httpServer = &http.Server{
    Addr:    addr,
    Handler: h2c.NewHandler(handler, h2s),
}
```

#### 阶段2: 测试（进行中）

1. ✅ 创建测试脚本 `scripts/test_http2.sh`
2. ⏳ 测试HTTP请求（Web界面、RESTful API）
3. ⏳ 测试gRPC请求（Agent、Desktop）
4. ⏳ 测试并发场景
5. ⏳ 性能测试

#### 阶段3: 部署（待完成）

1. ⏳ 更新部署配置
2. ⏳ 更新防火墙规则
3. ⏳ 更新Traefik配置（如果需要）

### 优势

1. ✅ **简化部署**: 只需开放2个端口（8080和7000）
2. ✅ **性能优化**: HTTP/2多路复用，减少连接数
3. ✅ **易于维护**: 统一的端口管理
4. ✅ **符合标准**: 现代微服务架构的最佳实践
5. ✅ **安全性**: 更少的攻击面

### 风险和注意事项

#### 风险

1. **兼容性**: 需要确保所有客户端支持HTTP/2
   - ✅ Go gRPC库原生支持
   - ✅ 现代浏览器都支持

2. **调试复杂度**: HTTP和gRPC混合可能增加调试难度
   - ✅ 可以通过Content-Type区分
   - ✅ 日志中记录请求类型

#### 注意事项

1. **TLS要求**:
   - 开发环境: 可以使用h2c（HTTP/2 without TLS）
   - 生产环境: 建议使用TLS

2. **代理配置**:
   - 确保Traefik支持HTTP/2（默认支持）
   - 确保正确转发gRPC请求

3. **测试工具**:
   - HTTP: curl, Postman
   - gRPC: grpcurl

### 回滚方案

如果遇到问题，可以快速回滚：

1. 恢复8081端口监听
2. 恢复原有的gRPC服务器代码
3. 更新客户端连接地址

### 参考资料

- [HTTP/2 RFC 7540](https://tools.ietf.org/html/rfc7540)
- [gRPC over HTTP/2](https://github.com/grpc/grpc/blob/master/doc/PROTOCOL-HTTP2.md)
- [Go HTTP/2 Package](https://pkg.go.dev/golang.org/x/net/http2)

---

**变更状态**: 设计完成，待实现  
**负责人**: 开发团队  
**审批日期**: 2025-11-27
