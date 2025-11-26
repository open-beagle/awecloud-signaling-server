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
# 更新 agent-secret.yaml 中的 agent-token
# 将 "your-agent-token-here" 替换为实际的 token

# 创建 Secret
kubectl apply -f agent-secret.yaml

# 创建 ConfigMap
kubectl apply -f agent-configmap.yaml

# 创建 Deployment
kubectl apply -f agent-deployment.yaml
```

### 7. 验证 Agent 部署

```bash
# 查看 Pod 状态
kubectl get pods -n awecloud

# 查看日志
kubectl logs -f -n awecloud deployment/awecloud-signaling-agent

# 应该看到类似输出：
# 连接到Server gRPC: awecloud-signaling-server.awecloud.svc.cluster.local:8081
# 注册成功，Agent ID: 1
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

- **7000**: FRP 信令服务（WebSocket）
- **8080**: Web 管理界面和 RESTful API
- **8081**: gRPC 服务（Agent 连接）

### Agent

- 无需暴露端口，主动连接到 Server

## 配置说明

### Server 配置

通过 ConfigMap `awecloud-server-config` 配置：

- `server.toml`: Server 配置文件

通过 Secret `awecloud-secrets` 配置：

- `jwt-secret`: JWT 密钥

### Agent 配置

通过 ConfigMap `awecloud-agent-config` 配置：

- `agent-name`: Agent 名称
- `server-address`: Server 地址
- `server-port`: Server 端口

通过 Secret `awecloud-agent-secret` 配置：

- `agent-token`: Agent 认证 token

## 故障排查

### Server 无法启动

```bash
# 查看日志
kubectl logs -n awecloud deployment/awecloud-signaling-server

# 检查 ConfigMap
kubectl get cm -n awecloud awecloud-server-config -o yaml

# 检查 Secret
kubectl get secret -n awecloud awecloud-secrets -o yaml
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
