import { createRouter, createWebHistory } from 'vue-router'
import type { RouteRecordRaw } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

const routes: RouteRecordRaw[] = [
  {
    path: '/login',
    name: 'Login',
    component: () => import('@/views/Login.vue'),
    meta: { requiresAuth: false }
  },
  {
    path: '/download',
    name: 'Download',
    component: () => import('@/views/Download.vue'),
    meta: { requiresAuth: false }
  },
  {
    path: '/',
    component: () => import('@/components/Layout/Layout.vue'),
    redirect: '/resources',
    meta: { requiresAuth: true },
    children: [
      // 用户管理
      {
        path: 'tenants',
        name: 'Tenants',
        component: () => import('@/views/Tenant/List.vue'),
        meta: { requiresAuth: true }
      },
      {
        path: 'users',
        name: 'Users',
        component: () => import('@/views/User/List.vue'),
        meta: { requiresAuth: true }
      },
      {
        path: 'users/:username',
        name: 'UserDetail',
        component: () => import('@/views/User/Detail.vue'),
        meta: { requiresAuth: true }
      },
      // 设备管理
      {
        path: 'nodes',
        redirect: '/nodes/agents',
        meta: { requiresAuth: true }
      },
      {
        path: 'nodes/agents',
        name: 'AgentNodes',
        component: () => import('@/views/Node/List.vue'),
        props: { fixedType: 'agent' },
        meta: { requiresAuth: true }
      },
      {
        path: 'nodes/desktops',
        name: 'DesktopNodes',
        component: () => import('@/views/Node/List.vue'),
        props: { fixedType: 'desktop' },
        meta: { requiresAuth: true }
      },
      {
        path: 'nodes/all',
        name: 'Nodes',
        component: () => import('@/views/Node/List.vue'),
        meta: { requiresAuth: true }
      },
      {
        path: 'nodes/:id',
        name: 'NodeDetail',
        component: () => import('@/views/Node/Detail.vue'),
        meta: { requiresAuth: true }
      },
      // 终端管理
      {
        path: 'endpoints',
        name: 'Endpoints',
        component: () => import('@/views/Endpoint/List.vue'),
        meta: { requiresAuth: true }
      },
      {
        path: 'endpoints/:id',
        name: 'EndpointDetail',
        component: () => import('@/views/Endpoint/Detail.vue'),
        meta: { requiresAuth: true }
      },
      {
        path: 'infrastructure/integrations',
        name: 'Integrations',
        component: () => import('@/views/Integration/List.vue'),
        meta: { requiresAuth: true }
      },
      // 分组管理
      {
        path: 'groups',
        name: 'Groups',
        component: () => import('@/views/Group/List.vue'),
        meta: { requiresAuth: true }
      },
      {
        path: 'groups/:id/members',
        name: 'GroupMembers',
        component: () => import('@/views/Group/Members.vue'),
        meta: { requiresAuth: true }
      },
      // 资源发现
      {
        path: 'resources',
        name: 'Resources',
        component: () => import('@/views/Resource/List.vue'),
        meta: { requiresAuth: true }
      },
      {
        path: 'resources/:id',
        name: 'ResourceDetail',
        component: () => import('@/views/Resource/Detail.vue'),
        meta: { requiresAuth: true }
      },
      {
        path: 'resource-candidates',
        name: 'ResourceCandidates',
        component: () => import('@/views/Resource/Candidates.vue'),
        meta: { requiresAuth: true }
      },
      {
        path: 'legacy-inventory',
        name: 'LegacyInventory',
        component: () => import('@/views/LegacyInventory/List.vue'),
        meta: { requiresAuth: true }
      },
      {
        path: 'access-policies',
        name: 'AccessPolicies',
        component: () => import('@/views/AccessPolicy/List.vue'),
        meta: { requiresAuth: true }
      },
      {
        path: 'sessions',
        name: 'Sessions',
        component: () => import('@/views/Session/List.vue'),
        meta: { requiresAuth: true }
      },
      // 域名管理
      {
        path: 'domains',
        name: 'Domains',
        component: () => import('@/views/Domain/List.vue'),
        meta: { requiresAuth: true }
      },
      // 授权管理
      {
        path: 'acl/services',
        name: 'ACLServices',
        component: () => import('@/views/ACL/ServiceList.vue'),
        meta: { requiresAuth: true }
      },
      {
        path: 'acl/services/:id',
        name: 'ACLServiceDetail',
        component: () => import('@/views/ACL/ServiceDetail.vue'),
        meta: { requiresAuth: true }
      },
      {
        path: 'acl/users',
        name: 'ACLUsers',
        component: () => import('@/views/ACL/UserList.vue'),
        meta: { requiresAuth: true }
      },
      {
        path: 'acl/users/:id',
        name: 'ACLUserDetail',
        component: () => import('@/views/ACL/UserDetail.vue'),
        meta: { requiresAuth: true }
      },
      {
        path: 'acl/groups',
        name: 'ACLGroups',
        component: () => import('@/views/ACL/GroupList.vue'),
        meta: { requiresAuth: true }
      },
      {
        path: 'acl/groups/:id',
        name: 'ACLGroupDetail',
        component: () => import('@/views/ACL/GroupDetail.vue'),
        meta: { requiresAuth: true }
      },
      {
        path: 'acl/ssh',
        name: 'ACLSSH',
        component: () => import('@/views/ACL/SSHList.vue'),
        meta: { requiresAuth: true }
      },
      {
        path: 'acl/ssh/:id',
        name: 'ACLSSHDetail',
        component: () => import('@/views/ACL/SSHDetail.vue'),
        meta: { requiresAuth: true }
      },
      // K8S API 授权（P9：合并展示 Agent + Endpoint）
      {
        path: 'acl/k8s',
        name: 'ACLK8S',
        component: () => import('@/views/ACL/K8SUnifiedList.vue'),
        meta: { requiresAuth: true }
      },
      {
        path: 'acl/k8s/:id',
        name: 'ACLK8SDetail',
        component: () => import('@/views/ACL/K8SDetail.vue'),
        meta: { requiresAuth: true }
      },
      // K8SService 授权（P9：合并展示 Agent + Endpoint）
      {
        path: 'acl/k8s-service',
        name: 'ACLK8SService',
        component: () => import('@/views/ACL/K8SServiceUnifiedList.vue'),
        meta: { requiresAuth: true }
      },
      {
        path: 'acl/k8s-service/:id',
        name: 'ACLK8SServiceDetail',
        component: () => import('@/views/ACL/K8SServiceDetail.vue'),
        meta: { requiresAuth: true }
      },
      // 隧道管理（保留旧路由）
      {
        path: 'tunnel/users',
        name: 'TunnelUsers',
        component: () => import('@/views/Tunnel/Users.vue'),
        meta: { requiresAuth: true }
      },
      {
        path: 'tunnel/nodes',
        name: 'TunnelNodes',
        component: () => import('@/views/Tunnel/Nodes.vue'),
        meta: { requiresAuth: true }
      },
      {
        path: 'tunnel/acl',
        name: 'TunnelACL',
        component: () => import('@/views/Tunnel/ACL.vue'),
        meta: { requiresAuth: true }
      },
      {
        path: 'tunnel/ssh',
        name: 'TunnelSSH',
        component: () => import('@/views/Tunnel/SSHPolicy.vue'),
        meta: { requiresAuth: true }
      },
      // 审计日志
      {
        path: 'audit-logs',
        name: 'AuditLogs',
        component: () => import('@/views/Audit/AuditLogs.vue'),
        meta: { requiresAuth: true }
      },
      // 操作审计
      {
        path: 'operation-audit',
        name: 'OperationAudit',
        component: () => import('@/views/Audit/OperationAuditLogs.vue'),
        meta: { requiresAuth: true }
      },
      // 系统配置
      {
        path: 'system/config',
        name: 'SystemConfig',
        component: () => import('@/views/System/Config.vue'),
        meta: { requiresAuth: true }
      }
    ]
  }
]

const router = createRouter({
  history: createWebHistory(),
  routes
})

// 路由守卫
router.beforeEach(async (to, _from, next) => {
  const authStore = useAuthStore()

  if (to.meta.requiresAuth !== false && !authStore.isAuthenticated) {
    next({ name: 'Login', query: { redirect: to.fullPath } })
	return
  }
  if (authStore.isAuthenticated && !authStore.role) {
    try { await authStore.loadProfile() } catch { return }
  }
  if (authStore.role === 'tenant_admin' && !tenantAdminPageAllowed(to.path)) {
    next('/resources')
    return
  }
  next()
})

const tenantAdminPageAllowed = (path: string) => path === '/resources' || path.startsWith('/resources/') || path === '/access-policies' || path === '/sessions' || path === '/groups' || /^\/groups\/\d+\/members$/.test(path)

export default router
