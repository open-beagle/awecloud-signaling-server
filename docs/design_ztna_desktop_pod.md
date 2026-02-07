# ZTNA Desktop.Pod 容器内 daemon 设计

## 概述

Desktop.Pod 运行在 K8S Pod 中（CloudIDE 容器），以 Client 角色加入 Tailscale 网络。它不是 Agent，而是 Desktop 的另一种形态——没有 GUI 的纯后台 daemon。

Desktop.Pod 使用 agent 二进制的 RunClient 模式运行，权限和 Desktop.Host 完全相同。

共通设计见 design_ztna_desktop.md。本文档仅描述 Desktop.Pod 特有的部分。

相关文档：

- design_cloudide.md — CloudIDE 集成设计（当前实现）
- design_cloudide_host.md — 主机集成（Desktop 发现 CloudIDE、双向 SSH）
- design_cloudide_env.md — 环境变量设计

## 当前实现状态

| 模块                      | 状态   | 说明                                         |
| ------------------------- | ------ | -------------------------------------------- |
| Agent 二进制复用          | 已完成 | Desktop.Pod 使用 agent 二进制 RunClient 模式 |
| Deploy Token 注册         | 已完成 | 统一 deploy_tokens 机制                      |
| tsnet 用户态连接          | 已完成 | 无需 TUN 设备，无需 NET_ADMIN                |
| Tailscale SSH 入站        | 已完成 | Desktop.Host → SSH → Desktop.Pod 可工作      |
| SIGNAL\_ 环境变量         | 已完成 | 统一前缀，仅需 SIGNAL_TOKEN + SIGNAL_SERVER  |
| 设备指纹（hostname 哈希） | 已完成 | Pod 重建不影响 Token 绑定                    |
| ACL 同用户互访            | 已完成 | Desktop.Host ↔ Desktop.Pod 天然互通          |
| signaling dial 子命令     | 待实现 | SSH 出站的核心                               |
| ~/.ssh/config 自动维护    | 待实现 | 静默劫持 SSH                                 |
| DNS 劫持                  | 待实现 | /etc/resolv.conf 指向本地 DNS                |
| VIP 分配                  | 待实现 | 127.1.x.x 本地地址                           |

## 与 Desktop.Host 的差异

### DNS 配置方式

Desktop.Pod 运行在容器内，有 root 权限，通过修改 /etc/resolv.conf 指向本地 DNS 服务器：

```
Desktop.Pod 启动后：
  1. 启动本地 DNS 服务器（127.0.0.1:53）
  2. 修改 /etc/resolv.conf
       nameserver 127.0.0.1
       # 保留原有的上游 DNS 作为转发目标
  3. 本地 DNS 处理逻辑：
     ├── .k8s 后缀 → 查询 VIP 映射表，返回 127.1.x.x
     └── 其他域名 → 转发到原上游 DNS
```

### SSH 出站

Desktop.Pod 没有系统 SSH 客户端的控制权，通过 signaling dial 子命令和 ~/.ssh/config 实现 SSH 劫持：

```
signaling dial 子命令：
  signaling dial <target-ip> <port>
    → 连接 Agent Unix Socket
    → 通过 tsnet 隧道连接目标
    → 桥接 stdin/stdout

~/.ssh/config 自动维护：
  Host *.k8s
    ProxyCommand signaling dial %h %p
    StrictHostKeyChecking no

效果：
  ssh deploy@web-server-1.beijing.k8s
    → SSH 客户端读取 ~/.ssh/config
    → 调用 signaling dial web-server-1.beijing.k8s 22
    → DNS 劫持 → VIP → tsnet → Agent
```

### kubectl 访问

Desktop.Pod 自动配置 kubeconfig：

```
Desktop.Pod 启动时：
  1. 从 Server 获取可访问的 K8S 集群列表
  2. 生成 kubeconfig
       每个集群一个 context
       server 指向域名（如 api.beijing.k8s:6443）
  3. 写入 ~/.kube/config
```

### 注册方式

Desktop.Pod 使用 Deploy Token 注册，不走 Logto 登录：

```
环境变量：
  SIGNAL_TOKEN=xxx          # Deploy Token
  SIGNAL_SERVER=https://signal.example.com

启动流程：
  1. Deploy Token 注册 → 获取 auth_key
  2. tsnet 连接 Headscale
  3. 启动 DNS 劫持 + VIP 分配 + 本地代理
  4. 启动 SSH 入站（如果 SIGNAL_SSH=true）
  5. 维护 ~/.ssh/config（如果 SIGNAL_SSH_CONFIG=true）
```

## 环境变量

| 环境变量          | 默认值 | 说明                            |
| ----------------- | ------ | ------------------------------- |
| SIGNAL_TOKEN      | 必填   | Deploy Token                    |
| SIGNAL_SERVER     | 必填   | Server 地址                     |
| SIGNAL_SSH        | false  | 是否启用 SSH 入站               |
| SIGNAL_SSH_CONFIG | false  | 是否自动维护 ~/.ssh/config      |
| SIGNAL_DNS        | false  | 是否启用 DNS 劫持（新增）       |
| SIGNAL_KUBECONFIG | false  | 是否自动配置 kubeconfig（新增） |

CloudIDE 部署时推荐配置：

```
SIGNAL_TOKEN=xxx
SIGNAL_SERVER=https://signal.example.com
SIGNAL_SSH=true
SIGNAL_SSH_CONFIG=true
SIGNAL_DNS=true
SIGNAL_KUBECONFIG=true
```

## 实现优先级

| 阶段 | 内容                         | 依赖             |
| ---- | ---------------------------- | ---------------- |
| P0   | signaling dial 子命令        | 无（当前待实现） |
| P0   | ~/.ssh/config 自动维护       | signaling dial   |
| P1   | DNS 劫持（/etc/resolv.conf） | 本地 DNS 服务器  |
| P1   | VIP 分配 + 本地代理          | DNS 劫持         |
| P2   | kubeconfig 自动配置          | 域名体系         |
