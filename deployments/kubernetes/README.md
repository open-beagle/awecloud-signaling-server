# Kubernetes 部署指南

## 文件说明

| 文件类型 | 命名规则         | Git 收录 | 用途                   |
| -------- | ---------------- | -------- | ---------------------- |
| 模板文件 | `xxx.yaml`       | ✅ 是    | 参考模板，不含敏感信息 |
| 部署文件 | `xxx.local.yaml` | ❌ 否    | 实际部署，包含敏感信息 |

## 快速开始

### 1. 创建命名空间

```bash
kubectl create namespace beagle-access
```

### 2. 部署 Headscale

```bash
# 复制模板并修改
cp deployments/kubernetes/headscale.local.yaml.example deployments/kubernetes/headscale.local.yaml
vim deployments/kubernetes/headscale.local.yaml

# 部署
kubectl apply -f deployments/kubernetes/headscale.local.yaml
```

### 3. 部署 Server

```bash
# 复制模板并修改
cp deployments/kubernetes/server.local.yaml.example deployments/kubernetes/server.local.yaml
vim deployments/kubernetes/server.local.yaml

# 部署
kubectl apply -f deployments/kubernetes/server.local.yaml
```

### 4. 部署 Agent

```bash
# 1. 在 Server Web 界面创建 Agent，获取 token
# 2. 复制模板并修改
cp deployments/kubernetes/agent.local.yaml.example deployments/kubernetes/agent.local.yaml
vim deployments/kubernetes/agent.local.yaml

# 3. 部署
kubectl apply -f deployments/kubernetes/agent.local.yaml
```

## 部署文件

### server.local.yaml

包含 Server 所有资源：

- Secret（JWT 密钥、Headscale API Key）
- ConfigMap（server.toml 配置）
- PVC（数据持久化）
- Deployment
- Service
- IngressRoute

需要修改的配置：

- `jwt-secret`: JWT 密钥
- `headscale-api-key`: Headscale API 密钥
- `default_admin_password`: 管理员密码
- IngressRoute 的 Host

### headscale.local.yaml

包含 Headscale 所有资源：

- Secret（noise private key）
- ConfigMap（config.yaml 配置）
- PVC（数据持久化）
- Deployment
- Service
- IngressRoute

需要修改的配置：

- `noise-private-key`: 私钥（`openssl rand -hex 32`）
- `server_url`: 公网访问地址
- IngressRoute 的 Host

### agent.local.yaml

包含 Agent 所有资源：

- Secret（Agent Token）
- Deployment

需要修改的配置：

- `agent-token`: 从 Server Web 界面获取
- `SIGNAL_NAME`: Agent 名称
- `SIGNAL_SERVER`: Server 地址

## 验证部署

```bash
# 查看 Pod 状态
kubectl get pods -n beagle-access

# 查看日志
kubectl logs -f -n beagle-access deployment/headscale
kubectl logs -f -n beagle-access deployment/awecloud-signaling-server
kubectl logs -f -n beagle-access deployment/awecloud-signaling-agent

# 健康检查
kubectl exec -it -n beagle-access deployment/awecloud-signaling-server -- curl localhost:8080/health
```

## 镜像版本

- Server: `registry.cn-qingdao.aliyuncs.com/wod/awecloud-signaling-server:v0.2.0`
- Agent: `registry.cn-qingdao.aliyuncs.com/wod/awecloud-signaling-agent:v0.2.0`
- Headscale: `headscale/headscale:0.23.0`

## 端口说明

| 组件      | 端口  | 协议      | 用途                              |
| --------- | ----- | --------- | --------------------------------- |
| Server    | 8080  | HTTP/gRPC | Web 界面、API、Agent/Desktop 连接 |
| Headscale | 8080  | HTTP      | API、Web 界面                     |
| Headscale | 50443 | gRPC      | 内部通信                          |
| Headscale | 3479  | UDP       | STUN                              |
| Headscale | 3478  | TCP/UDP   | DERP                              |

## 清理

```bash
kubectl delete namespace beagle-access
```
