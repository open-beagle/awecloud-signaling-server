# AWECloud Signaling Web

Vue 3 + Element Plus + TypeScript 管理界面

## 已创建的文件

✅ 项目配置
- package.json
- vite.config.ts
- tsconfig.json
- tsconfig.node.json
- index.html

✅ API接口
- src/api/admin.ts
- src/api/agent.ts
- src/api/stcp.ts

✅ 类型定义
- src/types/models.ts

✅ 工具函数
- src/utils/request.ts

✅ 国际化
- src/locales/zh-CN.ts
- src/locales/en-US.ts

## 待创建的文件

### 核心文件
- [ ] src/main.ts - 入口文件
- [ ] src/App.vue - 根组件
- [ ] src/router/index.ts - 路由配置
- [ ] src/stores/auth.ts - 认证状态
- [ ] src/stores/app.ts - 应用状态
- [ ] src/assets/styles/main.css - 全局样式

### 布局组件
- [ ] src/components/Layout/Layout.vue - 主布局
- [ ] src/components/Layout/Header.vue - 顶部导航
- [ ] src/components/Layout/Sidebar.vue - 侧边栏

### 公共组件
- [ ] src/components/Common/StatusTag.vue - 状态标签
- [ ] src/components/Common/CopyButton.vue - 复制按钮

### 页面组件
- [ ] src/views/Login.vue - 登录页
- [ ] src/views/Agent/List.vue - Agent列表
- [ ] src/views/Agent/components/CreateDialog.vue - 创建Agent弹窗
- [ ] src/views/Agent/components/TokenDialog.vue - Token弹窗
- [ ] src/views/STCP/List.vue - STCP实例列表
- [ ] src/views/STCP/components/CreateDialog.vue - 创建实例弹窗

## 安装依赖

```bash
cd web
npm install
```

## 开发

```bash
npm run dev
```

访问 http://localhost:3000

## 构建

```bash
npm run build
```

构建产物在 `dist/` 目录

## 配色方案

- 主色: #4A90E2
- 背景色: #EFF3F8
- 卡片背景: #FFFFFF
- 文字主色: #2C3E50
- 成功色: #67C23A (在线)
- 危险色: #F56C6C (删除)
- 信息色: #909399 (离线)
