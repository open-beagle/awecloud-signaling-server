# ZTNA 实施计划

## 总览

基于 14 份 ZTNA 设计文档，按优先级分 4 个阶段实施。每个阶段内按组件（Server / Agent / Desktop / Endpoint / Web）拆分任务。

估算单位：人天（1 人全职开发）

## 阶段依赖关系

```
P0（基础设施）──▶ P1（K8S 能力）──▶ P2（Endpoint 体系）──▶ P3（远期）
```

---

## P0 — Desktop tsnet 化 + 域名体系（基础设施）✅ 已完成

目标：Desktop 去掉 tailscaled，改用 tsnet 用户态 + DNS 劫持 + VIP，建立域名体系。这是所有后续能力的基础。

### P0-1. Server：域名注册表 ✅

| 任务               | 说明                                                 | 估算 | 状态 |
| ------------------ | ---------------------------------------------------- | ---- | ---- |
| domain_registry 表 | 新增数据库表，GORM AutoMigrate                       | 0.5d | ✅   |
| 域名注册 API       | Agent 心跳上报时注册/更新域名                        | 1d   | ✅   |
| 域名查询 API       | GET /api/v1/client/dns/resolve，Desktop 查询域名路由 | 1d   | ✅   |
| 域名管理 API       | GET /api/v1/domains，列表查询（分页、筛选）          | 1d   | ✅   |

小计：3.5d（已完成）

### P0-2. Desktop.Host：tsnet 迁移 ✅

| 任务           | 说明                                                    | 估算 | 状态 |
| -------------- | ------------------------------------------------------- | ---- | ---- |
| tsnet 引擎     | 替换 tailscaled 进程管理，改为 tsnet.Server 内嵌        | 3d   | ✅   |
| 删除平台代码   | 删除 platform_darwin/linux/windows.go、embed_windows.go | 0.5d | ✅   |
| 连接流程重写   | app.go 中 tailscaled 启动/等待/连接 改为 tsnet 初始化   | 2d   | ✅   |
| DNS 劫持模块   | 本地 DNS 服务器（监听 15353），拦截 .beagle 域名        | 2d   | ✅   |
| macOS DNS 配置 | /etc/resolver/beagle 自动创建/清理                      | 1d   | ✅   |
| VIP 分配器     | 127.1.x.x 地址分配，域名 → VIP 映射表                   | 1.5d | ✅   |
| 本地代理       | VIP:端口 → tsnet Dial → Agent，按需启动/空闲关闭        | 2d   | ✅   |
| 域名体系集成   | 调用 Server 域名查询 API，DNS 查询触发域名解析          | 1d   | ✅   |

小计：13d（已完成，T8-T10 测试通过 2026-02-11）

### P0-3. Desktop.Pod：基础能力 ✅

| 任务                     | 说明                             | 估算 | 状态 |
| ------------------------ | -------------------------------- | ---- | ---- |
| signal_agent dial 子命令 | tsnet 隧道连接桥接 stdin/stdout  | 2d   | ✅   |
| ~/.ssh/config 自动维护   | Host \*.beagle ProxyCommand 配置 | 1d   | ✅   |

小计：3d（已完成，T6、T7 测试通过）

### P0-4. Agent：域名上报 ✅

| 任务                  | 说明                                                          | 估算 | 状态 |
| --------------------- | ------------------------------------------------------------- | ---- | ---- |
| AgentSSH 域名注册     | 心跳上报 <agent-name>.beagle 域名                             | 0.5d | ✅   |
| AgentService 域名注册 | 现有 ProxyService 改名 AgentService（代码层面可选，表名不变） | 0.5d | ✅   |

小计：1d（已完成）

### P0 合计：20.5d（全部完成，T1-T10 测试通过 2026-02-11）

---

## P1 — K8S 能力 + ACL 扩展

目标：Agent 新增 K8SAPI 和 K8SService 能力，Server 新增对应 ACL 模型和 API，Web 新增授权管理页面。

### P1-1. Server：K8S ACL 模型

| 任务                                     | 说明                                                       | 估算 |
| ---------------------------------------- | ---------------------------------------------------------- | ---- |
| acl*k8s*\*\_permission 表（2张）         | AgentK8SAPI 用户/分组授权表                                | 0.5d |
| acl*k8s_service*\*\_permission 表（2张） | AgentK8SService 用户/分组授权表                            | 0.5d |
| K8S API 授权 API                         | /api/v1/acl/k8s CRUD（列表、详情、添加、撤销）             | 2d   |
| K8S Service 授权 API                     | /api/v1/acl/k8s-service CRUD                               | 2d   |
| 心跳响应扩展                             | 心跳响应新增 k8s_permissions、k8s_service_permissions 字段 | 1.5d |
| 资源发现 API                             | GET /api/v1/client/resources，查询可访问资源               | 1.5d |
| K8SServiceDiscoveryCache                 | Server 内存缓存，Agent 心跳上报 K8S Service 发现数据       | 1d   |

小计：9d

### P1-2. Server：Endpoint 数据模型

| 任务                   | 说明                                  | 估算 |
| ---------------------- | ------------------------------------- | ---- |
| endpoint_ssh 表        | EndpointSSH 数据模型                  | 0.5d |
| endpoint_k8sapi 表     | EndpointK8SAPI 数据模型               | 0.5d |
| endpoint_k8sservice 表 | EndpointK8SService 数据模型           | 0.5d |
| Endpoint 管理 API      | /api/v1/endpoints/\* CRUD（三种类型） | 2d   |
| EndpointStatusCache    | Endpoint 在线状态缓存                 | 0.5d |

小计：4d

### P1-3. Agent：K8SAPI 能力

| 任务                  | 说明                                            | 估算 |
| --------------------- | ----------------------------------------------- | ---- |
| agent.toml [k8s] 配置 | 配置解析，k8s.enabled / kubeconfig / api_server | 0.5d |
| K8S API 代理          | tsnet 监听 K8SAPI 端口，Impersonation 转发      | 3d   |
| 身份提取              | 从 tsnet 连接提取对端身份                       | 1d   |
| 权限缓存              | 心跳同步 k8s_permissions，本地缓存 + 鉴权       | 1.5d |
| 域名注册              | 心跳上报 kubernetes.<agent-name>.beagle 域名    | 0.5d |

小计：6.5d

### P1-4. Agent：K8SService 能力

| 任务                  | 说明                                                        | 估算 |
| --------------------- | ----------------------------------------------------------- | ---- |
| agent.toml [svc] 配置 | 配置解析，svc.enabled / label_selector / namespaces         | 0.5d |
| K8S Service Informer  | Watch 带标签的 Service，自动发现                            | 2d   |
| gRPC SVCProxy         | tsnet gRPC 代理，通过 RPC 参数传递 namespace + service name | 2.5d |
| 心跳上报              | discovered_services 字段上报 Server                         | 1d   |
| 域名注册              | 心跳上报 <service>.<namespace>.<agent-name>.beagle 域名     | 0.5d |
| 权限缓存              | 心跳同步 k8s_service_permissions，本地鉴权                  | 1d   |

小计：7.5d

### P1-5. Desktop：K8S 访问支持

| 任务                 | 说明                                                         | 估算 |
| -------------------- | ------------------------------------------------------------ | ---- |
| K8SAPI 域名解析      | kubernetes.<agent>.beagle → VIP → tsnet → Agent K8SAPI 端口  | 1d   |
| K8SService gRPC 代理 | VIP → tsnet → Agent SVCProxy RPC（传递 namespace + service） | 2d   |
| 资源发现 UI（.Host） | 资源浏览页面，展示可访问资源，复制连接命令                   | 2d   |

小计：5d

### P1-6. Web：新增页面

| 任务                     | 说明                                           | 估算 |
| ------------------------ | ---------------------------------------------- | ---- |
| AuthGrantDialog 通用组件 | 穿梭框模式授权弹窗（用户/分组 + 授权参数）     | 3d   |
| K8S API 授权列表页       | /acl/k8s，Agent 列表 + 授权数统计              | 1.5d |
| K8S API 授权详情页       | /acl/k8s/:id，基本信息 + 用户/分组授权表       | 2d   |
| K8S Service 授权列表页   | /acl/k8s-service                               | 1.5d |
| K8S Service 授权详情页   | /acl/k8s-service/:id                           | 2d   |
| Endpoint 管理页面（3个） | /endpoints/ssh、/endpoints/k8s、/endpoints/svc | 3d   |
| 资源发现页面             | /resources，K8S Service 汇总视图               | 2d   |
| 域名管理页面             | /domains，域名注册表查看                       | 1.5d |
| 导航结构更新             | Sidebar 新增菜单项、路由配置                   | 0.5d |
| 面包屑统一               | 现有 6 个页面去掉返回按钮，改用面包屑          | 1d   |
| 搜索区域统一             | 现有 8 个页面搜索区域样式统一                  | 2d   |
| 国际化                   | zh-CN.ts / en-US.ts 新增翻译 key               | 1d   |

小计：21d

### P1 合计：53d

---

## P2 — Endpoint 体系 + 审计增强

目标：实现 Endpoint 跳跃能力，Agent 双 gRPC Server，signal_endpoint 二进制，操作级审计。

### P2-1. Server：Endpoint ACL

| 任务                          | 说明                                                   | 估算 |
| ----------------------------- | ------------------------------------------------------ | ---- |
| Endpoint 跳跃 ACL 表（4张）   | k8sapi_jump / k8sservice_jump × user/group             | 0.5d |
| Endpoint K8S API 授权 API     | /api/v1/acl/k8s 增强（Agent K8SAPI + Endpoint K8SAPI） | 1.5d |
| Endpoint K8S Service 授权 API | /api/v1/acl/k8s-service 增强                           | 1.5d |
| 心跳响应扩展                  | 新增 k8sapi/k8sservice_endpoint_permissions 字段       | 1d   |
| EndpointK8SServiceCache       | Endpoint 发现的 K8S Service 缓存                       | 1d   |

小计：5.5d

### P2-2. Server：操作级审计

| 任务                   | 说明                                      | 估算 |
| ---------------------- | ----------------------------------------- | ---- |
| operation_audit_log 表 | 新增操作级审计日志表                      | 0.5d |
| 审计上报 API           | Agent 上报操作审计记录                    | 1.5d |
| 审计查询 API           | /api/v1/audit-logs 增强，支持操作类型筛选 | 1d   |

小计：3d

### P2-3. Agent：双 gRPC Server

| 任务              | 说明                                                      | 估算 |
| ----------------- | --------------------------------------------------------- | ---- |
| tsnet gRPC Server | 面向 Desktop，监听 Tailscale 网络                         | 2d   |
| SSHJump RPC       | Desktop → Agent → EndpointSSH 桥接                        | 2.5d |
| K8sAPIProxy RPC   | Desktop → Agent → EndpointK8SAPI 桥接                     | 2d   |
| SVCProxy RPC 扩展 | 支持 Endpoint 跳跃（除 Agent 直连外）                     | 1.5d |
| 内网 gRPC Server  | 面向 Endpoint，监听内网地址                               | 1.5d |
| Endpoint 注册管理 | RegisterEndpoint RPC + 心跳保活 + 连接池                  | 2d   |
| 反向指令流        | Agent 下发 OpenShell / K8sAPIProxy / SVCProxy 到 Endpoint | 2d   |
| 权限缓存扩展      | 心跳同步 Endpoint 授权数据                                | 1d   |
| 审计记录上报      | 所有操作记录审计日志，上报 Server                         | 1.5d |

小计：16d

### P2-4. Endpoint：signal_endpoint 二进制

| 任务               | 说明                                        | 估算 |
| ------------------ | ------------------------------------------- | ---- |
| cmd/endpoint 入口  | main.go，配置解析（endpoint.toml）          | 1d   |
| 连接框架           | 反向连接 Agent 内网 gRPC，注册 + 心跳保活   | 2d   |
| EndpointSSH        | SSH 会话桥接（OpenShell → 本地 shell）      | 3d   |
| EndpointK8SAPI     | K8S API Impersonation 代理                  | 2.5d |
| EndpointK8SService | K8S Service Informer + SVC 代理             | 2.5d |
| 构建脚本           | scripts/build.sh 扩展，支持 signal_endpoint | 0.5d |

小计：11.5d

### P2-5. Desktop：Endpoint 跳跃

| 任务                       | 说明                                               | 估算 |
| -------------------------- | -------------------------------------------------- | ---- |
| SSH 跳跃                   | Endpoint SSH 域名解析 → Agent gRPC SSHJump         | 1.5d |
| K8SAPI 跳跃                | Endpoint K8SAPI 域名解析 → Agent gRPC K8sAPIProxy  | 1.5d |
| K8SService 跳跃            | Endpoint K8SService 域名解析 → Agent gRPC SVCProxy | 1.5d |
| Desktop.Pod DNS 劫持       | /etc/resolv.conf 指向本地 DNS                      | 1.5d |
| Desktop.Pod VIP + 本地代理 | 127.1.x.x 分配 + 代理                              | 1d   |

小计：7d

### P2-6. Desktop.Host：平台适配 ✅ Windows 已完成

| 任务             | 说明                                 | 估算 | 状态 |
| ---------------- | ------------------------------------ | ---- | ---- |
| Linux DNS 劫持   | systemd-resolved 或 /etc/resolv.conf | 1.5d | ⏳   |
| Windows DNS 劫持 | NRPT 或网络适配器 DNS                | 2d   | ✅   |
| 连接状态监控     | 本地代理连接状态展示                 | 1d   | ⏳   |

小计：4.5d（Windows 已完成，Linux 待实现）

### P2-7. Desktop.Pod：K8S 集成

| 任务                | 说明                                        | 估算 |
| ------------------- | ------------------------------------------- | ---- |
| kubeconfig 自动配置 | 从 Server 获取集群列表，生成 ~/.kube/config | 1.5d |

小计：1.5d

### P2-8. Web：Endpoint 授权 + 审计增强

| 任务                     | 说明                                                | 估算 |
| ------------------------ | --------------------------------------------------- | ---- |
| K8S API 授权页面增强     | /acl/k8s 增加 Endpoint K8SAPI 授权 Tab              | 2d   |
| K8S Service 授权页面增强 | /acl/k8s-service 增加 Endpoint K8S Service 授权 Tab | 2d   |
| 审计日志增强             | 操作类型筛选、Agent/Endpoint 列、详情列             | 2d   |
| 国际化补充               | Endpoint 授权 + 审计相关翻译                        | 0.5d |

国际化补充 | Endpoint 授权 + 审计相关翻译 | 0.5d |

小计：6.5d

### P2 合计：54d

---

## P3 — 远期目标

| 任务             | 说明                            | 估算   |
| ---------------- | ------------------------------- | ------ |
| AI 安全审计      | SSH 指令拦截、会话录像、AI 分析 | 待评估 |
| Vaultwarden 集成 | 凭据管理集成                    | 待评估 |

---

## 总估算

| 阶段 | 内容                           | 估算   |
| ---- | ------------------------------ | ------ |
| P0   | Desktop tsnet 化 + 域名体系    | 20.5d  |
| P1   | K8S 能力 + ACL 扩展 + Web 页面 | 53d    |
| P2   | Endpoint 体系 + 审计增强       | 54d    |
| P3   | AI 审计 + Vaultwarden          | 待评估 |
| 合计 | P0-P2                          | 127.5d |

## 里程碑

| 里程碑 | 完成标志                                                     | 累计      | 状态          |
| ------ | ------------------------------------------------------------ | --------- | ------------- |
| M1     | Desktop.Host 可通过域名 SSH 到 Agent（tsnet + DNS + VIP）    | P0 完成   | ✅ 2026-02-11 |
| M2     | kubectl 可通过域名访问 Agent K8S API（Impersonation）        | P1-3 完成 | ⏳            |
| M3     | psql 可通过域名访问 Agent 自动发现的 K8S Service             | P1-4 完成 | ⏳            |
| M4     | Web 管理界面 K8S API 授权 + Endpoint 管理 + 资源发现页面上线 | P1 完成   | ⏳            |
| M5     | ssh 可通过 Agent 跳跃到内网 Endpoint                         | P2-4 完成 | ⏳            |
| M6     | 全链路操作审计可查                                           | P2 完成   | ⏳            |

## 关键技术风险

| 风险                   | 影响             | 缓解措施                                       |
| ---------------------- | ---------------- | ---------------------------------------------- |
| tsnet 稳定性           | P0 核心依赖      | 先在测试环境验证 tsnet 长时间运行稳定性        |
| macOS DNS 劫持权限     | P0 Desktop.Host  | /etc/resolver 需要 sudo，考虑安装时一次性配置  |
| K8S Impersonation RBAC | P1 AgentK8SAPI   | 需要 ClusterRole 绑定，文档化部署要求          |
| gRPC 双向流稳定性      | P2 Endpoint 桥接 | 心跳保活 + 自动重连 + 超时控制                 |
| 多平台 DNS 劫持        | P2 Windows/Linux | 各平台方案差异大，优先 macOS，其他平台逐步适配 |

## 数据库迁移顺序

| 阶段 | 新增表                                               | 数量 |
| ---- | ---------------------------------------------------- | ---- |
| P0   | domain_registry                                      | 1    |
| P1   | acl*k8s*_*permission (4), endpoint*_ (3)             | 7    |
| P2   | acl*\*\_jump*\*\_permission (4), operation_audit_log | 5    |
| 合计 |                                                      | 13   |

全部通过 GORM AutoMigrate 自动创建，现有 23 张表不做结构变更。

## Proto 变更

| 阶段 | 变更                                                                                                                                                                |
| ---- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| P0   | 心跳请求新增域名注册字段                                                                                                                                            |
| P1   | 心跳请求新增 discovered_services、心跳响应新增 k8s_permissions / k8s_service_permissions                                                                            |
| P2   | 新增 Agent tsnet gRPC proto（SSHJump / K8sAPIProxy / SVCProxy），新增 Endpoint gRPC proto（Register / Heartbeat / OpenShell 等），心跳响应新增 endpoint_permissions |
