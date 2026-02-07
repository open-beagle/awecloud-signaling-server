# Desktop 注销设计

## 概述

Desktop 注销时应安全离场，通知 Server 清理相关资源，而不是仅在本地清除凭证。

## 当前问题

当前 Desktop 注销流程：

1. 停止 gRPC 客户端
2. 断开 Tunnel
3. 清除本地配置（DeviceToken、ClientID 等）

问题：

- Server 不知道 Desktop 已注销
- Server 端的心跳连接会等到超时才断开
- Headscale 中的节点和 PreAuthKey 不会被清理
- 上游 Logto 的登录状态不会被注销

## 目标行为

Desktop 注销时应按以下顺序执行：

```
用户点击注销
    │
    ▼
调用 Server gRPC Logout
    │
    ├── Server 清除心跳连接
    ├── Server 清除设备心跳时间
    ├── Server 过期 Headscale 节点（可选）
    │
    ▼
断开 Tunnel 连接
    │
    ▼
停止 gRPC 客户端
    │
    ▼
清除本地凭证
    │
    ▼
跳转登录页
```

## gRPC 接口设计

新增 Logout RPC：

服务定义：DesktopService
方法名：Logout
请求参数：desktop_id（当前设备 ID）
响应参数：success（是否成功）、message（消息）

## Server 端处理

收到 Logout 请求后，Server 执行：

1. 验证 Desktop 是否存在
2. 关闭该设备的心跳连接（从 connections map 中移除并 Cancel）
3. 清除数据库中的心跳时间（last_heartbeat 设为 null）
4. 可选：过期 Headscale 节点（调用 ExpireNode），使隧道立即失效
5. 可选：删除 Headscale PreAuthKey
6. 返回成功

## Desktop 端处理

注销流程改为：

1. 调用 gRPC Logout（带超时，最多等 5 秒）
2. 无论 Logout 是否成功，继续执行本地清理
3. 断开 Tunnel
4. 停止 gRPC 客户端
5. 清除本地凭证
6. 跳转登录页

注意：即使 Server 不可达，注销也不应阻塞。gRPC Logout 调用失败时静默忽略，继续本地清理。

## Headscale 节点处理策略

两种方案：

| 方案     | 描述             | 优点               | 缺点               |
| -------- | ---------------- | ------------------ | ------------------ |
| 过期节点 | 调用 ExpireNode  | 隧道立即失效，安全 | 重新登录需要新节点 |
| 仅断开   | 不操作 Headscale | 重新登录可复用节点 | 节点残留           |

建议采用"仅断开"方案：

- 注销只清除 Server 端的认证状态
- Headscale 节点保留，下次登录时复用
- 节点的 PreAuthKey 自然过期即可

## 前端交互

注销按钮行为：

1. 弹出确认对话框："确定要注销吗？"
2. 确认后显示 loading 状态
3. 调用 Go 后端的 Logout 方法
4. 完成后跳转到登录页

## 注销后重新登录

注销后再登录不会创建新设备。设备唯一标识为 user_id + type + hostname（主机名），同一用户在同一台机器上重新登录时，Server 会复用已有设备记录，仅更新密钥和心跳时间。

只有在不同物理机器上登录（hostname 不同）才会创建新设备记录。

## 实现步骤

1. Proto 定义：在 desktop.proto 中添加 Logout RPC
2. Server 实现：在 desktop_service.go 中实现 Logout 方法
3. Desktop 客户端：在 client.go 中添加 Logout 方法
4. Desktop 后端：修改 app.go 的 Logout 方法，先调用 gRPC Logout
5. Desktop 前端：确认注销交互（已有确认对话框）
