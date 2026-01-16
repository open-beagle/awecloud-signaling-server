---
inclusion: manual
---

# 面包屑导航规范

## 概述

本项目使用统一的面包屑导航组件 `web/src/components/Common/Breadcrumb.vue`，所有页面的面包屑都通过该组件自动生成。

## 核心原则

1. **统一实现**：所有页面使用统一的 Breadcrumb 组件，禁止在页面组件内部自己实现面包屑
2. **路由驱动**：面包屑根据路由路径自动生成，不依赖组件状态
3. **集中维护**：所有面包屑规则集中在 Breadcrumb 组件中维护

## 实现位置

**组件文件**：`web/src/components/Common/Breadcrumb.vue`

**使用位置**：在 `web/src/components/Layout/Layout.vue` 中统一引入，所有页面自动显示

## 添加新路由的面包屑

当添加新路由时，需要在 Breadcrumb 组件的 `items` computed 属性中添加对应的匹配规则。

### 规则结构

每个路由匹配规则包含：

- 路径匹配条件（使用 `if/else if` 或正则匹配）
- 面包屑项数组（包含标题和可选的跳转路径）

### 面包屑项格式

```typescript
interface BreadcrumbItem {
  path?: string; // 可选，有则可点击跳转
  title: string; // 必填，显示文本
}
```

### 示例说明

**一级页面**（列表页）：

```
路由：/ssh
面包屑：[{ path: '/ssh', title: 'SSH 管理' }]
显示：SSH 管理
```

**二级页面**（子菜单）：

```
路由：/service-auth/desktop
面包屑：[
  { title: '服务授权' },           // 无 path，不可点击
  { path: '/service-auth/desktop', title: '桌面授权' }
]
显示：服务授权 > 桌面授权
```

**详情页**（带动态参数）：

```
路由：/ssh/:id
面包屑：[
  { path: '/ssh', title: 'SSH 管理' },
  { title: `Agent 详情: ${agentName}` }
]
显示：SSH 管理 > Agent 详情: agent-name
```

### 动态名称获取

对于详情页等需要显示动态名称的场景，按以下优先级获取：

1. `route.meta.breadcrumbName` - 路由 meta 中的名称
2. `route.query.name` - URL query 参数中的名称
3. `#${route.params.id}` - 使用 ID 作为后备

示例：

```typescript
const agentName =
  (route.meta.breadcrumbName as string) ||
  (route.query.name as string) ||
  `#${route.params.id}`;
```

## 注意事项

### 1. 禁止在页面组件中实现面包屑

❌ **错误做法**：

```vue
<!-- 在页面组件中自己实现面包屑 -->
<template>
  <div>
    <el-breadcrumb>
      <el-breadcrumb-item :to="{ path: '/' }">首页</el-breadcrumb-item>
      <el-breadcrumb-item>当前页</el-breadcrumb-item>
    </el-breadcrumb>
    <!-- 页面内容 -->
  </div>
</template>
```

✅ **正确做法**：

- 在 Breadcrumb.vue 中添加路由匹配规则
- 页面组件不需要任何面包屑相关代码
- 布局组件会自动显示面包屑

### 2. 路由匹配顺序

在 Breadcrumb 组件中，路由匹配按照 `if/else if` 顺序进行：

- 更具体的路由（如 `/ssh/:id`）应该放在前面
- 更通用的路由（如 `/ssh`）应该放在后面
- 使用正则匹配时注意精确性

### 3. 国际化支持

面包屑文本应该使用硬编码的中文，不使用 i18n：

- 原因：Breadcrumb 组件是通用组件，不依赖特定页面的 i18n 配置
- 如需国际化，可以在组件中引入 `useI18n()` 并使用翻译键

### 4. 路由重定向处理

对于有重定向的路由（如 `/service-auth` → `/service-auth/desktop`）：

- 只需要为重定向后的实际路径添加面包屑规则
- 不需要为重定向路由本身添加规则

## 当前路由映射

参考 `docs/design_navigation.md` 中的完整路由映射表。

主要路由包括：

- `/agents` - 代理管理
- `/service-auth/*` - 服务授权
- `/agent-auth/*` - 代理授权
- `/clients` - 客户管理
- `/groups/*` - 分组管理
- `/ssh` - SSH 管理
- `/tunnel/*` - 隧道管理
- `/audit-logs` - 审计日志
- `/system/config` - 系统配置

## 修改流程

当需要添加新路由的面包屑时：

1. 在 `web/src/components/Common/Breadcrumb.vue` 中找到 `items` computed 属性
2. 在合适的位置添加新的 `else if` 分支
3. 定义面包屑项数组
4. 测试验证面包屑显示正确
5. 更新 `docs/design_navigation.md` 文档

## 参考文档

- `docs/design_navigation.md` - 导航条设计规范（包含完整的路由映射表）
- `web/src/router/index.ts` - 路由配置
- `web/src/components/Layout/Layout.vue` - 布局组件（面包屑使用位置）
