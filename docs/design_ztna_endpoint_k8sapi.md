# Endpoint K8SAPI 代理重构设计

## 问题分析

### 当前实现（有缺陷）

当前 Endpoint K8SAPI 采用纯 TCP 透传方案：

```
Desktop kubectl
  → Desktop TLS 终止（自签证书，解密为明文 HTTP）
  → tsnet → Agent FallbackTCPHandler（端口 50153+N）
  → gRPC 双向流（K8SAPIProxyData，传输原始字节）
  → Endpoint 收到原始字节
  → Endpoint 用 tls.Dial 建立新 TLS 连接到 K8S API Server
  → 把明文 HTTP 字节塞进新 TLS 连接
  → K8S API Server 收到无认证的 HTTP 请求 → 401
```

问题：

1. Desktop TLS 终止后，数据已经是明文 HTTP
2. Endpoint 又用 tls.Dial 建立新 TLS 连接，等于把明文 HTTP 包进了 TLS
3. 这个新 TLS 连接没有携带任何 K8S 认证信息（没有 client cert，没有 bearer token）
4. K8S API Server 收到的是一个有 TLS 但无认证的请求，返回 401

### 正确方案

复用 Agent K8SAPI 已验证的架构：HTTP 反向代理 + Impersonation + KubeconfigManager。

Agent K8SAPI 已经稳定运行，核心设计：

- Desktop TLS 终止 → 明文 HTTP → tsnet WireGuard 加密隧道 → Agent
- Agent 用 HTTP 反向代理接收明文 HTTP 请求
- Agent 从 tsnet 连接提取用户身份（WhoIs）
- Agent 查询 ACL 权限，获取 Impersonation 分组
- Agent 用本地 kubeconfig（超管权限）+ Impersonation 头转发到 K8S API Server
- K8S API Server 以模拟用户身份执行请求

Endpoint K8SAPI 应该在 Endpoint 侧实现完全相同的 HTTP 反向代理 + Impersonation 逻辑。

## 重构后架构

### 数据流

```
Desktop kubectl
  │
  │ HTTPS（kubectl → 本地自签证书）
  ↓
Desktop 本地代理（127.1.x.x:6443）
  │
  │ TLS 终止，解密为明文 HTTP
  ↓
tsnet 隧道（WireGuard 加密）
  │
  ↓
Agent FallbackTCPHandler（端口 50153+N）
  │
  │ 1. 从 tsnet 连接提取用户身份（WhoIs → zhangsan）
  │ 2. 查询 Endpoint K8SAPI ACL 权限
  │ 3. 获取 Impersonation 分组（如 developer）
  │ 4. 将用户名和分组通过 gRPC 消息传递给 Endpoint
  │
  ↓
gRPC 双向流（OpenK8SAPIProxy）
  │
  │ 首包携带：session_id, token, user_name, k8s_groups
  │ 后续：HTTP 请求/响应的原始字节
  │
  ↓
Endpoint K8SAPI 代理
  │
  │ 1. 从 gRPC 首包获取用户身份和分组
  │ 2. 用 KubeconfigManager 加载本地 kubeconfig（超管权限）
  │ 3. 启动 HTTP 反向代理
  │ 4. 对每个请求注入 Impersonation 头
  │ 5. 转发到本地 K8S API Server
  │
  ↓
K8S API Server（localhost:6443）
  │
  │ 以 zhangsan 身份 + developer 角色执行
  │ 应用 RBAC 规则
  ↓
返回结果（原路返回）
```

### 与 Agent K8SAPI 的对比

```
                    Agent K8SAPI                    Endpoint K8SAPI（重构后）
─────────────────────────────────────────────────────────────────────────────
身份提取          Agent 从 tsnet WhoIs 提取        Agent 从 tsnet WhoIs 提取
权限检查          Agent 查 ACL 缓存                Agent 查 ACL 缓存
身份传递          不需要（Agent 本地处理）          Agent → gRPC 首包 → Endpoint
HTTP 反向代理     Agent 本地运行                   Endpoint 本地运行
Impersonation     Agent 注入请求头                 Endpoint 注入请求头
K8S 认证          Agent KubeconfigManager          Endpoint KubeconfigManager
K8S API Server    Agent 本地 localhost:6443         Endpoint 本地 K8S API 地址
协议升级          Agent 处理 SPDY/WebSocket         Endpoint 处理 SPDY/WebSocket
```

关键区别：身份提取和权限检查仍然在 Agent 侧完成（因为只有 Agent 在 Tailscale 网络中能做 WhoIs），但 HTTP 反向代理和 Impersonation 注入在 Endpoint 侧完成。

## 详细设计

### 一、gRPC 协议变更

当前 K8SAPIProxyData 消息只传输原始字节，需要扩展首包以携带用户身份信息。

#### 方案：扩展 K8SAPIProxyData 首包

在现有 K8SAPIProxyData 消息中新增字段，仅在首包（is_open=true）中使用：

```
K8SAPIProxyData 消息扩展：
  现有字段（不变）：
    data        bytes   原始数据
    is_open     bool    首包标志
    is_close    bool    关闭标志
    session_id  string  会话 ID
    token       string  认证令牌
    error       string  错误信息

  新增字段（仅首包使用）：
    user_name   string  Desktop 用户名（Agent WhoIs 提取）
    k8s_groups  repeated string  Impersonation 分组（Agent ACL 查询）
```

### 二、Agent 侧变更

Agent 端 endpoint_k8sapi.go 的 handleConn 需要重构：

#### 当前流程（TCP 透传）

```
1. 从 tsnet 提取用户身份
2. 检查 Endpoint K8SAPI 权限
3. 调用 RequestK8SAPIProxy 获取 gRPC 流
4. 等待 Desktop 第一个数据包
5. 双向桥接：TCP conn ↔ gRPC stream（原始字节透传）
```

#### 重构后流程（HTTP 感知）

```
1. 从 tsnet 提取用户身份 → user_name
2. 检查 Endpoint K8SAPI 权限 → k8s_groups
3. 调用 RequestK8SAPIProxy 获取 gRPC 流
   （传递 user_name 和 k8s_groups，写入 gRPC 首包）
4. 双向桥接：TCP conn ↔ gRPC stream（原始字节透传，不变）
```

变更点：

- RequestK8SAPIProxy 接口新增 user_name 和 k8s_groups 参数
- Agent 在 gRPC 首包中携带用户身份信息
- TCP 双向桥接逻辑不变（仍然是原始字节透传）
- Agent 不再需要等待第一个数据包的特殊逻辑

注意：Agent 侧仍然是 TCP 透传，不做 HTTP 解析。HTTP 反向代理在 Endpoint 侧完成。

### 三、Endpoint 侧变更（核心）

Endpoint 端 k8sapi.go 需要完全重写，从 TCP 透传改为 HTTP 反向代理。

#### 当前流程（TCP 透传，有缺陷）

```
1. 收到 gRPC 流
2. 用 tls.Dial 连接 K8S API Server
3. 双向桥接：gRPC stream ↔ TLS conn（原始字节透传）
```

#### 重构后流程（HTTP 反向代理）

```
1. 收到 gRPC 流，从首包提取 user_name 和 k8s_groups
2. 用 KubeconfigManager 加载本地 kubeconfig（超管权限）
3. 将 gRPC 流包装为 net.Conn（双向字节流 → 标准连接接口）
4. 在该连接上启动 HTTP Server
5. HTTP Server 的 Handler 实现：
   a. 注入 Impersonate-User 和 Impersonate-Group 请求头
   b. 检查是否为协议升级请求（SPDY/WebSocket）
   c. 普通请求：用 httputil.ReverseProxy 转发
   d. 升级请求：建立 TCP 隧道双向透传
6. 会话结束时清理资源
```

#### Endpoint KubeconfigManager

Endpoint 复用与 Agent 完全相同的 KubeconfigManager 逻辑：

```
加载优先级：
  1. 主机模式：~/.kube/config（物理机部署）
  2. Pod 模式：ServiceAccount token（K8S Pod 部署）
  3. 失败：K8SAPI 能力不可用

要求：
  kubeconfig 中的凭证必须有 Impersonation 权限
  即 kubeconfig 对应的用户/SA 必须有 ClusterRole 允许 impersonate
```

#### gRPC 流包装为 net.Conn

Endpoint 需要将 gRPC 双向流包装为标准 net.Conn 接口，以便 HTTP Server 可以在上面运行。

```
包装逻辑：
  Read()  → 从 gRPC stream.Recv() 读取 data 字段
  Write() → 通过 gRPC stream.Send() 发送 data 字段
  Close() → 发送 is_close=true

这样 HTTP Server 可以像处理普通 TCP 连接一样处理 gRPC 流。
```

#### Impersonation 注入

```
对每个 HTTP 请求：
  1. 清除已有的 Impersonate-* 头（防止客户端伪造）
  2. 设置 Impersonate-User: <user_name>（来自 gRPC 首包）
  3. 设置 Impersonate-Group: <group>（来自 gRPC 首包，可能多个）
  4. 转发到 K8S API Server
```

#### 协议升级处理（SPDY/WebSocket）

kubectl exec/attach/port-forward/logs -f 使用 SPDY 或 WebSocket 协议升级。

```
检测方式：
  Connection 头包含 "Upgrade"

处理方式：
  1. 建立到 K8S API Server 的 TLS 连接（使用 KubeconfigManager 的 TLS 配置）
  2. 注入 Impersonation 头
  3. 将原始 HTTP 请求写入后端连接
  4. 劫持前端连接（Hijack）
  5. 双向透传数据
```

### 四、Desktop 侧

Desktop 不需要任何变更。当前的 TLS 终止 + TCP 透传方案完全正确：

```
kubectl → HTTPS → Desktop 本地代理（TLS 终止）→ 明文 HTTP → tsnet → Agent
```

### 五、权限模型

权限检查仍然在 Agent 侧完成，与当前实现一致：

```
Agent handleConn:
  1. tsnet WhoIs → user_name
  2. permCache.CheckEndpointK8SAPIAccess(user_name, endpoint_name)
     → k8s_groups, allowed
  3. 如果 allowed，将 user_name 和 k8s_groups 传递给 Endpoint
  4. Endpoint 用这些信息做 Impersonation

权限数据来源：
  Server → Agent 心跳响应 → EndpointK8SAPIPermission
  包含：user_name, endpoint_name, k8s_groups, namespaces
```

注意：命名空间级别的权限控制暂不在 Endpoint 侧实现（与 Agent K8SAPI 一致，命名空间过滤由 K8S RBAC 通过 Impersonation 角色的 ClusterRoleBinding 实现）。

## 变更范围

### 需要修改的文件

```
Proto 层：
  pkg/proto/endpoint.proto        新增 K8SAPIProxyData 的 user_name 和 k8s_groups 字段

Agent 端：
  internal/agent/endpoint_k8sapi.go   handleConn 传递用户身份到 gRPC 首包
  internal/agent/endpoint_server.go   RequestK8SAPIProxy 接口新增参数

Endpoint 端：
  cmd/endpoint/k8sapi.go              完全重写，TCP 透传 → HTTP 反向代理
  cmd/endpoint/main.go                初始化 KubeconfigManager
```

### 需要新增的文件

```
Endpoint 端：
  cmd/endpoint/grpc_conn.go           gRPC 流包装为 net.Conn 的适配器
```

### 不需要修改的文件

```
Desktop 端：无变更
Server 端：无变更
Web 前端：无变更
```

## 实现步骤

```
1. 扩展 Proto
   K8SAPIProxyData 新增 user_name 和 k8s_groups 字段
   重新生成 protobuf 代码

2. 修改 Agent 端
   endpoint_k8sapi.go handleConn：将 user_name 和 k8s_groups 传递给 RequestK8SAPIProxy
   endpoint_server.go RequestK8SAPIProxy：接口新增参数，写入 gRPC 首包

3. 实现 gRPC 流 → net.Conn 适配器
   cmd/endpoint/grpc_conn.go

4. 重写 Endpoint K8SAPI 代理
   cmd/endpoint/k8sapi.go：HTTP 反向代理 + Impersonation
   cmd/endpoint/main.go：初始化 KubeconfigManager

5. 编译部署测试
   编译 Agent + Endpoint
   部署到 unicom-08（Agent）和 beagle-002（Endpoint）
   Desktop 测试 kubectl --context neimeng get node
```

## Endpoint KubeconfigManager 复用说明

Endpoint 的 KubeconfigManager 与 Agent 的完全相同，但 Endpoint 是独立二进制（cmd/endpoint），不能直接 import internal/agent 包。

处理方式：将 KubeconfigManager 提取到公共包，或在 Endpoint 中实现一个轻量版本。

考虑到 Endpoint 是轻量 daemon，推荐在 cmd/endpoint 中实现轻量版本：

```
轻量版 KubeconfigManager：
  1. 尝试加载 ~/.kube/config（主机模式）
  2. 尝试 InClusterConfig（Pod 模式）
  3. 返回 rest.Config 和 HTTP Client

与 Agent 版本的区别：
  不需要 GetStatusForHeartbeat（Endpoint 不上报 K8S 状态）
  不需要 ValidateConnection（Endpoint 启动时不验证）
  只需要 GetHTTPClient 和 GetRESTConfig
```

## 安全性说明

```
1. 身份不可伪造
   用户身份由 Agent 从 tsnet WhoIs 提取，Tailscale 网络保证身份真实性
   Endpoint 信任 Agent 传递的身份（gRPC 连接已经过 endpoint_token 认证）

2. Impersonation 权限要求
   Endpoint 的 kubeconfig 必须有 Impersonation 权限
   即对应的用户/SA 需要绑定包含 impersonate 动词的 ClusterRole
   如果没有 Impersonation 权限，K8S API Server 会返回 403

3. 信任链
   Desktop 身份 → Tailscale WhoIs（Agent 验证）
   Agent → Endpoint 身份传递 → gRPC 认证连接（endpoint_token）
   Endpoint → K8S API Server → kubeconfig 超管凭证 + Impersonation

4. gRPC 首包中的身份信息
   user_name 和 k8s_groups 仅在 gRPC 首包中传递一次
   Endpoint 在整个会话中使用同一身份
   不存在中途篡改身份的可能（gRPC 流是有序的）
```
