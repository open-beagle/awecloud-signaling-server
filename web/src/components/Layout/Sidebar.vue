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
        <el-icon><Monitor /></el-icon>
        <template #title>
          <span>{{ t('menu.agents') }}</span>
        </template>
      </el-menu-item>
      <el-sub-menu index="services">
        <template #title>
          <el-icon><Connection /></el-icon>
          <span>{{ t('menu.serviceManagement') }}</span>
        </template>
        <el-menu-item index="/services/stcp">
          {{ t('menu.stcpInstances') }}
        </el-menu-item>
        <el-menu-item index="/services/tcp">
          {{ t('menu.tcpServices') }}
        </el-menu-item>
      </el-sub-menu>
      <el-menu-item index="/clients">
        <el-icon><User /></el-icon>
        <template #title>
          <span>{{ t('menu.clients') }}</span>
        </template>
      </el-menu-item>
      <el-menu-item index="/groups">
        <el-icon><UserFilled /></el-icon>
        <template #title>
          <span>{{ t('menu.groups') }}</span>
        </template>
      </el-menu-item>
      <el-menu-item index="/favorites">
        <el-icon><Star /></el-icon>
        <template #title>
          <span>{{ t('menu.favorites') }}</span>
        </template>
      </el-menu-item>
      <el-menu-item index="/audit-logs">
        <el-icon><Document /></el-icon>
        <template #title>
          <span>{{ t('menu.auditLogs') }}</span>
        </template>
      </el-menu-item>
      <el-menu-item index="/system/config">
        <el-icon><Setting /></el-icon>
        <template #title>
          <span>{{ t('menu.systemConfig') }}</span>
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
import { Star } from '@element-plus/icons-vue'

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

/* 子菜单样式 */
:deep(.el-sub-menu) {
  background-color: #ffffff !important;
}

:deep(.el-sub-menu__title) {
  height: 56px;
  line-height: 56px;
  padding: 0 20px !important;
  background-color: #ffffff !important;
  color: #303133 !important;
}

:deep(.el-sub-menu__title:hover) {
  background-color: #ecf5ff !important;
}

:deep(.el-sub-menu__title .el-icon) {
  font-size: 18px;
  width: 18px;
  height: 18px;
  margin-right: 12px !important;
}

/* 子菜单项样式 */
:deep(.el-sub-menu .el-menu-item) {
  height: 48px;
  line-height: 48px;
  padding-left: 52px !important; /* 增加左侧缩进 */
  background-color: #fafafa !important;
  font-size: 14px;
}

:deep(.el-sub-menu .el-menu-item:hover) {
  background-color: #ecf5ff !important;
}

:deep(.el-sub-menu .el-menu-item.is-active) {
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

:deep(.el-menu--collapse .el-sub-menu__title) {
  padding: 0 !important;
  text-align: center;
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
