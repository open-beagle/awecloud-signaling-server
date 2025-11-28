<template>
  <el-container class="layout-container">
    <el-header class="layout-header">
      <Header />
    </el-header>
    <el-container class="layout-body">
      <el-aside :width="sidebarWidth" class="layout-aside">
        <Sidebar />
      </el-aside>
      <el-main class="layout-main">
        <Breadcrumb />
        <router-view />
      </el-main>
    </el-container>
  </el-container>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useAppStore } from '@/stores/app'
import Header from './Header.vue'
import Sidebar from './Sidebar.vue'
import Breadcrumb from '@/components/Common/Breadcrumb.vue'

const appStore = useAppStore()

const sidebarWidth = computed(() => {
  return appStore.sidebarCollapsed ? '64px' : '200px'
})
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
  box-shadow: 0 1px 4px 0 rgba(0, 0, 0, 0.08);
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
  background-color: #f0f2f5;
  padding: 20px;
  overflow-y: auto;
  height: 100%;
}

/* 响应式适配 */
@media (max-width: 768px) {
  .layout-main {
    padding: 12px;
  }
}
</style>
