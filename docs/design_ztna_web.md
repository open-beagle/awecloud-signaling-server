# ZTNA Web 管理界面设计

## 概述

Web 管理界面在 ZTNA 架构中承担管理控制台角色。在现有 Agent/Client/Service 管理基础上，新增 Endpoint 管理、K8S 权限、域名管理、操作审计等功能模块。

## 与现有界面的关系

| 模块       | 当前状态                   | ZTNA 新增/变更                    |
| ---------- | -------------------------- | --------------------------------- |
| Agent 管理 | Agent 列表、详情、服务配置 | 新增能力展示（SSH/K8S/SVC）       |
| Endpoint   | 无                         | 新增 Endpoint 管理（SSH/K8S/SVC） |
| 用户管理   | 用户列表、启用/禁用        | 不变                              |
| 服务管理   | 手动配置 ProxyService      | 新增 AgentSVC 自动发现视图        |
| 权限管理   | SSH ACL、服务权限          | 新增 K8S ACL + Endpoint 跳跃 ACL  |
| 域名管理   | 无                         | 域名注册表查看和管理              |
| 审计仪表盘 | 连接审计日志               | 操作级审计（直连 + 跳跃）         |

## 导航结构变更

```
现有导航：
  ├── Agent 管理
  ├── Client 管理
  ├── 服务管理
  ├── 分组管理
  ├── SSH 管理
  ├── 用户管理
  ├── 审计日志
  └── 系统设置

ZTNA 导航：
  ├── 资源总览（新增）          ← 仪表盘，展示所有资源状态
  ├── Agent 管理（增强）
  │     ├── Agent 列表
  │     └── Agent 详情（能力 + Endpoint）
  ├── Endpoint 管理（新增）
  │     ├── EndpointSSH
  │     ├── EndpointK8S
  │     └── EndpointSVC
  ├── 资源发现（新增）          ← AgentSVC 自动发现的 Service
  ├── 域名管理（新增）          ← 域名注册表
  ├── 用户管理
  ├── 权限管理（增强）
  │     ├── SSH 权限（已有）
  │     ├── K8S 权限（新增）
  │     ├── SVC 权限（已有，增强）
  │     └── Endpoint 跳跃权限（新增）
  ├── 审计中心（增强）
  │     ├── 连接日志（已有）
  │     └── 操作日志（新增）
  └── 系统设置
```

## Agent 管理增强

### Agent 详情页增强

```
Agent 详情页新增 Tab：

  ┌──────┬──────────┬──────────┬──────────┬──────────┐
  │ 基本 │ 能力配置 │ Endpoint │ 发现资源 │ 操作日志 │
  └──────┴──────────┴──────────┴──────────┴──────────┘

  "能力配置" Tab：
    展示 Agent 挂载的能力对象
    AgentSSH: 启用/禁用，SSH 用户列表
    AgentK8S: 启用/禁用，kubeconfig 路径，API Server 地址
    AgentSVC: 启用/禁用，标签选择器，命名空间过滤

  "Endpoint" Tab：
    展示连接到该 Agent 的 Endpoint 列表
    EndpointSSH: 名称、内网地址、状态
    EndpointK8S: 集群名、API 地址、状态
    EndpointSVC: 集群名、发现的 Service 数、状态

  "发现资源" Tab：
    展示 AgentSVC 自动发现的 K8S Service 列表
    名称、命名空间、端口、标签、域名、状态

  "操作日志" Tab：
    展示通过该 Agent 的操作审计记录
    SSH 直连/跳跃、K8S API 请求、SVC 连接
```

## Endpoint 管理页

### 功能

管理连接到 Agent 的 Endpoint（轻量 daemon）。按类型分 Tab 展示。

### EndpointSSH 列表

| 列        | 说明                             |
| --------- | -------------------------------- |
| 名称      | Endpoint 名称（如 web-server-1） |
| 别名      | 显示名称                         |
| 所属Agent | 连接的 Agent 名称                |
| 内网地址  | Endpoint 上报的内网 IP           |
| 域名      | 自动生成的域名                   |
| 状态      | online/offline                   |
| 操作      | 编辑、权限配置、禁用             |

### EndpointK8S 列表

| 列        | 说明                 |
| --------- | -------------------- |
| 集群名    | K8S 集群名称         |
| 别名      | 显示名称             |
| 所属Agent | 连接的 Agent 名称    |
| API 地址  | K8S API Server 地址  |
| 域名      | 自动生成的域名       |
| 状态      | online/offline       |
| 操作      | 编辑、权限配置、禁用 |

### EndpointSVC 列表

| 列        | 说明                               |
| --------- | ---------------------------------- |
| 集群名    | 远程集群名称                       |
| 别名      | 显示名称                           |
| 所属Agent | 连接的 Agent 名称                  |
| Service数 | 发现的 Service 数量                |
| 域名      | 自动生成的域名前缀                 |
| 状态      | online/offline                     |
| 操作      | 编辑、权限配置、禁用、查看 Service |

## 资源发现页

### 功能

展示所有 AgentSVC 和 EndpointSVC 自动发现的 K8S Service，支持搜索、过滤。

### 列表视图

| 列       | 说明                               |
| -------- | ---------------------------------- |
| 域名     | 完整域名（如 pg.yygl.beijing.k8s） |
| 来源     | AgentSVC / EndpointSVC             |
| 端口     | 服务端口列表                       |
| Agent    | 所属 Agent 名称                    |
| 命名空间 | K8S 命名空间                       |
| 状态     | 在线/离线                          |
| 操作     | 查看详情                           |

### 过滤条件

- 按 Agent 过滤
- 按来源过滤（AgentSVC / EndpointSVC）
- 按命名空间过滤
- 按状态过滤
- 关键词搜索（域名、名称）

## 权限管理增强

### K8S 权限（新增）

管理 AgentK8S 的访问权限（第 3 层）：

```
K8s 权限配置页面：

  ┌──────────────────────────────────────────────────────┐
  │ 规则列表                                              │
  ├──────────────────────────────────────────────────────┤
  │ Agent       │ 用户/分组   │ 命名空间    │ K8S 角色   │
  │ beijing     │ 张三        │ yygl        │ developer  │
  │ beijing     │ 运维组      │ *           │ admin      │
  │ shanghai    │ 上海团队    │ *           │ developer  │
  └──────────────────────────────────────────────────────┘
```

### Endpoint 跳跃权限（新增）

管理 Endpoint 跳跃的访问权限（第 4 层），按 Endpoint 类型分 Tab：

```
Endpoint 跳跃权限：

  ┌──────────────┬──────────────┬──────────────┐
  │ SSH 跳跃权限 │ K8S 跳跃权限 │ SVC 跳跃权限 │
  └──────────────┴──────────────┴──────────────┘

  SSH 跳跃权限：
    ┌──────────────────────────────────────────────────┐
    │ EndpointSSH     │ 用户/分组 │ Linux 用户         │
    │ web-server-1    │ 张三      │ root, deploy       │
    │ web-server-1    │ 开发组    │ deploy             │
    │ db-server       │ DBA 组    │ root               │
    └──────────────────────────────────────────────────┘

  K8S 跳跃权限：
    ┌──────────────────────────────────────────────────┐
    │ EndpointK8S     │ 用户/分组 │ 命名空间 │ K8S 角色│
    │ beijing-prod    │ 开发组    │ yygl     │ developer│
    │ beijing-prod    │ 运维组    │ *        │ admin    │
    └──────────────────────────────────────────────────┘

  SVC 跳跃权限：
    ┌──────────────────────────────────────────────────┐
    │ EndpointSVC     │ 用户/分组 │ Service 模式       │
    │ remote-cluster  │ 开发组    │ *.yygl             │
    │ remote-cluster  │ DBA 组    │ *                  │
    └──────────────────────────────────────────────────┘
```

## 审计中心增强

### 操作日志页

在现有连接日志基础上，新增操作级日志：

| 列    | 说明                                                                  |
| ----- | --------------------------------------------------------------------- |
| 时间  | 操作时间                                                              |
| 用户  | 操作用户                                                              |
| 类型  | ssh_direct / k8s_direct / svc_direct / ssh_jump / k8s_jump / svc_jump |
| Agent | 经过的 Agent                                                          |
| 目标  | 目标域名或 Endpoint 名称                                              |
| 详情  | 操作详情（命令摘要、API 路径等）                                      |
| 结果  | 成功 / 拦截 / 失败                                                    |

## 国际化

所有新增页面和组件需要同步更新中英文翻译文件：

- `web/src/locales/zh-CN.ts` — 中文翻译
- `web/src/locales/en-US.ts` — 英文翻译

翻译 key 命名规范：

```
ztna.endpoint.title        — Endpoint 管理
ztna.endpoint.ssh          — EndpointSSH
ztna.endpoint.k8s          — EndpointK8S
ztna.endpoint.svc          — EndpointSVC
ztna.resource.title        — 资源发现
ztna.domain.title          — 域名管理
ztna.acl.k8s               — K8S 权限
ztna.acl.jump              — 跳跃权限
ztna.audit.operations      — 操作日志
```

## 实现优先级

| 阶段 | 内容                  | 依赖               |
| ---- | --------------------- | ------------------ |
| P0   | Agent 能力展示        | Agent 上报能力信息 |
| P0   | 资源发现页面          | 资源发现 API       |
| P0   | 域名管理页面          | 域名注册表 API     |
| P1   | Endpoint 管理页面     | Endpoint 模型 API  |
| P1   | K8S 权限配置页面      | ACL 模型           |
| P1   | Endpoint 跳跃权限页面 | 跳跃 ACL 模型      |
| P2   | 操作级审计日志页面    | 操作审计 API       |
| P2   | 资源总览仪表盘        | 各模块数据汇总     |
