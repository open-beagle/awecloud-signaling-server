# AWECloud-Signaling 部署方案

## 1. 部署架构

```
                    ┌─────────────────────────────────┐
                    │      公有云（有公网IP）          │
                    │                                 │
                    │    ┌──────────────────┐         │
                    │    │  Traefik网关      │         │
                    │    │  :80, :443       │         │
                    │    └────────┬─────────┘         │
                    │             │                   │
                    │   ┌─────────┴─────────┐         │
                    │   │                   │         │
                    │   ▼                   ▼         │
                    │ ┌──────────┐    ┌──────────┐   │
                    │ │Server-Web│    │Server-FRP│   │
                    │ │  :8080   │◄──►│  :7000   │   │
                    │ └──────────┘    └──────────┘   │
                    │       │              │         │
                    │       └──────┬───────┘         │
                    │              ▼                 │
                    │         ┌─────────┐            │
                    │         │ SQLite  │            │
                    │         │   DB    │            │
                    │         └─────────┘            │
                    └─────────────────────────────────┘
```

## 2. Docker部署

### 2.1 Server端Dockerfile

参考 `deployments/docker/Dockerfile.server`：

```dockerfile
FROM golang:1.21-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -o server ./cmd/server

FROM alpine:latest
RUN apk --no-cache add ca-certificates sqlite

WORKDIR /app
COPY --from=builder /app/server .

EXPOSE 8080 7000
CMD ["./server"]
```

### 2.2 Agent端Dockerfile

参考 `deployments/docker/Dockerfile.agent`：

```dockerfile
FROM golang:1.21-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o agent ./cmd/agent

FROM alpine:latest
RUN apk --no-cache add ca-certificates

WORKDIR /app
COPY --from=builder /app/agent .

CMD ["./agent"]
```



## 3. Kubernetes部署

### 3.1 部署架构

```
Ingress (Traefik)
    ↓
Service (server-service)
    ↓
Deployment (server-deployment)
    ↓
Pod (server)
    ↓
PVC (server-pvc) → PV
```

### 3.2 ConfigMap

参考 `deployments/kubernetes/server-configmap.yaml`：

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: server-config
  namespace: awecloud
data:
  SERVER_DOMAIN: "your-domain.com"
  SERVER_WEB_PORT: "8080"
  SERVER_FRP_PORT: "7000"
  DB_PATH: "/data/awecloud.db"
```

### 3.3 Secret

参考 `deployments/kubernetes/server-secret.yaml`：

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: server-secret
  namespace: awecloud
type: Opaque
stringData:
  ADMIN_USERNAME: "admin"
  ADMIN_PASSWORD: "admin123"
```

### 3.4 PersistentVolumeClaim

参考 `deployments/kubernetes/server-pvc.yaml`：

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: server-pvc
  namespace: awecloud
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 10Gi
  storageClassName: standard
```

### 3.5 Deployment

参考 `deployments/kubernetes/server-deployment.yaml`：

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: server-deployment
  namespace: awecloud
spec:
  replicas: 1
  selector:
    matchLabels:
      app: awecloud-server
  template:
    metadata:
      labels:
        app: awecloud-server
    spec:
      containers:
      - name: server
        image: your-registry/awecloud-server:latest
        ports:
        - containerPort: 8080
          name: web
        - containerPort: 7000
          name: frp
        envFrom:
        - configMapRef:
            name: server-config
        - secretRef:
            name: server-secret
        volumeMounts:
        - name: data
          mountPath: /data
        livenessProbe:
          httpGet:
            path: /health
            port: 8080
          initialDelaySeconds: 30
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /ready
            port: 8080
          initialDelaySeconds: 5
          periodSeconds: 5
      volumes:
      - name: data
        persistentVolumeClaim:
          claimName: server-pvc
```

### 3.6 Service

参考 `deployments/kubernetes/server-service.yaml`：

```yaml
apiVersion: v1
kind: Service
metadata:
  name: server-service
  namespace: awecloud
spec:
  selector:
    app: awecloud-server
  ports:
  - name: web
    port: 8080
    targetPort: 8080
  - name: frp
    port: 7000
    targetPort: 7000
  type: ClusterIP
```

### 3.7 Ingress (Traefik)

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: server-ingress
  namespace: awecloud
  annotations:
    kubernetes.io/ingress.class: traefik
    cert-manager.io/cluster-issuer: letsencrypt-prod
    traefik.ingress.kubernetes.io/router.entrypoints: websecure
    traefik.ingress.kubernetes.io/router.tls: "true"
spec:
  tls:
  - hosts:
    - your-domain.com
    secretName: awecloud-tls
  rules:
  - host: your-domain.com
    http:
      paths:
      # Server-Web路由
      - path: /
        pathType: Prefix
        backend:
          service:
            name: server-service
            port:
              number: 8080
      # Server-FRP路由
      - path: /ws
        pathType: Prefix
        backend:
          service:
            name: server-service
            port:
              number: 7000
```

### 3.8 部署步骤

```bash
# 创建命名空间
kubectl create namespace awecloud

# 应用配置
kubectl apply -f deployments/kubernetes/server-configmap.yaml
kubectl apply -f deployments/kubernetes/server-secret.yaml
kubectl apply -f deployments/kubernetes/server-pvc.yaml
kubectl apply -f deployments/kubernetes/server-deployment.yaml
kubectl apply -f deployments/kubernetes/server-service.yaml
kubectl apply -f deployments/kubernetes/server-ingress.yaml

# 查看状态
kubectl get pods -n awecloud
kubectl get svc -n awecloud
kubectl get ingress -n awecloud

# 查看日志
kubectl logs -f deployment/server-deployment -n awecloud
```

## 4. Traefik配置

### 4.1 静态配置

`traefik.yml`:

```yaml
entryPoints:
  web:
    address: ":80"
    http:
      redirections:
        entryPoint:
          to: websecure
          scheme: https
  websecure:
    address: ":443"

providers:
  docker:
    exposedByDefault: false
  file:
    filename: /etc/traefik/dynamic.yml

certificatesResolvers:
  myresolver:
    acme:
      email: your-email@example.com
      storage: /letsencrypt/acme.json
      tlsChallenge: {}

api:
  dashboard: true
  insecure: true
```

### 4.2 动态配置

`dynamic.yml`:

```yaml
http:
  routers:
    server-web:
      rule: "Host(`your-domain.com`) && PathPrefix(`/`)"
      service: server-web
      entryPoints:
        - websecure
      tls:
        certResolver: myresolver
    
    server-frp:
      rule: "Host(`your-domain.com`) && PathPrefix(`/ws`)"
      service: server-frp
      entryPoints:
        - websecure
      tls:
        certResolver: myresolver

  services:
    server-web:
      loadBalancer:
        servers:
          - url: "http://server:8080"
    
    server-frp:
      loadBalancer:
        servers:
          - url: "http://server:7000"
```

## 5. 环境变量配置

### 5.1 Server端环境变量

| 变量名 | 说明 | 默认值 | 必填 |
|--------|------|--------|------|
| `SERVER_DOMAIN` | 服务器域名 | - | 是 |
| `SERVER_WEB_PORT` | Web服务端口 | 8080 | 否 |
| `SERVER_FRP_PORT` | FRP服务端口 | 7000 | 否 |
| `DB_PATH` | 数据库文件路径 | ./awecloud.db | 否 |
| `ADMIN_USERNAME` | 默认管理员用户名 | admin | 否 |
| `ADMIN_PASSWORD` | 默认管理员密码 | admin123 | 否 |
| `SESSION_SECRET` | Session密钥 | 随机生成 | 否 |
| `LOG_LEVEL` | 日志级别 | info | 否 |

### 5.2 Agent端环境变量

| 变量名 | 说明 | 默认值 | 必填 |
|--------|------|--------|------|
| `SERVER_URL` | Server地址 | - | 是 |
| `AGENT_NAME` | Agent名称 | - | 是 |
| `AGENT_TOKEN` | Agent认证Token | - | 是 |
| `LOG_LEVEL` | 日志级别 | info | 否 |

### 5.3 Desktop端环境变量

| 变量名 | 说明 | 默认值 | 必填 |
|--------|------|--------|------|
| `SERVER_URL` | Server地址 | - | 是 |
| `CLIENT_ID` | Client ID | - | 是 |
| `CLIENT_SECRET` | Client密钥 | - | 是 |
| `LOG_LEVEL` | 日志级别 | info | 否 |

## 6. 健康检查

### 6.1 健康检查端点

**Server-Web**:
- `GET /health`: 健康检查
- `GET /ready`: 就绪检查

**响应**:
```json
{
  "status": "ok",
  "timestamp": 1732531200
}
```

### 6.2 监控指标

建议集成Prometheus监控：

```yaml
# Prometheus配置
scrape_configs:
  - job_name: 'awecloud-server'
    static_configs:
      - targets: ['server:8080']
    metrics_path: '/metrics'
```

## 7. 备份和恢复

### 7.1 数据备份

**Docker部署**:
```bash
# 备份数据库
docker exec awecloud-server sqlite3 /data/awecloud.db ".backup /data/backup.db"
docker cp awecloud-server:/data/backup.db ./backup_$(date +%Y%m%d).db
```

**Kubernetes部署**:
```bash
# 备份PVC数据
kubectl exec -n awecloud deployment/server-deployment -- \
  sqlite3 /data/awecloud.db ".backup /data/backup.db"

kubectl cp awecloud/server-deployment-xxx:/data/backup.db \
  ./backup_$(date +%Y%m%d).db
```

### 7.2 数据恢复

**Docker部署**:
```bash
# 停止服务
docker-compose stop server

# 恢复数据库
docker cp ./backup.db awecloud-server:/data/awecloud.db

# 启动服务
docker-compose start server
```

**Kubernetes部署**:
```bash
# 缩容到0
kubectl scale deployment/server-deployment -n awecloud --replicas=0

# 恢复数据库
kubectl cp ./backup.db awecloud/server-deployment-xxx:/data/awecloud.db

# 扩容到1
kubectl scale deployment/server-deployment -n awecloud --replicas=1
```

## 8. 安全建议

### 8.1 生产环境配置

1. **修改默认密码**: 首次部署后立即修改管理员密码
2. **使用强密钥**: 为Session和Token生成强随机密钥
3. **启用HTTPS**: 使用Let's Encrypt或其他证书
4. **限制访问**: 使用防火墙限制管理端口访问
5. **定期备份**: 配置自动备份任务
6. **日志审计**: 启用详细日志并定期审计

### 8.2 网络安全

```yaml
# Kubernetes NetworkPolicy示例
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: server-network-policy
  namespace: awecloud
spec:
  podSelector:
    matchLabels:
      app: awecloud-server
  policyTypes:
  - Ingress
  - Egress
  ingress:
  - from:
    - namespaceSelector:
        matchLabels:
          name: ingress-nginx
    ports:
    - protocol: TCP
      port: 8080
    - protocol: TCP
      port: 7000
  egress:
  - to:
    - namespaceSelector: {}
    ports:
    - protocol: TCP
      port: 53
    - protocol: UDP
      port: 53
```

## 9. 故障排查

### 9.1 常见问题

**问题1**: Agent无法连接到Server
- 检查网络连接
- 验证Server域名和端口
- 检查Agent Token是否正确
- 查看Server日志

**问题2**: Desktop无法访问服务
- 检查Client是否已授权
- 验证STCP实例是否创建
- 检查Agent是否在线
- 查看Desktop和Server日志

**问题3**: 数据库锁定错误
- 检查是否有多个Server实例
- 确保只有一个Server实例运行
- 重启Server服务

### 9.2 日志查看

**Docker**:
```bash
docker-compose logs -f server
docker-compose logs -f --tail=100 server
```

**Kubernetes**:
```bash
kubectl logs -f deployment/server-deployment -n awecloud
kubectl logs --tail=100 deployment/server-deployment -n awecloud
```

---

**文档版本**: 1.0  
**最后更新**: 2025-11-25
