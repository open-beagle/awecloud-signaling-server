# Docker 部署指南

## 镜像

| 组件   | 镜像地址                                                         |
| ------ | ---------------------------------------------------------------- |
| Server | `registry.cn-qingdao.aliyuncs.com/wod/awecloud-signaling-server` |
| Agent  | `registry.cn-qingdao.aliyuncs.com/wod/awecloud-signaling-agent`  |

## Agent 部署

### 快速启动

```bash
docker run -d \
  --name signaling-agent \
  --restart unless-stopped \
  -e AGENT_NAME="my-agent" \
  -e AGENT_TOKEN="your-agent-token" \
  -e AGENT_ADDRESS="https://signaling.example.com" \
  registry.cn-qingdao.aliyuncs.com/wod/awecloud-signaling-agent:v0.1.2
```

### 环境变量

| 变量          | 必需 | 说明                                     | 示例                            |
| ------------- | ---- | ---------------------------------------- | ------------------------------- |
| AGENT_NAME    | 是   | Agent 名称，用于在 Server 中标识         | `my-agent`                      |
| AGENT_TOKEN   | 是   | Agent 认证 Token，从 Server 管理界面获取 | `abc123...`                     |
| AGENT_ADDRESS | 是   | Server 地址                              | `https://signaling.example.com` |

### 带日志持久化

```bash
docker run -d \
  --name signaling-agent \
  --restart unless-stopped \
  -e AGENT_NAME="my-agent" \
  -e AGENT_TOKEN="your-agent-token" \
  -e AGENT_ADDRESS="https://signaling.example.com" \
  -v /var/log/signaling-agent:/app/logs \
  registry.cn-qingdao.aliyuncs.com/wod/awecloud-signaling-agent:v0.1.2
```

### 使用 host 网络（推荐用于需要暴露服务的场景）

```bash
docker run -d \
  --name signaling-agent \
  --restart unless-stopped \
  --network host \
  -e AGENT_NAME="my-agent" \
  -e AGENT_TOKEN="your-agent-token" \
  -e AGENT_ADDRESS="https://signaling.example.com" \
  registry.cn-qingdao.aliyuncs.com/wod/awecloud-signaling-agent:v0.1.2
```

## Server 部署

### 快速启动

```bash
docker run -d \
  --name signaling-server \
  --restart unless-stopped \
  -p 8080:8080 \
  -p 7000:7000 \
  -v signaling-data:/app/data \
  registry.cn-qingdao.aliyuncs.com/wod/awecloud-signaling-server:v0.1.2
```

### 端口说明

| 端口 | 说明               |
| ---- | ------------------ |
| 8080 | Web 管理界面和 API |
| 7000 | 隧道服务端口       |

## Docker Compose

```yaml
version: "3.8"

services:
  server:
    image: registry.cn-qingdao.aliyuncs.com/wod/awecloud-signaling-server:v0.1.2
    restart: unless-stopped
    ports:
      - "8080:8080"
      - "7000:7000"
    volumes:
      - signaling-data:/app/data

  agent:
    image: registry.cn-qingdao.aliyuncs.com/wod/awecloud-signaling-agent:v0.1.2
    restart: unless-stopped
    environment:
      - AGENT_NAME=my-agent
      - AGENT_TOKEN=your-agent-token
      - AGENT_ADDRESS=http://server:8080
    depends_on:
      - server

volumes:
  signaling-data:
```

## 健康检查

Agent 提供健康检查端点：

- `/health` - 存活性检查
- `/health/ready` - 就绪性检查（检查与 Server 的连接状态）

```bash
# 检查 Agent 健康状态
curl http://localhost:8090/health
```

## 常见问题

### Agent 无法连接 Server

1. 检查 `AGENT_ADDRESS` 是否正确
2. 检查 `AGENT_TOKEN` 是否有效
3. 检查网络连通性

### 查看日志

```bash
docker logs -f signaling-agent
```
