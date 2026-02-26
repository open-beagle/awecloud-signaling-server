# ZTNA Agent 能力对象设计

## 概述

Agent 部署在内网环境，以 agent 角色加入 Tailscale 网络。Agent 不区分类型（没有 Host Agent / K8s Agent 的分类），通过配置和挂载的能力对象决定它能做什么。

## Agent 部署方式

Agent 有两种部署方式，能力范围不同：

### 1. Service 部署（systemd 服务）

通过 scripts/install_agent.sh 安装，以 root 用户运行 systemd 服务。

```
适用场景：物理机 / 虚拟机，需要全功能
运行用户：root
服务管理：systemctl（k8s-signaling.service）
安装方式：scripts/install_agent.sh

可用能力：
  AgentSSH          ✅  需要 root 权限做 setuid 切换用户
  AgentK8SAPI       ✅  需要 kubeconfig
  AgentK8SService   ✅  需要 K8S API 访问
  AgentService      ✅  手动端口映射
  Endpoint Server   ✅  内网 gRPC Server（面向 Endpoint）

特点：
  以 root 运行，拥有 setuid 权限，SSH 可切换到任意系统用户
  直接监听物理网卡端口（Endpoint gRPC 50052 等）
  Tailscale 状态存储在本地磁盘
```

### 2. Pod 部署（K8S 容器）

通过 K8S Deployment 部署，以非 root 用户（UID 1000）运行。

```
适用场景：K8S 集群内，主要做 SVC 转发
运行用户：code（UID 1000），非 root
服务管理：kubectl（Deployment）
镜像构建：.beagle/agent.dockerfile

可用能力：
  AgentSSH          ⚠️  受限，无法 setuid，只能以容器用户身份运行
  AgentK8SAPI       ✅  通过 ServiceAccount 访问 K8S API
  AgentK8SService   ✅  通过 ServiceAccount 发现 Service
  AgentService      ✅  手动端口映射
  Endpoint Server   ✅  需要 hostNetwork 或 NodePort 暴露端口

特点：
  非 root 运行，不需要集群特权
  不需要 NET_ADMIN（tsnet 用户态）
  不需要特权容器
  SSH 功能受限：Server 可以开启 SSH，但容器内无法 setuid 切换用户
```

### 两种部署方式对比

```
                    Service 部署              Pod 部署
  运行用户          root                      code (UID 1000)
  SSH setuid        ✅ 可切换任意用户          ❌ 无法切换用户
  Endpoint gRPC     直接监听物理端口           需要 hostNetwork
  K8S API 访问      kubeconfig                ServiceAccount
  安装升级          install_agent.sh          镜像构建 + kubectl
  服务管理          systemctl                 kubectl
```

### 同一节点不能同时运行两种部署

同一台机器上 Service 部署和 Pod 部署不能同时运行，原因：

- Tailscale 状态目录冲突（同一个 Agent 身份不能有两个实例）
- 端口冲突（Endpoint gRPC 50052、健康检查 8090 等）
- 域名注册冲突（同一个 Agent 名称注册两次）

如果节点需要 SSH 全功能，必须用 Service 部署。
如果节点只需要 SVC 转发，可以用 Pod 部署。

## 四种本机能力

Agent 本机能力由 Server 远程控制，通过心跳下发配置。

```
          能力              说明                              控制方式
  AgentSSH          Agent 本机 SSH（已有）                Server 下发 ssh_enabled + AclSSHPermission
  AgentK8SAPI       Agent 本机 K8S API 代理（新增）       Server 下发 k8s_enabled + AclK8sPermission
  AgentK8SService   Agent 本机 K8S SVC 代理（新增）       Server 下发 k8s_service_enabled + AclK8SServicePermission
  AgentService      Agent 手动端口映射（现有 ProxyService 改名） Server 下发配置 + AclServicePermission
```

## AgentSSH — Agent 本机 SSH

这不是新对象，是现有 Tailscale SSH 能力的描述。

```
已有实现：
  Server 下发 ssh_enabled = true  → Agent 启用 SSH
  AclSSHUserPermission             → 谁能 SSH 到这个 Agent，用哪些 Linux 用户
  AclSSHGroupPermission            → 分组级 SSH 授权

  Desktop → tsnet SSH → Agent → Tailscale SSH 模块 → shell

不需要额外改动。
```

## AgentK8SAPI — Agent 本机 K8S API 代理

Agent 部署在 K8S 主节点时，直接访问本机 K8S API Server，做 Impersonation 代理。

### 能力控制

AgentK8SAPI 能力由 Server 远程控制，通过心跳下发 `k8s_enabled` 字段：

```
心跳响应:
  k8s_enabled: true   → Agent 启动 K8S API 代理
  k8s_enabled: false  → Agent 不启动 K8S API 代理
```

Agent 本地不需要任何 K8S 相关配置，只需要自动检测环境并加载凭证。

### 业务流程

```
完整访问流程（Desktop → Agent → K8S API Server）：

  1. Desktop 从 Server 获取可访问的 K8S 集群列表
       → 发现 beijing 集群（AgentK8SAPI）
       → 域名: kubernetes.beijing.beagle
       → 端口: 6443

  2. Desktop 魔法 DNS 解析
       → kubernetes.beijing.beagle → 127.1.x.x（VIP 地址）
       → 本地代理监听 127.1.x.x:6443

  3. Desktop 生成 kubeconfig
       → server: https://kubernetes.beijing.beagle:6443
       → 证书验证跳过（TLS 由 tsnet 隧道保证）
       → 写入 ~/.kube/config

  4. 用户执行 kubectl 命令
       → kubectl get pods -n yygl
       → 请求发送到 kubernetes.beijing.beagle:6443

  5. Desktop 魔法 DNS 劫持
       → kubernetes.beijing.beagle → 127.1.x.x
       → 本地代理接收请求（127.1.x.x:6443）

  6. Desktop 本地代理转发
       → 查询域名注册表：target_ip=100.64.0.23, target_port=50050
       → 通过 tsnet Dial 连接 Agent（100.64.0.23:50050）

  7. Agent 接收请求
       → 从 tsnet 连接提取身份（zhangsan）
       → 查询权限（本地缓存）
       → 允许的 namespaces: ["yygl"]
       → k8s_role: developer

  8. Agent 构造 Impersonation 请求
       → Impersonate-User: zhangsan
       → Impersonate-Group: developer
       → 使用主节点 kubeconfig 转发到本机 K8S API Server

  9. K8S API Server 处理请求
       → 以 zhangsan 身份执行
       → 应用 developer 角色的 RBAC 规则
       → 返回结果

  10. 结果返回给 Desktop → kubectl 显示
```

### 技术实现

```
Agent 侧实现：

  ├── tsnet 监听 K8S API 代理端口（50050）
  │     从 tsnet 连接提取身份
  │     查询 AclK8sPermission（本地缓存）
  │
  ├── Impersonation 转发
  │     添加 Impersonate-User 和 Impersonate-Group 请求头
  │     使用主节点 kubeconfig 转发到 localhost:6443
  │
  └── 响应返回
        透明转发 K8S API Server 的响应

Desktop 侧实现：

  ├── 魔法 DNS 劫持
  │     *.beagle → 127.1.x.x（VIP 地址）
  │
  ├── 本地代理监听 VIP
  │     127.1.x.x:6443 → 监听 K8S API 请求
  │
  ├── 查询域名注册表
  │     kubernetes.beijing.beagle → target_ip=100.64.0.23, target_port=50050
  │
  ├── tsnet Dial 转发
  │     通过 Tailscale 隧道连接 Agent（100.64.0.23:50050）
  │     桥接请求和响应
  │
  └── kubeconfig 生成
        server: https://kubernetes.beijing.beagle:6443
        insecure-skip-tls-verify: true
```

### Impersonation 机制

```
Desktop kubectl 请求到达 Agent：

  1. Agent 从 tsnet 连接提取身份 → zhangsan
  2. 查询 AclK8sPermission → namespace: yygl, role: developer
  3. 构造 Impersonation 请求头
       Impersonate-User: zhangsan
       Impersonate-Group: developer
  4. 使用主节点 kubeconfig 转发到 K8S API Server
       （localhost:6443，管理员权限执行 Impersonation）
  5. 返回结果给 Desktop
```

### 权限控制

```
AclK8sUserPermission:
  agent_user_id   → 哪个 Agent（哪个集群）
  user_id         → 被授权用户
  namespaces      → 允许的命名空间列表（"*" = 全部）
  k8s_role        → Impersonation 使用的 K8S 角色

AclK8sGroupPermission:
  agent_user_id   → 哪个 Agent
  group_id        → 被授权分组
  namespaces      → 允许的命名空间列表
  k8s_role        → K8S 角色
```

### 域名

```
kubernetes.<agent-name>.beagle:6443
kubernetes.beijing.beagle:6443
```

### 为什么必须走域名 + 魔法 DNS

Desktop 不能用 127.0.0.1:6443，必须用 kubernetes.beijing.beagle:6443。原因：

1. kubectl 需要稳定的域名地址，不能是 127.0.0.1（多集群会冲突）
2. 魔法 DNS 将域名解析为 VIP（127.1.x.x），每个集群独立的 VIP
3. 本地代理监听 VIP:6443，通过域名查询注册表获取 Agent 的 target_ip 和 target_port
4. 统一的域名体系，SSH、K8S API、K8S Service 都走相同的魔法 DNS 机制

### 认证机制详解

AgentK8SAPI 的认证完全依赖 Tailscale 身份，不验证 kubeconfig 中的 token。

#### kubeconfig 中的 token

Desktop 生成的 kubeconfig 必须包含 token 字段（kubectl 要求），但 token 的值可以是任意字符串：

```
users:
- name: beijing-user
  user:
    token: "whoisyourdaddy"  # 必须提供，但值任意
```

**为什么必须提供 token**：

- kubectl 是原生的 K8S 客户端工具
- kubectl 要求 kubeconfig 必须包含认证信息（token/cert/username+password）
- 如果不提供 token，kubectl 会报错拒绝执行
- 所以必须提供一个 token，但这个 token 的值不重要

**Agent 不验证 token**：

- Agent 从 tsnet 连接中提取 Tailscale 用户身份（如 `tag:client-zhangsan`）
- Agent 根据 Tailscale 身份查询 AclK8sPermission（本地缓存）
- Agent 完全忽略 HTTP 请求中的 Authorization 头（即 kubeconfig 中的 token）

#### 认证流程

```
Desktop kubectl 请求
  ↓
kubeconfig:
  server: https://kubernetes.beijing.beagle:6443
  token: "whoisyourdaddy"  ← kubectl 要求必须有，但 Agent 不验证
  ↓
魔法 DNS: kubernetes.beijing.beagle → 127.1.0.3 (VIP)
  ↓
本地代理: 127.1.0.3:6443 监听
  ↓
tsnet Dial: 100.64.0.19:50050
  ↓ (Tailscale 隧道自动携带身份: tag:client-zhangsan)
  ↓
Agent 接收请求
  ↓
从 tsnet 连接提取身份: zhangsan  ← 真正的认证
  ↓
查询 AclK8sPermission (本地缓存):
  user_id=zhangsan → namespaces=["yygl"], k8s_role="developer"
  ↓
如果有权限，构造 Impersonation 请求:
  Impersonate-User: zhangsan
  Impersonate-Group: developer
  ↓
使用主节点 kubeconfig 转发到 localhost:6443
  ↓
K8S API Server 以 zhangsan 身份执行
```

#### 安全性说明

1. **Tailscale 身份不可伪造**：tsnet 连接的身份由 Tailscale 网络保证，无法伪造
2. **权限由 Server 下发**：AclK8sPermission 通过 Agent 心跳从 Server 同步，Agent 本地缓存
3. **K8S RBAC 仍然生效**：通过 Impersonation 机制，K8S 的 RBAC 规则仍然应用
4. **kubeconfig token 无关紧要**：即使 token 泄露，攻击者也无法绕过 Tailscale 身份认证

### Agent Kubeconfig 管理

Agent 需要使用主节点的 kubeconfig 凭证来访问 K8S API Server，以便执行 Impersonation 操作。

#### 设计原则

1. **标准化**：使用标准的 `~/.kube/config` 格式，不做个性化解析
2. **自动检测**：自动检测主机模式（kubeconfig）和 Pod 模式（ServiceAccount）
3. **优先级明确**：主机模式 > Pod 模式 > 匿名访问
4. **复用性**：Endpoint 的 K8S API 代理也使用相同的 Kubeconfig 管理逻辑

#### Kubeconfig 加载流程

```
Agent 收到 Server 下发的 k8s_enabled=true
  ↓
1. 尝试主机模式
   - 检查 ~/.kube/config 文件是否存在
   - 解析 current-context
   - 提取 API Server URL
   - 提取认证凭证（client-certificate-data + client-key-data）
   - 如果成功 → 使用主机模式
  ↓
2. 尝试 Pod 模式（主机模式失败时）
   - 检查 /var/run/secrets/kubernetes.io/serviceaccount/token
   - 检查环境变量 KUBERNETES_SERVICE_HOST 和 KUBERNETES_SERVICE_PORT
   - 如果成功 → 使用 Pod 模式
  ↓
3. 启动失败
   - 记录错误日志
   - 心跳上报失败原因给 Server
   - K8S API 代理不启动
```

#### 认证方式优先级

| 模式     | 认证方式                             | 适用场景       |
| -------- | ------------------------------------ | -------------- |
| 主机模式 | client-certificate + client-key      | 物理机、虚拟机 |
| Pod 模式 | ServiceAccount token                 | K8S Pod 内部   |
| 匿名访问 | 无认证（仅用于测试，生产环境会 403） | 开发测试       |

#### Kubeconfig 解析规则

使用标准的 `k8s.io/client-go` 库解析 kubeconfig，支持所有标准字段和认证方式。

**解析流程：**

1. 使用 `clientcmd.BuildConfigFromFlags()` 或 `clientcmd.NewNonInteractiveDeferredLoadingClientConfig()` 加载 kubeconfig
2. 自动解析 `current-context` 并提取配置
3. 自动处理所有认证方式：
   - `client-certificate-data` + `client-key-data`（base64 编码的证书）
   - `client-certificate` + `client-key`（文件路径引用）
   - `token`（Bearer token 认证）
   - `username` + `password`（基本认证）
   - `exec`（外部命令认证，如 aws-iam-authenticator）
4. 自动处理 CA 证书：
   - `certificate-authority-data`（base64 编码）
   - `certificate-authority`（文件路径）
   - `insecure-skip-tls-verify`（跳过证书验证）
5. 构造 `rest.Config` 用于创建 K8S 客户端

**优势：**

- 完全兼容标准 kubeconfig 格式
- 支持所有 kubectl 支持的认证方式
- 自动处理证书轮换、token 刷新等复杂场景
- 无需手动解析和维护

#### 心跳上报

Agent 在心跳时应该上报 K8S API 代理的状态：

```protobuf
message AgentHeartbeatRequest {
  // ... 其他字段

  // K8S API 代理状态
  optional K8SAPIStatus k8s_api_status = 10;
}

message K8SAPIStatus {
  bool enabled = 1;              // 是否启用
  string mode = 2;               // "host" / "pod" / "anonymous"
  string api_server = 3;         // API Server 地址
  bool auth_configured = 4;      // 是否配置了认证凭证
  string error = 5;              // 启动失败的错误信息（如果有）
}
```

**上报示例：**

- 主机模式成功：`mode=host, api_server=https://192.168.1.242:6443, auth_configured=true`
- Pod 模式成功：`mode=pod, api_server=https://10.96.0.1:443, auth_configured=true`
- 启动失败：`enabled=false, error="kubeconfig not found: ~/.kube/config"`

#### 实现建议

**新增文件：** `internal/agent/kubeconfig_manager.go`

**核心结构：**

```
KubeconfigManager
  - restConfig *rest.Config      // client-go 的标准配置对象
  - apiServerURL string           // API Server 地址
  - mode string                   // "host" / "pod"

方法：
  - NewKubeconfigManager() (*KubeconfigManager, error)
  - GetRESTConfig() *rest.Config
  - GetAPIServerURL() string
  - GetMode() string
  - GetHTTPClient() (*http.Client, error)
```

**使用方式：**

```
K8SAPIProxy 初始化时：
  1. 创建 KubeconfigManager 实例
  2. 调用 GetRESTConfig() 获取 rest.Config
  3. 使用 rest.Config 创建 HTTP 客户端（包含所有认证信息）
  4. 在 ReverseProxy 中使用该 HTTP 客户端

优势：
  - client-go 自动处理所有认证方式
  - 自动处理证书轮换、token 刷新
  - 完全兼容标准 kubeconfig
```

**使用方式：**

```
K8SAPIProxy 初始化时：
  1. 创建 KubeconfigManager 实例
  2. 调用 GetHTTPClient() 获取配置好认证的 HTTP 客户端
  3. 在 ReverseProxy 的 Transport 中使用该客户端的 Transport

client-go 自动处理：
  - 所有认证方式（cert、token、exec 等）
  - 证书轮换和 token 刷新
  - TLS 配置和 CA 验证
```

#### 错误处理

| 错误场景               | 处理方式                                 |
| ---------------------- | ---------------------------------------- |
| kubeconfig 文件不存在  | 尝试 Pod 模式，失败则报错                |
| kubeconfig 解析失败    | 报错，K8S API 代理启动失败               |
| API Server 连接失败    | 警告，但不阻止启动（可能是网络暂时不通） |
| Impersonation 权限不足 | 运行时错误，返回 403 给 Desktop          |

#### kubeconfig 完整示例

```yaml
apiVersion: v1
kind: Config
clusters:
  - cluster:
      insecure-skip-tls-verify: true
      server: https://kubernetes.beijing.beagle:6443
    name: beijing
contexts:
  - context:
      cluster: beijing
      user: beijing-user
    name: beijing
current-context: beijing
users:
  - name: beijing-user
    user:
      token: "whoisyourdaddy" # 必须提供，值任意（Agent 不验证）
```

## AgentK8SService — Agent 本机 K8S SVC 代理

Agent 部署在 K8S 集群中时，通过 K8S API 监听 Service 资源变更，自动发现带有特定标签的 Service，并通过 tsnet gRPC 代理暴露。

AgentK8SService 和 AgentService（原 ProxyService）共存不冲突。AgentService 走 tsnet 独立端口（手动配置），AgentK8SService 走 tsnet gRPC 代理（自动发现）。

### 能力控制

AgentK8SService 能力由 Server 远程控制，通过心跳下发 `k8s_service_enabled` 字段：

```
心跳响应:
  k8s_service_enabled: true   → Agent 启动 K8S Service 自动发现
  k8s_service_enabled: false  → Agent 不启动 K8S Service 自动发现
```

Agent 本地不需要任何 K8S Service 相关配置。标签选择器和命名空间过滤由 Server 控制，通过心跳下发。

### Service 自动发现

```
Agent 启动时：
  如果收到 k8s_service_enabled = true
    → 启动 K8S Service Informer（Watch 带标签的 Service）
    → 发现 Service 后注册到 gRPC 代理
    → 收到 gRPC SVCProxy 请求时从 tsnet 提取身份
    → 查询 AclK8SServicePermission（心跳同步的缓存）
    → 透明转发到 K8S ClusterIP
```

### 标签约定

K8S 管理员通过给 Service 添加标签来控制暴露。标签使用 `signal.beagle.io` 域名前缀，符合 K8S 标签规范，避免与其他项目冲突：

| 标签                    | 值     | 说明                   |
| ----------------------- | ------ | ---------------------- |
| signal.beagle.io/expose | "true" | 标记为可通过隧道访问   |
| signal.beagle.io/alias  | 自定义 | 自定义域名前缀（可选） |

### 发现流程

```
K8S Service 变更事件
  ├── Add: postgresql.yygl (5432, signal.beagle.io/expose=true)
  │     → tsnet 监听 :5432
  │     → 上报 Server（心跳）
  │     → 域名注册: pg.yygl.beijing.beagle:5432
  │
  ├── Update: 标签变更
  │     → 更新监听和上报
  │
  └── Delete: postgresql.yygl
        → 关闭监听
        → 注销域名
```

### 域名生成规则

```
默认规则：
  <service_name>.<namespace>.<agent-name>.beagle

使用 alias：
  <signal.beagle.io/alias>.<namespace>.<agent-name>.beagle

示例：
  postgresql.yygl.beijing.beagle:5432     （默认）
  pg.yygl.beijing.beagle:5432             （signal.beagle.io/alias=pg）
```

### 端口冲突处理

同一 Agent 上可能有多个 Service 使用相同端口：

```
冲突场景：
  postgresql.yygl:5432
  postgresql.prod:5432

解决方案：
  AgentK8SService 走 gRPC 代理（SVCProxy RPC），不走独立端口
  通过 RPC 参数传递目标 Service 信息（namespace + service name）
  Desktop 侧用 VIP 隔离端口冲突：
    pg.yygl.beijing.beagle:5432   → VIP 127.1.0.1:5432 → gRPC SVCProxy(pg, yygl)
    pg.prod.beijing.beagle:5432   → VIP 127.1.0.2:5432 → gRPC SVCProxy(pg, prod)
```

## AgentService — Agent 手动端口映射

AgentService 是现有 ProxyService 的改名，保留用于非 K8S 场景。管理员通过 Server 配置端口映射，Agent 在 tsnet 上监听对应端口，透明转发到目标地址。

与 AgentK8SService 的区别：AgentService 是手动配置、走 tsnet 独立端口、Headscale ACL 端口级控制；AgentK8SService 是自动发现、走 gRPC 代理、应用层鉴权。

### 能力控制

AgentService 配置由 Server 管理，通过心跳下发：

```
心跳响应:
  services: [
    {
      name: "postgresql",
      local_addr: "192.168.1.100:5432",
      remote_port: 5432
    }
  ]
```

Agent 根据 Server 下发的配置启动相应的端口映射。

### 权限控制

```
AclServicePermission（现有，不变）：
  service_id → 哪个 AgentService
  user_id/group_id → 被授权用户/分组

翻译成 Headscale ACL：
  { "action": "accept", "src": ["tag:client-xxx"], "dst": ["tag:agent-xxx:5432"] }
```

### 域名

```
AgentService 不参与域名体系（手动配置，用户直接用 Tailscale IP + 端口访问）。
未来可选：支持手动绑定域名。
```

## Agent gRPC Server

Agent 需要两个 gRPC 接口，用于对接 Desktop 和 Endpoint：

```
Agent 进程
  │
  ├── tsnet gRPC Server（面向 Desktop）
  │     监听在 Tailscale 网络上
  │     身份来自 tsnet 连接
  │     │
  │     ├── SSHJump RPC     → 桥接到 EndpointSSH
  │     ├── K8sAPIProxy RPC → 桥接到 EndpointK8SAPI
  │     └── SVCProxy RPC    → 桥接到 EndpointK8SService
  │
  └── 内网 gRPC Server（面向 Endpoint）
        监听在内网地址上（如 0.0.0.0:50052）
        Endpoint 主动连接并注册
        │
        ├── RegisterEndpoint RPC  → Endpoint 注册自己
        ├── Heartbeat RPC         → Endpoint 心跳保活
        └── OpenShell RPC         → Agent 指令 Endpoint 开启 shell 会话（gRPC 双向流）
```

### Endpoint SSH 身份中继机制

Agent 作为 Endpoint SSH 的身份中继，实现与 tailssh 等价的零信任体验。

核心思路：tailssh 通过 Tailscale WhoIs 确认身份 + ACL 授权 + 内置 SSH Server。
Endpoint 无法加入 Tailscale 网络，因此由 Agent 完成身份确认和授权，
然后通过已有的 gRPC 连接直接指令 Endpoint 开启 shell 会话。

不需要额外端口、不需要内置 SSH Server、不需要签名密钥——
Endpoint 与 Agent 之间已有经过 token 认证的 gRPC 长连接，这就是信任通道。

```
身份中继流程：

  Desktop SSH 请求到达 Agent（通过 Tailscale 隧道）
    │
    ├── 1. Agent 通过 tsnet WhoIs 确认来源身份
    │       对端 IP: 100.64.1.1
    │       用户: zhangsan
    │       节点: desktop-zhangsan-macbook
    │
    ├── 2. Agent 查询权限（本地缓存）
    │       zhangsan 能否访问 EndpointSSH "web-server-1"？
    │       允许的 ssh_users: ["root", "deploy"]
    │       请求的 login: deploy → 在允许列表中 → 放行
    │
    ├── 3. Agent 通过已有 gRPC 连接向 Endpoint 发送 OpenShell 指令
    │       参数: login=deploy
    │       Endpoint 收到后以 deploy 用户 spawn shell（PTY）
    │
    └── 4. 双向桥接
            Desktop SSH 客户端 ←tsnet→ Agent ←gRPC stream→ Endpoint shell
```

### 信任模型

```
不需要签名密钥或身份 Token，原因：

  Endpoint → Agent 的 gRPC 连接已经过 endpoint_token 认证
  Agent 是唯一能向 Endpoint 发送 OpenShell 指令的实体
  Agent 在发送指令前已完成 WhoIs 身份确认 + ACL 权限检查
  Endpoint 只需信任来自 Agent 的指令即可

  信任链：
    Desktop 身份 → Tailscale WhoIs（Agent 验证）
    Agent → Endpoint 指令 → gRPC 认证连接（endpoint_token）
    整条链路不需要额外的密钥或证书
```

### Desktop 发起跳跃的完整流程

```
Desktop 用户执行: ssh deploy@web-server-1.beijing.beagle

  1. ProxyCommand 拦截 *.beagle 域名
     → DialSocket → tsnet 隧道 → 到达 Agent

  2. Agent 根据域名判断请求类型
       web-server-1 匹配某个 Endpoint 名称 → Endpoint SSH 代理
       （如果匹配 Agent 自身设备名 → 走 tailssh）

  3. Agent 从 tsnet 连接提取身份 → zhangsan

  4. Agent 查找 EndpointSSH "web-server-1"
       → 找到，状态 online（有活跃 gRPC 连接）

  5. Agent 查询 AclSSHJumpPermission
       → zhangsan 对 web-server-1 的权限
       → 允许，ssh_users: ["root", "deploy"]
       → deploy ∈ 允许列表 → 放行

  6. Agent 通过 gRPC 连接向 Endpoint 发送 OpenShell 指令
       → OpenShell(login: "deploy", rows: 24, cols: 80)

  7. Endpoint 收到指令
       → 检查 deploy 用户存在
       → 创建 PTY，以 deploy 用户 spawn shell
       → 通过 gRPC 双向流传输 shell I/O

  8. 双向桥接
       Desktop SSH 客户端 ←tsnet→ Agent ←gRPC stream→ Endpoint PTY

  9. 审计记录
       zhangsan → agent-beijing → web-server-1 → deploy → 成功
```

## 身份提取

Agent 从 tsnet 连接中提取对端身份，不需要 Identity Token：

```
连接进来
  │
  ├── 从 tsnet 连接中提取对端信息
  │     对端 IP: 100.64.1.1
  │     对端节点名: desktop-zhangsan-macbook
  │     对端用户: client-zhangsan
  │
  ├── 从用户名中提取身份
  │     client-zhangsan → User: zhangsan, Role: client
  │
  └── 根据身份查询权限（心跳同步的本地缓存）
```

对于 Endpoint SSH 场景，Agent 在确认身份和权限后，直接通过 gRPC 连接
向 Endpoint 发送 OpenShell 指令。Endpoint 不需要做身份验证，
因为 gRPC 连接本身已经过 endpoint_token 认证，Agent 是唯一的指令来源。

## 权限数据来源

Agent 通过 gRPC 心跳从 Server 获取权限数据：

```
心跳响应新增字段：
  k8s_permissions:          → 第 3 层，AgentK8SAPI 权限
  k8s_service_permissions:  → 第 3 层，AgentK8SService 权限
  ssh_endpoint_permissions: → Endpoint SSH 授权
  k8sapi_endpoint_permissions: → Endpoint K8SAPI 授权
  k8sservice_endpoint_permissions: → Endpoint K8S Service 授权

Agent 本地缓存，随心跳刷新（30 秒一次）。
```

## 对象关系总览

```
User (agent-beijing)
  │
  ├── AgentSSH（已有能力）— 本机 SSH
  │     通过 Server 下发 User.SSHEnabled + AclSSHPermission 控制
  │
  ├── AgentK8SAPI（新增能力）— 本机 K8S API 代理
  │     通过 Server 下发 k8s_enabled + AclK8sPermission 控制
  │
  ├── AgentK8SService（新增能力）— 本机 K8S SVC 代理
  │     通过 Server 下发 k8s_service_enabled + AclK8SServicePermission 控制
  │     自动发现 K8S Service，走 tsnet gRPC 代理
  │
  ├── EndpointSSH（新增对象）— 内网 SSH 跳跃端点
  │     ├── web-server-1  → 192.168.1.100 (online)
  │     ├── web-server-2  → 192.168.1.101 (online)
  │     └── db-server     → 192.168.1.200 (offline)
  │     通过 AclSSHJumpPermission 控制
  │
  ├── EndpointK8SAPI（新增对象）— 内网 K8S API 跳跃端点
  │     └── beijing-prod  → 192.168.1.10 (online)
  │     通过 AclK8SAPIJumpPermission 控制
  │
  └── EndpointK8SService（新增对象）— 内网 K8S SVC 跳跃端点
        └── remote-cluster  → (online, 发现 12 个 Service)
        通过 AclK8SServiceJumpPermission 控制
```

## 能力控制总结

所有 Agent 能力都由 Server 远程控制，通过心跳下发：

```
心跳响应字段：
  ssh_enabled: bool              → AgentSSH 是否启用
  k8s_enabled: bool              → AgentK8SAPI 是否启用
  k8s_service_enabled: bool      → AgentK8SService 是否启用
  endpoint_server_enabled: bool  → Endpoint gRPC Server 是否启用
  endpoint_server_listen: string → Endpoint gRPC Server 监听地址（如 "0.0.0.0:50052"）

权限数据：
  ssh_permissions: []            → AgentSSH 权限
  k8s_permissions: []            → AgentK8SAPI 权限
  k8s_service_permissions: []    → AgentK8SService 权限
  ssh_endpoint_permissions: []   → Endpoint SSH 授权
  k8sapi_endpoint_permissions: [] → Endpoint K8SAPI 授权
  k8sservice_endpoint_permissions: [] → Endpoint K8S Service 授权
```

Agent 本地不需要任何业务配置，只需要：

- 自动检测环境（主机模式/Pod 模式）
- 加载凭证（~/.kube/config 或 ServiceAccount）
- 根据 Server 下发的配置启动相应能力

## 实现优先级

| 阶段 | 内容                                          | 依赖             |
| ---- | --------------------------------------------- | ---------------- |
| P1   | AgentK8SAPI — K8S API 代理 + Impersonation    | AclK8sPermission |
| P1   | AgentK8SService — Service 自动发现 + SVC 代理 | K8S RBAC         |
| P2   | Agent tsnet gRPC Server（面向 Desktop）       | Endpoint 体系    |
| P2   | Agent 内网 gRPC Server（面向 Endpoint）       | Endpoint 体系    |
| P2   | 心跳同步扩展（权限数据下发）                  | ACL 模型         |
