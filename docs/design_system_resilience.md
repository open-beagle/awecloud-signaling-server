# 系统韧性设计

## 概述

当 Server 或 Tunnel 发生中断后恢复时，Desktop 和 Agent 应能自动恢复连接，无需用户手动干预。

## 1. Desktop 与 Server 韧性

### 1.1 gRPC 连接恢复

当 Desktop 登录成功后，Server(gRPC) 中断再上线，Desktop 应自动恢复连接。

当前状态：

- 心跳流断开后不会自动重连
- 需要用户手动重启 Desktop

目标行为：

- 心跳流断开后，Desktop 自动进入重连循环
- 使用指数退避策略（5s → 10s → 20s → 40s → 60s 上限）
- 重连时使用已保存的凭证重新 Authenticate
- 重连成功后恢复心跳流和服务列表刷新
- 前端显示 gRPC 状态变化（断开 → 重连中 → 已连接）

重连流程：

```
心跳流断开
    │
    ▼
等待退避时间
    │
    ▼
创建新 gRPC 连接
    │
    ├── 失败 ──▶ 增加退避时间，回到等待
    │
    ▼
重新 Authenticate
    │
    ├── 失败（凭证无效）──▶ 跳转登录页
    │
    ▼
启动心跳流
    │
    ▼
恢复正常
```

### 1.2 Tunnel 连接恢复

当 Tunnel 中断后恢复时，Desktop 应自动重连隧道。

当前状态：

- Tailscale 底层有一定的自动重连能力
- 但上层没有监控和恢复机制

目标行为：

- 定期检测 Tunnel 连接状态（每 30s）
- 检测到断开后，尝试重新连接
- 如果 AuthKey 过期，通过 gRPC 获取新的 AuthKey
- 重连成功后更新心跳中的 Tunnel IP
- 前端显示 Tunnel 状态变化

重连流程：

```
检测到 Tunnel 断开
    │
    ▼
尝试重新连接（使用现有 AuthKey）
    │
    ├── 成功 ──▶ 更新心跳，恢复正常
    │
    ▼
AuthKey 过期
    │
    ▼
通过 gRPC 获取新 AuthKey（Authenticate）
    │
    ├── 失败 ──▶ 等待退避时间后重试
    │
    ▼
使用新 AuthKey 重连 Tunnel
    │
    ▼
恢复正常
```

## 2. Agent 与 Server 韧性

### 2.1 gRPC 连接恢复

当 Agent 注册成功后，Server(gRPC) 中断再上线，Agent 应自动恢复连接。

当前状态：

- Agent 心跳流（heartbeatLoop）已实现自动重连
- 使用指数退避策略（5s → 60s 上限）
- 但心跳流重连不会重新注册

目标行为：

- 心跳流断开后自动重连（已实现）
- 如果 gRPC 连接完全断开（Server 长时间不可用），需要重新建立连接
- 重连后如果心跳被 Server 拒绝（Agent 不存在），自动重新注册
- 保持 Tailscale 连接不中断（Agent 的 Tailscale 状态独立于 gRPC）

### 2.2 Tunnel 连接恢复

当 Tunnel 中断后恢复时，Agent 应自动重连隧道。

当前状态：

- Agent 的 TailscaleManager 没有自动重连机制

目标行为：

- 定期检测 Tunnel 连接状态
- 断开后尝试重连
- AuthKey 过期时通过重新注册获取新 AuthKey
- 重连成功后更新心跳中的 Tunnel IP

## 3. 状态机

Desktop 和 Agent 的连接状态可以用以下状态机描述：

```
┌──────────┐     登录/注册成功     ┌──────────┐
│  未连接  │ ──────────────────▶  │  已连接  │
└──────────┘                      └──────────┘
                                       │
                                  gRPC 断开
                                       │
                                       ▼
                                  ┌──────────┐
                              ┌── │  重连中  │ ◀─┐
                              │   └──────────┘   │
                              │        │         │
                         重连成功   重连失败      │
                              │        │         │
                              ▼        └─────────┘
                         ┌──────────┐
                         │  已连接  │
                         └──────────┘
```

凭证失效时从"重连中"跳转到"未连接"，需要用户重新登录。

## 4. 前端状态展示

Desktop 前端 Layout 组件已有 gRPC 和 Tunnel 状态指示器，需要增加：

- 重连中状态的显示（黄色闪烁）
- 重连次数和下次重试时间的 tooltip
- 断开原因的提示

## 5. 实现优先级

1. Desktop gRPC 自动重连（最重要，影响用户体验）
2. Desktop Tunnel 状态监控和重连
3. Agent Tunnel 状态监控和重连（Agent 心跳重连已有）
