<template>
  <div class="sidebar">
    <el-menu
      :default-active="activeMenu"
      :collapse="appStore.sidebarCollapsed"
      :collapse-transition="false"
      background-color="#ffffff"
      text-color="#303133"
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
          <span>{{ t('menu.groups') }}</span>
        </template>
      </el-menu-item>
      <el-menu-item index="/audit-logs">
        <template #title>
          <el-icon><Document /></el-icon>
          <span>{{ t('menu.auditLogs') }}</span>
        </template>
      </el-menu-item>
    </el-menu>
    <div class="sidebar-footer">
      <el-icon class="collapse-icon" @click="toggleSidebar">
        <Fold v-if="!appStore.sidebarCollapsed" />
        <Expand v-else />
      </el-icon>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'

const route = useRoute()
const { t } = useI18n()
const appStore = useAppStore()

const activeMenu = computed(() => route.path)

const toggleSidebar = () => {
  appStore.toggleSidebar()
}
</script>

<style scoped>
.sidebar {
  height: 100%;
  display: flex;
  flex-direction: column;
  background-color: #ffffff;
  border-right: 1px solid #e4e7ed;
}

.el-menu {
  border-right: none;
  flex: 1;
  background-color: #ffffff !important;
}

.el-menu:not(.el-menu--collapse) {
  width: 200px;
}

/* 强制覆盖 Element Plus 的菜单背景色 */
:deep(.el-menu),
:deep(.el-menu.el-menu--vertical) {
  background-color: #ffffff !important;
}

/* 统一菜单项样式 */
:deep(.el-menu-item) {
  height: 56px;
  line-height: 56px;
  padding: 0 20px !important;
  transition: all 0.3s;
  background-color: #ffffff !important;
  color: #303133 !important;
}

/* 关键修复：确保 title 内容水平排列 */
:deep(.el-menu-item .el-menu-title-content) {
  display: flex;
  align-items: center;
  gap: 8px;
}

/* 统一图标样式 */
:deep(.el-menu-item .el-icon) {
  font-size: 18px;
  width: 18px;
  height: 18px;
  flex-shrink: 0;
  margin-right: 12px !important; /* 强制设置右边距 */
}

/* 统一文字样式 */
:deep(.el-menu-item span) {
  font-size: 14px;
  line-height: 1;
}

/* hover 状态 */
:deep(.el-menu-item:hover) {
  background-color: #ecf5ff !important;
}

/* 激活状态 */
:deep(.el-menu-item.is-active) {
  background-color: #ecf5ff !important;
  color: #409eff !important;
  font-weight: 500;
}

/* 折叠状态下的样式 */
:deep(.el-menu--collapse .el-menu-item) {
  padding: 0 !important;
  text-align: center;
}

:deep(.el-menu--collapse .el-menu-item .el-icon) {
  margin-right: 0;
}

/* 侧边栏底部折叠按钮 */
.sidebar-footer {
  height: 48px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-top: 1px solid #e4e7ed;
  background-color: #ffffff;
  cursor: pointer;
  transition: all 0.3s;
}

.sidebar-footer:hover {
  background-color: #f5f7fa;
}

.collapse-icon {
  font-size: 20px;
  color: #606266;
  transition: all 0.3s;
}

.collapse-icon:hover {
  color: #409eff;
}
</style>
