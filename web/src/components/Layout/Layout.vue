<template>
  <el-container class="layout-container">
    <el-header class="layout-header">
      <Header />
    </el-header>
    <el-container class="layout-body">
      <el-aside :width="sidebarWidth" class="layout-aside" :class="{ 'mobile-open': appStore.mobileSidebarOpen }">
        <Sidebar />
      </el-aside>
      <button v-if="appStore.mobileSidebarOpen" class="mobile-backdrop" aria-label="关闭导航" @click="appStore.closeMobileSidebar" />
      <el-main class="layout-main">
        <Breadcrumb v-if="showLegacyBreadcrumb" />
        <router-view />
      </el-main>
    </el-container>
  </el-container>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { useAppStore } from '@/stores/app'
import Header from './Header.vue'
import Sidebar from './Sidebar.vue'
import Breadcrumb from '@/components/Common/Breadcrumb.vue'

const appStore = useAppStore()
const route = useRoute()

const sidebarWidth = computed(() => {
  return appStore.sidebarCollapsed ? '64px' : '200px'
})
const modernPaths = ['/resources', '/resource-candidates', '/legacy-inventory', '/access-policies', '/sessions', '/tenants', '/infrastructure/integrations']
const showLegacyBreadcrumb = computed(() => !modernPaths.some(path => route.path === path || route.path.startsWith(`${path}/`)))
</script>

<style scoped>
.layout-container {
  width: 100%;
  height: 100%;
  display: flex;
  flex-direction: column;
}

.layout-header {
  height: 60px;
  background-color: #ffffff;
  border-bottom: 1px solid #e4e7ed;
  padding: 0 20px;
  display: flex;
  align-items: center;
  box-shadow: none;
  z-index: 10;
  flex-shrink: 0;
}

.layout-body {
  flex: 1;
  overflow: hidden;
}

.layout-aside {
  background-color: #ffffff;
  border-right: 1px solid #e4e7ed;
  transition: width 0.3s;
  height: 100%;
}

.layout-main {
  background-color: var(--bg-page);
  padding: 20px 24px 28px;
  overflow-y: auto;
  height: 100%;
}

/* 响应式适配 */
@media (max-width: 768px) {
  .layout-header { height: 104px; padding: 0 12px; }
  .layout-aside { position: fixed; top: 104px; bottom: 0; left: 0; z-index: 30; width: 240px !important; transform: translateX(-100%); transition: transform 0.2s ease; }
  .layout-aside.mobile-open { transform: translateX(0); }
  .mobile-backdrop { position: fixed; inset: 104px 0 0; z-index: 20; border: 0; background: rgba(24, 30, 42, 0.32); }
  .layout-main {
    padding: 16px 12px 24px;
  }
}
</style>
