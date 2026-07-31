import { createRouter, createWebHistory } from 'vue-router'
import type { RouteRecordRaw } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useTenantStore } from '@/stores/tenant'

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
    redirect: () => ['admin', 'viewer', 'platform_admin', 'platform_viewer'].includes(localStorage.getItem('admin_role') || '')
      ? '/platform-overview'
      : '/tenant-overview',
    meta: { requiresAuth: true },
    children: [
		{
			path: 'platform-overview',
			name: 'PlatformOverview',
			component: () => import('@/views/Platform/Overview.vue'),
			meta: { requiresAuth: true, scope: 'platform', menuDomain: 'platform' }
		},
		{
			path: 'tenant-overview',
			name: 'TenantOverview',
			component: () => import('@/views/Tenant/Overview.vue'),
			meta: { requiresAuth: true, scope: 'tenant', permission: 'tenant.overview.read', menuDomain: 'tenant' }
		},
		{
			path: 'tenant-switch',
			name: 'TenantSwitch',
			component: () => import('@/views/Tenant/Switch.vue'),
			meta: { requiresAuth: true, scope: 'platform' }
		},
		{
			path: 'tenant-members',
			name: 'TenantMembers',
			component: () => import('@/views/Tenant/Members.vue'),
			meta: { requiresAuth: true, scope: 'tenant', permission: 'tenant.members.read' }
		},
		{
			path: 'tenant-member-devices',
			name: 'TenantMemberDevices',
			component: () => import('@/views/Tenant/MemberDevices.vue'),
			meta: { requiresAuth: true, scope: 'tenant', permission: 'tenant.devices.read', menuDomain: 'tenant' }
		},
		{
			path: 'tenant-audit',
			name: 'TenantAudit',
			component: () => import('@/views/Tenant/Audit.vue'),
			meta: { requiresAuth: true, scope: 'tenant', permission: 'tenant.audit.read', menuDomain: 'tenant' }
		},
		{
			path: 'tenant-settings',
			name: 'TenantSettings',
			component: () => import('@/views/Tenant/Settings.vue'),
			meta: { requiresAuth: true, scope: 'tenant', permission: 'tenant.settings.read', menuDomain: 'tenant' }
		},
      // 用户管理
      {
        path: 'tenants',
        name: 'Tenants',
        component: () => import('@/views/Tenant/List.vue'),
		meta: { requiresAuth: true, scope: 'platform' }
      },
		{
			path: 'tenant-admin-memberships',
			name: 'TenantAdminMemberships',
			component: () => import('@/views/Tenant/AdminMemberships.vue'),
			meta: { requiresAuth: true, scope: 'platform', menuDomain: 'platform' }
		},
		{
			path: 'platform-admins',
			name: 'PlatformAdmins',
			component: () => import('@/views/Platform/AdminAccounts.vue'),
			meta: { requiresAuth: true, scope: 'platform', menuDomain: 'platform' }
		},
      {
        path: 'platform-identities',
        name: 'PlatformIdentities',
        component: () => import('@/views/User/List.vue'),
        meta: { requiresAuth: true, scope: 'platform', menuDomain: 'platform' }
      },
      {
        path: 'platform-identities/:username',
        name: 'PlatformIdentityDetail',
        component: () => import('@/views/User/Detail.vue'),
        meta: { requiresAuth: true, scope: 'platform', menuDomain: 'platform' }
      },
      {
        path: 'users',
        redirect: to => ({ name: 'PlatformIdentities', query: to.query }),
        meta: { requiresAuth: true, scope: 'platform' }
      },
      {
        path: 'users/:username',
        redirect: to => ({ name: 'PlatformIdentityDetail', params: to.params, query: to.query }),
        meta: { requiresAuth: true, scope: 'platform' }
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
        path: 'diagnostics/desktop-nodes',
        name: 'DiagnosticDesktopNodes',
        component: () => import('@/views/Node/List.vue'),
        props: { fixedType: 'desktop' },
        meta: { requiresAuth: true, scope: 'platform', menuDomain: 'platform' }
      },
      {
        path: 'diagnostics/nodes',
        name: 'DiagnosticNodes',
        component: () => import('@/views/Node/List.vue'),
        meta: { requiresAuth: true, scope: 'platform', menuDomain: 'platform' }
      },
      {
        path: 'nodes/desktops',
        redirect: to => ({ name: 'DiagnosticDesktopNodes', query: to.query }),
        meta: { requiresAuth: true, scope: 'platform' }
      },
      {
        path: 'nodes/all',
        redirect: to => ({ name: 'DiagnosticNodes', query: to.query }),
        meta: { requiresAuth: true, scope: 'platform' }
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
		meta: { requiresAuth: true, scope: 'tenant', permission: 'tenant.groups.read' }
      },
      {
        path: 'groups/:id/members',
        name: 'GroupMembers',
        component: () => import('@/views/Group/Members.vue'),
		meta: { requiresAuth: true, scope: 'tenant', permission: 'tenant.groups.read' }
      },
      // 资源发现
      {
        path: 'resources',
        name: 'Resources',
        component: () => import('@/views/Resource/List.vue'),
		meta: { requiresAuth: true, scope: 'tenant', permission: 'tenant.resources.read' }
      },
      {
        path: 'resources/:id',
        name: 'ResourceDetail',
        component: () => import('@/views/Resource/Detail.vue'),
		meta: { requiresAuth: true, scope: 'tenant', permission: 'tenant.resources.read' }
      },
		{
			path: 'resource-candidates',
        name: 'ResourceCandidates',
        component: () => import('@/views/Resource/Candidates.vue'),
		meta: { requiresAuth: true, scope: 'platform' }
		},
		{
			path: 'platform-resources',
			name: 'PlatformResources',
			component: () => import('@/views/Platform/Resources.vue'),
			meta: { requiresAuth: true, scope: 'platform', menuDomain: 'platform' }
		},
      {
        path: 'legacy-inventory',
        name: 'LegacyInventory',
        component: () => import('@/views/LegacyInventory/List.vue'),
		meta: { requiresAuth: true, scope: 'platform' }
      },
      {
        path: 'access-policies',
        name: 'AccessPolicies',
        component: () => import('@/views/AccessPolicy/List.vue'),
		meta: { requiresAuth: true, scope: 'tenant', permission: 'tenant.grants.read' }
      },
      {
        path: 'sessions',
        name: 'Sessions',
        component: () => import('@/views/Session/List.vue'),
		meta: { requiresAuth: true, scope: 'tenant', permission: 'tenant.sessions.read' }
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
        path: 'platform-audit',
        name: 'PlatformAudit',
        component: () => import('@/views/Audit/AuditLogs.vue'),
        meta: { requiresAuth: true, scope: 'platform', menuDomain: 'platform' }
      },
      // 操作审计
      {
        path: 'diagnostics/operation-audit',
        name: 'DiagnosticOperationAudit',
        component: () => import('@/views/Audit/OperationAuditLogs.vue'),
        meta: { requiresAuth: true, scope: 'platform', menuDomain: 'platform' }
      },
      {
        path: 'audit-logs',
        redirect: to => ({ name: 'PlatformAudit', query: to.query }),
        meta: { requiresAuth: true, scope: 'platform' }
      },
      {
        path: 'operation-audit',
        redirect: to => ({ name: 'DiagnosticOperationAudit', query: to.query }),
        meta: { requiresAuth: true, scope: 'platform' }
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

type BreadcrumbMetaItem = { title: string; path?: string }

const breadcrumbByRouteName: Record<string, BreadcrumbMetaItem[]> = {
	PlatformOverview: [{ title: '管理员' }, { title: '平台概览' }],
	TenantOverview: [{ title: '租户业务' }, { title: '租户概览' }],
	TenantSwitch: [{ title: '管理员' }, { title: '租户切换' }],
	TenantMembers: [{ title: '租户业务' }, { title: '成员' }],
	TenantMemberDevices: [{ title: '租户业务' }, { title: '成员设备' }],
	TenantAudit: [{ title: '租户业务' }, { title: '租户审计' }],
	TenantSettings: [{ title: '租户业务' }, { title: '租户设置' }],
	Tenants: [{ title: '租户治理' }, { title: '租户管理' }],
	TenantAdminMemberships: [{ title: '租户治理' }, { title: '租户管理员授权' }],
	PlatformAdmins: [{ title: '平台治理' }, { title: '平台管理账号' }],
	PlatformIdentities: [{ title: '平台治理' }, { title: '访问主体目录', path: '/platform-identities' }],
	PlatformIdentityDetail: [{ title: '访问主体目录', path: '/platform-identities' }, { title: '主体详情' }],
	AgentNodes: [{ title: '设备管理' }, { title: '代理设备', path: '/nodes/agents' }],
	DiagnosticDesktopNodes: [{ title: '高级诊断' }, { title: 'Desktop 节点', path: '/diagnostics/desktop-nodes' }],
	DiagnosticNodes: [{ title: '高级诊断' }, { title: '全部节点', path: '/diagnostics/nodes' }],
	NodeDetail: [{ title: '设备管理' }, { title: '设备详情' }],
	Endpoints: [{ title: '终端管理', path: '/endpoints' }],
	EndpointDetail: [{ title: '终端管理', path: '/endpoints' }, { title: '终端详情' }],
	Integrations: [{ title: '基础设施' }, { title: '集成' }],
	Groups: [{ title: '租户业务' }, { title: '成员分组', path: '/groups' }],
	GroupMembers: [{ title: '成员分组', path: '/groups' }, { title: '成员管理' }],
	Resources: [{ title: '租户业务' }, { title: '资源目录' }],
	ResourceDetail: [{ title: '资源目录', path: '/resources' }, { title: '资源详情' }],
	ResourceCandidates: [{ title: '资源治理' }, { title: '发现候选' }],
	PlatformResources: [{ title: '资源治理' }, { title: '全局资源目录' }],
	LegacyInventory: [{ title: '资源治理' }, { title: '存量认领' }],
	AccessPolicies: [{ title: '租户业务' }, { title: '访问策略' }],
	Sessions: [{ title: '租户业务' }, { title: '活动会话' }],
	Domains: [{ title: '域名管理', path: '/domains' }],
	ACLServices: [{ title: '授权管理' }, { title: '服务授权', path: '/acl/services' }],
	ACLServiceDetail: [{ title: '服务授权', path: '/acl/services' }, { title: '授权详情' }],
	ACLUsers: [{ title: '授权管理' }, { title: '用户授权', path: '/acl/users' }],
	ACLUserDetail: [{ title: '用户授权', path: '/acl/users' }, { title: '授权详情' }],
	ACLGroups: [{ title: '授权管理' }, { title: '分组授权', path: '/acl/groups' }],
	ACLGroupDetail: [{ title: '分组授权', path: '/acl/groups' }, { title: '授权详情' }],
	ACLSSH: [{ title: '授权管理' }, { title: 'SSH 授权', path: '/acl/ssh' }],
	ACLSSHDetail: [{ title: 'SSH 授权', path: '/acl/ssh' }, { title: '授权详情' }],
	ACLK8S: [{ title: '授权管理' }, { title: 'Kubernetes 授权', path: '/acl/k8s' }],
	ACLK8SDetail: [{ title: 'Kubernetes 授权', path: '/acl/k8s' }, { title: '授权详情' }],
	ACLK8SService: [{ title: '授权管理' }, { title: 'Kubernetes Service 授权', path: '/acl/k8s-service' }],
	ACLK8SServiceDetail: [{ title: 'Kubernetes Service 授权', path: '/acl/k8s-service' }, { title: '授权详情' }],
	TunnelUsers: [{ title: '隧道管理' }, { title: 'User 管理' }],
	TunnelNodes: [{ title: '隧道管理' }, { title: 'Node 管理' }],
	TunnelACL: [{ title: '隧道管理' }, { title: 'ACL 管理' }],
	TunnelSSH: [{ title: '隧道管理' }, { title: 'SSH 策略' }],
	PlatformAudit: [{ title: '平台治理' }, { title: '平台审计' }],
	DiagnosticOperationAudit: [{ title: '高级诊断' }, { title: '连接操作审计' }],
	SystemConfig: [{ title: '系统配置' }]
}

const normalizeProtectedRouteMetadata = (records: RouteRecordRaw[]) => {
	for (const route of records) {
		if (route.meta?.requiresAuth !== false) {
			const routeName = typeof route.name === 'string' ? route.name : ''
			route.meta = {
				...route.meta,
				scope: route.meta?.scope || 'platform',
				menuDomain: route.meta?.menuDomain || (route.meta?.scope === 'tenant' ? 'tenant' : 'platform'),
				...(breadcrumbByRouteName[routeName] ? { breadcrumb: breadcrumbByRouteName[routeName] } : {})
			}
		}
		if ('children' in route && route.children) normalizeProtectedRouteMetadata(route.children)
	}
}

normalizeProtectedRouteMetadata(routes)

const router = createRouter({
  history: createWebHistory(),
  routes
})

// 路由守卫
router.beforeEach(async (to, _from, next) => {
  const authStore = useAuthStore()
	const tenantStore = useTenantStore()

	if (to.meta.requiresAuth === false) {
		next()
		return
	}
  if (!authStore.isAuthenticated) {
    next({ name: 'Login', query: { redirect: to.fullPath } })
	return
  }
  if (!authStore.role) {
    try { await authStore.loadProfile() } catch { return }
  }
	const scope = to.meta.scope || 'platform'
	if (scope === 'platform' && !authStore.hasPlatformScope) {
		try { await tenantStore.loadContexts() } catch { next(false); return }
		next(firstTenantDestination(tenantStore))
		return
	}
	if (scope === 'tenant') {
		try { await tenantStore.loadContexts() } catch { next(false); return }
		if (!tenantStore.current) {
			if (authStore.hasPlatformScope) next('/tenant-switch')
			else next(false)
			return
		}
		const permission = typeof to.meta.permission === 'string' ? to.meta.permission : ''
		if (permission && !tenantStore.canTenant(permission)) {
			next(firstTenantDestination(tenantStore))
			return
		}
	}
  next()
})

const firstTenantDestination = (tenantStore: ReturnType<typeof useTenantStore>) => {
	if (tenantStore.canTenant('tenant.overview.read')) return '/tenant-overview'
	if (tenantStore.canTenant('tenant.resources.read')) return '/resources'
	if (tenantStore.canTenant('tenant.members.read')) return '/tenant-members'
	if (tenantStore.canTenant('tenant.groups.read')) return '/groups'
	if (tenantStore.canTenant('tenant.devices.read')) return '/tenant-member-devices'
	if (tenantStore.canTenant('tenant.grants.read')) return '/access-policies'
	if (tenantStore.canTenant('tenant.sessions.read')) return '/sessions'
	if (tenantStore.canTenant('tenant.audit.read')) return '/tenant-audit'
	if (tenantStore.canTenant('tenant.settings.read')) return '/tenant-settings'
	return '/login'
}

const recoverTenantRoute = async () => {
	if (router.currentRoute.value.meta.scope !== 'tenant') return
	const authStore = useAuthStore()
	const tenantStore = useTenantStore()
	try { await tenantStore.loadContexts(true) } catch { return }
	if (!tenantStore.current) {
		await router.replace(authStore.hasPlatformScope ? '/tenant-switch' : '/login')
		return
	}
	const permission = router.currentRoute.value.meta.permission
	if (typeof permission === 'string' && !tenantStore.canTenant(permission)) {
		await router.replace(firstTenantDestination(tenantStore))
	}
}

window.addEventListener('tenant-context-invalid', () => { recoverTenantRoute() })
window.addEventListener('tenant-context-changed', () => { recoverTenantRoute() })

export default router
