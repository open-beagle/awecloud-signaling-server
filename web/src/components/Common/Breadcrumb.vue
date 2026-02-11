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
  
  // 1. 用户管理
  if (path === '/users') {
    breadcrumbs.push({ path: '/users', title: '用户管理' })
  } else if (path.startsWith('/users/')) {
    const username = route.params.username as string
    breadcrumbs.push({ path: '/users', title: '用户管理' })
    breadcrumbs.push({ title: `用户详情: ${username}` })
  }
  
  // 2. 设备管理
  else if (path === '/nodes') {
    breadcrumbs.push({ path: '/nodes', title: '设备管理' })
  } else if (path.match(/^\/nodes\/\d+$/)) {
    const nodeName = (route.query.name as string) || `#${route.params.id}`
    breadcrumbs.push({ path: '/nodes', title: '设备管理' })
    breadcrumbs.push({ title: `设备详情: ${nodeName}` })
  }
  
  // 3. 分组管理
  else if (path === '/groups') {
    breadcrumbs.push({ path: '/groups', title: '分组管理' })
  } else if (path.match(/^\/groups\/\d+\/members$/)) {
    const groupName = (route.query.name as string) || `#${route.params.id}`
    breadcrumbs.push({ path: '/groups', title: '分组管理' })
    breadcrumbs.push({ title: `成员管理: ${groupName}` })
  }
  
  // 4. 授权管理 - 服务授权
  else if (path === '/acl/services') {
    breadcrumbs.push({ title: '授权管理' })
    breadcrumbs.push({ path: '/acl/services', title: '服务授权' })
  } else if (path.match(/^\/acl\/services\/\d+$/)) {
    const serviceName = (route.query.name as string) || `#${route.params.id}`
    breadcrumbs.push({ title: '授权管理' })
    breadcrumbs.push({ path: '/acl/services', title: '服务授权' })
    breadcrumbs.push({ title: `授权详情: ${serviceName}` })
  }
  
  // 5. 授权管理 - 用户授权
  else if (path === '/acl/users') {
    breadcrumbs.push({ title: '授权管理' })
    breadcrumbs.push({ path: '/acl/users', title: '用户授权' })
  } else if (path.match(/^\/acl\/users\/\d+$/)) {
    const userName = (route.query.name as string) || `#${route.params.id}`
    breadcrumbs.push({ title: '授权管理' })
    breadcrumbs.push({ path: '/acl/users', title: '用户授权' })
    breadcrumbs.push({ title: `授权详情: ${userName}` })
  }
  
  // 6. 授权管理 - 分组授权
  else if (path === '/acl/groups') {
    breadcrumbs.push({ title: '授权管理' })
    breadcrumbs.push({ path: '/acl/groups', title: '分组授权' })
  } else if (path.match(/^\/acl\/groups\/\d+$/)) {
    const groupName = (route.query.name as string) || `#${route.params.id}`
    breadcrumbs.push({ title: '授权管理' })
    breadcrumbs.push({ path: '/acl/groups', title: '分组授权' })
    breadcrumbs.push({ title: `授权详情: ${groupName}` })
  }
  
  // 7. 授权管理 - SSH 授权
  else if (path === '/acl/ssh') {
    breadcrumbs.push({ title: '授权管理' })
    breadcrumbs.push({ path: '/acl/ssh', title: 'SSH 授权' })
  } else if (path.match(/^\/acl\/ssh\/\d+$/)) {
    const sshName = (route.query.name as string) || `#${route.params.id}`
    breadcrumbs.push({ title: '授权管理' })
    breadcrumbs.push({ path: '/acl/ssh', title: 'SSH 授权' })
    breadcrumbs.push({ title: `授权详情: ${sshName}` })
  }
  
  // 8. 授权管理 - K8S API 授权
  else if (path === '/acl/k8s') {
    breadcrumbs.push({ title: '授权管理' })
    breadcrumbs.push({ path: '/acl/k8s', title: 'K8S API 授权' })
  } else if (path.match(/^\/acl\/k8s\/\d+$/)) {
    const k8sName = (route.query.name as string) || `#${route.params.id}`
    breadcrumbs.push({ title: '授权管理' })
    breadcrumbs.push({ path: '/acl/k8s', title: 'K8S API 授权' })
    breadcrumbs.push({ title: `授权详情: ${k8sName}` })
  }
  
  // 9. 授权管理 - K8S Service 授权
  else if (path === '/acl/k8s-service') {
    breadcrumbs.push({ title: '授权管理' })
    breadcrumbs.push({ path: '/acl/k8s-service', title: 'K8S Service 授权' })
  } else if (path.match(/^\/acl\/k8s-service\/\d+$/)) {
    const k8sSvcName = (route.query.name as string) || `#${route.params.id}`
    breadcrumbs.push({ title: '授权管理' })
    breadcrumbs.push({ path: '/acl/k8s-service', title: 'K8S Service 授权' })
    breadcrumbs.push({ title: `授权详情: ${k8sSvcName}` })
  }
  
  // 10. Endpoint 管理
  else if (path === '/endpoints/ssh') {
    breadcrumbs.push({ title: 'Endpoint 管理' })
    breadcrumbs.push({ path: '/endpoints/ssh', title: 'SSH' })
  } else if (path === '/endpoints/k8sapi') {
    breadcrumbs.push({ title: 'Endpoint 管理' })
    breadcrumbs.push({ path: '/endpoints/k8sapi', title: 'K8S API' })
  } else if (path === '/endpoints/k8sservice') {
    breadcrumbs.push({ title: 'Endpoint 管理' })
    breadcrumbs.push({ path: '/endpoints/k8sservice', title: 'K8S Service' })
  }
  
  // 11. 资源发现
  else if (path === '/resources') {
    breadcrumbs.push({ path: '/resources', title: '资源发现' })
  }
  
  // 12. 隧道管理
  else if (path === '/tunnel/users') {
    breadcrumbs.push({ title: '隧道管理' })
    breadcrumbs.push({ path: '/tunnel/users', title: 'User 管理' })
  } else if (path === '/tunnel/nodes') {
    breadcrumbs.push({ title: '隧道管理' })
    breadcrumbs.push({ path: '/tunnel/nodes', title: 'Node 管理' })
  } else if (path === '/tunnel/acl') {
    breadcrumbs.push({ title: '隧道管理' })
    breadcrumbs.push({ path: '/tunnel/acl', title: 'ACL 管理' })
  } else if (path === '/tunnel/ssh') {
    breadcrumbs.push({ title: '隧道管理' })
    breadcrumbs.push({ path: '/tunnel/ssh', title: 'SSH 策略' })
  }
  
  // 13. 域名管理
  else if (path === '/domains') {
    breadcrumbs.push({ path: '/domains', title: '域名管理' })
  }
  
  // 14. 审计日志
  else if (path === '/audit-logs') {
    breadcrumbs.push({ path: '/audit-logs', title: '审计日志' })
  }
  
  // 15. 系统配置
  else if (path === '/system/config') {
    breadcrumbs.push({ path: '/system/config', title: '系统配置' })
  }
  
  // 16. 下载页面
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
