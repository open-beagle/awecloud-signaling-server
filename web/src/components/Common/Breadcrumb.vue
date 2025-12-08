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
  
  // 处理服务管理相关页面
  if (path.startsWith('/services/stcp/') && path.includes('/access')) {
    // STCP授权管理页面
    breadcrumbs.push({ path: '', title: t('menu.serviceManagement') })
    breadcrumbs.push({ path: '/services/stcp', title: t('menu.stcpInstances') })
    breadcrumbs.push({ path: '', title: t('stcp.grantAccess') })
  } else if (path.startsWith('/services/stcp-visitors')) {
    breadcrumbs.push({ path: '', title: t('menu.serviceManagement') })
    breadcrumbs.push({ path: '/services/stcp-visitors', title: t('menu.stcpVisitors') })
  } else if (path.startsWith('/services/stcp')) {
    breadcrumbs.push({ path: '', title: t('menu.serviceManagement') })
    breadcrumbs.push({ path: '/services/stcp', title: t('menu.stcpInstances') })
  } else if (path.startsWith('/services/tcp')) {
    breadcrumbs.push({ path: '', title: t('menu.serviceManagement') })
    breadcrumbs.push({ path: '/services/tcp', title: t('menu.tcpInstances') })
  } else if (path.startsWith('/groups/') && path.includes('/members')) {
    breadcrumbs.push({ path: '/groups', title: 'Group管理' })
    breadcrumbs.push({ path: '', title: '成员管理' })
  } else {
    // 一级页面
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
