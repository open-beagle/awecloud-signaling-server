<template>
  <div class="sidebar">
		<div v-if="tenantStore.current" class="tenant-context" :class="{ collapsed: appStore.sidebarCollapsed }">
			<el-icon><OfficeBuilding /></el-icon>
			<div v-if="!appStore.sidebarCollapsed" class="tenant-context-copy">
				<strong>{{ tenantStore.current.tenant_name }}</strong>
				<span>{{ tenantRoleLabel }}</span>
			</div>
		</div>

    <el-menu :default-active="activeMenu" :collapse="appStore.sidebarCollapsed" :collapse-transition="false" router unique-opened>
      <template v-if="tenantStore.current">
				<el-menu-item v-if="tenantStore.canTenant('tenant.overview.read')" index="/tenant-overview"><el-icon><DataAnalysis /></el-icon><template #title>租户概览</template></el-menu-item>

        <div v-if="!appStore.sidebarCollapsed && hasTenantOrganizationMenu" class="nav-section">组织</div>
        <el-menu-item v-if="tenantStore.canTenant('tenant.members.read')" index="/tenant-members"><el-icon><UserFilled /></el-icon><template #title>成员</template></el-menu-item>
        <el-menu-item v-if="tenantStore.canTenant('tenant.groups.read')" index="/groups"><el-icon><Collection /></el-icon><template #title>成员分组</template></el-menu-item>
				<el-menu-item v-if="tenantStore.canTenant('tenant.devices.read')" index="/tenant-member-devices"><el-icon><Monitor /></el-icon><template #title>成员设备</template></el-menu-item>

        <div v-if="!appStore.sidebarCollapsed && hasTenantResourceMenu" class="nav-section">资源与访问</div>
        <el-menu-item v-if="tenantStore.canTenant('tenant.resources.read')" index="/resources"><el-icon><Box /></el-icon><template #title>资源目录</template></el-menu-item>
        <el-menu-item v-if="tenantStore.canTenant('tenant.grants.read')" index="/access-policies"><el-icon><Key /></el-icon><template #title>访问策略</template></el-menu-item>
        <el-menu-item v-if="tenantStore.canTenant('tenant.sessions.read')" index="/sessions"><el-icon><Clock /></el-icon><template #title>活动会话</template></el-menu-item>

				<div v-if="!appStore.sidebarCollapsed && hasTenantGovernanceMenu" class="nav-section">租户治理</div>
				<el-menu-item v-if="tenantStore.canTenant('tenant.audit.read')" index="/tenant-audit"><el-icon><DocumentChecked /></el-icon><template #title>租户审计</template></el-menu-item>
				<el-menu-item v-if="tenantStore.canTenant('tenant.settings.read')" index="/tenant-settings"><el-icon><Setting /></el-icon><template #title>租户设置</template></el-menu-item>
      </template>

      <div v-else-if="!tenantStore.loading && !appStore.sidebarCollapsed" class="tenant-empty">
        <strong>无租户管理权限</strong>
        <span>当前身份没有可管理的租户。</span>
      </div>

      <template v-if="authStore.hasPlatformScope">
        <div class="scope-divider" />
				<el-menu-item index="/platform-overview"><el-icon><DataAnalysis /></el-icon><template #title>平台概览</template></el-menu-item>

        <div v-if="!appStore.sidebarCollapsed" class="nav-section">租户治理</div>
        <el-menu-item index="/tenant-switch"><el-icon><Switch /></el-icon><template #title>租户切换</template></el-menu-item>
        <el-menu-item index="/tenants"><el-icon><OfficeBuilding /></el-icon><template #title>租户管理</template></el-menu-item>
				<el-menu-item index="/tenant-admin-memberships"><el-icon><Avatar /></el-icon><template #title>租户授权</template></el-menu-item>

        <div v-if="!appStore.sidebarCollapsed" class="nav-section">资源治理</div>
        <el-menu-item index="/resource-candidates"><el-icon><Search /></el-icon><template #title>发现候选</template></el-menu-item>
        <el-menu-item index="/legacy-inventory"><el-icon><Finished /></el-icon><template #title>存量认领</template></el-menu-item>
				<el-menu-item index="/platform-resources"><el-icon><Box /></el-icon><template #title>全局资源</template></el-menu-item>

        <div v-if="!appStore.sidebarCollapsed" class="nav-section">基础设施</div>
        <el-menu-item index="/nodes/agents"><el-icon><Monitor /></el-icon><template #title>Agent</template></el-menu-item>
        <el-menu-item index="/endpoints"><el-icon><Connection /></el-icon><template #title>Endpoint</template></el-menu-item>
        <el-menu-item index="/infrastructure/integrations"><el-icon><Link /></el-icon><template #title>集成</template></el-menu-item>

        <div v-if="!appStore.sidebarCollapsed" class="nav-section">平台治理</div>
				<el-menu-item index="/platform-admins"><el-icon><UserFilled /></el-icon><template #title>管理账号</template></el-menu-item>
				<el-menu-item index="/platform-identities"><el-icon><User /></el-icon><template #title>访问主体</template></el-menu-item>
        <el-menu-item index="/platform-audit"><el-icon><Document /></el-icon><template #title>平台审计</template></el-menu-item>
        <el-menu-item index="/system/config"><el-icon><Setting /></el-icon><template #title>系统设置</template></el-menu-item>
        <el-sub-menu index="diagnostics">
          <template #title><el-icon><Tools /></el-icon><span>高级诊断</span></template>
          <el-menu-item index="/domains">连接入口</el-menu-item>
          <el-menu-item index="/diagnostics/desktop-nodes">Desktop 节点</el-menu-item>
          <el-menu-item index="/diagnostics/nodes">全部节点</el-menu-item>
          <el-menu-item index="/diagnostics/operation-audit">操作审计</el-menu-item>
          <el-menu-item index="/acl/ssh">旧版授权</el-menu-item>
          <el-menu-item index="/tunnel/acl">网络策略</el-menu-item>
        </el-sub-menu>
      </template>
    </el-menu>
    <button class="sidebar-footer" type="button" :aria-label="appStore.sidebarCollapsed ? '展开导航' : '折叠导航'" @click="toggleSidebar">
      <el-icon class="collapse-icon"><Fold v-if="!appStore.sidebarCollapsed" /><Expand v-else /></el-icon>
    </button>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, watch } from 'vue'
import { Avatar, Box, Clock, Collection, Connection, DataAnalysis, Document, DocumentChecked, Expand, Finished, Fold, Key, Link, Monitor, OfficeBuilding, Search, Setting, Switch, Tools, User, UserFilled } from '@element-plus/icons-vue'
import { useRoute } from 'vue-router'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { useTenantStore } from '@/stores/tenant'

const route = useRoute()
const appStore = useAppStore()
const authStore = useAuthStore()
const tenantStore = useTenantStore()

const tenantRoleLabel = computed(() => ({ tenant_admin: '租户管理员', security_auditor: '安全审计员', tenant_viewer: '租户观察员' }[tenantStore.tenantRole] || tenantStore.tenantRole))
const hasTenantOrganizationMenu = computed(() => ['tenant.members.read', 'tenant.groups.read', 'tenant.devices.read'].some(permission => tenantStore.canTenant(permission)))
const hasTenantResourceMenu = computed(() => ['tenant.resources.read', 'tenant.grants.read', 'tenant.sessions.read'].some(permission => tenantStore.canTenant(permission)))
const hasTenantGovernanceMenu = computed(() => ['tenant.audit.read', 'tenant.settings.read'].some(permission => tenantStore.canTenant(permission)))
const activeMenu = computed(() => {
	if (route.path.startsWith('/tenant-overview')) return '/tenant-overview'
	if (route.path.startsWith('/platform-overview')) return '/platform-overview'
	if (route.path.startsWith('/platform-admins')) return '/platform-admins'
	if (route.path.startsWith('/platform-identities')) return '/platform-identities'
	if (route.path.startsWith('/diagnostics/desktop-nodes')) return '/diagnostics/desktop-nodes'
	if (route.path.startsWith('/diagnostics/nodes')) return '/diagnostics/nodes'
	if (route.path.startsWith('/diagnostics/operation-audit')) return '/diagnostics/operation-audit'
	if (route.path.startsWith('/platform-audit')) return '/platform-audit'
	if (route.path.startsWith('/platform-resources')) return '/platform-resources'
  if (route.path.startsWith('/resources')) return '/resources'
  if (route.path.startsWith('/tenant-members')) return '/tenant-members'
	if (route.path.startsWith('/tenant-member-devices')) return '/tenant-member-devices'
	if (route.path.startsWith('/tenant-audit')) return '/tenant-audit'
	if (route.path.startsWith('/tenant-settings')) return '/tenant-settings'
  if (route.path.startsWith('/resource-candidates')) return '/resource-candidates'
  if (route.path.startsWith('/legacy-inventory')) return '/legacy-inventory'
  if (route.path.startsWith('/nodes')) return '/nodes/agents'
  if (route.path.startsWith('/acl') || route.path.startsWith('/tunnel')) return '/acl/ssh'
  return route.path
})
const toggleSidebar = () => appStore.toggleSidebar()

const handleContextInvalid = () => {
	tenantStore.clearTenantState()
	tenantStore.loadContexts(true).catch(() => undefined)
}
const handleSessionCleared = () => tenantStore.reset()
onMounted(() => {
	tenantStore.loadContexts().catch(() => undefined)
	window.addEventListener('tenant-context-invalid', handleContextInvalid)
	window.addEventListener('admin-session-cleared', handleSessionCleared)
})
onBeforeUnmount(() => {
	window.removeEventListener('tenant-context-invalid', handleContextInvalid)
	window.removeEventListener('admin-session-cleared', handleSessionCleared)
})
watch(() => route.fullPath, () => appStore.closeMobileSidebar())
</script>

<style scoped>
.sidebar { height: 100%; display: flex; flex-direction: column; overflow: hidden; background: #fff; border-right: 1px solid var(--border-light); }
.tenant-context { display: flex; align-items: center; gap: 10px; margin: 10px; padding: 10px; border: 1px solid var(--border-light); border-radius: 6px; background: #f8fafc; }
.tenant-context.collapsed { justify-content: center; margin: 8px 6px; padding: 9px 0; }
.tenant-context > .el-icon { flex: 0 0 auto; color: var(--primary-color); font-size: 18px; }
.tenant-context-copy { min-width: 0; display: flex; flex-direction: column; gap: 2px; }
.tenant-context-copy strong { overflow: hidden; color: var(--text-primary); font-size: 13px; line-height: 18px; text-overflow: ellipsis; white-space: nowrap; }
.tenant-context-copy span { color: var(--text-secondary); font-size: 11px; line-height: 16px; }
.scope-divider { height: 1px; margin: 12px 10px 4px; background: var(--border-light); }
.tenant-empty { display: flex; flex-direction: column; gap: 4px; margin: 12px 10px; padding: 12px; border: 1px solid #d9e1ec; border-radius: 6px; background: #f8fafc; }
.tenant-empty strong { color: var(--text-primary); font-size: 12px; }
.tenant-empty span { color: var(--text-secondary); font-size: 11px; line-height: 17px; }
.el-menu { flex: 1; overflow-y: auto; overflow-x: hidden; border-right: 0; background: #fff !important; }
.el-menu::-webkit-scrollbar { width: 0; }
.nav-section { margin-top: 10px; padding: 0 20px 4px; color: var(--text-secondary); font-size: 11px; font-weight: 650; line-height: 18px; }
:deep(.el-menu-item), :deep(.el-sub-menu__title) { height: 40px; line-height: 40px; margin: 2px 10px; padding: 0 12px !important; border-radius: 5px; background: #fff !important; color: var(--text-regular) !important; }
:deep(.el-menu-item .el-icon), :deep(.el-sub-menu__title .el-icon) { width: 18px; height: 18px; margin-right: 10px !important; font-size: 17px; }
:deep(.el-menu-item:hover), :deep(.el-sub-menu__title:hover) { background: var(--sidebar-hover-bg) !important; }
:deep(.el-menu-item.is-active) { background: var(--sidebar-active-bg) !important; color: var(--primary-color) !important; font-weight: 650; }
:deep(.el-sub-menu .el-menu-item) { height: 38px; line-height: 38px; margin-left: 30px; padding-left: 17px !important; background: #fafcfb !important; font-size: 13px; }
:deep(.el-menu--collapse .el-menu-item), :deep(.el-menu--collapse .el-sub-menu__title) { margin-right: 6px; margin-left: 6px; padding: 0 !important; justify-content: center; }
:deep(.el-menu--collapse .el-menu-item .el-icon), :deep(.el-menu--collapse .el-sub-menu__title .el-icon) { margin-right: 0 !important; }
.sidebar-footer { height: 48px; display: flex; align-items: center; justify-content: center; flex-shrink: 0; width: 100%; border: 0; border-top: 1px solid var(--border-light); background: #fff; color: var(--text-secondary); cursor: pointer; }
.sidebar-footer:hover { background: var(--bg-page); color: var(--primary-color); }
.sidebar-footer:focus-visible { outline: 2px solid var(--primary-color); outline-offset: -2px; }
.collapse-icon { font-size: 19px; }
@media (max-width: 768px) { .sidebar-footer { display: none; } }
</style>
