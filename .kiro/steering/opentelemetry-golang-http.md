# HTTP OpenTelemetry 集成规范

## 概述

HTTP 支持服务端（Gin）和客户端的 OpenTelemetry 追踪。服务端作为入口接收或创建 trace，客户端用于追踪对外部 HTTP API 的调用。

## 依赖包

```go
require (
    github.com/gin-gonic/gin v1.9.1
    go.opentelemetry.io/otel v1.39.0
    go.opentelemetry.io/otel/trace v1.39.0
    go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.58.0  // HTTP Client
)
```

## HTTP Server（Gin 中间件）

### 集成位置

Gin 路由初始化处，通常在 `internal/server/server.go` 或路由配置文件中。

### 集成方式

自定义 Gin 中间件实现（位于 `internal/common/telemetry/gin.go`）：

```go
import (
    "github.com/gin-gonic/gin"
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/propagation"
    "go.opentelemetry.io/otel/trace"
    semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// GinMiddleware 返回 Gin 的 OpenTelemetry 中间件
func GinMiddleware(serviceName string) gin.HandlerFunc {
    tracer := otel.Tracer(serviceName)
    propagator := otel.GetTextMapPropagator()

    return func(c *gin.Context) {
        // 1. 从请求头提取上游 trace context
        ctx := propagator.Extract(c.Request.Context(), propagation.HeaderCarrier(c.Request.Header))

        // 2. 创建 span
        spanName := c.Request.Method + " " + c.FullPath()
        ctx, span := tracer.Start(ctx, spanName,
            trace.WithSpanKind(trace.SpanKindServer),
            trace.WithAttributes(
                semconv.HTTPMethodKey.String(c.Request.Method),
                semconv.HTTPRouteKey.String(c.FullPath()),
                semconv.HTTPURLKey.String(c.Request.URL.String()),
                semconv.HTTPSchemeKey.String(c.Request.URL.Scheme),
                semconv.NetHostNameKey.String(c.Request.Host),
                semconv.HTTPUserAgentKey.String(c.Request.UserAgent()),
                semconv.HTTPClientIPKey.String(c.ClientIP()),
            ),
        )
        defer span.End()

        // 3. 将 context 注入到 gin.Context
        c.Request = c.Request.WithContext(ctx)

        // 4. 执行后续处理器
        c.Next()

        // 5. 记录响应状态
        span.SetAttributes(
            semconv.HTTPStatusCodeKey.Int(c.Writer.Status()),
        )

        // 6. 如果有错误，记录错误信息
        if len(c.Errors) > 0 {
            span.RecordError(c.Errors.Last())
        }
    }
}
```

### 注册中间件

```go
func SetupRouter() *gin.Engine {
    r := gin.New()

    // 注册 OpenTelemetry 中间件
    r.Use(telemetry.GinMiddleware("your-service"))

    // 注册其他中间件
    r.Use(gin.Logger())
    r.Use(gin.Recovery())

    // 注册路由
    api := r.Group("/api/v1")
    {
        api.GET("/users/:id", GetUser)
        api.POST("/users", CreateUser)
    }

    logger.Info("HTTP Server OpenTelemetry 追踪已启用")
    return r
}
```

### Handler 实现

```go
func GetUser(c *gin.Context) {
    // 从 gin.Context 获取带 trace 的 context
    ctx := c.Request.Context()

    userID := c.Param("id")

    // 传递 context 给 Service 层
    user, err := userService.GetByID(ctx, userID)
    if err != nil {
        c.JSON(500, gin.H{"error": err.Error()})
        return
    }

    c.JSON(200, user)
}
```

### Jaeger 显示效果

**Span 名称**：`GET /api/v1/users/:id`

**Span 属性**：

| 属性               | 说明       | 示例                                     |
| ------------------ | ---------- | ---------------------------------------- |
| `http.method`      | HTTP 方法  | `GET`, `POST`, `PUT`, `DELETE`           |
| `http.route`       | 路由模板   | `/api/v1/users/:id`                      |
| `http.url`         | 完整 URL   | `http://localhost:8080/api/v1/users/123` |
| `http.scheme`      | 协议       | `http`, `https`                          |
| `http.status_code` | 响应状态码 | `200`, `404`, `500`                      |
| `http.user_agent`  | User-Agent | `Mozilla/5.0...`                         |
| `http.client_ip`   | 客户端 IP  | `192.168.1.100`                          |
| `net.host.name`    | 主机名     | `localhost:8080`                         |

## HTTP Client（外部 API 调用）

### 集成位置

HTTP 客户端初始化处，用于调用外部 API。

### 集成方式

```go
import (
    "net/http"
    "time"

    "go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func NewHTTPClient() *http.Client {
    return &http.Client{
        // 使用 otelhttp.NewTransport 包装默认 Transport
        Transport: otelhttp.NewTransport(http.DefaultTransport),
        Timeout:   30 * time.Second,
    }
}
```

### 自定义配置（可选）

```go
func NewHTTPClient() *http.Client {
    return &http.Client{
        Transport: otelhttp.NewTransport(
            http.DefaultTransport,
            otelhttp.WithSpanNameFormatter(func(operation string, r *http.Request) string {
                // 自定义 Span 名称
                return fmt.Sprintf("%s %s", r.Method, r.URL.Path)
            }),
        ),
        Timeout: 30 * time.Second,
    }
}
```

### 使用方式

```go
type APIService struct {
    httpClient *http.Client
}

func NewAPIService() *APIService {
    return &APIService{
        httpClient: NewHTTPClient(),
    }
}

// 调用外部 API
func (s *APIService) GetUserInfo(ctx context.Context, userID string) (*UserInfo, error) {
    url := fmt.Sprintf("https://api.example.com/users/%s", userID)

    // 使用 NewRequestWithContext 传递 context
    req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
    if err != nil {
        return nil, fmt.Errorf("创建请求失败: %w", err)
    }

    // 添加请求头
    req.Header.Set("Authorization", "Bearer "+s.token)
    req.Header.Set("Content-Type", "application/json")

    // 执行请求
    resp, err := s.httpClient.Do(req)
    if err != nil {
        return nil, fmt.Errorf("请求失败: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("请求失败: status=%d", resp.StatusCode)
    }

    var userInfo UserInfo
    if err := json.NewDecoder(resp.Body).Decode(&userInfo); err != nil {
        return nil, fmt.Errorf("解析响应失败: %w", err)
    }

    return &userInfo, nil
}

// POST 请求示例
func (s *APIService) CreateUser(ctx context.Context, user *User) error {
    url := "https://api.example.com/users"

    body, err := json.Marshal(user)
    if err != nil {
        return fmt.Errorf("序列化失败: %w", err)
    }

    req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
    if err != nil {
        return fmt.Errorf("创建请求失败: %w", err)
    }

    req.Header.Set("Content-Type", "application/json")

    resp, err := s.httpClient.Do(req)
    if err != nil {
        return fmt.Errorf("请求失败: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusCreated {
        return fmt.Errorf("创建失败: status=%d", resp.StatusCode)
    }

    return nil
}
```

### Jaeger 显示效果

**Span 名称**：`HTTP GET`、`HTTP POST`

**Span 属性**：

| 属性               | 说明       | 示例                                |
| ------------------ | ---------- | ----------------------------------- |
| `http.method`      | HTTP 方法  | `GET`, `POST`                       |
| `http.url`         | 完整 URL   | `https://api.example.com/users/123` |
| `http.status_code` | 响应状态码 | `200`, `404`, `500`                 |
| `http.scheme`      | 协议       | `https`                             |
| `net.peer.name`    | 目标主机   | `api.example.com`                   |
| `net.peer.port`    | 目标端口   | `443`                               |

### Trace 示例

```txt
▼ GET /api/v1/users/:id                    [300ms]
  │
  ├─▶ SELECT                               [50ms]
  │   db.system: sqlite
  │   db.statement: SELECT * FROM users WHERE id = ?
  │
  └─▶ HTTP GET                             [200ms]
      http.method: GET
      http.url: https://api.example.com/users/123/profile
      http.status_code: 200
      net.peer.name: api.example.com
```

## 常见问题

### 问题 1: Gin 中间件没有创建 Span

**原因**：中间件注册顺序错误或没有注册

**解决方法**：

```go
r := gin.New()
// OpenTelemetry 中间件应该在最前面
r.Use(telemetry.GinMiddleware("your-service"))
r.Use(gin.Logger())
r.Use(gin.Recovery())
```

### 问题 2: Handler 中获取不到 trace context

**原因**：使用了错误的方式获取 context

**错误示例**：

```go
func Handler(c *gin.Context) {
    // 错误：使用 context.Background()
    ctx := context.Background()
    user, _ := service.Get(ctx, id)
}
```

**正确示例**：

```go
func Handler(c *gin.Context) {
    // 正确：从 gin.Context 获取
    ctx := c.Request.Context()
    user, _ := service.Get(ctx, id)
}
```

### 问题 3: HTTP Client 调用没有显示为独立节点

**原因**：没有使用 `otelhttp.NewTransport` 包装 Transport

**解决方法**：

```go
client := &http.Client{
    Transport: otelhttp.NewTransport(http.DefaultTransport), // 必须包装
    Timeout:   30 * time.Second,
}
```

### 问题 4: HTTP Client 调用链断开

**原因**：没有使用 `NewRequestWithContext` 传递 context

**错误示例**：

```go
// 错误：使用 http.NewRequest
req, _ := http.NewRequest("GET", url, nil)
resp, _ := client.Do(req)
```

**正确示例**：

```go
// 正确：使用 NewRequestWithContext
req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
resp, _ := client.Do(req)
```

### 问题 5: Span 名称不友好

**解决方法**：自定义 Span 名称格式化器

```go
Transport: otelhttp.NewTransport(
    http.DefaultTransport,
    otelhttp.WithSpanNameFormatter(func(operation string, r *http.Request) string {
        return fmt.Sprintf("%s %s", r.Method, r.URL.Path)
    }),
)
```

## 验证清单

### HTTP Server (Gin)

- [ ] 注册了 `telemetry.GinMiddleware()`
- [ ] 中间件在最前面注册
- [ ] 启动日志显示 "HTTP Server OpenTelemetry 追踪已启用"
- [ ] Handler 使用 `c.Request.Context()` 获取 context
- [ ] Jaeger 中能看到 HTTP 请求 Span
- [ ] Span 包含 `http.method`、`http.route`、`http.status_code` 属性

### HTTP Client

- [ ] 使用 `otelhttp.NewTransport` 包装 Transport
- [ ] 所有请求使用 `NewRequestWithContext` 传递 context
- [ ] Jaeger 中能看到 HTTP 调用 Span
- [ ] Span 显示为独立节点
- [ ] Span 包含 `http.url`、`http.status_code` 属性

## 参考资料

- otelhttp 文档: https://pkg.go.dev/go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp
- Gin 文档: https://gin-gonic.com/docs/
- OpenTelemetry HTTP Semantic Conventions: https://opentelemetry.io/docs/specs/semconv/http/
