<template>
  <el-breadcrumb separator="/" class="breadcrumb">
    <el-breadcrumb-item v-for="(item, index) in items" :key="index" :to="item.path || undefined">
      {{ item.title }}
    </el-breadcrumb-item>
  </el-breadcrumb>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'

const route = useRoute()

interface BreadcrumbItem {
  path?: string
  title: string
}

const items = computed(() => {
  const path = route.path
  const breadcrumbs: BreadcrumbItem[] = []
  
  // 根据路由路径生成面包屑
  
  // 1. 代理管理
  if (path === '/agents') {
    breadcrumbs.push({ path: '/agents', title: '代理管理' })
  } else if (path.match(/^\/agents\/\d+$/)) {
    // 从 route.meta 或 query 获取名称，否则使用 ID
    const agentName = (route.meta.breadcrumbName as string) || (route.query.name as string) || `#${route.params.id}`
    breadcrumbs.push({ path: '/agents', title: '代理管理' })
    breadcrumbs.push({ title: `Agent 详情: ${agentName}` })
  }
  
  // 2. 服务管理
  else if (path === '/services') {
    breadcrumbs.push({ path: '/services', title: '服务管理' })
  } else if (path === '/services/desktop-auth') {
    breadcrumbs.push({ title: '服务管理' })
    breadcrumbs.push({ path: '/services/desktop-auth', title: '桌面授权' })
  } else if (path === '/services/agent-auth') {
    breadcrumbs.push({ title: '服务管理' })
    breadcrumbs.push({ path: '/services/agent-auth', title: '代理授权' })
  }
  
  // 3. 客户管理
  else if (path === '/clients') {
    breadcrumbs.push({ path: '/clients', title: '客户管理' })
  } else if (path.match(/^\/clients\/\d+$/)) {
    const clientName = (route.meta.breadcrumbName as string) || (route.query.name as string) || `#${route.params.id}`
    breadcrumbs.push({ path: '/clients', title: '客户管理' })
    breadcrumbs.push({ title: `客户详情: ${clientName}` })
  }
  
  // 4. 分组管理
  else if (path === '/groups/clients') {
    breadcrumbs.push({ title: '分组管理' })
    breadcrumbs.push({ path: '/groups/clients', title: '用户分组' })
  } else if (path.match(/^\/groups\/clients\/\d+\/members$/)) {
    const groupName = (route.meta.breadcrumbName as string) || (route.query.name as string) || `#${route.params.id}`
    breadcrumbs.push({ title: '分组管理' })
    breadcrumbs.push({ path: '/groups/clients', title: '用户分组' })
    breadcrumbs.push({ title: `分组成员: ${groupName}` })
  } else if (path === '/groups/agents') {
    breadcrumbs.push({ title: '分组管理' })
    breadcrumbs.push({ path: '/groups/agents', title: '代理分组' })
  } else if (path.match(/^\/groups\/agents\/\d+\/members$/)) {
    const groupName = (route.meta.breadcrumbName as string) || (route.query.name as string) || `#${route.params.id}`
    breadcrumbs.push({ title: '分组管理' })
    breadcrumbs.push({ path: '/groups/agents', title: '代理分组' })
    breadcrumbs.push({ title: `分组成员: ${groupName}` })
  } else if (path === '/groups') {
    breadcrumbs.push({ path: '/groups/clients', title: '分组管理' })
  }
  
  // 5. 隧道管理
  else if (path === '/tunnel/users') {
    breadcrumbs.push({ title: '隧道管理' })
    breadcrumbs.push({ path: '/tunnel/users', title: 'User 管理' })
  } else if (path === '/tunnel/nodes') {
    breadcrumbs.push({ title: '隧道管理' })
    breadcrumbs.push({ path: '/tunnel/nodes', title: 'Node 管理' })
  } else if (path === '/tunnel/acl') {
    breadcrumbs.push({ title: '隧道管理' })
    breadcrumbs.push({ path: '/tunnel/acl', title: 'ACL 管理' })
  }
  
  // 6. 审计日志
  else if (path === '/audit-logs') {
    breadcrumbs.push({ path: '/audit-logs', title: '审计日志' })
  }
  
  // 7. 系统配置
  else if (path === '/system/config') {
    breadcrumbs.push({ path: '/system/config', title: '系统配置' })
  }
  
  // 8. 下载页面
  else if (path === '/download') {
    breadcrumbs.push({ path: '/download', title: '客户端下载' })
  }
  
  // 默认：首页
  else {
    breadcrumbs.push({ path: '/', title: '首页' })
  }
  
  return breadcrumbs
})
</script>

<style scoped>
.breadcrumb {
  margin-bottom: 16px;
  padding: 12px 0;
  font-size: 14px;
}

.breadcrumb :deep(.el-breadcrumb__item:last-child .el-breadcrumb__inner) {
  color: var(--el-text-color-primary);
  font-weight: 500;
}

.breadcrumb :deep(.el-breadcrumb__item a:hover) {
  color: var(--el-color-primary);
  transition: color 0.2s;
}

/* 响应式设计 */
@media (max-width: 768px) {
  .breadcrumb {
    font-size: 12px;
    padding: 8px 0;
  }
}
</style>
