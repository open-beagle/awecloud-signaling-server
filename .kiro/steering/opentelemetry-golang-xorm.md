# XORM OpenTelemetry 集成规范

## 概述

XORM 是另一个流行的 Go ORM 库。由于没有官方的 OpenTelemetry 插件，需要通过自定义 Hook 实现追踪。

## 依赖包

```go
require (
    xorm.io/xorm v1.3.0
    go.opentelemetry.io/otel v1.39.0
    go.opentelemetry.io/otel/trace v1.39.0
)
```

## 集成位置

数据库初始化函数，通常在 `internal/server/db/db.go` 或类似位置。

## 集成方式

### 自定义 Hook 实现

创建 `internal/common/telemetry/xorm_hook.go`：

```go
import (
    "context"
    "fmt"
    "time"

    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/codes"
    "go.opentelemetry.io/otel/trace"
    semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
    "xorm.io/xorm/contexts"
)

type XORMHook struct {
    tracer trace.Tracer
}

func NewXORMHook(serviceName string) *XORMHook {
    return &XORMHook{
        tracer: otel.Tracer(serviceName),
    }
}

// BeforeProcess 在 SQL 执行前调用
func (h *XORMHook) BeforeProcess(c *contexts.ContextHook) (context.Context, error) {
    ctx := c.Ctx
    if ctx == nil {
        ctx = context.Background()
    }

    // 创建 span
    spanName := c.SQL
    if len(spanName) > 100 {
        spanName = spanName[:100] + "..."
    }

    ctx, span := h.tracer.Start(ctx, spanName,
        trace.WithSpanKind(trace.SpanKindClient),
        trace.WithAttributes(
            semconv.DBSystemKey.String("xorm"),
            semconv.DBStatementKey.String(c.SQL),
        ),
    )

    // 将 span 存储到 context 中
    c.Ctx = ctx
    return ctx, nil
}

// AfterProcess 在 SQL 执行后调用
func (h *XORMHook) AfterProcess(c *contexts.ContextHook) error {
    span := trace.SpanFromContext(c.Ctx)
    if !span.IsRecording() {
        return nil
    }

    // 记录执行时间
    if c.ExecuteTime > 0 {
        span.SetAttributes(
            attribute.Int64("db.execution_time_ms", c.ExecuteTime.Milliseconds()),
        )
    }

    // 记录错误
    if c.Err != nil {
        span.RecordError(c.Err)
        span.SetStatus(codes.Error, c.Err.Error())
    } else {
        span.SetStatus(codes.Ok, "")
    }

    span.End()
    return nil
}
```

### 注册 Hook

```go
import (
    "xorm.io/xorm"
    _ "github.com/mattn/go-sqlite3"
)

func InitDB(cfg *config.DatabaseConfig) (*xorm.Engine, error) {
    // 1. 创建数据库引擎
    engine, err := xorm.NewEngine("sqlite3", cfg.DSN)
    if err != nil {
        return nil, fmt.Errorf("创建数据库引擎失败: %w", err)
    }

    // 2. 注册 OpenTelemetry Hook
    hook := telemetry.NewXORMHook("your-service")
    engine.AddHook(hook)

    logger.Info("XORM OpenTelemetry 追踪已启用")
    return engine, nil
}
```

## 使用方式

### 基本查询

```go
type UserRepository struct {
    engine *xorm.Engine
}

func NewUserRepository(engine *xorm.Engine) *UserRepository {
    return &UserRepository{engine: engine}
}

// 查询单条记录
func (r *UserRepository) GetByID(ctx context.Context, id int64) (*User, error) {
    var user User
    // 使用 Context 方法传递 context
    has, err := r.engine.Context(ctx).ID(id).Get(&user)
    if err != nil {
        return nil, err
    }
    if !has {
        return nil, ErrNotFound
    }
    return &user, nil
}

// 查询多条记录
func (r *UserRepository) List(ctx context.Context, limit int) ([]User, error) {
    var users []User
    err := r.engine.Context(ctx).Limit(limit).Find(&users)
    return users, err
}

// 条件查询
func (r *UserRepository) FindByEmail(ctx context.Context, email string) (*User, error) {
    var user User
    has, err := r.engine.Context(ctx).Where("email = ?", email).Get(&user)
    if err != nil {
        return nil, err
    }
    if !has {
        return nil, ErrNotFound
    }
    return &user, nil
}
```

### 创建和更新

```go
// 插入记录
func (r *UserRepository) Create(ctx context.Context, user *User) error {
    _, err := r.engine.Context(ctx).Insert(user)
    return err
}

// 更新记录
func (r *UserRepository) Update(ctx context.Context, user *User) error {
    _, err := r.engine.Context(ctx).ID(user.ID).Update(user)
    return err
}

// 删除记录
func (r *UserRepository) Delete(ctx context.Context, id int64) error {
    _, err := r.engine.Context(ctx).ID(id).Delete(&User{})
    return err
}
```

### 事务操作

```go
func (r *UserRepository) Transfer(ctx context.Context, fromID, toID int64, amount int) error {
    // 开启事务
    session := r.engine.Context(ctx).NewSession()
    defer session.Close()

    if err := session.Begin(); err != nil {
        return err
    }

    // 扣减余额
    _, err := session.Exec("UPDATE users SET balance = balance - ? WHERE id = ?", amount, fromID)
    if err != nil {
        session.Rollback()
        return err
    }

    // 增加余额
    _, err = session.Exec("UPDATE users SET balance = balance + ? WHERE id = ?", amount, toID)
    if err != nil {
        session.Rollback()
        return err
    }

    return session.Commit()
}
```

### 复杂查询

```go
// JOIN 查询
func (r *UserRepository) GetWithOrders(ctx context.Context, userID int64) (*User, []Order, error) {
    var user User
    has, err := r.engine.Context(ctx).ID(userID).Get(&user)
    if err != nil {
        return nil, nil, err
    }
    if !has {
        return nil, nil, ErrNotFound
    }

    var orders []Order
    err = r.engine.Context(ctx).Where("user_id = ?", userID).Find(&orders)
    return &user, orders, err
}

// 聚合查询
func (r *UserRepository) Count(ctx context.Context) (int64, error) {
    return r.engine.Context(ctx).Count(&User{})
}
```

## Jaeger 显示效果

### Span 名称

Span 名称为 SQL 语句的前 100 个字符（如果超过则截断）。

示例:

- `SELECT * FROM users WHERE id = ?`
- `INSERT INTO users (name, email) VALUES (?, ?)`
- `UPDATE users SET balance = balance - ? WHERE id = ?`

### Span 属性

| 属性                   | 说明             | 示例                               |
| ---------------------- | ---------------- | ---------------------------------- |
| `db.system`            | 数据库类型       | `xorm`                             |
| `db.statement`         | SQL 语句         | `SELECT * FROM users WHERE id = ?` |
| `db.execution_time_ms` | 执行时间（毫秒） | `50`                               |

### Trace 示例

```txt
▼ GET /api/v1/users/:id                    [200ms]
  │
  ├─▶ SELECT * FROM users WHERE id = ?     [50ms]
  │   db.system: xorm
  │   db.statement: SELECT * FROM users WHERE id = ?
  │   db.execution_time_ms: 50
  │
  └─▶ SELECT * FROM orders WHERE user_id = ? [30ms]
      db.system: xorm
      db.statement: SELECT * FROM orders WHERE user_id = ?
      db.execution_time_ms: 30
```

## 常见问题

### 问题 1: 没有看到 SQL Span

**原因**：忘记使用 `Context(ctx)` 方法

**错误示例**：

```go
// 错误：直接使用 engine，没有传递 context
has, err := r.engine.ID(id).Get(&user)
```

**正确示例**：

```go
// 正确：使用 Context 传递 context
has, err := r.engine.Context(ctx).ID(id).Get(&user)
```

### 问题 2: Hook 没有被调用

**原因**：Hook 注册时机错误或没有注册

**解决方法**：

```go
engine, err := xorm.NewEngine("sqlite3", dsn)
// 必须在创建引擎后立即注册
hook := telemetry.NewXORMHook("your-service")
engine.AddHook(hook)
```

### 问题 3: 事务中的 Span 没有关联

**原因**：Session 创建时没有传递 context

**错误示例**：

```go
// 错误：没有传递 context
session := r.engine.NewSession()
```

**正确示例**：

```go
// 正确：使用 Context 创建 Session
session := r.engine.Context(ctx).NewSession()
```

### 问题 4: Span 名称过长

**说明**：默认实现会截断超过 100 字符的 SQL 语句。如需调整，修改 `BeforeProcess` 中的截断逻辑：

```go
spanName := c.SQL
if len(spanName) > 200 {  // 调整为 200
    spanName = spanName[:200] + "..."
}
```

### 问题 5: 性能影响

**说明**：自定义 Hook 对性能影响很小（< 1%），但在高并发场景下可以考虑：

1. 使用采样率降低追踪数据量
2. 在 Hook 中添加条件判断，跳过某些高频查询
3. 优化 SQL 语句本身

## 验证清单

- [ ] 数据库引擎初始化时注册了 `XORMHook`
- [ ] 启动日志显示 "XORM OpenTelemetry 追踪已启用"
- [ ] 所有查询都使用 `engine.Context(ctx)`
- [ ] Jaeger 中能看到 SQL Span
- [ ] Span 包含 `db.system`、`db.statement` 等属性
- [ ] 事务操作的 Span 正确嵌套

## 与 GORM 对比

| 特性         | GORM                       | XORM                  |
| ------------ | -------------------------- | --------------------- |
| 官方插件     | ✅ otelgorm                | ❌ 需自定义 Hook      |
| 集成难度     | 简单                       | 中等                  |
| Context 传递 | `db.WithContext(ctx)`      | `engine.Context(ctx)` |
| Span 属性    | 更丰富（表名、操作类型等） | 基础（SQL 语句）      |
| 性能影响     | < 1%                       | < 1%                  |

## 参考资料

- XORM 文档: https://xorm.io/docs/
- XORM Hook 文档: https://xorm.io/docs/chapter-11/readme/
- OpenTelemetry Database Semantic Conventions: https://opentelemetry.io/docs/specs/semconv/database/
