# 配置文件说明

## 配置文件分类

### 1. 运行时配置（每次部署可能不同）

#### `server.toml.example` / `server.toml`

- **用途**: Server 运行时配置
- **包含**: 数据库连接、API 地址、密钥等
- **使用**:
  - 开发: 复制 `server.toml.example` 为 `server.toml` 并修改
  - 生产: 通过环境变量或 ConfigMap 覆盖

#### `agent.toml.example` / `agent.toml`

- **用途**: Agent 运行时配置
- **包含**: Server 连接地址、认证信息等
- **使用**: 同上

### 2. 静态配置（设计时确定，很少改动）

#### `network.toml`

- **用途**: Headscale 网段规划
- **包含**: Agent/Desktop/Server 的 IP 网段分配
- **特点**:
  - 设计时确定，编译时 Copy 到镜像
  - 启动时读取并写入数据库（如果数据库为空）
  - 运行时可在 Web 界面修改，修改后存储在数据库
  - 数据库配置优先级高于此文件

## 配置优先级

### 运行时配置优先级

```
环境变量 > server.toml > 默认值
```

### 网段配置优先级

```
数据库 > network.toml > 硬编码默认值
```

## Dockerfile 使用示例

```dockerfile
# 复制静态配置（设计时确定）
COPY config/network.toml /app/config/

# 复制运行时配置模板（可选）
COPY config/server.toml.example /app/config/

# 运行时通过环境变量或 ConfigMap 覆盖
ENV HEADSCALE_URL=http://headscale:8080
ENV HEADSCALE_API_KEY=your-api-key
```

## Kubernetes 使用示例

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: server-config
data:
  # 运行时配置通过 ConfigMap
  server.toml: |
    [web]
    listen_addr = "0.0.0.0"
    listen_port = 8080

    # 遗留兼容配置段名称，控制面始终使用 Headscale
    [tailscale]
    headscale_url = "http://headscale:8080"

  # 网段配置已在镜像中，无需 ConfigMap
  # network.toml 在镜像构建时已 COPY 进去
```

## 配置文件加载流程

### Server 启动时

1. **加载运行时配置** (`server.toml`)

   - 读取配置文件
   - 环境变量覆盖
   - 连接数据库

2. **加载网段配置** (`network.toml`)

   - 检查数据库是否有配置
   - 如果没有，读取 `network.toml`
   - 写入数据库作为默认值

3. **后续使用**
   - 所有网段配置从数据库读取
   - Web 界面可动态修改
   - 修改后立即生效

## 配置文件对比

| 配置文件       | 类型   | 修改频率 | 存储位置      | 优先级          |
| -------------- | ------ | -------- | ------------- | --------------- |
| `server.toml`  | 运行时 | 每次部署 | 文件/环境变量 | 环境变量 > 文件 |
| `agent.toml`   | 运行时 | 每次部署 | 文件/环境变量 | 环境变量 > 文件 |
| `network.toml` | 静态   | 很少     | 镜像 + 数据库 | 数据库 > 文件   |
