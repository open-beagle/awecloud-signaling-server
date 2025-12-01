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
  '/stcp-instances': 'menu.stcpInstances',
  '/groups': 'menu.groups',
  '/audit-logs': 'menu.auditLogs'
}

const items = computed(() => {
  const path = route.path
  const breadcrumbs: Array<{ path: string; title: string }> = [
    { path: '/', title: t('common.home') }
  ]
  
  // 处理二级页面
  if (path.startsWith('/groups/') && path.includes('/members')) {
    breadcrumbs.push({ path: '/groups', title: 'Group管理' })
    breadcrumbs.push({ path: '', title: '成员管理' })
  } else if (path.startsWith('/stcp-instances/') && path.includes('/access')) {
    breadcrumbs.push({ path: '/stcp-instances', title: 'STCP管理' })
    breadcrumbs.push({ path: '', title: '授权管理' })
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
