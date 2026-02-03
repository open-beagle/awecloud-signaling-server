# Web 管理界面设计方案

## 1. 概述

Web 管理界面是 AWECloud Signaling Server 的管理后台，供管理员管理 Agent、Client 和 STCP 实例。

### 1.1 技术栈

- **前端框架**: Vue 3 (Composition API)
- **UI 组件库**: Element Plus
- **路由**: Vue Router 4
- **状态管理**: Pinia
- **HTTP 客户端**: Axios
- **WebSocket**: 原生 WebSocket API
- **国际化**: vue-i18n
- **构建工具**: Vite
- **语言**: TypeScript

### 1.2 部署方式

前端打包后嵌入 Server，实现单一部署：

- 前端构建产物放在 `web/dist/`
- Server 通过静态文件服务提供前端页面
- 前后端通过 RESTful API 通信

## 2. 功能模块

### 2.1 登录模块

**路由**: `/login`

**功能**:

- 管理员登录（用户名+密码）
- 记住登录状态（JWT Token 存储在 localStorage）
- 登录失败提示

**界面元素**:

- 用户名输入框
- 密码输入框
- 登录按钮
- 系统标题和 Logo

### 2.2 仪表盘

**路由**: `/admin/dashboard`

**功能**:

- 系统概览统计
- Agent 在线状态
- 最近活动日志

**数据展示**:

- Agent 总数 / 在线数量
- Client 总数 / 启用数量
- STCP 实例总数
- Agent 列表（名称、状态、最后心跳时间）

### 2.3 Agent 管理

**路由**: `/admin/agents`

**功能**:

- Agent 列表展示
- 创建 Agent
- 删除 Agent
- 重新生成 Token
- 查看 Agent 详情

**列表字段**:

- ID
- Agent 名称
- 描述
- 状态（在线/离线）
- 创建时间
- 操作（查看 Token、删除）

**创建 Agent 表单**:

- Agent 名称（必填）
- 描述（可选）

**查看 Token 弹窗**:

- 显示 Agent Token
- 复制按钮
- 重新生成按钮

### 2.4 Client 管理

**路由**: `/admin/clients`

**功能**:

- Client 列表展示
- 创建 Client
- 启用/禁用 Client
- 删除 Client
- 重新生成 Secret
- 查看 Client 详情

**列表字段**:

- ID
- Client ID（用户名/邮箱）
- Client 名称
- 状态（启用/禁用）
- 创建时间
- 操作（查看 Secret、启用/禁用、删除）

**创建 Client 表单**:

- Client ID（必填，用户名或邮箱）
- Client 名称（必填）

**查看 Secret 弹窗**:

- 显示 Client Secret
- 复制按钮
- 重新生成按钮

### 2.5 STCP 实例管理

**路由**: `/admin/stcp-instances`

**功能**:

- STCP 实例列表展示
- 创建 STCP 实例
- 删除 STCP 实例
- 管理访问权限（授权/撤销 Client 访问）

**列表字段**:

- ID
- 实例名称
- 所属 Agent
- 本地地址（IP:Port）
- 描述
- 授权 Client 数量
- 创建时间
- 操作（管理权限、删除）

**创建 STCP 实例表单**:

- 选择 Agent（下拉框）
- 实例名称（必填）
- 本地 IP（必填）
- 本地端口（必填）
- 描述（可选）

**管理权限弹窗**:

- 已授权 Client 列表（可撤销）
- 未授权 Client 列表（可授权）

## 3. 界面布局

### 3.1 整体布局

采用经典的管理后台布局：

```
┌─────────────────────────────────────────┐
│  Header (顶部导航栏)                      │
│  - Logo + 系统名称                        │
│  - 用户信息 + 退出按钮                     │
├──────────┬──────────────────────────────┤
│          │                              │
│  Sidebar │  Main Content                │
│  (侧边栏) │  (主内容区)                   │
│          │                              │
│  - 仪表盘 │  - 面包屑导航                 │
│  - Agent │  - 页面标题                   │
│  - Client│  - 操作按钮                   │
│  - STCP  │  - 数据表格/表单              │
│          │                              │
│          │                              │
└──────────┴──────────────────────────────┘
```

### 3.2 颜色方案

参考图片的实际配色方案：

- **主色**: #4A90E2 (亮蓝色，主按钮和强调元素)
- **背景色**: #EFF3F8 (浅蓝灰色，页面背景)
- **卡片背景**: #FFFFFF (白色)
- **文字主色**: #2C3E50 (深灰色)
- **文字次色**: #606266 (中灰色)
- **文字辅助**: #909399 (浅灰色)
- **边框色**: #E4E7ED (浅灰色)
- **分割线**: #EBEEF5 (极浅灰)
- **成功色**: #67C23A (绿色，在线状态)
- **警告色**: #E6A23C (橙色)
- **危险色**: #F56C6C (红色，删除操作)
- **信息色**: #909399 (灰色，离线状态)

### 3.3 响应式设计

- 桌面端：侧边栏固定展开
- 平板端：侧边栏可折叠
- 移动端：侧边栏抽屉式

## 4. 前端架构

### 4.1 目录结构

```
web/
├── public/                 # 静态资源
│   └── favicon.ico
├── src/
│   ├── api/               # API接口
│   │   ├── admin.ts       # 管理员API
│   │   ├── agent.ts       # Agent API
│   │   └── stcp.ts        # STCP API
│   ├── assets/            # 资源文件
│   │   ├── logo.png
│   │   └── styles/
│   │       └── main.css
│   ├── components/        # 公共组件
│   │   ├── Layout/
│   │   │   ├── Header.vue
│   │   │   ├── Sidebar.vue
│   │   │   └── Layout.vue
│   │   └── Common/
│   │       ├── StatusTag.vue
│   │       └── CopyButton.vue
│   ├── locales/           # 国际化语言文件
│   │   ├── zh-CN.ts       # 中文
│   │   └── en-US.ts       # 英文
│   ├── router/            # 路由配置
│   │   └── index.ts
│   ├── stores/            # Pinia状态管理
│   │   ├── auth.ts        # 认证状态
│   │   ├── app.ts         # 应用状态
│   │   └── websocket.ts   # WebSocket状态
│   ├── types/             # TypeScript类型定义
│   │   ├── api.ts
│   │   └── models.ts
│   ├── utils/             # 工具函数
│   │   ├── request.ts     # Axios封装
│   │   ├── auth.ts        # 认证工具
│   │   └── websocket.ts   # WebSocket封装
│   ├── views/             # 页面组件
│   │   ├── Login.vue
│   │   ├── Dashboard.vue
│   │   ├── Agent/
│   │   │   ├── List.vue
│   │   │   └── components/
│   │   │       ├── CreateDialog.vue
│   │   │       └── TokenDialog.vue
│   │   └── STCP/
│   │       ├── List.vue
│   │       └── components/
│   │           ├── CreateDialog.vue
│   │           └── PermissionDialog.vue
│   ├── App.vue            # 根组件
│   └── main.ts            # 入口文件
├── index.html
├── package.json
├── tsconfig.json
├── vite.config.ts
└── README.md
```

### 4.2 路由设计

```typescript
const routes = [
  {
    path: "/login",
    name: "Login",
    component: () => import("@/views/Login.vue"),
  },
  {
    path: "/admin",
    component: Layout,
    redirect: "/admin/dashboard",
    meta: { requiresAuth: true },
    children: [
      {
        path: "dashboard",
        name: "Dashboard",
        component: () => import("@/views/Dashboard.vue"),
      },
      {
        path: "agents",
        name: "Agents",
        component: () => import("@/views/Agent/List.vue"),
      },
      {
        path: "clients",
        name: "Clients",
        component: () => import("@/views/Client/List.vue"),
      },
      {
        path: "stcp-instances",
        name: "STCPInstances",
        component: () => import("@/views/STCP/List.vue"),
      },
    ],
  },
];
```

### 4.3 状态管理

**认证状态 (auth.ts)**:

```typescript
export const useAuthStore = defineStore("auth", {
  state: () => ({
    token: localStorage.getItem("token") || "",
    username: "",
  }),
  actions: {
    async login(username: string, password: string) {
      // 调用登录API
      // 保存token
    },
    logout() {
      // 清除token
      // 跳转到登录页
    },
  },
});
```

**应用状态 (app.ts)**:

```typescript
export const useAppStore = defineStore("app", {
  state: () => ({
    sidebarCollapsed: false,
    loading: false,
  }),
});
```

### 4.4 API 封装

**Axios 实例配置**:

```typescript
const request = axios.create({
  baseURL: "/api",
  timeout: 10000,
});

// 请求拦截器：添加Token
request.interceptors.request.use((config) => {
  const token = localStorage.getItem("token");
  if (token) {
    config.headers.Authorization = `Bearer ${token}`;
  }
  return config;
});

// 响应拦截器：处理错误
request.interceptors.response.use(
  (response) => response.data,
  (error) => {
    if (error.response?.status === 401) {
      // 跳转到登录页
    }
    return Promise.reject(error);
  }
);
```

## 5. 核心页面设计

### 5.1 登录页

**布局**:

- 居中卡片式登录框
- 系统 Logo 和标题
- 用户名和密码输入框
- 登录按钮

**交互**:

- 表单验证（必填项）
- 登录成功跳转到仪表盘
- 登录失败显示错误提示

### 5.2 Agent 列表页

**顶部操作栏**:

- 创建 Agent 按钮（右侧）
- 搜索框（可选）

**数据表格**:

- 列：ID、名称、描述、状态、创建时间、操作
- 状态标签：在线（绿色）、离线（灰色）
- 操作按钮：查看 Token、删除

**创建 Agent 弹窗**:

- 表单：名称（必填）、描述（可选）
- 提交后显示生成的 Token
- 提示：请妥善保存 Token，关闭后无法再次查看

### 5.3 STCP 实例列表页

**顶部操作栏**:

- 创建实例按钮
- Agent 筛选下拉框（可选）

**数据表格**:

- 列：ID、名称、Agent、本地地址、授权数、创建时间、操作
- 本地地址格式：192.168.1.100:3306
- 授权数：显示已授权的 Client 数量
- 操作按钮：管理权限、删除

**创建实例弹窗**:

- 选择 Agent（下拉框，只显示在线的 Agent）
- 实例名称（必填）
- 本地 IP（必填）
- 本地端口（必填，数字）
- 描述（可选）

**管理权限弹窗**:

- 左侧：已授权 Client 列表（带撤销按钮）
- 右侧：未授权 Client 列表（带授权按钮）
- 实时更新授权状态

## 6. 国际化设计

### 6.1 语言配置

**中文语言包** (locales/zh-CN.ts):

```typescript
export default {
  common: {
    confirm: "确认",
    cancel: "取消",
    save: "保存",
    delete: "删除",
    edit: "编辑",
    search: "搜索",
    reset: "重置",
    create: "创建",
    actions: "操作",
    status: "状态",
    online: "在线",
    offline: "离线",
  },
  login: {
    title: "登录",
    username: "用户名",
    password: "密码",
    login: "登录",
    loginSuccess: "登录成功",
    loginFailed: "登录失败",
  },
  menu: {
    dashboard: "仪表盘",
    agents: "Agent管理",
    stcpInstances: "STCP实例",
  },
  agent: {
    list: "Agent列表",
    create: "创建Agent",
    name: "Agent名称",
    description: "描述",
    token: "Token",
    createdAt: "创建时间",
    viewToken: "查看Token",
    deleteConfirm: "确认删除此Agent吗？",
  },
  stcp: {
    list: "STCP实例列表",
    create: "创建实例",
    instanceName: "实例名称",
    agent: "所属Agent",
    localIp: "本地IP",
    localPort: "本地端口",
    localAddress: "本地地址",
  },
};
```

**英文语言包** (locales/en-US.ts):

```typescript
export default {
  common: {
    confirm: "Confirm",
    cancel: "Cancel",
    save: "Save",
    delete: "Delete",
    edit: "Edit",
    search: "Search",
    reset: "Reset",
    create: "Create",
    actions: "Actions",
    status: "Status",
    online: "Online",
    offline: "Offline",
  },
  login: {
    title: "Login",
    username: "Username",
    password: "Password",
    login: "Login",
    loginSuccess: "Login successful",
    loginFailed: "Login failed",
  },
  menu: {
    dashboard: "Dashboard",
    agents: "Agents",
    stcpInstances: "STCP Instances",
  },
  agent: {
    list: "Agent List",
    create: "Create Agent",
    name: "Agent Name",
    description: "Description",
    token: "Token",
    createdAt: "Created At",
    viewToken: "View Token",
    deleteConfirm: "Are you sure to delete this agent?",
  },
  stcp: {
    list: "STCP Instance List",
    create: "Create Instance",
    instanceName: "Instance Name",
    agent: "Agent",
    localIp: "Local IP",
    localPort: "Local Port",
    localAddress: "Local Address",
  },
};
```

### 6.2 i18n 配置

**main.ts**:

```typescript
import { createI18n } from "vue-i18n";
import zhCN from "./locales/zh-CN";
import enUS from "./locales/en-US";

const i18n = createI18n({
  legacy: false,
  locale: localStorage.getItem("locale") || "zh-CN",
  fallbackLocale: "zh-CN",
  messages: {
    "zh-CN": zhCN,
    "en-US": enUS,
  },
});

app.use(i18n);
```

### 6.3 使用方式

**在组件中使用**:

```vue
<template>
  <el-button>{{ t("common.confirm") }}</el-button>
  <h1>{{ t("agent.list") }}</h1>
</template>

<script setup lang="ts">
import { useI18n } from "vue-i18n";

const { t, locale } = useI18n();

// 切换语言
const switchLanguage = (lang: string) => {
  locale.value = lang;
  localStorage.setItem("locale", lang);
};
</script>
```

### 6.4 语言切换

**Header 组件**:

- 右上角添加语言切换下拉菜单
- 选项：中文、English
- 切换后立即生效，保存到 localStorage

## 7. 开发流程

### 6.1 第一阶段：项目初始化

1. 创建 Vue 3 项目（使用 Vite）
2. 安装依赖（Element Plus、Vue Router、Pinia、Axios）
3. 配置 TypeScript
4. 配置 Vite 代理（开发环境）
5. 创建基础目录结构

### 6.2 第二阶段：基础功能

1. 实现登录页
2. 实现 Layout 布局
3. 实现路由守卫（认证检查）
4. 封装 API 请求
5. 实现仪表盘（简单版）

### 6.3 第三阶段：核心功能

1. 实现 Agent 管理（列表、创建、删除）
2. 实现 Client 管理（列表、创建、启用/禁用）
3. 实现 STCP 实例管理（列表、创建、删除）
4. 实现权限管理（授权/撤销）

### 6.4 第四阶段：优化和完善

1. 添加加载状态
2. 添加错误处理
3. 优化用户体验
4. 响应式适配
5. 性能优化

## 7. WebSocket 实时更新

### 7.1 WebSocket 连接

**连接地址**: `ws://localhost:8080/ws/admin` (开发环境)

**连接时机**:

- 用户登录成功后建立连接
- 携带 JWT Token 进行认证
- 连接断开后自动重连

**WebSocket 封装** (utils/websocket.ts):

```typescript
class WebSocketClient {
  private ws: WebSocket | null = null;
  private reconnectTimer: number | null = null;
  private heartbeatTimer: number | null = null;

  connect(token: string) {
    const url = `${WS_BASE_URL}/ws/admin?token=${token}`;
    this.ws = new WebSocket(url);

    this.ws.onopen = () => {
      console.log("WebSocket connected");
      this.startHeartbeat();
    };

    this.ws.onmessage = (event) => {
      const data = JSON.parse(event.data);
      this.handleMessage(data);
    };

    this.ws.onclose = () => {
      console.log("WebSocket disconnected");
      this.reconnect(token);
    };
  }

  private handleMessage(data: any) {
    // 分发消息到对应的处理器
    switch (data.type) {
      case "agent_online":
        // 更新Agent状态为在线
        break;
      case "agent_offline":
        // 更新Agent状态为离线
        break;
      case "stcp_created":
        // STCP实例创建成功
        break;
      case "stcp_deleted":
        // STCP实例删除成功
        break;
    }
  }

  private reconnect(token: string) {
    // 5秒后重连
    this.reconnectTimer = setTimeout(() => {
      this.connect(token);
    }, 5000);
  }

  disconnect() {
    if (this.ws) {
      this.ws.close();
    }
  }
}
```

### 7.2 消息类型

**Agent 状态变更**:

```json
{
  "type": "agent_online",
  "data": {
    "agent_id": 1,
    "agent_name": "agent-dev-001",
    "timestamp": 1732618890
  }
}
```

```json
{
  "type": "agent_offline",
  "data": {
    "agent_id": 1,
    "agent_name": "agent-dev-001",
    "timestamp": 1732618890
  }
}
```

**STCP 实例变更**:

```json
{
  "type": "stcp_created",
  "data": {
    "instance_id": 1,
    "instance_name": "mysql-dev-001",
    "agent_id": 1
  }
}
```

### 7.3 状态同步

**WebSocket Store** (stores/websocket.ts):

```typescript
export const useWebSocketStore = defineStore("websocket", {
  state: () => ({
    connected: false,
    client: null as WebSocketClient | null,
  }),
  actions: {
    connect(token: string) {
      this.client = new WebSocketClient();
      this.client.connect(token);
      this.connected = true;
    },
    disconnect() {
      if (this.client) {
        this.client.disconnect();
        this.connected = false;
      }
    },
  },
});
```

## 8. 与 Server 集成

### 7.1 开发环境

**Vite 代理配置** (vite.config.ts):

```typescript
export default defineConfig({
  server: {
    proxy: {
      "/api": {
        target: "http://localhost:8080",
        changeOrigin: true,
      },
    },
  },
});
```

### 7.2 生产环境

**构建配置**:

```typescript
export default defineConfig({
  build: {
    outDir: "../internal/server/web/dist",
    emptyOutDir: true,
  },
});
```

**Server 静态文件服务** (internal/server/server.go):

```go
// 提供静态文件服务
router.Static("/", "./web/dist")

// API路由
apiGroup := router.Group("/api")
{
  // ... API路由
}
```

## 8. 待确认事项

### 8.1 功能优先级

**MVP 核心功能**:

- [x] 登录功能
- [x] Agent 管理（列表、创建、删除、查看 Token）
- [x] STCP 实例管理（列表、创建、删除）
- [x] WebSocket 实时状态更新
- [x] 国际化（中英文切换）

**暂不实现**:

- [ ] Client 管理（后续版本）
- [ ] 权限管理（后续版本）
- [ ] 仪表盘统计（后续版本）
- [ ] 搜索和筛选（后续版本）
- [ ] 日志查看（后续版本）
- [ ] 性能优化（后续版本）
- [ ] 单元测试（后续版本）

### 8.2 界面风格

- [ ] 使用 Element Plus 默认主题
- [ ] 还是自定义主题？

### 8.3 实时更新

- [x] 使用 WebSocket 实时推送 Agent 状态
  - Server 推送 Agent 上线/离线事件
  - 前端自动更新 Agent 状态显示
  - 仪表盘实时更新统计数据

### 8.4 国际化

- [x] 支持中英文切换
  - 使用 vue-i18n
  - 默认语言：中文
  - 语言切换按钮在 Header

## 10. MVP 开发计划

### 10.1 第一阶段：项目初始化（0.5 天）

- [ ] 创建 Vue 3 项目（使用 Vite）
- [ ] 安装依赖（Element Plus、Vue Router、Pinia、Axios、vue-i18n）
- [ ] 配置 TypeScript
- [ ] 配置 Vite 代理
- [ ] 创建基础目录结构
- [ ] 配置国际化

### 10.2 第二阶段：基础功能（1 天）

- [ ] 实现登录页（中英文）
- [ ] 实现 Layout 布局（Header + Sidebar）
- [ ] 实现路由守卫
- [ ] 封装 API 请求
- [ ] 封装 WebSocket 连接

### 10.3 第三阶段：Agent 管理（1 天）

- [ ] Agent 列表页
- [ ] 创建 Agent 弹窗
- [ ] 查看 Token 弹窗
- [ ] 删除 Agent 功能
- [ ] WebSocket 实时更新 Agent 状态

### 10.4 第四阶段：STCP 实例管理（1 天）

- [ ] STCP 实例列表页
- [ ] 创建实例弹窗（选择 Agent）
- [ ] 删除实例功能
- [ ] WebSocket 实时更新

### 10.5 第五阶段：集成和调试（0.5 天）

- [ ] 前后端联调
- [ ] 修复 bug
- [ ] 优化用户体验
- [ ] 构建生产版本

**总计**: 约 4 天（MVP 核心功能）

---

**文档版本**: 2.0  
**创建日期**: 2025-11-26  
**最后更新**: 2025-11-26  
**状态**: 已确认，准备开发

## 11. 设计确认

### 已确认的设计决策

✅ **技术栈**: Vue 3 + Element Plus + TypeScript + Vite  
✅ **配色方案**: 参考图片的蓝色系配色（#4A7BF7 主色）  
✅ **国际化**: 支持中英文切换（vue-i18n）  
✅ **实时更新**: WebSocket 推送 Agent 状态  
✅ **MVP 范围**: 登录 + Agent 管理 + STCP 实例管理  
✅ **开发周期**: 约 4 天

### 暂不实现的功能

❌ Client 管理  
❌ 权限管理  
❌ 仪表盘统计  
❌ 搜索和筛选  
❌ 性能优化  
❌ 单元测试

### 下一步

开始实现 MVP 核心功能
