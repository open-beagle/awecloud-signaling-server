# CloudIDE 环境变量设计

## 概述

Agent 支持两种部署模式，通过环境变量区分：

- 主机模式（传统）：使用 TOML 配置文件，环境变量可覆盖
- CloudIDE 模式：纯环境变量启动，无配置文件

所有环境变量统一使用 SIGNAL\_ 前缀。

## 环境变量总表

### 认证（必填）

| 环境变量      | 必填 | 说明                           | 示例                       |
| ------------- | ---- | ------------------------------ | -------------------------- |
| SIGNAL_TOKEN  | 是   | 部署 Token（统一，无前缀区分） | eyJhbGci...                |
| SIGNAL_SERVER | 是   | Server 地址                    | https://signal.example.com |

Agent 启动时统一调用 /api/v1/register 注册，Server 根据 Token 关联的 User.Role 返回角色信息，Agent 据此决定后续行为。

### 身份（按模式）

| 环境变量    | 必填           | 说明       | 示例    |
| ----------- | -------------- | ---------- | ------- |
| SIGNAL_NAME | Agent 模式必填 | Agent 名称 | beijing |

CloudIDE 模式下不需要 SIGNAL_NAME 和 SIGNAL_DEVICE。Token 已关联用户，设备名自动使用容器 hostname。

### 隧道

| 环境变量         | 必填 | 默认值         | 说明                           |
| ---------------- | ---- | -------------- | ------------------------------ |
| SIGNAL_SSH       | 否   | false          | 是否启用 Tailscale SSH（入站） |
| SIGNAL_STATE_DIR | 否   | 空（临时目录） | 隧道状态存储目录               |

CloudIDE 建议 SIGNAL_SSH=true（允许 Desktop SSH 进来）。
SIGNAL_STATE_DIR 留空时使用临时目录（无状态模式），容器重启后重新注册。

### CloudIDE 专属

| 环境变量           | 必填 | 默认值              | 说明                           |
| ------------------ | ---- | ------------------- | ------------------------------ |
| SIGNAL_SSH_CONFIG  | 否   | false               | 是否自动维护 ~/.ssh/config     |
| SIGNAL_SOCKS       | 否   | false               | 是否启用 SOCKS5 代理           |
| SIGNAL_SOCKS_ADDR  | 否   | 127.0.0.1:1080      | SOCKS5 监听地址                |
| SIGNAL_DIAL_SOCKET | 否   | /tmp/signaling.sock | dial 子命令的 Unix Socket 路径 |

SIGNAL_SSH_CONFIG=true 时，Agent 自动在 ~/.ssh/config 中写入 ProxyCommand 规则，劫持 100.64.\* 的 SSH 连接。

SIGNAL_SOCKS=true 时，Agent 启动 SOCKS5 代理，供非 SSH 程序按需使用。

### 日志

| 环境变量         | 必填 | 默认值 | 说明     |
| ---------------- | ---- | ------ | -------- |
| SIGNAL_LOG_LEVEL | 否   | info   | 日志级别 |

### 健康检查

| 环境变量           | 必填 | 默认值 | 说明               |
| ------------------ | ---- | ------ | ------------------ |
| SIGNAL_HEALTH_PORT | 否   | 8090   | 健康检查 HTTP 端口 |

## CloudIDE 部署示例

### k8s Pod 环境变量

```yaml
env:
  # 认证（必填，仅需这两个）
  - name: SIGNAL_TOKEN
    valueFrom:
      secretKeyRef:
        name: cloudide-signal
        key: token
  - name: SIGNAL_SERVER
    value: "https://signal.example.com"
  # 隧道（可选）
  - name: SIGNAL_SSH
    value: "true"
  # CloudIDE 功能（可选）
  - name: SIGNAL_SSH_CONFIG
    value: "true"
  - name: SIGNAL_SOCKS
    value: "true"
```

### Agent 启动流程（CloudIDE 模式）

```
Agent 启动
  │
  ├── 1. 读取 SIGNAL_TOKEN
  │
  ├── 2. 调用 /api/v1/register（统一注册接口）
  │     ├── 发送: token + device_fingerprint + hostname
  │     └── 返回: auth_key + headscale_url + user_name + user_role
  │
  ├── 3. 根据 user_role 决定后续行为
  │     ├── agent → 传统 Agent 模式
  │     └── client → CloudIDE 模式
  │
  ├── 4. 启动 tsnet（使用 auth_key 加入 Tailscale 网络）
  │
  ├── 5. 启用 Tailscale SSH（SIGNAL_SSH=true）
  │
  ├── 6. 启动 Unix Socket（SIGNAL_DIAL_SOCKET）
  │
  ├── 7. 写入 ~/.ssh/config（SIGNAL_SSH_CONFIG=true）
  │     └── Host 100.64.*
  │           ProxyCommand signaling dial %h %p
  │
  ├── 8. 启动 SOCKS5 代理（SIGNAL_SOCKS=true）
  │
  └── 9. 启动 gRPC 心跳
```

## 与旧环境变量的兼容

| 旧变量            | 新变量           | 说明        |
| ----------------- | ---------------- | ----------- |
| AGENT_NAME        | SIGNAL_NAME      | Agent 名称  |
| AGENT_TOKEN       | SIGNAL_TOKEN     | Token       |
| AGENT_ADDRESS     | SIGNAL_SERVER    | Server 地址 |
| AGENT_DEVICE      | SIGNAL_DEVICE    | 设备名      |
| TUNNEL_ENABLE_SSH | SIGNAL_SSH       | SSH 开关    |
| TUNNEL_STATE_DIR  | SIGNAL_STATE_DIR | 状态目录    |

过渡期两套都支持，SIGNAL\_ 优先级更高。旧变量在日志中输出 deprecation 警告。

## 最小化部署

CloudIDE 最小配置只需要 2 个环境变量（设备名自动使用容器 hostname）：

```
SIGNAL_TOKEN=xxx...
SIGNAL_SERVER=https://signal.example.com
```

加上 CloudIDE 出站功能：

```
SIGNAL_SSH_CONFIG=true
SIGNAL_SOCKS=true
```
