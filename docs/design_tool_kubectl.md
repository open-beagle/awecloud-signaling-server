# kubectl ts 工具设计

> 版本：v0.2.3

## 1. 概述

用户通过 Desktop 客户端管理多个异地 K8s 集群，无需维护各集群的 kubeconfig 和 token。

## 2. 架构

与 SSH 工具类似，Agent 作为代理转发到内网 K8s API Server：

```
场景：访问内网 K8s 集群
──────────────────────────

kubectl ──▶ Desktop ══Tailscale══▶ Agent ──TCP──▶ K8s API Server
                                   (代理)        (内网 6443)
                                                      │
                                                      ▼
                                                  K8s 集群

认证：Tailscale ACL + K8s 认证（Token/证书）
```

**与 SSH 的对比**

| 对比项     | SSH 场景 B                    | kubectl ts                     |
| ---------- | ----------------------------- | ------------------------------ |
| 目标服务   | 内网 SSH 服务器               | 内网 K8s API Server            |
| 默认端口   | 22                            | 6443                           |
| Agent 角色 | TCP 代理                      | TCP 代理                       |
| 认证方式   | Tailscale ACL + SSH 密码/密钥 | Tailscale ACL + K8s Token/证书 |

## 3. 服务配置

K8s 集群作为 Agent 的一个 TCP 服务进行管理：

```
服务配置示例：
├─ 服务名称: k8s-prod
├─ 服务类型: k8s（或 tcp）
├─ Agent: agent-beijing
├─ Agent 服务端口: 16443
├─ 内网目标地址: 192.168.1.100:6443
└─ K8s 认证信息: Token / 证书（加密存储）
```

**数据模型扩展**

在现有 Service 模型基础上，增加 K8s 相关字段：

| 字段          | 类型   | 说明                         |
| ------------- | ------ | ---------------------------- |
| Type          | string | 服务类型，新增 "k8s"         |
| K8sAuthType   | string | K8s 认证方式：token/cert     |
| K8sToken      | text   | ServiceAccount Token（加密） |
| K8sCACert     | text   | CA 证书                      |
| K8sClientCert | text   | 客户端证书（cert 认证）      |
| K8sClientKey  | text   | 客户端私钥（cert 认证）      |

## 4. 连接流程

```
1. 管理员配置
   ├─ 在 Web 界面为 Agent 创建 K8s 服务
   ├─ 填写内网 API Server 地址和认证信息
   └─ 授权给指定 Desktop 用户

2. Desktop 用户连接
   ├─ 方式一：直接使用 kubectl
   │   └─ kubectl --server=https://100.64.0.5:16443 get pods
   │
   └─ 方式二：使用 kubectl ts 封装命令
       └─ kubectl ts --cluster=k8s-prod get pods

3. 数据流向
   kubectl → Desktop → Tailscale 隧道 → Agent:16443 → 192.168.1.100:6443
```

## 5. kubeconfig 生成

Desktop 可以为用户生成 kubeconfig 文件：

```yaml
apiVersion: v1
kind: Config
clusters:
  - name: k8s-prod
    cluster:
      server: https://100.64.0.5:16443
      certificate-authority-data: <CA证书>
contexts:
  - name: k8s-prod
    context:
      cluster: k8s-prod
      user: k8s-prod-user
current-context: k8s-prod
users:
  - name: k8s-prod-user
    user:
      token: <ServiceAccount Token>
```

**kubeconfig 中的 server 地址**

| 场景         | server 地址              | 说明                          |
| ------------ | ------------------------ | ----------------------------- |
| 直连 Agent   | https://100.64.0.5:16443 | Agent Tailscale IP + 服务端口 |
| 通过 Visitor | https://127.0.0.1:16443  | Desktop 本地端口              |

## 6. kubectl ts 命令

| 命令                               | 说明                                    |
| ---------------------------------- | --------------------------------------- |
| kubectl ts list                    | 列出有权限的 K8s 集群                   |
| kubectl ts use \<cluster\>         | 切换当前集群上下文                      |
| kubectl ts config                  | 导出当前集群的 kubeconfig               |
| kubectl ts get pods                | 执行 kubectl 命令（自动使用当前上下文） |
| kubectl ts --cluster=xxx get nodes | 指定集群执行命令                        |

## 7. 与 SSH 的统一管理

一个 Agent 可以同时提供 SSH 和 K8s 服务：

```
Agent: agent-beijing (100.64.0.5)
├─ SSH 服务
│   ├─ 端口 10022 → 192.168.1.10:22 (数据库服务器)
│   └─ 端口 10023 → 192.168.1.11:22 (应用服务器)
│
└─ K8s 服务
    ├─ 端口 16443 → 192.168.1.100:6443 (生产集群)
    └─ 端口 16444 → 192.168.1.101:6443 (测试集群)
```

## 8. 实现步骤

1. 扩展 Service 模型，增加 K8s 认证字段
2. Agent 支持 K8s 类型服务的 TCP 代理
3. Web 界面增加 K8s 服务配置表单
4. Desktop 实现 kubectl ts 命令封装
5. Desktop 实现 kubeconfig 生成和管理
