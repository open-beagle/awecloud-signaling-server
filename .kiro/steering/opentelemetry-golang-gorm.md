# GORM OpenTelemetry 集成规范

## 概述

GORM 是 Go 的 ORM 库，通过 `otelgorm` 插件可以自动追踪所有数据库操作，在 Jaeger 中显示为独立的数据库节点。

## 依赖包

```go
require (
    gorm.io/gorm v1.25.0
    github.com/uptrace/opentelemetry-go-extra/otelgorm v0.3.2
)
```

## 集成位置

数据库初始化函数，通常在 `internal/server/db/db.go` 或类似位置。

## 集成方式

### 初始化代码

```go
import (
    "fmt"

    "gorm.io/driver/sqlite"
    "gorm.io/gorm"

    "github.com/uptrace/opentelemetry-go-extra/otelgorm"
)

func InitDB(cfg *config.DatabaseConfig) (*gorm.DB, error) {
    // 1. 打开数据库连接
    db, err := gorm.Open(sqlite.Open(cfg.DSN), &gorm.Config{
        Logger: newGormLogger(),
    })
    if err != nil {
        return nil, fmt.Errorf("打开数据库失败: %w", err)
    }

    // 2. 注册 OpenTelemetry 插件
    if err := db.Use(otelgorm.NewPlugin()); err != nil {
        return nil, fmt.Errorf("注册 GORM OpenTelemetry 插件失败: %w", err)
    }

    logger.Info("GORM OpenTelemetry 追踪已启用")
    return db, nil
}
```

### 自定义配置（可选）

```go
// 使用自定义配置
if err := db.Use(otelgorm.NewPlugin(
    otelgorm.WithDBName("my-database"),           // 自定义数据库名称
    otelgorm.WithAttributes(                      // 添加自定义属性
        attribute.String("db.instance", "prod"),
    ),
)); err != nil {
    return nil, err
}
```

## 使用方式

### 基本查询

```go
// Service 层
func (s *UserService) GetByID(ctx context.Context, id uint) (*User, error) {
    var user User
    // 必须使用 WithContext 传递 context
    err := s.db.WithContext(ctx).First(&user, id).Error
    return &user, err
}

func (s *UserService) List(ctx context.Context, limit int) ([]User, error) {
    var users []User
    err := s.db.WithContext(ctx).Limit(limit).Find(&users).Error
    return users, err
}
```

### 创建和更新

```go
func (s *UserService) Create(ctx context.Context, user *User) error {
    return s.db.WithContext(ctx).Create(user).Error
}

func (s *UserService) Update(ctx context.Context, user *User) error {
    return s.db.WithContext(ctx).Save(user).Error
}
```

### 事务操作

```go
func (s *UserService) Transfer(ctx context.Context, fromID, toID uint, amount int) error {
    return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
        // 事务内的操作会自动继承 context
        if err := tx.Model(&User{}).Where("id = ?", fromID).
            Update("balance", gorm.Expr("balance - ?", amount)).Error; err != nil {
            return err
        }

        if err := tx.Model(&User{}).Where("id = ?", toID).
            Update("balance", gorm.Expr("balance + ?", amount)).Error; err != nil {
            return err
        }

        return nil
    })
}
```

### 关联查询

```go
func (s *UserService) GetWithOrders(ctx context.Context, id uint) (*User, error) {
    var user User
    err := s.db.WithContext(ctx).
        Preload("Orders").
        First(&user, id).Error
    return &user, err
}
```

## Jaeger 显示效果

### Span 名称

- `SELECT` - 查询操作
- `INSERT` - 插入操作
- `UPDATE` - 更新操作
- `DELETE` - 删除操作

### Span 属性

| 属性               | 说明       | 示例                                   |
| ------------------ | ---------- | -------------------------------------- |
| `db.system`        | 数据库类型 | `sqlite`, `mysql`, `postgres`          |
| `db.name`          | 数据库名称 | `my-database`                          |
| `db.statement`     | SQL 语句   | `SELECT * FROM users WHERE id = ?`     |
| `db.operation`     | 操作类型   | `SELECT`, `INSERT`, `UPDATE`, `DELETE` |
| `db.sql.table`     | 表名       | `users`                                |
| `db.rows_affected` | 影响的行数 | `1`                                    |

### Trace 示例

```txt
▼ GET /api/v1/users/:id                    [200ms]
  │
  ├─▶ SELECT                               [50ms]
  │   db.system: sqlite
  │   db.statement: SELECT * FROM users WHERE id = ?
  │   db.sql.table: users
  │   db.rows_affected: 1
  │
  └─▶ SELECT                               [30ms]
      db.system: sqlite
      db.statement: SELECT * FROM orders WHERE user_id = ?
      db.sql.table: orders
      db.rows_affected: 5
```

## 常见问题

### 问题 1: 没有看到 SQL Span

**原因**：忘记使用 `WithContext(ctx)`

**错误示例**：

```go
// 错误：直接使用 db，没有传递 context
err := s.db.First(&user, id).Error
```

**正确示例**：

```go
// 正确：使用 WithContext 传递 context
err := s.db.WithContext(ctx).First(&user, id).Error
```

### 问题 2: Span 中没有 SQL 语句

**原因**：GORM 日志级别设置为 Silent

**解决方法**：

```go
db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
    Logger: logger.Default.LogMode(logger.Info), // 设置为 Info 级别
})
```

### 问题 3: 事务中的 Span 没有关联

**原因**：事务回调函数中没有使用传入的 `tx` 参数

**错误示例**：

```go
s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
    // 错误：使用了 s.db 而不是 tx
    return s.db.Create(&user).Error
})
```

**正确示例**：

```go
s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
    // 正确：使用 tx
    return tx.Create(&user).Error
})
```

### 问题 4: 性能影响

**说明**：otelgorm 插件对性能影响很小（< 1%），但在高并发场景下可以考虑：

1. 使用采样率降低追踪数据量
2. 在 `telemetry.Init()` 中配置采样策略
3. 对于批量操作，考虑使用 `db.Session(&gorm.Session{SkipHooks: true})` 跳过钩子

## 验证清单

- [ ] 数据库初始化时注册了 `otelgorm.NewPlugin()`
- [ ] 启动日志显示 "GORM OpenTelemetry 追踪已启用"
- [ ] 所有查询都使用 `db.WithContext(ctx)`
- [ ] Jaeger 中能看到 SQL Span
- [ ] Span 包含 `db.system`、`db.statement` 等属性
- [ ] 事务操作的 Span 正确嵌套

## 参考资料

- otelgorm 文档: https://github.com/uptrace/opentelemetry-go-extra/tree/main/otelgorm
- GORM 文档: https://gorm.io/docs/
- OpenTelemetry Database Semantic Conventions: https://opentelemetry.io/docs/specs/semconv/database/
