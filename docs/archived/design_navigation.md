# 导航条设计

## 概述

本文档定义 Web 管理界面的导航结构，包括侧边栏菜单和面包屑导航的规范。

## 功能模块说明

### 服务授权（Service Auth）

管理 Agent 提供的服务（SSH、MySQL、Redis 等）的访问权限。

**子模块**：

- 桌面授权：管理 Desktop 客户端对服务的访问权限
- 代理授权：管理 Agent 之间的服务访问权限

### 代理授权（Agent Auth）

管理 Desktop/Agent 对 Agent 本身的访问权限（不是服务）。

**子模块**：

- 桌面授权：管理 Desktop 客户端对 Agent 的访问权限
- 代理授权：管理 Agent 之间的访问权限

### SSH 管理

管理 SSH 访问权限，配置哪些用户/分组可以 SSH 到哪些 Agent。

**页面**：

- 列表页：显示所有启用 SSH 的 Agent
- 详情页：显示特定 Agent 的 SSH 授权详情

### 隧道管理

管理 Tailscale/Headscale 隧道配置。

**子模块**：

- User 管理：管理隧道用户
- Node 管理：管理隧道节点
- ACL 管理：管理访问控制列表
- SSH 策略：管理隧道层面的 SSH 策略（与应用层 SSH 管理不同）

## 导航结构

### 侧边栏菜单层级

```
├── 代理管理（Agents）
├── 服务授权（Service Auth）
│   ├── 桌面授权（Desktop Auth）
│   └── 代理授权（Agent Auth）
├── 代理授权（Agent Auth）
│   ├── 桌面授权（Desktop Auth）
│   └── 代理授权（Agent Auth）
├── 客户管理（Clients）
├── 分组管理（Groups）
│   ├── 用户分组（Client Groups）
│   └── 代理分组（Agent Groups）
├── SSH 管理（SSH Management）
├── 隧道管理（Tunnel）
│   ├── User 管理（Users）
│   ├── Node 管理（Nodes）
│   ├── ACL 管理（ACL）
│   └── SSH 策略（SSH Policy）
├── 审计日志（Audit Logs）
└── 系统配置（System Config）
```

### 路由路径映射

| 菜单项                     | 路由路径                      | 说明                |
| -------------------------- | ----------------------------- | ------------------- |
| 代理管理                   | `/agents`                     | Agent 列表页        |
| 代理管理 > 详情            | `/agents/:id`                 | Agent 详情页        |
| 服务授权 > 桌面授权        | `/service-auth/desktop`       | 服务的桌面授权      |
| 服务授权 > 代理授权        | `/service-auth/agent`         | 服务的代理授权      |
| 代理授权 > 桌面授权        | `/agent-auth/desktop`         | Agent 的桌面授权    |
| 代理授权 > 代理授权        | `/agent-auth/agent`           | Agent 的代理授权    |
| 客户管理                   | `/clients`                    | Client 列表页       |
| 客户管理 > 详情            | `/clients/:id`                | Client 详情页       |
| 分组管理 > 用户分组        | `/groups/clients`             | 用户分组列表        |
| 分组管理 > 用户分组 > 成员 | `/groups/clients/:id/members` | 分组成员管理        |
| 分组管理 > 代理分组        | `/groups/agents`              | 代理分组列表        |
| 分组管理 > 代理分组 > 成员 | `/groups/agents/:id/members`  | 分组成员管理        |
| SSH 管理                   | `/ssh`                        | SSH 授权列表        |
| SSH 管理 > 详情            | `/ssh/:id`                    | Agent SSH 授权详情  |
| 隧道管理 > User 管理       | `/tunnel/users`               | Headscale User 管理 |
| 隧道管理 > Node 管理       | `/tunnel/nodes`               | Headscale Node 管理 |
| 隧道管理 > ACL 管理        | `/tunnel/acl`                 | Headscale ACL 管理  |
| 隧道管理 > SSH 策略        | `/tunnel/ssh`                 | Headscale SSH 策略  |
| 审计日志                   | `/audit-logs`                 | 连接审计日志        |
| 系统配置                   | `/system/config`              | 系统配置管理        |

## 面包屑导航规范

### 基本规则

1. 面包屑显示当前页面的层级路径
2. 除最后一项外，其他项均可点击跳转
3. 最后一项为当前页面，不可点击
4. 使用 `/` 作为分隔符

### 面包屑路径定义

#### 代理管理

```
代理管理                           (/agents)
代理管理 > Agent 详情: {name}      (/agents/:id)
```

#### 服务授权

```
服务授权 > 桌面授权                (/service-auth/desktop)
服务授权 > 代理授权                (/service-auth/agent)
```

#### 代理授权

```
代理授权 > 桌面授权                (/agent-auth/desktop)
代理授权 > 代理授权                (/agent-auth/agent)
```

#### 客户管理

```
客户管理                           (/clients)
客户管理 > 客户详情: {name}        (/clients/:id)
```

#### 分组管理

```
分组管理 > 用户分组                (/groups/clients)
分组管理 > 用户分组 > 分组成员: {name}  (/groups/clients/:id/members)
分组管理 > 代理分组                (/groups/agents)
分组管理 > 代理分组 > 分组成员: {name}  (/groups/agents/:id/members)
```

#### SSH 管理

```
SSH 管理                           (/ssh)
SSH 管理 > Agent 详情: {name}      (/ssh/:id)
```

#### 隧道管理

```
隧道管理 > User 管理               (/tunnel/users)
隧道管理 > Node 管理               (/tunnel/nodes)
隧道管理 > ACL 管理                (/tunnel/acl)
隧道管理 > SSH 策略                (/tunnel/ssh)
```

#### 其他

```
审计日志                           (/audit-logs)
系统配置                           (/system/config)
客户端下载                         (/download)
```

### 动态名称获取

对于包含动态名称的面包屑（如 Agent 详情、客户详情等），名称获取优先级：

1. 路由 meta 中的 breadcrumbName
2. 路由 query 参数中的 name
3. 使用 ID 作为后备：`#{id}`

示例：

```
优先级 1: route.meta.breadcrumbName
优先级 2: route.query.name
优先级 3: `#${route.params.id}`
```

## 国际化文本

### 菜单项翻译键

| 翻译键                  | 中文      | 英文           |
| ----------------------- | --------- | -------------- |
| menu.agents             | 代理管理  | Agents         |
| menu.serviceAuth        | 服务授权  | Service Auth   |
| menu.serviceAuthDesktop | 桌面授权  | Desktop Auth   |
| menu.serviceAuthAgent   | 代理授权  | Agent Auth     |
| menu.agentAuth          | 代理授权  | Agent Auth     |
| menu.agentAuthDesktop   | 桌面授权  | Desktop Auth   |
| menu.agentAuthAgent     | 代理授权  | Agent Auth     |
| menu.clients            | 客户管理  | Clients        |
| menu.groups             | 分组管理  | Groups         |
| menu.clientGroups       | 用户分组  | Client Groups  |
| menu.agentGroups        | 代理分组  | Agent Groups   |
| menu.ssh                | SSH 管理  | SSH Management |
| menu.tunnel             | 隧道管理  | Tunnel         |
| menu.tunnelUsers        | User 管理 | Users          |
| menu.tunnelNodes        | Node 管理 | Nodes          |
| menu.tunnelACL          | ACL 管理  | ACL            |
| menu.tunnelSSH          | SSH 策略  | SSH Policy     |
| menu.auditLogs          | 审计日志  | Audit Logs     |
| menu.systemConfig       | 系统配置  | System Config  |

### 面包屑翻译键

| 翻译键                  | 中文       | 英文          |
| ----------------------- | ---------- | ------------- |
| common.home             | 首页       | Home          |
| breadcrumb.agentDetail  | Agent 详情 | Agent Detail  |
| breadcrumb.clientDetail | 客户详情   | Client Detail |
| breadcrumb.groupMembers | 分组成员   | Group Members |

## 注意事项

### 命名冲突处理

"代理授权"在两个地方出现：

1. 服务授权的子菜单：指服务的代理授权
2. 独立菜单项：指 Agent 本身的代理授权

通过上下文（父菜单）区分，无需修改名称。

### SSH 管理 vs SSH 策略

- SSH 管理：应用层面的 SSH 访问权限管理
- SSH 策略：隧道层面的 SSH 策略配置（Headscale）

两者功能不同，需要明确区分。

### 路由重定向

以下路由存在重定向：

- `/service-auth` → `/service-auth/desktop`
- `/agent-auth` → `/agent-auth/desktop`
- `/groups` → `/groups/clients`

面包屑需要处理重定向后的实际路径。

## 实现文件

### 前端组件

- `web/src/components/Layout/Sidebar.vue` - 侧边栏菜单
- `web/src/components/Common/Breadcrumb.vue` - 面包屑导航
- `web/src/router/index.ts` - 路由配置

### 国际化文件

- `web/src/locales/zh-CN.ts` - 中文翻译
- `web/src/locales/en-US.ts` - 英文翻译
