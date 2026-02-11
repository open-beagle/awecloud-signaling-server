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
    redirect: '/users',
    meta: { requiresAuth: true },
    children: [
      // 用户管理
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
      // 域名管理
      {
        path: 'domains',
        name: 'Domains',
        component: () => import('@/views/Domain/List.vue'),
        meta: { requiresAuth: true }
      },
      // ACL 授权管理
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
      // K8S 授权
      {
        path: 'acl/k8s',
        name: 'ACLK8S',
        component: () => import('@/views/ACL/K8SList.vue'),
        meta: { requiresAuth: true }
      },
      {
        path: 'acl/k8s/:id',
        name: 'ACLK8SDetail',
        component: () => import('@/views/ACL/K8SDetail.vue'),
        meta: { requiresAuth: true }
      },
      // K8SService 授权
      {
        path: 'acl/k8s-service',
        name: 'ACLK8SService',
        component: () => import('@/views/ACL/K8SServiceList.vue'),
        meta: { requiresAuth: true }
      },
      {
        path: 'acl/k8s-service/:id',
        name: 'ACLK8SServiceDetail',
        component: () => import('@/views/ACL/K8SServiceDetail.vue'),
        meta: { requiresAuth: true }
      },
      // Endpoint 管理
      {
        path: 'endpoints/ssh',
        name: 'EndpointSSH',
        component: () => import('@/views/Endpoint/SSHList.vue'),
        meta: { requiresAuth: true }
      },
      {
        path: 'endpoints/k8sapi',
        name: 'EndpointK8SAPI',
        component: () => import('@/views/Endpoint/K8SAPIList.vue'),
        meta: { requiresAuth: true }
      },
      {
        path: 'endpoints/k8sservice',
        name: 'EndpointK8SService',
        component: () => import('@/views/Endpoint/K8SServiceList.vue'),
        meta: { requiresAuth: true }
      },
      // 资源发现
      {
        path: 'resources',
        name: 'Resources',
        component: () => import('@/views/Resource/List.vue'),
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
      // 系统配置
      {
        path: 'system/config',
        name: 'SystemConfig',
        component: () => import('@/views/System/Config.vue'),
        meta: { requiresAuth: true }
      },
      // 旧路由重定向（兼容）
      {
        path: 'agents',
        redirect: '/users?role=agent'
      },
      {
        path: 'agents/:identifier',
        redirect: to => `/users/${to.params.identifier}`
      },
      {
        path: 'clients',
        redirect: '/users?role=client'
      },
      {
        path: 'clients/:identifier',
        redirect: to => `/users/${to.params.identifier}`
      },
      {
        path: 'groups/clients',
        redirect: '/groups'
      },
      {
        path: 'groups/agents',
        redirect: '/groups'
      },
      {
        path: 'ssh',
        redirect: '/acl/ssh'
      },
      {
        path: 'service-auth/desktop',
        redirect: '/acl/services'
      },
      {
        path: 'service-auth/agent',
        redirect: '/acl/services'
      },
      {
        path: 'agent-auth/desktop',
        redirect: '/acl/users'
      },
      {
        path: 'agent-auth/agent',
        redirect: '/acl/users'
      }
    ]
  }
]

const router = createRouter({
  history: createWebHistory(),
  routes
})

// 路由守卫
router.beforeEach((to, _from, next) => {
  const authStore = useAuthStore()
  
  if (to.meta.requiresAuth !== false && !authStore.isAuthenticated) {
    next({ name: 'Login', query: { redirect: to.fullPath } })
  } else {
    next()
  }
})

export default router
