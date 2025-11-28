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
  const title = breadcrumbMap[path]
  
  if (!title) return []
  
  return [
    { path: '/', title: t('common.home') },
    { path, title: t(title) }
  ]
})
</script>

<style scoped>
.breadcrumb {
  margin-bottom: 16px;
  font-size: 14px;
}
</style>
