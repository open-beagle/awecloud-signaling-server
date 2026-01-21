# Redis OpenTelemetry 集成规范

## 概述

Redis 客户端通过 `redisotel` 插件可以自动追踪所有 Redis 操作，在 Jaeger 中显示为独立的缓存节点。

## 依赖包

```go
require (
    github.com/redis/go-redis/v9 v9.7.0
    github.com/redis/go-redis/extra/redisotel/v9 v9.7.0
)
```

## 集成位置

Redis 客户端初始化处，通常在 `internal/server/cache/` 或类似位置。

## 集成方式

### 单机模式

```go
import (
    "context"
    "time"

    "github.com/redis/go-redis/v9"
    "github.com/redis/go-redis/extra/redisotel/v9"
)

func NewRedisClient(addr string) *redis.Client {
    rdb := redis.NewClient(&redis.Options{
        Addr:         addr,
        Password:     "",
        DB:           0,
        DialTimeout:  5 * time.Second,
        ReadTimeout:  3 * time.Second,
        WriteTimeout: 3 * time.Second,
        PoolSize:     10,
    })

    // 启用 OpenTelemetry 追踪
    if err := redisotel.InstrumentTracing(rdb); err != nil {
        logger.Errorf("Redis OpenTelemetry 追踪启用失败: %v", err)
    } else {
        logger.Info("Redis OpenTelemetry 追踪已启用")
    }

    // 可选：启用 Metrics
    if err := redisotel.InstrumentMetrics(rdb); err != nil {
        logger.Errorf("Redis OpenTelemetry Metrics 启用失败: %v", err)
    }

    return rdb
}
```

### 集群模式

```go
func NewRedisClusterClient(addrs []string) *redis.ClusterClient {
    rdb := redis.NewClusterClient(&redis.ClusterOptions{
        Addrs:        addrs,
        DialTimeout:  5 * time.Second,
        ReadTimeout:  3 * time.Second,
        WriteTimeout: 3 * time.Second,
        PoolSize:     10,
    })

    // 启用 OpenTelemetry 追踪
    if err := redisotel.InstrumentTracing(rdb); err != nil {
        logger.Errorf("Redis Cluster OpenTelemetry 追踪启用失败: %v", err)
    } else {
        logger.Info("Redis Cluster OpenTelemetry 追踪已启用")
    }

    return rdb
}
```

### 自定义配置（可选）

```go
import (
    "go.opentelemetry.io/otel/attribute"
)

// 使用自定义配置
if err := redisotel.InstrumentTracing(
    rdb,
    redisotel.WithAttributes(
        attribute.String("db.instance", "cache-prod"),
        attribute.String("db.redis.database_index", "0"),
    ),
); err != nil {
    logger.Errorf("Redis OpenTelemetry 追踪启用失败: %v", err)
}
```

## 使用方式

### 基本操作

```go
type CacheService struct {
    redis *redis.Client
}

func NewCacheService(redis *redis.Client) *CacheService {
    return &CacheService{redis: redis}
}

// GET 操作
func (s *CacheService) Get(ctx context.Context, key string) (string, error) {
    // 传递 context，自动创建子 span
    val, err := s.redis.Get(ctx, key).Result()
    if err == redis.Nil {
        return "", ErrNotFound
    }
    return val, err
}

// SET 操作
func (s *CacheService) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
    return s.redis.Set(ctx, key, value, expiration).Err()
}

// DEL 操作
func (s *CacheService) Delete(ctx context.Context, keys ...string) error {
    return s.redis.Del(ctx, keys...).Err()
}

// EXISTS 操作
func (s *CacheService) Exists(ctx context.Context, keys ...string) (int64, error) {
    return s.redis.Exists(ctx, keys...).Result()
}
```

### 复杂操作

```go
// HSET/HGET 操作
func (s *CacheService) SetHash(ctx context.Context, key string, values map[string]interface{}) error {
    return s.redis.HSet(ctx, key, values).Err()
}

func (s *CacheService) GetHash(ctx context.Context, key, field string) (string, error) {
    return s.redis.HGet(ctx, key, field).Result()
}

// LIST 操作
func (s *CacheService) PushList(ctx context.Context, key string, values ...interface{}) error {
    return s.redis.RPush(ctx, key, values...).Err()
}

func (s *CacheService) PopList(ctx context.Context, key string) (string, error) {
    return s.redis.LPop(ctx, key).Result()
}

// SET 操作
func (s *CacheService) AddToSet(ctx context.Context, key string, members ...interface{}) error {
    return s.redis.SAdd(ctx, key, members...).Err()
}

func (s *CacheService) GetSetMembers(ctx context.Context, key string) ([]string, error) {
    return s.redis.SMembers(ctx, key).Result()
}
```

### Pipeline 操作

```go
func (s *CacheService) BatchSet(ctx context.Context, items map[string]interface{}) error {
    pipe := s.redis.Pipeline()

    for key, value := range items {
        pipe.Set(ctx, key, value, time.Hour)
    }

    _, err := pipe.Exec(ctx)
    return err
}
```

### 事务操作

```go
func (s *CacheService) IncrementWithLimit(ctx context.Context, key string, limit int64) error {
    return s.redis.Watch(ctx, func(tx *redis.Tx) error {
        val, err := tx.Get(ctx, key).Int64()
        if err != nil && err != redis.Nil {
            return err
        }

        if val >= limit {
            return ErrLimitExceeded
        }

        _, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
            pipe.Incr(ctx, key)
            return nil
        })
        return err
    }, key)
}
```

## Jaeger 显示效果

### Span 名称

- `redis.get` - GET 操作
- `redis.set` - SET 操作
- `redis.del` - DEL 操作
- `redis.hget` - HGET 操作
- `redis.hset` - HSET 操作
- `redis.lpush` - LPUSH 操作
- `redis.rpush` - RPUSH 操作
- `redis.sadd` - SADD 操作

### Span 属性

| 属性                      | 说明         | 示例                |
| ------------------------- | ------------ | ------------------- |
| `db.system`               | 数据库类型   | `redis`             |
| `db.statement`            | Redis 命令   | `GET user:123`      |
| `db.operation`            | 操作类型     | `get`, `set`, `del` |
| `db.redis.database_index` | 数据库索引   | `0`                 |
| `net.peer.name`           | Redis 服务器 | `localhost`         |
| `net.peer.port`           | Redis 端口   | `6379`              |

### Trace 示例

```txt
▼ GET /api/v1/users/:id                    [200ms]
  │
  ├─▶ redis.get                            [5ms]
  │   db.system: redis
  │   db.statement: GET user:cache:123
  │   db.operation: get
  │   net.peer.name: localhost
  │   net.peer.port: 6379
  │
  ├─▶ SELECT                               [50ms]
  │   db.system: sqlite
  │   db.statement: SELECT * FROM users WHERE id = ?
  │
  └─▶ redis.set                            [3ms]
      db.system: redis
      db.statement: SET user:cache:123 {...}
      db.operation: set
```

## 常见问题

### 问题 1: 没有看到 Redis Span

**原因**：忘记调用 `redisotel.InstrumentTracing()`

**解决方法**：

```go
rdb := redis.NewClient(&redis.Options{...})
// 必须调用
if err := redisotel.InstrumentTracing(rdb); err != nil {
    logger.Errorf("启用追踪失败: %v", err)
}
```

### 问题 2: Redis 操作没有传递 context

**原因**：使用了 `context.Background()` 或没有传递 context

**错误示例**：

```go
// 错误：使用 Background
val, err := s.redis.Get(context.Background(), key).Result()
```

**正确示例**：

```go
// 正确：传递接收到的 ctx
val, err := s.redis.Get(ctx, key).Result()
```

### 问题 3: Pipeline 操作的 Span 没有关联

**原因**：Pipeline 中没有传递 context

**错误示例**：

```go
pipe := s.redis.Pipeline()
pipe.Set(context.Background(), key, value, 0) // 错误
```

**正确示例**：

```go
pipe := s.redis.Pipeline()
pipe.Set(ctx, key, value, 0) // 正确
```

### 问题 4: 连接失败但没有追踪信息

**原因**：连接错误发生在追踪启用之前

**解决方法**：

```go
rdb := redis.NewClient(&redis.Options{...})

// 先启用追踪
if err := redisotel.InstrumentTracing(rdb); err != nil {
    return nil, err
}

// 再测试连接
if err := rdb.Ping(context.Background()).Err(); err != nil {
    return nil, fmt.Errorf("Redis 连接失败: %w", err)
}
```

### 问题 5: Span 数量过多影响性能

**说明**：redisotel 对性能影响很小，但在极高并发场景下可以考虑：

1. 使用 Pipeline 批量操作减少 Span 数量
2. 配置采样率
3. 对于健康检查等高频操作，使用独立的 Redis 客户端（不启用追踪）

```go
// 健康检查专用客户端（不追踪）
healthCheckRedis := redis.NewClient(&redis.Options{...})
// 不调用 InstrumentTracing

// 业务操作客户端（追踪）
businessRedis := redis.NewClient(&redis.Options{...})
redisotel.InstrumentTracing(businessRedis)
```

## 验证清单

- [ ] Redis 客户端初始化时调用了 `redisotel.InstrumentTracing()`
- [ ] 启动日志显示 "Redis OpenTelemetry 追踪已启用"
- [ ] 所有 Redis 操作都传递 context
- [ ] Jaeger 中能看到 Redis Span
- [ ] Span 包含 `db.system: redis`、`db.statement` 等属性
- [ ] Pipeline 和事务操作的 Span 正确嵌套
- [ ] Redis 节点在 Service Map 中显示

## 性能建议

1. **连接池配置**：合理设置 `PoolSize`，避免连接数过多
2. **超时设置**：设置合理的 `DialTimeout`、`ReadTimeout`、`WriteTimeout`
3. **Pipeline 使用**：批量操作使用 Pipeline 减少网络往返
4. **键命名规范**：使用有意义的键名，便于在 Jaeger 中识别
5. **过期时间**：合理设置过期时间，避免内存泄漏

## 参考资料

- redisotel 文档: https://github.com/redis/go-redis/tree/master/extra/redisotel
- go-redis 文档: https://redis.uptrace.dev/
- OpenTelemetry Database Semantic Conventions: https://opentelemetry.io/docs/specs/semconv/database/
- Redis 命令参考: https://redis.io/commands/
