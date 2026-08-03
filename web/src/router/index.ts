import { createRouter, createWebHistory } from 'vue-router'
import type { RouteLocationNormalizedLoaded, RouteRecordRaw } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useTenantStore } from '@/stores/tenant'
import { useWorkspaceStore, workspaceHome } from '@/stores/workspace'
import type { ManagementWorkspace } from '@/api/managementContext'

const storedWorkspace = (): ManagementWorkspace => {
	const value = localStorage.getItem('management_workspace')
	return value === 'tenant' || value === 'provider' || value === 'platform' ? value : 'platform'
}

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
    redirect: '/workspace-entry',
    meta: { requiresAuth: true },
    children: [
		{
			path: 'workspace-entry',
			name: 'WorkspaceEntry',
			component: () => import('@/views/Workspace/Unavailable.vue'),
			meta: { requiresAuth: true, scope: 'any', workspace: 'any', menuDomain: 'any' }
		},
		{
			path: 'workspace-unavailable',
			name: 'WorkspaceUnavailable',
			component: () => import('@/views/Workspace/Unavailable.vue'),
			meta: { requiresAuth: true, scope: 'any', workspace: 'any', menuDomain: 'any' }
		},
		{
			path: 'platform-overview',
			name: 'PlatformOverview',
			component: () => import('@/views/Platform/Overview.vue'),
			meta: { requiresAuth: true, scope: 'platform', workspace: 'platform', permission: 'platform.overview.read', menuDomain: 'platform' }
		},
		{
			path: 'provider-overview',
			name: 'ProviderOverview',
			component: () => import('@/views/Provider/Overview.vue'),
			meta: { requiresAuth: true, scope: 'provider', workspace: 'provider', permission: 'provider.overview.read', menuDomain: 'provider' }
		},
		{
			path: 'provider-technical-resources',
			name: 'ProviderTechnicalResources',
			component: () => import('@/views/Provider/TechnicalResources.vue'),
			meta: { requiresAuth: true, scope: 'provider', workspace: 'provider', permission: 'provider.technical_resources.read', menuDomain: 'provider' }
		},
		{
			path: 'provider-supply-candidates',
			name: 'ProviderSupplyCandidates',
			component: () => import('@/views/Provider/SupplyCandidates.vue'),
			meta: { requiresAuth: true, scope: 'provider', workspace: 'provider', permission: 'provider.resources.read', menuDomain: 'provider' }
		},
		{
			path: 'provider-hosts',
			name: 'ProviderHosts',
			component: () => import('@/views/Provider/Resources.vue'),
			props: { resourceType: 'host' },
			meta: { requiresAuth: true, scope: 'provider', workspace: 'provider', permission: 'provider.resources.read', menuDomain: 'provider' }
		},
		{
			path: 'provider-kubernetes',
			name: 'ProviderKubernetes',
			component: () => import('@/views/Provider/Resources.vue'),
			props: { resourceType: 'kubernetes' },
			meta: { requiresAuth: true, scope: 'provider', workspace: 'provider', permission: 'provider.resources.read', menuDomain: 'provider' }
		},
		{
			path: 'provider-namespaces',
			name: 'ProviderNamespaces',
			component: () => import('@/views/Provider/Namespaces.vue'),
			meta: { requiresAuth: true, scope: 'provider', workspace: 'provider', permission: 'provider.resources.read', menuDomain: 'provider' }
		},
		{
			path: 'provider-memberships',
			name: 'ProviderMemberships',
			component: () => import('@/views/Provider/Memberships.vue'),
			meta: { requiresAuth: true, scope: 'provider', workspace: 'provider', permission: 'provider.memberships.read', menuDomain: 'provider' }
		},
		{
			path: 'provider-audit',
			name: 'ProviderAudit',
			component: () => import('@/views/Provider/Audit.vue'),
			meta: { requiresAuth: true, scope: 'provider', workspace: 'provider', permission: 'provider.audit.read', menuDomain: 'provider' }
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
			path: 'tenant-management-memberships',
			name: 'TenantManagementMemberships',
			component: () => import('@/views/Tenant/ManagementMemberships.vue'),
			meta: { requiresAuth: true, scope: 'tenant', workspace: 'tenant', permission: 'tenant.admins.read', menuDomain: 'tenant' }
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
        component: () => import('@/views/Platform/Organizations.vue'),
		meta: { requiresAuth: true, scope: 'platform', workspace: 'platform', permission: 'platform.organizations.read', menuDomain: 'platform' }
      },
		{
			path: 'tenant-admin-memberships',
			name: 'TenantAdminMemberships',
			component: () => import('@/views/Platform/Memberships.vue'),
			meta: { requiresAuth: true, scope: 'platform', workspace: 'platform', permission: 'platform.memberships.read', menuDomain: 'platform' }
		},
		{
			path: 'platform-admins',
			name: 'PlatformAdmins',
			component: () => import('@/views/Platform/AdminAccounts.vue'),
			meta: { requiresAuth: true, scope: 'platform', workspace: 'platform', permission: 'platform.memberships.read', menuDomain: 'platform' }
		},
		{
			path: 'platform-user-simulations',
			name: 'PlatformUserSimulations',
			component: () => import('@/views/Platform/UserSimulations.vue'),
			meta: { requiresAuth: true, scope: 'platform', workspace: 'platform', permission: 'platform.user_simulations.read', menuDomain: 'platform' }
		},
      {
        path: 'platform-identities',
        name: 'PlatformIdentities',
        component: () => import('@/views/User/List.vue'),
        meta: { requiresAuth: true, scope: 'platform', workspace: 'platform', permission: 'platform.identities.read', menuDomain: 'platform' }
      },
      {
        path: 'platform-identities/:username',
        name: 'PlatformIdentityDetail',
        component: () => import('@/views/User/Detail.vue'),
        meta: { requiresAuth: true, scope: 'platform', workspace: 'platform', permission: 'platform.identities.read', menuDomain: 'platform' }
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
		meta: { requiresAuth: true, scope: 'tenant', workspace: 'tenant', permission: 'tenant.groups.read', menuDomain: 'tenant' }
      },
      {
        path: 'groups/:id/members',
        name: 'GroupMembers',
        component: () => import('@/views/Group/Members.vue'),
		meta: { requiresAuth: true, scope: 'tenant', workspace: 'tenant', permission: 'tenant.groups.read', menuDomain: 'tenant' }
      },
      // 资源发现
      {
        path: 'resources',
        name: 'Resources',
        component: () => import('@/views/Tenant/ResourcesV2.vue'),
		meta: { requiresAuth: true, scope: 'tenant', permission: 'tenant.resources.read' }
      },
      {
        path: 'resources/:id',
        name: 'ResourceDetail',
        component: () => import('@/views/Tenant/ResourceDetailV2.vue'),
		meta: { requiresAuth: true, scope: 'tenant', workspace: 'tenant', permission: 'tenant.resources.read', menuDomain: 'tenant' }
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
			meta: { requiresAuth: true, scope: 'platform', workspace: 'platform', permission: 'platform.resources.read', menuDomain: 'platform' }
		},
		{
			path: 'platform-allocations',
			name: 'PlatformAllocations',
			component: () => import('@/views/Platform/Allocations.vue'),
			meta: { requiresAuth: true, scope: 'platform', workspace: 'platform', permission: 'platform.allocations.read', menuDomain: 'platform' }
		},
		{
			path: 'platform-allocations/:id',
			name: 'PlatformAllocationDetail',
			component: () => import('@/views/Platform/AllocationDetail.vue'),
			meta: { requiresAuth: true, scope: 'platform', workspace: 'platform', permission: 'platform.allocations.read', menuDomain: 'platform' }
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
        component: () => import('@/views/Tenant/GrantsV2.vue'),
		meta: { requiresAuth: true, scope: 'tenant', permission: 'tenant.grants.read' }
      },
      {
        path: 'sessions',
        name: 'Sessions',
        component: () => import('@/views/Tenant/SessionsV2.vue'),
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
        meta: { requiresAuth: true, scope: 'platform', workspace: 'platform', permission: 'platform.audit.read', menuDomain: 'platform' }
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
        meta: { requiresAuth: true, scope: 'platform', workspace: 'platform', permission: 'platform.settings.read', menuDomain: 'platform' }
      }
    ]
  }
]

type BreadcrumbMetaItem = { title: string; path?: string }
type BreadcrumbMeta = BreadcrumbMetaItem[] | ((route: RouteLocationNormalizedLoaded) => BreadcrumbMetaItem[])

const breadcrumbByRouteName: Record<string, BreadcrumbMeta> = {
	WorkspaceUnavailable: route => {
		const workspace = route.query.workspace
		const label = workspace === 'tenant' ? '租户' : workspace === 'provider' ? '资源' : workspace === 'platform' ? '平台' : '工作空间'
		return [{ title: '工作空间' }, { title: `${label}业务不可用` }]
	},
	PlatformOverview: [{ title: '管理员' }, { title: '平台概览' }],
	ProviderOverview: [{ title: '资源业务' }, { title: '资源概览' }],
	ProviderTechnicalResources: [{ title: '资源业务' }, { title: '技术资源' }],
	ProviderSupplyCandidates: [{ title: '资源业务' }, { title: '供给候选' }],
	ProviderHosts: [{ title: '资源业务' }, { title: '主机资源' }],
	ProviderKubernetes: [{ title: '资源业务' }, { title: 'Kubernetes 资源' }],
	ProviderNamespaces: [{ title: '资源业务' }, { title: 'Namespace' }],
	ProviderMemberships: [{ title: '资源治理' }, { title: '管理员' }],
	ProviderAudit: [{ title: '资源治理' }, { title: '审计日志' }],
	TenantOverview: [{ title: '租户业务' }, { title: '租户概览' }],
	TenantSwitch: [{ title: '管理员' }, { title: '租户切换' }],
	TenantMembers: [{ title: '租户业务' }, { title: '成员' }],
	TenantMemberDevices: [{ title: '租户业务' }, { title: '成员设备' }],
	TenantManagementMemberships: [{ title: '租户治理' }, { title: '管理员' }],
	TenantAudit: [{ title: '租户业务' }, { title: '租户审计' }],
	TenantSettings: [{ title: '租户业务' }, { title: '租户设置' }],
	Tenants: [{ title: '组织治理' }, { title: '组织管理' }],
	TenantAdminMemberships: [{ title: '平台治理' }, { title: '管理授权' }],
	PlatformAdmins: [{ title: '平台治理' }, { title: '平台管理账号' }],
	PlatformUserSimulations: [{ title: '平台治理' }, { title: '用户模拟' }],
	PlatformIdentities: [{ title: '平台治理' }, { title: '访问主体目录', path: '/platform-identities' }],
	PlatformIdentityDetail: [{ title: '访问主体目录', path: '/platform-identities' }, { title: '主体详情' }],
	AgentNodes: [{ title: '设备管理' }, { title: '代理设备', path: '/nodes/agents' }],
	DiagnosticDesktopNodes: [{ title: '高级诊断' }, { title: 'Desktop 节点', path: '/diagnostics/desktop-nodes' }],
	DiagnosticNodes: [{ title: '高级诊断' }, { title: '全部节点', path: '/diagnostics/nodes' }],
	NodeDetail: route => {
		if (route.query.source === 'desktop') return [{ title: '高级诊断' }, { title: 'Desktop 节点', path: '/diagnostics/desktop-nodes' }, { title: '节点详情' }]
		if (route.query.source === 'all') return [{ title: '高级诊断' }, { title: '全部节点', path: '/diagnostics/nodes' }, { title: '节点详情' }]
		return [{ title: '设备管理' }, { title: '代理设备', path: '/nodes/agents' }, { title: '节点详情' }]
	},
	Endpoints: [{ title: '终端管理', path: '/endpoints' }],
	EndpointDetail: [{ title: '终端管理', path: '/endpoints' }, { title: '终端详情' }],
	Integrations: [{ title: '基础设施' }, { title: '集成' }],
	Groups: [{ title: '租户业务' }, { title: '成员分组', path: '/groups' }],
	GroupMembers: [{ title: '租户业务' }, { title: '成员分组', path: '/groups' }, { title: '成员管理' }],
	Resources: [{ title: '租户业务' }, { title: '资源目录' }],
	ResourceDetail: [{ title: '租户业务' }, { title: '资源目录', path: '/resources' }, { title: '资源详情' }],
	ResourceCandidates: [{ title: '资源治理' }, { title: '发现候选' }],
	PlatformResources: [{ title: '资源治理' }, { title: '全局资源目录' }],
	PlatformAllocations: [{ title: '资源治理' }, { title: '资源分配', path: '/platform-allocations' }],
	PlatformAllocationDetail: [{ title: '资源治理' }, { title: '资源分配', path: '/platform-allocations' }, { title: '分配详情' }],
	LegacyInventory: [{ title: '资源治理' }, { title: '存量认领' }],
	AccessPolicies: [{ title: '租户业务' }, { title: '访问授权' }],
	Sessions: [{ title: '租户业务' }, { title: '访问会话' }],
	Domains: [{ title: '高级诊断' }, { title: '域名管理', path: '/domains' }],
	ACLServices: [{ title: '授权管理' }, { title: '服务授权', path: '/acl/services' }],
	ACLServiceDetail: [{ title: '服务授权', path: '/acl/services' }, { title: '授权详情' }],
	ACLUsers: [{ title: '授权管理' }, { title: '用户授权', path: '/acl/users' }],
	ACLUserDetail: [{ title: '用户授权', path: '/acl/users' }, { title: '授权详情' }],
	ACLGroups: [{ title: '授权管理' }, { title: '分组授权', path: '/acl/groups' }],
	ACLGroupDetail: [{ title: '分组授权', path: '/acl/groups' }, { title: '授权详情' }],
	ACLSSH: [{ title: '高级诊断' }, { title: 'SSH 授权', path: '/acl/ssh' }],
	ACLSSHDetail: [{ title: 'SSH 授权', path: '/acl/ssh' }, { title: '授权详情' }],
	ACLK8S: [{ title: '授权管理' }, { title: 'Kubernetes 授权', path: '/acl/k8s' }],
	ACLK8SDetail: [{ title: 'Kubernetes 授权', path: '/acl/k8s' }, { title: '授权详情' }],
	ACLK8SService: [{ title: '授权管理' }, { title: 'Kubernetes Service 授权', path: '/acl/k8s-service' }],
	ACLK8SServiceDetail: [{ title: 'Kubernetes Service 授权', path: '/acl/k8s-service' }, { title: '授权详情' }],
	TunnelUsers: [{ title: '隧道管理' }, { title: 'User 管理' }],
	TunnelNodes: [{ title: '隧道管理' }, { title: 'Node 管理' }],
	TunnelACL: [{ title: '高级诊断' }, { title: 'ACL 管理' }],
	TunnelSSH: [{ title: '隧道管理' }, { title: 'SSH 策略' }],
	PlatformAudit: [{ title: '平台治理' }, { title: '平台审计' }],
	DiagnosticOperationAudit: [{ title: '高级诊断' }, { title: '连接操作审计' }],
	SystemConfig: [{ title: '平台治理' }, { title: '系统配置' }]
}

const normalizeProtectedRouteMetadata = (records: RouteRecordRaw[]) => {
	for (const route of records) {
		if (route.meta?.requiresAuth !== false) {
			const routeName = typeof route.name === 'string' ? route.name : ''
			const workspace = route.meta?.workspace || route.meta?.scope || 'platform'
			route.meta = {
				...route.meta,
				scope: route.meta?.scope || workspace,
				workspace,
				menuDomain: route.meta?.menuDomain || workspace,
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
	const workspaceStore = useWorkspaceStore()

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
	try { await workspaceStore.loadContexts() } catch { next(false); return }
	if (to.name === 'WorkspaceEntry') {
		const initialWorkspace = workspaceStore.currentWorkspace || storedWorkspace()
		next(workspaceStore.hasContext(initialWorkspace)
			? workspaceHome(initialWorkspace)
			: { name: 'WorkspaceUnavailable', query: { workspace: initialWorkspace }, replace: true })
		return
	}

	if (to.name === 'WorkspaceUnavailable') {
		const requested = typeof to.query.workspace === 'string' ? to.query.workspace : workspaceStore.currentWorkspace
		if (requested === 'tenant' || requested === 'provider' || requested === 'platform') workspaceStore.activateWorkspace(requested)
		next()
		return
	}

	const workspace = (to.meta.workspace || to.meta.scope || 'platform') as ManagementWorkspace
	workspaceStore.activateWorkspace(workspace)
	if (!workspaceStore.hasContext(workspace)) {
		next({ name: 'WorkspaceUnavailable', query: { workspace }, replace: true })
		return
	}

	if (workspace === 'tenant') {
		const selectedTenantId = workspaceStore.selectedContextId('tenant')
		if (selectedTenantId && tenantStore.tenantId !== selectedTenantId) tenantStore.syncTenantContext(selectedTenantId)
		try { await tenantStore.loadContexts() } catch { next(false); return }
		if (!tenantStore.current) {
			next({ name: 'WorkspaceUnavailable', query: { workspace: 'tenant' }, replace: true })
			return
		}
	}
	const permission = typeof to.meta.permission === 'string' ? to.meta.permission : ''
	if (permission && !workspaceStore.can(permission)) {
		if (workspace === 'tenant') {
			next(firstTenantDestination(tenantStore, workspaceStore))
		} else if (workspace === 'provider') {
			next(firstProviderDestination(workspaceStore))
		} else if (workspace === 'platform') {
			next(firstPlatformDestination(workspaceStore))
		} else {
			next({ name: 'WorkspaceUnavailable', query: { workspace }, replace: true })
		}
		return
	}
  next()
})

const firstProviderDestination = (workspaceStore = useWorkspaceStore()) => {
	if (workspaceStore.can('provider.overview.read')) return '/provider-overview'
	if (workspaceStore.can('provider.technical_resources.read')) return '/provider-technical-resources'
	if (workspaceStore.can('provider.resources.read')) return '/provider-supply-candidates'
	if (workspaceStore.can('provider.memberships.read')) return '/provider-memberships'
	if (workspaceStore.can('provider.audit.read')) return '/provider-audit'
	return { name: 'WorkspaceUnavailable', query: { workspace: 'provider' } }
}

const firstPlatformDestination = (workspaceStore = useWorkspaceStore()) => {
	if (workspaceStore.can('platform.overview.read')) return '/platform-overview'
	if (workspaceStore.can('platform.organizations.read')) return '/tenants'
	if (workspaceStore.can('platform.memberships.read')) return '/tenant-admin-memberships'
	if (workspaceStore.can('platform.resources.read')) return '/platform-resources'
	if (workspaceStore.can('platform.allocations.read')) return '/platform-allocations'
	if (workspaceStore.can('platform.identities.read')) return '/platform-identities'
	if (workspaceStore.can('platform.user_simulations.read')) return '/platform-user-simulations'
	if (workspaceStore.can('platform.audit.read')) return '/platform-audit'
	if (workspaceStore.can('platform.settings.read')) return '/system/config'
	return { name: 'WorkspaceUnavailable', query: { workspace: 'platform' } }
}

const firstTenantDestination = (
	tenantStore: ReturnType<typeof useTenantStore>,
	workspaceStore = useWorkspaceStore()
) => {
	const can = (permission: string) => workspaceStore.can(permission) && tenantStore.canTenant(permission)
	if (can('tenant.overview.read')) return '/tenant-overview'
	if (can('tenant.resources.read')) return '/resources'
	if (can('tenant.members.read')) return '/tenant-members'
	if (can('tenant.groups.read')) return '/groups'
	if (can('tenant.devices.read')) return '/tenant-member-devices'
	if (can('tenant.admins.read')) return '/tenant-management-memberships'
	if (can('tenant.grants.read')) return '/access-policies'
	if (can('tenant.sessions.read')) return '/sessions'
	if (can('tenant.audit.read')) return '/tenant-audit'
	if (can('tenant.settings.read')) return '/tenant-settings'
	return { name: 'WorkspaceUnavailable', query: { workspace: 'tenant' } }
}

const recoverTenantRoute = async () => {
	if (router.currentRoute.value.meta.scope !== 'tenant') return
	const tenantStore = useTenantStore()
	const workspaceStore = useWorkspaceStore()
	try { await workspaceStore.loadContexts(true) } catch { return }
	try { await tenantStore.loadContexts(true) } catch { return }
	if (!tenantStore.current) {
		await router.replace({ name: 'WorkspaceUnavailable', query: { workspace: 'tenant' } })
		return
	}
	const permission = router.currentRoute.value.meta.permission
	if (typeof permission === 'string' && !tenantStore.canTenant(permission)) {
		await router.replace(firstTenantDestination(tenantStore, workspaceStore))
	}
}

window.addEventListener('tenant-context-invalid', () => { recoverTenantRoute() })
window.addEventListener('tenant-context-changed', () => { recoverTenantRoute() })
window.addEventListener('management-context-changed', () => { recoverTenantRoute() })

export default router
