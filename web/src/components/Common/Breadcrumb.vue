<template>
  <el-breadcrumb separator="/" class="breadcrumb">
    <el-breadcrumb-item v-for="item in items" :key="item.path" :to="item.path">
      {{ item.title }}
    </el-breadcrumb-item>
  </el-breadcrumb>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'

const route = useRoute()
const { t } = useI18n()

// 一级页面映射
const breadcrumbMap: Record<string, string> = {
  '/agents': 'menu.agents',
  '/clients': 'menu.clients',
  '/groups': 'menu.groups',
  '/favorites': 'menu.favorites',
  '/audit-logs': 'menu.auditLogs',
  '/system/config': 'menu.systemConfig'
}

const items = computed(() => {
  const path = route.path
  const breadcrumbs: Array<{ path: string; title: string }> = [
    { path: '/', title: t('common.home') }
  ]
  
  // 处理 Agent 详情页
  if (path.match(/^\/agents\/\d+$/)) {
    breadcrumbs.push({ path: '/agents', title: t('menu.agents') })
    breadcrumbs.push({ path: '', title: t('agent.detail') })
  }
  // 处理服务管理相关页面
  else if (path === '/services') {
    breadcrumbs.push({ path: '', title: t('menu.serviceManagement') })
    breadcrumbs.push({ path: '/services', title: t('menu.serviceList') })
  }
  else if (path === '/services/desktop-auth') {
    breadcrumbs.push({ path: '', title: t('menu.serviceManagement') })
    breadcrumbs.push({ path: '', title: t('menu.desktopAuth') })
  }
  else if (path === '/services/agent-auth') {
    breadcrumbs.push({ path: '', title: t('menu.serviceManagement') })
    breadcrumbs.push({ path: '', title: t('menu.agentAuth') })
  }
  // 处理分组成员页面
  else if (path.match(/^\/groups\/clients\/\d+\/members$/)) {
    breadcrumbs.push({ path: '', title: t('menu.groups') })
    breadcrumbs.push({ path: '/groups/clients', title: t('menu.clientGroups') })
    breadcrumbs.push({ path: '', title: t('group.members') })
  }
  else if (path.match(/^\/groups\/agents\/\d+\/members$/)) {
    breadcrumbs.push({ path: '', title: t('menu.groups') })
    breadcrumbs.push({ path: '/groups/agents', title: t('menu.agentGroups') })
    breadcrumbs.push({ path: '', title: t('group.members') })
  }
  else if (path.match(/^\/groups\/client-groups\/\d+\/members$/)) {
    breadcrumbs.push({ path: '', title: t('menu.groups') })
    breadcrumbs.push({ path: '/groups/clients', title: t('menu.clientGroups') })
    breadcrumbs.push({ path: '', title: t('group.members') })
  }
  else if (path.match(/^\/groups\/agent-groups\/\d+\/members$/)) {
    breadcrumbs.push({ path: '', title: t('menu.groups') })
    breadcrumbs.push({ path: '/groups/agents', title: t('menu.agentGroups') })
    breadcrumbs.push({ path: '', title: t('group.members') })
  }
  else if (path.match(/^\/groups\/\d+\/members$/)) {
    breadcrumbs.push({ path: '/groups', title: t('menu.groups') })
    breadcrumbs.push({ path: '', title: t('group.members') })
  }
  // 一级页面
  else {
    const title = breadcrumbMap[path]
    if (title) {
      breadcrumbs.push({ path, title: t(title) })
    }
  }
  
  return breadcrumbs
})
</script>

<style scoped>
.breadcrumb {
  margin-bottom: 16px;
  font-size: 14px;
}
</style>
