# Tailscale 升级 - 基础设施设计

> 本文档描述 Tailscale 升级所需的基础设施部署，包括 Headscale、DERP Server 的部署配置。

## 1. 基础设施概述

### 1.1 组件说明

| 组件        | 说明                           | 部署位置     |
| ----------- | ------------------------------ | ------------ |
| Headscale   | Tailscale 控制平面（开源实现） | 公网 K8S     |
| DERP Server | 中继服务器（P2P 失败时使用）   | 公网 K8S     |
| Server      | 业务管理服务（现有）           | 公网 K8S     |
| Agent       | 内网代理（现有）               | 公司内网 K8S |

### 1.2 网络规划

| 网络         | 地址段         | 说明                     |
| ------------ | -------------- | ------------------------ |
| Tailscale IP | 100.64.0.0/10  | 虚拟网络 IP 段           |
| Pod 网络     | 10.2.0.0/16    | K8S Pod 网络（避免冲突） |
| 内网网络     | 192.168.1.0/24 | 公司/家庭内网            |

### 1.3 端口规划

| 端口 | 协议  | 服务      | 说明                            |
| ---- | ----- | --------- | ------------------------------- |
| 443  | HTTPS | Traefik   | TLS 终止                        |
| 8080 | HTTP  | Headscale | 控制平面 API                    |
| 8081 | HTTP  | DERP      | 中继服务 HTTPS                  |
| 3479 | UDP   | DERP STUN | NAT 探测（避开 Coturn 的 3478） |

---

## 2. Headscale 部署

### 2.1 部署架构

```txt
┌─────────────────────────────────────────────────────────────────┐
│                        公网 K8S 集群                             │
│                                                                 │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐ │
│  │   Headscale     │  │   DERP Server   │  │     Server      │ │
│  │   (控制平面)     │  │   (中继服务)     │  │   (业务管理)    │ │
│  │   :8080         │  │   :8081 (HTTPS) │  │   :8080         │ │
│  │                 │  │   :3479 (STUN)  │  │                 │ │
│  └────────┬────────┘  └────────┬────────┘  └────────┬────────┘ │
│           │                    │                    │          │
│           └────────────────────┼────────────────────┘          │
│                                │                               │
│                         ┌──────┴──────┐                        │
│                         │   Traefik   │                        │
│                         │   (入口)     │                        │
│                         └──────┬──────┘                        │
└────────────────────────────────┼────────────────────────────────┘
                                 │
                    https://signaling.example.com
                    ├── /headscale/* → Headscale
                    ├── /derp        → DERP Server
                    ├── /ts2021      → Headscale (Tailscale 协议)
                    └── /*           → Server (业务 API)
```

### 2.2 Kubernetes 资源

Headscale 部署包含以下 Kubernetes 资源：

| 资源类型   | 名称             | 说明               |
| ---------- | ---------------- | ------------------ |
| ConfigMap  | headscale-config | Headscale 配置文件 |
| Secret     | headscale-secret | API Key 等敏感信息 |
| PVC        | headscale-data   | 数据持久化存储     |
| Deployment | headscale        | Headscale 服务     |
| Service    | headscale        | ClusterIP 服务     |

配置文件：[deployments/kubernetes/headscale-configmap.yaml](../deployments/kubernetes/headscale-configmap.yaml)

### 2.3 Headscale 配置说明

```yaml
# 关键配置项说明
server_url: https://signaling.example.com/headscale # 外部访问地址
listen_addr: 0.0.0.0:8080 # 监听地址
grpc_listen_addr: 0.0.0.0:50443 # gRPC 地址（内部）

# IP 地址段配置
ip_prefixes:
  - 100.64.0.0/10 # Tailscale IP 段，避免与 Pod 网络冲突

# DERP 配置
derp:
  server:
    enabled: true
    region_id: 900
    region_code: "private"
    stun_listen_addr: "0.0.0.0:3479" # 避开 Coturn 的 3478

# 数据库
db_type: sqlite3
db_path: /var/lib/headscale/db.sqlite
```

### 2.4 部署文件

- ConfigMap: [headscale-configmap.yaml](../deployments/kubernetes/headscale-configmap.yaml)
- Secret: [headscale-secret.yaml](../deployments/kubernetes/headscale-secret.yaml)
- PVC: [headscale-pvc.yaml](../deployments/kubernetes/headscale-pvc.yaml)
- Deployment: [headscale-deployment.yaml](../deployments/kubernetes/headscale-deployment.yaml)
- Service: [headscale-service.yaml](../deployments/kubernetes/headscale-service.yaml)

---

## 3. DERP Server 部署

### 3.1 DERP 说明

DERP (Designated Encrypted Relay for Packets) 是 Tailscale 的中继服务：

- 当 P2P 直连失败时，流量通过 DERP 中继
- 支持 HTTPS 模式，难以被防火墙封锁
- 可以与 Headscale 集成部署

### 3.2 部署方式

**方式 A：Headscale 内置 DERP（推荐）**

Headscale 可以内置 DERP 服务，无需单独部署：

```yaml
# headscale config.yaml
derp:
  server:
    enabled: true
    region_id: 900
    region_code: "private"
    region_name: "Private DERP"
    stun_listen_addr: "0.0.0.0:3479"
```

**方式 B：独立 DERP Server**

如需更高性能或多区域部署，可单独部署 DERP：

- Deployment: [derp-deployment.yaml](../deployments/kubernetes/derp-deployment.yaml)
- Service: [derp-service.yaml](../deployments/kubernetes/derp-service.yaml)

### 3.3 STUN 端口说明

```txt
STUN 端口用于 NAT 类型探测：
- 标准端口: 3478（被 Coturn 占用）
- 本项目使用: 3479

需要在防火墙开放 UDP 3479 端口
```

---

## 4. Traefik 路由配置

### 4.1 路由规则

```txt
https://signaling.example.com
│
├── /headscale/*     → headscale:8080    (Headscale API)
├── /ts2021          → headscale:8080    (Tailscale 协议)
├── /derp            → headscale:8081    (DERP HTTPS)
└── /*               → server:8080       (业务 API，默认)
```

### 4.2 IngressRoute 配置

配置文件：[server-ingressroute-tailscale.yaml](../deployments/kubernetes/server-ingressroute-tailscale.yaml)

关键配置说明：

```yaml
# Headscale 路由
- match: Host(`signaling.example.com`) && PathPrefix(`/headscale`)
  services:
    - name: headscale
      port: 8080
  middlewares:
    - name: headscale-stripprefix # 去除 /headscale 前缀

# Tailscale 协议路由
- match: Host(`signaling.example.com`) && PathPrefix(`/ts2021`)
  services:
    - name: headscale
      port: 8080

# DERP 路由
- match: Host(`signaling.example.com`) && PathPrefix(`/derp`)
  services:
    - name: headscale
      port: 8081

# 业务 API 路由（默认）
- match: Host(`signaling.example.com`)
  services:
    - name: awecloud-signaling-server
      port: 8080
      scheme: h2c
```

---

## 5. Server 配置变更

### 5.1 ConfigMap 变更

原配置（FRP）：

```toml
[server]
bind_addr = "0.0.0.0"
bind_port = 7000
transport_protocol = "websocket"
token = "xxx"
```

新配置（Tailscale）：

```toml
[tailscale]
headscale_url = "http://headscale:8080"
headscale_api_key = ""  # 从 Secret 获取
namespace = "default"
derp_url = "https://signaling.example.com/derp"
stun_port = 3479
ip_prefix = "100.64.0.0/10"
auth_key_expiry_hours = 24
```

配置文件：[server-configmap-tailscale.yaml](../deployments/kubernetes/server-configmap-tailscale.yaml)

### 5.2 Deployment 变更

主要变更：

- 移除 FRP 端口（7000）
- 新增 Headscale API Key 环境变量
- 新增 Headscale 服务依赖

配置文件：[server-deployment-tailscale.yaml](../deployments/kubernetes/server-deployment-tailscale.yaml)

---

## 6. Agent 配置变更

### 6.1 ConfigMap 变更

原配置（FRP）：无独立配置，通过环境变量

新配置（Tailscale）：

```toml
[tailscale]
state_dir = "/var/lib/awecloud-agent/tailscale"
```

### 6.2 Deployment 变更

主要变更：

- 新增 Tailscale 状态存储卷
- 新增 NET_ADMIN capability（Tailscale 需要）
- 移除 FRP 相关配置

配置文件：[agent-deployment-tailscale.yaml](../deployments/kubernetes/agent-deployment-tailscale.yaml)

---

## 7. 部署步骤

### 7.1 前置条件

1. K8S 集群已部署 Traefik
2. 域名 DNS 已配置
3. TLS 证书已配置

### 7.2 部署顺序

```bash
# 1. 部署 Headscale
kubectl apply -f deployments/kubernetes/headscale-configmap.yaml
kubectl apply -f deployments/kubernetes/headscale-secret.yaml
kubectl apply -f deployments/kubernetes/headscale-pvc.yaml
kubectl apply -f deployments/kubernetes/headscale-deployment.yaml
kubectl apply -f deployments/kubernetes/headscale-service.yaml

# 2. 创建 Headscale 命名空间和 API Key
kubectl exec -it deploy/headscale -- headscale namespaces create default
kubectl exec -it deploy/headscale -- headscale apikeys create --expiration 365d
# 记录输出的 API Key

# 3. 更新 Server Secret（添加 Headscale API Key）
kubectl edit secret awecloud-signaling-server
# 添加 headscale-api-key

# 4. 部署更新后的 Server
kubectl apply -f deployments/kubernetes/server-configmap-tailscale.yaml
kubectl apply -f deployments/kubernetes/server-deployment-tailscale.yaml

# 5. 更新 IngressRoute
kubectl apply -f deployments/kubernetes/server-ingressroute-tailscale.yaml

# 6. 部署更新后的 Agent
kubectl apply -f deployments/kubernetes/agent-deployment-tailscale.yaml
```

### 7.3 验证部署

```bash
# 检查 Headscale 状态
kubectl exec -it deploy/headscale -- headscale status

# 检查 DERP 连接
curl https://signaling.example.com/derp

# 检查 Server 健康
curl https://signaling.example.com/health

# 检查 Agent 注册
kubectl logs deploy/awecloud-signaling-agent
```

---

## 8. 安全配置

### 8.1 Headscale ACL

```json
{
  "acls": [
    {
      "action": "accept",
      "src": ["group:users"],
      "dst": ["tag:agent:*"]
    }
  ],
  "groups": {
    "group:users": ["*"]
  },
  "tagOwners": {
    "tag:agent": ["autogroup:admin"]
  }
}
```

### 8.2 网络策略

建议配置 NetworkPolicy 限制 Pod 间通信：

- Headscale 只允许来自 Traefik 和 Server 的访问
- Server 只允许来自 Traefik 的访问
- Agent 允许出站到 Headscale 和内网

---

**文档版本**: 1.0
**创建日期**: 2025-01-08
**关联文档**:

- [Tailscale 升级方案设计](design_tailscale_upgrade.md)
- [Tailscale 升级 - Server 端变更设计](design_tailscale_server.md)
