# 前端界面设计改进方案

## 1. 设计分析

### 1.1 参考平台设计特点

#### 平台一（BD-Wind 算数据一体化平台）
- **整体风格**：清爽简洁，白色为主色调
- **导航结构**：左侧折叠式导航菜单，支持多级展开
- **顶部导航**：水平导航栏，包含主要功能入口和用户信息
- **搜索区域**：顶部搜索框 + 高级筛选标签（核心能力、数据集下载、文件在线预览）
- **表格设计**：
  - 简洁的表头设计
  - 蓝色链接突出可点击项
  - 右侧操作列统一样式
  - 底部分页器居中显示
- **色彩方案**：
  - 主色：蓝色（#409EFF）
  - 背景：浅灰色（#F5F7FA）
  - 文字：深灰色主文字 + 浅灰色辅助文字

#### 平台二（AWECloud）
- **整体风格**：深色主题，专业感强
- **导航结构**：左侧深色侧边栏，图标 + 文字
- **顶部区域**：标题 + 语言切换 + 用户信息
- **搜索区域**：
  - 多字段搜索表单（Client ID、实例ID、操作、日期范围等）
  - 搜索和重置按钮右对齐
- **表格设计**：
  - 表头固定，字段清晰
  - 无数据时显示 "No Data" 提示
  - 底部分页器包含总数、每页条数、跳转功能
- **色彩方案**：
  - 主色：深蓝灰色（#2C3E50）
  - 强调色：蓝色（#409EFF）
  - 背景：浅灰色（#ECF0F1）

### 1.2 当前系统问题分析

根据当前系统截图和代码分析，存在以下关键问题：

#### 问题一：配色不统一
1. **主色调混乱**：
   - 左侧菜单使用深蓝灰色（#2C3E50）
   - 顶部和内容区域使用浅色背景
   - 按钮和链接使用不同的蓝色色调
   - 缺少统一的色彩规范

2. **对比度问题**：
   - 部分文字颜色对比度不足，影响可读性
   - 状态标签颜色不够明显
   - 激活状态和普通状态区分不明显

3. **视觉层次不清晰**：
   - 卡片阴影过重或过轻
   - 边框颜色不统一
   - 缺少视觉焦点引导

#### 问题二：左侧菜单不一致
1. **图标和文字对齐问题**：
   - 图标和文字垂直方向没有对齐
   - 不同菜单项的间距不一致
   - 图标大小不统一

2. **菜单项样式问题**：
   - 激活状态样式不够明显
   - hover 状态缺少视觉反馈
   - 菜单项高度不统一

3. **结构问题**：
   - Element Plus 的 `template #title` 导致布局问题
   - 图标和文字不在同一个 flex 容器中
   - 深度选择器 `:deep()` 可能没有生效

#### 其他问题
1. **布局结构**：
   - 缺少整体页面布局框架
   - 搜索表单和表格都在一个 Card 内，层次感不够

2. **搜索区域**：
   - 表单项排列较密集
   - 缺少视觉分组
   - 按钮位置不够突出

3. **表格设计**：
   - 列宽固定，不够灵活
   - 缺少空状态提示
   - 辅助信息（如服务器地址、主机名）显示不够清晰

4. **交互体验**：
   - 缺少加载状态的友好提示
   - 导出功能位置不够明显
   - 缺少快捷操作入口

## 2. 改进方案

### 2.1 整体布局改进

```
┌─────────────────────────────────────────────────────────┐
│  顶部导航栏（Logo + 主导航 + 用户信息）                    │
├──────┬──────────────────────────────────────────────────┤
│      │  页面标题区域                                      │
│      ├──────────────────────────────────────────────────┤
│ 左侧 │  搜索筛选区域（独立卡片）                          │
│ 导航 │  ┌─────────────────────────────────────────────┐ │
│ 菜单 │  │ 搜索表单（多行布局，字段分组）                │ │
│      │  │ [查询] [重置] [导出]                         │ │
│      │  └─────────────────────────────────────────────┘ │
│      ├──────────────────────────────────────────────────┤
│      │  数据表格区域（独立卡片）                          │
│      │  ┌─────────────────────────────────────────────┐ │
│      │  │ 表格工具栏（统计信息 + 快捷操作）             │ │
│      │  ├─────────────────────────────────────────────┤ │
│      │  │ 数据表格                                     │ │
│      │  ├─────────────────────────────────────────────┤ │
│      │  │ 分页器（居右）                               │ │
│      │  └─────────────────────────────────────────────┘ │
└──────┴──────────────────────────────────────────────────┘
```

### 2.2 搜索区域改进

**改进点**：
1. 将搜索区域独立成一个卡片，与表格分离
2. 表单采用多行布局，每行 3-4 个字段
3. 相关字段分组（如：客户端信息、实例信息、时间范围、操作类型）
4. 按钮组右对齐，增加导出按钮
5. 支持展开/收起高级搜索

**布局示例**：
```
┌─────────────────────────────────────────────────────────┐
│ 搜索筛选                                    [展开] [收起] │
├─────────────────────────────────────────────────────────┤
│ Client ID: [_________]  实例ID: [_________]  操作: [▼]  │
│ 日期范围: [开始日期] - [结束日期]          状态: [▼]    │
│                                                          │
│                          [查询] [重置] [导出]            │
└─────────────────────────────────────────────────────────┘
```

### 2.3 表格区域改进

**改进点**：
1. 添加表格工具栏，显示统计信息
2. 优化列宽，使用自适应宽度
3. 添加空状态提示组件
4. 优化辅助信息显示（使用 Tooltip 或 Popover）
5. 添加行操作菜单（查看详情、导出单条等）
6. 状态标签使用更明显的颜色区分

**表格工具栏示例**：
```
┌─────────────────────────────────────────────────────────┐
│ 共 1,234 条记录                    [刷新] [列设置] [导出] │
└─────────────────────────────────────────────────────────┘
```

### 2.4 色彩方案统一（重点改进）

#### 统一色彩规范（推荐方案）

**基础色彩**：
```css
:root {
  /* 主色调 - 统一使用 Element Plus 默认蓝 */
  --primary-color: #409EFF;
  --primary-light: #79BBFF;
  --primary-lighter: #A0CFFF;
  --primary-dark: #337ECC;
  
  /* 功能色 */
  --success-color: #67C23A;
  --warning-color: #E6A23C;
  --danger-color: #F56C6C;
  --info-color: #909399;
  
  /* 中性色 */
  --text-primary: #303133;    /* 主要文字 */
  --text-regular: #606266;    /* 常规文字 */
  --text-secondary: #909399;  /* 次要文字 */
  --text-placeholder: #C0C4CC; /* 占位文字 */
  
  /* 边框色 */
  --border-base: #DCDFE6;
  --border-light: #E4E7ED;
  --border-lighter: #EBEEF5;
  --border-extra-light: #F2F6FC;
  
  /* 背景色 */
  --bg-color: #FFFFFF;
  --bg-page: #F5F7FA;
  --bg-overlay: rgba(0, 0, 0, 0.5);
  
  /* 侧边栏专用色 */
  --sidebar-bg: #2C3E50;
  --sidebar-text: #FFFFFF;
  --sidebar-text-active: #409EFF;
  --sidebar-hover-bg: rgba(255, 255, 255, 0.1);
}
```

**色彩使用规则**：
1. **主色调**：仅用于主要操作按钮、链接、激活状态
2. **功能色**：严格按照语义使用（成功/警告/危险/信息）
3. **文字颜色**：根据重要性使用 4 个层级
4. **背景色**：页面背景统一使用 `--bg-page`，卡片使用 `--bg-color`
5. **边框色**：根据视觉层次使用 4 个层级

#### 侧边栏配色方案
```css
/* 侧边栏统一配色 */
.sidebar {
  background-color: var(--sidebar-bg);
  color: var(--sidebar-text);
}

.sidebar .el-menu {
  background-color: var(--sidebar-bg);
}

.sidebar .el-menu-item {
  color: var(--sidebar-text);
}

.sidebar .el-menu-item:hover {
  background-color: var(--sidebar-hover-bg);
}

.sidebar .el-menu-item.is-active {
  color: var(--sidebar-text-active);
  background-color: rgba(64, 158, 255, 0.1);
}
```

### 2.5 左侧菜单修复方案（重点改进）

#### 问题根源分析
Element Plus 的 `el-menu-item` 在使用 `template #title` 时，会创建两个独立的容器：
1. 图标直接在 `el-menu-item` 下
2. 文字在 `template #title` 生成的 span 中

这导致图标和文字无法在同一个 flex 容器中对齐。

#### 解决方案一：调整 DOM 结构（推荐）
```vue
<template>
  <el-menu-item index="/agents">
    <template #title>
      <el-icon><Monitor /></el-icon>
      <span>{{ t('menu.agents') }}</span>
    </template>
  </el-menu-item>
</template>

<style scoped>
:deep(.el-menu-item .el-menu-title-content) {
  display: flex;
  align-items: center;
  gap: 8px;
}

:deep(.el-menu-item .el-icon) {
  font-size: 18px;
  width: 18px;
  height: 18px;
  flex-shrink: 0;
}

:deep(.el-menu-item span) {
  font-size: 14px;
  line-height: 1;
}
</style>
```

#### 解决方案二：使用自定义结构
```vue
<template>
  <el-menu-item index="/agents">
    <div class="menu-item-content">
      <el-icon class="menu-icon"><Monitor /></el-icon>
      <span class="menu-text">{{ t('menu.agents') }}</span>
    </div>
  </el-menu-item>
</template>

<style scoped>
.menu-item-content {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
}

.menu-icon {
  font-size: 18px;
  width: 18px;
  height: 18px;
  flex-shrink: 0;
}

.menu-text {
  font-size: 14px;
  line-height: 1;
  flex: 1;
}

/* 统一菜单项高度和内边距 */
:deep(.el-menu-item) {
  height: 56px;
  line-height: 56px;
  padding: 0 20px !important;
}

/* 确保图标和文字垂直居中 */
:deep(.el-menu-item) {
  display: flex;
  align-items: center;
}
</style>
```

#### 完整的侧边栏组件代码
```vue
<template>
  <div class="sidebar">
    <div class="logo">
      <span v-if="!appStore.sidebarCollapsed">AWECloud</span>
      <span v-else>AC</span>
    </div>
    <el-menu
      :default-active="activeMenu"
      :collapse="appStore.sidebarCollapsed"
      :collapse-transition="false"
      background-color="#2c3e50"
      text-color="#ffffff"
      active-text-color="#409eff"
      router
    >
      <el-menu-item index="/agents">
        <template #title>
          <el-icon><Monitor /></el-icon>
          <span>{{ t('menu.agents') }}</span>
        </template>
      </el-menu-item>
      <el-menu-item index="/clients">
        <template #title>
          <el-icon><User /></el-icon>
          <span>{{ t('menu.clients') }}</span>
        </template>
      </el-menu-item>
      <el-menu-item index="/stcp-instances">
        <template #title>
          <el-icon><Connection /></el-icon>
          <span>{{ t('menu.stcpInstances') }}</span>
        </template>
      </el-menu-item>
      <el-menu-item index="/groups">
        <template #title>
          <el-icon><UserFilled /></el-icon>
          <span>用户组</span>
        </template>
      </el-menu-item>
      <el-menu-item index="/audit-logs">
        <template #title>
          <el-icon><Document /></el-icon>
          <span>{{ t('menu.auditLogs') }}</span>
        </template>
      </el-menu-item>
    </el-menu>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { Monitor, User, Connection, UserFilled, Document } from '@element-plus/icons-vue'

const route = useRoute()
const { t } = useI18n()
const appStore = useAppStore()

const activeMenu = computed(() => route.path)
</script>

<style scoped>
.sidebar {
  height: 100%;
  display: flex;
  flex-direction: column;
  background-color: #2c3e50;
}

.logo {
  height: 60px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #ffffff;
  font-size: 20px;
  font-weight: bold;
  border-bottom: 1px solid rgba(255, 255, 255, 0.1);
  transition: all 0.3s;
  cursor: pointer;
}

.logo:hover {
  background-color: rgba(255, 255, 255, 0.05);
}

.el-menu {
  border-right: none;
  flex: 1;
}

.el-menu:not(.el-menu--collapse) {
  width: 200px;
}

/* 关键修复：确保 title 内容水平排列 */
:deep(.el-menu-item .el-menu-title-content) {
  display: flex;
  align-items: center;
  gap: 8px;
}

/* 统一菜单项样式 */
:deep(.el-menu-item) {
  height: 56px;
  line-height: 56px;
  padding: 0 20px !important;
  transition: all 0.3s;
}

/* 统一图标样式 */
:deep(.el-menu-item .el-icon) {
  font-size: 18px;
  width: 18px;
  height: 18px;
  flex-shrink: 0;
  margin-right: 0; /* 移除默认 margin，使用 gap 控制间距 */
}

/* 统一文字样式 */
:deep(.el-menu-item span) {
  font-size: 14px;
  line-height: 1;
}

/* hover 状态 */
:deep(.el-menu-item:hover) {
  background-color: rgba(255, 255, 255, 0.1) !important;
}

/* 激活状态 */
:deep(.el-menu-item.is-active) {
  background-color: rgba(64, 158, 255, 0.1) !important;
  border-left: 3px solid #409eff;
  padding-left: 17px !important; /* 减去边框宽度 */
}

/* 折叠状态下的样式 */
:deep(.el-menu--collapse .el-menu-item) {
  padding: 0 !important;
  text-align: center;
}

:deep(.el-menu--collapse .el-menu-item .el-icon) {
  margin-right: 0;
}
</style>
```

### 2.6 组件优化建议

#### 2.6.1 搜索表单组件
```vue
<template>
  <el-card class="search-card" shadow="never">
    <template #header>
      <div class="search-header">
        <span>搜索筛选</span>
        <el-button 
          text 
          @click="toggleAdvanced"
        >
          {{ showAdvanced ? '收起' : '展开' }}
          <el-icon><ArrowUp v-if="showAdvanced" /><ArrowDown v-else /></el-icon>
        </el-button>
      </div>
    </template>
    
    <el-form :model="form" label-width="100px">
      <!-- 基础搜索 -->
      <el-row :gutter="20">
        <el-col :span="8">
          <el-form-item label="Client ID">
            <el-input v-model="form.clientId" placeholder="请输入Client ID" clearable />
          </el-form-item>
        </el-col>
        <el-col :span="8">
          <el-form-item label="实例ID">
            <el-input v-model="form.instanceId" placeholder="请输入实例ID" clearable />
          </el-form-item>
        </el-col>
        <el-col :span="8">
          <el-form-item label="操作">
            <el-select v-model="form.action" placeholder="请选择" clearable>
              <el-option label="连接" value="connect" />
              <el-option label="断开" value="disconnect" />
            </el-select>
          </el-form-item>
        </el-col>
      </el-row>
      
      <!-- 高级搜索 -->
      <template v-if="showAdvanced">
        <el-row :gutter="20">
          <el-col :span="12">
            <el-form-item label="日期范围">
              <el-date-picker
                v-model="form.dateRange"
                type="daterange"
                range-separator="-"
                start-placeholder="开始日期"
                end-placeholder="结束日期"
              />
            </el-form-item>
          </el-col>
          <el-col :span="8">
            <el-form-item label="状态">
              <el-select v-model="form.status" placeholder="请选择" clearable>
                <el-option label="成功" value="success" />
                <el-option label="失败" value="failed" />
              </el-select>
            </el-form-item>
          </el-col>
        </el-row>
      </template>
      
      <!-- 操作按钮 -->
      <el-row>
        <el-col :span="24" class="text-right">
          <el-button @click="handleReset">重置</el-button>
          <el-button type="primary" @click="handleSearch">查询</el-button>
          <el-button type="success" @click="handleExport">导出</el-button>
        </el-col>
      </el-row>
    </el-form>
  </el-card>
</template>
```

#### 2.6.2 表格工具栏组件
```vue
<template>
  <div class="table-toolbar">
    <div class="toolbar-left">
      <span class="total-text">共 {{ total }} 条记录</span>
    </div>
    <div class="toolbar-right">
      <el-button text @click="handleRefresh">
        <el-icon><Refresh /></el-icon>
        刷新
      </el-button>
      <el-button text @click="handleColumnSetting">
        <el-icon><Setting /></el-icon>
        列设置
      </el-button>
      <el-button text @click="handleExport">
        <el-icon><Download /></el-icon>
        导出
      </el-button>
    </div>
  </div>
</template>
```

#### 2.6.3 空状态组件
```vue
<template>
  <el-empty 
    v-if="!data.length && !loading"
    description="暂无数据"
    :image-size="120"
  >
    <el-button type="primary" @click="handleRefresh">刷新数据</el-button>
  </el-empty>
</template>
```

### 2.7 响应式设计

**断点设置**：
- `xs`: < 768px（手机）
- `sm`: ≥ 768px（平板）
- `md`: ≥ 992px（小屏电脑）
- `lg`: ≥ 1200px（大屏电脑）
- `xl`: ≥ 1920px（超大屏）

**适配方案**：
1. 搜索表单在小屏幕下改为单列布局
2. 表格在小屏幕下隐藏部分次要列
3. 操作按钮在小屏幕下改为下拉菜单
4. 侧边栏在小屏幕下自动收起

## 3. 实施计划

### 3.1 第一阶段：紧急修复（已完成 ✅）
- [x] **创建 Logo 组件**
  - [x] 使用 logo.png（狗头图标）
  - [x] 只显示 "Signaling" 文字
  - [x] 支持折叠/展开状态
  - [x] 放置在顶部 Header 左侧，不参与侧边栏伸缩
- [x] **修复左侧菜单对齐问题**
  - [x] 调整 Sidebar.vue 的 DOM 结构
  - [x] 将图标和文字都放在 template #title 内
  - [x] 修复图标和文字的对齐样式
  - [x] 统一菜单项高度为 56px
  - [x] 设置图标和文字间距为 12px
  - [x] 优化激活状态和 hover 状态样式
  - [x] 折叠状态下只显示图标
  - [x] 将折叠按钮移到侧边栏底部
- [x] **统一色彩方案**
  - [x] 在 main.css 中定义完整的 CSS 变量系统
  - [x] 侧边栏改为白色背景（参考 BD-Wind）
  - [x] 主内容区使用深灰色背景 (#f0f2f5)
  - [x] 更新所有组件使用统一的色彩变量
  - [x] 定义完整的色彩层级系统
- [x] **布局结构调整**
  - [x] Header 横跨整个页面顶部
  - [x] Sidebar 在 Header 下方
  - [x] 主内容区有深灰色背景作为过渡

### 3.2 第二阶段：基础布局改造（已完成 ✅）
- [x] 创建整体布局组件（Layout）
- [x] 创建顶部导航组件（Header）
- [x] 优化侧边栏组件（Sidebar）
- [x] 创建主内容区组件（Main）

### 3.3 第三阶段：搜索区域优化（待开始）
- [ ] 创建搜索表单组件（SearchForm）
- [ ] 实现展开/收起功能
- [ ] 优化表单布局和样式
- [ ] 添加表单验证

### 3.4 第四阶段：表格区域优化（待开始）
- [ ] 创建表格工具栏组件（TableToolbar）
- [ ] 创建空状态组件（EmptyState）
- [ ] 优化表格列配置
- [ ] 添加列设置功能
- [ ] 优化分页器样式

### 3.5 第五阶段：交互体验优化（待开始）
- [ ] 添加加载动画
- [ ] 优化错误提示
- [ ] 添加操作确认对话框
- [ ] 实现快捷键支持

### 3.6 第六阶段：响应式适配（待开始）
- [ ] 实现移动端适配
- [ ] 优化平板端显示
- [ ] 测试各种屏幕尺寸

## 4. 技术选型

### 4.1 UI 框架
- **Element Plus**：继续使用，版本保持最新
- **图标库**：Element Plus Icons

### 4.2 样式方案
- **CSS 预处理器**：SCSS
- **CSS 方法论**：BEM 命名规范
- **响应式**：使用 Element Plus 的栅格系统

### 4.3 状态管理
- **Pinia**：用于全局状态管理
- **组合式 API**：使用 Vue 3 Composition API

## 5. 设计规范

### 5.1 间距规范
- **页面边距**：20px
- **卡片间距**：20px
- **表单项间距**：20px（水平）、16px（垂直）
- **按钮间距**：8px

### 5.2 字体规范
- **标题字体**：16px / 18px / 20px
- **正文字体**：14px
- **辅助文字**：12px
- **字体家族**：-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif

### 5.3 圆角规范
- **卡片圆角**：4px
- **按钮圆角**：4px
- **输入框圆角**：4px

### 5.4 阴影规范
- **卡片阴影**：0 2px 12px 0 rgba(0, 0, 0, 0.1)
- **悬浮阴影**：0 2px 12px 0 rgba(0, 0, 0, 0.15)

## 6. 待确认事项

### 6.1 色彩方案（已确定）
- [x] 使用统一的清爽蓝色方案
- [x] 侧边栏使用深色背景（#2C3E50）
- [ ] 是否需要支持主题切换（亮色/暗色）？

### 6.2 布局结构
- [ ] 是否需要顶部导航栏？
- [ ] 侧边栏是否需要支持折叠？
- [ ] 是否需要面包屑导航？

### 6.3 功能优先级
- [ ] 哪些功能需要优先实现？
- [ ] 是否需要移动端适配？
- [ ] 是否需要国际化支持？

### 6.4 交互细节
- [ ] 表格是否需要支持拖拽排序？
- [ ] 是否需要支持批量操作？
- [ ] 是否需要支持自定义列显示？

## 7. 参考资源

- Element Plus 官方文档：https://element-plus.org/
- Vue 3 官方文档：https://vuejs.org/
- 设计规范参考：Ant Design、Material Design

## 8. 关键问题修复清单

### 8.1 Logo 组件（已完成 ✅）
- [x] 创建 Logo.vue 组件（`web/src/components/Common/Logo.vue`）
- [x] 使用 logo.png（狗头图标）
- [x] 只显示 "Signaling" 文字
- [x] 支持折叠/展开状态切换
- [x] 放置在顶部 Header 左侧
- [x] Logo 不参与侧边栏伸缩

### 8.2 左侧菜单修复（已完成 ✅）
- [x] 将图标和文字都放在 `template #title` 内
- [x] 使用 `:deep(.el-menu-title-content)` 设置 flex 布局
- [x] 统一图标大小为 18px
- [x] 统一菜单项高度为 56px
- [x] 设置图标右边距为 12px
- [x] 优化激活状态：浅蓝色背景 + 蓝色文字
- [x] 优化 hover 状态：浅蓝色背景
- [x] 折叠状态下只显示图标
- [x] 将折叠按钮移到侧边栏底部

### 8.3 配色统一修复（已完成 ✅）
- [x] 在 main.css 中定义完整的 CSS 变量系统
- [x] 侧边栏改为白色背景（参考 BD-Wind）
- [x] 主内容区使用深灰色背景 (#f0f2f5)
- [x] 确保主色调统一使用 #409EFF
- [x] 定义文字颜色层级（4 个层级）
- [x] 定义边框颜色层级（4 个层级）

### 8.4 布局结构调整（已完成 ✅）
- [x] Header 横跨整个页面顶部
- [x] Sidebar 在 Header 下方
- [x] 主内容区有深灰色背景作为过渡
- [x] 侧边栏有右边框分隔

## 9. 实施记录

### 2025-11-28

#### 第一批：Logo 和基础修复
- ✅ 创建 Logo.vue 组件，包含 SVG 图标和文字
- ✅ 更新 Sidebar.vue 集成 Logo 组件
- ✅ 实现折叠状态下的 Logo 显示逻辑

#### 第二批：菜单对齐修复
- ✅ 调整菜单项 DOM 结构，将图标放入 template #title
- ✅ 使用 :deep(.el-menu-title-content) 设置 flex 布局
- ✅ 统一图标和文字样式
- ✅ 优化激活状态和 hover 状态

#### 第三批：色彩统一
- ✅ 在 main.css 中定义完整的 CSS 变量系统
- ✅ 侧边栏改为白色背景（参考 BD-Wind）
- ✅ 主内容区使用深灰色背景 (#f0f2f5)
- ✅ 定义 4 个层级的文字颜色
- ✅ 定义 4 个层级的边框颜色

#### 第四批：布局结构调整
- ✅ 调整 Layout 结构：Header 在顶部横跨全屏
- ✅ Logo 移到 Header 左侧
- ✅ 折叠按钮移到 Sidebar 底部
- ✅ 优化侧边栏折叠状态（只显示图标）
- ✅ 添加侧边栏底部折叠按钮样式

#### 第五批：细节优化
- ✅ 调整图标和文字间距为 12px
- ✅ 优化 Logo 文字颜色（深色）
- ✅ 优化菜单项激活和 hover 状态
- ✅ 添加侧边栏右边框分隔

#### 完成总结
第一阶段和第二阶段的所有核心任务已完成：
- ✅ Logo 组件创建并正确放置
- ✅ 左侧菜单样式完全修复
- ✅ 色彩方案统一（参考 BD-Wind 浅色风格）
- ✅ 布局结构调整完成
- ✅ 折叠功能正常工作

#### 下一步计划
- ⏳ 第三阶段：优化搜索区域
- ⏳ 第四阶段：优化表格区域
- ⏳ 第五阶段：交互体验优化
- ⏳ 第六阶段：响应式适配

---

**文档版本**：v1.2  
**创建日期**：2025-11-28  
**最后更新**：2025-11-28  
**更新内容**：
- 新增左侧菜单修复方案（2.5 节）
- 新增统一色彩规范（2.4 节）
- 调整实施计划优先级
- 新增关键问题修复清单（第 8 节）
- ✅ 完成 Logo 组件创建
- 新增实施记录（第 9 节）
