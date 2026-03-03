<template>
  <div class="sidebar">
    <el-menu
      :default-active="activeMenu"
      :collapse="appStore.sidebarCollapsed"
      :collapse-transition="false"
      background-color="#ffffff"
      text-color="#303133"
      active-text-color="#409eff"
      :unique-opened="false"
      router
    >
      <!-- 用户管理 -->
      <el-menu-item index="/users">
        <el-icon><User /></el-icon>
        <template #title>
          <span>{{ t('menu.users') }}</span>
        </template>
      </el-menu-item>
      <!-- 设备管理 -->
      <el-menu-item index="/nodes">
        <el-icon><Monitor /></el-icon>
        <template #title>
          <span>{{ t('menu.nodes') }}</span>
        </template>
      </el-menu-item>
      <!-- 终端管理 -->
      <el-menu-item index="/endpoints">
        <el-icon><Connection /></el-icon>
        <template #title>
          <span>{{ t('menu.endpoints') }}</span>
        </template>
      </el-menu-item>
      <!-- 分组管理 -->
      <el-menu-item index="/groups">
        <el-icon><UserFilled /></el-icon>
        <template #title>
          <span>{{ t('menu.groups') }}</span>
        </template>
      </el-menu-item>
      <!-- 资源发现 -->
      <el-menu-item index="/resources">
        <el-icon><Search /></el-icon>
        <template #title>
          <span>{{ t('menu.resources') }}</span>
        </template>
      </el-menu-item>
      <!-- 域名管理 -->
      <el-menu-item index="/domains">
        <el-icon><Link /></el-icon>
        <template #title>
          <span>{{ t('menu.domains') }}</span>
        </template>
      </el-menu-item>
      <!-- 授权管理 -->
      <el-sub-menu index="acl">
        <template #title>
          <el-icon><Key /></el-icon>
          <span>{{ t('menu.acl') }}</span>
        </template>
        <el-menu-item index="/acl/services">
          {{ t('menu.aclServices') }}
        </el-menu-item>
        <el-menu-item index="/acl/users">
          {{ t('menu.aclUsers') }}
        </el-menu-item>
        <el-menu-item index="/acl/groups">
          {{ t('menu.aclGroups') }}
        </el-menu-item>
        <el-menu-item index="/acl/ssh">
          {{ t('menu.aclSSH') }}
        </el-menu-item>
        <el-menu-item index="/acl/k8s">
          {{ t('menu.aclK8S') }}
        </el-menu-item>
        <el-menu-item index="/acl/k8s-service">
          {{ t('menu.aclK8SService') }}
        </el-menu-item>
        <el-menu-item index="/acl/endpoint-ssh">
          {{ t('menu.aclEndpointSSH') }}
        </el-menu-item>
      </el-sub-menu>
      <!-- 隧道管理 -->
      <el-sub-menu index="tunnel">
        <template #title>
          <el-icon><Share /></el-icon>
          <span>{{ t('menu.tunnel') }}</span>
        </template>
        <el-menu-item index="/tunnel/users">
          {{ t('menu.tunnelUsers') }}
        </el-menu-item>
        <el-menu-item index="/tunnel/nodes">
          {{ t('menu.tunnelNodes') }}
        </el-menu-item>
        <el-menu-item index="/tunnel/acl">
          {{ t('menu.tunnelACL') }}
        </el-menu-item>
        <el-menu-item index="/tunnel/ssh">
          {{ t('menu.tunnelSSH') }}
        </el-menu-item>
      </el-sub-menu>
      <!-- 审计日志 -->
      <el-sub-menu index="audit">
        <template #title>
          <el-icon><Document /></el-icon>
          <span>{{ t('menu.auditLogs') }}</span>
        </template>
        <el-menu-item index="/audit-logs">
          {{ t('menu.adminAudit') }}
        </el-menu-item>
        <el-menu-item index="/operation-audit">
          {{ t('menu.operationAudit') }}
        </el-menu-item>
      </el-sub-menu>
      <!-- 系统配置 -->
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
  overflow: hidden;
}

.el-menu {
  overflow-y: auto;
  overflow-x: hidden;
}

.el-menu::-webkit-scrollbar {
  width: 0;
  height: 0;
}

.el-menu {
  border-right: none;
  flex: 1;
  background-color: #ffffff !important;
}

.el-menu:not(.el-menu--collapse) {
  width: 200px;
}

:deep(.el-menu),
:deep(.el-menu.el-menu--vertical) {
  background-color: #ffffff !important;
}

:deep(.el-menu-item) {
  height: 56px;
  line-height: 56px;
  padding: 0 20px !important;
  transition: all 0.3s;
  background-color: #ffffff !important;
  color: #303133 !important;
}

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
  margin-right: 12px !important;
}

:deep(.el-menu-item span) {
  font-size: 14px;
  line-height: 1;
}

:deep(.el-menu-item:hover) {
  background-color: #ecf5ff !important;
}

:deep(.el-menu-item.is-active) {
  background-color: #ecf5ff !important;
  color: #409eff !important;
  font-weight: 500;
}

:deep(.el-sub-menu) {
  background-color: #ffffff !important;
}

:deep(.el-sub-menu__title) {
  height: 56px;
  line-height: 56px;
  padding: 0 20px !important;
  background-color: #ffffff !important;
  color: #303133 !important;
  transition: background-color 0.15s ease !important;
}

:deep(.el-sub-menu__title:hover) {
  background-color: #ecf5ff !important;
}

:deep(.el-menu--inline) {
  transition: none !important;
  animation: none !important;
}

:deep(.el-sub-menu .el-menu) {
  transition: none !important;
  animation: none !important;
}

:deep(.el-sub-menu .el-menu-item) {
  transition: background-color 0.15s ease !important;
}

:deep(.el-sub-menu__icon-arrow) {
  transition: transform 0.15s ease !important;
}

:deep(.el-menu--collapse .el-sub-menu) {
  transition: none !important;
}

:deep(.el-sub-menu),
:deep(.el-menu-item) {
  will-change: auto;
  transform: translateZ(0);
  backface-visibility: hidden;
}

:deep(.el-sub-menu__title .el-icon) {
  font-size: 18px;
  width: 18px;
  height: 18px;
  margin-right: 12px !important;
}

:deep(.el-sub-menu .el-menu-item) {
  height: 48px;
  line-height: 48px;
  padding-left: 52px !important;
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

.sidebar-footer {
  height: 48px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-top: 1px solid #e4e7ed;
  background-color: #ffffff;
  cursor: pointer;
  transition: all 0.3s;
  flex-shrink: 0;
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

.sidebar-footer * {
  pointer-events: auto;
}

.sidebar-footer::before,
.sidebar-footer::after {
  display: none;
}
</style>
