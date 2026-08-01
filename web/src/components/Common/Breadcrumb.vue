<template>
  <el-breadcrumb v-if="items.length" separator="/" class="breadcrumb" aria-label="面包屑">
    <el-breadcrumb-item :to="{ path: '/' }">首页</el-breadcrumb-item>
    <el-breadcrumb-item v-for="item in items" :key="`${item.title}:${item.path || ''}`" :to="item.path ? { path: item.path } : undefined">
      {{ item.title }}
    </el-breadcrumb-item>
  </el-breadcrumb>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import type { RouteLocationNormalizedLoaded } from 'vue-router'

interface BreadcrumbItem {
  path?: string
  title: string
}

const route = useRoute()
const items = computed<BreadcrumbItem[]>(() => {
  const metadata = route.meta.breadcrumb
  const value = typeof metadata === 'function'
    ? metadata(route as RouteLocationNormalizedLoaded)
    : metadata
  if (!Array.isArray(value)) return []
  return value.filter((item): item is BreadcrumbItem => {
    return Boolean(item && typeof item === 'object' && typeof item.title === 'string')
  })
})
</script>

<style scoped>
.breadcrumb {
  margin-bottom: 16px;
  padding: 4px 0;
  font-size: 13px;
}

.breadcrumb :deep(.el-breadcrumb__item:last-child .el-breadcrumb__inner) {
  color: var(--el-text-color-primary);
  font-weight: 500;
}

.breadcrumb :deep(.el-breadcrumb__item a:hover) {
  color: var(--el-color-primary);
}

@media (max-width: 768px) {
  .breadcrumb { font-size: 12px; }
}
</style>
