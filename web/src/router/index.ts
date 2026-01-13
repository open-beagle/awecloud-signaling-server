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
    redirect: '/agents',
    meta: { requiresAuth: true },
    children: [
      {
        path: 'agents',
        name: 'Agents',
        component: () => import('@/views/Agent/List.vue'),
        meta: { requiresAuth: true }
      },
      {
        path: 'agents/:id',
        name: 'AgentDetail',
        component: () => import('@/views/Agent/Detail.vue'),
        meta: { requiresAuth: true }
      },
      {
        path: 'clients',
        name: 'Clients',
        component: () => import('@/views/Client/List.vue'),
        meta: { requiresAuth: true }
      },
      {
        path: 'clients/:id',
        name: 'ClientDetail',
        component: () => import('@/views/Client/Detail.vue'),
        meta: { requiresAuth: true }
      },
      {
        path: 'services',
        redirect: '/services/desktop-auth',
        meta: { requiresAuth: true }
      },
      {
        path: 'services/desktop-auth',
        name: 'DesktopAuth',
        component: () => import('@/views/Service/DesktopAuth.vue'),
        meta: { requiresAuth: true }
      },
      {
        path: 'services/agent-auth',
        name: 'AgentAuth',
        component: () => import('@/views/Service/AgentAuth.vue'),
        meta: { requiresAuth: true }
      },
      // 旧路由重定向
      {
        path: 'services/stcp',
        redirect: '/services',
        meta: { requiresAuth: true }
      },
      {
        path: 'services/stcp-visitors',
        redirect: '/services',
        meta: { requiresAuth: true }
      },
      {
        path: 'services/tcp',
        redirect: '/services',
        meta: { requiresAuth: true }
      },
      {
        path: 'services/stcp/:id/access',
        redirect: '/services',
        meta: { requiresAuth: true }
      },
      {
        path: 'groups/clients',
        name: 'ClientGroups',
        component: () => import('@/views/Group/ClientGroups.vue'),
        meta: { requiresAuth: true }
      },
      {
        path: 'groups/clients/:id/members',
        name: 'ClientGroupMembers',
        component: () => import('@/views/Group/ClientGroupMembers.vue'),
        meta: { requiresAuth: true }
      },
      {
        path: 'groups/agents',
        name: 'AgentGroups',
        component: () => import('@/views/Group/AgentGroups.vue'),
        meta: { requiresAuth: true }
      },
      {
        path: 'groups/agents/:id/members',
        name: 'AgentGroupMembers',
        component: () => import('@/views/Group/AgentGroupMembers.vue'),
        meta: { requiresAuth: true }
      },
      // 隧道管理
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
      // 旧路由重定向
      {
        path: 'groups',
        redirect: '/groups/clients',
        meta: { requiresAuth: true }
      },
      {
        path: 'groups/:id/members',
        redirect: to => `/groups/clients/${to.params.id}/members`,
        meta: { requiresAuth: true }
      },
      {
        path: 'audit-logs',
        name: 'AuditLogs',
        component: () => import('@/views/Audit/AuditLogs.vue'),
        meta: { requiresAuth: true }
      },
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
router.beforeEach((to, from, next) => {
  const authStore = useAuthStore()
  
  if (to.meta.requiresAuth && !authStore.isAuthenticated()) {
    next('/login')
  } else if (to.path === '/login' && authStore.isAuthenticated()) {
    next('/')
  } else {
    next()
  }
})

export default router
