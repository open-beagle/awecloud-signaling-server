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
      active-text-color="#4a90e2"
      router
    >
      <el-menu-item index="/agents">
        <el-icon><Monitor /></el-icon>
        <template #title>{{ t('menu.agents') }}</template>
      </el-menu-item>
      <el-menu-item index="/clients">
        <el-icon><User /></el-icon>
        <template #title>{{ t('menu.clients') }}</template>
      </el-menu-item>
      <el-menu-item index="/stcp-instances">
        <el-icon><Connection /></el-icon>
        <template #title>{{ t('menu.stcpInstances') }}</template>
      </el-menu-item>
      <el-menu-item index="/groups">
        <el-icon><UserFilled /></el-icon>
        <template #title>用户组</template>
      </el-menu-item>
      <el-menu-item index="/audit-logs">
        <el-icon><Document /></el-icon>
        <template #title>{{ t('menu.auditLogs') }}</template>
      </el-menu-item>
    </el-menu>
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
</script>

<style scoped>
.sidebar {
  height: 100%;
  display: flex;
  flex-direction: column;
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
}

.el-menu {
  border-right: none;
  flex: 1;
}

.el-menu:not(.el-menu--collapse) {
  width: 200px;
}
</style>
