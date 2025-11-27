# Kubernetes 部署指南

## 前置条件

1. Kubernetes 集群已就绪
2. kubectl 已配置
3. 镜像已推送到阿里云容器镜像服务

## 部署步骤

### 1. 创建命名空间

```bash
kubectl create namespace awecloud
```

### 2. 部署 Server

```bash
# 创建 Secret（JWT密钥）
kubectl apply -f server-secret.yaml

# 创建 ConfigMap
kubectl apply -f server-configmap.yaml

# 创建 PVC（持久化存储）
kubectl apply -f server-pvc.yaml

# 创建 Deployment
kubectl apply -f server-deployment.yaml

# 创建 Service
kubectl apply -f server-service.yaml
```

### 3. 验证 Server 部署

```bash
# 查看 Pod 状态
kubectl get pods -n awecloud

# 查看日志
kubectl logs -f -n awecloud deployment/awecloud-signaling-server

# 查看 Service
kubectl get svc -n awecloud
```

### 4. 获取 Server 访问地址

```bash
# 如果使用 NodePort
kubectl get svc -n awecloud awecloud-signaling-server

# 如果使用 Ingress
kubectl get ingress -n awecloud
```

### 5. 创建 Agent（在 Web 界面）

1. 访问 Server Web 界面
2. 登录（admin/admin123）
3. 创建 Agent，获取 agent_token

### 6. 部署 Agent

```bash
# 1. 更新 agent-secret.yaml 中的 agent-token
#    将 "your-agent-token-here" 替换为从 Server Web 界面获取的实际 token
vim agent-secret.yaml

# 2. （可选）更新 agent-deployment.yaml 中的 AGENT_NAME
#    默认为 "agent-k8s-001"，可以根据需要修改
vim agent-deployment.yaml

# 3. 创建 Secret
kubectl apply -f agent-secret.yaml

# 4. 创建 ConfigMap（包含 Server 连接配置）
kubectl apply -f agent-configmap.yaml

# 5. 创建 Deployment
kubectl apply -f agent-deployment.yaml
```

### 7. 验证 Agent 部署

```bash
# 查看 Pod 状态
kubectl get pods -n awecloud

# 查看日志
kubectl logs -f -n awecloud deployment/awecloud-signaling-agent

# 应该看到类似输出：
# Agent-Web: 连接到 Server gRPC: awecloud-signaling-server.awecloud.svc.cluster.local:8080
# Agent-Web: 注册成功，Agent ID: 1
# Agent-FRP: 连接到 Server FRP: awecloud-signaling-server.awecloud.svc.cluster.local:7000
```

## 镜像版本

当前使用的镜像版本：`v0.1.0`

- Server: `registry.cn-qingdao.aliyuncs.com/wod/awecloud-signaling-server:v0.1.0`
- Agent: `registry.cn-qingdao.aliyuncs.com/wod/awecloud-signaling-agent:v0.1.0`

## 更新镜像

```bash
# 更新 Server
kubectl set image deployment/awecloud-signaling-server \
  server=registry.cn-qingdao.aliyuncs.com/wod/awecloud-signaling-server:v0.1.0 \
  -n awecloud

# 更新 Agent
kubectl set image deployment/awecloud-signaling-agent \
  agent=registry.cn-qingdao.aliyuncs.com/wod/awecloud-signaling-agent:v0.1.0 \
  -n awecloud
```

## 端口说明

### Server

- **7000**: Server-FRP 线程（WebSocket 信令服务）
  - Agent-FRP 连接
  - Desktop-FRP 连接
  - 协调 STCP 隧道建立
  
- **8080**: Server-Web 线程（HTTP/2 统一端口）
  - Web 管理界面（HTTP）
  - RESTful API（HTTP）
  - gRPC 服务（HTTP/2）
  - Agent-Web 连接（gRPC）
  - Desktop-Web 连接（gRPC）

**说明**：Server 使用 HTTP/2 协议，在同一端口（8080）上同时支持 HTTP 和 gRPC，服务器根据 Content-Type 自动路由。

### Agent

- 无需暴露端口，主动连接到 Server
  - Agent-Web 线程 → Server-Web（gRPC，端口 8080）
  - Agent-FRP 线程 → Server-FRP（WebSocket，端口 7000）

## 配置说明

### Server 配置

通过 ConfigMap `awecloud-signaling-server` 配置：

- `server.toml`: Server 配置文件

通过 Secret `awecloud-signaling-server` 配置：

- `jwt-secret`: JWT 密钥

### Agent 配置

**通过环境变量配置**（推荐）：

在 `agent-deployment.yaml` 中配置：
- `AGENT_NAME`: Agent 名称（直接在 Deployment 中设置）
- `AGENT_TOKEN`: Agent 认证 token（从 Secret 获取）

**通过 ConfigMap 配置**：

`awecloud-signaling-agent` 包含基础配置：
- Server 连接地址和端口
- 日志配置

**通过 Secret 配置**：

`awecloud-signaling-agent` 包含敏感信息：
- `agent-token`: Agent 认证 token（从 Server Web 界面创建 Agent 时获取）

**配置优先级**：环境变量 > 配置文件

## 故障排查

### Server 无法启动

```bash
# 查看日志
kubectl logs -n awecloud deployment/awecloud-signaling-server

# 检查 ConfigMap
kubectl get cm -n awecloud awecloud-signaling-server -o yaml

# 检查 Secret
kubectl get secret -n awecloud awecloud-signaling-server -o yaml
```

### Agent 无法连接

```bash
# 查看日志
kubectl logs -n awecloud deployment/awecloud-signaling-agent

# 检查 Server Service
kubectl get svc -n awecloud awecloud-signaling-server

# 测试连接
kubectl run -it --rm debug --image=alpine --restart=Never -n awecloud -- sh
# 在容器内执行：
# apk add curl
# curl http://awecloud-signaling-server:8080/health
```

### 数据持久化

Server 使用 PVC 持久化数据：

```bash
# 查看 PVC
kubectl get pvc -n awecloud

# 查看 PV
kubectl get pv
```

## 清理

```bash
# 删除所有资源
kubectl delete namespace awecloud
```

## 生产环境建议

1. **启用 TLS**：配置 TLS 证书
2. **使用 Ingress**：通过 Ingress 暴露服务
3. **配置资源限制**：根据实际负载调整 resources
4. **配置 HPA**：自动扩缩容
5. **配置监控**：Prometheus + Grafana
6. **配置日志**：ELK 或 Loki
7. **配置备份**：定期备份数据库
