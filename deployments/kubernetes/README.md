# Kubernetes 部署文档

## 架构说明

- Server使用WebSocket协议（非加密）
- TLS加密由Traefik网关统一处理
- 客户端通过WSS连接到Traefik，Traefik转发到Server的WebSocket端口

## 部署步骤

### 1. 创建命名空间

```bash
kubectl create namespace awecloud
```

### 2. 应用配置

```bash
# 按顺序应用配置
kubectl apply -f server-secret.yaml
kubectl apply -f server-configmap.yaml
kubectl apply -f server-pvc.yaml
kubectl apply -f server-deployment.yaml
kubectl apply -f server-service.yaml
```

### 3. 查看状态

```bash
# 查看Pod状态
kubectl get pods -n awecloud

# 查看Service
kubectl get svc -n awecloud

# 查看日志
kubectl logs -f deployment/awecloud-signaling-server -n awecloud
```

### 4. 访问服务

**Web管理界面**:
- 访问地址: `https://your-domain.com/`
- 默认账号: admin / admin123
- 说明: Traefik转发到Server的8080端口

**FRP连接地址**:
- Agent连接: `wss://your-domain.com/ws`
- Desktop连接: `wss://your-domain.com/ws`
- 说明: Traefik转发到Server的7000端口（WebSocket）

## Traefik配置说明

Traefik作为网关处理TLS终止，配置由运维团队负责：

### 端口和路由

Server提供两个独立的服务端口：

**端口 8080 - Web管理界面**
- 路由: `/` - 前端页面和API
- 协议: HTTP
- Traefik配置: `https://your-domain.com/` → `awecloud-signaling-server:8080`

**端口 7000 - FRP信令服务**
- 路由: `/` - FRP WebSocket连接
- 协议: WebSocket
- Traefik配置: `wss://your-domain.com/ws` → `awecloud-signaling-server:7000`

### Traefik配置要点

1. **TLS证书**: Traefik自动从Let's Encrypt获取或使用自定义证书
2. **路由规则**:
   - Web管理: `https://your-domain.com/` → Server:8080 (HTTP)
   - FRP信令: `wss://your-domain.com/ws` → Server:7000 (WebSocket)
3. **WebSocket支持**: Traefik自动处理WebSocket升级
4. **路径重写**: `/ws` 路径需要重写为 `/` 转发到Server:7000

## 更新部署

```bash
# 更新镜像
kubectl set image deployment/awecloud-signaling-server \
  server=awecloud-signaling-server:new-version \
  -n awecloud

# 重启Pod
kubectl rollout restart deployment/awecloud-signaling-server -n awecloud

# 查看滚动更新状态
kubectl rollout status deployment/awecloud-signaling-server -n awecloud
```

## 删除部署

```bash
kubectl delete -f server-service.yaml
kubectl delete -f server-deployment.yaml
kubectl delete -f server-pvc.yaml
kubectl delete -f server-configmap.yaml
kubectl delete -f server-secret.yaml
kubectl delete namespace awecloud
```

## 注意事项

1. **证书管理**: 由Traefik统一管理，Server不需要配置证书
2. **持久化存储**: 数据库文件存储在PVC中，确保数据持久化
3. **资源限制**: 根据实际负载调整resources配置
4. **安全配置**: 生产环境必须修改JWT Secret和管理员密码
