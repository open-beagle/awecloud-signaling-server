---
inclusion: fileMatch
fileMatchPattern: "**/views/*/Detail.vue"
---

# 详情页设计规范

## 适用范围

所有 `web/src/views/*/Detail.vue` 详情页面。

## 页面结构

详情页由多个 `el-card` 纵向排列组成，每个 card 代表一个信息分区。

### 整体布局

```
  列表页 > 详情: #ID 或 名称          ← 面包屑导航（由 Layout 统一渲染）
（间距 20px）
┌─────────────────────────────────────────┐
│  分区标题                    [状态标签]  │
├─────────────────────────────────────────┤
│  标签 │ 值          │ 标签 │ 值         │
│  标签 │ 值          │ 标签 │ 值         │
│  标签 │ 值          │ 标签 │ 值         │
└─────────────────────────────────────────┘
（间距 20px）
┌─────────────────────────────────────────┐
│  分区标题                               │
├─────────────────────────────────────────┤
│  标签 │ 值          │ 标签 │ 值         │
└─────────────────────────────────────────┘
```

### 面包屑导航

详情页顶部由 Layout 自动渲染面包屑，格式为「列表页 > 详情: 名称」。

详情页不需要自行实现面包屑或"返回"按钮，具体规范参见 #[[file:.kiro/steering/ui-breadcrumb.md]]。

### 卡片规范

- 使用 `el-card`，`shadow="never"`
- 卡片间距 `margin-bottom: 20px`
- 卡片标题通过 `#header` 插槽实现
- 标题右侧可放置状态标签（`el-tag`）

### 描述列表规范

使用 `el-descriptions` 组件展示键值对信息：

- 固定 2 列布局：`:column="2"`
- 启用边框：`border`
- 标签列固定宽度：`label-class-name="desc-label"`
- 禁止使用 `:span="2"` 跨列，所有字段统一占 1 列
- 奇数字段的分区，末尾补一个空占位项：`<el-descriptions-item label="">&nbsp;</el-descriptions-item>`

### 两列等宽

两列必须各占 50% 宽度。

`el-descriptions` 底层是 `<table>`，默认列宽由内容自适应。当某个字段值较宽（如多个 tag、长文本）时，会撑宽该列，破坏均分布局。

通过全局样式（非 scoped）强制表格等宽分配：

```css
.页面容器类名 .el-descriptions__body .el-descriptions__table {
  table-layout: fixed;
}
```

### 标签列宽度

通过全局样式（非 scoped）固定标签列宽度为 100px：

```css
.页面容器类名 .el-descriptions__label {
  width: 100px !important;
  min-width: 100px !important;
  max-width: 100px !important;
}
```

- 标签列固定 100px，值列弹性填充剩余空间
- 使用页面容器类名作为选择器前缀，避免影响其他页面

### 值列溢出处理

由于 `table-layout: fixed` 后列宽固定，长内容需要处理溢出：

- 长文本：自动换行（默认行为）
- Tag 列表：允许换行，通过 `margin-bottom: 4px` 保持间距

## 国际化

- 所有标签文本必须使用 `$t()` 国际化，禁止硬编码中文
- 通用标签（如"创建时间"）使用 `common.*` 命名空间
- 页面专属标签使用对应模块命名空间（如 `node.*`）
- 英文缩写或专有名词可直接写（如 `ID`、`Node ID`、`CPU`）
- 新增 i18n key 时，`zh-CN.ts` 和 `en-US.ts` 必须同步添加

## 数据展示

### 空值处理

- 文本字段为空时显示 `-`
- 使用模式：`{{ value || '-' }}`

### 时间字段

- 统一使用 `formatTime()` 工具函数
- 带空值保护：`{{ time ? formatTime(time) : '-' }}`

### 状态标签

- 在线/离线：`el-tag` type 为 `success` / `info`
- 类型标签：根据语义选择 `success`、`primary`、`warning` 等

### 标签列表（Tag 数组）

- 使用 `v-for` 渲染 `el-tag`
- 标签间距：`margin-right: 8px; margin-bottom: 4px`
- 空数组显示 `-`

### 链接

- 关联实体使用 `router-link` 跳转
- 样式：主题色，hover 下划线

## 页面组件结构

### template 层级

```
div.页面容器（v-loading）
  template v-if="data"
    el-card（分区1）
      #header → card-header
      el-descriptions
        el-descriptions-item × N
    el-card（分区2）
      ...
```

### script 部分

- 使用 `<script setup lang="ts">`
- 路由参数通过 `useRoute()` 获取
- 数据加载在 `onMounted` 中触发
- loading 状态控制 `v-loading`
- 需要解析的嵌套数据使用 `computed`

## 参考实现

`web/src/views/Node/Detail.vue` 是详情页的标准参考。
