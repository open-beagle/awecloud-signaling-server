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
        path: 'clients',
        name: 'Clients',
        component: () => import('@/views/Client/List.vue'),
        meta: { requiresAuth: true }
      },
      {
        path: 'services/stcp',
        name: 'STCPInstances',
        component: () => import('@/views/STCP/List.vue'),
        meta: { requiresAuth: true }
      },
      {
        path: 'services/tcp',
        name: 'TCPServices',
        component: () => import('@/views/TCP/List.vue'),
        meta: { requiresAuth: true }
      },
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
      {
        path: 'services/stcp/:id/access',
        name: 'STCPAccess',
        component: () => import('@/views/STCP/Access.vue'),
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
      },
      {
        path: 'favorites',
        name: 'Favorites',
        component: () => import('@/views/Favorite/List.vue'),
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
