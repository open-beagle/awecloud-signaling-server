# Desktop.Pod (CloudIDE) 用户禁用体验设计

相关文档：

- `design_ztna_server_user_disable.md` — Server 用户禁用设计（禁用业务逻辑）
- `design_ztna_desktop_pod.md` — Desktop.Pod 架构设计（CloudIDE 整体架构）
- `design_ztna_desktop_host_user_disable.md` — Desktop.Host 用户禁用体验设计

## 概述

Desktop.Pod 运行在 CloudIDE 容器中，作为无 GUI 的后台 daemon，使用 Deploy Token 注册。当用户被禁用时，Desktop.Pod 的行为与 Desktop.Host 有所不同，因为它是后台服务，没有用户界面。

## 禁用感知机制

Desktop.Pod 通过以下方式感知用户被禁用：

### 1. 注册阶段感知

#### 场景 1：Deploy Token 注册

```
Desktop.Pod 启动
    │
    ├─ 读取环境变量（SIGNAL_TOKEN + SIGNAL_SERVER）
    │
    ├─ 调用 POST /api/v1/register
    │      └─ Server 检查 user.Enabled = false
    │
    └─ 收到注册响应
           ├─ Success = false
           └─ Message = "用户已禁用"
```

行为：

- 注册被拒绝，记录日志："安全提示: 用户已禁用"
- signal_agent 进程直接退出（exit code 1）
- 容器保持运行（因为启动脚本使用 nohup 后台启动，脚本本身已执行完毕）
- 不重试注册，不启动任何服务

### 2. 运行阶段感知

Desktop.Pod 使用 gRPC 心跳机制（每 30 秒一次）。当用户被禁用时：

#### 场景 2：已运行的 Pod 被禁用

```
Desktop.Pod 正常运行
    │
    ├─ 管理员禁用用户
    │      └─ Server 更新 user.Enabled = false
    │
    ├─ Desktop.Pod 发送心跳（最多 30 秒后）
    │      ├─ Server 检查 user.Enabled = false
    │      ├─ Server 返回错误："用户已禁用"
    │      └─ Server 主动断开心跳流
    │
    ├─ Desktop.Pod 收到心跳错误
    │      ├─ 记录日志："用户已禁用，心跳流被断开"
    │      ├─ 停止所有服务（DNS、代理、SSH）
    │      ├─ 断开 tsnet 连接
    │      └─ 主动退出进程（exit）
    │
    └─ Desktop.Pod 退出
```

行为：

- Desktop.Pod 在心跳检测到禁用后主动退出
- 停止所有服务（DNS 劫持、本地代理、SSH 入站）
- 断开 tsnet 连接
- 进程退出，容器停止
- 日志明确记录："用户已禁用，进程退出"

原因：

- 用户被禁用应该立即失去所有访问能力
- 主动退出避免资源浪费和安全风险
- 容器编排平台（K8S）会根据重启策略决定是否重启

## 用户体验设计

### 注册被拒绝时

由于 CloudIDE 是后台服务，没有 GUI，用户无法直接看到错误提示。需要通过其他方式告知用户：

#### 1. 日志记录

Desktop.Pod 记录详细的错误日志：

```
[signal_agent] 安全提示: 用户已禁用
[signal_agent] 用户: zhangsan (role=client)
[signal_agent] 进程退出
```

#### 2. 进程检查

```bash
# 检查进程
ps aux | grep signal_agent | grep -v grep
# 输出为空（进程已退出）

# 查看日志
tail -f $HOME/.local/share/signal/logs/agent.log
# 最后一行：[signal_agent] 安全提示: 用户已禁用
```

### 运行中被禁用时

#### 1. 用户感知

用户在 CloudIDE 终端中执行命令时，会遇到各种错误（DNS 解析、连接等），需要查看日志排查问题。

#### 2. 日志记录

Desktop.Pod 在退出前记录详细日志：

```
[signal_agent] 安全提示: 用户已禁用
[signal_agent] 用户: zhangsan (role=client)
[signal_agent] 停止所有服务...
[signal_agent] 停止 DNS 服务器
[signal_agent] 停止本地代理管理器
[signal_agent] 断开 tsnet 连接
[signal_agent] 进程退出
```

#### 3. 进程检查

```bash
# 检查进程
ps aux | grep signal_agent | grep -v grep
# 输出为空（进程已退出）

# 查看日志
tail -f $HOME/.local/share/signal/logs/agent.log
# 最后几行：
# [signal_agent] 安全提示: 用户已禁用
# [signal_agent] 停止所有服务...
# [signal_agent] 进程退出
```

### 重新启用后

#### 场景 3：禁用后重新启用

```
管理员重新启用用户
    │
    ├─ signal_agent 已退出（容器仍在运行）
    │
    ├─ 用户或管理员手动重启 signal_agent
    │      └─ 执行 scripts/install_signal.sh
    │
    ├─ 注册成功
    │      ├─ 获取 AuthKey
    │      ├─ 连接 Headscale
    │      └─ 启动所有服务
    │
    └─ 功能恢复
```

行为：

- signal_agent 需要手动重启（执行启动脚本）
- 重新注册并连接 Headscale
- 启动所有服务（DNS、代理、SSH）
- 用户可以重新访问资源

恢复时间：

- 取决于手动重启的时间（通常 5-10 秒）

注意：

- 容器本身不会自动重启 signal_agent
- 需要手动执行 `bash scripts/install_signal.sh` 才能恢复服务

## 错误消息设计

### 注册被拒绝消息

| 场景              | 日志消息               | 日志级别 |
| ----------------- | ---------------------- | -------- |
| Deploy Token 注册 | "安全提示: 用户已禁用" | ERROR    |

### 运行时消息

| 场景     | 日志消息               | 日志级别 |
| -------- | ---------------------- | -------- |
| 心跳响应 | "安全提示: 用户已禁用" | ERROR    |
| 停止服务 | "停止所有服务..."      | INFO     |
| 进程退出 | "进程退出"             | INFO     |

## 日志记录

Desktop.Pod 应记录详细的禁用相关日志：

### 注册被拒绝日志

```
[signal_agent] 开始注册: token=xxx***, server=https://signal.example.com
[signal_agent] 安全提示: 用户已禁用
[signal_agent] 用户: zhangsan (role=client)
[signal_agent] 进程退出
```

### 心跳检测日志

```
[signal_agent] 安全提示: 用户已禁用
[signal_agent] 用户: zhangsan (role=client)
[signal_agent] 停止所有服务...
[signal_agent] 停止 DNS 服务器
[signal_agent] 停止本地代理管理器
[signal_agent] 断开 tsnet 连接
[signal_agent] 进程退出
```

## 测试场景

### 场景 1：注册时用户已禁用

1. 管理员禁用用户
2. CloudIDE 容器启动
3. Desktop.Pod 尝试注册
4. 验证：注册被拒绝，记录日志："安全提示: 用户已禁用"
5. 验证：Desktop.Pod 进程退出
6. 验证：容器仍在运行

### 场景 2：运行中被禁用

1. Desktop.Pod 正常运行
2. 管理员禁用用户
3. 验证：最多 30 秒后，Desktop.Pod 心跳检测到禁用
4. 验证：Desktop.Pod 记录日志："用户已禁用，进程退出"
5. 验证：Desktop.Pod 停止所有服务并退出
6. 验证：容器停止运行

### 场景 3：禁用后重新启用

1. 管理员禁用用户
2. Desktop.Pod 注册失败并退出
3. 管理员重新启用用户
4. K8S 重启容器（如果配置了重启策略）
5. 验证：Desktop.Pod 重新注册成功
6. 验证：功能恢复

### 场景 4：运行中被禁用后重新启用

1. Desktop.Pod 正常运行
2. 管理员禁用用户
3. Desktop.Pod 检测到禁用并退出
4. 管理员重新启用用户
5. 手动重启 signal_agent（执行启动脚本）
6. 验证：signal_agent 重新注册成功
7. 验证：用户可以访问资源

## 实施要点

### 代码修改位置

1. 心跳处理（`internal/agent/agent.go`）
   - 在 `heartbeatLoop` 中检查心跳响应内容
   - 对"用户已禁用"响应，停止所有服务并退出进程
   - 记录详细的退出日志

2. 注册逻辑（`cmd/agent/main.go` 或 `internal/agent/agent.go`）
   - 在注册被拒绝时检查错误类型
   - 对"用户已禁用"错误，记录日志并退出进程（不重试）
   - 对"Deploy Token 无效"错误，记录日志并退出进程（不重试）
   - 对网络错误，实施重试策略

3. 日志记录（`internal/agent/agent.go`）
   - 记录注册被拒绝的详细信息
   - 记录心跳检测到禁用的详细信息
   - 记录用户信息（用户名、角色）
   - 记录进程退出原因

### 优先级

1. 高优先级：心跳检测到禁用时主动退出逻辑
2. 高优先级：注册被拒绝时主动退出逻辑
3. 中优先级：详细日志记录

## 与 Desktop.Host 的差异

| 特性     | Desktop.Host           | Desktop.Pod                       |
| -------- | ---------------------- | --------------------------------- |
| 用户界面 | 有 GUI，显示错误对话框 | 无 GUI，只记录日志                |
| 禁用感知 | 心跳检测 + 认证被拒绝  | 心跳检测 + 注册被拒绝             |
| 用户操作 | 点击"退出"按钮         | 无操作，进程自动退出              |
| 退出行为 | 用户点击后退出         | 检测到禁用后自动退出              |
| 恢复方式 | 重新打开应用并登录     | 手动重启 signal_agent（执行脚本） |

## 设计权衡

### 为什么 signal_agent 直接退出？

1. **安全性**：用户被禁用应该立即失去所有访问能力，进程退出是最彻底的方式
2. **明确状态**：进程退出后，状态清晰，用户和管理员都能明确知道服务已停止
3. **避免资源浪费**：禁用用户的进程继续运行会浪费 CPU、内存和网络资源
4. **简单可靠**：直接退出比复杂的重试逻辑更简单、更可靠

### 为什么不自动重试？

1. **明确的业务决策**："用户已禁用"和"Deploy Token 无效"是明确的业务决策，不是临时故障
2. **需要管理员干预**：这些情况需要管理员重新启用用户或更新 Token，自动重试没有意义
3. **快速暴露问题**：立即退出让问题更快暴露，便于排查和修复
4. **避免无效请求**：持续重试会给 Server 造成不必要的负担

### 为什么容器不自动重启 signal_agent？

1. **启动方式决定**：CloudIDE 使用 `nohup` 后台启动，启动脚本执行完毕后就退出了
2. **手动控制**：需要手动重新执行启动脚本，给管理员更多控制权
3. **避免无限重启**：如果自动重启，会导致注册被拒绝 → 退出 → 重启 → 注册被拒绝的无限循环

### 为什么运行中被禁用时主动退出？

1. **心跳机制已存在**：Desktop.Pod 已经有心跳机制，可以及时检测到禁用状态（最多 30 秒）
2. **一致性**：与 Desktop.Host 的行为保持一致（都是检测到禁用后退出）
3. **彻底断开**：进程退出会释放所有资源（DNS、代理、SSH、tsnet 连接）
